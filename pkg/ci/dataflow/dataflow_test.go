package dataflow

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSimpleWorkflow(t *testing.T) {
	wf := NewWorkflow()

	// Simple task with no dependencies
	task1 := wf.Run(func(ctx context.Context, args Args) (Result, error) {
		return Result{"value": 42}, nil
	}, Args{})

	if err := wf.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	result, err := wf.Result(task1)
	if err != nil {
		t.Fatalf("Result failed: %v", err)
	}

	if result.Int("value") != 42 {
		t.Errorf("expected 42, got %d", result.Int("value"))
	}
}

func TestImplicitDependency(t *testing.T) {
	wf := NewWorkflow()

	// Task 1: produces data
	task1 := wf.Run(func(ctx context.Context, args Args) (Result, error) {
		return Result{"packages": []string{"pkg/a", "pkg/b", "pkg/c"}}, nil
	}, Args{})

	// Task 2: depends on task1 through Field()
	task2 := wf.Run(func(ctx context.Context, args Args) (Result, error) {
		packages := args.Strings("packages")
		return Result{"count": len(packages)}, nil
	}, Args{"packages": task1.Field("packages")})

	if err := wf.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	result, err := wf.Result(task2)
	if err != nil {
		t.Fatalf("Result failed: %v", err)
	}

	if result.Int("count") != 3 {
		t.Errorf("expected 3, got %d", result.Int("count"))
	}
}

func TestParallelExecution(t *testing.T) {
	wf := NewWorkflow()
	var execOrder []int
	var orderMu atomic.Int32

	// Task 1: produces data
	task1 := wf.Run(func(ctx context.Context, args Args) (Result, error) {
		time.Sleep(10 * time.Millisecond)
		return Result{"data": "from-task1"}, nil
	}, Args{}, WithName("trigger"))

	// Task 2 and 3: both depend on task1, should run in parallel
	task2 := wf.Run(func(ctx context.Context, args Args) (Result, error) {
		idx := orderMu.Add(1)
		time.Sleep(50 * time.Millisecond)
		execOrder = append(execOrder, int(idx))
		return Result{"from": "task2"}, nil
	}, Args{"input": task1.Field("data")}, WithName("build"))

	task3 := wf.Run(func(ctx context.Context, args Args) (Result, error) {
		idx := orderMu.Add(1)
		time.Sleep(50 * time.Millisecond)
		execOrder = append(execOrder, int(idx))
		return Result{"from": "task3"}, nil
	}, Args{"input": task1.Field("data")}, WithName("test"))

	// Task 4: depends on both task2 and task3
	wf.Run(func(ctx context.Context, args Args) (Result, error) {
		return Result{"done": true}, nil
	}, Args{
		"build": task2.Field("from"),
		"test":  task3.Field("from"),
	}, WithName("deploy"))

	start := time.Now()
	if err := wf.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	elapsed := time.Since(start)

	// If running in parallel, should take ~60ms (10 trigger + 50 parallel)
	// If sequential, would take ~110ms (10 + 50 + 50)
	if elapsed > 100*time.Millisecond {
		t.Errorf("expected parallel execution, but took %v", elapsed)
	}
}

func TestConcurrencyLimit(t *testing.T) {
	wf := NewWorkflow(WithMaxConcurrency(1))
	var running atomic.Int32
	var maxConcurrent atomic.Int32

	for i := 0; i < 5; i++ {
		wf.Run(func(ctx context.Context, args Args) (Result, error) {
			curr := running.Add(1)
			if curr > maxConcurrent.Load() {
				maxConcurrent.Store(curr)
			}
			time.Sleep(10 * time.Millisecond)
			running.Add(-1)
			return Result{}, nil
		}, Args{})
	}

	if err := wf.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if maxConcurrent.Load() != 1 {
		t.Errorf("expected max concurrency 1, got %d", maxConcurrent.Load())
	}
}

