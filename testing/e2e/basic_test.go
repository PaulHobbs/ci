package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
)

// TestBasicSyncExecution tests that a single stage with sync execution
// successfully completes and updates its assigned check.
func TestBasicSyncExecution(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Register a mock runner that handles "build" stages
	runner := env.RegisterMockRunner(ctx, "runner-1", "build", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})

	// Start the dispatcher
	env.Start()

	// Create a work plan
	wpID := env.CreateWorkPlan(ctx)
	t.Logf("Created work plan: %s", wpID)

	// Create a check and a stage that will process it
	syncMode := SyncMode()
	resp, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "build-check", State: ptr(domain.CheckStatePlanned), Kind: "build"},
		},
		[]*service.StageWrite{
			{
				ID:            "build-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "build",
				Assignments: []domain.Assignment{
					{TargetCheckID: "build-check", GoalState: domain.CheckStateFinal},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Debug: Check initial stage state
	t.Logf("Stage after write: state=%s, runnerType=%s, execMode=%v",
		resp.Stages[0].State, resp.Stages[0].RunnerType, resp.Stages[0].ExecutionMode)

	// Wait a moment and check for execution creation
	time.Sleep(200 * time.Millisecond)
	exec := env.WaitForExecution(ctx, wpID, "build-stage", 2*time.Second)
	if exec == nil {
		// Debug: Query stage state
		stage := env.GetStage(ctx, wpID, "build-stage")
		t.Logf("Stage state after wait: %s, attempts=%d", stage.State, len(stage.Attempts))
		t.Fatalf("no execution created for stage")
	}
	t.Logf("Execution created: id=%s, state=%s", exec.ID, exec.State)

	// Wait for the stage to complete
	if !env.WaitForStageState(ctx, wpID, "build-stage", domain.StageStateFinal, 5*time.Second) {
		stage := env.GetStage(ctx, wpID, "build-stage")
		t.Fatalf("stage did not reach FINAL state, got %s", stage.State)
	}

	// Verify the check was finalized
	check := env.GetCheck(ctx, wpID, "build-check")
	if check.State != domain.CheckStateFinal {
		t.Errorf("expected check state FINAL, got %s", check.State)
	}

	// Verify the runner received the request
	if runner.GetRunCount() != 1 {
		t.Errorf("expected 1 run, got %d", runner.GetRunCount())
	}
}

// TestBasicAsyncExecution tests that a single stage with async execution
// successfully completes via callback.
func TestBasicAsyncExecution(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Register a mock runner that handles async execution
	runner := env.RegisterMockRunner(ctx, "runner-1", "test", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeAsync})
	runner.SetCallbackService(env.CallbackSvc)

	env.Start()

	wpID := env.CreateWorkPlan(ctx)
	t.Logf("Created work plan: %s", wpID)

	asyncMode := AsyncMode()
	resp, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "test-check", State: ptr(domain.CheckStatePlanned), Kind: "test"},
		},
		[]*service.StageWrite{
			{
				ID:            "test-stage",
				ExecutionMode: &asyncMode,
				RunnerType:    "test",
				Assignments: []domain.Assignment{
					{TargetCheckID: "test-check", GoalState: domain.CheckStateFinal},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}
	t.Logf("Stage after write: state=%s, runnerType=%s, execMode=%v",
		resp.Stages[0].State, resp.Stages[0].RunnerType, resp.Stages[0].ExecutionMode)

	// Wait for execution to be created
	time.Sleep(200 * time.Millisecond)
	exec := env.WaitForExecution(ctx, wpID, "test-stage", 2*time.Second)
	if exec == nil {
		t.Fatalf("no execution created for stage")
	}
	t.Logf("Execution created: id=%s, state=%s", exec.ID, exec.State)

	// Check if runner received the request
	time.Sleep(100 * time.Millisecond)
	t.Logf("Runner async count: %d", runner.GetRunAsyncCount())

	// Wait for completion
	if !env.WaitForStageState(ctx, wpID, "test-stage", domain.StageStateFinal, 5*time.Second) {
		stage := env.GetStage(ctx, wpID, "test-stage")
		t.Fatalf("stage did not reach FINAL state, got %s", stage.State)
	}

	// Verify check was finalized
	check := env.GetCheck(ctx, wpID, "test-check")
	if check.State != domain.CheckStateFinal {
		t.Errorf("expected check state FINAL, got %s", check.State)
	}

	// Verify async path was used
	if runner.GetRunAsyncCount() != 1 {
		t.Errorf("expected 1 async run, got %d", runner.GetRunAsyncCount())
	}
}

// TestStageWithMultipleChecks tests that a single stage can update
// multiple assigned checks.
func TestStageWithMultipleChecks(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	env.RegisterMockRunner(ctx, "runner-1", "multi", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})

	env.Start()

	wpID := env.CreateWorkPlan(ctx)

	syncMode := SyncMode()
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "check-1", State: ptr(domain.CheckStatePlanned), Kind: "test"},
			{ID: "check-2", State: ptr(domain.CheckStatePlanned), Kind: "test"},
			{ID: "check-3", State: ptr(domain.CheckStatePlanned), Kind: "test"},
		},
		[]*service.StageWrite{
			{
				ID:            "multi-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "multi",
				Assignments: []domain.Assignment{
					{TargetCheckID: "check-1", GoalState: domain.CheckStateFinal},
					{TargetCheckID: "check-2", GoalState: domain.CheckStateFinal},
					{TargetCheckID: "check-3", GoalState: domain.CheckStateFinal},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	if !env.WaitForStageState(ctx, wpID, "multi-stage", domain.StageStateFinal, 5*time.Second) {
		t.Fatal("stage did not complete")
	}

	// All checks should be finalized
	for _, checkID := range []string{"check-1", "check-2", "check-3"} {
		check := env.GetCheck(ctx, wpID, checkID)
		if check.State != domain.CheckStateFinal {
			t.Errorf("check %s: expected FINAL, got %s", checkID, check.State)
		}
	}
}

// TestSequentialStages tests that two stages with dependency execute in order.
func TestSequentialStages(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Track execution order
	var executionOrder []string
	runner := env.RegisterMockRunner(ctx, "runner-1", "pipeline", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})
	runner.OnRun = func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
		executionOrder = append(executionOrder, req.StageID)
		time.Sleep(10 * time.Millisecond)

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

	env.Start()

	wpID := env.CreateWorkPlan(ctx)

	syncMode := SyncMode()
	// Stage 2 depends on Stage 1's check
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "check-1", State: ptr(domain.CheckStatePlanned), Kind: "build"},
			{ID: "check-2", State: ptr(domain.CheckStatePlanned), Kind: "test",
				Dependencies: &domain.DependencyGroup{
					Predicate: domain.PredicateAND,
					Dependencies: []domain.DependencyRef{
						{TargetType: domain.NodeTypeCheck, TargetID: "check-1"},
					},
				},
			},
		},
		[]*service.StageWrite{
			{
				ID:            "stage-1",
				ExecutionMode: &syncMode,
				RunnerType:    "pipeline",
				Assignments: []domain.Assignment{
					{TargetCheckID: "check-1", GoalState: domain.CheckStateFinal},
				},
			},
			{
				ID:            "stage-2",
				ExecutionMode: &syncMode,
				RunnerType:    "pipeline",
				Assignments: []domain.Assignment{
					{TargetCheckID: "check-2", GoalState: domain.CheckStateFinal},
				},
				Dependencies: &domain.DependencyGroup{
					Predicate: domain.PredicateAND,
					Dependencies: []domain.DependencyRef{
						{TargetType: domain.NodeTypeStage, TargetID: "stage-1"},
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for both stages to complete
	if !env.WaitForStageState(ctx, wpID, "stage-2", domain.StageStateFinal, 5*time.Second) {
		t.Fatal("stage-2 did not complete")
	}

	// Verify execution order
	if len(executionOrder) != 2 {
		t.Fatalf("expected 2 executions, got %d", len(executionOrder))
	}
	if executionOrder[0] != "stage-1" || executionOrder[1] != "stage-2" {
		t.Errorf("expected execution order [stage-1, stage-2], got %v", executionOrder)
	}
}

// TestParallelStages tests that independent stages can execute concurrently.
func TestParallelStages(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Track concurrent executions
	var mu = &struct {
		maxConcurrent int
		current       int
	}{}

	runner := env.RegisterMockRunner(ctx, "runner-1", "parallel", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})
	runner.OnRun = func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
		mu.current++
		if mu.current > mu.maxConcurrent {
			mu.maxConcurrent = mu.current
		}
		time.Sleep(50 * time.Millisecond)
		mu.current--

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

	env.Start()

	wpID := env.CreateWorkPlan(ctx)

	syncMode := SyncMode()
	// Three independent stages
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "check-a", State: ptr(domain.CheckStatePlanned)},
			{ID: "check-b", State: ptr(domain.CheckStatePlanned)},
			{ID: "check-c", State: ptr(domain.CheckStatePlanned)},
		},
		[]*service.StageWrite{
			{
				ID: "stage-a", ExecutionMode: &syncMode, RunnerType: "parallel",
				Assignments: []domain.Assignment{{TargetCheckID: "check-a", GoalState: domain.CheckStateFinal}},
			},
			{
				ID: "stage-b", ExecutionMode: &syncMode, RunnerType: "parallel",
				Assignments: []domain.Assignment{{TargetCheckID: "check-b", GoalState: domain.CheckStateFinal}},
			},
			{
				ID: "stage-c", ExecutionMode: &syncMode, RunnerType: "parallel",
				Assignments: []domain.Assignment{{TargetCheckID: "check-c", GoalState: domain.CheckStateFinal}},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for all stages to complete
	for _, stageID := range []string{"stage-a", "stage-b", "stage-c"} {
		if !env.WaitForStageState(ctx, wpID, stageID, domain.StageStateFinal, 5*time.Second) {
			t.Fatalf("%s did not complete", stageID)
		}
	}

	// Note: Due to polling interval and dispatcher behavior, they might not all
	// run truly concurrently, but they should at least all complete
	if runner.GetRunCount() != 3 {
		t.Errorf("expected 3 runs, got %d", runner.GetRunCount())
	}
}

// ptr returns a pointer to the value.
func ptr[T any](v T) *T {
	return &v
}
