package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
)

// TestDiamondDependency tests the A→B, A→C, B→D, C→D pattern.
// Stage A runs first, then B and C in parallel, then D runs after both complete.
func TestDiamondDependency(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	var executionOrder []string
	runner := env.RegisterMockRunner(ctx, "runner-1", "diamond", "localhost:50052",
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
	// Create diamond structure: A → B, C → D
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "check-a", State: ptr(domain.CheckStatePlanned)},
			{ID: "check-b", State: ptr(domain.CheckStatePlanned),
				Dependencies: &domain.DependencyGroup{
					Predicate:    domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeCheck, TargetID: "check-a"}},
				}},
			{ID: "check-c", State: ptr(domain.CheckStatePlanned),
				Dependencies: &domain.DependencyGroup{
					Predicate:    domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeCheck, TargetID: "check-a"}},
				}},
			{ID: "check-d", State: ptr(domain.CheckStatePlanned),
				Dependencies: &domain.DependencyGroup{
					Predicate: domain.PredicateAND,
					Dependencies: []domain.DependencyRef{
						{TargetType: domain.NodeTypeCheck, TargetID: "check-b"},
						{TargetType: domain.NodeTypeCheck, TargetID: "check-c"},
					},
				}},
		},
		[]*service.StageWrite{
			{ID: "stage-a", ExecutionMode: &syncMode, RunnerType: "diamond",
				Assignments: []domain.Assignment{{TargetCheckID: "check-a", GoalState: domain.CheckStateFinal}}},
			{ID: "stage-b", ExecutionMode: &syncMode, RunnerType: "diamond",
				Assignments: []domain.Assignment{{TargetCheckID: "check-b", GoalState: domain.CheckStateFinal}},
				Dependencies: &domain.DependencyGroup{
					Predicate:    domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeStage, TargetID: "stage-a"}},
				}},
			{ID: "stage-c", ExecutionMode: &syncMode, RunnerType: "diamond",
				Assignments: []domain.Assignment{{TargetCheckID: "check-c", GoalState: domain.CheckStateFinal}},
				Dependencies: &domain.DependencyGroup{
					Predicate:    domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeStage, TargetID: "stage-a"}},
				}},
			{ID: "stage-d", ExecutionMode: &syncMode, RunnerType: "diamond",
				Assignments: []domain.Assignment{{TargetCheckID: "check-d", GoalState: domain.CheckStateFinal}},
				Dependencies: &domain.DependencyGroup{
					Predicate: domain.PredicateAND,
					Dependencies: []domain.DependencyRef{
						{TargetType: domain.NodeTypeStage, TargetID: "stage-b"},
						{TargetType: domain.NodeTypeStage, TargetID: "stage-c"},
					},
				}},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for stage-d to complete
	if !env.WaitForStageState(ctx, wpID, "stage-d", domain.StageStateFinal, 10*time.Second) {
		t.Fatal("stage-d did not complete")
	}

	// Verify execution order: A must be first, D must be last
	if len(executionOrder) != 4 {
		t.Fatalf("expected 4 executions, got %d: %v", len(executionOrder), executionOrder)
	}
	if executionOrder[0] != "stage-a" {
		t.Errorf("expected stage-a first, got %s", executionOrder[0])
	}
	if executionOrder[3] != "stage-d" {
		t.Errorf("expected stage-d last, got %s", executionOrder[3])
	}
	// B and C should be in the middle (order between them doesn't matter)
	middle := executionOrder[1:3]
	hasB := middle[0] == "stage-b" || middle[1] == "stage-b"
	hasC := middle[0] == "stage-c" || middle[1] == "stage-c"
	if !hasB || !hasC {
		t.Errorf("expected B and C in middle, got %v", middle)
	}
}

