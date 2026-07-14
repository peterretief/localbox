package main

import (
	"context"
	"strings"
	"testing"
)

func TestComponentManagerRendersRegisteredComponentsInOrder(t *testing.T) {
	manager := NewComponentManager()
	manager.Register(rawHTMLComponent("first"))
	manager.Register(nil)
	manager.Register(rawHTMLComponent(" second"))

	if got := manager.Render(); got != "first second" {
		t.Fatalf("unexpected component output: %q", got)
	}
}

func TestShoppingCartComponentManagerRendersCheckoutCatalog(t *testing.T) {
	manager := newShoppingCartComponentManager("example.co.za", "buyer", "USD", []SiteMenuItem{
		{Name: "software.html", Label: "Software"},
	}, nil, nil)
	rendered := manager.Render()

	wants := []string{
		"<title>LocalBox Shopping Cart</title>",
		`<a href="/software.html">Software</a>`,
		"Choose what you need",
		"No core services have been added for this domain yet.",
		"No add-ons have been added for this domain yet.",
		`data-selected-cart-fields`,
		`data-checkout-button disabled`,
		`method="POST" action="/order"`,
		`name="notes" data-order-notes`,
		`field.name = "cart_items"`,
		`quantityField.name = "cart_quantities"`,
		`data-cart-total`,
		`data-cart-qty`,
		`event.preventDefault()`,
		`Logged in as buyer`,
	}
	for _, want := range wants {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered cart to contain %q, got %s", want, rendered)
		}
	}
	if strings.Contains(rendered, `data-cart-item value=`) {
		t.Fatalf("expected empty default cart to have no prefilled selectable items, got %s", rendered)
	}
}

func TestShoppingCartComponentManagerRendersPublicAuthLinks(t *testing.T) {
	rendered := newShoppingCartComponentManager("example.co.za", "", "", nil, nil, nil).Render()
	for _, want := range []string{`href="/register?next=%2Fshopping.html"`, `href="/login?public=1&next=%2Fshopping.html"`, `>Register</a>`, `>Sign in</a>`} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered cart to contain %q, got %s", want, rendered)
		}
	}
	if strings.Contains(rendered, "Logout") {
		t.Fatalf("anonymous cart should not show logout, got %s", rendered)
	}
}

func TestShoppingCartComponentManagerUsesCustomCatalog(t *testing.T) {
	manager := newShoppingCartComponentManager("example.co.za", "", "", nil, []cartProduct{
		{
			ID:          "custom-service",
			Name:        "Custom service",
			Description: "A saved catalog item.",
			Price:       "R123",
			Category:    "Core",
		},
	}, []cartProduct{
		{
			ID:          "custom-addon",
			Name:        "Custom add-on",
			Description: "A saved add-on.",
			Price:       "R45",
			Category:    "Add-on",
		},
	})

	rendered := manager.Render()
	if !strings.Contains(rendered, "Custom service") || !strings.Contains(rendered, "Custom add-on") {
		t.Fatalf("expected custom catalog items, got %s", rendered)
	}
}

func TestShoppingCartComponentManagerEstimatesForeignCurrencyPrices(t *testing.T) {
	oldFetcher := defaultFXRateFetcher
	defaultFXRateFetcher = func(ctx context.Context, source, target string) (float64, error) {
		if source != "ZAR" || target != "USD" {
			t.Fatalf("unexpected fx request %s -> %s", source, target)
		}
		return 0.05, nil
	}
	t.Cleanup(func() { defaultFXRateFetcher = oldFetcher })

	manager := newShoppingCartComponentManager("example.co.za", "buyer", "USD", nil, []cartProduct{
		{
			ID:          "custom-service",
			Name:        "Custom service",
			Description: "A saved catalog item.",
			Price:       "R100",
			Category:    "Core",
		},
	}, nil)

	rendered := manager.Render()
	if !strings.Contains(rendered, "Estimated $5.00") {
		t.Fatalf("expected estimated USD label, got %s", rendered)
	}
	if !strings.Contains(rendered, `data-price-amount="5.00"`) {
		t.Fatalf("expected converted amount attribute, got %s", rendered)
	}
}

func TestShoppingCartComponentManagerUsesCoZaBundlePrice(t *testing.T) {
	manager := newShoppingCartComponentManager("example.co.za", "", "ZAR", nil, nil, nil)
	rendered := manager.Render()
	if !strings.Contains(rendered, "Estimated R150.00") {
		t.Fatalf("expected co.za bundle price, got %s", rendered)
	}
	if !strings.Contains(rendered, "certificate of ownership") || !strings.Contains(rendered, "Cloudflare DNS setup") {
		t.Fatalf("expected included services description, got %s", rendered)
	}
}
