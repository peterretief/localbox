package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFilesRouteRedirectsToFilebrowser(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	withTestUsersFile(t, "info:localboxzen.co.za:hash\n")

	token := newSession("info", "localboxzen.co.za")
	req := httptest.NewRequest(http.MethodGet, "/files", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rec := httptest.NewRecorder()

	filesHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != "/apps/files/" {
		t.Fatalf("Location = %q, want /apps/files/", got)
	}
}

func TestPublicUserCannotOpenFiles(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	withTestUsersFile(t, "")
	withTestOrderDB(t)

	if err := registerPublicUserProfile("localboxmail.co.za", "buyer", "buyer@example.net", "Buyer Example", "+27 82 123 4567", "1 Example Street", "secret-password", "ZAR"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/files", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", "localboxmail.co.za")})
	rec := httptest.NewRecorder()

	filesHandler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestCleanUserFileNameRejectsPathTraversal(t *testing.T) {
	bad := []string{"", ".", "..", "../secret.txt", `..\secret.txt`}
	for _, name := range bad {
		if cleaned, err := cleanUserFileName(name); err == nil {
			t.Fatalf("cleanUserFileName(%q) = %q, want error", name, cleaned)
		}
	}
	cleaned, err := cleanUserFileName("report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if cleaned != "report.pdf" {
		t.Fatalf("cleaned = %q, want report.pdf", cleaned)
	}
}

func withTestUsersFile(t *testing.T, content string) {
	t.Helper()
	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })
	if err := os.WriteFile(usersFile, []byte(content), 0640); err != nil {
		t.Fatal(err)
	}
}

func withTestOrderDB(t *testing.T) {
	t.Helper()
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })
}

func TestListUserFilesSortsByName(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	s := &Session{User: "info", Domain: "localboxzen.co.za", Expires: time.Now().Add(time.Hour)}
	dir := userFilesDir(s)
	if err := os.MkdirAll(dir, 0770); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"z.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0660); err != nil {
			t.Fatal(err)
		}
	}
	files, err := listUserFiles(s)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{files[0].Name, files[1].Name}
	if strings.Join(got, ",") != "a.txt,z.txt" {
		t.Fatalf("files = %v, want sorted names", got)
	}
}
