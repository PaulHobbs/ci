// Package main implements the Submit Queue runner with 8x8 matrix group testing.
// This implements non-adaptive group testing where 64 CLs are arranged in an 8x8 matrix,
// tested by rows first, then columns are used for culprit finding.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/example/turboci-lite/cmd/ci/runners/common"
	pb "github.com/example/turboci-lite/gen/turboci/v1"
)

var (
	runnerID         = flag.String("runner-id", "submit-queue-1", "Runner ID")
	listenAddr       = flag.String("listen", ":50070", "Address to listen on")
	orchestratorAddr = flag.String("orchestrator", "localhost:50051", "Orchestrator address")
	runnerType       = flag.String("type", "kickoff", "Runner type (kickoff, config, assignment, planning, test_executor, minibatch_finished, batch_finished)")
)

func main() {
	flag.Parse()

	var handler common.RunHandler

	switch *runnerType {
	case "kickoff":
		handler = NewKickoffRunner().HandleRun
	case "config":
		handler = NewConfigRunner().HandleRun
	case "assignment":
		handler = NewAssignmentRunner().HandleRun
	case "planning":
		handler = NewPlanningRunner().HandleRun
	case "test_executor":
		handler = NewTestExecutorRunner().HandleRun
	case "minibatch_finished":
		handler = NewMinibatchFinishedRunner().HandleRun
	case "batch_finished":
		handler = NewBatchFinishedRunner().HandleRun
	default:
		log.Fatalf("Unknown runner type: %s", *runnerType)
	}

	runner := common.NewBaseRunner(*runnerID, *runnerType, *listenAddr, *orchestratorAddr, 10)
	runner.SetRunHandler(handler)

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

	log.Println("Shutting down submit queue runner...")
	runner.Shutdown(ctx)
}

// KickoffRunner handles the initial kickoff stage
type KickoffRunner struct {
	orch pb.TurboCIOrchestratorClient
}

func NewKickoffRunner() *KickoffRunner {
	return &KickoffRunner{}
}

// HandleRun processes the kickoff request and creates config, assignment, and planning stages
func (r *KickoffRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	log.Printf("[kickoff] Starting for work_plan=%s", req.WorkPlanId)

	// Get the orchestrator client
	conn, err := getOrchestratorClient(req)
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to connect: %v", err))
	}
	defer conn.Close()
	r.orch = pb.NewTurboCIOrchestratorClient(conn)

	// Extract CLs from args
	cls, err := extractCLs(req)
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to extract CLs: %v", err))
	}

	if len(cls) != 64 {
		return failResponse(req, fmt.Sprintf("expected 64 CLs, got %d", len(cls)))
	}

	log.Printf("[kickoff] Processing %d CLs", len(cls))

	// Create checks for config, assignment, and planning stages
	checks := []*pb.CheckWrite{
		common.NewCheck("sq:config").Kind("sq_config").State(pb.CheckState_CHECK_STATE_PLANNED).Build(),
		common.NewCheck("sq:assignment").Kind("sq_assignment").State(pb.CheckState_CHECK_STATE_PLANNED).DependsOn("sq:config").Build(),
		common.NewCheck("sq:planning").Kind("sq_planning").State(pb.CheckState_CHECK_STATE_PLANNED).DependsOn("sq:assignment").Build(),
	}

	// Create stages for config, assignment, and planning
	stages := []*pb.StageWrite{
		common.NewStage("stage:sq:config").
			RunnerType("config").
			Sync().
			Args(map[string]any{"cls": cls}).
			Assigns("sq:config").
			Build(),
		common.NewStage("stage:sq:assignment").
			RunnerType("assignment").
			Sync().
			Assigns("sq:assignment").
			DependsOnStages("stage:sq:config").
			Build(),
		common.NewStage("stage:sq:planning").
			RunnerType("planning").
			Sync().
			Assigns("sq:planning").
			DependsOnStages("stage:sq:assignment").
			Build(),
	}

	// Write nodes
	_, err = r.orch.WriteNodes(ctx, &pb.WriteNodesRequest{
		WorkPlanId: req.WorkPlanId,
		Checks:     checks,
		Stages:     stages,
	})
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to write nodes: %v", err))
	}

	return common.NewResponse().
		FinalizeCheck(req.AssignedCheckIds[0], map[string]any{
			"cls_count": len(cls),
			"status":    "kickoff_complete",
		}).
		Build(), nil
}

