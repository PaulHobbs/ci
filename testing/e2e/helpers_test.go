package e2e

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage/sqlite"
)

// TestEnv provides a complete test environment with all services configured.
type TestEnv struct {
	Storage      *sqlite.SQLiteStorage
	Orchestrator *service.OrchestratorService
	RunnerSvc    *service.RunnerService
	CallbackSvc  *service.CallbackService
	Dispatcher   *service.Dispatcher

	// MockRunners is a registry of mock runners by address
	MockRunners map[string]*MockRunner
	mu          sync.Mutex

	t      *testing.T
	dbPath string // path to temp db file for cleanup
}

// NewTestEnv creates a new test environment with a temp database.
func NewTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	ctx := context.Background()

	// Create a temp file database.
	// Using a real file with WAL mode provides better concurrent write handling
	// than shared memory, which is important since the dispatcher has multiple
	// background goroutines that all write to the database.
	tmpDir := os.TempDir()
	dbPath := filepath.Join(tmpDir, "turboci_test_"+t.Name()+".db")
	// Clean up any leftover file from previous runs
	os.Remove(dbPath)
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")

	storage, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Run migrations
	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// Create services
	orchestrator := service.NewOrchestrator(storage)
	runnerSvc := service.NewRunnerService(storage)
	callbackSvc := service.NewCallbackService(storage, orchestrator)

	// Create dispatcher with fast polling for tests
	dispatcherCfg := service.DispatcherConfig{
		PollInterval:       50 * time.Millisecond,
		CleanupInterval:    time.Second,
		StaleCheckInterval: 500 * time.Millisecond,
		StaleDuration:      2 * time.Second,
		DefaultTimeout:     30 * time.Second,
		CallbackAddress:    "localhost:50051",
	}
	dispatcher := service.NewDispatcher(storage, runnerSvc, orchestrator, dispatcherCfg)

	env := &TestEnv{
		Storage:      storage,
		Orchestrator: orchestrator,
		RunnerSvc:    runnerSvc,
		CallbackSvc:  callbackSvc,
		Dispatcher:   dispatcher,
		MockRunners:  make(map[string]*MockRunner),
		t:            t,
		dbPath:       dbPath,
	}

	// Inject our mock client factory into the dispatcher
	dispatcher.SetClientFactory(env.mockClientFactory)

	return env
}

// Start starts the dispatcher.
func (e *TestEnv) Start() {
	e.Dispatcher.Start()
}

// Stop stops the dispatcher and closes storage.
func (e *TestEnv) Stop() {
	e.Dispatcher.Stop()
	e.Storage.Close()

	// Clean up temp database files
	if e.dbPath != "" {
		os.Remove(e.dbPath)
		os.Remove(e.dbPath + "-wal")
		os.Remove(e.dbPath + "-shm")
	}
}

// RegisterMockRunner creates and registers a mock runner.
func (e *TestEnv) RegisterMockRunner(ctx context.Context, runnerID, runnerType, address string, modes []domain.ExecutionMode) *MockRunner {
	e.t.Helper()

	mock := NewMockRunner(runnerID, runnerType, address, modes)

	e.mu.Lock()
	e.MockRunners[address] = mock
	e.mu.Unlock()

	// Register with the runner service
	_, err := e.RunnerSvc.RegisterRunner(ctx, &service.RegisterRunnerRequest{
		RunnerID:       runnerID,
		RunnerType:     runnerType,
		Address:        address,
		SupportedModes: modes,
		MaxConcurrent:  10,
		TTLSeconds:     300,
	})
	if err != nil {
		e.t.Fatalf("failed to register mock runner: %v", err)
	}

	return mock
}

// mockClientFactory returns mock clients for registered runners.
func (e *TestEnv) mockClientFactory(address string) (service.StageRunnerClient, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if mock, ok := e.MockRunners[address]; ok {
		return mock, nil
	}

	return nil, domain.ErrRunnerUnavailable
}

// CreateWorkPlan creates a new work plan.
func (e *TestEnv) CreateWorkPlan(ctx context.Context) string {
	e.t.Helper()

	wp, err := e.Orchestrator.CreateWorkPlan(ctx, nil)
	if err != nil {
		e.t.Fatalf("failed to create work plan: %v", err)
	}
	return wp.ID
}

// WriteCheck creates or updates a check.
func (e *TestEnv) WriteCheck(ctx context.Context, workPlanID string, check *service.CheckWrite) *domain.Check {
	e.t.Helper()

	resp, err := e.Orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks:     []*service.CheckWrite{check},
	})
	if err != nil {
		e.t.Fatalf("failed to write check: %v", err)
	}
	return resp.Checks[0]
}