// TestLongChain tests a sequential chain of 5 stages.
func TestLongChain(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	var executionOrder []string
	runner := env.RegisterMockRunner(ctx, "runner-1", "chain", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})
	runner.OnRun = func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
		executionOrder = append(executionOrder, req.StageID)

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
	numStages := 5

	// Build checks and stages for the chain
	var checks []*service.CheckWrite
	var stages []*service.StageWrite

	for i := 1; i <= numStages; i++ {
		checkID := "check-" + string(rune('0'+i))
		stageID := "stage-" + string(rune('0'+i))

		checkWrite := &service.CheckWrite{
			ID:    checkID,
			State: ptr(domain.CheckStatePlanned),
		}
		if i > 1 {
			prevCheckID := "check-" + string(rune('0'+i-1))
			checkWrite.Dependencies = &domain.DependencyGroup{
				Predicate:    domain.PredicateAND,
				Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeCheck, TargetID: prevCheckID}},
			}
		}
		checks = append(checks, checkWrite)

		stageWrite := &service.StageWrite{
			ID:            stageID,
			ExecutionMode: &syncMode,
			RunnerType:    "chain",
			Assignments:   []domain.Assignment{{TargetCheckID: checkID, GoalState: domain.CheckStateFinal}},
		}
		if i > 1 {
			prevStageID := "stage-" + string(rune('0'+i-1))
			stageWrite.Dependencies = &domain.DependencyGroup{
				Predicate:    domain.PredicateAND,
				Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeStage, TargetID: prevStageID}},
			}
		}
		stages = append(stages, stageWrite)
	}

	_, err := env.WriteNodes(ctx, wpID, checks, stages)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for last stage
	lastStageID := "stage-" + string(rune('0'+numStages))
	if !env.WaitForStageState(ctx, wpID, lastStageID, domain.StageStateFinal, 10*time.Second) {
		t.Fatalf("%s did not complete", lastStageID)
	}

	// Verify all stages ran in order
	if len(executionOrder) != numStages {
		t.Fatalf("expected %d executions, got %d", numStages, len(executionOrder))
	}
	for i := 0; i < numStages; i++ {
		expected := "stage-" + string(rune('0'+i+1))
		if executionOrder[i] != expected {
			t.Errorf("position %d: expected %s, got %s", i, expected, executionOrder[i])
		}
	}
}

