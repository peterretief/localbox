package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyPasswordAcceptsStrongAndLegacyHashes(t *testing.T) {
	strongHash, err := hashPasswordStrong("secret-password")
	if err != nil {
		t.Fatal(err)
	}

	if !verifyPassword("secret-password", strongHash) {
		t.Fatal("expected strong hash to verify")
	}
	if verifyPassword("wrong-password", strongHash) {
		t.Fatal("expected wrong password to fail strong hash")
	}

	legacyHash := hashPassword("secret-password")
	if !verifyPassword("secret-password", legacyHash) {
		t.Fatal("expected legacy hash to verify")
	}
	if verifyPassword("wrong-password", legacyHash) {
		t.Fatal("expected wrong password to fail legacy hash")
	}
}

func TestVerifyPasswordAcceptsScriptPBKDF2Hash(t *testing.T) {
	hash := "pbkdf2-sha256$120000$YWJjZGVmZ2hpamtsbW5vcA$wtG0Zzm9/wPMUpyttrAi1ApTAI/XOXMH8SMq2lGub9U"

	if !verifyPassword("secret-password", hash) {
		t.Fatal("expected script-style PBKDF2 hash to verify")
	}
	if verifyPassword("wrong-password", hash) {
		t.Fatal("expected wrong password to fail")
	}
}

func TestAuthenticateRehashesLegacyPassword(t *testing.T) {
	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })

	if err := saveUsers([]User{{
		Username: "info",
		Domain:   "example.co.za",
		Hash:     hashPassword("secret-password"),
	}}); err != nil {
		t.Fatal(err)
	}

	if !authenticate("info", "example.co.za", "secret-password") {
		t.Fatal("expected legacy password to authenticate")
	}

	data, err := os.ReadFile(usersFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), passwordHashAlgorithm+"$") {
		t.Fatalf("expected legacy hash to be upgraded, got:\n%s", string(data))
	}
	if !authenticate("info", "example.co.za", "secret-password") {
		t.Fatal("expected upgraded password to authenticate")
	}
}

func TestRunAuthCheck(t *testing.T) {
	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })

	hash, err := hashPasswordStrong("secret-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveUsers([]User{{
		Username: "info",
		Domain:   "example.co.za",
		Hash:     hash,
	}}); err != nil {
		t.Fatal(err)
	}

	if code := runAuthCheck([]string{"info@example.co.za"}, strings.NewReader("secret-password\n"), &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if code := runAuthCheck([]string{"info@example.co.za"}, strings.NewReader("wrong-password\n"), &bytes.Buffer{}); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if code := runAuthCheck([]string{"bad-address"}, strings.NewReader("secret-password\n"), &bytes.Buffer{}); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestRunResetPassword(t *testing.T) {
	oldUsersFile := usersFile
	oldDovecotUsersFile := dovecotUsersFile
	oldRadicaleConfigFile := radicaleConfigFile
	oldRadicaleUsersFile := radicaleUsersFile
	tmp := t.TempDir()
	usersFile = filepath.Join(tmp, "users.conf")
	dovecotUsersFile = filepath.Join(tmp, "missing-dovecot-users")
	radicaleConfigFile = filepath.Join(tmp, "missing-radicale-config")
	radicaleUsersFile = filepath.Join(tmp, "missing-radicale-users")
	t.Cleanup(func() {
		usersFile = oldUsersFile
		dovecotUsersFile = oldDovecotUsersFile
		radicaleConfigFile = oldRadicaleConfigFile
		radicaleUsersFile = oldRadicaleUsersFile
	})

	hash, err := hashPasswordStrong("old-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveUsers([]User{{
		Username: "info",
		Domain:   "example.co.za",
		Hash:     hash,
	}}); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	if code := runResetPassword([]string{"info@example.co.za"}, strings.NewReader("new-password\n"), &stderr); code != 0 {
		t.Fatalf("code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if authenticate("info", "example.co.za", "old-password") {
		t.Fatal("old password should not authenticate")
	}
	if !authenticate("info", "example.co.za", "new-password") {
		t.Fatal("new password should authenticate")
	}
}

func TestRunResetPasswordRejectsInvalidInput(t *testing.T) {
	if code := runResetPassword([]string{"bad-address"}, strings.NewReader("new-password\n"), &bytes.Buffer{}); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if code := runResetPassword([]string{"info@example.co.za"}, strings.NewReader("short\n"), &bytes.Buffer{}); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
}
