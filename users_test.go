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

func TestRunSetRecoveryEmail(t *testing.T) {
	oldUsersFile := usersFile
	oldDomainsFile := domainsFile
	oldLocalBoxBase := localBoxBase
	tmp := t.TempDir()
	usersFile = filepath.Join(tmp, "users.conf")
	domainsFile = filepath.Join(tmp, "domains.conf")
	localBoxBase = filepath.Join(tmp, "localbox")
	t.Cleanup(func() {
		usersFile = oldUsersFile
		domainsFile = oldDomainsFile
		localBoxBase = oldLocalBoxBase
	})
	if err := saveDomains([]string{"example.co.za"}); err != nil {
		t.Fatal(err)
	}
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

	var stderr bytes.Buffer
	if code := runSetRecoveryEmail([]string{"info@example.co.za", "Owner@External.net"}, &stderr); code != 0 {
		t.Fatalf("code = %d, want 0, stderr=%q", code, stderr.String())
	}

	users, err := loadUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].RecoveryEmail != "owner@external.net" {
		t.Fatalf("unexpected users: %#v", users)
	}
}

func TestRadicaleUsesDovecotAuthReadsAuthSection(t *testing.T) {
	oldRadicaleConfigFile := radicaleConfigFile
	radicaleConfigFile = filepath.Join(t.TempDir(), "radicale.conf")
	t.Cleanup(func() { radicaleConfigFile = oldRadicaleConfigFile })

	err := os.WriteFile(radicaleConfigFile, []byte(`[server]
hosts = 127.0.0.1:5232

[auth]
type = dovecot
dovecot_socket = /run/dovecot/auth-radicale

[rights]
type = owner_only
`), 0640)
	if err != nil {
		t.Fatal(err)
	}
	if !radicaleUsesDovecotAuth() {
		t.Fatal("expected Radicale Dovecot auth to be detected from [auth] section")
	}
}
