package ci

import (
	"testing"

	"github.com/example/turboci-lite/internal/domain"
)

func TestPtr(t *testing.T) {
	intVal := 42
	ptr := Ptr(intVal)
	if *ptr != 42 {
		t.Errorf("Ptr() = %d, want 42", *ptr)
	}

	strVal := "hello"
	strPtr := Ptr(strVal)
	if *strPtr != "hello" {
		t.Errorf("Ptr() = %s, want hello", *strPtr)
	}

	statePtr := Ptr(domain.CheckStatePlanning)
	if *statePtr != domain.CheckStatePlanning {
		t.Errorf("Ptr() = %v, want PLANNING", *statePtr)
	}
}

func TestCheck(t *testing.T) {
	ref := Check("my-check")
	if ref.Type != domain.NodeTypeCheck {
		t.Errorf("Check().Type = %v, want check", ref.Type)
	}
	if ref.ID != "my-check" {
		t.Errorf("Check().ID = %s, want my-check", ref.ID)
	}
}

func TestCheckPanicsOnEmptyID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Check() did not panic on empty id")
		}
	}()
	Check("")
}

func TestStage(t *testing.T) {
	ref := Stage("my-stage")
	if ref.Type != domain.NodeTypeStage {
		t.Errorf("Stage().Type = %v, want stage", ref.Type)
	}
	if ref.ID != "my-stage" {
		t.Errorf("Stage().ID = %s, want my-stage", ref.ID)
	}
}

func TestStagePanicsOnEmptyID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Stage() did not panic on empty id")
		}
	}()
	Stage("")
}

func TestDependsOn(t *testing.T) {
	deps := DependsOn(Check("c1"), Check("c2"), Stage("s1"))

	if deps == nil {
		t.Fatal("DependsOn() returned nil")
	}
	if deps.Predicate != domain.PredicateAND {
		t.Errorf("DependsOn().Predicate = %v, want AND", deps.Predicate)
	}
	if len(deps.Dependencies) != 3 {
		t.Errorf("len(DependsOn().Dependencies) = %d, want 3", len(deps.Dependencies))
	}

	// Verify order and types
	if deps.Dependencies[0].TargetType != domain.NodeTypeCheck || deps.Dependencies[0].TargetID != "c1" {
		t.Errorf("deps[0] = %+v, want check:c1", deps.Dependencies[0])
	}
	if deps.Dependencies[1].TargetType != domain.NodeTypeCheck || deps.Dependencies[1].TargetID != "c2" {
		t.Errorf("deps[1] = %+v, want check:c2", deps.Dependencies[1])
	}
	if deps.Dependencies[2].TargetType != domain.NodeTypeStage || deps.Dependencies[2].TargetID != "s1" {
		t.Errorf("deps[2] = %+v, want stage:s1", deps.Dependencies[2])
	}
}

func TestDependsOnEmpty(t *testing.T) {
	deps := DependsOn()
	if deps != nil {
		t.Errorf("DependsOn() = %+v, want nil", deps)
	}
}

func TestDependsOnAny(t *testing.T) {
	deps := DependsOnAny(Check("c1"), Check("c2"))

	if deps == nil {
		t.Fatal("DependsOnAny() returned nil")
	}
	if deps.Predicate != domain.PredicateOR {
		t.Errorf("DependsOnAny().Predicate = %v, want OR", deps.Predicate)
	}
	if len(deps.Dependencies) != 2 {
		t.Errorf("len(DependsOnAny().Dependencies) = %d, want 2", len(deps.Dependencies))
	}
}

func TestDependsOnChecks(t *testing.T) {
	deps := DependsOnChecks("c1", "c2")

	if deps == nil {
		t.Fatal("DependsOnChecks() returned nil")
	}
	if deps.Predicate != domain.PredicateAND {
		t.Errorf("DependsOnChecks().Predicate = %v, want AND", deps.Predicate)
	}
	if len(deps.Dependencies) != 2 {
		t.Errorf("len(DependsOnChecks().Dependencies) = %d, want 2", len(deps.Dependencies))
	}
	for _, dep := range deps.Dependencies {
		if dep.TargetType != domain.NodeTypeCheck {
			t.Errorf("DependsOnChecks() has non-check type: %v", dep.TargetType)
		}
	}
}

func TestDependsOnStages(t *testing.T) {
	deps := DependsOnStages("s1", "s2")

	if deps == nil {
		t.Fatal("DependsOnStages() returned nil")
	}
	if deps.Predicate != domain.PredicateAND {
		t.Errorf("DependsOnStages().Predicate = %v, want AND", deps.Predicate)
	}
	if len(deps.Dependencies) != 2 {
		t.Errorf("len(DependsOnStages().Dependencies) = %d, want 2", len(deps.Dependencies))
	}
	for _, dep := range deps.Dependencies {
		if dep.TargetType != domain.NodeTypeStage {
			t.Errorf("DependsOnStages() has non-stage type: %v", dep.TargetType)
		}
	}
}

func TestAssigns(t *testing.T) {
	a := Assigns("my-check")
	if a.TargetCheckID != "my-check" {
		t.Errorf("Assigns().TargetCheckID = %s, want my-check", a.TargetCheckID)
	}
	if a.GoalState != domain.CheckStateFinal {
		t.Errorf("Assigns().GoalState = %v, want FINAL", a.GoalState)
	}
}

func TestAssignsPanicsOnEmptyID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Assigns() did not panic on empty checkID")
		}
	}()
	Assigns("")
}

func TestAssignsWithGoal(t *testing.T) {
	a := AssignsWithGoal("my-check", domain.CheckStateWaiting)
	if a.TargetCheckID != "my-check" {
		t.Errorf("AssignsWithGoal().TargetCheckID = %s, want my-check", a.TargetCheckID)
	}
	if a.GoalState != domain.CheckStateWaiting {
		t.Errorf("AssignsWithGoal().GoalState = %v, want WAITING", a.GoalState)
	}
}

func TestAssignmentList(t *testing.T) {
	list := AssignmentList("c1", "c2", "c3")
	if len(list) != 3 {
		t.Errorf("len(AssignmentList()) = %d, want 3", len(list))
	}
	for i, a := range list {
		if a.GoalState != domain.CheckStateFinal {
			t.Errorf("AssignmentList()[%d].GoalState = %v, want FINAL", i, a.GoalState)
		}
	}
	if list[0].TargetCheckID != "c1" || list[1].TargetCheckID != "c2" || list[2].TargetCheckID != "c3" {
		t.Error("AssignmentList() check IDs don't match")
	}
}
