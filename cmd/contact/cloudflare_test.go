package main

import (
	"context"
	"encoding/json"
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
tunnel_target=tunnel123
`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadCloudflareConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.APIURL != "https://api.cloudflare.test/client/v4" || cfg.AccountID != "account123" || cfg.APIToken != "token123" || cfg.TunnelTarget != "tunnel123.cfargotunnel.com" {
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

func TestCloudflareCreateZone(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/zones" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token123" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		bodyText := string(body)
		for _, want := range []string{`"name":"example.co.za"`, `"id":"account123"`, `"type":"full"`} {
			if !strings.Contains(bodyText, want) {
				t.Fatalf("expected %s in request body, got %s", want, bodyText)
			}
		}

		return jsonResponse(t, cloudflareZoneEnvelope{
			Success: true,
			Result: cloudflareZoneResult{
				ID:          "zone123",
				Name:        "example.co.za",
				Status:      "pending",
				NameServers: []string{"sam.ns.cloudflare.com", "marissa.ns.cloudflare.com"},
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

	zone, err := client.CreateZone(context.Background(), "https://www.Example.co.za/path")
	if err != nil {
		t.Fatal(err)
	}
	if zone.ID != "zone123" || zone.Name != "example.co.za" || zone.Status != "pending" || strings.Join(zone.NameServers, ",") != "sam.ns.cloudflare.com,marissa.ns.cloudflare.com" {
		t.Fatalf("unexpected zone result: %+v", zone)
	}
}

func TestCloudflareUpsertDNSRecordCreatesMissingRecord(t *testing.T) {
	calls := 0
	proxied := true
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			if r.Method != http.MethodGet || r.URL.Path != "/zones/zone123/dns_records" {
				t.Fatalf("unexpected list request: %s %s", r.Method, r.URL.String())
			}
			if r.URL.Query().Get("type") != "CNAME" || r.URL.Query().Get("name") != "example.co.za" {
				t.Fatalf("unexpected DNS list query: %s", r.URL.RawQuery)
			}
			return jsonResponse(t, cloudflareDNSRecordListEnvelope{Success: true}), nil
		case 2:
			if r.Method != http.MethodPost || r.URL.Path != "/zones/zone123/dns_records" {
				t.Fatalf("unexpected create request: %s %s", r.Method, r.URL.Path)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			bodyText := string(body)
			for _, want := range []string{`"type":"CNAME"`, `"name":"example.co.za"`, `"content":"tunnel123.cfargotunnel.com"`, `"proxied":true`} {
				if !strings.Contains(bodyText, want) {
					t.Fatalf("expected %s in request body, got %s", want, bodyText)
				}
			}
			return jsonResponse(t, cloudflareDNSRecordEnvelope{
				Success: true,
				Result: cloudflareDNSRecord{
					ID:      "dns123",
					Type:    "CNAME",
					Name:    "example.co.za",
					Content: "tunnel123.cfargotunnel.com",
					Proxied: &proxied,
				},
			}), nil
		default:
			t.Fatalf("unexpected request %d: %s %s", calls, r.Method, r.URL.Path)
			return nil, nil
		}
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:   true,
		APIURL:    "https://api.cloudflare.test",
		AccountID: "account123",
		APIToken:  "token123",
	})
	client.httpClient.Transport = transport

	record, err := client.UpsertDNSRecord(context.Background(), "zone123", CloudflareDNSRecord{
		Type:    "cname",
		Name:    "Example.co.za",
		Content: "tunnel123.cfargotunnel.com",
		Proxied: &proxied,
		Comment: "LocalBox tunnel route",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != "dns123" || record.Type != "CNAME" || record.Name != "example.co.za" || record.Content != "tunnel123.cfargotunnel.com" {
		t.Fatalf("unexpected DNS record: %+v", record)
	}
	if calls != 2 {
		t.Fatalf("expected two API calls, got %d", calls)
	}
}

func TestCloudflareUpsertDNSRecordUpdatesExistingRecord(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return jsonResponse(t, cloudflareDNSRecordListEnvelope{
				Success: true,
				Result: []cloudflareDNSRecord{{
					ID:      "dns123",
					Type:    "CNAME",
					Name:    "www.example.co.za",
					Content: "old.cfargotunnel.com",
				}},
			}), nil
		case 2:
			if r.Method != http.MethodPut || r.URL.Path != "/zones/zone123/dns_records/dns123" {
				t.Fatalf("unexpected update request: %s %s", r.Method, r.URL.Path)
			}
			return jsonResponse(t, cloudflareDNSRecordEnvelope{
				Success: true,
				Result: cloudflareDNSRecord{
					ID:      "dns123",
					Type:    "CNAME",
					Name:    "www.example.co.za",
					Content: "tunnel123.cfargotunnel.com",
				},
			}), nil
		default:
			t.Fatalf("unexpected request %d: %s %s", calls, r.Method, r.URL.Path)
			return nil, nil
		}
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:   true,
		APIURL:    "https://api.cloudflare.test",
		AccountID: "account123",
		APIToken:  "token123",
	})
	client.httpClient.Transport = transport

	record, err := client.UpsertDNSRecord(context.Background(), "zone123", CloudflareDNSRecord{
		Type:    "CNAME",
		Name:    "www.example.co.za",
		Content: "tunnel123.cfargotunnel.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Content != "tunnel123.cfargotunnel.com" {
		t.Fatalf("unexpected DNS record: %+v", record)
	}
}

func TestCloudflareUpsertTunnelRecordsUsesConfiguredTarget(t *testing.T) {
	var created []string
	var tunnelHosts []string
	proxied := true
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/zones/") {
			return jsonResponse(t, cloudflareDNSRecordListEnvelope{Success: true}), nil
		}
		if r.Method == http.MethodGet && r.URL.Path == "/accounts/account123/cfd_tunnel/tunnel123/configurations" {
			return jsonResponse(t, cloudflareTunnelConfigEnvelope{
				Success: true,
				Result: cloudflareTunnelConfigResult{
					Config: cloudflareTunnelConfig{
						Ingress: []cloudflareTunnelIngress{
							{Hostname: "localboxmail.co.za", Service: "http://127.0.0.1:80"},
							{Service: "http_status:404"},
						},
						WarpRouting: map[string]any{"enabled": false},
					},
				},
			}), nil
		}
		if r.Method == http.MethodPut && r.URL.Path == "/accounts/account123/cfd_tunnel/tunnel123/configurations" {
			var payload struct {
				Config cloudflareTunnelConfig `json:"config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			for _, ingress := range payload.Config.Ingress {
				if ingress.Hostname == "example.co.za" || ingress.Hostname == "www.example.co.za" {
					tunnelHosts = append(tunnelHosts, ingress.Hostname)
				}
			}
			return jsonResponse(t, cloudflareTunnelConfigEnvelope{Success: true}), nil
		}
		switch r.Method {
		case http.MethodPost:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			bodyText := string(body)
			if !strings.Contains(bodyText, `"type":"CNAME"`) || !strings.Contains(bodyText, `"content":"tunnel123.cfargotunnel.com"`) || !strings.Contains(bodyText, `"proxied":true`) {
				t.Fatalf("unexpected tunnel DNS payload: %s", bodyText)
			}
			if strings.Contains(bodyText, `"name":"example.co.za"`) {
				created = append(created, "example.co.za")
				return jsonResponse(t, cloudflareDNSRecordEnvelope{
					Success: true,
					Result:  cloudflareDNSRecord{ID: "root", Type: "CNAME", Name: "example.co.za", Content: "tunnel123.cfargotunnel.com", Proxied: &proxied},
				}), nil
			}
			if strings.Contains(bodyText, `"name":"www.example.co.za"`) {
				created = append(created, "www.example.co.za")
				return jsonResponse(t, cloudflareDNSRecordEnvelope{
					Success: true,
					Result:  cloudflareDNSRecord{ID: "www", Type: "CNAME", Name: "www.example.co.za", Content: "tunnel123.cfargotunnel.com", Proxied: &proxied},
				}), nil
			}
			t.Fatalf("unexpected tunnel DNS payload: %s", bodyText)
			return nil, nil
		default:
			t.Fatalf("unexpected method: %s", r.Method)
			return nil, nil
		}
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:      true,
		APIURL:       "https://api.cloudflare.test",
		AccountID:    "account123",
		APIToken:     "token123",
		TunnelTarget: "tunnel123",
	})
	client.httpClient.Transport = transport

	if err := client.UpsertTunnelRecords(context.Background(), "zone123", "example.co.za"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(created, ",") != "example.co.za,www.example.co.za" {
		t.Fatalf("unexpected created records: %v", created)
	}
	if strings.Join(tunnelHosts, ",") != "example.co.za,www.example.co.za" {
		t.Fatalf("unexpected tunnel routes: %v", tunnelHosts)
	}
}

