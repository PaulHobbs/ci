package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/example/turboci-lite/culprit/decoder"
	"github.com/example/turboci-lite/culprit/domain"
	"github.com/example/turboci-lite/culprit/matrix"
	orchDomain "github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage"
)

// StageType identifies the type of culprit finder stage.
type StageType string

const (
	StageTypeTestGroup StageType = "culprit_test_group"
	StageTypeDecode    StageType = "culprit_decode"
)

// CheckKind identifies the type of culprit finder check.
type CheckKind string

const (
	CheckKindTestGroup CheckKind = "culprit_test_group"
	CheckKindDecode    CheckKind = "culprit_decode"
)

// CulpritSearchRunner coordinates culprit finding using the orchestrator.
type CulpritSearchRunner struct {
	orchestrator *service.OrchestratorService
	materializer CommitMaterializer
	testRunner   TestRunner
	idGenerator  func() string
	processUID   string

	mu       sync.RWMutex
	sessions map[string]*domain.SearchSession
	results  map[string][]domain.TestGroupResult // sessionID -> results
}

// NewRunner creates a new CulpritSearchRunner.
func NewRunner(
	storage storage.Storage,
	materializer CommitMaterializer,
	testRunner TestRunner,
	idGenerator func() string,
) *CulpritSearchRunner {
	return &CulpritSearchRunner{
		orchestrator: service.NewOrchestrator(storage),
		materializer: materializer,
		testRunner:   testRunner,
		idGenerator:  idGenerator,
		processUID:   idGenerator(),
		sessions:     make(map[string]*domain.SearchSession),
		results:      make(map[string][]domain.TestGroupResult),
	}
}

