package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestParseRandCents(t *testing.T) {
	tests := map[string]int{
		"R200":   20000,
		"200.50": 20050,
		"0.99":   99,
	}
	for input, want := range tests {
		got, err := parseRandCents(input)
		if err != nil {
			t.Fatalf("parseRandCents(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("parseRandCents(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestOrderAdminActionsUpdateOrderAndEvents(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

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
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, notes, created_at, updated_at)
		VALUES ('ord_test', 1, 'example.co.za', 'info@example.co.za', 'pending_review', '', ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := saveOrderQuote("ord_test", "R250.00", "https://pay.example/ord_test"); err != nil {
		t.Fatal(err)
	}
	if err := markOrderPaid("ord_test", "pay_123"); err != nil {
		t.Fatal(err)
	}

	detail, err := loadOrderDetail("ord_test")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "paid" || detail.QuotedAmountCents != 25000 || detail.PaymentURL != "https://pay.example/ord_test" || detail.PaymentReference != "pay_123" {
		t.Fatalf("unexpected order detail: %#v", detail)
	}
	if len(detail.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(detail.Events))
	}
}

func TestDNSPendingOrders(t *testing.T) {
	orders := []OrderSummary{
		{ID: "ord_registered", Domain: "ready.example", Status: "registered"},
		{ID: "ord_live", Domain: "live.example", Status: "site_live", SiteLiveSentAt: "2026-07-13T10:00:00Z"},
		{ID: "ord_paid", Domain: "paid.example", Status: "paid"},
		{ID: "ord_registered_sent", Domain: "sent.example", Status: "registered", SiteLiveSentAt: "2026-07-13T10:00:00Z"},
	}

	pending := dnsPendingOrders(orders)

	if len(pending) != 1 {
		t.Fatalf("expected 1 DNS-pending order, got %d", len(pending))
	}
	if pending[0].ID != "ord_registered" {
		t.Fatalf("unexpected DNS-pending order: %#v", pending[0])
	}
}

func TestFilterOrderSummariesHidesProcessedAndArchivedByDefault(t *testing.T) {
	orders := []OrderSummary{
		{ID: "active", FullName: "Active User", Domain: "active.example", Status: "paid"},
		{ID: "processed", FullName: "Processed User", Domain: "done.example", Status: "site_live"},
		{ID: "archived", FullName: "Archived User", Domain: "old.example", Status: "paid", ArchivedAt: "2026-07-14T10:00:00Z"},
	}

	filtered := filterOrderSummaries(orders, OrdersFilter{})
	if len(filtered) != 1 || filtered[0].ID != "active" {
		t.Fatalf("expected only active order by default, got %#v", filtered)
	}

	filtered = filterOrderSummaries(orders, OrdersFilter{ShowProcessed: true, ShowArchived: true, Query: "old.example"})
	if len(filtered) != 1 || filtered[0].ID != "archived" {
		t.Fatalf("expected search to find archived order when enabled, got %#v", filtered)
	}
}

func TestArchiveAndRestoreOrder(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

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
		VALUES (1, 'Test User', 'test@example.com', '+27 82 123 4567', '1 Example Street', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, notes, created_at, updated_at)
		VALUES ('ord_archive', 1, 'archive.example', 'info@archive.example', 'paid', '', ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := archiveOrder("ord_archive"); err != nil {
		t.Fatal(err)
	}
	detail, err := loadOrderDetail("ord_archive")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ArchivedAt == "" {
		t.Fatalf("expected archived timestamp: %#v", detail)
	}

	if err := restoreOrder("ord_archive"); err != nil {
		t.Fatal(err)
	}
	detail, err = loadOrderDetail("ord_archive")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ArchivedAt != "" {
		t.Fatalf("expected archived timestamp to be cleared: %#v", detail)
	}
}
