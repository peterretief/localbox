package main

import (
	"html/template"
	"io"
	"net/http"
)

func pageHandler(w http.ResponseWriter, r *http.Request) {
	s := requireDomainAdmin(w, r)
	if s == nil {
		return
	}

	content, err := loadHomeMarkdown(s.Domain)
	if err != nil {
		http.Error(w, "Could not read page content", http.StatusInternalServerError)
		return
	}

	data := newPageEditorData(s.Domain, content)

	if r.Method == http.MethodPost {
		content = r.FormValue("content")
		data = newPageEditorData(s.Domain, content)
		if err := saveHomePage(s.Domain, content); err != nil {
			data.Error = "Could not save page."
		} else {
			data.Success = "Page saved."
		}
	} else if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl := template.Must(template.ParseFiles("templates/page.html"))
	tmpl.Execute(w, data)
}

func pageImagesHandler(w http.ResponseWriter, r *http.Request) {
	s := requireDomainAdmin(w, r)
	if s == nil {
		return
	}

	content, err := loadHomeMarkdown(s.Domain)
	if err != nil {
		http.Error(w, "Could not read page content", http.StatusInternalServerError)
		return
	}
	data := newPageEditorData(s.Domain, content)

	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/page", http.StatusFound)
		return
	}

	action := r.FormValue("action")
	switch action {
	case "upload":
		file, header, err := r.FormFile("image")
		if err != nil {
			data.ImageError = "Choose an image to upload."
			break
		}
		defer file.Close()

		image, err := saveSiteImage(s.Domain, file, header)
		if err != nil {
			data.ImageError = err.Error()
			break
		}
		data.ImageSuccess = "Uploaded " + image.Name
	case "delete":
		name := r.FormValue("name")
		if err := deleteSiteImage(s.Domain, name); err != nil {
			data.ImageError = "Could not delete image."
		} else {
			data.ImageSuccess = "Deleted " + name
		}
	default:
		data.ImageError = "Unknown image action."
	}

	data.Images, _ = listSiteImages(s.Domain)
	tmpl := template.Must(template.ParseFiles("templates/page.html"))
	tmpl.Execute(w, data)
}

func pageHTMLHandler(w http.ResponseWriter, r *http.Request) {
	s := requireDomainAdmin(w, r)
	if s == nil {
		return
	}

	content, err := loadHomeMarkdown(s.Domain)
	if err != nil {
		http.Error(w, "Could not read page content", http.StatusInternalServerError)
		return
	}
	data := newPageEditorData(s.Domain, content)

	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/page", http.StatusFound)
		return
	}

	action := r.FormValue("action")
	switch action {
	case "upload_html":
		file, header, err := r.FormFile("html")
		if err != nil {
			data.UploadError = "Choose an HTML file to upload."
			break
		}
		defer file.Close()
		if header.Size > 1<<20 {
			data.UploadError = "HTML file must be 1 MB or smaller."
			break
		}
		body, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			data.UploadError = "Could not read HTML file."
			break
		}
		if err := saveUploadedHomeHTML(s.Domain, body); err != nil {
			data.UploadError = err.Error()
		} else {
			data.Mode = "html"
			data.UploadSuccess = "HTML homepage uploaded."
		}
	case "restore_html":
		name := r.FormValue("name")
		if err := restoreSiteHTMLFile(s.Domain, name); err != nil {
			data.UploadError = "Could not restore HTML file."
		} else {
			data.Mode = "html"
			data.UploadSuccess = "Restored " + name + "."
		}
	case "delete_html":
		name := r.FormValue("name")
		if err := deleteSiteHTMLFile(s.Domain, name); err != nil {
			data.UploadError = "Could not delete HTML file."
		} else {
			data.UploadSuccess = "Deleted " + name + "."
		}
	case "use_html":
		name, err := restoreLatestSiteHTMLBackup(s.Domain)
		if err != nil {
			data.UploadError = "No saved HTML file is available to publish."
		} else {
			data.Mode = "html"
			data.UploadSuccess = "HTML homepage published from " + name + "."
		}
	case "use_markdown":
		if err := saveHomePage(s.Domain, content); err != nil {
			data.UploadError = "Could not switch to Markdown."
		} else {
			data.Mode = "markdown"
			data.UploadSuccess = "Markdown homepage published."
		}
	default:
		data.UploadError = "Unknown HTML action."
	}

	data.HTMLFiles, _ = listSiteHTMLFiles(s.Domain)
	tmpl := template.Must(template.ParseFiles("templates/page.html"))
	tmpl.Execute(w, data)
}

func newPageEditorData(domain, content string) PageEditorData {
	images, _ := listSiteImages(domain)
	htmlFiles, _ := listSiteHTMLFiles(domain)
	return PageEditorData{
		Content:   content,
		Preview:   template.HTML(renderMarkdown(content)),
		Images:    images,
		HTMLFiles: htmlFiles,
		Domain:    domain,
		Mode:      loadSiteMode(domain),
	}
}
