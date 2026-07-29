package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type AccountDomainsData struct {
	User   string
	Email  string
	Orders []AccountDomainOrder
	Error  string
}

type AccountDomainOrder struct {
	ID          string
	Domain      string
	Status      string
	StatusLabel string
	Email       string
	Amount      string
	PaymentURL  string
	WebsiteURL  string
	AdminURL    string
	CanPay      bool
	CanResume   bool
	CanHost     bool
	CreatedAt   string
	UpdatedAt   string
}

type AccountOrderData struct {
	Email       string
	Order       *OrderDetail
	StatusLabel string
	WantedEmail string
	Amount      string
	AdminURL    string
	CanPay      bool
	CanResume   bool
	CanHost     bool
	Success     string
	Error       string
}

func accountDomainsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s := getRegisteredSession(r)
	if s == nil {
		http.Redirect(w, r, "/login?public=1&next=/account/domains", http.StatusFound)
		return
	}
	if userExists(s.User, s.Domain) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	profile, err := loadPublicUserProfile(s.User, s.Domain)
	if err != nil {
		http.Redirect(w, r, "/login?public=1&next=/account/domains", http.StatusFound)
		return
	}
	data := AccountDomainsData{
		User:  profile.Username,
		Email: profile.Email,
	}
	orders, err := accountDomainOrdersForEmail(profile.Email)
	if err != nil {
		data.Error = err.Error()
	} else {
		data.Orders = orders
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl := template.Must(template.ParseFiles("templates/account_domains.html"))
	_ = tmpl.Execute(w, data)
}

func accountOrderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s := getRegisteredSession(r)
	if s == nil {
		http.Redirect(w, r, "/login?public=1&next="+url.QueryEscape("/account/order?id="+r.URL.Query().Get("id")), http.StatusFound)
		return
	}
	if userExists(s.User, s.Domain) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	profile, err := loadPublicUserProfile(s.User, s.Domain)
	if err != nil {
		http.Redirect(w, r, "/login?public=1&next=/account/domains", http.StatusFound)
		return
	}
	orderID := strings.TrimSpace(r.URL.Query().Get("id"))
	if orderID == "" {
		http.Redirect(w, r, "/account/domains", http.StatusFound)
		return
	}
	order, err := loadOrderDetail(orderID)
	if err != nil || !strings.EqualFold(strings.TrimSpace(order.Email), strings.TrimSpace(profile.Email)) || order.ArchivedAt != "" {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", http.StatusBadRequest)
			return
		}
		switch r.FormValue("action") {
		case "start_localbox_hosting":
			if err := startLocalBoxHostingForAccountOrder(r.Context(), order.ID, profile.Email); err != nil {
				http.Redirect(w, r, "/account/order?id="+url.QueryEscape(order.ID)+"&error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, "/account/order?id="+url.QueryEscape(order.ID)+"&success=localbox_hosting_started", http.StatusSeeOther)
			return
		default:
			http.Error(w, "Unknown action", http.StatusBadRequest)
			return
		}
	}
	data := AccountOrderData{
		Email:       profile.Email,
		Order:       order,
		StatusLabel: accountOrderStatusLabel(order.Status),
		WantedEmail: accountWantedEmailLabel(order.WantedEmail),
		Amount:      accountOrderAmountLabel(order.QuotedAmountCents, order.PaymentCurrency),
		AdminURL:    accountOrderAdminLoginURL(order.OrderSummary),
		CanPay:      accountOrderCanPay(order.OrderSummary),
		CanResume:   accountOrderCanResume(order.OrderSummary),
		CanHost:     accountOrderCanStartHosting(order.OrderSummary),
		Success:     accountOrderSuccessMessage(r.URL.Query().Get("success")),
		Error:       strings.TrimSpace(r.URL.Query().Get("error")),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl := template.Must(template.ParseFiles("templates/account_order.html"))
	_ = tmpl.Execute(w, data)
}

func accountDomainOrdersForEmail(email string) ([]AccountDomainOrder, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, nil
	}
	summaries, err := loadOrderSummaries()
	if err != nil {
		return nil, err
	}
	orders := make([]AccountDomainOrder, 0)
	domainIndex := make(map[string]int)
	for _, order := range summaries {
		if order.ArchivedAt != "" || !strings.EqualFold(strings.TrimSpace(order.Email), email) {
			continue
		}
		row := AccountDomainOrder{
			ID:          order.ID,
			Domain:      order.Domain,
			Status:      order.Status,
			StatusLabel: accountOrderStatusLabel(order.Status),
			Email:       accountWantedEmailLabel(order.WantedEmail),
			Amount:      accountOrderAmountLabel(order.QuotedAmountCents, order.PaymentCurrency),
			PaymentURL:  order.PaymentURL,
			WebsiteURL:  order.WebsiteURL,
			AdminURL:    accountOrderAdminLoginURL(order),
			CanPay:      accountOrderCanPay(order),
			CanResume:   accountOrderCanResume(order),
			CanHost:     accountOrderCanStartHosting(order),
			CreatedAt:   order.CreatedAt,
			UpdatedAt:   order.UpdatedAt,
		}
		domainKey := strings.ToLower(strings.TrimSpace(order.Domain))
		if existingIndex, ok := domainIndex[domainKey]; ok {
			if accountOrderPriority(row.Status) > accountOrderPriority(orders[existingIndex].Status) {
				orders[existingIndex] = row
			}
			continue
		}
		domainIndex[domainKey] = len(orders)
		orders = append(orders, row)
	}
	return orders, nil
}

