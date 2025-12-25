// Package testing provides integration tests for the CI system.
package testing

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage/sqlite"
)

// MockOrchestratorClient implements pb.TurboCIOrchestratorClient for testing runners.
type MockOrchestratorClient struct {
	mu sync.Mutex

	// Recorded calls for verification
	WriteNodesCalls []*pb.WriteNodesRequest
	QueryNodesCalls []*pb.QueryNodesRequest

	// Configurable responses
	WriteNodesResponse  *pb.WriteNodesResponse
	WriteNodesErr       error
	QueryNodesResponse  *pb.QueryNodesResponse
	QueryNodesErr       error
	QueryNodesResponses map[string]*pb.QueryNodesResponse // keyed by first check ID

	// Handlers for dynamic responses
	OnWriteNodes func(req *pb.WriteNodesRequest) (*pb.WriteNodesResponse, error)
	OnQueryNodes func(req *pb.QueryNodesRequest) (*pb.QueryNodesResponse, error)
}

func NewMockOrchestratorClient() *MockOrchestratorClient {
	return &MockOrchestratorClient{
		QueryNodesResponses: make(map[string]*pb.QueryNodesResponse),
	}
}

func (m *MockOrchestratorClient) CreateWorkPlan(ctx context.Context, in *pb.CreateWorkPlanRequest, opts ...grpc.CallOption) (*pb.CreateWorkPlanResponse, error) {
	return &pb.CreateWorkPlanResponse{WorkPlan: &pb.WorkPlan{Id: "test-wp-001"}}, nil
}

func (m *MockOrchestratorClient) GetWorkPlan(ctx context.Context, in *pb.GetWorkPlanRequest, opts ...grpc.CallOption) (*pb.GetWorkPlanResponse, error) {
	return &pb.GetWorkPlanResponse{WorkPlan: &pb.WorkPlan{Id: in.Id}}, nil
}

func (m *MockOrchestratorClient) WriteNodes(ctx context.Context, in *pb.WriteNodesRequest, opts ...grpc.CallOption) (*pb.WriteNodesResponse, error) {
	m.mu.Lock()
	m.WriteNodesCalls = append(m.WriteNodesCalls, in)
	m.mu.Unlock()

	if m.OnWriteNodes != nil {
		return m.OnWriteNodes(in)
	}
	if m.WriteNodesErr != nil {
		return nil, m.WriteNodesErr
	}
	if m.WriteNodesResponse != nil {
		return m.WriteNodesResponse, nil
	}
	return &pb.WriteNodesResponse{}, nil
}

func (m *MockOrchestratorClient) QueryNodes(ctx context.Context, in *pb.QueryNodesRequest, opts ...grpc.CallOption) (*pb.QueryNodesResponse, error) {
	m.mu.Lock()
	m.QueryNodesCalls = append(m.QueryNodesCalls, in)
	m.mu.Unlock()

	if m.OnQueryNodes != nil {
		return m.OnQueryNodes(in)
	}

	// Check for specific responses based on check ID
	if len(in.CheckIds) > 0 {
		if resp, ok := m.QueryNodesResponses[in.CheckIds[0]]; ok {
			return resp, nil
		}
	}

	if m.QueryNodesErr != nil {
		return nil, m.QueryNodesErr
	}
	if m.QueryNodesResponse != nil {
		return m.QueryNodesResponse, nil
	}
	return &pb.QueryNodesResponse{}, nil
}

func (m *MockOrchestratorClient) RegisterStageRunner(ctx context.Context, in *pb.RegisterStageRunnerRequest, opts ...grpc.CallOption) (*pb.RegisterStageRunnerResponse, error) {
	return &pb.RegisterStageRunnerResponse{RegistrationId: "reg-001"}, nil
}

func (m *MockOrchestratorClient) UnregisterStageRunner(ctx context.Context, in *pb.UnregisterStageRunnerRequest, opts ...grpc.CallOption) (*pb.UnregisterStageRunnerResponse, error) {
	return &pb.UnregisterStageRunnerResponse{}, nil
}

func (m *MockOrchestratorClient) ListStageRunners(ctx context.Context, in *pb.ListStageRunnersRequest, opts ...grpc.CallOption) (*pb.ListStageRunnersResponse, error) {
	return &pb.ListStageRunnersResponse{}, nil
}

func (m *MockOrchestratorClient) UpdateStageExecution(ctx context.Context, in *pb.UpdateStageExecutionRequest, opts ...grpc.CallOption) (*pb.UpdateStageExecutionResponse, error) {
	return &pb.UpdateStageExecutionResponse{}, nil
}

func (m *MockOrchestratorClient) CompleteStageExecution(ctx context.Context, in *pb.CompleteStageExecutionRequest, opts ...grpc.CallOption) (*pb.CompleteStageExecutionResponse, error) {
	return &pb.CompleteStageExecutionResponse{}, nil
}

func (m *MockOrchestratorClient) WatchWorkPlan(ctx context.Context, in *pb.WatchWorkPlanRequest, opts ...grpc.CallOption) (pb.TurboCIOrchestrator_WatchWorkPlanClient, error) {
	// Return nil - mock doesn't support streaming
	return nil, nil
}

// GetWriteNodesCallCount returns the number of WriteNodes calls.
func (m *MockOrchestratorClient) GetWriteNodesCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.WriteNodesCalls)
}

