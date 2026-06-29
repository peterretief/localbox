package main

import (
	"fmt"
	"os"
	"time"
)

func saveMessage(name, email, subject, message string) error {
	if err := os.MkdirAll("/var/localbox", 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(
		"/var/localbox/contact.log",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f,
		"\n[%s]\nName: %s\nEmail: %s\nSubject: %s\n\n%s\n",
		time.Now().Format(time.RFC3339),
		name,
		email,
		subject,
		message,
	)

	return nil
}
