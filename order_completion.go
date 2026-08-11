package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"net/mail"
	"net/url"
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

const setupChecklistRecipient = "peter@diode.co.za"

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
	customerEmail, err := mail.ParseAddress(order.Email)
	if err != nil {
		return fmt.Errorf("valid customer email required")
	}
	wantedEmailAddress := strings.TrimSpace(order.WantedEmail)
	if wantedEmailAddress != "" {
		wantedEmail, err := mail.ParseAddress(wantedEmailAddress)
		if err != nil || !strings.HasSuffix(strings.ToLower(wantedEmail.Address), "@"+domain) {
			return fmt.Errorf("valid first email address required")
		}
		wantedEmailAddress = wantedEmail.Address
	}

	now := time.Now().UTC()
	voucher := strings.TrimSpace(order.EmailVoucherCode)
	if voucher == "" && wantedEmailAddress != "" {
		voucher, err = newEmailVoucherCode()
		if err != nil {
			return err
		}
	}
	websiteURL := order.WebsiteURL
	if strings.TrimSpace(websiteURL) == "" {
		websiteURL = "https://" + domain + "/"
	}
	if err := addDomain(domain); err != nil {
		return err
	}
	if err := appendOrderEvent(order.ID, "localbox_domain_added", "LocalBox domain entry confirmed for "+domain+"."); err != nil {
		return err
	}
	if err := createStarterWebsite(domain, order.FullName, now); err != nil {
		return err
	}
	if err := appendOrderEvent(order.ID, "starter_website_created", "Starter website files prepared for "+domain+"."); err != nil {
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
	if err := appendOrderEvent(order.ID, "certificate_created", "Domain registration certificate created."); err != nil {
		return err
	}

	if order.CertificateSentAt == "" {
		if err := sendOrderNotificationEmail(orderEmailMessage{
			To:      customerEmail.Address,
			Subject: "Domain registration certificate for " + domain,
			Body:    certificateEmailBody(domain, wantedEmailAddress, voucher),
			Attachment: &OutgoingAttachment{
				Filename:    domain + "-localbox-certificate.txt",
				ContentType: "text/plain; charset=utf-8",
				Data:        certificate,
			},
		}); err != nil {
			return err
		}
		order.CertificateSentAt = now.Format(time.RFC3339)
		if err := appendOrderEvent(order.ID, "certificate_sent", "Domain registration certificate emailed to customer."); err != nil {
			return err
		}
	}

	status := "registered"
	note := "Domain registration completed, certificate sent, and starter website prepared for DNS propagation."
	fields := map[string]any{
		"registrar_reference": order.RegistrarReference,
		"certificate_path":    certificatePath,
		"email_voucher_code":  voucher,
		"website_url":         websiteURL,
		"certificate_sent_at": order.CertificateSentAt,
	}
	if wantedEmailAddress != "" {
		if err := createFirstOrderMailbox(ctx, order.ID); err != nil && !strings.Contains(err.Error(), "first mailbox already exists") {
			return err
		}
		if err := verifyFirstOrderMailboxReady(order.ID); err != nil {
			return err
		}
		if err := ensureBrevoDomainSetupForOrder(ctx, order.ID); err != nil {
			_ = appendOrderEvent(order.ID, "brevo_setup_failed", "Brevo sender domain setup failed: "+err.Error())
			if isBrevoSetupPending(err) {
				fields["site_live_sent_at"] = ""
				if updateErr := updateOrderFields(order.ID, "provisioning", "Domain, website, mailbox, Cloudflare mail routing, and Brevo DNS records are prepared. Waiting for Brevo to confirm DNS authentication or sender activation.", fields); updateErr != nil {
					return updateErr
				}
			}
			return err
		}
		if err := sendPaidCustomerSetupChecklist(order.ID, websiteURL); err != nil {
			_ = appendOrderEvent(order.ID, "setup_checklist_failed", "Setup checklist email to "+setupChecklistRecipient+" failed: "+err.Error())
			return err
		}
		status = "site_live"
		note = "Domain registration completed, LocalBox hosting started, first mailbox created, and domain admin login prepared."
		fields["site_live_sent_at"] = now.Format(time.RFC3339)
	}

	return updateOrderFields(order.ID, status, note, fields)
}

