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
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	localBoxBase = t.TempDir()
	configDir := t.TempDir()
	usersFile = filepath.Join(configDir, "users.conf")
	adminsFile = filepath.Join(configDir, "admins.conf")
	domainsFile = filepath.Join(configDir, "domains.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		localBoxBase = oldBase
		sendOrderNotificationEmail = oldSender
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
		domainsFile = oldDomainsFile
	})
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		return nil
	}
	insertProvisioningTestOrder(t, "paid")

	provisioner := &fakeDomainProvisioner{
		zone: provisionedCloudflareZone{
			ID:          "zone123",
			NameServers: []string{"sam.ns.cloudflare.com", "marissa.ns.cloudflare.com"},
		},
		registerRef: "domainscoza-uuid",
	}

	if err := provisionPaidDomainOrderWith(context.Background(), "ord_test", provisioner); err != nil {
		t.Fatal(err)
	}

	wantCalls := strings.Join([]string{
		"check:example.co.za",
		"zone:example.co.za",
		"tunnel:zone123:example.co.za",
		"register:example.co.za",
		"nameservers:example.co.za:sam.ns.cloudflare.com,marissa.ns.cloudflare.com",
		"activation:zone123",
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
	if detail.Status != "registered" || detail.RegistrarReference != "domainscoza-uuid" || detail.CloudflareZoneID != "zone123" || detail.CloudflareNameservers != "sam.ns.cloudflare.com,marissa.ns.cloudflare.com" {
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
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() { orderDBFile = oldDB })
	insertProvisioningTestOrder(t, "paid")

	provisioner := &fakeDomainProvisioner{checkErr: fmt.Errorf("domain no longer available")}
	if err := provisionPaidDomainOrderWith(context.Background(), "ord_test", provisioner); err == nil || !strings.Contains(err.Error(), "domain no longer available") {
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
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	localBoxBase = t.TempDir()
	configDir := t.TempDir()
	usersFile = filepath.Join(configDir, "users.conf")
	adminsFile = filepath.Join(configDir, "admins.conf")
	domainsFile = filepath.Join(configDir, "domains.conf")
	t.Cleanup(func() {
		orderDBFile = oldDB
		localBoxBase = oldBase
		sendOrderNotificationEmail = oldSender
		usersFile = oldUsersFile
		adminsFile = oldAdminsFile
		domainsFile = oldDomainsFile
	})
	sendOrderNotificationEmail = func(message orderEmailMessage) error {
		return nil
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
	if err := provisionPaidDomainOrderWith(context.Background(), "ord_test", provisioner); err != nil {
		t.Fatal(err)
	}

	wantCalls := strings.Join([]string{
		"tunnel:zone123:example.co.za",
		"nameservers:example.co.za:sam.ns.cloudflare.com,marissa.ns.cloudflare.com",
		"activation:zone123",
	}, "|")
	if got := strings.Join(provisioner.calls, "|"); got != wantCalls {
		t.Fatalf("unexpected calls:\n got: %s\nwant: %s", got, wantCalls)
	}
	detail, err := loadOrderDetail("ord_test")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "registered" || detail.RegistrarReference != "domainscoza-uuid" {
		t.Fatalf("unexpected resumed order: %#v", detail)
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
