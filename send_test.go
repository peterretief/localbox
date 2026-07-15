package main

import (
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestBuildOutgoingEmailWithAttachment(t *testing.T) {
	raw := buildOutgoingEmail(
		"someone@opamp.co.za",
		"info@opamp.co.za",
		"Attachment test",
		"See attached.",
		time.Unix(100, 0).UTC(),
		&OutgoingAttachment{
			Filename:    "test.txt",
			ContentType: "text/plain",
			Data:        []byte("hello attachment"),
		},
	)

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	body, attachments, err := parseBody(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), msg.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "See attached.") {
		t.Fatalf("expected message body, got %q", body)
	}
	if len(attachments) != 1 || attachments[0] != "test.txt" {
		t.Fatalf("expected test.txt attachment, got %#v", attachments)
	}
}

func TestBuildOutgoingHTMLEmail(t *testing.T) {
	raw := buildOutgoingHTMLEmail(
		"someone@opamp.co.za",
		"buyer@example.net",
		"Welcome",
		"Open https://example.net/register/verify?token=abc",
		`<p><a href="https://example.net/register/verify?token=abc">Continue</a></p>`,
		time.Unix(100, 0).UTC(),
	)

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	contentType := msg.Header.Get("Content-Type")
	if !strings.Contains(contentType, "multipart/alternative") {
		t.Fatalf("expected multipart alternative email, got %q", contentType)
	}
	rawText := string(raw)
	if !strings.Contains(rawText, "Content-Type: text/html; charset=utf-8") ||
		!strings.Contains(rawText, `<a href="https://example.net/register/verify?token=abc">Continue</a>`) {
		t.Fatalf("expected html link body, got %q", rawText)
	}
}