// StartSearch initiates a culprit search and runs it to completion.
func (r *CulpritSearchRunner) StartSearch(ctx context.Context, req *SearchRequest) (*domain.SearchSession, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Create session
	session := domain.NewSearchSession(
		r.idGenerator(),
		req.CommitRange,
		req.TestCommand,
		req.TestTimeout,
		req.Config,
	)

	// Build test matrix
	builder := matrix.NewBuilder(r.idGenerator)
	testMatrix, err := builder.Build(req.CommitRange.Commits, req.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to build test matrix: %w", err)
	}
	session.SetMatrix(testMatrix)

	// Store session
	r.mu.Lock()
	r.sessions[session.ID] = session
	r.results[session.ID] = make([]domain.TestGroupResult, 0, testMatrix.TotalTests())
	r.mu.Unlock()

	// Create work plan in orchestrator
	wp, err := r.orchestrator.CreateWorkPlan(ctx, &service.CreateWorkPlanRequest{
		Metadata: map[string]string{
			"type":       "culprit_search",
			"session_id": session.ID,
			"repository": req.CommitRange.Repository,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create work plan: %w", err)
	}
	session.WorkPlanID = wp.ID

	// Create all test group stages and checks
	if err := r.createTestGroupNodes(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to create test group nodes: %w", err)
	}

	// Update session status
	if err := session.SetStatus(domain.StatusRunning); err != nil {
		return nil, err
	}

	return session, nil
}

// createTestGroupNodes creates checks and stages for all test groups.
func (r *CulpritSearchRunner) createTestGroupNodes(ctx context.Context, session *domain.SearchSession) error {
	testMatrix := session.Matrix
	req := &service.WriteNodesRequest{WorkPlanID: session.WorkPlanID}

	// Create a check and stage for each test group instance
	var allStageIDs []string
	for _, group := range testMatrix.AllGroups() {
		checkID := fmt.Sprintf("group-%d-r%d", group.GroupIndex, group.Repetition)
		stageID := fmt.Sprintf("test-%d-r%d", group.GroupIndex, group.Repetition)
		allStageIDs = append(allStageIDs, stageID)

		// Create check (in PLANNING state initially)
		checkState := orchDomain.CheckStatePlanning
		req.Checks = append(req.Checks, &service.CheckWrite{
			ID:    checkID,
			State: &checkState,
			Kind:  string(CheckKindTestGroup),
			Options: map[string]any{
				"group_id":       group.GroupID,
				"group_index":    group.GroupIndex,
				"repetition":     group.Repetition,
				"commit_indices": group.CommitIndices,
			},
		})

		// Create stage (all run in parallel, no inter-dependencies)
		stageState := orchDomain.StageStatePlanned
		req.Stages = append(req.Stages, &service.StageWrite{
			ID:    stageID,
			State: &stageState,
			Args: map[string]any{
				"stage_type":     string(StageTypeTestGroup),
				"check_id":       checkID,
				"group_id":       group.GroupID,
				"group_index":    group.GroupIndex,
				"repetition":     group.Repetition,
				"commit_indices": group.CommitIndices,
			},
			Assignments: []orchDomain.Assignment{
				{TargetCheckID: checkID, GoalState: orchDomain.CheckStateFinal},
			},
			// No dependencies - all test groups run in parallel
		})
	}

	// Create decode check and stage
	decodeCheckState := orchDomain.CheckStatePlanning
	req.Checks = append(req.Checks, &service.CheckWrite{
		ID:    "decode",
		State: &decodeCheckState,
		Kind:  string(CheckKindDecode),
	})

	// Decode stage depends on all test stages
	decodeDeps := make([]orchDomain.DependencyRef, len(allStageIDs))
	for i, stageID := range allStageIDs {
		decodeDeps[i] = orchDomain.DependencyRef{
			TargetType: orchDomain.NodeTypeStage,
			TargetID:   stageID,
		}
	}

	decodeStageState := orchDomain.StageStatePlanned
	req.Stages = append(req.Stages, &service.StageWrite{
		ID:    "decode",
		State: &decodeStageState,
		Args: map[string]any{
			"stage_type": string(StageTypeDecode),
		},
		Assignments: []orchDomain.Assignment{
			{TargetCheckID: "decode", GoalState: orchDomain.CheckStateFinal},
		},
		Dependencies: &orchDomain.DependencyGroup{
			Predicate:    orchDomain.PredicateAND,
			Dependencies: decodeDeps,
		},
	})

	_, err := r.orchestrator.WriteNodes(ctx, req)
	return err
}

// RunLoop processes stages until completion.
func (r *CulpritSearchRunner) RunLoop(ctx context.Context, sessionID string) error {
	r.mu.RLock()
	session, exists := r.sessions[sessionID]
	r.mu.RUnlock()
	if !exists {
		return domain.ErrSessionNotFound
	}

	iteration := 0
	maxIterations := 10000 // Safety limit

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

		// Query for stages that need execution
		stages, err := r.queryPendingStages(ctx, session.WorkPlanID)
		if err != nil {
			return fmt.Errorf("failed to query stages: %w", err)
		}

		if len(stages) == 0 {
			// Check if all stages are complete
			allDone, err := r.checkAllComplete(ctx, session.WorkPlanID)
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
			if err := r.executeStage(ctx, session, stage); err != nil {
				return fmt.Errorf("failed to execute stage %s: %w", stage.ID, err)
			}
		}
	}
}

// queryPendingStages returns stages ready for execution.
func (r *CulpritSearchRunner) queryPendingStages(ctx context.Context, workPlanID string) ([]*orchDomain.Stage, error) {
	resp, err := r.orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		StageStates:   []orchDomain.StageState{orchDomain.StageStateAttempting},
		IncludeStages: true,
	})
	if err != nil {
		return nil, err
	}

	// Filter to stages with pending attempts
	var pending []*orchDomain.Stage
	for _, stage := range resp.Stages {
		attempt := stage.CurrentAttempt()
		if attempt != nil && attempt.State == orchDomain.AttemptStatePending {
			pending = append(pending, stage)
		}
	}
	return pending, nil
}

// checkAllComplete returns true if all stages are in FINAL state.
func (r *CulpritSearchRunner) checkAllComplete(ctx context.Context, workPlanID string) (bool, error) {
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

	for _, stage := range resp.Stages {
		if stage.State != orchDomain.StageStateFinal {
			return false, nil
		}
	}
	return true, nil
}

// executeStage runs a stage based on its type.
func (r *CulpritSearchRunner) executeStage(ctx context.Context, session *domain.SearchSession, stage *orchDomain.Stage) error {
	stageType, _ := stage.Args["stage_type"].(string)

	// Claim the attempt
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: session.WorkPlanID,
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
	case StageTypeTestGroup:
		execErr = r.executeTestGroupStage(ctx, session, stage)
	case StageTypeDecode:
		execErr = r.executeDecodeStage(ctx, session, stage)
	default:
		execErr = fmt.Errorf("unknown stage type: %s", stageType)
	}

	// Complete or fail the stage
	if execErr != nil {
		return r.failStage(ctx, session.WorkPlanID, stage.ID, execErr.Error())
	}
	return r.completeStage(ctx, session.WorkPlanID, stage.ID)
}

