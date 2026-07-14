package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestStarterOrderFromFormRequiresRegistrationDetails(t *testing.T) {
	order, err := starterOrderFromForm(orderFormData{
		FullName:    "Peter Example",
		Email:       "peter@example.com",
		Phone:       "+27 82 123 4567",
		Address:     "1 Example Street, Cape Town, 8001",
		Domain:      "example.co.za",
		WantedEmail: "info@example.co.za",
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != initialOrderStatus {
		t.Fatalf("expected status %q, got %q", initialOrderStatus, order.Status)
	}
	if order.Domain != "example.co.za" || order.WantedEmail != "info@example.co.za" {
		t.Fatalf("unexpected domain/email: %#v", order)
	}
}

func TestStarterOrderFromFormRejectsWrongEmailDomain(t *testing.T) {
	_, err := starterOrderFromForm(orderFormData{
		FullName:    "Peter Example",
		Email:       "peter@example.com",
		Phone:       "+27 82 123 4567",
		Address:     "1 Example Street, Cape Town, 8001",
		Domain:      "example.co.za",
		WantedEmail: "info@other.co.za",
	})
	if err == nil || !strings.Contains(err.Error(), "must use example.co.za") {
		t.Fatalf("expected wanted email domain error, got %v", err)
	}
}

func TestSaveStarterOrderWritesCustomerOrderAndEvent(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	order, err := starterOrderFromForm(orderFormData{
		FullName:    "Peter Example",
		Email:       "peter@example.com",
		Phone:       "+27 82 123 4567",
		Address:     "1 Example Street, Cape Town, 8001",
		Domain:      "example.co.za",
		WantedEmail: "info@example.co.za",
		Notes:       "Please send payment details.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := saveStarterOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", orderDBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var status, fullName, eventType string
	if err := db.QueryRow(`
		SELECT orders.status, customers.full_name, order_events.event_type
		FROM orders
		JOIN customers ON customers.id = orders.customer_id
		JOIN order_events ON order_events.order_id = orders.id
		WHERE orders.id = ?
	`, order.ID).Scan(&status, &fullName, &eventType); err != nil {
		t.Fatal(err)
	}
	if status != initialOrderStatus || fullName != "Peter Example" || eventType != "created" {
		t.Fatalf("unexpected saved values: status=%q fullName=%q eventType=%q", status, fullName, eventType)
	}
}
