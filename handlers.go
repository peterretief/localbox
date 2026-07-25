package main

import (
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LoginPageData struct {
	Error       string
	Username    string
	Domain      string
	Domains     []string
	Next        string
	RegisterURL string
	CanRegister bool
	Public      bool
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		next := safeRedirectPath(r.URL.Query().Get("next"))
		public := publicLoginRequest(r, next)
		tmpl := template.Must(template.ParseFiles("templates/login.html"))
		tmpl.Execute(w, LoginPageData{
			Domain:      lastLoginDomain(r),
			Domains:     loginFormDomains(public),
			Next:        next,
			RegisterURL: loginRegisterURL(r, next),
			CanRegister: publicRegistrationAllowed(r),
			Public:      public,
		})
		return
	}

	rawUsername := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	username := rawUsername
	domain := strings.ToLower(strings.TrimSpace(r.FormValue("domain")))
	password := r.FormValue("password")
	next := safeRedirectPath(r.FormValue("next"))
	public := publicLoginRequest(r, next)
	if public && domain == "" {
		domain = registrationDomain(r)
	}
	username, domain = normalizeLoginIdentity(username, domain)
	if domain != "" {
		rememberLoginDomain(w, domain)
	}
	if authenticate(username, domain, password) {
		if next == "" {
			next = "/dashboard"
		}
		completeLogin(w, r, username, domain, "", next)
		return
	}
	if ok, currency := authenticatePublicUserWithCurrency(username, domain, password); ok {
		if next == "" {
			next = "/order/login"
		}
		completeLogin(w, r, username, domain, currency, next)
		return
	}

	tmpl := template.Must(template.ParseFiles("templates/login.html"))
	tmpl.Execute(w, LoginPageData{
		Error:       "Invalid credentials",
		Username:    rawUsername,
		Domain:      domain,
		Domains:     loginFormDomains(public),
		Next:        next,
		RegisterURL: loginRegisterURL(r, next),
		CanRegister: publicRegistrationAllowed(r),
		Public:      public,
	})
}

func normalizeLoginIdentity(username, domain string) (string, string) {
	username = normalizeMailboxUser(username)
	domain = normalizeDomain(domain)
	local, mailDomain, ok := strings.Cut(username, "@")
	if !ok {
		return username, domain
	}
	local = normalizeMailboxUser(local)
	mailDomain = normalizeDomain(mailDomain)
	if !safeMailboxUser(local) || !safeDomain(mailDomain) {
		return username, domain
	}
	return local, mailDomain
}

func publicLoginFlow(next string) bool {
	return next == "/shopping.html" || strings.HasPrefix(next, "/shopping.html?") ||
		next == "/payment/start" || strings.HasPrefix(next, "/payment/start?")
}

func publicLoginRequest(r *http.Request, next string) bool {
	return publicLoginFlow(next) || r.URL.Query().Get("public") == "1" || r.FormValue("public") == "1"
}

func completeLogin(w http.ResponseWriter, r *http.Request, username, domain, currency, next string) {
	s := &Session{User: username, Domain: domain, Currency: currency}
	if requestHost(r) == primaryLocalBoxHost && safeDomain(domain) && domain != primaryLocalBoxHost {
		redirectWithSessionHandoff(w, r, s, next)
		return
	}
	token := newSessionWithCurrency(username, domain, currency)
	setSessionCookie(w, r, token)
	http.Redirect(w, r, next, http.StatusFound)
}

func loginFormDomains(public bool) []string {
	if public {
		return nil
	}
	return loginDomains()
}

func registerURL(next string) string {
	if next == "" {
		return "/register"
	}
	return "/register?next=" + url.QueryEscape(next)
}

func loginRegisterURL(r *http.Request, next string) string {
	if !publicRegistrationAllowed(r) {
		return ""
	}
	return registerURL(next)
}

func publicRegistrationAllowed(r *http.Request) bool {
	return publicRegistrationAllowedForDomain(registrationDomain(r))
}

