package service

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// StageRunnerClient is the interface for calling stage runners.
// This matches the generated gRPC client interface.
type StageRunnerClient interface {
	Run(ctx context.Context, req *RunRequest) (*RunResponse, error)
	RunAsync(ctx context.Context, req *RunAsyncRequest) (*RunAsyncResponse, error)
	Ping(ctx context.Context, req *PingRequest) (*PingResponse, error)
}

// RunRequest mirrors the proto RunRequest for stage execution.
type RunRequest struct {
	ExecutionID    string
	WorkPlanID     string
	StageID        string
	AttemptIdx     int
	RunnerType     string
	CheckOptions   []*CheckOptions
	CallbackAddr   string
	DeadlineMillis int64
}

// RunResponse mirrors the proto RunResponse.
type RunResponse struct {
	StageState   domain.StageState
	CheckUpdates []*CheckUpdate
	Failure      *domain.Failure
}

// RunAsyncRequest mirrors the proto RunAsyncRequest.
type RunAsyncRequest struct {
	ExecutionID    string
	WorkPlanID     string
	StageID        string
	AttemptIdx     int
	RunnerType     string
	CheckOptions   []*CheckOptions
	CallbackAddr   string
	DeadlineMillis int64
}

// RunAsyncResponse mirrors the proto RunAsyncResponse.
type RunAsyncResponse struct {
	Accepted bool
}

// PingRequest mirrors the proto PingRequest.
type PingRequest struct{}

// PingResponse mirrors the proto PingResponse.
type PingResponse struct {
	Healthy bool
}

// CheckOptions contains options for a check to be processed.
type CheckOptions struct {
	CheckID string
	Kind    string
	Options map[string]any
}

// CheckUpdate contains updates from a stage execution.
type CheckUpdate struct {
	CheckID  string
	State    domain.CheckState
	Data     map[string]any
	Finalize bool
}

// DispatcherConfig holds configuration for the Dispatcher.
type DispatcherConfig struct {
	PollInterval       time.Duration // How often to poll for pending executions
	CleanupInterval    time.Duration // How often to cleanup expired runners
	StaleCheckInterval time.Duration // How often to check for stale executions
	StaleDuration      time.Duration // How long before an execution is considered stale
	DefaultTimeout     time.Duration // Default execution timeout
	CallbackAddress    string        // Address runners should call back to
}

// DefaultDispatcherConfig returns reasonable defaults.
func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		PollInterval:       time.Second,
		CleanupInterval:    time.Minute,
		StaleCheckInterval: 30 * time.Second,
		StaleDuration:      5 * time.Minute,
		DefaultTimeout:     10 * time.Minute,
		CallbackAddress:    "localhost:50051",
	}
}

// Dispatcher polls the execution queue and dispatches work to runners.
type Dispatcher struct {
	storage        storage.Storage
	runnerService  *RunnerService
	orchestrator   *OrchestratorService
	config         DispatcherConfig
	clientCache    map[string]*grpc.ClientConn
	clientCacheMu  sync.RWMutex
	clientFactory  func(address string) (StageRunnerClient, error)
	stopCh         chan struct{}
	wg             sync.WaitGroup
}

// NewDispatcher creates a new Dispatcher.
func NewDispatcher(
	store storage.Storage,
	runnerService *RunnerService,
	orchestrator *OrchestratorService,
	config DispatcherConfig,
) *Dispatcher {
	d := &Dispatcher{
		storage:       store,
		runnerService: runnerService,
		orchestrator:  orchestrator,
		config:        config,
		clientCache:   make(map[string]*grpc.ClientConn),
		stopCh:        make(chan struct{}),
	}
	d.clientFactory = d.defaultClientFactory
	return d
}

// SetClientFactory allows injecting a custom client factory for testing.
func (d *Dispatcher) SetClientFactory(factory func(address string) (StageRunnerClient, error)) {
	d.clientFactory = factory
}

// Start begins the dispatcher loops.
func (d *Dispatcher) Start() {
	d.wg.Add(3)
	go d.pollLoop()
	go d.cleanupLoop()
	go d.staleCheckLoop()
}

// Stop gracefully stops the dispatcher.
func (d *Dispatcher) Stop() {
	close(d.stopCh)
	d.wg.Wait()

	// Close all cached connections
	d.clientCacheMu.Lock()
	for _, conn := range d.clientCache {
		conn.Close()
	}
	d.clientCache = nil
	d.clientCacheMu.Unlock()
}

// pollLoop polls for pending executions and dispatches them.
func (d *Dispatcher) pollLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			if err := d.processPendingExecutions(context.Background()); err != nil {
				log.Printf("dispatcher: error processing pending executions: %v", err)
			}
		}
	}
}