// GetQueryNodesCallCount returns the number of QueryNodes calls.
func (m *MockOrchestratorClient) GetQueryNodesCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.QueryNodesCalls)
}

// ========== TriggerRunner Tests ==========

// TriggerRunnerTestable is a testable version of the trigger runner.
type TriggerRunnerTestable struct {
	Orchestrator pb.TurboCIOrchestratorClient
	RepoRoot     string
}

// HandleRun processes a trigger builds request (extracted logic for testing).
func (t *TriggerRunnerTestable) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	// For testing, we'll create a simplified version that:
	// 1. Scans for Go packages in the repo
	// 2. Creates checks for them

	// Count packages and binaries (simplified)
	packages, binaries, e2eTests := t.analyzeRepo()

	// Create checks via WriteNodes
	var checks []*pb.CheckWrite

	// Create build checks for binaries
	for _, bin := range binaries {
		opts, _ := structpb.NewStruct(map[string]any{
			"binary": bin.Path,
			"output": bin.Name,
		})
		checks = append(checks, &pb.CheckWrite{
			Id:      "build:" + bin.Name,
			State:   pb.CheckState_CHECK_STATE_PLANNING,
			Kind:    "build",
			Options: opts,
		})
	}

	// Create test checks for packages
	for _, pkg := range packages {
		opts, _ := structpb.NewStruct(map[string]any{
			"package": pkg.Path,
		})
		checks = append(checks, &pb.CheckWrite{
			Id:      "test:" + pkg.Path,
			State:   pb.CheckState_CHECK_STATE_PLANNING,
			Kind:    "unit_test",
			Options: opts,
		})
	}

	// Create E2E test checks
	for _, e2e := range e2eTests {
		opts, _ := structpb.NewStruct(map[string]any{
			"test_path": e2e.Path,
			"name":      e2e.Name,
		})

		var deps []*pb.Dependency
		for _, depID := range e2e.DependsOn {
			deps = append(deps, &pb.Dependency{
				TargetType: pb.NodeType_NODE_TYPE_CHECK,
				TargetId:   depID,
			})
		}

		var depGroup *pb.DependencyGroup
		if len(deps) > 0 {
			depGroup = &pb.DependencyGroup{
				Predicate:    pb.PredicateType_PREDICATE_TYPE_AND,
				Dependencies: deps,
			}
		}

		checks = append(checks, &pb.CheckWrite{
			Id:           "e2e:" + e2e.Name,
			State:        pb.CheckState_CHECK_STATE_PLANNING,
			Kind:         "e2e_test",
			Options:      opts,
			Dependencies: depGroup,
		})
	}

	if len(checks) > 0 {
		_, err := t.Orchestrator.WriteNodes(ctx, &pb.WriteNodesRequest{
			WorkPlanId: req.WorkPlanId,
			Checks:     checks,
		})
		if err != nil {
			return nil, err
		}
	}

	resultData, _ := structpb.NewStruct(map[string]any{
		"packages_count":  len(packages),
		"binaries_count":  len(binaries),
		"e2e_tests_count": len(e2eTests),
	})

	return &pb.RunResponse{
		StageState: pb.StageState_STAGE_STATE_FINAL,
		CheckUpdates: []*pb.CheckUpdate{{
			CheckId:    req.AssignedCheckIds[0],
			State:      pb.CheckState_CHECK_STATE_FINAL,
			ResultData: resultData,
			Finalize:   true,
		}},
	}, nil
}

type testPackage struct {
	Path string
}

type testBinary struct {
	Name string
	Path string
}

type testE2E struct {
	Name      string
	Path      string
	DependsOn []string
}

func (t *TriggerRunnerTestable) analyzeRepo() ([]testPackage, []testBinary, []testE2E) {
	var packages []testPackage
	var binaries []testBinary
	var e2eTests []testE2E

	// Check if repo has Go files
	entries, err := os.ReadDir(t.RepoRoot)
	if err != nil {
		return packages, binaries, e2eTests
	}

	hasGoFiles := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".go" {
			hasGoFiles = true
			break
		}
	}

	if hasGoFiles {
		packages = append(packages, testPackage{Path: "."})
	}

	// Check for cmd directory
	cmdDir := filepath.Join(t.RepoRoot, "cmd")
	if cmdEntries, err := os.ReadDir(cmdDir); err == nil {
		for _, e := range cmdEntries {
			if e.IsDir() {
				binPath := filepath.Join(cmdDir, e.Name(), "main.go")
				if _, err := os.Stat(binPath); err == nil {
					binaries = append(binaries, testBinary{
						Name: e.Name(),
						Path: "./cmd/" + e.Name(),
					})
				}
			}
		}
	}

	return packages, binaries, e2eTests
}

