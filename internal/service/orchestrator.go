package service

import (
	"context"
	"fmt"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/storage"
	"github.com/example/turboci-lite/pkg/id"
)

// OrchestratorService provides the core workflow orchestration logic.
type OrchestratorService struct {
	storage storage.Storage
}

// NewOrchestrator creates a new OrchestratorService.
func NewOrchestrator(store storage.Storage) *OrchestratorService {
	return &OrchestratorService{storage: store}
}

// CreateWorkPlanRequest is the request for CreateWorkPlan.
type CreateWorkPlanRequest struct {
	Metadata map[string]string
}

// CreateWorkPlan creates a new empty WorkPlan.
func (s *OrchestratorService) CreateWorkPlan(ctx context.Context, req *CreateWorkPlanRequest) (*domain.WorkPlan, error) {
	wp := domain.NewWorkPlan(id.Generate())
	if req != nil && req.Metadata != nil {
		wp.Metadata = req.Metadata
	}

	uow, err := s.storage.BeginImmediate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer uow.Rollback()

	if err := uow.WorkPlans().Create(ctx, wp); err != nil {
		return nil, fmt.Errorf("failed to create work plan: %w", err)
	}

	if err := uow.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}

	return wp, nil
}

// GetWorkPlan retrieves a WorkPlan by ID.
func (s *OrchestratorService) GetWorkPlan(ctx context.Context, id string) (*domain.WorkPlan, error) {
	uow, err := s.storage.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer uow.Rollback()

	wp, err := uow.WorkPlans().Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return wp, nil
}

// WriteNodesRequest is the request for WriteNodes.
type WriteNodesRequest struct {
	WorkPlanID string
	Checks     []*CheckWrite
	Stages     []*StageWrite
}

// CheckWrite describes a check to create or update.
type CheckWrite struct {
	ID           string
	State        *domain.CheckState
	Kind         string
	Options      map[string]any
	Dependencies *domain.DependencyGroup
	Result       *CheckResultWrite
}

// CheckResultWrite describes a result to add to a check.
type CheckResultWrite struct {
	OwnerType string
	OwnerID   string
	Data      map[string]any
	Finalize  bool
	Failure   *domain.Failure
}

// StageWrite describes a stage to create or update.
type StageWrite struct {
	ID             string
	State          *domain.StageState
	Args           map[string]any
	Assignments    []domain.Assignment
	Dependencies   *domain.DependencyGroup
	ExecutionMode  *domain.ExecutionMode // Sync or async execution
	RunnerType     string                // Which runner type handles this stage
	CurrentAttempt *AttemptWrite
}

// AttemptWrite describes an update to the current attempt.
type AttemptWrite struct {
	State      *domain.AttemptState
	ProcessUID string
	Details    map[string]any
	Progress   *domain.ProgressEntry
	Failure    *domain.Failure
}

// WriteNodesResponse is the response from WriteNodes.
type WriteNodesResponse struct {
	Checks []*domain.Check
	Stages []*domain.Stage
}

// WriteNodes atomically writes or updates multiple nodes within a WorkPlan.
func (s *OrchestratorService) WriteNodes(ctx context.Context, req *WriteNodesRequest) (*WriteNodesResponse, error) {
	uow, err := s.storage.BeginImmediate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer uow.Rollback()

	// Verify work plan exists
	if _, err := uow.WorkPlans().Get(ctx, req.WorkPlanID); err != nil {
		return nil, fmt.Errorf("work plan not found: %w", err)
	}

	resp := &WriteNodesResponse{}

	// Process checks
	for _, cw := range req.Checks {
		check, err := s.writeCheck(ctx, uow, req.WorkPlanID, cw)
		if err != nil {
			return nil, fmt.Errorf("failed to write check %s: %w", cw.ID, err)
		}
		resp.Checks = append(resp.Checks, check)
	}

	// Process stages
	for _, sw := range req.Stages {
		stage, err := s.writeStage(ctx, uow, req.WorkPlanID, sw)
		if err != nil {
			return nil, fmt.Errorf("failed to write stage %s: %w", sw.ID, err)
		}
		resp.Stages = append(resp.Stages, stage)
	}

	// Resolve dependencies and advance states
	if err := s.resolveDependencies(ctx, uow, req.WorkPlanID); err != nil {
		return nil, fmt.Errorf("failed to resolve dependencies: %w", err)
	}

	if err := uow.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}

	return resp, nil
}

