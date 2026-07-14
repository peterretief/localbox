package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

var publicUsernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,31}$`)

type PublicRegisterData struct {
	Error           string
	Username        string
	Email           string
	FullName        string
	Phone           string
	PhysicalAddress string
	AddressLine1    string
	AddressLine2    string
	City            string
	Province        string
	PostalCode      string
	Country         string
	Currency        string
	Domain          string
	Next            string
	LoginURL        string
	SignedIn        bool
	Notice          string
}

type publicUserProfileInput struct {
	Domain          string
	Username        string
	Email           string
	FullName        string
	Phone           string
	PhysicalAddress string
	AddressLine1    string
	AddressLine2    string
	City            string
	Province        string
	PostalCode      string
	Country         string
	Password        string
	Currency        string
}

func registerPublicUser(domain, username, email, password string) error {
	return registerPublicUserWithCurrency(domain, username, email, password, "ZAR")
}

func registerPublicUserWithCurrency(domain, username, email, password, currency string) error {
	return registerPublicUserProfile(domain, username, email, username, "not provided", "not provided", password, currency)
}

func registerPublicUserProfile(domain, username, email, fullName, phone, physicalAddress, password, currency string) error {
	if err := registerPublicUserProfileDetails(publicUserProfileInput{
		Domain:          domain,
		Username:        username,
		Email:           email,
		FullName:        fullName,
		Phone:           phone,
		PhysicalAddress: physicalAddress,
		AddressLine1:    physicalAddress,
		City:            "not provided",
		Province:        "not provided",
		PostalCode:      "0000",
		Country:         "ZA",
		Password:        password,
		Currency:        currency,
	}); err != nil {
		return err
	}
	return markPublicUserEmailVerified(username, domain)
}

func registerPublicUserProfileDetails(input publicUserProfileInput) error {
	if err := normalizePublicUserProfileInput(&input, true); err != nil {
		return err
	}
	hash, err := hashPasswordStrong(input.Password)
	if err != nil {
		return err
	}
	db, err := openOrdersDB()
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensurePublicUsersSchema(ctx, db); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var existingUsername, existingEmail string
	err = db.QueryRowContext(ctx, `
		SELECT username, email
		FROM public_users
		WHERE domain = ? AND (username = ? OR email = ?)
	`, input.Domain, input.Username, input.Email).Scan(&existingUsername, &existingEmail)
	if err == nil {
		if existingUsername != input.Username || existingEmail != input.Email {
			return fmt.Errorf("that username or email is already registered for this site")
		}
		_, err = db.ExecContext(ctx, `
			UPDATE public_users
			SET full_name = ?, phone = ?, physical_address = ?, address_line_1 = ?, address_line_2 = ?, city = ?, province = ?, postal_code = ?, country = ?, password_hash = ?, currency = ?, updated_at = ?
			WHERE domain = ? AND username = ? AND email = ?
		`, input.FullName, input.Phone, input.PhysicalAddress, input.AddressLine1, input.AddressLine2, input.City, input.Province, input.PostalCode, input.Country, hash, input.Currency, now, input.Domain, input.Username, input.Email)
		return err
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO public_users (domain, username, email, full_name, phone, physical_address, address_line_1, address_line_2, city, province, postal_code, country, password_hash, currency, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, input.Domain, input.Username, input.Email, input.FullName, input.Phone, input.PhysicalAddress, input.AddressLine1, input.AddressLine2, input.City, input.Province, input.PostalCode, input.Country, hash, input.Currency, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("that username or email is already registered for this site")
		}
		return err
	}
	return nil
}

func normalizePublicUserProfileInput(input *publicUserProfileInput, requirePassword bool) error {
	input.Domain = strings.TrimSpace(strings.ToLower(input.Domain))
	input.Username = strings.TrimSpace(strings.ToLower(input.Username))
	input.Email = strings.TrimSpace(strings.ToLower(input.Email))
	input.FullName = strings.TrimSpace(input.FullName)
	input.Phone = strings.TrimSpace(input.Phone)
	input.PhysicalAddress = strings.TrimSpace(input.PhysicalAddress)
	input.AddressLine1 = strings.TrimSpace(input.AddressLine1)
	input.AddressLine2 = strings.TrimSpace(input.AddressLine2)
	input.City = strings.TrimSpace(input.City)
	input.Province = strings.TrimSpace(input.Province)
	input.PostalCode = strings.TrimSpace(input.PostalCode)
	input.Country = strings.ToUpper(strings.TrimSpace(input.Country))
	input.Currency = cleanCurrency(input.Currency)
	if input.Country == "" {
		input.Country = "ZA"
	}
	if input.AddressLine1 == "" && input.PhysicalAddress != "" {
		input.AddressLine1 = input.PhysicalAddress
	}
	if input.PhysicalAddress == "" {
		input.PhysicalAddress = composePublicUserPhysicalAddress(*input)
	}
	if input.Domain == "" {
		return fmt.Errorf("domain required")
	}
	if !publicUsernamePattern.MatchString(input.Username) {
		return fmt.Errorf("username must be 2-32 lowercase letters, numbers, dots, dashes, or underscores")
	}
	if _, err := mail.ParseAddress(input.Email); err != nil {
		return fmt.Errorf("valid email address required")
	}
	if input.FullName == "" {
		return fmt.Errorf("full name required")
	}
	if input.Phone == "" {
		return fmt.Errorf("phone number required")
	}
	if input.AddressLine1 == "" {
		return fmt.Errorf("street address required")
	}
	if input.City == "" {
		return fmt.Errorf("city required")
	}
	if input.Province == "" {
		return fmt.Errorf("province required")
	}
	if input.PostalCode == "" {
		return fmt.Errorf("postal code required")
	}
	if input.Country == "" {
		return fmt.Errorf("country required")
	}
	if len(input.PhysicalAddress) < 10 {
		return fmt.Errorf("physical address required")
	}
	if requirePassword && len(input.Password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	return nil
}

func composePublicUserPhysicalAddress(input publicUserProfileInput) string {
	parts := []string{input.AddressLine1, input.AddressLine2, input.City, input.Province, input.PostalCode, input.Country}
	var clean []string
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			clean = append(clean, part)
		}
	}
	return strings.Join(clean, ", ")
}

type publicUserProfile struct {
	Username        string
	Domain          string
	Email           string
	FullName        string
	Phone           string
	PhysicalAddress string
	AddressLine1    string
	AddressLine2    string
	City            string
	Province        string
	PostalCode      string
	Country         string
	Currency        string
	CreatedAt       string
	UpdatedAt       string
	EmailVerifiedAt string
}

func loadPublicUserProfile(username, domain string) (publicUserProfile, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	domain = strings.TrimSpace(strings.ToLower(domain))
	db, err := openOrdersDB()
	if err != nil {
		return publicUserProfile{}, err
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensurePublicUsersSchema(ctx, db); err != nil {
		return publicUserProfile{}, err
	}
	var profile publicUserProfile
	err = db.QueryRowContext(ctx, `
		SELECT username, domain, email, full_name, phone, physical_address, address_line_1, address_line_2, city, province, postal_code, country, currency, email_verified_at
		FROM public_users
		WHERE username = ? AND domain = ?
	`, username, domain).Scan(&profile.Username, &profile.Domain, &profile.Email, &profile.FullName, &profile.Phone, &profile.PhysicalAddress, &profile.AddressLine1, &profile.AddressLine2, &profile.City, &profile.Province, &profile.PostalCode, &profile.Country, &profile.Currency, &profile.EmailVerifiedAt)
	return profile, err
}

func loadPublicUserProfileByEmail(email string) (publicUserProfile, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return publicUserProfile{}, fmt.Errorf("registered account email required")
	}
	db, err := openOrdersDB()
	if err != nil {
		return publicUserProfile{}, err
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensurePublicUsersSchema(ctx, db); err != nil {
		return publicUserProfile{}, err
	}
	var profile publicUserProfile
	err = db.QueryRowContext(ctx, `
		SELECT username, domain, email, full_name, phone, physical_address, address_line_1, address_line_2, city, province, postal_code, country, currency, email_verified_at
		FROM public_users
		WHERE email = ?
		ORDER BY updated_at DESC
		LIMIT 1
	`, email).Scan(&profile.Username, &profile.Domain, &profile.Email, &profile.FullName, &profile.Phone, &profile.PhysicalAddress, &profile.AddressLine1, &profile.AddressLine2, &profile.City, &profile.Province, &profile.PostalCode, &profile.Country, &profile.Currency, &profile.EmailVerifiedAt)
	return profile, err
}

func listPublicUsers(domain string) ([]publicUserProfile, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil, nil
	}
	db, err := openOrdersDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensurePublicUsersSchema(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT username, domain, email, full_name, phone, physical_address, address_line_1, address_line_2, city, province, postal_code, country, currency, created_at, updated_at, email_verified_at
		FROM public_users
		WHERE domain = ?
		ORDER BY created_at DESC, username ASC
	`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []publicUserProfile
	for rows.Next() {
		var user publicUserProfile
		if err := rows.Scan(&user.Username, &user.Domain, &user.Email, &user.FullName, &user.Phone, &user.PhysicalAddress, &user.AddressLine1, &user.AddressLine2, &user.City, &user.Province, &user.PostalCode, &user.Country, &user.Currency, &user.CreatedAt, &user.UpdatedAt, &user.EmailVerifiedAt); err != nil {
			return nil, err
		}
		user.Currency = cleanCurrency(user.Currency)
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func issuePublicUserEmailVerification(username, domain string) (string, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	domain = strings.TrimSpace(strings.ToLower(domain))
	token, err := newPublicEmailVerificationToken()
	if err != nil {
		return "", err
	}
	db, err := openOrdersDB()
	if err != nil {
		return "", err
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensurePublicUsersSchema(ctx, db); err != nil {
		return "", err
	}
	result, err := db.ExecContext(ctx, `
		UPDATE public_users
		SET email_verification_token = ?, email_verification_sent_at = ?, updated_at = ?
		WHERE domain = ? AND username = ?
	`, token, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), domain, username)
	if err != nil {
		return "", err
	}
	if rows, err := result.RowsAffected(); err != nil || rows == 0 {
		return "", fmt.Errorf("registered account not found")
	}
	return token, nil
}

func verifyPublicUserEmailToken(token string) (publicUserProfile, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return publicUserProfile{}, fmt.Errorf("verification token required")
	}
	db, err := openOrdersDB()
	if err != nil {
		return publicUserProfile{}, err
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensurePublicUsersSchema(ctx, db); err != nil {
		return publicUserProfile{}, err
	}
	var profile publicUserProfile
	err = db.QueryRowContext(ctx, `
		SELECT username, domain, email, full_name, phone, physical_address, address_line_1, address_line_2, city, province, postal_code, country, currency, email_verified_at
		FROM public_users
		WHERE email_verification_token = ?
	`, token).Scan(&profile.Username, &profile.Domain, &profile.Email, &profile.FullName, &profile.Phone, &profile.PhysicalAddress, &profile.AddressLine1, &profile.AddressLine2, &profile.City, &profile.Province, &profile.PostalCode, &profile.Country, &profile.Currency, &profile.EmailVerifiedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return publicUserProfile{}, fmt.Errorf("verification link is invalid or expired")
		}
		return publicUserProfile{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `
		UPDATE public_users
		SET email_verified_at = ?, email_verification_token = '', updated_at = ?
		WHERE domain = ? AND username = ?
	`, now, now, profile.Domain, profile.Username); err != nil {
		return publicUserProfile{}, err
	}
	profile.EmailVerifiedAt = now
	return profile, nil
}

func publicUserEmailVerified(profile publicUserProfile) bool {
	return strings.TrimSpace(profile.EmailVerifiedAt) != ""
}

func markPublicUserEmailVerified(username, domain string) error {
	username = strings.TrimSpace(strings.ToLower(username))
	domain = strings.TrimSpace(strings.ToLower(domain))
	db, err := openOrdersDB()
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensurePublicUsersSchema(ctx, db); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.ExecContext(ctx, `
		UPDATE public_users
		SET email_verified_at = ?, updated_at = ?
		WHERE domain = ? AND username = ?
	`, now, now, domain, username)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows == 0 {
		return fmt.Errorf("registered account not found")
	}
	return nil
}

func newPublicEmailVerificationToken() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func updatePublicUserProfile(username, domain, fullName, phone, physicalAddress, currency string) error {
	return updatePublicUserProfileDetails(publicUserProfileInput{
		Domain:          domain,
		Username:        username,
		Email:           username + "@example.invalid",
		FullName:        fullName,
		Phone:           phone,
		PhysicalAddress: physicalAddress,
		AddressLine1:    physicalAddress,
		City:            "not provided",
		Province:        "not provided",
		PostalCode:      "0000",
		Country:         "ZA",
		Currency:        currency,
	})
}

func updatePublicUserProfileDetails(input publicUserProfileInput) error {
	if err := normalizePublicUserProfileInput(&input, false); err != nil {
		return err
	}
	db, err := openOrdersDB()
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensurePublicUsersSchema(ctx, db); err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `
		UPDATE public_users
		SET full_name = ?, phone = ?, physical_address = ?, address_line_1 = ?, address_line_2 = ?, city = ?, province = ?, postal_code = ?, country = ?, currency = ?, updated_at = ?
		WHERE domain = ? AND username = ?
	`, input.FullName, input.Phone, input.PhysicalAddress, input.AddressLine1, input.AddressLine2, input.City, input.Province, input.PostalCode, input.Country, input.Currency, time.Now().UTC().Format(time.RFC3339), input.Domain, input.Username)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows == 0 {
		return fmt.Errorf("registered account not found")
	}
	return nil
}

