package main

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
)

type SiteComponent interface {
	Render(*strings.Builder)
}

type ComponentManager struct {
	components []SiteComponent
}

func NewComponentManager() *ComponentManager {
	return &ComponentManager{}
}

func (m *ComponentManager) Register(component SiteComponent) {
	if component == nil {
		return
	}
	m.components = append(m.components, component)
}

func (m *ComponentManager) Render() string {
	var b strings.Builder
	for _, component := range m.components {
		component.Render(&b)
	}
	return b.String()
}

type rawHTMLComponent string

func (c rawHTMLComponent) Render(b *strings.Builder) {
	b.WriteString(string(c))
}

type contentPageComponent struct {
	Menu    []SiteMenuItem
	Content string
}

func newContentPageComponentManager(content string, menu []SiteMenuItem) *ComponentManager {
	manager := NewComponentManager()
	manager.Register(rawHTMLComponent("<!DOCTYPE html>\n<html>\n"))
	manager.Register(rawHTMLComponent(contentPageHeadHTML()))
	manager.Register(contentPageComponent{Menu: menu, Content: content})
	manager.Register(rawHTMLComponent("</html>\n"))
	return manager
}

func (c contentPageComponent) Render(b *strings.Builder) {
	fmt.Fprint(b, "<body>\n")
	if len(c.Menu) > 0 {
		renderContentPageNav(b, c.Menu)
	}
	fmt.Fprint(b, "<main><article>\n")
	b.WriteString(c.Content)
	fmt.Fprint(b, "\n</article></main>\n</body>\n")
}

func renderContentPageNav(b *strings.Builder, menu []SiteMenuItem) {
	fmt.Fprint(b, `<nav><div class="nav-inner"><a class="brand" href="/">Home</a><div class="menu">`)
	for _, item := range menu {
		fmt.Fprintf(b, `<a href="/%s">%s</a>`, html.EscapeString(item.Name), html.EscapeString(item.Label))
	}
	fmt.Fprint(b, "</div></div></nav>\n")
}

func contentPageHeadHTML() string {
	return `<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>LocalBox</title>
<style>
body{font-family:sans-serif;margin:0;background:#f5f5f5;color:#1a1a2e;line-height:1.6;}
nav{background:#1a1a2e;color:white;padding:0 22px;}
.nav-inner{max-width:760px;margin:0 auto;min-height:58px;display:flex;align-items:center;justify-content:space-between;gap:18px;}
.brand{font-weight:bold;text-decoration:none;color:white;}
.menu{display:flex;gap:14px;flex-wrap:wrap;font-size:14px;}
.menu a{color:white;text-decoration:none;opacity:.88;}.menu a:hover{opacity:1;text-decoration:underline;}
main{max-width:760px;margin:48px auto;padding:0 22px;}
article{background:white;border-radius:8px;padding:34px;}
h1,h2,h3{line-height:1.2;}a{color:#00a0c0;}li{margin:6px 0;}img{max-width:100%;height:auto;border-radius:6px;}
@media(max-width:700px){.nav-inner{align-items:flex-start;flex-direction:column;padding:14px 0;}.menu{gap:10px;}main{margin:24px auto;}article{padding:24px;}}
</style>
</head>
`
}

type shoppingCartPageComponent struct {
	Domain      string
	User        string
	Currency    string
	Menu        []SiteMenuItem
	Products    []cartProduct
	AddOns      []cartProduct
	CheckoutURL string
}

type cartProduct struct {
	ID          string
	Name        string
	Description string
	Price       string
	Category    string
	Image       string
	Featured    bool
}

func newShoppingCartComponentManager(domain, user, currency string, menu []SiteMenuItem, products, addOns []cartProduct) *ComponentManager {
	manager := NewComponentManager()
	manager.Register(rawHTMLComponent("<!DOCTYPE html>\n<html>\n"))
	manager.Register(rawHTMLComponent(cartHeadHTML()))
	manager.Register(shoppingCartPageComponent{
		Domain:      domain,
		User:        user,
		Currency:    cleanCurrency(currency),
		Menu:        menu,
		Products:    products,
		AddOns:      addOns,
		CheckoutURL: "/order",
	})
	manager.Register(rawHTMLComponent("</html>\n"))
	return manager
}

