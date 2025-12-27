package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/example/turboci-lite/cmd/ci/runners/common"
	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/endpoint"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage/sqlite"
	grpctransport "github.com/example/turboci-lite/internal/transport/grpc"
	"github.com/example/turboci-lite/pkg/ci"
)

// TestSubmitQueueIntegration tests the full submit queue workflow
func TestSubmitQueueIntegration(t *testing.T) {
	t.Skip("Integration test requires dedicated test infrastructure with dynamic ports")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start embedded orchestrator
	orchestrator, cleanup := setupEmbeddedOrchestrator(t, ctx)
	defer cleanup()

	// Start all required runners
	runners := startAllRunners(t, ctx, "localhost:50051")
	defer stopAllRunners(runners)

	// Wait for runners to register
	time.Sleep(500 * time.Millisecond)

	// Generate 64 CLs
	cls := make([]string, 64)
	for i := 0; i < 64; i++ {
		cls[i] = fmt.Sprintf("CL-%d", i+1)
	}

	// Create submit queue workflow using DAG API
	dag := ci.NewDAG()
	kickoffCheck := dag.Check("sq:kickoff").Kind("sq_kickoff")
	dag.Stage("stage:sq:kickoff").
		Runner("kickoff").
		Sync().
		Arg("cls", cls).
		Assigns(kickoffCheck)

	// Execute the DAG
	exec, err := dag.Execute(ctx, orchestrator)
	if err != nil {
		t.Fatalf("Failed to execute DAG: %v", err)
	}

	t.Logf("Submit queue started: work_plan_id=%s", exec.WorkPlanID())

	// Wait for completion with timeout
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	maxPolls := 30
	pollCount := 0

	for {
		select {
		case <-ctx.Done():
			t.Fatal("Test timed out")
		case <-ticker.C:
			pollCount++
			if pollCount > maxPolls {
				t.Fatal("Test exceeded maximum polling attempts")
			}

			// Check if batch-finished check exists and is complete
			check, err := exec.GetCheckResult(ctx, "sq:batch:finished")
			if err != nil {
				// Not ready yet
				continue
			}

			if check.State != domain.CheckStateFinal {
				t.Logf("Poll %d: batch-finished not final yet (state=%v)", pollCount, check.State)
				continue
			}

			// Batch finished - verify results
			t.Log("Batch finished check completed")
			verifyResults(t, ctx, exec)
			return
		}
	}
}

// TestKickoffRunner tests the kickoff runner in isolation
func TestKickoffRunner(t *testing.T) {
	ctx := context.Background()
	runner := NewKickoffRunner()

	// Mock orchestrator client
	orch, wpID, cleanup := setupTestOrchestrator(t, ctx)
	defer cleanup()

	runner.orch = orch

	// Create CLs
	cls := make([]interface{}, 64)
	for i := 0; i < 64; i++ {
		cls[i] = fmt.Sprintf("CL-%d", i+1)
	}

	req := &pb.RunRequest{
		WorkPlanId:       wpID,
		StageId:          "stage:sq:kickoff",
		AssignedCheckIds: []string{"sq:kickoff"},
		Args:             mustMakeStruct(t, map[string]interface{}{"cls": cls}),
	}

	resp, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	if resp.StageState != pb.StageState_STAGE_STATE_FINAL {
		t.Errorf("Expected FINAL state, got %v", resp.StageState)
	}

	if len(resp.CheckUpdates) == 0 {
		t.Error("Expected check updates")
	}
}

