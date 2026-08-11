package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeDomainProvisioner struct {
	calls           []string
	checkErr        error
	zone            provisionedCloudflareZone
	zoneErr         error
	registerRef     string
	registerErr     error
	nameserversSeen []string
}

func (f *fakeDomainProvisioner) CheckDomainAvailable(ctx context.Context, domain string) error {
	f.calls = append(f.calls, "check:"+domain)
	return f.checkErr
}

func (f *fakeDomainProvisioner) CreateCloudflareZone(ctx context.Context, domain string) (provisionedCloudflareZone, error) {
	f.calls = append(f.calls, "zone:"+domain)
	return f.zone, f.zoneErr
}

func (f *fakeDomainProvisioner) RegisterDomain(ctx context.Context, order *OrderDetail, nameservers []string) (string, error) {
	f.calls = append(f.calls, "register:"+order.Domain)
	f.nameserversSeen = append([]string(nil), nameservers...)
	return f.registerRef, f.registerErr
}

func (f *fakeDomainProvisioner) UpdateNameservers(ctx context.Context, domain string, nameservers []string) error {
	f.calls = append(f.calls, "nameservers:"+domain+":"+strings.Join(nameservers, ","))
	return nil
}

func (f *fakeDomainProvisioner) UpsertTunnelRecords(ctx context.Context, zoneID, domain string) error {
	f.calls = append(f.calls, "tunnel:"+zoneID+":"+domain)
	return nil
}

func (f *fakeDomainProvisioner) CheckZoneActivation(ctx context.Context, zoneID string) error {
	f.calls = append(f.calls, "activation:"+zoneID)
	return nil
}

func TestProvisionPaidDomainOrderWithFakesCompletesOrder(t *testing.T) {
	oldDB := orderDBFile
	oldBase := localBoxBase
	oldSender := sendOrderNotificationEmail
	oldUsersFile := usersFile
	oldAdminsFile := adminsFile
	oldDomainsFile := domainsFile
	oldRegistrarConfigFile := registrarConfigFile
	oldBrevoSetup := ensureBrevoDomainSetupForOrder
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	localBoxBase = t.TempDir()
	configDir := t.TempDir()
	usersFile = filepath.Join(configDir, "users.conf")
	adminsFile = filepath.Join(configDir, "admins.conf")
	domainsFile = filepath.Join(configDir, "domains.conf")
	registrarConfigFile = filepath.Join(configDir, "registrar.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		localBoxBase = oldBase
		sendOrderNotificationEmail = oldSender
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
		domainsFile = oldDomainsFile
		registrarConfigFile = oldRegistrarConfigFile
		ensureBrevoDomainSetupForOrder = oldBrevoSetup
	})
	enableRegistrarForTest(t, registrarConfigFile)
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		return nil
	}
	ensureBrevoDomainSetupForOrder = func(ctx context.Context, id string) error {
		return appendOrderEvent(id, "brevo_setup_verified", "Brevo sender domain DNS setup completed for example.co.za.")
	}
	insertProvisioningTestOrder(t, "paid")

	provisioner := &fakeDomainProvisioner{
		zone: provisionedCloudflareZone{
			ID:          "zone123",
			NameServers: []string{"sam.ns.cloudflare.com", "marissa.ns.cloudflare.com"},
		},
		registerRef: "domainscoza-uuid",
	}

	if err := runRegistrationWorkflow(context.Background(), "ord_test", provisioner); err != nil {
		t.Fatal(err)
	}

	wantCalls := strings.Join([]string{
		"check:example.co.za",
		"zone:example.co.za",
		"register:example.co.za",
		"nameservers:example.co.za:sam.ns.cloudflare.com,marissa.ns.cloudflare.com",
		"activation:zone123",
		"tunnel:zone123:example.co.za",
	}, "|")
	if got := strings.Join(provisioner.calls, "|"); got != wantCalls {
		t.Fatalf("unexpected calls:\n got: %s\nwant: %s", got, wantCalls)
	}
	if strings.Join(provisioner.nameserversSeen, ",") != "sam.ns.cloudflare.com,marissa.ns.cloudflare.com" {
		t.Fatalf("registrar did not receive Cloudflare nameservers: %v", provisioner.nameserversSeen)
	}

	detail, err := loadOrderDetail("ord_test")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "site_live" || detail.RegistrarReference != "domainscoza-uuid" || detail.CloudflareZoneID != "zone123" || detail.CloudflareNameservers != "sam.ns.cloudflare.com,marissa.ns.cloudflare.com" {
		t.Fatalf("unexpected provisioned order: %#v", detail)
	}
	if _, err := os.Stat(registrationCertificatePath("example.co.za")); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareCloudflareForDomainOrderStoresGeneratedNameservers(t *testing.T) {
	oldDB := orderDBFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })
	insertProvisioningTestOrder(t, "paid")

	provisioner := &fakeDomainProvisioner{
		zone: provisionedCloudflareZone{
			ID:          "zone456",
			NameServers: []string{"bella.ns.cloudflare.com", "nikon.ns.cloudflare.com"},
		},
	}

	zone, err := prepareCloudflareForDomainOrderWith(context.Background(), "ord_test", provisioner)
	if err != nil {
		t.Fatal(err)
	}
	if zone.ID != "zone456" || strings.Join(zone.NameServers, ",") != "bella.ns.cloudflare.com,nikon.ns.cloudflare.com" {
		t.Fatalf("unexpected zone: %#v", zone)
	}
	wantCalls := "zone:example.co.za|tunnel:zone456:example.co.za"
	if got := strings.Join(provisioner.calls, "|"); got != wantCalls {
		t.Fatalf("unexpected calls:\n got: %s\nwant: %s", got, wantCalls)
	}
	detail, err := loadOrderDetail("ord_test")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "ready_for_registration" || detail.CloudflareZoneID != "zone456" || detail.CloudflareNameservers != "bella.ns.cloudflare.com,nikon.ns.cloudflare.com" {
		t.Fatalf("unexpected prepared order: %#v", detail)
	}
}

