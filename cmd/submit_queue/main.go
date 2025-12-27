// Package main implements a submit queue client using 8x8 matrix group testing.
// This demonstrates how to use the submit queue runners to process 64 CLs.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/endpoint"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage/sqlite"
	grpctransport "github.com/example/turboci-lite/internal/transport/grpc"
	"github.com/example/turboci-lite/pkg/ci"
	pb "github.com/example/turboci-lite/gen/turboci/v1"
)

var (
	orchestratorAddr = flag.String("orchestrator", "", "Connect to existing orchestrator (empty = start embedded)")
	grpcPort         = flag.Int("port", 50051, "gRPC port for embedded orchestrator")
	dbPath           = flag.String("db", "", "SQLite database path (empty = temp file)")
	numCLs           = flag.Int("cls", 64, "Number of CLs to process (must be 64)")
	verbose          = flag.Bool("verbose", false, "Verbose output")
)

func main() {
	flag.Parse()

	if *verbose {
		log.SetFlags(log.Ltime | log.Lmicroseconds | log.Lshortfile)
	} else {
		log.SetFlags(log.Ltime)
	}

	if *numCLs != 64 {
		log.Fatalf("Only 64 CLs supported for 8x8 matrix, got %d", *numCLs)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Received interrupt, shutting down...")
		cancel()
	}()

	client := &SubmitQueueClient{
		verbose: *verbose,
	}

	exitCode := client.Run(ctx, *orchestratorAddr, *grpcPort, *dbPath, *numCLs)
	os.Exit(exitCode)
}

// SubmitQueueClient manages the submit queue execution.
type SubmitQueueClient struct {
	orch    ci.Orchestrator
	verbose bool
}

func (c *SubmitQueueClient) Run(ctx context.Context, orchestratorAddr string, grpcPort int, dbPath string, numCLs int) int {
	// Set up orchestrator (embedded or remote)
	if orchestratorAddr != "" {
		log.Printf("Connecting to orchestrator at %s", orchestratorAddr)
		orch, err := ci.NewRemoteOrchestrator(orchestratorAddr)
		if err != nil {
			log.Fatalf("Failed to connect to orchestrator: %v", err)
			return 1
		}
		c.orch = orch
	} else {
		log.Printf("Starting embedded orchestrator on port %d", grpcPort)
		orch, cleanup, err := c.startEmbeddedOrchestrator(ctx, grpcPort, dbPath)
		if err != nil {
			log.Fatalf("Failed to start orchestrator: %v", err)
			return 1
		}
		defer cleanup()
		c.orch = orch
	}

	// Create and execute the submit queue workflow
	exec, err := c.createSubmitQueue(ctx, numCLs)
	if err != nil {
		log.Fatalf("Failed to create submit queue: %v", err)
		return 1
	}

	log.Printf("Submit queue started: work_plan_id=%s", exec.WorkPlanID())

	// Wait for completion
	if err := c.waitForCompletion(ctx, exec); err != nil {
		log.Printf("Submit queue failed: %v", err)
		return 1
	}

	log.Println("Submit queue completed successfully!")
	return 0
}

func (c *SubmitQueueClient) startEmbeddedOrchestrator(ctx context.Context, grpcPort int, dbPath string) (ci.Orchestrator, func(), error) {
	// Create storage
	if dbPath == "" {
		tmpFile, err := os.CreateTemp("", "submit-queue-*.db")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create temp db: %w", err)
		}
		dbPath = tmpFile.Name()
		tmpFile.Close()
	}

	store, err := sqlite.New(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create storage: %w", err)
	}

	if err := store.Migrate(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to migrate: %w", err)
	}

	// Create services
	orchestratorSvc := service.NewOrchestrator(store)
	runnerSvc := service.NewRunnerService(store)
	callbackSvc := service.NewCallbackService(store, orchestratorSvc)

	// Create dispatcher
	dispatcherCfg := service.DefaultDispatcherConfig()
	dispatcherCfg.PollInterval = 50 * time.Millisecond
	dispatcherCfg.CallbackAddress = fmt.Sprintf("localhost:%d", grpcPort)
	dispatcher := service.NewDispatcher(store, runnerSvc, orchestratorSvc, dispatcherCfg)

	// Wire orchestrator to dispatcher
	orchestratorSvc.SetDispatcher(dispatcher)

	// Create gRPC client factory for dispatcher
	dispatcher.SetClientFactory(func(addr string) (service.StageRunnerClient, error) {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, err
		}
		return &grpcRunnerClient{client: pb.NewStageRunnerClient(conn), conn: conn}, nil
	})

	// Start dispatcher
	dispatcher.Start()

	// Create endpoints
	endpoints := endpoint.MakeEndpoints(orchestratorSvc)

	// Create gRPC server
	server := grpctransport.NewServer(
		endpoints,
		grpctransport.WithRunnerService(runnerSvc),
		grpctransport.WithCallbackService(callbackSvc),
	)

	// Start server
	addr := fmt.Sprintf(":%d", grpcPort)
	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.Serve(addr); err != nil {
			serverErrCh <- err
		}
	}()

	// Wait for server to start
	select {
	case err := <-serverErrCh:
		return nil, nil, fmt.Errorf("gRPC server failed to start: %w", err)
	case <-time.After(200 * time.Millisecond):
		// Server started successfully
	}

	log.Printf("Embedded orchestrator listening on localhost:%d", grpcPort)

	// Create local orchestrator client
	orch := ci.NewLocalOrchestrator(orchestratorSvc)

	cleanup := func() {
		log.Println("Shutting down embedded orchestrator...")
		server.GracefulStop()
		store.Close()
	}

	return orch, cleanup, nil
}