func createFirstOrderMailbox(ctx context.Context, id string) error {
	order, err := loadOrderDetail(id)
	if err != nil {
		return err
	}
	domain := normalizeDomain(order.Domain)
	if !safeDomain(domain) {
		return fmt.Errorf("invalid domain")
	}
	wantedEmail, err := mail.ParseAddress(order.WantedEmail)
	if strings.TrimSpace(order.WantedEmail) == "" {
		return fmt.Errorf("no first mailbox was requested for this order")
	}
	if err != nil || !strings.HasSuffix(strings.ToLower(wantedEmail.Address), "@"+domain) {
		return fmt.Errorf("valid first email address required")
	}
	customerEmail, err := mail.ParseAddress(order.Email)
	if err != nil {
		return fmt.Errorf("valid customer email required")
	}
	mailboxUser := normalizeMailboxUser(strings.TrimSuffix(strings.ToLower(wantedEmail.Address), "@"+domain))
	if !safeMailboxUser(mailboxUser) {
		return fmt.Errorf("valid first email address required")
	}
	if err := appendOrderEvent(order.ID, "mailbox_setup_started", "First mailbox setup started for "+wantedEmail.Address+"."); err != nil {
		return err
	}
	if userExists(mailboxUser, domain) {
		if err := addDomainAdmin(mailboxUser, domain); err != nil {
			return err
		}
		if err := appendOrderEvent(order.ID, "domain_admin_confirmed", "Domain admin access confirmed for "+wantedEmail.Address+"."); err != nil {
			return err
		}
		if err := syncMailboxEmailRoute(mailboxUser, domain); err != nil {
			return err
		}
		if err := appendOrderEvent(order.ID, "email_routing_synced", "Cloudflare email routing synced for "+wantedEmail.Address+"."); err != nil {
			return err
		}
		if err := writeOrderWelcomeToMailbox(order, mailboxUser, wantedEmail.Address); err != nil {
			return err
		}
		// Registration retries must not replace a password that was already emailed.
		if orderHasEvent(order.Events, "mailbox_login_sent") {
			return nil
		}
		temporaryPassword, err := newTemporaryMailboxPassword()
		if err != nil {
			return err
		}
		if err := resetMailboxPassword(mailboxUser, domain, temporaryPassword); err != nil {
			return err
		}
		if err := sendOrderNotificationEmail(orderEmailMessage{
			To:      customerEmail.Address,
			Subject: "First LocalBox mailbox for " + domain,
			Body:    firstMailboxEmailBody(domain, wantedEmail.Address, mailboxUser, temporaryPassword),
		}); err != nil {
			return err
		}
		return appendOrderEvent(order.ID, "mailbox_login_sent", "First mailbox login emailed to customer.")
	}
	temporaryPassword, err := newTemporaryMailboxPassword()
	if err != nil {
		return err
	}
	if err := addMailboxWithRequiredRecovery(mailboxUser, domain, temporaryPassword, customerEmail.Address); err != nil {
		return err
	}
	if err := appendOrderEvent(order.ID, "mailbox_created", "First mailbox created for "+wantedEmail.Address+"."); err != nil {
		return err
	}
	if err := addDomainAdmin(mailboxUser, domain); err != nil {
		return err
	}
	if err := appendOrderEvent(order.ID, "domain_admin_confirmed", "Domain admin access confirmed for "+wantedEmail.Address+"."); err != nil {
		return err
	}
	if err := syncMailboxEmailRoute(mailboxUser, domain); err != nil {
		return err
	}
	if err := appendOrderEvent(order.ID, "email_routing_synced", "Cloudflare email routing synced for "+wantedEmail.Address+"."); err != nil {
		return err
	}
	if err := writeOrderWelcomeToMailbox(order, mailboxUser, wantedEmail.Address); err != nil {
		return err
	}
	if err := sendOrderNotificationEmail(orderEmailMessage{
		To:      customerEmail.Address,
		Subject: "First LocalBox mailbox for " + domain,
		Body:    firstMailboxEmailBody(domain, wantedEmail.Address, mailboxUser, temporaryPassword),
	}); err != nil {
		return err
	}
	return appendOrderEvent(order.ID, "mailbox_login_sent", "First mailbox login emailed to customer.")
}

