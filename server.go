package main

import "net/http"

func newServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin", adminHandler)
	mux.HandleFunc("/attachment", attachmentHandler)
	mux.HandleFunc("/compose", composeHandler)
	mux.HandleFunc("/ingest", ingestHandler)
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/logout", logoutHandler)
	mux.HandleFunc("/message", messageHandler)
	mux.HandleFunc("/page", pageHandler)
	mux.HandleFunc("/page/html", pageHTMLHandler)
	mux.HandleFunc("/page/images", pageImagesHandler)
	mux.HandleFunc("/password", passwordHandler)
	mux.HandleFunc("/system", systemHandler)
	mux.HandleFunc("/assets/", assetsHandler)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/", inboxHandler)
	return mux
}
