package main

import (
	"path/filepath"
)

func domainBase(domain string) string {
	return filepath.Join(localBoxBase, "domains", domain)
}

func domainMailBase(domain string) string {
	return filepath.Join(domainBase(domain), "mail")
}

func mailboxBase(domain, user string) string {
	return filepath.Join(domainMailBase(domain), user)
}

func maildirPath(domain, user string) string {
	return filepath.Join(mailboxBase(domain, user), "Maildir")
}

func sentMaildirPath(domain, user string) string {
	return filepath.Join(maildirPath(domain, user), ".Sent")
}

func legacySentMaildirPath(domain, user string) string {
	return filepath.Join(mailboxBase(domain, user), "Sent")
}

func attachmentsBase(domain, user string) string {
	return filepath.Join(mailboxBase(domain, user), "attachments")
}

func messageAttachmentDir(domain, user, messageID string) string {
	return filepath.Join(attachmentsBase(domain, user), messageID)
}

func domainSiteBase(domain string) string {
	return filepath.Join(domainBase(domain), "site")
}

func currentSiteBase() string {
	return domainSiteBase("opamp.co.za")
}

func domainImagesPath(domain string) string {
	return filepath.Join(domainSiteBase(domain), "assets", "images")
}
