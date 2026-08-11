package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	applyRuntimeConfig()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "add-domain":
			os.Exit(runAddDomain(os.Args[2:], os.Stderr))
		case "auth-check":
			os.Exit(runAuthCheck(os.Args[2:], os.Stdin, os.Stderr))
		case "reset-password":
			os.Exit(runResetPassword(os.Args[2:], os.Stdin, os.Stderr))
		case "set-recovery-email":
			os.Exit(runSetRecoveryEmail(os.Args[2:], os.Stderr))
		case "sync-email-route":
			os.Exit(runSyncEmailRoute(os.Args[2:], os.Stderr))
		case "delete-email-route":
			os.Exit(runDeleteEmailRoute(os.Args[2:], os.Stderr))
		case "sync-brevo-sender":
			os.Exit(runSyncBrevoSender(os.Args[2:], os.Stderr))
		case "send-full-setup-checklist":
			os.Exit(runSendFullSetupChecklist(os.Args[2:], os.Stderr))
		case "sync-web-route":
			os.Exit(runSyncWebRoute(os.Args[2:], os.Stderr))
		case "delete-web-route":
			os.Exit(runDeleteWebRoute(os.Args[2:], os.Stderr))
		case "repair-localbox-subdomain":
			os.Exit(runRepairLocalBoxSubdomain(os.Args[2:], os.Stderr))
		case "provision-order":
			os.Exit(runProvisionOrder(os.Args[2:], os.Stderr))
		case "order-site-live":
			os.Exit(runOrderSiteLive(os.Args[2:], os.Stderr))
		case "order-start-hosting":
			os.Exit(runOrderStartHosting(os.Args[2:], os.Stderr))
		case "order-create-mailbox":
			os.Exit(runOrderCreateMailbox(os.Args[2:], os.Stderr))
		case "order-resend-mailbox-login":
			os.Exit(runOrderResendMailboxLogin(os.Args[2:], os.Stderr))
		case "order-resend-welcome":
			os.Exit(runOrderResendWelcome(os.Args[2:], os.Stderr))
		case "order-sync-brevo":
			os.Exit(runOrderSyncBrevo(os.Args[2:], os.Stderr))
		case "order-send-checklist":
			os.Exit(runOrderSendChecklist(os.Args[2:], os.Stderr))
		}
	}

	log.Printf("localbox-mail starting on %s", listenAddr)
	if err := os.MkdirAll(localBoxConfigDir, 0755); err != nil {
		log.Fatalf("could not create config dir: %v", err)
	}

	startOrderSetupRetryWorker(context.Background())
	routes := newServer()
	if err := http.ListenAndServe(listenAddr, routes); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func runProvisionOrder(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest provision-order ORD_...")
		return 2
	}
	orderID := strings.TrimSpace(args[0])
	order, err := loadOrderDetail(orderID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if isPaidOrderLocalBoxTestSubdomain(orderID) {
		if err := provisionLocalBoxTestSubdomainOrder(context.Background(), orderID); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stderr, "LocalBox test subdomain provisioned for %s\n", order.Domain)
		return 0
	}
	// Test payments may still be used to repair an already-registered domain.
	// Only block flows that would create a new registrar registration.
	if paystackTestModeEnabled() && !registrarTestRegistrationAllowed() && strings.TrimSpace(order.RegistrarReference) == "" {
		fmt.Fprintln(stderr, errRegistrarDisabledInPaymentTestMode())
		return 1
	}
	provisioner, err := newConfiguredDomainProvisioner()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := runRegistrationWorkflow(context.Background(), orderID, provisioner); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Domain registered and Cloudflare routing created for %s\n", order.Domain)
	return 0
}

func runOrderSiteLive(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest order-site-live ORD_...")
		return 2
	}
	orderID := strings.TrimSpace(args[0])
	if err := markOrderDomainPropagated(context.Background(), orderID); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	order, err := loadOrderDetail(orderID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Website link sent for %s\n", order.Domain)
	return 0
}

