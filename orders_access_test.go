package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestOrdersAdminRequiresPrimaryDomainAdmin(t *testing.T) {
	tmp := t.TempDir()
	oldDomainsFile := domainsFile
	oldAdminsFile := adminsFile
	oldBoxAdminsFile := boxAdminsFile
	oldUsersFile := usersFile
	domainsFile = filepath.Join(tmp, "domains.conf")
	adminsFile = filepath.Join(tmp, "admins.conf")
	boxAdminsFile = filepath.Join(tmp, "box-admins.conf")
	usersFile = filepath.Join(tmp, "users.conf")
	t.Cleanup(func() {
		domainsFile = oldDomainsFile
		adminsFile = oldAdminsFile
		boxAdminsFile = oldBoxAdminsFile
		usersFile = oldUsersFile
	})

	if err := os.WriteFile(domainsFile, []byte("localboxmail.co.za\nlocalboxzen.co.za\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adminsFile, []byte("peter:localboxmail.co.za\ninfo:localboxzen.co.za\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(boxAdminsFile, []byte("info:localboxzen.co.za\npeter:localboxmail.co.za\n"), 0640); err != nil {
		t.Fatal(err)
	}

	if !isOrdersAdmin("peter", "localboxmail.co.za") {
		t.Fatal("expected primary-domain domain admin to manage orders")
	}
	if isOrdersAdmin("info", "localboxzen.co.za") {
		t.Fatal("expected customer-domain box admin not to manage orders")
	}
}

func TestRequireOrdersAdminRejectsNonPrimaryBoxAdmin(t *testing.T) {
	tmp := t.TempDir()
	oldDomainsFile := domainsFile
	oldAdminsFile := adminsFile
	oldBoxAdminsFile := boxAdminsFile
	oldUsersFile := usersFile
	domainsFile = filepath.Join(tmp, "domains.conf")
	adminsFile = filepath.Join(tmp, "admins.conf")
	boxAdminsFile = filepath.Join(tmp, "box-admins.conf")
	usersFile = filepath.Join(tmp, "users.conf")
	t.Cleanup(func() {
		domainsFile = oldDomainsFile
		adminsFile = oldAdminsFile
		boxAdminsFile = oldBoxAdminsFile
		usersFile = oldUsersFile
	})

	if err := os.WriteFile(domainsFile, []byte("localboxmail.co.za\nlocalboxzen.co.za\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adminsFile, []byte("peter:localboxmail.co.za\ninfo:localboxzen.co.za\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(boxAdminsFile, []byte("info:localboxzen.co.za\n"), 0640); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("info", "localboxzen.co.za")})
	rec := httptest.NewRecorder()

	if s := requireOrdersAdmin(rec, req); s != nil {
		t.Fatal("expected non-primary box admin to be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
