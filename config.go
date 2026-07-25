package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
)

const (
	listenAddr          = ":8080"
	defaultIngestSecret = "220961"
	cookieName          = "localbox_session"
)

var (
	usersFile              = "/etc/localbox/users.conf"
	domainsFile            = "/etc/localbox/domains.conf"
	adminsFile             = "/etc/localbox/admins.conf"
	boxAdminsFile          = "/etc/localbox/box-admins.conf"
	dovecotUsersFile       = "/etc/dovecot/localbox/users"
	radicaleConfigFile     = "/etc/radicale/config"
	radicaleUsersFile      = "/etc/radicale/users"
	localBoxSSOSecretFile  = "/etc/localbox/sso-secret"
	passwordResetTokenFile = "/etc/localbox/password-reset-tokens.conf"
	securityLogFile        = "/var/localbox/security.log"
	localBoxBase           = "/mnt/data/localbox"
	orderDBFile            = "/mnt/data/localbox/localbox.db"
	paystackConfigFile     = "/etc/localbox/paystack.conf"
	domainsCoZaConfigFile  = "/etc/localbox/domainscoza.conf"
	cloudflareConfigFile   = "/etc/localbox/cloudflare.conf"
	ingestSecretFile       = "/etc/localbox/secret"
	ingestSecret           = defaultIngestSecret
	safeLabel              = regexp.MustCompile(`^[a-z0-9._-]+$`)
)

func applyRuntimeConfig() {
	if base := os.Getenv("LOCALBOX_BASE"); base != "" {
		localBoxBase = base
		orderDBFile = filepath.Join(base, "localbox.db")
	}
	if dbFile := os.Getenv("LOCALBOX_ORDER_DB"); dbFile != "" {
		orderDBFile = dbFile
	}
	if secret := os.Getenv("LOCALBOX_SECRET"); secret != "" {
		ingestSecret = secret
	} else if data, err := os.ReadFile(ingestSecretFile); err == nil {
		if secret := string(bytes.TrimSpace(data)); secret != "" {
			ingestSecret = secret
		}
	}
}
