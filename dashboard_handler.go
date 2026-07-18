package main

import (
	"html/template"
	"net/http"
	"net/url"
)

type DashboardLink struct {
	Title       string
	Description string
	URL         string
	External    bool
}

type DashboardData struct {
	User       string
	Domain     string
	Email      string
	Links      []DashboardLink
	AdminLinks []DashboardLink
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login?next=/dashboard", http.StatusFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	email := s.User + "@" + s.Domain
	mailURL := "https://mail.localboxmail.co.za/#/login?username=" + url.QueryEscape(email)
	data := DashboardData{
		User:   s.User,
		Domain: s.Domain,
		Email:  email,
		Links: []DashboardLink{
			{
				Title:       "Mail",
				Description: "Open ALPS webmail, contacts, and calendar.",
				URL:         mailURL,
				External:    true,
			},
			{
				Title:       "Files",
				Description: "Upload, manage, and download files for this mailbox.",
				URL:         "/files",
			},
			{
				Title:       "WebDAV",
				Description: "Open the standards-based WebDAV file endpoint.",
				URL:         "https://files.localboxmail.co.za/",
				External:    true,
			},
			{
				Title:       "Password",
				Description: "Change your LocalBox mailbox password.",
				URL:         "/password",
			},
		},
	}

	if isDomainAdmin(s.User, s.Domain) {
		data.AdminLinks = append(data.AdminLinks,
			DashboardLink{
				Title:       "Domain Admin",
				Description: "Manage mailboxes and domain users.",
				URL:         "/admin",
			},
			DashboardLink{
				Title:       "Website",
				Description: "Edit the domain website pages and images.",
				URL:         "/page",
			},
		)
	}
	if isBoxAdmin(s.User, s.Domain) {
		data.AdminLinks = append(data.AdminLinks,
			DashboardLink{
				Title:       "Orders",
				Description: "Review LocalBox orders and customer requests.",
				URL:         "/orders",
			},
			DashboardLink{
				Title:       "System",
				Description: "Manage domains, certificates, and box admin roles.",
				URL:         "/system",
			},
		)
	}

	tmpl := template.Must(template.ParseFiles("templates/dashboard.html"))
	tmpl.Execute(w, data)
}
