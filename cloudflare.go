package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultCloudflareAPIURL = "https://api.cloudflare.com/client/v4"

type CloudflareConfig struct {
	Enabled              bool
	APIURL               string
	AccountID            string
	APIToken             string
	EmailRoutingAPIToken string
	TunnelTarget         string
	EmailWorker          string
	EmailForwardTo       string
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

type cloudflareZoneListEnvelope struct {
	Success bool                   `json:"success"`
	Result  []cloudflareZoneResult `json:"result"`
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

type cloudflareEmailRoutingRuleEnvelope struct {
	Success bool                       `json:"success"`
	Result  cloudflareEmailRoutingRule `json:"result"`
	Errors  []cloudflareAPIMessage     `json:"errors"`
}

type cloudflareEmailRoutingRuleListEnvelope struct {
	Success bool                         `json:"success"`
	Result  []cloudflareEmailRoutingRule `json:"result"`
	Errors  []cloudflareAPIMessage       `json:"errors"`
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

type cloudflareEmailRoutingRule struct {
	ID       string                          `json:"id,omitempty"`
	Name     string                          `json:"name"`
	Enabled  bool                            `json:"enabled"`
	Matchers []cloudflareEmailRoutingMatcher `json:"matchers"`
	Actions  []cloudflareEmailRoutingAction  `json:"actions"`
}

type cloudflareEmailRoutingMatcher struct {
	Type  string `json:"type"`
	Field string `json:"field,omitempty"`
	Value string `json:"value,omitempty"`
}

type cloudflareEmailRoutingAction struct {
	Type  string   `json:"type"`
	Value []string `json:"value"`
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
		case "email_routing_api_token":
			cfg.EmailRoutingAPIToken = value
		case "tunnel_target", "cfargotunnel", "tunnel_cname":
			cfg.TunnelTarget = normalizeCloudflareTunnelTarget(value)
		case "email_worker":
			cfg.EmailWorker = strings.TrimSpace(value)
		case "email_forward_to":
			cfg.EmailForwardTo = normalizeEmailAddress(value)
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

func (cfg CloudflareConfig) emailRoutingToken() string {
	if strings.TrimSpace(cfg.EmailRoutingAPIToken) != "" {
		return strings.TrimSpace(cfg.EmailRoutingAPIToken)
	}
	return strings.TrimSpace(cfg.APIToken)
}

func (cfg CloudflareConfig) emailRoutingEnabled() bool {
	return cfg.Enabled && (strings.TrimSpace(cfg.EmailWorker) != "" || strings.TrimSpace(cfg.EmailForwardTo) != "")
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

func (c *cloudflareClient) FindZoneByName(ctx context.Context, domain string) (CloudflareZoneResult, error) {
	domain, err := normalizePublicDomain(domain)
	if err != nil {
		return CloudflareZoneResult{}, err
	}
	values := url.Values{}
	values.Set("name", domain)
	values.Set("account.id", c.cfg.AccountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.APIURL+"/zones?"+values.Encode(), nil)
	if err != nil {
		return CloudflareZoneResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	req.Header.Set("Accept", "application/json")

	var response cloudflareZoneListEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return CloudflareZoneResult{}, err
	}
	if !response.Success {
		return CloudflareZoneResult{}, fmt.Errorf("Cloudflare zone lookup failed: %s", cloudflareErrorMessage(response.Errors))
	}
	if len(response.Result) == 0 || strings.TrimSpace(response.Result[0].ID) == "" {
		return CloudflareZoneResult{}, fmt.Errorf("Cloudflare zone not found for %s", domain)
	}
	zone := response.Result[0]
	return CloudflareZoneResult{
		ID:          zone.ID,
		Name:        zone.Name,
		Status:      zone.Status,
		NameServers: zone.NameServers,
	}, nil
}

func (c *cloudflareClient) UpsertEmailRoute(ctx context.Context, address string) error {
	address, domain, err := normalizeRouteEmailAddress(address)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.cfg.EmailWorker) != "" && strings.TrimSpace(c.cfg.EmailForwardTo) != "" {
		return fmt.Errorf("choose either Cloudflare email_worker or email_forward_to, not both")
	}
	if strings.TrimSpace(c.cfg.EmailWorker) == "" && strings.TrimSpace(c.cfg.EmailForwardTo) == "" {
		return fmt.Errorf("Cloudflare email routing target is required")
	}

	zone, err := c.FindZoneForEmailDomain(ctx, domain)
	if err != nil {
		return err
	}
	rules, err := c.ListEmailRoutingRules(ctx, zone.ID)
	if err != nil {
		return err
	}

	rule := cloudflareEmailRoutingRule{
		Name:    "LocalBox " + address,
		Enabled: true,
		Matchers: []cloudflareEmailRoutingMatcher{{
			Type:  "literal",
			Field: "to",
			Value: address,
		}},
		Actions: []cloudflareEmailRoutingAction{c.emailRoutingAction()},
	}
	// The setup audit calls this for every mailbox every two minutes, so a rule
	// that is already right is left alone: no DNS enable, no rewrite.
	for _, existing := range rules {
		if emailRoutingRuleMatchesAddress(existing, address) && emailRoutingRulesEqual(existing, rule) {
			return nil
		}
	}
	if err := c.EnableEmailRoutingDNSForDomain(ctx, zone.ID, zone.Name, domain); err != nil {
		return err
	}
	for _, existing := range rules {
		if emailRoutingRuleMatchesAddress(existing, address) {
			return c.writeEmailRoutingRule(ctx, http.MethodPut, zone.ID, existing.ID, rule)
		}
	}
	return c.writeEmailRoutingRule(ctx, http.MethodPost, zone.ID, "", rule)
}

func emailRoutingRulesEqual(a, b cloudflareEmailRoutingRule) bool {
	if a.Name != b.Name || a.Enabled != b.Enabled || len(a.Matchers) != len(b.Matchers) || len(a.Actions) != len(b.Actions) {
		return false
	}
	for i := range a.Matchers {
		if !strings.EqualFold(a.Matchers[i].Type, b.Matchers[i].Type) ||
			!strings.EqualFold(a.Matchers[i].Field, b.Matchers[i].Field) ||
			normalizeEmailAddress(a.Matchers[i].Value) != normalizeEmailAddress(b.Matchers[i].Value) {
			return false
		}
	}
	for i := range a.Actions {
		if !strings.EqualFold(a.Actions[i].Type, b.Actions[i].Type) || len(a.Actions[i].Value) != len(b.Actions[i].Value) {
			return false
		}
		for j := range a.Actions[i].Value {
			if !strings.EqualFold(a.Actions[i].Value[j], b.Actions[i].Value[j]) {
				return false
			}
		}
	}
	return true
}

func (c *cloudflareClient) FindZoneForEmailDomain(ctx context.Context, domain string) (CloudflareZoneResult, error) {
	domain = normalizeDomain(domain)
	for candidate := domain; ; {
		zone, err := c.FindZoneByName(ctx, candidate)
		if err == nil {
			return zone, nil
		}
		dot := strings.IndexByte(candidate, '.')
		if dot < 0 {
			return CloudflareZoneResult{}, err
		}
		candidate = candidate[dot+1:]
	}
}

func (c *cloudflareClient) DeleteEmailRoute(ctx context.Context, address string) error {
	address, domain, err := normalizeRouteEmailAddress(address)
	if err != nil {
		return err
	}
	zone, err := c.FindZoneByName(ctx, domain)
	if err != nil {
		return err
	}
	rules, err := c.ListEmailRoutingRules(ctx, zone.ID)
	if err != nil {
		return err
	}
	for _, existing := range rules {
		if emailRoutingRuleMatchesAddress(existing, address) {
			return c.deleteEmailRoutingRule(ctx, zone.ID, existing.ID)
		}
	}
	return nil
}

func (c *cloudflareClient) EnableEmailRoutingDNS(ctx context.Context, zoneID string) error {
	return c.EnableEmailRoutingDNSForDomain(ctx, zoneID, "", "")
}

func (c *cloudflareClient) EnableEmailRoutingDNSForDomain(ctx context.Context, zoneID, zoneDomain, emailDomain string) error {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return fmt.Errorf("Cloudflare zone ID is required")
	}
	payload := "{}"
	if emailDomain != "" && !strings.EqualFold(normalizeDomain(zoneDomain), normalizeDomain(emailDomain)) {
		encoded, err := json.Marshal(map[string]string{"name": normalizeDomain(emailDomain)})
		if err != nil {
			return err
		}
		payload = string(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIURL+"/zones/"+zoneID+"/email/routing/dns", strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.emailRoutingToken())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	var response struct {
		Success bool                   `json:"success"`
		Errors  []cloudflareAPIMessage `json:"errors"`
	}
	if err := c.doJSON(req, &response); err != nil {
		return err
	}
	if !response.Success {
		return fmt.Errorf("Cloudflare Email Routing DNS enable failed: %s", cloudflareErrorMessage(response.Errors))
	}
	return nil
}

func (c *cloudflareClient) ListEmailRoutingRules(ctx context.Context, zoneID string) ([]cloudflareEmailRoutingRule, error) {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return nil, fmt.Errorf("Cloudflare zone ID is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.APIURL+"/zones/"+zoneID+"/email/routing/rules", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.emailRoutingToken())
	req.Header.Set("Accept", "application/json")

	var response cloudflareEmailRoutingRuleListEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return nil, err
	}
	if !response.Success {
		return nil, fmt.Errorf("Cloudflare Email Routing rules list failed: %s", cloudflareErrorMessage(response.Errors))
	}
	return response.Result, nil
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

func (c *cloudflareClient) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	zoneID = strings.TrimSpace(zoneID)
	recordID = strings.TrimSpace(recordID)
	if zoneID == "" {
		return fmt.Errorf("Cloudflare zone ID is required")
	}
	if recordID == "" {
		return fmt.Errorf("Cloudflare DNS record ID is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.cfg.APIURL+"/zones/"+zoneID+"/dns_records/"+recordID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	req.Header.Set("Accept", "application/json")

	var response cloudflareDNSRecordEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return err
	}
	if !response.Success {
		return fmt.Errorf("Cloudflare DNS record delete failed: %s", cloudflareErrorMessage(response.Errors))
	}
	return nil
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
	hosts := []string{domain, "www." + domain, "files." + domain}
	if !usesCentralWebmailHost(domain) {
		hosts = append(hosts, "mail."+domain)
	}
	for _, host := range hosts {
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

func (c *cloudflareClient) UpsertTunnelWildcardWebRoute(ctx context.Context, zoneID, parentDomain string) error {
	parentDomain, err := normalizePublicDomain(parentDomain)
	if err != nil {
		return err
	}
	target := normalizeCloudflareTunnelTarget(c.cfg.TunnelTarget)
	if target == "" {
		return fmt.Errorf("Cloudflare tunnel target is required")
	}
	proxied := true
	wildcard := "*." + parentDomain
	if _, err := c.UpsertDNSRecord(ctx, zoneID, CloudflareDNSRecord{
		Type:    "CNAME",
		Name:    wildcard,
		Content: target,
		TTL:     1,
		Proxied: &proxied,
		Comment: "LocalBox hosted subdomain wildcard route",
	}); err != nil {
		return err
	}
	return c.UpsertTunnelIngressWildcardWebRoute(ctx, parentDomain)
}

func (c *cloudflareClient) DeleteTunnelRecords(ctx context.Context, zoneID, domain string) error {
	domain, err := normalizePublicDomain(domain)
	if err != nil {
		return err
	}
	target := normalizeCloudflareTunnelTarget(c.cfg.TunnelTarget)
	if target == "" {
		return fmt.Errorf("Cloudflare tunnel target is required")
	}
	for _, host := range []string{domain, "www." + domain, "mail." + domain, "files." + domain} {
		records, err := c.ListDNSRecords(ctx, zoneID, "CNAME", host)
		if err != nil {
			return err
		}
		for _, record := range records {
			if normalizeCloudflareTunnelTarget(record.Content) == target {
				if err := c.DeleteDNSRecord(ctx, zoneID, record.ID); err != nil {
					return err
				}
			}
		}
	}
	return c.DeleteTunnelIngressRoutes(ctx, domain)
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
	routes := []cloudflareTunnelHostRoute{
		{Hostname: domain, Service: "http://127.0.0.1:80"},
		{Hostname: "www." + domain, Service: "http://127.0.0.1:80"},
		{Hostname: "files." + domain, Service: "http://localhost:8082"},
	}
	if !usesCentralWebmailHost(domain) {
		routes = append(routes, cloudflareTunnelHostRoute{Hostname: "mail." + domain, Service: "http://127.0.0.1:1323"})
	}
	config.Ingress = upsertCloudflareIngressHosts(config.Ingress, routes)
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

func (c *cloudflareClient) UpsertTunnelIngressWildcardWebRoute(ctx context.Context, parentDomain string) error {
	parentDomain, err := normalizePublicDomain(parentDomain)
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
	config.Ingress = upsertCloudflareIngressHosts(config.Ingress, []cloudflareTunnelHostRoute{
		{Hostname: "*." + parentDomain, Service: "http://127.0.0.1:80"},
	})
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

func (c *cloudflareClient) DeleteTunnelIngressRoutes(ctx context.Context, domain string) error {
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
	config.Ingress = removeCloudflareIngressHosts(config.Ingress, []string{domain, "www." + domain, "mail." + domain, "files." + domain})
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

type cloudflareTunnelHostRoute struct {
	Hostname string
	Service  string
}

func upsertCloudflareIngressHosts(existing []cloudflareTunnelIngress, routes []cloudflareTunnelHostRoute) []cloudflareTunnelIngress {
	seen := map[string]bool{}
	for i := range existing {
		host := strings.ToLower(strings.TrimSpace(existing[i].Hostname))
		if host == "" {
			continue
		}
		seen[host] = true
		for _, route := range routes {
			if host == strings.ToLower(strings.TrimSpace(route.Hostname)) {
				existing[i].Service = route.Service
				if existing[i].OriginRequest == nil {
					existing[i].OriginRequest = map[string]any{}
				}
			}
		}
	}
	var additions []cloudflareTunnelIngress
	for _, route := range routes {
		host := strings.ToLower(strings.TrimSpace(route.Hostname))
		if host == "" || seen[host] {
			continue
		}
		additions = append(additions, cloudflareTunnelIngress{
			Hostname:      host,
			Service:       route.Service,
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

func removeCloudflareIngressHosts(existing []cloudflareTunnelIngress, hosts []string) []cloudflareTunnelIngress {
	remove := map[string]bool{}
	for _, host := range hosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" {
			remove[host] = true
		}
	}
	updated := existing[:0]
	for _, ingress := range existing {
		host := strings.ToLower(strings.TrimSpace(ingress.Hostname))
		if host != "" && remove[host] {
			continue
		}
		updated = append(updated, ingress)
	}
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
	const attempts = 5
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		lastErr = c.checkZoneActivationOnce(ctx, zoneID)
		if lastErr == nil {
			return nil
		}
		if attempt < attempts {
			select {
			case <-time.After(time.Duration(attempt) * 3 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return fmt.Errorf("Cloudflare zone %s activation not confirmed after %d attempts: %w", zoneID, attempts, lastErr)
}

func (c *cloudflareClient) checkZoneActivationOnce(ctx context.Context, zoneID string) error {
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

func normalizeEmailAddress(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeRouteEmailAddress(value string) (string, string, error) {
	address := normalizeEmailAddress(value)
	if address == "" {
		return "", "", fmt.Errorf("email address is required")
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil || normalizeEmailAddress(parsed.Address) != address {
		return "", "", fmt.Errorf("valid email address required")
	}
	local, domain, ok := strings.Cut(address, "@")
	if !ok || local == "" {
		return "", "", fmt.Errorf("valid email address required")
	}
	domain = normalizeDomain(domain)
	if !safeDomain(domain) {
		return "", "", fmt.Errorf("valid email domain required")
	}
	return address, domain, nil
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

func (c *cloudflareClient) writeEmailRoutingRule(ctx context.Context, method, zoneID, ruleID string, rule cloudflareEmailRoutingRule) error {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return fmt.Errorf("Cloudflare zone ID is required")
	}
	body, err := json.Marshal(rule)
	if err != nil {
		return err
	}
	path := c.cfg.APIURL + "/zones/" + zoneID + "/email/routing/rules"
	if strings.TrimSpace(ruleID) != "" {
		path += "/" + strings.TrimSpace(ruleID)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.emailRoutingToken())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	var response cloudflareEmailRoutingRuleEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return err
	}
	if !response.Success {
		return fmt.Errorf("Cloudflare Email Routing rule write failed: %s", cloudflareErrorMessage(response.Errors))
	}
	return nil
}

func (c *cloudflareClient) deleteEmailRoutingRule(ctx context.Context, zoneID, ruleID string) error {
	zoneID = strings.TrimSpace(zoneID)
	ruleID = strings.TrimSpace(ruleID)
	if zoneID == "" {
		return fmt.Errorf("Cloudflare zone ID is required")
	}
	if ruleID == "" {
		return fmt.Errorf("Cloudflare Email Routing rule ID is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.cfg.APIURL+"/zones/"+zoneID+"/email/routing/rules/"+ruleID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.emailRoutingToken())
	req.Header.Set("Accept", "application/json")

	var response cloudflareEmailRoutingRuleEnvelope
	if err := c.doJSON(req, &response); err != nil {
		return err
	}
	if !response.Success {
		return fmt.Errorf("Cloudflare Email Routing rule delete failed: %s", cloudflareErrorMessage(response.Errors))
	}
	return nil
}

func (c *cloudflareClient) emailRoutingAction() cloudflareEmailRoutingAction {
	if worker := strings.TrimSpace(c.cfg.EmailWorker); worker != "" {
		return cloudflareEmailRoutingAction{Type: "worker", Value: []string{worker}}
	}
	return cloudflareEmailRoutingAction{Type: "forward", Value: []string{normalizeEmailAddress(c.cfg.EmailForwardTo)}}
}

func emailRoutingRuleMatchesAddress(rule cloudflareEmailRoutingRule, address string) bool {
	address = normalizeEmailAddress(address)
	for _, matcher := range rule.Matchers {
		if strings.EqualFold(matcher.Field, "to") && normalizeEmailAddress(matcher.Value) == address {
			return true
		}
	}
	return false
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
