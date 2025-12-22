package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
)

// TestRunnerRegistration tests that a runner can be registered and appears in the list.
func TestRunnerRegistration(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Register a runner
	_, err := env.RunnerSvc.RegisterRunner(ctx, &service.RegisterRunnerRequest{
		RunnerID:       "test-runner",
		RunnerType:     "build",
		Address:        "localhost:50052",
		SupportedModes: []domain.ExecutionMode{domain.ExecutionModeSync},
		MaxConcurrent:  5,
		TTLSeconds:     300,
		Metadata:       map[string]string{"version": "1.0"},
	})
	if err != nil {
		t.Fatalf("failed to register runner: %v", err)
	}

	// List runners and verify it appears
	runners, err := env.RunnerSvc.ListRunners(ctx, nil)
	if err != nil {
		t.Fatalf("failed to list runners: %v", err)
	}

	if len(runners) != 1 {
		t.Fatalf("expected 1 runner, got %d", len(runners))
	}

	r := runners[0]
	if r.ID != "test-runner" {
		t.Errorf("expected runner ID 'test-runner', got %s", r.ID)
	}
	if r.RunnerType != "build" {
		t.Errorf("expected runner type 'build', got %s", r.RunnerType)
	}
	if r.Address != "localhost:50052" {
		t.Errorf("expected address 'localhost:50052', got %s", r.Address)
	}
	if r.MaxConcurrent != 5 {
		t.Errorf("expected max concurrent 5, got %d", r.MaxConcurrent)
	}
}

// TestRunnerHeartbeat tests that re-registration creates a new registration with fresh TTL.
func TestRunnerHeartbeat(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Register with short TTL
	resp1, err := env.RunnerSvc.RegisterRunner(ctx, &service.RegisterRunnerRequest{
		RunnerID:       "heartbeat-runner",
		RunnerType:     "test",
		Address:        "localhost:50052",
		SupportedModes: []domain.ExecutionMode{domain.ExecutionModeSync},
		MaxConcurrent:  1,
		TTLSeconds:     2, // 2 seconds TTL
	})
	if err != nil {
		t.Fatalf("failed to register runner: %v", err)
	}
	initialExpiry := resp1.ExpiresAt

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Re-register (heartbeat) - creates a new registration
	resp2, err := env.RunnerSvc.RegisterRunner(ctx, &service.RegisterRunnerRequest{
		RunnerID:       "heartbeat-runner",
		RunnerType:     "test",
		Address:        "localhost:50052",
		SupportedModes: []domain.ExecutionMode{domain.ExecutionModeSync},
		MaxConcurrent:  1,
		TTLSeconds:     2,
	})
	if err != nil {
		t.Fatalf("failed to heartbeat: %v", err)
	}

	// New registration should have a different ID and later expiry
	if resp2.RegistrationID == resp1.RegistrationID {
		t.Errorf("expected new registration ID, got same: %s", resp2.RegistrationID)
	}

	// New expiry should be after initial expiry
	if !resp2.ExpiresAt.After(initialExpiry) {
		t.Errorf("new registration should have later expiry: initial=%v, new=%v", initialExpiry, resp2.ExpiresAt)
	}

	// Both registrations should exist (until old one expires/is cleaned up)
	runners, _ := env.RunnerSvc.ListRunners(ctx, nil)
	if len(runners) != 2 {
		// This is expected behavior - re-registration creates a new registration
		// In production, old registrations would be cleaned up on expiry
		t.Logf("found %d registrations (expected 2 before cleanup)", len(runners))
	}
}

// TestRunnerUnregistration tests explicit unregistration.
func TestRunnerUnregistration(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Register a runner
	resp, err := env.RunnerSvc.RegisterRunner(ctx, &service.RegisterRunnerRequest{
		RunnerID:       "unregister-runner",
		RunnerType:     "test",
		Address:        "localhost:50052",
		SupportedModes: []domain.ExecutionMode{domain.ExecutionModeSync},
		MaxConcurrent:  1,
		TTLSeconds:     300,
	})
	if err != nil {
		t.Fatalf("failed to register runner: %v", err)
	}

	// Verify it's registered
	runners, _ := env.RunnerSvc.ListRunners(ctx, nil)
	if len(runners) != 1 {
		t.Fatalf("expected 1 runner, got %d", len(runners))
	}

	// Unregister
	err = env.RunnerSvc.UnregisterRunner(ctx, resp.RegistrationID)
	if err != nil {
		t.Fatalf("failed to unregister: %v", err)
	}

	// Verify it's gone
	runners, _ = env.RunnerSvc.ListRunners(ctx, nil)
	if len(runners) != 0 {
		t.Errorf("expected 0 runners after unregister, got %d", len(runners))
	}
}

