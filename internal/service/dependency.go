package service

import (
	"context"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/storage"
)

// resolveDependencies resolves dependencies and advances node states.
func (s *OrchestratorService) resolveDependencies(ctx context.Context, uow storage.UnitOfWork, workPlanID string) error {
	// Get all checks and stages to check for finalized nodes
	checks, err := uow.Checks().List(ctx, workPlanID, storage.ListOptions{})
	if err != nil {
		return err
	}

	stages, err := uow.Stages().List(ctx, workPlanID, storage.ListOptions{})
	if err != nil {
		return err
	}

	// Build a map of finalized nodes
	finalizedNodes := make(map[string]bool)
	for _, check := range checks {
		if check.State == domain.CheckStateFinal {
			key := string(domain.NodeTypeCheck) + ":" + check.ID
			finalizedNodes[key] = true
		}
	}
	for _, stage := range stages {
		if stage.State == domain.StageStateFinal {
			key := string(domain.NodeTypeStage) + ":" + stage.ID
			finalizedNodes[key] = true
		}
	}

	// For each finalized node, mark dependencies as resolved
	for _, check := range checks {
		if check.State == domain.CheckStateFinal {
			if err := s.propagateResolution(ctx, uow, workPlanID, domain.NodeTypeCheck, check.ID); err != nil {
				return err
			}
		}
	}
	for _, stage := range stages {
		if stage.State == domain.StageStateFinal {
			if err := s.propagateResolution(ctx, uow, workPlanID, domain.NodeTypeStage, stage.ID); err != nil {
				return err
			}
		}
	}

	// Check if any nodes can advance state due to resolved dependencies
	if err := s.advanceNodeStates(ctx, uow, workPlanID); err != nil {
		return err
	}

	return nil
}

// propagateResolution marks all dependencies pointing to a finalized node as resolved.
func (s *OrchestratorService) propagateResolution(ctx context.Context, uow storage.UnitOfWork, workPlanID string, nodeType domain.NodeType, nodeID string) error {
	deps, err := uow.Dependencies().GetByTarget(ctx, workPlanID, nodeType, nodeID)
	if err != nil {
		return err
	}

	for _, dep := range deps {
		if !dep.Resolved {
			if err := uow.Dependencies().MarkResolved(ctx, dep.ID, true); err != nil {
				return err
			}
		}
	}

	return nil
}

// advanceNodeStates checks and advances states for nodes whose dependencies are satisfied.
func (s *OrchestratorService) advanceNodeStates(ctx context.Context, uow storage.UnitOfWork, workPlanID string) error {
	// Advance checks from PLANNED to WAITING
	checks, err := uow.Checks().List(ctx, workPlanID, storage.ListOptions{
		CheckStates: []domain.CheckState{domain.CheckStatePlanned},
	})
	if err != nil {
		return err
	}

	for _, check := range checks {
		if check.Dependencies == nil || len(check.Dependencies.Dependencies) == 0 {
			// No dependencies, can advance immediately
			check.State = domain.CheckStateWaiting
			check.UpdatedAt = time.Now().UTC()
			if err := uow.Checks().Update(ctx, check); err != nil {
				return err
			}
			continue
		}

		// Check if dependencies are satisfied
		resolved, err := s.getDependencyResolution(ctx, uow, workPlanID, domain.NodeTypeCheck, check.ID)
		if err != nil {
			return err
		}

		if check.Dependencies.IsSatisfied(resolved) {
			check.State = domain.CheckStateWaiting
			check.UpdatedAt = time.Now().UTC()
			if err := uow.Checks().Update(ctx, check); err != nil {
				return err
			}
		}
	}

	// Advance stages from PLANNED to ATTEMPTING
	stages, err := uow.Stages().List(ctx, workPlanID, storage.ListOptions{
		StageStates: []domain.StageState{domain.StageStatePlanned},
	})
	if err != nil {
		return err
	}

	for _, stage := range stages {
		if stage.Dependencies == nil || len(stage.Dependencies.Dependencies) == 0 {
			// No dependencies, can advance immediately
			if err := s.advanceStageToAttempting(ctx, uow, stage); err != nil {
				return err
			}
			continue
		}

		// Check if dependencies are satisfied
		resolved, err := s.getDependencyResolution(ctx, uow, workPlanID, domain.NodeTypeStage, stage.ID)
		if err != nil {
			return err
		}

		if stage.Dependencies.IsSatisfied(resolved) {
			if err := s.advanceStageToAttempting(ctx, uow, stage); err != nil {
				return err
			}
		}
	}

	return nil
}

// getDependencyResolution returns a map of resolved dependencies for a node.
func (s *OrchestratorService) getDependencyResolution(ctx context.Context, uow storage.UnitOfWork, workPlanID string, nodeType domain.NodeType, nodeID string) (map[string]bool, error) {
	deps, err := uow.Dependencies().GetBySource(ctx, workPlanID, nodeType, nodeID)
	if err != nil {
		return nil, err
	}

	resolved := make(map[string]bool)
	for _, dep := range deps {
		if dep.Resolved && dep.Satisfied != nil {
			key := string(dep.TargetType) + ":" + dep.TargetID
			resolved[key] = *dep.Satisfied
		}
	}

	return resolved, nil
}

// advanceStageToAttempting advances a stage to ATTEMPTING, creates the first attempt,
// and queues a StageExecution for the dispatcher.
func (s *OrchestratorService) advanceStageToAttempting(ctx context.Context, uow storage.UnitOfWork, stage *domain.Stage) error {
	stage.State = domain.StageStateAttempting
	stage.UpdatedAt = time.Now().UTC()

	// Create first attempt
	now := time.Now().UTC()
	attempt := domain.Attempt{
		Idx:       1,
		State:     domain.AttemptStatePending,
		Details:   make(map[string]any),
		CreatedAt: now,
		UpdatedAt: now,
	}
	stage.Attempts = append(stage.Attempts, attempt)

	if err := uow.Stages().Update(ctx, stage); err != nil {
		return err
	}

	if err := uow.Stages().AddAttempt(ctx, stage.WorkPlanID, stage.ID, &attempt); err != nil {
		return err
	}

	// Create a StageExecution entry for the dispatcher to pick up.
	// Only create if runner type is specified (push-based execution).
	if stage.RunnerType != "" {
		executionMode := stage.ExecutionMode
		if executionMode == domain.ExecutionModeUnknown {
			executionMode = domain.ExecutionModeSync // Default to sync
		}
		exec := domain.NewStageExecution(stage.WorkPlanID, stage.ID, attempt.Idx, stage.RunnerType, executionMode)
		if err := uow.StageExecutions().Create(ctx, exec); err != nil {
			return err
		}
	}

	return nil
}
