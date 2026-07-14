package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCertificateHandlerRequiresLogin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/certificate", nil)
	rec := httptest.NewRecorder()

	certificateHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "/login" {
		t.Fatalf("expected login redirect, got %q", location)
	}
}

func TestCertificateHandlerServesSessionDomainCertificate(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	if err := saveRegistrationCertificate("example.co.za", "info", time.Date(2026, 7, 8, 10, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	token := newSession("info", "example.co.za")
	req := httptest.NewRequest(http.MethodGet, "/certificate", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rec := httptest.NewRecorder()

	certificateHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "First email address: info@example.co.za") {
		t.Fatalf("unexpected certificate body:\n%s", rec.Body.String())
	}
}
