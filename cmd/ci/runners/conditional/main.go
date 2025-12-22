// Package main implements the ConditionalTester runner.
// This runner runs E2E tests only if upstream checks have passed.
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

	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/cmd/ci/runners/common"
)

var (
	runnerID         = flag.String("runner-id", "conditional-runner-1", "Runner ID")
	listenAddr       = flag.String("listen", ":50065", "Address to listen on")
	orchestratorAddr = flag.String("orchestrator", "localhost:50051", "Orchestrator address")
	maxConcurrent    = flag.Int("max-concurrent", 2, "Maximum concurrent tests")
	repoRoot         = flag.String("repo-root", ".", "Repository root directory")
)

func main() {
	flag.Parse()

	runner := common.NewBaseRunner(*runnerID, "conditional_tester", *listenAddr, *orchestratorAddr, *maxConcurrent)

	conditional := &ConditionalRunner{
		BaseRunner: runner,
		repoRoot:   *repoRoot,
	}
	runner.SetRunHandler(conditional.HandleRun)

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

	log.Println("Shutting down conditional runner...")
	runner.Shutdown(ctx)
}

// ConditionalRunner handles running E2E tests conditionally.
type ConditionalRunner struct {
	*common.BaseRunner
	repoRoot string
}

// HandleRun processes a conditional test request.
func (c *ConditionalRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	if len(req.AssignedCheckIds) == 0 {
		return nil, fmt.Errorf("no assigned check IDs")
	}

	checkID := req.AssignedCheckIds[0]
	log.Printf("[conditional] Starting conditional test for check=%s", checkID)

	// Extract parameters from args
	var testPath, testName string
	var dependsOnChecks []string

	if req.Args != nil {
		if v := req.Args.Fields["test_path"]; v != nil {
			testPath = v.GetStringValue()
		}
		if v := req.Args.Fields["name"]; v != nil {
			testName = v.GetStringValue()
		}
		if v := req.Args.Fields["depends_on_checks"]; v != nil {
			if list := v.GetListValue(); list != nil {
				for _, item := range list.Values {
					dependsOnChecks = append(dependsOnChecks, item.GetStringValue())
				}
			}
		}
	}

	if testPath == "" {
		return c.createFailureResponse(req, "missing test_path parameter")
	}

	// Check if upstream dependencies have passed
	upstreamOK, failedChecks := c.checkUpstreamDependencies(ctx, req.WorkPlanId, dependsOnChecks)

	if !upstreamOK {
		log.Printf("[conditional] Skipping %s due to failed upstream checks: %v", testName, failedChecks)
		return c.createSkipResponse(req, failedChecks)
	}

	log.Printf("[conditional] All upstream checks passed, running E2E test %s", testName)

	// Run the E2E test
	return c.runE2ETest(ctx, req, testPath, testName)
}

func (c *ConditionalRunner) checkUpstreamDependencies(ctx context.Context, workPlanID string, checkIDs []string) (bool, []string) {
	if len(checkIDs) == 0 {
		return true, nil
	}

	var failedChecks []string

	for _, checkID := range checkIDs {
		// Query the check
		resp, err := c.Orchestrator.QueryNodes(ctx, &pb.QueryNodesRequest{
			WorkPlanId:    workPlanID,
			CheckIds:      []string{checkID},
			IncludeChecks: true,
		})
		if err != nil {
			log.Printf("[conditional] Failed to query check %s: %v", checkID, err)
			failedChecks = append(failedChecks, checkID)
			continue
		}

		if len(resp.Checks) == 0 {
			log.Printf("[conditional] Check %s not found", checkID)
			continue
		}

		check := resp.Checks[0]

		// Check if the check has completed with a failure
		if check.State == pb.CheckState_CHECK_STATE_FINAL {
			// Check for failure in results - look at the last result
			if len(check.Results) > 0 {
				lastResult := check.Results[len(check.Results)-1]
				if lastResult.Failure != nil {
					log.Printf("[conditional] Upstream check %s failed: %s", checkID, lastResult.Failure.Message)
					failedChecks = append(failedChecks, checkID)
				}
			}
		} else {
			// Check hasn't completed yet - this shouldn't happen due to dependencies
			log.Printf("[conditional] Warning: upstream check %s not in FINAL state (state=%v)", checkID, check.State)
		}
	}

	return len(failedChecks) == 0, failedChecks
}

