package main

import "testing"

func TestValidateRejectsInvalidEmail(t *testing.T) {
	err := validate("Peter", "not-an-email", "Please contact me")
	if err == nil {
		t.Fatal("expected invalid email to be rejected")
	}
}

func TestValidateRejectsTooManyLinks(t *testing.T) {
	err := validate("Peter", "peter@example.com", "Look at http://a.example https://b.example www.c.example https://d.example")
	if err == nil {
		t.Fatal("expected too many links to be rejected")
	}
}

func TestValidateAcceptsNormalMessage(t *testing.T) {
	err := validate("Peter", "peter@example.com", "Please contact me about LocalBox.")
	if err != nil {
		t.Fatalf("expected normal message to pass: %v", err)
	}
}