// ConfigRunner creates the matrix configuration
type ConfigRunner struct {
	orch pb.TurboCIOrchestratorClient
}

func NewConfigRunner() *ConfigRunner {
	return &ConfigRunner{}
}

func (r *ConfigRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	log.Printf("[config] Starting for work_plan=%s", req.WorkPlanId)

	conn, err := getOrchestratorClient(req)
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to connect: %v", err))
	}
	defer conn.Close()
	r.orch = pb.NewTurboCIOrchestratorClient(conn)

	// Query the kickoff stage to get CLs
	queryResp, err := r.orch.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    req.WorkPlanId,
		IncludeStages: true,
		StageIds:      []string{"stage:sq:kickoff"},
	})
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to query kickoff: %v", err))
	}

	if len(queryResp.Stages) == 0 {
		return failResponse(req, "kickoff stage not found")
	}

	// Extract CLs from kickoff stage args
	kickoffStage := queryResp.Stages[0]
	clsInterface := kickoffStage.Args.Fields["cls"].AsInterface()
	cls, ok := clsInterface.([]interface{})
	if !ok {
		return failResponse(req, "invalid cls format")
	}

	// Create matrix configuration
	config := map[string]any{
		"matrix_size": 8,
		"total_cls":   len(cls),
		"cls":         cls,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	return common.NewResponse().
		FinalizeCheck(req.AssignedCheckIds[0], config).
		Build(), nil
}

// AssignmentRunner assigns CLs to minibatches (rows and columns)
type AssignmentRunner struct {
	orch pb.TurboCIOrchestratorClient
}

func NewAssignmentRunner() *AssignmentRunner {
	return &AssignmentRunner{}
}

func (r *AssignmentRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	log.Printf("[assignment] Starting for work_plan=%s", req.WorkPlanId)

	conn, err := getOrchestratorClient(req)
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to connect: %v", err))
	}
	defer conn.Close()
	r.orch = pb.NewTurboCIOrchestratorClient(conn)

	// Query config check to get CLs
	queryResp, err := r.orch.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    req.WorkPlanId,
		IncludeChecks: true,
		CheckIds:      []string{"sq:config"},
	})
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to query config: %v", err))
	}

	if len(queryResp.Checks) == 0 || len(queryResp.Checks[0].Results) == 0 {
		return failResponse(req, "config check not found or no results")
	}

	// Extract CLs from config results
	configData := queryResp.Checks[0].Results[0].Data.AsMap()
	clsInterface := configData["cls"]
	cls, ok := clsInterface.([]interface{})
	if !ok {
		return failResponse(req, "invalid cls format")
	}

	// Create row assignments (8 rows, 8 CLs each)
	rowAssignments := make([][]string, 8)
	for i := 0; i < 8; i++ {
		rowAssignments[i] = make([]string, 8)
		for j := 0; j < 8; j++ {
			idx := i*8 + j
			if idx < len(cls) {
				rowAssignments[i][j] = cls[idx].(string)
			}
		}
	}

	// Create column assignments (8 columns, 8 CLs each)
	columnAssignments := make([][]string, 8)
	for j := 0; j < 8; j++ {
		columnAssignments[j] = make([]string, 8)
		for i := 0; i < 8; i++ {
			idx := i*8 + j
			if idx < len(cls) {
				columnAssignments[j][i] = cls[idx].(string)
			}
		}
	}

	assignments := map[string]any{
		"rows":    rowAssignments,
		"columns": columnAssignments,
	}

	return common.NewResponse().
		FinalizeCheck(req.AssignedCheckIds[0], assignments).
		Build(), nil
}

// PlanningRunner creates the execution stages for row tests
type PlanningRunner struct {
	orch pb.TurboCIOrchestratorClient
}

func NewPlanningRunner() *PlanningRunner {
	return &PlanningRunner{}
}

