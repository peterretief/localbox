package main

import "regexp"

const (
	listenAddr    = ":8080"
	secret        = "220961"
	usersFile     = "/etc/localbox/users.conf"
	domainsFile   = "/etc/localbox/domains.conf"
	adminsFile    = "/etc/localbox/admins.conf"
	boxAdminsFile = "/etc/localbox/box-admins.conf"
	cookieName    = "localbox_session"
)

var (
	localBoxBase = "/mnt/data/localbox"
	safeLabel    = regexp.MustCompile(`^[a-z0-9._-]+$`)
)
