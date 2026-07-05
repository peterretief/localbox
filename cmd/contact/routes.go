package main

import "net/http"

func routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/contact", contactHandler)
	mux.HandleFunc("/api/domain/check", domainCheckHandler)
	mux.HandleFunc("/api/domain/search", domainSearchHandler)
	return mux
}