func (c *ConditionalRunner) createSkipResponse(req *pb.RunRequest, failedChecks []string) (*pb.RunResponse, error) {
	resultData, _ := structpb.NewStruct(map[string]any{
		"skipped":        true,
		"reason":         "upstream_dependency_failed",
		"failed_checks":  failedChecks,
	})

	return &pb.RunResponse{
		StageState: pb.StageState_STAGE_STATE_FINAL,
		CheckUpdates: []*pb.CheckUpdate{{
			CheckId:    req.AssignedCheckIds[0],
			State:      pb.CheckState_CHECK_STATE_FINAL,
			ResultData: resultData,
			Finalize:   true,
		}},
	}, nil
}

func (c *ConditionalRunner) runE2ETest(ctx context.Context, req *pb.RunRequest, testPath, testName string) (*pb.RunResponse, error) {
	checkID := req.AssignedCheckIds[0]

	// Run tests with JSON output
	startTime := time.Now()
	cmd := exec.CommandContext(ctx, "go", "test", "-v", "-json", "-count=1", testPath)
	cmd.Dir = c.repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	// Parse test output
	result := c.parseTestOutput(output, testPath)
	result.DurationMs = duration.Milliseconds()

	testFailed := err != nil || result.Failed > 0

	// Build result data
	resultData, _ := structpb.NewStruct(map[string]any{
		"test_path":   testPath,
		"test_name":   testName,
		"passed":      result.Passed,
		"failed":      result.Failed,
		"skipped":     result.Skipped,
		"duration_ms": result.DurationMs,
		"success":     !testFailed,
	})

	log.Printf("[conditional] E2E test %s completed: passed=%d failed=%d",
		testName, result.Passed, result.Failed)

	if testFailed {
		return &pb.RunResponse{
			StageState: pb.StageState_STAGE_STATE_FINAL,
			CheckUpdates: []*pb.CheckUpdate{{
				CheckId:    checkID,
				State:      pb.CheckState_CHECK_STATE_FINAL,
				ResultData: resultData,
				Finalize:   true,
				Failure:    &pb.Failure{Message: fmt.Sprintf("E2E tests failed: %d failures", result.Failed)},
			}},
		}, nil
	}

	return &pb.RunResponse{
		StageState: pb.StageState_STAGE_STATE_FINAL,
		CheckUpdates: []*pb.CheckUpdate{{
			CheckId:    checkID,
			State:      pb.CheckState_CHECK_STATE_FINAL,
			ResultData: resultData,
			Finalize:   true,
		}},
	}, nil
}

// TestResult contains aggregated test results.
type TestResult struct {
	Passed     int   `json:"passed"`
	Failed     int   `json:"failed"`
	Skipped    int   `json:"skipped"`
	DurationMs int64 `json:"duration_ms"`
}

func (c *ConditionalRunner) parseTestOutput(output []byte, packagePath string) *TestResult {
	result := &TestResult{}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		// Only process test-level events
		if event.Test == "" {
			continue
		}

		switch event.Action {
		case "pass":
			result.Passed++
		case "fail":
			result.Failed++
		case "skip":
			result.Skipped++
		}
	}

	return result
}

func (c *ConditionalRunner) createFailureResponse(req *pb.RunRequest, msg string) (*pb.RunResponse, error) {
	log.Printf("[conditional] Error: %s", msg)
	return &pb.RunResponse{
		StageState: pb.StageState_STAGE_STATE_FINAL,
		CheckUpdates: []*pb.CheckUpdate{{
			CheckId:  req.AssignedCheckIds[0],
			State:    pb.CheckState_CHECK_STATE_FINAL,
			Finalize: true,
			Failure:  &pb.Failure{Message: msg},
		}},
	}, nil
}
