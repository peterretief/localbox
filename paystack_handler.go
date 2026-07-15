package main

import (
	"encoding/json"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
)

func paystackReturnHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	reference := r.URL.Query().Get("reference")
	if reference == "" {
		http.Error(w, "Missing payment reference", http.StatusBadRequest)
		return
	}
	orderID, err := orderIDForPaymentReference(r.Context(), reference)
	if err != nil {
		log.Printf("Paystack return order lookup failed for %s: %v", reference, err)
	}
	if err := verifyPaystackPayment(r.Context(), reference); err != nil {
		log.Printf("Paystack return verification failed for %s: %v", reference, err)
		if retryURL := failedPaymentRegisterURL(orderID); retryURL != "" {
			http.Redirect(w, r, retryURL, http.StatusSeeOther)
			return
		}
		renderPaystackResultPage(w, "Payment not completed", "The payment was not completed or was declined. You can return to the order progress page and try again.", orderID)
		return
	}
	renderPaystackResultPage(w, "Payment verified", "Your payment has been received. You can return to your order progress page.", orderID)
}

func failedPaymentRegisterURL(orderID string) string {
	if orderID == "" {
		return ""
	}
	order, err := loadOrderDetail(orderID)
	if err != nil {
		return ""
	}
	domain, err := normalizePaymentDomain(order.Domain)
	if err != nil {
		return ""
	}
	next := paymentStartPath(domain, "")
	values := url.Values{}
	values.Set("payment", "failed")
	values.Set("next", next)
	return "/register?" + values.Encode()
}

func renderPaystackResultPage(w http.ResponseWriter, title, message, orderID string) {
	title = html.EscapeString(title)
	message = html.EscapeString(message)
	orderID = html.EscapeString(orderID)
	progressLink := "/order/login"
	if orderID != "" {
		progressLink += "?order_id=" + url.QueryEscape(orderID)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><meta charset="UTF-8"><title>` + title + `</title></head>
<body><main style="max-width:640px;margin:40px auto;font-family:sans-serif;">
<h1>` + title + `</h1>
<p>` + message + `</p>
` + orderReferenceHTML(orderID) + `
<p><a href="` + progressLink + `">View order progress</a></p>
</main></body></html>`))
}

func orderReferenceHTML(orderID string) string {
	if orderID == "" {
		return ""
	}
	return `<p><strong>Order reference:</strong> <code>` + orderID + `</code></p>`
}

func paystackWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, err := loadPaystackConfig(paystackConfigFile)
	if err != nil || cfg.validate() != nil {
		log.Printf("Paystack webhook config error: %v", err)
		http.Error(w, "Payment integration unavailable", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		http.Error(w, "Read error", http.StatusInternalServerError)
		return
	}
	if !validPaystackSignature(cfg.SecretKey, body, r.Header.Get("x-paystack-signature")) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}
	var event paystackWebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	switch event.Event {
	case "charge.success":
		if err := verifyPaystackPayment(r.Context(), event.Data.Reference); err != nil {
			log.Printf("Paystack webhook verification failed for %s: %v", event.Data.Reference, err)
			http.Error(w, "Verification failed", http.StatusBadRequest)
			return
		}
	case "charge.failed":
		if err := markPaystackPaymentFailed(event.Data.Reference, event.Event, "Paystack reported charge.failed", string(body)); err != nil {
			log.Printf("Paystack webhook failed-payment update failed for %s: %v", event.Data.Reference, err)
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK\n"))
}
