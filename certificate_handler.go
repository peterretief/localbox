package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func certificateHandler(w http.ResponseWriter, r *http.Request) {
	s := requireMailboxSession(w, r)
	if s == nil {
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
	if strings.EqualFold(r.URL.Query().Get("format"), "svg") {
		path = registrationCertificateSVGPath(domain)
		if _, err := os.Stat(path); err != nil {
			if err := saveRegistrationCertificate(domain, s.User, time.Now().UTC()); err != nil {
				http.Error(w, "Certificate unavailable", http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+domain+`-localbox-certificate.svg"`)
		http.ServeFile(w, r, filepath.Clean(path))
		return
	}

	w.Header().Set("Content-Disposition", `inline; filename="`+domain+`-localbox-certificate.txt"`)
	http.ServeFile(w, r, filepath.Clean(path))
}