func runOrderCreateMailbox(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest order-create-mailbox ORD_...")
		return 2
	}
	orderID := strings.TrimSpace(args[0])
	if err := createFirstOrderMailbox(context.Background(), orderID); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	order, err := loadOrderDetail(orderID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "First mailbox %s created and login email sent.\n", order.WantedEmail)
	return 0
}

func runOrderResendMailboxLogin(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest order-resend-mailbox-login ORD_...")
		return 2
	}
	if err := resendFirstOrderMailboxLogin(args[0]); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stderr, "Mailbox login reset and emailed to the customer.")
	return 0
}

func runOrderResendWelcome(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest order-resend-welcome ORD_...")
		return 2
	}
	if err := resendOrderWelcomeEmail(args[0]); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stderr, "Welcome email and registration certificate resent to the customer.")
	return 0
}

func runOrderSyncBrevo(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest order-sync-brevo ORD_...")
		return 2
	}
	orderID := strings.TrimSpace(args[0])
	if err := ensureBrevoDomainSetupForOrder(context.Background(), orderID); err != nil {
		if isBrevoSetupPending(err) {
			_ = updateOrderFields(orderID, "provisioning", "Domain, website, mailbox, Cloudflare mail routing, and Brevo DNS records are prepared. Waiting for Brevo to confirm DNS authentication or sender activation.", map[string]any{
				"site_live_sent_at": "",
			})
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	order, err := loadOrderDetail(orderID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if order.Status == "provisioning" {
		if err := updateOrderFields(orderID, "site_live", "Domain registration completed, LocalBox hosting started, first mailbox created, Cloudflare mail routing synced, and Brevo sender domain authenticated.", map[string]any{
			"site_live_sent_at": time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	fmt.Fprintf(stderr, "Brevo sender domain DNS synced for %s\n", order.Domain)
	return 0
}

func runOrderSendChecklist(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest order-send-checklist ORD_...")
		return 2
	}
	orderID := strings.TrimSpace(args[0])
	order, err := loadOrderDetail(orderID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := sendPaidCustomerSetupChecklist(orderID, order.WebsiteURL); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Setup checklist emailed to %s for %s\n", setupChecklistRecipient, order.Domain)
	return 0
}

func runOrderStartHosting(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest order-start-hosting ORD_...")
		return 2
	}
	orderID := strings.TrimSpace(args[0])
	order, err := loadOrderDetail(orderID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := startLocalBoxHostingForAccountOrder(context.Background(), orderID, order.Email); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "LocalBox hosting started for %s: info@ mailbox, website, and routing prepared.\n", order.Domain)
	return 0
}

func runRepairLocalBoxSubdomain(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest repair-localbox-subdomain demo.localboxmail.co.za")
		return 2
	}
	domain, err := normalizePaymentDomain(args[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if !isLocalBoxTestSubdomain(domain) {
		fmt.Fprintln(stderr, "not a LocalBox subdomain")
		return 2
	}
	order, err := activeOrderForDomain(context.Background(), domain)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if order == nil {
		fmt.Fprintln(stderr, "no active order for "+domain)
		return 1
	}
	if _, err := prepareLocalBoxTestSubdomainOrder(context.Background(), order.ID); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := completeLocalBoxTestSubdomainRoutes(context.Background(), order.ID); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "LocalBox subdomain repaired for %s\n", domain)
	return 0
}

func runAddDomain(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest add-domain example.co.za")
		return 2
	}
	domain := normalizeDomain(args[0])
	if !safeDomain(domain) {
		fmt.Fprintln(stderr, "invalid domain")
		return 2
	}
	if err := addDomain(domain); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Domain added: %s\n", domain)
	return 0
}

func runAuthCheck(args []string, stdin io.Reader, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest auth-check user@example.co.za")
		return 2
	}
	user, domain, ok := splitMailboxAddress(args[0])
	if !ok {
		fmt.Fprintln(stderr, "invalid mailbox address")
		return 2
	}
	passwordData, err := io.ReadAll(io.LimitReader(stdin, 4096))
	if err != nil {
		fmt.Fprintln(stderr, "could not read password")
		return 2
	}
	password := strings.TrimRight(string(passwordData), "\r\n")
	if authenticate(user, domain, password) {
		return 0
	}
	return 1
}

func runResetPassword(args []string, stdin io.Reader, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest reset-password user@example.co.za")
		return 2
	}
	user, domain, ok := splitMailboxAddress(args[0])
	if !ok {
		fmt.Fprintln(stderr, "invalid mailbox address")
		return 2
	}
	passwordData, err := io.ReadAll(io.LimitReader(stdin, 4096))
	if err != nil {
		fmt.Fprintln(stderr, "could not read password")
		return 2
	}
	password := strings.TrimRight(string(passwordData), "\r\n")
	if err := resetMailboxPassword(user, domain, password); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Password reset for %s@%s\n", user, domain)
	return 0
}

func splitMailboxAddress(address string) (string, string, bool) {
	address = strings.ToLower(strings.TrimSpace(address))
	user, domain, ok := strings.Cut(address, "@")
	return user, domain, ok && user != "" && domain != ""
}

func runSetRecoveryEmail(args []string, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "Usage: mail-ingest set-recovery-email user@example.co.za recovery@example.net")
		return 2
	}
	user, domain, ok := splitMailboxAddress(args[0])
	if !ok {
		fmt.Fprintln(stderr, "invalid mailbox address")
		return 2
	}
	if err := updateMailboxRecoveryEmail(user, domain, args[1]); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Recovery email updated for %s@%s\n", user, domain)
	return 0
}

