package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const initialOrderStatus = "pending_review"

var phonePattern = regexp.MustCompile(`^[0-9+().\-\s]{7,40}$`)

type orderFormData struct {
	OrderID      string
	Error        string
	FullName     string
	Email        string
	Phone        string
	Address      string
	Domain       string
	WantedEmail  string
	Notes        string
	Submitted    bool
	SubmittedAt  string
	NextStepText string
}

type starterOrder struct {
	ID                string
	FullName          string
	Email             string
	Phone             string
	Address           string
	Domain            string
	WantedEmail       string
	Notes             string
	Status            string
	PaymentURL        string
	PaymentReference  string
	QuotedAmountCents int
	CreatedAtUTC      time.Time
}

func orderHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		renderOrderForm(w, orderFormData{
			Domain:      strings.TrimSpace(r.URL.Query().Get("domain")),
			WantedEmail: defaultWantedEmail(r.URL.Query().Get("domain")),
		})
	case http.MethodPost:
		handleOrderPost(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleOrderPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	data := orderFormData{
		FullName:    r.FormValue("full_name"),
		Email:       r.FormValue("email"),
		Phone:       r.FormValue("phone"),
		Address:     r.FormValue("physical_address"),
		Domain:      r.FormValue("domain"),
		WantedEmail: r.FormValue("wanted_email"),
		Notes:       r.FormValue("notes"),
	}
	if strings.TrimSpace(r.FormValue("website")) != "" {
		log.Printf("order spam blocked by honeypot from %s", contactClientIP(r))
		renderOrderForm(w, orderSubmittedData(""))
		return
	}
	limitKey := "order|" + contactClientIP(r)
	if !contactLimiter.allow(limitKey, time.Now()) {
		http.Error(w, "Too many order attempts. Please try again later.", http.StatusTooManyRequests)
		return
	}

	order, err := starterOrderFromForm(data)
	if err != nil {
		data.Error = err.Error()
		renderOrderForm(w, data)
		return
	}
	if err := saveStarterOrder(r.Context(), order); err != nil {
		log.Printf("could not save order %s: %v", order.ID, err)
		http.Error(w, "Unable to save order", http.StatusInternalServerError)
		return
	}

	paymentURL, reference, amount, err := createImmediatePaystackCheckout(r.Context(), order)
	if err != nil {
		log.Printf("could not create Paystack checkout for %s: %v", order.ID, err)
		renderOrderForm(w, orderPaymentPendingData(order.ID, err))
		return
	}
	if err := saveOrderPayment(r.Context(), order.ID, paymentURL, reference, amount); err != nil {
		log.Printf("could not save Paystack checkout for %s: %v", order.ID, err)
		http.Error(w, "Unable to save payment link", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, paymentURL, http.StatusSeeOther)
}

func starterOrderFromForm(data orderFormData) (starterOrder, error) {
	fullName := strings.TrimSpace(data.FullName)
	email := strings.TrimSpace(data.Email)
	phone := strings.TrimSpace(data.Phone)
	address := strings.TrimSpace(data.Address)
	domain, err := normalizePublicDomain(data.Domain)
	if err != nil {
		return starterOrder{}, err
	}
	wantedEmail := strings.ToLower(strings.TrimSpace(data.WantedEmail))
	notes := strings.TrimSpace(data.Notes)

	switch {
	case fullName == "":
		return starterOrder{}, errors.New("full name required")
	case len(fullName) > 160:
		return starterOrder{}, errors.New("full name too long")
	case email == "":
		return starterOrder{}, errors.New("email address required")
	case len(email) > 254:
		return starterOrder{}, errors.New("email address too long")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return starterOrder{}, errors.New("valid email address required")
	}
	switch {
	case phone == "":
		return starterOrder{}, errors.New("phone number required")
	case !phonePattern.MatchString(phone):
		return starterOrder{}, errors.New("valid phone number required")
	case address == "":
		return starterOrder{}, errors.New("physical address required")
	case len(address) < 10:
		return starterOrder{}, errors.New("physical address too short")
	case len(address) > 1000:
		return starterOrder{}, errors.New("physical address too long")
	}
	if wantedEmail == "" {
		wantedEmail = defaultWantedEmail(domain)
	}
	if _, err := mail.ParseAddress(wantedEmail); err != nil {
		return starterOrder{}, errors.New("valid first email address required")
	}
	if !strings.HasSuffix(wantedEmail, "@"+domain) {
		return starterOrder{}, fmt.Errorf("first email address must use %s", domain)
	}
	if len(notes) > 2000 {
		return starterOrder{}, errors.New("notes too long")
	}

	id, err := newOrderID()
	if err != nil {
		return starterOrder{}, err
	}
	return starterOrder{
		ID:           id,
		FullName:     fullName,
		Email:        email,
		Phone:        phone,
		Address:      address,
		Domain:       domain,
		WantedEmail:  wantedEmail,
		Notes:        notes,
		Status:       initialOrderStatus,
		CreatedAtUTC: time.Now().UTC(),
	}, nil
}

func saveStarterOrder(ctx context.Context, order starterOrder) error {
	db, err := openOrderDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := ensureOrderSchema(ctx, db); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO customers (full_name, email, phone, physical_address, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(email) DO UPDATE SET
			full_name = excluded.full_name,
			phone = excluded.phone,
			physical_address = excluded.physical_address
	`, order.FullName, order.Email, order.Phone, order.Address, order.CreatedAtUTC.Format(time.RFC3339))
	if err != nil {
		return err
	}
	var customerID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM customers WHERE email = ?`, order.Email).Scan(&customerID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, notes, payment_url, payment_reference, quoted_amount_cents, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, order.ID, customerID, order.Domain, order.WantedEmail, order.Status, order.Notes, order.PaymentURL, order.PaymentReference, order.QuotedAmountCents, order.CreatedAtUTC.Format(time.RFC3339), order.CreatedAtUTC.Format(time.RFC3339))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO order_events (order_id, event_type, message, created_at)
		VALUES (?, ?, ?, ?)
	`, order.ID, "created", "Starter order submitted from public order form.", order.CreatedAtUTC.Format(time.RFC3339))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func saveOrderPayment(ctx context.Context, orderID, paymentURL, reference string, amountCents int) error {
	orderID = strings.TrimSpace(orderID)
	paymentURL = strings.TrimSpace(paymentURL)
	reference = strings.TrimSpace(reference)
	if orderID == "" {
		return fmt.Errorf("order id required")
	}
	if paymentURL == "" {
		return fmt.Errorf("payment URL required")
	}
	if reference == "" {
		return fmt.Errorf("payment reference required")
	}
	if amountCents <= 0 {
		return fmt.Errorf("quoted amount required")
	}
	db, err := openOrderDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := ensureOrderSchema(ctx, db); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET status = ?, payment_url = ?, payment_reference = ?, quoted_amount_cents = ?, updated_at = ?
		WHERE id = ?
	`, "pending_payment", paymentURL, reference, amountCents, now, orderID)
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
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO order_events (order_id, event_type, message, created_at)
		VALUES (?, ?, ?, ?)
	`, orderID, "pending_payment", "Paystack checkout created from public order flow.", now); err != nil {
		return err
	}
	return tx.Commit()
}

func openOrderDB() (*sql.DB, error) {
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

func ensureOrderSchema(ctx context.Context, db *sql.DB) error {
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
			registrar_reference TEXT NOT NULL DEFAULT '',
			certificate_path TEXT NOT NULL DEFAULT '',
			email_voucher_code TEXT NOT NULL DEFAULT '',
			website_url TEXT NOT NULL DEFAULT '',
			certificate_sent_at TEXT NOT NULL DEFAULT '',
			site_live_sent_at TEXT NOT NULL DEFAULT '',
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
		`CREATE TABLE IF NOT EXISTS order_portal_sessions (
			token TEXT PRIMARY KEY,
			order_id TEXT NOT NULL,
			email TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(order_id) REFERENCES orders(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_status_created ON orders(status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_domain ON orders(domain)`,
		`CREATE INDEX IF NOT EXISTS idx_order_portal_sessions_expires ON order_portal_sessions(expires_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for column, definition := range map[string]string{
		"payment_url":         "TEXT NOT NULL DEFAULT ''",
		"quoted_amount_cents": "INTEGER NOT NULL DEFAULT 0",
		"payment_currency":    "TEXT NOT NULL DEFAULT 'ZAR'",
		"registrar_reference": "TEXT NOT NULL DEFAULT ''",
		"certificate_path":    "TEXT NOT NULL DEFAULT ''",
		"email_voucher_code":  "TEXT NOT NULL DEFAULT ''",
		"website_url":         "TEXT NOT NULL DEFAULT ''",
		"certificate_sent_at": "TEXT NOT NULL DEFAULT ''",
		"site_live_sent_at":   "TEXT NOT NULL DEFAULT ''",
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

func newOrderID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "ord_" + time.Now().UTC().Format("20060102T150405Z") + "_" + hex.EncodeToString(raw[:]), nil
}

func defaultWantedEmail(domain string) string {
	domain, err := normalizePublicDomain(domain)
	if err != nil {
		return ""
	}
	return "info@" + domain
}

func orderSubmittedData(orderID string) orderFormData {
	return orderFormData{
		OrderID:      orderID,
		Submitted:    true,
		SubmittedAt:  time.Now().UTC().Format(time.RFC3339),
		NextStepText: "We will confirm the price and send the payment route before registration starts.",
	}
}

func orderPaymentPendingData(orderID string, err error) orderFormData {
	message := "Your order was saved, but the payment checkout could not be created automatically. Please use the order progress page while we resolve the payment setup."
	if err != nil {
		message = message + " Error: " + err.Error()
	}
	return orderFormData{
		OrderID:      orderID,
		Submitted:    true,
		SubmittedAt:  time.Now().UTC().Format(time.RFC3339),
		NextStepText: message,
	}
}

func renderOrderForm(w http.ResponseWriter, data orderFormData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := orderTemplate.Execute(w, data); err != nil {
		log.Printf("order template failed: %v", err)
	}
}

var orderTemplate = template.Must(template.New("order").Parse(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Register Domain</title>
    <style>
        * { box-sizing: border-box; }
        body { margin: 0; font-family: sans-serif; background: #f5f7f4; color: #20242a; }
        main { max-width: 760px; margin: 0 auto; padding: 42px 18px; }
        a { color: #1e6f5c; }
        .panel { background: #fff; border: 1px solid #dfe3dc; border-radius: 8px; padding: 24px; }
        h1 { margin: 0 0 10px; font-size: 30px; }
        p { line-height: 1.5; }
        form { display: grid; gap: 14px; margin-top: 20px; }
        label { display: grid; gap: 6px; font-weight: 700; color: #35413c; }
        input, textarea { width: 100%; min-height: 42px; padding: 10px 12px; border: 1px solid #cbd1ca; border-radius: 6px; font: inherit; }
        textarea { min-height: 108px; resize: vertical; }
        button { min-height: 44px; border: 0; border-radius: 7px; background: #24352f; color: white; font-weight: 800; cursor: pointer; font-size: 15px; }
        button:hover { background: #1e6f5c; }
        .fine { color: #68716b; font-size: 13px; margin: 0; }
        .error { color: #b42318; background: #fff3f1; border: 1px solid #d9a49b; border-radius: 8px; padding: 12px; }
        .success { color: #1e6f5c; background: #eef8f2; border: 1px solid #87bca7; border-radius: 8px; padding: 16px; }
    </style>
</head>
<body>
<main>
    <div class="panel">
        {{if .Submitted}}
            <h1>Order request received</h1>
            <div class="success">
                {{if .OrderID}}<p><strong>Order reference:</strong> {{.OrderID}}</p>{{end}}
                <p>{{.NextStepText}}</p>
            </div>
            {{if .OrderID}}<p><a href="/order/login?order_id={{.OrderID}}">View order progress</a></p>{{end}}
            <p><a href="/">Back to LocalBox Mail</a></p>
        {{else}}
            <h1>Register domain details</h1>
            <p>Enter the registrant details for this domain. Payment happens after these details are saved, and registrar registration starts only after Paystack confirms payment.</p>
            {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
            <form method="POST" action="/order">
                <input type="text" name="website" autocomplete="off" tabindex="-1" style="position:absolute;left:-10000px;top:auto;width:1px;height:1px;overflow:hidden;" aria-hidden="true">
                <label>Full name
                    <input name="full_name" autocomplete="name" value="{{.FullName}}" required>
                </label>
                <label>Email address
                    <input name="email" type="email" autocomplete="email" value="{{.Email}}" required>
                </label>
                <label>Phone number
                    <input name="phone" type="tel" autocomplete="tel" value="{{.Phone}}" required>
                </label>
                <label>Physical address
                    <textarea name="physical_address" autocomplete="street-address" required>{{.Address}}</textarea>
                </label>
                <label>Domain
                    <input name="domain" value="{{.Domain}}" required>
                </label>
                <label>First email address
                    <input name="wanted_email" value="{{.WantedEmail}}" placeholder="info@example.co.za" required>
                </label>
                <label>Notes
                    <textarea name="notes" placeholder="Business name, preferred payment timing, or setup notes.">{{.Notes}}</textarea>
                </label>
                <button type="submit">Save registration details and continue to payment</button>
                <p class="fine">These details are stored for order handling, domain registration, payment follow-up, and LocalBox setup.</p>
            </form>
        {{end}}
    </div>
</main>
</body>
</html>`))
