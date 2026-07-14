package main

import (
	"context"
	"fmt"
	"strings"
)

type realDomainProvisioner struct {
	domainsCoZa *domainsCoZaClient
	cloudflare  *cloudflareClient
}

func newRealDomainProvisioner() (*realDomainProvisioner, error) {
	domainsCfg, err := loadDomainsCoZaConfig(domainsCoZaConfigFile)
	if err != nil {
		return nil, fmt.Errorf("Domains.co.za config not available: %w", err)
	}
	if err := domainsCfg.validate(); err != nil {
		return nil, err
	}
	cloudflareCfg, err := loadCloudflareConfig(cloudflareConfigFile)
	if err != nil {
		return nil, fmt.Errorf("Cloudflare config not available: %w", err)
	}
	if err := cloudflareCfg.validate(); err != nil {
		return nil, err
	}
	return &realDomainProvisioner{
		domainsCoZa: newDomainsCoZaClient(domainsCfg),
		cloudflare:  newCloudflareClient(cloudflareCfg),
	}, nil
}

func (p *realDomainProvisioner) CheckDomainAvailable(ctx context.Context, domain string) error {
	sld, tld, err := splitZADomain(domain)
	if err != nil {
		return err
	}
	result, err := p.domainsCoZa.CheckDomain(ctx, sld, tld)
	if err != nil {
		return err
	}
	if !result.Available {
		reason := strings.TrimSpace(result.Reason)
		if reason == "" {
			reason = strings.TrimSpace(result.Message)
		}
		if reason == "" {
			reason = "not available"
		}
		return fmt.Errorf("%s is %s", domain, reason)
	}
	return nil
}

func (p *realDomainProvisioner) CreateCloudflareZone(ctx context.Context, domain string) (provisionedCloudflareZone, error) {
	zone, err := p.cloudflare.CreateZone(ctx, domain)
	if err != nil {
		return provisionedCloudflareZone{}, err
	}
	return provisionedCloudflareZone{ID: zone.ID, NameServers: zone.NameServers}, nil
}

func (p *realDomainProvisioner) RegisterDomain(ctx context.Context, order *OrderDetail, nameservers []string) (string, error) {
	if order == nil {
		return "", fmt.Errorf("order is required")
	}
	sld, tld, err := splitZADomain(order.Domain)
	if err != nil {
		return "", err
	}
	result, err := p.domainsCoZa.RegisterDomain(ctx, DomainsCoZaRegistration{
		SLD:         sld,
		TLD:         tld,
		PeriodYears: 1,
		Nameservers: nameservers,
		Registrant:  domainsCoZaRegistrantForOrder(order),
	})
	if err != nil {
		return "", err
	}
	if result.UUID != "" {
		return result.UUID, nil
	}
	return result.DomainName, nil
}

func (p *realDomainProvisioner) UpdateNameservers(ctx context.Context, domain string, nameservers []string) error {
	sld, tld, err := splitZADomain(domain)
	if err != nil {
		return err
	}
	return p.domainsCoZa.UpdateNameservers(ctx, sld, tld, nameservers)
}

func (p *realDomainProvisioner) UpsertTunnelRecords(ctx context.Context, zoneID, domain string) error {
	return p.cloudflare.UpsertTunnelRecords(ctx, zoneID, domain)
}

func (p *realDomainProvisioner) CheckZoneActivation(ctx context.Context, zoneID string) error {
	return p.cloudflare.CheckZoneActivation(ctx, zoneID)
}

func domainsCoZaRegistrantForOrder(order *OrderDetail) DomainsCoZaRegistrant {
	if profile, err := loadPublicUserProfileByEmail(order.Email); err == nil {
		return DomainsCoZaRegistrant{
			Name:          profile.FullName,
			Email:         profile.Email,
			Country:       profile.Country,
			Province:      profile.Province,
			ContactNumber: profile.Phone,
			PostalCode:    profile.PostalCode,
			Address1:      profile.AddressLine1,
			Address2:      profile.AddressLine2,
			City:          profile.City,
		}
	}
	address1 := strings.TrimSpace(order.PhysicalAddress)
	if address1 == "" {
		address1 = "not provided"
	}
	return DomainsCoZaRegistrant{
		Name:          order.FullName,
		Email:         order.Email,
		Country:       "ZA",
		Province:      "not provided",
		ContactNumber: order.Phone,
		PostalCode:    "0000",
		Address1:      address1,
		City:          "not provided",
	}
}

func normalizePublicDomain(value string) (string, error) {
	return normalizePaymentDomain(value)
}

func validDomainLabel(value string) bool {
	return validPaymentDomainLabel(value)
}