func TestTriggerRunner_CreatesChecks(t *testing.T) {
	ctx := context.Background()

	// Create a temp directory with Go files
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "cmd"), 0755)
	os.Mkdir(filepath.Join(tmpDir, "cmd", "myapp"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "cmd", "myapp", "main.go"), []byte("package main\nfunc main() {}\n"), 0644)

	mockClient := NewMockOrchestratorClient()
	runner := &TriggerRunnerTestable{
		Orchestrator: mockClient,
		RepoRoot:     tmpDir,
	}

	req := &pb.RunRequest{
		WorkPlanId:       "wp-001",
		StageId:          "trigger-stage",
		AssignedCheckIds: []string{"trigger:check"},
	}

	resp, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	// Verify stage completed
	if resp.StageState != pb.StageState_STAGE_STATE_FINAL {
		t.Errorf("expected FINAL stage state, got %v", resp.StageState)
	}

	// Verify check was finalized
	if len(resp.CheckUpdates) != 1 || resp.CheckUpdates[0].State != pb.CheckState_CHECK_STATE_FINAL {
		t.Errorf("expected check to be finalized")
	}

	// Verify WriteNodes was called
	if mockClient.GetWriteNodesCallCount() != 1 {
		t.Fatalf("expected 1 WriteNodes call, got %d", mockClient.GetWriteNodesCallCount())
	}

	writeReq := mockClient.WriteNodesCalls[0]
	if writeReq.WorkPlanId != "wp-001" {
		t.Errorf("expected work plan ID wp-001, got %s", writeReq.WorkPlanId)
	}

	// Should have created at least 1 package check and 1 binary check
	hasPackageCheck := false
	hasBinaryCheck := false
	for _, check := range writeReq.Checks {
		if check.Kind == "unit_test" {
			hasPackageCheck = true
		}
		if check.Kind == "build" {
			hasBinaryCheck = true
		}
	}

	if !hasPackageCheck {
		t.Error("expected at least one unit_test check")
	}
	if !hasBinaryCheck {
		t.Error("expected at least one build check")
	}
}

func TestTriggerRunner_EmptyRepo(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	mockClient := NewMockOrchestratorClient()
	runner := &TriggerRunnerTestable{
		Orchestrator: mockClient,
		RepoRoot:     tmpDir,
	}

	req := &pb.RunRequest{
		WorkPlanId:       "wp-001",
		StageId:          "trigger-stage",
		AssignedCheckIds: []string{"trigger:check"},
	}

	resp, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	// Should still complete successfully
	if resp.StageState != pb.StageState_STAGE_STATE_FINAL {
		t.Errorf("expected FINAL stage state, got %v", resp.StageState)
	}

	// No WriteNodes call for empty repo
	if mockClient.GetWriteNodesCallCount() != 0 {
		t.Errorf("expected 0 WriteNodes calls for empty repo, got %d", mockClient.GetWriteNodesCallCount())
	}
}

// ========== MaterializeRunner Tests ==========

// MaterializeRunnerTestable is a testable version of the materialize runner.
type MaterializeRunnerTestable struct {
	Orchestrator pb.TurboCIOrchestratorClient
}

func (m *MaterializeRunnerTestable) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	// Query all checks in the work plan
	queryResp, err := m.Orchestrator.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    req.WorkPlanId,
		IncludeChecks: true,
	})
	if err != nil {
		return nil, err
	}

	// Create collector check
	_, err = m.Orchestrator.WriteNodes(ctx, &pb.WriteNodesRequest{
		WorkPlanId: req.WorkPlanId,
		Checks: []*pb.CheckWrite{{
			Id:    "collector:results",
			State: pb.CheckState_CHECK_STATE_PLANNING,
			Kind:  "collector",
		}},
	})
	if err != nil {
		return nil, err
	}

	// Create stages for each check
	var stages []*pb.StageWrite
	var allStageIDs []string

	for _, check := range queryResp.Checks {
		// Skip infrastructure checks
		if check.Kind == "trigger" || check.Kind == "materialize" || check.Kind == "collector" {
			continue
		}

		var runnerType string
		switch check.Kind {
		case "build":
			runnerType = "go_builder"
		case "unit_test":
			runnerType = "go_tester"
		case "e2e_test":
			runnerType = "conditional_tester"
		default:
			continue
		}

		stageID := "stage:" + check.Id
		stages = append(stages, &pb.StageWrite{
			Id:            stageID,
			State:         pb.StageState_STAGE_STATE_PLANNED,
			RunnerType:    runnerType,
			ExecutionMode: pb.ExecutionMode_EXECUTION_MODE_SYNC,
			Args:          check.Options,
			Assignments: []*pb.Assignment{{
				TargetCheckId: check.Id,
				GoalState:     pb.CheckState_CHECK_STATE_FINAL,
			}},
		})
		allStageIDs = append(allStageIDs, stageID)
	}

	// Create collector stage
	if len(allStageIDs) > 0 {
		var deps []*pb.Dependency
		for _, stageID := range allStageIDs {
			deps = append(deps, &pb.Dependency{
				TargetType: pb.NodeType_NODE_TYPE_STAGE,
				TargetId:   stageID,
			})
		}

		stages = append(stages, &pb.StageWrite{
			Id:            "stage:collector",
			State:         pb.StageState_STAGE_STATE_PLANNED,
			RunnerType:    "result_collector",
			ExecutionMode: pb.ExecutionMode_EXECUTION_MODE_SYNC,
			Assignments: []*pb.Assignment{{
				TargetCheckId: "collector:results",
				GoalState:     pb.CheckState_CHECK_STATE_FINAL,
			}},
			Dependencies: &pb.DependencyGroup{
				Predicate:    pb.PredicateType_PREDICATE_TYPE_AND,
				Dependencies: deps,
			},
		})
	}

	// Write stages
	if len(stages) > 0 {
		_, err = m.Orchestrator.WriteNodes(ctx, &pb.WriteNodesRequest{
			WorkPlanId: req.WorkPlanId,
			Stages:     stages,
		})
		if err != nil {
			return nil, err
		}
	}

	resultData, _ := structpb.NewStruct(map[string]any{
		"stages_created": len(stages),
	})

	return &pb.RunResponse{
		StageState: pb.StageState_STAGE_STATE_FINAL,
		CheckUpdates: []*pb.CheckUpdate{{
			CheckId:    req.AssignedCheckIds[0],
			State:      pb.CheckState_CHECK_STATE_FINAL,
			ResultData: resultData,
			Finalize:   true,
		}},
	}, nil
}

