package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderMarkdownImage(t *testing.T) {
	rendered := renderMarkdown(`![Alt text](https://example.com/image.jpg "Title")`)

	if !strings.Contains(rendered, `<img src="https://example.com/image.jpg" alt="Alt text" title="Title" loading="lazy">`) {
		t.Fatalf("rendered markdown did not include expected image: %s", rendered)
	}
}

func TestRenderMarkdownLinkedImage(t *testing.T) {
	rendered := renderMarkdown(`[![Alt text](/assets/image.jpg "Title")](/contact.html)`)

	want := `<a href="/contact.html"><img src="/assets/image.jpg" alt="Alt text" title="Title" loading="lazy"></a>`
	if !strings.Contains(rendered, want) {
		t.Fatalf("rendered markdown did not include expected linked image: %s", rendered)
	}
}

func TestRenderHomeHTMLIncludesMenuItems(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	site := domainSiteBase(domain)
	if err := os.MkdirAll(site, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "software.html"), []byte("software"), 0664); err != nil {
		t.Fatal(err)
	}
	if err := recordUploadedHTML(domain, "software.html"); err != nil {
		t.Fatal(err)
	}
	if err := saveSiteMenuItems(domain, []SiteMenuItem{{Name: "software.html", Label: "Software"}}); err != nil {
		t.Fatal(err)
	}

	rendered := string(renderHomeHTML(domain, "# Welcome"))
	if !strings.Contains(rendered, `<a href="/software.html">Software</a>`) {
		t.Fatalf("expected rendered homepage to include menu link, got %s", rendered)
	}
}

func TestRenderHomeHTMLAlwaysUsesInformationPage(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		t.Fatal(err)
	}

	rendered := string(renderHomeHTML(domain, "# Welcome"))
	if strings.Contains(rendered, "LocalBox Shopping Cart") || strings.Contains(rendered, `data-cart-item`) {
		t.Fatalf("expected markdown homepage to stay informational, got %s", rendered)
	}
}

func TestSaveShoppingCartPageCreatesDomainScopedPage(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	domain := "example.com"
	if err := saveSiteCatalog(domain, []cartProduct{
		{
			Name:        "Managed mailbox",
			Description: "Editable mailbox catalog item.",
			Price:       "R99",
			Category:    "Core",
			Image:       "/assets/mailbox.jpg",
			Featured:    true,
		},
		{
			Name:        "Onsite setup",
			Description: "Editable add-on item.",
			Price:       "Quoted",
			Category:    "Add-on",
		},
	}); err != nil {
		t.Fatal(err)
	}

	products, addOns := loadSiteCatalog(domain)
	if len(products) != 1 || products[0].Name != "Managed mailbox" || !products[0].Featured {
		t.Fatalf("unexpected products: %#v", products)
	}
	if products[0].Image != "/assets/mailbox.jpg" {
		t.Fatalf("expected catalog image to persist, got %q", products[0].Image)
	}
	if len(addOns) != 1 || addOns[0].Name != "Onsite setup" {
		t.Fatalf("unexpected add-ons: %#v", addOns)
	}
	db, err := openOrdersDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var savedDomain string
	var savedCount int
	if err := db.QueryRow(`SELECT domain, COUNT(*) FROM shopping_cart_items GROUP BY domain`).Scan(&savedDomain, &savedCount); err != nil {
		t.Fatal(err)
	}
	if savedDomain != domain || savedCount != 2 {
		t.Fatalf("expected SQL cart rows for %s only, got domain=%q count=%d", domain, savedDomain, savedCount)
	}

	if err := saveShoppingCartPage(domain); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(domainSiteBase(domain), shoppingPageName))
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(page)
	if !strings.Contains(rendered, "Managed mailbox") || !strings.Contains(rendered, "Onsite setup") || !strings.Contains(rendered, "/assets/mailbox.jpg") {
		t.Fatalf("expected editable catalog items in generated cart, got %s", rendered)
	}
	if !isUploadedHTML(domain, shoppingPageName) {
		t.Fatal("expected shopping page to be recorded as uploaded HTML")
	}
	menu := loadSiteMenuItems(domain)
	if len(menu) != 1 || menu[0].Name != shoppingPageName || menu[0].Label != "Shopping" {
		t.Fatalf("expected shopping page in menu, got %#v", menu)
	}

	otherDomain := "other.com"
	if _, err := os.Stat(filepath.Join(domainSiteBase(otherDomain), shoppingPageName)); !os.IsNotExist(err) {
		t.Fatalf("expected other domain to have no generated shopping page, err=%v", err)
	}
	otherProducts, otherAddOns := loadSiteCatalog(otherDomain)
	if len(otherProducts) != 0 || len(otherAddOns) != 0 {
		t.Fatalf("expected other domain to have no cart items, got products=%#v addOns=%#v", otherProducts, otherAddOns)
	}
}

