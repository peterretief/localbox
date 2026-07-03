package main

import (
	"os"
	"path/filepath"
	"testing"
)

func withContactConfigFiles(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	oldUsersFile := contactUsersFile
	oldAdminsFile := contactAdminsFile
	oldBoxAdminsFile := contactBoxAdminsFile

	contactUsersFile = filepath.Join(dir, "users.conf")
	contactAdminsFile = filepath.Join(dir, "admins.conf")
	contactBoxAdminsFile = filepath.Join(dir, "box-admins.conf")

	t.Cleanup(func() {
		contactUsersFile = oldUsersFile
		contactAdminsFile = oldAdminsFile
		contactBoxAdminsFile = oldBoxAdminsFile
	})

	return dir
}

func TestContactMailboxUserPrefersDomainAdminOwner(t *testing.T) {
	dir := withContactConfigFiles(t)

	if err := os.WriteFile(contactUsersFile, []byte("info:example.com:hash\npeter:example.com:hash\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contactAdminsFile, []byte("peter:example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "box-admins.conf"), []byte("info:example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if got := contactMailboxUser("example.com"); got != "peter" {
		t.Fatalf("expected domain admin owner peter, got %q", got)
	}
	if got := contactOwnerAddress("example.com"); got != "peter@example.com" {
		t.Fatalf("expected owner address peter@example.com, got %q", got)
	}
}

func TestContactMailboxUserFallsBackToBoxAdminThenInfo(t *testing.T) {
	withContactConfigFiles(t)

	if err := os.WriteFile(contactUsersFile, []byte("info:example.com:hash\nowner:example.com:hash\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contactAdminsFile, []byte("missing:example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contactBoxAdminsFile, []byte("owner:example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if got := contactMailboxUser("example.com"); got != "owner" {
		t.Fatalf("expected box admin owner, got %q", got)
	}
}

func TestContactMailboxUserFallsBackToInfo(t *testing.T) {
	withContactConfigFiles(t)

	if err := os.WriteFile(contactUsersFile, []byte("sales:example.com:hash\ninfo:example.com:hash\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if got := contactMailboxUser("example.com"); got != "info" {
		t.Fatalf("expected info fallback, got %q", got)
	}
}
