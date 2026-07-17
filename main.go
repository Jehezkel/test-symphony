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
	authEmail := os.Getenv("AUTH_EMAIL")
	authPassword := os.Getenv("AUTH_PASSWORD")
	if (authEmail == "") != (authPassword == "") {
		log.Fatal("AUTH_EMAIL and AUTH_PASSWORD must be set together")
	}
	sessionTTL := 24 * time.Hour
	if raw := os.Getenv("SESSION_TTL_HOURS"); raw != "" {
		hours, parseErr := strconv.Atoi(raw)
		if parseErr != nil || hours <= 0 {
			log.Fatal("SESSION_TTL_HOURS must be a positive integer")
		}
		sessionTTL = time.Duration(hours) * time.Hour
	}
	auth := newAuthService(db, sessionTTL, os.Getenv("APP_ENV") == "production")
	mailer, err := emailSenderFromEnv()
	if err != nil {
		log.Fatalf("initialize email provider: %v", err)
	}
	auth.mailer = mailer
	if baseURL := os.Getenv("APP_BASE_URL"); baseURL != "" {
		auth.baseURL = baseURL
	}
	if authEmail != "" {
		if err := auth.ensureUser(context.Background(), authEmail, os.Getenv("AUTH_DISPLAY_NAME"), authPassword); err != nil {
			log.Fatalf("initialize authentication: %v", err)
		}
	}

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
	server := &http.Server{Addr: addr, Handler: newAuthenticatedApp(newProductStore(db), allegro, auth)}
	log.Printf("product app listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
