// Package common provides shared utilities for CI runners.
package common

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
)

// RunHandler is a function that handles Run requests.
type RunHandler func(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error)

// BaseRunner provides common runner functionality.
type BaseRunner struct {
	pb.UnimplementedStageRunnerServer

	RunnerID         string
	RunnerType       string
	ListenAddr       string
	OrchestratorAddr string
	MaxConcurrent    int
	SupportedModes   []pb.ExecutionMode

	RegistrationID string
	GRPCServer     *grpc.Server
	Orchestrator   pb.TurboCIOrchestratorClient
	Conn           *grpc.ClientConn

	mu          sync.Mutex
	currentLoad int

	// RunHandler is the custom handler for Run requests
	runHandler RunHandler
}

// NewBaseRunner creates a new base runner.
func NewBaseRunner(runnerID, runnerType, listenAddr, orchestratorAddr string, maxConcurrent int) *BaseRunner {
	return &BaseRunner{
		RunnerID:         runnerID,
		RunnerType:       runnerType,
		ListenAddr:       listenAddr,
		OrchestratorAddr: orchestratorAddr,
		MaxConcurrent:    maxConcurrent,
		SupportedModes: []pb.ExecutionMode{
			pb.ExecutionMode_EXECUTION_MODE_SYNC,
		},
	}
}

// SetRunHandler sets the custom run handler.
func (r *BaseRunner) SetRunHandler(handler RunHandler) {
	r.runHandler = handler
}

// StartServer starts the gRPC server for receiving execution requests.
func (r *BaseRunner) StartServer() error {
	lis, err := net.Listen("tcp", r.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	r.GRPCServer = grpc.NewServer()
	pb.RegisterStageRunnerServer(r.GRPCServer, r)
	reflection.Register(r.GRPCServer)

	log.Printf("[%s] Stage runner listening on %s", r.RunnerType, r.ListenAddr)
	return r.GRPCServer.Serve(lis)
}

// Register registers this runner with the orchestrator.
func (r *BaseRunner) Register(ctx context.Context) error {
	// Connect to orchestrator
	conn, err := grpc.NewClient(r.OrchestratorAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to orchestrator: %w", err)
	}
	r.Conn = conn
	r.Orchestrator = pb.NewTurboCIOrchestratorClient(conn)

	// Register
	resp, err := r.Orchestrator.RegisterStageRunner(ctx, &pb.RegisterStageRunnerRequest{
		RunnerId:       r.RunnerID,
		RunnerType:     r.RunnerType,
		Address:        r.ListenAddr,
		SupportedModes: r.SupportedModes,
		MaxConcurrent:  int32(r.MaxConcurrent),
		TtlSeconds:     120,
		Metadata: map[string]string{
			"version": "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to register: %w", err)
	}

	r.RegistrationID = resp.RegistrationId
	log.Printf("[%s] Registered with orchestrator: registration_id=%s",
		r.RunnerType, r.RegistrationID)

	return nil
}

// HeartbeatLoop sends periodic heartbeats to the orchestrator.
func (r *BaseRunner) HeartbeatLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.RegistrationID == "" {
				continue
			}

			resp, err := r.Orchestrator.RegisterStageRunner(ctx, &pb.RegisterStageRunnerRequest{
				RunnerId:       r.RunnerID,
				RunnerType:     r.RunnerType,
				Address:        r.ListenAddr,
				SupportedModes: r.SupportedModes,
				MaxConcurrent:  int32(r.MaxConcurrent),
				TtlSeconds:     120,
			})
			if err != nil {
				log.Printf("[%s] Heartbeat failed: %v", r.RunnerType, err)
				continue
			}
			r.RegistrationID = resp.RegistrationId
		}
	}
}

// Shutdown gracefully shuts down the runner.
func (r *BaseRunner) Shutdown(ctx context.Context) {
	if r.RegistrationID != "" && r.Orchestrator != nil {
		_, err := r.Orchestrator.UnregisterStageRunner(ctx, &pb.UnregisterStageRunnerRequest{
			RegistrationId: r.RegistrationID,
		})
		if err != nil {
			log.Printf("[%s] Failed to unregister: %v", r.RunnerType, err)
		} else {
			log.Printf("[%s] Unregistered from orchestrator", r.RunnerType)
		}
	}

	if r.Conn != nil {
		r.Conn.Close()
	}

	if r.GRPCServer != nil {
		r.GRPCServer.GracefulStop()
	}
}

// Run handles synchronous stage execution.
func (r *BaseRunner) Run(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	log.Printf("[%s] Run: execution_id=%s, stage_id=%s",
		r.RunnerType, req.ExecutionId, req.StageId)

	r.mu.Lock()
	r.currentLoad++
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.currentLoad--
		r.mu.Unlock()
	}()

	if r.runHandler != nil {
		resp, err := r.runHandler(ctx, req)
		log.Printf("[%s] Run complete: execution_id=%s, err=%v", r.RunnerType, req.ExecutionId, err)
		return resp, err
	}

	return &pb.RunResponse{
		StageState: pb.StageState_STAGE_STATE_FINAL,
	}, nil
}

// Ping checks if the runner is healthy.
func (r *BaseRunner) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingResponse, error) {
	r.mu.Lock()
	availableCapacity := int32(r.MaxConcurrent - r.currentLoad)
	r.mu.Unlock()

	return &pb.PingResponse{
		RunnerType:        r.RunnerType,
		AvailableCapacity: availableCapacity,
		SupportedModes:    r.SupportedModes,
	}, nil
}

// RunAsync is not supported by default - override if needed.
func (r *BaseRunner) RunAsync(ctx context.Context, req *pb.RunAsyncRequest) (*pb.RunAsyncResponse, error) {
	return &pb.RunAsyncResponse{Accepted: false}, nil
}
