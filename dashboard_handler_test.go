package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDomainAdminDashboardLinksToCustomerDomains(t *testing.T) {
	oldUsersFile := usersFile
	oldAdminsFile := adminsFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	adminsFile = filepath.Join(t.TempDir(), "admins.conf")
	t.Cleanup(func() {
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
	})
	if err := saveUsers([]User{{Username: "info", Domain: "example.co.za", Hash: "hash"}}); err != nil {
		t.Fatal(err)
	}
	if err := addDomainAdmin("info", "example.co.za"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("info", "example.co.za")})
	rec := httptest.NewRecorder()

	dashboardHandler(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK ||
		!strings.Contains(body, "https://localboxmail.co.za/account/domains") ||
		!strings.Contains(body, "Register another domain or manage your LocalBox domain orders") ||
		!strings.Contains(body, "sign in with the customer account that registered the domain if prompted") {
		t.Fatalf("expected customer domains link on domain admin dashboard, code=%d body=%q", rec.Code, body)
	}
}
