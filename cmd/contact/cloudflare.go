package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultCloudflareAPIURL = "https://api.cloudflare.com/client/v4"

type CloudflareConfig struct {
	Enabled      bool
	APIURL       string
	AccountID    string
	APIToken     string
	TunnelTarget string
}

type cloudflareClient struct {
	cfg        CloudflareConfig
	httpClient *http.Client
}

type cloudflareEnvelope struct {
	Success bool                   `json:"success"`
	Result  cloudflareCheckResult  `json:"result"`
	Errors  []cloudflareAPIMessage `json:"errors"`
}

type cloudflareZoneEnvelope struct {
	Success bool                   `json:"success"`
	Result  cloudflareZoneResult   `json:"result"`
	Errors  []cloudflareAPIMessage `json:"errors"`
}

type cloudflareDNSRecordEnvelope struct {
	Success bool                   `json:"success"`
	Result  cloudflareDNSRecord    `json:"result"`
	Errors  []cloudflareAPIMessage `json:"errors"`
}

type cloudflareDNSRecordListEnvelope struct {
	Success bool                   `json:"success"`
	Result  []cloudflareDNSRecord  `json:"result"`
	Errors  []cloudflareAPIMessage `json:"errors"`
}

type cloudflareTunnelConfigEnvelope struct {
	Success bool                         `json:"success"`
	Result  cloudflareTunnelConfigResult `json:"result"`
	Errors  []cloudflareAPIMessage       `json:"errors"`
}

type cloudflareTunnelConfigResult struct {
	TunnelID string                 `json:"tunnel_id"`
	Version  int                    `json:"version"`
	Config   cloudflareTunnelConfig `json:"config"`
}

type cloudflareTunnelConfig struct {
	Ingress     []cloudflareTunnelIngress `json:"ingress"`
	WarpRouting map[string]any            `json:"warp-routing,omitempty"`
}

type cloudflareTunnelIngress struct {
	Service       string         `json:"service"`
	Hostname      string         `json:"hostname,omitempty"`
	OriginRequest map[string]any `json:"originRequest,omitempty"`
}

type cloudflareCheckResult struct {
	Domains []cloudflareDomainResult `json:"domains"`
}

type cloudflareZoneResult struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	NameServers []string `json:"name_servers"`
}

type CloudflareZoneResult struct {
	ID          string
	Name        string
	Status      string
	NameServers []string
}

type cloudflareDNSRecord struct {
	ID       string   `json:"id,omitempty"`
	Type     string   `json:"type"`
	Name     string   `json:"name"`
	Content  string   `json:"content"`
	TTL      int      `json:"ttl,omitempty"`
	Proxied  *bool    `json:"proxied,omitempty"`
	Priority int      `json:"priority,omitempty"`
	Comment  string   `json:"comment,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type CloudflareDNSRecord struct {
	ID       string
	Type     string
	Name     string
	Content  string
	TTL      int
	Proxied  *bool
	Priority int
	Comment  string
	Tags     []string
}

type cloudflareDomainResult struct {
	Name        string            `json:"name"`
	Registrable bool              `json:"registrable"`
	Pricing     cloudflarePricing `json:"pricing"`
	Reason      string            `json:"reason"`
	Tier        string            `json:"tier"`
}

type cloudflarePricing struct {
	Currency         string `json:"currency"`
	RegistrationCost string `json:"registration_cost"`
	RenewalCost      string `json:"renewal_cost"`
}

type cloudflareAPIMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func loadCloudflareConfig(path string) (CloudflareConfig, error) {
	cfg := CloudflareConfig{APIURL: defaultCloudflareAPIURL}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"'`)

		switch key {
		case "enabled":
			cfg.Enabled = parseConfigBool(value)
		case "api_url", "url":
			cfg.APIURL = strings.TrimRight(value, "/")
		case "account_id", "account":
			cfg.AccountID = value
		case "api_token", "token", "api_key", "apikey", "key":
			cfg.APIToken = value
		case "tunnel_target", "cfargotunnel", "tunnel_cname":
			cfg.TunnelTarget = normalizeCloudflareTunnelTarget(value)
		}
	}

	return cfg, nil
}

