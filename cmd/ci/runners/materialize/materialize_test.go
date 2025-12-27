// Package main contains tests for the materialize runner.
package main

import (
	"context"
	"sync"
	"testing"

	"google.golang.org/grpc"

	"github.com/example/turboci-lite/cmd/ci/runners/common"
	pb "github.com/example/turboci-lite/gen/turboci/v1"
)

// mockOrchestratorClient is a mock implementation for testing.
type mockOrchestratorClient struct {
	mu sync.Mutex

	// Recorded calls
	writeNodesCalls []*pb.WriteNodesRequest
	queryNodesCalls []*pb.QueryNodesRequest

	// Configurable responses
	queryNodesResponse *pb.QueryNodesResponse
}

func newMockOrchestratorClient() *mockOrchestratorClient {
	return &mockOrchestratorClient{}
}

func (m *mockOrchestratorClient) CreateWorkPlan(ctx context.Context, in *pb.CreateWorkPlanRequest, opts ...grpc.CallOption) (*pb.CreateWorkPlanResponse, error) {
	return &pb.CreateWorkPlanResponse{WorkPlan: &pb.WorkPlan{Id: "test-wp"}}, nil
}

func (m *mockOrchestratorClient) GetWorkPlan(ctx context.Context, in *pb.GetWorkPlanRequest, opts ...grpc.CallOption) (*pb.GetWorkPlanResponse, error) {
	return &pb.GetWorkPlanResponse{WorkPlan: &pb.WorkPlan{Id: in.Id}}, nil
}

func (m *mockOrchestratorClient) WriteNodes(ctx context.Context, in *pb.WriteNodesRequest, opts ...grpc.CallOption) (*pb.WriteNodesResponse, error) {
	m.mu.Lock()
	m.writeNodesCalls = append(m.writeNodesCalls, in)
	m.mu.Unlock()
	return &pb.WriteNodesResponse{}, nil
}

func (m *mockOrchestratorClient) QueryNodes(ctx context.Context, in *pb.QueryNodesRequest, opts ...grpc.CallOption) (*pb.QueryNodesResponse, error) {
	m.mu.Lock()
	m.queryNodesCalls = append(m.queryNodesCalls, in)
	resp := m.queryNodesResponse
	m.mu.Unlock()
	if resp != nil {
		return resp, nil
	}
	return &pb.QueryNodesResponse{}, nil
}

func (m *mockOrchestratorClient) RegisterStageRunner(ctx context.Context, in *pb.RegisterStageRunnerRequest, opts ...grpc.CallOption) (*pb.RegisterStageRunnerResponse, error) {
	return &pb.RegisterStageRunnerResponse{RegistrationId: "reg-001"}, nil
}

func (m *mockOrchestratorClient) UnregisterStageRunner(ctx context.Context, in *pb.UnregisterStageRunnerRequest, opts ...grpc.CallOption) (*pb.UnregisterStageRunnerResponse, error) {
	return &pb.UnregisterStageRunnerResponse{}, nil
}

func (m *mockOrchestratorClient) ListStageRunners(ctx context.Context, in *pb.ListStageRunnersRequest, opts ...grpc.CallOption) (*pb.ListStageRunnersResponse, error) {
	return &pb.ListStageRunnersResponse{}, nil
}

func (m *mockOrchestratorClient) UpdateStageExecution(ctx context.Context, in *pb.UpdateStageExecutionRequest, opts ...grpc.CallOption) (*pb.UpdateStageExecutionResponse, error) {
	return &pb.UpdateStageExecutionResponse{}, nil
}

func (m *mockOrchestratorClient) CompleteStageExecution(ctx context.Context, in *pb.CompleteStageExecutionRequest, opts ...grpc.CallOption) (*pb.CompleteStageExecutionResponse, error) {
	return &pb.CompleteStageExecutionResponse{}, nil
}

func (m *mockOrchestratorClient) WatchWorkPlan(ctx context.Context, in *pb.WatchWorkPlanRequest, opts ...grpc.CallOption) (pb.TurboCIOrchestrator_WatchWorkPlanClient, error) {
	return nil, nil
}

func (m *mockOrchestratorClient) getWriteNodesCalls() []*pb.WriteNodesRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeNodesCalls
}

