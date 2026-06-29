package main

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	listenAddr = ":8080"
	mailBase   = "/mnt/data/mail"
	dataBase   = "/mnt/data/mail"
	secret     = "220961"
	usersFile  = "/etc/localbox/users.conf"
	cookieName = "localbox_session"
)

var safeLabel = regexp.MustCompile(`^[a-z0-9._-]+$`)

type Session struct {
	User    string
	Domain  string
	Expires time.Time
}

var (
	sessions   = map[string]*Session{}
	sessionsMu sync.RWMutex
)

func newSession(user, domain string) string {
	b := make([]byte, 32)
	rand.Read(b)
	token := base64.URLEncoding.EncodeToString(b)
	sessionsMu.Lock()
	sessions[token] = &Session{
		User:    user,
		Domain:  domain,
		Expires: time.Now().Add(24 * time.Hour),
	}
	sessionsMu.Unlock()
	return token
}

func getSession(r *http.Request) *Session {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	sessionsMu.RLock()
	s := sessions[c.Value]
	sessionsMu.RUnlock()
	if s == nil || time.Now().After(s.Expires) {
		return nil
	}
	return s
}

func deleteSession(r *http.Request) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return
	}
	sessionsMu.Lock()
	delete(sessions, c.Value)
	sessionsMu.Unlock()
}

type User struct {
	Username string
	Domain   string
	Hash     string
}

func loadUsers() ([]User, error) {
	data, err := os.ReadFile(usersFile)
	if err != nil {
		return nil, err
	}
	var users []User
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		users = append(users, User{
			Username: parts[0],
			Domain:   parts[1],
			Hash:     parts[2],
		})
	}
	return users, nil
}

func hashPassword(password string) string {
	h := sha512.Sum512([]byte(password))
	return base64.StdEncoding.EncodeToString(h[:])
}

func authenticate(username, domain, password string) bool {
	users, err := loadUsers()
	if err != nil {
		log.Printf("error loading users: %v", err)
		return false
	}
	hash := hashPassword(password)
	for _, u := range users {
		if u.Username == username && u.Domain == domain && u.Hash == hash {
			return true
		}
	}
	return false
}

type Message struct {
	ID          string
	From        string
	To          string
	Subject     string
	Date        string
	Body        string
	IsNew       bool
	Attachments []string
}

var htmlTagRegex = regexp.MustCompile(`<[^>]+>`)

func stripHTML(s string) string {
	s = htmlTagRegex.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&#39;", "'")
	return strings.TrimSpace(s)
}

func readPart(mediaType, encoding string, r io.Reader) (string, error) {
	var reader io.Reader = r
	switch strings.ToLower(encoding) {
	case "quoted-printable":
		reader = quotedprintable.NewReader(r)
	case "base64":
		reader = base64.NewDecoder(base64.StdEncoding, r)
	}
	b, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	text := string(b)
	if mediaType == "text/html" {
		text = stripHTML(text)
	}
	return text, nil
}

func parseMultipart(boundary string, r io.Reader) (string, []string, error) {
	mr := multipart.NewReader(r, boundary)
	var plainText, htmlText string
	var attachments []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		ct := p.Header.Get("Content-Type")
		cte := p.Header.Get("Content-Transfer-Encoding")
		mediaType, params, _ := mime.ParseMediaType(ct)
		disposition, dParams, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		if disposition == "attachment" {
			name := dParams["filename"]
			if name == "" {
				name = "unnamed"
			}
			attachments = append(attachments, name)
			continue
		}
		if strings.HasPrefix(mediaType, "multipart/") {
			text, atts, _ := parseMultipart(params["boundary"], p)
			if plainText == "" {
				plainText = text
			}
			attachments = append(attachments, atts...)
			continue
		}
		text, _ := readPart(mediaType, cte, p)
		switch mediaType {
		case "text/plain":
			plainText = text
		case "text/html":
			if htmlText == "" {
				htmlText = stripHTML(text)
			}
		}
	}
	if plainText != "" {
		return plainText, attachments, nil
	}
	return htmlText, attachments, nil
}

