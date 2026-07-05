package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

type domainSearchResponse struct {
	Query   string              `json:"query"`
	Results []DomainCheckResult `json:"results"`
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

	cfg, err := loadCloudflareConfig(cloudflareConfigFile)
	if os.IsNotExist(err) || !cfg.Enabled {
		writeDomainCheckError(w, http.StatusServiceUnavailable, "Cloudflare domain search is not configured yet")
		return
	}
	if err != nil {
		log.Printf("Cloudflare config error: %v", err)
		writeDomainCheckError(w, http.StatusInternalServerError, "could not read Cloudflare configuration")
		return
	}
	if err := cfg.validate(); err != nil {
		log.Printf("Cloudflare config invalid: %v", err)
		writeDomainCheckError(w, http.StatusServiceUnavailable, "Cloudflare domain search is not configured yet")
		return
	}

	tlds := parseSearchTLDs(r.URL.Query().Get("tlds"))
	results, err := newCloudflareClient(cfg).SearchDomains(r.Context(), sld, tlds)
	if err != nil {
		log.Printf("Cloudflare search failed for %s: %v", sld, err)
		writeDomainCheckError(w, http.StatusBadGateway, "could not search domain availability")
		return
	}

	if err := json.NewEncoder(w).Encode(domainSearchResponse{Query: sld, Results: results}); err != nil {
		log.Printf("domain search response encode failed: %v", err)
	}
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
	for _, suffix := range []string{".com", ".net", ".org", ".info"} {
		name = strings.TrimSuffix(name, suffix)
	}
	return name
}

func parseSearchTLDs(value string) []string {
	defaults := []string{"com", "net", "org", "info"}
	if strings.TrimSpace(value) == "" {
		return defaults
	}
	var tlds []string
	for _, part := range strings.Split(value, ",") {
		part = strings.Trim(strings.ToLower(strings.TrimSpace(part)), ".")
		switch part {
		case "com", "net", "org", "info":
			tlds = append(tlds, part)
		}
	}
	if len(tlds) == 0 {
		return defaults
	}
	return tlds
}
