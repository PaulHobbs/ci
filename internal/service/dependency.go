package service

import (
	"context"
	"log"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/storage"
)

// resolveDependencies resolves dependencies and advances node states.
// For backward compatibility, this creates a new cache on each call.
func (s *OrchestratorService) resolveDependencies(ctx context.Context, uow storage.UnitOfWork, workPlanID string) error {
	cache := newDependencyCache()
	return s.resolveDependenciesCached(ctx, uow, workPlanID, cache)
}

// resolveDependenciesCached resolves dependencies using a cache to avoid redundant queries.
func (s *OrchestratorService) resolveDependenciesCached(ctx context.Context, uow storage.UnitOfWork, workPlanID string, cache *dependencyCache) error {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.DependencyResolutionTime().Observe(time.Since(start))
		}
	}()

	// Load nodes into cache if first call
	if !cache.loaded {
		checks, err := uow.Checks().List(ctx, workPlanID, storage.ListOptions{})
		if err != nil {
			return err
		}
		cache.checks = checks

		stages, err := uow.Stages().List(ctx, workPlanID, storage.ListOptions{})
		if err != nil {
			return err
		}
		cache.stages = stages

		// Build finalized nodes map
		for _, check := range checks {
			if check.State == domain.CheckStateFinal {
				cache.finalizedNodes[string(domain.NodeTypeCheck)+":"+check.ID] = true
			}
		}
		for _, stage := range stages {
			if stage.State == domain.StageStateFinal {
				cache.finalizedNodes[string(domain.NodeTypeStage)+":"+stage.ID] = true
			}
		}

		cache.loaded = true

		// Track node counts only on first load
		if s.metrics != nil {
			s.metrics.NodesEvaluated().WithLabels("check").Add(int64(len(checks)))
			s.metrics.NodesEvaluated().WithLabels("stage").Add(int64(len(stages)))
		}
	}

	// Use batch propagation for all finalized nodes
	if err := s.propagateResolutionBatch(ctx, uow, workPlanID, cache); err != nil {
		return err
	}

	// Advance node states using the cached data and finalized nodes map
	if err := s.advanceNodeStatesOptimized(ctx, uow, workPlanID, cache.checks, cache.stages, cache.finalizedNodes); err != nil {
		return err
	}

	duration := time.Since(start)
	if duration > 100*time.Millisecond {
		log.Printf("[WP:%s] resolveDependenciesCached took %v (checks=%d, stages=%d, cached=%v)",
			workPlanID, duration, len(cache.checks), len(cache.stages), cache.loaded)
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

// propagateResolutionBatch processes all finalized nodes in one batch.
func (s *OrchestratorService) propagateResolutionBatch(ctx context.Context, uow storage.UnitOfWork, workPlanID string, cache *dependencyCache) error {
	// Build list of finalized targets
	targets := make([]storage.TargetNode, 0)

	for _, check := range cache.checks {
		if check.State == domain.CheckStateFinal {
			targets = append(targets, storage.TargetNode{
				Type: domain.NodeTypeCheck,
				ID:   check.ID,
			})
		}
	}
	for _, stage := range cache.stages {
		if stage.State == domain.StageStateFinal {
			targets = append(targets, storage.TargetNode{
				Type: domain.NodeTypeStage,
				ID:   stage.ID,
			})
		}
	}

	if len(targets) == 0 {
		return nil
	}

	// Fetch all dependencies in one query
	depsByTarget, err := uow.Dependencies().GetByTargets(ctx, workPlanID, targets)
	if err != nil {
		return err
	}

	// Collect updates
	updates := make([]storage.DependencyUpdate, 0)
	for _, deps := range depsByTarget {
		for _, dep := range deps {
			if !dep.Resolved {
				updates = append(updates, storage.DependencyUpdate{
					ID:        dep.ID,
					Satisfied: true, // Finalized = satisfied
				})
			}
		}
	}

	// Apply all updates in batch
	return uow.Dependencies().MarkResolvedBatch(ctx, updates)
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

		// Notify dispatcher that new work is available (event-driven dispatch)
		if s.dispatcher != nil {
			s.dispatcher.NotifyWorkAvailable()
		}
	}

	return nil
}