func (r *PlanningRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	log.Printf("[planning] Starting for work_plan=%s", req.WorkPlanId)

	conn, err := getOrchestratorClient(req)
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to connect: %v", err))
	}
	defer conn.Close()
	r.orch = pb.NewTurboCIOrchestratorClient(conn)

	// Query assignment check to get row assignments
	queryResp, err := r.orch.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    req.WorkPlanId,
		IncludeChecks: true,
		CheckIds:      []string{"sq:assignment"},
	})
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to query assignment: %v", err))
	}

	if len(queryResp.Checks) == 0 || len(queryResp.Checks[0].Results) == 0 {
		return failResponse(req, "assignment check not found or no results")
	}

	assignmentData := queryResp.Checks[0].Results[0].Data.AsMap()
	rowsInterface := assignmentData["rows"]
	rows, ok := rowsInterface.([]interface{})
	if !ok {
		return failResponse(req, "invalid rows format")
	}

	// Create checks and stages for each row minibatch
	var checks []*pb.CheckWrite
	var stages []*pb.StageWrite
	var allRowFinishedStageIDs []string

	for i := 0; i < 8; i++ {
		rowCheckID := fmt.Sprintf("sq:row:%d:test", i)
		rowFinishedCheckID := fmt.Sprintf("sq:row:%d:finished", i)

		// Get row CLs
		rowCLs := rows[i].([]interface{})

		// Create check for row test
		checks = append(checks, common.NewCheck(rowCheckID).
			Kind("sq_row_test").
			State(pb.CheckState_CHECK_STATE_PLANNED).
			Options(map[string]any{
				"row":  i,
				"cls":  rowCLs,
				"type": "row",
			}).
			Build())

		// Create stage for row test execution
		stages = append(stages, common.NewStage(fmt.Sprintf("stage:sq:row:%d:test", i)).
			RunnerType("test_executor").
			Sync().
			Args(map[string]any{
				"row":  i,
				"cls":  rowCLs,
				"type": "row",
			}).
			Assigns(rowCheckID).
			Build())

		// Create check for row finished
		checks = append(checks, common.NewCheck(rowFinishedCheckID).
			Kind("sq_row_finished").
			State(pb.CheckState_CHECK_STATE_PLANNED).
			DependsOn(rowCheckID).
			Build())

		// Create stage for row finished (handles submission)
		rowFinishedStageID := fmt.Sprintf("stage:sq:row:%d:finished", i)
		stages = append(stages, common.NewStage(rowFinishedStageID).
			RunnerType("minibatch_finished").
			Sync().
			Args(map[string]any{
				"row":  i,
				"type": "row",
			}).
			Assigns(rowFinishedCheckID).
			DependsOnStages(fmt.Sprintf("stage:sq:row:%d:test", i)).
			Build())

		allRowFinishedStageIDs = append(allRowFinishedStageIDs, rowFinishedStageID)
	}

	// Create batch-finished check (depends on all row finished checks)
	var batchFinishedDeps []string
	for i := 0; i < 8; i++ {
		batchFinishedDeps = append(batchFinishedDeps, fmt.Sprintf("sq:row:%d:finished", i))
	}

	checks = append(checks, common.NewCheck("sq:batch:finished").
		Kind("sq_batch_finished").
		State(pb.CheckState_CHECK_STATE_PLANNED).
		DependsOn(batchFinishedDeps...).
		Build())

	// Create batch-finished stage (depends on all row finished stages)
	stages = append(stages, common.NewStage("stage:sq:batch:finished").
		RunnerType("batch_finished").
		Sync().
		Assigns("sq:batch:finished").
		DependsOnStages(allRowFinishedStageIDs...).
		Build())

	// Write all nodes
	_, err = r.orch.WriteNodes(ctx, &pb.WriteNodesRequest{
		WorkPlanId: req.WorkPlanId,
		Checks:     checks,
		Stages:     stages,
	})
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to write nodes: %v", err))
	}

	return common.NewResponse().
		FinalizeCheck(req.AssignedCheckIds[0], map[string]any{
			"row_stages_created":     8,
			"row_finished_created":   8,
			"batch_finished_created": 1,
		}).
		Build(), nil
}

