package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

type domainCheckErrorResponse struct {
	Error string `json:"error"`
}

func domainCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	domain, err := normalizePublicDomain(r.URL.Query().Get("domain"))
	if err != nil {
		writeDomainCheckError(w, http.StatusBadRequest, err.Error())
		return
	}

	var result DomainCheckResult
	if isZADomain(domain) {
		result, err = checkDomainsCoZa(r, domain)
	} else {
		result, err = checkCloudflareDomain(r, domain)
	}
	if err != nil {
		writeDomainCheckError(w, http.StatusBadGateway, "could not check domain availability")
		return
	}

	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("domain check response encode failed: %v", err)
	}
}

func checkDomainsCoZa(r *http.Request, domain string) (DomainCheckResult, error) {
	sld, tld, err := splitZADomain(domain)
	if err != nil {
		return DomainCheckResult{}, err
	}

	cfg, err := loadDomainsCoZaConfig(domainsCoZaConfigFile)
	if os.IsNotExist(err) || !cfg.Enabled {
		return DomainCheckResult{}, err
	}
	if err != nil {
		log.Printf("Domains.co.za config error: %v", err)
		return DomainCheckResult{}, err
	}
	if err := cfg.validate(); err != nil {
		log.Printf("Domains.co.za config invalid: %v", err)
		return DomainCheckResult{}, err
	}

	result, err := newDomainsCoZaClient(cfg).CheckDomain(r.Context(), sld, tld)
	if err != nil {
		log.Printf("Domains.co.za check failed for %s.%s: %v", sld, tld, err)
		return DomainCheckResult{}, err
	}
	return result, nil
}

func checkCloudflareDomain(r *http.Request, domain string) (DomainCheckResult, error) {
	cfg, err := loadCloudflareConfig(cloudflareConfigFile)
	if os.IsNotExist(err) || !cfg.Enabled {
		return DomainCheckResult{}, err
	}
	if err != nil {
		log.Printf("Cloudflare config error: %v", err)
		return DomainCheckResult{}, err
	}
	if err := cfg.validate(); err != nil {
		log.Printf("Cloudflare config invalid: %v", err)
		return DomainCheckResult{}, err
	}

	result, err := newCloudflareClient(cfg).CheckDomain(r.Context(), domain)
	if err != nil {
		log.Printf("Cloudflare check failed for %s: %v", domain, err)
		return DomainCheckResult{}, err
	}
	return result, nil
}

func writeDomainCheckError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(domainCheckErrorResponse{Error: message})
}

func normalizePublicDomain(value string) (string, error) {
	domain := strings.ToLower(strings.TrimSpace(value))
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "www.")
	if i := strings.IndexAny(domain, "/?#"); i >= 0 {
		domain = domain[:i]
	}
	domain = strings.Trim(domain, ". ")
	if domain == "" {
		return "", errDomainRequired()
	}
	if len(domain) > 253 || !strings.Contains(domain, ".") {
		return "", errInvalidDomain()
	}
	for _, label := range strings.Split(domain, ".") {
		if !validDomainLabel(label) {
			return "", errInvalidDomain()
		}
	}
	return domain, nil
}

func isZADomain(domain string) bool {
	for _, suffix := range []string{".co.za", ".org.za", ".net.za", ".web.za"} {
		if strings.HasSuffix(domain, suffix) {
			return true
		}
	}
	return false
}

func validDomainLabel(value string) bool {
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

func errDomainRequired() error {
	return domainCheckInputError("domain is required")
}

func errInvalidDomain() error {
	return domainCheckInputError("invalid domain name")
}

type domainCheckInputError string

func (err domainCheckInputError) Error() string {
	return string(err)
}
