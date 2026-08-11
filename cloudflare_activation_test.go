package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestCheckZoneActivationRetriesTransientFailures(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodPut || r.URL.Path != "/zones/zone123/activation_check" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token123" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if calls < 3 {
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Status:     "403 Forbidden",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       http.NoBody,
				Request:    r,
			}, nil
		}
		return cloudflareTestJSONResponse(t, cloudflareZoneEnvelope{Success: true}), nil
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:  true,
		APIURL:   "https://api.cloudflare.test",
		APIToken: "token123",
	})
	client.httpClient.Transport = transport

	if err := client.CheckZoneActivation(context.Background(), "zone123"); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("expected three attempts before success, got %d", calls)
	}
}

func TestCheckZoneActivationReturnsPendingError(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       http.NoBody,
			Request:    r,
		}, nil
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:  true,
		APIURL:   "https://api.cloudflare.test",
		APIToken: "token123",
	})
	client.httpClient.Transport = transport

	if err := client.CheckZoneActivation(context.Background(), "zone123"); err == nil || !strings.Contains(err.Error(), "activation not confirmed") {
		t.Fatalf("expected persistent activation failure, got %v", err)
	}
	if calls != 5 {
		t.Fatalf("expected five attempts before giving up, got %d", calls)
	}
}

func TestCheckZoneActivationStopsOnContextCancellation(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       http.NoBody,
			Request:    r,
		}, nil
	})

	client := newCloudflareClient(CloudflareConfig{
		Enabled:  true,
		APIURL:   "https://api.cloudflare.test",
		APIToken: "token123",
	})
	client.httpClient.Transport = transport

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := client.CheckZoneActivation(ctx, "zone123")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected only one attempt before cancellation, got %d", calls)
	}
}

func TestCheckZoneActivationRequiresZoneID(t *testing.T) {
	client := newCloudflareClient(CloudflareConfig{Enabled: true})
	err := client.CheckZoneActivation(context.Background(), "  ")
	if err == nil || !strings.Contains(err.Error(), "zone ID is required") {
		t.Fatalf("expected zone ID error, got %v", err)
	}
}
