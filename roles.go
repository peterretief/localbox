package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
)

type RoleIdentity struct {
	User   string
	Domain string
}

func isDomainAdmin(user, domain string) bool {
	admins, err := loadRoleFile(adminsFile)
	if err != nil {
		return false
	}
	return admins[user+":"+domain]
}

func isBoxAdmin(user, domain string) bool {
	admins, err := loadRoleFile(boxAdminsFile)
	if err != nil {
		return false
	}
	if len(admins) == 0 {
		fallback, err := firstConfiguredUser()
		if err != nil {
			return false
		}
		return fallback.User == user && fallback.Domain == domain
	}
	return admins[user+":"+domain]
}

func isOrdersAdmin(user, domain string) bool {
	primary, err := primaryDomain()
	if err != nil || primary == "" {
		return false
	}
	domain = normalizeDomain(domain)
	user = normalizeMailboxUser(user)
	return domain == primary && isDomainAdmin(user, domain)
}

func primaryDomain() (string, error) {
	data, err := os.ReadFile(domainsFile)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			domain := normalizeDomain(line)
			if safeDomain(domain) {
				return domain, nil
			}
		}
	}
	fallback, err := firstConfiguredUser()
	if err != nil {
		return "", err
	}
	return normalizeDomain(fallback.Domain), nil
}

func loadRoleFile(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}

	admins := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		user := normalizeMailboxUser(parts[0])
		domain := normalizeDomain(parts[1])
		if safeMailboxUser(user) && safeDomain(domain) {
			admins[user+":"+domain] = true
		}
	}
	return admins, nil
}

func saveRoleFile(path string, roles map[string]bool) error {
	var keys []string
	for key := range roles {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 || !safeMailboxUser(parts[0]) || !safeDomain(parts[1]) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b bytes.Buffer
	for _, key := range keys {
		fmt.Fprintln(&b, key)
	}
	return os.WriteFile(path, b.Bytes(), 0640)
}

func roleList(roles map[string]bool) []RoleIdentity {
	var list []RoleIdentity
	for key := range roles {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		list = append(list, RoleIdentity{User: parts[0], Domain: parts[1]})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Domain == list[j].Domain {
			return list[i].User < list[j].User
		}
		return list[i].Domain < list[j].Domain
	})
	return list
}

func roleKey(user, domain string) (string, error) {
	key, err := normalizeRoleKey(user, domain)
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(key, ":", 2)
	if !userExists(parts[0], parts[1]) {
		return "", fmt.Errorf("mailbox not found")
	}
	return key, nil
}

func normalizeRoleKey(user, domain string) (string, error) {
	user = normalizeMailboxUser(user)
	domain = normalizeDomain(domain)
	if !safeMailboxUser(user) || !safeDomain(domain) {
		return "", fmt.Errorf("invalid user or domain")
	}
	return user + ":" + domain, nil
}

func userExists(username, domain string) bool {
	users, err := loadUsers()
	if err != nil {
		return false
	}
	for _, user := range users {
		if user.Username == username && user.Domain == domain {
			return true
		}
	}
	return false
}

func requireMailboxSession(w http.ResponseWriter, r *http.Request) *Session {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return nil
	}
	if !userExists(s.User, s.Domain) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil
	}
	return s
}

func firstConfiguredUser() (RoleIdentity, error) {
	users, err := loadUsers()
	if err != nil {
		return RoleIdentity{}, err
	}
	if len(users) == 0 {
		return RoleIdentity{}, fmt.Errorf("no users configured")
	}
	return RoleIdentity{User: users[0].Username, Domain: users[0].Domain}, nil
}

func requireDomainAdmin(w http.ResponseWriter, r *http.Request) *Session {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return nil
	}
	if !isDomainAdmin(s.User, s.Domain) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil
	}
	return s
}

func requireBoxAdmin(w http.ResponseWriter, r *http.Request) *Session {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return nil
	}
	if !isBoxAdmin(s.User, s.Domain) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil
	}
	return s
}

func requireOrdersAdmin(w http.ResponseWriter, r *http.Request) *Session {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return nil
	}
	if !isOrdersAdmin(s.User, s.Domain) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil
	}
	return s
}
