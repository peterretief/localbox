package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAccountDomainsListsOrdersForSignedInPublicUser(t *testing.T) {
	oldDB := orderDBFile
	oldUsersFile := usersFile
	oldAdminsFile := adminsFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	adminsFile = filepath.Join(t.TempDir(), "admins.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
	})

	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
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
		VALUES
			(1, 'Buyer Example', 'buyer@example.net', '+27 82 123 4567', '1 Example Street', ?),
			(2, 'Other Example', 'other@example.net', '+27 82 123 4567', '2 Example Street', ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, payment_url, quoted_amount_cents, payment_currency, website_url, created_at, updated_at)
		VALUES
			('ord_one', 1, 'one.co.za', '', 'quoted', 'https://pay.example/one', 15000, 'ZAR', '', ?, ?),
			('ord_two', 1, 'two.org.za', 'info@two.org.za', 'site_live', '', 20000, 'ZAR', 'https://two.org.za', ?, ?),
			('ord_two_duplicate', 1, 'two.org.za', 'info@two.org.za', 'pending_payment', 'https://pay.example/two-duplicate', 20000, 'ZAR', '', ?, ?),
			('ord_other', 2, 'other.co.za', '', 'quoted', 'https://pay.example/other', 15000, 'ZAR', '', ?, ?)
	`, now, now, now, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := saveUsers([]User{{Username: "info", Domain: "two.org.za", Hash: "hash"}}); err != nil {
		t.Fatal(err)
	}
	if err := addDomainAdmin("info", "two.org.za"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/account/domains", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	accountDomainsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{"one.co.za", "two.org.za", "https://pay.example/one", "https://two.org.za", "https://two.org.za/login?domain=two.org.za&amp;username=info", "Domain admin", "None requested"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q; body=%s", want, body)
		}
	}
	for _, hidden := range []string{"other.co.za", "ord_two_duplicate", "https://pay.example/two-duplicate"} {
		if strings.Contains(body, hidden) {
			t.Fatalf("expected %q to be hidden; body=%s", hidden, body)
		}
	}
}

func TestAccountDomainsRequiresPublicLogin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/account/domains", nil)
	rec := httptest.NewRecorder()

	accountDomainsHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if location := rec.Header().Get("Location"); location != "/login?public=1&next=/account/domains" {
		t.Fatalf("redirect = %q", location)
	}
}

func TestAccountOrderShowsSignedInCustomersOrder(t *testing.T) {
	oldDB := orderDBFile
	oldUsersFile := usersFile
	oldAdminsFile := adminsFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	adminsFile = filepath.Join(t.TempDir(), "admins.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
	})

	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
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
		VALUES (1, 'Buyer Example', 'buyer@example.net', '+27 82 123 4567', '1 Example Street', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, payment_reference, quoted_amount_cents, created_at, updated_at)
		VALUES ('ord_one', 1, 'one.co.za', 'info@one.co.za', 'site_live', 'lb-paid', 15000, ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := saveUsers([]User{{Username: "info", Domain: "one.co.za", Hash: "hash"}}); err != nil {
		t.Fatal(err)
	}
	if err := addDomainAdmin("info", "one.co.za"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/account/order?id=ord_one", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	accountOrderHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{"one.co.za", "lb-paid", "Payment reference", "https://one.co.za/login?domain=one.co.za&amp;username=info", "Domain admin"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q; body=%s", want, body)
		}
	}
	if strings.Contains(body, "/order/login") {
		t.Fatalf("account order page should not link to order login; body=%s", body)
	}
}

func TestAccountOrderRejectsOtherCustomersOrder(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
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
		VALUES (1, 'Other Example', 'other@example.net', '+27 82 123 4567', '2 Example Street', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, created_at, updated_at)
		VALUES ('ord_other', 1, 'other.co.za', '', 'paid', ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	req := httptest.NewRequest(http.MethodGet, "/account/order?id=ord_other", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	accountOrderHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAccountOrderStartLocalBoxHostingCreatesSiteMailboxAndAdmin(t *testing.T) {
	oldDB := orderDBFile
	oldUsersFile := usersFile
	oldAdminsFile := adminsFile
	oldDomainsFile := domainsFile
	oldBase := localBoxBase
	oldSender := sendOrderNotificationEmail
	configDir := t.TempDir()
	orderDBFile = filepath.Join(configDir, "localbox.db")
	usersFile = filepath.Join(configDir, "users.conf")
	adminsFile = filepath.Join(configDir, "admins.conf")
	domainsFile = filepath.Join(configDir, "domains.conf")
	localBoxBase = filepath.Join(configDir, "data")
	t.Cleanup(func() {
		orderDBFile = oldDB
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
		domainsFile = oldDomainsFile
		localBoxBase = oldBase
		sendOrderNotificationEmail = oldSender
	})
	var sent []orderEmailMessage
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		sent = append(sent, message)
		return nil
	}

	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
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
		VALUES (1, 'Buyer Example', 'buyer@example.net', '+27 82 123 4567', '1 Example Street', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, payment_reference, quoted_amount_cents, created_at, updated_at)
		VALUES ('ord_one', 1, 'one.co.za', '', 'paid', 'lb-paid', 15000, ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	form := strings.NewReader("action=start_localbox_hosting")
	req := httptest.NewRequest(http.MethodPost, "/account/order?id=ord_one", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	accountOrderHandler(rec, req)

	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "success=localbox_hosting_started") {
		t.Fatalf("expected hosting redirect, code=%d location=%q body=%q", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if len(sent) != 1 || !strings.Contains(sent[0].Body, "Email address: info@one.co.za") {
		t.Fatalf("expected first mailbox email, got %#v", sent)
	}
	password := strings.TrimPrefix(firstLineWithPrefix(sent[0].Body, "Temporary password: "), "Temporary password: ")
	if !authenticate("info", "one.co.za", password) {
		t.Fatal("expected created mailbox to authenticate")
	}
	if !isDomainAdmin("info", "one.co.za") {
		t.Fatal("expected created mailbox to be domain admin")
	}
	if _, err := os.Stat(filepath.Join(domainSiteBase("one.co.za"), "index.html")); err != nil {
		t.Fatalf("expected starter website: %v", err)
	}
	detail, err := loadOrderDetail("ord_one")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "site_live" || detail.WantedEmail != "info@one.co.za" || detail.WebsiteURL != "https://one.co.za/" {
		t.Fatalf("unexpected hosted order detail: %#v", detail)
	}
}
