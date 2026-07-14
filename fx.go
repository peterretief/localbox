package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const fxCacheTTL = 6 * time.Hour

type fxRateFetcher func(ctx context.Context, source, target string) (float64, error)

var (
	priceAmountPattern                 = regexp.MustCompile(`(\d+(?:\.\d{1,2})?)`)
	defaultFXRateFetcher fxRateFetcher = fetchFrankfurterRate
	fxRateFetcherMu      sync.RWMutex
	fxRatesCache         = map[string]cachedFXRate{}
	fxRatesMu            sync.Mutex
)

type cachedFXRate struct {
	Rate      float64
	ExpiresAt time.Time
}

func estimateCurrencyAmount(ctx context.Context, amount float64, sourceCurrency, targetCurrency string) (float64, bool, error) {
	sourceCurrency = cleanCurrency(sourceCurrency)
	targetCurrency = cleanCurrency(targetCurrency)
	if sourceCurrency == "" {
		sourceCurrency = "ZAR"
	}
	if targetCurrency == "" {
		targetCurrency = "ZAR"
	}
	if sourceCurrency == targetCurrency {
		return amount, false, nil
	}
	rate, err := lookupFXRate(ctx, sourceCurrency, targetCurrency)
	if err != nil {
		return amount, false, err
	}
	return amount * rate, true, nil
}

func lookupFXRate(ctx context.Context, sourceCurrency, targetCurrency string) (float64, error) {
	key := sourceCurrency + "->" + targetCurrency
	now := time.Now()

	fxRatesMu.Lock()
	if cached, ok := fxRatesCache[key]; ok && now.Before(cached.ExpiresAt) {
		fxRatesMu.Unlock()
		return cached.Rate, nil
	}
	fxRatesMu.Unlock()

	fxRateFetcherMu.RLock()
	fetcher := defaultFXRateFetcher
	fxRateFetcherMu.RUnlock()

	rate, err := fetcher(ctx, sourceCurrency, targetCurrency)
	if err != nil {
		return 0, err
	}

	fxRatesMu.Lock()
	fxRatesCache[key] = cachedFXRate{
		Rate:      rate,
		ExpiresAt: now.Add(fxCacheTTL),
	}
	fxRatesMu.Unlock()

	return rate, nil
}

func fetchFrankfurterRate(ctx context.Context, sourceCurrency, targetCurrency string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://api.frankfurter.dev/v2/rate/%s/%s", sourceCurrency, targetCurrency), nil)
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
	return payload.Rate, nil
}

func parsePriceAmount(value string) (float64, error) {
	return strconv.ParseFloat(value, 64)
}

func parseCatalogPrice(price string) (amount float64, sourceCurrency string, priced bool) {
	price = strings.TrimSpace(price)
	if price == "" {
		return 0, "", false
	}
	lower := strings.ToLower(price)
	if strings.Contains(lower, "confirm") || strings.Contains(lower, "quote") || strings.Contains(lower, "optional") || strings.Contains(lower, "from") {
		return 0, "", false
	}
	sourceCurrency = ""
	switch {
	case strings.Contains(price, "R") || strings.Contains(strings.ToUpper(price), "ZAR"):
		sourceCurrency = "ZAR"
	case strings.Contains(price, "$") || strings.Contains(strings.ToUpper(price), "USD"):
		sourceCurrency = "USD"
	case strings.Contains(price, "€") || strings.Contains(strings.ToUpper(price), "EUR"):
		sourceCurrency = "EUR"
	case strings.Contains(price, "£") || strings.Contains(strings.ToUpper(price), "GBP"):
		sourceCurrency = "GBP"
	default:
		sourceCurrency = "ZAR"
	}
	re := priceAmountPattern.FindStringSubmatch(strings.ReplaceAll(price, ",", ""))
	if len(re) < 2 {
		return 0, "", false
	}
	var err error
	amount, err = parsePriceAmount(re[1])
	if err != nil {
		return 0, "", false
	}
	return amount, sourceCurrency, true
}
