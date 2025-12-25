package ci

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
)

// WorkflowContext manages a workflow execution with an imperative async API.
// It provides futures for waiting on checks and stages to complete.
// Thread-safe: can be used from multiple goroutines concurrently.
type WorkflowContext struct {
	orchestrator *service.OrchestratorService
	grpcClient   pb.TurboCIOrchestratorClient // Optional, for streaming
	workPlanID   string

	mu          sync.RWMutex
	checks      map[string]*CheckHandle
	stages      map[string]*StageHandle
	eventSubs   map[string][]chan *pb.WorkPlanEvent // Per-node subscriptions
	streamConn  *grpc.ClientConn
	eventStream pb.TurboCIOrchestrator_WatchWorkPlanClient
	usePolling  bool // Fallback to polling if streaming unavailable

	pollInterval time.Duration
}

// WorkflowOption configures a WorkflowContext.
type WorkflowOption func(*WorkflowContext)

// WithGRPCClient sets a gRPC client for streaming events.
func WithGRPCClient(client pb.TurboCIOrchestratorClient) WorkflowOption {
	return func(w *WorkflowContext) {
		w.grpcClient = client
	}
}

// WithPollingFallback forces polling mode even if streaming is available.
func WithPollingFallback() WorkflowOption {
	return func(w *WorkflowContext) {
		w.usePolling = true
	}
}

// WithPollInterval sets the polling interval for non-streaming mode.
func WithPollInterval(d time.Duration) WorkflowOption {
	return func(w *WorkflowContext) {
		w.pollInterval = d
	}
}

// NewWorkflowContext creates a new workflow context with a fresh work plan.
func NewWorkflowContext(ctx context.Context, orch *service.OrchestratorService, opts ...WorkflowOption) (*WorkflowContext, error) {
	wp, err := orch.CreateWorkPlan(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create work plan: %w", err)
	}

	wf := &WorkflowContext{
		orchestrator: orch,
		workPlanID:   wp.ID,
		checks:       make(map[string]*CheckHandle),
		stages:       make(map[string]*StageHandle),
		eventSubs:    make(map[string][]chan *pb.WorkPlanEvent),
		pollInterval: 50 * time.Millisecond,
	}

	for _, opt := range opts {
		opt(wf)
	}

	// Try to start streaming if client available and not forced to polling
	if wf.grpcClient != nil && !wf.usePolling {
		if err := wf.startStreaming(ctx); err != nil {
			log.Printf("ci: streaming unavailable, falling back to polling: %v", err)
			wf.usePolling = true
		}
	} else {
		wf.usePolling = true
	}

	return wf, nil
}

// WorkPlanID returns the underlying work plan ID.
func (w *WorkflowContext) WorkPlanID() string {
	return w.workPlanID
}

// Orchestrator returns the underlying orchestrator service.
func (w *WorkflowContext) Orchestrator() *service.OrchestratorService {
	return w.orchestrator
}

// DefineCheck creates a check and returns a handle for async waiting.
func (w *WorkflowContext) DefineCheck(ctx context.Context, id string, opts ...CheckOption) (*CheckHandle, error) {
	if id == "" {
		panic("ci: DefineCheck() called with empty id")
	}

	builder := NewCheck(id).Planned()
	for _, opt := range opts {
		opt(builder)
	}

	_, err := w.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: w.workPlanID,
		Checks:     []*service.CheckWrite{builder.Build()},
	})
	if err != nil {
		return nil, err
	}

	handle := &CheckHandle{
		wf: w,
		id: id,
	}

	w.mu.Lock()
	w.checks[id] = handle
	w.mu.Unlock()

	return handle, nil
}

// DefineStage creates a stage and returns a handle for async waiting.
func (w *WorkflowContext) DefineStage(ctx context.Context, id string, opts ...StageOption) (*StageHandle, error) {
	if id == "" {
		panic("ci: DefineStage() called with empty id")
	}

	builder := NewStage(id).Planned()
	for _, opt := range opts {
		opt(builder)
	}

	_, err := w.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: w.workPlanID,
		Stages:     []*service.StageWrite{builder.Build()},
	})
	if err != nil {
		return nil, err
	}

	handle := &StageHandle{
		wf: w,
		id: id,
	}

	w.mu.Lock()
	w.stages[id] = handle
	w.mu.Unlock()

	return handle, nil
}

