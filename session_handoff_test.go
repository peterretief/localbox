package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrimaryHostSessionRedirectsToDomainHandoff(t *testing.T) {
	token := newSession("info", "localboxzen.co.za")
	req := httptest.NewRequest(http.MethodGet, "https://localboxmail.co.za/apps/files/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rec := httptest.NewRecorder()

	canonicalHostMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("wrapped handler should not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "https://localboxzen.co.za/apps/files/?"+sessionHandoffParam+"=") {
		t.Fatalf("Location = %q, want domain handoff redirect", location)
	}
}

func TestSessionHandoffSetsDomainCookieAndRemovesToken(t *testing.T) {
	handoffToken := newSessionHandoff(&Session{User: "info", Domain: "localboxzen.co.za", Currency: "ZAR"})
	req := httptest.NewRequest(http.MethodGet, "https://localboxzen.co.za/dashboard?"+sessionHandoffParam+"="+handoffToken, nil)
	rec := httptest.NewRecorder()

	canonicalHostMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("wrapped handler should not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != "/dashboard" {
		t.Fatalf("Location = %q, want /dashboard", got)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != cookieName {
		t.Fatalf("cookies = %#v, want %s cookie", cookies, cookieName)
	}
}

func TestSessionHandoffRejectsWrongHost(t *testing.T) {
	handoffToken := newSessionHandoff(&Session{User: "info", Domain: "localboxzen.co.za"})
	req := httptest.NewRequest(http.MethodGet, "https://other.co.za/dashboard?"+sessionHandoffParam+"="+handoffToken, nil)
	rec := httptest.NewRecorder()

	canonicalHostMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("wrapped handler should not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
