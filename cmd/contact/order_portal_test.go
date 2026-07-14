package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPortalOrderRequiresMatchingEmail(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	order := testStarterOrder(t)
	if err := saveStarterOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}

	if _, err := loadPortalOrder(context.Background(), order.ID, "wrong@example.com"); err == nil {
		t.Fatal("expected wrong email to be rejected")
	}
}

func TestOrderPortalLoginAndProgress(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	order := testStarterOrder(t)
	if err := saveStarterOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}

	form := strings.NewReader("order_id=" + order.ID + "&email=peter%40example.com")
	loginReq := httptest.NewRequest(http.MethodPost, "/order/login", form)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	orderLoginHandler(loginRec, loginReq)
	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("expected login redirect, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}

	progressReq := httptest.NewRequest(http.MethodGet, "/order/progress", nil)
	progressReq.AddCookie(cookies[0])
	progressRec := httptest.NewRecorder()
	orderProgressHandler(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("expected progress page, got %d: %s", progressRec.Code, progressRec.Body.String())
	}
	if !strings.Contains(progressRec.Body.String(), "example.co.za") {
		t.Fatalf("progress page missing domain:\n%s", progressRec.Body.String())
	}
	if !strings.Contains(progressRec.Body.String(), "Order reference:") || !strings.Contains(progressRec.Body.String(), order.ID) {
		t.Fatalf("progress page missing order reference:\n%s", progressRec.Body.String())
	}
}

func TestOrderLoginPrefillsReferenceAlias(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/order/login?reference=ord_visible", nil)
	rec := httptest.NewRecorder()

	orderLoginHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `value="ord_visible"`) {
		t.Fatalf("login page did not prefill order reference:\n%s", rec.Body.String())
	}
}

func testStarterOrder(t *testing.T) starterOrder {
	t.Helper()
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
	return order
}
