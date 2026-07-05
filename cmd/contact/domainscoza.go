package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultDomainsCoZaAPIURL = "https://api.domains.co.za/api"

type DomainsCoZaConfig struct {
	Enabled       bool
	APIURL        string
	Username      string
	Password      string
	Code          string
	RegisterPrice map[string]int
	RenewPrice    map[string]int
}

type DomainCheckResult struct {
	Domain        string `json:"domain"`
	SLD           string `json:"sld"`
	TLD           string `json:"tld"`
	Available     bool   `json:"available"`
	Premium       bool   `json:"premium"`
	UsesEPPKey    bool   `json:"uses_epp_key"`
	Message       string `json:"message"`
	Reason        string `json:"reason,omitempty"`
	RegisterPrice int    `json:"register_price,omitempty"`
	RenewPrice    int    `json:"renew_price,omitempty"`
	Currency      string `json:"currency"`
	Provider      string `json:"provider"`
}

type domainsCoZaClient struct {
	cfg        DomainsCoZaConfig
	httpClient *http.Client
}

type domainsCoZaLoginResponse struct {
	IntReturnCode int    `json:"intReturnCode"`
	StrMessage    string `json:"strMessage"`
	StrReason     string `json:"strReason"`
	Token         string `json:"token"`
}

type domainsCoZaCheckResponse struct {
	IntReturnCode int    `json:"intReturnCode"`
	StrMessage    string `json:"strMessage"`
	StrReason     string `json:"strReason"`
	UsesEPPKey    any    `json:"usesEppKey"`
	IsAvailable   any    `json:"isAvailable"`
	TLD           string `json:"tld"`
	SLD           string `json:"sld"`
	IsPremium     any    `json:"isPremium"`
}

type domainsCoZaActionResponse struct {
	IntReturnCode int    `json:"intReturnCode"`
	StrMessage    string `json:"strMessage"`
	StrReason     string `json:"strReason"`
	StrEPPMessage string `json:"strEppMessage"`
	StrEPPReason  string `json:"strEppReason"`
}

func loadDomainsCoZaConfig(path string) (DomainsCoZaConfig, error) {
	cfg := DomainsCoZaConfig{
		APIURL:        defaultDomainsCoZaAPIURL,
		RegisterPrice: map[string]int{},
		RenewPrice:    map[string]int{},
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
		case "username", "user":
			cfg.Username = value
		case "password", "api_key", "apikey", "key":
			cfg.Password = value
		case "code", "two_factor_code", "2fa_code":
			cfg.Code = value
		default:
			if strings.HasSuffix(key, "_register_price") {
				tld := strings.TrimSuffix(key, "_register_price")
				if cents, ok := parseRandPriceCents(value); ok {
					cfg.RegisterPrice[tld] = cents
				}
			} else if strings.HasSuffix(key, "_renew_price") {
				tld := strings.TrimSuffix(key, "_renew_price")
				if cents, ok := parseRandPriceCents(value); ok {
					cfg.RenewPrice[tld] = cents
				}
			}
		}
	}

	return cfg, nil
}

func (cfg DomainsCoZaConfig) validate() error {
	if !cfg.Enabled {
		return fmt.Errorf("Domains.co.za integration is disabled")
	}
	if cfg.APIURL == "" {
		return fmt.Errorf("Domains.co.za API URL is required")
	}
	if cfg.Username == "" {
		return fmt.Errorf("Domains.co.za username is required")
	}
	if cfg.Password == "" {
		return fmt.Errorf("Domains.co.za password is required")
	}
	return nil
}

func newDomainsCoZaClient(cfg DomainsCoZaConfig) *domainsCoZaClient {
	return &domainsCoZaClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 12 * time.Second,
		},
	}
}

func (c *domainsCoZaClient) CheckDomain(ctx context.Context, sld, tld string) (DomainCheckResult, error) {
	token, err := c.login(ctx)
	if err != nil {
		return DomainCheckResult{}, err
	}

	values := url.Values{}
	values.Set("sld", sld)
	values.Set("tld", tld)
	endpoint := c.cfg.APIURL + "/domain/check?" + values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return DomainCheckResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	var response domainsCoZaCheckResponse
	if err := c.doJSON(req, &response); err != nil {
		return DomainCheckResult{}, err
	}

	available := apiBool(response.IsAvailable)
	result := DomainCheckResult{
		Domain:        sld + "." + tld,
		SLD:           response.SLD,
		TLD:           response.TLD,
		Available:     available,
		Premium:       apiBool(response.IsPremium),
		UsesEPPKey:    apiBool(response.UsesEPPKey),
		Message:       response.StrMessage,
		Reason:        response.StrReason,
		RegisterPrice: c.cfg.RegisterPrice[priceKey(tld)],
		RenewPrice:    c.cfg.RenewPrice[priceKey(tld)],
		Currency:      "ZAR",
		Provider:      "domains.co.za",
	}
	if result.SLD == "" {
		result.SLD = sld
	}
	if result.TLD == "" {
		result.TLD = tld
	}
	if result.Domain == "." {
		result.Domain = result.SLD + "." + result.TLD
	}
	if response.IntReturnCode != 1 && response.IntReturnCode != 0 {
		return result, fmt.Errorf("Domains.co.za check failed: %s", response.StrMessage)
	}
	return result, nil
}

