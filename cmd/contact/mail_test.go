package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadSMTPConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smtp.conf")
	err := os.WriteFile(path, []byte(`
enabled=true
host=smtp-relay.brevo.com
port=587
username=user@example.com
password=secret
from=info@example.com
to=sales@example.com
`), 0600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := loadSMTPConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.Enabled || cfg.Host != "smtp-relay.brevo.com" || cfg.Port != 587 || cfg.Username != "user@example.com" || cfg.Password != "secret" || cfg.From != "info@example.com" || cfg.To != "sales@example.com" {
		t.Fatalf("unexpected SMTP config: %+v", cfg)
	}
}

func TestContactSMTPEmail(t *testing.T) {
	msg := string(contactSMTPEmail(
		"info@example.com",
		"sales@example.com",
		"example.com",
		"Jane User",
		"jane@example.net",
		"Quote\nplease",
		"Hello",
		time.Unix(100, 0).UTC(),
	))

	for _, want := range []string{
		"From: <info@example.com>\r\n",
		"To: <sales@example.com>\r\n",
		"Reply-To: \"Jane User\" <jane@example.net>\r\n",
		"Subject: Contact form: Quote please\r\n",
		"Domain: example.com\n",
		"Hello\n",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected SMTP email to contain %q:\n%s", want, msg)
		}
	}
}

func TestLocalContactRecipientFromUsersFile(t *testing.T) {
	dir := t.TempDir()
	oldUsersFile := contactUsersFile
	contactUsersFile = filepath.Join(dir, "users.conf")
	t.Cleanup(func() { contactUsersFile = oldUsersFile })

	err := os.WriteFile(contactUsersFile, []byte("someone:opamp.co.za:hash\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}

	if !localContactRecipient("someone@opamp.co.za") {
		t.Fatal("expected local recipient from users file")
	}
	if localContactRecipient("sales@example.com") {
		t.Fatal("did not expect unknown recipient to be local")
	}
}

func TestSMTPConfigToIsExplicitRelayRecipient(t *testing.T) {
	cfg := SMTPConfig{To: "sales@example.com"}
	recipient := strings.TrimSpace(cfg.To)
	if recipient == "" {
		recipient = contactOwnerAddress("example.com")
	}
	if recipient != "sales@example.com" {
		t.Fatalf("expected explicit recipient, got %q", recipient)
	}
}
