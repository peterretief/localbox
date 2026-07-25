package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionBarMiddlewareInjectsMailboxUser(t *testing.T) {
	oldUsersFile := usersFile
	oldAdminsFile := adminsFile
	oldBoxAdminsFile := boxAdminsFile
	dir := t.TempDir()
	usersFile = filepath.Join(dir, "users.conf")
	adminsFile = filepath.Join(dir, "admins.conf")
	boxAdminsFile = filepath.Join(dir, "box-admins.conf")
	t.Cleanup(func() {
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
		boxAdminsFile = oldBoxAdminsFile
	})
	if err := saveUsers([]User{{Username: "admin", Domain: "example.co.za", Hash: "hash"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(boxAdminsFile, []byte("other:example.co.za\n"), 0640); err != nil {
		t.Fatal(err)
	}

	handler := sessionBarMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body><main>Dashboard</main></body></html>`))
	}))
	req := httptest.NewRequest(http.MethodGet, "http://example.co.za/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("admin", "example.co.za")})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "localbox-session-bar") || !strings.Contains(body, "Logged in as admin@example.co.za") {
		t.Fatalf("expected injected mailbox session bar, got %s", body)
	}
	if !strings.Contains(body, "Type: mailbox user") {
		t.Fatalf("expected mailbox user type, got %s", body)
	}
	if !strings.Contains(body, `href="/logout"`) {
		t.Fatalf("expected mailbox logout link, got %s", body)
	}
}

func TestSessionBarMiddlewareShowsBoxAdminType(t *testing.T) {
	oldUsersFile := usersFile
	oldBoxAdminsFile := boxAdminsFile
	dir := t.TempDir()
	usersFile = filepath.Join(dir, "users.conf")
	boxAdminsFile = filepath.Join(dir, "box-admins.conf")
	t.Cleanup(func() {
		usersFile = oldUsersFile
		boxAdminsFile = oldBoxAdminsFile
	})
	if err := saveUsers([]User{{Username: "admin", Domain: "example.co.za", Hash: "hash"}}); err != nil {
		t.Fatal(err)
	}
	if err := saveRoleFile(boxAdminsFile, map[string]bool{"admin:example.co.za": true}); err != nil {
		t.Fatal(err)
	}

	handler := sessionBarMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body><main>System</main></body></html>`))
	}))
	req := httptest.NewRequest(http.MethodGet, "http://example.co.za/system", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("admin", "example.co.za")})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "Type: box admin") {
		t.Fatalf("expected box admin type, got %s", rec.Body.String())
	}
}

func TestSessionBarMiddlewareShowsPublicCustomerType(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })
	if err := registerPublicUserProfile("example.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}

	handler := sessionBarMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body><main>Orders</main></body></html>`))
	}))
	req := httptest.NewRequest(http.MethodGet, "http://example.co.za/order/login", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "example.co.za")})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Logged in as buyer") || !strings.Contains(body, "Type: customer") {
		t.Fatalf("expected public customer session bar, got %s", body)
	}
	if !strings.Contains(body, `href="/public/logout"`) {
		t.Fatalf("expected public logout link, got %s", body)
	}
}

func TestSessionBarMiddlewareSkipsAnonymousHTML(t *testing.T) {
	handler := sessionBarMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><html><body><main>Public</main></body></html>`))
	}))
	req := httptest.NewRequest(http.MethodGet, "http://example.co.za/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "localbox-session-bar") {
		t.Fatalf("expected anonymous response without session bar, got %s", rec.Body.String())
	}
}

func TestSessionBarMiddlewareDoesNotDuplicatePublicBar(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })
	if err := registerPublicUserProfile("example.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}

	body := injectPublicSessionBar(
		[]byte(`<!doctype html><html><body><main>Site</main></body></html>`),
		&Session{User: "buyer", Domain: "example.co.za"},
		"/",
		true,
	)
	handler := sessionBarMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://example.co.za/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "example.co.za")})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := strings.Count(rec.Body.String(), "localbox-session-bar"); got != 1 {
		t.Fatalf("session bar count = %d, want 1:\n%s", got, rec.Body.String())
	}
}

func TestSessionBarMiddlewareSkipsNonHTML(t *testing.T) {
	handler := sessionBarMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest(http.MethodGet, "http://example.co.za/health", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("admin", "example.co.za")})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "ok" {
		t.Fatalf("expected plain response unchanged, got %q", rec.Body.String())
	}
}
