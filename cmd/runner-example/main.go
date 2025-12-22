package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
)

var (
	runnerID          = flag.String("runner-id", "example-runner-1", "Runner ID")
	runnerType        = flag.String("runner-type", "example", "Runner type")
	listenAddr        = flag.String("listen", ":50052", "Address to listen on")
	orchestratorAddr  = flag.String("orchestrator", "localhost:50051", "Orchestrator address")
	maxConcurrent     = flag.Int("max-concurrent", 5, "Maximum concurrent executions")
	heartbeatInterval = flag.Duration("heartbeat", 60*time.Second, "Heartbeat interval")
)

func main() {
	flag.Parse()

	runner := NewExampleRunner(*runnerID, *runnerType, *listenAddr, *orchestratorAddr, *maxConcurrent)

	// Start gRPC server
	go func() {
		if err := runner.StartServer(); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait a moment for server to start
	time.Sleep(100 * time.Millisecond)

	// Register with orchestrator
	ctx := context.Background()
	if err := runner.Register(ctx); err != nil {
		log.Fatalf("Failed to register: %v", err)
	}

	// Start heartbeat loop
	go runner.HeartbeatLoop(ctx, *heartbeatInterval)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	runner.Shutdown(ctx)
}

// ExampleRunner is an example implementation of the StageRunner service.
type ExampleRunner struct {
	pb.UnimplementedStageRunnerServer

	runnerID         string
	runnerType       string
	listenAddr       string
	orchestratorAddr string
	maxConcurrent    int

	registrationID string
	grpcServer     *grpc.Server
	orchestrator   pb.TurboCIOrchestratorClient
	conn           *grpc.ClientConn

	mu          sync.Mutex
	currentLoad int
}

// NewExampleRunner creates a new example runner.
func NewExampleRunner(runnerID, runnerType, listenAddr, orchestratorAddr string, maxConcurrent int) *ExampleRunner {
	return &ExampleRunner{
		runnerID:         runnerID,
		runnerType:       runnerType,
		listenAddr:       listenAddr,
		orchestratorAddr: orchestratorAddr,
		maxConcurrent:    maxConcurrent,
	}
}

// StartServer starts the gRPC server for receiving execution requests.
func (r *ExampleRunner) StartServer() error {
	lis, err := net.Listen("tcp", r.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	r.grpcServer = grpc.NewServer()
	pb.RegisterStageRunnerServer(r.grpcServer, r)
	reflection.Register(r.grpcServer)

	log.Printf("Stage runner listening on %s", r.listenAddr)
	return r.grpcServer.Serve(lis)
}

// Register registers this runner with the orchestrator.
func (r *ExampleRunner) Register(ctx context.Context) error {
	// Connect to orchestrator
	conn, err := grpc.NewClient(r.orchestratorAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to orchestrator: %w", err)
	}
	r.conn = conn
	r.orchestrator = pb.NewTurboCIOrchestratorClient(conn)

	// Register
	resp, err := r.orchestrator.RegisterStageRunner(ctx, &pb.RegisterStageRunnerRequest{
		RunnerId:   r.runnerID,
		RunnerType: r.runnerType,
		Address:    r.listenAddr,
		SupportedModes: []pb.ExecutionMode{
			pb.ExecutionMode_EXECUTION_MODE_SYNC,
			pb.ExecutionMode_EXECUTION_MODE_ASYNC,
		},
		MaxConcurrent: int32(r.maxConcurrent),
		TtlSeconds:    120, // 2 minutes
		Metadata: map[string]string{
			"version": "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to register: %w", err)
	}

	r.registrationID = resp.RegistrationId
	log.Printf("Registered with orchestrator: registration_id=%s, expires_at=%v",
		r.registrationID, resp.ExpiresAt.AsTime())

	return nil
}

// HeartbeatLoop sends periodic heartbeats to the orchestrator.
func (r *ExampleRunner) HeartbeatLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.registrationID == "" {
				continue
			}

			// Re-register to extend TTL
			resp, err := r.orchestrator.RegisterStageRunner(ctx, &pb.RegisterStageRunnerRequest{
				RunnerId:   r.runnerID,
				RunnerType: r.runnerType,
				Address:    r.listenAddr,
				SupportedModes: []pb.ExecutionMode{
					pb.ExecutionMode_EXECUTION_MODE_SYNC,
					pb.ExecutionMode_EXECUTION_MODE_ASYNC,
				},
				MaxConcurrent: int32(r.maxConcurrent),
				TtlSeconds:    120,
			})
			if err != nil {
				log.Printf("Heartbeat failed: %v", err)
				continue
			}
			r.registrationID = resp.RegistrationId
			log.Printf("Heartbeat sent, expires_at=%v", resp.ExpiresAt.AsTime())
		}
	}
}

