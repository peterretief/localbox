package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
)

type domainProvisioner interface {
	CheckDomainAvailable(ctx context.Context, domain string) error
	CreateCloudflareZone(ctx context.Context, domain string) (provisionedCloudflareZone, error)
	RegisterDomain(ctx context.Context, order *OrderDetail, nameservers []string) (string, error)
	UpdateNameservers(ctx context.Context, domain string, nameservers []string) error
	UpsertTunnelRecords(ctx context.Context, zoneID, domain string) error
	CheckZoneActivation(ctx context.Context, zoneID string) error
}

type provisionedCloudflareZone struct {
	ID          string
	NameServers []string
}

func prepareCloudflareForDomainOrderWith(ctx context.Context, orderID string, provisioner domainProvisioner) (provisionedCloudflareZone, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return provisionedCloudflareZone{}, fmt.Errorf("order ID required")
	}
	if provisioner == nil {
		return provisionedCloudflareZone{}, fmt.Errorf("domain provisioner required")
	}
	order, err := loadOrderDetail(orderID)
	if err != nil {
		return provisionedCloudflareZone{}, err
	}
	if order.Status != "paid" && order.Status != "provisioning" && order.Status != "provisioning_failed" && order.Status != "ready_for_registration" {
		return provisionedCloudflareZone{}, fmt.Errorf("order must be paid before Cloudflare preparation")
	}
	domain, err := normalizePaymentDomain(order.Domain)
	if err != nil {
		_ = markOrderProvisioningFailed(orderID, err)
		return provisionedCloudflareZone{}, err
	}

	if err := updateOrderFields(orderID, "provisioning", "Cloudflare preparation started.", nil); err != nil {
		return provisionedCloudflareZone{}, err
	}
	zone, err := ensureCloudflareZoneForOrder(ctx, order, domain, provisioner)
	if err != nil {
		_ = markOrderProvisioningFailed(orderID, err)
		return provisionedCloudflareZone{}, err
	}
	if err := provisioner.UpsertTunnelRecords(ctx, zone.ID, domain); err != nil {
		_ = markOrderProvisioningFailed(orderID, err)
		return provisionedCloudflareZone{}, err
	}
	if err := updateOrderFields(orderID, "ready_for_registration", "Cloudflare zone, tunnel DNS, and generated nameservers are ready for registrar registration.", map[string]any{
		"cloudflare_zone_id":     zone.ID,
		"cloudflare_nameservers": strings.Join(zone.NameServers, ","),
	}); err != nil {
		return provisionedCloudflareZone{}, err
	}
	return zone, nil
}

func delegatedNameserversMatch(domain string, expected []string) bool {
	if len(expected) == 0 {
		return false
	}
	answers, err := net.LookupNS(domain)
	if err != nil {
		return false
	}
	actual := make(map[string]bool, len(answers))
	for _, answer := range answers {
		actual[strings.TrimSuffix(strings.ToLower(strings.TrimSpace(answer.Host)), ".")] = true
	}
	for _, nameserver := range expected {
		if !actual[strings.TrimSuffix(strings.ToLower(strings.TrimSpace(nameserver)), ".")] {
			return false
		}
	}
	return true
}

func ensureCloudflareZoneForOrder(ctx context.Context, order *OrderDetail, domain string, provisioner domainProvisioner) (provisionedCloudflareZone, error) {
	zone := provisionedCloudflareZone{
		ID:          strings.TrimSpace(order.CloudflareZoneID),
		NameServers: parseProvisionedNameservers(order.CloudflareNameservers),
	}
	if zone.ID != "" && len(zone.NameServers) >= 2 {
		return zone, nil
	}
	zone, err := provisioner.CreateCloudflareZone(ctx, domain)
	if err != nil {
		return provisionedCloudflareZone{}, err
	}
	if zone.ID == "" {
		return provisionedCloudflareZone{}, fmt.Errorf("Cloudflare did not return a zone ID")
	}
	if len(zone.NameServers) < 2 {
		return provisionedCloudflareZone{}, fmt.Errorf("Cloudflare did not return at least two nameservers")
	}
	if err := updateOrderFields(order.ID, "provisioning", "Cloudflare zone created. Use the generated nameservers at Domains.co.za.", map[string]any{
		"cloudflare_zone_id":     zone.ID,
		"cloudflare_nameservers": strings.Join(zone.NameServers, ","),
	}); err != nil {
		return provisionedCloudflareZone{}, err
	}
	return zone, nil
}

var queuePaidDomainOrderProvisioning = queuePaidDomainOrderProvisioningImpl

func queuePaidDomainOrderProvisioningImpl(orderID string) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return
	}
	go func() {
		provisioner, err := newConfiguredDomainProvisioner()
		if err != nil {
			log.Printf("Automatic domain provisioning skipped for %s: %v", orderID, err)
			return
		}
		if err := runRegistrationWorkflow(context.Background(), orderID, provisioner); err != nil {
			log.Printf("Automatic domain provisioning failed for %s: %v", orderID, err)
		}
	}()
}

func markOrderProvisioningFailed(orderID string, cause error) error {
	message := "Domain provisioning failed."
	if cause != nil {
		message += " " + cause.Error()
	}
	return updateOrderFields(orderID, "provisioning_failed", message, nil)
}

func parseProvisionedNameservers(value string) []string {
	var nameservers []string
	for _, part := range strings.Split(value, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			nameservers = append(nameservers, part)
		}
	}
	return nameservers
}
