// Package main implements the ResultCollector runner.
// This runner collects all check results, produces a final verdict,
// and writes artifacts (summary, JSON report) to the filesystem.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/cmd/ci/runners/common"
)

var (
	runnerID         = flag.String("runner-id", "collector-runner-1", "Runner ID")
	listenAddr       = flag.String("listen", ":50066", "Address to listen on")
	orchestratorAddr = flag.String("orchestrator", "localhost:50051", "Orchestrator address")
	outputDir        = flag.String("output-dir", "ci-results", "Output directory for artifacts")
)

func main() {
	flag.Parse()

	runner := common.NewBaseRunner(*runnerID, "result_collector", *listenAddr, *orchestratorAddr, 1)

	collector := &CollectorRunner{
		BaseRunner: runner,
		outputDir:  *outputDir,
	}
	runner.SetRunHandler(collector.HandleRun)

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

	log.Println("Shutting down collector runner...")
	runner.Shutdown(ctx)
}

// CollectorRunner handles collecting results and producing artifacts.
type CollectorRunner struct {
	*common.BaseRunner
	outputDir string
}

// CIReport is the complete CI report structure.
type CIReport struct {
	WorkPlanID   string         `json:"work_plan_id"`
	StartTime    time.Time      `json:"start_time"`
	EndTime      time.Time      `json:"end_time"`
	Duration     string         `json:"duration"`
	Status       string         `json:"status"` // passed, failed, partial
	Summary      ReportSummary  `json:"summary"`
	BuildResults []BuildResult  `json:"build_results"`
	TestResults  []TestResult   `json:"test_results"`
	E2EResults   []E2EResult    `json:"e2e_results"`
	Artifacts    []ArtifactInfo `json:"artifacts"`
}

// ReportSummary contains aggregate statistics.
type ReportSummary struct {
	TotalBuilds   int `json:"total_builds"`
	PassedBuilds  int `json:"passed_builds"`
	FailedBuilds  int `json:"failed_builds"`
	TotalTests    int `json:"total_tests"`
	PassedTests   int `json:"passed_tests"`
	FailedTests   int `json:"failed_tests"`
	SkippedTests  int `json:"skipped_tests"`
	TotalE2E      int `json:"total_e2e"`
	PassedE2E     int `json:"passed_e2e"`
	FailedE2E     int `json:"failed_e2e"`
	SkippedE2E    int `json:"skipped_e2e"`
}

// BuildResult contains a single build result.
type BuildResult struct {
	CheckID    string `json:"check_id"`
	Binary     string `json:"binary"`
	Output     string `json:"output"`
	Success    bool   `json:"success"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// TestResult contains a single test package result.
type TestResult struct {
	CheckID    string `json:"check_id"`
	Package    string `json:"package"`
	Passed     int    `json:"passed"`
	Failed     int    `json:"failed"`
	Skipped    int    `json:"skipped"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// E2EResult contains a single E2E test result.
type E2EResult struct {
	CheckID    string `json:"check_id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Passed     int    `json:"passed"`
	Failed     int    `json:"failed"`
	Skipped    bool   `json:"skipped"`
	SkipReason string `json:"skip_reason,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// ArtifactInfo describes a generated artifact.
type ArtifactInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // json, txt, html
}

// HandleRun processes a result collection request.
func (c *CollectorRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	if len(req.AssignedCheckIds) == 0 {
		return nil, fmt.Errorf("no assigned check IDs")
	}

	checkID := req.AssignedCheckIds[0]
	log.Printf("[collector] Starting result collection for work_plan=%s", req.WorkPlanId)

	// Get start time from args if available
	startTime := time.Now().Add(-time.Hour) // Default to 1 hour ago
	if req.Args != nil {
		if v := req.Args.Fields["start_time"]; v != nil {
			if t, err := time.Parse(time.RFC3339, v.GetStringValue()); err == nil {
				startTime = t
			}
		}
	}

	// Query all checks
	resp, err := c.Orchestrator.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    req.WorkPlanId,
		IncludeChecks: true,
	})
	if err != nil {
		return c.createFailureResponse(req, fmt.Sprintf("failed to query checks: %v", err))
	}

	// Build the report
	report := c.buildReport(req.WorkPlanId, startTime, resp.Checks)

	// Ensure output directory exists
	if err := os.MkdirAll(c.outputDir, 0755); err != nil {
		return c.createFailureResponse(req, fmt.Sprintf("failed to create output directory: %v", err))
	}

	// Write artifacts
	artifacts, err := c.writeArtifacts(report)
	if err != nil {
		return c.createFailureResponse(req, fmt.Sprintf("failed to write artifacts: %v", err))
	}
	report.Artifacts = artifacts

	// Print summary to console
	c.printSummary(report)

	// Build result data with artifact links
	resultData, _ := structpb.NewStruct(map[string]any{
		"status":         report.Status,
		"total_builds":   report.Summary.TotalBuilds,
		"passed_builds":  report.Summary.PassedBuilds,
		"failed_builds":  report.Summary.FailedBuilds,
		"total_tests":    report.Summary.TotalTests,
		"passed_tests":   report.Summary.PassedTests,
		"failed_tests":   report.Summary.FailedTests,
		"skipped_tests":  report.Summary.SkippedTests,
		"total_e2e":      report.Summary.TotalE2E,
		"passed_e2e":     report.Summary.PassedE2E,
		"failed_e2e":     report.Summary.FailedE2E,
		"skipped_e2e":    report.Summary.SkippedE2E,
		"json_report":    artifacts[0].Path,
		"summary_report": artifacts[1].Path,
	})

	// Determine if overall result is a failure
	var failure *pb.Failure
	if report.Status == "failed" {
		failure = &pb.Failure{
			Message: fmt.Sprintf("CI failed: %d build failures, %d test failures, %d E2E failures",
				report.Summary.FailedBuilds, report.Summary.FailedTests, report.Summary.FailedE2E),
		}
	}

	return &pb.RunResponse{
		StageState: pb.StageState_STAGE_STATE_FINAL,
		CheckUpdates: []*pb.CheckUpdate{{
			CheckId:    checkID,
			State:      pb.CheckState_CHECK_STATE_FINAL,
			ResultData: resultData,
			Finalize:   true,
			Failure:    failure,
		}},
	}, nil
}