func TestProvisionPaidDomainOrderWithFakesMarksFailureRetryable(t *testing.T) {
	oldDB := orderDBFile
	oldRegistrarConfigFile := registrarConfigFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	registrarConfigFile = filepath.Join(t.TempDir(), "registrar.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		registrarConfigFile = oldRegistrarConfigFile
	})
	enableRegistrarForTest(t, registrarConfigFile)
	insertProvisioningTestOrder(t, "paid")

	provisioner := &fakeDomainProvisioner{checkErr: fmt.Errorf("domain no longer available")}
	if err := runRegistrationWorkflow(context.Background(), "ord_test", provisioner); err == nil || !strings.Contains(err.Error(), "domain no longer available") {
		t.Fatalf("expected availability error, got %v", err)
	}

	detail, err := loadOrderDetail("ord_test")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "provisioning_failed" {
		t.Fatalf("expected provisioning_failed status, got %#v", detail)
	}
	if len(provisioner.calls) != 1 || provisioner.calls[0] != "check:example.co.za" {
		t.Fatalf("expected only availability check, got %v", provisioner.calls)
	}
}

func TestProvisionPaidDomainOrderWithFakesResumesAfterRegistrarSuccess(t *testing.T) {
	oldDB := orderDBFile
	oldBase := localBoxBase
	oldSender := sendOrderNotificationEmail
	oldUsersFile := usersFile
	oldAdminsFile := adminsFile
	oldDomainsFile := domainsFile
	oldRegistrarConfigFile := registrarConfigFile
	oldBrevoSetup := ensureBrevoDomainSetupForOrder
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	localBoxBase = t.TempDir()
	configDir := t.TempDir()
	usersFile = filepath.Join(configDir, "users.conf")
	adminsFile = filepath.Join(configDir, "admins.conf")
	domainsFile = filepath.Join(configDir, "domains.conf")
	registrarConfigFile = filepath.Join(configDir, "registrar.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		localBoxBase = oldBase
		sendOrderNotificationEmail = oldSender
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
		domainsFile = oldDomainsFile
		registrarConfigFile = oldRegistrarConfigFile
		ensureBrevoDomainSetupForOrder = oldBrevoSetup
	})
	enableRegistrarForTest(t, registrarConfigFile)
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		return nil
	}
	ensureBrevoDomainSetupForOrder = func(ctx context.Context, id string) error {
		return appendOrderEvent(id, "brevo_setup_verified", "Brevo sender domain DNS setup completed for example.co.za.")
	}
	insertProvisioningTestOrder(t, "provisioning_failed")
	if err := updateOrderFields("ord_test", "provisioning_failed", "Previous attempt registered domain but failed later.", map[string]any{
		"registrar_reference":    "domainscoza-uuid",
		"cloudflare_zone_id":     "zone123",
		"cloudflare_nameservers": "sam.ns.cloudflare.com,marissa.ns.cloudflare.com",
	}); err != nil {
		t.Fatal(err)
	}

	provisioner := &fakeDomainProvisioner{checkErr: fmt.Errorf("domain no longer available")}
	if err := runRegistrationWorkflow(context.Background(), "ord_test", provisioner); err != nil {
		t.Fatal(err)
	}

	wantCalls := strings.Join([]string{
		"nameservers:example.co.za:sam.ns.cloudflare.com,marissa.ns.cloudflare.com",
		"activation:zone123",
		"tunnel:zone123:example.co.za",
	}, "|")
	if got := strings.Join(provisioner.calls, "|"); got != wantCalls {
		t.Fatalf("unexpected calls:\n got: %s\nwant: %s", got, wantCalls)
	}
	detail, err := loadOrderDetail("ord_test")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "site_live" || detail.RegistrarReference != "domainscoza-uuid" {
		t.Fatalf("unexpected resumed order: %#v", detail)
	}
}