func accountWantedEmailLabel(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "None requested"
	}
	return email
}

func accountOrderAmountLabel(cents int, currency string) string {
	if cents <= 0 {
		return "To be confirmed"
	}
	currency = cleanCurrency(currency)
	if currency == "" || currency == "ZAR" {
		return formatRand(cents)
	}
	return currency + " " + formatCents(cents)
}

func formatCents(cents int) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

func accountOrderStatusLabel(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "new", "pending_review":
		return "Awaiting review"
	case "quoted":
		return "Ready for payment"
	case "paid":
		return "Paid"
	case "registered":
		return "Registered"
	case "site_live":
		return "Live"
	case "cancelled":
		return "Cancelled"
	default:
		if strings.TrimSpace(status) == "" {
			return "In progress"
		}
		return strings.ReplaceAll(status, "_", " ")
	}
}

func accountOrderCanPay(order OrderSummary) bool {
	status := strings.TrimSpace(strings.ToLower(order.Status))
	return order.PaymentURL != "" && (status == "quoted" || status == "pending_payment")
}

func accountOrderCanResume(order OrderSummary) bool {
	switch strings.TrimSpace(strings.ToLower(order.Status)) {
	case "paid", "registered", "site_live", "cancelled", "payment_failed":
		return false
	default:
		return true
	}
}

func accountOrderCanStartHosting(order OrderSummary) bool {
	if strings.TrimSpace(order.WantedEmail) != "" {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(order.Status)) {
	case "paid", "registered":
		return true
	default:
		return false
	}
}

func accountOrderAdminLoginURL(order OrderSummary) string {
	domain := normalizeDomain(order.Domain)
	if !safeDomain(domain) || strings.TrimSpace(order.WantedEmail) == "" {
		return ""
	}
	user, mailDomain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(order.WantedEmail)), "@")
	if !ok {
		return ""
	}
	user = normalizeMailboxUser(user)
	mailDomain = normalizeDomain(mailDomain)
	if !safeMailboxUser(user) || mailDomain != domain || !isDomainAdmin(user, domain) {
		return ""
	}
	values := url.Values{}
	values.Set("domain", domain)
	values.Set("username", user)
	return "https://" + domain + "/login?" + values.Encode()
}

func accountOrderSuccessMessage(value string) string {
	switch strings.TrimSpace(value) {
	case "localbox_hosting_started":
		return "LocalBox hosting has started. The starter website, first email address, and domain admin login are being prepared."
	default:
		return ""
	}
}

func accountOrderPriority(status string) int {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "site_live":
		return 60
	case "registered":
		return 50
	case "paid":
		return 40
	case "quoted", "pending_payment":
		return 30
	case "pending_review", "new":
		return 20
	case "cancelled", "payment_failed":
		return 10
	default:
		return 0
	}
}

func startLocalBoxHostingForAccountOrder(ctx context.Context, orderID, accountEmail string) error {
	order, err := loadOrderDetail(orderID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(order.Email), strings.TrimSpace(accountEmail)) {
		return fmt.Errorf("order not found")
	}
	if order.ArchivedAt != "" {
		return fmt.Errorf("order is archived")
	}
	if !accountOrderCanStartHosting(order.OrderSummary) {
		return fmt.Errorf("LocalBox hosting can start after payment, and only when no first mailbox has been requested yet")
	}
	domain := normalizeDomain(order.Domain)
	if !safeDomain(domain) {
		return fmt.Errorf("invalid domain")
	}
	wantedEmail := "info@" + domain
	websiteURL := strings.TrimSpace(order.WebsiteURL)
	if websiteURL == "" {
		websiteURL = "https://" + domain + "/"
	}
	if err := updateOrderFields(order.ID, order.Status, "LocalBox hosting requested from customer account.", map[string]any{
		"wanted_email": wantedEmail,
		"website_url":  websiteURL,
	}); err != nil {
		return err
	}
	if err := addDomain(domain); err != nil {
		return err
	}
	if err := createStarterWebsite(domain, order.FullName, time.Now().UTC()); err != nil {
		return err
	}
	if err := createFirstOrderMailbox(ctx, order.ID); err != nil && !strings.Contains(err.Error(), "first mailbox already exists") {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return updateOrderFields(order.ID, "site_live", "LocalBox hosting started: starter website, first mailbox, and domain admin login prepared.", map[string]any{
		"website_url":       websiteURL,
		"site_live_sent_at": now,
	})
}