func TestMaterializeRunner_CreatesStages(t *testing.T) {
	ctx := context.Background()

	mockClient := NewMockOrchestratorClient()

	// Set up query response with some checks
	mockClient.QueryNodesResponse = &pb.QueryNodesResponse{
		Checks: []*pb.Check{
			{Id: "build:myapp", Kind: "build", State: pb.CheckState_CHECK_STATE_PLANNING},
			{Id: "test:pkg1", Kind: "unit_test", State: pb.CheckState_CHECK_STATE_PLANNING},
			{Id: "test:pkg2", Kind: "unit_test", State: pb.CheckState_CHECK_STATE_PLANNING},
		},
	}

	runner := &MaterializeRunnerTestable{
		Orchestrator: mockClient,
	}

	req := &pb.RunRequest{
		WorkPlanId:       "wp-001",
		StageId:          "materialize-stage",
		AssignedCheckIds: []string{"materialize:check"},
	}

	resp, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	// Verify stage completed
	if resp.StageState != pb.StageState_STAGE_STATE_FINAL {
		t.Errorf("expected FINAL stage state, got %v", resp.StageState)
	}

	// Verify WriteNodes was called (collector check + stages)
	if mockClient.GetWriteNodesCallCount() < 2 {
		t.Fatalf("expected at least 2 WriteNodes calls, got %d", mockClient.GetWriteNodesCallCount())
	}

	// Check that stages were created
	stagesWriteReq := mockClient.WriteNodesCalls[1]
	if len(stagesWriteReq.Stages) != 4 { // 3 check stages + 1 collector
		t.Errorf("expected 4 stages, got %d", len(stagesWriteReq.Stages))
	}

	// Verify runner types
	runnerTypes := make(map[string]string)
	for _, stage := range stagesWriteReq.Stages {
		runnerTypes[stage.Id] = stage.RunnerType
	}

	if runnerTypes["stage:build:myapp"] != "go_builder" {
		t.Errorf("expected go_builder for build stage, got %s", runnerTypes["stage:build:myapp"])
	}
	if runnerTypes["stage:test:pkg1"] != "go_tester" {
		t.Errorf("expected go_tester for test stage, got %s", runnerTypes["stage:test:pkg1"])
	}
	if runnerTypes["stage:collector"] != "result_collector" {
		t.Errorf("expected result_collector for collector stage, got %s", runnerTypes["stage:collector"])
	}
}

func TestMaterializeRunner_CollectorDependsOnAllStages(t *testing.T) {
	ctx := context.Background()

	mockClient := NewMockOrchestratorClient()
	mockClient.QueryNodesResponse = &pb.QueryNodesResponse{
		Checks: []*pb.Check{
			{Id: "build:app1", Kind: "build", State: pb.CheckState_CHECK_STATE_PLANNING},
			{Id: "build:app2", Kind: "build", State: pb.CheckState_CHECK_STATE_PLANNING},
		},
	}

	runner := &MaterializeRunnerTestable{
		Orchestrator: mockClient,
	}

	req := &pb.RunRequest{
		WorkPlanId:       "wp-001",
		StageId:          "materialize-stage",
		AssignedCheckIds: []string{"materialize:check"},
	}

	_, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	// Find the collector stage
	var collectorStage *pb.StageWrite
	for _, call := range mockClient.WriteNodesCalls {
		for _, stage := range call.Stages {
			if stage.Id == "stage:collector" {
				collectorStage = stage
				break
			}
		}
	}

	if collectorStage == nil {
		t.Fatal("collector stage not found")
	}

	// Verify it depends on both build stages
	if collectorStage.Dependencies == nil {
		t.Fatal("collector stage has no dependencies")
	}

	depIDs := make(map[string]bool)
	for _, dep := range collectorStage.Dependencies.Dependencies {
		depIDs[dep.TargetId] = true
	}

	if !depIDs["stage:build:app1"] || !depIDs["stage:build:app2"] {
		t.Errorf("collector should depend on both build stages, got %v", depIDs)
	}
}

// ========== ConditionalRunner Tests ==========

// ConditionalRunnerTestable is a testable version of the conditional runner.
type ConditionalRunnerTestable struct {
	Orchestrator pb.TurboCIOrchestratorClient
	RunTest      func(testPath string) (passed, failed int, err error)
}

