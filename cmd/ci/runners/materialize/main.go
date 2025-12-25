// Package main implements the Materialize runner.
// This runner queries existing checks and creates stages to fulfill them.
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

	"github.com/example/turboci-lite/cmd/ci/runners/common"
	pb "github.com/example/turboci-lite/gen/turboci/v1"
)

var (
	runnerID         = flag.String("runner-id", "materialize-runner-1", "Runner ID")
	listenAddr       = flag.String("listen", ":50062", "Address to listen on")
	orchestratorAddr = flag.String("orchestrator", "localhost:50051", "Orchestrator address")
)

func main() {
	flag.Parse()

	runner := common.NewBaseRunner(*runnerID, "materialize", *listenAddr, *orchestratorAddr, 1)

	materialize := &MaterializeRunner{
		BaseRunner: runner,
	}
	runner.SetRunHandler(materialize.HandleRun)

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

	log.Println("Shutting down materialize runner...")
	runner.Shutdown(ctx)
}

// MaterializeRunner handles creating stages for checks.
type MaterializeRunner struct {
	*common.BaseRunner
}

// HandleRun processes a materialize request.
func (m *MaterializeRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	log.Printf("[materialize] Starting materialization for work_plan=%s", req.WorkPlanId)

	// Query all checks in the work plan
	queryResp, err := m.Orchestrator.QueryNodes(ctx, &pb.QueryNodesRequest{
		WorkPlanId:    req.WorkPlanId,
		IncludeChecks: true,
	})
	if err != nil {
		return m.createFailureResponse(req, fmt.Sprintf("failed to query checks: %v", err))
	}

	log.Printf("[materialize] Found %d checks to materialize", len(queryResp.Checks))

	// First, create the collector check (will be fulfilled by collector stage)
	_, err = m.Orchestrator.WriteNodes(ctx, &pb.WriteNodesRequest{
		WorkPlanId: req.WorkPlanId,
		Checks: []*pb.CheckWrite{{
			Id:    "collector:results",
			State: pb.CheckState_CHECK_STATE_PLANNING,
			Kind:  "collector",
		}},
	})
	if err != nil {
		return m.createFailureResponse(req, fmt.Sprintf("failed to create collector check: %v", err))
	}

	// Create stages for each check
	stages, err := m.createStages(queryResp.Checks)
	if err != nil {
		return m.createFailureResponse(req, fmt.Sprintf("failed to create stages: %v", err))
	}

	if len(stages) == 0 {
		log.Printf("[materialize] No stages to create")
		return m.createSuccessResponse(req, 0)
	}

	log.Printf("[materialize] Creating %d stages", len(stages))

	// Write stages via WriteNodes
	_, err = m.Orchestrator.WriteNodes(ctx, &pb.WriteNodesRequest{
		WorkPlanId: req.WorkPlanId,
		Stages:     stages,
	})
	if err != nil {
		return m.createFailureResponse(req, fmt.Sprintf("failed to write stages: %v", err))
	}

	return m.createSuccessResponse(req, len(stages))
}

func (m *MaterializeRunner) createStages(checks []*pb.Check) ([]*pb.StageWrite, error) {
	var stages []*pb.StageWrite
	var allStageIDs []string

	// Build a map of check IDs for dependency resolution
	checkMap := make(map[string]*pb.Check)
	for _, check := range checks {
		checkMap[check.Id] = check
	}

	for _, check := range checks {
		// Skip checks that are already handled (trigger/materialize/collector checks)
		if check.Kind == "trigger" || check.Kind == "materialize" || check.Kind == "collector" {
			continue
		}

		stage, err := m.createStageForCheck(check, checkMap)
		if err != nil {
			return nil, fmt.Errorf("failed to create stage for check %s: %w", check.Id, err)
		}
		if stage != nil {
			stages = append(stages, stage)
			allStageIDs = append(allStageIDs, stage.Id)
		}
	}

	// Create collector stage that depends on all other stages
	if len(allStageIDs) > 0 {
		collectorStage := m.createCollectorStage(allStageIDs)
		stages = append(stages, collectorStage)
	}

	return stages, nil
}

func (m *MaterializeRunner) createCollectorStage(dependsOnStages []string) *pb.StageWrite {
	return common.NewStage("stage:collector").
		RunnerType("result_collector").
		Sync().
		Args(map[string]any{
			"start_time": time.Now().Format(time.RFC3339),
		}).
		Assigns("collector:results").
		DependsOnStages(dependsOnStages...).
		Build()
}

func (m *MaterializeRunner) createStageForCheck(check *pb.Check, allChecks map[string]*pb.Check) (*pb.StageWrite, error) {
	stageID := fmt.Sprintf("stage:%s", check.Id)

	// Determine runner type based on check kind
	var runnerType string
	switch check.Kind {
	case "build":
		runnerType = "go_builder"
	case "unit_test":
		runnerType = "go_tester"
	case "e2e_test":
		runnerType = "conditional_tester"
	default:
		log.Printf("[materialize] Unknown check kind %s, skipping", check.Kind)
		return nil, nil
	}

	// Build stage dependencies from check dependencies (convert check deps to stage deps)
	var stageDeps []string
	if check.Dependencies != nil {
		for _, checkDep := range check.Dependencies.Dependencies {
			if checkDep.TargetType == pb.NodeType_NODE_TYPE_CHECK {
				stageDeps = append(stageDeps, fmt.Sprintf("stage:%s", checkDep.TargetId))
			}
		}
	}

	// Build args map from check options
	argsMap := make(map[string]any)
	if check.Options != nil {
		for k, v := range check.Options.Fields {
			argsMap[k] = v.AsInterface()
		}
	}

	// For conditional tester, add dependency check IDs to args for upstream checking
	if check.Kind == "e2e_test" && check.Dependencies != nil {
		var depCheckIDs []any
		for _, dep := range check.Dependencies.Dependencies {
			if dep.TargetType == pb.NodeType_NODE_TYPE_CHECK {
				depCheckIDs = append(depCheckIDs, dep.TargetId)
			}
		}
		argsMap["depends_on_checks"] = depCheckIDs
	}

	return common.NewStage(stageID).
		RunnerType(runnerType).
		Sync().
		Args(argsMap).
		Assigns(check.Id).
		DependsOnStages(stageDeps...).
		Build(), nil
}

func (m *MaterializeRunner) createSuccessResponse(req *pb.RunRequest, stageCount int) (*pb.RunResponse, error) {
	return common.NewResponse().
		FinalizeCheck(req.AssignedCheckIds[0], map[string]any{
			"stages_created": stageCount,
		}).
		Build(), nil
}

func (m *MaterializeRunner) createFailureResponse(req *pb.RunRequest, msg string) (*pb.RunResponse, error) {
	log.Printf("[materialize] Error: %s", msg)
	return common.NewResponse().
		FailCheck(req.AssignedCheckIds[0], msg, nil).
		Build(), nil
}
