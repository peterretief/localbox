package main

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type FileEntry struct {
	Name    string
	Size    string
	ModTime string
	URL     string
}

type FilesPageData struct {
	User   string
	Domain string
	Email  string
	Error  string
	Files  []FileEntry
}

func filesHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login?next=/apps/files/", http.StatusFound)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.Redirect(w, r, "/apps/files/", http.StatusFound)
}

func fileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name, err := cleanUserFileName(r.URL.Query().Get("name"))
	if err != nil {
		http.Error(w, "Invalid file", http.StatusBadRequest)
		return
	}
	path := filepath.Join(userFilesDir(s), name)
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	http.ServeFile(w, r, path)
}

func fileDeleteHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name, err := cleanUserFileName(r.FormValue("name"))
	if err == nil {
		err = os.Remove(filepath.Join(userFilesDir(s), name))
	}
	if err != nil && !os.IsNotExist(err) {
		renderFilesPage(w, s, "Could not delete file.")
		return
	}
	http.Redirect(w, r, "/files", http.StatusFound)
}

func renderFilesPage(w http.ResponseWriter, s *Session, message string) {
	files, err := listUserFiles(s)
	if err != nil && message == "" {
		message = "Could not read files."
	}
	data := FilesPageData{
		User:   s.User,
		Domain: s.Domain,
		Email:  s.User + "@" + s.Domain,
		Error:  message,
		Files:  files,
	}
	tmpl := template.Must(template.ParseFiles("templates/files.html"))
	tmpl.Execute(w, data)
}

func saveUploadedFile(r *http.Request, s *Session) error {
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		return fmt.Errorf("could not read upload")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return fmt.Errorf("choose a file to upload")
	}
	defer file.Close()

	name, err := cleanUserFileName(header.Filename)
	if err != nil {
		return err
	}
	dir := userFilesDir(s)
	if err := os.MkdirAll(dir, 0770); err != nil {
		return fmt.Errorf("could not create file directory")
	}
	dst, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0660)
	if err != nil {
		return fmt.Errorf("could not save file")
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		return fmt.Errorf("could not write file")
	}
	return nil
}

func listUserFiles(s *Session) ([]FileEntry, error) {
	dir := userFilesDir(s)
	if err := os.MkdirAll(dir, 0770); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]FileEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		files = append(files, FileEntry{
			Name:    name,
			Size:    humanFileSize(info.Size()),
			ModTime: info.ModTime().Format("2006-01-02 15:04"),
			URL:     "/files/download?name=" + template.URLQueryEscaper(name),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	return files, nil
}

func userFilesDir(s *Session) string {
	return filepath.Join(localBoxBase, "files", s.User+"@"+s.Domain)
}

func cleanUserFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("invalid filename")
	}
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("invalid filename")
	}
	return filepath.Base(name), nil
}

func humanFileSize(size int64) string {
	units := []string{"B", "KB", "MB", "GB"}
	value := float64(size)
	unit := units[0]
	for i := 1; i < len(units) && value >= 1024; i++ {
		value /= 1024
		unit = units[i]
	}
	if unit == "B" {
		return fmt.Sprintf("%d B", size)
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}
