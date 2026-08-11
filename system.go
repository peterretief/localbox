package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SystemView struct {
	Domains            []AdminDomain
	DomainAdmins       []RoleIdentity
	BoxAdmins          []RoleIdentity
	AdminInventoryPath string
	Access             AccessView
	Error              string
	Success            string
}

func systemView() SystemView {
	domainNames, _ := loadDomains()
	users, _ := loadUsers()

	userCounts := map[string]int{}
	for _, user := range users {
		userCounts[user.Domain]++
	}

	var domains []AdminDomain
	for _, domain := range domainNames {
		_ = ensureRegistrationCertificateForUsers(domain, users, time.Now())
		parent := configuredParentDomainFromList(domain, domainNames)
		domains = append(domains, AdminDomain{
			Name:                 domain,
			ParentDomain:         parent,
			IsSubdomain:          parent != "",
			UserCount:            userCounts[domain],
			SitePath:             domainSiteBase(domain),
			MailPath:             domainMailBase(domain),
			CertificatePath:      registrationCertificatePath(domain),
			CertificateAvailable: registrationCertificateExists(domain),
		})
	}

	domainAdmins, _ := loadRoleFile(adminsFile)
	boxAdmins, _ := loadBoxAdminsForDisplay()

	return SystemView{
		Domains:            domains,
		DomainAdmins:       roleList(domainAdmins),
		BoxAdmins:          roleList(boxAdmins),
		AdminInventoryPath: effectiveAdminInventoryFile(),
	}
}

func loadBoxAdminsForDisplay() (map[string]bool, error) {
	boxAdmins, err := loadRoleFile(boxAdminsFile)
	if err != nil {
		return nil, err
	}
	if len(boxAdmins) == 0 {
		fallback, err := firstConfiguredUser()
		if err != nil {
			return boxAdmins, nil
		}
		boxAdmins[fallback.User+":"+fallback.Domain] = true
	}
	return boxAdmins, nil
}

func addRole(path, username, domain string) error {
	key, err := roleKey(username, domain)
	if err != nil {
		return err
	}
	roles, err := loadRoleFile(path)
	if err != nil {
		return err
	}
	roles[key] = true
	return saveRoleFile(path, roles)
}

func removeRole(path, username, domain string) error {
	key, err := normalizeRoleKey(username, domain)
	if err != nil {
		return err
	}
	roles, err := loadRoleFile(path)
	if err != nil {
		return err
	}
	delete(roles, key)
	return saveRoleFile(path, roles)
}

func addDomainAdmin(username, domain string) error {
	return addRole(adminsFile, username, domain)
}

func createDomainAdmin(username, domain, password string) error {
	username = normalizeMailboxUser(username)
	domain = normalizeDomain(domain)
	if !userExists(username, domain) {
		if err := addMailbox(username, domain, password); err != nil {
			return err
		}
	}
	if err := addDomainAdmin(username, domain); err != nil {
		return err
	}
	if err := saveRegistrationCertificate(domain, username, time.Now()); err != nil {
		return err
	}
	return syncDomainWebRoute(domain)
}

func removeDomainAdmin(username, domain string) error {
	return removeRole(adminsFile, username, domain)
}

func addBoxAdmin(username, domain string) error {
	return addRole(boxAdminsFile, username, domain)
}

func removeBoxAdmin(username, domain string) error {
	key, err := normalizeRoleKey(username, domain)
	if err != nil {
		return err
	}
	roles, err := loadBoxAdminsForDisplay()
	if err != nil {
		return err
	}
	if !roles[key] {
		return fmt.Errorf("box admin not found")
	}
	if len(roles) <= 1 {
		return fmt.Errorf("cannot remove the last box admin")
	}
	delete(roles, key)
	return saveRoleFile(boxAdminsFile, roles)
}

var syncDomainWebRoute = syncDomainWebRouteImpl

func syncDomainWebRouteImpl(domain string) error {
	client, domain, err := domainWebRouteClient(domain)
	if err != nil || client == nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	zone, err := findCloudflareZoneForDomain(ctx, client, domain)
	if err != nil {
		return err
	}
	if err := client.UpsertTunnelRecords(ctx, zone.ID, domain); err != nil {
		return err
	}
	for _, suffix := range localboxTestSubdomainSuffixes() {
		if suffix == domain {
			return client.UpsertTunnelWildcardWebRoute(ctx, zone.ID, domain)
		}
	}
	return nil
}

