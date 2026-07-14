package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func loadSiteCatalog(domain string) ([]cartProduct, []cartProduct) {
	products, err := loadDomainCartProducts(domain)
	if err != nil {
		return nil, nil
	}
	return splitCartProducts(products)
}

func saveSiteCatalog(domain string, products []cartProduct) error {
	return saveDomainCartProducts(domain, products)
}

func loadDomainCartProducts(domain string) ([]cartProduct, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil, nil
	}
	db, err := openOrdersDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensureCartCatalogSchema(ctx, db); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, name, description, price, category, image, featured
		FROM shopping_cart_items
		WHERE domain = ?
		ORDER BY sort_order ASC, created_at ASC, id ASC
	`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []cartProduct
	for rows.Next() {
		var product cartProduct
		var featured int
		if err := rows.Scan(
			&product.ID,
			&product.Name,
			&product.Description,
			&product.Price,
			&product.Category,
			&product.Image,
			&featured,
		); err != nil {
			return nil, err
		}
		product.Featured = featured == 1
		products = append(products, cleanCatalogProduct(product))
	}
	return products, rows.Err()
}

func saveDomainCartProducts(domain string, products []cartProduct) error {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return fmt.Errorf("domain required")
	}
	db, err := openOrdersDB()
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	if err := ensureCartCatalogSchema(ctx, db); err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM shopping_cart_items WHERE domain = ?`, domain); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	seen := map[string]bool{}
	for i, product := range products {
		product = cleanCatalogProduct(product)
		if product.Name == "" {
			continue
		}
		if seen[product.ID] {
			product.ID = uniqueCatalogID(product.ID, seen)
		}
		seen[product.ID] = true
		featured := 0
		if product.Featured {
			featured = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO shopping_cart_items (
				domain, id, name, description, price, category, image, featured,
				sort_order, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, domain, product.ID, product.Name, product.Description, product.Price, product.Category,
			product.Image, featured, i, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func splitCartProducts(products []cartProduct) ([]cartProduct, []cartProduct) {
	var core []cartProduct
	var addOns []cartProduct
	for _, product := range products {
		if strings.EqualFold(product.Category, "Add-on") || strings.EqualFold(product.Category, "Add on") {
			product.Category = "Add-on"
			addOns = append(addOns, product)
		} else {
			core = append(core, product)
		}
	}
	return core, addOns
}

func catalogEditorRows(domain string) []CatalogEditorRow {
	products, err := loadDomainCartProducts(domain)
	if err != nil {
		products = nil
	}
	for i := 0; i < 3; i++ {
		products = append(products, cartProduct{})
	}
	rows := make([]CatalogEditorRow, 0, len(products))
	for i, item := range products {
		rows = append(rows, CatalogEditorRow{Index: i, Product: item})
	}
	return rows
}

func ensureCartCatalogSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS shopping_cart_items (
			domain TEXT NOT NULL,
			id TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			price TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT '',
			image TEXT NOT NULL DEFAULT '',
			featured INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (domain, id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_shopping_cart_items_domain_sort
			ON shopping_cart_items(domain, sort_order, created_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
