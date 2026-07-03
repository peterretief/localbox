package main

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const maxImageUploadBytes = 5 << 20

var safeImageFilename = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type SiteImage struct {
	Name        string
	Path        string
	Markdown    string
	Size        int64
	DisplaySize string
}

func imageDir(domain string) string {
	return filepath.Join(domainSiteBase(domain), "assets", "images")
}

func listSiteImages(domain string) ([]SiteImage, error) {
	entries, err := os.ReadDir(imageDir(domain))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var images []SiteImage
	for _, entry := range entries {
		if entry.IsDir() || !allowedImageName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := "/assets/images/" + entry.Name()
		images = append(images, SiteImage{
			Name:        entry.Name(),
			Path:        path,
			Markdown:    fmt.Sprintf("![%s](%s)", imageAlt(entry.Name()), path),
			Size:        info.Size(),
			DisplaySize: displaySize(info.Size()),
		})
	}

	sort.Slice(images, func(i, j int) bool {
		return images[i].Name < images[j].Name
	})

	return images, nil
}

func saveSiteImage(domain string, file multipart.File, header *multipart.FileHeader) (SiteImage, error) {
	name := cleanImageFilename(header.Filename)
	if name == "" {
		return SiteImage{}, fmt.Errorf("invalid filename")
	}
	if !allowedImageName(name) {
		return SiteImage{}, fmt.Errorf("only jpg, jpeg, png, gif, and webp images are allowed")
	}
	if header.Size > maxImageUploadBytes {
		return SiteImage{}, fmt.Errorf("image must be 5 MB or smaller")
	}

	if err := os.MkdirAll(imageDir(domain), 0775); err != nil {
		return SiteImage{}, err
	}

	limited := io.LimitReader(file, maxImageUploadBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return SiteImage{}, err
	}
	if len(data) > maxImageUploadBytes {
		return SiteImage{}, fmt.Errorf("image must be 5 MB or smaller")
	}
	if !allowedImageContent(data) {
		return SiteImage{}, fmt.Errorf("file content does not look like an allowed image")
	}

	path := filepath.Join(imageDir(domain), name)
	if err := os.WriteFile(path, data, 0664); err != nil {
		return SiteImage{}, err
	}

	return SiteImage{
		Name:        name,
		Path:        "/assets/images/" + name,
		Markdown:    fmt.Sprintf("![%s](/assets/images/%s)", imageAlt(name), name),
		Size:        int64(len(data)),
		DisplaySize: displaySize(int64(len(data))),
	}, nil
}

func deleteSiteImage(domain, name string) error {
	name = cleanImageFilename(name)
	if name == "" || !allowedImageName(name) {
		return fmt.Errorf("invalid image name")
	}
	return os.Remove(filepath.Join(imageDir(domain), name))
}

func cleanImageFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")
	name = safeImageFilename.ReplaceAllString(name, "-")
	name = strings.Trim(name, ".-")
	return strings.ToLower(name)
}

func allowedImageName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func allowedImageContent(data []byte) bool {
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func imageAlt(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

func displaySize(size int64) string {
	if size >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	}
	if size >= 1<<10 {
		return fmt.Sprintf("%.1f KB", float64(size)/(1<<10))
	}
	return fmt.Sprintf("%d B", size)
}