// GetCheck returns an existing check handle by ID.
func (w *WorkflowContext) GetCheck(id string) (*CheckHandle, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	h, ok := w.checks[id]
	return h, ok
}

// GetStage returns an existing stage handle by ID.
func (w *WorkflowContext) GetStage(id string) (*StageHandle, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	h, ok := w.stages[id]
	return h, ok
}

// Close cleans up streaming resources.
func (w *WorkflowContext) Close() error {
	if w.eventStream != nil {
		w.eventStream.CloseSend()
	}
	return nil
}

// startStreaming initializes the event stream.
func (w *WorkflowContext) startStreaming(ctx context.Context) error {
	stream, err := w.grpcClient.WatchWorkPlan(ctx, &pb.WatchWorkPlanRequest{
		WorkPlanId:    w.workPlanID,
		IncludeChecks: true,
		IncludeStages: true,
	})
	if err != nil {
		return err
	}
	w.eventStream = stream

	// Start background goroutine to receive and dispatch events
	go w.eventLoop()

	return nil
}

// eventLoop reads events from the stream and dispatches them.
func (w *WorkflowContext) eventLoop() {
	for {
		event, err := w.eventStream.Recv()
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Printf("ci: stream error: %v", err)
			return
		}

		w.dispatchEvent(event)
	}
}

// dispatchEvent sends an event to relevant subscribers.
func (w *WorkflowContext) dispatchEvent(event *pb.WorkPlanEvent) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var nodeID string
	switch e := event.Event.(type) {
	case *pb.WorkPlanEvent_CheckEvent:
		nodeID = e.CheckEvent.CheckId
	case *pb.WorkPlanEvent_StageEvent:
		nodeID = e.StageEvent.StageId
	}

	if subs, ok := w.eventSubs[nodeID]; ok {
		for _, ch := range subs {
			select {
			case ch <- event:
			default:
				// Channel full, skip
			}
		}
	}
}

// subscribeToNode creates a subscription for a specific node.
func (w *WorkflowContext) subscribeToNode(nodeID string) chan *pb.WorkPlanEvent {
	ch := make(chan *pb.WorkPlanEvent, 10)

	w.mu.Lock()
	w.eventSubs[nodeID] = append(w.eventSubs[nodeID], ch)
	w.mu.Unlock()

	return ch
}