func TestGeneratedShoppingPageDoesNotAppearInMenuByDefault(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(domainSiteBase(domain), shoppingPageName), []byte("cart"), 0664); err != nil {
		t.Fatal(err)
	}

	files, err := listSiteHTMLFiles(domain)
	if err != nil {
		t.Fatal(err)
	}
	pages := siteMenuPages(files)
	if len(pages) != 1 || pages[0].Name != shoppingPageName || pages[0].MenuLabel != "Shopping" || pages[0].InMenu {
		t.Fatalf("expected generated shopping page to be available but off by default, got %#v", pages)
	}

	rendered := string(renderHomeHTML(domain, "# Welcome"))
	if strings.Contains(rendered, `<a href="/shopping.html">Shopping</a>`) {
		t.Fatalf("expected homepage menu to omit default shopping link, got %s", rendered)
	}
}

func TestRenderHomeHTMLOmitsStaleMenuItems(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siteMenuPath(domain), []byte("shopping.html\tShopping\n"), 0664); err != nil {
		t.Fatal(err)
	}

	rendered := string(renderHomeHTML(domain, "# Welcome"))
	if strings.Contains(rendered, "shopping.html") || strings.Contains(rendered, "Shopping") {
		t.Fatalf("expected stale shopping menu item to be omitted, got %s", rendered)
	}
}

func TestRootHandlerServesDomainSiteHTMLPage(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })

	domain := "example.com"
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(domainSiteBase(domain), shoppingPageName), []byte("<h1>Shopping</h1>"), 0664); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "http://example.com/shopping.html", nil)
	rr := httptest.NewRecorder()
	rootHandler(rr, req)
	if rr.Code != 302 || !strings.HasPrefix(rr.Header().Get("Location"), "/register?next=") {
		t.Fatalf("expected unauthenticated shopping page to redirect to registration, code=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}

	hash, err := hashPasswordStrong("secret-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveUsers([]User{{Username: "info", Domain: domain, Hash: hash}}); err != nil {
		t.Fatal(err)
	}
	token := newSession("info", domain)
	req = httptest.NewRequest("GET", "http://example.com/shopping.html", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rr = httptest.NewRecorder()
	rootHandler(rr, req)

	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Logged in as info") || !strings.Contains(rr.Body.String(), "LocalBox Shopping Cart") {
		t.Fatalf("expected dynamic shopping page response, code=%d body=%q", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") == "" {
		t.Fatal("expected shopping page to disable caching")
	}
}

func TestMailRouteRedirectsToLoginWithReturnPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/mail?folder=Clients", nil)
	rr := httptest.NewRecorder()

	mailHandler(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rr.Code)
	}
	location := rr.Header().Get("Location")
	if location != "/login?next=%2Fmail%3Ffolder%3DClients" {
		t.Fatalf("unexpected redirect location: %q", location)
	}
}

