package main

import (
	"fmt"
	"strings"
)

type SystemView struct {
	Domains      []AdminDomain
	DomainAdmins []RoleIdentity
	BoxAdmins    []RoleIdentity
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
		domains = append(domains, AdminDomain{
			Name:      domain,
			UserCount: userCounts[domain],
			SitePath:  domainSiteBase(domain),
			MailPath:  domainMailBase(domain),
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
	return addDomainAdmin(username, domain)
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
