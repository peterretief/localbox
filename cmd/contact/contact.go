package main

import (
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"
)

func contactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		renderContactForm(w, requestDomain(r))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	email := r.FormValue("email")
	subject := r.FormValue("subject")
	message := r.FormValue("message")
	domain := requestDomain(r)

	if contactHoneypotFilled(r) {
		log.Printf("contact spam blocked by honeypot for %s from %s", domain, contactClientIP(r))
		http.Redirect(w, r, "/thankyou.html", http.StatusSeeOther)
		return
	}
	limitKey := domain + "|" + contactClientIP(r)
	if !contactLimiter.allow(limitKey, time.Now()) {
		log.Printf("contact spam blocked by rate limit for %s from %s", domain, contactClientIP(r))
		http.Error(w, "Too many messages. Please try again later.", http.StatusTooManyRequests)
		return
	}

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

var contactFormTemplate = template.Must(template.New("contact-form").Parse(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Contact {{.Domain}}</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: sans-serif; background: #f5f5f5; color: #1f2933; }
        nav { background: #1a1a2e; padding: 0 40px; height: 60px; display: flex; align-items: center; }
        nav a { color: white; text-decoration: none; font-size: 1.1em; font-weight: bold; }
        .content { max-width: 600px; margin: 60px auto; padding: 0 20px; }
        h1 { color: #1a1a2e; margin-bottom: 8px; }
        p.sub { color: #667085; margin-bottom: 32px; }
        .card { background: white; padding: 32px; border-radius: 8px; border: 1px solid #e5e7eb; }
        label { display: block; color: #475467; font-size: 0.85em; margin-bottom: 4px; margin-top: 16px; }
        input, textarea { width: 100%; padding: 10px 14px; border: 1px solid #d0d5dd; border-radius: 4px; font-size: 1em; font-family: sans-serif; }
        input:focus, textarea:focus { outline: none; border-color: #0077a3; }
        textarea { height: 150px; resize: vertical; }
        button { margin-top: 24px; background: #0077a3; color: white; border: none; padding: 12px 32px; border-radius: 4px; font-size: 1em; cursor: pointer; width: 100%; }
        button:hover { background: #005f82; }
        footer { text-align: center; padding: 40px; color: #667085; font-size: 0.85em; }
        footer a { color: #0077a3; }
    </style>
</head>
<body>
    <nav><a href="/">Back</a></nav>
    <div class="content">
        <h1>Contact {{.Domain}}</h1>
        <p class="sub">Send a message and we will get back to you shortly.</p>
        <div class="card">
            <form method="POST" action="/contact">
                <input type="text" name="website" autocomplete="off" tabindex="-1" style="position:absolute;left:-10000px;top:auto;width:1px;height:1px;overflow:hidden;" aria-hidden="true">
                <label for="name">Your Name</label>
                <input id="name" type="text" name="name" required>
                <label for="email">Email Address</label>
                <input id="email" type="email" name="email" required>
                <label for="subject">Subject</label>
                <input id="subject" type="text" name="subject">
                <label for="message">Message</label>
                <textarea id="message" name="message" required></textarea>
                <button type="submit">Send Message</button>
            </form>
        </div>
    </div>
    <footer>Powered by <a href="https://diode.co.za">LocalBox</a></footer>
</body>
</html>
`))

func renderContactForm(w http.ResponseWriter, domain string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := contactFormTemplate.Execute(w, struct{ Domain string }{Domain: domain}); err != nil {
		log.Printf("contact form render failed for %s: %v", domain, err)
	}
}
