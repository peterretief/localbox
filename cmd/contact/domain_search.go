package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

type domainSearchResponse struct {
	Query   string                `json:"query"`
	Results []DomainCheckResult   `json:"results"`
	Errors  []domainProviderError `json:"errors,omitempty"`
}

type domainProviderError struct {
	Provider string `json:"provider"`
	Error    string `json:"error"`
}

func domainSearchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	sld := normalizeSearchSLD(r.URL.Query().Get("name"))
	if !validDomainSLD(sld) {
		writeDomainCheckError(w, http.StatusBadRequest, "enter a valid domain name without an extension")
		return
	}

	results, providerErrors := searchDomainProviders(r, sld, r.URL.Query().Get("tlds"))
	if len(results) == 0 && len(providerErrors) > 0 {
		writeDomainCheckError(w, http.StatusServiceUnavailable, "could not search domain availability: "+joinProviderErrors(providerErrors))
		return
	}
	for i := range results {
		results[i] = decorateResultWithPaymentPrice(r, results[i])
	}

	if err := json.NewEncoder(w).Encode(domainSearchResponse{Query: sld, Results: results, Errors: providerErrors}); err != nil {
		log.Printf("domain search response encode failed: %v", err)
	}
}

func searchDomainProviders(r *http.Request, sld, tldValue string) ([]DomainCheckResult, []domainProviderError) {
	var results []DomainCheckResult
	var providerErrors []domainProviderError

	cloudflareTLDs, domainsCoZaTLDs := parseSearchTLDs(tldValue)
	if len(cloudflareTLDs) > 0 {
		found, err := searchCloudflareDomains(r, sld, cloudflareTLDs)
		if err != nil {
			log.Printf("Cloudflare search failed for %s: %v", sld, err)
			providerErrors = append(providerErrors, domainProviderError{Provider: "cloudflare", Error: err.Error()})
		} else {
			results = append(results, found...)
		}
	}

	if len(domainsCoZaTLDs) > 0 {
		found, err := searchDomainsCoZaDomains(r, sld, domainsCoZaTLDs)
		if err != nil {
			log.Printf("Domains.co.za search failed for %s: %v", sld, err)
			providerErrors = append(providerErrors, domainProviderError{Provider: "domains.co.za", Error: err.Error()})
		} else {
			results = append(results, found...)
		}
	}

	return results, providerErrors
}

func searchCloudflareDomains(r *http.Request, sld string, tlds []string) ([]DomainCheckResult, error) {
	cfg, err := loadCloudflareConfig(cloudflareConfigFile)
	if os.IsNotExist(err) || !cfg.Enabled {
		return nil, fmt.Errorf("Cloudflare domain search is not configured yet")
	}
	if err != nil {
		return nil, fmt.Errorf("could not read Cloudflare configuration")
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("Cloudflare domain search is not configured yet")
	}
	return newCloudflareClient(cfg).SearchDomains(r.Context(), sld, tlds)
}

func searchDomainsCoZaDomains(r *http.Request, sld string, tlds []string) ([]DomainCheckResult, error) {
	cfg, err := loadDomainsCoZaConfig(domainsCoZaConfigFile)
	if os.IsNotExist(err) || !cfg.Enabled {
		return nil, fmt.Errorf("Domains.co.za domain search is not configured yet")
	}
	if err != nil {
		return nil, fmt.Errorf("could not read Domains.co.za configuration")
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("Domains.co.za domain search is not configured yet")
	}

	client := newDomainsCoZaClient(cfg)
	var results []DomainCheckResult
	for _, tld := range tlds {
		result, err := client.CheckDomain(r.Context(), sld, tld)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func normalizeSearchSLD(value string) string {
	name := strings.ToLower(strings.TrimSpace(value))
	name = strings.TrimPrefix(name, "http://")
	name = strings.TrimPrefix(name, "https://")
	name = strings.TrimPrefix(name, "www.")
	if i := strings.IndexAny(name, "/?#"); i >= 0 {
		name = name[:i]
	}
	name = strings.Trim(name, ". ")
	for _, suffix := range []string{".co.za", ".org.za", ".net.za", ".web.za", ".com", ".net", ".org", ".info"} {
		name = strings.TrimSuffix(name, suffix)
	}
	return name
}

func parseSearchTLDs(value string) ([]string, []string) {
	defaultCloudflare := []string{"com", "net", "org", "info"}
	defaultDomainsCoZa := []string{"co.za", "org.za", "net.za", "web.za"}
	if strings.TrimSpace(value) == "" {
		return defaultCloudflare, defaultDomainsCoZa
	}
	var cloudflareTLDs []string
	var domainsCoZaTLDs []string
	for _, part := range strings.Split(value, ",") {
		part = strings.Trim(strings.ToLower(strings.TrimSpace(part)), ".")
		switch part {
		case "com", "net", "org", "info":
			cloudflareTLDs = append(cloudflareTLDs, part)
		case "co.za", "org.za", "net.za", "web.za":
			domainsCoZaTLDs = append(domainsCoZaTLDs, part)
		}
	}
	if len(cloudflareTLDs) == 0 && len(domainsCoZaTLDs) == 0 {
		return defaultCloudflare, defaultDomainsCoZa
	}
	return cloudflareTLDs, domainsCoZaTLDs
}

func joinProviderErrors(errors []domainProviderError) string {
	var parts []string
	for _, err := range errors {
		if err.Provider == "" {
			parts = append(parts, err.Error)
			continue
		}
		parts = append(parts, err.Provider+": "+err.Error)
	}
	return strings.Join(parts, "; ")
}
