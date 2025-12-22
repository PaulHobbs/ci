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

	"google.golang.org/protobuf/types/known/structpb"

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
	// Build dependencies on all other stages
	var deps []*pb.Dependency
	for _, stageID := range dependsOnStages {
		deps = append(deps, &pb.Dependency{
			TargetType: pb.NodeType_NODE_TYPE_STAGE,
			TargetId:   stageID,
		})
	}

	args, _ := structpb.NewStruct(map[string]any{
		"start_time": time.Now().Format(time.RFC3339),
	})

	return &pb.StageWrite{
		Id:            "stage:collector",
		State:         pb.StageState_STAGE_STATE_PLANNED,
		RunnerType:    "result_collector",
		ExecutionMode: pb.ExecutionMode_EXECUTION_MODE_SYNC,
		Args:          args,
		Assignments: []*pb.Assignment{{
			TargetCheckId: "collector:results",
			GoalState:     pb.CheckState_CHECK_STATE_FINAL,
		}},
		Dependencies: &pb.DependencyGroup{
			Predicate:    pb.PredicateType_PREDICATE_TYPE_AND,
			Dependencies: deps,
		},
	}
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

	// Build stage dependencies from check dependencies
	var stageDeps *pb.DependencyGroup
	if check.Dependencies != nil && len(check.Dependencies.Dependencies) > 0 {
		// Convert check dependencies to stage dependencies
		var deps []*pb.Dependency
		for _, checkDep := range check.Dependencies.Dependencies {
			// The stage depends on the stage that handles the dependent check
			if checkDep.TargetType == pb.NodeType_NODE_TYPE_CHECK {
				deps = append(deps, &pb.Dependency{
					TargetType: pb.NodeType_NODE_TYPE_STAGE,
					TargetId:   fmt.Sprintf("stage:%s", checkDep.TargetId),
				})
			}
		}
		if len(deps) > 0 {
			stageDeps = &pb.DependencyGroup{
				Predicate:    check.Dependencies.Predicate,
				Dependencies: deps,
			}
		}
	}

	// For conditional tester, add dependency check IDs to args for upstream checking
	args := check.Options
	if check.Kind == "e2e_test" && check.Dependencies != nil {
		// Clone args and add depends_on_checks
		argsMap := make(map[string]any)
		if args != nil {
			for k, v := range args.Fields {
				argsMap[k] = v.AsInterface()
			}
		}

		var depCheckIDs []any
		for _, dep := range check.Dependencies.Dependencies {
			if dep.TargetType == pb.NodeType_NODE_TYPE_CHECK {
				depCheckIDs = append(depCheckIDs, dep.TargetId)
			}
		}
		argsMap["depends_on_checks"] = depCheckIDs
		args, _ = structpb.NewStruct(argsMap)
	}

	return &pb.StageWrite{
		Id:            stageID,
		State:         pb.StageState_STAGE_STATE_PLANNED,
		RunnerType:    runnerType,
		ExecutionMode: pb.ExecutionMode_EXECUTION_MODE_SYNC,
		Args:          args,
		Assignments: []*pb.Assignment{{
			TargetCheckId: check.Id,
			GoalState:     pb.CheckState_CHECK_STATE_FINAL,
		}},
		Dependencies: stageDeps,
	}, nil
}

func (m *MaterializeRunner) createSuccessResponse(req *pb.RunRequest, stageCount int) (*pb.RunResponse, error) {
	resultData, _ := structpb.NewStruct(map[string]any{
		"stages_created": stageCount,
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

func (m *MaterializeRunner) createFailureResponse(req *pb.RunRequest, msg string) (*pb.RunResponse, error) {
	log.Printf("[materialize] Error: %s", msg)
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
