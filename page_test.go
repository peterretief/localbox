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
		"software.html",
		"about-us.html",
	}
	for _, name := range allowed {
		if !allowedUploadedHTMLName(name) {
			t.Fatalf("expected %q to be allowed", name)
		}
	}

	rejected := []string{
		"index.htm",
		"../index.html",
		"Software Page.html",
		"software.php",
	}
	for _, name := range rejected {
		if allowedUploadedHTMLName(name) {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
}

func TestSaveUploadedSiteHTMLStoresAdditionalPageWithoutPublishingHome(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	site := domainSiteBase(domain)
	if err := os.MkdirAll(site, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("active home"), 0664); err != nil {
		t.Fatal(err)
	}

	name, publishedHome, err := saveUploadedSiteHTML(domain, "software.html", []byte("software page"))
	if err != nil {
		t.Fatal(err)
	}
	if name != "software.html" {
		t.Fatalf("expected software.html, got %q", name)
	}
	if publishedHome {
		t.Fatal("software.html should not publish the homepage")
	}

	active, err := os.ReadFile(filepath.Join(site, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "active home" {
		t.Fatalf("expected active homepage to stay unchanged, got %q", string(active))
	}

	page, err := os.ReadFile(filepath.Join(site, "software.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(page) != "software page" {
		t.Fatalf("expected uploaded page content, got %q", string(page))
	}
	if !isUploadedHTML(domain, "software.html") {
		t.Fatal("expected software.html to be recorded as uploaded")
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

func TestSaveUploadedHomeHTMLStoresVersionAndPublishesActiveCopy(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	site := domainSiteBase(domain)
	if err := os.MkdirAll(site, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("old active"), 0664); err != nil {
		t.Fatal(err)
	}

	name, err := saveUploadedHomeHTML(domain, []byte("new uploaded html"))
	if err != nil {
		t.Fatal(err)
	}
	if name == "" || name == "index.html" {
		t.Fatalf("expected timestamped upload name, got %q", name)
	}

	active, err := os.ReadFile(filepath.Join(site, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "new uploaded html" {
		t.Fatalf("expected active index to contain uploaded html, got %q", string(active))
	}

	stored, err := os.ReadFile(filepath.Join(site, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != "new uploaded html" {
		t.Fatalf("expected saved upload to contain uploaded html, got %q", string(stored))
	}
	if !isUploadedHTML(domain, name) {
		t.Fatalf("expected %s to be marked as uploaded html", name)
	}
	if loadSiteMode(domain) != "html" {
		t.Fatalf("expected html mode after upload")
	}
}