func (c *ConditionalRunnerTestable) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	checkID := req.AssignedCheckIds[0]

	var testPath, testName string
	var dependsOnChecks []string

	if req.Args != nil {
		if v := req.Args.Fields["test_path"]; v != nil {
			testPath = v.GetStringValue()
		}
		if v := req.Args.Fields["name"]; v != nil {
			testName = v.GetStringValue()
		}
		if v := req.Args.Fields["depends_on_checks"]; v != nil {
			if list := v.GetListValue(); list != nil {
				for _, item := range list.Values {
					dependsOnChecks = append(dependsOnChecks, item.GetStringValue())
				}
			}
		}
	}

	// Check upstream dependencies
	for _, depCheckID := range dependsOnChecks {
		resp, err := c.Orchestrator.QueryNodes(ctx, &pb.QueryNodesRequest{
			WorkPlanId:    req.WorkPlanId,
			CheckIds:      []string{depCheckID},
			IncludeChecks: true,
		})
		if err != nil {
			continue
		}

		if len(resp.Checks) > 0 {
			check := resp.Checks[0]
			if check.State == pb.CheckState_CHECK_STATE_FINAL && len(check.Results) > 0 {
				lastResult := check.Results[len(check.Results)-1]
				if lastResult.Failure != nil {
					// Skip - upstream failed
					resultData, _ := structpb.NewStruct(map[string]any{
						"skipped": true,
						"reason":  "upstream_dependency_failed",
					})
					return &pb.RunResponse{
						StageState: pb.StageState_STAGE_STATE_FINAL,
						CheckUpdates: []*pb.CheckUpdate{{
							CheckId:    checkID,
							State:      pb.CheckState_CHECK_STATE_FINAL,
							ResultData: resultData,
							Finalize:   true,
						}},
					}, nil
				}
			}
		}
	}

	// Run the test
	passed, failed, err := c.RunTest(testPath)
	testFailed := err != nil || failed > 0

	resultData, _ := structpb.NewStruct(map[string]any{
		"test_path": testPath,
		"test_name": testName,
		"passed":    passed,
		"failed":    failed,
		"success":   !testFailed,
	})

	resp := &pb.RunResponse{
		StageState: pb.StageState_STAGE_STATE_FINAL,
		CheckUpdates: []*pb.CheckUpdate{{
			CheckId:    checkID,
			State:      pb.CheckState_CHECK_STATE_FINAL,
			ResultData: resultData,
			Finalize:   true,
		}},
	}

	if testFailed {
		resp.CheckUpdates[0].Failure = &pb.Failure{Message: "E2E tests failed"}
	}

	return resp, nil
}

func TestConditionalRunner_RunsWhenUpstreamPasses(t *testing.T) {
	ctx := context.Background()

	mockClient := NewMockOrchestratorClient()
	// Set up upstream check as passed
	mockClient.QueryNodesResponses["upstream:test"] = &pb.QueryNodesResponse{
		Checks: []*pb.Check{{
			Id:    "upstream:test",
			State: pb.CheckState_CHECK_STATE_FINAL,
			Results: []*pb.CheckResult{{
				Failure: nil, // Passed
			}},
		}},
	}

	testRan := false
	runner := &ConditionalRunnerTestable{
		Orchestrator: mockClient,
		RunTest: func(testPath string) (passed, failed int, err error) {
			testRan = true
			return 5, 0, nil
		},
	}

	args, _ := structpb.NewStruct(map[string]any{
		"test_path":         "./e2e/...",
		"name":              "e2e-suite",
		"depends_on_checks": []any{"upstream:test"},
	})

	req := &pb.RunRequest{
		WorkPlanId:       "wp-001",
		StageId:          "e2e-stage",
		AssignedCheckIds: []string{"e2e:test"},
		Args:             args,
	}

	resp, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	if !testRan {
		t.Error("test should have run when upstream passed")
	}

	if resp.CheckUpdates[0].Failure != nil {
		t.Error("test should have passed")
	}

	resultData := resp.CheckUpdates[0].ResultData.AsMap()
	if resultData["success"] != true {
		t.Errorf("expected success=true, got %v", resultData["success"])
	}
}

func TestConditionalRunner_SkipsWhenUpstreamFails(t *testing.T) {
	ctx := context.Background()

	mockClient := NewMockOrchestratorClient()
	// Set up upstream check as failed
	mockClient.QueryNodesResponses["upstream:test"] = &pb.QueryNodesResponse{
		Checks: []*pb.Check{{
			Id:    "upstream:test",
			State: pb.CheckState_CHECK_STATE_FINAL,
			Results: []*pb.CheckResult{{
				Failure: &pb.Failure{Message: "upstream failed"},
			}},
		}},
	}

	testRan := false
	runner := &ConditionalRunnerTestable{
		Orchestrator: mockClient,
		RunTest: func(testPath string) (passed, failed int, err error) {
			testRan = true
			return 5, 0, nil
		},
	}

	args, _ := structpb.NewStruct(map[string]any{
		"test_path":         "./e2e/...",
		"name":              "e2e-suite",
		"depends_on_checks": []any{"upstream:test"},
	})

	req := &pb.RunRequest{
		WorkPlanId:       "wp-001",
		StageId:          "e2e-stage",
		AssignedCheckIds: []string{"e2e:test"},
		Args:             args,
	}

	resp, err := runner.HandleRun(ctx, req)
	if err != nil {
		t.Fatalf("HandleRun failed: %v", err)
	}

	if testRan {
		t.Error("test should NOT have run when upstream failed")
	}

	resultData := resp.CheckUpdates[0].ResultData.AsMap()
	if resultData["skipped"] != true {
		t.Errorf("expected skipped=true, got %v", resultData["skipped"])
	}
	if resultData["reason"] != "upstream_dependency_failed" {
		t.Errorf("expected reason=upstream_dependency_failed, got %v", resultData["reason"])
	}
}

