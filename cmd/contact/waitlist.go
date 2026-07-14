package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

func waitlistHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	domain := requestDomain(r)
	if contactHoneypotFilled(r) {
		http.Redirect(w, r, "/thankyou.html", http.StatusSeeOther)
		return
	}
	limitKey := "waitlist|" + domain + "|" + contactClientIP(r)
	if !contactLimiter.allow(limitKey, time.Now()) {
		http.Error(w, "Too many signups. Please try again later.", http.StatusTooManyRequests)
		return
	}

	email, err := normalizeWaitlistEmail(r.FormValue("email"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := saveWaitlistSignup(r.Context(), waitlistSignup{
		Domain:    domain,
		Email:     email,
		Source:    strings.TrimSpace(r.FormValue("source")),
		IPAddress: contactClientIP(r),
		UserAgent: strings.TrimSpace(r.UserAgent()),
	}); err != nil {
		http.Error(w, "Unable to save signup", http.StatusInternalServerError)
		return
	}
	renderWaitlistThanks(w, email)
}

type waitlistSignup struct {
	Domain    string
	Email     string
	Source    string
	IPAddress string
	UserAgent string
}

func normalizeWaitlistEmail(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", fmt.Errorf("email required")
	}
	if len(value) > 254 {
		return "", fmt.Errorf("email too long")
	}
	addr, err := mail.ParseAddress(value)
	if err != nil {
		return "", fmt.Errorf("valid email required")
	}
	return strings.ToLower(addr.Address), nil
}

func saveWaitlistSignup(ctx context.Context, signup waitlistSignup) error {
	if !safeDomain(signup.Domain) {
		return fmt.Errorf("invalid domain")
	}
	db, err := openOrderDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := ensureWaitlistSchema(ctx, db); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `
		INSERT INTO mailing_list (domain, email, source, ip_address, user_agent, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(domain, email) DO UPDATE SET
			source = excluded.source,
			ip_address = excluded.ip_address,
			user_agent = excluded.user_agent,
			updated_at = excluded.updated_at
	`, signup.Domain, signup.Email, signup.Source, signup.IPAddress, signup.UserAgent, now, now)
	return err
}

func ensureWaitlistSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS mailing_list (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL,
			email TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			ip_address TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(domain, email)
		)
	`)
	return err
}

func renderWaitlistThanks(w http.ResponseWriter, email string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>Subscribed</title></head>
<body style="margin:0;font-family:Arial,Helvetica,sans-serif;background:#f7f8f5;color:#20242a;">
<main style="max-width:640px;margin:48px auto;padding:24px;">
<h1 style="margin:0 0 12px;">Almost done</h1>
<p>We saved ` + htmlEscape(email) + ` for LocalBox updates. You will be first to know when we release new products, guides, and early access options.</p>
<p><a href="/" style="color:#1e6f5c;font-weight:700;">Return to LocalBox Mail</a></p>
</main></body></html>`))
}

func htmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	return value
}
