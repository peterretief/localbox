package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListSiteImagesIncludesCopySnippets(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	if err := os.MkdirAll(imageDir(domain), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imageDir(domain), "clinic-team.jpg"), []byte("image"), 0664); err != nil {
		t.Fatal(err)
	}

	images, err := listSiteImages(domain)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("expected one image, got %#v", images)
	}
	image := images[0]
	if image.Path != "/assets/images/clinic-team.jpg" {
		t.Fatalf("unexpected image path %q", image.Path)
	}
	if image.Markdown != "![clinic team](/assets/images/clinic-team.jpg)" {
		t.Fatalf("unexpected markdown snippet %q", image.Markdown)
	}
	if image.HTML != `<img src="/assets/images/clinic-team.jpg" alt="clinic team">` {
		t.Fatalf("unexpected HTML snippet %q", image.HTML)
	}
}