func (cfg CloudflareConfig) validate() error {
	if !cfg.Enabled {
		return fmt.Errorf("Cloudflare integration is disabled")
	}
	if cfg.APIURL == "" {
		return fmt.Errorf("Cloudflare API URL is required")
	}
	if cfg.AccountID == "" {
		return fmt.Errorf("Cloudflare account ID is required")
	}
	if cfg.APIToken == "" {
		return fmt.Errorf("Cloudflare API token is required")
	}
	return nil
}

func newCloudflareClient(cfg CloudflareConfig) *cloudflareClient {
	return &cloudflareClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 12 * time.Second,
		},
	}
}

func (c *cloudflareClient) CheckDomain(ctx context.Context, domain string) (DomainCheckResult, error) {
	results, err := c.checkDomains(ctx, []string{domain})
	if err != nil {
		return DomainCheckResult{}, err
	}
	if len(results) == 0 {
		return DomainCheckResult{}, fmt.Errorf("Cloudflare did not return a domain result")
	}
	return results[0], nil
}

func (c *cloudflareClient) SearchDomains(ctx context.Context, sld string, tlds []string) ([]DomainCheckResult, error) {
	if !validDomainSLD(sld) {
		return nil, fmt.Errorf("invalid domain name")
	}
	var domains []string
	for _, tld := range tlds {
		tld = strings.Trim(strings.ToLower(strings.TrimSpace(tld)), ".")
		if tld == "" || strings.Contains(tld, ".") || !validDomainLabel(tld) {
			continue
		}
		domains = append(domains, sld+"."+tld)
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("no valid TLDs to search")
	}
	return c.checkDomains(ctx, domains)
}

func (c *cloudflareClient) CreateZone(ctx context.Context, domain string) (CloudflareZoneResult, error) {
	domain, err := normalizePublicDomain(domain)
	if err != nil {
		return CloudflareZoneResult{}, err
	}
	payload := map[string]any{
		"name": domain,
		"account": map[string]string{
			"id": c.cfg.AccountID,
		},
		"type": "full",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return CloudflareZoneResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIURL+"/zones", bytes.NewReader(body))
	if err != nil {
		return CloudflareZoneResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	var response cloudflareZoneEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return CloudflareZoneResult{}, err
	}
	if !response.Success {
		return CloudflareZoneResult{}, fmt.Errorf("Cloudflare zone create failed: %s", cloudflareErrorMessage(response.Errors))
	}
	if response.Result.ID == "" {
		return CloudflareZoneResult{}, fmt.Errorf("Cloudflare did not return a zone ID")
	}
	return CloudflareZoneResult{
		ID:          response.Result.ID,
		Name:        response.Result.Name,
		Status:      response.Result.Status,
		NameServers: response.Result.NameServers,
	}, nil
}

func (c *cloudflareClient) ListDNSRecords(ctx context.Context, zoneID, recordType, name string) ([]CloudflareDNSRecord, error) {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return nil, fmt.Errorf("Cloudflare zone ID is required")
	}
	values := url.Values{}
	values.Set("type", strings.ToUpper(strings.TrimSpace(recordType)))
	values.Set("name", strings.ToLower(strings.TrimSpace(name)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.APIURL+"/zones/"+zoneID+"/dns_records?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	req.Header.Set("Accept", "application/json")

	var response cloudflareDNSRecordListEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return nil, err
	}
	if !response.Success {
		return nil, fmt.Errorf("Cloudflare DNS record list failed: %s", cloudflareErrorMessage(response.Errors))
	}
	records := make([]CloudflareDNSRecord, 0, len(response.Result))
	for _, record := range response.Result {
		records = append(records, cloudflareDNSRecordFromAPI(record))
	}
	return records, nil
}

