package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"html/template"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const passwordResetTTL = 30 * time.Minute

type ForgotPasswordPageData struct {
	Email  string
	Notice string
}

type ResetPasswordPageData struct {
	Token   string
	Error   string
	Success string
}

type passwordResetToken struct {
	Hash          string
	Username      string
	Domain        string
	RecoveryEmail string
	ExpiresAt     time.Time
}

var passwordResetLimiter = newResetRateLimiter()

func forgotPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
		renderForgotPassword(w, ForgotPasswordPageData{Email: email})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	notice := "If that mailbox exists and has a recovery email, reset instructions have been sent."
	reason := requestPasswordReset(r, email)
	if reason != "" {
		logPasswordResetIncident(r, email, reason)
	}
	renderForgotPassword(w, ForgotPasswordPageData{Email: email, Notice: notice})
}

func renderForgotPassword(w http.ResponseWriter, data ForgotPasswordPageData) {
	tmpl := template.Must(template.ParseFiles("templates/forgot_password.html"))
	_ = tmpl.Execute(w, data)
}

func resetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		token = strings.TrimSpace(r.FormValue("token"))
	}
	data := ResetPasswordPageData{Token: token}

	if r.Method == http.MethodGet {
		renderResetPassword(w, data)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")
	switch {
	case token == "":
		data.Error = "Reset link is missing or invalid."
	case len(password) < 8:
		data.Error = "New password must be at least 8 characters."
	case password != confirmPassword:
		data.Error = "New passwords do not match."
	default:
		if err := completePasswordReset(token, password); err != nil {
			if err.Error() == "reset token not found" || err.Error() == "invalid token" {
				data.Error = "Reset link is invalid or expired."
			} else {
				data.Error = "Could not update password. Please try again or ask the box admin to reset it."
			}
			logPasswordResetIncident(r, "", err.Error())
		} else {
			data.Token = ""
			data.Success = "Password updated. You can now sign in."
		}
	}
	renderResetPassword(w, data)
}

func renderResetPassword(w http.ResponseWriter, data ResetPasswordPageData) {
	tmpl := template.Must(template.ParseFiles("templates/reset_password.html"))
	_ = tmpl.Execute(w, data)
}

func requestPasswordReset(r *http.Request, address string) string {
	if !passwordResetLimiter.allow(resetClientIP(r), address, time.Now()) {
		return "rate_limited"
	}

	parsed, err := mail.ParseAddress(address)
	if err != nil || strings.ToLower(parsed.Address) != address {
		return "invalid_email"
	}
	user, ok := findMailboxByAddress(address)
	if !ok {
		return "unknown_mailbox"
	}
	if strings.TrimSpace(user.RecoveryEmail) == "" {
		return "missing_recovery_email"
	}

	token, err := createPasswordResetToken(user, time.Now().Add(passwordResetTTL))
	if err != nil {
		return "token_create_failed: " + err.Error()
	}
	if err := sendPasswordResetEmail(r, user, token); err != nil {
		return "send_failed: " + err.Error()
	}
	return ""
}

func findMailboxByAddress(address string) (User, bool) {
	local, domain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(address)), "@")
	if !ok || !safeMailboxUser(local) || !safeDomain(domain) {
		return User{}, false
	}
	users, err := loadUsers()
	if err != nil {
		return User{}, false
	}
	for _, user := range users {
		if user.Username == local && user.Domain == domain {
			return user, true
		}
	}
	return User{}, false
}

func createPasswordResetToken(user User, expiresAt time.Time) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	records, err := loadPasswordResetTokens()
	if err != nil {
		return "", err
	}
	now := time.Now()
	var kept []passwordResetToken
	for _, record := range records {
		if record.ExpiresAt.After(now) && !(record.Username == user.Username && record.Domain == user.Domain) {
			kept = append(kept, record)
		}
	}
	kept = append(kept, passwordResetToken{
		Hash:          hashResetToken(token),
		Username:      user.Username,
		Domain:        user.Domain,
		RecoveryEmail: user.RecoveryEmail,
		ExpiresAt:     expiresAt,
	})
	if err := savePasswordResetTokens(kept); err != nil {
		return "", err
	}
	return token, nil
}

