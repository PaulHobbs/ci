package dataflow

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// Workflow builds and executes a DAG of tasks with implicit dependencies.
type Workflow struct {
	mu        sync.RWMutex
	tasks     map[string]*task
	taskOrder []string // Maintains insertion order
	idCounter int
	executed  bool
	executor  Executor
	config    workflowConfig
}

// NewWorkflow creates a new workflow.
func NewWorkflow(opts ...Option) *Workflow {
	wf := &Workflow{
		tasks: make(map[string]*task),
	}

	for _, opt := range opts {
		opt(&wf.config)
	}

	wf.executor = NewLocalExecutor(wf.config.maxConcurrency)

	return wf
}

// Run adds a task to the workflow and returns a Future for its result.
// Dependencies are automatically inferred from any Future or FutureField
// values in the args.
func (w *Workflow) Run(fn RunFunc, args Args, opts ...RunOption) *Future {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.executed {
		panic("dataflow: cannot add tasks after Execute()")
	}

	// Apply run options
	cfg := &runConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Generate task ID
	w.idCounter++
	taskID := fmt.Sprintf("task-%d", w.idCounter)
	if cfg.name != "" {
		taskID = fmt.Sprintf("%s-%d", cfg.name, w.idCounter)
	}

	// Create task
	t := newTask(taskID, fn, args)
	if cfg.name != "" {
		t.name = cfg.name
	}

	// Infer dependencies from args
	deps := w.inferDeps(args)
	deps = append(deps, cfg.explicitDeps...)
	t.deps = deps

	// Register task
	w.tasks[taskID] = t
	w.taskOrder = append(w.taskOrder, taskID)

	return &Future{wf: w, taskID: taskID}
}

// inferDeps extracts dependencies from args containing Future/FutureField.
func (w *Workflow) inferDeps(args Args) []string {
	var deps []string
	seen := make(map[string]bool)

	for _, val := range args {
		switch v := val.(type) {
		case *Future:
			if !seen[v.taskID] {
				deps = append(deps, v.taskID)
				seen[v.taskID] = true
			}
		case *FutureField:
			if !seen[v.future.taskID] {
				deps = append(deps, v.future.taskID)
				seen[v.future.taskID] = true
			}
		}
	}

	return deps
}

// Execute runs all tasks in dependency order.
// Returns the first error encountered, if any.
func (w *Workflow) Execute(ctx context.Context) error {
	w.mu.Lock()
	if w.executed {
		w.mu.Unlock()
		return fmt.Errorf("dataflow: workflow already executed")
	}
	w.executed = true
	w.mu.Unlock()

	return w.executor.Execute(ctx, w)
}

// Result returns the result of a completed task.
// Must be called after Execute().
func (w *Workflow) Result(f *Future) (Result, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	task, ok := w.tasks[f.taskID]
	if !ok {
		return nil, fmt.Errorf("task %s not found", f.taskID)
	}

	return task.getResult()
}

// waitForTask blocks until a specific task completes.
func (w *Workflow) waitForTask(ctx context.Context, taskID string) (Result, error) {
	w.mu.RLock()
	task, ok := w.tasks[taskID]
	w.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	if err := task.wait(ctx); err != nil {
		return nil, err
	}

	return task.getResult()
}

// MustResult returns the result or panics on error.
// Useful for testing.
func (w *Workflow) MustResult(f *Future) Result {
	r, err := w.Result(f)
	if err != nil {
		panic(err)
	}
	return r
}

// Validate checks the workflow for errors before execution.
func (w *Workflow) Validate() error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Check for missing dependencies
	for id, t := range w.tasks {
		for _, depID := range t.deps {
			if _, ok := w.tasks[depID]; !ok {
				return fmt.Errorf("task %s depends on unknown task %s", id, depID)
			}
		}
	}

	// Check for cycles using DFS
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var hasCycle func(id string) bool
	hasCycle = func(id string) bool {
		visited[id] = true
		recStack[id] = true

		task := w.tasks[id]
		for _, depID := range task.deps {
			if !visited[depID] {
				if hasCycle(depID) {
					return true
				}
			} else if recStack[depID] {
				return true
			}
		}

		recStack[id] = false
		return false
	}

	for id := range w.tasks {
		if !visited[id] {
			if hasCycle(id) {
				return fmt.Errorf("cycle detected in workflow")
			}
		}
	}

	return nil
}

// PrintDAG writes a textual representation of the task DAG.
func (w *Workflow) PrintDAG(out io.Writer) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	fmt.Fprintln(out, "Workflow DAG:")
	for _, id := range w.taskOrder {
		t := w.tasks[id]
		name := t.name
		if name == "" {
			name = id
		}
		if len(t.deps) == 0 {
			fmt.Fprintf(out, "  %s (no dependencies)\n", name)
		} else {
			fmt.Fprintf(out, "  %s -> depends on: %v\n", name, t.deps)
		}
	}
}

// TaskCount returns the number of tasks in the workflow.
func (w *Workflow) TaskCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.tasks)
}

// FanOut creates multiple tasks in parallel, one for each item.
// All returned futures depend on the same inputs but run independently.
func (w *Workflow) FanOut(items []any, builder func(item any) (RunFunc, Args)) []*Future {
	futures := make([]*Future, len(items))
	for i, item := range items {
		fn, args := builder(item)
		futures[i] = w.Run(fn, args)
	}
	return futures
}
