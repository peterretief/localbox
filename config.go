package main

import "regexp"

const (
	listenAddr = ":8080"
	mailBase   = "/mnt/data/mail"
	dataBase   = "/mnt/data/mail"
	secret     = "220961"
	usersFile  = "/etc/localbox/users.conf"
	cookieName = "localbox_session"
)

var safeLabel = regexp.MustCompile(`^[a-z0-9._-]+$`)
