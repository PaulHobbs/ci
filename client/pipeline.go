package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage"
	"github.com/example/turboci-lite/pkg/id"
)

// StageType identifies the type of pipeline stage.
type StageType string

const (
	StageTypePlanning      StageType = "planning"
	StageTypeMaterializer  StageType = "materializer"
	StageTypeBuildExecutor StageType = "build_executor"
	StageTypeTestExecutor  StageType = "test_executor"
	StageTypeCompletion    StageType = "completion"
)

// CheckKind identifies the type of check.
type CheckKind string

const (
	CheckKindBuild      CheckKind = "build"
	CheckKindTest       CheckKind = "test"
	CheckKindCompletion CheckKind = "completion"
)

// PipelineRunner coordinates execution of pipeline stages using the orchestrator.
type PipelineRunner struct {
	orchestrator *service.OrchestratorService
	config       ConfigProvider
	executor     Executor
	gerrit       GerritClient
	processUID   string
}

// NewPipelineRunner creates a new PipelineRunner.
func NewPipelineRunner(
	storage storage.Storage,
	config ConfigProvider,
	executor Executor,
	gerrit GerritClient,
) *PipelineRunner {
	return &PipelineRunner{
		orchestrator: service.NewOrchestrator(storage),
		config:       config,
		executor:     executor,
		gerrit:       gerrit,
		processUID:   id.Generate(),
	}
}

// RunPipeline executes the complete CI pipeline for a change.
// Returns the final work plan ID and any error.
func (r *PipelineRunner) RunPipeline(ctx context.Context, change *ChangeInfo) (string, error) {
	// Create work plan
	wp, err := r.orchestrator.CreateWorkPlan(ctx, &service.CreateWorkPlanRequest{
		Metadata: map[string]string{
			"change_id": change.ID,
			"project":   change.Project,
			"branch":    change.Branch,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create work plan: %w", err)
	}

	// Create initial planning stage (no dependencies, starts immediately)
	if err := r.createPlanningStage(ctx, wp.ID, change); err != nil {
		return wp.ID, fmt.Errorf("failed to create planning stage: %w", err)
	}

	// Run the pipeline loop until completion or error
	if err := r.runLoop(ctx, wp.ID, change); err != nil {
		return wp.ID, err
	}

	return wp.ID, nil
}

// createPlanningStage creates the initial planning stage.
func (r *PipelineRunner) createPlanningStage(ctx context.Context, workPlanID string, change *ChangeInfo) error {
	stageState := domain.StageStatePlanned
	_, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages: []*service.StageWrite{
			{
				ID:    "planning",
				State: &stageState,
				Args: map[string]any{
					"stage_type": string(StageTypePlanning),
					"change_id":  change.ID,
				},
			},
		},
	})
	return err
}

// runLoop processes stages until all are complete.
func (r *PipelineRunner) runLoop(ctx context.Context, workPlanID string, change *ChangeInfo) error {
	iteration := 0
	maxIterations := 100 // Safety limit
	for {
		iteration++
		if iteration > maxIterations {
			return fmt.Errorf("max iterations reached")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Query for stages that need execution (ATTEMPTING state with PENDING attempts)
		stages, err := r.queryPendingStages(ctx, workPlanID)
		if err != nil {
			return fmt.Errorf("failed to query stages: %w", err)
		}

		if len(stages) == 0 {
			// Check if all stages are complete
			allDone, err := r.checkAllComplete(ctx, workPlanID)
			if err != nil {
				return err
			}
			if allDone {
				return nil
			}
			// Wait briefly and retry
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Execute each pending stage
		for _, stage := range stages {
			if err := r.executeStage(ctx, workPlanID, stage, change); err != nil {
				return fmt.Errorf("failed to execute stage %s: %w", stage.ID, err)
			}
		}
	}
}

// queryPendingStages returns stages ready for execution.
func (r *PipelineRunner) queryPendingStages(ctx context.Context, workPlanID string) ([]*domain.Stage, error) {
	resp, err := r.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		StageStates:   []domain.StageState{domain.StageStateAttempting},
		IncludeStages: true,
	})
	if err != nil {
		return nil, err
	}

	// Filter to stages with pending attempts
	var pending []*domain.Stage
	for _, stage := range resp.Stages {
		attempt := stage.CurrentAttempt()
		if attempt != nil && attempt.State == domain.AttemptStatePending {
			pending = append(pending, stage)
		}
	}
	return pending, nil
}

