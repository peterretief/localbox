package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestWaitlistHandlerStoresSignup(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	form := strings.NewReader("email=Peter%40Example.com&source=homepage")
	req := httptest.NewRequest(http.MethodPost, "http://localboxmail.co.za/waitlist", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.10:12345"
	rec := httptest.NewRecorder()

	waitlistHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	db, err := sql.Open("sqlite", orderDBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var domain, email, source string
	if err := db.QueryRow(`SELECT domain, email, source FROM mailing_list`).Scan(&domain, &email, &source); err != nil {
		t.Fatal(err)
	}
	if domain != "localboxmail.co.za" || email != "peter@example.com" || source != "homepage" {
		t.Fatalf("unexpected signup domain=%q email=%q source=%q", domain, email, source)
	}
}

func TestSaveWaitlistSignupUpdatesDuplicate(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	ctx := context.Background()
	for _, source := range []string{"first", "second"} {
		if err := saveWaitlistSignup(ctx, waitlistSignup{
			Domain: "localboxmail.co.za",
			Email:  "peter@example.com",
			Source: source,
		}); err != nil {
			t.Fatal(err)
		}
	}

	db, err := sql.Open("sqlite", orderDBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	var source string
	if err := db.QueryRow(`SELECT COUNT(*), MAX(source) FROM mailing_list`).Scan(&count, &source); err != nil {
		t.Fatal(err)
	}
	if count != 1 || source != "second" {
		t.Fatalf("expected one updated signup, count=%d source=%q", count, source)
	}
}
