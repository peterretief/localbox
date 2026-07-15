package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/mail"
	"strings"
)

type mailingListEntry struct {
	Email     string
	Source    string
	CreatedAt string
	UpdatedAt string
}

func listMailingListEntries(domain string) ([]mailingListEntry, error) {
	domain = normalizeDomain(domain)
	if !safeDomain(domain) {
		return nil, fmt.Errorf("invalid domain")
	}
	db, err := openOrdersDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := ensureMailingListSchema(context.Background(), db); err != nil {
		return nil, err
	}
	rows, err := db.Query(`
		SELECT email, source, created_at, updated_at
		FROM mailing_list
		WHERE domain = ?
		ORDER BY updated_at DESC, email ASC
	`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []mailingListEntry
	for rows.Next() {
		var entry mailingListEntry
		if err := rows.Scan(&entry.Email, &entry.Source, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func ensureMailingListSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS mailing_list (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL,
			email TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			ip_address TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(domain, email)
		)
	`)
	return err
}

func normalizeMailingListEmail(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", fmt.Errorf("email required")
	}
	if len(value) > 254 {
		return "", fmt.Errorf("email too long")
	}
	addr, err := mail.ParseAddress(value)
	if err != nil {
		return "", fmt.Errorf("valid email required")
	}
	return strings.ToLower(addr.Address), nil
}
