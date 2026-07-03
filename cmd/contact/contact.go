package main

import (
	"log"
	"net/http"
	"strings"
)

func contactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.FormValue("name")
	email := r.FormValue("email")
	subject := r.FormValue("subject")
	message := r.FormValue("message")
	domain := requestDomain(r)

	if err := validate(name, email, message); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := saveMessage(domain, name, email, subject, message); err != nil {
		http.Error(w, "Unable to save message", http.StatusInternalServerError)
		return
	}

	if err := sendContactMessage(domain, name, email, subject, message); err != nil {
		log.Printf("contact SMTP send failed for %s: %v", domain, err)
	}

	http.Redirect(w, r, "/thankyou.html", http.StatusSeeOther)
}

func requestDomain(r *http.Request) string {
	host := strings.ToLower(strings.TrimSpace(r.Host))
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = strings.ToLower(forwardedHost)
	}
	if i := strings.Index(host, ","); i >= 0 {
		host = strings.TrimSpace(host[:i])
	}
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	return strings.TrimPrefix(host, "www.")
}
