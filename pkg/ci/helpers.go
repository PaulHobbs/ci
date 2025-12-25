// Package ci provides a simplified developer API for TurboCI-Lite workflows.
//
// This package offers four abstraction levels:
//   - Level 1 (helpers.go): Thin syntax helpers - Ptr, DependsOn, Check, Stage
//   - Level 2 (builders.go): Fluent builders - CheckBuilder, StageBuilder
//   - Level 3 (workflow.go): Async futures - WorkflowContext, CheckHandle, StageHandle
//   - Level 4 (dag.go): Declarative DAG DSL with hybrid dynamic support
//
// All levels are composable - you can mix and match, escaping to lower levels when needed.
package ci

import "github.com/example/turboci-lite/internal/domain"

// Ptr returns a pointer to the value. Generic helper to avoid inline address-of operators.
func Ptr[T any](v T) *T {
	return &v
}

// NodeRef identifies a node for dependency references.
type NodeRef struct {
	Type domain.NodeType
	ID   string
}

// Check returns a NodeRef for a check.
func Check(id string) NodeRef {
	if id == "" {
		panic("ci: Check() called with empty id")
	}
	return NodeRef{Type: domain.NodeTypeCheck, ID: id}
}

// Stage returns a NodeRef for a stage.
func Stage(id string) NodeRef {
	if id == "" {
		panic("ci: Stage() called with empty id")
	}
	return NodeRef{Type: domain.NodeTypeStage, ID: id}
}

// DependsOn creates an AND dependency group from node references.
// All dependencies must be satisfied for the dependent node to proceed.
func DependsOn(refs ...NodeRef) *domain.DependencyGroup {
	if len(refs) == 0 {
		return nil
	}
	deps := make([]domain.DependencyRef, len(refs))
	for i, ref := range refs {
		deps[i] = domain.DependencyRef{TargetType: ref.Type, TargetID: ref.ID}
	}
	return &domain.DependencyGroup{Predicate: domain.PredicateAND, Dependencies: deps}
}

// DependsOnAny creates an OR dependency group from node references.
// At least one dependency must be satisfied for the dependent node to proceed.
func DependsOnAny(refs ...NodeRef) *domain.DependencyGroup {
	if len(refs) == 0 {
		return nil
	}
	deps := make([]domain.DependencyRef, len(refs))
	for i, ref := range refs {
		deps[i] = domain.DependencyRef{TargetType: ref.Type, TargetID: ref.ID}
	}
	return &domain.DependencyGroup{Predicate: domain.PredicateOR, Dependencies: deps}
}

// DependsOnChecks creates an AND dependency group from check IDs.
// Convenience wrapper for the common case of depending on checks.
func DependsOnChecks(checkIDs ...string) *domain.DependencyGroup {
	refs := make([]NodeRef, len(checkIDs))
	for i, id := range checkIDs {
		refs[i] = Check(id)
	}
	return DependsOn(refs...)
}

// DependsOnStages creates an AND dependency group from stage IDs.
// Convenience wrapper for the common case of depending on stages.
func DependsOnStages(stageIDs ...string) *domain.DependencyGroup {
	refs := make([]NodeRef, len(stageIDs))
	for i, id := range stageIDs {
		refs[i] = Stage(id)
	}
	return DependsOn(refs...)
}

// Assigns creates an assignment for a check with FINAL goal state.
// Use AssignsWithGoal for custom goal states.
func Assigns(checkID string) domain.Assignment {
	if checkID == "" {
		panic("ci: Assigns() called with empty checkID")
	}
	return domain.Assignment{
		TargetCheckID: checkID,
		GoalState:     domain.CheckStateFinal,
	}
}

// AssignsWithGoal creates an assignment for a check with a custom goal state.
func AssignsWithGoal(checkID string, goal domain.CheckState) domain.Assignment {
	if checkID == "" {
		panic("ci: AssignsWithGoal() called with empty checkID")
	}
	return domain.Assignment{
		TargetCheckID: checkID,
		GoalState:     goal,
	}
}

// AssignmentList creates a list of assignments from check IDs (all with FINAL goal).
func AssignmentList(checkIDs ...string) []domain.Assignment {
	assignments := make([]domain.Assignment, len(checkIDs))
	for i, id := range checkIDs {
		assignments[i] = Assigns(id)
	}
	return assignments
}
