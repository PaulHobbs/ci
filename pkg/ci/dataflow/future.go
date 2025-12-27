package dataflow

import (
	"context"
	"fmt"
)

// Future represents a pending task result.
// Use Field() to access specific fields and create implicit dependencies.
type Future struct {
	wf     *Workflow
	taskID string
}

// Field returns a FutureField that captures a dependency on a specific field.
// When this FutureField is used as an argument to another task, it creates
// an implicit dependency.
func (f *Future) Field(name string) *FutureField {
	return &FutureField{
		future:    f,
		fieldPath: name,
	}
}

// After adds explicit dependencies on other futures.
// Use this when you need to depend on a task but don't need its output.
func (f *Future) After(deps ...*Future) *Future {
	f.wf.mu.Lock()
	defer f.wf.mu.Unlock()

	task := f.wf.tasks[f.taskID]
	for _, dep := range deps {
		task.deps = append(task.deps, dep.taskID)
	}
	return f
}

// Wait blocks until this task completes and returns its result.
func (f *Future) Wait(ctx context.Context) (Result, error) {
	return f.wf.waitForTask(ctx, f.taskID)
}

// TaskID returns the internal task ID (for debugging).
func (f *Future) TaskID() string {
	return f.taskID
}

// FutureField is a reference to a specific field in a Future's result.
// When used as an argument, it creates an implicit dependency.
type FutureField struct {
	future    *Future
	fieldPath string
}

// Result is the output from a RunFunc.
type Result map[string]any

// Get retrieves a value from the result.
func (r Result) Get(key string) any {
	return r[key]
}

// String retrieves a string value.
func (r Result) String(key string) string {
	if v, ok := r[key].(string); ok {
		return v
	}
	return ""
}

// Strings retrieves a string slice value.
func (r Result) Strings(key string) []string {
	if v, ok := r[key].([]string); ok {
		return v
	}
	// Try to convert from []any
	if v, ok := r[key].([]any); ok {
		strs := make([]string, len(v))
		for i, item := range v {
			strs[i] = fmt.Sprintf("%v", item)
		}
		return strs
	}
	return nil
}

// Int retrieves an int value.
func (r Result) Int(key string) int {
	switch v := r[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// Bool retrieves a bool value.
func (r Result) Bool(key string) bool {
	if v, ok := r[key].(bool); ok {
		return v
	}
	return false
}

// Args is the input to a RunFunc.
// May contain literal values or Future/FutureField references.
type Args map[string]any

// Get retrieves a value (after resolution).
func (a Args) Get(key string) any {
	return a[key]
}

// String retrieves a string value.
func (a Args) String(key string) string {
	if v, ok := a[key].(string); ok {
		return v
	}
	return ""
}

// Strings retrieves a string slice value.
func (a Args) Strings(key string) []string {
	if v, ok := a[key].([]string); ok {
		return v
	}
	// Try to convert from []any
	if v, ok := a[key].([]any); ok {
		strs := make([]string, len(v))
		for i, item := range v {
			strs[i] = fmt.Sprintf("%v", item)
		}
		return strs
	}
	return nil
}

// Int retrieves an int value.
func (a Args) Int(key string) int {
	switch v := a[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// Bool retrieves a bool value.
func (a Args) Bool(key string) bool {
	if v, ok := a[key].(bool); ok {
		return v
	}
	return false
}

// Has checks if a key exists.
func (a Args) Has(key string) bool {
	_, ok := a[key]
	return ok
}

// FutureGroup represents multiple futures that can be waited on together.
type FutureGroup struct {
	futures []*Future
}

// Wait blocks until all futures in the group complete.
func (g *FutureGroup) Wait(ctx context.Context) ([]Result, error) {
	results := make([]Result, len(g.futures))
	for i, f := range g.futures {
		result, err := f.Wait(ctx)
		if err != nil {
			return nil, err
		}
		results[i] = result
	}
	return results, nil
}

// All creates a FutureGroup from multiple futures.
func All(futures ...*Future) *FutureGroup {
	return &FutureGroup{futures: futures}
}