func TestProvisionPaidDomainOrderBlocksWhenRegistrarDisabled(t *testing.T) {
	oldDB := orderDBFile
	oldRegistrarConfigFile := registrarConfigFile
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	registrarConfigFile = filepath.Join(t.TempDir(), "missing-registrar.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		registrarConfigFile = oldRegistrarConfigFile
	})
	insertProvisioningTestOrder(t, "paid")

	provisioner := &fakeDomainProvisioner{
		zone: provisionedCloudflareZone{
			ID:          "zone123",
			NameServers: []string{"sam.ns.cloudflare.com", "marissa.ns.cloudflare.com"},
		},
		registerRef: "domainscoza-uuid",
	}

	err := runRegistrationWorkflow(context.Background(), "ord_test", provisioner)
	if err == nil || !strings.Contains(err.Error(), "registrar live registration is disabled") {
		t.Fatalf("expected disabled registrar error, got %v", err)
	}
	if len(provisioner.calls) != 0 {
		t.Fatalf("registrar disabled should block before provider calls, got %v", provisioner.calls)
	}
}

func TestReusableTestRegistrarReferenceRequiresDevelopmentMode(t *testing.T) {
	oldRegistrarConfigFile := registrarConfigFile
	registrarConfigFile = filepath.Join(t.TempDir(), "registrar.conf")
	t.Cleanup(func() { registrarConfigFile = oldRegistrarConfigFile })

	if err := os.WriteFile(registrarConfigFile, []byte("mode=development\nreusable_test_domain=example.co.za\nreusable_test_registrar_reference=dev-uuid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if reference, ok := reusableTestRegistrarReference("example.co.za"); !ok || reference != "dev-uuid" {
		t.Fatalf("unexpected reusable reference %q, %v", reference, ok)
	}
	if _, ok := reusableTestRegistrarReference("other.co.za"); ok {
		t.Fatal("unexpected reusable reference for another domain")
	}
	if err := os.WriteFile(registrarConfigFile, []byte("mode=live\nreusable_test_domain=example.co.za\nreusable_test_registrar_reference=dev-uuid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := reusableTestRegistrarReference("example.co.za"); ok {
		t.Fatal("live mode must not enable a reusable test domain")
	}
}

func TestRegistrarConfigSupportsLiveModeAllowlistAndBalanceFloor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registrar.conf")
	if err := os.WriteFile(path, []byte("mode=live\nallow_domain=example.co.za\nmin_live_balance=150.00\n"), 0640); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadRegistrarConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "live" || !cfg.AllowDomains["example.co.za"] || cfg.MinLiveBalanceCents != 15000 {
		t.Fatalf("unexpected registrar config: %#v", cfg)
	}
}

func TestDomainsCoZaBalanceCentsPrefersFundsFields(t *testing.T) {
	response := domainsCoZaFundsResponse{
		StrBalance: "R250.50",
	}
	response.ObjReseller.Balance = "R300.00"
	if got := domainsCoZaBalanceCents(response); got != 25050 {
		t.Fatalf("balance cents = %d", got)
	}
}

func enableRegistrarForTest(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("mode=live\n"), 0640); err != nil {
		t.Fatal(err)
	}
}

func insertProvisioningTestOrder(t *testing.T, status string) {
	t.Helper()
	db, err := openOrdersDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureOrdersAdminSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO customers (id, full_name, email, phone, physical_address, created_at)
		VALUES (1, 'Buyer Example', 'buyer@example.net', '+27 82 123 4567', '1 Example Street, Cape Town', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO orders (id, customer_id, domain, wanted_email, status, notes, payment_reference, created_at, updated_at)
		VALUES ('ord_test', 1, 'example.co.za', 'info@example.co.za', ?, '', 'pay_123', ?, ?)
	`, status, now, now); err != nil {
		t.Fatal(err)
	}
}