func TestErrorPropagation(t *testing.T) {
	wf := NewWorkflow()

	expectedErr := errors.New("task failed")

	wf.Run(func(ctx context.Context, args Args) (Result, error) {
		return nil, expectedErr
	}, Args{})

	err := wf.Execute(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestDependencyOnFailedTask(t *testing.T) {
	wf := NewWorkflow()

	task1 := wf.Run(func(ctx context.Context, args Args) (Result, error) {
		return nil, errors.New("task1 failed")
	}, Args{})

	// Task 2 depends on task1, should not run
	var task2Ran bool
	wf.Run(func(ctx context.Context, args Args) (Result, error) {
		task2Ran = true
		return Result{}, nil
	}, Args{"input": task1.Field("data")})

	err := wf.Execute(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	if task2Ran {
		t.Error("task2 should not have run")
	}
}

func TestContextCancellation(t *testing.T) {
	wf := NewWorkflow()

	ctx, cancel := context.WithCancel(context.Background())

	wf.Run(func(ctx context.Context, args Args) (Result, error) {
		// Cancel while task is running
		cancel()
		time.Sleep(100 * time.Millisecond)
		return Result{}, nil
	}, Args{})

	err := wf.Execute(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestValidateCycleDetection(t *testing.T) {
	// This is a bit tricky since we infer deps from args
	// We'd need After() to create a cycle manually
	// For now just test valid case
	wf := NewWorkflow()

	t1 := wf.Run(func(ctx context.Context, args Args) (Result, error) {
		return Result{"x": 1}, nil
	}, Args{})

	wf.Run(func(ctx context.Context, args Args) (Result, error) {
		return Result{"y": 2}, nil
	}, Args{"x": t1.Field("x")})

	if err := wf.Validate(); err != nil {
		t.Errorf("Validate failed: %v", err)
	}
}

func TestArgsHelpers(t *testing.T) {
	args := Args{
		"str":     "hello",
		"strs":    []string{"a", "b"},
		"num":     42,
		"num64":   int64(100),
		"float":   3.14,
		"bool":    true,
		"missing": nil,
	}

	if args.String("str") != "hello" {
		t.Error("String failed")
	}
	if len(args.Strings("strs")) != 2 {
		t.Error("Strings failed")
	}
	if args.Int("num") != 42 {
		t.Error("Int failed")
	}
	if args.Int("num64") != 100 {
		t.Error("Int64 conversion failed")
	}
	if args.Bool("bool") != true {
		t.Error("Bool failed")
	}
	if args.String("missing") != "" {
		t.Error("missing string should be empty")
	}
}

func TestResultHelpers(t *testing.T) {
	result := Result{
		"str":   "world",
		"count": 5,
		"items": []string{"x", "y"},
	}

	if result.String("str") != "world" {
		t.Error("String failed")
	}
	if result.Int("count") != 5 {
		t.Error("Int failed")
	}
	if len(result.Strings("items")) != 2 {
		t.Error("Strings failed")
	}
}

func TestFanOut(t *testing.T) {
	wf := NewWorkflow()

	items := []any{"a", "b", "c"}
	futures := wf.FanOut(items, func(item any) (RunFunc, Args) {
		letter := item.(string)
		return func(ctx context.Context, args Args) (Result, error) {
			return Result{"upper": letter + letter}, nil
		}, Args{}
	})

	if len(futures) != 3 {
		t.Fatalf("expected 3 futures, got %d", len(futures))
	}

	if err := wf.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	results, err := All(futures...).Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	expected := []string{"aa", "bb", "cc"}
	for i, r := range results {
		if r.String("upper") != expected[i] {
			t.Errorf("expected %s, got %s", expected[i], r.String("upper"))
		}
	}
}

func TestExplicitDependency(t *testing.T) {
	wf := NewWorkflow()
	var task1Done atomic.Bool

	task1 := wf.Run(func(ctx context.Context, args Args) (Result, error) {
		time.Sleep(10 * time.Millisecond)
		task1Done.Store(true)
		return Result{}, nil
	}, Args{})

	// task2 depends on task1 via After(), not data flow
	wf.Run(func(ctx context.Context, args Args) (Result, error) {
		if !task1Done.Load() {
			return nil, errors.New("task1 should have completed first")
		}
		return Result{}, nil
	}, Args{}, DependsOn(task1))

	if err := wf.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestWorkflowReuse(t *testing.T) {
	wf := NewWorkflow()

	wf.Run(func(ctx context.Context, args Args) (Result, error) {
		return Result{}, nil
	}, Args{})

	if err := wf.Execute(context.Background()); err != nil {
		t.Fatalf("first Execute failed: %v", err)
	}

	err := wf.Execute(context.Background())
	if err == nil {
		t.Error("expected error on second Execute")
	}
}

func TestPrintDAG(t *testing.T) {
	wf := NewWorkflow()

	t1 := wf.Run(func(ctx context.Context, args Args) (Result, error) {
		return Result{"x": 1}, nil
	}, Args{}, WithName("first"))

	wf.Run(func(ctx context.Context, args Args) (Result, error) {
		return Result{"y": 2}, nil
	}, Args{"x": t1.Field("x")}, WithName("second"))

	// Just verify it doesn't panic
	var buf strings.Builder
	wf.PrintDAG(&buf)
	output := buf.String()

	if !strings.Contains(output, "first") {
		t.Error("expected 'first' in output")
	}
	if !strings.Contains(output, "second") {
		t.Error("expected 'second' in output")
	}
}