func (c *SubmitQueueClient) createSubmitQueue(ctx context.Context, numCLs int) (*ci.WorkflowExecution, error) {
	// Generate CL IDs
	cls := make([]string, numCLs)
	for i := 0; i < numCLs; i++ {
		cls[i] = fmt.Sprintf("CL-%d", i+1)
	}

	log.Printf("Creating submit queue for %d CLs", len(cls))

	// Build workflow using DAG API
	dag := ci.NewDAG()

	// Create kickoff check and stage
	kickoffCheck := dag.Check("sq:kickoff").Kind("sq_kickoff")
	dag.Stage("stage:sq:kickoff").
		Runner("kickoff").
		Sync().
		Arg("cls", cls).
		Assigns(kickoffCheck)

	// Execute the DAG
	exec, err := dag.Execute(ctx, c.orch)
	if err != nil {
		return nil, fmt.Errorf("failed to execute workflow: %w", err)
	}

	return exec, nil
}

func (c *SubmitQueueClient) waitForCompletion(ctx context.Context, exec *ci.WorkflowExecution) error {
	log.Println("Waiting for submit queue completion...")

	workPlanID := exec.WorkPlanID()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	pollCount := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pollCount++

			// Check if batch-finished check exists and is complete
			check, err := exec.GetCheckResult(ctx, "sq:batch:finished")
			if err != nil {
				// Batch finished check might not exist yet if all rows passed
				// Try checking for decode finished instead
				decodeCheck, decodeErr := exec.GetCheckResult(ctx, "sq:decode:finished")
				if decodeErr != nil {
					if pollCount%5 == 0 {
						c.printProgress(ctx, exec, workPlanID)
					}
					continue
				}
				check = decodeCheck
			}

			if check.State != domain.CheckStateFinal {
				if pollCount%5 == 0 {
					c.printProgress(ctx, exec, workPlanID)
				}
				continue
			}

			// Final check completed - print results
			c.printFinalResults(ctx, exec, check)
			return nil
		}
	}
}

func (c *SubmitQueueClient) printProgress(ctx context.Context, exec *ci.WorkflowExecution, workPlanID string) {
	log.Printf("Checking progress for work_plan=%s", workPlanID)

	// Count completed row tests
	rowsPassed := 0
	rowsFailed := 0
	for i := 0; i < 8; i++ {
		checkID := fmt.Sprintf("sq:row:%d:finished", i)
		check, err := exec.GetCheckResult(ctx, checkID)
		if err == nil && check.State == domain.CheckStateFinal {
			if len(check.Results) > 0 && check.Results[0].Data != nil {
				if passed, ok := check.Results[0].Data["passed"].(bool); ok {
					if passed {
						rowsPassed++
					} else {
						rowsFailed++
					}
				}
			}
		}
	}

	// Count completed column tests
	colsCompleted := 0
	for i := 0; i < 8; i++ {
		checkID := fmt.Sprintf("sq:col:%d:finished", i)
		check, err := exec.GetCheckResult(ctx, checkID)
		if err == nil && check.State == domain.CheckStateFinal {
			colsCompleted++
		}
	}

	log.Printf("Progress: rows_passed=%d, rows_failed=%d, columns_tested=%d", rowsPassed, rowsFailed, colsCompleted)
}

