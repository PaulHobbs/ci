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
	GRPCPort        int
	SQLitePath      string
	CallbackAddress string
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

	// Create services
	orchestratorSvc := service.NewOrchestrator(store)
	runnerSvc := service.NewRunnerService(store)
	callbackSvc := service.NewCallbackService(store, orchestratorSvc)

	// Create dispatcher with configuration
	dispatcherCfg := service.DefaultDispatcherConfig()
	dispatcherCfg.CallbackAddress = cfg.CallbackAddress
	dispatcher := service.NewDispatcher(store, runnerSvc, orchestratorSvc, dispatcherCfg)

	// Create endpoints
	endpoints := endpoint.MakeEndpoints(orchestratorSvc)

	// Create gRPC server with services
	server := grpcTransport.NewServer(
		endpoints,
		grpcTransport.WithRunnerService(runnerSvc),
		grpcTransport.WithCallbackService(callbackSvc),
	)

	// Start dispatcher
	log.Println("Starting dispatcher...")
	dispatcher.Start()

	// Handle graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")

		// Stop dispatcher first
		log.Println("Stopping dispatcher...")
		dispatcher.Stop()

		// Then stop gRPC server
		log.Println("Stopping gRPC server...")
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
		GRPCPort:        50051,
		SQLitePath:      "turboci.db",
		CallbackAddress: "localhost:50051",
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

	if addr := os.Getenv("CALLBACK_ADDRESS"); addr != "" {
		cfg.CallbackAddress = addr
	}

	return cfg
}