// cleanupLoop periodically cleans up expired runner registrations.
func (d *Dispatcher) cleanupLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			count, err := d.runnerService.CleanupExpired(context.Background())
			if err != nil {
				log.Printf("dispatcher: error cleaning up expired runners: %v", err)
			} else if count > 0 {
				log.Printf("dispatcher: cleaned up %d expired runner registrations", count)
			}
		}
	}
}

// staleCheckLoop periodically checks for stale/timed-out executions.
func (d *Dispatcher) staleCheckLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.StaleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			if err := d.handleStaleExecutions(context.Background()); err != nil {
				log.Printf("dispatcher: error handling stale executions: %v", err)
			}
		}
	}
}

// processPendingExecutions finds and dispatches pending executions.
func (d *Dispatcher) processPendingExecutions(ctx context.Context) error {
	// Get list of registered runner types
	runners, err := d.runnerService.ListRunners(ctx, nil)
	if err != nil {
		return err
	}

	// Collect unique runner types
	runnerTypes := make(map[string]bool)
	for _, r := range runners {
		runnerTypes[r.RunnerType] = true
	}

	// For each runner type, get pending executions
	for runnerType := range runnerTypes {
		if err := d.dispatchForRunnerType(ctx, runnerType); err != nil {
			log.Printf("dispatcher: error dispatching for runner type %s: %v", runnerType, err)
		}
	}

	return nil
}

// dispatchForRunnerType dispatches pending executions for a specific runner type.
func (d *Dispatcher) dispatchForRunnerType(ctx context.Context, runnerType string) error {
	uow, err := d.storage.Begin(ctx)
	if err != nil {
		return err
	}
	defer uow.Rollback()

	// Get pending executions for this runner type
	executions, err := uow.StageExecutions().GetPending(ctx, runnerType, 10)
	if err != nil {
		return err
	}

	// Close initial query transaction before processing
	if err := uow.Commit(); err != nil {
		return err
	}

	for _, exec := range executions {
		// Start a fresh transaction for each execution
		uow, err = d.storage.Begin(ctx)
		if err != nil {
			return err
		}

		// Select an available runner
		runner, err := d.runnerService.SelectRunner(ctx, runnerType, exec.ExecutionMode)
		if err == domain.ErrRunnerNotFound {
			// No runners available, skip for now
			uow.Rollback()
			continue
		}
		if err != nil {
			log.Printf("dispatcher: error selecting runner for %s: %v", exec.ID, err)
			uow.Rollback()
			continue
		}

		// Dispatch to the runner
		// Note: dispatchSync commits the transaction before calling the runner
		if err := d.dispatchExecution(ctx, uow, exec, runner); err != nil {
			log.Printf("dispatcher: error dispatching execution %s: %v", exec.ID, err)
			uow.Rollback()
			continue
		}

		// For async executions, we need to commit here.
		// For sync executions, dispatchSync already committed.
		if exec.ExecutionMode != domain.ExecutionModeSync {
			if err := uow.Commit(); err != nil {
				return err
			}
		}
	}

	return nil
}

// dispatchExecution dispatches a single execution to a runner.
func (d *Dispatcher) dispatchExecution(ctx context.Context, uow storage.UnitOfWork, exec *domain.StageExecution, runner *domain.StageRunner) error {
	// Get the stage to build check options
	stage, err := uow.Stages().Get(ctx, exec.WorkPlanID, exec.StageID)
	if err != nil {
		return err
	}

	// Build check options from assignments
	var checkOptions []*CheckOptions
	for _, assignment := range stage.Assignments {
		check, err := uow.Checks().Get(ctx, exec.WorkPlanID, assignment.TargetCheckID)
		if err != nil {
			log.Printf("dispatcher: warning: could not get check %s: %v", assignment.TargetCheckID, err)
			continue
		}
		checkOptions = append(checkOptions, &CheckOptions{
			CheckID: check.ID,
			Kind:    check.Kind,
			Options: check.Options,
		})
	}

	// Set deadline
	deadline := time.Now().UTC().Add(d.config.DefaultTimeout)
	exec.SetDeadline(deadline)

	// Mark as dispatched
	if err := uow.StageExecutions().MarkDispatched(ctx, exec.ID, runner.RegistrationID); err != nil {
		return err
	}

	// Increment runner load
	if err := uow.StageRunners().IncrementLoad(ctx, runner.RegistrationID); err != nil {
		return err
	}

	// Get or create client connection
	client, err := d.getClient(runner.Address)
	if err != nil {
		// Mark execution as failed if we can't connect
		if markErr := uow.StageExecutions().MarkFailed(ctx, exec.ID, "failed to connect to runner: "+err.Error()); markErr != nil {
			log.Printf("dispatcher: error marking execution failed: %v", markErr)
		}
		return err
	}

	// Dispatch based on execution mode
	if exec.ExecutionMode == domain.ExecutionModeAsync {
		return d.dispatchAsync(ctx, client, exec, checkOptions, deadline)
	}
	return d.dispatchSync(ctx, uow, client, exec, checkOptions, deadline)
}

