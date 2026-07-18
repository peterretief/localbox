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
		case "reset-password":
			os.Exit(runResetPassword(os.Args[2:], os.Stdin, os.Stderr))
		case "set-recovery-email":
			os.Exit(runSetRecoveryEmail(os.Args[2:], os.Stderr))
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
