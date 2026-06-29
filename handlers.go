package main

import (
	"html/template"
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
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tmpl := template.Must(template.ParseFiles("templates/login.html"))
		tmpl.Execute(w, LoginPageData{
			Domain: lastLoginDomain(r),
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
	})
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
