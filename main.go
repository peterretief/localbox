package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	applyRuntimeConfig()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "auth-check":
			os.Exit(runAuthCheck(os.Args[2:], os.Stdin, os.Stderr))
		}
	}

	log.Printf("localbox-mail starting on %s", listenAddr)
	if err := os.MkdirAll("/etc/localbox", 0755); err != nil {
		log.Fatalf("could not create config dir: %v", err)
	}

	routes := newServer()
	if err := http.ListenAndServe(listenAddr, routes); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func runAuthCheck(args []string, stdin io.Reader, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: mail-ingest auth-check user@example.co.za")
		return 2
	}
	address := strings.ToLower(strings.TrimSpace(args[0]))
	user, domain, ok := strings.Cut(address, "@")
	if !ok || user == "" || domain == "" {
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
