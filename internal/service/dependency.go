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
// Uses incremental resolution: only processes newly-finalized nodes and their dependents.
func (s *OrchestratorService) resolveDependenciesCached(ctx context.Context, uow storage.UnitOfWork, workPlanID string, cache *dependencyCache) error {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.DependencyResolutionTime().Observe(time.Since(start))
		}
	}()

	// Fast path: if nothing was finalized, we only need to check if any newly-created
	// nodes (with no dependencies) can be advanced.
	if !cache.hasNewlyFinalized() {
		// Only process newly written nodes that might need state advancement
		return s.advanceNewlyWrittenNodes(ctx, uow, workPlanID, cache)
	}

	// Propagate resolution only for newly-finalized nodes
	if err := s.propagateResolutionIncremental(ctx, uow, workPlanID, cache); err != nil {
		return err
	}

	// Advance only nodes that depend on the newly-finalized nodes
	if err := s.advanceAffectedNodes(ctx, uow, workPlanID, cache); err != nil {
		return err
	}

	duration := time.Since(start)
	if duration > 100*time.Millisecond {
		log.Printf("[WP:%s] resolveDependenciesCached took %v (newly_finalized_checks=%d, newly_finalized_stages=%d)",
			workPlanID, duration, len(cache.newlyFinalizedChecks), len(cache.newlyFinalizedStages))
	}

	return nil
}