// checkAllComplete returns true if all stages are in FINAL state.
func (r *PipelineRunner) checkAllComplete(ctx context.Context, workPlanID string) (bool, error) {
	resp, err := r.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		IncludeStages: true,
	})
	if err != nil {
		return false, err
	}

	if len(resp.Stages) == 0 {
		return false, nil
	}

	allFinal := true
	for _, stage := range resp.Stages {
		if stage.State != domain.StageStateFinal {
			allFinal = false
		}
	}
	return allFinal, nil
}

// executeStage runs a stage based on its type.
func (r *PipelineRunner) executeStage(ctx context.Context, workPlanID string, stage *domain.Stage, change *ChangeInfo) error {
	stageType, _ := stage.Args["stage_type"].(string)

	// Claim the attempt (ProcessUID triggers Claim which sets state to RUNNING)
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages: []*service.StageWrite{
			{
				ID: stage.ID,
				CurrentAttempt: &service.AttemptWrite{
					ProcessUID: r.processUID,
				},
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to claim attempt: %w", err)
	}

	var execErr error
	switch StageType(stageType) {
	case StageTypePlanning:
		execErr = r.executePlanningStage(ctx, workPlanID, stage, change)
	case StageTypeMaterializer:
		execErr = r.executeMaterializerStage(ctx, workPlanID, stage, change)
	case StageTypeBuildExecutor:
		execErr = r.executeBuildStage(ctx, workPlanID, stage, change)
	case StageTypeTestExecutor:
		execErr = r.executeTestStage(ctx, workPlanID, stage, change)
	case StageTypeCompletion:
		execErr = r.executeCompletionStage(ctx, workPlanID, stage, change)
	default:
		execErr = fmt.Errorf("unknown stage type: %s", stageType)
	}

	// Complete the attempt
	if execErr != nil {
		return r.failStage(ctx, workPlanID, stage.ID, execErr.Error())
	}
	return r.completeStage(ctx, workPlanID, stage.ID)
}

// executePlanningStage determines which checks are needed and creates the materializer.
func (r *PipelineRunner) executePlanningStage(ctx context.Context, workPlanID string, stage *domain.Stage, change *ChangeInfo) error {
	// Get build and test configs
	buildConfigs, err := r.config.GetBuildConfigs(ctx, change)
	if err != nil {
		return fmt.Errorf("failed to get build configs: %w", err)
	}

	testConfigs, err := r.config.GetTestConfigs(ctx, change)
	if err != nil {
		return fmt.Errorf("failed to get test configs: %w", err)
	}

	// Create checks for each build and test (in PLANNING state)
	req := &service.WriteNodesRequest{WorkPlanID: workPlanID}

	for _, bc := range buildConfigs {
		checkID := fmt.Sprintf("build:%s", bc.ID)
		checkState := domain.CheckStatePlanning
		req.Checks = append(req.Checks, &service.CheckWrite{
			ID:    checkID,
			State: &checkState,
			Kind:  string(CheckKindBuild),
			Options: map[string]any{
				"build_config_id": bc.ID,
				"name":            bc.Name,
				"target":          bc.Target,
				"platform":        bc.Platform,
			},
		})
	}

	for _, tc := range testConfigs {
		checkID := fmt.Sprintf("test:%s", tc.ID)
		checkState := domain.CheckStatePlanning

		var deps *domain.DependencyGroup
		if tc.DependsOnBuild != "" {
			deps = &domain.DependencyGroup{
				Predicate: domain.PredicateAND,
				Dependencies: []domain.DependencyRef{
					{TargetType: domain.NodeTypeCheck, TargetID: fmt.Sprintf("build:%s", tc.DependsOnBuild)},
				},
			}
		}

		req.Checks = append(req.Checks, &service.CheckWrite{
			ID:           checkID,
			State:        &checkState,
			Kind:         string(CheckKindTest),
			Dependencies: deps,
			Options: map[string]any{
				"test_config_id":   tc.ID,
				"name":             tc.Name,
				"suite":            tc.Suite,
				"platform":         tc.Platform,
				"depends_on_build": tc.DependsOnBuild,
			},
		})
	}

	// Create the materializer stage that depends on planning completion
	stageState := domain.StageStatePlanned
	req.Stages = append(req.Stages, &service.StageWrite{
		ID:    "materializer",
		State: &stageState,
		Args: map[string]any{
			"stage_type": string(StageTypeMaterializer),
			"change_id":  change.ID,
		},
		Dependencies: &domain.DependencyGroup{
			Predicate: domain.PredicateAND,
			Dependencies: []domain.DependencyRef{
				{TargetType: domain.NodeTypeStage, TargetID: "planning"},
			},
		},
	})

	_, err = r.orchestrator.WriteNodes(ctx, req)
	return err
}

// executeMaterializerStage creates execution stages for all planned checks.
func (r *PipelineRunner) executeMaterializerStage(ctx context.Context, workPlanID string, stage *domain.Stage, change *ChangeInfo) error {
	// Query all checks
	resp, err := r.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		IncludeChecks: true,
	})
	if err != nil {
		return err
	}

	req := &service.WriteNodesRequest{WorkPlanID: workPlanID}

	// Collect all build/test check IDs for the completion stage dependency
	var allCheckIDs []string

	for _, check := range resp.Checks {
		// Transition check from PLANNING to PLANNED
		plannedState := domain.CheckStatePlanned
		req.Checks = append(req.Checks, &service.CheckWrite{
			ID:    check.ID,
			State: &plannedState,
		})

		allCheckIDs = append(allCheckIDs, check.ID)

		// Create executor stage for this check
		stageState := domain.StageStatePlanned
		stageID := fmt.Sprintf("exec:%s", check.ID)

		var stageType StageType
		if check.Kind == string(CheckKindBuild) {
			stageType = StageTypeBuildExecutor
		} else if check.Kind == string(CheckKindTest) {
			stageType = StageTypeTestExecutor
		} else {
			continue
		}

		// Stage dependencies: depends on materializer + check's dependencies
		deps := []domain.DependencyRef{
			{TargetType: domain.NodeTypeStage, TargetID: "materializer"},
		}
		if check.Dependencies != nil {
			for _, d := range check.Dependencies.Dependencies {
				// Convert check dependency to stage dependency
				if d.TargetType == domain.NodeTypeCheck {
					deps = append(deps, domain.DependencyRef{
						TargetType: domain.NodeTypeStage,
						TargetID:   fmt.Sprintf("exec:%s", d.TargetID),
					})
				}
			}
		}

		req.Stages = append(req.Stages, &service.StageWrite{
			ID:    stageID,
			State: &stageState,
			Args: map[string]any{
				"stage_type": string(stageType),
				"check_id":   check.ID,
				"change_id":  change.ID,
			},
			Assignments: []domain.Assignment{
				{TargetCheckID: check.ID, GoalState: domain.CheckStateFinal},
			},
			Dependencies: &domain.DependencyGroup{
				Predicate:    domain.PredicateAND,
				Dependencies: deps,
			},
		})
	}

	// Create completion check
	completionCheckState := domain.CheckStatePlanning
	req.Checks = append(req.Checks, &service.CheckWrite{
		ID:    "completion",
		State: &completionCheckState,
		Kind:  string(CheckKindCompletion),
		Options: map[string]any{
			"change_id": change.ID,
		},
	})

	// Create completion stage that depends on all executor stages
	completionDeps := []domain.DependencyRef{}
	for _, check := range resp.Checks {
		completionDeps = append(completionDeps, domain.DependencyRef{
			TargetType: domain.NodeTypeStage,
			TargetID:   fmt.Sprintf("exec:%s", check.ID),
		})
	}

	completionStageState := domain.StageStatePlanned
	req.Stages = append(req.Stages, &service.StageWrite{
		ID:    "completion",
		State: &completionStageState,
		Args: map[string]any{
			"stage_type": string(StageTypeCompletion),
			"change_id":  change.ID,
		},
		Assignments: []domain.Assignment{
			{TargetCheckID: "completion", GoalState: domain.CheckStateFinal},
		},
		Dependencies: &domain.DependencyGroup{
			Predicate:    domain.PredicateAND,
			Dependencies: completionDeps,
		},
	})

	_, err = r.orchestrator.WriteNodes(ctx, req)
	return err
}