// ========== End-to-End Integration Test ==========

// CITestEnv provides a complete test environment for CI integration testing.
type CITestEnv struct {
	Storage      *sqlite.SQLiteStorage
	Orchestrator *service.OrchestratorService
	RunnerSvc    *service.RunnerService
	CallbackSvc  *service.CallbackService
	Dispatcher   *service.Dispatcher

	MockRunners map[string]*MockCIRunner
	mu          sync.Mutex

	t      *testing.T
	dbPath string
}

// MockCIRunner is a mock runner for CI integration testing.
type MockCIRunner struct {
	RunnerID   string
	RunnerType string
	Address    string
	OnRun      func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error)

	mu           sync.Mutex
	ReceivedRuns []*service.RunRequest
}

func (m *MockCIRunner) Run(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
	m.mu.Lock()
	m.ReceivedRuns = append(m.ReceivedRuns, req)
	m.mu.Unlock()

	if m.OnRun != nil {
		return m.OnRun(ctx, req)
	}

	// Default: finalize all checks
	var updates []*service.CheckUpdate
	for _, opt := range req.CheckOptions {
		updates = append(updates, &service.CheckUpdate{
			CheckID:  opt.CheckID,
			State:    domain.CheckStateFinal,
			Finalize: true,
		})
	}
	return &service.RunResponse{
		StageState:   domain.StageStateFinal,
		CheckUpdates: updates,
	}, nil
}

func (m *MockCIRunner) RunAsync(ctx context.Context, req *service.RunAsyncRequest) (*service.RunAsyncResponse, error) {
	return &service.RunAsyncResponse{Accepted: false}, nil
}

func (m *MockCIRunner) Ping(ctx context.Context, req *service.PingRequest) (*service.PingResponse, error) {
	return &service.PingResponse{Healthy: true}, nil
}

func (m *MockCIRunner) GetRunCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ReceivedRuns)
}

// NewCITestEnv creates a new CI test environment.
func NewCITestEnv(t *testing.T) *CITestEnv {
	t.Helper()

	ctx := context.Background()
	tmpDir := os.TempDir()
	dbPath := filepath.Join(tmpDir, "turboci_ci_test_"+t.Name()+".db")

	os.Remove(dbPath)
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")

	storage, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	orchestrator := service.NewOrchestrator(storage)
	runnerSvc := service.NewRunnerService(storage)
	callbackSvc := service.NewCallbackService(storage, orchestrator)

	dispatcherCfg := service.DispatcherConfig{
		PollInterval:       50 * time.Millisecond,
		CleanupInterval:    time.Second,
		StaleCheckInterval: 500 * time.Millisecond,
		StaleDuration:      2 * time.Second,
		DefaultTimeout:     30 * time.Second,
		CallbackAddress:    "localhost:50051",
	}
	dispatcher := service.NewDispatcher(storage, runnerSvc, orchestrator, dispatcherCfg)

	env := &CITestEnv{
		Storage:      storage,
		Orchestrator: orchestrator,
		RunnerSvc:    runnerSvc,
		CallbackSvc:  callbackSvc,
		Dispatcher:   dispatcher,
		MockRunners:  make(map[string]*MockCIRunner),
		t:            t,
		dbPath:       dbPath,
	}

	dispatcher.SetClientFactory(env.mockClientFactory)

	return env
}

func (e *CITestEnv) mockClientFactory(address string) (service.StageRunnerClient, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if mock, ok := e.MockRunners[address]; ok {
		return mock, nil
	}
	return nil, domain.ErrRunnerUnavailable
}

func (e *CITestEnv) Start() {
	e.Dispatcher.Start()
}

func (e *CITestEnv) Stop() {
	e.Dispatcher.Stop()
	e.Storage.Close()
	if e.dbPath != "" {
		os.Remove(e.dbPath)
		os.Remove(e.dbPath + "-wal")
		os.Remove(e.dbPath + "-shm")
	}
}

func (e *CITestEnv) RegisterMockRunner(ctx context.Context, runnerID, runnerType, address string) *MockCIRunner {
	e.t.Helper()

	mock := &MockCIRunner{
		RunnerID:   runnerID,
		RunnerType: runnerType,
		Address:    address,
	}

	e.mu.Lock()
	e.MockRunners[address] = mock
	e.mu.Unlock()

	_, err := e.RunnerSvc.RegisterRunner(ctx, &service.RegisterRunnerRequest{
		RunnerID:       runnerID,
		RunnerType:     runnerType,
		Address:        address,
		SupportedModes: []domain.ExecutionMode{domain.ExecutionModeSync},
		MaxConcurrent:  10,
		TTLSeconds:     300,
	})
	if err != nil {
		e.t.Fatalf("failed to register mock runner: %v", err)
	}

	return mock
}

