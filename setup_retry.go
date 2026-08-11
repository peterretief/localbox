package main

import (
	"context"
	"log"
	"strings"
	"time"
)

var retryFailedDomainProvisioningOrder = retryFailedDomainProvisioningOrderImpl

func startOrderSetupRetryWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		retryActiveOrderSetups(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				retryActiveOrderSetups(ctx)
			}
		}
	}()
}

func retryActiveOrderSetups(ctx context.Context) {
	retryTimedOutDomainProvisioning(ctx)
	retryMailboxBrevoSenders(ctx)
	orders, err := loadOrderSummaries()
	if err != nil {
		log.Printf("setup retry: could not load orders: %v", err)
		return
	}
	provisioner, err := newConfiguredDomainProvisioner()
	if err != nil {
		log.Printf("setup retry: could not configure domain provisioner: %v", err)
		return
	}
	for _, order := range orders {
		if strings.TrimSpace(order.ArchivedAt) != "" ||
			strings.TrimSpace(order.RegistrarReference) == "" ||
			strings.TrimSpace(order.WantedEmail) == "" ||
			order.Status != "provisioning" {
			continue
		}
		// Re-enter the complete, locked provisioning path. This repairs partial
		// orders before Brevo readiness can promote them to site_live.
		if err := runRegistrationWorkflow(ctx, order.ID, provisioner); err != nil {
			if !isBrevoSetupPending(err) {
				log.Printf("setup retry: %s failed: %v", order.ID, err)
			}
		}
	}
}

func retryTimedOutDomainProvisioning(ctx context.Context) {
	orders, err := loadOrderSummaries()
	if err != nil {
		log.Printf("provisioning retry: could not load orders: %v", err)
		return
	}
	for _, order := range orders {
		if strings.TrimSpace(order.ArchivedAt) != "" ||
			strings.TrimSpace(order.RegistrarReference) != "" ||
			order.Status != "provisioning_failed" {
			continue
		}
		detail, err := loadOrderDetail(order.ID)
		if err != nil {
			log.Printf("provisioning retry: could not load %s: %v", order.ID, err)
			continue
		}
		if !isTimedOutDomainProvisioning(detail) {
			continue
		}
		if err := retryFailedDomainProvisioningOrder(ctx, order.ID); err != nil {
			log.Printf("provisioning retry: %s failed: %v", order.ID, err)
		}
	}
}

func isTimedOutDomainProvisioning(order *OrderDetail) bool {
	if order == nil {
		return false
	}
	for _, event := range order.Events {
		if event.EventType == "provisioning_failed" {
			return strings.Contains(strings.ToLower(event.Message), "context deadline exceeded")
		}
	}
	return false
}

func retryFailedDomainProvisioningOrderImpl(ctx context.Context, orderID string) error {
	provisioner, err := newConfiguredDomainProvisioner()
	if err != nil {
		return err
	}
	return runRegistrationWorkflow(ctx, orderID, provisioner)
}

func retryMailboxBrevoSenders(ctx context.Context) {
	rows, err := auditAllDomainEmailSetups(ctx)
	if err != nil {
		log.Printf("setup retry: could not audit domain email setup: %v", err)
		return
	}
	for _, row := range rows {
		if strings.HasPrefix(row.CloudflareInbound, "FAILED:") || strings.HasPrefix(row.BrevoSender, "FAILED:") {
			log.Printf("setup retry: %s %s Cloudflare=%s Brevo=%s", row.Domain, row.Mailbox, row.CloudflareInbound, row.BrevoSender)
		}
	}
}