func parseBody(contentType, transferEncoding string, r io.Reader) (string, []string, error) {
	if contentType == "" {
		b, err := io.ReadAll(r)
		return string(b), nil, err
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		b, err := io.ReadAll(r)
		return string(b), nil, err
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		return parseMultipart(params["boundary"], r)
	}
	body, err := readPart(mediaType, transferEncoding, r)
	return body, nil, err
}

func parseMessage(path string, isNew bool) (*Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m, err := mail.ReadMessage(f)
	if err != nil {
		return nil, err
	}
	msg := &Message{
		ID:      filepath.Base(path),
		IsNew:   isNew,
		From:    m.Header.Get("From"),
		To:      m.Header.Get("To"),
		Subject: m.Header.Get("Subject"),
		Date:    m.Header.Get("Date"),
	}
	body, attachments, err := parseBody(
		m.Header.Get("Content-Type"),
		m.Header.Get("Content-Transfer-Encoding"),
		m.Body,
	)
	if err != nil {
		msg.Body = "(could not parse body)"
	} else {
		msg.Body = body
		msg.Attachments = attachments
	}
	return msg, nil
}

func listMessages(maildir string) ([]*Message, error) {
	var messages []*Message
	for _, dir := range []string{"new", "cur"} {
		isNew := dir == "new"
		path := filepath.Join(maildir, dir)
		entries, err := os.ReadDir(path)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			msg, err := parseMessage(filepath.Join(path, e.Name()), isNew)
			if err != nil {
				log.Printf("parse error: %v", err)
				continue
			}
			messages = append(messages, msg)
		}
	}
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].ID > messages[j].ID
	})
	return messages, nil
}

func markAsRead(maildir, id string) {
	src := filepath.Join(maildir, "new", id)
	dst := filepath.Join(maildir, "cur", id)
	os.Rename(src, dst)
}

type MailPayload struct {
	To   string `json:"to"`
	From string `json:"from"`
	Raw  string `json:"raw"`
}

func attachmentHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	messageID := r.URL.Query().Get("id")
	filename := r.URL.Query().Get("file")
	if messageID == "" || filename == "" {
		http.Error(w, "Missing parameters", http.StatusBadRequest)
		return
	}
	// sanitize
	filename = filepath.Base(filename)
	messageID = filepath.Base(messageID)
	//	path := filepath.Join(dataBase, s.Domain, s.User, "files", "attachments", messageID, filename)
	path := filepath.Join(dataBase, s.Domain, s.User, "attachments", messageID, filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.Error(w, "Attachment not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	http.ServeFile(w, r, path)
}

func saveAttachments(domain, user, messageID string, r io.Reader, contentType string) error {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil
	}
	mr := multipart.NewReader(r, params["boundary"])
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		disposition, dParams, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		if disposition != "attachment" {
			continue
		}
		filename := dParams["filename"]
		if filename == "" {
			filename = "unnamed"
		}
		// sanitize filename
		filename = filepath.Base(filename)
		attDir := filepath.Join(dataBase, domain, user, "attachments", messageID)
		//	attDir := filepath.Join(dataBase, domain, user, "files", "attachments", messageID)
		if err := os.MkdirAll(attDir, 0770); err != nil {
			return err
		}
		cte := p.Header.Get("Content-Transfer-Encoding")
		var reader io.Reader = p
		if strings.ToLower(cte) == "base64" {
			reader = base64.NewDecoder(base64.StdEncoding, p)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(attDir, filename), data, 0660); err != nil {
			return err
		}
		log.Printf("saved attachment: %s/%s", messageID, filename)
	}
	return nil
}

func ingestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Ingest-Secret") != secret {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 25<<20))
	if err != nil {
		http.Error(w, "Read error", http.StatusInternalServerError)
		return
	}
	var payload MailPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	parts := strings.SplitN(strings.ToLower(payload.To), "@", 2)
	if len(parts) != 2 {
		http.Error(w, "Invalid to address", http.StatusBadRequest)
		return
	}
	user, domain := parts[0], parts[1]
	if !safeLabel.MatchString(user) || !safeLabel.MatchString(domain) {
		http.Error(w, "Invalid address characters", http.StatusBadRequest)
		return
	}
	maildirBase := filepath.Join(mailBase, domain, user, "Maildir")
	for _, dir := range []string{"new", "cur", "tmp"} {
		if err := os.MkdirAll(filepath.Join(maildirBase, dir), 0770); err != nil {
			log.Printf("mkdir error: %v", err)
			http.Error(w, "Storage error", http.StatusInternalServerError)
			return
		}
	}
	filename := fmt.Sprintf("%d.%s.localmail", time.Now().UnixNano(), user)
	path := filepath.Join(maildirBase, "new", filename)
	if err := os.WriteFile(path, []byte(payload.Raw), 0660); err != nil {
		log.Printf("write error: %v", err)
		http.Error(w, "Write error", http.StatusInternalServerError)
		return
	}

	// extract and save attachments
	msgReader := strings.NewReader(payload.Raw)
	m, err := mail.ReadMessage(msgReader)
	if err == nil {
		saveAttachments(domain, user, filename, m.Body, m.Header.Get("Content-Type"))
	}

	log.Printf("delivered: %s -> %s", payload.From, payload.To)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tmpl := template.Must(template.ParseFiles("templates/login.html"))
		tmpl.Execute(w, nil)
		return
	}
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	domain := strings.ToLower(strings.TrimSpace(r.FormValue("domain")))
	password := r.FormValue("password")
	if authenticate(username, domain, password) {
		token := newSession(username, domain)
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    token,
			Path:     "/",
			Expires:  time.Now().Add(24 * time.Hour),
			HttpOnly: true,
		})
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	tmpl := template.Must(template.ParseFiles("templates/login.html"))
	tmpl.Execute(w, map[string]string{
		"Error":    "Invalid credentials",
		"Username": username,
		"Domain":   domain,
	})
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	deleteSession(r)
	http.SetCookie(w, &http.Cookie{
		Name:    cookieName,
		Value:   "",
		Path:    "/",
		Expires: time.Now().Add(-1 * time.Hour),
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func inboxHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	maildir := filepath.Join(mailBase, s.Domain, s.User, "Maildir")
	messages, err := listMessages(maildir)
	if err != nil {
		http.Error(w, "Error reading mailbox", http.StatusInternalServerError)
		return
	}
	data := struct {
		Messages []*Message
		User     string
		Domain   string
	}{messages, s.User, s.Domain}
	tmpl := template.Must(template.ParseFiles("templates/inbox.html"))
	tmpl.Execute(w, data)
}

func messageHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	maildir := filepath.Join(mailBase, s.Domain, s.User, "Maildir")
	path := filepath.Join(maildir, "new", id)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		path = filepath.Join(maildir, "cur", id)
	}
	msg, err := parseMessage(path, false)
	if err != nil {
		http.Error(w, "Message not found", http.StatusNotFound)
		return
	}
	markAsRead(maildir, id)
	tmpl := template.Must(template.ParseFiles("templates/message.html"))
	tmpl.Execute(w, msg)
}

func main() {
	log.Printf("localbox-mail starting on %s", listenAddr)
	if err := os.MkdirAll("/etc/localbox", 0755); err != nil {
		log.Fatalf("could not create config dir: %v", err)
	}
	http.HandleFunc("/attachment", attachmentHandler)
	http.HandleFunc("/ingest", ingestHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/logout", logoutHandler)
	http.HandleFunc("/message", messageHandler)
	http.HandleFunc("/", inboxHandler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
