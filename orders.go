package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type OrdersView struct {
	Orders        []OrderSummary
	DNSPending    []OrderSummary
	Current       *OrderDetail
	Query         string
	ShowProcessed bool
	ShowArchived  bool
	FilterSuffix  string
	Error         string
	Success       string
}

type OrderSummary struct {
	ID                    string
	FullName              string
	Email                 string
	Phone                 string
	Domain                string
	WantedEmail           string
	Status                string
	PaymentURL            string
	PaymentReference      string
	QuotedAmountCents     int
	PaymentCurrency       string
	RegistrarReference    string
	CloudflareZoneID      string
	CloudflareNameservers string
	CertificatePath       string
	EmailVoucherCode      string
	WebsiteURL            string
	CertificateSentAt     string
	SiteLiveSentAt        string
	ArchivedAt            string
	CreatedAt             string
	UpdatedAt             string
}

type OrderDetail struct {
	OrderSummary
	PhysicalAddress string
	Notes           string
	Events          []OrderEvent
}

type OrderEvent struct {
	EventType string
	Message   string
	CreatedAt string
}

type OrdersFilter struct {
	Query         string
	ShowProcessed bool
	ShowArchived  bool
}

func ordersView(selectedID string) OrdersView {
	return ordersViewFiltered(selectedID, OrdersFilter{})
}

func ordersViewFiltered(selectedID string, filter OrdersFilter) OrdersView {
	orders, err := loadOrderSummaries()
	view := OrdersView{
		Query:         strings.TrimSpace(filter.Query),
		ShowProcessed: filter.ShowProcessed,
		ShowArchived:  filter.ShowArchived,
		FilterSuffix:  orderFilterSuffix(filter),
	}
	if err != nil {
		view.Error = err.Error()
		return view
	}
	view.DNSPending = dnsPendingOrders(orders)
	orders = filterOrderSummaries(orders, filter)
	view.Orders = orders
	if selectedID == "" && len(orders) > 0 {
		selectedID = orders[0].ID
	}
	if selectedID != "" {
		detail, err := loadOrderDetail(selectedID)
		if err != nil {
			view.Error = err.Error()
			return view
		}
		view.Current = detail
	}
	return view
}

func orderFilterSuffix(filter OrdersFilter) string {
	values := url.Values{}
	if query := strings.TrimSpace(filter.Query); query != "" {
		values.Set("q", query)
	}
	if filter.ShowProcessed {
		values.Set("show_processed", "1")
	}
	if filter.ShowArchived {
		values.Set("show_archived", "1")
	}
	if encoded := values.Encode(); encoded != "" {
		return "&" + encoded
	}
	return ""
}

func dnsPendingOrders(orders []OrderSummary) []OrderSummary {
	var pending []OrderSummary
	for _, order := range orders {
		if order.ArchivedAt == "" && order.Status == "registered" && strings.TrimSpace(order.SiteLiveSentAt) == "" {
			pending = append(pending, order)
		}
	}
	return pending
}

func filterOrderSummaries(orders []OrderSummary, filter OrdersFilter) []OrderSummary {
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	var filtered []OrderSummary
	for _, order := range orders {
		if order.ArchivedAt != "" && !filter.ShowArchived {
			continue
		}
		if !filter.ShowProcessed && orderProcessed(order) {
			continue
		}
		if query != "" && !orderMatchesQuery(order, query) {
			continue
		}
		filtered = append(filtered, order)
	}
	return filtered
}

func orderProcessed(order OrderSummary) bool {
	switch order.Status {
	case "site_live", "cancelled", "payment_failed":
		return true
	default:
		return false
	}
}

func orderMatchesQuery(order OrderSummary, query string) bool {
	searchText := strings.ToLower(strings.Join([]string{
		order.ID,
		order.FullName,
		order.Email,
		order.Phone,
		order.Domain,
		order.WantedEmail,
		order.Status,
		order.PaymentReference,
		order.RegistrarReference,
		order.WebsiteURL,
	}, " "))
	return strings.Contains(searchText, query)
}