// executeBuildStage runs a build.
func (r *PipelineRunner) executeBuildStage(ctx context.Context, workPlanID string, stage *domain.Stage, change *ChangeInfo) error {
	checkID, _ := stage.Args["check_id"].(string)

	// Get the check to retrieve build config
	resp, err := r.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		CheckIDs:      []string{checkID},
		IncludeChecks: true,
	})
	if err != nil {
		return err
	}
	if len(resp.Checks) == 0 {
		return fmt.Errorf("check %s not found", checkID)
	}
	check := resp.Checks[0]

	// Build the config from check options
	config := &BuildConfig{
		ID:       check.Options["build_config_id"].(string),
		Name:     check.Options["name"].(string),
		Target:   check.Options["target"].(string),
		Platform: check.Options["platform"].(string),
	}

	// Execute the build
	result, err := r.executor.ExecuteBuild(ctx, config, change)
	if err != nil {
		return err
	}

	// Finalize the check with results
	waitingState := domain.CheckStateWaiting
	finalState := domain.CheckStateFinal

	// First transition to WAITING, then add result and finalize
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{
				ID:    checkID,
				State: &waitingState,
			},
		},
	}); err != nil {
		return err
	}

	resultData := map[string]any{
		"success":   result.Success,
		"duration":  result.Duration.String(),
		"logs":      result.Logs,
		"artifacts": result.Artifacts,
	}

	var failure *domain.Failure
	if !result.Success {
		failure = &domain.Failure{Message: "Build failed"}
	}

	_, err = r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{
				ID:    checkID,
				State: &finalState,
				Result: &service.CheckResultWrite{
					OwnerType: "stage_attempt",
					OwnerID:   fmt.Sprintf("%s:%d", stage.ID, stage.CurrentAttempt().Idx),
					Data:      resultData,
					Finalize:  true,
					Failure:   failure,
				},
			},
		},
	})

	return err
}

