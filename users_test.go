package main

import (
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
