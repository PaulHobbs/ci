package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/endpoint"
	"github.com/example/turboci-lite/internal/observability"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage/sqlite"
	grpcTransport "github.com/example/turboci-lite/internal/transport/grpc"
	"github.com/example/turboci-lite/internal/web"
)

// Config holds the server configuration.
type Config struct {
	GRPCPort        int
	WebPort         int
	SQLitePath      string
	CallbackAddress string
}

func main() {
	// Load configuration
	cfg := loadConfig()

	// Enable profiling
	runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(1)

	// Create metrics infrastructure
	metrics := observability.NewMetrics()

	// Start debug server for pprof and metrics
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics)
		// pprof endpoints are registered automatically via import
		log.Println("Starting debug server on :6060 (pprof + metrics)")
		if err := http.ListenAndServe(":6060", mux); err != nil {
			log.Printf("Debug server error: %v", err)
		}
	}()

	// Initialize storage with metrics
	log.Printf("Initializing SQLite storage at %s", cfg.SQLitePath)
	store, err := sqlite.NewWithMetrics(cfg.SQLitePath, metrics)
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Run migrations
	log.Println("Running database migrations...")
	if err := store.Migrate(context.Background()); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Create services with metrics
	orchestratorSvc := service.NewOrchestratorWithMetrics(store, metrics)
	runnerSvc := service.NewRunnerService(store)
	callbackSvc := service.NewCallbackService(store, orchestratorSvc)

	// Create dispatcher with configuration and metrics
	dispatcherCfg := service.DefaultDispatcherConfig()
	dispatcherCfg.CallbackAddress = cfg.CallbackAddress
	dispatcher := service.NewDispatcherWithMetrics(store, runnerSvc, orchestratorSvc, dispatcherCfg, metrics)

	// Wire orchestrator to dispatcher for event-driven dispatch
	orchestratorSvc.SetDispatcher(dispatcher)

	// Create gRPC client factory for dispatcher to call runners
	dispatcher.SetClientFactory(func(addr string) (service.StageRunnerClient, error) {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, err
		}
		return &grpcRunnerClient{client: pb.NewStageRunnerClient(conn), conn: conn}, nil
	})

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

	// Start web server
	webAddr := fmt.Sprintf(":%d", cfg.WebPort)
	webServer := web.NewServer(webAddr, orchestratorSvc, store)
	go func() {
		log.Printf("Starting web UI on %s", webAddr)
		if err := webServer.Start(); err != nil && err != http.ErrServerClosed {
			log.Printf("Web server error: %v", err)
		}
	}()

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
		WebPort:         8080,
		SQLitePath:      "turboci.db",
		CallbackAddress: "localhost:50051",
	}

	// Override from environment
	if port := os.Getenv("GRPC_PORT"); port != "" {
		if _, err := fmt.Sscanf(port, "%d", &cfg.GRPCPort); err != nil {
			log.Printf("Invalid GRPC_PORT, using default: %v", err)
		}
	}

	if port := os.Getenv("WEB_PORT"); port != "" {
		if _, err := fmt.Sscanf(port, "%d", &cfg.WebPort); err != nil {
			log.Printf("Invalid WEB_PORT, using default: %v", err)
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

// grpcRunnerClient wraps a gRPC client to implement StageRunnerClient
type grpcRunnerClient struct {
	client pb.StageRunnerClient
	conn   *grpc.ClientConn
}

func (c *grpcRunnerClient) Run(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
	// Convert service types to proto types
	pbReq := &pb.RunRequest{
		ExecutionId: req.ExecutionID,
		WorkPlanId:  req.WorkPlanID,
		StageId:     req.StageID,
		AttemptIdx:  int32(req.AttemptIdx),
	}

	// Convert stage args
	if req.Args != nil {
		pbReq.Args, _ = structpb.NewStruct(req.Args)
	}

	// Convert check options
	for _, opt := range req.CheckOptions {
		pbOpt := &pb.CheckOptions{
			CheckId: opt.CheckID,
			Kind:    opt.Kind,
		}
		if opt.Options != nil {
			pbOpt.Options, _ = structpb.NewStruct(opt.Options)
		}
		pbReq.CheckOptions = append(pbReq.CheckOptions, pbOpt)
		pbReq.AssignedCheckIds = append(pbReq.AssignedCheckIds, opt.CheckID)
	}

	pbResp, err := c.client.Run(ctx, pbReq)
	if err != nil {
		return nil, err
	}

	resp := &service.RunResponse{
		StageState: convertStageState(pbResp.StageState),
	}

	for _, update := range pbResp.CheckUpdates {
		cu := &service.CheckUpdate{
			CheckID:  update.CheckId,
			State:    convertCheckState(update.State),
			Finalize: update.Finalize,
		}
		if update.ResultData != nil {
			cu.Data = update.ResultData.AsMap()
		}
		if update.Failure != nil {
			cu.Failure = &domain.Failure{Message: update.Failure.Message}
		}
		resp.CheckUpdates = append(resp.CheckUpdates, cu)
	}

	if pbResp.Failure != nil {
		resp.Failure = &domain.Failure{Message: pbResp.Failure.Message}
	}

	return resp, nil
}

func (c *grpcRunnerClient) RunAsync(ctx context.Context, req *service.RunAsyncRequest) (*service.RunAsyncResponse, error) {
	pbReq := &pb.RunAsyncRequest{
		ExecutionId: req.ExecutionID,
		WorkPlanId:  req.WorkPlanID,
		StageId:     req.StageID,
		AttemptIdx:  int32(req.AttemptIdx),
	}

	if req.Args != nil {
		pbReq.Args, _ = structpb.NewStruct(req.Args)
	}

	for _, opt := range req.CheckOptions {
		pbOpt := &pb.CheckOptions{
			CheckId: opt.CheckID,
			Kind:    opt.Kind,
		}
		if opt.Options != nil {
			pbOpt.Options, _ = structpb.NewStruct(opt.Options)
		}
		pbReq.CheckOptions = append(pbReq.CheckOptions, pbOpt)
	}

	pbResp, err := c.client.RunAsync(ctx, pbReq)
	if err != nil {
		return nil, err
	}

	return &service.RunAsyncResponse{Accepted: pbResp.Accepted}, nil
}

func (c *grpcRunnerClient) Ping(ctx context.Context, req *service.PingRequest) (*service.PingResponse, error) {
	pbResp, err := c.client.Ping(ctx, &pb.PingRequest{})
	if err != nil {
		return nil, err
	}
	return &service.PingResponse{Healthy: pbResp.AvailableCapacity > 0}, nil
}

func convertStageState(s pb.StageState) domain.StageState {
	switch s {
	case pb.StageState_STAGE_STATE_PLANNED:
		return domain.StageStatePlanned
	case pb.StageState_STAGE_STATE_ATTEMPTING:
		return domain.StageStateAttempting
	case pb.StageState_STAGE_STATE_AWAITING_GROUP:
		return domain.StageStateAwaitingGroup
	case pb.StageState_STAGE_STATE_FINAL:
		return domain.StageStateFinal
	default:
		return domain.StageStateUnknown
	}
}

func convertCheckState(s pb.CheckState) domain.CheckState {
	switch s {
	case pb.CheckState_CHECK_STATE_PLANNED:
		return domain.CheckStatePlanned
	case pb.CheckState_CHECK_STATE_WAITING:
		return domain.CheckStateWaiting
	case pb.CheckState_CHECK_STATE_FINAL:
		return domain.CheckStateFinal
	default:
		return domain.CheckStateUnknown
	}
}
