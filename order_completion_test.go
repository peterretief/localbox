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
	if !strings.Contains(sent[0].Body, "Email voucher:") || !strings.Contains(sent[0].Body, "info@example.co.za") {
		t.Fatalf("certificate email missing voucher or address:\n%s", sent[0].Body)
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
		"Website: https://example.co.za/",
	} {
		if !strings.Contains(cert, want) {
			t.Fatalf("certificate did not include %q:\n%s", want, cert)
		}
	}
}
