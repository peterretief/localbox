package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultPaystackAPIURL = "https://api.paystack.co"

type PaystackConfig struct {
	Enabled             bool
	APIURL              string
	PublicKey           string
	SecretKey           string
	Currency            string
	CallbackURL         string
	StarterCents        int
	DomainMarkupPercent int
}

type paystackClient struct {
	cfg        PaystackConfig
	httpClient *http.Client
}

var newPaystackHTTPClient = func() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
	}
}

type paystackInitRequest struct {
	Email       string `json:"email"`
	Amount      int    `json:"amount"`
	Currency    string `json:"currency,omitempty"`
	Reference   string `json:"reference"`
	CallbackURL string `json:"callback_url,omitempty"`
	Metadata    string `json:"metadata,omitempty"`
}

type paystackInitResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		AuthorizationURL string `json:"authorization_url"`
		Reference        string `json:"reference"`
	} `json:"data"`
}

type paystackVerifyResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	RawJSON string `json:"-"`
	Data    struct {
		Status    string `json:"status"`
		Reference string `json:"reference"`
		Amount    int    `json:"amount"`
		Currency  string `json:"currency"`
		Message   string `json:"message"`
	} `json:"data"`
}

func (r *paystackVerifyResponse) setPaystackRawJSON(raw string) {
	r.RawJSON = raw
}

type paystackWebhookEvent struct {
	Event string `json:"event"`
	Data  struct {
		Reference string `json:"reference"`
	} `json:"data"`
}

func loadPaystackConfig(path string) (PaystackConfig, error) {
	cfg := PaystackConfig{
		APIURL:              defaultPaystackAPIURL,
		Currency:            "ZAR",
		StarterCents:        15000,
		DomainMarkupPercent: 35,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "enabled":
			cfg.Enabled = parseBool(value)
		case "api_url", "url":
			cfg.APIURL = strings.TrimRight(value, "/")
		case "public_key":
			cfg.PublicKey = value
		case "secret_key", "secret", "api_key", "key":
			cfg.SecretKey = value
		case "currency":
			cfg.Currency = strings.ToUpper(value)
		case "callback_url", "return_url":
			cfg.CallbackURL = value
		case "starter_price", "starter_amount", "starter_order_price":
			if cents, err := parseRandCents(value); err == nil && cents > 0 {
				cfg.StarterCents = cents
			}
		case "domain_markup_percent", "markup_percent":
			var parsed int
			if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil && parsed >= 0 && parsed <= 500 {
				cfg.DomainMarkupPercent = parsed
			}
		}
	}
	return cfg, nil
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (cfg PaystackConfig) validate() error {
	if !cfg.Enabled {
		return fmt.Errorf("Paystack integration is disabled")
	}
	if cfg.APIURL == "" {
		return fmt.Errorf("Paystack API URL is required")
	}
	if cfg.SecretKey == "" {
		return fmt.Errorf("Paystack secret key is required")
	}
	if cfg.Currency == "" {
		return fmt.Errorf("Paystack currency is required")
	}
	if cfg.StarterCents <= 0 {
		return fmt.Errorf("Paystack starter price must be greater than zero")
	}
	return nil
}

func newPaystackClient(cfg PaystackConfig) *paystackClient {
	return &paystackClient{
		cfg:        cfg,
		httpClient: newPaystackHTTPClient(),
	}
}

func createPaystackPaymentForOrder(ctx context.Context, orderID, quotedAmount string) error {
	cents, err := parseRandCents(quotedAmount)
	if err != nil {
		return err
	}
	return createPaystackPaymentForOrderWithCurrency(ctx, orderID, cents, "")
}

