package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/turboci-lite/internal/endpoint"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage/sqlite"
	grpcTransport "github.com/example/turboci-lite/internal/transport/grpc"
)

// Config holds the server configuration.
type Config struct {
	GRPCPort   int
	SQLitePath string
}

func main() {
	// Load configuration
	cfg := loadConfig()

	// Initialize storage
	log.Printf("Initializing SQLite storage at %s", cfg.SQLitePath)
	store, err := sqlite.New(cfg.SQLitePath)
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Run migrations
	log.Println("Running database migrations...")
	if err := store.Migrate(context.Background()); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Create service
	svc := service.NewOrchestrator(store)

	// Create endpoints
	endpoints := endpoint.MakeEndpoints(svc)

	// Create and start gRPC server
	server := grpcTransport.NewServer(endpoints)

	// Handle graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down server...")
		server.GracefulStop()
	}()

	// Start server
	addr := fmt.Sprintf(":%d", cfg.GRPCPort)
	log.Printf("Starting TurboCI-Lite server on %s", addr)
	if err := server.Serve(addr); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func loadConfig() Config {
	cfg := Config{
		GRPCPort:   50051,
		SQLitePath: "turboci.db",
	}

	// Override from environment
	if port := os.Getenv("GRPC_PORT"); port != "" {
		if _, err := fmt.Sscanf(port, "%d", &cfg.GRPCPort); err != nil {
			log.Printf("Invalid GRPC_PORT, using default: %v", err)
		}
	}

	if path := os.Getenv("SQLITE_PATH"); path != "" {
		cfg.SQLitePath = path
	}

	return cfg
}
