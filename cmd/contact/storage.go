package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func saveMessage(domain, name, email, subject, message string) error {
	if err := saveContactEmail(domain, name, email, subject, message); err != nil {
		return err
	}

	if err := os.MkdirAll("/var/localbox", 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(
		"/var/localbox/contact.log",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f,
		"\n[%s]\nDomain: %s\nName: %s\nEmail: %s\nSubject: %s\n\n%s\n",
		time.Now().Format(time.RFC3339),
		domain,
		name,
		email,
		subject,
		message,
	)

	return nil
}

func saveContactEmail(domain, name, email, subject, message string) error {
	if !safeDomain(domain) {
		return fmt.Errorf("invalid domain")
	}

	recipient := contactMailboxUser(domain)
	maildir := contactMaildirPath(domain, recipient)
	for _, dir := range []string{"new", "cur", "tmp"} {
		if err := os.MkdirAll(filepath.Join(maildir, dir), 0770); err != nil {
			return err
		}
	}

	now := time.Now().UTC()
	filename := fmt.Sprintf("%d.contact.%d.localbox", now.UnixNano(), os.Getpid())
	tmpPath := filepath.Join(maildir, "tmp", filename)
	newPath := filepath.Join(maildir, "new", filename)

	if err := os.WriteFile(tmpPath, contactEmail(domain, recipient, name, email, subject, message, now), 0660); err != nil {
		return err
	}

	return os.Rename(tmpPath, newPath)
}

func contactEmail(domain, recipient, name, email, subject, message string, now time.Time) []byte {
	cleanName := cleanHeader(name)
	cleanEmail := cleanHeader(email)
	cleanSubject := cleanHeader(subject)
	if cleanSubject == "" {
		cleanSubject = "Contact form message"
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s <%s>\r\n", cleanName, cleanEmail)
	fmt.Fprintf(&b, "To: %s@%s\r\n", recipient, domain)
	fmt.Fprintf(&b, "Subject: Contact form: %s\r\n", cleanSubject)
	fmt.Fprintf(&b, "Date: %s\r\n", now.Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%d.contact@localbox>\r\n", now.UnixNano())
	fmt.Fprint(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprint(&b, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprint(&b, "Content-Transfer-Encoding: 8bit\r\n")
	fmt.Fprint(&b, "\r\n")
	fmt.Fprintf(&b, "Name: %s\n", name)
	fmt.Fprintf(&b, "Email: %s\n", email)
	fmt.Fprintf(&b, "Domain: %s\n", domain)
	fmt.Fprintf(&b, "Subject: %s\n\n", subject)
	fmt.Fprint(&b, message)
	fmt.Fprint(&b, "\n")

	return b.Bytes()
}

func contactMailboxUser(domain string) string {
	users := contactDomainUsers(domain)
	if len(users) == 0 {
		return defaultContactMailboxUser
	}

	for _, path := range []string{contactAdminsFile, contactBoxAdminsFile} {
		if user := firstContactRoleUser(path, domain, users); user != "" {
			return user
		}
	}

	if users[defaultContactMailboxUser] {
		return defaultContactMailboxUser
	}

	for _, user := range contactDomainUserList(domain) {
		if users[user] {
			return user
		}
	}

	return defaultContactMailboxUser
}

func contactOwnerAddress(domain string) string {
	return contactMailboxUser(domain) + "@" + domain
}

func contactDomainUsers(domain string) map[string]bool {
	data, err := os.ReadFile(contactUsersFile)
	if err != nil {
		return nil
	}

	users := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || parts[1] != domain {
			continue
		}
		users[parts[0]] = true
	}
	return users
}

func contactDomainUserList(domain string) []string {
	data, err := os.ReadFile(contactUsersFile)
	if err != nil {
		return nil
	}

	var users []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && parts[1] == domain {
			users = append(users, parts[0])
		}
	}
	return users
}

func firstContactRoleUser(path, domain string, users map[string]bool) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || parts[1] != domain {
			continue
		}
		if users[parts[0]] {
			return parts[0]
		}
	}
	return ""
}

func cleanHeader(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func safeDomain(domain string) bool {
	if domain == "" || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	for _, part := range strings.Split(domain, ".") {
		if !safeLabel(part) {
			return false
		}
	}
	return strings.Contains(domain, ".")
}

func safeLabel(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