// QueryNodesRequest is the request for QueryNodes.
type QueryNodesRequest struct {
	WorkPlanID    string
	CheckIDs      []string
	StageIDs      []string
	CheckStates   []domain.CheckState
	StageStates   []domain.StageState
	IncludeChecks bool
	IncludeStages bool
}

// QueryNodesResponse is the response from QueryNodes.
type QueryNodesResponse struct {
	Checks []*domain.Check
	Stages []*domain.Stage
}

// QueryNodes queries nodes in a WorkPlan.
func (s *OrchestratorService) QueryNodes(ctx context.Context, req *QueryNodesRequest) (*QueryNodesResponse, error) {
	uow, err := s.storage.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer uow.Rollback()

	resp := &QueryNodesResponse{}

	if req.IncludeChecks {
		opts := storage.ListOptions{
			IDs:         req.CheckIDs,
			CheckStates: req.CheckStates,
		}
		checks, err := uow.Checks().List(ctx, req.WorkPlanID, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list checks: %w", err)
		}
		resp.Checks = checks
	}

	if req.IncludeStages {
		opts := storage.ListOptions{
			IDs:         req.StageIDs,
			StageStates: req.StageStates,
		}
		stages, err := uow.Stages().List(ctx, req.WorkPlanID, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list stages: %w", err)
		}
		resp.Stages = stages
	}

	return resp, nil
}

// writeCheck creates or updates a check.
func (s *OrchestratorService) writeCheck(ctx context.Context, uow storage.UnitOfWork, workPlanID string, cw *CheckWrite) (*domain.Check, error) {
	// Try to get existing check
	var isNew bool
	check, err := uow.Checks().Get(ctx, workPlanID, cw.ID)
	if err == domain.ErrNotFound {
		// Create new check
		check = domain.NewCheck(workPlanID, cw.ID)
		isNew = true
	} else if err != nil {
		return nil, err
	}

	// Update fields
	if cw.Kind != "" {
		check.Kind = cw.Kind
	}
	if cw.Options != nil {
		for k, v := range cw.Options {
			check.Options[k] = v
		}
	}
	if cw.Dependencies != nil {
		check.Dependencies = cw.Dependencies
	}

	// Handle state transition
	if cw.State != nil && *cw.State != check.State {
		if err := check.SetState(*cw.State); err != nil {
			return nil, err
		}
	}

	// Add result if provided
	if cw.Result != nil {
		result := domain.NewCheckResult(cw.Result.OwnerType, cw.Result.OwnerID, cw.Result.Data)
		if cw.Result.Finalize {
			result.Finalize()
		}
		if cw.Result.Failure != nil {
			result.Failure = cw.Result.Failure
		}
		if err := uow.Checks().AddResult(ctx, workPlanID, check.ID, &result); err != nil {
			return nil, err
		}
		check.Results = append(check.Results, result)
	}

	check.UpdatedAt = time.Now().UTC()

	// Save
	if isNew {
		if err := uow.Checks().Create(ctx, check); err != nil {
			return nil, err
		}
		// Create dependencies in the dependency table
		if check.Dependencies != nil {
			if err := s.createDependencies(ctx, uow, workPlanID, domain.NodeTypeCheck, check.ID, check.Dependencies); err != nil {
				return nil, err
			}
		}
	} else {
		if err := uow.Checks().Update(ctx, check); err != nil {
			return nil, err
		}
	}

	return check, nil
}