func (c shoppingCartPageComponent) Render(b *strings.Builder) {
	domain := strings.TrimSpace(c.Domain)
	if domain == "" {
		domain = "your domain"
	}

	currency := cleanCurrency(c.Currency)
	if currency == "" {
		currency = "ZAR"
	}
	fmt.Fprintf(b, "<body data-preferred-currency=\"%s\">\n", html.EscapeString(currency))
	renderCartNav(b, c.User, c.Domain, c.Menu)
	fmt.Fprintf(b, `<main>
<section class="hero">
    <div class="hero-copy">
        <p class="eyebrow">LocalBox checkout</p>
        <h1>Choose what you need, then checkout when the cart is ready.</h1>
        <p class="lede">Build a LocalBox order for %s. Select the domain, mailboxes, website help, and support you want included before sending the request for review.</p>
        <div class="hero-actions">
            <a class="button primary" href="#catalog">Select items</a>
            <a class="button secondary" href="#checkout">Checkout</a>
        </div>
    </div>
    <aside class="cart-summary" aria-live="polite">
        <div class="summary-label">Cart</div>
        <div class="summary-count"><span data-cart-count>0</span> selected</div>
        <div class="summary-total" data-cart-total>Total: %s</div>
        <ul data-cart-list><li>No items selected yet.</li></ul>
    </aside>
</section>
`, html.EscapeString(domain), html.EscapeString(formatCurrencyAmount(currency, 0)))

	fmt.Fprint(b, `<section id="catalog" class="catalog">
    <div class="section-heading">
        <p class="eyebrow">Catalog</p>
        <h2>Core services</h2>
    </div>
    <div class="product-grid">
`)
	if len(c.Products) == 0 {
		renderEmptyCatalogMessage(b, "No core services have been added for this domain yet.")
	} else {
		for _, product := range c.Products {
			renderCartProduct(b, product, currency)
		}
	}
	fmt.Fprint(b, `    </div>
</section>
<section class="catalog add-ons">
    <div class="section-heading">
        <p class="eyebrow">Add-ons</p>
        <h2>Useful extras</h2>
    </div>
    <div class="product-grid">
`)
	if len(c.AddOns) == 0 {
		renderEmptyCatalogMessage(b, "No add-ons have been added for this domain yet.")
	} else {
		for _, product := range c.AddOns {
			renderCartProduct(b, product, currency)
		}
	}
	fmt.Fprint(b, `    </div>
</section>
`)
	renderCheckout(b, c.CheckoutURL, c.Products, c.AddOns)
	fmt.Fprint(b, "</main>\n")
	renderCartScript(b)
	fmt.Fprint(b, "</body>\n")
}

func renderEmptyCatalogMessage(b *strings.Builder, message string) {
	fmt.Fprintf(b, `<div class="empty-catalog">%s</div>
`, html.EscapeString(message))
}

func renderCartNav(b *strings.Builder, user, domain string, menu []SiteMenuItem) {
	fmt.Fprint(b, `<nav><div class="nav-inner"><a class="brand" href="/">LocalBox</a><div class="menu"><a href="#catalog">Catalog</a><a href="#checkout">Checkout</a>`)
	for _, item := range menu {
		fmt.Fprintf(b, `<a href="/%s">%s</a>`, html.EscapeString(item.Name), html.EscapeString(item.Label))
	}
	if strings.TrimSpace(user) != "" {
		label := strings.TrimSpace(user)
		fmt.Fprintf(b, `<span class="login-state">Logged in as %s</span>`, html.EscapeString(label))
		fmt.Fprint(b, `<a href="/public/logout">Logout</a>`)
	} else {
		next := "/shopping.html"
		if strings.TrimSpace(domain) != "" {
			next = "/shopping.html"
		}
		fmt.Fprintf(b, `<a href="/register?next=%s">Register</a><a href="/login?public=1&next=%s">Sign in</a>`, html.EscapeString(url.QueryEscape(next)), html.EscapeString(url.QueryEscape(next)))
	}
	fmt.Fprint(b, `</div></div></nav>`+"\n")
}

