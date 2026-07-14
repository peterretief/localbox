package main

import (
	"os"
	"path/filepath"
)

const (
	defaultContactMailboxUser = "info"
)

var (
	contactLocalBoxBase   = "/mnt/data/localbox"
	contactUsersFile      = "/etc/localbox/users.conf"
	contactAdminsFile     = "/etc/localbox/admins.conf"
	contactBoxAdminsFile  = "/etc/localbox/box-admins.conf"
	orderDBFile           = "/mnt/data/localbox/localbox.db"
	domainsCoZaConfigFile = "/etc/localbox/domainscoza.conf"
	cloudflareConfigFile  = "/etc/localbox/cloudflare.conf"
	paystackConfigFile    = "/etc/localbox/paystack.conf"
)

func applyRuntimeConfig() {
	if base := os.Getenv("LOCALBOX_BASE"); base != "" {
		contactLocalBoxBase = base
		orderDBFile = filepath.Join(base, "localbox.db")
	}
	if dbFile := os.Getenv("LOCALBOX_ORDER_DB"); dbFile != "" {
		orderDBFile = dbFile
	}
}
