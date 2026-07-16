package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"xiuxian/internal/api"
	"xiuxian/internal/storage"
	"xiuxian/internal/world"
)

func main() {
	address := os.Getenv("GAME_SERVER_ADDRESS")
	if address == "" {
		address = ":8080"
	}
	service := world.NewService(world.SystemClock{})
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		store, err := storage.OpenPostgres(context.Background(), databaseURL)
		if err != nil {
			log.Fatal(err)
		}
		defer store.Close()
		service, err = world.NewPersistentService(context.Background(), world.SystemClock{}, store)
		if err != nil {
			log.Fatal(err)
		}
	}
	server := &http.Server{
		Addr:              address,
		Handler:           api.NewHandler(service, api.Options{SecureCookies: os.Getenv("COOKIE_SECURE") != "false"}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("game-server listening on %s", address)
	log.Fatal(server.ListenAndServe())
}