func (c *SubmitQueueClient) printFinalResults(ctx context.Context, exec *ci.WorkflowExecution, finalCheck *domain.Check) {
	log.Println("\n" + strings.Repeat("=", 80))
	log.Println("SUBMIT QUEUE RESULTS")
	log.Println(strings.Repeat("=", 80))

	// Print row results
	log.Println("\nRow Test Results:")
	submittedCount := 0
	for i := 0; i < 8; i++ {
		checkID := fmt.Sprintf("sq:row:%d:finished", i)
		check, err := exec.GetCheckResult(ctx, checkID)
		if err == nil && len(check.Results) > 0 && check.Results[0].Data != nil {
			data := check.Results[0].Data
			passed := data["passed"].(bool)
			status := "PASSED ✓"
			if !passed {
				status = "FAILED ✗"
			} else {
				if cls, ok := data["submitted_cls"].([]interface{}); ok {
					submittedCount += len(cls)
				}
			}
			log.Printf("  Row %d: %s", i, status)
		}
	}

	// Check if columns were tested
	columnsTested := false
	for i := 0; i < 8; i++ {
		checkID := fmt.Sprintf("sq:col:%d:test", i)
		if _, err := exec.GetCheckResult(ctx, checkID); err == nil {
			columnsTested = true
			break
		}
	}

	if columnsTested {
		log.Println("\nColumn Test Results (for culprit finding):")
		for i := 0; i < 8; i++ {
			checkID := fmt.Sprintf("sq:col:%d:test", i)
			check, err := exec.GetCheckResult(ctx, checkID)
			if err == nil && len(check.Results) > 0 && check.Results[0].Data != nil {
				data := check.Results[0].Data
				passed := data["passed"].(bool)
				status := "PASSED ✓"
				if !passed {
					status = "FAILED ✗"
				}
				log.Printf("  Column %d: %s", i, status)
			}
		}

		// Print culprit CLs (intersection of failed rows and columns)
		log.Println("\nCulprit CLs (failed in both row and column tests):")
		failedRows := make(map[int]bool)
		failedCols := make(map[int]bool)

		for i := 0; i < 8; i++ {
			checkID := fmt.Sprintf("sq:row:%d:finished", i)
			check, err := exec.GetCheckResult(ctx, checkID)
			if err == nil && len(check.Results) > 0 && check.Results[0].Data != nil {
				if passed, ok := check.Results[0].Data["passed"].(bool); ok && !passed {
					failedRows[i] = true
				}
			}
		}

		for i := 0; i < 8; i++ {
			checkID := fmt.Sprintf("sq:col:%d:test", i)
			check, err := exec.GetCheckResult(ctx, checkID)
			if err == nil && len(check.Results) > 0 && check.Results[0].Data != nil {
				if passed, ok := check.Results[0].Data["passed"].(bool); ok && !passed {
					failedCols[i] = true
				}
			}
		}

		for row := range failedRows {
			for col := range failedCols {
				clIndex := row*8 + col
				log.Printf("  CL-%d (row=%d, col=%d)", clIndex+1, row, col)
			}
		}
	}

	log.Printf("\n")
	log.Printf("Total CLs submitted: %d/64", submittedCount)
	log.Println(strings.Repeat("=", 80))
}

// grpcRunnerClient implements service.StageRunnerClient by wrapping a gRPC client
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
		if s, err := structpb.NewStruct(req.Args); err == nil {
			pbReq.Args = s
		}
	}

	// Convert check options to assigned check IDs and options
	for _, opt := range req.CheckOptions {
		pbReq.AssignedCheckIds = append(pbReq.AssignedCheckIds, opt.CheckID)

		// Convert options to proto
		pbOpt := &pb.CheckOptions{
			CheckId: opt.CheckID,
			Kind:    opt.Kind,
		}
		if opt.Options != nil {
			if s, err := structpb.NewStruct(opt.Options); err == nil {
				pbOpt.Options = s
			}
		}
		pbReq.CheckOptions = append(pbReq.CheckOptions, pbOpt)
	}

	// Call gRPC
	pbResp, err := c.client.Run(ctx, pbReq)
	if err != nil {
		return nil, err
	}

	// Convert response back
	resp := &service.RunResponse{
		StageState: domain.StageState(pbResp.StageState),
	}

	if pbResp.Failure != nil {
		resp.Failure = &domain.Failure{Message: pbResp.Failure.Message}
	}

	for _, cu := range pbResp.CheckUpdates {
		update := service.CheckUpdate{
			CheckID:  cu.CheckId,
			State:    domain.CheckState(cu.State),
			Finalize: cu.Finalize,
		}
		if cu.ResultData != nil {
			update.Data = cu.ResultData.AsMap()
		}
		if cu.Failure != nil {
			update.Failure = &domain.Failure{Message: cu.Failure.Message}
		}
		resp.CheckUpdates = append(resp.CheckUpdates, &update)
	}

	return resp, nil
}

func (c *grpcRunnerClient) RunAsync(ctx context.Context, req *service.RunAsyncRequest) (*service.RunAsyncResponse, error) {
	// Convert service types to proto types
	pbReq := &pb.RunAsyncRequest{
		ExecutionId: req.ExecutionID,
		WorkPlanId:  req.WorkPlanID,
		StageId:     req.StageID,
		AttemptIdx:  int32(req.AttemptIdx),
	}

	// Convert stage args
	if req.Args != nil {
		if s, err := structpb.NewStruct(req.Args); err == nil {
			pbReq.Args = s
		}
	}

	// Convert check options to assigned check IDs
	for _, opt := range req.CheckOptions {
		pbReq.AssignedCheckIds = append(pbReq.AssignedCheckIds, opt.CheckID)
	}

	// Call gRPC
	pbResp, err := c.client.RunAsync(ctx, pbReq)
	if err != nil {
		return nil, err
	}

	return &service.RunAsyncResponse{
		Accepted: pbResp.Accepted,
	}, nil
}

func (c *grpcRunnerClient) Ping(ctx context.Context, req *service.PingRequest) (*service.PingResponse, error) {
	pbResp, err := c.client.Ping(ctx, &pb.PingRequest{})
	if err != nil {
		return nil, err
	}

	return &service.PingResponse{
		Healthy: len(pbResp.RunnerType) > 0, // Consider healthy if it has a runner type
	}, nil
}

func (c *grpcRunnerClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