func (c *cloudflareClient) CreateDNSRecord(ctx context.Context, zoneID string, record CloudflareDNSRecord) (CloudflareDNSRecord, error) {
	return c.writeDNSRecord(ctx, http.MethodPost, zoneID, "", record)
}

func (c *cloudflareClient) UpdateDNSRecord(ctx context.Context, zoneID, recordID string, record CloudflareDNSRecord) (CloudflareDNSRecord, error) {
	recordID = strings.TrimSpace(recordID)
	if recordID == "" {
		return CloudflareDNSRecord{}, fmt.Errorf("Cloudflare DNS record ID is required")
	}
	return c.writeDNSRecord(ctx, http.MethodPut, zoneID, recordID, record)
}

func (c *cloudflareClient) UpsertDNSRecord(ctx context.Context, zoneID string, record CloudflareDNSRecord) (CloudflareDNSRecord, error) {
	if err := validateCloudflareDNSRecord(record); err != nil {
		return CloudflareDNSRecord{}, err
	}
	existing, err := c.ListDNSRecords(ctx, zoneID, record.Type, record.Name)
	if err != nil {
		return CloudflareDNSRecord{}, err
	}
	if len(existing) > 0 {
		return c.UpdateDNSRecord(ctx, zoneID, existing[0].ID, record)
	}
	return c.CreateDNSRecord(ctx, zoneID, record)
}

func (c *cloudflareClient) UpsertTunnelRecords(ctx context.Context, zoneID, domain string) error {
	domain, err := normalizePublicDomain(domain)
	if err != nil {
		return err
	}
	target := normalizeCloudflareTunnelTarget(c.cfg.TunnelTarget)
	if target == "" {
		return fmt.Errorf("Cloudflare tunnel target is required")
	}
	proxied := true
	for _, host := range []string{domain, "www." + domain} {
		if _, err := c.UpsertDNSRecord(ctx, zoneID, CloudflareDNSRecord{
			Type:    "CNAME",
			Name:    host,
			Content: target,
			TTL:     1,
			Proxied: &proxied,
			Comment: "LocalBox tunnel route",
		}); err != nil {
			return err
		}
	}
	return c.UpsertTunnelIngressRoutes(ctx, domain)
}

func (c *cloudflareClient) UpsertTunnelIngressRoutes(ctx context.Context, domain string) error {
	domain, err := normalizePublicDomain(domain)
	if err != nil {
		return err
	}
	tunnelID := cloudflareTunnelID(c.cfg.TunnelTarget)
	if tunnelID == "" {
		return fmt.Errorf("Cloudflare tunnel target is required")
	}
	endpoint := c.cfg.APIURL + "/accounts/" + c.cfg.AccountID + "/cfd_tunnel/" + tunnelID + "/configurations"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	req.Header.Set("Accept", "application/json")

	var response cloudflareTunnelConfigEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return err
	}
	if !response.Success {
		return fmt.Errorf("Cloudflare tunnel config read failed: %s", cloudflareErrorMessage(response.Errors))
	}
	config := response.Result.Config
	config.Ingress = upsertCloudflareIngressHosts(config.Ingress, []string{domain, "www." + domain}, "http://127.0.0.1:80")
	if config.WarpRouting == nil {
		config.WarpRouting = map[string]any{"enabled": false}
	}

	body, err := json.Marshal(map[string]any{"config": config})
	if err != nil {
		return err
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	var updated cloudflareTunnelConfigEnvelope
	if err := c.doJSON(req, &updated); err != nil {
		return err
	}
	if !updated.Success {
		return fmt.Errorf("Cloudflare tunnel config update failed: %s", cloudflareErrorMessage(updated.Errors))
	}
	return nil
}

