package main

import "net/http"

func routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/contact", contactHandler)
	mux.HandleFunc("/contact.html", contactHandler)
	mux.HandleFunc("/waitlist", waitlistHandler)
	mux.HandleFunc("/order", orderHandler)
	mux.HandleFunc("/order/login", orderLoginHandler)
	mux.HandleFunc("/order/logout", orderLogoutHandler)
	mux.HandleFunc("/order/progress", orderProgressHandler)
	mux.HandleFunc("/api/domain/check", domainCheckHandler)
	mux.HandleFunc("/api/domain/search", domainSearchHandler)
	return mux
}