func TestMailRouteServesInboxForLoggedInUser(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })

	domain := "example.com"
	if err := os.WriteFile(usersFile, []byte("info:example.com:hash\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := ensureMailboxDirs(domain, "info"); err != nil {
		t.Fatal(err)
	}
	token := newSession("info", domain)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/mail", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rr := httptest.NewRecorder()

	mailHandler(rr, req)

	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, "Webmail") {
		t.Fatalf("expected inbox response, code=%d body=%q", rr.Code, rr.Body.String())
	}
	if strings.Contains(body, "<div class=\"nav-title\">Admin</div>") {
		t.Fatalf("webmail route should not show admin navigation, body=%q", body)
	}
	if !strings.Contains(body, `href="/mail?view=new"`) || !strings.Contains(body, `action="/mail"`) {
		t.Fatalf("webmail navigation should stay under /mail, body=%q", body)
	}
}

func TestMailAppRedirectsToDomainWebmailHost(t *testing.T) {
	oldSecretFile := localBoxSSOSecretFile
	localBoxSSOSecretFile = filepath.Join(t.TempDir(), "sso-secret")
	t.Cleanup(func() { localBoxSSOSecretFile = oldSecretFile })
	if err := os.WriteFile(localBoxSSOSecretFile, []byte("test-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })
	if err := os.WriteFile(usersFile, []byte("info:example.com:hash\n"), 0640); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/apps/mail", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("info", "example.com")})
	rr := httptest.NewRecorder()

	mailAppHandler(rr, req)

	location := rr.Header().Get("Location")
	if rr.Code != http.StatusFound || !strings.HasPrefix(location, "https://mail.example.com/localbox/sso?token=") {
		t.Fatalf("expected domain webmail handoff, code=%d location=%q body=%q", rr.Code, location, rr.Body.String())
	}
}

func TestLocalBoxWebmailBaseURLFallsBackForInvalidDomain(t *testing.T) {
	t.Setenv("LOCALBOX_WEBMAIL_URL", "https://mail.localboxmail.co.za/")

	if got := localBoxWebmailBaseURL("not a domain"); got != "https://mail.localboxmail.co.za" {
		t.Fatalf("unexpected fallback webmail URL: %q", got)
	}
}

func TestSiteHTMLPageShowsLoggedInPublicUser(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	domain := "example.com"
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(domainSiteBase(domain), "software.html"), []byte("<!doctype html><html><body><h1>Software</h1></body></html>"), 0664); err != nil {
		t.Fatal(err)
	}
	if err := registerPublicUser(domain, "buyer", "buyer@example.net", "secret-password"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "http://example.com/software.html", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", domain)})
	rr := httptest.NewRecorder()
	rootHandler(rr, req)

	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, "Logged in as buyer") || !strings.Contains(body, "/public/logout") || !strings.Contains(body, "<h1>Software</h1>") {
		t.Fatalf("expected static page with logged-in public user bar, code=%d body=%q", rr.Code, body)
	}
	if rr.Header().Get("Cache-Control") == "" {
		t.Fatal("expected logged-in static page to disable caching")
	}
}

func TestSiteHTMLPageShowsPublicSignInWithoutRegisterOnCustomerDomain(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(domainSiteBase(domain), "software.html"), []byte("<!doctype html><html><body><h1>Software</h1></body></html>"), 0664); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "http://example.com/software.html", nil)
	rr := httptest.NewRecorder()
	rootHandler(rr, req)

	body := rr.Body.String()
	if rr.Code != http.StatusOK || strings.Contains(body, `href="/register?next=%2Fsoftware.html"`) || !strings.Contains(body, `href="/login?public=1&next=%2Fsoftware.html"`) || strings.Contains(body, "Logout") {
		t.Fatalf("expected static customer page with sign-in only, code=%d body=%q", rr.Code, body)
	}
}