func loadOrderSummaries() ([]OrderSummary, error) {
	db, err := openOrdersDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT orders.id, customers.full_name, customers.email, customers.phone,
			orders.domain, orders.wanted_email, orders.status,
			orders.payment_url, orders.payment_reference, orders.quoted_amount_cents, orders.payment_currency,
			orders.registrar_reference, orders.cloudflare_zone_id, orders.cloudflare_nameservers, orders.certificate_path,
			orders.email_voucher_code, orders.website_url, orders.certificate_sent_at, orders.site_live_sent_at, orders.archived_at,
			orders.created_at, orders.updated_at
		FROM orders
		JOIN customers ON customers.id = orders.customer_id
		ORDER BY orders.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []OrderSummary
	for rows.Next() {
		var order OrderSummary
		if err := rows.Scan(
			&order.ID,
			&order.FullName,
			&order.Email,
			&order.Phone,
			&order.Domain,
			&order.WantedEmail,
			&order.Status,
			&order.PaymentURL,
			&order.PaymentReference,
			&order.QuotedAmountCents,
			&order.PaymentCurrency,
			&order.RegistrarReference,
			&order.CloudflareZoneID,
			&order.CloudflareNameservers,
			&order.CertificatePath,
			&order.EmailVoucherCode,
			&order.WebsiteURL,
			&order.CertificateSentAt,
			&order.SiteLiveSentAt,
			&order.ArchivedAt,
			&order.CreatedAt,
			&order.UpdatedAt,
		); err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

func loadOrderDetail(id string) (*OrderDetail, error) {
	db, err := openOrdersDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		return nil, err
	}

	var detail OrderDetail
	err = db.QueryRow(`
		SELECT orders.id, customers.full_name, customers.email, customers.phone, customers.physical_address,
			orders.domain, orders.wanted_email, orders.status,
			orders.payment_url, orders.payment_reference, orders.quoted_amount_cents, orders.payment_currency,
			orders.registrar_reference, orders.cloudflare_zone_id, orders.cloudflare_nameservers, orders.certificate_path,
			orders.email_voucher_code, orders.website_url, orders.certificate_sent_at, orders.site_live_sent_at, orders.archived_at,
			orders.notes, orders.created_at, orders.updated_at
		FROM orders
		JOIN customers ON customers.id = orders.customer_id
		WHERE orders.id = ?
	`, id).Scan(
		&detail.ID,
		&detail.FullName,
		&detail.Email,
		&detail.Phone,
		&detail.PhysicalAddress,
		&detail.Domain,
		&detail.WantedEmail,
		&detail.Status,
		&detail.PaymentURL,
		&detail.PaymentReference,
		&detail.QuotedAmountCents,
		&detail.PaymentCurrency,
		&detail.RegistrarReference,
		&detail.CloudflareZoneID,
		&detail.CloudflareNameservers,
		&detail.CertificatePath,
		&detail.EmailVoucherCode,
		&detail.WebsiteURL,
		&detail.CertificateSentAt,
		&detail.SiteLiveSentAt,
		&detail.ArchivedAt,
		&detail.Notes,
		&detail.CreatedAt,
		&detail.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("order not found")
		}
		return nil, err
	}

	rows, err := db.Query(`
		SELECT event_type, message, created_at
		FROM order_events
		WHERE order_id = ?
		ORDER BY created_at DESC, id DESC
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var event OrderEvent
		if err := rows.Scan(&event.EventType, &event.Message, &event.CreatedAt); err != nil {
			return nil, err
		}
		detail.Events = append(detail.Events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &detail, nil
}

func saveOrderQuote(id, quotedAmount, paymentURL string) error {
	cents, err := parseRandCents(quotedAmount)
	if err != nil {
		return err
	}
	paymentURL = strings.TrimSpace(paymentURL)
	if paymentURL == "" {
		return fmt.Errorf("payment link required")
	}
	return updateOrderFields(id, "quoted", "Quote and payment link saved.", map[string]any{
		"quoted_amount_cents": cents,
		"payment_url":         paymentURL,
	})
}

func markOrderPaid(id, paymentReference string) error {
	paymentReference = strings.TrimSpace(paymentReference)
	if paymentReference == "" {
		return fmt.Errorf("payment reference required")
	}
	return updateOrderFields(id, "paid", "Payment marked as paid.", map[string]any{
		"payment_reference": paymentReference,
	})
}

func markOrderReadyForRegistration(id, registrarReference string) error {
	return updateOrderFields(id, "ready_for_registration", "Order marked ready for registrar registration.", map[string]any{
		"registrar_reference": strings.TrimSpace(registrarReference),
	})
}

func archiveOrder(id string) error {
	return setOrderArchived(id, time.Now().UTC().Format(time.RFC3339), "Order archived from admin.")
}

func restoreOrder(id string) error {
	return setOrderArchived(id, "", "Order restored from archive.")
}

func setOrderArchived(id, archivedAt, eventMessage string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("order id required")
	}
	db, err := openOrdersDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.Exec(`UPDATE orders SET archived_at = ?, updated_at = ? WHERE id = ?`, archivedAt, now, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("order not found")
	}
	eventType := "archived"
	if archivedAt == "" {
		eventType = "restored"
	}
	if _, err := tx.Exec(`
		INSERT INTO order_events (order_id, event_type, message, created_at)
		VALUES (?, ?, ?, ?)
	`, id, eventType, eventMessage, now); err != nil {
		return err
	}
	return tx.Commit()
}

func updateOrderFields(id, status, eventMessage string, fields map[string]any) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("order id required")
	}
	db, err := openOrdersDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var assignments []string
	var args []any
	for column, value := range fields {
		assignments = append(assignments, column+" = ?")
		args = append(args, value)
	}
	assignments = append(assignments, "status = ?", "updated_at = ?")
	args = append(args, status, time.Now().UTC().Format(time.RFC3339), id)
	result, err := tx.Exec("UPDATE orders SET "+strings.Join(assignments, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("order not found")
	}
	if _, err := tx.Exec(`
		INSERT INTO order_events (order_id, event_type, message, created_at)
		VALUES (?, ?, ?, ?)
	`, id, status, eventMessage, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

func openOrdersDB() (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(orderDBFile), 0770); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", orderDBFile)
	if err != nil {
		return nil, fmt.Errorf("open orders database %q: %w", orderDBFile, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("open orders database %q: %w", orderDBFile, err)
	}
	return db, nil
}

func ensureOrdersAdminSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS customers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			full_name TEXT NOT NULL,
			email TEXT NOT NULL UNIQUE,
			phone TEXT NOT NULL,
			physical_address TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS orders (
			id TEXT PRIMARY KEY,
			customer_id INTEGER NOT NULL,
			domain TEXT NOT NULL,
			wanted_email TEXT NOT NULL,
			status TEXT NOT NULL,
			notes TEXT NOT NULL DEFAULT '',
			payment_url TEXT NOT NULL DEFAULT '',
			payment_reference TEXT NOT NULL DEFAULT '',
			quoted_amount_cents INTEGER NOT NULL DEFAULT 0,
			payment_currency TEXT NOT NULL DEFAULT 'ZAR',
			registrar_reference TEXT NOT NULL DEFAULT '',
			cloudflare_zone_id TEXT NOT NULL DEFAULT '',
			cloudflare_nameservers TEXT NOT NULL DEFAULT '',
			certificate_path TEXT NOT NULL DEFAULT '',
			email_voucher_code TEXT NOT NULL DEFAULT '',
			website_url TEXT NOT NULL DEFAULT '',
			certificate_sent_at TEXT NOT NULL DEFAULT '',
			site_live_sent_at TEXT NOT NULL DEFAULT '',
			archived_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(customer_id) REFERENCES customers(id)
		)`,
		`CREATE TABLE IF NOT EXISTS order_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			message TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(order_id) REFERENCES orders(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_status_created ON orders(status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_domain ON orders(domain)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for column, definition := range map[string]string{
		"payment_url":            "TEXT NOT NULL DEFAULT ''",
		"quoted_amount_cents":    "INTEGER NOT NULL DEFAULT 0",
		"payment_currency":       "TEXT NOT NULL DEFAULT 'ZAR'",
		"registrar_reference":    "TEXT NOT NULL DEFAULT ''",
		"cloudflare_zone_id":     "TEXT NOT NULL DEFAULT ''",
		"cloudflare_nameservers": "TEXT NOT NULL DEFAULT ''",
		"certificate_path":       "TEXT NOT NULL DEFAULT ''",
		"email_voucher_code":     "TEXT NOT NULL DEFAULT ''",
		"website_url":            "TEXT NOT NULL DEFAULT ''",
		"certificate_sent_at":    "TEXT NOT NULL DEFAULT ''",
		"site_live_sent_at":      "TEXT NOT NULL DEFAULT ''",
		"archived_at":            "TEXT NOT NULL DEFAULT ''",
	} {
		if err := ensureOrderColumn(ctx, db, column, definition); err != nil {
			return err
		}
	}
	return nil
}

func ensureOrderColumn(ctx context.Context, db *sql.DB, column, definition string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(orders)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "ALTER TABLE orders ADD COLUMN "+column+" "+definition)
	return err
}

func parseRandCents(value string) (int, error) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(value), "R"))
	if value == "" {
		return 0, fmt.Errorf("quoted amount required")
	}
	parts := strings.SplitN(value, ".", 3)
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid quoted amount")
	}
	rands, err := strconv.Atoi(strings.ReplaceAll(parts[0], " ", ""))
	if err != nil || rands < 0 {
		return 0, fmt.Errorf("invalid quoted amount")
	}
	cents := 0
	if len(parts) == 2 {
		centText := parts[1]
		if len(centText) == 1 {
			centText += "0"
		}
		if len(centText) > 2 {
			return 0, fmt.Errorf("invalid quoted amount")
		}
		cents, err = strconv.Atoi(centText)
		if err != nil || cents < 0 {
			return 0, fmt.Errorf("invalid quoted amount")
		}
	}
	return rands*100 + cents, nil
}

func formatRand(cents int) string {
	if cents <= 0 {
		return ""
	}
	return fmt.Sprintf("R%d.%02d", cents/100, cents%100)
}
