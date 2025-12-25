// Package main implements the GoTester runner.
// This runner executes Go tests and parses JSON output.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/turboci-lite/cmd/ci/runners/common"
	pb "github.com/example/turboci-lite/gen/turboci/v1"
)

var (
	runnerID         = flag.String("runner-id", "tester-runner-1", "Runner ID")
	listenAddr       = flag.String("listen", ":50064", "Address to listen on")
	orchestratorAddr = flag.String("orchestrator", "localhost:50051", "Orchestrator address")
	maxConcurrent    = flag.Int("max-concurrent", 4, "Maximum concurrent tests")
	repoRoot         = flag.String("repo-root", ".", "Repository root directory")
)

func main() {
	flag.Parse()

	runner := common.NewBaseRunner(*runnerID, "go_tester", *listenAddr, *orchestratorAddr, *maxConcurrent)

	tester := &TesterRunner{
		BaseRunner: runner,
		repoRoot:   *repoRoot,
	}
	runner.SetRunHandler(tester.HandleRun)

	go func() {
		if err := runner.StartServer(); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	ctx := context.Background()
	if err := runner.Register(ctx); err != nil {
		log.Fatalf("Failed to register: %v", err)
	}

	go runner.HeartbeatLoop(ctx, 60*time.Second)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down tester runner...")
	runner.Shutdown(ctx)
}

// TesterRunner handles running Go tests.
type TesterRunner struct {
	*common.BaseRunner
	repoRoot string
}

// TestEvent represents a single test event from go test -json output.
type TestEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test"`
	Output  string    `json:"Output"`
	Elapsed float64   `json:"Elapsed"`
}

// TestResult contains aggregated test results.
type TestResult struct {
	Package    string       `json:"package"`
	Passed     int          `json:"passed"`
	Failed     int          `json:"failed"`
	Skipped    int          `json:"skipped"`
	DurationMs int64        `json:"duration_ms"`
	Tests      []TestDetail `json:"tests"`
	RawOutput  string       `json:"raw_output"`
}

// TestDetail contains details about a single test.
type TestDetail struct {
	Name     string  `json:"name"`
	Status   string  `json:"status"` // pass, fail, skip
	Duration float64 `json:"duration"`
	Output   string  `json:"output"`
}

// HandleRun processes a test request.
func (t *TesterRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	if len(req.AssignedCheckIds) == 0 {
		return nil, fmt.Errorf("no assigned check IDs")
	}

	checkID := req.AssignedCheckIds[0]
	log.Printf("[tester] Starting tests for check=%s", checkID)

	// Extract test parameters from args
	var packagePath, testDir, modulePath string
	if req.Args != nil {
		if v := req.Args.Fields["package"]; v != nil {
			packagePath = v.GetStringValue()
		}
		if v := req.Args.Fields["dir"]; v != nil {
			testDir = v.GetStringValue()
		}
		if v := req.Args.Fields["module"]; v != nil {
			modulePath = v.GetStringValue()
		}
	}

	if packagePath == "" {
		return t.createFailureResponse(req, "missing package parameter")
	}

	// Determine working directory
	workDir := t.repoRoot
	testTarget := packagePath

	// If we have a directory, use it
	if testDir != "" {
		workDir = testDir
		testTarget = "."
	}

	// Run tests with JSON output
	startTime := time.Now()
	cmd := exec.CommandContext(ctx, "go", "test", "-v", "-json", "-count=1", testTarget)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	// Parse test output
	result := t.parseTestOutput(output, packagePath)
	result.DurationMs = duration.Milliseconds()

	// Determine if tests failed
	testFailed := err != nil || result.Failed > 0

	// Build result data
	resultData := map[string]any{
		"package":     packagePath,
		"module":      modulePath,
		"work_dir":    workDir,
		"passed":      result.Passed,
		"failed":      result.Failed,
		"skipped":     result.Skipped,
		"duration_ms": result.DurationMs,
		"test_count":  len(result.Tests),
		"success":     !testFailed,
	}

	log.Printf("[tester] Tests completed for %s: passed=%d failed=%d skipped=%d",
		packagePath, result.Passed, result.Failed, result.Skipped)

	if testFailed {
		return common.NewResponse().
			FailCheck(checkID, fmt.Sprintf("tests failed: %d failures", result.Failed), resultData).
			Build(), nil
	}

	return common.NewResponse().
		FinalizeCheck(checkID, resultData).
		Build(), nil
}

func (t *TesterRunner) parseTestOutput(output []byte, packagePath string) *TestResult {
	result := &TestResult{
		Package:   packagePath,
		RawOutput: string(output),
	}

	// Track test outputs
	testOutputs := make(map[string]string)
	testDurations := make(map[string]float64)

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event TestEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// Not JSON, skip
			continue
		}

		// Only process test-level events (not package-level)
		if event.Test == "" {
			continue
		}

		switch event.Action {
		case "output":
			testOutputs[event.Test] += event.Output
		case "pass":
			result.Passed++
			testDurations[event.Test] = event.Elapsed
			result.Tests = append(result.Tests, TestDetail{
				Name:     event.Test,
				Status:   "pass",
				Duration: event.Elapsed,
				Output:   testOutputs[event.Test],
			})
		case "fail":
			result.Failed++
			testDurations[event.Test] = event.Elapsed
			result.Tests = append(result.Tests, TestDetail{
				Name:     event.Test,
				Status:   "fail",
				Duration: event.Elapsed,
				Output:   testOutputs[event.Test],
			})
		case "skip":
			result.Skipped++
			result.Tests = append(result.Tests, TestDetail{
				Name:     event.Test,
				Status:   "skip",
				Duration: event.Elapsed,
				Output:   testOutputs[event.Test],
			})
		}
	}

	return result
}

func (t *TesterRunner) createFailureResponse(req *pb.RunRequest, msg string) (*pb.RunResponse, error) {
	log.Printf("[tester] Error: %s", msg)
	return common.NewResponse().
		FailCheck(req.AssignedCheckIds[0], msg, nil).
		Build(), nil
}