var syncDomainWildcardWebRoute = syncDomainWildcardWebRouteImpl

func syncDomainWildcardWebRouteImpl(parentDomain string) error {
	client, parentDomain, err := domainWebRouteClient(parentDomain)
	if err != nil || client == nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	zone, err := findCloudflareZoneForDomain(ctx, client, parentDomain)
	if err != nil {
		return err
	}
	return client.UpsertTunnelWildcardWebRoute(ctx, zone.ID, parentDomain)
}

func deleteDomainWebRoute(domain string) error {
	client, domain, err := domainWebRouteClient(domain)
	if err != nil || client == nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	zone, err := findCloudflareZoneForDomain(ctx, client, domain)
	if err != nil {
		return err
	}
	return client.DeleteTunnelRecords(ctx, zone.ID, domain)
}

func findCloudflareZoneForDomain(ctx context.Context, client *cloudflareClient, domain string) (CloudflareZoneResult, error) {
	domain = normalizeDomain(domain)
	if !safeDomain(domain) {
		return CloudflareZoneResult{}, fmt.Errorf("invalid domain")
	}
	labels := strings.Split(domain, ".")
	var lastErr error
	for i := 0; i <= len(labels)-2; i++ {
		candidate := strings.Join(labels[i:], ".")
		zone, err := client.FindZoneByName(ctx, candidate)
		if err == nil {
			return zone, nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "zone not found") {
			return CloudflareZoneResult{}, err
		}
	}
	if lastErr != nil {
		return CloudflareZoneResult{}, lastErr
	}
	return CloudflareZoneResult{}, fmt.Errorf("Cloudflare zone not found for %s", domain)
}

func domainWebRouteClient(domain string) (*cloudflareClient, string, error) {
	domain = normalizeDomain(domain)
	if !safeDomain(domain) {
		return nil, domain, fmt.Errorf("invalid domain")
	}
	cfg, err := loadCloudflareConfig(cloudflareConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain, nil
		}
		return nil, domain, err
	}
	if !cfg.Enabled {
		return nil, domain, nil
	}
	if err := cfg.validate(); err != nil {
		return nil, domain, err
	}
	return newCloudflareClient(cfg), domain, nil
}

func userHasRoles(username, domain string) bool {
	key := normalizeMailboxUser(username) + ":" + normalizeDomain(domain)
	for _, path := range []string{adminsFile, boxAdminsFile} {
		roles, err := loadRoleFile(path)
		if err == nil && roles[key] {
			return true
		}
	}
	if isBoxAdmin(username, domain) {
		return true
	}
	return false
}

func domainHasRoles(domain string) bool {
	domain = normalizeDomain(domain)
	for _, path := range []string{adminsFile, boxAdminsFile} {
		roles, err := loadRoleFile(path)
		if err != nil {
			continue
		}
		for key := range roles {
			parts := splitRoleKey(key)
			if parts.Domain == domain {
				return true
			}
		}
	}
	boxAdmins, err := loadBoxAdminsForDisplay()
	if err == nil {
		for key := range boxAdmins {
			if splitRoleKey(key).Domain == domain {
				return true
			}
		}
	}
	return false
}

func splitRoleKey(key string) RoleIdentity {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return RoleIdentity{}
	}
	return RoleIdentity{User: parts[0], Domain: parts[1]}
}

func registrationCertificatePath(domain string) string {
	return filepath.Join(domainBase(domain), "certificates", "registration-certificate.txt")
}

func registrationCertificateSVGPath(domain string) string {
	return filepath.Join(domainBase(domain), "certificates", "registration-certificate.svg")
}

func registrationCertificateNumber(domain, registrarReference string, issued time.Time) string {
	seed := normalizeDomain(domain) + "|" + strings.TrimSpace(registrarReference) + "|" + issued.UTC().Format("2006-01-02")
	digest := sha256.Sum256([]byte(seed))
	return "LB-" + issued.UTC().Format("20060102") + "-" + strings.ToUpper(hex.EncodeToString(digest[:])[:10])
}