// executeTestStage runs a test suite.
func (r *PipelineRunner) executeTestStage(ctx context.Context, workPlanID string, stage *domain.Stage, change *ChangeInfo) error {
	checkID, _ := stage.Args["check_id"].(string)

	// Get the check to retrieve test config
	resp, err := r.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		CheckIDs:      []string{checkID},
		IncludeChecks: true,
	})
	if err != nil {
		return err
	}
	if len(resp.Checks) == 0 {
		return fmt.Errorf("check %s not found", checkID)
	}
	check := resp.Checks[0]

	// Build the config from check options
	config := &TestConfig{
		ID:             check.Options["test_config_id"].(string),
		Name:           check.Options["name"].(string),
		Suite:          check.Options["suite"].(string),
		Platform:       check.Options["platform"].(string),
		DependsOnBuild: check.Options["depends_on_build"].(string),
	}

	// Get build result if this test depends on a build
	var buildResult *BuildResult
	if config.DependsOnBuild != "" {
		buildCheckID := fmt.Sprintf("build:%s", config.DependsOnBuild)
		buildResp, err := r.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
			WorkPlanID:    workPlanID,
			CheckIDs:      []string{buildCheckID},
			IncludeChecks: true,
		})
		if err != nil {
			return err
		}
		if len(buildResp.Checks) > 0 && len(buildResp.Checks[0].Results) > 0 {
			buildData := buildResp.Checks[0].Results[0].Data
			buildResult = &BuildResult{
				Success: buildData["success"].(bool),
			}
			if artifacts, ok := buildData["artifacts"].([]any); ok {
				for _, a := range artifacts {
					buildResult.Artifacts = append(buildResult.Artifacts, a.(string))
				}
			}
		}
	}

	// Execute the test
	result, err := r.executor.ExecuteTest(ctx, config, change, buildResult)
	if err != nil {
		return err
	}

	// Finalize the check with results
	waitingState := domain.CheckStateWaiting
	finalState := domain.CheckStateFinal

	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{
				ID:    checkID,
				State: &waitingState,
			},
		},
	}); err != nil {
		return err
	}

	resultData := map[string]any{
		"success":  result.Success,
		"duration": result.Duration.String(),
		"logs":     result.Logs,
		"passed":   result.Passed,
		"failed":   result.Failed,
		"skipped":  result.Skipped,
	}

	var failure *domain.Failure
	if !result.Success {
		failure = &domain.Failure{Message: fmt.Sprintf("Test failed: %d failures", result.Failed)}
	}

	_, err = r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{
				ID:    checkID,
				State: &finalState,
				Result: &service.CheckResultWrite{
					OwnerType: "stage_attempt",
					OwnerID:   fmt.Sprintf("%s:%d", stage.ID, stage.CurrentAttempt().Idx),
					Data:      resultData,
					Finalize:  true,
					Failure:   failure,
				},
			},
		},
	})

	return err
}

