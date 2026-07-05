package main

import (
	"net/mail"
	"strings"
	"testing"
)

func TestReplyURLDoesNotIncludeUntrustedSubject(t *testing.T) {
	header := mail.Header{
		"Reply-To": []string{"Jane User <jane@example.net>"},
		"Subject":  []string{"https://bad.example phishing text"},
	}

	got := replyURL(header)
	if got != "/compose?to=jane%40example.net" {
		t.Fatalf("replyURL() = %q", got)
	}
	if strings.Contains(got, "bad.example") || strings.Contains(got, "subject=") {
		t.Fatalf("reply URL should not include untrusted subject: %q", got)
	}
}

func TestComposeURLStillSupportsExplicitSubject(t *testing.T) {
	got := composeURL("jane@example.net", "Question")
	if got != "/compose?subject=Re%3A+Question&to=jane%40example.net" {
		t.Fatalf("composeURL() = %q", got)
	}
}
