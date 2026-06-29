package main

import "net/http"

func contactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.FormValue("name")
	email := r.FormValue("email")
	subject := r.FormValue("subject")
	message := r.FormValue("message")

	if err := validate(name, email, message); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := saveMessage(name, email, subject, message); err != nil {
		http.Error(w, "Unable to save message", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/thankyou.html", http.StatusSeeOther)
}