func publicRegistrationAllowedForDomain(domain string) bool {
	return normalizeDomain(domain) == primaryLocalBoxHost
}

func loginURL(next string) string {
	if next == "" {
		return "/login"
	}
	return "/login?next=" + url.QueryEscape(next)
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	next := safeRedirectPath(r.URL.Query().Get("next"))
	if next == "" {
		next = safeRedirectPath(r.FormValue("next"))
	}
	domain := registrationDomain(r)
	if formDomain := strings.ToLower(strings.TrimSpace(r.FormValue("domain"))); formDomain != "" {
		domain = formDomain
	}
	if !publicRegistrationAllowedForDomain(domain) {
		http.NotFound(w, r)
		return
	}
	data := PublicRegisterData{
		Currency: "ZAR",
		Country:  "ZA",
		Domain:   domain,
		Next:     next,
		LoginURL: loginURL(next),
	}
	populateRegisterDataFromSession(r, &data)
	if r.URL.Query().Get("payment") == "failed" {
		data.Error = "Payment was not completed. Please check your details and try again."
	}
	if r.URL.Query().Get("verification") == "required" {
		data.Error = "Please verify your email address before continuing to payment."
	}
	if r.Method == http.MethodGet {
		tmpl := template.Must(template.ParseFiles("templates/register.html"))
		tmpl.Execute(w, data)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data.Username = strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	data.Email = strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	data.FullName = strings.TrimSpace(r.FormValue("full_name"))
	data.Phone = strings.TrimSpace(r.FormValue("phone"))
	data.PhysicalAddress = strings.TrimSpace(r.FormValue("physical_address"))
	data.AddressLine1 = strings.TrimSpace(r.FormValue("address_line_1"))
	data.AddressLine2 = strings.TrimSpace(r.FormValue("address_line_2"))
	data.City = strings.TrimSpace(r.FormValue("city"))
	data.Province = strings.TrimSpace(r.FormValue("province"))
	data.PostalCode = strings.TrimSpace(r.FormValue("postal_code"))
	data.Country = strings.ToUpper(strings.TrimSpace(r.FormValue("country")))
	if data.Country == "" {
		data.Country = "ZA"
	}
	data.PhysicalAddress = composePublicUserPhysicalAddress(publicUserProfileInput{
		AddressLine1: data.AddressLine1,
		AddressLine2: data.AddressLine2,
		City:         data.City,
		Province:     data.Province,
		PostalCode:   data.PostalCode,
		Country:      data.Country,
	})
	data.Currency = cleanCurrency(r.FormValue("currency"))
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")
	session := getRegisteredSession(r)
	data.SignedIn = session != nil && session.Domain == data.Domain && session.User == data.Username
	switch {
	case data.Domain == "":
		data.Error = "Domain required."
	case data.SignedIn:
		if err := updatePublicUserProfileDetails(publicUserProfileInput{
			Domain:          data.Domain,
			Username:        data.Username,
			Email:           data.Email,
			FullName:        data.FullName,
			Phone:           data.Phone,
			PhysicalAddress: data.PhysicalAddress,
			AddressLine1:    data.AddressLine1,
			AddressLine2:    data.AddressLine2,
			City:            data.City,
			Province:        data.Province,
			PostalCode:      data.PostalCode,
			Country:         data.Country,
			Currency:        data.Currency,
		}); err != nil {
			data.Error = err.Error()
		}
	case password != confirmPassword:
		data.Error = "Passwords do not match."
	default:
		if err := registerPublicUserProfileDetails(publicUserProfileInput{
			Domain:          data.Domain,
			Username:        data.Username,
			Email:           data.Email,
			FullName:        data.FullName,
			Phone:           data.Phone,
			PhysicalAddress: data.PhysicalAddress,
			AddressLine1:    data.AddressLine1,
			AddressLine2:    data.AddressLine2,
			City:            data.City,
			Province:        data.Province,
			PostalCode:      data.PostalCode,
			Country:         data.Country,
			Password:        password,
			Currency:        data.Currency,
		}); err != nil {
			data.Error = err.Error()
		}
	}
	if data.Error != "" {
		tmpl := template.Must(template.ParseFiles("templates/register.html"))
		tmpl.Execute(w, data)
		return
	}

	if !data.SignedIn {
		verificationToken, err := issuePublicUserEmailVerification(data.Username, data.Domain)
		if err != nil {
			data.Error = err.Error()
			tmpl := template.Must(template.ParseFiles("templates/register.html"))
			tmpl.Execute(w, data)
			return
		}
		if err := sendPublicUserVerificationEmail(r, data, verificationToken, next); err != nil {
			data.Error = "Could not send verification email: " + err.Error()
			tmpl := template.Must(template.ParseFiles("templates/register.html"))
			tmpl.Execute(w, data)
			return
		}
		data.Notice = "Check your email and click the verification link. You can then make purchases on this site."
		tmpl := template.Must(template.ParseFiles("templates/register.html"))
		tmpl.Execute(w, data)
		return
	}
	profile, err := loadPublicUserProfile(data.Username, data.Domain)
	if err != nil {
		data.Error = err.Error()
		tmpl := template.Must(template.ParseFiles("templates/register.html"))
		tmpl.Execute(w, data)
		return
	}
	if !publicUserEmailVerified(profile) {
		verificationToken, err := issuePublicUserEmailVerification(data.Username, data.Domain)
		if err != nil {
			data.Error = err.Error()
			tmpl := template.Must(template.ParseFiles("templates/register.html"))
			tmpl.Execute(w, data)
			return
		}
		if err := sendPublicUserVerificationEmail(r, data, verificationToken, next); err != nil {
			data.Error = "Could not send verification email: " + err.Error()
			tmpl := template.Must(template.ParseFiles("templates/register.html"))
			tmpl.Execute(w, data)
			return
		}
		data.Notice = "Check your email and click the verification link. You can then make purchases on this site."
		tmpl := template.Must(template.ParseFiles("templates/register.html"))
		tmpl.Execute(w, data)
		return
	}

	rememberLoginDomain(w, data.Domain)
	token := newSessionWithCurrency(data.Username, data.Domain, data.Currency)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
	})
	if next == "" {
		next = "/shopping.html"
	}
	http.Redirect(w, r, next, http.StatusFound)
}

func publicEmailVerifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	next := safeRedirectPath(r.URL.Query().Get("next"))
	profile, err := verifyPublicUserEmailToken(r.URL.Query().Get("token"))
	if err != nil {
		data := PublicRegisterData{
			Currency: "ZAR",
			Country:  "ZA",
			Domain:   registrationDomain(r),
			Next:     next,
			LoginURL: loginURL(next),
			Error:    err.Error(),
		}
		tmpl := template.Must(template.ParseFiles("templates/register.html"))
		tmpl.Execute(w, data)
		return
	}
	rememberLoginDomain(w, profile.Domain)
	token := newSessionWithCurrency(profile.Username, profile.Domain, cleanCurrency(profile.Currency))
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
	})
	if next == "" {
		next = "/shopping.html"
	}
	http.Redirect(w, r, next, http.StatusFound)
}

func sendPublicUserVerificationEmail(r *http.Request, data PublicRegisterData, token, next string) error {
	values := url.Values{}
	values.Set("token", token)
	if next != "" {
		values.Set("next", next)
	}
	link := publicRequestBaseURL(r) + "/register/verify?" + values.Encode()
	domain := strings.TrimSpace(data.Domain)
	if domain == "" {
		domain = "this domain"
	}
	body := "Welcome to LocalBox, " + data.FullName + ".\n\n" +
		"You are registered as a user for " + domain + ". Please open this link to confirm the email address and continue. You can then make domain orders if you wish:\n\n" +
		link + "\n\n" +
		"If you did not create this account, you can ignore this email.\n"
	htmlBody := "<!doctype html><html><body>" +
		"<p>Welcome to LocalBox, " + html.EscapeString(data.FullName) + ".</p>" +
		"<p>You are registered as a user for <strong>" + html.EscapeString(domain) + "</strong>. Please open this link to confirm the email address and continue. You can then make domain orders if you wish.</p>" +
		"<p><a href=\"" + html.EscapeString(link) + "\">Continue to LocalBox</a></p>" +
		"<p>If you did not create this account, you can ignore this email.</p>" +
		"</body></html>"
	return sendOrderNotificationEmail(orderEmailMessage{
		To:       data.Email,
		Subject:  "Welcome to LocalBox",
		Body:     body,
		HTMLBody: htmlBody,
	})
}