func createPaystackPaymentForOrderWithCurrency(ctx context.Context, orderID string, cents int, currency string) error {
	if cents <= 0 {
		return fmt.Errorf("quoted amount must be greater than zero")
	}
	order, err := loadOrderDetail(orderID)
	if err != nil {
		return err
	}
	cfg, err := loadPaystackConfig(paystackConfigFile)
	if err != nil {
		return fmt.Errorf("Paystack config not available: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	currency = cleanCurrency(currency)
	if currency == "" {
		currency = cfg.Currency
	}
	if !supportedPaystackPaymentCurrency(currency) {
		return fmt.Errorf("Paystack payment currency %s is not supported by this checkout", currency)
	}
	client := newPaystackClient(cfg)
	reference := paystackReference(order.ID)
	paymentURL, returnedReference, err := client.initializeTransaction(ctx, paystackInitRequest{
		Email:       order.Email,
		Amount:      cents,
		Currency:    currency,
		Reference:   reference,
		CallbackURL: cfg.CallbackURL,
		Metadata:    fmt.Sprintf(`{"order_id":%q,"domain":%q}`, order.ID, order.Domain),
	})
	if err != nil {
		return err
	}
	if returnedReference != "" {
		reference = returnedReference
	}
	return updateOrderFields(order.ID, "pending_payment", "Paystack checkout link created.", map[string]any{
		"quoted_amount_cents": cents,
		"payment_currency":    currency,
		"payment_url":         paymentURL,
		"payment_reference":   reference,
	})
}

func starterPaymentAmountForCurrency(ctx context.Context, cfg PaystackConfig, targetCurrency string) (int, string, error) {
	sourceCurrency := cleanCurrency(cfg.Currency)
	if sourceCurrency == "" {
		sourceCurrency = "ZAR"
	}
	targetCurrency = cleanCurrency(targetCurrency)
	if targetCurrency == "" {
		targetCurrency = sourceCurrency
	}
	if !supportedPaystackPaymentCurrency(targetCurrency) {
		return 0, "", fmt.Errorf("Paystack payment currency %s is not supported by this checkout", targetCurrency)
	}
	if targetCurrency == sourceCurrency {
		return cfg.StarterCents, targetCurrency, nil
	}
	converted, _, err := estimateCurrencyAmount(ctx, float64(cfg.StarterCents)/100, sourceCurrency, targetCurrency)
	if err != nil {
		return 0, "", fmt.Errorf("could not convert %s starter price to %s: %w", sourceCurrency, targetCurrency, err)
	}
	cents := int(math.Round(converted * 100))
	if cents <= 0 {
		return 0, "", fmt.Errorf("converted payment amount is invalid")
	}
	return cents, targetCurrency, nil
}

func supportedPaystackPaymentCurrency(currency string) bool {
	switch cleanCurrency(currency) {
	case "ZAR", "USD":
		return true
	default:
		return false
	}
}

func (c *paystackClient) initializeTransaction(ctx context.Context, payload paystackInitRequest) (string, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIURL+"/transaction/initialize", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.SecretKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	var response paystackInitResponse
	if err := c.doJSON(req, &response); err != nil {
		return "", "", err
	}
	if !response.Status || response.Data.AuthorizationURL == "" {
		if response.Message == "" {
			response.Message = "Paystack did not return a checkout URL"
		}
		return "", "", errors.New(response.Message)
	}
	return response.Data.AuthorizationURL, response.Data.Reference, nil
}

func (c *paystackClient) verifyTransaction(ctx context.Context, reference string) (paystackVerifyResponse, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return paystackVerifyResponse{}, fmt.Errorf("payment reference required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.APIURL+"/transaction/verify/"+reference, nil)
	if err != nil {
		return paystackVerifyResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.SecretKey)
	req.Header.Set("Accept", "application/json")
	var response paystackVerifyResponse
	if err := c.doJSON(req, &response); err != nil {
		return paystackVerifyResponse{}, err
	}
	if !response.Status {
		if response.Message == "" {
			response.Message = "Paystack verification failed"
		}
		return response, errors.New(response.Message)
	}
	return response, nil
}

func (c *paystackClient) doJSON(req *http.Request, target any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Paystack API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, target); err != nil {
		return err
	}
	if rawTarget, ok := target.(interface{ setPaystackRawJSON(string) }); ok {
		rawTarget.setPaystackRawJSON(compactPaystackJSON(body))
	}
	return nil
}

func compactPaystackJSON(body []byte) string {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return strings.TrimSpace(string(body))
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return strings.TrimSpace(string(body))
	}
	return string(encoded)
}

func verifyPaystackPayment(ctx context.Context, reference string) error {
	cfg, err := loadPaystackConfig(paystackConfigFile)
	if err != nil {
		return fmt.Errorf("Paystack config not available: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	client := newPaystackClient(cfg)
	response, err := client.verifyTransaction(ctx, reference)
	if err != nil {
		return err
	}
	if response.Data.Status != "success" {
		message := strings.TrimSpace(response.Data.Message)
		if message == "" {
			message = "Paystack transaction is " + response.Data.Status
		}
		_ = markPaystackPaymentFailed(response.Data.Reference, response.Data.Status, message, response.RawJSON)
		return errors.New(message)
	}
	return markPaystackPaymentVerified(response.Data.Reference, response.Data.Amount, response.Data.Currency)
}

func orderIDForPaymentReference(ctx context.Context, reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return "", fmt.Errorf("payment reference required")
	}
	db, err := openOrdersDB()
	if err != nil {
		return "", err
	}
	defer db.Close()
	if err := ensureOrdersAdminSchema(ctx, db); err != nil {
		return "", err
	}
	var orderID string
	err = db.QueryRowContext(ctx, `
		SELECT id
		FROM orders
		WHERE payment_reference = ?
	`, reference).Scan(&orderID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("order not found for payment reference")
		}
		return "", err
	}
	return orderID, nil
}

func markPaystackPaymentVerified(reference string, amount int, currency string) error {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return fmt.Errorf("payment reference required")
	}
	db, err := openOrdersDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		return err
	}
	var orderID, status, paymentCurrency string
	var quotedAmount int
	err = db.QueryRow(`
		SELECT id, status, quoted_amount_cents, payment_currency
		FROM orders
		WHERE payment_reference = ?
	`, reference).Scan(&orderID, &status, &quotedAmount, &paymentCurrency)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("order not found for payment reference")
		}
		return err
	}
	if quotedAmount != amount {
		return fmt.Errorf("payment amount mismatch: expected %d got %d", quotedAmount, amount)
	}
	if !strings.EqualFold(cleanCurrency(paymentCurrency), cleanCurrency(currency)) {
		return fmt.Errorf("payment currency mismatch: expected %s got %s", paymentCurrency, currency)
	}
	if status == "paid" {
		return nil
	}
	return updateOrderFields(orderID, "paid", "Paystack payment verified.", map[string]any{
		"payment_reference": reference,
	})
}

