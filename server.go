package main

import "net/http"

func newServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/attachment", attachmentHandler)
	mux.HandleFunc("/ingest", ingestHandler)
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/logout", logoutHandler)
	mux.HandleFunc("/message", messageHandler)
	mux.HandleFunc("/", inboxHandler)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	return mux
}