// unsubscribeFromNode removes a subscription.
func (w *WorkflowContext) unsubscribeFromNode(nodeID string, ch chan *pb.WorkPlanEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()

	subs := w.eventSubs[nodeID]
	for i, sub := range subs {
		if sub == ch {
			w.eventSubs[nodeID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	close(ch)
}

// CheckOption is a functional option for DefineCheck.
type CheckOption func(*CheckBuilder)

// WithKind sets the check kind.
func WithKind(kind string) CheckOption {
	return func(b *CheckBuilder) { b.Kind(kind) }
}

// WithCheckDeps sets check dependencies.
func WithCheckDeps(checkIDs ...string) CheckOption {
	return func(b *CheckBuilder) { b.DependsOn(checkIDs...) }
}

// WithCheckOptions sets check options.
func WithCheckOptions(opts map[string]any) CheckOption {
	return func(b *CheckBuilder) { b.Options(opts) }
}

// WithCheckOption sets a single check option.
func WithCheckOption(key string, value any) CheckOption {
	return func(b *CheckBuilder) { b.Option(key, value) }
}

// StageOption is a functional option for DefineStage.
type StageOption func(*StageBuilder)

// WithRunner sets the runner type.
func WithRunner(rt string) StageOption {
	return func(b *StageBuilder) { b.RunnerType(rt) }
}

// WithSyncMode sets sync execution mode.
func WithSyncMode() StageOption {
	return func(b *StageBuilder) { b.Sync() }
}

// WithAsyncMode sets async execution mode.
func WithAsyncMode() StageOption {
	return func(b *StageBuilder) { b.Async() }
}

// WithStageDeps sets stage dependencies.
func WithStageDeps(stageIDs ...string) StageOption {
	return func(b *StageBuilder) { b.DependsOnStages(stageIDs...) }
}

// AssignsTo sets check assignments.
func AssignsTo(checkIDs ...string) StageOption {
	return func(b *StageBuilder) {
		for _, id := range checkIDs {
			b.Assigns(id)
		}
	}
}

// WithStageArgs sets stage arguments.
func WithStageArgs(args map[string]any) StageOption {
	return func(b *StageBuilder) { b.Args(args) }
}

// WithStageArg sets a single stage argument.
func WithStageArg(key string, value any) StageOption {
	return func(b *StageBuilder) { b.Arg(key, value) }
}

// CheckHandle provides async operations on a check.
type CheckHandle struct {
	wf     *WorkflowContext
	id     string
	mu     sync.RWMutex
	result *domain.Check
	err    error
}

// ID returns the check ID.
func (h *CheckHandle) ID() string { return h.id }

// Wait blocks until the check reaches FINAL state.
func (h *CheckHandle) Wait(ctx context.Context) (*domain.Check, error) {
	if h.wf.usePolling {
		return h.waitPolling(ctx)
	}
	return h.waitStreaming(ctx)
}

// waitPolling polls for check completion.
func (h *CheckHandle) waitPolling(ctx context.Context) (*domain.Check, error) {
	ticker := time.NewTicker(h.wf.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			resp, err := h.wf.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
				WorkPlanID:    h.wf.workPlanID,
				CheckIDs:      []string{h.id},
				IncludeChecks: true,
			})
			if err != nil {
				return nil, err
			}
			if len(resp.Checks) > 0 && resp.Checks[0].State == domain.CheckStateFinal {
				h.mu.Lock()
				h.result = resp.Checks[0]
				h.mu.Unlock()
				return resp.Checks[0], nil
			}
		}
	}
}

// waitStreaming waits using the event stream.
func (h *CheckHandle) waitStreaming(ctx context.Context) (*domain.Check, error) {
	eventCh := h.wf.subscribeToNode(h.id)
	defer h.wf.unsubscribeFromNode(h.id, eventCh)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event := <-eventCh:
			if ce, ok := event.Event.(*pb.WorkPlanEvent_CheckEvent); ok {
				if ce.CheckEvent.NewState == pb.CheckState_CHECK_STATE_FINAL {
					// Fetch full check details
					resp, err := h.wf.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
						WorkPlanID:    h.wf.workPlanID,
						CheckIDs:      []string{h.id},
						IncludeChecks: true,
					})
					if err != nil {
						return nil, err
					}
					if len(resp.Checks) > 0 {
						h.mu.Lock()
						h.result = resp.Checks[0]
						h.mu.Unlock()
						return resp.Checks[0], nil
					}
				}
			}
		}
	}
}

// WaitWithTimeout waits with a timeout.
func (h *CheckHandle) WaitWithTimeout(ctx context.Context, timeout time.Duration) (*domain.Check, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return h.Wait(ctx)
}

// OnComplete registers a callback for when the check completes.
// The callback is invoked in a separate goroutine.
func (h *CheckHandle) OnComplete(ctx context.Context, callback func(*domain.Check, error)) {
	go func() {
		result, err := h.Wait(ctx)
		callback(result, err)
	}()
}

// Result returns the cached result if available.
func (h *CheckHandle) Result() (*domain.Check, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.result, h.result != nil
}

// StageHandle provides async operations on a stage.
type StageHandle struct {
	wf     *WorkflowContext
	id     string
	mu     sync.RWMutex
	result *domain.Stage
	err    error
}

// ID returns the stage ID.
func (h *StageHandle) ID() string { return h.id }

// Wait blocks until the stage reaches FINAL state.
func (h *StageHandle) Wait(ctx context.Context) (*domain.Stage, error) {
	if h.wf.usePolling {
		return h.waitPolling(ctx)
	}
	return h.waitStreaming(ctx)
}

