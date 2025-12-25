package ci

import (
	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
)

// CheckBuilder provides a fluent API for constructing CheckWrite objects.
type CheckBuilder struct {
	write *service.CheckWrite
}

// NewCheck creates a new CheckBuilder with the given ID.
// Panics if id is empty.
func NewCheck(id string) *CheckBuilder {
	if id == "" {
		panic("ci: NewCheck() called with empty id")
	}
	return &CheckBuilder{
		write: &service.CheckWrite{
			ID:      id,
			Options: make(map[string]any),
		},
	}
}

// Kind sets the check kind (e.g., "build", "test", "deploy").
func (b *CheckBuilder) Kind(kind string) *CheckBuilder {
	b.write.Kind = kind
	return b
}

// State sets the check state.
func (b *CheckBuilder) State(state domain.CheckState) *CheckBuilder {
	b.write.State = &state
	return b
}

// Planning sets the state to PLANNING.
func (b *CheckBuilder) Planning() *CheckBuilder {
	return b.State(domain.CheckStatePlanning)
}

// Planned sets the state to PLANNED.
func (b *CheckBuilder) Planned() *CheckBuilder {
	return b.State(domain.CheckStatePlanned)
}

// Waiting sets the state to WAITING.
func (b *CheckBuilder) Waiting() *CheckBuilder {
	return b.State(domain.CheckStateWaiting)
}

// Final sets the state to FINAL.
func (b *CheckBuilder) Final() *CheckBuilder {
	return b.State(domain.CheckStateFinal)
}

// Options sets all check options (replaces existing).
func (b *CheckBuilder) Options(opts map[string]any) *CheckBuilder {
	b.write.Options = opts
	return b
}

// Option sets a single option key-value pair.
func (b *CheckBuilder) Option(key string, value any) *CheckBuilder {
	if b.write.Options == nil {
		b.write.Options = make(map[string]any)
	}
	b.write.Options[key] = value
	return b
}

// DependsOn sets AND dependencies on the given checks.
func (b *CheckBuilder) DependsOn(checkIDs ...string) *CheckBuilder {
	if len(checkIDs) == 0 {
		return b
	}
	refs := make([]domain.DependencyRef, len(checkIDs))
	for i, id := range checkIDs {
		if id == "" {
			panic("ci: CheckBuilder.DependsOn() called with empty checkID")
		}
		refs[i] = domain.DependencyRef{TargetType: domain.NodeTypeCheck, TargetID: id}
	}
	b.write.Dependencies = &domain.DependencyGroup{
		Predicate:    domain.PredicateAND,
		Dependencies: refs,
	}
	return b
}

// DependsOnAny sets OR dependencies on the given checks.
func (b *CheckBuilder) DependsOnAny(checkIDs ...string) *CheckBuilder {
	if len(checkIDs) == 0 {
		return b
	}
	refs := make([]domain.DependencyRef, len(checkIDs))
	for i, id := range checkIDs {
		if id == "" {
			panic("ci: CheckBuilder.DependsOnAny() called with empty checkID")
		}
		refs[i] = domain.DependencyRef{TargetType: domain.NodeTypeCheck, TargetID: id}
	}
	b.write.Dependencies = &domain.DependencyGroup{
		Predicate:    domain.PredicateOR,
		Dependencies: refs,
	}
	return b
}

// Dependencies sets the full dependency group.
func (b *CheckBuilder) Dependencies(deps *domain.DependencyGroup) *CheckBuilder {
	b.write.Dependencies = deps
	return b
}

// Result adds a result to the check.
func (b *CheckBuilder) Result(ownerType, ownerID string, data map[string]any, finalize bool) *CheckBuilder {
	b.write.Result = &service.CheckResultWrite{
		OwnerType: ownerType,
		OwnerID:   ownerID,
		Data:      data,
		Finalize:  finalize,
	}
	return b
}

// ResultWithFailure adds a failed result to the check.
func (b *CheckBuilder) ResultWithFailure(ownerType, ownerID, failureMsg string, data map[string]any) *CheckBuilder {
	b.write.Result = &service.CheckResultWrite{
		OwnerType: ownerType,
		OwnerID:   ownerID,
		Data:      data,
		Finalize:  true,
		Failure:   &domain.Failure{Message: failureMsg},
	}
	return b
}

// Build returns the constructed CheckWrite.
func (b *CheckBuilder) Build() *service.CheckWrite {
	return b.write
}

// StageBuilder provides a fluent API for constructing StageWrite objects.
type StageBuilder struct {
	write *service.StageWrite
}

// NewStage creates a new StageBuilder with the given ID.
// Panics if id is empty.
func NewStage(id string) *StageBuilder {
	if id == "" {
		panic("ci: NewStage() called with empty id")
	}
	return &StageBuilder{
		write: &service.StageWrite{
			ID:   id,
			Args: make(map[string]any),
		},
	}
}