// executeTestGroupStage materializes commits and runs the test.
func (r *CulpritSearchRunner) executeTestGroupStage(ctx context.Context, session *domain.SearchSession, stage *orchDomain.Stage) error {
	checkID, _ := stage.Args["check_id"].(string)
	groupIndex := int(stage.Args["group_index"].(float64))
	repetition := int(stage.Args["repetition"].(float64))

	// Get commit indices from the stage args
	commitIndicesRaw, _ := stage.Args["commit_indices"].([]any)
	commitIndices := make([]int, len(commitIndicesRaw))
	for i, v := range commitIndicesRaw {
		commitIndices[i] = int(v.(float64))
	}

	// Select commits for this group
	commits := session.CommitRange.SelectCommits(commitIndices)

	// Materialize the commits
	state, err := r.materializer.Materialize(ctx, session.CommitRange.BaseRef, commits)
	if err != nil {
		return fmt.Errorf("materialization failed: %w", err)
	}
	defer r.materializer.Cleanup(ctx, state)

	// Run the test
	testConfig := TestConfig{
		Command: session.TestCommand,
		Timeout: session.TestTimeout,
	}
	testResult, err := r.testRunner.Run(ctx, state, testConfig)
	if err != nil {
		return fmt.Errorf("test execution failed: %w", err)
	}

	// Store the result with correct metadata
	testResult.GroupID = stage.Args["group_id"].(string)
	testResult.GroupIndex = groupIndex
	testResult.Repetition = repetition

	r.mu.Lock()
	r.results[session.ID] = append(r.results[session.ID], *testResult)
	session.RecordResult(*testResult)
	r.mu.Unlock()

	// Update the check with the result
	return r.updateCheckWithResult(ctx, session.WorkPlanID, checkID, stage.ID, testResult)
}

// updateCheckWithResult updates a check with a test result.
func (r *CulpritSearchRunner) updateCheckWithResult(
	ctx context.Context,
	workPlanID string,
	checkID string,
	stageID string,
	result *domain.TestGroupResult,
) error {
	// Transition check: PLANNING → PLANNED → WAITING → FINAL
	plannedState := orchDomain.CheckStatePlanned
	waitingState := orchDomain.CheckStateWaiting
	finalState := orchDomain.CheckStateFinal

	// PLANNING → PLANNED
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{ID: checkID, State: &plannedState},
		},
	}); err != nil {
		return err
	}

	// PLANNED → WAITING
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{ID: checkID, State: &waitingState},
		},
	}); err != nil {
		return err
	}

	// WAITING → FINAL with result
	resultData := map[string]any{
		"outcome":     result.Outcome.String(),
		"duration_ms": result.Duration.Milliseconds(),
		"logs":        result.Logs,
	}

	var failure *orchDomain.Failure
	if result.Outcome == domain.OutcomeFail {
		failure = &orchDomain.Failure{Message: "Test failed"}
	} else if result.Outcome == domain.OutcomeInfra {
		failure = &orchDomain.Failure{Message: "Infrastructure failure"}
	}

	_, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{
				ID:    checkID,
				State: &finalState,
				Result: &service.CheckResultWrite{
					OwnerType: "stage_attempt",
					OwnerID:   stageID + ":0",
					Data:      resultData,
					Finalize:  true,
					Failure:   failure,
				},
			},
		},
	})
	return err
}

// executeDecodeStage runs the decoder on all collected results.
func (r *CulpritSearchRunner) executeDecodeStage(ctx context.Context, session *domain.SearchSession, stage *orchDomain.Stage) error {
	// Collect all results
	r.mu.RLock()
	results := make([]domain.TestGroupResult, len(r.results[session.ID]))
	copy(results, r.results[session.ID])
	r.mu.RUnlock()

	// Run the decoder
	dec := decoder.NewDecoder(session.Config)
	decodingResult, err := dec.Decode(session.CommitRange.Commits, session.Matrix, results)
	if err != nil {
		return fmt.Errorf("decoding failed: %w", err)
	}

	// Update session with result
	r.mu.Lock()
	session.SetResult(decodingResult)
	session.SetStatus(domain.StatusComplete)
	r.mu.Unlock()

	// Update the decode check
	return r.updateDecodeCheck(ctx, session.WorkPlanID, stage.ID, decodingResult)
}

