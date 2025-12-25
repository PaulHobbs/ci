package ci

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
)

// DAG represents a directed acyclic graph of workflow nodes.
// Supports both static definition (before Execute) and dynamic additions (after Execute).
type DAG struct {
	mu        sync.RWMutex
	nodes     []dagNode
	edges     map[string][]string // sourceID -> targetIDs (target depends on source)
	checks    map[string]*dagCheck
	stages    map[string]*dagStage
	idCounter int

	// Execution state (nil until Execute called)
	execution *WorkflowExecution
}

type dagNode interface {
	nodeID() string
	nodeType() domain.NodeType
}

type dagCheck struct {
	id      string
	kind    string
	options map[string]any
	deps    []string // Check IDs this depends on
}

func (c *dagCheck) nodeID() string            { return c.id }
func (c *dagCheck) nodeType() domain.NodeType { return domain.NodeTypeCheck }

type dagStage struct {
	id          string
	runnerType  string
	execMode    domain.ExecutionMode
	args        map[string]any
	assigns     []string // Check IDs this stage handles
	deps        []string // Stage IDs this depends on
}

func (s *dagStage) nodeID() string            { return s.id }
func (s *dagStage) nodeType() domain.NodeType { return domain.NodeTypeStage }

// NewDAG creates a new DAG builder.
func NewDAG() *DAG {
	return &DAG{
		edges:  make(map[string][]string),
		checks: make(map[string]*dagCheck),
		stages: make(map[string]*dagStage),
	}
}

// CheckNode defines a check in the DAG.
type CheckNode struct {
	dag   *DAG
	check *dagCheck
}

