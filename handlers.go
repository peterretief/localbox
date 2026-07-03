package main

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LoginPageData struct {
	Error    string
	Username string
	Domain   string
	Domains  []string
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tmpl := template.Must(template.ParseFiles("templates/login.html"))
		tmpl.Execute(w, LoginPageData{
			Domain:  lastLoginDomain(r),
			Domains: loginDomains(),
		})
		return
	}

	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	domain := strings.ToLower(strings.TrimSpace(r.FormValue("domain")))
	password := r.FormValue("password")
	rememberLoginDomain(w, domain)
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
	tmpl.Execute(w, LoginPageData{
		Error:    "Invalid credentials",
		Username: username,
		Domain:   domain,
		Domains:  loginDomains(),
	})
}

func loginDomains() []string {
	domains, err := loadDomains()
	if err != nil {
		return nil
	}
	return domains
}

func lastLoginDomain(r *http.Request) string {
	c, err := r.Cookie("localbox_login_domain")
	if err != nil {
		return ""
	}
	return c.Value
}

func rememberLoginDomain(w http.ResponseWriter, domain string) {
	if domain == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "localbox_login_domain",
		Value:    domain,
		Path:     "/login",
		Expires:  time.Now().Add(365 * 24 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
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

type PasswordPageData struct {
	Error   string
	Success string
}

func passwordHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	if r.Method == http.MethodGet {
		tmpl := template.Must(template.ParseFiles("templates/password.html"))
		tmpl.Execute(w, PasswordPageData{})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	data := PasswordPageData{}
	switch {
	case len(newPassword) < 8:
		data.Error = "New password must be at least 8 characters."
	case newPassword != confirmPassword:
		data.Error = "New passwords do not match."
	default:
		if err := changePassword(s.User, s.Domain, currentPassword, newPassword); err != nil {
			data.Error = err.Error()
		} else {
			data.Success = "Password updated."
		}
	}

	tmpl := template.Must(template.ParseFiles("templates/password.html"))
	tmpl.Execute(w, data)
}

func inboxHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	view := r.URL.Query().Get("view")
	maildir := maildirPath(s.Domain, s.User)
	folder := ""
	var messages []*Message
	var err error
	if view == "sent" {
		folder = "sent"
		messages, err = listMessagesInFolder(sentMaildirPath(s.Domain, s.User), folder)
		if err != nil {
			http.Error(w, "Error reading mailbox", http.StatusInternalServerError)
			return
		}
		legacyMessages, legacyErr := listMessagesInFolder(legacySentMaildirPath(s.Domain, s.User), folder)
		if legacyErr == nil {
			messages = append(messages, legacyMessages...)
		}
	} else {
		messages, err = listMessagesInFolder(maildir, folder)
		if err != nil {
			http.Error(w, "Error reading mailbox", http.StatusInternalServerError)
			return
		}
	}

	data := newInboxView(messages, s.User, s.Domain, r)

	tmpl := template.Must(template.ParseFiles("templates/inbox.html"))
	tmpl.Execute(w, data)
}

func composeHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	data := ComposePageData{
		To:      r.URL.Query().Get("to"),
		Subject: r.URL.Query().Get("subject"),
		User:    s.User,
		Domain:  s.Domain,
	}

	if r.Method == http.MethodPost {
		data.To = r.FormValue("to")
		data.Subject = r.FormValue("subject")
		data.Body = r.FormValue("body")
		attachment, err := composeAttachment(r)
		if err != nil {
			data.Error = err.Error()
		} else {
			result, err := sendMailMessage(s.User, s.Domain, data.To, data.Subject, data.Body, attachment)
			if err != nil {
				data.Error = err.Error()
			} else {
				data.Success = result
				data.To = ""
				data.Subject = ""
				data.Body = ""
			}
		}
	} else if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl := template.Must(template.ParseFiles("templates/compose.html"))
	tmpl.Execute(w, data)
}

func composeAttachment(r *http.Request) (*OutgoingAttachment, error) {
	file, header, err := r.FormFile("attachment")
	if err == http.ErrMissingFile {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if header.Size > maxComposeAttachmentBytes {
		return nil, fmt.Errorf("attachment must be 10 MB or smaller")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxComposeAttachmentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxComposeAttachmentBytes {
		return nil, fmt.Errorf("attachment must be 10 MB or smaller")
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return &OutgoingAttachment{
		Filename:    filepath.Base(header.Filename),
		ContentType: contentType,
		Data:        data,
	}, nil
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

	maildir := maildirPath(s.Domain, s.User)
	folder := r.URL.Query().Get("folder")
	if folder == "sent" {
		maildir = sentMaildirPath(s.Domain, s.User)
	}
	path := filepath.Join(maildir, "new", id)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		path = filepath.Join(maildir, "cur", id)
	}
	if folder == "sent" {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			path = filepath.Join(legacySentMaildirPath(s.Domain, s.User), "new", id)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				path = filepath.Join(legacySentMaildirPath(s.Domain, s.User), "cur", id)
			}
		}
	}

	msg, err := parseMessage(path, false)
	if err != nil {
		http.Error(w, "Message not found", http.StatusNotFound)
		return
	}
	msg.BackURL = "/"
	if folder == "sent" {
		msg.BackURL = "/?view=sent"
		msg.ReplyURL = composeURL(msg.To, msg.Subject)
	}

	if folder != "sent" {
		markAsRead(maildir, id)
	}
	tmpl := template.Must(template.ParseFiles("templates/message.html"))
	tmpl.Execute(w, msg)
}