func publicRequestBaseURL(r *http.Request) string {
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "https"
		}
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = registrationDomain(r)
	}
	return scheme + "://" + host
}

func populateRegisterDataFromSession(r *http.Request, data *PublicRegisterData) {
	s := getRegisteredSession(r)
	if s == nil {
		return
	}
	profile, err := loadPublicUserProfile(s.User, s.Domain)
	if err != nil {
		return
	}
	data.SignedIn = true
	data.Domain = profile.Domain
	data.Username = profile.Username
	data.Email = profile.Email
	data.FullName = profile.FullName
	data.Phone = profile.Phone
	data.PhysicalAddress = profile.PhysicalAddress
	data.AddressLine1 = profile.AddressLine1
	data.AddressLine2 = profile.AddressLine2
	data.City = profile.City
	data.Province = profile.Province
	data.PostalCode = profile.PostalCode
	data.Country = profile.Country
	if data.Country == "" {
		data.Country = "ZA"
	}
	data.Currency = cleanCurrency(profile.Currency)
}

func registrationDomain(r *http.Request) string {
	if s := getSession(r); s != nil && s.Domain != "" {
		return strings.ToLower(strings.TrimSpace(s.Domain))
	}
	if domain := requestSiteDomain(r); domain != "" {
		return strings.ToLower(strings.TrimSpace(domain))
	}
	return strings.ToLower(strings.TrimSpace(lastLoginDomain(r)))
}

func loginDomains() []string {
	domains, err := loadDomains()
	if err != nil {
		return nil
	}
	return domains
}

func lastLoginDomain(r *http.Request) string {
	c, err := r.Cookie("localbox_login_domain")
	if err != nil {
		return ""
	}
	return c.Value
}

