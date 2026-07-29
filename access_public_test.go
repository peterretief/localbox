package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicUserCannotOpenMailApp(t *testing.T) {
	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/apps/mail", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	mailAppHandler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestPublicUserDashboardRedirectsToAccountDomains(t *testing.T) {
	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	dashboardHandler(rec, req)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/account/domains" {
		t.Fatalf("expected account domains redirect, code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestPublicLoginWithoutNextRedirectsToAccountDomains(t *testing.T) {
	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("username", "buyer")
	form.Set("domain", "localboxmail.co.za")
	form.Set("password", "secret-password")
	form.Set("public", "1")
	req := httptest.NewRequest(http.MethodPost, "https://localboxmail.co.za/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	loginHandler(rec, req)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/account/domains" {
		t.Fatalf("expected account domains redirect, code=%d location=%q body=%q", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
}