func markPaystackPaymentFailed(reference, paystackStatus, message, rawJSON string) error {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return fmt.Errorf("payment reference required")
	}
	paystackStatus = strings.TrimSpace(paystackStatus)
	if paystackStatus == "" {
		paystackStatus = "failed"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Paystack payment did not complete."
	}
	rawJSON = strings.TrimSpace(rawJSON)
	db, err := openOrdersDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		return err
	}
	var orderID, status string
	err = db.QueryRow(`
		SELECT id, status
		FROM orders
		WHERE payment_reference = ?
	`, reference).Scan(&orderID, &status)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("order not found for payment reference")
		}
		return err
	}
	if status == "paid" {
		return nil
	}
	eventMessage := "Paystack payment not completed: " + message
	if rawJSON != "" {
		eventMessage += "\nPaystack verify JSON: " + rawJSON
	}
	return updateOrderFields(orderID, "payment_failed", eventMessage, map[string]any{
		"payment_reference": reference,
	})
}

func validPaystackSignature(secret string, body []byte, signature string) bool {
	signature = strings.TrimSpace(signature)
	if secret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(strings.ToLower(signature)))
}

func paystackReference(orderID string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-', r == '.', r == '=':
			return r
		default:
			return '-'
		}
	}, strings.TrimSpace(orderID))
	return "lb-" + strings.Trim(cleaned, "-")
}