// Check adds a check to the DAG.
func (d *DAG) Check(id string) *CheckNode {
	if id == "" {
		panic("ci: DAG.Check() called with empty id")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	check := &dagCheck{id: id, options: make(map[string]any)}
	d.checks[id] = check
	d.nodes = append(d.nodes, check)
	return &CheckNode{dag: d, check: check}
}

// AutoCheck adds a check with auto-generated ID.
func (d *DAG) AutoCheck(kind string) *CheckNode {
	d.mu.Lock()
	d.idCounter++
	id := fmt.Sprintf("%s-%d", kind, d.idCounter)
	d.mu.Unlock()
	return d.Check(id).Kind(kind)
}

// Kind sets the check kind.
func (n *CheckNode) Kind(kind string) *CheckNode {
	n.check.kind = kind
	return n
}

// Options sets check options.
func (n *CheckNode) Options(opts map[string]any) *CheckNode {
	n.check.options = opts
	return n
}

// Option sets a single check option.
func (n *CheckNode) Option(key string, value any) *CheckNode {
	if n.check.options == nil {
		n.check.options = make(map[string]any)
	}
	n.check.options[key] = value
	return n
}

// DependsOn sets check dependencies.
func (n *CheckNode) DependsOn(checks ...*CheckNode) *CheckNode {
	n.dag.mu.Lock()
	defer n.dag.mu.Unlock()

	for _, c := range checks {
		n.check.deps = append(n.check.deps, c.check.id)
		n.dag.edges[c.check.id] = append(n.dag.edges[c.check.id], n.check.id)
	}
	return n
}

// ID returns the check ID.
func (n *CheckNode) ID() string { return n.check.id }

// StageNode defines a stage in the DAG.
type StageNode struct {
	dag   *DAG
	stage *dagStage
}

// Stage adds a stage to the DAG.
func (d *DAG) Stage(id string) *StageNode {
	if id == "" {
		panic("ci: DAG.Stage() called with empty id")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	stage := &dagStage{id: id, execMode: domain.ExecutionModeSync, args: make(map[string]any)}
	d.stages[id] = stage
	d.nodes = append(d.nodes, stage)
	return &StageNode{dag: d, stage: stage}
}

// AutoStage adds a stage with auto-generated ID.
func (d *DAG) AutoStage(runnerType string) *StageNode {
	d.mu.Lock()
	d.idCounter++
	id := fmt.Sprintf("stage-%s-%d", runnerType, d.idCounter)
	d.mu.Unlock()
	return d.Stage(id).Runner(runnerType)
}

// Runner sets the runner type.
func (n *StageNode) Runner(rt string) *StageNode {
	if rt == "" {
		panic("ci: StageNode.Runner() called with empty runnerType")
	}
	n.stage.runnerType = rt
	return n
}

// Sync sets sync execution mode.
func (n *StageNode) Sync() *StageNode {
	n.stage.execMode = domain.ExecutionModeSync
	return n
}

// Async sets async execution mode.
func (n *StageNode) Async() *StageNode {
	n.stage.execMode = domain.ExecutionModeAsync
	return n
}

// Args sets stage arguments.
func (n *StageNode) Args(args map[string]any) *StageNode {
	n.stage.args = args
	return n
}

// Arg sets a single stage argument.
func (n *StageNode) Arg(key string, value any) *StageNode {
	if n.stage.args == nil {
		n.stage.args = make(map[string]any)
	}
	n.stage.args[key] = value
	return n
}

// Assigns sets which checks this stage is responsible for.
func (n *StageNode) Assigns(checks ...*CheckNode) *StageNode {
	for _, c := range checks {
		n.stage.assigns = append(n.stage.assigns, c.check.id)
	}
	return n
}

// AssignsIDs sets which checks (by ID) this stage is responsible for.
func (n *StageNode) AssignsIDs(checkIDs ...string) *StageNode {
	n.stage.assigns = append(n.stage.assigns, checkIDs...)
	return n
}

// After sets stage dependencies (this stage runs after the given stages).
func (n *StageNode) After(stages ...*StageNode) *StageNode {
	n.dag.mu.Lock()
	defer n.dag.mu.Unlock()

	for _, s := range stages {
		n.stage.deps = append(n.stage.deps, s.stage.id)
		n.dag.edges[s.stage.id] = append(n.dag.edges[s.stage.id], n.stage.id)
	}
	return n
}

// AfterIDs sets stage dependencies by ID.
func (n *StageNode) AfterIDs(stageIDs ...string) *StageNode {
	n.dag.mu.Lock()
	defer n.dag.mu.Unlock()

	for _, id := range stageIDs {
		n.stage.deps = append(n.stage.deps, id)
		n.dag.edges[id] = append(n.dag.edges[id], n.stage.id)
	}
	return n
}

// ID returns the stage ID.
func (n *StageNode) ID() string { return n.stage.id }

// Execute materializes the DAG into the orchestrator and starts execution.
func (d *DAG) Execute(ctx context.Context, orch *service.OrchestratorService) (*WorkflowExecution, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Create work plan
	wp, err := orch.CreateWorkPlan(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create work plan: %w", err)
	}

	// Build WriteNodesRequest
	req := &service.WriteNodesRequest{WorkPlanID: wp.ID}

	// Add checks
	for _, check := range d.checks {
		cw := buildCheckWrite(check)
		req.Checks = append(req.Checks, cw)
	}

	// Add stages
	for _, stage := range d.stages {
		sw := buildStageWrite(stage)
		req.Stages = append(req.Stages, sw)
	}

	// Execute
	if _, err := orch.WriteNodes(ctx, req); err != nil {
		return nil, fmt.Errorf("failed to write nodes: %w", err)
	}

	exec := &WorkflowExecution{
		orchestrator: orch,
		workPlanID:   wp.ID,
		dag:          d,
		pollInterval: 100 * time.Millisecond,
	}

	d.execution = exec

	return exec, nil
}

// AddCheck adds a check to a running DAG (hybrid mode).
// If the DAG hasn't been executed yet, this behaves like Check().
func (d *DAG) AddCheck(ctx context.Context, id string) (*CheckNode, error) {
	d.mu.Lock()
	exec := d.execution
	d.mu.Unlock()

	if exec == nil {
		// Pre-execution: just build the graph
		return d.Check(id), nil
	}

	// Post-execution: immediately write to orchestrator
	check := &dagCheck{id: id, options: make(map[string]any)}

	d.mu.Lock()
	d.checks[id] = check
	d.nodes = append(d.nodes, check)
	d.mu.Unlock()

	// Write to running work plan
	cw := buildCheckWrite(check)
	_, err := exec.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: exec.workPlanID,
		Checks:     []*service.CheckWrite{cw},
	})
	if err != nil {
		return nil, err
	}

	return &CheckNode{dag: d, check: check}, nil
}

