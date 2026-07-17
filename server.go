package main

import "net/http"

func newServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin", adminHandler)
	mux.HandleFunc("/admin/mailing-list.csv", mailingListExportHandler)
	mux.HandleFunc("/attachment", attachmentHandler)
	mux.HandleFunc("/certificate", certificateHandler)
	mux.HandleFunc("/compose", composeHandler)
	mux.HandleFunc("/dashboard", dashboardHandler)
	mux.HandleFunc("/forgot-password", forgotPasswordHandler)
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ingest", ingestHandler)
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/logout", logoutHandler)
	mux.HandleFunc("/mail", mailHandler)
	mux.HandleFunc("/message", messageHandler)
	mux.HandleFunc("/message/action", messageActionHandler)
	mux.HandleFunc("/orders", ordersHandler)
	mux.HandleFunc("/page", pageHandler)
	mux.HandleFunc("/page/html", pageHTMLHandler)
	mux.HandleFunc("/page/images", pageImagesHandler)
	mux.HandleFunc("/payment/start", paymentStartHandler)
	mux.HandleFunc("/payment/paystack/return", paystackReturnHandler)
	mux.HandleFunc("/payment/paystack/webhook", paystackWebhookHandler)
	mux.HandleFunc("/password", passwordHandler)
	mux.HandleFunc("/public/logout", publicLogoutHandler)
	mux.HandleFunc("/register", registerHandler)
	mux.HandleFunc("/register/verify", publicEmailVerifyHandler)
	mux.HandleFunc("/reset-password", resetPasswordHandler)
	mux.HandleFunc("/system", systemHandler)
	mux.HandleFunc("/system/certificate", systemCertificateHandler)
	mux.HandleFunc("/assets/", assetsHandler)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/", rootHandler)
	return mux
}
