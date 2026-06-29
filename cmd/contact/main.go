package main

import (
	"log"
	"net/http"
)

func main() {
	router := routes()

	log.Println("Contact service listening on :8081")

	log.Fatal(http.ListenAndServe(":8081", router))
}
