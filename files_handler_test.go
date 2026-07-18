package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFilesUploadUsesSessionMailbox(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("hello files")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	token := newSession("info", "localboxzen.co.za")
	req := httptest.NewRequest(http.MethodPost, "/files", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rec := httptest.NewRecorder()

	filesHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	want := filepath.Join(localBoxBase, "files", "info@localboxzen.co.za", "hello.txt")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("uploaded file missing at %s: %v", want, err)
	}
	wrong := filepath.Join(localBoxBase, "files", "someone@localboxmail.co.za", "hello.txt")
	if _, err := os.Stat(wrong); !os.IsNotExist(err) {
		t.Fatalf("file unexpectedly appeared in main mailbox path")
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
