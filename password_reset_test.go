package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordResetSendsRecoveryEmailAndUpdatesMailboxPassword(t *testing.T) {
	configDir := t.TempDir()
	oldUsersFile := usersFile
	oldTokenFile := passwordResetTokenFile
	oldSecurityLogFile := securityLogFile
	oldDovecotUsersFile := dovecotUsersFile
	oldRadicaleConfigFile := radicaleConfigFile
	oldRadicaleUsersFile := radicaleUsersFile
	oldSender := sendOrderNotificationEmail
	oldLimiter := passwordResetLimiter
	usersFile = filepath.Join(configDir, "users.conf")
	passwordResetTokenFile = filepath.Join(configDir, "password-reset-tokens.conf")
	securityLogFile = filepath.Join(configDir, "security.log")
	dovecotUsersFile = filepath.Join(configDir, "dovecot-users")
	radicaleConfigFile = filepath.Join(configDir, "radicale-config")
	radicaleUsersFile = filepath.Join(configDir, "radicale-users")
	passwordResetLimiter = newResetRateLimiter()
	t.Cleanup(func() {
		usersFile = oldUsersFile
		passwordResetTokenFile = oldTokenFile
		securityLogFile = oldSecurityLogFile
		dovecotUsersFile = oldDovecotUsersFile
		radicaleConfigFile = oldRadicaleConfigFile
		radicaleUsersFile = oldRadicaleUsersFile
		sendOrderNotificationEmail = oldSender
		passwordResetLimiter = oldLimiter
	})

	hash, err := hashPasswordStrong("old-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveUsers([]User{{
		Username:      "info",
		Domain:        "example.co.za",
		Hash:          hash,
		RecoveryEmail: "owner@example.net",
	}}); err != nil {
		t.Fatal(err)
	}

	var sent []orderEmailMessage
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		sent = append(sent, message)
		return nil
	}

	req := httptest.NewRequest("POST", "https://mail.example.co.za/forgot-password", nil)
	reason := requestPasswordReset(req, "info@example.co.za")
	if reason != "" {
		t.Fatalf("expected reset request to succeed, got %q", reason)
	}
	if len(sent) != 1 {
		t.Fatalf("expected one reset email, got %d", len(sent))
	}
	if sent[0].To != "owner@example.net" || !strings.Contains(sent[0].Body, "/reset-password?token=") {
		t.Fatalf("unexpected reset email: %#v", sent[0])
	}

	token := tokenFromBody(t, sent[0].Body)
	if err := completePasswordReset(token, "new-password"); err != nil {
		t.Fatal(err)
	}
	if !authenticate("info", "example.co.za", "new-password") {
		t.Fatal("expected new password to authenticate")
	}
	if authenticate("info", "example.co.za", "old-password") {
		t.Fatal("old password should no longer authenticate")
	}
	if err := completePasswordReset(token, "another-password"); err == nil {
		t.Fatal("expected reset token to be single-use")
	}
}

func TestPasswordResetUnknownMailboxDoesNotSendEmail(t *testing.T) {
	configDir := t.TempDir()
	oldUsersFile := usersFile
	oldTokenFile := passwordResetTokenFile
	oldSecurityLogFile := securityLogFile
	oldSender := sendOrderNotificationEmail
	oldLimiter := passwordResetLimiter
	usersFile = filepath.Join(configDir, "users.conf")
	passwordResetTokenFile = filepath.Join(configDir, "password-reset-tokens.conf")
	securityLogFile = filepath.Join(configDir, "security.log")
	passwordResetLimiter = newResetRateLimiter()
	t.Cleanup(func() {
		usersFile = oldUsersFile
		passwordResetTokenFile = oldTokenFile
		securityLogFile = oldSecurityLogFile
		sendOrderNotificationEmail = oldSender
		passwordResetLimiter = oldLimiter
	})

	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		t.Fatalf("unknown mailbox should not send email: %#v", message)
		return nil
	}

	req := httptest.NewRequest("POST", "https://mail.example.co.za/forgot-password", nil)
	reason := requestPasswordReset(req, "missing@example.co.za")
	if reason != "unknown_mailbox" {
		t.Fatalf("expected unknown mailbox incident, got %q", reason)
	}
	logPasswordResetIncident(req, "missing@example.co.za", reason)
	data, err := os.ReadFile(securityLogFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "unknown_mailbox") {
		t.Fatalf("expected incident log, got %s", data)
	}
}

func TestPasswordResetSendsCustomerAccountEmailAndUpdatesPassword(t *testing.T) {
	configDir := t.TempDir()
	oldDB := orderDBFile
	oldUsersFile := usersFile
	oldTokenFile := passwordResetTokenFile
	oldSecurityLogFile := securityLogFile
	oldSender := sendOrderNotificationEmail
	oldLimiter := passwordResetLimiter
	orderDBFile = filepath.Join(configDir, "localbox.db")
	usersFile = filepath.Join(configDir, "users.conf")
	passwordResetTokenFile = filepath.Join(configDir, "password-reset-tokens.conf")
	securityLogFile = filepath.Join(configDir, "security.log")
	passwordResetLimiter = newResetRateLimiter()
	t.Cleanup(func() {
		orderDBFile = oldDB
		usersFile = oldUsersFile
		passwordResetTokenFile = oldTokenFile
		securityLogFile = oldSecurityLogFile
		sendOrderNotificationEmail = oldSender
		passwordResetLimiter = oldLimiter
	})

	if err := registerPublicUserProfile("localboxmail.co.za", "peter001", "peter@example.com", "Peter Retief", "+27 82 123 4567", "1 Example Street", "old-password", "ZAR"); err != nil {
		t.Fatal(err)
	}
	var sent []orderEmailMessage
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		sent = append(sent, message)
		return nil
	}

	req := httptest.NewRequest("POST", "https://localboxmail.co.za/forgot-password", nil)
	reason := requestPasswordReset(req, "peter@example.com")
	if reason != "" {
		t.Fatalf("expected customer reset request to succeed, got %q", reason)
	}
	if len(sent) != 1 || sent[0].To != "peter@example.com" || !strings.Contains(sent[0].Subject, "customer account") || !strings.Contains(sent[0].Body, "/reset-password?token=") {
		t.Fatalf("unexpected customer reset email: %#v", sent)
	}

	token := tokenFromBody(t, sent[0].Body)
	if err := completePasswordReset(token, "new-password"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := authenticatePublicUserWithCurrency("peter001", "localboxmail.co.za", "new-password"); !ok {
		t.Fatal("expected new customer password to authenticate")
	}
	if ok, _ := authenticatePublicUserWithCurrency("peter001", "localboxmail.co.za", "old-password"); ok {
		t.Fatal("old customer password should no longer authenticate")
	}
}

func tokenFromBody(t *testing.T, body string) string {
	t.Helper()
	const marker = "token="
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("reset token missing from body: %s", body)
	}
	token := body[start+len(marker):]
	if end := strings.IndexAny(token, "\r\n& "); end >= 0 {
		token = token[:end]
	}
	if token == "" {
		t.Fatal("empty reset token")
	}
	return token
}
