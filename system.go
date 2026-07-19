package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SystemView struct {
	Domains      []AdminDomain
	DomainAdmins []RoleIdentity
	BoxAdmins    []RoleIdentity
	Access       AccessView
	Error        string
	Success      string
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
		domains = append(domains, AdminDomain{
			Name:                 domain,
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
		Domains:      domains,
		DomainAdmins: roleList(domainAdmins),
		BoxAdmins:    roleList(boxAdmins),
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
	return saveRegistrationCertificate(domain, username, time.Now())
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
	return os.WriteFile(path, registrationCertificate(domain, username, now), 0640)
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
		"The temporary mailbox password is not printed on this certificate.",
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