func writeOrderWelcomeToMailbox(order *OrderDetail, mailboxUser, wantedEmail string) error {
	if orderHasEvent(order.Events, "mailbox_welcome_local_sent") {
		return nil
	}
	domain := normalizeDomain(order.Domain)
	body := fmt.Sprintf(`Welcome to LocalBox Mail.

Your first mailbox is ready.

Mailbox: %s
Login URL: https://%s/login
Domain admin: https://%s/admin

Your temporary password is sent in the separate mailbox-login email. Sign in and change it from the password page.

LocalBox Mail
`, wantedEmail, domain, domain)
	raw := buildOutgoingEmail("admin@localboxmail.co.za", wantedEmail, "Welcome to LocalBox Mail: "+domain, body, time.Now().UTC(), nil)
	if _, err := writeRawToMaildir(maildirPath(domain, mailboxUser), raw, time.Now().UTC(), "welcome", "new"); err != nil {
		return err
	}
	return appendOrderEvent(order.ID, "mailbox_welcome_local_sent", "Welcome message written to the first mailbox.")
}

func orderHasEvent(events []OrderEvent, eventType string) bool {
	for _, event := range events {
		if event.EventType == eventType {
			return true
		}
	}
	return false
}

func resendFirstOrderMailboxLogin(id string) error {
	order, err := loadOrderDetail(id)
	if err != nil {
		return err
	}
	domain := normalizeDomain(order.Domain)
	wantedEmail, err := mail.ParseAddress(strings.TrimSpace(order.WantedEmail))
	if err != nil || !safeDomain(domain) || !strings.HasSuffix(strings.ToLower(wantedEmail.Address), "@"+domain) {
		return fmt.Errorf("valid first email address required")
	}
	customerEmail, err := mail.ParseAddress(order.Email)
	if err != nil {
		return fmt.Errorf("valid customer email required")
	}
	mailboxUser := normalizeMailboxUser(strings.TrimSuffix(strings.ToLower(wantedEmail.Address), "@"+domain))
	if !userExists(mailboxUser, domain) || !isDomainAdmin(mailboxUser, domain) {
		return fmt.Errorf("first mailbox or domain admin is not ready for %s", wantedEmail.Address)
	}
	password, err := newTemporaryMailboxPassword()
	if err != nil {
		return err
	}
	if err := sendOrderNotificationEmail(orderEmailMessage{
		To:      customerEmail.Address,
		Subject: "Updated LocalBox mailbox login for " + domain,
		Body:    firstMailboxEmailBody(domain, wantedEmail.Address, mailboxUser, password),
	}); err != nil {
		return err
	}
	if err := resetMailboxPassword(mailboxUser, domain, password); err != nil {
		return err
	}
	return appendOrderEvent(order.ID, "mailbox_login_resent", "Updated first mailbox login emailed to customer.")
}

