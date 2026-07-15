package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type orderEmailMessage struct {
	To         string
	Subject    string
	Body       string
	HTMLBody   string
	Attachment *OutgoingAttachment
}

var sendOrderNotificationEmail = sendOrderSMTPNotification

func completeOrderRegistration(ctx context.Context, id, registrarReference string) error {
	order, err := loadOrderDetail(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(registrarReference) != "" {
		order.RegistrarReference = strings.TrimSpace(registrarReference)
	}
	if strings.TrimSpace(order.RegistrarReference) == "" {
		return fmt.Errorf("registrar reference required")
	}

	domain := normalizeDomain(order.Domain)
	if !safeDomain(domain) {
		return fmt.Errorf("invalid domain")
	}
	wantedEmail, err := mail.ParseAddress(order.WantedEmail)
	if err != nil || !strings.HasSuffix(strings.ToLower(wantedEmail.Address), "@"+domain) {
		return fmt.Errorf("valid first email address required")
	}
	customerEmail, err := mail.ParseAddress(order.Email)
	if err != nil {
		return fmt.Errorf("valid customer email required")
	}

	now := time.Now().UTC()
	voucher := strings.TrimSpace(order.EmailVoucherCode)
	if voucher == "" {
		voucher, err = newEmailVoucherCode()
		if err != nil {
			return err
		}
	}
	websiteURL := order.WebsiteURL
	if strings.TrimSpace(websiteURL) == "" {
		websiteURL = "https://" + domain + "/"
	}
	if err := createStarterWebsite(domain, order.FullName, now); err != nil {
		return err
	}

	certificate := orderRegistrationCertificate(*order, voucher, websiteURL, now)
	certificatePath := registrationCertificatePath(domain)
	if err := os.MkdirAll(filepath.Dir(certificatePath), 0770); err != nil {
		return err
	}
	if err := os.WriteFile(certificatePath, certificate, 0640); err != nil {
		return err
	}

	if order.CertificateSentAt == "" {
		if err := sendOrderNotificationEmail(orderEmailMessage{
			To:      customerEmail.Address,
			Subject: "Domain registration certificate for " + domain,
			Body:    certificateEmailBody(domain, wantedEmail.Address, voucher),
			Attachment: &OutgoingAttachment{
				Filename:    domain + "-localbox-certificate.txt",
				ContentType: "text/plain; charset=utf-8",
				Data:        certificate,
			},
		}); err != nil {
			return err
		}
		order.CertificateSentAt = now.Format(time.RFC3339)
	}

	return updateOrderFields(order.ID, "registered", "Domain registration completed, certificate sent, and starter website prepared for DNS propagation.", map[string]any{
		"registrar_reference": order.RegistrarReference,
		"certificate_path":    certificatePath,
		"email_voucher_code":  voucher,
		"website_url":         websiteURL,
		"certificate_sent_at": order.CertificateSentAt,
	})
}

func markOrderDomainPropagated(ctx context.Context, id string) error {
	order, err := loadOrderDetail(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(order.CertificateSentAt) == "" {
		return fmt.Errorf("complete domain registration before sending the website link")
	}
	domain := normalizeDomain(order.Domain)
	if !safeDomain(domain) {
		return fmt.Errorf("invalid domain")
	}
	customerEmail, err := mail.ParseAddress(order.Email)
	if err != nil {
		return fmt.Errorf("valid customer email required")
	}
	websiteURL := strings.TrimSpace(order.WebsiteURL)
	if websiteURL == "" {
		websiteURL = "https://" + domain + "/"
	}
	if err := createStarterWebsite(domain, order.FullName, time.Now().UTC()); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(order.SiteLiveSentAt) == "" {
		if err := sendOrderNotificationEmail(orderEmailMessage{
			To:      customerEmail.Address,
			Subject: "Your new website is live: " + domain,
			Body:    siteLiveEmailBody(domain, websiteURL),
		}); err != nil {
			return err
		}
	}
	return updateOrderFields(order.ID, "site_live", "Domain propagation confirmed and website link sent.", map[string]any{
		"website_url":       websiteURL,
		"site_live_sent_at": now,
	})
}

func newEmailVoucherCode() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "EMAIL-" + strings.ToUpper(hex.EncodeToString(raw[:4])) + "-" + strings.ToUpper(hex.EncodeToString(raw[4:])), nil
}

func orderRegistrationCertificate(order OrderDetail, voucher, websiteURL string, now time.Time) []byte {
	var b bytes.Buffer
	fmt.Fprintln(&b, "LOCALBOX DOMAIN REGISTRATION CERTIFICATE")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Domain: "+normalizeDomain(order.Domain))
	fmt.Fprintln(&b, "Registrant: "+order.FullName)
	fmt.Fprintln(&b, "Registrar reference: "+order.RegistrarReference)
	fmt.Fprintln(&b, "First email address reserved: "+strings.ToLower(order.WantedEmail))
	fmt.Fprintln(&b, "Website: "+websiteURL)
	fmt.Fprintln(&b, "Issued: "+now.UTC().Format(time.RFC3339))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Email address registration voucher")
	fmt.Fprintln(&b, "Voucher code: "+voucher)
	fmt.Fprintln(&b, "Redeem this voucher with LocalBox to activate one email address on this domain.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "This confirms that LocalBox has completed the domain registration step for this order.")
	return b.Bytes()
}

func certificateEmailBody(domain, wantedEmail, voucher string) string {
	return fmt.Sprintf(`Your domain registration for %s is complete.

Your registration certificate is attached.

Email voucher: %s
Redeem it with LocalBox to register your first email address: %s

LocalBox Mail
`, domain, voucher, wantedEmail)
}

func siteLiveEmailBody(domain, websiteURL string) string {
	return fmt.Sprintf(`Your new domain website is available.

Domain: %s
Website: %s

DNS propagation can vary between networks. If the link does not open immediately, try again shortly.

LocalBox Mail
`, domain, websiteURL)
}

func createStarterWebsite(domain, fullName string, now time.Time) error {
	domain = normalizeDomain(domain)
	if !safeDomain(domain) {
		return fmt.Errorf("invalid domain")
	}
	owner := strings.TrimSpace(fullName)
	if owner == "" {
		owner = domain
	}
	htmlData := []byte(fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>%s</title>
    <style>
        body { margin: 0; font-family: Arial, sans-serif; color: #20242a; background: #f6f7f4; }
        main { max-width: 760px; margin: 0 auto; padding: 56px 20px; }
        h1 { margin: 0 0 12px; font-size: 34px; }
        p { font-size: 18px; line-height: 1.5; }
        .meta { margin-top: 32px; color: #68716b; font-size: 14px; }
    </style>
</head>
<body>
<main>
    <h1>%s</h1>
    <p>This domain is registered and hosted with LocalBox.</p>
    <p>The owner can update this starter website from LocalBox Mail.</p>
    <p class="meta">Published %s for %s.</p>
</main>
</body>
</html>
`, html.EscapeString(domain), html.EscapeString(owner), now.UTC().Format("2006-01-02"), html.EscapeString(domain)))
	if err := writeFileAtomic(filepath.Join(domainSiteBase(domain), "index.html"), htmlData, 0664); err != nil {
		return err
	}
	return saveSiteMode(domain, "html")
}

func sendOrderSMTPNotification(message orderEmailMessage) error {
	cfg, err := loadSMTPConfig(smtpConfigFile)
	if os.IsNotExist(err) || !cfg.Enabled {
		return fmt.Errorf("SMTP is not enabled")
	}
	if err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	toAddress, err := mail.ParseAddress(message.To)
	if err != nil {
		return fmt.Errorf("valid recipient email required")
	}
	fromAddress, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return fmt.Errorf("valid SMTP from address is required")
	}
	subject := cleanMailHeader(message.Subject)
	var raw []byte
	if strings.TrimSpace(message.HTMLBody) != "" && message.Attachment == nil {
		raw = buildOutgoingHTMLEmail(fromAddress.Address, toAddress.Address, subject, message.Body, message.HTMLBody, time.Now().UTC())
	} else {
		raw = buildOutgoingEmail(fromAddress.Address, toAddress.Address, subject, message.Body, time.Now().UTC(), message.Attachment)
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	return smtp.SendMail(addr, auth, cfg.From, []string{toAddress.Address}, raw)
}
