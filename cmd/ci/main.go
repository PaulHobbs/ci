// Package main implements the CI controller for turboci-lite.
// This controller orchestrates the entire CI pipeline using turboci-lite itself.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/endpoint"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage/sqlite"
	grpctransport "github.com/example/turboci-lite/internal/transport/grpc"
)

var (
	// Connection options
	orchestratorAddr = flag.String("orchestrator", "", "Connect to existing orchestrator (empty = start embedded)")
	grpcPort         = flag.Int("port", 50051, "gRPC port for embedded orchestrator")

	// Git refs for change detection
	baseRef = flag.String("base", "origin/main", "Base ref for change detection")
	headRef = flag.String("head", "HEAD", "Head ref for change detection")

	// Paths
	repoRoot  = flag.String("repo", ".", "Repository root directory")
	outputDir = flag.String("output", "ci-results", "Output directory for results")
	dbPath    = flag.String("db", "", "SQLite database path (empty = temp file)")

	// Options
	verbose = flag.Bool("verbose", false, "Verbose output")
	timeout = flag.Duration("timeout", 30*time.Minute, "Overall timeout for CI run")
)

func main() {
	flag.Parse()

	// Set up logging
	if *verbose {
		log.SetFlags(log.Ltime | log.Lmicroseconds | log.Lshortfile)
	} else {
		log.SetFlags(log.Ltime)
	}

	// Resolve repo root to absolute path
	absRepoRoot, err := filepath.Abs(*repoRoot)
	if err != nil {
		log.Fatalf("Failed to resolve repo root: %v", err)
	}

	// Create CI controller
	ctrl := &CIController{
		repoRoot:  absRepoRoot,
		outputDir: *outputDir,
		baseRef:   *baseRef,
		headRef:   *headRef,
		verbose:   *verbose,
	}

	// Set up context with timeout and cancellation
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Received interrupt, shutting down...")
		cancel()
	}()

	// Run CI
	exitCode := ctrl.Run(ctx, *orchestratorAddr, *grpcPort, *dbPath)
	os.Exit(exitCode)
}

// CIController manages the CI pipeline execution.
type CIController struct {
	repoRoot  string
	outputDir string
	baseRef   string
	headRef   string
	verbose   bool

	orchestrator pb.TurboCIOrchestratorClient
	conn         *grpc.ClientConn
	server       *grpc.Server
	processes    []*exec.Cmd
}

// Run executes the CI pipeline and returns an exit code.
func (c *CIController) Run(ctx context.Context, orchestratorAddr string, grpcPort int, dbPath string) int {
	var err error

	// Start or connect to orchestrator
	if orchestratorAddr != "" {
		log.Printf("Connecting to existing orchestrator at %s", orchestratorAddr)
		err = c.connectToOrchestrator(ctx, orchestratorAddr)
	} else {
		log.Printf("Starting embedded orchestrator on port %d", grpcPort)
		err = c.startEmbeddedOrchestrator(ctx, grpcPort, dbPath)
	}
	if err != nil {
		log.Printf("Failed to set up orchestrator: %v", err)
		return 1
	}
	defer c.cleanup()

	// Start runner processes
	if err := c.startRunners(ctx, orchestratorAddr, grpcPort); err != nil {
		log.Printf("Failed to start runners: %v", err)
		return 1
	}

	// Wait for runners to register
	log.Println("Waiting for runners to register...")
	time.Sleep(500 * time.Millisecond)

	// Create work plan and initial stages
	workPlanID, err := c.createWorkPlan(ctx)
	if err != nil {
		log.Printf("Failed to create work plan: %v", err)
		return 1
	}
	log.Printf("Created work plan: %s", workPlanID)

	// Wait for completion
	status, err := c.waitForCompletion(ctx, workPlanID)
	if err != nil {
		log.Printf("CI execution failed: %v", err)
		return 1
	}

	// Return exit code based on status
	switch status {
	case "passed":
		log.Println("CI PASSED")
		return 0
	case "failed":
		log.Println("CI FAILED")
		return 1
	default:
		log.Printf("CI completed with status: %s", status)
		return 0
	}
}

