package dataflow

import (
	"context"
	"fmt"
	"sync"
)

// Executor runs tasks in dependency order.
type Executor interface {
	Execute(ctx context.Context, wf *Workflow) error
}

// LocalExecutor runs tasks as goroutines in-process.
type LocalExecutor struct {
	maxConcurrency int
}

// NewLocalExecutor creates a new local executor.
func NewLocalExecutor(maxConcurrency int) *LocalExecutor {
	return &LocalExecutor{maxConcurrency: maxConcurrency}
}

// Execute runs all tasks in the workflow in dependency order.
func (e *LocalExecutor) Execute(ctx context.Context, wf *Workflow) error {
	// Build reverse dependency map (who depends on each task)
	dependents := make(map[string][]string)
	for id, t := range wf.tasks {
		for _, depID := range t.deps {
			dependents[depID] = append(dependents[depID], id)
		}
	}

	// Track remaining dependencies for each task
	remaining := make(map[string]int)
	for id, t := range wf.tasks {
		remaining[id] = len(t.deps)
	}

	// Find initially ready tasks (no dependencies)
	var ready []string
	for id, count := range remaining {
		if count == 0 {
			ready = append(ready, id)
		}
	}

	if len(ready) == 0 && len(wf.tasks) > 0 {
		return fmt.Errorf("cycle detected: no tasks without dependencies")
	}

	// Execution coordination
	var wg sync.WaitGroup
	errChan := make(chan error, len(wf.tasks))
	doneChan := make(chan string, len(wf.tasks))

	// Semaphore for concurrency limiting
	var sem chan struct{}
	if e.maxConcurrency > 0 {
		sem = make(chan struct{}, e.maxConcurrency)
	}

	// Start execution of ready tasks
	runTask := func(taskID string) {
		if sem != nil {
			sem <- struct{}{} // Acquire
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sem != nil {
				defer func() { <-sem }() // Release
			}

			task := wf.tasks[taskID]
			task.markRunning()

			// Resolve args (replace Future/FutureField with actual values)
			resolvedArgs, err := wf.resolveArgs(task.args)
			if err != nil {
				task.markFailed(fmt.Errorf("failed to resolve args: %w", err))
				errChan <- task.err
				doneChan <- taskID
				return
			}

			// Execute the task function
			result, err := task.fn(ctx, resolvedArgs)
			if err != nil {
				task.markFailed(err)
				errChan <- err
			} else {
				task.markCompleted(result)
			}
			doneChan <- taskID
		}()
	}

	// Start initially ready tasks
	for _, id := range ready {
		runTask(id)
	}

	// Process completions and start newly-ready tasks
	completed := 0
	skipped := 0
	var firstErr error

	// skipTask recursively marks a task and its dependents as skipped
	var skipTask func(taskID string)
	skipTask = func(taskID string) {
		skipped++
		// Recursively skip dependents
		for _, depID := range dependents[taskID] {
			remaining[depID]--
			if remaining[depID] == 0 {
				skipTask(depID)
			}
		}
	}

	for completed+skipped < len(wf.tasks) {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-errChan:
			if firstErr == nil {
				firstErr = err
			}

		case taskID := <-doneChan:
			completed++

			// Check if this task failed
			task := wf.tasks[taskID]
			taskFailed := task.err != nil

			// Check dependents
			for _, depID := range dependents[taskID] {
				remaining[depID]--
				if remaining[depID] == 0 {
					if taskFailed || firstErr != nil {
						// Skip this task and its dependents
						skipTask(depID)
					} else {
						runTask(depID)
					}
				}
			}
		}
	}

	wg.Wait()
	return firstErr
}

// resolveArgs replaces Future and FutureField values with resolved results.
func (wf *Workflow) resolveArgs(args Args) (Args, error) {
	resolved := make(Args, len(args))

	for key, val := range args {
		switch v := val.(type) {
		case *Future:
			// Get entire result
			task := wf.tasks[v.taskID]
			result, err := task.getResult()
			if err != nil {
				return nil, fmt.Errorf("dependency %s failed: %w", v.taskID, err)
			}
			resolved[key] = result

		case *FutureField:
			// Get specific field from result
			task := wf.tasks[v.future.taskID]
			result, err := task.getResult()
			if err != nil {
				return nil, fmt.Errorf("dependency %s failed: %w", v.future.taskID, err)
			}
			fieldVal, ok := result[v.fieldPath]
			if !ok {
				return nil, fmt.Errorf("field %q not found in result of %s", v.fieldPath, v.future.taskID)
			}
			resolved[key] = fieldVal

		default:
			resolved[key] = val
		}
	}

	return resolved, nil
}