func registrationCertificateExists(domain string) bool {
	_, err := os.Stat(registrationCertificatePath(domain))
	return err == nil
}

func ensureRegistrationCertificateForUsers(domain string, users []User, now time.Time) error {
	domain = normalizeDomain(domain)
	if !safeDomain(domain) || registrationCertificateExists(domain) {
		return nil
	}
	var usernames []string
	for _, user := range users {
		if user.Domain == domain {
			usernames = append(usernames, user.Username)
		}
	}
	if len(usernames) == 0 {
		return nil
	}
	sort.Strings(usernames)
	return saveRegistrationCertificate(domain, usernames[0], now)
}

func saveRegistrationCertificate(domain, username string, now time.Time) error {
	domain = normalizeDomain(domain)
	username = normalizeMailboxUser(username)
	if !safeDomain(domain) || !safeMailboxUser(username) {
		return fmt.Errorf("invalid certificate details")
	}
	path := registrationCertificatePath(domain)
	if err := os.MkdirAll(filepath.Dir(path), 0770); err != nil {
		return err
	}
	if err := os.WriteFile(path, registrationCertificate(domain, username, now), 0640); err != nil {
		return err
	}
	return os.WriteFile(registrationCertificateSVGPath(domain), registrationCertificateSVG(domain, "", "", username, "", "", "", "", "", now.Format(time.RFC3339), now), 0640)
}

func registrationCertificate(domain, username string, now time.Time) []byte {
	lines := []string{
		"LOCALBOX DOMAIN SETUP CERTIFICATE",
		"",
		"Domain: " + normalizeDomain(domain),
		"First email address: " + normalizeMailboxUser(username) + "@" + normalizeDomain(domain),
		"Issued: " + now.UTC().Format(time.RFC3339),
		"",
		"This confirms that LocalBox has completed the local setup for this registered domain.",
		"Domain registration is handled as a separate registrar step and is not performed by the public request form.",
	}

	var b bytes.Buffer
	for _, line := range lines {
		fmt.Fprintln(&b, line)
	}

	users, err := loadUsers()
	if err == nil {
		var addresses []string
		domain = normalizeDomain(domain)
		for _, user := range users {
			if user.Domain == domain {
				addresses = append(addresses, user.Username+"@"+user.Domain)
			}
		}
		if len(addresses) > 0 {
			sort.Strings(addresses)
			fmt.Fprintln(&b)
			fmt.Fprintln(&b, "Mailboxes present at issue time:")
			for _, address := range addresses {
				fmt.Fprintln(&b, "- "+address)
			}
		}
	}

	return b.Bytes()
}

