package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompleteOrderRegistrationIsIdempotent(t *testing.T) {
	oldDB := orderDBFile
	oldBase := localBoxBase
	oldSender := sendOrderNotificationEmail
	oldUsersFile := usersFile
	oldAdminsFile := adminsFile
	oldDomainsFile := domainsFile
	oldBrevoSetup := ensureBrevoDomainSetupForOrder
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	localBoxBase = t.TempDir()
	configDir := t.TempDir()
	usersFile = filepath.Join(configDir, "users.conf")
	adminsFile = filepath.Join(configDir, "admins.conf")
	domainsFile = filepath.Join(configDir, "domains.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		localBoxBase = oldBase
		sendOrderNotificationEmail = oldSender
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
		domainsFile = oldDomainsFile
		ensureBrevoDomainSetupForOrder = oldBrevoSetup
	})

	var sent []orderEmailMessage
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		sent = append(sent, message)
		return nil
	}
	ensureBrevoDomainSetupForOrder = func(ctx context.Context, id string) error {
		return appendOrderEvent(id, "brevo_setup_verified", "Brevo sender domain DNS setup completed for example.co.za.")
	}

	db, err := openOrdersDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO customers (id, full_name, email, phone, physical_address, created_at)
		VALUES (1, 'Peter Example', 'peter@example.com', '+27 82 123 4567', '1 Example Street', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, notes, payment_reference, created_at, updated_at)
		VALUES ('ord_test', 1, 'example.co.za', 'info@example.co.za', 'paid', '', 'pay_123', ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := completeOrderRegistration(context.Background(), "ord_test", "reg_123"); err != nil {
		t.Fatal(err)
	}

	if len(sent) != 3 {
		t.Fatalf("expected 3 emails (certificate + mailbox login + internal checklist), got %d", len(sent))
	}
	if sent[0].Attachment == nil || sent[0].Attachment.ContentType != "image/svg+xml" || !strings.Contains(string(sent[0].Attachment.Data), "<svg") || !strings.Contains(string(sent[0].Attachment.Data), "DOMAIN REGISTRATION CERTIFICATE") {
		t.Fatalf("expected certificate attachment in first email: %#v", sent[0].Attachment)
	}
	if !strings.Contains(sent[0].Body, "Email voucher/reference:") ||
		!strings.Contains(sent[0].Body, "Username: info@example.co.za") ||
		!strings.Contains(sent[0].Body, "Login URL: https://example.co.za/login") ||
		!strings.Contains(sent[0].Body, "Admin page: https://example.co.za/admin") {
		t.Fatalf("certificate email missing certificate details:\n%s", sent[0].Body)
	}
	if !strings.Contains(sent[1].Body, "Email address: info@example.co.za") ||
		!strings.Contains(sent[1].Body, "Temporary password: LB-") {
		t.Fatalf("first mailbox email missing login details:\n%s", sent[1].Body)
	}
	if sent[2].To != setupChecklistRecipient ||
		!strings.Contains(sent[2].Subject, "LocalBox setup checklist: example.co.za") ||
		!strings.Contains(sent[2].Body, "Cloudflare inbound email routing: synced for info@example.co.za") ||
		!strings.Contains(sent[2].Body, "Brevo sender domain:") ||
		!strings.Contains(sent[2].Body, "Brevo sender address: not completed: info@example.co.za") {
		t.Fatalf("setup checklist email missing expected details: %#v\n%s", sent[2], sent[2].Body)
	}
	if !userExists("info", "example.co.za") || !isDomainAdmin("info", "example.co.za") {
		t.Fatalf("registration step should auto-create mailbox/admin login")
	}
	firstPassword := firstLineWithPrefix(sent[1].Body, "Temporary password: ")
	if firstPassword == "" || !authenticate("info", "example.co.za", strings.TrimPrefix(firstPassword, "Temporary password: ")) {
		t.Fatalf("initial emailed password should authenticate")
	}

	if err := completeOrderRegistration(context.Background(), "ord_test", "reg_123"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 4 {
		t.Fatalf("second completion should send only the internal checklist, got %d emails", len(sent))
	}
	if !authenticate("info", "example.co.za", strings.TrimPrefix(firstPassword, "Temporary password: ")) {
		t.Fatalf("repeated order completion must not replace the emailed mailbox password")
	}
	mailboxLoginEmails := 0
	for _, message := range sent {
		if strings.Contains(message.Body, "Temporary password: ") {
			mailboxLoginEmails++
		}
	}
	if mailboxLoginEmails != 1 {
		t.Fatalf("expected exactly one mailbox login email, got %d", mailboxLoginEmails)
	}

	detail, err := loadOrderDetail("ord_test")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "site_live" || detail.RegistrarReference != "reg_123" || detail.EmailVoucherCode == "" || detail.WebsiteURL != "https://example.co.za/" {
		t.Fatalf("unexpected order detail: %#v", detail)
	}
	for _, want := range []string{
		"localbox_domain_added",
		"starter_website_created",
		"certificate_created",
		"certificate_sent",
		"mailbox_setup_started",
		"mailbox_created",
		"domain_admin_confirmed",
		"email_routing_synced",
		"mailbox_setup_verified",
		"brevo_setup_verified",
		"mailbox_login_sent",
		"setup_checklist_sent",
		"site_live",
	} {
		if !orderEventsContain(detail.Events, want) {
			t.Fatalf("expected order event %q in %#v", want, detail.Events)
		}
	}
	if detail.CertificateSentAt == "" || detail.SiteLiveSentAt == "" || detail.CertificatePath == "" {
		t.Fatalf("expected notification timestamps and certificate path: %#v", detail)
	}
	domains, err := loadDomains()
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(domains, "example.co.za") {
		t.Fatalf("expected domain to be added to LocalBox domains config, got %v", domains)
	}

	certificate, err := os.ReadFile(registrationCertificatePath("example.co.za"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(certificate), detail.EmailVoucherCode) || strings.Contains(string(certificate), "Voucher code:") {
		t.Fatalf("certificate should not print the email voucher:\n%s", string(certificate))
	}
	if strings.Contains(string(certificate), "Temporary password:") {
		t.Fatalf("certificate should not print temporary password:\n%s", string(certificate))
	}

	site, err := os.ReadFile(filepath.Join(domainSiteBase("example.co.za"), "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(site), "Peter Example") || !strings.Contains(string(site), "This domain is registered") {
		t.Fatalf("starter site missing expected content:\n%s", string(site))
	}
}

func TestCompleteOrderRegistrationAllowsParkedDomainWithoutMailbox(t *testing.T) {
	oldDB := orderDBFile
	oldBase := localBoxBase
	oldSender := sendOrderNotificationEmail
	oldUsersFile := usersFile
	oldAdminsFile := adminsFile
	oldDomainsFile := domainsFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	localBoxBase = t.TempDir()
	configDir := t.TempDir()
	usersFile = filepath.Join(configDir, "users.conf")
	adminsFile = filepath.Join(configDir, "admins.conf")
	domainsFile = filepath.Join(configDir, "domains.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		localBoxBase = oldBase
		sendOrderNotificationEmail = oldSender
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
		domainsFile = oldDomainsFile
	})

	var sent []orderEmailMessage
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		sent = append(sent, message)
		return nil
	}

	db, err := openOrdersDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO customers (id, full_name, email, phone, physical_address, created_at)
		VALUES (1, 'Parked Example', 'peter@example.com', '+27 82 123 4567', '1 Example Street', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, notes, payment_reference, created_at, updated_at)
		VALUES ('ord_parked', 1, 'parked.co.za', '', 'paid', 'Selected LocalBox option: Park it for free', 'pay_123', ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := completeOrderRegistration(context.Background(), "ord_parked", "reg_123"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || !strings.Contains(sent[0].Body, "The domain is parked. No mailbox was requested for this order.") || strings.Contains(sent[0].Body, "first mailbox requested") {
		t.Fatalf("expected parked-domain certificate email, got %#v", sent)
	}
	detail, err := loadOrderDetail("ord_parked")
	if err != nil {
		t.Fatal(err)
	}
	if detail.EmailVoucherCode != "" {
		t.Fatalf("parked domain should not get mailbox voucher, got %q", detail.EmailVoucherCode)
	}
	certificate, err := os.ReadFile(registrationCertificatePath("parked.co.za"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(certificate), "First email address reserved: none requested") || strings.Contains(string(certificate), "Email address registration voucher") {
		t.Fatalf("parked-domain certificate has wrong mailbox details:\n%s", string(certificate))
	}
}

func TestCreateFirstOrderMailboxCreatesAdminMailboxAndSendsLogin(t *testing.T) {
	oldDB := orderDBFile
	oldBase := localBoxBase
	oldSender := sendOrderNotificationEmail
	oldUsersFile := usersFile
	oldAdminsFile := adminsFile
	oldDomainsFile := domainsFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	localBoxBase = t.TempDir()
	configDir := t.TempDir()
	usersFile = filepath.Join(configDir, "users.conf")
	adminsFile = filepath.Join(configDir, "admins.conf")
	domainsFile = filepath.Join(configDir, "domains.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		localBoxBase = oldBase
		sendOrderNotificationEmail = oldSender
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
		domainsFile = oldDomainsFile
	})

	var sent []orderEmailMessage
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		sent = append(sent, message)
		return nil
	}

	db, err := openOrdersDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO customers (id, full_name, email, phone, physical_address, created_at)
		VALUES (1, 'Peter Example', 'peter@example.com', '+27 82 123 4567', '1 Example Street', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, notes, registrar_reference, certificate_sent_at, website_url, created_at, updated_at)
		VALUES ('ord_test', 1, 'example.co.za', 'info@example.co.za', 'registered', '', 'reg_123', ?, 'https://example.co.za/', ?, ?)
	`, now, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := createFirstOrderMailbox(context.Background(), "ord_test"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatalf("expected 1 email, got %d", len(sent))
	}
	if !strings.Contains(sent[0].Body, "Email address: info@example.co.za") ||
		!strings.Contains(sent[0].Body, "Username: info") ||
		!strings.Contains(sent[0].Body, "Temporary password: LB-") ||
		!strings.Contains(sent[0].Body, "Open your domain admin page: https://example.co.za/login?next=%2Fadmin&username=info%40example.co.za") ||
		!strings.Contains(sent[0].Body, "Outbound sending is not enabled automatically") {
		t.Fatalf("first mailbox email missing login details:\n%s", sent[0].Body)
	}
	password := strings.TrimPrefix(firstLineWithPrefix(sent[0].Body, "Temporary password: "), "Temporary password: ")
	if !authenticate("info", "example.co.za", password) {
		t.Fatalf("expected first mailbox login to authenticate")
	}
	if !isDomainAdmin("info", "example.co.za") {
		t.Fatalf("expected first mailbox to be domain admin")
	}
	detail, err := loadOrderDetail("ord_test")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mailbox_setup_started", "mailbox_created", "domain_admin_confirmed", "email_routing_synced", "mailbox_login_sent"} {
		if !orderEventsContain(detail.Events, want) {
			t.Fatalf("expected order event %q in %#v", want, detail.Events)
		}
	}
}

func orderEventsContain(events []OrderEvent, eventType string) bool {
	for _, event := range events {
		if event.EventType == eventType {
			return true
		}
	}
	return false
}

func TestMarkOrderDomainPropagatedSendsWebsiteLink(t *testing.T) {
	oldDB := orderDBFile
	oldBase := localBoxBase
	oldSender := sendOrderNotificationEmail
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	localBoxBase = t.TempDir()
	t.Cleanup(func() {
		orderDBFile = oldDB
		localBoxBase = oldBase
		sendOrderNotificationEmail = oldSender
	})

	var sent []orderEmailMessage
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		sent = append(sent, message)
		return nil
	}

	db, err := openOrdersDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO customers (id, full_name, email, phone, physical_address, created_at)
		VALUES (1, 'Peter Example', 'peter@example.com', '+27 82 123 4567', '1 Example Street', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, notes, registrar_reference, certificate_sent_at, website_url, created_at, updated_at)
		VALUES ('ord_test', 1, 'example.co.za', 'info@example.co.za', 'registered', '', 'reg_123', ?, 'https://example.co.za/', ?, ?)
	`, now, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := markOrderDomainPropagated(context.Background(), "ord_test"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatalf("expected 1 email, got %d", len(sent))
	}
	if sent[0].Attachment != nil {
		t.Fatalf("website link email should not include attachment")
	}
	if !strings.Contains(sent[0].Body, "https://example.co.za/") {
		t.Fatalf("site email missing website link:\n%s", sent[0].Body)
	}

	detail, err := loadOrderDetail("ord_test")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "site_live" || detail.SiteLiveSentAt == "" {
		t.Fatalf("expected site_live status and timestamp: %#v", detail)
	}
}

func firstLineWithPrefix(text, prefix string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func TestOrderRegistrationCertificateOmitsVoucher(t *testing.T) {
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	cert := string(orderRegistrationCertificate(OrderDetail{
		OrderSummary: OrderSummary{
			FullName:           "Peter Example",
			Domain:             "Example.CO.ZA",
			WantedEmail:        "Info@Example.CO.ZA",
			RegistrarReference: "reg_123",
		},
	}, "EMAIL-AAAA-BBBB", "https://example.co.za/", now))

	for _, want := range []string{
		"LOCALBOX DOMAIN REGISTRATION CERTIFICATE",
		"Domain: example.co.za",
		"Registrant: Peter Example",
		"Registrar reference: reg_123",
		"Mailbox login: https://example.co.za/login",
		"Domain admin page: https://example.co.za/admin",
		"Website: https://example.co.za/",
	} {
		if !strings.Contains(cert, want) {
			t.Fatalf("certificate did not include %q:\n%s", want, cert)
		}
	}
	if strings.Contains(cert, "EMAIL-AAAA-BBBB") || strings.Contains(cert, "Voucher code:") {
		t.Fatalf("certificate should omit the email voucher:\n%s", cert)
	}
}
