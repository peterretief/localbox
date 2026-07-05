package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultCloudflareAPIURL = "https://api.cloudflare.com/client/v4"

type CloudflareConfig struct {
	Enabled   bool
	APIURL    string
	AccountID string
	APIToken  string
}

type cloudflareClient struct {
	cfg        CloudflareConfig
	httpClient *http.Client
}

type cloudflareEnvelope struct {
	Success bool                   `json:"success"`
	Result  cloudflareCheckResult  `json:"result"`
	Errors  []cloudflareAPIMessage `json:"errors"`
}

type cloudflareCheckResult struct {
	Domains []cloudflareDomainResult `json:"domains"`
}

type cloudflareDomainResult struct {
	Name        string            `json:"name"`
	Registrable bool              `json:"registrable"`
	Pricing     cloudflarePricing `json:"pricing"`
	Reason      string            `json:"reason"`
	Tier        string            `json:"tier"`
}

type cloudflarePricing struct {
	Currency         string `json:"currency"`
	RegistrationCost string `json:"registration_cost"`
	RenewalCost      string `json:"renewal_cost"`
}

type cloudflareAPIMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func loadCloudflareConfig(path string) (CloudflareConfig, error) {
	cfg := CloudflareConfig{APIURL: defaultCloudflareAPIURL}
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
		case "account_id", "account":
			cfg.AccountID = value
		case "api_token", "token", "api_key", "apikey", "key":
			cfg.APIToken = value
		}
	}

	return cfg, nil
}

func (cfg CloudflareConfig) validate() error {
	if !cfg.Enabled {
		return fmt.Errorf("Cloudflare integration is disabled")
	}
	if cfg.APIURL == "" {
		return fmt.Errorf("Cloudflare API URL is required")
	}
	if cfg.AccountID == "" {
		return fmt.Errorf("Cloudflare account ID is required")
	}
	if cfg.APIToken == "" {
		return fmt.Errorf("Cloudflare API token is required")
	}
	return nil
}

func newCloudflareClient(cfg CloudflareConfig) *cloudflareClient {
	return &cloudflareClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 12 * time.Second,
		},
	}
}

func (c *cloudflareClient) CheckDomain(ctx context.Context, domain string) (DomainCheckResult, error) {
	results, err := c.checkDomains(ctx, []string{domain})
	if err != nil {
		return DomainCheckResult{}, err
	}
	if len(results) == 0 {
		return DomainCheckResult{}, fmt.Errorf("Cloudflare did not return a domain result")
	}
	return results[0], nil
}

func (c *cloudflareClient) SearchDomains(ctx context.Context, sld string, tlds []string) ([]DomainCheckResult, error) {
	if !validDomainSLD(sld) {
		return nil, fmt.Errorf("invalid domain name")
	}
	var domains []string
	for _, tld := range tlds {
		tld = strings.Trim(strings.ToLower(strings.TrimSpace(tld)), ".")
		if tld == "" || strings.Contains(tld, ".") || !validDomainLabel(tld) {
			continue
		}
		domains = append(domains, sld+"."+tld)
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("no valid TLDs to search")
	}
	return c.checkDomains(ctx, domains)
}

func (c *cloudflareClient) checkDomains(ctx context.Context, domains []string) ([]DomainCheckResult, error) {
	payload := map[string][]string{"domains": domains}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	endpoint := c.cfg.APIURL + "/accounts/" + c.cfg.AccountID + "/registrar/domain-check"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	var response cloudflareEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return nil, err
	}
	if !response.Success {
		return nil, fmt.Errorf("Cloudflare check failed: %s", cloudflareErrorMessage(response.Errors))
	}
	if len(response.Result.Domains) == 0 {
		return nil, fmt.Errorf("Cloudflare did not return a domain result")
	}

	var results []DomainCheckResult
	for _, checked := range response.Result.Domains {
		results = append(results, cloudflareResultToDomainCheck(checked))
	}
	return results, nil
}

func cloudflareResultToDomainCheck(checked cloudflareDomainResult) DomainCheckResult {
	sld, tld := splitDomainForDisplay(checked.Name)
	result := DomainCheckResult{
		Domain:        checked.Name,
		SLD:           sld,
		TLD:           tld,
		Available:     checked.Registrable,
		Premium:       checked.Tier == "premium",
		Message:       cloudflareDomainMessage(checked),
		Reason:        checked.Reason,
		RegisterPrice: parseCloudflarePriceCents(checked.Pricing.RegistrationCost),
		RenewPrice:    parseCloudflarePriceCents(checked.Pricing.RenewalCost),
		Currency:      checked.Pricing.Currency,
		Provider:      "cloudflare",
	}
	if result.Domain == "" {
		result.Domain = checked.Name
	}
	if result.Currency == "" {
		result.Currency = "USD"
	}
	return result
}

func (c *cloudflareClient) doJSON(req *http.Request, target any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Cloudflare returned HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("could not parse Cloudflare response")
	}
	return nil
}

func cloudflareDomainMessage(domain cloudflareDomainResult) string {
	if domain.Registrable {
		return "Domain Available"
	}
	if domain.Reason != "" {
		return domain.Reason
	}
	return "Domain Not Available"
}

func cloudflareErrorMessage(errors []cloudflareAPIMessage) string {
	if len(errors) == 0 {
		return "unknown error"
	}
	return errors[0].Message
}

func parseCloudflarePriceCents(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return int(f*100 + 0.5)
}

func splitDomainForDisplay(domain string) (string, string) {
	parts := strings.SplitN(domain, ".", 2)
	if len(parts) != 2 {
		return domain, ""
	}
	return parts[0], parts[1]
}