// updateDecodeCheck updates the decode check with the decoding result.
func (r *CulpritSearchRunner) updateDecodeCheck(
	ctx context.Context,
	workPlanID string,
	stageID string,
	result *domain.DecodingResult,
) error {
	checkID := "decode"

	// Transition check: PLANNING → PLANNED → WAITING → FINAL
	plannedState := orchDomain.CheckStatePlanned
	waitingState := orchDomain.CheckStateWaiting
	finalState := orchDomain.CheckStateFinal

	// PLANNING → PLANNED
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{ID: checkID, State: &plannedState},
		},
	}); err != nil {
		return err
	}

	// PLANNED → WAITING
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{ID: checkID, State: &waitingState},
		},
	}); err != nil {
		return err
	}

	// Build culprit data for storage
	culpritsData := make([]map[string]any, len(result.IdentifiedCulprits))
	for i, c := range result.IdentifiedCulprits {
		culpritsData[i] = map[string]any{
			"sha":        c.Commit.SHA,
			"index":      c.Commit.Index,
			"score":      c.Score,
			"confidence": c.Confidence,
		}
	}

	resultData := map[string]any{
		"culprits":          culpritsData,
		"confidence":        result.Confidence,
		"innocent_commits":  result.InnocentCommits,
		"ambiguous_commits": result.AmbiguousCommits,
	}

	_, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks: []*service.CheckWrite{
			{
				ID:    checkID,
				State: &finalState,
				Result: &service.CheckResultWrite{
					OwnerType: "stage_attempt",
					OwnerID:   stageID + ":0",
					Data:      resultData,
					Finalize:  true,
				},
			},
		},
	})
	return err
}

// completeStage marks a stage as complete.
func (r *CulpritSearchRunner) completeStage(ctx context.Context, workPlanID, stageID string) error {
	attemptState := orchDomain.AttemptStateComplete
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

	stageState := orchDomain.StageStateFinal
	_, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages: []*service.StageWrite{
			{ID: stageID, State: &stageState},
		},
	})
	return err
}

// failStage marks a stage as failed.
func (r *CulpritSearchRunner) failStage(ctx context.Context, workPlanID, stageID, message string) error {
	attemptState := orchDomain.AttemptStateIncomplete
	if _, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages: []*service.StageWrite{
			{
				ID: stageID,
				CurrentAttempt: &service.AttemptWrite{
					State:   &attemptState,
					Failure: &orchDomain.Failure{Message: message},
				},
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to mark attempt as failed: %w", err)
	}

	stageState := orchDomain.StageStateFinal
	_, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages: []*service.StageWrite{
			{ID: stageID, State: &stageState},
		},
	})
	return err
}

// GetSession retrieves an existing search session.
func (r *CulpritSearchRunner) GetSession(ctx context.Context, sessionID string) (*domain.SearchSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, exists := r.sessions[sessionID]
	if !exists {
		return nil, domain.ErrSessionNotFound
	}
	return session, nil
}

// GetResults retrieves the current results for a session.
func (r *CulpritSearchRunner) GetResults(ctx context.Context, sessionID string) (*domain.DecodingResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, exists := r.sessions[sessionID]
	if !exists {
		return nil, domain.ErrSessionNotFound
	}
	return session.Result, nil
}

// WaitForCompletion runs the search and waits for completion.
func (r *CulpritSearchRunner) WaitForCompletion(ctx context.Context, sessionID string) (*domain.DecodingResult, error) {
	if err := r.RunLoop(ctx, sessionID); err != nil {
		return nil, err
	}
	return r.GetResults(ctx, sessionID)
}

// RunSearch is a convenience method that starts a search and runs it to completion.
func (r *CulpritSearchRunner) RunSearch(ctx context.Context, req *SearchRequest) (*domain.DecodingResult, error) {
	session, err := r.StartSearch(ctx, req)
	if err != nil {
		return nil, err
	}
	return r.WaitForCompletion(ctx, session.ID)
}