// waitPolling polls for stage completion.
func (h *StageHandle) waitPolling(ctx context.Context) (*domain.Stage, error) {
	ticker := time.NewTicker(h.wf.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			resp, err := h.wf.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
				WorkPlanID:    h.wf.workPlanID,
				StageIDs:      []string{h.id},
				IncludeStages: true,
			})
			if err != nil {
				return nil, err
			}
			if len(resp.Stages) > 0 && resp.Stages[0].State == domain.StageStateFinal {
				h.mu.Lock()
				h.result = resp.Stages[0]
				h.mu.Unlock()
				return resp.Stages[0], nil
			}
		}
	}
}

// waitStreaming waits using the event stream.
func (h *StageHandle) waitStreaming(ctx context.Context) (*domain.Stage, error) {
	eventCh := h.wf.subscribeToNode(h.id)
	defer h.wf.unsubscribeFromNode(h.id, eventCh)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event := <-eventCh:
			if se, ok := event.Event.(*pb.WorkPlanEvent_StageEvent); ok {
				if se.StageEvent.NewState == pb.StageState_STAGE_STATE_FINAL {
					// Fetch full stage details
					resp, err := h.wf.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
						WorkPlanID:    h.wf.workPlanID,
						StageIDs:      []string{h.id},
						IncludeStages: true,
					})
					if err != nil {
						return nil, err
					}
					if len(resp.Stages) > 0 {
						h.mu.Lock()
						h.result = resp.Stages[0]
						h.mu.Unlock()
						return resp.Stages[0], nil
					}
				}
			}
		}
	}
}

// WaitWithTimeout waits with a timeout.
func (h *StageHandle) WaitWithTimeout(ctx context.Context, timeout time.Duration) (*domain.Stage, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return h.Wait(ctx)
}

// OnComplete registers a callback for when the stage completes.
func (h *StageHandle) OnComplete(ctx context.Context, callback func(*domain.Stage, error)) {
	go func() {
		result, err := h.Wait(ctx)
		callback(result, err)
	}()
}

// Result returns the cached result if available.
func (h *StageHandle) Result() (*domain.Stage, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.result, h.result != nil
}

// WaitAllStages waits for all stage handles to complete.
func WaitAllStages(ctx context.Context, handles ...*StageHandle) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(handles))

	for _, h := range handles {
		wg.Add(1)
		go func(handle *StageHandle) {
			defer wg.Done()
			if _, err := handle.Wait(ctx); err != nil {
				select {
				case errChan <- err:
				default:
				}
			}
		}(h)
	}

	wg.Wait()
	close(errChan)

	// Return first error
	for err := range errChan {
		if err != nil {
			return err
		}
	}
	return nil
}

// WaitAnyStage waits for any stage handle to complete and returns the first one.
func WaitAnyStage(ctx context.Context, handles ...*StageHandle) (*StageHandle, error) {
	resultChan := make(chan *StageHandle, 1)
	errChan := make(chan error, len(handles))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, h := range handles {
		go func(handle *StageHandle) {
			if _, err := handle.Wait(ctx); err != nil {
				select {
				case errChan <- err:
				default:
				}
			} else {
				select {
				case resultChan <- handle:
					cancel() // Cancel other waiters
				default:
				}
			}
		}(h)
	}

	select {
	case h := <-resultChan:
		return h, nil
	case err := <-errChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// WaitAllChecks waits for all check handles to complete.
func WaitAllChecks(ctx context.Context, handles ...*CheckHandle) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(handles))

	for _, h := range handles {
		wg.Add(1)
		go func(handle *CheckHandle) {
			defer wg.Done()
			if _, err := handle.Wait(ctx); err != nil {
				select {
				case errChan <- err:
				default:
				}
			}
		}(h)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			return err
		}
	}
	return nil
}

// WaitAnyCheck waits for any check handle to complete and returns the first one.
func WaitAnyCheck(ctx context.Context, handles ...*CheckHandle) (*CheckHandle, error) {
	resultChan := make(chan *CheckHandle, 1)
	errChan := make(chan error, len(handles))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, h := range handles {
		go func(handle *CheckHandle) {
			if _, err := handle.Wait(ctx); err != nil {
				select {
				case errChan <- err:
				default:
				}
			} else {
				select {
				case resultChan <- handle:
					cancel()
				default:
				}
			}
		}(h)
	}

	select {
	case h := <-resultChan:
		return h, nil
	case err := <-errChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