// RunnerType sets the runner type that handles this stage.
// Panics if rt is empty.
func (b *StageBuilder) RunnerType(rt string) *StageBuilder {
	if rt == "" {
		panic("ci: StageBuilder.RunnerType() called with empty runnerType")
	}
	b.write.RunnerType = rt
	return b
}

// Sync sets the execution mode to synchronous.
func (b *StageBuilder) Sync() *StageBuilder {
	mode := domain.ExecutionModeSync
	b.write.ExecutionMode = &mode
	return b
}

// Async sets the execution mode to asynchronous.
func (b *StageBuilder) Async() *StageBuilder {
	mode := domain.ExecutionModeAsync
	b.write.ExecutionMode = &mode
	return b
}

// ExecutionMode sets the execution mode explicitly.
func (b *StageBuilder) ExecutionMode(mode domain.ExecutionMode) *StageBuilder {
	b.write.ExecutionMode = &mode
	return b
}

// State sets the stage state.
func (b *StageBuilder) State(state domain.StageState) *StageBuilder {
	b.write.State = &state
	return b
}

// Planned sets the state to PLANNED.
func (b *StageBuilder) Planned() *StageBuilder {
	return b.State(domain.StageStatePlanned)
}

// Attempting sets the state to ATTEMPTING.
func (b *StageBuilder) Attempting() *StageBuilder {
	return b.State(domain.StageStateAttempting)
}

// AwaitingGroup sets the state to AWAITING_GROUP.
func (b *StageBuilder) AwaitingGroup() *StageBuilder {
	return b.State(domain.StageStateAwaitingGroup)
}

// Final sets the state to FINAL.
func (b *StageBuilder) Final() *StageBuilder {
	return b.State(domain.StageStateFinal)
}

// Args sets all stage arguments (replaces existing).
func (b *StageBuilder) Args(args map[string]any) *StageBuilder {
	b.write.Args = args
	return b
}

// Arg sets a single argument key-value pair.
func (b *StageBuilder) Arg(key string, value any) *StageBuilder {
	if b.write.Args == nil {
		b.write.Args = make(map[string]any)
	}
	b.write.Args[key] = value
	return b
}

// Assigns adds an assignment to a check with FINAL goal state.
func (b *StageBuilder) Assigns(checkID string) *StageBuilder {
	if checkID == "" {
		panic("ci: StageBuilder.Assigns() called with empty checkID")
	}
	b.write.Assignments = append(b.write.Assignments, domain.Assignment{
		TargetCheckID: checkID,
		GoalState:     domain.CheckStateFinal,
	})
	return b
}

// AssignsWithGoal adds an assignment with a custom goal state.
func (b *StageBuilder) AssignsWithGoal(checkID string, goal domain.CheckState) *StageBuilder {
	if checkID == "" {
		panic("ci: StageBuilder.AssignsWithGoal() called with empty checkID")
	}
	b.write.Assignments = append(b.write.Assignments, domain.Assignment{
		TargetCheckID: checkID,
		GoalState:     goal,
	})
	return b
}

// AssignsAll adds assignments for multiple checks with FINAL goal state.
func (b *StageBuilder) AssignsAll(checkIDs ...string) *StageBuilder {
	for _, checkID := range checkIDs {
		b.Assigns(checkID)
	}
	return b
}

// Assignments sets the full assignment list (replaces existing).
func (b *StageBuilder) Assignments(assignments []domain.Assignment) *StageBuilder {
	b.write.Assignments = assignments
	return b
}

// DependsOnStages sets AND dependencies on the given stages.
func (b *StageBuilder) DependsOnStages(stageIDs ...string) *StageBuilder {
	if len(stageIDs) == 0 {
		return b
	}
	refs := make([]domain.DependencyRef, len(stageIDs))
	for i, id := range stageIDs {
		if id == "" {
			panic("ci: StageBuilder.DependsOnStages() called with empty stageID")
		}
		refs[i] = domain.DependencyRef{TargetType: domain.NodeTypeStage, TargetID: id}
	}
	b.write.Dependencies = &domain.DependencyGroup{
		Predicate:    domain.PredicateAND,
		Dependencies: refs,
	}
	return b
}

// DependsOnChecks sets AND dependencies on the given checks.
func (b *StageBuilder) DependsOnChecks(checkIDs ...string) *StageBuilder {
	if len(checkIDs) == 0 {
		return b
	}
	refs := make([]domain.DependencyRef, len(checkIDs))
	for i, id := range checkIDs {
		if id == "" {
			panic("ci: StageBuilder.DependsOnChecks() called with empty checkID")
		}
		refs[i] = domain.DependencyRef{TargetType: domain.NodeTypeCheck, TargetID: id}
	}
	if b.write.Dependencies == nil {
		b.write.Dependencies = &domain.DependencyGroup{Predicate: domain.PredicateAND}
	}
	b.write.Dependencies.Dependencies = append(b.write.Dependencies.Dependencies, refs...)
	return b
}