// Shutdown gracefully shuts down the runner.
func (r *ExampleRunner) Shutdown(ctx context.Context) {
	// Unregister from orchestrator
	if r.registrationID != "" && r.orchestrator != nil {
		_, err := r.orchestrator.UnregisterStageRunner(ctx, &pb.UnregisterStageRunnerRequest{
			RegistrationId: r.registrationID,
		})
		if err != nil {
			log.Printf("Failed to unregister: %v", err)
		} else {
			log.Println("Unregistered from orchestrator")
		}
	}

	// Close orchestrator connection
	if r.conn != nil {
		r.conn.Close()
	}

	// Stop gRPC server
	if r.grpcServer != nil {
		r.grpcServer.GracefulStop()
	}
}

// Run handles synchronous stage execution.
func (r *ExampleRunner) Run(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	log.Printf("Run: execution_id=%s, work_plan_id=%s, stage_id=%s",
		req.ExecutionId, req.WorkPlanId, req.StageId)

	r.mu.Lock()
	r.currentLoad++
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.currentLoad--
		r.mu.Unlock()
	}()

	// Simulate work
	checkUpdates := r.processChecks(req.CheckOptions)

	// Add a small delay to simulate processing
	time.Sleep(100 * time.Millisecond)

	log.Printf("Run complete: execution_id=%s, updates=%d", req.ExecutionId, len(checkUpdates))

	return &pb.RunResponse{
		StageState:   pb.StageState_STAGE_STATE_FINAL,
		CheckUpdates: checkUpdates,
	}, nil
}

// RunAsync handles asynchronous stage execution.
func (r *ExampleRunner) RunAsync(ctx context.Context, req *pb.RunAsyncRequest) (*pb.RunAsyncResponse, error) {
	log.Printf("RunAsync: execution_id=%s, work_plan_id=%s, stage_id=%s",
		req.ExecutionId, req.WorkPlanId, req.StageId)

	r.mu.Lock()
	if r.currentLoad >= r.maxConcurrent {
		r.mu.Unlock()
		return &pb.RunAsyncResponse{Accepted: false}, nil
	}
	r.currentLoad++
	r.mu.Unlock()

	// Start async work in a goroutine
	go r.runAsyncWork(context.Background(), req)

	return &pb.RunAsyncResponse{Accepted: true}, nil
}

// runAsyncWork performs the async work and calls back to the orchestrator.
func (r *ExampleRunner) runAsyncWork(ctx context.Context, req *pb.RunAsyncRequest) {
	defer func() {
		r.mu.Lock()
		r.currentLoad--
		r.mu.Unlock()
	}()

	// Send progress updates
	for i := 0; i <= 100; i += 25 {
		_, err := r.orchestrator.UpdateStageExecution(ctx, &pb.UpdateStageExecutionRequest{
			ExecutionId:     req.ExecutionId,
			ProgressPercent: int32(i),
			Message:         fmt.Sprintf("Processing... %d%%", i),
		})
		if err != nil {
			log.Printf("Failed to send progress update: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Process checks
	checkUpdates := r.processChecks(req.CheckOptions)

	// Send completion callback
	_, err := r.orchestrator.CompleteStageExecution(ctx, &pb.CompleteStageExecutionRequest{
		ExecutionId:  req.ExecutionId,
		StageState:   pb.StageState_STAGE_STATE_FINAL,
		CheckUpdates: checkUpdates,
	})
	if err != nil {
		log.Printf("Failed to send completion callback: %v", err)
	} else {
		log.Printf("RunAsync complete: execution_id=%s", req.ExecutionId)
	}
}

// processChecks simulates processing checks and generating updates.
func (r *ExampleRunner) processChecks(checkOptions []*pb.CheckOptions) []*pb.CheckUpdate {
	var updates []*pb.CheckUpdate

	for _, opt := range checkOptions {
		log.Printf("Processing check: id=%s, kind=%s", opt.CheckId, opt.Kind)

		// Create a result for each check
		resultData, _ := structpb.NewStruct(map[string]any{
			"processed_at": time.Now().UTC().Format(time.RFC3339),
			"runner_id":    r.runnerID,
			"runner_type":  r.runnerType,
			"status":       "success",
		})

		updates = append(updates, &pb.CheckUpdate{
			CheckId:    opt.CheckId,
			State:      pb.CheckState_CHECK_STATE_FINAL,
			ResultData: resultData,
			Finalize:   true,
		})
	}

	return updates
}

// Ping checks if the runner is healthy.
func (r *ExampleRunner) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingResponse, error) {
	r.mu.Lock()
	availableCapacity := int32(r.maxConcurrent - r.currentLoad)
	r.mu.Unlock()

	return &pb.PingResponse{
		RunnerType:        r.runnerType,
		AvailableCapacity: availableCapacity,
		SupportedModes: []pb.ExecutionMode{
			pb.ExecutionMode_EXECUTION_MODE_SYNC,
			pb.ExecutionMode_EXECUTION_MODE_ASYNC,
		},
	}, nil
}
