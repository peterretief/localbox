package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderMarkdownImage(t *testing.T) {
	rendered := renderMarkdown(`![Alt text](https://example.com/image.jpg "Title")`)

	if !strings.Contains(rendered, `<img src="https://example.com/image.jpg" alt="Alt text" title="Title" loading="lazy">`) {
		t.Fatalf("rendered markdown did not include expected image: %s", rendered)
	}
}

func TestRenderMarkdownLinkedImage(t *testing.T) {
	rendered := renderMarkdown(`[![Alt text](/assets/image.jpg "Title")](/contact.html)`)

	want := `<a href="/contact.html"><img src="/assets/image.jpg" alt="Alt text" title="Title" loading="lazy"></a>`
	if !strings.Contains(rendered, want) {
		t.Fatalf("rendered markdown did not include expected linked image: %s", rendered)
	}
}

func TestAllowedUploadedHTMLName(t *testing.T) {
	allowed := []string{
		"index.html",
		"index-20260701T103012Z.html",
	}
	for _, name := range allowed {
		if !allowedUploadedHTMLName(name) {
			t.Fatalf("expected %q to be allowed", name)
		}
	}

	rejected := []string{
		"home.html",
		"index.htm",
		"../index.html",
		"index-20260701.html",
	}
	for _, name := range rejected {
		if allowedUploadedHTMLName(name) {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
}

func TestRestoreLatestSiteHTMLBackupPrefersUploadedHTML(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	site := domainSiteBase(domain)
	if err := os.MkdirAll(site, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("markdown default"), 0664); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index-20260702T175908Z.html"), []byte("uploaded html"), 0664); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index-20260702T175919Z.html"), []byte("markdown backup"), 0664); err != nil {
		t.Fatal(err)
	}
	if err := recordUploadedHTML(domain, "index-20260702T175908Z.html"); err != nil {
		t.Fatal(err)
	}

	name, err := restoreLatestSiteHTMLBackup(domain)
	if err != nil {
		t.Fatal(err)
	}
	if name != "index-20260702T175908Z.html" {
		t.Fatalf("expected uploaded backup to be restored, got %s", name)
	}

	data, err := os.ReadFile(filepath.Join(site, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "uploaded html" {
		t.Fatalf("expected uploaded html active file, got %q", string(data))
	}
}
