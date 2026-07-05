package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestContactRateLimiter(t *testing.T) {
	limiter := newContactRateLimiter(2, time.Hour)
	now := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)

	if !limiter.allow("example.com|192.0.2.10", now) {
		t.Fatal("first message should be allowed")
	}
	if !limiter.allow("example.com|192.0.2.10", now.Add(time.Minute)) {
		t.Fatal("second message should be allowed")
	}
	if limiter.allow("example.com|192.0.2.10", now.Add(2*time.Minute)) {
		t.Fatal("third message should be rate limited")
	}
	if !limiter.allow("example.com|192.0.2.10", now.Add(2*time.Hour)) {
		t.Fatal("message after window should be allowed")
	}
}

func TestContactClientIPPrefersCloudflareHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/contact", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("CF-Connecting-IP", "203.0.113.9")

	if got := contactClientIP(req); got != "203.0.113.9" {
		t.Fatalf("contactClientIP() = %q", got)
	}
}

func TestContactHoneypotFilled(t *testing.T) {
	req := httptest.NewRequest("POST", "/contact", strings.NewReader("name=Peter&email=peter%40example.com&message=Hello&website=https%3A%2F%2Fspam.example"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if !contactHoneypotFilled(req) {
		t.Fatal("expected honeypot field to be detected")
	}
}
