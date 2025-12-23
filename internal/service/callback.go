package service

import (
	"context"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/storage"
)

// CallbackService handles callbacks from async stage runners.
type CallbackService struct {
	storage      storage.Storage
	orchestrator *OrchestratorService
}

// NewCallbackService creates a new callback service.
func NewCallbackService(store storage.Storage, orchestrator *OrchestratorService) *CallbackService {
	return &CallbackService{
		storage:      store,
		orchestrator: orchestrator,
	}
}

// UpdateExecutionRequest is the request for UpdateExecution.
type UpdateExecutionRequest struct {
	ExecutionID     string
	ProgressPercent int
	ProgressMessage string
}

// UpdateExecution updates the progress of an async execution.
func (s *CallbackService) UpdateExecution(ctx context.Context, req *UpdateExecutionRequest) error {
	if req.ExecutionID == "" {
		return domain.ErrInvalidArgument
	}

	uow, err := s.storage.BeginImmediate(ctx)
	if err != nil {
		return err
	}
	defer uow.Rollback()

	// Get the execution
	exec, err := uow.StageExecutions().Get(ctx, req.ExecutionID)
	if err != nil {
		return err
	}

	// Verify execution is in a valid state for updates
	if exec.State.IsFinal() {
		return domain.ErrInvalidState
	}

	// Mark as running if still dispatched
	if exec.State == domain.ExecutionStateDispatched {
		if err := uow.StageExecutions().MarkRunning(ctx, exec.ID); err != nil {
			return err
		}
	}

	// Update progress
	if err := uow.StageExecutions().UpdateProgress(ctx, exec.ID, req.ProgressPercent, req.ProgressMessage); err != nil {
		return err
	}

	// Also update the attempt progress and state
	stage, err := uow.Stages().Get(ctx, exec.WorkPlanID, exec.StageID)
	if err != nil {
		return err
	}

	if attempt := stage.CurrentAttempt(); attempt != nil && attempt.Idx == exec.AttemptIdx {
		// Transition attempt to RUNNING if still PENDING or SCHEDULED
		if attempt.State == domain.AttemptStatePending || attempt.State == domain.AttemptStateScheduled {
			if err := attempt.SetState(domain.AttemptStateRunning); err != nil {
				return err
			}
		}
		attempt.AddProgress(req.ProgressMessage, map[string]any{
			"percent": req.ProgressPercent,
		})
		if err := uow.Stages().UpdateAttempt(ctx, exec.WorkPlanID, exec.StageID, attempt.Idx, attempt); err != nil {
			return err
		}
	}

	return uow.Commit()
}

// CompleteExecutionRequest is the request for CompleteExecution.
type CompleteExecutionRequest struct {
	ExecutionID  string
	Success      bool
	ErrorMessage string
	CheckUpdates []*CheckUpdate
}

// CompleteExecution marks an async execution as complete.
func (s *CallbackService) CompleteExecution(ctx context.Context, req *CompleteExecutionRequest) error {
	if req.ExecutionID == "" {
		return domain.ErrInvalidArgument
	}

	uow, err := s.storage.BeginImmediate(ctx)
	if err != nil {
		return err
	}
	defer uow.Rollback()

	// Get the execution
	exec, err := uow.StageExecutions().Get(ctx, req.ExecutionID)
	if err != nil {
		return err
	}

	// Verify execution is in a valid state for completion
	if exec.State.IsFinal() {
		return domain.ErrInvalidState
	}

	// Mark complete or failed
	if req.Success {
		if err := uow.StageExecutions().MarkComplete(ctx, exec.ID); err != nil {
			return err
		}
	} else {
		if err := uow.StageExecutions().MarkFailed(ctx, exec.ID, req.ErrorMessage); err != nil {
			return err
		}
	}

	// Decrement runner load
	if exec.RunnerID != "" {
		if err := uow.StageRunners().DecrementLoad(ctx, exec.RunnerID); err != nil {
			// Log but don't fail
		}
	}

	// Update the attempt state
	stage, err := uow.Stages().Get(ctx, exec.WorkPlanID, exec.StageID)
	if err != nil {
		return err
	}

	if attempt := stage.CurrentAttempt(); attempt != nil && attempt.Idx == exec.AttemptIdx {
		if req.Success {
			if err := attempt.Complete(); err != nil {
				return err
			}
		} else {
			attempt.SetFailure(req.ErrorMessage)
		}
		attempt.UpdatedAt = time.Now().UTC()
		if err := uow.Stages().UpdateAttempt(ctx, exec.WorkPlanID, exec.StageID, attempt.Idx, attempt); err != nil {
			return err
		}
	}

	if err := uow.Commit(); err != nil {
		return err
	}

	// Apply check updates (outside the main transaction)
	if len(req.CheckUpdates) > 0 {
		if err := s.applyCheckUpdates(ctx, exec.WorkPlanID, req.CheckUpdates); err != nil {
			return err
		}
	}

	// Update stage state to FINAL if successful
	if req.Success {
		if err := s.updateStageState(ctx, exec.WorkPlanID, exec.StageID, domain.StageStateFinal); err != nil {
			return err
		}
	}

	return nil
}

// applyCheckUpdates applies check updates from a stage execution.
func (s *CallbackService) applyCheckUpdates(ctx context.Context, workPlanID string, updates []*CheckUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	// Build WriteNodes request with check updates
	var checkWrites []*CheckWrite
	for _, update := range updates {
		cw := &CheckWrite{
			ID:    update.CheckID,
			State: &update.State,
		}
		if update.Data != nil || update.Finalize {
			cw.Result = &CheckResultWrite{
				OwnerType: "stage",
				OwnerID:   "",
				Data:      update.Data,
				Finalize:  update.Finalize,
			}
		}
		checkWrites = append(checkWrites, cw)
	}

	_, err := s.orchestrator.WriteNodes(ctx, &WriteNodesRequest{
		WorkPlanID: workPlanID,
		Checks:     checkWrites,
	})
	return err
}

// updateStageState updates the stage state after execution completes.
func (s *CallbackService) updateStageState(ctx context.Context, workPlanID, stageID string, state domain.StageState) error {
	_, err := s.orchestrator.WriteNodes(ctx, &WriteNodesRequest{
		WorkPlanID: workPlanID,
		Stages: []*StageWrite{
			{
				ID:    stageID,
				State: &state,
			},
		},
	})
	return err
}

// GetExecution retrieves an execution by ID.
func (s *CallbackService) GetExecution(ctx context.Context, executionID string) (*domain.StageExecution, error) {
	if executionID == "" {
		return nil, domain.ErrInvalidArgument
	}

	uow, err := s.storage.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer uow.Rollback()

	return uow.StageExecutions().Get(ctx, executionID)
}