func (c *domainsCoZaClient) UpdateNameservers(ctx context.Context, sld, tld string, nameservers []string) error {
	if len(nameservers) < 2 {
		return fmt.Errorf("at least two nameservers are required")
	}
	if len(nameservers) > 5 {
		return fmt.Errorf("at most five nameservers are supported")
	}

	token, err := c.login(ctx)
	if err != nil {
		return err
	}

	values := url.Values{}
	values.Set("sld", sld)
	values.Set("tld", tld)
	values.Set("dns", "custom")
	for i, ns := range nameservers {
		ns = strings.ToLower(strings.TrimSpace(ns))
		if !validNameserver(ns) {
			return fmt.Errorf("invalid nameserver: %s", ns)
		}
		values.Set(fmt.Sprintf("ns%d", i+1), ns)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIURL+"/domain/ns", strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	var response domainsCoZaActionResponse
	if err := c.doJSON(req, &response); err != nil {
		return err
	}
	if response.IntReturnCode != 1 && response.IntReturnCode != 2 {
		if response.StrReason != "" {
			return fmt.Errorf("Domains.co.za nameserver update failed: %s", response.StrReason)
		}
		if response.StrEPPReason != "" {
			return fmt.Errorf("Domains.co.za nameserver update failed: %s", response.StrEPPReason)
		}
		return fmt.Errorf("Domains.co.za nameserver update failed: %s", response.StrMessage)
	}
	return nil
}

func (c *domainsCoZaClient) login(ctx context.Context) (string, error) {
	values := url.Values{}
	values.Set("username", c.cfg.Username)
	values.Set("password", c.cfg.Password)
	if c.cfg.Code != "" {
		values.Set("code", c.cfg.Code)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIURL+"/login", strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	var response domainsCoZaLoginResponse
	if err := c.doJSON(req, &response); err != nil {
		return "", err
	}
	if response.IntReturnCode != 1 || response.Token == "" {
		if response.StrReason != "" {
			return "", fmt.Errorf("Domains.co.za login failed: %s", response.StrReason)
		}
		return "", fmt.Errorf("Domains.co.za login failed: %s", response.StrMessage)
	}
	return response.Token, nil
}

func (c *domainsCoZaClient) doJSON(req *http.Request, target any) error {
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
		return fmt.Errorf("Domains.co.za returned HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("could not parse Domains.co.za response")
	}
	return nil
}

func splitZADomain(value string) (string, string, error) {
	domain := strings.ToLower(strings.TrimSpace(value))
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "www.")
	if i := strings.IndexAny(domain, "/?#"); i >= 0 {
		domain = domain[:i]
	}
	domain = strings.Trim(domain, ". ")
	if domain == "" {
		return "", "", fmt.Errorf("domain is required")
	}

	for _, tld := range []string{"co.za", "org.za", "net.za", "web.za"} {
		suffix := "." + tld
		if strings.HasSuffix(domain, suffix) {
			sld := strings.TrimSuffix(domain, suffix)
			if validDomainSLD(sld) {
				return sld, tld, nil
			}
			return "", "", fmt.Errorf("invalid domain name")
		}
	}
	return "", "", fmt.Errorf("only .co.za, .org.za, .net.za and .web.za checks are supported")
}

func validDomainSLD(value string) bool {
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

func validNameserver(value string) bool {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if value == "" || len(value) > 253 || !strings.Contains(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func parseConfigBool(value string) bool {
	return strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "yes") || strings.EqualFold(value, "on")
}

func parseRandPriceCents(value string) (int, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(value), "R"))
	if value == "" {
		return 0, false
	}
	if strings.Contains(value, ".") {
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, false
		}
		return int(f*100 + 0.5), true
	}
	i, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return i * 100, true
}

func priceKey(tld string) string {
	return strings.ReplaceAll(strings.ToLower(tld), ".", "_")
}

func apiBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return parseConfigBool(v)
	case float64:
		return v != 0
	case int:
		return v != 0
	default:
		return false
	}
}