// TestExecutorRunner executes tests for a minibatch (row or column)
type TestExecutorRunner struct {
	orch pb.TurboCIOrchestratorClient
}

func NewTestExecutorRunner() *TestExecutorRunner {
	return &TestExecutorRunner{}
}

func (r *TestExecutorRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	testType := "unknown"
	if req.Args != nil && req.Args.Fields["type"] != nil {
		testType = req.Args.Fields["type"].GetStringValue()
	}

	var batchID int
	if req.Args != nil {
		if req.Args.Fields["row"] != nil {
			batchID = int(req.Args.Fields["row"].GetNumberValue())
		} else if req.Args.Fields["col"] != nil {
			batchID = int(req.Args.Fields["col"].GetNumberValue())
		}
	}

	log.Printf("[test_executor] Starting %s test for batch %d", testType, batchID)

	// Extract CLs from args
	var cls []string
	if req.Args != nil && req.Args.Fields["cls"] != nil {
		clsList := req.Args.Fields["cls"].GetListValue().AsSlice()
		for _, cl := range clsList {
			cls = append(cls, cl.(string))
		}
	}

	// Simulate test execution
	// For demonstration, we'll make some rows/columns fail based on a pattern
	passed := simulateTest(testType, batchID, cls)

	result := map[string]any{
		"type":     testType,
		"batch_id": batchID,
		"cls":      cls,
		"passed":   passed,
		"tested_at": time.Now().Format(time.RFC3339),
	}

	if !passed {
		log.Printf("[test_executor] %s %d FAILED", testType, batchID)
		return common.NewResponse().
			FailCheck(req.AssignedCheckIds[0], fmt.Sprintf("%s %d failed", testType, batchID), result).
			Build(), nil
	}

	log.Printf("[test_executor] %s %d PASSED", testType, batchID)
	return common.NewResponse().
		FinalizeCheck(req.AssignedCheckIds[0], result).
		Build(), nil
}

// MinibatchFinishedRunner handles minibatch completion and submission
type MinibatchFinishedRunner struct {
	orch pb.TurboCIOrchestratorClient
}

func NewMinibatchFinishedRunner() *MinibatchFinishedRunner {
	return &MinibatchFinishedRunner{}
}

func (r *MinibatchFinishedRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	testType := "unknown"
	if req.Args != nil && req.Args.Fields["type"] != nil {
		testType = req.Args.Fields["type"].GetStringValue()
	}

	var batchID int
	if req.Args != nil {
		if req.Args.Fields["row"] != nil {
			batchID = int(req.Args.Fields["row"].GetNumberValue())
		} else if req.Args.Fields["col"] != nil {
			batchID = int(req.Args.Fields["col"].GetNumberValue())
		}
	}

	log.Printf("[minibatch_finished] Processing %s %d", testType, batchID)

	conn, err := getOrchestratorClient(req)
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to connect: %v", err))
	}
	defer conn.Close()
	r.orch = pb.NewTurboCIOrchestratorClient(conn)

	// Query the test check to see if it passed
	testCheckID := fmt.Sprintf("sq:%s:%d:test", testType, batchID)
	queryResp, err := r.orch.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    req.WorkPlanId,
		IncludeChecks: true,
		CheckIds:      []string{testCheckID},
	})
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to query test check: %v", err))
	}

	if len(queryResp.Checks) == 0 || len(queryResp.Checks[0].Results) == 0 {
		return failResponse(req, "test check not found or no results")
	}

	testCheck := queryResp.Checks[0]
	testResult := testCheck.Results[0]
	passed := testResult.Failure == nil

	result := map[string]any{
		"type":     testType,
		"batch_id": batchID,
		"passed":   passed,
	}

	if passed && testType == "row" {
		// Submit CLs if row passed
		clsInterface := testResult.Data.AsMap()["cls"]
		cls, _ := clsInterface.([]interface{})

		log.Printf("[minibatch_finished] Row %d PASSED - submitting %d CLs", batchID, len(cls))
		result["submitted_cls"] = cls
		result["action"] = "submitted"
	} else if !passed {
		log.Printf("[minibatch_finished] %s %d FAILED - marking for column tests", testType, batchID)
		result["action"] = "marked_for_column_test"
	}

	return common.NewResponse().
		FinalizeCheck(req.AssignedCheckIds[0], result).
		Build(), nil
}

