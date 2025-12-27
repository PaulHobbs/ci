// Package dataflow provides an async-io style API for defining workflows
// where dependencies are implicit through data flow.
//
// Example:
//
//	wf := dataflow.NewWorkflow()
//	trigger := wf.Run(func(ctx context.Context, args Args) (Result, error) {
//	    return Result{"packages": []string{"pkg/a", "pkg/b"}}, nil
//	}, Args{"base": "main"})
//
//	// Dependency is implicit because we use trigger.Field()
//	build := wf.Run(func(ctx context.Context, args Args) (Result, error) {
//	    packages := args.Strings("packages")
//	    return Result{"artifacts": buildAll(packages)}, nil
//	}, Args{"packages": trigger.Field("packages")})
//
//	wf.Execute(ctx)
package dataflow

import (
	"context"
	"sync"
)

// RunFunc is the signature for inline task functions.
// It receives resolved arguments and returns a result map.
type RunFunc func(ctx context.Context, args Args) (Result, error)

// task represents a unit of work in the workflow DAG.
type task struct {
	id   string
	name string   // optional human-readable name
	fn   RunFunc
	args Args     // may contain Future/FutureField values
	deps []string // task IDs this depends on (computed from args)

	// Execution state
	mu       sync.RWMutex
	state    taskState
	result   Result
	err      error
	doneChan chan struct{}
}

type taskState int

const (
	taskStatePending taskState = iota
	taskStateRunning
	taskStateCompleted
	taskStateFailed
)

// newTask creates a new task with the given ID and function.
func newTask(id string, fn RunFunc, args Args) *task {
	return &task{
		id:       id,
		fn:       fn,
		args:     args,
		state:    taskStatePending,
		doneChan: make(chan struct{}),
	}
}

// wait blocks until the task completes (success or failure).
func (t *task) wait(ctx context.Context) error {
	select {
	case <-t.doneChan:
		t.mu.RLock()
		defer t.mu.RUnlock()
		return t.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// markRunning transitions the task to running state.
func (t *task) markRunning() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = taskStateRunning
}

// markCompleted transitions the task to completed state with result.
func (t *task) markCompleted(result Result) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = taskStateCompleted
	t.result = result
	close(t.doneChan)
}

// markFailed transitions the task to failed state with error.
func (t *task) markFailed(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = taskStateFailed
	t.err = err
	close(t.doneChan)
}

// isComplete returns true if task is completed or failed.
func (t *task) isComplete() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.state == taskStateCompleted || t.state == taskStateFailed
}

// getResult returns the task result (only valid after completion).
func (t *task) getResult() (Result, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.result, t.err
}