func (c *CIController) connectToOrchestrator(ctx context.Context, addr string) error {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	c.conn = conn
	c.orchestrator = pb.NewTurboCIOrchestratorClient(conn)
	return nil
}

func (c *CIController) startEmbeddedOrchestrator(ctx context.Context, port int, dbPath string) error {
	// Create database
	if dbPath == "" {
		tmpFile, err := os.CreateTemp("", "turboci-*.db")
		if err != nil {
			return fmt.Errorf("failed to create temp db: %w", err)
		}
		dbPath = tmpFile.Name()
		tmpFile.Close()
	}

	store, err := sqlite.New(dbPath)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	if err := store.Migrate(ctx); err != nil {
		return fmt.Errorf("failed to migrate: %w", err)
	}

	// Create services (matching cmd/server/main.go pattern)
	orchestratorSvc := service.NewOrchestrator(store)
	runnerSvc := service.NewRunnerService(store)
	callbackSvc := service.NewCallbackService(store, orchestratorSvc)

	// Create dispatcher with faster polling for CI
	dispatcherCfg := service.DefaultDispatcherConfig()
	dispatcherCfg.PollInterval = 50 * time.Millisecond
	dispatcherCfg.CallbackAddress = fmt.Sprintf("localhost:%d", port)
	dispatcher := service.NewDispatcher(store, runnerSvc, orchestratorSvc, dispatcherCfg)

	// Create gRPC client factory for dispatcher
	dispatcher.SetClientFactory(func(addr string) (service.StageRunnerClient, error) {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, err
		}
		return &grpcRunnerClient{client: pb.NewStageRunnerClient(conn), conn: conn}, nil
	})

	// Start dispatcher (fire and forget - uses internal goroutines)
	dispatcher.Start()

	// Create endpoints
	endpoints := endpoint.MakeEndpoints(orchestratorSvc)

	// Create gRPC server with services
	server := grpctransport.NewServer(
		endpoints,
		grpctransport.WithRunnerService(runnerSvc),
		grpctransport.WithCallbackService(callbackSvc),
	)

	// Start server in background with proper error handling
	addr := fmt.Sprintf(":%d", port)
	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.Serve(addr); err != nil {
			serverErrCh <- err
		}
	}()

	// Wait for server to start (or fail)
	select {
	case err := <-serverErrCh:
		return fmt.Errorf("gRPC server failed to start: %w", err)
	case <-time.After(200 * time.Millisecond):
		// Server started successfully (no immediate error)
	}

	// Connect to our own server
	return c.connectToOrchestrator(ctx, fmt.Sprintf("localhost:%d", port))
}

func (c *CIController) startRunners(ctx context.Context, orchestratorAddr string, grpcPort int) error {
	if orchestratorAddr == "" {
		orchestratorAddr = fmt.Sprintf("localhost:%d", grpcPort)
	}

	// Runner configurations: type, port, extra args
	runners := []struct {
		name      string
		binary    string
		port      int
		extraArgs []string
	}{
		{"trigger", "trigger", 50061, []string{"-repo-root", c.repoRoot}},
		{"materialize", "materialize", 50062, nil},
		{"builder", "builder", 50063, []string{"-repo-root", c.repoRoot}},
		{"tester", "tester", 50064, []string{"-repo-root", c.repoRoot}},
		{"conditional", "conditional", 50065, []string{"-repo-root", c.repoRoot}},
		{"collector", "collector", 50066, []string{"-output-dir", c.outputDir}},
	}

	// Find runner binaries
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	binDir := filepath.Dir(execPath)

	for _, r := range runners {
		runnerPath := filepath.Join(binDir, r.binary+"-runner")
		// Also check in cmd/ci/runners/<name>
		if _, err := os.Stat(runnerPath); os.IsNotExist(err) {
			// Try building inline
			log.Printf("Runner binary %s not found, using go run", r.binary)
			runnerPath = filepath.Join(c.repoRoot, "cmd", "ci", "runners", r.binary, "main.go")
		}

		args := []string{
			"-runner-id", fmt.Sprintf("%s-runner-1", r.name),
			"-listen", fmt.Sprintf(":%d", r.port),
			"-orchestrator", orchestratorAddr,
		}
		args = append(args, r.extraArgs...)

		var cmd *exec.Cmd
		if filepath.Ext(runnerPath) == ".go" {
			// Use go run
			goArgs := append([]string{"run", runnerPath}, args...)
			cmd = exec.CommandContext(ctx, "go", goArgs...)
		} else {
			cmd = exec.CommandContext(ctx, runnerPath, args...)
		}

		cmd.Dir = c.repoRoot
		if c.verbose {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start %s runner: %w", r.name, err)
		}

		c.processes = append(c.processes, cmd)
		log.Printf("Started %s runner (pid=%d, port=%d)", r.name, cmd.Process.Pid, r.port)
	}

	return nil
}

