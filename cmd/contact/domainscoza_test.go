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
ns1=sam.ns.cloudflare.com
ns2=marissa.ns.cloudflare.com
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
	if strings.Join(cfg.Nameservers, ",") != "sam.ns.cloudflare.com,marissa.ns.cloudflare.com" {
		t.Fatalf("unexpected nameservers: %+v", cfg.Nameservers)
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

func TestDomainsCoZaRegisterDomainUsesRegistrantAndCloudflareNameservers(t *testing.T) {
	var saw url.Values
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/login":
			return jsonResponse(t, domainsCoZaLoginResponse{IntReturnCode: 1, Token: "token"}), nil
		case "/domain":
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST /domain, got %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			saw = mustParseForm(t, string(body))
			return jsonResponse(t, domainsCoZaActionResponse{
				IntReturnCode: 1,
				UUID:          "7ec167a0-0034-11ec-80f6-13a1aba3d2d2",
				StrMessage:    "Successful",
				StrEPPMessage: "Domain Create completed successfully",
				Registrant:    "ZLAKv3J3JAvLukkP",
				DomainName:    "mydomainname.co.za",
				ExpiryDate:    1660834602,
				CreatedDate:   1629298602,
			}), nil
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

	result, err := client.RegisterDomain(context.Background(), DomainsCoZaRegistration{
		Domain: "mydomainname.co.za",
		Registrant: DomainsCoZaRegistrant{
			Name:          "John Doe",
			Email:         "john.doe@mydomainname.co.za",
			Country:       "ZA",
			Province:      "Gauteng",
			ContactNumber: "+27.0123456789",
			PostalCode:    "2198",
			Address1:      "219 Osborn Rd",
			Address2:      "Houghton Estate Office Park",
			City:          "Johannesburg",
		},
		Nameservers: []string{"ABBY.ns.cloudflare.com", "jake.ns.cloudflare.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.UUID == "" || result.DomainName != "mydomainname.co.za" || result.Registrant == "" {
		t.Fatalf("unexpected registration result: %+v", result)
	}
	for key, want := range map[string]string{
		"sld":                     "mydomainname",
		"tld":                     "co.za",
		"registrantName":          "John Doe",
		"registrantEmail":         "john.doe@mydomainname.co.za",
		"registrantCountry":       "ZA",
		"registrantProvince":      "Gauteng",
		"registrantContactNumber": "+27.0123456789",
		"registrantPostalCode":    "2198",
		"registrantAddress1":      "219 Osborn Rd",
		"registrantAddress2":      "Houghton Estate Office Park",
		"registrantCity":          "Johannesburg",
		"period":                  "1",
		"ns1":                     "abby.ns.cloudflare.com",
		"ns2":                     "jake.ns.cloudflare.com",
	} {
		if got := saw.Get(key); got != want {
			t.Fatalf("expected %s=%q, got %q in %v", key, want, got, saw)
		}
	}
}

func TestDomainsCoZaRegisterDomainNormalizesSouthAfricanPhone(t *testing.T) {
	var saw url.Values
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/login":
			return jsonResponse(t, domainsCoZaLoginResponse{IntReturnCode: 1, Token: "token"}), nil
		case "/domain":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			saw = mustParseForm(t, string(body))
			return jsonResponse(t, domainsCoZaActionResponse{IntReturnCode: 1, UUID: "uuid", DomainName: "example.co.za"}), nil
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

	_, err := client.RegisterDomain(context.Background(), DomainsCoZaRegistration{
		Domain: "example.co.za",
		Registrant: DomainsCoZaRegistrant{
			Name:          "Buyer Example",
			Email:         "buyer@example.net",
			Country:       "ZA",
			Province:      "Gauteng",
			ContactNumber: "+27 82 123 4567",
			PostalCode:    "2198",
			Address1:      "1 Example Street",
			City:          "Johannesburg",
		},
		Nameservers: []string{"sam.ns.cloudflare.com", "marissa.ns.cloudflare.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := saw.Get("registrantContactNumber"); got != "+27.821234567" {
		t.Fatalf("unexpected normalized phone: %q", got)
	}
}

func TestDomainsCoZaRegisterDomainUsesConfiguredNameservers(t *testing.T) {
	var saw url.Values
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/login":
			return jsonResponse(t, domainsCoZaLoginResponse{IntReturnCode: 1, Token: "token"}), nil
		case "/domain":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			saw = mustParseForm(t, string(body))
			return jsonResponse(t, domainsCoZaActionResponse{
				IntReturnCode: 1,
				UUID:          "uuid",
				DomainName:    "mydomainname.co.za",
				Registrant:    "registrant",
			}), nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}
	})

	client := newDomainsCoZaClient(DomainsCoZaConfig{
		Enabled:     true,
		APIURL:      "https://api.test",
		Username:    "reseller",
		Password:    "secret",
		Nameservers: []string{"sam.ns.cloudflare.com", "marissa.ns.cloudflare.com"},
	})
	client.httpClient.Transport = transport

	_, err := client.RegisterDomain(context.Background(), DomainsCoZaRegistration{
		Domain: "mydomainname.co.za",
		Registrant: DomainsCoZaRegistrant{
			Name:          "John Doe",
			Email:         "john.doe@example.co.za",
			Country:       "ZA",
			Province:      "Gauteng",
			ContactNumber: "+27.0123456789",
			PostalCode:    "2198",
			Address1:      "219 Osborn Rd",
			City:          "Johannesburg",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saw.Get("ns1") != "sam.ns.cloudflare.com" || saw.Get("ns2") != "marissa.ns.cloudflare.com" {
		t.Fatalf("expected configured nameservers, got %v", saw)
	}
}

func TestDomainsCoZaHTTPErrorIncludesProviderMessage(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"intReturnCode": 0,
				"strMessage": "Login Failed",
				"strReason": "Invalid credentials"
			}`)),
		}, nil
	})

	client := newDomainsCoZaClient(DomainsCoZaConfig{
		Enabled:  true,
		APIURL:   "https://api.test",
		Username: "reseller",
		Password: "secret",
	})
	client.httpClient.Transport = transport

	_, err := client.CheckDomain(context.Background(), "example", "co.za")
	if err == nil || !strings.Contains(err.Error(), "Domains.co.za returned HTTP 401: Invalid credentials") {
		t.Fatalf("expected provider error message, got %v", err)
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