// dispatchSync dispatches a synchronous execution (blocks until complete).
// Note: This commits the uow before calling the runner to avoid transaction conflicts.
func (d *Dispatcher) dispatchSync(ctx context.Context, uow storage.UnitOfWork, client StageRunnerClient, exec *domain.StageExecution, checkOptions []*CheckOptions, deadline time.Time) error {
	req := &RunRequest{
		ExecutionID:    exec.ID,
		WorkPlanID:     exec.WorkPlanID,
		StageID:        exec.StageID,
		AttemptIdx:     exec.AttemptIdx,
		RunnerType:     exec.RunnerType,
		CheckOptions:   checkOptions,
		CallbackAddr:   d.config.CallbackAddress,
		DeadlineMillis: deadline.UnixMilli(),
	}

	// Commit the transaction before making the blocking call.
	// This ensures that:
	// 1. MarkDispatched and IncrementLoad are persisted
	// 2. applyCheckUpdates (which starts a new transaction) won't conflict
	if err := uow.Commit(); err != nil {
		return err
	}

	// Create context with deadline
	execCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	resp, err := client.Run(execCtx, req)

	// Start a new transaction for post-execution updates
	postUow, uowErr := d.storage.Begin(ctx)
	if uowErr != nil {
		log.Printf("dispatcher: error starting post-execution transaction: %v", uowErr)
		return uowErr
	}
	defer postUow.Rollback()

	if err != nil {
		// Mark execution as failed
		if markErr := postUow.StageExecutions().MarkFailed(ctx, exec.ID, err.Error()); markErr != nil {
			log.Printf("dispatcher: error marking execution failed: %v", markErr)
		}
		// Decrement runner load
		if loadErr := postUow.StageRunners().DecrementLoad(ctx, exec.RunnerID); loadErr != nil {
			log.Printf("dispatcher: error decrementing load: %v", loadErr)
		}
		postUow.Commit()
		return err
	}

	// Apply check updates (this starts its own transaction via WriteNodes)
	if err := d.applyCheckUpdates(ctx, exec.WorkPlanID, resp.CheckUpdates); err != nil {
		log.Printf("dispatcher: error applying check updates: %v", err)
	}

	// Update stage state based on response
	if resp.StageState != domain.StageStateUnknown {
		if err := d.updateStageState(ctx, exec.WorkPlanID, exec.StageID, resp.StageState); err != nil {
			log.Printf("dispatcher: error updating stage state: %v", err)
		}
	}

	// Mark execution complete or failed based on stage state and failure
	if resp.Failure != nil {
		if err := postUow.StageExecutions().MarkFailed(ctx, exec.ID, resp.Failure.Message); err != nil {
			return err
		}
	} else if resp.StageState == domain.StageStateFinal || resp.StageState == domain.StageStateAwaitingGroup {
		if err := postUow.StageExecutions().MarkComplete(ctx, exec.ID); err != nil {
			return err
		}
	} else {
		if err := postUow.StageExecutions().MarkFailed(ctx, exec.ID, "unexpected stage state"); err != nil {
			return err
		}
	}

	// Decrement runner load
	if err := postUow.StageRunners().DecrementLoad(ctx, exec.RunnerID); err != nil {
		log.Printf("dispatcher: error decrementing load: %v", err)
	}

	return postUow.Commit()
}

// dispatchAsync dispatches an asynchronous execution (returns immediately).
func (d *Dispatcher) dispatchAsync(ctx context.Context, client StageRunnerClient, exec *domain.StageExecution, checkOptions []*CheckOptions, deadline time.Time) error {
	req := &RunAsyncRequest{
		ExecutionID:    exec.ID,
		WorkPlanID:     exec.WorkPlanID,
		StageID:        exec.StageID,
		AttemptIdx:     exec.AttemptIdx,
		RunnerType:     exec.RunnerType,
		CheckOptions:   checkOptions,
		CallbackAddr:   d.config.CallbackAddress,
		DeadlineMillis: deadline.UnixMilli(),
	}

	resp, err := client.RunAsync(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Accepted {
		return domain.ErrRunnerUnavailable
	}

	// For async, we don't wait for completion - the runner will call back
	return nil
}

// applyCheckUpdates applies check updates from a stage execution.
func (d *Dispatcher) applyCheckUpdates(ctx context.Context, workPlanID string, updates []*CheckUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	// Build WriteNodes request with check updates
	var checkWrites []*CheckWrite
	for _, update := range updates {
		cw := &CheckWrite{
			ID:    update.CheckID,
			State: &update.State,
		}
		if update.Data != nil || update.Finalize {
			cw.Result = &CheckResultWrite{
				OwnerType: "stage",
				OwnerID:   "",
				Data:      update.Data,
				Finalize:  update.Finalize,
			}
		}
		checkWrites = append(checkWrites, cw)
	}

	_, err := d.orchestrator.WriteNodes(ctx, &WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks:     checkWrites,
	})
	return err
}