func resendOrderWelcomeEmail(id string) error {
	order, err := loadOrderDetail(id)
	if err != nil {
		return err
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
	voucher := strings.TrimSpace(order.EmailVoucherCode)
	certificatePath := registrationCertificatePath(domain)
	certificate, readErr := os.ReadFile(certificatePath)
	if readErr != nil {
		certificate = orderRegistrationCertificate(*order, voucher, websiteURL, time.Now().UTC())
	}
	if err := sendOrderNotificationEmail(orderEmailMessage{
		To:      customerEmail.Address,
		Subject: "Welcome to LocalBox: " + domain,
		Body:    certificateEmailBody(domain, order.WantedEmail, voucher),
		Attachment: &OutgoingAttachment{
			Filename:    domain + "-localbox-certificate.txt",
			ContentType: "text/plain; charset=utf-8",
			Data:        certificate,
		},
	}); err != nil {
		return err
	}
	return appendOrderEvent(order.ID, "welcome_email_resent", "Welcome email and registration certificate resent to the customer.")
}

func verifyFirstOrderMailboxReady(id string) error {
	order, err := loadOrderDetail(id)
	if err != nil {
		return err
	}
	domain := normalizeDomain(order.Domain)
	wantedEmail, err := mail.ParseAddress(strings.TrimSpace(order.WantedEmail))
	if err != nil || !safeDomain(domain) || !strings.HasSuffix(strings.ToLower(wantedEmail.Address), "@"+domain) {
		return fmt.Errorf("valid first email address required")
	}
	mailboxUser := normalizeMailboxUser(strings.TrimSuffix(strings.ToLower(wantedEmail.Address), "@"+domain))
	if !userExists(mailboxUser, domain) {
		return fmt.Errorf("first mailbox was not created for %s", wantedEmail.Address)
	}
	if !isDomainAdmin(mailboxUser, domain) {
		return fmt.Errorf("domain admin access was not created for %s", wantedEmail.Address)
	}
	if err := syncMailboxEmailRoute(mailboxUser, domain); err != nil {
		return fmt.Errorf("Cloudflare email routing was not created for %s: %w", wantedEmail.Address, err)
	}
	if err := appendOrderEvent(order.ID, "mailbox_setup_verified", "Mailbox, domain admin, and Cloudflare email routing verified for "+wantedEmail.Address+"."); err != nil {
		return err
	}
	return nil
}

func sendPaidCustomerSetupChecklist(id, websiteURL string) error {
	order, err := loadOrderDetail(id)
	if err != nil {
		return err
	}
	domain := normalizeDomain(order.Domain)
	wantedEmail := strings.TrimSpace(order.WantedEmail)
	mailboxStatus := "not requested"
	adminStatus := "not requested"
	cloudflareStatus := "not requested"
	if wantedEmail != "" {
		address, err := mail.ParseAddress(wantedEmail)
		if err != nil {
			mailboxStatus = "invalid requested mailbox: " + err.Error()
			adminStatus = "not checked"
			cloudflareStatus = "not checked"
		} else {
			user := normalizeMailboxUser(strings.TrimSuffix(strings.ToLower(address.Address), "@"+domain))
			if userExists(user, domain) {
				mailboxStatus = "created: " + address.Address
			} else {
				mailboxStatus = "MISSING: " + address.Address
			}
			if isDomainAdmin(user, domain) {
				adminStatus = "created for " + address.Address
			} else {
				adminStatus = "MISSING for " + address.Address
			}
			if err := syncMailboxEmailRoute(user, domain); err != nil {
				cloudflareStatus = "FAILED: " + err.Error()
			} else {
				cloudflareStatus = "synced for " + address.Address
			}
		}
	}
	brevoStatus := setupChecklistBrevoStatus(domain, wantedEmail)
	brevoSenderStatus := setupChecklistBrevoSenderStatus(order.Events, wantedEmail)
	body := strings.Join([]string{
		"LocalBox paid customer setup checklist",
		"",
		"Order: " + order.ID,
		"Customer: " + order.FullName + " <" + order.Email + ">",
		"Domain: " + domain,
		"Status before final closeout: " + order.Status,
		"",
		"Checklist:",
		"- Registrar reference: " + presentOrMissing(order.RegistrarReference),
		"- Cloudflare zone ID: " + presentOrMissing(order.CloudflareZoneID),
		"- Cloudflare nameservers: " + presentOrMissing(order.CloudflareNameservers),
		"- Website URL: " + presentOrMissing(firstNonEmpty(websiteURL, order.WebsiteURL)),
		"- Starter website files: " + setupChecklistSiteStatus(domain),
		"- First mailbox: " + mailboxStatus,
		"- Domain admin: " + adminStatus,
		"- Cloudflare inbound email routing: " + cloudflareStatus,
		"- Brevo sender domain: " + brevoStatus,
		"- Brevo sender address: " + brevoSenderStatus,
		"",
		"Admin order page: https://localboxmail.co.za/orders?id=" + url.QueryEscape(order.ID),
		"Customer order page: https://localboxmail.co.za/account/order?id=" + url.QueryEscape(order.ID),
	}, "\n")
	if err := sendOrderNotificationEmail(orderEmailMessage{
		To:      setupChecklistRecipient,
		Subject: "LocalBox setup checklist: " + domain,
		Body:    body,
	}); err != nil {
		return err
	}
	return appendOrderEvent(order.ID, "setup_checklist_sent", "Setup checklist emailed to "+setupChecklistRecipient+".")
}

func presentOrMissing(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "MISSING"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func setupChecklistSiteStatus(domain string) string {
	if _, err := os.Stat(filepath.Join(domainSiteBase(domain), "index.html")); err == nil {
		return "created"
	}
	return "MISSING"
}

func setupChecklistBrevoStatus(domain, wantedEmail string) string {
	cfg, err := loadSMTPConfig(smtpConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "not configured: smtp.conf missing"
		}
		return "not checked: " + err.Error()
	}
	if !cfg.Enabled {
		return "not configured: SMTP disabled"
	}
	host := strings.ToLower(strings.TrimSpace(cfg.Host))
	if strings.Contains(host, "brevo") {
		from := strings.TrimSpace(cfg.From)
		if from == "" {
			from = "unknown sender"
		}
		return "Brevo SMTP relay configured globally via " + host + " from " + from + "; domain sender check required for " + strings.TrimSpace(wantedEmail)
	}
	return "SMTP configured via " + cfg.Host + "; not Brevo"
}

func setupChecklistBrevoSenderStatus(events []OrderEvent, wantedEmail string) string {
	wantedEmail = strings.TrimSpace(wantedEmail)
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].EventType {
		case "brevo_sender_created":
			return "created: " + wantedEmail
		case "brevo_sender_confirmed":
			return "confirmed: " + wantedEmail
		case "brevo_sender_pending":
			return "pending activation: " + wantedEmail
		}
	}
	if wantedEmail == "" {
		return "not requested"
	}
	return "not completed: " + wantedEmail
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

