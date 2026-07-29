package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyLocalDomainReservationsMarksActiveOrderUnavailable(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	db, err := sql.Open("sqlite", orderDBFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureOrderSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO customers (id, full_name, email, phone, physical_address, created_at)
		VALUES (1, 'Buyer Example', 'buyer@example.net', '+27 82 123 4567', '1 Example Street', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, payment_url, quoted_amount_cents, created_at, updated_at)
		VALUES ('ord_paid', 1, 'anvil.org.za', '', 'paid', 'https://checkout.paystack.test/pay', 15000, ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	results := applyLocalDomainReservations(context.Background(), []DomainCheckResult{
		{Domain: "anvil.org.za", Available: true, PaymentPrice: 15000, PaymentCurrency: "ZAR", Message: "Domain Available"},
		{Domain: "fresh.org.za", Available: true, PaymentPrice: 15000, PaymentCurrency: "ZAR", Message: "Domain Available"},
	})

	if results[0].Available || results[0].PaymentPrice != 0 || results[0].Reason != "localbox_order" {
		t.Fatalf("expected anvil.org.za to be locally reserved, got %#v", results[0])
	}
	if !results[1].Available || results[1].PaymentPrice != 15000 {
		t.Fatalf("expected fresh.org.za to remain available, got %#v", results[1])
	}
}
