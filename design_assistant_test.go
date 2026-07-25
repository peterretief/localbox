package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateLocalDesignDraft(t *testing.T) {
	draft, err := generateLocalDesignDraft("example.co.za", DesignAssistantRequest{
		BusinessName: "Dave's Repairs",
		BusinessType: "Appliance repairs",
		Location:     "Cape Town",
		Services:     "Fridge repairs\nWashing machine repairs",
		Contact:      "Email dave@example.co.za",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Dave's Repairs", "Fridge repairs", "Washing machine repairs", "[Contact us](/contact.html)", "generated locally"} {
		if !strings.Contains(draft, want) {
			t.Fatalf("draft missing %q:\n%s", want, draft)
		}
	}
}

func TestDesignDraftSaveLoadAndPublish(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.co.za"
	name, err := saveDesignDraft(domain, "# Draft\n\nHello")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "draft-") || !strings.HasSuffix(name, ".md") {
		t.Fatalf("unexpected draft name %q", name)
	}
	drafts, err := listDesignDrafts(domain)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].Name != name {
		t.Fatalf("unexpected drafts: %#v", drafts)
	}
	content, err := loadDesignDraft(domain, name)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveHomePage(domain, content); err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(homeMarkdownPath(domain))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(published), "Hello") {
		t.Fatalf("expected published markdown, got %s", published)
	}
}

func TestPageAssistantHandlerGeneratesDraftWithoutPublishing(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	oldUsers := usersFile
	oldAdmins := adminsFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	adminsFile = filepath.Join(t.TempDir(), "admins.conf")
	t.Cleanup(func() {
		usersFile = oldUsers
		adminsFile = oldAdmins
	})
	if err := os.WriteFile(usersFile, []byte("admin:example.co.za:x\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adminsFile, []byte("admin:example.co.za\n"), 0640); err != nil {
		t.Fatal(err)
	}

	token := newSession("admin", "example.co.za")
	form := url.Values{}
	form.Set("action", "generate")
	form.Set("business_name", "Example Clinic")
	form.Set("business_type", "Clinic")
	form.Set("services", "Appointments\nFollow-up care")
	req := httptest.NewRequest(http.MethodPost, "https://example.co.za/page/assistant", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rec := httptest.NewRecorder()

	pageAssistantHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Draft saved as") || !strings.Contains(body, "Example Clinic") {
		t.Fatalf("expected generated draft response, got %s", body)
	}
	if _, err := os.Stat(homeMarkdownPath("example.co.za")); !os.IsNotExist(err) {
		t.Fatalf("generate should not publish markdown, err=%v", err)
	}
}
