package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistrationCertificateIncludesDomainAndFirstEmail(t *testing.T) {
	now := time.Date(2026, 7, 8, 10, 30, 0, 0, time.UTC)

	cert := string(registrationCertificate("Example.CO.ZA", "Info", now))

	for _, want := range []string{
		"LOCALBOX DOMAIN SETUP CERTIFICATE",
		"Domain: example.co.za",
		"First email address: info@example.co.za",
		"Issued: 2026-07-08T10:30:00Z",
		"Domain registration is handled as a separate registrar step",
	} {
		if !strings.Contains(cert, want) {
			t.Fatalf("certificate did not include %q:\n%s", want, cert)
		}
	}
	if strings.Contains(strings.ToLower(cert), "password:") {
		t.Fatalf("certificate should not include passwords:\n%s", cert)
	}
}

func TestSaveRegistrationCertificate(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	now := time.Date(2026, 7, 8, 10, 30, 0, 0, time.UTC)
	if err := saveRegistrationCertificate("example.co.za", "hello", now); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(registrationCertificatePath("example.co.za"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "First email address: hello@example.co.za") {
		t.Fatalf("saved certificate missing first email:\n%s", string(data))
	}
	if !registrationCertificateExists("example.co.za") {
		t.Fatal("expected certificate to exist")
	}
}

func TestAdminViewBackfillsDomainCertificate(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })

	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	if err := saveUsers([]User{{Username: "info", Domain: "example.co.za", Hash: "hash"}}); err != nil {
		t.Fatal(err)
	}

	view := adminView("example.co.za")
	if !view.Domain.CertificateAvailable {
		t.Fatal("expected admin view to make certificate available")
	}
	data, err := os.ReadFile(registrationCertificatePath("example.co.za"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "First email address: info@example.co.za") {
		t.Fatalf("certificate missing first mailbox:\n%s", string(data))
	}
}
