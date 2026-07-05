package main

import (
	"errors"
	"net/mail"
	"strings"
)

func validate(name, email, message string) error {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	message = strings.TrimSpace(message)

	if name == "" {
		return errors.New("name required")
	}
	if len(name) > 120 {
		return errors.New("name too long")
	}

	if email == "" {
		return errors.New("email required")
	}
	if len(email) > 254 {
		return errors.New("email too long")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return errors.New("valid email required")
	}

	if message == "" {
		return errors.New("message required")
	}
	if len(message) < 5 {
		return errors.New("message too short")
	}
	if len(message) > 5000 {
		return errors.New("message too long")
	}
	if countLinks(message) > 3 {
		return errors.New("too many links")
	}

	return nil
}

func countLinks(message string) int {
	lower := strings.ToLower(message)
	return strings.Count(lower, "http://") + strings.Count(lower, "https://") + strings.Count(lower, "www.")
}