func (e *CITestEnv) WaitForCheckState(ctx context.Context, workPlanID, checkID string, expectedState domain.CheckState, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := e.Orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
			WorkPlanID:    workPlanID,
			CheckIDs:      []string{checkID},
			IncludeChecks: true,
		})
		if err == nil && len(resp.Checks) > 0 && resp.Checks[0].State == expectedState {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestE2E_FullCIWorkflow(t *testing.T) {
	ctx := context.Background()
	env := NewCITestEnv(t)
	defer env.Stop()

	// Register mock runners for each stage type
	triggerRunner := env.RegisterMockRunner(ctx, "trigger-1", "trigger_builds", "localhost:50061")
	materializeRunner := env.RegisterMockRunner(ctx, "materialize-1", "materialize", "localhost:50062")
	builderRunner := env.RegisterMockRunner(ctx, "builder-1", "go_builder", "localhost:50063")
	testerRunner := env.RegisterMockRunner(ctx, "tester-1", "go_tester", "localhost:50064")
	collectorRunner := env.RegisterMockRunner(ctx, "collector-1", "result_collector", "localhost:50066")

	// Set up trigger runner to create build and test checks
	triggerRunner.OnRun = func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
		// Simulate creating checks
		_, err := env.Orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
			WorkPlanID: req.WorkPlanID,
			Checks: []*service.CheckWrite{
				{ID: "build:app", State: ptr(domain.CheckStatePlanned), Kind: "build"},
				{ID: "test:pkg", State: ptr(domain.CheckStatePlanned), Kind: "unit_test"},
			},
		})
		if err != nil {
			return nil, err
		}

		return &service.RunResponse{
			StageState: domain.StageStateFinal,
			CheckUpdates: []*service.CheckUpdate{{
				CheckID:  req.CheckOptions[0].CheckID,
				State:    domain.CheckStateFinal,
				Finalize: true,
			}},
		}, nil
	}

	// Set up materialize runner to create stages
	materializeRunner.OnRun = func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
		// Create stages for the checks
		syncMode := domain.ExecutionModeSync
		_, err := env.Orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
			WorkPlanID: req.WorkPlanID,
			Checks: []*service.CheckWrite{
				{ID: "collector:results", State: ptr(domain.CheckStatePlanned), Kind: "collector"},
			},
			Stages: []*service.StageWrite{
				{
					ID:            "stage:build:app",
					ExecutionMode: &syncMode,
					RunnerType:    "go_builder",
					Assignments:   []domain.Assignment{{TargetCheckID: "build:app", GoalState: domain.CheckStateFinal}},
				},
				{
					ID:            "stage:test:pkg",
					ExecutionMode: &syncMode,
					RunnerType:    "go_tester",
					Assignments:   []domain.Assignment{{TargetCheckID: "test:pkg", GoalState: domain.CheckStateFinal}},
					Dependencies: &domain.DependencyGroup{
						Predicate: domain.PredicateAND,
						Dependencies: []domain.DependencyRef{{
							TargetType: domain.NodeTypeStage,
							TargetID:   "stage:build:app",
						}},
					},
				},
				{
					ID:            "stage:collector",
					ExecutionMode: &syncMode,
					RunnerType:    "result_collector",
					Assignments:   []domain.Assignment{{TargetCheckID: "collector:results", GoalState: domain.CheckStateFinal}},
					Dependencies: &domain.DependencyGroup{
						Predicate: domain.PredicateAND,
						Dependencies: []domain.DependencyRef{
							{TargetType: domain.NodeTypeStage, TargetID: "stage:build:app"},
							{TargetType: domain.NodeTypeStage, TargetID: "stage:test:pkg"},
						},
					},
				},
			},
		})
		if err != nil {
			return nil, err
		}

		return &service.RunResponse{
			StageState: domain.StageStateFinal,
			CheckUpdates: []*service.CheckUpdate{{
				CheckID:  req.CheckOptions[0].CheckID,
				State:    domain.CheckStateFinal,
				Finalize: true,
			}},
		}, nil
	}

	// Builder and tester runners use default behavior (succeed)

	// Start the dispatcher
	env.Start()

	// Create work plan
	wp, err := env.Orchestrator.CreateWorkPlan(ctx, nil)
	if err != nil {
		t.Fatalf("failed to create work plan: %v", err)
	}

	// Create initial trigger check and stage
	syncMode := domain.ExecutionModeSync
	_, err = env.Orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: wp.ID,
		Checks: []*service.CheckWrite{
			{ID: "trigger:check", State: ptr(domain.CheckStatePlanned), Kind: "trigger"},
			{ID: "materialize:check", State: ptr(domain.CheckStatePlanned), Kind: "materialize"},
		},
		Stages: []*service.StageWrite{
			{
				ID:            "trigger-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "trigger_builds",
				Assignments:   []domain.Assignment{{TargetCheckID: "trigger:check", GoalState: domain.CheckStateFinal}},
			},
			{
				ID:            "materialize-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "materialize",
				Assignments:   []domain.Assignment{{TargetCheckID: "materialize:check", GoalState: domain.CheckStateFinal}},
				Dependencies: &domain.DependencyGroup{
					Predicate: domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{
						TargetType: domain.NodeTypeStage,
						TargetID:   "trigger-stage",
					}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to write initial nodes: %v", err)
	}

	// Wait for the collector check to complete (final stage)
	if !env.WaitForCheckState(ctx, wp.ID, "collector:results", domain.CheckStateFinal, 10*time.Second) {
		t.Fatal("collector check did not reach FINAL state")
	}

	// Verify all stages ran
	if triggerRunner.GetRunCount() != 1 {
		t.Errorf("expected trigger runner to run 1 time, got %d", triggerRunner.GetRunCount())
	}
	if materializeRunner.GetRunCount() != 1 {
		t.Errorf("expected materialize runner to run 1 time, got %d", materializeRunner.GetRunCount())
	}
	if builderRunner.GetRunCount() != 1 {
		t.Errorf("expected builder runner to run 1 time, got %d", builderRunner.GetRunCount())
	}
	if testerRunner.GetRunCount() != 1 {
		t.Errorf("expected tester runner to run 1 time, got %d", testerRunner.GetRunCount())
	}
	if collectorRunner.GetRunCount() != 1 {
		t.Errorf("expected collector runner to run 1 time, got %d", collectorRunner.GetRunCount())
	}

	// Verify all checks are finalized
	for _, checkID := range []string{"trigger:check", "materialize:check", "build:app", "test:pkg", "collector:results"} {
		resp, err := env.Orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
			WorkPlanID:    wp.ID,
			CheckIDs:      []string{checkID},
			IncludeChecks: true,
		})
		if err != nil {
			t.Errorf("failed to query check %s: %v", checkID, err)
			continue
		}
		if len(resp.Checks) == 0 {
			t.Errorf("check %s not found", checkID)
			continue
		}
		if resp.Checks[0].State != domain.CheckStateFinal {
			t.Errorf("check %s: expected FINAL, got %s", checkID, resp.Checks[0].State)
		}
	}
}

