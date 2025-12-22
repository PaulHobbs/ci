package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
)

// TestSyncExecutionFailure tests that sync execution failure is properly recorded.
func TestSyncExecutionFailure(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	runner := env.RegisterMockRunner(ctx, "runner-1", "failing", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})

	// Configure runner to fail
	runner.OnRun = func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
		return nil, errors.New("simulated runner failure")
	}

	env.Start()
	wpID := env.CreateWorkPlan(ctx)

	syncMode := SyncMode()
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "fail-check", State: ptr(domain.CheckStatePlanned)},
		},
		[]*service.StageWrite{
			{
				ID:            "fail-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "failing",
				Assignments: []domain.Assignment{
					{TargetCheckID: "fail-check", GoalState: domain.CheckStateFinal},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for execution to be created and potentially fail
	time.Sleep(500 * time.Millisecond)

	// Stage should still be ATTEMPTING (not FINAL) since execution failed
	stage := env.GetStage(ctx, wpID, "fail-stage")
	if stage.State == domain.StageStateFinal {
		t.Error("stage should not be FINAL after runner failure")
	}

	// Check should not be finalized
	check := env.GetCheck(ctx, wpID, "fail-check")
	if check.State == domain.CheckStateFinal {
		t.Error("check should not be FINAL after runner failure")
	}
}

// TestStageWithFailureResponse tests that runner returning failure is handled.
func TestStageWithFailureResponse(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	runner := env.RegisterMockRunner(ctx, "runner-1", "failing", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})

	// Configure runner to return failure response
	runner.OnRun = func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
		return &service.RunResponse{
			StageState: domain.StageStateFinal, // Stage still completes
			Failure: &domain.Failure{
				Message: "build failed: exit code 1",
			},
			// No check updates - check remains unfinalized
		}, nil
	}

	env.Start()
	wpID := env.CreateWorkPlan(ctx)

	syncMode := SyncMode()
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "fail-check", State: ptr(domain.CheckStatePlanned)},
		},
		[]*service.StageWrite{
			{
				ID:            "fail-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "failing",
				Assignments: []domain.Assignment{
					{TargetCheckID: "fail-check", GoalState: domain.CheckStateFinal},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for stage to complete (even with failure)
	if !env.WaitForStageState(ctx, wpID, "fail-stage", domain.StageStateFinal, 5*time.Second) {
		t.Fatal("stage should reach FINAL even with failure response")
	}

	// Get execution and verify it recorded the failure
	exec := env.WaitForExecution(ctx, wpID, "fail-stage", 1*time.Second)
	if exec == nil {
		t.Fatal("execution should exist")
	}
	// Execution should be marked as failed
	if exec.State != domain.ExecutionStateFailed {
		t.Errorf("execution state: expected FAILED, got %s", exec.State)
	}
}

// TestNoRunnerAvailable tests that executions stay pending when no runners are available.
func TestNoRunnerAvailable(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Start dispatcher but DON'T register any runners
	env.Start()

	wpID := env.CreateWorkPlan(ctx)

	syncMode := SyncMode()
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "orphan-check", State: ptr(domain.CheckStatePlanned)},
		},
		[]*service.StageWrite{
			{
				ID:            "orphan-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "nonexistent",
				Assignments: []domain.Assignment{
					{TargetCheckID: "orphan-check", GoalState: domain.CheckStateFinal},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait a bit for dispatcher to poll
	time.Sleep(200 * time.Millisecond)

	// Stage should still be ATTEMPTING (execution created but not dispatched)
	stage := env.GetStage(ctx, wpID, "orphan-stage")
	if stage.State == domain.StageStateFinal {
		t.Error("stage should not be FINAL without runner")
	}

	// Execution should exist but be PENDING
	exec := env.WaitForExecution(ctx, wpID, "orphan-stage", 1*time.Second)
	if exec != nil && exec.State != domain.ExecutionStatePending {
		t.Errorf("execution should be PENDING, got %s", exec.State)
	}
}

// TestRunnerBecomesAvailable tests that pending execution runs when runner registers.
func TestRunnerBecomesAvailable(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	env.Start()
	wpID := env.CreateWorkPlan(ctx)

	syncMode := SyncMode()
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "delayed-check", State: ptr(domain.CheckStatePlanned)},
		},
		[]*service.StageWrite{
			{
				ID:            "delayed-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "delayed",
				Assignments: []domain.Assignment{
					{TargetCheckID: "delayed-check", GoalState: domain.CheckStateFinal},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait a bit - stage should not complete yet (no runner)
	time.Sleep(200 * time.Millisecond)
	stage := env.GetStage(ctx, wpID, "delayed-stage")
	if stage.State == domain.StageStateFinal {
		t.Fatal("stage completed before runner was registered")
	}

	// Now register a runner
	env.RegisterMockRunner(ctx, "runner-1", "delayed", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})

	// Wait for stage to complete
	if !env.WaitForStageState(ctx, wpID, "delayed-stage", domain.StageStateFinal, 5*time.Second) {
		t.Fatal("stage did not complete after runner became available")
	}

	// Check should also be finalized
	check := env.GetCheck(ctx, wpID, "delayed-check")
	if check.State != domain.CheckStateFinal {
		t.Errorf("check should be FINAL, got %s", check.State)
	}
}

// TestPartialWorkflowFailure tests that some stages can fail while others complete.
func TestPartialWorkflowFailure(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	runner := env.RegisterMockRunner(ctx, "runner-1", "partial", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})

	// Configure runner to fail only for specific stage
	runner.OnRun = func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
		if req.StageID == "fail-stage" {
			return nil, errors.New("simulated failure for fail-stage")
		}

		// Success for other stages
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
	// Two independent stages - one will fail, one will succeed
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "success-check", State: ptr(domain.CheckStatePlanned)},
			{ID: "fail-check", State: ptr(domain.CheckStatePlanned)},
		},
		[]*service.StageWrite{
			{
				ID:            "success-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "partial",
				Assignments: []domain.Assignment{
					{TargetCheckID: "success-check", GoalState: domain.CheckStateFinal},
				},
			},
			{
				ID:            "fail-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "partial",
				Assignments: []domain.Assignment{
					{TargetCheckID: "fail-check", GoalState: domain.CheckStateFinal},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for success stage to complete
	if !env.WaitForStageState(ctx, wpID, "success-stage", domain.StageStateFinal, 5*time.Second) {
		t.Fatal("success-stage should complete")
	}

	// Success check should be finalized
	successCheck := env.GetCheck(ctx, wpID, "success-check")
	if successCheck.State != domain.CheckStateFinal {
		t.Errorf("success-check: expected FINAL, got %s", successCheck.State)
	}

	// Fail stage should not be FINAL
	failStage := env.GetStage(ctx, wpID, "fail-stage")
	if failStage.State == domain.StageStateFinal {
		t.Error("fail-stage should not be FINAL after runner error")
	}
}

// TestCallbackForUnknownExecution tests that callbacks for unknown executions are rejected.
func TestCallbackForUnknownExecution(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Try to update an execution that doesn't exist
	err := env.CallbackSvc.UpdateExecution(ctx, &service.UpdateExecutionRequest{
		ExecutionID:     "nonexistent-execution-id",
		ProgressPercent: 50,
		ProgressMessage: "should fail",
	})

	if err == nil {
		t.Error("expected error for unknown execution ID")
	}

	// Try to complete an execution that doesn't exist
	err = env.CallbackSvc.CompleteExecution(ctx, &service.CompleteExecutionRequest{
		ExecutionID: "nonexistent-execution-id",
		Success:     true,
	})

	if err == nil {
		t.Error("expected error for unknown execution ID")
	}
}

// TestInvalidArgumentErrors tests validation of service inputs.
func TestInvalidArgumentErrors(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Empty runner ID should fail
	_, err := env.RunnerSvc.RegisterRunner(ctx, &service.RegisterRunnerRequest{
		RunnerID:       "",
		RunnerType:     "test",
		Address:        "localhost:50052",
		SupportedModes: []domain.ExecutionMode{domain.ExecutionModeSync},
	})
	if err == nil {
		t.Error("expected error for empty runner ID")
	}

	// Empty runner type should fail
	_, err = env.RunnerSvc.RegisterRunner(ctx, &service.RegisterRunnerRequest{
		RunnerID:       "test",
		RunnerType:     "",
		Address:        "localhost:50052",
		SupportedModes: []domain.ExecutionMode{domain.ExecutionModeSync},
	})
	if err == nil {
		t.Error("expected error for empty runner type")
	}

	// Empty address should fail
	_, err = env.RunnerSvc.RegisterRunner(ctx, &service.RegisterRunnerRequest{
		RunnerID:       "test",
		RunnerType:     "test",
		Address:        "",
		SupportedModes: []domain.ExecutionMode{domain.ExecutionModeSync},
	})
	if err == nil {
		t.Error("expected error for empty address")
	}

	// Empty execution ID for callback should fail
	err = env.CallbackSvc.UpdateExecution(ctx, &service.UpdateExecutionRequest{
		ExecutionID: "",
	})
	if err == nil {
		t.Error("expected error for empty execution ID")
	}
}
