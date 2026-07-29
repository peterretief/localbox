package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func paymentStartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	domain, err := normalizePaymentDomain(r.URL.Query().Get("domain"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	packageOption, err := normalizeLocalBoxPackageOption(r.URL.Query().Get("option"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	next := paymentStartPath(domain, packageOption)
	existingOrder, err := activeOrderForDomain(r.Context(), domain)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Unable to check existing order", http.StatusInternalServerError)
		return
	}
	session := getRegisteredSession(r)
	if session == nil {
		if existingOrder != nil {
			http.Error(w, "This domain already has an active LocalBox order.", http.StatusConflict)
			return
		}
		http.Redirect(w, r, "/register?next="+url.QueryEscape(next), http.StatusFound)
		return
	}
	profile, err := loadPublicUserProfile(session.User, session.Domain)
	if err != nil || strings.TrimSpace(profile.FullName) == "" || strings.TrimSpace(profile.Phone) == "" || strings.TrimSpace(profile.PhysicalAddress) == "" {
		http.Redirect(w, r, "/register?next="+url.QueryEscape(next), http.StatusFound)
		return
	}
	if !publicUserEmailVerified(profile) {
		http.Redirect(w, r, "/register?verification=required&next="+url.QueryEscape(next), http.StatusFound)
		return
	}
	orderID := ""
	if existingOrder != nil {
		if !strings.EqualFold(strings.TrimSpace(existingOrder.Email), strings.TrimSpace(profile.Email)) {
			http.Error(w, "This domain already has an active LocalBox order.", http.StatusConflict)
			return
		}
		if paymentOrderAlreadyComplete(existingOrder.Status) {
			http.Redirect(w, r, "/account/domains", http.StatusSeeOther)
			return
		}
		if existingOrder.PaymentURL != "" {
			http.Redirect(w, r, existingOrder.PaymentURL, http.StatusSeeOther)
			return
		}
		orderID = existingOrder.ID
	}
	cfg, err := loadPaystackConfig(paystackConfigFile)
	if err != nil || cfg.validate() != nil {
		http.Error(w, "Payment is not configured", http.StatusServiceUnavailable)
		return
	}
	amountCents, paymentCurrency, err := quoteDomainPaymentAmountZAR(r.Context(), domain, cfg)
	if err != nil {
		http.Error(w, "Unable to calculate payment amount: "+err.Error(), http.StatusBadGateway)
		return
	}
	if orderID == "" {
		orderID, err = createOrderFromRegisteredProfile(r.Context(), profile, domain, packageOption)
	}
	if err != nil {
		http.Error(w, "Unable to create order", http.StatusInternalServerError)
		return
	}
	if err := createPaystackPaymentForOrderWithCurrency(r.Context(), orderID, amountCents, paymentCurrency); err != nil {
		http.Error(w, "Unable to create payment link: "+err.Error(), http.StatusBadGateway)
		return
	}
	detail, err := loadOrderDetail(orderID)
	if err != nil || detail.PaymentURL == "" {
		http.Error(w, "Unable to load payment link", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, detail.PaymentURL, http.StatusSeeOther)
}

func activeOrderForDomain(ctx context.Context, domain string) (*OrderSummary, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil, sql.ErrNoRows
	}
	db, err := openOrdersDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := ensureOrdersAdminSchema(ctx, db); err != nil {
		return nil, err
	}
	var order OrderSummary
	err = db.QueryRowContext(ctx, `
		SELECT orders.id, customers.email, orders.domain, orders.status, orders.payment_url, orders.payment_reference,
			orders.quoted_amount_cents, orders.payment_currency, orders.created_at, orders.updated_at, orders.archived_at
		FROM orders
		JOIN customers ON customers.id = orders.customer_id
		WHERE lower(orders.domain) = ? AND orders.archived_at = ''
			AND orders.status NOT IN ('cancelled', 'payment_failed')
		ORDER BY
			CASE orders.status
				WHEN 'site_live' THEN 6
				WHEN 'registered' THEN 5
				WHEN 'paid' THEN 4
				WHEN 'pending_payment' THEN 3
				WHEN 'quoted' THEN 3
				WHEN 'pending_review' THEN 2
				ELSE 1
			END DESC,
			orders.created_at DESC
		LIMIT 1
	`, domain).Scan(&order.ID, &order.Email, &order.Domain, &order.Status, &order.PaymentURL, &order.PaymentReference, &order.QuotedAmountCents, &order.PaymentCurrency, &order.CreatedAt, &order.UpdatedAt, &order.ArchivedAt)
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func paymentOrderAlreadyComplete(status string) bool {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "paid", "registered", "site_live":
		return true
	default:
		return false
	}
}

func createOrderFromRegisteredProfile(ctx context.Context, profile publicUserProfile, domain, packageOption string) (string, error) {
	orderID, err := newPaymentOrderID()
	if err != nil {
		return "", err
	}
	packageOption, err = normalizeLocalBoxPackageOption(packageOption)
	if err != nil {
		return "", err
	}
	wantedEmail := wantedEmailForPackageOption(domain, packageOption)
	notes := "Created from registered customer profile.\nSelected LocalBox option: " + localBoxPackageOptionLabel(packageOption)
	now := time.Now().UTC().Format(time.RFC3339)
	db, err := openOrdersDB()
	if err != nil {
		return "", err
	}
	defer db.Close()
	if err := ensureOrdersAdminSchema(ctx, db); err != nil {
		return "", err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO customers (full_name, email, phone, physical_address, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(email) DO UPDATE SET
			full_name = excluded.full_name,
			phone = excluded.phone,
			physical_address = excluded.physical_address
	`, profile.FullName, profile.Email, profile.Phone, profile.PhysicalAddress, now)
	if err != nil {
		return "", err
	}
	var customerID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM customers WHERE email = ?`, profile.Email).Scan(&customerID); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, orderID, customerID, domain, wantedEmail, "pending_payment", notes, now, now)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO order_events (order_id, event_type, message, created_at)
		VALUES (?, ?, ?, ?)
	`, orderID, "created", "Order created from registered customer profile.", now); err != nil {
		return "", err
	}
	return orderID, tx.Commit()
}

func paymentStartPath(domain, packageOption string) string {
	values := url.Values{}
	values.Set("domain", domain)
	if option, err := normalizeLocalBoxPackageOption(packageOption); err == nil && option != "free-parking" {
		values.Set("option", option)
	}
	return "/payment/start?" + values.Encode()
}

func normalizeLocalBoxPackageOption(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "free-parking", nil
	}
	switch value {
	case "free-parking", "localbox-hosting":
		return value, nil
	case "own-nameservers":
		return "free-parking", nil
	case "hosted-domain-email", "hosted-pi", "full-pi":
		return "localbox-hosting", nil
	default:
		return "", fmt.Errorf("invalid LocalBox option")
	}
}

func localBoxPackageOptionLabel(value string) string {
	switch value {
	case "localbox-hosting":
		return "Host it with LocalBox"
	default:
		return "Park it for free"
	}
}

func wantedEmailForPackageOption(domain, packageOption string) string {
	if packageOption != "localbox-hosting" {
		return ""
	}
	return "info@" + domain
}

func paystackTestModeEnabled() bool {
	cfg, err := loadPaystackConfig(paystackConfigFile)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(cfg.SecretKey), "sk_test_") || strings.HasPrefix(strings.TrimSpace(cfg.PublicKey), "pk_test_")
}

func errRegistrarDisabledInPaymentTestMode() error {
	return fmt.Errorf("Domains.co.za registration is disabled while Paystack test keys are configured")
}

type paymentDomainCheckResult struct {
	Domain        string `json:"domain"`
	Available     bool   `json:"available"`
	RegisterPrice int    `json:"register_price"`
	Currency      string `json:"currency"`
	Provider      string `json:"provider"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	Error         string `json:"error"`
}

var domainPaymentChecker = checkDomainPaymentViaContactService

func quoteDomainPaymentAmountZAR(ctx context.Context, domain string, cfg PaystackConfig) (int, string, error) {
	result, err := domainPaymentChecker(ctx, domain)
	if err != nil {
		return 0, "", err
	}
	if !result.Available {
		reason := strings.TrimSpace(result.Reason)
		if reason == "" {
			reason = strings.TrimSpace(result.Message)
		}
		if reason == "" {
			reason = "not available"
		}
		return 0, "", fmt.Errorf("%s is %s", result.Domain, reason)
	}
	if result.RegisterPrice <= 0 {
		return cfg.StarterCents, "ZAR", nil
	}
	sourceCurrency := cleanCurrency(result.Currency)
	if sourceCurrency == "" {
		sourceCurrency = "ZAR"
	}
	if sourceCurrency == "ZAR" {
		return result.RegisterPrice, "ZAR", nil
	}
	if sourceCurrency != "USD" {
		return 0, "", fmt.Errorf("registrar price is in unsupported currency %s", sourceCurrency)
	}
	markedUpCents := addPercentCeil(result.RegisterPrice, cfg.DomainMarkupPercent)
	zar, _, err := estimateCurrencyAmount(ctx, float64(markedUpCents)/100, "USD", "ZAR")
	if err != nil {
		return 0, "", fmt.Errorf("could not convert Cloudflare USD price to ZAR: %w", err)
	}
	zarCents := int(math.Ceil(zar * 100))
	if zarCents <= 0 {
		return 0, "", fmt.Errorf("converted Cloudflare price is invalid")
	}
	return zarCents, "ZAR", nil
}

func checkDomainPaymentViaContactService(ctx context.Context, domain string) (paymentDomainCheckResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8081/api/domain/check?domain="+url.QueryEscape(domain), nil)
	if err != nil {
		return paymentDomainCheckResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return paymentDomainCheckResult{}, fmt.Errorf("could not check domain price: %w", err)
	}
	defer resp.Body.Close()
	var result paymentDomainCheckResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return paymentDomainCheckResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if result.Error != "" {
			return paymentDomainCheckResult{}, errors.New(result.Error)
		}
		if result.Message != "" {
			return paymentDomainCheckResult{}, errors.New(result.Message)
		}
		return paymentDomainCheckResult{}, fmt.Errorf("domain price check failed: %s", resp.Status)
	}
	return result, nil
}

func addPercentCeil(cents, percent int) int {
	if cents <= 0 {
		return 0
	}
	if percent <= 0 {
		return cents
	}
	return (cents*(100+percent) + 99) / 100
}

func newPaymentOrderID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "ord_" + time.Now().UTC().Format("20060102T150405Z") + "_" + hex.EncodeToString(raw[:]), nil
}

func normalizePaymentDomain(value string) (string, error) {
	domain := strings.ToLower(strings.TrimSpace(value))
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "www.")
	if i := strings.IndexAny(domain, "/?#"); i >= 0 {
		domain = domain[:i]
	}
	domain = strings.Trim(domain, ". ")
	if domain == "" || len(domain) > 253 || !strings.Contains(domain, ".") {
		return "", fmt.Errorf("invalid domain")
	}
	for _, label := range strings.Split(domain, ".") {
		if !validPaymentDomainLabel(label) {
			return "", fmt.Errorf("invalid domain")
		}
	}
	return domain, nil
}

func validPaymentDomainLabel(value string) bool {
	if value == "" || len(value) > 63 || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}