// BatchFinishedRunner handles the final batch completion and column test creation
type BatchFinishedRunner struct {
	orch pb.TurboCIOrchestratorClient
}

func NewBatchFinishedRunner() *BatchFinishedRunner {
	return &BatchFinishedRunner{}
}

func (r *BatchFinishedRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	log.Printf("[batch_finished] Starting for work_plan=%s", req.WorkPlanId)

	conn, err := getOrchestratorClient(req)
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to connect: %v", err))
	}
	defer conn.Close()
	r.orch = pb.NewTurboCIOrchestratorClient(conn)

	// Query all row finished checks to determine which rows failed
	var rowFinishedCheckIDs []string
	for i := 0; i < 8; i++ {
		rowFinishedCheckIDs = append(rowFinishedCheckIDs, fmt.Sprintf("sq:row:%d:finished", i))
	}

	queryResp, err := r.orch.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    req.WorkPlanId,
		IncludeChecks: true,
		CheckIds:      rowFinishedCheckIDs,
	})
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to query row finished checks: %v", err))
	}

	// Determine which columns need testing (columns with at least one failure)
	columnsWithFailures := make(map[int]bool)
	for _, check := range queryResp.Checks {
		if len(check.Results) > 0 {
			resultData := check.Results[0].Data.AsMap()
			if passed, ok := resultData["passed"].(bool); ok && !passed {
				if batchID, ok := resultData["batch_id"].(float64); ok {
					rowID := int(batchID)
					// Mark all columns in this row as needing testing
					for col := 0; col < 8; col++ {
						columnsWithFailures[col] = true
					}
					log.Printf("[batch_finished] Row %d failed, marking all columns for testing", rowID)
				}
			}
		}
	}

	// If no failures, we're done
	if len(columnsWithFailures) == 0 {
		log.Printf("[batch_finished] No failures detected, all rows passed!")
		return common.NewResponse().
			FinalizeCheck(req.AssignedCheckIds[0], map[string]any{
				"all_passed":         true,
				"column_tests_needed": 0,
			}).
			Build(), nil
	}

	// Query assignments to get column CLs
	assignmentResp, err := r.orch.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    req.WorkPlanId,
		IncludeChecks: true,
		CheckIds:      []string{"sq:assignment"},
	})
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to query assignment: %v", err))
	}

	if len(assignmentResp.Checks) == 0 || len(assignmentResp.Checks[0].Results) == 0 {
		return failResponse(req, "assignment check not found")
	}

	assignmentData := assignmentResp.Checks[0].Results[0].Data.AsMap()
	columnsInterface := assignmentData["columns"]
	columns, ok := columnsInterface.([]interface{})
	if !ok {
		return failResponse(req, "invalid columns format")
	}

	// Create column test stages for failed columns
	var checks []*pb.CheckWrite
	var stages []*pb.StageWrite
	var columnFinishedStageIDs []string

	for col := range columnsWithFailures {
		colCheckID := fmt.Sprintf("sq:col:%d:test", col)
		colFinishedCheckID := fmt.Sprintf("sq:col:%d:finished", col)

		// Get column CLs
		colCLs := columns[col].([]interface{})

		// Create check for column test
		checks = append(checks, common.NewCheck(colCheckID).
			Kind("sq_col_test").
			State(pb.CheckState_CHECK_STATE_PLANNED).
			Options(map[string]any{
				"col":  col,
				"cls":  colCLs,
				"type": "column",
			}).
			Build())

		// Create stage for column test execution
		stages = append(stages, common.NewStage(fmt.Sprintf("stage:sq:col:%d:test", col)).
			RunnerType("test_executor").
			Sync().
			Args(map[string]any{
				"col":  col,
				"cls":  colCLs,
				"type": "column",
			}).
			Assigns(colCheckID).
			Build())

		// Create check for column finished
		checks = append(checks, common.NewCheck(colFinishedCheckID).
			Kind("sq_col_finished").
			State(pb.CheckState_CHECK_STATE_PLANNED).
			DependsOn(colCheckID).
			Build())

		// Create stage for column finished
		colFinishedStageID := fmt.Sprintf("stage:sq:col:%d:finished", col)
		stages = append(stages, common.NewStage(colFinishedStageID).
			RunnerType("minibatch_finished").
			Sync().
			Args(map[string]any{
				"col":  col,
				"type": "column",
			}).
			Assigns(colFinishedCheckID).
			DependsOnStages(fmt.Sprintf("stage:sq:col:%d:test", col)).
			Build())

		columnFinishedStageIDs = append(columnFinishedStageIDs, colFinishedStageID)
	}

	// Create final decode check (depends on all column finished checks)
	var decodeDeps []string
	for col := range columnsWithFailures {
		decodeDeps = append(decodeDeps, fmt.Sprintf("sq:col:%d:finished", col))
	}

	checks = append(checks, common.NewCheck("sq:decode:finished").
		Kind("sq_decode").
		State(pb.CheckState_CHECK_STATE_PLANNED).
		DependsOn(decodeDeps...).
		Build())

	// Create decode stage
	stages = append(stages, common.NewStage("stage:sq:decode:finished").
		RunnerType("batch_finished").
		Sync().
		Assigns("sq:decode:finished").
		DependsOnStages(columnFinishedStageIDs...).
		Build())

	// Write column test nodes
	_, err = r.orch.WriteNodes(ctx, &pb.WriteNodesRequest{
		WorkPlanId: req.WorkPlanId,
		Checks:     checks,
		Stages:     stages,
	})
	if err != nil {
		return failResponse(req, fmt.Sprintf("failed to write column nodes: %v", err))
	}

	log.Printf("[batch_finished] Created %d column test stages", len(columnsWithFailures))

	return common.NewResponse().
		FinalizeCheck(req.AssignedCheckIds[0], map[string]any{
			"column_tests_created": len(columnsWithFailures),
			"columns":              getColumnIDs(columnsWithFailures),
		}).
		Build(), nil
}