// TestConfigRunner tests the config runner
func TestConfigRunner(t *testing.T) {
	ctx := context.Background()
	runner := NewConfigRunner()

	orch, wpID, cleanup := setupTestOrchestrator(t, ctx)
	defer cleanup()

	runner.orch = orch

	// Create kickoff stage with CLs first
	cls := make([]interface{}, 64)
	for i := 0; i < 64; i++ {
		cls[i] = fmt.Sprintf("CL-%d", i+1)
	}

	_, err := orch.WriteNodes(ctx, &pb.WriteNodesRequest{
		WorkPlanId: wpID,
		Stages: []*pb.StageWrite{
			{
				Id:   "stage:sq:kickoff",
				Args: mustMakeStruct(t, map[string]interface{}{"cls": cls}),
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to write kickoff stage: %v", err)
	}

	req := &pb.RunRequest{
		WorkPlanId:       wpID,
		StageId:          "stage:sq:config",
		AssignedCheckIds: []string{"sq:config"},
	}

	resp, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	if resp.StageState != pb.StageState_STAGE_STATE_FINAL {
		t.Errorf("Expected FINAL state, got %v", resp.StageState)
	}

	// Verify config was created
	if len(resp.CheckUpdates) == 0 {
		t.Error("Expected check updates with config")
	}
}

// TestTestExecutorRunner tests the test executor with simulated results
func TestTestExecutorRunner(t *testing.T) {
	ctx := context.Background()
	runner := NewTestExecutorRunner()

	tests := []struct {
		name     string
		testType string
		batchID  int
		wantPass bool
	}{
		{"Row 0 should pass", "row", 0, true},
		{"Row 2 should fail", "row", 2, false},
		{"Row 5 should fail", "row", 5, false},
		{"Column 3 should fail", "column", 3, false},
		{"Column 0 should pass", "column", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cls := make([]interface{}, 8)
			for i := 0; i < 8; i++ {
				cls[i] = fmt.Sprintf("CL-%d", i+1)
			}

			args := map[string]interface{}{
				"type": tt.testType,
				"cls":  cls,
			}

			if tt.testType == "row" {
				args["row"] = float64(tt.batchID)
			} else {
				args["col"] = float64(tt.batchID)
			}

			req := &pb.RunRequest{
				WorkPlanId:       "test-wp",
				StageId:          fmt.Sprintf("stage:sq:%s:%d:test", tt.testType, tt.batchID),
				AssignedCheckIds: []string{fmt.Sprintf("sq:%s:%d:test", tt.testType, tt.batchID)},
				Args:             mustMakeStruct(t, args),
			}

			resp, err := runner.HandleRun(ctx, req)
			if err != nil {
				t.Fatalf("HandleRun failed: %v", err)
			}

			// Check the check update's failure, not the response failure
			gotPass := len(resp.CheckUpdates) == 0 || resp.CheckUpdates[0].Failure == nil
			if gotPass != tt.wantPass {
				t.Errorf("Expected pass=%v, got pass=%v", tt.wantPass, gotPass)
			}
		})
	}
}

// Helper functions

func setupEmbeddedOrchestrator(t *testing.T, ctx context.Context) (ci.Orchestrator, func()) {
	// Create temp database
	tmpFile, err := os.CreateTemp("", "submit-queue-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	dbPath := tmpFile.Name()
	tmpFile.Close()

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Create services
	orchestratorSvc := service.NewOrchestrator(store)
	runnerSvc := service.NewRunnerService(store)
	callbackSvc := service.NewCallbackService(store, orchestratorSvc)

	// Create dispatcher
	dispatcherCfg := service.DefaultDispatcherConfig()
	dispatcherCfg.PollInterval = 50 * time.Millisecond
	dispatcherCfg.CallbackAddress = "localhost:50051"
	dispatcher := service.NewDispatcher(store, runnerSvc, orchestratorSvc, dispatcherCfg)

	orchestratorSvc.SetDispatcher(dispatcher)

	dispatcher.SetClientFactory(func(addr string) (service.StageRunnerClient, error) {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, err
		}
		return &grpcRunnerClient{client: pb.NewStageRunnerClient(conn), conn: conn}, nil
	})

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
	go func() {
		if err := server.Serve(":50051"); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	// Connect to our own server using RemoteOrchestrator
	orchestrator, err := ci.NewRemoteOrchestrator("localhost:50051")
	if err != nil {
		t.Fatalf("Failed to connect to orchestrator: %v", err)
	}

	cleanup := func() {
		orchestrator.Close()
		server.GracefulStop()
		store.Close()
		os.Remove(dbPath)
	}

	return orchestrator, cleanup
}

func setupTestOrchestrator(t *testing.T, ctx context.Context) (pb.TurboCIOrchestratorClient, string, func()) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	orchestratorSvc := service.NewOrchestrator(store)

	// Create work plan
	wp, err := orchestratorSvc.CreateWorkPlan(ctx, &service.CreateWorkPlanRequest{})
	if err != nil {
		t.Fatalf("Failed to create work plan: %v", err)
	}

	// Wrap in gRPC for testing
	endpoints := endpoint.MakeEndpoints(orchestratorSvc)
	server := grpctransport.NewServer(endpoints)

	go server.Serve(":50052")
	time.Sleep(100 * time.Millisecond)

	conn, err := grpc.NewClient("localhost:50052", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	client := pb.NewTurboCIOrchestratorClient(conn)

	cleanup := func() {
		conn.Close()
		server.GracefulStop()
		store.Close()
	}

	return client, wp.ID, cleanup
}

func startAllRunners(t *testing.T, ctx context.Context, orchAddr string) []*RunnerProcess {
	runnerTypes := []struct {
		typ  string
		port int
	}{
		{"kickoff", 50070},
		{"config", 50071},
		{"assignment", 50072},
		{"planning", 50073},
		{"test_executor", 50074},
		{"minibatch_finished", 50075},
		{"batch_finished", 50076},
	}

	var runners []*RunnerProcess

	for _, rt := range runnerTypes {
		runner := &RunnerProcess{
			typ:      rt.typ,
			port:     rt.port,
			orchAddr: orchAddr,
		}
		runner.Start(t, ctx)
		runners = append(runners, runner)
	}

	return runners
}

func stopAllRunners(runners []*RunnerProcess) {
	for _, r := range runners {
		r.Stop()
	}
}

type RunnerProcess struct {
	typ      string
	port     int
	orchAddr string
	runner   *common.BaseRunner
	done     chan struct{}
}

func (rp *RunnerProcess) Start(t *testing.T, ctx context.Context) {
	runnerID := fmt.Sprintf("%s-test-%d", rp.typ, rp.port)
	listenAddr := fmt.Sprintf(":%d", rp.port)

	var handler common.RunHandler
	switch rp.typ {
	case "kickoff":
		handler = NewKickoffRunner().HandleRun
	case "config":
		handler = NewConfigRunner().HandleRun
	case "assignment":
		handler = NewAssignmentRunner().HandleRun
	case "planning":
		handler = NewPlanningRunner().HandleRun
	case "test_executor":
		handler = NewTestExecutorRunner().HandleRun
	case "minibatch_finished":
		handler = NewMinibatchFinishedRunner().HandleRun
	case "batch_finished":
		handler = NewBatchFinishedRunner().HandleRun
	default:
		t.Fatalf("Unknown runner type: %s", rp.typ)
	}

	baseRunner := common.NewBaseRunner(runnerID, rp.typ, listenAddr, rp.orchAddr, 10)
	baseRunner.SetRunHandler(handler)

	rp.runner = baseRunner
	rp.done = make(chan struct{})

	// Start server
	go func() {
		if err := baseRunner.StartServer(); err != nil {
			t.Logf("Runner %s server error: %v", rp.typ, err)
		}
		close(rp.done)
	}()

	time.Sleep(50 * time.Millisecond)

	// Register
	if err := baseRunner.Register(ctx); err != nil {
		t.Fatalf("Failed to register %s runner: %v", rp.typ, err)
	}

	t.Logf("Started %s runner on :%d", rp.typ, rp.port)
}

func (rp *RunnerProcess) Stop() {
	if rp.runner != nil {
		rp.runner.Shutdown(context.Background())
		<-rp.done
	}
}

func verifyResults(t *testing.T, ctx context.Context, exec *ci.WorkflowExecution) {
	// Verify row results
	for i := 0; i < 8; i++ {
		checkID := fmt.Sprintf("sq:row:%d:finished", i)
		check, err := exec.GetCheckResult(ctx, checkID)
		if err != nil {
			t.Errorf("Failed to get row %d finished check: %v", i, err)
			continue
		}

		if check.State != domain.CheckStateFinal {
			t.Errorf("Row %d finished check not final: %v", i, check.State)
		}

		t.Logf("Row %d: state=%v, results=%d", i, check.State, len(check.Results))
	}

	// Count passed and failed rows
	passedRows := 0
	failedRows := 0

	for i := 0; i < 8; i++ {
		checkID := fmt.Sprintf("sq:row:%d:finished", i)
		check, _ := exec.GetCheckResult(ctx, checkID)
		if len(check.Results) > 0 && check.Results[0].Data != nil {
			if passed, ok := check.Results[0].Data["passed"].(bool); ok {
				if passed {
					passedRows++
				} else {
					failedRows++
				}
			}
		}
	}

	t.Logf("Results: rows_passed=%d, rows_failed=%d", passedRows, failedRows)

	// Expect rows 2 and 5 to fail (based on simulateTest logic)
	if passedRows != 6 {
		t.Errorf("Expected 6 passed rows, got %d", passedRows)
	}
	if failedRows != 2 {
		t.Errorf("Expected 2 failed rows, got %d", failedRows)
	}
}

func mustMakeStruct(t *testing.T, data map[string]interface{}) *structpb.Struct {
	s, err := structpb.NewStruct(data)
	if err != nil {
		t.Fatalf("Failed to create struct: %v", err)
	}
	return s
}

// grpcRunnerClient implements service.StageRunnerClient by wrapping a gRPC client
type grpcRunnerClient struct {
	client pb.StageRunnerClient
	conn   *grpc.ClientConn
}

func (c *grpcRunnerClient) Run(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
	pbReq := &pb.RunRequest{
		ExecutionId: req.ExecutionID,
		WorkPlanId:  req.WorkPlanID,
		StageId:     req.StageID,
		AttemptIdx:  int32(req.AttemptIdx),
	}

	// Convert check options to assigned check IDs and options
	for _, opt := range req.CheckOptions {
		pbReq.AssignedCheckIds = append(pbReq.AssignedCheckIds, opt.CheckID)

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

	if req.Args != nil {
		if s, err := structpb.NewStruct(req.Args); err == nil {
			pbReq.Args = s
		}
	}

	pbResp, err := c.client.Run(ctx, pbReq)
	if err != nil {
		return nil, err
	}

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
	pbReq := &pb.RunAsyncRequest{
		ExecutionId: req.ExecutionID,
		WorkPlanId:  req.WorkPlanID,
		StageId:     req.StageID,
		AttemptIdx:  int32(req.AttemptIdx),
	}

	if req.Args != nil {
		if s, err := structpb.NewStruct(req.Args); err == nil {
			pbReq.Args = s
		}
	}

	for _, opt := range req.CheckOptions {
		pbReq.AssignedCheckIds = append(pbReq.AssignedCheckIds, opt.CheckID)
	}

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
		Healthy: len(pbResp.RunnerType) > 0,
	}, nil
}

func (c *grpcRunnerClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
