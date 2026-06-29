package main

import (
	"errors"
	"strings"
)

func validate(name, email, message string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name required")
	}

	if strings.TrimSpace(email) == "" {
		return errors.New("email required")
	}

	if strings.TrimSpace(message) == "" {
		return errors.New("message required")
	}

	return nil
}
