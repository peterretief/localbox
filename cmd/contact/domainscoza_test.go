package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitZADomain(t *testing.T) {
	tests := []struct {
		input string
		sld   string
		tld   string
	}{
		{"example.co.za", "example", "co.za"},
		{"https://www.example.org.za/path", "example", "org.za"},
		{"my-domain.net.za", "my-domain", "net.za"},
	}

	for _, tt := range tests {
		sld, tld, err := splitZADomain(tt.input)
		if err != nil {
			t.Fatalf("splitZADomain(%q) returned error: %v", tt.input, err)
		}
		if sld != tt.sld || tld != tt.tld {
			t.Fatalf("splitZADomain(%q) = %q, %q; want %q, %q", tt.input, sld, tld, tt.sld, tt.tld)
		}
	}
}

func TestSplitZADomainRejectsUnsupportedTLD(t *testing.T) {
	if _, _, err := splitZADomain("example.com"); err == nil {
		t.Fatal("expected unsupported TLD error")
	}
}

func TestLoadDomainsCoZaConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "domainscoza.conf")
	if err := os.WriteFile(path, []byte(`
enabled=true
api_url=https://lapi-dev.domains.co.za/api
username=reseller
api_key=secret
co_za_register_price=150.00
co_za_renew_price=175
`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadDomainsCoZaConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.Enabled || cfg.APIURL != "https://lapi-dev.domains.co.za/api" || cfg.Username != "reseller" || cfg.Password != "secret" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if got := cfg.RegisterPrice["co_za"]; got != 15000 {
		t.Fatalf("expected register price 15000 cents, got %d", got)
	}
	if got := cfg.RenewPrice["co_za"]; got != 17500 {
		t.Fatalf("expected renew price 17500 cents, got %d", got)
	}
}

func TestValidNameserver(t *testing.T) {
	allowed := []string{
		"abby.ns.cloudflare.com",
		"ns1.example.co.za.",
	}
	for _, ns := range allowed {
		if !validNameserver(ns) {
			t.Fatalf("expected nameserver %q to be valid", ns)
		}
	}

	rejected := []string{
		"",
		"localhost",
		"-bad.example.com",
		"bad_.example.com",
	}
	for _, ns := range rejected {
		if validNameserver(ns) {
			t.Fatalf("expected nameserver %q to be rejected", ns)
		}
	}
}

func TestDomainsCoZaUpdateNameservers(t *testing.T) {
	var sawNS1, sawNS2 string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/login":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			values := mustParseForm(t, string(body))
			if values.Get("username") != "reseller" || values.Get("password") != "secret" {
				t.Fatalf("unexpected login form: %v", values)
			}
			return jsonResponse(t, domainsCoZaLoginResponse{IntReturnCode: 1, Token: "token"}), nil
		case "/domain/ns":
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			values := mustParseForm(t, string(body))
			if values.Get("sld") != "example" || values.Get("tld") != "co.za" || values.Get("dns") != "custom" {
				t.Fatalf("unexpected nameserver form: %v", values)
			}
			sawNS1 = values.Get("ns1")
			sawNS2 = values.Get("ns2")
			return jsonResponse(t, domainsCoZaActionResponse{IntReturnCode: 2, StrMessage: "Successful Action Pending"}), nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}
	})

	client := newDomainsCoZaClient(DomainsCoZaConfig{
		Enabled:  true,
		APIURL:   "https://api.test",
		Username: "reseller",
		Password: "secret",
	})
	client.httpClient.Transport = transport

	err := client.UpdateNameservers(context.Background(), "example", "co.za", []string{"ABBY.ns.cloudflare.com", "jake.ns.cloudflare.com"})
	if err != nil {
		t.Fatal(err)
	}
	if sawNS1 != "abby.ns.cloudflare.com" || sawNS2 != "jake.ns.cloudflare.com" {
		t.Fatalf("unexpected nameservers %q %q", sawNS1, sawNS2)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func jsonResponse(t *testing.T, value any) *http.Response {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(data))),
	}
}

func mustParseForm(t *testing.T, body string) url.Values {
	t.Helper()
	values, err := url.ParseQuery(body)
	if err != nil {
		t.Fatal(err)
	}
	return values
}