func registrationCertificateSVG(domain, registrant, websiteURL, username, registrarReference, voucher, physicalAddress, phone, emailAddress, detailsSuppliedAt string, now time.Time) []byte {
	domain = normalizeDomain(domain)
	registrant = strings.TrimSpace(registrant)
	websiteURL = strings.TrimSpace(websiteURL)
	email := strings.TrimSpace(emailAddress)
	if safeMailboxUser(username) {
		email = normalizeMailboxUser(username) + "@" + domain
	}
	row := func(label, value string, y int) string {
		if strings.TrimSpace(value) == "" {
			return ""
		}
		return fmt.Sprintf(`<text x="150" y="%d" class="label">%s</text><text x="390" y="%d" class="value">%s</text>`, y, html.EscapeString(label), y, html.EscapeString(value))
	}

	var b bytes.Buffer
	party := func(x, y int, title, contact, postal, phoneNumber, emailAddress string) string {
		return fmt.Sprintf(`<text x="%d" y="%d" class="section">%s</text><text x="%d" y="%d" class="party">Contact: %s</text><text x="%d" y="%d" class="party">Postal: %s</text><text x="%d" y="%d" class="party">E-Mail: %s</text>`, x, y, html.EscapeString(title), x, y+23, html.EscapeString(contact), x, y+44, html.EscapeString(postal), x, y+65, html.EscapeString(emailAddress))
	}

	fmt.Fprint(&b, `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="1120" height="950" viewBox="0 0 1120 950" data-template="v5" role="document" aria-labelledby="title description">
  <title id="title">LocalBox Domain Registration Certificate</title>
  <desc id="description">Official registration record issued by LocalBox.</desc>
  <rect width="1120" height="950" fill="#f5f1e8"/>
  <rect x="34" y="34" width="1052" height="882" fill="#fffdf8" stroke="#183b35" stroke-width="4"/>
  <rect x="54" y="54" width="1012" height="842" fill="none" stroke="#c6924b" stroke-width="2"/>
  <circle cx="560" cy="128" r="42" fill="#183b35"/>
  <path d="M540 128l14 14 28-32" fill="none" stroke="#fffdf8" stroke-width="7" stroke-linecap="round" stroke-linejoin="round"/>
  <text x="560" y="220" text-anchor="middle" class="title">DOMAIN REGISTRATION CERTIFICATE</text>
  <text x="560" y="254" text-anchor="middle" class="subtitle">Issued by LocalBox</text>
  <line x1="150" y1="286" x2="970" y2="286" stroke="#c6924b" stroke-width="2"/>
  <style>
    .title { font: 700 30px Georgia, serif; letter-spacing: 1px; fill: #183b35; }
    .subtitle { font: 18px Arial, sans-serif; fill: #5d6b66; }
    .label { font: 700 16px Arial, sans-serif; fill: #5d6b66; }
    .value { font: 18px Arial, sans-serif; fill: #183b35; }
    .section { font: 700 16px Arial, sans-serif; fill: #183b35; }
    .party { font: 13px Arial, sans-serif; fill: #5d6b66; }
    .note { font: 15px Arial, sans-serif; fill: #5d6b66; }
  </style>`)
	fmt.Fprint(&b, row("Domain", domain, 340))
	fmt.Fprint(&b, row("Registrant", registrant, 388))
	fmt.Fprint(&b, row("First email address", email, 436))
	fmt.Fprint(&b, row("Website", websiteURL, 484))
	fmt.Fprint(&b, row("Certificate number", registrationCertificateNumber(domain, registrarReference, now), 532))
	fmt.Fprint(&b, row("Registrar reference", registrarReference, 580))
	fmt.Fprint(&b, row("Issued", now.UTC().Format(time.RFC3339), 676))
	fmt.Fprint(&b, row("Details supplied", detailsSuppliedAt, 724))
	fmt.Fprint(&b, party(120, 790, "ISSUED BY LOCALBOX", "LocalBox Mail", "https://localboxmail.co.za", "", "admin@localboxmail.co.za"))
	fmt.Fprint(&b, party(590, 790, "CUSTOMER DETAILS", registrant, physicalAddress, "", email))
	fmt.Fprint(&b, `</svg>`)
	return b.Bytes()
}

func ensureRegistrationCertificateSVG(domain, username string) error {
	path := registrationCertificateSVGPath(domain)
	data, err := os.ReadFile(path)
	if err == nil && bytes.Contains(data, []byte(`data-template="v5"`)) {
		return nil
	}

	now := time.Now().UTC()
	certificate := registrationCertificateSVG(domain, "", "", username, "", "", "", "", "", now.Format(time.RFC3339), now)
	if summaries, summaryErr := loadOrderSummaries(); summaryErr == nil {
		for _, summary := range summaries {
			if normalizeDomain(summary.Domain) != normalizeDomain(domain) || strings.TrimSpace(summary.ArchivedAt) != "" {
				continue
			}
			if detail, detailErr := loadOrderDetail(summary.ID); detailErr == nil {
				if issued, parseErr := time.Parse(time.RFC3339, detail.CreatedAt); parseErr == nil {
					now = issued.UTC()
				}
				certificate = registrationCertificateSVG(domain, detail.FullName, detail.WebsiteURL, detail.WantedEmail, detail.RegistrarReference, detail.EmailVoucherCode, detail.PhysicalAddress, detail.Phone, detail.Email, detail.CreatedAt, now)
			}
			break
		}
	}
	return os.WriteFile(path, certificate, 0640)
}
