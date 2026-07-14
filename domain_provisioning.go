package main

import (
	"context"
	"fmt"
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

func provisionPaidDomainOrderWith(ctx context.Context, orderID string, provisioner domainProvisioner) error {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return fmt.Errorf("order ID required")
	}
	if provisioner == nil {
		return fmt.Errorf("domain provisioner required")
	}
	order, err := loadOrderDetail(orderID)
	if err != nil {
		return err
	}
	if order.Status == "registered" && order.RegistrarReference != "" {
		return nil
	}
	if order.Status != "paid" && order.Status != "provisioning" && order.Status != "provisioning_failed" {
		return fmt.Errorf("order must be paid before provisioning")
	}
	domain, err := normalizePaymentDomain(order.Domain)
	if err != nil {
		_ = markOrderProvisioningFailed(orderID, err)
		return err
	}

	if strings.TrimSpace(order.RegistrarReference) == "" {
		if err := provisioner.CheckDomainAvailable(ctx, domain); err != nil {
			_ = markOrderProvisioningFailed(orderID, err)
			return err
		}
	}
	if err := updateOrderFields(orderID, "provisioning", "Domain provisioning started.", nil); err != nil {
		return err
	}

	zone := provisionedCloudflareZone{
		ID:          strings.TrimSpace(order.CloudflareZoneID),
		NameServers: parseProvisionedNameservers(order.CloudflareNameservers),
	}
	if zone.ID == "" || len(zone.NameServers) < 2 {
		zone, err = provisioner.CreateCloudflareZone(ctx, domain)
		if err != nil {
			_ = markOrderProvisioningFailed(orderID, err)
			return err
		}
		if zone.ID == "" {
			err = fmt.Errorf("Cloudflare did not return a zone ID")
			_ = markOrderProvisioningFailed(orderID, err)
			return err
		}
		if len(zone.NameServers) < 2 {
			err = fmt.Errorf("Cloudflare did not return at least two nameservers")
			_ = markOrderProvisioningFailed(orderID, err)
			return err
		}
		if err := updateOrderFields(orderID, "provisioning", "Cloudflare zone created.", map[string]any{
			"cloudflare_zone_id":     zone.ID,
			"cloudflare_nameservers": strings.Join(zone.NameServers, ","),
		}); err != nil {
			return err
		}
	}

	registrarReference := strings.TrimSpace(order.RegistrarReference)
	if registrarReference == "" {
		registrarReference, err = provisioner.RegisterDomain(ctx, order, zone.NameServers)
		if err != nil {
			_ = markOrderProvisioningFailed(orderID, err)
			return err
		}
		registrarReference = strings.TrimSpace(registrarReference)
		if registrarReference == "" {
			err = fmt.Errorf("registrar did not return a reference")
			_ = markOrderProvisioningFailed(orderID, err)
			return err
		}
		if err := updateOrderFields(orderID, "provisioning", "Domain registered at registrar.", map[string]any{
			"registrar_reference": registrarReference,
		}); err != nil {
			return err
		}
	}

	if err := provisioner.UpdateNameservers(ctx, domain, zone.NameServers); err != nil {
		_ = markOrderProvisioningFailed(orderID, err)
		return err
	}
	if err := provisioner.UpsertTunnelRecords(ctx, zone.ID, domain); err != nil {
		_ = markOrderProvisioningFailed(orderID, err)
		return err
	}
	if err := provisioner.CheckZoneActivation(ctx, zone.ID); err != nil {
		_ = markOrderProvisioningFailed(orderID, err)
		return err
	}

	return completeOrderRegistration(ctx, orderID, registrarReference)
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