// WriteStage creates or updates a stage.
func (e *TestEnv) WriteStage(ctx context.Context, workPlanID string, stage *service.StageWrite) *domain.Stage {
	e.t.Helper()

	resp, err := e.Orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages:     []*service.StageWrite{stage},
	})
	if err != nil {
		e.t.Fatalf("failed to write stage: %v", err)
	}
	return resp.Stages[0]
}

// WriteNodes writes multiple checks and stages atomically.
func (e *TestEnv) WriteNodes(ctx context.Context, workPlanID string, checks []*service.CheckWrite, stages []*service.StageWrite) (*service.WriteNodesResponse, error) {
	return e.Orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks:     checks,
		Stages:     stages,
	})
}

// GetCheck retrieves a check by ID.
func (e *TestEnv) GetCheck(ctx context.Context, workPlanID, checkID string) *domain.Check {
	e.t.Helper()

	resp, err := e.Orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		CheckIDs:      []string{checkID},
		IncludeChecks: true,
	})
	if err != nil {
		e.t.Fatalf("failed to query check: %v", err)
	}
	if len(resp.Checks) == 0 {
		e.t.Fatalf("check %s not found", checkID)
	}
	return resp.Checks[0]
}

// GetStage retrieves a stage by ID.
func (e *TestEnv) GetStage(ctx context.Context, workPlanID, stageID string) *domain.Stage {
	e.t.Helper()

	resp, err := e.Orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		StageIDs:      []string{stageID},
		IncludeStages: true,
	})
	if err != nil {
		e.t.Fatalf("failed to query stage: %v", err)
	}
	if len(resp.Stages) == 0 {
		e.t.Fatalf("stage %s not found", stageID)
	}
	return resp.Stages[0]
}