func completePasswordReset(token, password string) error {
	if len(strings.TrimSpace(token)) < 32 {
		return fmt.Errorf("invalid token")
	}
	records, err := loadPasswordResetTokens()
	if err != nil {
		return err
	}
	tokenHash := hashResetToken(token)
	now := time.Now()
	var matched *passwordResetToken
	var kept []passwordResetToken
	for _, record := range records {
		if record.ExpiresAt.Before(now) {
			continue
		}
		if record.Hash == tokenHash {
			copy := record
			matched = &copy
			continue
		}
		kept = append(kept, record)
	}
	if matched == nil {
		_ = savePasswordResetTokens(kept)
		return fmt.Errorf("reset token not found")
	}
	if err := resetMailboxPassword(matched.Username, matched.Domain, password); err != nil {
		kept = append(kept, *matched)
		_ = savePasswordResetTokens(kept)
		return err
	}
	return savePasswordResetTokens(kept)
}

func loadPasswordResetTokens() ([]passwordResetToken, error) {
	data, err := os.ReadFile(passwordResetTokenFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var records []passwordResetToken
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != 5 {
			continue
		}
		expiresUnix, err := parseUnix(parts[4])
		if err != nil {
			continue
		}
		records = append(records, passwordResetToken{
			Hash:          parts[0],
			Username:      parts[1],
			Domain:        parts[2],
			RecoveryEmail: parts[3],
			ExpiresAt:     time.Unix(expiresUnix, 0),
		})
	}
	return records, nil
}

func savePasswordResetTokens(records []passwordResetToken) error {
	if err := os.MkdirAll(filepath.Dir(passwordResetTokenFile), 0750); err != nil {
		return err
	}
	var b strings.Builder
	for _, record := range records {
		fmt.Fprintf(&b, "%s:%s:%s:%s:%d\n", record.Hash, record.Username, record.Domain, record.RecoveryEmail, record.ExpiresAt.Unix())
	}
	return os.WriteFile(passwordResetTokenFile, []byte(b.String()), 0640)
}

func parseUnix(value string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(value, "%d", &n)
	return n, err
}

func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func sendPasswordResetEmail(r *http.Request, user User, token string) error {
	link := resetLink(r, token)
	address := user.Username + "@" + user.Domain
	textBody := fmt.Sprintf("A password reset was requested for %s.\n\nOpen this link within 30 minutes to set a new password:\n\n%s\n\nIf you did not request this, you can ignore this email.\n", address, link)
	htmlBody := "<p>A password reset was requested for <strong>" + html.EscapeString(address) + "</strong>.</p>" +
		"<p><a href=\"" + html.EscapeString(link) + "\">Reset your password</a></p>" +
		"<p>This link expires in 30 minutes. If you did not request this, you can ignore this email.</p>"
	return sendOrderNotificationEmail(orderEmailMessage{
		To:       user.RecoveryEmail,
		Subject:  "LocalBox password reset",
		Body:     textBody,
		HTMLBody: htmlBody,
	})
}

func resetLink(r *http.Request, token string) string {
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "https"
		}
	}
	host := strings.TrimSpace(r.Host)
	return proto + "://" + host + "/reset-password?token=" + url.QueryEscape(token)
}

func logPasswordResetIncident(r *http.Request, address, reason string) {
	if err := os.MkdirAll(filepath.Dir(securityLogFile), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(securityLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "[%s] password_reset ip=%s email=%q reason=%q\n", time.Now().Format(time.RFC3339), resetClientIP(r), address, reason)
}

func resetClientIP(r *http.Request) string {
	for _, header := range []string{"CF-Connecting-IP", "X-Forwarded-For"} {
		value := strings.TrimSpace(r.Header.Get(header))
		if value == "" {
			continue
		}
		if first, _, ok := strings.Cut(value, ","); ok {
			value = strings.TrimSpace(first)
		}
		if net.ParseIP(value) != nil {
			return value
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && net.ParseIP(host) != nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

type resetRateLimiter struct {
	mu      sync.Mutex
	byIP    map[string][]time.Time
	byEmail map[string][]time.Time
}

func newResetRateLimiter() *resetRateLimiter {
	return &resetRateLimiter{
		byIP:    map[string][]time.Time{},
		byEmail: map[string][]time.Time{},
	}
}

func (l *resetRateLimiter) allow(ip, email string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	ipHits := recentResetHits(l.byIP[ip], now)
	emailHits := recentResetHits(l.byEmail[email], now)
	if len(ipHits) >= 8 || (email != "" && len(emailHits) >= 4) {
		l.byIP[ip] = ipHits
		l.byEmail[email] = emailHits
		return false
	}
	l.byIP[ip] = append(ipHits, now)
	if email != "" {
		l.byEmail[email] = append(emailHits, now)
	}
	return true
}

func recentResetHits(hits []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-1 * time.Hour)
	var kept []time.Time
	for _, hit := range hits {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	return kept
}
