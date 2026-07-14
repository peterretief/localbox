package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	applyRuntimeConfig()
	if len(os.Args) >= 2 && os.Args[1] == "test-smtp" {
		domain := "example.com"
		if len(os.Args) >= 3 {
			domain = os.Args[2]
		}
		if err := sendContactMessage(domain, "LocalBox SMTP Test", "test@localbox.invalid", "SMTP test", "This is a LocalBox SMTP test message."); err != nil {
			log.Fatalf("SMTP test failed: %v", err)
		}
		log.Println("SMTP test completed")
		return
	}

	router := routes()

	log.Println("Contact service listening on :8081")

	log.Fatal(http.ListenAndServe(":8081", router))
}