// createTestRunner creates a MaterializeRunner with a mock orchestrator for testing.
func createTestRunner(mockClient *mockOrchestratorClient) *MaterializeRunner {
	baseRunner := common.NewBaseRunner("test-runner", "materialize", ":0", "localhost:50051", 1)
	baseRunner.Orchestrator = mockClient
	return &MaterializeRunner{BaseRunner: baseRunner}
}

// TestMaterializeRunner_CollectorCheckUsesPlannedState is a regression test to ensure
// the collector check is created with PLANNED state, not PLANNING state.
// Bug: The collector check was created in PLANNING state, but dependency resolution
// only processes checks in PLANNED state, so it would never advance to WAITING.
func TestMaterializeRunner_CollectorCheckUsesPlannedState(t *testing.T) {
	ctx := context.Background()

	mockClient := newMockOrchestratorClient()
	// Return only infrastructure checks (no build/test checks)
	mockClient.queryNodesResponse = &pb.QueryNodesResponse{
		Checks: []*pb.Check{
			{Id: "trigger:detect_changes", Kind: "trigger", State: pb.CheckState_CHECK_STATE_FINAL},
			{Id: "materialize:create_stages", Kind: "materialize", State: pb.CheckState_CHECK_STATE_WAITING},
		},
	}

	runner := createTestRunner(mockClient)

	req := &pb.RunRequest{
		WorkPlanId:       "wp-001",
		StageId:          "stage:materialize",
		AssignedCheckIds: []string{"materialize:create_stages"},
	}

	_, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	// Find the WriteNodes call that creates the collector check
	calls := mockClient.getWriteNodesCalls()
	if len(calls) < 1 {
		t.Fatal("expected at least 1 WriteNodes call")
	}

	// First call should create the collector check
	collectorCall := calls[0]
	if len(collectorCall.Checks) != 1 {
		t.Fatalf("expected 1 check in first WriteNodes call, got %d", len(collectorCall.Checks))
	}

	collectorCheck := collectorCall.Checks[0]
	if collectorCheck.Id != "collector:results" {
		t.Errorf("expected collector:results check, got %s", collectorCheck.Id)
	}

	// REGRESSION CHECK: Must use PLANNED state, not PLANNING
	if collectorCheck.State != pb.CheckState_CHECK_STATE_PLANNED {
		t.Errorf("REGRESSION: collector check must use PLANNED state (not PLANNING) for dependency resolution to work; got %v", collectorCheck.State)
	}
}

// TestMaterializeRunner_CollectorStageCreatedWhenNoChecks is a regression test to ensure
// the collector stage is always created, even when there are no build/test checks.
// Bug: When no build/test checks existed, the code returned early without creating
// the collector stage, causing the CI to hang forever.
func TestMaterializeRunner_CollectorStageCreatedWhenNoChecks(t *testing.T) {
	ctx := context.Background()

	mockClient := newMockOrchestratorClient()
	// Return only infrastructure checks (trigger and materialize - no build/test checks)
	mockClient.queryNodesResponse = &pb.QueryNodesResponse{
		Checks: []*pb.Check{
			{Id: "trigger:detect_changes", Kind: "trigger", State: pb.CheckState_CHECK_STATE_FINAL},
			{Id: "materialize:create_stages", Kind: "materialize", State: pb.CheckState_CHECK_STATE_WAITING},
		},
	}

	runner := createTestRunner(mockClient)

	req := &pb.RunRequest{
		WorkPlanId:       "wp-001",
		StageId:          "stage:materialize",
		AssignedCheckIds: []string{"materialize:create_stages"},
	}

	_, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	// Find the WriteNodes call that creates stages
	calls := mockClient.getWriteNodesCalls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 WriteNodes calls (collector check + stages), got %d", len(calls))
	}

	// Second call should create stages (including collector stage)
	stagesCall := calls[1]

	// REGRESSION CHECK: Must have at least the collector stage
	if len(stagesCall.Stages) < 1 {
		t.Fatal("REGRESSION: collector stage must be created even when there are no build/test checks")
	}

	// Find the collector stage
	var collectorStage *pb.StageWrite
	for _, stage := range stagesCall.Stages {
		if stage.Id == "stage:collector" {
			collectorStage = stage
			break
		}
	}

	if collectorStage == nil {
		t.Fatal("REGRESSION: stage:collector not found in WriteNodes call")
	}

	// Verify the collector stage has the right runner type
	if collectorStage.RunnerType != "result_collector" {
		t.Errorf("expected result_collector runner type, got %s", collectorStage.RunnerType)
	}

	// Verify it assigns to the collector check
	if len(collectorStage.Assignments) != 1 || collectorStage.Assignments[0].TargetCheckId != "collector:results" {
		t.Errorf("collector stage should assign to collector:results check")
	}
}

