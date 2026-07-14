package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

const orderSessionCookie = "localbox_order_session"

type orderLoginData struct {
	Error   string
	OrderID string
	Email   string
}

type orderProgressData struct {
	OrderID            string
	FullName           string
	Email              string
	Domain             string
	WantedEmail        string
	Status             string
	QuotedAmount       string
	PaymentURL         string
	PaymentReference   string
	RegistrarReference string
	CertificatePath    string
	CertificateReady   bool
	CreatedAt          string
	UpdatedAt          string
	Events             []orderPortalEvent
}

type orderPortalEvent struct {
	EventType string
	Message   string
	CreatedAt string
}

func orderLoginHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		orderID := r.URL.Query().Get("order_id")
		if orderID == "" {
			orderID = r.URL.Query().Get("reference")
		}
		renderOrderLogin(w, orderLoginData{OrderID: orderID})
	case http.MethodPost:
		handleOrderLoginPost(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleOrderLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	data := orderLoginData{
		OrderID: strings.TrimSpace(r.FormValue("order_id")),
		Email:   strings.TrimSpace(r.FormValue("email")),
	}
	if data.OrderID == "" || data.Email == "" {
		data.Error = "order reference and email address required"
		renderOrderLogin(w, data)
		return
	}
	if _, err := mail.ParseAddress(data.Email); err != nil {
		data.Error = "valid email address required"
		renderOrderLogin(w, data)
		return
	}
	if _, err := loadPortalOrder(r.Context(), data.OrderID, data.Email); err != nil {
		data.Error = "order not found for that email address"
		renderOrderLogin(w, data)
		return
	}
	token, expires, err := createOrderPortalSession(r.Context(), data.OrderID, data.Email)
	if err != nil {
		log.Printf("could not create order portal session for %s: %v", data.OrderID, err)
		http.Error(w, "Unable to start session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     orderSessionCookie,
		Value:    token,
		Path:     "/order",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/order/progress", http.StatusSeeOther)
}

func orderProgressHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session, err := getOrderPortalSession(r.Context(), r)
	if err != nil {
		http.Redirect(w, r, "/order/login", http.StatusFound)
		return
	}
	data, err := loadPortalOrder(r.Context(), session.OrderID, session.Email)
	if err != nil {
		log.Printf("could not load portal order %s: %v", session.OrderID, err)
		http.Redirect(w, r, "/order/login", http.StatusFound)
		return
	}
	renderOrderProgress(w, *data)
}

func orderLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(orderSessionCookie); err == nil {
		if db, dbErr := openOrderDB(); dbErr == nil {
			_, _ = db.ExecContext(r.Context(), `DELETE FROM order_portal_sessions WHERE token = ?`, c.Value)
			_ = db.Close()
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     orderSessionCookie,
		Value:    "",
		Path:     "/order",
		Expires:  time.Now().Add(-1 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/order/login", http.StatusSeeOther)
}

type orderPortalSession struct {
	OrderID string
	Email   string
}

func createOrderPortalSession(ctx context.Context, orderID, email string) (string, time.Time, error) {
	db, err := openOrderDB()
	if err != nil {
		return "", time.Time{}, err
	}
	defer db.Close()
	if err := ensureOrderSchema(ctx, db); err != nil {
		return "", time.Time{}, err
	}
	token, err := newOrderPortalToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().UTC().Add(24 * time.Hour)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO order_portal_sessions (token, order_id, email, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, token, orderID, strings.ToLower(strings.TrimSpace(email)), expires.Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

func getOrderPortalSession(ctx context.Context, r *http.Request) (orderPortalSession, error) {
	c, err := r.Cookie(orderSessionCookie)
	if err != nil {
		return orderPortalSession{}, err
	}
	db, err := openOrderDB()
	if err != nil {
		return orderPortalSession{}, err
	}
	defer db.Close()
	if err := ensureOrderSchema(ctx, db); err != nil {
		return orderPortalSession{}, err
	}
	var session orderPortalSession
	var expiresText string
	err = db.QueryRowContext(ctx, `
		SELECT order_id, email, expires_at
		FROM order_portal_sessions
		WHERE token = ?
	`, c.Value).Scan(&session.OrderID, &session.Email, &expiresText)
	if err != nil {
		return orderPortalSession{}, err
	}
	expires, err := time.Parse(time.RFC3339, expiresText)
	if err != nil || time.Now().UTC().After(expires) {
		_, _ = db.ExecContext(ctx, `DELETE FROM order_portal_sessions WHERE token = ?`, c.Value)
		return orderPortalSession{}, fmt.Errorf("session expired")
	}
	return session, nil
}

func loadPortalOrder(ctx context.Context, orderID, email string) (*orderProgressData, error) {
	db, err := openOrderDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := ensureOrderSchema(ctx, db); err != nil {
		return nil, err
	}

	var data orderProgressData
	var quotedAmountCents int
	err = db.QueryRowContext(ctx, `
		SELECT orders.id, customers.full_name, customers.email,
			orders.domain, orders.wanted_email, orders.status,
			orders.payment_url, orders.payment_reference, orders.quoted_amount_cents,
			orders.registrar_reference, orders.certificate_path,
			orders.created_at, orders.updated_at
		FROM orders
		JOIN customers ON customers.id = orders.customer_id
		WHERE orders.id = ? AND lower(customers.email) = lower(?)
	`, strings.TrimSpace(orderID), strings.TrimSpace(email)).Scan(
		&data.OrderID,
		&data.FullName,
		&data.Email,
		&data.Domain,
		&data.WantedEmail,
		&data.Status,
		&data.PaymentURL,
		&data.PaymentReference,
		&quotedAmountCents,
		&data.RegistrarReference,
		&data.CertificatePath,
		&data.CreatedAt,
		&data.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("order not found")
		}
		return nil, err
	}
	data.QuotedAmount = formatPortalRand(quotedAmountCents)
	data.CertificateReady = strings.TrimSpace(data.CertificatePath) != ""

	rows, err := db.QueryContext(ctx, `
		SELECT event_type, message, created_at
		FROM order_events
		WHERE order_id = ?
		ORDER BY created_at ASC, id ASC
	`, data.OrderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var event orderPortalEvent
		if err := rows.Scan(&event.EventType, &event.Message, &event.CreatedAt); err != nil {
			return nil, err
		}
		data.Events = append(data.Events, event)
	}
	return &data, rows.Err()
}

func newOrderPortalToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func formatPortalRand(cents int) string {
	if cents <= 0 {
		return ""
	}
	return fmt.Sprintf("R%d.%02d", cents/100, cents%100)
}

func renderOrderLogin(w http.ResponseWriter, data orderLoginData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := orderLoginTemplate.Execute(w, data); err != nil {
		log.Printf("order login template failed: %v", err)
	}
}

func renderOrderProgress(w http.ResponseWriter, data orderProgressData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := orderProgressTemplate.Execute(w, data); err != nil {
		log.Printf("order progress template failed: %v", err)
	}
}

var orderLoginTemplate = template.Must(template.New("order-login").Parse(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Order Progress</title>
    <style>
        * { box-sizing: border-box; }
        body { margin: 0; font-family: sans-serif; background: #f5f7f4; color: #20242a; }
        main { max-width: 560px; margin: 0 auto; padding: 42px 18px; }
        a { color: #1e6f5c; }
        .panel { background: #fff; border: 1px solid #dfe3dc; border-radius: 8px; padding: 24px; }
        h1 { margin: 0 0 10px; font-size: 30px; }
        form { display: grid; gap: 14px; margin-top: 20px; }
        label { display: grid; gap: 6px; font-weight: 700; color: #35413c; }
        input { width: 100%; min-height: 42px; padding: 10px 12px; border: 1px solid #cbd1ca; border-radius: 6px; font: inherit; }
        button { min-height: 44px; border: 0; border-radius: 7px; background: #24352f; color: white; font-weight: 800; cursor: pointer; font-size: 15px; }
        .error { color: #b42318; background: #fff3f1; border: 1px solid #d9a49b; border-radius: 8px; padding: 12px; }
    </style>
</head>
<body>
<main><div class="panel">
    <h1>View Order Progress</h1>
    <p>Use the order reference from your request confirmation and the email address used on the order.</p>
    {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
    <form method="POST" action="/order/login">
        <label>Order reference
            <input name="order_id" value="{{.OrderID}}" required>
        </label>
        <label>Email address
            <input name="email" type="email" value="{{.Email}}" required>
        </label>
        <button type="submit">View progress</button>
    </form>
    <p><a href="/">Back to LocalBox Mail</a></p>
</div></main>
</body>
</html>`))

var orderProgressTemplate = template.Must(template.New("order-progress").Parse(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Order Progress</title>
    <style>
        * { box-sizing: border-box; }
        body { margin: 0; font-family: sans-serif; background: #f5f7f4; color: #20242a; }
        main { max-width: 820px; margin: 0 auto; padding: 42px 18px; }
        a { color: #1e6f5c; }
        .panel { background: #fff; border: 1px solid #dfe3dc; border-radius: 8px; padding: 24px; margin-bottom: 16px; }
        h1 { margin: 0 0 10px; font-size: 30px; }
        .badge { display: inline-block; background: #e8f7fb; color: #007c96; border-radius: 999px; padding: 4px 10px; font-size: 0.82em; font-weight: bold; }
        .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
        .field { padding: 12px; background: #f8f8f8; border-radius: 6px; overflow-wrap: anywhere; }
        .field strong { display: block; margin-bottom: 5px; color: #555; font-size: 0.82em; }
        table { width: 100%; border-collapse: collapse; margin-top: 12px; }
        th, td { text-align: left; padding: 9px 6px; border-bottom: 1px solid #eee; font-size: 0.92em; vertical-align: top; }
        th { color: #777; font-weight: normal; }
        .muted { color: #68716b; font-size: 0.9em; }
        .pay-form { margin: 0; }
        .pay-button { min-height: 40px; border: 0; border-radius: 7px; background: #24352f; color: white; font-weight: 800; cursor: pointer; padding: 9px 14px; font-size: 14px; }
        @media (max-width: 700px) { .grid { grid-template-columns: 1fr; } }
    </style>
</head>
<body>
<main>
    <div class="panel">
        <h1>Order Progress</h1>
        <p><span class="badge">{{.Status}}</span> <strong>Order reference:</strong> <span class="muted">{{.OrderID}}</span></p>
        <div class="grid">
            <div class="field"><strong>Name</strong>{{.FullName}}</div>
            <div class="field"><strong>Email</strong>{{.Email}}</div>
            <div class="field"><strong>Domain</strong>{{.Domain}}</div>
            <div class="field"><strong>First email address</strong>{{.WantedEmail}}</div>
            <div class="field"><strong>Quoted amount</strong>{{if .QuotedAmount}}{{.QuotedAmount}}{{else}}<span class="muted">Not quoted yet</span>{{end}}</div>
            <div class="field"><strong>Payment</strong>{{if .PaymentURL}}<form class="pay-form" method="GET" action="{{.PaymentURL}}"><button class="pay-button" type="submit">Pay with Paystack</button></form>{{else}}<span class="muted">Payment link not ready yet</span>{{end}}</div>
            <div class="field"><strong>Payment reference</strong>{{if .PaymentReference}}{{.PaymentReference}}{{else}}<span class="muted">Not paid yet</span>{{end}}</div>
            <div class="field"><strong>Certificate</strong>{{if .CertificateReady}}Ready after mailbox login{{else}}<span class="muted">Not issued yet</span>{{end}}</div>
        </div>
    </div>
    <div class="panel">
        <h2>Timeline</h2>
        <table>
            <thead><tr><th>Time</th><th>Status</th><th>Message</th></tr></thead>
            <tbody>
            {{range .Events}}
            <tr><td class="muted">{{.CreatedAt}}</td><td>{{.EventType}}</td><td>{{.Message}}</td></tr>
            {{end}}
            </tbody>
        </table>
    </div>
    <p><a href="/order/logout">Sign out</a> · <a href="/">Back to LocalBox Mail</a></p>
</main>
</body>
</html>`))