// Helper functions

func getOrchestratorClient(req *pb.RunRequest) (*grpc.ClientConn, error) {
	// Extract orchestrator address from environment or use default
	orchestratorAddr := os.Getenv("ORCHESTRATOR_ADDR")
	if orchestratorAddr == "" {
		orchestratorAddr = "localhost:50051"
	}

	conn, err := grpc.NewClient(orchestratorAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to orchestrator: %w", err)
	}
	return conn, nil
}

func extractCLs(req *pb.RunRequest) ([]string, error) {
	if req.Args == nil || req.Args.Fields["cls"] == nil {
		return nil, fmt.Errorf("cls not found in args")
	}

	clsList := req.Args.Fields["cls"].GetListValue().AsSlice()
	cls := make([]string, len(clsList))
	for i, cl := range clsList {
		if clStr, ok := cl.(string); ok {
			cls[i] = clStr
		} else {
			return nil, fmt.Errorf("invalid CL format at index %d", i)
		}
	}
	return cls, nil
}

func failResponse(req *pb.RunRequest, msg string) (*pb.RunResponse, error) {
	log.Printf("Error: %s", msg)
	if len(req.AssignedCheckIds) > 0 {
		return common.NewResponse().
			FailCheck(req.AssignedCheckIds[0], msg, nil).
			Build(), nil
	}
	return common.NewResponse().
		Failure(msg).
		Build(), nil
}

func simulateTest(testType string, batchID int, cls []string) bool {
	// Simulate some failures for demonstration
	// Row 2 and Row 5 fail
	if testType == "row" && (batchID == 2 || batchID == 5) {
		return false
	}
	// Column 3 and Column 6 fail (to identify specific CLs)
	if testType == "column" && (batchID == 3 || batchID == 6) {
		return false
	}
	return true
}

func getColumnIDs(columnsWithFailures map[int]bool) []int {
	var cols []int
	for col := range columnsWithFailures {
		cols = append(cols, col)
	}
	return cols
}