func TestE2E_DependentStageRunsAfterUpstreamCompletes(t *testing.T) {
	// Note: In TurboCI-Lite, dependencies are "completion" dependencies, not "success" dependencies.
	// A stage with a dependency on another stage will run when that stage completes,
	// regardless of whether it succeeded or failed. This is by design - it allows
	// downstream stages to query upstream results and decide what to do (like skip).

	ctx := context.Background()
	env := NewCITestEnv(t)
	defer env.Stop()

	// Register runners
	builderRunner := env.RegisterMockRunner(ctx, "builder-1", "go_builder", "localhost:50063")
	testerRunner := env.RegisterMockRunner(ctx, "tester-1", "go_tester", "localhost:50064")

	// Make builder fail
	builderRunner.OnRun = func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
		return &service.RunResponse{
			StageState: domain.StageStateFinal,
			CheckUpdates: []*service.CheckUpdate{{
				CheckID:  req.CheckOptions[0].CheckID,
				State:    domain.CheckStateFinal,
				Finalize: true,
				Data:     map[string]any{"success": false, "error": "build failed"},
			}},
			Failure: &domain.Failure{Message: "build failed"},
		}, nil
	}

	env.Start()

	// Create work plan with build -> test dependency
	wp, _ := env.Orchestrator.CreateWorkPlan(ctx, nil)

	syncMode := domain.ExecutionModeSync
	_, err := env.Orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: wp.ID,
		Checks: []*service.CheckWrite{
			{ID: "build:app", State: ptr(domain.CheckStatePlanned), Kind: "build"},
			{ID: "test:pkg", State: ptr(domain.CheckStatePlanned), Kind: "unit_test"},
		},
		Stages: []*service.StageWrite{
			{
				ID:            "build-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "go_builder",
				Assignments:   []domain.Assignment{{TargetCheckID: "build:app", GoalState: domain.CheckStateFinal}},
			},
			{
				ID:            "test-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "go_tester",
				Assignments:   []domain.Assignment{{TargetCheckID: "test:pkg", GoalState: domain.CheckStateFinal}},
				Dependencies: &domain.DependencyGroup{
					Predicate: domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{
						TargetType: domain.NodeTypeStage,
						TargetID:   "build-stage",
					}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for test check to complete (should run after build completes, even if build failed)
	if !env.WaitForCheckState(ctx, wp.ID, "test:pkg", domain.CheckStateFinal, 5*time.Second) {
		t.Fatal("test check did not complete")
	}

	// Both stages should have run - test runs after build completes (regardless of failure)
	if builderRunner.GetRunCount() != 1 {
		t.Errorf("expected builder to run 1 time, got %d", builderRunner.GetRunCount())
	}
	if testerRunner.GetRunCount() != 1 {
		t.Errorf("expected tester to run 1 time (after build completes), got %d", testerRunner.GetRunCount())
	}

	// Verify build check recorded the failure data
	resp, _ := env.Orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    wp.ID,
		CheckIDs:      []string{"build:app"},
		IncludeChecks: true,
	})
	if len(resp.Checks) == 0 || len(resp.Checks[0].Results) == 0 {
		t.Fatal("build check has no results")
	}
	// Check that the data indicates failure
	data := resp.Checks[0].Results[0].Data
	if data == nil || data["success"] != false {
		t.Errorf("expected build check data to indicate failure, got %v", data)
	}
}

func ptr[T any](v T) *T {
	return &v
}