func runSyncEmailRoute(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest sync-email-route user@example.co.za")
		return 2
	}
	user, domain, ok := splitMailboxAddress(args[0])
	if !ok {
		fmt.Fprintln(stderr, "invalid mailbox address")
		return 2
	}
	if !userExists(user, domain) {
		fmt.Fprintln(stderr, "mailbox not found")
		return 1
	}
	if err := syncMailboxEmailRoute(user, domain); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Email route synced for %s@%s\n", user, domain)
	return 0
}

func runDeleteEmailRoute(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest delete-email-route user@example.co.za")
		return 2
	}
	user, domain, ok := splitMailboxAddress(args[0])
	if !ok {
		fmt.Fprintln(stderr, "invalid mailbox address")
		return 2
	}
	if err := deleteMailboxEmailRoute(user, domain); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Email route deleted for %s@%s\n", user, domain)
	return 0
}

func runSyncBrevoSender(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest sync-brevo-sender user@example.co.za")
		return 2
	}
	user, domain, ok := splitMailboxAddress(args[0])
	if !ok {
		fmt.Fprintln(stderr, "invalid mailbox address")
		return 2
	}
	if !userExists(user, domain) {
		fmt.Fprintln(stderr, "mailbox not found")
		return 1
	}
	if err := ensureBrevoSenderForMailbox(context.Background(), user, domain); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Brevo sender synced for %s@%s\n", user, domain)
	return 0
}

func runSendFullSetupChecklist(args []string, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: mail-ingest send-full-setup-checklist")
		return 2
	}
	if err := sendAllDomainSetupChecklist(context.Background()); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Full setup checklist emailed to %s\n", setupChecklistRecipient)
	return 0
}

func runSyncWebRoute(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest sync-web-route example.co.za")
		return 2
	}
	domain := normalizeDomain(args[0])
	if !safeDomain(domain) {
		fmt.Fprintln(stderr, "invalid domain")
		return 2
	}
	if err := syncDomainWebRoute(domain); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Web route synced for %s\n", domain)
	return 0
}

func runDeleteWebRoute(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest delete-web-route example.co.za")
		return 2
	}
	domain := normalizeDomain(args[0])
	if !safeDomain(domain) {
		fmt.Fprintln(stderr, "invalid domain")
		return 2
	}
	if err := deleteDomainWebRoute(domain); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "Web route deleted for %s\n", domain)
	return 0
}