// updateStageState updates the stage state after execution completes.
func (d *Dispatcher) updateStageState(ctx context.Context, workPlanID, stageID string, state domain.StageState) error {
	_, err := d.orchestrator.WriteNodes(ctx, &WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages: []*StageWrite{
			{
				ID:    stageID,
				State: &state,
			},
		},
	})
	return err
}

// handleStaleExecutions marks stale/timed-out executions as failed.
func (d *Dispatcher) handleStaleExecutions(ctx context.Context) error {
	uow, err := d.storage.Begin(ctx)
	if err != nil {
		return err
	}
	defer uow.Rollback()

	// Get timed-out executions (past deadline)
	timedOut, err := uow.StageExecutions().GetTimedOut(ctx)
	if err != nil {
		return err
	}

	for _, exec := range timedOut {
		log.Printf("dispatcher: marking execution %s as timed out", exec.ID)
		if err := uow.StageExecutions().MarkFailed(ctx, exec.ID, "execution timed out"); err != nil {
			log.Printf("dispatcher: error marking execution timed out: %v", err)
			continue
		}
		// Decrement runner load if assigned
		if exec.RunnerID != "" {
			if err := uow.StageRunners().DecrementLoad(ctx, exec.RunnerID); err != nil {
				log.Printf("dispatcher: error decrementing load: %v", err)
			}
		}
	}

	// Get stale executions (no progress for too long)
	stale, err := uow.StageExecutions().GetStale(ctx, d.config.StaleDuration)
	if err != nil {
		return err
	}

	for _, exec := range stale {
		log.Printf("dispatcher: marking execution %s as stale", exec.ID)
		if err := uow.StageExecutions().MarkFailed(ctx, exec.ID, "execution stale - no progress"); err != nil {
			log.Printf("dispatcher: error marking execution stale: %v", err)
			continue
		}
		// Decrement runner load if assigned
		if exec.RunnerID != "" {
			if err := uow.StageRunners().DecrementLoad(ctx, exec.RunnerID); err != nil {
				log.Printf("dispatcher: error decrementing load: %v", err)
			}
		}
	}

	return uow.Commit()
}

// getClient returns a cached or new client for the given runner address.
func (d *Dispatcher) getClient(address string) (StageRunnerClient, error) {
	return d.clientFactory(address)
}

// defaultClientFactory creates a real gRPC client.
func (d *Dispatcher) defaultClientFactory(address string) (StageRunnerClient, error) {
	d.clientCacheMu.RLock()
	conn, ok := d.clientCache[address]
	d.clientCacheMu.RUnlock()

	if ok {
		return &grpcRunnerClient{conn: conn}, nil
	}

	d.clientCacheMu.Lock()
	defer d.clientCacheMu.Unlock()

	// Double-check after acquiring write lock
	if conn, ok := d.clientCache[address]; ok {
		return &grpcRunnerClient{conn: conn}, nil
	}

	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	d.clientCache[address] = conn
	return &grpcRunnerClient{conn: conn}, nil
}

// grpcRunnerClient wraps a gRPC connection to implement StageRunnerClient.
// This will be replaced with the generated client once protos are compiled.
type grpcRunnerClient struct {
	conn *grpc.ClientConn
}

func (c *grpcRunnerClient) Run(ctx context.Context, req *RunRequest) (*RunResponse, error) {
	// TODO: Use generated client once protos are compiled
	// For now, return an error indicating not implemented
	return nil, domain.ErrRunnerUnavailable
}

func (c *grpcRunnerClient) RunAsync(ctx context.Context, req *RunAsyncRequest) (*RunAsyncResponse, error) {
	// TODO: Use generated client once protos are compiled
	return nil, domain.ErrRunnerUnavailable
}

func (c *grpcRunnerClient) Ping(ctx context.Context, req *PingRequest) (*PingResponse, error) {
	// TODO: Use generated client once protos are compiled
	return nil, domain.ErrRunnerUnavailable
}
