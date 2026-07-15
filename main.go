package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	server := &http.Server{Addr: addr, Handler: newApp(newProductStore())}
	log.Printf("product app listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