func TestSiteHTMLPageShowsRegisterOnPrimaryDomain(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := primaryLocalBoxHost
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(domainSiteBase(domain), "software.html"), []byte("<!doctype html><html><body><h1>Software</h1></body></html>"), 0664); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "http://"+primaryLocalBoxHost+"/software.html", nil)
	rr := httptest.NewRecorder()
	rootHandler(rr, req)

	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, `href="/register?next=%2Fsoftware.html"`) || !strings.Contains(body, `href="/login?public=1&next=%2Fsoftware.html"`) {
		t.Fatalf("expected primary domain page with register and sign-in links, code=%d body=%q", rr.Code, body)
	}
}

func TestIndexHTMLPageShowsLoggedInPublicUser(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	domain := "example.com"
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(domainSiteBase(domain), "index.html"), []byte("<!doctype html><html><body><h1>Home</h1></body></html>"), 0664); err != nil {
		t.Fatal(err)
	}
	if err := registerPublicUser(domain, "buyer", "buyer@example.net", "secret-password"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "http://example.com/index.html", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("buyer", domain)})
	rr := httptest.NewRecorder()
	rootHandler(rr, req)

	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, "Logged in as buyer") || !strings.Contains(body, "<h1>Home</h1>") {
		t.Fatalf("expected index page with logged-in public user bar, code=%d body=%q", rr.Code, body)
	}
}

func TestRegisterHandlerAllowsPrimaryDomain(t *testing.T) {
	oldDB := orderDBFile
	oldSender := sendOrderNotificationEmail
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() {
		orderDBFile = oldDB
		sendOrderNotificationEmail = oldSender
	})
	var sent orderEmailMessage
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		sent = message
		return nil
	}

	form := strings.NewReader("next=%2Fshopping.html&full_name=Buyer+Example&email=buyer%40example.net&phone=%2B27821234567&address_line_1=1+Example+Street&address_line_2=Unit+4&city=Cape+Town&province=Western+Cape&postal_code=8001&country=ZA&username=buyer&currency=USD&password=secret-password&confirm_password=secret-password")
	req := httptest.NewRequest("POST", "http://"+primaryLocalBoxHost+"/register", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	registerHandler(rr, req)

	body := rr.Body.String()
	if rr.Code != 200 || !strings.Contains(body, "You can then make purchases on this site") || !strings.Contains(body, ">Close</button>") || strings.Contains(body, `<form method="POST" action="/register">`) {
		t.Fatalf("expected verification notice, code=%d body=%q", rr.Code, rr.Body.String())
	}
	if sent.To != "buyer@example.net" || sent.Subject != "Welcome to LocalBox" || !strings.Contains(sent.Body, "registered as a user for "+primaryLocalBoxHost) || !strings.Contains(sent.Body, "/register/verify?") || !strings.Contains(sent.HTMLBody, "<a href=") {
		t.Fatalf("expected verification email, got %#v", sent)
	}
	if !authenticatePublicUser("buyer", primaryLocalBoxHost, "secret-password") {
		t.Fatal("expected public user to be registered under request host domain")
	}
	db, err := openOrdersDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var currency, addressLine1, addressLine2, city, province, postalCode, country, physicalAddress string
	if err := db.QueryRow(`SELECT currency, address_line_1, address_line_2, city, province, postal_code, country, physical_address FROM public_users WHERE domain = ? AND username = ?`, primaryLocalBoxHost, "buyer").Scan(&currency, &addressLine1, &addressLine2, &city, &province, &postalCode, &country, &physicalAddress); err != nil {
		t.Fatal(err)
	}
	if currency != "USD" {
		t.Fatalf("expected saved public user currency USD, got %q", currency)
	}
	if addressLine1 != "1 Example Street" || addressLine2 != "Unit 4" || city != "Cape Town" || province != "Western Cape" || postalCode != "8001" || country != "ZA" {
		t.Fatalf("expected registrar address fields to persist, got line1=%q line2=%q city=%q province=%q postal=%q country=%q", addressLine1, addressLine2, city, province, postalCode, country)
	}
	if physicalAddress != "1 Example Street, Unit 4, Cape Town, Western Cape, 8001, ZA" {
		t.Fatalf("expected composed physical address, got %q", physicalAddress)
	}
}