func (c *CollectorRunner) buildReport(workPlanID string, startTime time.Time, checks []*pb.Check) *CIReport {
	endTime := time.Now()
	report := &CIReport{
		WorkPlanID: workPlanID,
		StartTime:  startTime,
		EndTime:    endTime,
		Duration:   endTime.Sub(startTime).String(),
	}

	for _, check := range checks {
		// Skip meta checks (trigger, materialize, collector)
		if check.Kind == "trigger" || check.Kind == "materialize" || check.Kind == "collector" {
			continue
		}

		switch check.Kind {
		case "build":
			br := c.extractBuildResult(check)
			report.BuildResults = append(report.BuildResults, br)
			report.Summary.TotalBuilds++
			if br.Success {
				report.Summary.PassedBuilds++
			} else {
				report.Summary.FailedBuilds++
			}

		case "unit_test":
			tr := c.extractTestResult(check)
			report.TestResults = append(report.TestResults, tr)
			report.Summary.TotalTests++
			if tr.Failed > 0 || tr.Error != "" {
				report.Summary.FailedTests++
			} else if tr.Passed > 0 {
				report.Summary.PassedTests++
			} else {
				report.Summary.SkippedTests++
			}

		case "e2e_test":
			er := c.extractE2EResult(check)
			report.E2EResults = append(report.E2EResults, er)
			report.Summary.TotalE2E++
			if er.Skipped {
				report.Summary.SkippedE2E++
			} else if er.Failed > 0 || er.Error != "" {
				report.Summary.FailedE2E++
			} else {
				report.Summary.PassedE2E++
			}
		}
	}

	// Sort results for consistent output
	sort.Slice(report.BuildResults, func(i, j int) bool {
		return report.BuildResults[i].CheckID < report.BuildResults[j].CheckID
	})
	sort.Slice(report.TestResults, func(i, j int) bool {
		return report.TestResults[i].CheckID < report.TestResults[j].CheckID
	})
	sort.Slice(report.E2EResults, func(i, j int) bool {
		return report.E2EResults[i].CheckID < report.E2EResults[j].CheckID
	})

	// Determine overall status
	if report.Summary.FailedBuilds > 0 || report.Summary.FailedTests > 0 || report.Summary.FailedE2E > 0 {
		report.Status = "failed"
	} else if report.Summary.TotalBuilds == 0 && report.Summary.TotalTests == 0 && report.Summary.TotalE2E == 0 {
		report.Status = "empty"
	} else {
		report.Status = "passed"
	}

	return report
}