func renderCartProduct(b *strings.Builder, product cartProduct, currency string) {
	featured := ""
	if product.Featured {
		featured = " featured"
	}
	image := strings.TrimSpace(product.Image)
	if image == "" {
		image = cartImageDataURI(product.ID)
	}
	priceLabel, amount, displayCurrency, estimated, priced := cartPricePresentation(context.Background(), product.Price, currency)
	priceDataAmount := ""
	priceDataCurrency := ""
	priceDataEstimated := "0"
	if priced {
		priceDataAmount = fmt.Sprintf("%.2f", amount)
		priceDataCurrency = displayCurrency
		if estimated {
			priceDataEstimated = "1"
		}
	}
	fmt.Fprintf(b, `<article class="product%s">
        <img class="product-image" src="%s" alt="">
        <div class="product-top">
            <span class="tag">%s</span>
            <label class="pick">
                <input type="checkbox" data-cart-item value="%s" data-name="%s" data-price="%s" data-price-display="%s" data-price-amount="%s" data-price-currency="%s" data-price-estimated="%s">
                <span>Select</span>
            </label>
        </div>
        <h3>%s</h3>
        <p>%s</p>
        <div class="price">%s</div>
    </article>
`, featured,
		html.EscapeString(image),
		html.EscapeString(product.Category),
		html.EscapeString(product.ID),
		html.EscapeString(product.Name),
		html.EscapeString(product.Price),
		html.EscapeString(priceLabel),
		html.EscapeString(priceDataAmount),
		html.EscapeString(priceDataCurrency),
		html.EscapeString(priceDataEstimated),
		html.EscapeString(product.Name),
		html.EscapeString(product.Description),
		html.EscapeString(priceLabel),
	)
}

func renderCheckout(b *strings.Builder, checkoutURL string, products, addOns []cartProduct) {
	fmt.Fprintf(b, `<section id="checkout" class="checkout">
    <div class="section-heading">
        <p class="eyebrow">Checkout</p>
        <h2>Send the cart for review</h2>
    </div>
    <form method="POST" action="%s" class="checkout-form">
        <input type="text" name="website" autocomplete="off" tabindex="-1" class="honeypot" aria-hidden="true">
        <input type="hidden" name="notes" data-order-notes>
        <div data-selected-cart-fields></div>
        <div class="field-grid">
            <label>Full name<input name="full_name" autocomplete="name" required></label>
            <label>Email address<input name="email" type="email" autocomplete="email" required></label>
            <label>Phone number<input name="phone" type="tel" autocomplete="tel" required></label>
            <label>Domain<input name="domain" data-domain-input placeholder="example.co.za" required></label>
            <label>First email address<input name="wanted_email" data-email-input placeholder="info@example.co.za" required></label>
            <label>Physical address<textarea name="physical_address" autocomplete="street-address" required></textarea></label>
        </div>
        <p class="domain-note">LocalBox is starting with new South African .za domain registrations. Transfers, existing domains, and international domains are planned as we broaden the offering.</p>
        <label>Extra notes<textarea data-extra-notes placeholder="Anything else we should include before quoting?"></textarea></label>
        <div class="checkout-actions">
            <button type="submit" data-checkout-button disabled>Submit checkout request</button>
            <p data-checkout-help>Select at least one catalog item before checkout.</p>
        </div>
    </form>
</section>
`, html.EscapeString(checkoutURL))
	_ = products
	_ = addOns
}

