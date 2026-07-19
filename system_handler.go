package main

import (
	"html/template"
	"net/http"
	"path/filepath"
)

func systemHandler(w http.ResponseWriter, r *http.Request) {
	s := requireBoxAdmin(w, r)
	if s == nil {
		return
	}

	data := systemView()
	data.Access = accessView(s)
	if r.Method == http.MethodPost {
		action := r.FormValue("action")
		var err error
		switch action {
		case "add_domain":
			err = addDomain(r.FormValue("domain"))
			data.Success = "Domain added."
		case "archive_domain":
			err = archiveDomain(r.FormValue("domain"))
			data.Success = "Domain archived."
		case "add_domain_admin":
			err = addDomainAdmin(r.FormValue("username"), r.FormValue("domain"))
			data.Success = "Domain admin added."
		case "create_domain_admin":
			err = createDomainAdmin(r.FormValue("username"), r.FormValue("domain"), r.FormValue("password"))
			data.Success = "Domain admin mailbox created."
		case "remove_domain_admin":
			err = removeDomainAdmin(r.FormValue("username"), r.FormValue("domain"))
			data.Success = "Domain admin removed."
		case "add_box_admin":
			err = addBoxAdmin(r.FormValue("username"), r.FormValue("domain"))
			data.Success = "Box admin added."
		case "remove_box_admin":
			err = removeBoxAdmin(r.FormValue("username"), r.FormValue("domain"))
			data.Success = "Box admin removed."
		default:
			err = errUnknownAdminAction()
		}
		if err != nil {
			data.Error = err.Error()
			data.Success = ""
		}
		fresh := systemView()
		data.Domains = fresh.Domains
		data.DomainAdmins = fresh.DomainAdmins
		data.BoxAdmins = fresh.BoxAdmins
		data.Access = accessView(s)
	} else if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl := template.Must(template.ParseFiles("templates/system.html"))
	tmpl.Execute(w, data)
}

func systemCertificateHandler(w http.ResponseWriter, r *http.Request) {
	if s := requireBoxAdmin(w, r); s == nil {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	domain := normalizeDomain(r.URL.Query().Get("domain"))
	if !safeDomain(domain) {
		http.Error(w, "Invalid domain", http.StatusBadRequest)
		return
	}
	path := registrationCertificatePath(domain)
	if !registrationCertificateExists(domain) {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Disposition", `attachment; filename="`+domain+`-localbox-certificate.txt"`)
	http.ServeFile(w, r, filepath.Clean(path))
}
