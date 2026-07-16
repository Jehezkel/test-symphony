package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func main() {
	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	databasePath := os.Getenv("DATABASE_PATH")
	if databasePath == "" {
		databasePath = "data/app.db"
	}
	if dir := filepath.Dir(databasePath); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			log.Fatalf("create database directory: %v", err)
		}
	}
	db, err := openDatabase(databasePath)
	if err != nil {
		log.Fatalf("initialize database: %v", err)
	}
	defer db.Close()

	allegroConfig, err := allegroConfigFromEnv()
	if err != nil {
		log.Fatalf("initialize Allegro integration: %v", err)
	}
	allegro := newAllegroService(db, allegroConfig, nil)
	interval := 15 * time.Minute
	if raw := os.Getenv("ALLEGRO_SYNC_INTERVAL_MINUTES"); raw != "" {
		minutes, parseErr := strconv.Atoi(raw)
		if parseErr != nil || minutes < 0 {
			log.Fatal("ALLEGRO_SYNC_INTERVAL_MINUTES must be a non-negative integer")
		}
		interval = time.Duration(minutes) * time.Minute
	}
	allegro.startScheduler(context.Background(), interval)
	server := &http.Server{Addr: addr, Handler: newApp(newProductStore(db), allegro)}
	log.Printf("product app listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