func cartHeadHTML() string {
	return `<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>LocalBox Shopping Cart</title>
<style>
*{box-sizing:border-box}
body{font-family:Arial,sans-serif;margin:0;background:#f6f7f2;color:#20242a;line-height:1.55}
nav{background:#20372f;color:white;padding:0 22px}
.nav-inner{max-width:1120px;margin:0 auto;min-height:62px;display:flex;align-items:center;justify-content:space-between;gap:18px}
.brand{font-weight:800;text-decoration:none;color:white}
.menu{display:flex;gap:14px;flex-wrap:wrap;font-size:14px}
.menu a{color:white;text-decoration:none;opacity:.9}.menu a:hover{opacity:1;text-decoration:underline}
.login-state{color:#f1d89a;font-weight:800}
main{max-width:1120px;margin:0 auto;padding:34px 22px 54px}
.hero{display:grid;grid-template-columns:minmax(0,1.5fr) minmax(280px,.8fr);gap:22px;align-items:stretch;margin-bottom:26px}
.hero-copy,.cart-summary,.checkout{background:#fff;border:1px solid #dde3dc;border-radius:8px;padding:26px}
.hero-copy{background:#fffdf8}
.eyebrow{margin:0 0 8px;color:#287667;font-size:12px;font-weight:800;letter-spacing:0;text-transform:uppercase}
h1,h2,h3{line-height:1.18;margin:0;color:#18211e}
h1{font-size:42px;max-width:760px}h2{font-size:26px}h3{font-size:19px}
.lede{font-size:18px;max-width:760px;color:#4c5651}
.hero-actions{display:flex;gap:12px;flex-wrap:wrap;margin-top:22px}
.button,button{min-height:44px;border:0;border-radius:7px;padding:11px 16px;font:inherit;font-weight:800;cursor:pointer;text-decoration:none}
.primary,button{background:#287667;color:white}.primary:hover,button:hover{background:#1f5f52}
.secondary{background:#f1d89a;color:#332910}.secondary:hover{background:#e5c675}
.cart-summary{position:sticky;top:18px;align-self:start}
.summary-label,.price{color:#656f68;font-size:13px;font-weight:800;text-transform:uppercase}
.summary-count{font-size:28px;font-weight:800;margin:8px 0 12px}.summary-total{margin:0 0 12px;font-weight:800;color:#287667}.cart-summary ul{margin:0;padding-left:18px}
.cart-summary li{margin:8px 0}.cart-line{display:flex;align-items:center;justify-content:space-between;gap:10px}.cart-line span{min-width:0}.cart-tools{display:flex;align-items:center;gap:8px;flex-shrink:0}.cart-qty{width:68px;min-height:30px;padding:5px 8px}
.cart-remove{min-height:30px;border:0;border-radius:7px;padding:5px 9px;background:#eef1ea;color:#344039;font-size:12px;font-weight:800;cursor:pointer}.cart-remove:hover{background:#dfe3dc}
.section-heading{display:flex;justify-content:space-between;align-items:end;gap:16px;margin:30px 0 14px}
.product-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px}
.empty-catalog{grid-column:1/-1;background:#fff;border:1px dashed #cbd3ca;border-radius:8px;padding:18px;color:#65706a}
.product{background:#fff;border:1px solid #dde3dc;border-radius:8px;padding:18px;display:grid;gap:12px;min-height:300px}
.product.featured{border-color:#f0b64e;box-shadow:inset 0 3px 0 #f0b64e}
.product-image{width:100%;aspect-ratio:16/10;object-fit:cover;border-radius:7px;background:#edf2ef}
.product-top{display:flex;justify-content:space-between;gap:12px;align-items:center}
.tag{background:#e8f3ef;color:#1f5f52;border-radius:999px;padding:4px 8px;font-size:12px;font-weight:800}
.pick{display:flex;align-items:center;gap:7px;font-weight:800}.pick input{width:18px;height:18px}
.product p{margin:0;color:#59625d}.price{margin-top:auto;color:#8a521f}
.checkout-form{background:#fff;border:1px solid #dde3dc;border-radius:8px;padding:22px;display:grid;gap:16px}
.field-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}
.domain-note{margin:0;color:#58645e;background:#f6f8f4;border:1px solid #dde3dc;border-radius:7px;padding:11px 13px}
label{display:grid;gap:6px;font-weight:800;color:#344039}
input,textarea{width:100%;min-height:42px;padding:10px 12px;border:1px solid #cbd3ca;border-radius:6px;font:inherit;background:white}
textarea{min-height:104px;resize:vertical}.honeypot{position:absolute;left:-10000px;top:auto;width:1px;height:1px;overflow:hidden}
.checkout-actions{display:flex;align-items:center;gap:14px;flex-wrap:wrap}.checkout-actions p{margin:0;color:#656f68}
@media(max-width:860px){.hero,.field-grid{grid-template-columns:1fr}.product-grid{grid-template-columns:1fr 1fr}h1{font-size:34px}.cart-summary{position:static}}
@media(max-width:620px){main{padding:22px 16px 42px}.nav-inner{align-items:flex-start;flex-direction:column;padding:14px 0}.product-grid{grid-template-columns:1fr}.hero-copy,.cart-summary,.checkout,.checkout-form{padding:18px}h1{font-size:30px}}
</style>
</head>
`
}

