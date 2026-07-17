package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHealthHandlerOK(t *testing.T) {
	tmp := t.TempDir()
	oldBase, oldDomains, oldUsers := localBoxBase, domainsFile, usersFile
	t.Cleanup(func() {
		localBoxBase = oldBase
		domainsFile = oldDomains
		usersFile = oldUsers
	})

	localBoxBase = filepath.Join(tmp, "localbox")
	domainsFile = filepath.Join(tmp, "domains.conf")
	usersFile = filepath.Join(tmp, "users.conf")
	if err := os.MkdirAll(localBoxBase, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(domainsFile, []byte("example.co.za\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(usersFile, []byte("info:example.co.za:hash\n"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	healthHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHealthHandlerMissingConfig(t *testing.T) {
	tmp := t.TempDir()
	oldBase, oldDomains, oldUsers := localBoxBase, domainsFile, usersFile
	t.Cleanup(func() {
		localBoxBase = oldBase
		domainsFile = oldDomains
		usersFile = oldUsers
	})

	localBoxBase = filepath.Join(tmp, "localbox")
	domainsFile = filepath.Join(tmp, "missing-domains.conf")
	usersFile = filepath.Join(tmp, "missing-users.conf")
	if err := os.MkdirAll(localBoxBase, 0755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	healthHandler(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rr.Code, rr.Body.String())
	}
}