// AddStage adds a stage to a running DAG (hybrid mode).
// If the DAG hasn't been executed yet, this behaves like Stage().
func (d *DAG) AddStage(ctx context.Context, id string) (*StageNode, error) {
	d.mu.Lock()
	exec := d.execution
	d.mu.Unlock()

	if exec == nil {
		// Pre-execution: just build the graph
		return d.Stage(id), nil
	}

	// Post-execution: immediately write to orchestrator
	stage := &dagStage{id: id, execMode: domain.ExecutionModeSync, args: make(map[string]any)}

	d.mu.Lock()
	d.stages[id] = stage
	d.nodes = append(d.nodes, stage)
	d.mu.Unlock()

	// Note: The stage won't be written until CommitStage is called
	// This allows setting properties before writing

	return &StageNode{dag: d, stage: stage}, nil
}

// CommitNode writes a dynamically-added node to the orchestrator.
// Use this after setting all properties on a node added via AddCheck/AddStage.
func (d *DAG) CommitNode(ctx context.Context, node interface{}) error {
	d.mu.RLock()
	exec := d.execution
	d.mu.RUnlock()

	if exec == nil {
		return nil // Not executing yet, no-op
	}

	switch n := node.(type) {
	case *CheckNode:
		d.mu.RLock()
		check := d.checks[n.check.id]
		d.mu.RUnlock()
		if check == nil {
			return fmt.Errorf("check %s not found", n.check.id)
		}
		cw := buildCheckWrite(check)
		_, err := exec.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
			WorkPlanID: exec.workPlanID,
			Checks:     []*service.CheckWrite{cw},
		})
		return err

	case *StageNode:
		d.mu.RLock()
		stage := d.stages[n.stage.id]
		d.mu.RUnlock()
		if stage == nil {
			return fmt.Errorf("stage %s not found", n.stage.id)
		}
		sw := buildStageWrite(stage)
		_, err := exec.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
			WorkPlanID: exec.workPlanID,
			Stages:     []*service.StageWrite{sw},
		})
		return err

	default:
		return fmt.Errorf("unknown node type: %T", node)
	}
}

// buildCheckWrite converts a dagCheck to a CheckWrite.
func buildCheckWrite(check *dagCheck) *service.CheckWrite {
	cw := &service.CheckWrite{
		ID:      check.id,
		Kind:    check.kind,
		Options: check.options,
	}
	state := domain.CheckStatePlanned
	cw.State = &state

	if len(check.deps) > 0 {
		refs := make([]domain.DependencyRef, len(check.deps))
		for i, dep := range check.deps {
			refs[i] = domain.DependencyRef{TargetType: domain.NodeTypeCheck, TargetID: dep}
		}
		cw.Dependencies = &domain.DependencyGroup{Predicate: domain.PredicateAND, Dependencies: refs}
	}

	return cw
}

// buildStageWrite converts a dagStage to a StageWrite.
func buildStageWrite(stage *dagStage) *service.StageWrite {
	sw := &service.StageWrite{
		ID:         stage.id,
		RunnerType: stage.runnerType,
		Args:       stage.args,
	}
	sw.ExecutionMode = &stage.execMode
	stageState := domain.StageStatePlanned
	sw.State = &stageState

	for _, checkID := range stage.assigns {
		sw.Assignments = append(sw.Assignments, domain.Assignment{
			TargetCheckID: checkID,
			GoalState:     domain.CheckStateFinal,
		})
	}

	if len(stage.deps) > 0 {
		refs := make([]domain.DependencyRef, len(stage.deps))
		for i, dep := range stage.deps {
			refs[i] = domain.DependencyRef{TargetType: domain.NodeTypeStage, TargetID: dep}
		}
		sw.Dependencies = &domain.DependencyGroup{Predicate: domain.PredicateAND, Dependencies: refs}
	}

	return sw
}