// executeCompletionStage finalizes the pipeline and updates Gerrit.
func (r *PipelineRunner) executeCompletionStage(ctx context.Context, workPlanID string, stage *domain.Stage, change *ChangeInfo) error {
	// Query all checks to summarize results
	resp, err := r.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		IncludeChecks: true,
	})
	if err != nil {
		return err
	}

	// Summarize results
	var totalBuilds, passedBuilds, failedBuilds int
	var totalTests, passedTests, failedTests int
	var failureMessages []string

	for _, check := range resp.Checks {
		if check.Kind == string(CheckKindCompletion) {
			continue
		}

		if len(check.Results) > 0 {
			result := check.Results[0]
			success, _ := result.Data["success"].(bool)

			if check.Kind == string(CheckKindBuild) {
				totalBuilds++
				if success {
					passedBuilds++
				} else {
					failedBuilds++
					failureMessages = append(failureMessages, fmt.Sprintf("Build %s failed", check.ID))
				}
			} else if check.Kind == string(CheckKindTest) {
				totalTests++
				if success {
					passedTests++
				} else {
					failedTests++
					failed, _ := result.Data["failed"].(int)
					failureMessages = append(failureMessages, fmt.Sprintf("Test %s: %d failures", check.ID, failed))
				}
			}
		}
	}

	// Build summary message
	var summary strings.Builder
	summary.WriteString("CI Pipeline Complete\n\n")
	summary.WriteString(fmt.Sprintf("Builds: %d/%d passed\n", passedBuilds, totalBuilds))
	summary.WriteString(fmt.Sprintf("Tests: %d/%d passed\n", passedTests, totalTests))

	if len(failureMessages) > 0 {
		summary.WriteString("\nFailures:\n")
		for _, msg := range failureMessages {
			summary.WriteString(fmt.Sprintf("  - %s\n", msg))
		}
	}

	// Post comment to Gerrit
	if err := r.gerrit.PostComment(ctx, change.ID, summary.String()); err != nil {
		return fmt.Errorf("failed to post comment: %w", err)
	}

	// Set verified label
	verifiedValue := 1
	if failedBuilds > 0 || failedTests > 0 {
		verifiedValue = -1
	}

	var verifiedMsg string
	if verifiedValue > 0 {
		verifiedMsg = "All checks passed!"
	} else {
		verifiedMsg = fmt.Sprintf("%d failure(s) detected", len(failureMessages))
	}

	if err := r.gerrit.SetVerified(ctx, change.ID, verifiedValue, verifiedMsg); err != nil {
		return fmt.Errorf("failed to set verified: %w", err)
	}

	// Finalize the completion check: PLANNING → PLANNED → WAITING → FINAL
	plannedState := domain.CheckStatePlanned
	waitingState := domain.CheckStateWaiting
	finalState := domain.CheckStateFinal

	// PLANNING → PLANNED
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{
				ID:    "completion",
				State: &plannedState,
			},
		},
	}); err != nil {
		return err
	}

	// PLANNED → WAITING
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{
				ID:    "completion",
				State: &waitingState,
			},
		},
	}); err != nil {
		return err
	}

	// WAITING → FINAL with result
	_, err = r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{
				ID:    "completion",
				State: &finalState,
				Result: &service.CheckResultWrite{
					OwnerType: "stage_attempt",
					OwnerID:   fmt.Sprintf("%s:%d", stage.ID, stage.CurrentAttempt().Idx),
					Data: map[string]any{
						"total_builds":  totalBuilds,
						"passed_builds": passedBuilds,
						"failed_builds": failedBuilds,
						"total_tests":   totalTests,
						"passed_tests":  passedTests,
						"failed_tests":  failedTests,
						"verified":      verifiedValue,
					},
					Finalize: true,
				},
			},
		},
	})

	return err
}

// completeStage marks a stage as complete.
func (r *PipelineRunner) completeStage(ctx context.Context, workPlanID, stageID string) error {
	// First, complete the attempt
	attemptState := domain.AttemptStateComplete
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages: []*service.StageWrite{
			{
				ID: stageID,
				CurrentAttempt: &service.AttemptWrite{
					State: &attemptState,
				},
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to complete attempt: %w", err)
	}

	// Then, finalize the stage
	stageState := domain.StageStateFinal
	_, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages: []*service.StageWrite{
			{
				ID:    stageID,
				State: &stageState,
			},
		},
	})
	return err
}

// failStage marks a stage as failed.
func (r *PipelineRunner) failStage(ctx context.Context, workPlanID, stageID, message string) error {
	// First, fail the attempt
	attemptState := domain.AttemptStateIncomplete
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages: []*service.StageWrite{
			{
				ID: stageID,
				CurrentAttempt: &service.AttemptWrite{
					State:   &attemptState,
					Failure: &domain.Failure{Message: message},
				},
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to mark attempt as failed: %w", err)
	}

	// Then, finalize the stage
	stageState := domain.StageStateFinal
	_, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages: []*service.StageWrite{
			{
				ID:    stageID,
				State: &stageState,
			},
		},
	})
	return err
}
