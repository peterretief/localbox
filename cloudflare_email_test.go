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

func TestLoadCloudflareConfigIncludesEmailRouting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloudflare.conf")
	if err := os.WriteFile(path, []byte(`
enabled=true
api_url=https://api.cloudflare.test/client/v4
account_id=account123
api_key=token123
email_routing_api_token=emailtoken123
tunnel_target=tunnel123
email_worker=localbox-001-mail-worker
`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadCloudflareConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled ||
		cfg.APIURL != "https://api.cloudflare.test/client/v4" ||
		cfg.AccountID != "account123" ||
		cfg.APIToken != "token123" ||
		cfg.EmailRoutingAPIToken != "emailtoken123" ||
		cfg.TunnelTarget != "tunnel123.cfargotunnel.com" ||
		cfg.EmailWorker != "localbox-001-mail-worker" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestCloudflareUpsertEmailRouteCreatesMissingWorkerRoute(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			if r.Method != http.MethodGet || r.URL.Path != "/zones" {
				t.Fatalf("unexpected zone lookup request: %s %s", r.Method, r.URL.String())
			}
			if r.Header.Get("Authorization") != "Bearer token123" {
				t.Fatalf("unexpected zone auth header: %s", r.Header.Get("Authorization"))
			}
			if r.URL.Query().Get("name") != "example.web.za" || r.URL.Query().Get("account.id") != "account123" {
				t.Fatalf("unexpected zone lookup query: %s", r.URL.RawQuery)
			}
			return cloudflareTestJSONResponse(t, cloudflareZoneListEnvelope{
				Success: true,
				Result:  []cloudflareZoneResult{{ID: "zone123", Name: "example.web.za"}},
			}), nil
		case 2:
			if r.Method != http.MethodGet || r.URL.Path != "/zones/zone123/email/routing/rules" {
				t.Fatalf("unexpected email rules list request: %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer emailtoken123" {
				t.Fatalf("unexpected email rules auth header: %s", r.Header.Get("Authorization"))
			}
			return cloudflareTestJSONResponse(t, cloudflareEmailRoutingRuleListEnvelope{Success: true}), nil
		case 3:
			if r.Method != http.MethodPost || r.URL.Path != "/zones/zone123/email/routing/dns" {
				t.Fatalf("unexpected email DNS request: %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer emailtoken123" {
				t.Fatalf("unexpected email DNS auth header: %s", r.Header.Get("Authorization"))
			}
			return cloudflareTestJSONResponse(t, struct {
				Success bool                   `json:"success"`
				Errors  []cloudflareAPIMessage `json:"errors"`
			}{Success: true}), nil
		case 4:
			if r.Method != http.MethodPost || r.URL.Path != "/zones/zone123/email/routing/rules" {
				t.Fatalf("unexpected email rule create request: %s %s", r.Method, r.URL.Path)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			bodyText := string(body)
			for _, want := range []string{
				`"name":"LocalBox admin@example.web.za"`,
				`"enabled":true`,
				`"field":"to"`,
				`"value":"admin@example.web.za"`,
				`"type":"worker"`,
				`"localbox-001-mail-worker"`,
			} {
				if !strings.Contains(bodyText, want) {
					t.Fatalf("expected %s in email route payload, got %s", want, bodyText)
				}
			}
			return cloudflareTestJSONResponse(t, cloudflareEmailRoutingRuleEnvelope{
				Success: true,
				Result:  cloudflareEmailRoutingRule{ID: "rule123"},
			}), nil
		default:
			t.Fatalf("unexpected request %d: %s %s", calls, r.Method, r.URL.Path)
			return nil, nil
		}
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:              true,
		APIURL:               "https://api.cloudflare.test",
		AccountID:            "account123",
		APIToken:             "token123",
		EmailRoutingAPIToken: "emailtoken123",
		EmailWorker:          "localbox-001-mail-worker",
	})
	client.httpClient.Transport = transport

	if err := client.UpsertEmailRoute(context.Background(), "Admin@example.web.za"); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("expected four API calls, got %d", calls)
	}
}

func cloudflareTestJSONResponse(t *testing.T, value any) *http.Response {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
}

func TestCloudflareUpsertEmailRouteLeavesMatchingRuleAlone(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return cloudflareTestJSONResponse(t, cloudflareZoneListEnvelope{
				Success: true,
				Result:  []cloudflareZoneResult{{ID: "zone123", Name: "example.web.za"}},
			}), nil
		case 2:
			if r.Method != http.MethodGet || r.URL.Path != "/zones/zone123/email/routing/rules" {
				t.Fatalf("unexpected email rules list request: %s %s", r.Method, r.URL.Path)
			}
			return cloudflareTestJSONResponse(t, cloudflareEmailRoutingRuleListEnvelope{
				Success: true,
				Result: []cloudflareEmailRoutingRule{{
					ID:       "rule123",
					Name:     "LocalBox admin@example.web.za",
					Enabled:  true,
					Matchers: []cloudflareEmailRoutingMatcher{{Type: "literal", Field: "to", Value: "admin@example.web.za"}},
					Actions:  []cloudflareEmailRoutingAction{{Type: "worker", Value: []string{"localbox-001-mail-worker"}}},
				}},
			}), nil
		default:
			t.Fatalf("unexpected request %d: %s %s", calls, r.Method, r.URL.Path)
			return nil, nil
		}
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:              true,
		APIURL:               "https://api.cloudflare.test",
		AccountID:            "account123",
		APIToken:             "token123",
		EmailRoutingAPIToken: "emailtoken123",
		EmailWorker:          "localbox-001-mail-worker",
	})
	client.httpClient.Transport = transport

	if err := client.UpsertEmailRoute(context.Background(), "admin@example.web.za"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected only the zone lookup and rules list, got %d calls", calls)
	}
}