// WorkflowExecution represents a running workflow.
type WorkflowExecution struct {
	orchestrator *service.OrchestratorService
	workPlanID   string
	dag          *DAG
	pollInterval time.Duration
}

// WorkPlanID returns the work plan ID.
func (e *WorkflowExecution) WorkPlanID() string { return e.workPlanID }

// DAG returns the underlying DAG.
func (e *WorkflowExecution) DAG() *DAG { return e.dag }

// Orchestrator returns the underlying orchestrator.
func (e *WorkflowExecution) Orchestrator() *service.OrchestratorService { return e.orchestrator }

// WaitForCompletion waits for all stages to complete.
func (e *WorkflowExecution) WaitForCompletion(ctx context.Context) error {
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			resp, err := e.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
				WorkPlanID:    e.workPlanID,
				IncludeStages: true,
			})
			if err != nil {
				return err
			}

			if len(resp.Stages) == 0 {
				continue // No stages yet
			}

			allDone := true
			for _, stage := range resp.Stages {
				if stage.State != domain.StageStateFinal {
					allDone = false
					break
				}
			}
			if allDone {
				return nil
			}
		}
	}
}

// WaitForCheck waits for a specific check to complete.
func (e *WorkflowExecution) WaitForCheck(ctx context.Context, checkID string) (*domain.Check, error) {
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			resp, err := e.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
				WorkPlanID:    e.workPlanID,
				CheckIDs:      []string{checkID},
				IncludeChecks: true,
			})
			if err != nil {
				return nil, err
			}
			if len(resp.Checks) > 0 && resp.Checks[0].State == domain.CheckStateFinal {
				return resp.Checks[0], nil
			}
		}
	}
}

// WaitForStage waits for a specific stage to complete.
func (e *WorkflowExecution) WaitForStage(ctx context.Context, stageID string) (*domain.Stage, error) {
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			resp, err := e.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
				WorkPlanID:    e.workPlanID,
				StageIDs:      []string{stageID},
				IncludeStages: true,
			})
			if err != nil {
				return nil, err
			}
			if len(resp.Stages) > 0 && resp.Stages[0].State == domain.StageStateFinal {
				return resp.Stages[0], nil
			}
		}
	}
}

// GetCheckResult retrieves the result for a check.
func (e *WorkflowExecution) GetCheckResult(ctx context.Context, checkID string) (*domain.Check, error) {
	resp, err := e.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    e.workPlanID,
		CheckIDs:      []string{checkID},
		IncludeChecks: true,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Checks) == 0 {
		return nil, fmt.Errorf("check %s not found", checkID)
	}
	return resp.Checks[0], nil
}

// GetStageResult retrieves the result for a stage.
func (e *WorkflowExecution) GetStageResult(ctx context.Context, stageID string) (*domain.Stage, error) {
	resp, err := e.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    e.workPlanID,
		StageIDs:      []string{stageID},
		IncludeStages: true,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Stages) == 0 {
		return nil, fmt.Errorf("stage %s not found", stageID)
	}
	return resp.Stages[0], nil
}

// QueryChecks queries checks with optional state filter.
func (e *WorkflowExecution) QueryChecks(ctx context.Context, states ...domain.CheckState) ([]*domain.Check, error) {
	resp, err := e.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    e.workPlanID,
		CheckStates:   states,
		IncludeChecks: true,
	})
	if err != nil {
		return nil, err
	}
	return resp.Checks, nil
}

// QueryStages queries stages with optional state filter.
func (e *WorkflowExecution) QueryStages(ctx context.Context, states ...domain.StageState) ([]*domain.Stage, error) {
	resp, err := e.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    e.workPlanID,
		StageStates:   states,
		IncludeStages: true,
	})
	if err != nil {
		return nil, err
	}
	return resp.Stages, nil
}
