package main

import (
	"html/template"
	"net/http"
)

func adminHandler(w http.ResponseWriter, r *http.Request) {
	s := requireDomainAdmin(w, r)
	if s == nil {
		return
	}

	data := adminView(s.Domain)
	if r.Method == http.MethodPost {
		action := r.FormValue("action")
		var err error
		switch action {
		case "add_mailbox":
			err = addMailbox(r.FormValue("username"), s.Domain, r.FormValue("password"))
			data.Success = "Mailbox added."
		case "reset_password":
			err = resetMailboxPassword(r.FormValue("username"), s.Domain, r.FormValue("password"))
			data.Success = "Password reset."
		case "archive_mailbox":
			err = archiveMailbox(r.FormValue("username"), s.Domain)
			data.Success = "Mailbox archived."
		default:
			err = errUnknownAdminAction()
		}
		if err != nil {
			data.Error = err.Error()
			data.Success = ""
		}
		fresh := adminView(s.Domain)
		data.Domain = fresh.Domain
		data.Users = fresh.Users
		data.RegisteredUsers = fresh.RegisteredUsers
	} else if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl := template.Must(template.ParseFiles("templates/admin.html"))
	tmpl.Execute(w, data)
}

func errUnknownAdminAction() error {
	return adminError("unknown admin action")
}

type adminError string

func (e adminError) Error() string {
	return string(e)
}