func cartImageDataURI(kind string) string {
	kind = cleanCatalogID(kind, "item")
	palette := map[string][3]string{
		"domain":    {"287667", "f1d89a", "Domain"},
		"mail":      {"2f6fb0", "cfe8ff", "Mail"},
		"site":      {"7a5cce", "eadfff", "Site"},
		"migration": {"b85c38", "ffe0c9", "Move"},
		"dns":       {"1f5f52", "d8efe7", "DNS"},
		"support":   {"8a521f", "f8dfb4", "Help"},
	}
	values, ok := palette[kind]
	if !ok {
		values = [3]string{"287667", "e8f3ef", "Item"}
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 400"><rect width="640" height="400" fill="#%s"/><circle cx="520" cy="82" r="98" fill="#%s" opacity=".95"/><rect x="70" y="92" width="360" height="216" rx="24" fill="#fff" opacity=".92"/><rect x="104" y="132" width="226" height="22" rx="11" fill="#%s" opacity=".82"/><rect x="104" y="178" width="278" height="18" rx="9" fill="#%s" opacity=".36"/><rect x="104" y="216" width="194" height="18" rx="9" fill="#%s" opacity=".36"/><text x="104" y="284" font-family="Arial, sans-serif" font-size="42" font-weight="700" fill="#20372f">%s</text></svg>`, values[0], values[1], values[0], values[0], values[0], values[2])
	replacer := strings.NewReplacer("#", "%23", " ", "%20", `"`, "'", "<", "%3C", ">", "%3E")
	return "data:image/svg+xml;utf8," + replacer.Replace(svg)
}

func cartPricePresentation(ctx context.Context, price, targetCurrency string) (label string, amount float64, displayCurrency string, estimated bool, priced bool) {
	amount, sourceCurrency, priced := parseCatalogPrice(price)
	if !priced {
		return price, 0, cleanCurrency(targetCurrency), false, false
	}
	sourceCurrency = cleanCurrency(sourceCurrency)
	displayCurrency = cleanCurrency(targetCurrency)
	if displayCurrency == "" {
		displayCurrency = sourceCurrency
	}
	symbols := map[string]string{"ZAR": "R", "USD": "$", "EUR": "€", "GBP": "£"}
	if sourceCurrency == displayCurrency {
		prefix := symbols[displayCurrency]
		if prefix == "" {
			prefix = displayCurrency + " "
		}
		return fmt.Sprintf("%s%.2f", prefix, amount), amount, displayCurrency, false, true
	}
	converted, convertedEstimate, err := estimateCurrencyAmount(ctx, amount, sourceCurrency, displayCurrency)
	if err != nil {
		prefix := symbols[sourceCurrency]
		if prefix == "" {
			prefix = sourceCurrency + " "
		}
		return fmt.Sprintf("%s%.2f", prefix, amount), amount, sourceCurrency, false, true
	}
	prefix := symbols[displayCurrency]
	if prefix == "" {
		prefix = displayCurrency + " "
	}
	if convertedEstimate {
		return fmt.Sprintf("Estimated %s%.2f", prefix, converted), converted, displayCurrency, true, true
	}
	return fmt.Sprintf("%s%.2f", prefix, converted), converted, displayCurrency, false, true
}

func formatCurrencyAmount(currency string, amount float64) string {
	symbols := map[string]string{"ZAR": "R", "USD": "$", "EUR": "€", "GBP": "£"}
	currency = cleanCurrency(currency)
	prefix := symbols[currency]
	if prefix == "" {
		prefix = currency + " "
	}
	return fmt.Sprintf("%s%.2f", prefix, amount)
}

func renderCartScript(b *strings.Builder) {
	fmt.Fprint(b, `<script>
(function(){
    const items = Array.from(document.querySelectorAll("[data-cart-item]"));
    const count = document.querySelector("[data-cart-count]");
    const total = document.querySelector("[data-cart-total]");
    const list = document.querySelector("[data-cart-list]");
    const notes = document.querySelector("[data-order-notes]");
    const selectedFields = document.querySelector("[data-selected-cart-fields]");
    const extra = document.querySelector("[data-extra-notes]");
    const help = document.querySelector("[data-checkout-help]");
    const button = document.querySelector("[data-checkout-button]");
    const form = document.querySelector(".checkout-form");
    const domainInput = document.querySelector("[data-domain-input]");
    const emailInput = document.querySelector("[data-email-input]");
    const quantities = {};
    const preferredCurrency = cleanCurrency(document.body.dataset.preferredCurrency || "ZAR");
    const params = new URLSearchParams(window.location.search);
    const urlDomain = cleanDomain(params.get("domain"));
    if(urlDomain){
        try {
            localStorage.setItem("localbox_cart_domain", urlDomain);
        } catch (error) {}
    }
    const storedDomain = cleanDomain(readStoredDomain());
    let cartDomain = urlDomain || storedDomain;
    if(cartDomain && domainInput && !domainInput.value){
        domainInput.value = cartDomain;
    }
    if(cartDomain && emailInput && !emailInput.value){
        emailInput.value = "info@" + cartDomain;
    }
    function cleanDomain(domain){
        domain = String(domain || "").trim().toLowerCase();
        domain = domain.replace(/^https?:\/\//, "").replace(/^www\./, "").split(/[\/?#]/)[0].replace(/\.+$/, "");
        return /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9][a-z0-9-]{0,61})+$/.test(domain) ? domain : "";
    }
    function cleanCurrency(currency){
        currency = String(currency || "").trim().toUpperCase();
        return /^(ZAR|USD|EUR|GBP)$/.test(currency) ? currency : "ZAR";
    }
    function readStoredDomain(){
        try {
            return localStorage.getItem("localbox_cart_domain") || "";
        } catch (error) {
            return "";
        }
    }
    function cartStorageKey(kind){
        const domain = cleanDomain(cartDomain || (domainInput && domainInput.value) || readStoredDomain());
        return domain ? "localbox_cart_" + kind + ":" + domain : "";
    }
    function readCartState(kind){
        const key = cartStorageKey(kind);
        if(!key){
            return null;
        }
        try {
            const raw = localStorage.getItem(key);
            return raw ? JSON.parse(raw) : null;
        } catch (error) {
            return null;
        }
    }
    function writeCartState(kind, value){
        const key = cartStorageKey(kind);
        if(!key){
            return;
        }
        try {
            localStorage.setItem(key, JSON.stringify(value));
        } catch (error) {}
    }
    function restoreCartState(){
        const selected = readCartState("items");
        const savedQuantities = readCartState("quantities");
        if(Array.isArray(selected)){
            selected.forEach(id => {
                const checkbox = items.find(item => item.value === id);
                if(checkbox){
                    checkbox.checked = true;
                }
            });
        }
        if(savedQuantities && typeof savedQuantities === "object"){
            Object.keys(savedQuantities).forEach(id => {
                const qty = parseInt(savedQuantities[id], 10);
                if(Number.isFinite(qty) && qty > 0){
                    quantities[id] = String(qty);
                }
            });
        }
    }
    function selectedItems(){
        const selected = items.filter(item => item.checked).map(item => ({id:item.value, name:item.dataset.name, price:item.dataset.price}));
        if(cartDomain){
            const isCoZa = /\.co\.za$/i.test(cartDomain);
            selected.unshift(isCoZa ? {
                id:"domain:" + cartDomain,
                name:"New .co.za domain registration: " + cartDomain,
                price:"Estimated R150.00",
                display:"Estimated R150.00",
                amount:"150",
                currency:"ZAR",
                estimated:"1",
                description:"Includes a certificate of ownership and Cloudflare DNS setup pointing to this domain."
            } : {
                id:"domain:" + cartDomain,
                name:"New domain registration: " + cartDomain,
                price:"To be confirmed",
                display:"To be confirmed",
                amount:"0",
                currency:preferredCurrency,
                estimated:"0",
                description:"Includes domain setup and DNS configuration."
            });
        }
        return selected;
    }
    function itemQuantity(id){
        const qty = parseInt(quantities[id] || "1", 10);
        return Number.isFinite(qty) && qty > 0 ? qty : 1;
    }
    function formatMoney(currency, amount){
        const symbols = {ZAR:"R", USD:"$", EUR:"€", GBP:"£"};
        return (symbols[currency] || (currency + " ")) + amount.toFixed(2);
    }
    function displayPrice(price){
        const text = String(price || "").trim();
        if(!text){
            return String(price || "");
        }
        return text;
    }
    function totalLabel(selected){
        const totals = {};
        let unpriced = 0;
        let estimated = false;
        selected.forEach(item => {
            const parsedAmount = parseFloat(item.amount || "0");
            if(!Number.isFinite(parsedAmount) || parsedAmount <= 0){
                unpriced++;
                return;
            }
            const currency = cleanCurrency(item.currency || preferredCurrency);
            estimated = estimated || item.estimated === "1" || currency !== preferredCurrency;
            totals[currency] = (totals[currency] || 0) + parsedAmount * itemQuantity(item.id);
        });
        const parts = Object.keys(totals).sort().map(currency => formatMoney(currency, totals[currency]));
        if(unpriced){
            parts.push("items to confirm");
        }
        return (estimated ? "Estimated total: " : "Total: ") + (parts.length ? parts.join(" + ") : formatMoney(preferredCurrency, 0));
    }
    function updateCart(){
        const selected = items.filter(item => item.checked).map(item => ({
            id: item.value,
            name: item.dataset.name,
            price: item.dataset.price,
            display: item.dataset.priceDisplay || item.dataset.price,
            amount: item.dataset.priceAmount,
            currency: item.dataset.priceCurrency,
            estimated: item.dataset.priceEstimated
        }));
        if(cartDomain){
            selected.unshift({id:"domain:" + cartDomain, name:"New domain registration: " + cartDomain, price:"To be confirmed", display:"To be confirmed", amount:"0", currency:preferredCurrency, estimated:"0"});
        }
        count.textContent = selected.reduce((sum, item) => sum + itemQuantity(item.id), 0);
        total.textContent = totalLabel(selected);
        list.innerHTML = "";
        selectedFields.innerHTML = "";
        if(selected.length === 0){
            const li = document.createElement("li");
            li.textContent = "No items selected yet.";
            list.appendChild(li);
        } else {
            selected.forEach(item => {
                const li = document.createElement("li");
                const row = document.createElement("div");
                row.className = "cart-line";
                const label = document.createElement("span");
                label.textContent = item.name + (item.display ? " - " + displayPrice(item.display) : "");
                if(item.description){
                    const desc = document.createElement("small");
                    desc.textContent = item.description;
                    desc.style.display = "block";
                    desc.style.fontWeight = "400";
                    desc.style.color = "#65706a";
                    desc.style.marginTop = "4px";
                    label.appendChild(desc);
                }
                const tools = document.createElement("div");
                tools.className = "cart-tools";
                const qty = document.createElement("input");
                qty.type = "number";
                qty.min = "1";
                qty.step = "1";
                qty.className = "cart-qty";
                qty.value = itemQuantity(item.id);
                qty.dataset.cartQty = item.id;
                qty.setAttribute("aria-label", "Quantity for " + item.name);
                const remove = document.createElement("button");
                remove.type = "button";
                remove.className = "cart-remove";
                remove.dataset.removeCartItem = item.id;
                remove.textContent = "Remove";
                tools.appendChild(qty);
                tools.appendChild(remove);
                row.appendChild(label);
                row.appendChild(tools);
                li.appendChild(row);
                list.appendChild(li);
                const field = document.createElement("input");
                field.type = "hidden";
                field.name = "cart_items";
                field.value = item.id;
                selectedFields.appendChild(field);
                const quantityField = document.createElement("input");
                quantityField.type = "hidden";
                quantityField.name = "cart_quantities";
                quantityField.value = item.id + "=" + itemQuantity(item.id);
                selectedFields.appendChild(quantityField);
            });
        }
        const lines = selected.map(item => "- " + itemQuantity(item.id) + " x " + item.name + (item.display ? " (" + displayPrice(item.display) + ")" : "") + (item.description ? " - " + item.description : ""));
        const extraText = extra.value.trim();
        notes.value = selected.length ? "Shopping cart:\n" + lines.join("\n") + "\n" + totalLabel(selected) + (extraText ? "\n\nExtra notes:\n" + extraText : "") : "";
        help.textContent = selected.length ? "Your selected items will be included with the checkout request." : "Select at least one catalog item before checkout.";
        button.disabled = selected.length === 0;
        writeCartState("items", selected.filter(item => item.id.indexOf("domain:") !== 0).map(item => item.id));
        writeCartState("quantities", quantities);
    }
    items.forEach(item => item.addEventListener("change", updateCart));
    list.addEventListener("input", function(event){
        const input = event.target.closest("[data-cart-qty]");
        if(!input){
            return;
        }
        quantities[input.dataset.cartQty] = String(Math.max(1, parseInt(input.value || "1", 10) || 1));
        updateCart();
    });
    list.addEventListener("click", function(event){
        const remove = event.target.closest("[data-remove-cart-item]");
        if(!remove){
            return;
        }
        const id = remove.dataset.removeCartItem || "";
        if(id.indexOf("domain:") === 0){
            delete quantities[id];
            cartDomain = "";
            if(domainInput){
                domainInput.value = "";
            }
            if(emailInput && emailInput.value.indexOf("info@") === 0){
                emailInput.value = "";
            }
            try {
                localStorage.removeItem("localbox_cart_domain");
            } catch (error) {}
            try {
                localStorage.removeItem(cartStorageKey("items"));
                localStorage.removeItem(cartStorageKey("quantities"));
            } catch (error) {}
        } else {
            const checkbox = items.find(item => item.value === id);
            if(checkbox){
                checkbox.checked = false;
            }
            delete quantities[id];
        }
        updateCart();
    });
    extra.addEventListener("input", updateCart);
    domainInput.addEventListener("input", function(){
        const domain = domainInput.value.trim().toLowerCase();
        if(domain && (!emailInput.value || emailInput.value === "info@")){
            emailInput.value = "info@" + domain;
        }
    });
    form.addEventListener("submit", function(event){
        updateCart();
        if(selectedItems().length === 0){
            event.preventDefault();
            help.textContent = "Choose at least one item before submitting checkout.";
        }
    });
    restoreCartState();
    updateCart();
})();
</script>
`)
}
