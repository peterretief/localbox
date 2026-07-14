package main

import (
	"net/http"
	"path/filepath"
)

func certificateHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	domain := normalizeDomain(s.Domain)
	if !safeDomain(domain) {
		http.Error(w, "Invalid domain", http.StatusBadRequest)
		return
	}
	path := registrationCertificatePath(domain)
	if !registrationCertificateExists(domain) {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Disposition", `inline; filename="`+domain+`-localbox-certificate.txt"`)
	http.ServeFile(w, r, filepath.Clean(path))
}
