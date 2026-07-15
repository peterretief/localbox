package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompleteOrderRegistrationPublishesSiteAndSendsCertificateOnly(t *testing.T) {
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
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, notes, payment_reference, created_at, updated_at)
		VALUES ('ord_test', 1, 'example.co.za', 'info@example.co.za', 'paid', '', 'pay_123', ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := completeOrderRegistration(context.Background(), "ord_test", "reg_123"); err != nil {
		t.Fatal(err)
	}

	if len(sent) != 1 {
		t.Fatalf("expected 1 email, got %d", len(sent))
	}
	if sent[0].Attachment == nil || !strings.Contains(string(sent[0].Attachment.Data), "LOCALBOX DOMAIN REGISTRATION CERTIFICATE") {
		t.Fatalf("expected certificate attachment in first email: %#v", sent[0].Attachment)
	}
	if !strings.Contains(sent[0].Body, "Email voucher/reference:") ||
		!strings.Contains(sent[0].Body, "Email address: info@example.co.za") ||
		!strings.Contains(sent[0].Body, "Username: info") ||
		!strings.Contains(sent[0].Body, "Temporary password: LB-") ||
		!strings.Contains(sent[0].Body, "Email login: https://example.co.za/login") ||
		!strings.Contains(sent[0].Body, "Domain admin page: https://example.co.za/admin") {
		t.Fatalf("certificate email missing login details:\n%s", sent[0].Body)
	}
	if !authenticate("info", "example.co.za", strings.TrimPrefix(firstLineWithPrefix(sent[0].Body, "Temporary password: "), "Temporary password: ")) {
		t.Fatalf("expected first mailbox login to authenticate")
	}
	if !isDomainAdmin("info", "example.co.za") {
		t.Fatalf("expected first mailbox to be domain admin")
	}

	detail, err := loadOrderDetail("ord_test")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "registered" || detail.RegistrarReference != "reg_123" || detail.EmailVoucherCode == "" || detail.WebsiteURL != "https://example.co.za/" {
		t.Fatalf("unexpected order detail: %#v", detail)
	}
	if detail.CertificateSentAt == "" || detail.SiteLiveSentAt != "" || detail.CertificatePath == "" {
		t.Fatalf("expected notification timestamps and certificate path: %#v", detail)
	}

	certificate, err := os.ReadFile(registrationCertificatePath("example.co.za"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(certificate), detail.EmailVoucherCode) {
		t.Fatalf("certificate missing voucher:\n%s", string(certificate))
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

func TestOrderRegistrationCertificateIncludesVoucher(t *testing.T) {
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
		"Voucher code: EMAIL-AAAA-BBBB",
		"Mailbox login: https://example.co.za/login",
		"Domain admin page: https://example.co.za/admin",
		"Website: https://example.co.za/",
	} {
		if !strings.Contains(cert, want) {
			t.Fatalf("certificate did not include %q:\n%s", want, cert)
		}
	}
}
