package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContactGetRendersForm(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/contact", nil)
	req.Host = "www.example.com"
	rec := httptest.NewRecorder()

	contactHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Contact example.com",
		`<form method="POST" action="/contact">`,
		`name="email"`,
		`name="message"`,
		`name="website"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected response to contain %q:\n%s", want, body)
		}
	}
}

func TestContactRejectsUnsupportedMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/contact", nil)
	rec := httptest.NewRecorder()

	contactHandler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestContactHeadIsAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodHead, "/contact", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()

	contactHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
}

func TestContactHTMLAliasRendersForm(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/contact.html", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()

	routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `action="/contact"`) {
		t.Fatalf("expected contact form alias to post to /contact, got %s", rec.Body.String())
	}
}