func (c *CIController) createWorkPlan(ctx context.Context) (string, error) {
	// Create work plan
	wpResp, err := c.orchestrator.CreateWorkPlan(ctx, &pb.CreateWorkPlanRequest{
		Metadata: map[string]string{
			"type":     "ci",
			"base_ref": c.baseRef,
			"head_ref": c.headRef,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create work plan: %w", err)
	}

	workPlanID := wpResp.WorkPlan.Id

	// Create trigger check and stage
	triggerArgs, _ := structpb.NewStruct(map[string]any{
		"base_ref": c.baseRef,
		"head_ref": c.headRef,
	})

	_, err = c.orchestrator.WriteNodes(ctx, &pb.WriteNodesRequest{
		WorkPlanId: workPlanID,
		Checks: []*pb.CheckWrite{
			{
				Id:    "trigger:detect_changes",
				State: pb.CheckState_CHECK_STATE_PLANNING,
				Kind:  "trigger",
			},
			{
				Id:    "materialize:create_stages",
				State: pb.CheckState_CHECK_STATE_PLANNING,
				Kind:  "materialize",
				Dependencies: &pb.DependencyGroup{
					Predicate: pb.PredicateType_PREDICATE_TYPE_AND,
					Dependencies: []*pb.Dependency{{
						TargetType: pb.NodeType_NODE_TYPE_CHECK,
						TargetId:   "trigger:detect_changes",
					}},
				},
			},
		},
		Stages: []*pb.StageWrite{
			{
				Id:            "stage:trigger",
				State:         pb.StageState_STAGE_STATE_PLANNED,
				RunnerType:    "trigger_builds",
				ExecutionMode: pb.ExecutionMode_EXECUTION_MODE_SYNC,
				Args:          triggerArgs,
				Assignments: []*pb.Assignment{{
					TargetCheckId: "trigger:detect_changes",
					GoalState:     pb.CheckState_CHECK_STATE_FINAL,
				}},
			},
			{
				Id:            "stage:materialize",
				State:         pb.StageState_STAGE_STATE_PLANNED,
				RunnerType:    "materialize",
				ExecutionMode: pb.ExecutionMode_EXECUTION_MODE_SYNC,
				Assignments: []*pb.Assignment{{
					TargetCheckId: "materialize:create_stages",
					GoalState:     pb.CheckState_CHECK_STATE_FINAL,
				}},
				Dependencies: &pb.DependencyGroup{
					Predicate: pb.PredicateType_PREDICATE_TYPE_AND,
					Dependencies: []*pb.Dependency{{
						TargetType: pb.NodeType_NODE_TYPE_STAGE,
						TargetId:   "stage:trigger",
					}},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create initial stages: %w", err)
	}

	return workPlanID, nil
}

func (c *CIController) waitForCompletion(ctx context.Context, workPlanID string) (string, error) {
	log.Println("Waiting for CI completion...")

	ticker := time.NewTicker(2500 * time.Millisecond)
	defer ticker.Stop()

	pollCount := 0
	lastDiagnosticAt := 0

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			pollCount++

			// Query the collector check for final status
			resp, err := c.orchestrator.QueryNodes(ctx, &pb.QueryNodesRequest{
				WorkPlanId:    workPlanID,
				CheckIds:      []string{"collector:results"},
				IncludeChecks: true,
			})
			if err != nil {
				// Check might not exist yet
				continue
			}

			if len(resp.Checks) == 0 {
				continue
			}

			check := resp.Checks[0]
			if check.State != pb.CheckState_CHECK_STATE_FINAL {
				// Print diagnostic every 5 seconds (2 polls at 2500ms)
				if pollCount-lastDiagnosticAt >= 2 {
					lastDiagnosticAt = pollCount
					c.printDiagnostics(ctx, workPlanID)
				}
				continue
			}

			// Check completed - extract status from results
			status := "passed"
			if len(check.Results) > 0 {
				lastResult := check.Results[len(check.Results)-1]
				if lastResult.Data != nil {
					if v := lastResult.Data.Fields["status"]; v != nil {
						status = v.GetStringValue()
					}
				}
				if lastResult.Failure != nil {
					status = "failed"
				}
			}

			return status, nil
		}
	}
}

func (c *CIController) printDiagnostics(ctx context.Context, workPlanID string) {
	// Query all stages
	stageResp, err := c.orchestrator.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    workPlanID,
		IncludeStages: true,
	})
	if err != nil {
		log.Printf("[diag] Failed to query stages: %v", err)
		return
	}

	// Count stages by state
	stageStates := make(map[string]int)
	var nonFinalStages []string
	for _, stage := range stageResp.Stages {
		stateName := stage.State.String()
		stageStates[stateName]++
		if stage.State != pb.StageState_STAGE_STATE_FINAL {
			nonFinalStages = append(nonFinalStages, stage.Id)
		}
	}

	log.Printf("[diag] Stages: total=%d states=%v", len(stageResp.Stages), stageStates)
	if len(nonFinalStages) > 0 && len(nonFinalStages) <= 10 {
		log.Printf("[diag] Non-final stages: %v", nonFinalStages)
	} else if len(nonFinalStages) > 10 {
		log.Printf("[diag] Non-final stages: %d remaining (first 10: %v)", len(nonFinalStages), nonFinalStages[:10])
	}

	// Query all checks
	checkResp, err := c.orchestrator.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    workPlanID,
		IncludeChecks: true,
	})
	if err != nil {
		log.Printf("[diag] Failed to query checks: %v", err)
		return
	}

	// Count checks by state
	checkStates := make(map[string]int)
	var nonFinalChecks []string
	for _, check := range checkResp.Checks {
		stateName := check.State.String()
		checkStates[stateName]++
		if check.State != pb.CheckState_CHECK_STATE_FINAL {
			nonFinalChecks = append(nonFinalChecks, fmt.Sprintf("\n\n  id: %s\n  kind: %s\n options: %v", check.Id, check.Id, check.Options))
		}
	}

	log.Printf("[diag] Checks: total=%d states=%v", len(checkResp.Checks), checkStates)
	if len(nonFinalChecks) > 0 && len(nonFinalChecks) <= 10 {
		log.Printf("[diag] Non-final checks: %v", nonFinalChecks)
	} else if len(nonFinalChecks) > 10 {
		log.Printf("[diag] Non-final checks: %d remaining (first 5: %v)", len(nonFinalChecks), nonFinalChecks[:10])
	}
}

