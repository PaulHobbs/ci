package domain

import "time"

// NodeType identifies the type of a graph node.
type NodeType string

const (
	NodeTypeCheck NodeType = "check"
	NodeTypeStage NodeType = "stage"
)

func (n NodeType) String() string {
	return string(n)
}

// PredicateType for dependency logic.
type PredicateType string

const (
	PredicateAND PredicateType = "AND" // All dependencies must be satisfied
	PredicateOR  PredicateType = "OR"  // Any dependency can satisfy
)

// Dependency represents an edge from one node to another.
type Dependency struct {
	ID         int64
	WorkPlanID string
	SourceType NodeType
	SourceID   string
	TargetType NodeType
	TargetID   string
	Resolved   bool
	Satisfied  *bool
	ResolvedAt *time.Time
}

// NewDependency creates a new dependency.
func NewDependency(workPlanID string, sourceType NodeType, sourceID string, targetType NodeType, targetID string) *Dependency {
	return &Dependency{
		WorkPlanID: workPlanID,
		SourceType: sourceType,
		SourceID:   sourceID,
		TargetType: targetType,
		TargetID:   targetID,
		Resolved:   false,
	}
}

// Resolve marks the dependency as resolved.
func (d *Dependency) Resolve(satisfied bool) {
	d.Resolved = true
	d.Satisfied = &satisfied
	now := time.Now().UTC()
	d.ResolvedAt = &now
}

// DependencyGroup represents a group of dependencies with a predicate.
type DependencyGroup struct {
	Predicate    PredicateType
	Dependencies []DependencyRef
}

// DependencyRef is a reference to a target node.
type DependencyRef struct {
	TargetType NodeType
	TargetID   string
}

// NewDependencyGroup creates a new dependency group.
func NewDependencyGroup(predicate PredicateType, deps ...DependencyRef) *DependencyGroup {
	return &DependencyGroup{
		Predicate:    predicate,
		Dependencies: deps,
	}
}

// IsSatisfied checks if the dependency group is satisfied given resolved dependencies.
func (g *DependencyGroup) IsSatisfied(resolved map[string]bool) bool {
	if g == nil || len(g.Dependencies) == 0 {
		return true // No dependencies = satisfied
	}

	switch g.Predicate {
	case PredicateAND:
		for _, dep := range g.Dependencies {
			key := string(dep.TargetType) + ":" + dep.TargetID
			satisfied, ok := resolved[key]
			if !ok || !satisfied {
				return false
			}
		}
		return true

	case PredicateOR:
		for _, dep := range g.Dependencies {
			key := string(dep.TargetType) + ":" + dep.TargetID
			if satisfied, ok := resolved[key]; ok && satisfied {
				return true
			}
		}
		return false

	default:
		return false
	}
}

// AllResolved checks if all dependencies have been resolved (regardless of satisfaction).
func (g *DependencyGroup) AllResolved(resolved map[string]bool) bool {
	if g == nil || len(g.Dependencies) == 0 {
		return true
	}

	for _, dep := range g.Dependencies {
		key := string(dep.TargetType) + ":" + dep.TargetID
		if _, ok := resolved[key]; !ok {
			return false
		}
	}
	return true
}
