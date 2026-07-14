package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultPaystackAPIURL      = "https://api.paystack.co"
	defaultStarterOrderCents   = 15000
	defaultStarterOrderMessage = "LocalBox domain registration and starter setup"
)

type PaystackConfig struct {
	Enabled       bool
	APIURL        string
	SecretKey     string
	Currency      string
	CallbackURL   string
	StarterCents  int
	MarkupPercent int
}

type paystackClient struct {
	cfg        PaystackConfig
	httpClient *http.Client
}

var newPaystackHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
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

func loadPaystackConfig(path string) (PaystackConfig, error) {
	cfg := PaystackConfig{
		APIURL:        defaultPaystackAPIURL,
		Currency:      "ZAR",
		StarterCents:  defaultStarterOrderCents,
		MarkupPercent: 35,
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
			cfg.Enabled = parseConfigBool(value)
		case "api_url", "url":
			cfg.APIURL = strings.TrimRight(value, "/")
		case "secret_key", "secret", "api_key", "key":
			cfg.SecretKey = value
		case "currency":
			cfg.Currency = strings.ToUpper(value)
		case "callback_url", "return_url":
			cfg.CallbackURL = value
		case "starter_price", "starter_amount", "starter_order_price":
			if cents, ok := parseRandPriceCents(value); ok {
				cfg.StarterCents = cents
			}
		case "domain_markup_percent", "markup_percent":
			var parsed int
			if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil && parsed >= 0 && parsed <= 500 {
				cfg.MarkupPercent = parsed
			}
		}
	}
	return cfg, nil
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

func createImmediatePaystackCheckout(ctx context.Context, order starterOrder) (string, string, int, error) {
	cfg, err := loadPaystackConfig(paystackConfigFile)
	if err != nil {
		return "", "", 0, fmt.Errorf("Paystack config not available: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return "", "", 0, err
	}
	amountCents, err := quoteDomainPaymentAmount(ctx, order.Domain, cfg)
	if err != nil {
		return "", "", 0, err
	}
	client := newPaystackClient(cfg)
	reference := paystackReference(order.ID)
	paymentURL, returnedReference, err := client.initializeTransaction(ctx, paystackInitRequest{
		Email:       order.Email,
		Amount:      amountCents,
		Currency:    cfg.Currency,
		Reference:   reference,
		CallbackURL: cfg.CallbackURL,
		Metadata:    fmt.Sprintf(`{"order_id":%q,"domain":%q,"description":%q}`, order.ID, order.Domain, defaultStarterOrderMessage),
	})
	if err != nil {
		return "", "", 0, err
	}
	if returnedReference != "" {
		reference = returnedReference
	}
	return paymentURL, reference, amountCents, nil
}

func quoteDomainPaymentAmount(ctx context.Context, domain string, cfg PaystackConfig) (int, error) {
	result, err := checkDomainForPayment(ctx, domain)
	if err != nil {
		return 0, err
	}
	if !result.Available {
		return 0, fmt.Errorf("%s is no longer available", result.Domain)
	}
	return paymentAmountForDomainResult(ctx, result, cfg)
}

func paymentAmountForDomainResult(ctx context.Context, result DomainCheckResult, cfg PaystackConfig) (int, error) {
	if result.RegisterPrice <= 0 {
		return cfg.StarterCents, nil
	}
	sourceCurrency := strings.ToUpper(strings.TrimSpace(result.Currency))
	if sourceCurrency == "" {
		sourceCurrency = cfg.Currency
	}
	if strings.EqualFold(sourceCurrency, cfg.Currency) {
		return result.RegisterPrice, nil
	}
	if strings.EqualFold(sourceCurrency, "USD") && strings.EqualFold(cfg.Currency, "ZAR") {
		markedUpCents := addMarkup(result.RegisterPrice, cfg.MarkupPercent)
		zarCents, err := convertUSDToZARCents(ctx, markedUpCents)
		if err != nil {
			return 0, fmt.Errorf("could not convert Cloudflare USD price to ZAR: %w", err)
		}
		if zarCents <= 0 {
			return 0, fmt.Errorf("converted Cloudflare price is invalid")
		}
		return zarCents, nil
	}
	return 0, fmt.Errorf("domain price is in %s but Paystack is configured for %s", sourceCurrency, cfg.Currency)
}

func decorateDomainPaymentPrice(ctx context.Context, result DomainCheckResult, cfg PaystackConfig) DomainCheckResult {
	if !result.Available {
		return result
	}
	amount, err := paymentAmountForDomainResult(ctx, result, cfg)
	if err != nil {
		return result
	}
	result.PaymentPrice = amount
	result.PaymentCurrency = cfg.Currency
	return result
}

func addMarkup(cents, percent int) int {
	if cents <= 0 {
		return 0
	}
	if percent <= 0 {
		return cents
	}
	return (cents*(100+percent) + 99) / 100
}

func convertUSDToZARCents(ctx context.Context, usdCents int) (int, error) {
	if usdCents <= 0 {
		return 0, fmt.Errorf("USD amount required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.frankfurter.dev/v2/rate/USD/ZAR", nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("fx rate lookup failed: %s", resp.Status)
	}
	var payload struct {
		Rate float64 `json:"rate"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	if payload.Rate <= 0 {
		return 0, fmt.Errorf("fx rate unavailable")
	}
	return int(math.Ceil((float64(usdCents) / 100) * payload.Rate * 100)), nil
}

func checkDomainForPayment(ctx context.Context, domain string) (DomainCheckResult, error) {
	normalized, err := normalizePublicDomain(domain)
	if err != nil {
		return DomainCheckResult{}, err
	}
	sld, tld, ok := strings.Cut(normalized, ".")
	if !ok {
		return DomainCheckResult{}, fmt.Errorf("invalid domain")
	}
	switch tld {
	case "com", "net", "org", "info":
		cfg, err := loadCloudflareConfig(cloudflareConfigFile)
		if err != nil || !cfg.Enabled {
			return DomainCheckResult{}, fmt.Errorf("Cloudflare domain pricing is not configured")
		}
		if err := cfg.validate(); err != nil {
			return DomainCheckResult{}, err
		}
		return newCloudflareClient(cfg).CheckDomain(ctx, normalized)
	default:
		if strings.Count(normalized, ".") >= 2 {
			parts := strings.SplitN(normalized, ".", 2)
			sld = parts[0]
			tld = parts[1]
		}
		switch tld {
		case "co.za", "org.za", "net.za", "web.za":
			cfg, err := loadDomainsCoZaConfig(domainsCoZaConfigFile)
			if err != nil || !cfg.Enabled {
				return DomainCheckResult{}, fmt.Errorf("Domains.co.za domain pricing is not configured")
			}
			if err := cfg.validate(); err != nil {
				return DomainCheckResult{}, err
			}
			return newDomainsCoZaClient(cfg).CheckDomain(ctx, sld, tld)
		default:
			return DomainCheckResult{}, fmt.Errorf("unsupported domain extension")
		}
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
		return "", "", fmt.Errorf(response.Message)
	}
	return response.Data.AuthorizationURL, response.Data.Reference, nil
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
	return nil
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
