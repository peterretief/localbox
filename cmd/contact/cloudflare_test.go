package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCloudflareConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloudflare.conf")
	if err := os.WriteFile(path, []byte(`
enabled=true
api_url=https://api.cloudflare.test/client/v4
account_id=account123
api_key=token123
`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadCloudflareConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.APIURL != "https://api.cloudflare.test/client/v4" || cfg.AccountID != "account123" || cfg.APIToken != "token123" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestCloudflareCheckDomain(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/accounts/account123/registrar/domain-check" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token123" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(body), `"example.com"`) {
			t.Fatalf("expected domain in request body, got %s", string(body))
		}

		return jsonResponse(t, cloudflareEnvelope{
			Success: true,
			Result: cloudflareCheckResult{
				Domains: []cloudflareDomainResult{
					{
						Name:        "example.com",
						Registrable: true,
						Tier:        "standard",
						Pricing: cloudflarePricing{
							Currency:         "USD",
							RegistrationCost: "10.44",
							RenewalCost:      "10.44",
						},
					},
				},
			},
		}), nil
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:   true,
		APIURL:    "https://api.cloudflare.test",
		AccountID: "account123",
		APIToken:  "token123",
	})
	client.httpClient.Transport = transport

	result, err := client.CheckDomain(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available || result.Provider != "cloudflare" || result.RegisterPrice != 1044 || result.RenewPrice != 1044 || result.Currency != "USD" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCloudflareSearchDomains(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		bodyText := string(body)
		for _, domain := range []string{"example.com", "example.net", "example.org"} {
			if !strings.Contains(bodyText, `"`+domain+`"`) {
				t.Fatalf("expected %s in request body, got %s", domain, bodyText)
			}
		}
		return jsonResponse(t, cloudflareEnvelope{
			Success: true,
			Result: cloudflareCheckResult{
				Domains: []cloudflareDomainResult{
					{Name: "example.com", Registrable: false, Reason: "registered"},
					{Name: "example.net", Registrable: true, Pricing: cloudflarePricing{Currency: "USD", RegistrationCost: "12.00", RenewalCost: "12.00"}},
					{Name: "example.org", Registrable: true, Pricing: cloudflarePricing{Currency: "USD", RegistrationCost: "9.50", RenewalCost: "9.50"}},
				},
			},
		}), nil
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:   true,
		APIURL:    "https://api.cloudflare.test",
		AccountID: "account123",
		APIToken:  "token123",
	})
	client.httpClient.Transport = transport

	results, err := client.SearchDomains(context.Background(), "example", []string{"com", "net", "org"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[1].Domain != "example.net" || !results[1].Available || results[1].RegisterPrice != 1200 {
		t.Fatalf("unexpected second result: %+v", results[1])
	}
}

func TestNormalizePublicDomain(t *testing.T) {
	got, err := normalizePublicDomain("https://www.Example.COM/path")
	if err != nil {
		t.Fatal(err)
	}
	if got != "example.com" {
		t.Fatalf("expected example.com, got %q", got)
	}
}

func TestNormalizeSearchSLD(t *testing.T) {
	if got := normalizeSearchSLD("https://www.Example.com/path"); got != "example" {
		t.Fatalf("expected example, got %q", got)
	}
}

func TestNormalizePublicDomainRejectsBadInput(t *testing.T) {
	for _, input := range []string{"", "localhost", "-bad.com", "bad_.com"} {
		if _, err := normalizePublicDomain(input); err == nil {
			t.Fatalf("expected %q to be rejected", input)
		}
	}
}
