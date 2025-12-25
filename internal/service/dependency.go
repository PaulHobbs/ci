package service

import (
	"context"
	"log"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/storage"
)

// resolveDependencies resolves dependencies and advances node states.
func (s *OrchestratorService) resolveDependencies(ctx context.Context, uow storage.UnitOfWork, workPlanID string) error {
	start := time.Now()
	
	// Get all checks and stages
	checks, err := uow.Checks().List(ctx, workPlanID, storage.ListOptions{})
	if err != nil {
		return err
	}

	stages, err := uow.Stages().List(ctx, workPlanID, storage.ListOptions{})
	if err != nil {
		return err
	}

	// Build a map of finalized nodes for quick lookup
	finalizedNodes := make(map[string]bool)
	for _, check := range checks {
		if check.State == domain.CheckStateFinal {
			finalizedNodes[string(domain.NodeTypeCheck)+":"+check.ID] = true
		}
	}
	for _, stage := range stages {
		if stage.State == domain.StageStateFinal {
			finalizedNodes[string(domain.NodeTypeStage)+":"+stage.ID] = true
		}
	}

	// Propagate resolution for all finalized nodes.
	// Optimization: Instead of propagateResolution for each, we could do this more efficiently,
	// but let's at least keep the timing log accurate.
	propagateCount := 0
	for _, check := range checks {
		if check.State == domain.CheckStateFinal {
			if err := s.propagateResolution(ctx, uow, workPlanID, domain.NodeTypeCheck, check.ID); err != nil {
				return err
			}
			propagateCount++
		}
	}
	for _, stage := range stages {
		if stage.State == domain.StageStateFinal {
			if err := s.propagateResolution(ctx, uow, workPlanID, domain.NodeTypeStage, stage.ID); err != nil {
				return err
			}
			propagateCount++
		}
	}

	// Advance node states using the finalized nodes map to avoid N+1 queries
	if err := s.advanceNodeStatesOptimized(ctx, uow, workPlanID, checks, stages, finalizedNodes); err != nil {
		return err
	}

	duration := time.Since(start)
	if duration > 100*time.Millisecond {
		log.Printf("[WP:%s] resolveDependencies took %v (checks=%d, stages=%d, propagated=%d)", 
			workPlanID, duration, len(checks), len(stages), propagateCount)
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

// advanceNodeStatesOptimized is an optimized version of advanceNodeStates that takes already-fetched data.
func (s *OrchestratorService) advanceNodeStatesOptimized(ctx context.Context, uow storage.UnitOfWork, workPlanID string, checks []*domain.Check, stages []*domain.Stage, finalizedNodes map[string]bool) error {
	start := time.Now()
	
	// Get ALL dependencies for this work plan at once to avoid N+1 queries
	// We'll need a new storage method for this, or just use GetBySource in a smart way.
	// For now, let's at least optimize the logic with the finalizedNodes map we already have.
	
	advancedChecks := 0
	for _, check := range checks {
		if check.State != domain.CheckStatePlanned {
			continue
		}

		if check.Dependencies == nil || len(check.Dependencies.Dependencies) == 0 {
			check.State = domain.CheckStateWaiting
			check.UpdatedAt = time.Now().UTC()
			if err := uow.Checks().Update(ctx, check); err != nil {
				return err
			}
			advancedChecks++
			continue
		}

		// Use the finalizedNodes map to check dependencies
		resolved := make(map[string]bool)
		for _, dep := range check.Dependencies.Dependencies {
			key := string(dep.TargetType) + ":" + dep.TargetID
			if finalizedNodes[key] {
				resolved[key] = true
			} else {
				resolved[key] = false
			}
		}

		if check.Dependencies.IsSatisfied(resolved) {
			check.State = domain.CheckStateWaiting
			check.UpdatedAt = time.Now().UTC()
			if err := uow.Checks().Update(ctx, check); err != nil {
				return err
			}
			advancedChecks++
		}
	}

	advancedStages := 0
	for _, stage := range stages {
		if stage.State != domain.StageStatePlanned {
			continue
		}

		if stage.Dependencies == nil || len(stage.Dependencies.Dependencies) == 0 {
			if err := s.advanceStageToAttempting(ctx, uow, stage); err != nil {
				return err
			}
			advancedStages++
			continue
		}

		// Use the finalizedNodes map to check dependencies
		resolved := make(map[string]bool)
		for _, dep := range stage.Dependencies.Dependencies {
			key := string(dep.TargetType) + ":" + dep.TargetID
			if finalizedNodes[key] {
				resolved[key] = true
			} else {
				resolved[key] = false
			}
		}

		if stage.Dependencies.IsSatisfied(resolved) {
			if err := s.advanceStageToAttempting(ctx, uow, stage); err != nil {
				return err
			}
			advancedStages++
		}
	}
	
	duration := time.Since(start)
	if duration > 50*time.Millisecond || advancedChecks > 0 || advancedStages > 0 {
		log.Printf("[WP:%s] advanceNodeStates took %v (checks_adv=%d, stages_adv=%d)", 
			workPlanID, duration, advancedChecks, advancedStages)
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
