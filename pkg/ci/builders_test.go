package ci

import (
	"testing"

	"github.com/example/turboci-lite/internal/domain"
)

func TestNewCheckBuilder(t *testing.T) {
	cw := NewCheck("my-check").Build()
	if cw.ID != "my-check" {
		t.Errorf("NewCheck().ID = %s, want my-check", cw.ID)
	}
}

func TestNewCheckPanicsOnEmptyID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewCheck() did not panic on empty id")
		}
	}()
	NewCheck("")
}

func TestCheckBuilderKind(t *testing.T) {
	cw := NewCheck("c").Kind("build").Build()
	if cw.Kind != "build" {
		t.Errorf("Kind() = %s, want build", cw.Kind)
	}
}

func TestCheckBuilderState(t *testing.T) {
	cw := NewCheck("c").Planning().Build()
	if cw.State == nil || *cw.State != domain.CheckStatePlanning {
		t.Errorf("Planning() = %v, want PLANNING", cw.State)
	}

	cw = NewCheck("c").Planned().Build()
	if cw.State == nil || *cw.State != domain.CheckStatePlanned {
		t.Errorf("Planned() = %v, want PLANNED", cw.State)
	}

	cw = NewCheck("c").Waiting().Build()
	if cw.State == nil || *cw.State != domain.CheckStateWaiting {
		t.Errorf("Waiting() = %v, want WAITING", cw.State)
	}

	cw = NewCheck("c").Final().Build()
	if cw.State == nil || *cw.State != domain.CheckStateFinal {
		t.Errorf("Final() = %v, want FINAL", cw.State)
	}
}

func TestCheckBuilderOptions(t *testing.T) {
	opts := map[string]any{"key": "value", "count": 42}
	cw := NewCheck("c").Options(opts).Build()
	if cw.Options["key"] != "value" {
		t.Errorf("Options() key = %v, want value", cw.Options["key"])
	}
	if cw.Options["count"] != 42 {
		t.Errorf("Options() count = %v, want 42", cw.Options["count"])
	}
}

func TestCheckBuilderOption(t *testing.T) {
	cw := NewCheck("c").Option("a", 1).Option("b", 2).Build()
	if cw.Options["a"] != 1 || cw.Options["b"] != 2 {
		t.Errorf("Option() = %v, want {a:1, b:2}", cw.Options)
	}
}

func TestCheckBuilderDependsOn(t *testing.T) {
	cw := NewCheck("c").DependsOn("c1", "c2").Build()
	if cw.Dependencies == nil {
		t.Fatal("DependsOn() returned nil Dependencies")
	}
	if cw.Dependencies.Predicate != domain.PredicateAND {
		t.Errorf("DependsOn().Predicate = %v, want AND", cw.Dependencies.Predicate)
	}
	if len(cw.Dependencies.Dependencies) != 2 {
		t.Errorf("len(DependsOn().Dependencies) = %d, want 2", len(cw.Dependencies.Dependencies))
	}
}

func TestCheckBuilderDependsOnAny(t *testing.T) {
	cw := NewCheck("c").DependsOnAny("c1", "c2").Build()
	if cw.Dependencies == nil {
		t.Fatal("DependsOnAny() returned nil Dependencies")
	}
	if cw.Dependencies.Predicate != domain.PredicateOR {
		t.Errorf("DependsOnAny().Predicate = %v, want OR", cw.Dependencies.Predicate)
	}
}

func TestCheckBuilderDependsOnPanicsOnEmptyID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("DependsOn() did not panic on empty checkID")
		}
	}()
	NewCheck("c").DependsOn("valid", "")
}

func TestCheckBuilderChaining(t *testing.T) {
	cw := NewCheck("my-check").
		Kind("build").
		Planned().
		Option("target", "//...").
		Option("platform", "linux").
		DependsOn("prereq").
		Build()

	if cw.ID != "my-check" {
		t.Errorf("ID = %s, want my-check", cw.ID)
	}
	if cw.Kind != "build" {
		t.Errorf("Kind = %s, want build", cw.Kind)
	}
	if cw.State == nil || *cw.State != domain.CheckStatePlanned {
		t.Errorf("State = %v, want PLANNED", cw.State)
	}
	if cw.Options["target"] != "//..." {
		t.Errorf("Options[target] = %v, want //...", cw.Options["target"])
	}
	if cw.Dependencies == nil || len(cw.Dependencies.Dependencies) != 1 {
		t.Errorf("Dependencies = %v, want 1 dep", cw.Dependencies)
	}
}

