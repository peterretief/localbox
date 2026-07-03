package main

import "path/filepath"

func contactDomainBase(domain string) string {
	return filepath.Join(contactLocalBoxBase, "domains", domain)
}

func contactDomainMailBase(domain string) string {
	return filepath.Join(contactDomainBase(domain), "mail")
}

func contactMaildirPath(domain, user string) string {
	return filepath.Join(contactDomainMailBase(domain), user, "Maildir")
}
