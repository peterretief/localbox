package main

import (
	"context"
	"crypto/rand"
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
	session := getRegisteredSession(r)
	if session == nil {
		http.Redirect(w, r, "/register?next="+url.QueryEscape("/payment/start?domain="+domain), http.StatusFound)
		return
	}
	profile, err := loadPublicUserProfile(session.User, session.Domain)
	if err != nil || strings.TrimSpace(profile.FullName) == "" || strings.TrimSpace(profile.Phone) == "" || strings.TrimSpace(profile.PhysicalAddress) == "" {
		http.Redirect(w, r, "/register?next="+url.QueryEscape("/payment/start?domain="+domain), http.StatusFound)
		return
	}
	if !publicUserEmailVerified(profile) {
		http.Redirect(w, r, "/register?verification=required&next="+url.QueryEscape("/payment/start?domain="+domain), http.StatusFound)
		return
	}
	orderID, err := createOrderFromRegisteredProfile(r.Context(), profile, domain)
	if err != nil {
		http.Error(w, "Unable to create order", http.StatusInternalServerError)
		return
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

func createOrderFromRegisteredProfile(ctx context.Context, profile publicUserProfile, domain string) (string, error) {
	orderID, err := newPaymentOrderID()
	if err != nil {
		return "", err
	}
	wantedEmail := "info@" + domain
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
	`, orderID, customerID, domain, wantedEmail, "pending_payment", "Created from registered customer profile.", now, now)
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