func (c *CollectorRunner) extractBuildResult(check *pb.Check) BuildResult {
	br := BuildResult{
		CheckID: check.Id,
	}

	if check.Options != nil {
		if v := check.Options.Fields["binary"]; v != nil {
			br.Binary = v.GetStringValue()
		}
		if v := check.Options.Fields["output"]; v != nil {
			br.Output = v.GetStringValue()
		}
	}

	if len(check.Results) > 0 {
		lastResult := check.Results[len(check.Results)-1]
		if lastResult.Data != nil {
			if v := lastResult.Data.Fields["success"]; v != nil {
				br.Success = v.GetBoolValue()
			}
			if v := lastResult.Data.Fields["duration_ms"]; v != nil {
				br.DurationMs = int64(v.GetNumberValue())
			}
		}
		if lastResult.Failure != nil {
			br.Error = lastResult.Failure.Message
		}
	}

	return br
}

func (c *CollectorRunner) extractTestResult(check *pb.Check) TestResult {
	tr := TestResult{
		CheckID: check.Id,
	}

	if check.Options != nil {
		if v := check.Options.Fields["package"]; v != nil {
			tr.Package = v.GetStringValue()
		}
	}

	if len(check.Results) > 0 {
		lastResult := check.Results[len(check.Results)-1]
		if lastResult.Data != nil {
			if v := lastResult.Data.Fields["passed"]; v != nil {
				tr.Passed = int(v.GetNumberValue())
			}
			if v := lastResult.Data.Fields["failed"]; v != nil {
				tr.Failed = int(v.GetNumberValue())
			}
			if v := lastResult.Data.Fields["skipped"]; v != nil {
				tr.Skipped = int(v.GetNumberValue())
			}
			if v := lastResult.Data.Fields["duration_ms"]; v != nil {
				tr.DurationMs = int64(v.GetNumberValue())
			}
		}
		if lastResult.Failure != nil {
			tr.Error = lastResult.Failure.Message
		}
	}

	return tr
}

func (c *CollectorRunner) extractE2EResult(check *pb.Check) E2EResult {
	er := E2EResult{
		CheckID: check.Id,
	}

	if check.Options != nil {
		if v := check.Options.Fields["name"]; v != nil {
			er.Name = v.GetStringValue()
		}
		if v := check.Options.Fields["test_path"]; v != nil {
			er.Path = v.GetStringValue()
		}
	}

	if len(check.Results) > 0 {
		lastResult := check.Results[len(check.Results)-1]
		if lastResult.Data != nil {
			if v := lastResult.Data.Fields["skipped"]; v != nil {
				er.Skipped = v.GetBoolValue()
			}
			if v := lastResult.Data.Fields["reason"]; v != nil {
				er.SkipReason = v.GetStringValue()
			}
			if v := lastResult.Data.Fields["passed"]; v != nil {
				er.Passed = int(v.GetNumberValue())
			}
			if v := lastResult.Data.Fields["failed"]; v != nil {
				er.Failed = int(v.GetNumberValue())
			}
			if v := lastResult.Data.Fields["duration_ms"]; v != nil {
				er.DurationMs = int64(v.GetNumberValue())
			}
		}
		if lastResult.Failure != nil {
			er.Error = lastResult.Failure.Message
		}
	}

	return er
}

func (c *CollectorRunner) writeArtifacts(report *CIReport) ([]ArtifactInfo, error) {
	var artifacts []ArtifactInfo

	// Write JSON report
	jsonPath := filepath.Join(c.outputDir, "ci-report.json")
	jsonData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}
	if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write JSON: %w", err)
	}
	artifacts = append(artifacts, ArtifactInfo{
		Name: "CI Report (JSON)",
		Path: jsonPath,
		Type: "json",
	})

	// Write text summary
	summaryPath := filepath.Join(c.outputDir, "ci-summary.txt")
	summary := c.generateTextSummary(report)
	if err := os.WriteFile(summaryPath, []byte(summary), 0644); err != nil {
		return nil, fmt.Errorf("failed to write summary: %w", err)
	}
	artifacts = append(artifacts, ArtifactInfo{
		Name: "CI Summary",
		Path: summaryPath,
		Type: "txt",
	})

	return artifacts, nil
}