func TestNewStageBuilder(t *testing.T) {
	sw := NewStage("my-stage").Build()
	if sw.ID != "my-stage" {
		t.Errorf("NewStage().ID = %s, want my-stage", sw.ID)
	}
}

func TestNewStagePanicsOnEmptyID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewStage() did not panic on empty id")
		}
	}()
	NewStage("")
}

func TestStageBuilderRunnerType(t *testing.T) {
	sw := NewStage("s").RunnerType("build_executor").Build()
	if sw.RunnerType != "build_executor" {
		t.Errorf("RunnerType() = %s, want build_executor", sw.RunnerType)
	}
}

func TestStageBuilderRunnerTypePanicsOnEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("RunnerType() did not panic on empty runnerType")
		}
	}()
	NewStage("s").RunnerType("")
}

func TestStageBuilderExecutionMode(t *testing.T) {
	sw := NewStage("s").Sync().Build()
	if sw.ExecutionMode == nil || *sw.ExecutionMode != domain.ExecutionModeSync {
		t.Errorf("Sync() = %v, want SYNC", sw.ExecutionMode)
	}

	sw = NewStage("s").Async().Build()
	if sw.ExecutionMode == nil || *sw.ExecutionMode != domain.ExecutionModeAsync {
		t.Errorf("Async() = %v, want ASYNC", sw.ExecutionMode)
	}
}

func TestStageBuilderState(t *testing.T) {
	sw := NewStage("s").Planned().Build()
	if sw.State == nil || *sw.State != domain.StageStatePlanned {
		t.Errorf("Planned() = %v, want PLANNED", sw.State)
	}

	sw = NewStage("s").Attempting().Build()
	if sw.State == nil || *sw.State != domain.StageStateAttempting {
		t.Errorf("Attempting() = %v, want ATTEMPTING", sw.State)
	}

	sw = NewStage("s").Final().Build()
	if sw.State == nil || *sw.State != domain.StageStateFinal {
		t.Errorf("Final() = %v, want FINAL", sw.State)
	}
}

func TestStageBuilderArgs(t *testing.T) {
	sw := NewStage("s").Arg("key", "value").Arg("count", 42).Build()
	if sw.Args["key"] != "value" {
		t.Errorf("Args[key] = %v, want value", sw.Args["key"])
	}
	if sw.Args["count"] != 42 {
		t.Errorf("Args[count] = %v, want 42", sw.Args["count"])
	}
}

func TestStageBuilderAssigns(t *testing.T) {
	sw := NewStage("s").Assigns("check1").Assigns("check2").Build()
	if len(sw.Assignments) != 2 {
		t.Errorf("len(Assignments) = %d, want 2", len(sw.Assignments))
	}
	if sw.Assignments[0].TargetCheckID != "check1" || sw.Assignments[0].GoalState != domain.CheckStateFinal {
		t.Errorf("Assignments[0] = %+v, want check1:FINAL", sw.Assignments[0])
	}
}

func TestStageBuilderAssignsPanicsOnEmptyID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Assigns() did not panic on empty checkID")
		}
	}()
	NewStage("s").Assigns("")
}

func TestStageBuilderAssignsAll(t *testing.T) {
	sw := NewStage("s").AssignsAll("c1", "c2", "c3").Build()
	if len(sw.Assignments) != 3 {
		t.Errorf("len(Assignments) = %d, want 3", len(sw.Assignments))
	}
}

func TestStageBuilderDependsOnStages(t *testing.T) {
	sw := NewStage("s").DependsOnStages("s1", "s2").Build()
	if sw.Dependencies == nil {
		t.Fatal("DependsOnStages() returned nil Dependencies")
	}
	if len(sw.Dependencies.Dependencies) != 2 {
		t.Errorf("len(Dependencies) = %d, want 2", len(sw.Dependencies.Dependencies))
	}
	for _, dep := range sw.Dependencies.Dependencies {
		if dep.TargetType != domain.NodeTypeStage {
			t.Errorf("DependsOnStages() has non-stage type: %v", dep.TargetType)
		}
	}
}

func TestStageBuilderDependsOnChecks(t *testing.T) {
	sw := NewStage("s").DependsOnChecks("c1", "c2").Build()
	if sw.Dependencies == nil {
		t.Fatal("DependsOnChecks() returned nil Dependencies")
	}
	for _, dep := range sw.Dependencies.Dependencies {
		if dep.TargetType != domain.NodeTypeCheck {
			t.Errorf("DependsOnChecks() has non-check type: %v", dep.TargetType)
		}
	}
}