// advanceNewlyWrittenNodes advances newly-written nodes that are ready to advance.
// This is the fast path when nothing was finalized in this batch.
func (s *OrchestratorService) advanceNewlyWrittenNodes(ctx context.Context, uow storage.UnitOfWork, workPlanID string, cache *dependencyCache) error {
	// Check newly-written checks
	for checkID := range cache.writtenChecks {
		priorState := cache.priorCheckStates[checkID]
		// Only process nodes that were newly created (priorState == Planned means it was just created)
		if priorState != domain.CheckStatePlanned {
			continue
		}

		check, err := uow.Checks().Get(ctx, workPlanID, checkID)
		if err != nil {
			return err
		}

		if check.State != domain.CheckStatePlanned {
			continue
		}

		// Advance if no dependencies
		if check.Dependencies == nil || len(check.Dependencies.Dependencies) == 0 {
			check.State = domain.CheckStateWaiting
			check.UpdatedAt = time.Now().UTC()
			if err := uow.Checks().Update(ctx, check); err != nil {
				return err
			}
			continue
		}

		// Check if all dependencies are already satisfied
		resolved, err := s.resolveDependencyStatus(ctx, uow, workPlanID, check.Dependencies.Dependencies, cache)
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

	// Check newly-written stages
	for stageID := range cache.writtenStages {
		priorState := cache.priorStageStates[stageID]
		if priorState != domain.StageStatePlanned {
			continue
		}

		stage, err := uow.Stages().Get(ctx, workPlanID, stageID)
		if err != nil {
			return err
		}

		if stage.State != domain.StageStatePlanned {
			continue
		}

		// Advance if no dependencies
		hasDeps := stage.Dependencies != nil && len(stage.Dependencies.Dependencies) > 0
		if !hasDeps {
			if err := s.advanceStageToAttempting(ctx, uow, stage); err != nil {
				return err
			}
			continue
		}

		// Check if all dependencies are already satisfied
		resolved, err := s.resolveDependencyStatus(ctx, uow, workPlanID, stage.Dependencies.Dependencies, cache)
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

// propagateResolutionIncremental marks dependencies resolved only for newly-finalized nodes.
func (s *OrchestratorService) propagateResolutionIncremental(ctx context.Context, uow storage.UnitOfWork, workPlanID string, cache *dependencyCache) error {
	// Build list of newly-finalized targets only
	targets := make([]storage.TargetNode, 0, len(cache.newlyFinalizedChecks)+len(cache.newlyFinalizedStages))

	for _, checkID := range cache.newlyFinalizedChecks {
		targets = append(targets, storage.TargetNode{
			Type: domain.NodeTypeCheck,
			ID:   checkID,
		})
	}
	for _, stageID := range cache.newlyFinalizedStages {
		targets = append(targets, storage.TargetNode{
			Type: domain.NodeTypeStage,
			ID:   stageID,
		})
	}

	if len(targets) == 0 {
		return nil
	}

	// Fetch dependencies pointing to newly-finalized nodes
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

// advanceAffectedNodes advances only nodes that depend on newly-finalized nodes.
func (s *OrchestratorService) advanceAffectedNodes(ctx context.Context, uow storage.UnitOfWork, workPlanID string, cache *dependencyCache) error {
	// Build list of newly-finalized targets
	targets := make([]storage.TargetNode, 0, len(cache.newlyFinalizedChecks)+len(cache.newlyFinalizedStages))

	for _, checkID := range cache.newlyFinalizedChecks {
		targets = append(targets, storage.TargetNode{
			Type: domain.NodeTypeCheck,
			ID:   checkID,
		})
	}
	for _, stageID := range cache.newlyFinalizedStages {
		targets = append(targets, storage.TargetNode{
			Type: domain.NodeTypeStage,
			ID:   stageID,
		})
	}

	if len(targets) == 0 {
		return nil
	}

	// Get all dependencies pointing to newly-finalized targets to find affected sources
	depsByTarget, err := uow.Dependencies().GetByTargets(ctx, workPlanID, targets)
	if err != nil {
		return err
	}

	// Collect unique source nodes that might need advancement
	affectedChecks := make(map[string]bool)
	affectedStages := make(map[string]bool)

	for _, deps := range depsByTarget {
		for _, dep := range deps {
			if dep.SourceType == domain.NodeTypeCheck {
				affectedChecks[dep.SourceID] = true
			} else if dep.SourceType == domain.NodeTypeStage {
				affectedStages[dep.SourceID] = true
			}
		}
	}

	// Also add any newly-written nodes that might need advancement
	for checkID := range cache.writtenChecks {
		if cache.priorCheckStates[checkID] == domain.CheckStatePlanned {
			affectedChecks[checkID] = true
		}
	}
	for stageID := range cache.writtenStages {
		if cache.priorStageStates[stageID] == domain.StageStatePlanned {
			affectedStages[stageID] = true
		}
	}

	// Process affected checks
	for checkID := range affectedChecks {
		check, err := uow.Checks().Get(ctx, workPlanID, checkID)
		if err == domain.ErrNotFound {
			continue // Node may have been deleted
		}
		if err != nil {
			return err
		}

		if check.State != domain.CheckStatePlanned {
			continue
		}

		if check.Dependencies == nil || len(check.Dependencies.Dependencies) == 0 {
			check.State = domain.CheckStateWaiting
			check.UpdatedAt = time.Now().UTC()
			if err := uow.Checks().Update(ctx, check); err != nil {
				return err
			}
			continue
		}

		// Check if all dependencies are satisfied
		// First check local cache, then query DB for unknown targets
		resolved, err := s.resolveDependencyStatus(ctx, uow, workPlanID, check.Dependencies.Dependencies, cache)
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

	// Process affected stages
	for stageID := range affectedStages {
		stage, err := uow.Stages().Get(ctx, workPlanID, stageID)
		if err == domain.ErrNotFound {
			continue
		}
		if err != nil {
			return err
		}

		if stage.State != domain.StageStatePlanned {
			continue
		}

		if stage.Dependencies == nil || len(stage.Dependencies.Dependencies) == 0 {
			if err := s.advanceStageToAttempting(ctx, uow, stage); err != nil {
				return err
			}
			continue
		}

		// Check if all dependencies are satisfied
		resolved, err := s.resolveDependencyStatus(ctx, uow, workPlanID, stage.Dependencies.Dependencies, cache)
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

// resolveDependencyStatus resolves the finalized status of dependencies.
// Uses the cache for newly-finalized nodes, and queries the database for others.
func (s *OrchestratorService) resolveDependencyStatus(ctx context.Context, uow storage.UnitOfWork, workPlanID string, deps []domain.DependencyRef, cache *dependencyCache) (map[string]bool, error) {
	resolved := make(map[string]bool)

	for _, dep := range deps {
		key := string(dep.TargetType) + ":" + dep.TargetID

		// First check if it's in our cache of newly-finalized nodes
		if cache.finalizedNodes[key] {
			resolved[key] = true
			continue
		}

		// Otherwise, query the database to check if it's finalized
		if dep.TargetType == domain.NodeTypeCheck {
			check, err := uow.Checks().Get(ctx, workPlanID, dep.TargetID)
			if err == domain.ErrNotFound {
				resolved[key] = false
				continue
			}
			if err != nil {
				return nil, err
			}
			resolved[key] = check.State == domain.CheckStateFinal
			// Cache it for future lookups in this batch
			if check.State == domain.CheckStateFinal {
				cache.finalizedNodes[key] = true
			}
		} else if dep.TargetType == domain.NodeTypeStage {
			stage, err := uow.Stages().Get(ctx, workPlanID, dep.TargetID)
			if err == domain.ErrNotFound {
				resolved[key] = false
				continue
			}
			if err != nil {
				return nil, err
			}
			resolved[key] = stage.State == domain.StageStateFinal
			// Cache it for future lookups in this batch
			if stage.State == domain.StageStateFinal {
				cache.finalizedNodes[key] = true
			}
		}
	}

	return resolved, nil
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