func (c *CollectorRunner) generateTextSummary(report *CIReport) string {
	var sb strings.Builder

	sb.WriteString("=" + strings.Repeat("=", 60) + "\n")
	sb.WriteString(fmt.Sprintf("  CI REPORT - %s\n", strings.ToUpper(report.Status)))
	sb.WriteString("=" + strings.Repeat("=", 60) + "\n\n")

	sb.WriteString(fmt.Sprintf("Work Plan: %s\n", report.WorkPlanID))
	sb.WriteString(fmt.Sprintf("Duration:  %s\n", report.Duration))
	sb.WriteString(fmt.Sprintf("Started:   %s\n", report.StartTime.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Ended:     %s\n\n", report.EndTime.Format(time.RFC3339)))

	sb.WriteString("-" + strings.Repeat("-", 60) + "\n")
	sb.WriteString("  SUMMARY\n")
	sb.WriteString("-" + strings.Repeat("-", 60) + "\n\n")

	sb.WriteString(fmt.Sprintf("Builds:    %d total, %d passed, %d failed\n",
		report.Summary.TotalBuilds, report.Summary.PassedBuilds, report.Summary.FailedBuilds))
	sb.WriteString(fmt.Sprintf("Tests:     %d total, %d passed, %d failed, %d skipped\n",
		report.Summary.TotalTests, report.Summary.PassedTests, report.Summary.FailedTests, report.Summary.SkippedTests))
	sb.WriteString(fmt.Sprintf("E2E Tests: %d total, %d passed, %d failed, %d skipped\n\n",
		report.Summary.TotalE2E, report.Summary.PassedE2E, report.Summary.FailedE2E, report.Summary.SkippedE2E))

	// Build details
	if len(report.BuildResults) > 0 {
		sb.WriteString("-" + strings.Repeat("-", 60) + "\n")
		sb.WriteString("  BUILDS\n")
		sb.WriteString("-" + strings.Repeat("-", 60) + "\n\n")
		for _, br := range report.BuildResults {
			status := "PASS"
			if !br.Success {
				status = "FAIL"
			}
			sb.WriteString(fmt.Sprintf("  [%s] %s (%dms)\n", status, br.Output, br.DurationMs))
			if br.Error != "" {
				sb.WriteString(fmt.Sprintf("         Error: %s\n", br.Error))
			}
		}
		sb.WriteString("\n")
	}

	// Test details
	if len(report.TestResults) > 0 {
		sb.WriteString("-" + strings.Repeat("-", 60) + "\n")
		sb.WriteString("  UNIT TESTS\n")
		sb.WriteString("-" + strings.Repeat("-", 60) + "\n\n")
		for _, tr := range report.TestResults {
			status := "PASS"
			if tr.Failed > 0 || tr.Error != "" {
				status = "FAIL"
			}
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", status, tr.Package))
			sb.WriteString(fmt.Sprintf("         %d passed, %d failed, %d skipped (%dms)\n",
				tr.Passed, tr.Failed, tr.Skipped, tr.DurationMs))
			if tr.Error != "" {
				sb.WriteString(fmt.Sprintf("         Error: %s\n", tr.Error))
			}
		}
		sb.WriteString("\n")
	}

	// E2E details
	if len(report.E2EResults) > 0 {
		sb.WriteString("-" + strings.Repeat("-", 60) + "\n")
		sb.WriteString("  E2E TESTS\n")
		sb.WriteString("-" + strings.Repeat("-", 60) + "\n\n")
		for _, er := range report.E2EResults {
			status := "PASS"
			if er.Skipped {
				status = "SKIP"
			} else if er.Failed > 0 || er.Error != "" {
				status = "FAIL"
			}
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", status, er.Name))
			if er.Skipped {
				sb.WriteString(fmt.Sprintf("         Reason: %s\n", er.SkipReason))
			} else {
				sb.WriteString(fmt.Sprintf("         %d passed, %d failed (%dms)\n",
					er.Passed, er.Failed, er.DurationMs))
			}
			if er.Error != "" {
				sb.WriteString(fmt.Sprintf("         Error: %s\n", er.Error))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("=" + strings.Repeat("=", 60) + "\n")

	return sb.String()
}

func (c *CollectorRunner) printSummary(report *CIReport) {
	summary := c.generateTextSummary(report)
	fmt.Println(summary)
}

func (c *CollectorRunner) createFailureResponse(req *pb.RunRequest, msg string) (*pb.RunResponse, error) {
	log.Printf("[collector] Error: %s", msg)
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
