package main

import (
	"context"
	"strings"
)

func applyLocalDomainReservations(ctx context.Context, results []DomainCheckResult) []DomainCheckResult {
	for i := range results {
		if localDomainHasActiveOrder(ctx, results[i].Domain) {
			results[i].Available = false
			results[i].PaymentPrice = 0
			results[i].PaymentCurrency = ""
			results[i].Reason = "localbox_order"
			results[i].Message = "Already ordered with LocalBox"
		}
	}
	return results
}

func localDomainHasActiveOrder(ctx context.Context, domain string) bool {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return false
	}
	db, err := openOrderDB()
	if err != nil {
		return false
	}
	defer db.Close()
	if err := ensureOrderSchema(ctx, db); err != nil {
		return false
	}
	var exists int
	err = db.QueryRowContext(ctx, `
		SELECT 1
		FROM orders
		WHERE lower(domain) = ?
			AND archived_at = ''
			AND status NOT IN ('cancelled', 'payment_failed')
		LIMIT 1
	`, domain).Scan(&exists)
	return err == nil && exists == 1
}