func newTemporaryMailboxPassword() (string, error) {
	var raw [9]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "LB-" + strings.ToUpper(hex.EncodeToString(raw[:])), nil
}

func orderRegistrationCertificate(order OrderDetail, voucher, websiteURL string, now time.Time) []byte {
	var b bytes.Buffer
	fmt.Fprintln(&b, "LOCALBOX DOMAIN REGISTRATION CERTIFICATE")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Domain: "+normalizeDomain(order.Domain))
	fmt.Fprintln(&b, "Registrant: "+order.FullName)
	fmt.Fprintln(&b, "Registrar reference: "+order.RegistrarReference)
	if strings.TrimSpace(order.WantedEmail) != "" {
		fmt.Fprintln(&b, "First email address reserved: "+strings.ToLower(order.WantedEmail))
		fmt.Fprintln(&b, "Mailbox login: https://"+normalizeDomain(order.Domain)+"/login")
		fmt.Fprintln(&b, "Domain admin page: https://"+normalizeDomain(order.Domain)+"/admin")
	} else {
		fmt.Fprintln(&b, "First email address reserved: none requested")
	}
	fmt.Fprintln(&b, "Website: "+websiteURL)
	fmt.Fprintln(&b, "Issued: "+now.UTC().Format(time.RFC3339))
	fmt.Fprintln(&b)
	if strings.TrimSpace(order.WantedEmail) != "" {
		fmt.Fprintln(&b, "Email address registration voucher")
		fmt.Fprintln(&b, "Voucher code: "+voucher)
		fmt.Fprintln(&b, "The first mailbox is created by LocalBox. The temporary password is sent in a separate email and is not printed on this certificate.")
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b, "This confirms that LocalBox has completed the domain registration step for this order.")
	return b.Bytes()
}

func certificateEmailBody(domain, wantedEmail, voucher string) string {
	if strings.TrimSpace(wantedEmail) == "" {
		return fmt.Sprintf(`Your domain registration for %s is complete.

Your registration certificate is attached.

The domain is parked. No mailbox was requested for this order.

When you are ready, you can send LocalBox the nameservers for another DNS provider or ask LocalBox to add hosting.

LocalBox Mail
`, domain)
	}
	loginURL := "https://" + domain + "/login"
	adminURL := "https://" + domain + "/admin"
	return fmt.Sprintf(`Your domain registration for %s is complete.

Your registration certificate is attached.

MAILBOX LOGIN DETAILS
Login URL: %s
Username: %s
Temporary password: See the separate mailbox login email.

Email voucher/reference: %s

DOMAIN ADMIN
Admin page: %s

LocalBox Mail
`, domain, loginURL, wantedEmail, voucher, adminURL)
}

func firstMailboxEmailBody(domain, wantedEmail, mailboxUser, temporaryPassword string) string {
	loginURL := "https://" + domain + "/login"
	return fmt.Sprintf(`The first LocalBox mailbox for %s has been created.

MAILBOX LOGIN DETAILS
Login URL: %s
Username: %s
Temporary password: %s

Email address: %s
Domain: %s

DOMAIN ADMIN
Admin page: https://%s/admin

Please sign in and change the temporary password from the password page.

Outbound sending is not enabled automatically. LocalBox will confirm any Brevo or sending-domain setup separately.

LocalBox Mail
`, domain, loginURL, mailboxUser+"@"+domain, temporaryPassword, wantedEmail, domain, domain)
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
        %s
        body { color: #20242a; background: #f6f7f4; }
        main { padding-top: 56px; padding-bottom: 56px; }
        h1 { margin: 0 0 12px; font-size: clamp(2rem, 8vw, 2.125rem); }
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
`, html.EscapeString(domain), publicSiteBaseCSS("760px"), html.EscapeString(owner), now.UTC().Format("2006-01-02"), html.EscapeString(domain)))
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
	return sendSMTPWithTimeout(cfg, cfg.From, toAddress.Address, raw)
}