func upsertCloudflareIngressHosts(existing []cloudflareTunnelIngress, hosts []string, service string) []cloudflareTunnelIngress {
	seen := map[string]bool{}
	for i := range existing {
		host := strings.ToLower(strings.TrimSpace(existing[i].Hostname))
		if host == "" {
			continue
		}
		seen[host] = true
		for _, wanted := range hosts {
			if host == strings.ToLower(strings.TrimSpace(wanted)) {
				existing[i].Service = service
				if existing[i].OriginRequest == nil {
					existing[i].OriginRequest = map[string]any{}
				}
			}
		}
	}
	var additions []cloudflareTunnelIngress
	for _, host := range hosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" || seen[host] {
			continue
		}
		additions = append(additions, cloudflareTunnelIngress{
			Hostname:      host,
			Service:       service,
			OriginRequest: map[string]any{},
		})
	}
	if len(additions) == 0 {
		return existing
	}
	insertAt := len(existing)
	for i, ingress := range existing {
		if strings.HasPrefix(strings.TrimSpace(ingress.Service), "http_status:") && strings.TrimSpace(ingress.Hostname) == "" {
			insertAt = i
			break
		}
	}
	updated := make([]cloudflareTunnelIngress, 0, len(existing)+len(additions))
	updated = append(updated, existing[:insertAt]...)
	updated = append(updated, additions...)
	updated = append(updated, existing[insertAt:]...)
	return updated
}

func cloudflareTunnelID(value string) string {
	value = normalizeCloudflareTunnelTarget(value)
	if value == "" {
		return ""
	}
	if before, _, ok := strings.Cut(value, "."); ok {
		return before
	}
	return value
}

func (c *cloudflareClient) CheckZoneActivation(ctx context.Context, zoneID string) error {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return fmt.Errorf("Cloudflare zone ID is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.cfg.APIURL+"/zones/"+zoneID+"/activation_check", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	req.Header.Set("Accept", "application/json")

	var response cloudflareZoneEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return err
	}
	if !response.Success {
		return fmt.Errorf("Cloudflare activation check failed: %s", cloudflareErrorMessage(response.Errors))
	}
	return nil
}

func normalizeCloudflareTunnelTarget(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.Trim(value, ". ")
	if value == "" {
		return ""
	}
	if strings.Contains(value, ".") {
		return value
	}
	return value + ".cfargotunnel.com"
}

func (c *cloudflareClient) writeDNSRecord(ctx context.Context, method, zoneID, recordID string, record CloudflareDNSRecord) (CloudflareDNSRecord, error) {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return CloudflareDNSRecord{}, fmt.Errorf("Cloudflare zone ID is required")
	}
	if err := validateCloudflareDNSRecord(record); err != nil {
		return CloudflareDNSRecord{}, err
	}
	body, err := json.Marshal(cloudflareDNSRecordToAPI(record))
	if err != nil {
		return CloudflareDNSRecord{}, err
	}
	path := c.cfg.APIURL + "/zones/" + zoneID + "/dns_records"
	if recordID != "" {
		path += "/" + recordID
	}
	req, err := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	if err != nil {
		return CloudflareDNSRecord{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	var response cloudflareDNSRecordEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return CloudflareDNSRecord{}, err
	}
	if !response.Success {
		return CloudflareDNSRecord{}, fmt.Errorf("Cloudflare DNS record write failed: %s", cloudflareErrorMessage(response.Errors))
	}
	return cloudflareDNSRecordFromAPI(response.Result), nil
}

func (c *cloudflareClient) checkDomains(ctx context.Context, domains []string) ([]DomainCheckResult, error) {
	payload := map[string][]string{"domains": domains}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	endpoint := c.cfg.APIURL + "/accounts/" + c.cfg.AccountID + "/registrar/domain-check"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	var response cloudflareEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return nil, err
	}
	if !response.Success {
		return nil, fmt.Errorf("Cloudflare check failed: %s", cloudflareErrorMessage(response.Errors))
	}
	if len(response.Result.Domains) == 0 {
		return nil, fmt.Errorf("Cloudflare did not return a domain result")
	}

	var results []DomainCheckResult
	for _, checked := range response.Result.Domains {
		results = append(results, cloudflareResultToDomainCheck(checked))
	}
	return results, nil
}