func TestCloudflareCheckZoneActivation(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPut || r.URL.Path != "/zones/zone123/activation_check" {
			t.Fatalf("unexpected activation request: %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(t, cloudflareZoneEnvelope{Success: true}), nil
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:   true,
		APIURL:    "https://api.cloudflare.test",
		AccountID: "account123",
		APIToken:  "token123",
	})
	client.httpClient.Transport = transport

	if err := client.CheckZoneActivation(context.Background(), "zone123"); err != nil {
		t.Fatal(err)
	}
}

func TestCloudflareHTTPErrorIncludesProviderMessage(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"success": false,
				"errors": [{"code": 10000, "message": "Authentication error"}]
			}`)),
		}, nil
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:   true,
		APIURL:    "https://api.cloudflare.test",
		AccountID: "account123",
		APIToken:  "token123",
	})
	client.httpClient.Transport = transport

	_, err := client.CheckDomain(context.Background(), "example.com")
	if err == nil || !strings.Contains(err.Error(), "Cloudflare returned HTTP 403: Authentication error") {
		t.Fatalf("expected provider error message, got %v", err)
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
	if got := normalizeSearchSLD("Example.co.za"); got != "example" {
		t.Fatalf("expected example, got %q", got)
	}
}

func TestParseSearchTLDsSplitsProviders(t *testing.T) {
	cloudflare, domainsCoZa := parseSearchTLDs("com,co.za,org.za,info,bad")
	if strings.Join(cloudflare, ",") != "com,info" {
		t.Fatalf("unexpected Cloudflare TLDs: %v", cloudflare)
	}
	if strings.Join(domainsCoZa, ",") != "co.za,org.za" {
		t.Fatalf("unexpected Domains.co.za TLDs: %v", domainsCoZa)
	}
}

func TestParseSearchTLDsDefaultsToBothProviders(t *testing.T) {
	cloudflare, domainsCoZa := parseSearchTLDs("")
	if strings.Join(cloudflare, ",") != "com,net,org,info" {
		t.Fatalf("unexpected Cloudflare defaults: %v", cloudflare)
	}
	if strings.Join(domainsCoZa, ",") != "co.za,org.za,net.za,web.za" {
		t.Fatalf("unexpected Domains.co.za defaults: %v", domainsCoZa)
	}
}

func TestNormalizePublicDomainRejectsBadInput(t *testing.T) {
	for _, input := range []string{"", "localhost", "-bad.com", "bad_.com"} {
		if _, err := normalizePublicDomain(input); err == nil {
			t.Fatalf("expected %q to be rejected", input)
		}
	}
}
