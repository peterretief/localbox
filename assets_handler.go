package main

import (
	"net/http"
	"path/filepath"
	"strings"
)

func assetsHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		http.NotFound(w, r)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	name = filepath.Clean("/" + name)
	if name == "/" || strings.HasPrefix(name, "/..") {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, filepath.Join(domainSiteBase(s.Domain), "assets", strings.TrimPrefix(name, "/")))
}