// TestFanOutFanIn tests one stage fanning out to many, then converging.
func TestFanOutFanIn(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	var executionOrder []string
	runner := env.RegisterMockRunner(ctx, "runner-1", "fanout", "localhost:50052",
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
	fanWidth := 4

	// Create fan-out structure: source → [fan-1, fan-2, fan-3, fan-4] → sink
	checks := []*service.CheckWrite{
		{ID: "check-source", State: ptr(domain.CheckStatePlanned)},
	}
	stages := []*service.StageWrite{
		{ID: "stage-source", ExecutionMode: &syncMode, RunnerType: "fanout",
			Assignments: []domain.Assignment{{TargetCheckID: "check-source", GoalState: domain.CheckStateFinal}}},
	}

	// Fan-out stages
	var fanDeps []domain.DependencyRef
	for i := 1; i <= fanWidth; i++ {
		checkID := "check-fan-" + string(rune('0'+i))
		stageID := "stage-fan-" + string(rune('0'+i))

		checks = append(checks, &service.CheckWrite{
			ID:    checkID,
			State: ptr(domain.CheckStatePlanned),
			Dependencies: &domain.DependencyGroup{
				Predicate:    domain.PredicateAND,
				Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeCheck, TargetID: "check-source"}},
			},
		})
		stages = append(stages, &service.StageWrite{
			ID:            stageID,
			ExecutionMode: &syncMode,
			RunnerType:    "fanout",
			Assignments:   []domain.Assignment{{TargetCheckID: checkID, GoalState: domain.CheckStateFinal}},
			Dependencies: &domain.DependencyGroup{
				Predicate:    domain.PredicateAND,
				Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeStage, TargetID: "stage-source"}},
			},
		})
		fanDeps = append(fanDeps, domain.DependencyRef{TargetType: domain.NodeTypeStage, TargetID: stageID})
	}

	// Sink stage (depends on all fan stages)
	var checkFanDeps []domain.DependencyRef
	for i := 1; i <= fanWidth; i++ {
		checkFanDeps = append(checkFanDeps, domain.DependencyRef{
			TargetType: domain.NodeTypeCheck,
			TargetID:   "check-fan-" + string(rune('0'+i)),
		})
	}
	checks = append(checks, &service.CheckWrite{
		ID:    "check-sink",
		State: ptr(domain.CheckStatePlanned),
		Dependencies: &domain.DependencyGroup{
			Predicate:    domain.PredicateAND,
			Dependencies: checkFanDeps,
		},
	})
	stages = append(stages, &service.StageWrite{
		ID:            "stage-sink",
		ExecutionMode: &syncMode,
		RunnerType:    "fanout",
		Assignments:   []domain.Assignment{{TargetCheckID: "check-sink", GoalState: domain.CheckStateFinal}},
		Dependencies: &domain.DependencyGroup{
			Predicate:    domain.PredicateAND,
			Dependencies: fanDeps,
		},
	})

	_, err := env.WriteNodes(ctx, wpID, checks, stages)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for sink to complete
	if !env.WaitForStageState(ctx, wpID, "stage-sink", domain.StageStateFinal, 10*time.Second) {
		t.Fatal("stage-sink did not complete")
	}

	// Verify execution order
	expectedTotal := 1 + fanWidth + 1 // source + fan stages + sink
	if len(executionOrder) != expectedTotal {
		t.Fatalf("expected %d executions, got %d: %v", expectedTotal, len(executionOrder), executionOrder)
	}
	if executionOrder[0] != "stage-source" {
		t.Errorf("expected source first, got %s", executionOrder[0])
	}
	if executionOrder[len(executionOrder)-1] != "stage-sink" {
		t.Errorf("expected sink last, got %s", executionOrder[len(executionOrder)-1])
	}
}

// TestCIStylePipeline tests a realistic Plan→Build→Test→Deploy pattern.
func TestCIStylePipeline(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	var executionOrder []string
	runner := env.RegisterMockRunner(ctx, "runner-1", "ci", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})
	runner.OnRun = func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
		executionOrder = append(executionOrder, req.StageID)

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
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "plan-check", State: ptr(domain.CheckStatePlanned), Kind: "plan"},
			{ID: "build-check", State: ptr(domain.CheckStatePlanned), Kind: "build",
				Dependencies: &domain.DependencyGroup{
					Predicate:    domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeCheck, TargetID: "plan-check"}},
				}},
			{ID: "test-check", State: ptr(domain.CheckStatePlanned), Kind: "test",
				Dependencies: &domain.DependencyGroup{
					Predicate:    domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeCheck, TargetID: "build-check"}},
				}},
			{ID: "deploy-check", State: ptr(domain.CheckStatePlanned), Kind: "deploy",
				Dependencies: &domain.DependencyGroup{
					Predicate:    domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeCheck, TargetID: "test-check"}},
				}},
		},
		[]*service.StageWrite{
			{ID: "plan-stage", ExecutionMode: &syncMode, RunnerType: "ci",
				Assignments: []domain.Assignment{{TargetCheckID: "plan-check", GoalState: domain.CheckStateFinal}}},
			{ID: "build-stage", ExecutionMode: &syncMode, RunnerType: "ci",
				Assignments: []domain.Assignment{{TargetCheckID: "build-check", GoalState: domain.CheckStateFinal}},
				Dependencies: &domain.DependencyGroup{
					Predicate:    domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeStage, TargetID: "plan-stage"}},
				}},
			{ID: "test-stage", ExecutionMode: &syncMode, RunnerType: "ci",
				Assignments: []domain.Assignment{{TargetCheckID: "test-check", GoalState: domain.CheckStateFinal}},
				Dependencies: &domain.DependencyGroup{
					Predicate:    domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeStage, TargetID: "build-stage"}},
				}},
			{ID: "deploy-stage", ExecutionMode: &syncMode, RunnerType: "ci",
				Assignments: []domain.Assignment{{TargetCheckID: "deploy-check", GoalState: domain.CheckStateFinal}},
				Dependencies: &domain.DependencyGroup{
					Predicate:    domain.PredicateAND,
					Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeStage, TargetID: "test-stage"}},
				}},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for deploy to complete
	if !env.WaitForStageState(ctx, wpID, "deploy-stage", domain.StageStateFinal, 10*time.Second) {
		t.Fatal("deploy-stage did not complete")
	}

	// Verify execution order
	expected := []string{"plan-stage", "build-stage", "test-stage", "deploy-stage"}
	if len(executionOrder) != len(expected) {
		t.Fatalf("expected %d executions, got %d", len(expected), len(executionOrder))
	}
	for i, exp := range expected {
		if executionOrder[i] != exp {
			t.Errorf("position %d: expected %s, got %s", i, exp, executionOrder[i])
		}
	}

	// Verify all checks are finalized
	for _, checkID := range []string{"plan-check", "build-check", "test-check", "deploy-check"} {
		check := env.GetCheck(ctx, wpID, checkID)
		if check.State != domain.CheckStateFinal {
			t.Errorf("%s: expected FINAL, got %s", checkID, check.State)
		}
	}
}

