package main

import (
	"html/template"
	"net/http"
)

type DashboardLink struct {
	Title       string
	Description string
	URL         string
	External    bool
}

type DashboardData struct {
	User        string
	Domain      string
	Email       string
	Links       []DashboardLink
	AdminLinks  []DashboardLink
	CustomerURL string
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

	if !userExists(s.User, s.Domain) {
		http.Redirect(w, r, "/account/domains", http.StatusFound)
		return
	}

	email := s.User + "@" + s.Domain
	data := DashboardData{
		User:        s.User,
		Domain:      s.Domain,
		Email:       email,
		CustomerURL: "https://" + primaryLocalBoxHost + "/account/domains",
	}
	data.Links = []DashboardLink{
		{
			Title:       "Mail",
			Description: "Open ALPS webmail, contacts, and calendar.",
			URL:         "/apps/mail",
		},
		{
			Title:       "Files",
			Description: "Open the full file manager with sharing, previews, and folder tools.",
			URL:         "/apps/files/",
		},
		{
			Title:       "Password",
			Description: "Change your LocalBox mailbox password.",
			URL:         "/password",
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
			DashboardLink{
				Title:       "Customer account",
				Description: "Register another domain or manage your LocalBox domain orders. Use the customer account that registered the domain.",
				URL:         data.CustomerURL,
				External:    true,
			},
		)
	}
	if isBoxAdmin(s.User, s.Domain) {
		data.AdminLinks = append(data.AdminLinks,
			DashboardLink{
				Title:       "System",
				Description: "Manage domains, certificates, and box admin roles.",
				URL:         "/system",
			},
		)
	}
	if isOrdersAdmin(s.User, s.Domain) {
		data.AdminLinks = append(data.AdminLinks,
			DashboardLink{
				Title:       "Orders",
				Description: "Review LocalBox orders and customer requests.",
				URL:         "/orders",
			},
		)
	}

	tmpl := template.Must(template.ParseFiles("templates/dashboard.html"))
	tmpl.Execute(w, data)
}