// WaitForStageState waits for a stage to reach the expected state.
func (e *TestEnv) WaitForStageState(ctx context.Context, workPlanID, stageID string, expectedState domain.StageState, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		stage := e.GetStage(ctx, workPlanID, stageID)
		if stage.State == expectedState {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// WaitForCheckState waits for a check to reach the expected state.
func (e *TestEnv) WaitForCheckState(ctx context.Context, workPlanID, checkID string, expectedState domain.CheckState, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		check := e.GetCheck(ctx, workPlanID, checkID)
		if check.State == expectedState {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// WaitForExecution waits for an execution to be created for a stage.
func (e *TestEnv) WaitForExecution(ctx context.Context, workPlanID, stageID string, timeout time.Duration) *domain.StageExecution {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		uow, err := e.Storage.Begin(ctx)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		exec, err := uow.StageExecutions().GetByStageAttempt(ctx, workPlanID, stageID, 1)
		uow.Rollback()

		if err == nil {
			return exec
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// MockRunner is an in-process mock implementation of StageRunnerClient.
type MockRunner struct {
	RunnerID       string
	RunnerType     string
	Address        string
	SupportedModes []domain.ExecutionMode
	MaxConcurrent  int

	// Behavior hooks - set these to customize behavior
	OnRun      func(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error)
	OnRunAsync func(ctx context.Context, req *service.RunAsyncRequest) (*service.RunAsyncResponse, error)
	OnPing     func(ctx context.Context, req *service.PingRequest) (*service.PingResponse, error)

	// For async completion simulation
	CallbackSvc *service.CallbackService

	// State tracking
	mu                sync.Mutex
	ReceivedRuns      []*service.RunRequest
	ReceivedRunAsyncs []*service.RunAsyncRequest
	CurrentLoad       int
}

// NewMockRunner creates a new mock runner with default success behavior.
func NewMockRunner(runnerID, runnerType, address string, modes []domain.ExecutionMode) *MockRunner {
	m := &MockRunner{
		RunnerID:       runnerID,
		RunnerType:     runnerType,
		Address:        address,
		SupportedModes: modes,
		MaxConcurrent:  10,
	}

	// Default behavior: succeed and finalize all checks
	m.OnRun = m.defaultRun
	m.OnRunAsync = m.defaultRunAsync

	return m
}

// Run implements StageRunnerClient.
func (m *MockRunner) Run(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
	m.mu.Lock()
	m.ReceivedRuns = append(m.ReceivedRuns, req)
	m.CurrentLoad++
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.CurrentLoad--
		m.mu.Unlock()
	}()

	if m.OnRun != nil {
		return m.OnRun(ctx, req)
	}
	return m.defaultRun(ctx, req)
}

// RunAsync implements StageRunnerClient.
func (m *MockRunner) RunAsync(ctx context.Context, req *service.RunAsyncRequest) (*service.RunAsyncResponse, error) {
	m.mu.Lock()
	m.ReceivedRunAsyncs = append(m.ReceivedRunAsyncs, req)
	m.CurrentLoad++
	m.mu.Unlock()

	if m.OnRunAsync != nil {
		return m.OnRunAsync(ctx, req)
	}
	return m.defaultRunAsync(ctx, req)
}

// Ping implements StageRunnerClient.
func (m *MockRunner) Ping(ctx context.Context, req *service.PingRequest) (*service.PingResponse, error) {
	if m.OnPing != nil {
		return m.OnPing(ctx, req)
	}
	return &service.PingResponse{Healthy: true}, nil
}

// defaultRun is the default sync execution behavior.
func (m *MockRunner) defaultRun(ctx context.Context, req *service.RunRequest) (*service.RunResponse, error) {
	// Simulate some work
	time.Sleep(10 * time.Millisecond)

	// Finalize all assigned checks
	var updates []*service.CheckUpdate
	for _, opt := range req.CheckOptions {
		updates = append(updates, &service.CheckUpdate{
			CheckID:  opt.CheckID,
			State:    domain.CheckStateFinal,
			Data:     map[string]any{"runner": m.RunnerID, "success": true},
			Finalize: true,
		})
	}

	return &service.RunResponse{
		StageState:   domain.StageStateFinal,
		CheckUpdates: updates,
	}, nil
}

// defaultRunAsync is the default async execution behavior.
func (m *MockRunner) defaultRunAsync(ctx context.Context, req *service.RunAsyncRequest) (*service.RunAsyncResponse, error) {
	// Start async work in goroutine
	go m.simulateAsyncWork(req)

	return &service.RunAsyncResponse{
		Accepted: true,
	}, nil
}

// simulateAsyncWork simulates async work and calls back on completion.
func (m *MockRunner) simulateAsyncWork(req *service.RunAsyncRequest) {
	defer func() {
		m.mu.Lock()
		m.CurrentLoad--
		m.mu.Unlock()
	}()

	// Small delay to ensure dispatch transaction is committed
	time.Sleep(50 * time.Millisecond)

	// Simulate progress
	if m.CallbackSvc != nil {
		for i := 25; i <= 75; i += 25 {
			if err := m.CallbackSvc.UpdateExecution(context.Background(), &service.UpdateExecutionRequest{
				ExecutionID:     req.ExecutionID,
				ProgressPercent: i,
				ProgressMessage: "processing...",
			}); err != nil {
				// Log error but continue
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Simulate work
	time.Sleep(20 * time.Millisecond)

	// Complete with check updates
	if m.CallbackSvc != nil {
		var updates []*service.CheckUpdate
		for _, opt := range req.CheckOptions {
			updates = append(updates, &service.CheckUpdate{
				CheckID:  opt.CheckID,
				State:    domain.CheckStateFinal,
				Data:     map[string]any{"runner": m.RunnerID, "async": true},
				Finalize: true,
			})
		}

		if err := m.CallbackSvc.CompleteExecution(context.Background(), &service.CompleteExecutionRequest{
			ExecutionID:  req.ExecutionID,
			Success:      true,
			CheckUpdates: updates,
		}); err != nil {
			log.Printf("mock runner: CompleteExecution error: %v", err)
		}
	}
}

// SetCallbackService sets the callback service for async completion.
func (m *MockRunner) SetCallbackService(svc *service.CallbackService) {
	m.CallbackSvc = svc
}

// GetRunCount returns the number of sync runs received.
func (m *MockRunner) GetRunCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ReceivedRuns)
}

// GetRunAsyncCount returns the number of async runs received.
func (m *MockRunner) GetRunAsyncCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ReceivedRunAsyncs)
}

// Helper functions for creating common test structures

// SyncMode returns the sync execution mode.
func SyncMode() domain.ExecutionMode {
	return domain.ExecutionModeSync
}

// AsyncMode returns the async execution mode.
func AsyncMode() domain.ExecutionMode {
	return domain.ExecutionModeAsync
}

// PlannedState returns the planned check state.
func PlannedState() domain.CheckState {
	return domain.CheckStatePlanned
}

// FinalState returns the final check state.
func FinalCheckState() domain.CheckState {
	return domain.CheckStateFinal
}

// FinalStageState returns the final stage state.
func FinalStageState() domain.StageState {
	return domain.StageStateFinal
}

// AttemptingState returns the attempting stage state.
func AttemptingState() domain.StageState {
	return domain.StageStateAttempting
}
