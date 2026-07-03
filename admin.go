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

type AdminDomain struct {
	Name      string
	UserCount int
	SitePath  string
	MailPath  string
}

type AdminView struct {
	Domain  AdminDomain
	Users   []User
	Error   string
	Success string
}

func loadDomains() ([]string, error) {
	domainSet := map[string]bool{}

	data, err := os.ReadFile(domainsFile)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			domain := strings.TrimSpace(line)
			if domain == "" || strings.HasPrefix(domain, "#") {
				continue
			}
			domainSet[domain] = true
		}
	}

	users, err := loadUsers()
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, user := range users {
		domainSet[user.Domain] = true
	}

	entries, err := os.ReadDir(filepath.Join(localBoxBase, "domains"))
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() && safeDomain(entry.Name()) {
				domainSet[entry.Name()] = true
			}
		}
	}
	var domains []string
	for domain := range domainSet {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains, nil
}

func saveDomains(domains []string) error {
	seen := map[string]bool{}
	var cleaned []string
	for _, domain := range domains {
		domain = normalizeDomain(domain)
		if !safeDomain(domain) || seen[domain] {
			continue
		}
		seen[domain] = true
		cleaned = append(cleaned, domain)
	}
	sort.Strings(cleaned)

	var b bytes.Buffer
	for _, domain := range cleaned {
		fmt.Fprintln(&b, domain)
	}
	return os.WriteFile(domainsFile, b.Bytes(), 0640)
}

func adminView(domain string) AdminView {
	users, _ := loadUsers()
	var domainUsers []User
	for _, user := range users {
		if user.Domain == domain {
			domainUsers = append(domainUsers, user)
		}
	}

	return AdminView{
		Users: domainUsers,
		Domain: AdminDomain{
			Name:      domain,
			UserCount: len(domainUsers),
			SitePath:  domainSiteBase(domain),
			MailPath:  domainMailBase(domain),
		},
	}
}

func addDomain(domain string) error {
	domain = normalizeDomain(domain)
	if !safeDomain(domain) {
		return fmt.Errorf("invalid domain")
	}

	domains, err := loadDomains()
	if err != nil {
		return err
	}
	for _, existing := range domains {
		if existing == domain {
			return ensureDomainDirs(domain)
		}
	}

	domains = append(domains, domain)
	if err := saveDomains(domains); err != nil {
		return err
	}
	return ensureDomainDirs(domain)
}

func ensureDomainDirs(domain string) error {
	if err := os.MkdirAll(filepath.Join(domainBase(domain), "mail"), 0770); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(domainSiteBase(domain), "assets", "images"), 0775); err != nil {
		return err
	}
	if err := ensureDefaultSite(domain); err != nil {
		return err
	}
	return nil
}

func ensureDefaultSite(domain string) error {
	if _, err := os.Stat(homeHTMLPath(domain)); os.IsNotExist(err) {
		if _, mdErr := os.Stat(homeMarkdownPath(domain)); os.IsNotExist(mdErr) {
			if err := saveHomePage(domain, defaultHomeMarkdown); err != nil {
				return err
			}
		}
	}

	for _, name := range []string{"contact.html", "thankyou.html"} {
		if err := copySiteTemplateIfMissing(domain, name); err != nil {
			return err
		}
	}
	return nil
}

func copySiteTemplateIfMissing(domain, name string) error {
	dst := filepath.Join(domainSiteBase(domain), name)
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	data, err := os.ReadFile(filepath.Join("templates", name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0664)
}

func archiveDomain(domain string) error {
	domain = normalizeDomain(domain)
	if !safeDomain(domain) {
		return fmt.Errorf("invalid domain")
	}

	users, err := loadUsers()
	if err != nil {
		return err
	}
	for _, user := range users {
		if user.Domain == domain {
			return fmt.Errorf("archive mailboxes before archiving this domain")
		}
	}
	if domainHasRoles(domain) {
		return fmt.Errorf("remove admin roles before archiving this domain")
	}

	domains, err := loadDomains()
	if err != nil {
		return err
	}
	var kept []string
	for _, existing := range domains {
		if existing != domain {
			kept = append(kept, existing)
		}
	}
	if err := saveDomains(kept); err != nil {
		return err
	}

	if err := archivePath(domainMailBase(domain)); err != nil {
		return err
	}
	return archivePath(domainSiteBase(domain))
}

func addMailbox(username, domain, password string) error {
	username = normalizeMailboxUser(username)
	domain = normalizeDomain(domain)
	if !safeMailboxUser(username) || !safeDomain(domain) {
		return fmt.Errorf("invalid email address")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if err := addDomain(domain); err != nil {
		return err
	}

	users, err := loadUsers()
	if err != nil {
		return err
	}
	for _, user := range users {
		if user.Username == username && user.Domain == domain {
			return fmt.Errorf("mailbox already exists")
		}
	}

	hash, err := hashPasswordStrong(password)
	if err != nil {
		return err
	}
	users = append(users, User{
		Username: username,
		Domain:   domain,
		Hash:     hash,
	})
	if err := saveUsers(users); err != nil {
		return err
	}
	return ensureMailboxDirs(domain, username)
}

func resetMailboxPassword(username, domain, password string) error {
	username = normalizeMailboxUser(username)
	domain = normalizeDomain(domain)
	if !safeMailboxUser(username) || !safeDomain(domain) {
		return fmt.Errorf("invalid mailbox")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}

	users, err := loadUsers()
	if err != nil {
		return err
	}
	for i, user := range users {
		if user.Username == username && user.Domain == domain {
			return setPasswordHashAt(users, i, password)
		}
	}
	return fmt.Errorf("mailbox not found")
}

func ensureMailboxDirs(domain, username string) error {
	base := maildirPath(domain, username)
	for _, dir := range []string{"new", "cur", "tmp"} {
		if err := os.MkdirAll(filepath.Join(base, dir), 0770); err != nil {
			return err
		}
	}
	return os.MkdirAll(attachmentsBase(domain, username), 0770)
}

func archiveMailbox(username, domain string) error {
	username = normalizeMailboxUser(username)
	domain = normalizeDomain(domain)
	if !safeMailboxUser(username) || !safeDomain(domain) {
		return fmt.Errorf("invalid mailbox")
	}

	users, err := loadUsers()
	if err != nil {
		return err
	}
	var kept []User
	found := false
	for _, user := range users {
		if user.Username == username && user.Domain == domain {
			found = true
			continue
		}
		kept = append(kept, user)
	}
	if !found {
		return fmt.Errorf("mailbox not found")
	}
	if userHasRoles(username, domain) {
		return fmt.Errorf("remove admin roles before archiving this mailbox")
	}
	if err := saveUsers(kept); err != nil {
		return err
	}
	return archivePath(mailboxBase(domain, username))
}

func archivePath(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	parent := filepath.Dir(path)
	archiveDir := filepath.Join(parent, ".archived")
	if err := os.MkdirAll(archiveDir, 0770); err != nil {
		return err
	}
	name := filepath.Base(path) + "-" + time.Now().UTC().Format("20060102T150405Z")
	return os.Rename(path, filepath.Join(archiveDir, name))
}

func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

func normalizeMailboxUser(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func safeDomain(domain string) bool {
	if domain == "" || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	for _, part := range strings.Split(domain, ".") {
		if !safeLabel.MatchString(part) {
			return false
		}
	}
	return strings.Contains(domain, ".")
}

func safeMailboxUser(username string) bool {
	return safeLabel.MatchString(username)
}