func rememberLoginDomain(w http.ResponseWriter, domain string) {
	if domain == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "localbox_login_domain",
		Value:    domain,
		Path:     "/login",
		Expires:  time.Now().Add(365 * 24 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	deleteSession(r)
	http.SetCookie(w, &http.Cookie{
		Name:    cookieName,
		Value:   "",
		Path:    "/",
		Expires: time.Now().Add(-1 * time.Hour),
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func publicLogoutHandler(w http.ResponseWriter, r *http.Request) {
	deleteSession(r)
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	http.SetCookie(w, &http.Cookie{
		Name:    cookieName,
		Value:   "",
		Path:    "/",
		Expires: time.Now().Add(-1 * time.Hour),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && strings.HasSuffix(strings.ToLower(r.URL.Path), ".html") {
		siteHTMLPageHandler(w, r)
		return
	}
	inboxHandler(w, r)
}

func siteHTMLPageHandler(w http.ResponseWriter, r *http.Request) {
	name := cleanUploadedHTMLName(strings.TrimPrefix(r.URL.Path, "/"))
	if !allowedUploadedHTMLName(name) {
		http.NotFound(w, r)
		return
	}
	if name == shoppingPageName {
		s := getRegisteredSession(r)
		if s == nil {
			redirectToRegister(w, r)
			return
		}
		serveShoppingCartPage(w, s)
		return
	}
	domain := requestSiteDomain(r)
	if domain == "" {
		http.NotFound(w, r)
		return
	}
	serveSiteHTMLFile(w, r, filepath.Join(domainSiteBase(domain), name), getSession(r))
}

func serveShoppingCartPage(w http.ResponseWriter, s *Session) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	products, addOns := loadSiteCatalog(s.Domain)
	menu := loadSiteMenuItems(s.Domain)
	fmt.Fprint(w, newShoppingCartComponentManager(s.Domain, s.User, s.Currency, menu, products, addOns).Render())
}

func serveSiteHTMLFile(w http.ResponseWriter, r *http.Request, path string, s *Session) {
	data, err := os.ReadFile(path)
	if err != nil {
		http.ServeFile(w, r, path)
		return
	}
	if s != nil && registeredUserExists(s.User, s.Domain) {
		data = injectPublicSessionBar(data, s, r.URL.RequestURI(), publicRegistrationAllowed(r))
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
	} else {
		data = injectPublicSessionBar(data, nil, r.URL.RequestURI(), publicRegistrationAllowed(r))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func injectPublicSessionBar(data []byte, s *Session, next string, canRegister bool) []byte {
	next = safeRedirectPath(next)
	if next == "" {
		next = "/"
	}
	escapedNext := template.URLQueryEscaper(next)
	content := `<a href="/login?public=1&next=` + escapedNext + `" style="color:#fff;text-decoration:underline;">Sign in</a>`
	if canRegister {
		content = `<a href="/register?next=` + escapedNext + `" style="color:#fff;text-decoration:underline;">Register</a>` + content
	}
	if s != nil && strings.TrimSpace(s.User) != "" {
		label := strings.TrimSpace(s.User)
		if userExists(s.User, s.Domain) {
			content = `<span>Logged in as ` + template.HTMLEscapeString(label) + `</span><a href="/dashboard" style="color:#fff;text-decoration:underline;">Dashboard</a><a href="/public/logout" style="color:#fff;text-decoration:underline;">Logout</a>`
		} else {
			content = `<span>Logged in as ` + template.HTMLEscapeString(label) + `</span><a href="/order/login" style="color:#fff;text-decoration:underline;">Orders</a><a href="/public/logout" style="color:#fff;text-decoration:underline;">Logout</a>`
		}
	}
	bar := `<div class="localbox-session-bar" style="box-sizing:border-box;width:100%;background:#1a1a2e;color:#fff;padding:10px 18px;font:14px Arial,sans-serif;display:flex;gap:14px;align-items:center;justify-content:flex-end;position:relative;z-index:9999;">` + content + `</div>`
	htmlText := string(data)
	lower := strings.ToLower(htmlText)
	if i := strings.Index(lower, "<body"); i >= 0 {
		if end := strings.Index(lower[i:], ">"); end >= 0 {
			insert := i + end + 1
			return []byte(htmlText[:insert] + bar + htmlText[insert:])
		}
	}
	return append([]byte(bar), data...)
}

func redirectToRegister(w http.ResponseWriter, r *http.Request) {
	next := safeRedirectPath(r.URL.RequestURI())
	target := "/register"
	if next != "" {
		target += "?next=" + url.QueryEscape(next)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	next := safeRedirectPath(r.URL.RequestURI())
	target := "/login"
	if next != "" {
		target += "?next=" + url.QueryEscape(next)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func safeRedirectPath(next string) string {
	next = strings.TrimSpace(next)
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, "\\") {
		return ""
	}
	return next
}

func getRegisteredSession(r *http.Request) *Session {
	s := getSession(r)
	if s == nil {
		return nil
	}
	if !registeredUserExists(s.User, s.Domain) {
		return nil
	}
	return s
}

func registeredUserExists(username, domain string) bool {
	users, err := loadUsers()
	if err != nil {
		return false
	}
	for _, user := range users {
		if user.Username == username && user.Domain == domain {
			return true
		}
	}
	return publicUserExists(username, domain)
}

func requestSiteDomain(r *http.Request) string {
	if s := getSession(r); s != nil {
		return s.Domain
	}
	return requestHost(r)
}

type PasswordPageData struct {
	Error   string
	Success string
}

func passwordHandler(w http.ResponseWriter, r *http.Request) {
	s := requireMailboxSession(w, r)
	if s == nil {
		return
	}

	if r.Method == http.MethodGet {
		tmpl := template.Must(template.ParseFiles("templates/password.html"))
		tmpl.Execute(w, PasswordPageData{})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	data := PasswordPageData{}
	switch {
	case len(newPassword) < 8:
		data.Error = "New password must be at least 8 characters."
	case newPassword != confirmPassword:
		data.Error = "New passwords do not match."
	default:
		if err := changePassword(s.User, s.Domain, currentPassword, newPassword); err != nil {
			data.Error = err.Error()
		} else {
			data.Success = "Password updated."
		}
	}

	tmpl := template.Must(template.ParseFiles("templates/password.html"))
	tmpl.Execute(w, data)
}

func inboxHandler(w http.ResponseWriter, r *http.Request) {
	s := requireMailboxSession(w, r)
	if s == nil {
		return
	}
	if r.Method == http.MethodPost {
		if r.FormValue("action") != "create_folder" {
			http.Error(w, "unknown mail action", http.StatusBadRequest)
			return
		}
		folder, err := cleanMailFolderName(r.FormValue("folder"))
		if err != nil || folder == "" || folder == sentFolderName {
			http.Redirect(w, r, inboxBasePath(r), http.StatusFound)
			return
		}
		if err := ensureMailFolder(s.Domain, s.User, folder); err != nil {
			http.Error(w, "Could not create folder", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, inboxFolderURLForBase(inboxBasePath(r), folder, 1), http.StatusFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	view := r.URL.Query().Get("view")
	folder, err := cleanMailFolderName(r.URL.Query().Get("folder"))
	if err != nil {
		http.Error(w, "Invalid folder", http.StatusBadRequest)
		return
	}
	maildir := maildirPath(s.Domain, s.User)
	var messages []*Message
	if folder != "" {
		maildir, err = mailboxFolderPath(s.Domain, s.User, folder)
		if err != nil {
			http.Error(w, "Invalid folder", http.StatusBadRequest)
			return
		}
		messages, err = listMessagesInFolder(maildir, folder)
		if err != nil {
			http.Error(w, "Error reading mailbox", http.StatusInternalServerError)
			return
		}
	} else if view == "sent" {
		folder = sentFolderName
		messages, err = listMessagesInFolder(sentMaildirPath(s.Domain, s.User), folder)
		if err != nil {
			http.Error(w, "Error reading mailbox", http.StatusInternalServerError)
			return
		}
		legacyMessages, legacyErr := listMessagesInFolder(legacySentMaildirPath(s.Domain, s.User), folder)
		if legacyErr == nil {
			messages = append(messages, legacyMessages...)
		}
	} else {
		messages, err = listMessagesInFolder(maildir, folder)
		if err != nil {
			http.Error(w, "Error reading mailbox", http.StatusInternalServerError)
			return
		}
	}

	data := newInboxView(messages, s, r)

	tmpl := template.Must(template.ParseFiles("templates/inbox.html"))
	tmpl.Execute(w, data)
}

func mailHandler(w http.ResponseWriter, r *http.Request) {
	s := getSession(r)
	if s == nil {
		next := "/mail"
		if r.URL.RawQuery != "" {
			next += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusFound)
		return
	}
	if !userExists(s.User, s.Domain) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	inboxHandler(w, r)
}

func composeHandler(w http.ResponseWriter, r *http.Request) {
	s := requireMailboxSession(w, r)
	if s == nil {
		return
	}

	data := ComposePageData{
		To:      r.URL.Query().Get("to"),
		Subject: r.URL.Query().Get("subject"),
		User:    s.User,
		Domain:  s.Domain,
		Access:  accessView(s),
	}

	if r.Method == http.MethodPost {
		data.To = r.FormValue("to")
		data.Subject = r.FormValue("subject")
		data.Body = r.FormValue("body")
		attachment, err := composeAttachment(r)
		if err != nil {
			data.Error = err.Error()
		} else {
			result, err := sendMailMessage(s.User, s.Domain, data.To, data.Subject, data.Body, attachment)
			if err != nil {
				data.Error = err.Error()
			} else {
				data.Success = result
				data.To = ""
				data.Subject = ""
				data.Body = ""
			}
		}
	} else if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tmpl := template.Must(template.ParseFiles("templates/compose.html"))
	tmpl.Execute(w, data)
}

func composeAttachment(r *http.Request) (*OutgoingAttachment, error) {
	file, header, err := r.FormFile("attachment")
	if err == http.ErrMissingFile {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if header.Size > maxComposeAttachmentBytes {
		return nil, fmt.Errorf("attachment must be 10 MB or smaller")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxComposeAttachmentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxComposeAttachmentBytes {
		return nil, fmt.Errorf("attachment must be 10 MB or smaller")
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return &OutgoingAttachment{
		Filename:    filepath.Base(header.Filename),
		ContentType: contentType,
		Data:        data,
	}, nil
}

func messageHandler(w http.ResponseWriter, r *http.Request) {
	s := requireMailboxSession(w, r)
	if s == nil {
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	maildir := maildirPath(s.Domain, s.User)
	folder, err := cleanMailFolderName(r.URL.Query().Get("folder"))
	if err != nil {
		http.Error(w, "Invalid folder", http.StatusBadRequest)
		return
	}
	if folder == sentFolderName {
		maildir = sentMaildirPath(s.Domain, s.User)
	} else if folder != "" {
		maildir, err = mailboxFolderPath(s.Domain, s.User, folder)
		if err != nil {
			http.Error(w, "Invalid folder", http.StatusBadRequest)
			return
		}
	}
	path, err := findMessagePath(s.Domain, s.User, folder, id)
	if err != nil {
		http.Error(w, "Message not found", http.StatusNotFound)
		return
	}

	msg, err := parseMessage(path, false)
	if err != nil {
		http.Error(w, "Message not found", http.StatusNotFound)
		return
	}
	msg.BackURL = "/"
	if folder == sentFolderName {
		msg.BackURL = "/?view=sent"
		msg.ReplyURL = composeURL(msg.To, msg.Subject)
	} else if folder != "" {
		msg.BackURL = inboxFolderURL(folder, 1)
	}
	msg.Folder = folder
	msg.MoveFolders = messageMoveFolders(s.Domain, s.User, folder)

	if folder == "" {
		markAsRead(maildir, id)
	}
	tmpl := template.Must(template.ParseFiles("templates/message.html"))
	tmpl.Execute(w, msg)
}

func messageActionHandler(w http.ResponseWriter, r *http.Request) {
	s := requireMailboxSession(w, r)
	if s == nil {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.FormValue("id")
	folder, err := cleanMailFolderName(r.FormValue("folder"))
	if err != nil {
		http.Error(w, "Invalid folder", http.StatusBadRequest)
		return
	}
	action := r.FormValue("action")
	switch action {
	case "archive":
		if folder != archiveFolderName {
			err = moveMessageToFolder(s.Domain, s.User, folder, id, archiveFolderName)
		}
	case "move":
		targetFolder, targetErr := cleanMailFolderName(r.FormValue("target_folder"))
		if targetErr != nil || targetFolder == "" || targetFolder == sentFolderName || targetFolder == folder {
			http.Error(w, "Invalid target folder", http.StatusBadRequest)
			return
		}
		err = moveMessageToFolder(s.Domain, s.User, folder, id, targetFolder)
	case "delete":
		err = deleteMessage(s.Domain, s.User, folder, id)
	default:
		http.Error(w, "unknown mail action", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "Could not update message", http.StatusInternalServerError)
		return
	}
	if folder != "" && folder != sentFolderName {
		http.Redirect(w, r, inboxFolderURL(folder, 1), http.StatusFound)
		return
	}
	if folder == sentFolderName {
		http.Redirect(w, r, "/?view=sent", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func messageMoveFolders(domain, user, current string) []MailFolder {
	folders := listMailFolders(domain, user, current, "/")
	var options []MailFolder
	for _, folder := range folders {
		if folder.Name == current {
			continue
		}
		options = append(options, folder)
	}
	return options
}
