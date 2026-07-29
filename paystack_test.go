package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreatePaystackPaymentForOrderStoresCheckoutLink(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath

	withPaystackTransport(t, func(r *http.Request) string {
		if r.URL.Path != "/transaction/initialize" {
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test_local" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		return `{"status":true,"message":"Authorization URL created","data":{"authorization_url":"https://checkout.paystack.test/pay","reference":"lb-ord-test"}}`
	})
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")

	if err := createPaystackPaymentForOrder(context.Background(), "ord_test", "R150.00"); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status, paymentURL, reference string
	var amount int
	if err := db.QueryRow(`SELECT status, payment_url, payment_reference, quoted_amount_cents FROM orders WHERE id = 'ord_test'`).Scan(&status, &paymentURL, &reference, &amount); err != nil {
		t.Fatal(err)
	}
	if status != "pending_payment" || paymentURL != "https://checkout.paystack.test/pay" || reference != "lb-ord-test" || amount != 15000 {
		t.Fatalf("unexpected payment values: status=%q url=%q ref=%q amount=%d", status, paymentURL, reference, amount)
	}
}

func TestPaystackReturnShowsOrderReference(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")

	withPaystackTransport(t, func(r *http.Request) string {
		if r.URL.Path != "/transaction/verify/lb-ord-test" {
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
		return `{"status":true,"message":"Verification successful","data":{"status":"success","reference":"lb-ord-test","amount":15000,"currency":"ZAR"}}`
	})

	req := httptest.NewRequest(http.MethodGet, "/payment/paystack/return?reference=lb-ord-test", nil)
	rec := httptest.NewRecorder()
	paystackReturnHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Order reference:") || !strings.Contains(body, "ord_test") {
		t.Fatalf("return page missing order reference:\n%s", body)
	}
	if !strings.Contains(body, "/order/login?order_id=ord_test") {
		t.Fatalf("return page missing prefilled progress link:\n%s", body)
	}
}

func TestPaystackReturnRedirectsFailedPaymentBackToRegisterForm(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")

	withPaystackTransport(t, func(r *http.Request) string {
		if r.URL.Path != "/transaction/verify/lb-ord-test" {
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
		return `{"status":true,"message":"Verification successful","data":{"status":"failed","reference":"lb-ord-test","amount":15000,"currency":"ZAR","message":"Declined"}}`
	})

	req := httptest.NewRequest(http.MethodGet, "/payment/paystack/return?reference=lb-ord-test", nil)
	rec := httptest.NewRecorder()
	paystackReturnHandler(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/register?") || !strings.Contains(location, "payment=failed") || !strings.Contains(location, "next=%2Fpayment%2Fstart%3Fdomain%3Dexample.co.za") {
		t.Fatalf("expected failed payment register redirect, got %q", location)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRow(`SELECT status FROM orders WHERE id = 'ord_test'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "payment_failed" {
		t.Fatalf("expected payment_failed status, got %q", status)
	}
}

func TestPaymentStartChargesDomainQuoteInZAREvenForUSDProfile(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")
	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street, Cape Town", "secret-password", "USD"); err != nil {
		t.Fatal(err)
	}
	oldChecker := domainPaymentChecker
	domainPaymentChecker = func(ctx context.Context, domain string) (paymentDomainCheckResult, error) {
		return paymentDomainCheckResult{
			Domain:        domain,
			Available:     true,
			RegisterPrice: 1000,
			Currency:      "USD",
			Provider:      "cloudflare",
		}, nil
	}
	oldFetcher := defaultFXRateFetcher
	defaultFXRateFetcher = func(ctx context.Context, source, target string) (float64, error) {
		if source != "USD" || target != "ZAR" {
			t.Fatalf("unexpected FX pair %s -> %s", source, target)
		}
		return 18.5, nil
	}
	fxRatesMu.Lock()
	fxRatesCache = map[string]cachedFXRate{}
	fxRatesMu.Unlock()
	t.Cleanup(func() {
		domainPaymentChecker = oldChecker
		defaultFXRateFetcher = oldFetcher
		fxRatesMu.Lock()
		fxRatesCache = map[string]cachedFXRate{}
		fxRatesMu.Unlock()
	})

	withPaystackTransport(t, func(r *http.Request) string {
		var payload paystackInitRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Amount != 24975 || payload.Currency != "ZAR" {
			t.Fatalf("expected ZAR 249.75 payload, got amount=%d currency=%q", payload.Amount, payload.Currency)
		}
		return `{"status":true,"message":"Authorization URL created","data":{"authorization_url":"https://checkout.paystack.test/pay","reference":"lb-usd-test"}}`
	})

	req := httptest.NewRequest(http.MethodGet, "http://localboxmail.co.za/payment/start?domain=chosen.co.za", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSessionWithCurrency("buyer", "localboxmail.co.za", "USD")})
	rec := httptest.NewRecorder()

	paymentStartHandler(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected Paystack redirect, code=%d body=%q", rec.Code, rec.Body.String())
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var amount int
	var currency string
	if err := db.QueryRow(`SELECT quoted_amount_cents, payment_currency FROM orders WHERE domain = 'chosen.co.za' ORDER BY created_at DESC LIMIT 1`).Scan(&amount, &currency); err != nil {
		t.Fatal(err)
	}
	if amount != 24975 || currency != "ZAR" {
		t.Fatalf("expected stored ZAR 249.75, got amount=%d currency=%q", amount, currency)
	}
}

func TestQuoteDomainPaymentAmountConvertsCloudflareUSDToZARWithMarkup(t *testing.T) {
	oldChecker := domainPaymentChecker
	domainPaymentChecker = func(ctx context.Context, domain string) (paymentDomainCheckResult, error) {
		if domain != "example.com" {
			t.Fatalf("unexpected domain %q", domain)
		}
		return paymentDomainCheckResult{
			Domain:        domain,
			Available:     true,
			RegisterPrice: 1000,
			Currency:      "USD",
			Provider:      "cloudflare",
		}, nil
	}
	oldFetcher := defaultFXRateFetcher
	defaultFXRateFetcher = func(ctx context.Context, source, target string) (float64, error) {
		if source != "USD" || target != "ZAR" {
			t.Fatalf("unexpected FX pair %s -> %s", source, target)
		}
		return 18.5, nil
	}
	fxRatesMu.Lock()
	fxRatesCache = map[string]cachedFXRate{}
	fxRatesMu.Unlock()
	t.Cleanup(func() {
		domainPaymentChecker = oldChecker
		defaultFXRateFetcher = oldFetcher
		fxRatesMu.Lock()
		fxRatesCache = map[string]cachedFXRate{}
		fxRatesMu.Unlock()
	})

	amount, currency, err := quoteDomainPaymentAmountZAR(context.Background(), "example.com", PaystackConfig{
		Currency:            "ZAR",
		StarterCents:        15000,
		DomainMarkupPercent: 35,
	})
	if err != nil {
		t.Fatal(err)
	}
	if amount != 24975 || currency != "ZAR" {
		t.Fatalf("expected USD 10 + 35%% converted to R249.75, got amount=%d currency=%q", amount, currency)
	}
}

func TestQuoteDomainPaymentAmountKeepsDomainsCoZaZARWithoutExtraMarkup(t *testing.T) {
	oldChecker := domainPaymentChecker
	domainPaymentChecker = func(ctx context.Context, domain string) (paymentDomainCheckResult, error) {
		return paymentDomainCheckResult{
			Domain:        domain,
			Available:     true,
			RegisterPrice: 15000,
			Currency:      "ZAR",
			Provider:      "domains.co.za",
		}, nil
	}
	t.Cleanup(func() { domainPaymentChecker = oldChecker })

	amount, currency, err := quoteDomainPaymentAmountZAR(context.Background(), "example.co.za", PaystackConfig{
		Currency:            "ZAR",
		StarterCents:        15000,
		DomainMarkupPercent: 35,
	})
	if err != nil {
		t.Fatal(err)
	}
	if amount != 15000 || currency != "ZAR" {
		t.Fatalf("expected configured R150 without extra markup, got amount=%d currency=%q", amount, currency)
	}
}

func TestPaystackWebhookMarksPaidAfterVerifiedSignatureAndTransaction(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath

	withPaystackTransport(t, func(r *http.Request) string {
		if r.URL.Path != "/transaction/verify/lb-ord-test" {
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
		return `{"status":true,"message":"Verification successful","data":{"status":"success","reference":"lb-ord-test","amount":15000,"currency":"ZAR"}}`
	})
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")

	body := []byte(`{"event":"charge.success","data":{"reference":"lb-ord-test"}}`)
	req := httptest.NewRequest(http.MethodPost, "/payment/paystack/webhook", strings.NewReader(string(body)))
	req.Header.Set("x-paystack-signature", paystackSignature("sk_test_local", body))
	rec := httptest.NewRecorder()

	paystackWebhookHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRow(`SELECT status FROM orders WHERE id = 'ord_test'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "paid" {
		t.Fatalf("expected paid status, got %q", status)
	}
}

func TestPaystackWebhookRejectsBadSignature(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	writePaystackConfig(t, configPath, "https://paystack.invalid", "sk_test_local")

	req := httptest.NewRequest(http.MethodPost, "/payment/paystack/webhook", strings.NewReader(`{"event":"charge.success","data":{"reference":"lb-ord-test"}}`))
	req.Header.Set("x-paystack-signature", "bad")
	rec := httptest.NewRecorder()

	paystackWebhookHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestVerifyPaystackPaymentMarksFailedTransaction(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")

	withPaystackTransport(t, func(r *http.Request) string {
		if r.URL.Path != "/transaction/verify/lb-ord-test" {
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
		return `{"status":true,"message":"Verification successful","data":{"status":"failed","reference":"lb-ord-test","amount":15000,"currency":"ZAR","message":"Declined"}}`
	})

	if err := verifyPaystackPayment(context.Background(), "lb-ord-test"); err == nil {
		t.Fatal("expected failed transaction error")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status, message string
	if err := db.QueryRow(`SELECT status FROM orders WHERE id = 'ord_test'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "payment_failed" {
		t.Fatalf("expected payment_failed status, got %q", status)
	}
	if err := db.QueryRow(`SELECT message FROM order_events WHERE order_id = 'ord_test' AND event_type = 'payment_failed' ORDER BY id DESC LIMIT 1`).Scan(&message); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, `"status":"failed"`) || !strings.Contains(message, `"message":"Declined"`) {
		t.Fatalf("expected failed Paystack JSON in event, got %q", message)
	}
}

func TestPaystackVerificationRejectsCurrencyMismatch(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")

	withPaystackTransport(t, func(r *http.Request) string {
		return `{"status":true,"message":"Verification successful","data":{"status":"success","reference":"lb-ord-test","amount":15000,"currency":"USD"}}`
	})

	if err := verifyPaystackPayment(context.Background(), "lb-ord-test"); err == nil || !strings.Contains(err.Error(), "currency mismatch") {
		t.Fatalf("expected currency mismatch, got %v", err)
	}
}

func TestPaymentStartUsesRegisteredProfileForPaystackCheckout(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")
	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street, Cape Town", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}
	oldChecker := domainPaymentChecker
	domainPaymentChecker = func(ctx context.Context, domain string) (paymentDomainCheckResult, error) {
		return paymentDomainCheckResult{
			Domain:        domain,
			Available:     true,
			RegisterPrice: 15000,
			Currency:      "ZAR",
			Provider:      "domains.co.za",
		}, nil
	}
	t.Cleanup(func() { domainPaymentChecker = oldChecker })
	withPaystackTransport(t, func(r *http.Request) string {
		if r.URL.Path != "/transaction/initialize" {
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
		return `{"status":true,"message":"Authorization URL created","data":{"authorization_url":"https://checkout.paystack.test/pay","reference":"lb-ord-start"}}`
	})

	req := httptest.NewRequest(http.MethodGet, "http://localboxmail.co.za/payment/start?domain=chosen.co.za&option=localbox-hosting", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	paymentStartHandler(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "https://checkout.paystack.test/pay" {
		t.Fatalf("expected Paystack redirect, code=%d location=%q body=%q", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var fullName, email, domain, wantedEmail, status, notes string
	var amount int
	if err := db.QueryRow(`
		SELECT customers.full_name, customers.email, orders.domain, orders.wanted_email, orders.status, orders.notes, orders.quoted_amount_cents
		FROM orders
		JOIN customers ON customers.id = orders.customer_id
		WHERE orders.domain = 'chosen.co.za'
	`).Scan(&fullName, &email, &domain, &wantedEmail, &status, &notes, &amount); err != nil {
		t.Fatal(err)
	}
	if fullName != "Buyer Example" || email != "buyer@example.net" || domain != "chosen.co.za" || wantedEmail != "info@chosen.co.za" || status != "pending_payment" || amount != 15000 {
		t.Fatalf("unexpected order values: name=%q email=%q domain=%q wantedEmail=%q status=%q amount=%d", fullName, email, domain, wantedEmail, status, amount)
	}
	if !strings.Contains(notes, "Selected LocalBox option: Host it with LocalBox") {
		t.Fatalf("expected selected option in notes, got %q", notes)
	}
}

func TestPaymentStartLeavesWantedEmailBlankForParkedDomain(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")
	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street, Cape Town", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}
	oldChecker := domainPaymentChecker
	domainPaymentChecker = func(ctx context.Context, domain string) (paymentDomainCheckResult, error) {
		return paymentDomainCheckResult{
			Domain:        domain,
			Available:     true,
			RegisterPrice: 15000,
			Currency:      "ZAR",
			Provider:      "domains.co.za",
		}, nil
	}
	t.Cleanup(func() { domainPaymentChecker = oldChecker })
	withPaystackTransport(t, func(r *http.Request) string {
		return `{"status":true,"message":"Authorization URL created","data":{"authorization_url":"https://checkout.paystack.test/pay","reference":"lb-ord-start"}}`
	})

	req := httptest.NewRequest(http.MethodGet, "http://localboxmail.co.za/payment/start?domain=parked.co.za", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	paymentStartHandler(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected Paystack redirect, code=%d body=%q", rec.Code, rec.Body.String())
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var wantedEmail, notes string
	if err := db.QueryRow(`SELECT wanted_email, notes FROM orders WHERE domain = 'parked.co.za'`).Scan(&wantedEmail, &notes); err != nil {
		t.Fatal(err)
	}
	if wantedEmail != "" {
		t.Fatalf("parked domain should not request first email, got %q", wantedEmail)
	}
	if !strings.Contains(notes, "Selected LocalBox option: Park it for free") {
		t.Fatalf("expected parking option in notes, got %q", notes)
	}
}

func TestPaymentStartRedirectsToAccountDomainsForAlreadyPaidDomain(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")
	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street, Cape Town", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO customers (id, full_name, email, phone, physical_address, created_at)
		VALUES (2, 'Buyer Example', 'buyer@example.net', '+27 82 123 4567', '1 Example Street, Cape Town', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, payment_url, payment_reference, quoted_amount_cents, created_at, updated_at)
		VALUES ('ord_paid', 2, 'chosen.co.za', '', 'paid', 'https://checkout.paystack.test/old', 'lb-paid', 15000, ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	req := httptest.NewRequest(http.MethodGet, "http://localboxmail.co.za/payment/start?domain=chosen.co.za", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	paymentStartHandler(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/domains" {
		t.Fatalf("expected account domains redirect, code=%d location=%q body=%q", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM orders WHERE domain = 'chosen.co.za'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected existing paid order to be reused, got %d orders", count)
	}
}

func TestPaymentStartResumesExistingPendingOrderWithoutPaymentURL(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")
	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street, Cape Town", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}
	oldChecker := domainPaymentChecker
	domainPaymentChecker = func(ctx context.Context, domain string) (paymentDomainCheckResult, error) {
		return paymentDomainCheckResult{
			Domain:        domain,
			Available:     true,
			RegisterPrice: 15000,
			Currency:      "ZAR",
			Provider:      "domains.co.za",
		}, nil
	}
	t.Cleanup(func() { domainPaymentChecker = oldChecker })
	withPaystackTransport(t, func(r *http.Request) string {
		return `{"status":true,"message":"Authorization URL created","data":{"authorization_url":"https://checkout.paystack.test/resume","reference":"lb-ord-resume"}}`
	})
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO customers (id, full_name, email, phone, physical_address, created_at)
		VALUES (2, 'Buyer Example', 'buyer@example.net', '+27 82 123 4567', '1 Example Street, Cape Town', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, created_at, updated_at)
		VALUES ('ord_pending', 2, 'chosen.co.za', '', 'pending_payment', ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	req := httptest.NewRequest(http.MethodGet, "http://localboxmail.co.za/payment/start?domain=chosen.co.za", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	paymentStartHandler(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "https://checkout.paystack.test/resume" {
		t.Fatalf("expected resumed Paystack redirect, code=%d location=%q body=%q", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	var paymentURL string
	if err := db.QueryRow(`SELECT count(*), max(payment_url) FROM orders WHERE domain = 'chosen.co.za'`).Scan(&count, &paymentURL); err != nil {
		t.Fatal(err)
	}
	if count != 1 || paymentURL != "https://checkout.paystack.test/resume" {
		t.Fatalf("expected existing order payment URL to be updated, count=%d paymentURL=%q", count, paymentURL)
	}
}

func TestPaymentStartRejectsDomainWithActiveOrderForAnotherCustomer(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	writePaystackConfig(t, configPath, "https://api.paystack.test", "sk_test_local")
	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street, Cape Town", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}
	if err := registerPublicUserProfile("localboxmail.co.za", "other", "other@example.net", "Other Example", "+27 82 123 4567", "2 Example Street, Cape Town", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO customers (id, full_name, email, phone, physical_address, created_at)
		VALUES (2, 'Buyer Example', 'buyer@example.net', '+27 82 123 4567', '1 Example Street, Cape Town', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, payment_url, payment_reference, quoted_amount_cents, created_at, updated_at)
		VALUES ('ord_paid', 2, 'chosen.co.za', '', 'paid', 'https://checkout.paystack.test/old', 'lb-paid', 15000, ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	req := httptest.NewRequest(http.MethodGet, "http://localboxmail.co.za/payment/start?domain=chosen.co.za", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("other", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	paymentStartHandler(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected conflict for already ordered domain, code=%d location=%q body=%q", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
}

func TestPaymentStartRejectsAnonymousCheckoutForActiveOrder(t *testing.T) {
	dbPath, configPath := setupPaystackOrderTest(t)
	orderDBFile = dbPath
	paystackConfigFile = configPath
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO customers (id, full_name, email, phone, physical_address, created_at)
		VALUES (2, 'Buyer Example', 'buyer@example.net', '+27 82 123 4567', '1 Example Street, Cape Town', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, payment_url, payment_reference, quoted_amount_cents, created_at, updated_at)
		VALUES ('ord_paid', 2, 'chosen.co.za', '', 'paid', 'https://checkout.paystack.test/old', 'lb-paid', 15000, ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	req := httptest.NewRequest(http.MethodGet, "http://localboxmail.co.za/payment/start?domain=chosen.co.za", nil)
	rec := httptest.NewRecorder()

	paymentStartHandler(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected conflict for anonymous checkout on active order, code=%d location=%q body=%q", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
}

func TestPaystackTestModeEnabledDetectsTestKeys(t *testing.T) {
	oldConfig := paystackConfigFile
	paystackConfigFile = filepath.Join(t.TempDir(), "paystack.conf")
	t.Cleanup(func() { paystackConfigFile = oldConfig })

	writePaystackConfig(t, paystackConfigFile, "https://api.paystack.test", "sk_test_local")

	if !paystackTestModeEnabled() {
		t.Fatal("expected Paystack test mode to be detected")
	}
}

func TestLegacyNameserverOptionMapsToFreeParking(t *testing.T) {
	option, err := normalizeLocalBoxPackageOption("own-nameservers")
	if err != nil {
		t.Fatal(err)
	}
	if option != "free-parking" {
		t.Fatalf("option = %q, want free-parking", option)
	}
	if got := localBoxPackageOptionLabel(option); got != "Park it for free" {
		t.Fatalf("unexpected legacy nameserver label: %q", got)
	}
}

func setupPaystackOrderTest(t *testing.T) (string, string) {
	t.Helper()
	oldDB := orderDBFile
	oldConfig := paystackConfigFile
	dbPath := filepath.Join(t.TempDir(), "localbox.db")
	configPath := filepath.Join(t.TempDir(), "paystack.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		paystackConfigFile = oldConfig
	})

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO customers (id, full_name, email, phone, physical_address, created_at)
		VALUES (1, 'Peter Example', 'peter@example.com', '+27 82 123 4567', '1 Example Street', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, quoted_amount_cents, payment_reference, created_at, updated_at)
		VALUES ('ord_test', 1, 'example.co.za', 'info@example.co.za', 'quoted', 15000, 'lb-ord-test', ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	return dbPath, configPath
}

func writePaystackConfig(t *testing.T, path, apiURL, secret string) {
	t.Helper()
	content := fmt.Sprintf("enabled=true\napi_url=%s\ncurrency=ZAR\nstarter_price=150.00\nsecret_key=%s\ncallback_url=https://localbox.test/payment/paystack/return\n", apiURL, secret)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func paystackSignature(secret string, body []byte) string {
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func withPaystackTransport(t *testing.T, handler func(*http.Request) string) {
	t.Helper()
	old := newPaystackHTTPClient
	newPaystackHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body := handler(r)
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    r,
			}, nil
		})}
	}
	t.Cleanup(func() { newPaystackHTTPClient = old })
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
