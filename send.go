package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const smtpConfigFile = "/etc/localbox/smtp.conf"
const maxComposeAttachmentBytes = 10 << 20

type SMTPConfig struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

type ComposePageData struct {
	To      string
	Subject string
	Body    string
	Error   string
	Success string
	User    string
	Domain  string
}

type OutgoingAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

type localRecipient struct {
	User   string
	Domain string
}

func sendMailMessage(senderUser, senderDomain, to, subject, body string, attachment *OutgoingAttachment) (string, error) {
	from := senderUser + "@" + senderDomain
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	subject = cleanMailHeader(subject)

	fromAddress, err := mail.ParseAddress(from)
	if err != nil {
		return "", fmt.Errorf("invalid from address")
	}
	toAddress, err := mail.ParseAddress(to)
	if err != nil {
		return "", fmt.Errorf("invalid to address")
	}
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("message body is required")
	}

	now := time.Now().UTC()
	raw := buildOutgoingEmail(fromAddress.Address, toAddress.Address, subject, body, now, attachment)
	delivery := ""
	if recipient, ok := findLocalRecipient(toAddress.Address); ok {
		if err := deliverLocalRaw(recipient, raw, now); err != nil {
			return "", err
		}
		delivery = "Message delivered locally."
	} else {
		cfg, err := loadSMTPConfig(smtpConfigFile)
		if os.IsNotExist(err) || !cfg.Enabled {
			return "", fmt.Errorf("recipient is not local and SMTP is not enabled")
		}
		if err != nil {
			return "", err
		}
		if err := cfg.validate(); err != nil {
			return "", err
		}

		addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := smtp.SendMail(addr, auth, cfg.From, []string{toAddress.Address}, raw); err != nil {
			return "", err
		}
		delivery = "Message sent by SMTP."
	}

	if err := saveSentRaw(senderDomain, senderUser, raw, now); err != nil {
		return "", err
	}
	if err := appendSendLog(senderDomain, senderUser, toAddress.Address, subject, delivery); err != nil {
		return "", err
	}
	return delivery + " Saved to Sent.", nil
}

func buildOutgoingEmail(from, to, subject, body string, now time.Time, attachment *OutgoingAttachment) []byte {
	if subject == "" {
		subject = "(no subject)"
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", (&mail.Address{Address: from}).String())
	fmt.Fprintf(&b, "To: %s\r\n", (&mail.Address{Address: to}).String())
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", now.Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%d.mail@localbox>\r\n", now.UnixNano())
	fmt.Fprint(&b, "MIME-Version: 1.0\r\n")
	if attachment != nil {
		writeMultipartEmail(&b, body, attachment)
		return b.Bytes()
	}
	fmt.Fprint(&b, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprint(&b, "Content-Transfer-Encoding: 8bit\r\n")
	fmt.Fprint(&b, "\r\n")
	fmt.Fprint(&b, body)
	if !strings.HasSuffix(body, "\n") {
		fmt.Fprint(&b, "\n")
	}
	return b.Bytes()
}

func writeMultipartEmail(b *bytes.Buffer, body string, attachment *OutgoingAttachment) {
	writer := multipart.NewWriter(b)
	fmt.Fprintf(b, "Content-Type: multipart/mixed; boundary=%q\r\n", writer.Boundary())
	fmt.Fprint(b, "\r\n")

	textHeader := make(textproto.MIMEHeader)
	textHeader.Set("Content-Type", "text/plain; charset=utf-8")
	textHeader.Set("Content-Transfer-Encoding", "8bit")
	textPart, _ := writer.CreatePart(textHeader)
	fmt.Fprint(textPart, body)
	if !strings.HasSuffix(body, "\n") {
		fmt.Fprint(textPart, "\n")
	}

	filename := filepath.Base(attachment.Filename)
	contentType := strings.TrimSpace(attachment.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	attachmentHeader := make(textproto.MIMEHeader)
	attachmentHeader.Set("Content-Type", fmt.Sprintf("%s; name=%q", contentType, filename))
	attachmentHeader.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	attachmentHeader.Set("Content-Transfer-Encoding", "base64")
	attachmentPart, _ := writer.CreatePart(attachmentHeader)
	encoded := base64.StdEncoding.EncodeToString(attachment.Data)
	for len(encoded) > 76 {
		fmt.Fprint(attachmentPart, encoded[:76], "\r\n")
		encoded = encoded[76:]
	}
	if encoded != "" {
		fmt.Fprint(attachmentPart, encoded, "\r\n")
	}
	_ = writer.Close()
}

func deliverLocalRaw(recipient localRecipient, raw []byte, now time.Time) error {
	maildir := maildirPath(recipient.Domain, recipient.User)
	filename, err := writeRawToMaildir(maildir, raw, now, "delivered", "new")
	if err != nil {
		return err
	}
	return saveRawAttachments(recipient.Domain, recipient.User, filename, raw)
}

func saveSentRaw(domain, user string, raw []byte, now time.Time) error {
	filename, err := writeRawToMaildir(sentMaildirPath(domain, user), raw, now, "sent", "cur")
	if err != nil {
		return err
	}
	return saveRawAttachments(domain, user, filename, raw)
}

func writeRawToMaildir(maildir string, raw []byte, now time.Time, label, dstDir string) (string, error) {
	for _, dir := range []string{"new", "cur", "tmp"} {
		if err := os.MkdirAll(filepath.Join(maildir, dir), 0770); err != nil {
			return "", err
		}
	}

	filename := fmt.Sprintf("%d.%s.%d.localbox", now.UnixNano(), label, os.Getpid())
	tmpPath := filepath.Join(maildir, "tmp", filename)
	dstPath := filepath.Join(maildir, dstDir, filename)
	if err := os.WriteFile(tmpPath, raw, 0660); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return "", err
	}
	return filename, nil
}

func saveRawAttachments(domain, user, filename string, raw []byte) error {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	return saveAttachments(domain, user, filename, msg.Body, msg.Header.Get("Content-Type"))
}

func findLocalRecipient(address string) (localRecipient, bool) {
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(address)), "@", 2)
	if len(parts) != 2 || !safeLabel.MatchString(parts[0]) || !safeDomain(parts[1]) {
		return localRecipient{}, false
	}

	users, err := loadUsers()
	if err == nil {
		for _, user := range users {
			if user.Username == parts[0] && user.Domain == parts[1] {
				return localRecipient{User: parts[0], Domain: parts[1]}, true
			}
		}
	}

	if _, err := os.Stat(maildirPath(parts[1], parts[0])); err == nil {
		return localRecipient{User: parts[0], Domain: parts[1]}, true
	}
	return localRecipient{}, false
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

func cleanMailHeader(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func appendSendLog(domain, user, to, subject, status string) error {
	if err := os.MkdirAll("/var/localbox", 0755); err != nil {
		return err
	}
	f, err := os.OpenFile("/var/localbox/send.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(
		f,
		"[%s] From: %s@%s To: %s Subject: %s Status: %s\n",
		time.Now().Format(time.RFC3339),
		user,
		domain,
		to,
		subject,
		status,
	)
	return err
}