// DependsOn sets a full dependency group.
func (b *StageBuilder) DependsOn(deps *domain.DependencyGroup) *StageBuilder {
	b.write.Dependencies = deps
	return b
}

// DependsOnMixed sets AND dependencies on mixed node types.
func (b *StageBuilder) DependsOnMixed(refs ...NodeRef) *StageBuilder {
	if len(refs) == 0 {
		return b
	}
	depRefs := make([]domain.DependencyRef, len(refs))
	for i, ref := range refs {
		depRefs[i] = domain.DependencyRef{TargetType: ref.Type, TargetID: ref.ID}
	}
	b.write.Dependencies = &domain.DependencyGroup{
		Predicate:    domain.PredicateAND,
		Dependencies: depRefs,
	}
	return b
}

// CurrentAttempt sets the current attempt state.
func (b *StageBuilder) CurrentAttempt(aw *service.AttemptWrite) *StageBuilder {
	b.write.CurrentAttempt = aw
	return b
}

// ClaimAttempt creates an AttemptWrite that claims the current attempt.
func (b *StageBuilder) ClaimAttempt(processUID string) *StageBuilder {
	b.write.CurrentAttempt = &service.AttemptWrite{
		ProcessUID: processUID,
	}
	return b
}

// CompleteAttempt creates an AttemptWrite that completes the current attempt.
func (b *StageBuilder) CompleteAttempt() *StageBuilder {
	state := domain.AttemptStateComplete
	b.write.CurrentAttempt = &service.AttemptWrite{
		State: &state,
	}
	return b
}

// FailAttempt creates an AttemptWrite that fails the current attempt.
func (b *StageBuilder) FailAttempt(message string) *StageBuilder {
	state := domain.AttemptStateIncomplete
	b.write.CurrentAttempt = &service.AttemptWrite{
		State:   &state,
		Failure: &domain.Failure{Message: message},
	}
	return b
}

// Build returns the constructed StageWrite.
func (b *StageBuilder) Build() *service.StageWrite {
	return b.write
}

// AttemptBuilder provides a fluent API for constructing AttemptWrite objects.
type AttemptBuilder struct {
	write *service.AttemptWrite
}

// NewAttempt creates a new AttemptBuilder.
func NewAttempt() *AttemptBuilder {
	return &AttemptBuilder{
		write: &service.AttemptWrite{},
	}
}

// State sets the attempt state.
func (b *AttemptBuilder) State(state domain.AttemptState) *AttemptBuilder {
	b.write.State = &state
	return b
}

// Pending sets the state to PENDING.
func (b *AttemptBuilder) Pending() *AttemptBuilder {
	return b.State(domain.AttemptStatePending)
}

// Scheduled sets the state to SCHEDULED.
func (b *AttemptBuilder) Scheduled() *AttemptBuilder {
	return b.State(domain.AttemptStateScheduled)
}

// Running sets the state to RUNNING.
func (b *AttemptBuilder) Running() *AttemptBuilder {
	return b.State(domain.AttemptStateRunning)
}

// Complete sets the state to COMPLETE.
func (b *AttemptBuilder) Complete() *AttemptBuilder {
	return b.State(domain.AttemptStateComplete)
}

// Incomplete sets the state to INCOMPLETE.
func (b *AttemptBuilder) Incomplete() *AttemptBuilder {
	return b.State(domain.AttemptStateIncomplete)
}

// ProcessUID sets the process UID claiming this attempt.
func (b *AttemptBuilder) ProcessUID(uid string) *AttemptBuilder {
	b.write.ProcessUID = uid
	return b
}

// Details sets execution details.
func (b *AttemptBuilder) Details(details map[string]any) *AttemptBuilder {
	b.write.Details = details
	return b
}

// Detail sets a single detail key-value pair.
func (b *AttemptBuilder) Detail(key string, value any) *AttemptBuilder {
	if b.write.Details == nil {
		b.write.Details = make(map[string]any)
	}
	b.write.Details[key] = value
	return b
}

// Progress adds a progress entry.
func (b *AttemptBuilder) Progress(message string, details map[string]any) *AttemptBuilder {
	b.write.Progress = &domain.ProgressEntry{
		Message: message,
		Details: details,
	}
	return b
}

// Failure sets a failure message.
func (b *AttemptBuilder) Failure(message string) *AttemptBuilder {
	b.write.Failure = &domain.Failure{Message: message}
	return b
}

// Build returns the constructed AttemptWrite.
func (b *AttemptBuilder) Build() *service.AttemptWrite {
	return b.write
}