func TestStageBuilderDependsOnMixed(t *testing.T) {
	sw := NewStage("s").DependsOnMixed(Check("c1"), Stage("s1"), Check("c2")).Build()
	if sw.Dependencies == nil {
		t.Fatal("DependsOnMixed() returned nil Dependencies")
	}
	if len(sw.Dependencies.Dependencies) != 3 {
		t.Errorf("len(Dependencies) = %d, want 3", len(sw.Dependencies.Dependencies))
	}
	if sw.Dependencies.Dependencies[0].TargetType != domain.NodeTypeCheck {
		t.Error("deps[0] should be check")
	}
	if sw.Dependencies.Dependencies[1].TargetType != domain.NodeTypeStage {
		t.Error("deps[1] should be stage")
	}
	if sw.Dependencies.Dependencies[2].TargetType != domain.NodeTypeCheck {
		t.Error("deps[2] should be check")
	}
}

func TestStageBuilderAttemptHelpers(t *testing.T) {
	sw := NewStage("s").ClaimAttempt("process-123").Build()
	if sw.CurrentAttempt == nil || sw.CurrentAttempt.ProcessUID != "process-123" {
		t.Errorf("ClaimAttempt() = %+v, want process-123", sw.CurrentAttempt)
	}

	sw = NewStage("s").CompleteAttempt().Build()
	if sw.CurrentAttempt == nil || *sw.CurrentAttempt.State != domain.AttemptStateComplete {
		t.Errorf("CompleteAttempt() = %+v, want COMPLETE", sw.CurrentAttempt)
	}

	sw = NewStage("s").FailAttempt("oops").Build()
	if sw.CurrentAttempt == nil || *sw.CurrentAttempt.State != domain.AttemptStateIncomplete {
		t.Errorf("FailAttempt() state = %+v, want INCOMPLETE", sw.CurrentAttempt)
	}
	if sw.CurrentAttempt.Failure == nil || sw.CurrentAttempt.Failure.Message != "oops" {
		t.Errorf("FailAttempt() failure = %+v, want 'oops'", sw.CurrentAttempt.Failure)
	}
}

func TestStageBuilderChaining(t *testing.T) {
	sw := NewStage("exec:build").
		RunnerType("build_executor").
		Sync().
		Planned().
		Arg("check_id", "build:main").
		Arg("change_id", "123").
		Assigns("build:main").
		DependsOnStages("materializer").
		Build()

	if sw.ID != "exec:build" {
		t.Errorf("ID = %s, want exec:build", sw.ID)
	}
	if sw.RunnerType != "build_executor" {
		t.Errorf("RunnerType = %s, want build_executor", sw.RunnerType)
	}
	if sw.ExecutionMode == nil || *sw.ExecutionMode != domain.ExecutionModeSync {
		t.Errorf("ExecutionMode = %v, want SYNC", sw.ExecutionMode)
	}
	if sw.State == nil || *sw.State != domain.StageStatePlanned {
		t.Errorf("State = %v, want PLANNED", sw.State)
	}
	if sw.Args["check_id"] != "build:main" {
		t.Errorf("Args[check_id] = %v, want build:main", sw.Args["check_id"])
	}
	if len(sw.Assignments) != 1 || sw.Assignments[0].TargetCheckID != "build:main" {
		t.Errorf("Assignments = %v, want [build:main]", sw.Assignments)
	}
	if sw.Dependencies == nil || len(sw.Dependencies.Dependencies) != 1 {
		t.Errorf("Dependencies = %v, want 1 dep", sw.Dependencies)
	}
}

func TestAttemptBuilder(t *testing.T) {
	aw := NewAttempt().
		Running().
		ProcessUID("proc-123").
		Detail("step", "compiling").
		Progress("50% complete", nil).
		Build()

	if aw.State == nil || *aw.State != domain.AttemptStateRunning {
		t.Errorf("State = %v, want RUNNING", aw.State)
	}
	if aw.ProcessUID != "proc-123" {
		t.Errorf("ProcessUID = %s, want proc-123", aw.ProcessUID)
	}
	if aw.Details["step"] != "compiling" {
		t.Errorf("Details[step] = %v, want compiling", aw.Details["step"])
	}
	if aw.Progress == nil || aw.Progress.Message != "50% complete" {
		t.Errorf("Progress = %+v, want 50%% complete", aw.Progress)
	}
}

func TestAttemptBuilderFailure(t *testing.T) {
	aw := NewAttempt().Incomplete().Failure("build error").Build()
	if aw.State == nil || *aw.State != domain.AttemptStateIncomplete {
		t.Errorf("State = %v, want INCOMPLETE", aw.State)
	}
	if aw.Failure == nil || aw.Failure.Message != "build error" {
		t.Errorf("Failure = %+v, want 'build error'", aw.Failure)
	}
}