// TestORDependency tests that stages with OR dependencies run when any dependency is satisfied.
func TestORDependency(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	var executionOrder []string
	runner := env.RegisterMockRunner(ctx, "runner-1", "or-test", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})
	runner.OnRun = func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
		executionOrder = append(executionOrder, req.StageID)

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
	// Create structure: A, B (independent) → C (depends on A OR B)
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "check-a", State: ptr(domain.CheckStatePlanned)},
			{ID: "check-b", State: ptr(domain.CheckStatePlanned)},
			{ID: "check-c", State: ptr(domain.CheckStatePlanned),
				Dependencies: &domain.DependencyGroup{
					Predicate: domain.PredicateOR,
					Dependencies: []domain.DependencyRef{
						{TargetType: domain.NodeTypeCheck, TargetID: "check-a"},
						{TargetType: domain.NodeTypeCheck, TargetID: "check-b"},
					},
				}},
		},
		[]*service.StageWrite{
			{ID: "stage-a", ExecutionMode: &syncMode, RunnerType: "or-test",
				Assignments: []domain.Assignment{{TargetCheckID: "check-a", GoalState: domain.CheckStateFinal}}},
			{ID: "stage-b", ExecutionMode: &syncMode, RunnerType: "or-test",
				Assignments: []domain.Assignment{{TargetCheckID: "check-b", GoalState: domain.CheckStateFinal}}},
			{ID: "stage-c", ExecutionMode: &syncMode, RunnerType: "or-test",
				Assignments: []domain.Assignment{{TargetCheckID: "check-c", GoalState: domain.CheckStateFinal}},
				Dependencies: &domain.DependencyGroup{
					Predicate: domain.PredicateOR,
					Dependencies: []domain.DependencyRef{
						{TargetType: domain.NodeTypeStage, TargetID: "stage-a"},
						{TargetType: domain.NodeTypeStage, TargetID: "stage-b"},
					},
				}},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for stage-c to complete
	if !env.WaitForStageState(ctx, wpID, "stage-c", domain.StageStateFinal, 10*time.Second) {
		t.Fatal("stage-c did not complete")
	}

	// C should have run after at least one of A or B
	if len(executionOrder) < 2 {
		t.Fatalf("expected at least 2 executions, got %d", len(executionOrder))
	}
	// Stage C should be in the list
	foundC := false
	for _, s := range executionOrder {
		if s == "stage-c" {
			foundC = true
			break
		}
	}
	if !foundC {
		t.Error("stage-c was not executed")
	}
}
