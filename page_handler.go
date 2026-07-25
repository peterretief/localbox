package main

import (
	"html/template"
	"io"
	"net/http"
	"strconv"
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
	data.Section = "markdown"

	if r.Method == http.MethodPost {
		content = r.FormValue("content")
		data = newPageEditorData(s.Domain, content)
		data.Section = "markdown"
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
	data.Section = "markdown"

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
	data.Section = "markdown"
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
	data.Section = "html"

	if r.Method != http.MethodPost {
		tmpl := template.Must(template.ParseFiles("templates/page.html"))
		tmpl.Execute(w, data)
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
		name, publishedHome, err := saveUploadedSiteHTML(s.Domain, header.Filename, body)
		if err != nil {
			data.UploadError = err.Error()
		} else if publishedHome {
			data.Mode = "html"
			data.UploadSuccess = "HTML homepage uploaded and published as " + name + "."
		} else {
			data.UploadSuccess = "Uploaded site page " + name + "."
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
	case "save_menu":
		data.Section = "markdown"
		files, err := listSiteHTMLFiles(s.Domain)
		if err != nil {
			data.UploadError = "Could not read HTML pages."
			break
		}
		selected := map[string]bool{}
		for _, name := range r.Form["menu"] {
			selected[name] = true
		}
		var items []SiteMenuItem
		for _, file := range siteMenuPages(files) {
			if !selected[file.Name] {
				continue
			}
			items = append(items, SiteMenuItem{
				Name:  file.Name,
				Label: r.FormValue("label_" + file.Name),
			})
		}
		if err := saveSiteMenuItems(s.Domain, items); err != nil {
			data.UploadError = "Could not save menu."
		} else {
			if loadSiteMode(s.Domain) == "markdown" {
				if err := saveHomePage(s.Domain, content); err != nil {
					data.UploadError = "Menu saved, but the homepage could not be republished."
					break
				}
				data.Mode = "markdown"
			}
			data.UploadSuccess = "Menu saved."
		}
	case "create_shopping":
		if err := saveShoppingCartPage(s.Domain); err != nil {
			data.UploadError = "Could not create shopping page."
		} else {
			if loadSiteMode(s.Domain) == "markdown" {
				if err := saveHomePage(s.Domain, content); err != nil {
					data.UploadError = "Shopping page created, but the homepage could not be republished."
					break
				}
				data.Mode = "markdown"
			}
			data.UploadSuccess = "Shopping page created as shopping.html and added to the menu."
		}
	case "save_catalog":
		catalog := catalogProductsFromForm(r)
		if err := saveSiteCatalog(s.Domain, catalog); err != nil {
			data.UploadError = "Could not save shopping items."
		} else if err := saveShoppingCartPage(s.Domain); err != nil {
			data.UploadError = "Shopping items saved, but shopping.html could not be regenerated."
		} else {
			if loadSiteMode(s.Domain) == "markdown" {
				if err := saveHomePage(s.Domain, content); err != nil {
					data.UploadError = "Shopping items saved, but the homepage could not be republished."
					break
				}
				data.Mode = "markdown"
			}
			data.UploadSuccess = "Shopping items saved for " + s.Domain + "."
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
	data.MenuPages = siteMenuPages(data.HTMLFiles)
	data.CatalogRows = catalogEditorRows(s.Domain)
	if data.Section == "" {
		data.Section = "html"
	}
	tmpl := template.Must(template.ParseFiles("templates/page.html"))
	tmpl.Execute(w, data)
}

func pageAssistantHandler(w http.ResponseWriter, r *http.Request) {
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
	data.Section = "assistant"

	if r.Method == http.MethodPost {
		switch r.FormValue("action") {
		case "generate":
			draft, err := generateLocalDesignDraft(s.Domain, DesignAssistantRequest{
				Goal:         r.FormValue("goal"),
				BusinessName: r.FormValue("business_name"),
				BusinessType: r.FormValue("business_type"),
				Location:     r.FormValue("location"),
				Services:     r.FormValue("services"),
				Tone:         r.FormValue("tone"),
				Contact:      r.FormValue("contact"),
				CallToAction: r.FormValue("call_to_action"),
			})
			if err != nil {
				data.AssistantError = err.Error()
				break
			}
			name, err := saveDesignDraft(s.Domain, draft)
			if err != nil {
				data.AssistantError = "Generated request, but could not save it."
				data.AssistantDraft = draft
				data.AssistantPreview = template.HTML(renderMarkdown(draft))
				break
			}
			data.AssistantSuccess = "Request saved as " + name + ". Copy it into an online AI website generator."
			data.AssistantDraft = draft
			data.AssistantPreview = template.HTML(renderMarkdown(draft))
		case "preview_draft":
			draft, err := loadDesignDraft(s.Domain, r.FormValue("name"))
			if err != nil {
				data.AssistantError = "Could not open request."
				break
			}
			data.AssistantDraft = draft
			data.AssistantPreview = template.HTML(renderMarkdown(draft))
			data.AssistantSuccess = "Request loaded."
		default:
			data.AssistantError = "Unknown assistant action."
		}
	} else if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data.DesignDrafts, _ = listDesignDrafts(s.Domain)
	tmpl := template.Must(template.ParseFiles("templates/page.html"))
	tmpl.Execute(w, data)
}

func newPageEditorData(domain, content string) PageEditorData {
	images, _ := listSiteImages(domain)
	htmlFiles, _ := listSiteHTMLFiles(domain)
	designDrafts, _ := listDesignDrafts(domain)
	return PageEditorData{
		Content:           content,
		Preview:           template.HTML(renderMarkdown(content)),
		Images:            images,
		HTMLFiles:         htmlFiles,
		MenuPages:         siteMenuPages(htmlFiles),
		CatalogRows:       catalogEditorRows(domain),
		DesignDrafts:      designDrafts,
		Domain:            domain,
		Mode:              loadSiteMode(domain),
		Section:           "markdown",
		AssistantProvider: designAssistantProviderName(),
		AssistantBilling:  designAssistantBillingNote(),
	}
}

func catalogProductsFromForm(r *http.Request) []cartProduct {
	count, _ := strconv.Atoi(r.FormValue("catalog_count"))
	if count <= 0 || count > 100 {
		count = 0
	}
	var products []cartProduct
	for i := 0; i < count; i++ {
		prefix := "catalog_" + strconv.Itoa(i) + "_"
		product := cartProduct{
			ID:          r.FormValue(prefix + "id"),
			Name:        r.FormValue(prefix + "name"),
			Description: r.FormValue(prefix + "description"),
			Price:       r.FormValue(prefix + "price"),
			Category:    r.FormValue(prefix + "category"),
			Image:       r.FormValue(prefix + "image"),
			Featured:    r.FormValue(prefix+"featured") == "1",
		}
		product = cleanCatalogProduct(product)
		if product.Name == "" {
			continue
		}
		products = append(products, product)
	}
	return products
}