// writeStage creates or updates a stage.
func (s *OrchestratorService) writeStage(ctx context.Context, uow storage.UnitOfWork, workPlanID string, sw *StageWrite) (*domain.Stage, error) {
	// Try to get existing stage
	var isNew bool
	stage, err := uow.Stages().Get(ctx, workPlanID, sw.ID)
	if err == domain.ErrNotFound {
		// Create new stage
		stage = domain.NewStage(workPlanID, sw.ID)
		isNew = true
	} else if err != nil {
		return nil, err
	}

	stageModified := isNew // Track if the stage itself needs updating

	// Update fields
	if sw.Args != nil {
		for k, v := range sw.Args {
			stage.Args[k] = v
		}
		stageModified = true
	}
	if sw.Assignments != nil {
		stage.Assignments = sw.Assignments
		stageModified = true
	}
	if sw.Dependencies != nil {
		stage.Dependencies = sw.Dependencies
		stageModified = true
	}
	if sw.ExecutionMode != nil {
		stage.ExecutionMode = *sw.ExecutionMode
		stageModified = true
	}
	if sw.RunnerType != "" {
		stage.RunnerType = sw.RunnerType
		stageModified = true
	}

	// Handle state transition
	if sw.State != nil && *sw.State != stage.State {
		if err := stage.SetState(*sw.State); err != nil {
			return nil, err
		}
		stageModified = true
	}

	// Handle attempt update
	if sw.CurrentAttempt != nil {
		attempt := stage.CurrentAttempt()
		if attempt == nil {
			return nil, fmt.Errorf("no current attempt to update")
		}

		if sw.CurrentAttempt.State != nil {
			if err := attempt.SetState(*sw.CurrentAttempt.State); err != nil {
				return nil, err
			}
		}
		if sw.CurrentAttempt.ProcessUID != "" {
			if err := attempt.Claim(sw.CurrentAttempt.ProcessUID); err != nil {
				return nil, err
			}
		}
		if sw.CurrentAttempt.Details != nil {
			if attempt.Details == nil {
				attempt.Details = make(map[string]any)
			}
			for k, v := range sw.CurrentAttempt.Details {
				attempt.Details[k] = v
			}
		}
		if sw.CurrentAttempt.Progress != nil {
			attempt.AddProgress(sw.CurrentAttempt.Progress.Message, sw.CurrentAttempt.Progress.Details)
		}
		if sw.CurrentAttempt.Failure != nil {
			attempt.SetFailure(sw.CurrentAttempt.Failure.Message)
		}

		if err := uow.Stages().UpdateAttempt(ctx, workPlanID, stage.ID, attempt.Idx, attempt); err != nil {
			return nil, err
		}
	}

	// Save stage only if it was modified (not just attempt updates)
	if stageModified {
		stage.UpdatedAt = time.Now().UTC()

		if isNew {
			if err := uow.Stages().Create(ctx, stage); err != nil {
				return nil, err
			}
			// Create dependencies in the dependency table
			if stage.Dependencies != nil {
				if err := s.createDependencies(ctx, uow, workPlanID, domain.NodeTypeStage, stage.ID, stage.Dependencies); err != nil {
					return nil, err
				}
			}
		} else {
			if err := uow.Stages().Update(ctx, stage); err != nil {
				return nil, err
			}
		}
	}

	return stage, nil
}

// createDependencies creates dependency records for a node.
func (s *OrchestratorService) createDependencies(ctx context.Context, uow storage.UnitOfWork, workPlanID string, sourceType domain.NodeType, sourceID string, group *domain.DependencyGroup) error {
	if group == nil {
		return nil
	}

	for _, ref := range group.Dependencies {
		dep := domain.NewDependency(workPlanID, sourceType, sourceID, ref.TargetType, ref.TargetID)
		if err := uow.Dependencies().Create(ctx, dep); err != nil {
			return err
		}
	}

	return nil
}
