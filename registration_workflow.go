package main

import (
	"context"
	"fmt"
	"log"
	"net/mail"
	"strings"
)

// runRegistrationWorkflow is the single controller for a paid domain
// registration. Every normal entry point reaches this ordered workflow.
func runRegistrationWorkflow(ctx context.Context, orderID string, provisioner domainProvisioner) (workflowErr error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return fmt.Errorf("order ID required")
	}
	if provisioner == nil {
		return fmt.Errorf("domain provisioner required")
	}
	provisioningLock, err := acquireOrderProvisioningLock(orderID)
	if err != nil {
		return err
	}
	defer provisioningLock.Release()
	defer func() {
		if workflowErr != nil {
			if err := sendRegistrationFailureDebug(orderID, workflowErr); err != nil {
				log.Printf("registration debug email failed for %s: %v", orderID, err)
			}
		}
	}()

	order, err := loadOrderDetail(orderID)
	if err != nil {
		return err
	}
	if order.Status == "site_live" && orderSetupComplete(order) {
		return nil
	}
	if order.Status != "paid" && order.Status != "provisioning" && order.Status != "provisioning_failed" &&
		order.Status != "registered" && order.Status != "site_live" && order.Status != "ready_for_registration" {
		return fmt.Errorf("order must be paid before provisioning")
	}

	domain, err := normalizePaymentDomain(order.Domain)
	if err != nil {
		_ = markOrderProvisioningFailed(orderID, err)
		return err
	}
	if err := registrarAllowedForDomain(domain); err != nil {
		return err
	}

	reusableReference, reusableTestDomain := reusableTestRegistrarReference(domain)
	if strings.TrimSpace(order.RegistrarReference) == "" && !reusableTestDomain {
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
		zone, err = ensureCloudflareZoneForOrder(ctx, order, domain, provisioner)
		if err != nil {
			_ = markOrderProvisioningFailed(orderID, err)
			return err
		}
	}

	registrarReference := strings.TrimSpace(order.RegistrarReference)
	if registrarReference == "" {
		if reusableTestDomain {
			registrarReference = reusableReference
			if err := updateOrderFields(orderID, "provisioning", "Reusing the configured Domains.co.za development test domain.", map[string]any{
				"registrar_reference": registrarReference,
			}); err != nil {
				return err
			}
		} else {
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
	}

	if registrarReference == "" || !delegatedNameserversMatch(domain, zone.NameServers) {
		if err := provisioner.UpdateNameservers(ctx, domain, zone.NameServers); err != nil {
			_ = markOrderProvisioningFailed(orderID, err)
			return err
		}
	}
	if err := provisioner.CheckZoneActivation(ctx, zone.ID); err != nil {
		_ = updateOrderFields(orderID, "provisioning", "Cloudflare zone is not active yet; provisioning will retry automatically.", nil)
		return err
	}
	if err := provisioner.UpsertTunnelRecords(ctx, zone.ID, domain); err != nil {
		if isCloudflareZonePending(err) {
			_ = updateOrderFields(orderID, "provisioning", "Cloudflare zone is not active for all services yet; provisioning will retry automatically.", nil)
			return err
		}
		_ = markOrderProvisioningFailed(orderID, err)
		return err
	}
	if err := completeOrderRegistration(ctx, orderID, registrarReference); err != nil {
		if isBrevoSetupPending(err) || isCloudflareZonePending(err) {
			if isCloudflareZonePending(err) {
				_ = updateOrderFields(orderID, "provisioning", "Cloudflare zone is not active for all services yet; provisioning will retry automatically.", nil)
			}
			return err
		}
		_ = markOrderProvisioningFailed(orderID, err)
		return err
	}
	return nil
}

func isCloudflareZonePending(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "active zone required") ||
		strings.Contains(message, "activation not confirmed") ||
		strings.Contains(message, "zone is not active")
}

func runRegistrationWorkflowWithRegistrarReference(ctx context.Context, orderID, registrarReference string, provisioner domainProvisioner) error {
	registrarReference = strings.TrimSpace(registrarReference)
	if registrarReference == "" {
		return fmt.Errorf("registrar reference required")
	}
	if err := updateOrderFields(orderID, "provisioning", "Registration workflow resumed with registrar reference.", map[string]any{
		"registrar_reference": registrarReference,
	}); err != nil {
		return err
	}
	return runRegistrationWorkflow(ctx, orderID, provisioner)
}

func orderSetupComplete(order *OrderDetail) bool {
	if order == nil || strings.TrimSpace(order.RegistrarReference) == "" ||
		strings.TrimSpace(order.WebsiteURL) == "" || strings.TrimSpace(order.CertificateSentAt) == "" {
		return false
	}
	if strings.TrimSpace(order.WantedEmail) == "" {
		return true
	}
	wantedEmail, err := mail.ParseAddress(order.WantedEmail)
	if err != nil {
		return false
	}
	domain := normalizeDomain(order.Domain)
	user := normalizeMailboxUser(strings.TrimSuffix(strings.ToLower(wantedEmail.Address), "@"+domain))
	return userExists(user, domain) && isDomainAdmin(user, domain) && orderHasEvent(order.Events, "brevo_setup_verified")
}
