package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	log.Printf("localbox-mail starting on %s", listenAddr)
	if err := os.MkdirAll("/etc/localbox", 0755); err != nil {
		log.Fatalf("could not create config dir: %v", err)
	}

	routes := newServer()
	if err := http.ListenAndServe(listenAddr, routes); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
