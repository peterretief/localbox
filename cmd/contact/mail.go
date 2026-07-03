package main

import (
	"bytes"
	"fmt"
	"log"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

const smtpConfigFile = "/etc/localbox/smtp.conf"

type SMTPConfig struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       string
}

func sendContactMessage(domain, name, email, subject, message string) error {
	cfg, err := loadSMTPConfig(smtpConfigFile)
	if os.IsNotExist(err) || !cfg.Enabled {
		return nil
	}
	if err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	recipient := contactOwnerAddress(domain)
	if localContactRecipient(recipient) {
		log.Printf("contact SMTP skipped for local recipient %s", recipient)
		return nil
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	return smtp.SendMail(addr, auth, cfg.From, []string{recipient}, contactSMTPEmail(cfg.From, recipient, domain, name, email, subject, message, time.Now()))
}

func loadSMTPConfig(path string) (SMTPConfig, error) {
	cfg := SMTPConfig{Port: 587}
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
			cfg.Enabled = strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "yes")
		case "host", "smtp_host":
			cfg.Host = value
		case "port", "smtp_port":
			port, err := strconv.Atoi(value)
			if err != nil || port <= 0 || port > 65535 {
				return cfg, fmt.Errorf("invalid SMTP port")
			}
			cfg.Port = port
		case "username", "user", "smtp_username":
			cfg.Username = value
		case "password", "api_key", "apikey", "key", "smtp_password":
			cfg.Password = value
		case "from", "sender":
			cfg.From = value
		case "to", "recipient":
			cfg.To = value
		}
	}

	return cfg, nil
}

func (cfg SMTPConfig) validate() error {
	if cfg.Host == "" {
		return fmt.Errorf("SMTP host is required")
	}
	if cfg.Username == "" {
		return fmt.Errorf("SMTP username is required")
	}
	if cfg.Password == "" {
		return fmt.Errorf("SMTP password is required")
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return fmt.Errorf("valid SMTP from address is required")
	}
	return nil
}

func localContactRecipient(address string) bool {
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return false
	}
	parts := strings.SplitN(strings.ToLower(parsed.Address), "@", 2)
	if len(parts) != 2 || !safeLabel(parts[0]) || !safeDomain(parts[1]) {
		return false
	}

	users, err := os.ReadFile(contactUsersFile)
	if err == nil {
		for _, line := range strings.Split(string(users), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.SplitN(line, ":", 3)
			if len(fields) == 3 && fields[0] == parts[0] && fields[1] == parts[1] {
				return true
			}
		}
	}

	if _, err := os.Stat(contactMaildirPath(parts[1], parts[0])); err == nil {
		return true
	}
	return false
}

func contactSMTPEmail(from, to, domain, name, email, subject, message string, now time.Time) []byte {
	cleanName := cleanHeader(name)
	cleanEmail := cleanHeader(email)
	cleanSubject := cleanHeader(subject)
	if cleanSubject == "" {
		cleanSubject = "Contact form message"
	}

	var b bytes.Buffer
	fromAddress := mail.Address{Address: from}
	toAddress := mail.Address{Address: to}
	fmt.Fprintf(&b, "From: %s\r\n", fromAddress.String())
	fmt.Fprintf(&b, "To: %s\r\n", toAddress.String())
	if cleanEmail != "" {
		replyTo := mail.Address{Name: cleanName, Address: cleanEmail}
		fmt.Fprintf(&b, "Reply-To: %s\r\n", replyTo.String())
	}
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