func (c *CIController) cleanup() {
	// Stop runner processes
	for _, cmd := range c.processes {
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
			// Give it a moment to exit gracefully
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				cmd.Process.Kill()
			}
		}
	}

	// Close connection
	if c.conn != nil {
		c.conn.Close()
	}

	// Stop server
	if c.server != nil {
		c.server.GracefulStop()
	}
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

	// Convert stage args
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
		pbReq.AssignedCheckIds = append(pbReq.AssignedCheckIds, opt.CheckID)
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

	return &service.PingResponse{
		Healthy: pbResp.AvailableCapacity > 0,
	}, nil
}

func (c *grpcRunnerClient) Close() error {
	return c.conn.Close()
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
		return domain.StageStatePlanned
	}
}

func convertCheckState(s pb.CheckState) domain.CheckState {
	switch s {
	case pb.CheckState_CHECK_STATE_PLANNING:
		return domain.CheckStatePlanning
	case pb.CheckState_CHECK_STATE_PLANNED:
		return domain.CheckStatePlanned
	case pb.CheckState_CHECK_STATE_WAITING:
		return domain.CheckStateWaiting
	case pb.CheckState_CHECK_STATE_FINAL:
		return domain.CheckStateFinal
	default:
		return domain.CheckStatePlanning
	}
}