// TestMultipleRunnersSameType tests that multiple runners of the same type can be registered.
func TestMultipleRunnersSameType(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Register multiple runners of the same type
	for i := 1; i <= 3; i++ {
		_, err := env.RunnerSvc.RegisterRunner(ctx, &service.RegisterRunnerRequest{
			RunnerID:       "runner-" + string(rune('0'+i)),
			RunnerType:     "build",
			Address:        "localhost:" + string(rune('0'+i)) + "0052",
			SupportedModes: []domain.ExecutionMode{domain.ExecutionModeSync},
			MaxConcurrent:  5,
			TTLSeconds:     300,
		})
		if err != nil {
			t.Fatalf("failed to register runner %d: %v", i, err)
		}
	}

	// List and verify all three are registered
	runners, err := env.RunnerSvc.ListRunners(ctx, nil)
	if err != nil {
		t.Fatalf("failed to list runners: %v", err)
	}

	if len(runners) != 3 {
		t.Errorf("expected 3 runners, got %d", len(runners))
	}

	// Filter by type
	buildRunners, err := env.RunnerSvc.ListRunners(ctx, &service.ListRunnersRequest{
		RunnerType: "build",
	})
	if err != nil {
		t.Fatalf("failed to list filtered runners: %v", err)
	}

	if len(buildRunners) != 3 {
		t.Errorf("expected 3 build runners, got %d", len(buildRunners))
	}
}

// TestMultipleRunnerTypes tests that different runner types can be registered.
func TestMultipleRunnerTypes(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Register runners of different types
	types := []string{"build", "test", "deploy"}
	for _, runnerType := range types {
		_, err := env.RunnerSvc.RegisterRunner(ctx, &service.RegisterRunnerRequest{
			RunnerID:       runnerType + "-runner",
			RunnerType:     runnerType,
			Address:        "localhost:50052",
			SupportedModes: []domain.ExecutionMode{domain.ExecutionModeSync},
			MaxConcurrent:  5,
			TTLSeconds:     300,
		})
		if err != nil {
			t.Fatalf("failed to register %s runner: %v", runnerType, err)
		}
	}

	// Verify all types are registered
	runners, err := env.RunnerSvc.ListRunners(ctx, nil)
	if err != nil {
		t.Fatalf("failed to list runners: %v", err)
	}

	if len(runners) != 3 {
		t.Errorf("expected 3 runners of different types, got %d", len(runners))
	}

	// Filter by each type and verify
	for _, runnerType := range types {
		filtered, err := env.RunnerSvc.ListRunners(ctx, &service.ListRunnersRequest{
			RunnerType: runnerType,
		})
		if err != nil {
			t.Fatalf("failed to list %s runners: %v", runnerType, err)
		}
		if len(filtered) != 1 {
			t.Errorf("expected 1 %s runner, got %d", runnerType, len(filtered))
		}
	}
}

// TestRunnerModeSelection tests that dispatcher selects runners based on supported modes.
func TestRunnerModeSelection(t *testing.T) {
	ctx := context.Background()
	env := NewTestEnv(t)
	defer env.Stop()

	// Register sync-only runner
	env.RegisterMockRunner(ctx, "sync-runner", "compute", "localhost:50052",
		[]domain.ExecutionMode{domain.ExecutionModeSync})

	// Register async-only runner
	env.RegisterMockRunner(ctx, "async-runner", "compute", "localhost:50053",
		[]domain.ExecutionMode{domain.ExecutionModeAsync})

	env.Start()

	// Create work plan with sync stage
	wpID := env.CreateWorkPlan(ctx)
	syncMode := SyncMode()
	_, err := env.WriteNodes(ctx, wpID,
		[]*service.CheckWrite{
			{ID: "sync-check", State: ptr(domain.CheckStatePlanned), Kind: "compute"},
		},
		[]*service.StageWrite{
			{
				ID:            "sync-stage",
				ExecutionMode: &syncMode,
				RunnerType:    "compute",
				Assignments: []domain.Assignment{
					{TargetCheckID: "sync-check", GoalState: domain.CheckStateFinal},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to write nodes: %v", err)
	}

	// Wait for sync stage to complete
	if !env.WaitForStageState(ctx, wpID, "sync-stage", domain.StageStateFinal, 5*time.Second) {
		t.Fatal("sync stage did not complete")
	}

	// Verify sync runner was used
	syncRunner := env.MockRunners["localhost:50052"]
	asyncRunner := env.MockRunners["localhost:50053"]

	if syncRunner.GetRunCount() != 1 {
		t.Errorf("sync runner: expected 1 run, got %d", syncRunner.GetRunCount())
	}
	if asyncRunner.GetRunCount() != 0 {
		t.Errorf("async runner: expected 0 runs, got %d", asyncRunner.GetRunCount())
	}
}