func TestRegisterHandlerRejectsCustomerDomainByDefault(t *testing.T) {
	form := strings.NewReader("username=buyer&email=buyer%40example.net&password=secret-password&confirm_password=secret-password")
	req := httptest.NewRequest("POST", "http://example.com/register", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	registerHandler(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected customer domain registration to be disabled, code=%d body=%q", rr.Code, rr.Body.String())
	}
}

func TestPublicUserCanRegisterLoginAndOpenShoppingPage(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	domain := "example.com"
	if err := os.MkdirAll(domainSiteBase(domain), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(domainSiteBase(domain), shoppingPageName), []byte("<h1>Shopping</h1>"), 0664); err != nil {
		t.Fatal(err)
	}
	if err := registerPublicUser(domain, "buyer", "buyer@example.net", "secret-password"); err != nil {
		t.Fatal(err)
	}
	if !authenticatePublicUser("buyer", domain, "secret-password") {
		t.Fatal("expected registered public user to authenticate")
	}

	token := newSession("buyer", domain)
	req := httptest.NewRequest("GET", "http://example.com/shopping.html", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rr := httptest.NewRecorder()
	rootHandler(rr, req)

	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Logged in as buyer") || !strings.Contains(rr.Body.String(), "LocalBox Shopping Cart") {
		t.Fatalf("expected public registered user to open dynamic shopping page, code=%d body=%q", rr.Code, rr.Body.String())
	}
}

func TestPublicLoginDoesNotRequirePostedDomain(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	if err := registerPublicUser("example.com", "buyer", "buyer@example.net", "secret-password"); err != nil {
		t.Fatal(err)
	}
	form := strings.NewReader("next=%2Fshopping.html&username=buyer&password=secret-password")
	req := httptest.NewRequest("POST", "http://example.com/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	loginHandler(rr, req)

	if rr.Code != 302 || rr.Header().Get("Location") != "/shopping.html" {
		t.Fatalf("expected public login redirect without domain, code=%d location=%q body=%q", rr.Code, rr.Header().Get("Location"), rr.Body.String())
	}
}

func TestPublicLoginAcceptsFullEmailUsername(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	if err := registerPublicUser("example.com", "buyer", "buyer@example.net", "secret-password"); err != nil {
		t.Fatal(err)
	}
	form := strings.NewReader("next=%2Fshopping.html&username=buyer%40example.com&password=secret-password")
	req := httptest.NewRequest("POST", "http://example.com/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	loginHandler(rr, req)

	if rr.Code != 302 || rr.Header().Get("Location") != "/shopping.html" {
		t.Fatalf("expected public login redirect with full email, code=%d location=%q body=%q", rr.Code, rr.Header().Get("Location"), rr.Body.String())
	}
}

func TestMailboxLoginAcceptsFullEmailUsername(t *testing.T) {
	oldUsersFile := usersFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	t.Cleanup(func() { usersFile = oldUsersFile })

	hash, err := hashPasswordStrong("secret-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveUsers([]User{{Username: "dan", Domain: "example.com", Hash: hash}}); err != nil {
		t.Fatal(err)
	}
	form := strings.NewReader("username=dan%40example.com&domain=other.com&password=secret-password")
	req := httptest.NewRequest("POST", "http://example.com/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	loginHandler(rr, req)

	if rr.Code != 302 || rr.Header().Get("Location") != "/dashboard" {
		t.Fatalf("expected mailbox login redirect with full email, code=%d location=%q body=%q", rr.Code, rr.Header().Get("Location"), rr.Body.String())
	}
}

func TestPublicLoginPageDoesNotShowMailboxDomainOptions(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/login?public=1&next=%2Fsoftware.html", nil)
	rr := httptest.NewRecorder()

	loginHandler(rr, req)

	body := rr.Body.String()
	if rr.Code != http.StatusOK {
		t.Fatalf("expected public login page, code=%d body=%q", rr.Code, body)
	}
	if !strings.Contains(body, "LocalBox Account") || !strings.Contains(body, "Sign in to your registered account") || !strings.Contains(body, `name="public" value="1"`) {
		t.Fatalf("expected public account login page, got %s", body)
	}
	if strings.Contains(body, "Sign in to your mailbox") || strings.Contains(body, "shopping cart") || strings.Contains(body, "Interested in LocalBox") || strings.Contains(body, `name="domain"`) || strings.Contains(body, "<select") {
		t.Fatalf("public login should not show mailbox/domain fields, got %s", body)
	}
}

func TestPublicLoginPageShowsForgotPasswordLink(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/login?public=1", nil)
	rr := httptest.NewRecorder()

	loginHandler(rr, req)

	body := rr.Body.String()
	if rr.Code != http.StatusOK {
		t.Fatalf("expected public login page, code=%d body=%q", rr.Code, body)
	}
	if !strings.Contains(body, `href="/forgot-password"`) {
		t.Fatalf("expected forgot password link on public login page, got %s", body)
	}
}

func TestForgotPasswordPagePrefillsEmailFromQuery(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/forgot-password?email=info%40example.com", nil)
	rr := httptest.NewRecorder()

	forgotPasswordHandler(rr, req)

	body := rr.Body.String()
	if rr.Code != http.StatusOK {
		t.Fatalf("expected forgot password page, code=%d body=%q", rr.Code, body)
	}
	if !strings.Contains(body, `value="info@example.com"`) {
		t.Fatalf("expected email prefilled, got %s", body)
	}
}

func TestPublicUserRegistrationRequiresUniqueUsernameAndEmailPerDomain(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })

	if err := registerPublicUser("example.com", "buyer", "buyer@example.net", "secret-password"); err != nil {
		t.Fatal(err)
	}
	if err := registerPublicUser("example.com", "buyer", "other@example.net", "secret-password"); err == nil {
		t.Fatal("expected duplicate username in same domain to fail")
	}
	if err := registerPublicUser("example.com", "other", "buyer@example.net", "secret-password"); err == nil {
		t.Fatal("expected duplicate email in same domain to fail")
	}
	if err := registerPublicUser("other.com", "buyer", "buyer@example.net", "secret-password"); err != nil {
		t.Fatalf("expected same username/email to be allowed in another domain, got %v", err)
	}
}

func TestPublicLogoutClearsSessionAndReturnsHome(t *testing.T) {
	token := newSession("buyer", "example.com")
	req := httptest.NewRequest("GET", "http://example.com/public/logout", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rr := httptest.NewRecorder()

	publicLogoutHandler(rr, req)

	if rr.Code != 302 || rr.Header().Get("Location") != "/" {
		t.Fatalf("expected public logout to return home, code=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
	if session := getSession(req); session != nil {
		t.Fatalf("expected session to be deleted, got %#v", session)
	}
	var cleared bool
	for _, cookie := range rr.Result().Cookies() {
		if cookie.Name == cookieName && cookie.Value == "" {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("expected logout response to clear session cookie")
	}
	if rr.Header().Get("Cache-Control") == "" {
		t.Fatal("expected public logout to disable caching")
	}
}

func TestAllowedUploadedHTMLName(t *testing.T) {
	allowed := []string{
		"index.html",
		"index-20260701T103012Z.html",
		"software.html",
		"about-us.html",
	}
	for _, name := range allowed {
		if !allowedUploadedHTMLName(name) {
			t.Fatalf("expected %q to be allowed", name)
		}
	}

	rejected := []string{
		"index.htm",
		"../index.html",
		"Software Page.html",
		"software.php",
	}
	for _, name := range rejected {
		if allowedUploadedHTMLName(name) {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
}

func TestSaveUploadedSiteHTMLStoresAdditionalPageWithoutPublishingHome(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	site := domainSiteBase(domain)
	if err := os.MkdirAll(site, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("active home"), 0664); err != nil {
		t.Fatal(err)
	}

	name, publishedHome, err := saveUploadedSiteHTML(domain, "software.html", []byte("software page"))
	if err != nil {
		t.Fatal(err)
	}
	if name != "software.html" {
		t.Fatalf("expected software.html, got %q", name)
	}
	if publishedHome {
		t.Fatal("software.html should not publish the homepage")
	}

	active, err := os.ReadFile(filepath.Join(site, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "active home" {
		t.Fatalf("expected active homepage to stay unchanged, got %q", string(active))
	}

	page, err := os.ReadFile(filepath.Join(site, "software.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(page) != "software page" {
		t.Fatalf("expected uploaded page content, got %q", string(page))
	}
	if !isUploadedHTML(domain, "software.html") {
		t.Fatal("expected software.html to be recorded as uploaded")
	}
}

func TestDeleteSiteHTMLFileRemovesMenuItem(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	site := domainSiteBase(domain)
	if err := os.MkdirAll(site, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "software.html"), []byte("software"), 0664); err != nil {
		t.Fatal(err)
	}
	if err := recordUploadedHTML(domain, "software.html"); err != nil {
		t.Fatal(err)
	}
	if err := saveSiteMenuItems(domain, []SiteMenuItem{{Name: "software.html", Label: "Software"}}); err != nil {
		t.Fatal(err)
	}

	if err := deleteSiteHTMLFile(domain, "software.html"); err != nil {
		t.Fatal(err)
	}
	if items := loadSiteMenuItems(domain); len(items) != 0 {
		t.Fatalf("expected menu item to be removed, got %#v", items)
	}
}

func TestRestoreLatestSiteHTMLBackupPrefersUploadedHTML(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	site := domainSiteBase(domain)
	if err := os.MkdirAll(site, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("markdown default"), 0664); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index-20260702T175908Z.html"), []byte("uploaded html"), 0664); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index-20260702T175919Z.html"), []byte("markdown backup"), 0664); err != nil {
		t.Fatal(err)
	}
	if err := recordUploadedHTML(domain, "index-20260702T175908Z.html"); err != nil {
		t.Fatal(err)
	}

	name, err := restoreLatestSiteHTMLBackup(domain)
	if err != nil {
		t.Fatal(err)
	}
	if name != "index-20260702T175908Z.html" {
		t.Fatalf("expected uploaded backup to be restored, got %s", name)
	}

	data, err := os.ReadFile(filepath.Join(site, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "uploaded html" {
		t.Fatalf("expected uploaded html active file, got %q", string(data))
	}
}

func TestSaveUploadedHomeHTMLStoresVersionAndPublishesActiveCopy(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })

	domain := "example.com"
	site := domainSiteBase(domain)
	if err := os.MkdirAll(site, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("old active"), 0664); err != nil {
		t.Fatal(err)
	}

	name, err := saveUploadedHomeHTML(domain, []byte("new uploaded html"))
	if err != nil {
		t.Fatal(err)
	}
	if name == "" || name == "index.html" {
		t.Fatalf("expected timestamped upload name, got %q", name)
	}

	active, err := os.ReadFile(filepath.Join(site, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "new uploaded html" {
		t.Fatalf("expected active index to contain uploaded html, got %q", string(active))
	}

	stored, err := os.ReadFile(filepath.Join(site, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != "new uploaded html" {
		t.Fatalf("expected saved upload to contain uploaded html, got %q", string(stored))
	}
	if !isUploadedHTML(domain, name) {
		t.Fatalf("expected %s to be marked as uploaded html", name)
	}
	if loadSiteMode(domain) != "html" {
		t.Fatalf("expected html mode after upload")
	}
}

func TestPageHTMLHandlerPublishesPastedHTML(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	oldUsers := usersFile
	oldAdmins := adminsFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	adminsFile = filepath.Join(t.TempDir(), "admins.conf")
	t.Cleanup(func() {
		usersFile = oldUsers
		adminsFile = oldAdmins
	})
	if err := os.WriteFile(usersFile, []byte("admin:example.com:x\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adminsFile, []byte("admin:example.com\n"), 0640); err != nil {
		t.Fatal(err)
	}

	htmlBody := "<!doctype html><html><body><h1>AI page</h1></body></html>"
	form := url.Values{}
	form.Set("action", "paste_html")
	form.Set("html_body", htmlBody)
	req := httptest.NewRequest(http.MethodPost, "https://example.com/page/html", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("admin", "example.com")})
	rec := httptest.NewRecorder()

	pageHTMLHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d: %s", rec.Code, rec.Body.String())
	}
	active, err := os.ReadFile(homeHTMLPath("example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(active), "<h1>AI page</h1>") {
		t.Fatalf("expected pasted html to be active, got %s", string(active))
	}
	if loadSiteMode("example.com") != "html" {
		t.Fatalf("expected pasted html to switch site mode")
	}
}

func TestPageHTMLHandlerSavesPastedHTMLPage(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	oldUsers := usersFile
	oldAdmins := adminsFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	adminsFile = filepath.Join(t.TempDir(), "admins.conf")
	t.Cleanup(func() {
		usersFile = oldUsers
		adminsFile = oldAdmins
	})
	if err := os.WriteFile(usersFile, []byte("admin:example.com:x\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adminsFile, []byte("admin:example.com\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(domainSiteBase("example.com"), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(homeHTMLPath("example.com"), []byte("active home"), 0664); err != nil {
		t.Fatal(err)
	}

	htmlBody := "<!doctype html><html><body><h1>About</h1></body></html>"
	form := url.Values{}
	form.Set("action", "paste_html")
	form.Set("html_name", "about.html")
	form.Set("html_body", htmlBody)
	req := httptest.NewRequest(http.MethodPost, "https://example.com/page/html", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("admin", "example.com")})
	rec := httptest.NewRecorder()

	pageHTMLHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d: %s", rec.Code, rec.Body.String())
	}
	page, err := os.ReadFile(filepath.Join(domainSiteBase("example.com"), "about.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "<h1>About</h1>") {
		t.Fatalf("expected pasted page to be saved, got %s", string(page))
	}
	active, err := os.ReadFile(homeHTMLPath("example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "active home" {
		t.Fatalf("expected active homepage to stay unchanged, got %q", string(active))
	}
	if !isUploadedHTML("example.com", "about.html") {
		t.Fatal("expected about.html to be recorded as uploaded HTML")
	}
}

func TestPageHTMLHandlerLoadsPublishedHTMLIntoEditor(t *testing.T) {
	oldBase := localBoxBase
	localBoxBase = t.TempDir()
	t.Cleanup(func() { localBoxBase = oldBase })
	oldUsers := usersFile
	oldAdmins := adminsFile
	usersFile = filepath.Join(t.TempDir(), "users.conf")
	adminsFile = filepath.Join(t.TempDir(), "admins.conf")
	t.Cleanup(func() {
		usersFile = oldUsers
		adminsFile = oldAdmins
	})
	if err := os.WriteFile(usersFile, []byte("admin:example.com:x\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adminsFile, []byte("admin:example.com\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(domainSiteBase("example.com"), 0775); err != nil {
		t.Fatal(err)
	}
	htmlBody := "<!doctype html><html><body><h1>Published page</h1></body></html>"
	if err := os.WriteFile(homeHTMLPath("example.com"), []byte(htmlBody), 0664); err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("action", "load_active_html")
	req := httptest.NewRequest(http.MethodPost, "https://example.com/page/html", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: newSession("admin", "example.com")})
	rec := httptest.NewRecorder()

	pageHTMLHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Published HTML loaded into the editor.") || !strings.Contains(body, "&lt;h1&gt;Published page&lt;/h1&gt;") {
		t.Fatalf("expected published HTML to be loaded into editor, got %s", body)
	}
}