func cleanCurrency(currency string) string {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "ZAR", "USD", "EUR", "GBP":
		return strings.ToUpper(strings.TrimSpace(currency))
	default:
		return "ZAR"
	}
}

func authenticatePublicUser(username, domain, password string) bool {
	ok, _ := authenticatePublicUserWithCurrency(username, domain, password)
	return ok
}

func authenticatePublicUserWithCurrency(username, domain, password string) (bool, string) {
	username = strings.TrimSpace(strings.ToLower(username))
	domain = strings.TrimSpace(strings.ToLower(domain))
	if username == "" || domain == "" {
		return false, ""
	}
	db, err := openOrdersDB()
	if err != nil {
		return false, ""
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensurePublicUsersSchema(ctx, db); err != nil {
		return false, ""
	}
	var hash, currency string
	err = db.QueryRowContext(ctx, `
		SELECT password_hash, currency
		FROM public_users
		WHERE domain = ? AND username = ?
	`, domain, username).Scan(&hash, &currency)
	if err != nil || !verifyPassword(password, hash) {
		return false, ""
	}
	return true, cleanCurrency(currency)
}

func publicUserExists(username, domain string) bool {
	username = strings.TrimSpace(strings.ToLower(username))
	domain = strings.TrimSpace(strings.ToLower(domain))
	if username == "" || domain == "" {
		return false
	}
	db, err := openOrdersDB()
	if err != nil {
		return false
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensurePublicUsersSchema(ctx, db); err != nil {
		return false
	}
	var exists int
	err = db.QueryRowContext(ctx, `
		SELECT 1
		FROM public_users
		WHERE domain = ? AND username = ?
	`, domain, username).Scan(&exists)
	return err == nil && exists == 1
}

func publicUserCurrency(username, domain string) string {
	username = strings.TrimSpace(strings.ToLower(username))
	domain = strings.TrimSpace(strings.ToLower(domain))
	if username == "" || domain == "" {
		return ""
	}
	db, err := openOrdersDB()
	if err != nil {
		return ""
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensurePublicUsersSchema(ctx, db); err != nil {
		return ""
	}
	var currency string
	if err := db.QueryRowContext(ctx, `
		SELECT currency
		FROM public_users
		WHERE domain = ? AND username = ?
	`, domain, username).Scan(&currency); err != nil {
		return ""
	}
	return cleanCurrency(currency)
}

func ensurePublicUsersSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS public_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL,
			username TEXT NOT NULL,
			email TEXT NOT NULL,
			full_name TEXT NOT NULL DEFAULT '',
			phone TEXT NOT NULL DEFAULT '',
			physical_address TEXT NOT NULL DEFAULT '',
			address_line_1 TEXT NOT NULL DEFAULT '',
			address_line_2 TEXT NOT NULL DEFAULT '',
			city TEXT NOT NULL DEFAULT '',
			province TEXT NOT NULL DEFAULT '',
			postal_code TEXT NOT NULL DEFAULT '',
			country TEXT NOT NULL DEFAULT 'ZA',
			password_hash TEXT NOT NULL,
			currency TEXT NOT NULL DEFAULT 'ZAR',
			email_verified_at TEXT NOT NULL DEFAULT '',
			email_verification_token TEXT NOT NULL DEFAULT '',
			email_verification_sent_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(domain, username),
			UNIQUE(domain, email)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_public_users_domain_username
			ON public_users(domain, username)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err := ensurePublicUserColumn(ctx, db, "currency", "TEXT NOT NULL DEFAULT 'ZAR'"); err != nil {
		return err
	}
	for column, definition := range map[string]string{
		"full_name":                  "TEXT NOT NULL DEFAULT ''",
		"phone":                      "TEXT NOT NULL DEFAULT ''",
		"physical_address":           "TEXT NOT NULL DEFAULT ''",
		"address_line_1":             "TEXT NOT NULL DEFAULT ''",
		"address_line_2":             "TEXT NOT NULL DEFAULT ''",
		"city":                       "TEXT NOT NULL DEFAULT ''",
		"province":                   "TEXT NOT NULL DEFAULT ''",
		"postal_code":                "TEXT NOT NULL DEFAULT ''",
		"country":                    "TEXT NOT NULL DEFAULT 'ZA'",
		"email_verified_at":          "TEXT NOT NULL DEFAULT ''",
		"email_verification_token":   "TEXT NOT NULL DEFAULT ''",
		"email_verification_sent_at": "TEXT NOT NULL DEFAULT ''",
	} {
		if err := ensurePublicUserColumn(ctx, db, column, definition); err != nil {
			return err
		}
	}
	return nil
}

func ensurePublicUserColumn(ctx context.Context, db *sql.DB, column, definition string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(public_users)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "ALTER TABLE public_users ADD COLUMN "+column+" "+definition)
	return err
}
