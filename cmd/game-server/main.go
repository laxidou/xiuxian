package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"

	worldv1 "xiuxian/gen/go/xiuxian/v1"
	"xiuxian/internal/api"
	"xiuxian/internal/rpc"
	"xiuxian/internal/storage"
	"xiuxian/internal/world"
)

func main() {
	address := os.Getenv("GAME_SERVER_ADDRESS")
	if address == "" {
		address = ":8080"
	}
	grpcAddress := os.Getenv("GAME_SERVER_GRPC_ADDRESS")
	if grpcAddress == "" {
		grpcAddress = ":9090"
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
		Addr: address,
		Handler: api.NewHandler(service, api.Options{
			SecureCookies: os.Getenv("COOKIE_SECURE") != "false",
			WorkerToken:   os.Getenv("WORKER_TOKEN"),
			Version:       os.Getenv("APP_VERSION"),
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", grpcAddress)
	if err != nil {
		log.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	worldv1.RegisterWorldServiceServer(grpcServer, rpc.NewServer(service))
	grpc_health_v1.RegisterHealthServer(grpcServer, health.NewServer())
	go func() {
		log.Printf("game-server gRPC listening on %s", grpcAddress)
		if err := grpcServer.Serve(listener); err != nil {
			log.Fatal(err)
		}
	}()
	log.Printf("game-server listening on %s", address)
	log.Fatal(server.ListenAndServe())
}