func validateCloudflareDNSRecord(record CloudflareDNSRecord) error {
	recordType := strings.ToUpper(strings.TrimSpace(record.Type))
	name := strings.ToLower(strings.TrimSpace(record.Name))
	content := strings.TrimSpace(record.Content)
	if recordType == "" {
		return fmt.Errorf("Cloudflare DNS record type is required")
	}
	if name == "" {
		return fmt.Errorf("Cloudflare DNS record name is required")
	}
	if content == "" {
		return fmt.Errorf("Cloudflare DNS record content is required")
	}
	if recordType == "MX" && record.Priority <= 0 {
		return fmt.Errorf("Cloudflare MX record priority is required")
	}
	return nil
}

func cloudflareDNSRecordToAPI(record CloudflareDNSRecord) cloudflareDNSRecord {
	return cloudflareDNSRecord{
		Type:     strings.ToUpper(strings.TrimSpace(record.Type)),
		Name:     strings.ToLower(strings.TrimSpace(record.Name)),
		Content:  strings.TrimSpace(record.Content),
		TTL:      record.TTL,
		Proxied:  record.Proxied,
		Priority: record.Priority,
		Comment:  strings.TrimSpace(record.Comment),
		Tags:     record.Tags,
	}
}

func cloudflareDNSRecordFromAPI(record cloudflareDNSRecord) CloudflareDNSRecord {
	return CloudflareDNSRecord{
		ID:       record.ID,
		Type:     record.Type,
		Name:     record.Name,
		Content:  record.Content,
		TTL:      record.TTL,
		Proxied:  record.Proxied,
		Priority: record.Priority,
		Comment:  record.Comment,
		Tags:     record.Tags,
	}
}

func cloudflareResultToDomainCheck(checked cloudflareDomainResult) DomainCheckResult {
	sld, tld := splitDomainForDisplay(checked.Name)
	result := DomainCheckResult{
		Domain:        checked.Name,
		SLD:           sld,
		TLD:           tld,
		Available:     checked.Registrable,
		Premium:       checked.Tier == "premium",
		Message:       cloudflareDomainMessage(checked),
		Reason:        checked.Reason,
		RegisterPrice: parseCloudflarePriceCents(checked.Pricing.RegistrationCost),
		RenewPrice:    parseCloudflarePriceCents(checked.Pricing.RenewalCost),
		Currency:      checked.Pricing.Currency,
		Provider:      "cloudflare",
	}
	if result.Domain == "" {
		result.Domain = checked.Name
	}
	if result.Currency == "" {
		result.Currency = "USD"
	}
	return result
}

func (c *cloudflareClient) doJSON(req *http.Request, target any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var response cloudflareEnvelope
		if err := json.Unmarshal(body, &response); err == nil {
			message := cloudflareErrorMessage(response.Errors)
			if message != "unknown error" {
				return fmt.Errorf("Cloudflare returned HTTP %d: %s", resp.StatusCode, message)
			}
		}
		return fmt.Errorf("Cloudflare returned HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("could not parse Cloudflare response")
	}
	return nil
}

func cloudflareDomainMessage(domain cloudflareDomainResult) string {
	if domain.Registrable {
		return "Domain Available"
	}
	if domain.Reason != "" {
		return domain.Reason
	}
	return "Domain Not Available"
}

func cloudflareErrorMessage(errors []cloudflareAPIMessage) string {
	if len(errors) == 0 {
		return "unknown error"
	}
	return errors[0].Message
}

func parseCloudflarePriceCents(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return int(f*100 + 0.5)
}

func splitDomainForDisplay(domain string) (string, string) {
	parts := strings.SplitN(domain, ".", 2)
	if len(parts) != 2 {
		return domain, ""
	}
	return parts[0], parts[1]
}