// TestMaterializeRunner_CollectorStageHasNoDepsWhenNoChecks verifies that when there
// are no build/test checks, the collector stage has no dependencies (so it runs immediately).
func TestMaterializeRunner_CollectorStageHasNoDepsWhenNoChecks(t *testing.T) {
	ctx := context.Background()

	mockClient := newMockOrchestratorClient()
	// Return only infrastructure checks (no build/test checks)
	mockClient.queryNodesResponse = &pb.QueryNodesResponse{
		Checks: []*pb.Check{
			{Id: "trigger:detect_changes", Kind: "trigger", State: pb.CheckState_CHECK_STATE_FINAL},
			{Id: "materialize:create_stages", Kind: "materialize", State: pb.CheckState_CHECK_STATE_WAITING},
		},
	}

	runner := createTestRunner(mockClient)

	req := &pb.RunRequest{
		WorkPlanId:       "wp-001",
		StageId:          "stage:materialize",
		AssignedCheckIds: []string{"materialize:create_stages"},
	}

	_, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	calls := mockClient.getWriteNodesCalls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 WriteNodes calls, got %d", len(calls))
	}

	stagesCall := calls[1]
	var collectorStage *pb.StageWrite
	for _, stage := range stagesCall.Stages {
		if stage.Id == "stage:collector" {
			collectorStage = stage
			break
		}
	}

	if collectorStage == nil {
		t.Fatal("collector stage not found")
	}

	// When there are no build/test stages, collector should have no dependencies
	// (or empty dependencies) so it can run immediately
	if collectorStage.Dependencies != nil && len(collectorStage.Dependencies.Dependencies) > 0 {
		t.Errorf("collector stage should have no dependencies when there are no build/test stages, got %d", len(collectorStage.Dependencies.Dependencies))
	}
}

// TestMaterializeRunner_WithBuildChecks verifies normal operation with build/test checks.
func TestMaterializeRunner_WithBuildChecks(t *testing.T) {
	ctx := context.Background()

	mockClient := newMockOrchestratorClient()
	mockClient.queryNodesResponse = &pb.QueryNodesResponse{
		Checks: []*pb.Check{
			{Id: "trigger:detect_changes", Kind: "trigger", State: pb.CheckState_CHECK_STATE_FINAL},
			{Id: "materialize:create_stages", Kind: "materialize", State: pb.CheckState_CHECK_STATE_WAITING},
			{Id: "build:myapp", Kind: "build", State: pb.CheckState_CHECK_STATE_PLANNED},
			{Id: "test:mypkg", Kind: "unit_test", State: pb.CheckState_CHECK_STATE_PLANNED},
		},
	}

	runner := createTestRunner(mockClient)

	req := &pb.RunRequest{
		WorkPlanId:       "wp-001",
		StageId:          "stage:materialize",
		AssignedCheckIds: []string{"materialize:create_stages"},
	}

	_, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	calls := mockClient.getWriteNodesCalls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 WriteNodes calls, got %d", len(calls))
	}

	stagesCall := calls[1]

	// Should have 3 stages: build, test, collector
	if len(stagesCall.Stages) != 3 {
		t.Errorf("expected 3 stages (build, test, collector), got %d", len(stagesCall.Stages))
	}

	// Verify collector stage exists and has dependencies on both build and test stages
	var collectorStage *pb.StageWrite
	for _, stage := range stagesCall.Stages {
		if stage.Id == "stage:collector" {
			collectorStage = stage
			break
		}
	}

	if collectorStage == nil {
		t.Fatal("collector stage not found")
	}

	if collectorStage.Dependencies == nil || len(collectorStage.Dependencies.Dependencies) != 2 {
		t.Errorf("collector stage should depend on 2 stages (build and test), got %v", collectorStage.Dependencies)
	}
}
