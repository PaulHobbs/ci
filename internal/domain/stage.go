package domain

import (
	"fmt"
	"time"
)

// StageState describes the current state of a Stage.
type StageState int

const (
	StageStateUnknown       StageState = 0
	StageStatePlanned       StageState = 10 // Stage inserted but deps unresolved
	StageStateAttempting    StageState = 20 // Deps resolved, executing
	StageStateAwaitingGroup StageState = 30 // Execution done, waiting for children
	StageStateFinal         StageState = 40 // Fully complete
)

// ExecutionMode specifies how a stage should be executed.
type ExecutionMode int

const (
	ExecutionModeUnknown ExecutionMode = 0
	ExecutionModeSync    ExecutionMode = 1 // Block until completion
	ExecutionModeAsync   ExecutionMode = 2 // Return immediately, callback on completion
)

func (m ExecutionMode) String() string {
	switch m {
	case ExecutionModeSync:
		return "SYNC"
	case ExecutionModeAsync:
		return "ASYNC"
	default:
		return "UNKNOWN"
	}
}

func (s StageState) String() string {
	switch s {
	case StageStatePlanned:
		return "PLANNED"
	case StageStateAttempting:
		return "ATTEMPTING"
	case StageStateAwaitingGroup:
		return "AWAITING_GROUP"
	case StageStateFinal:
		return "FINAL"
	default:
		return "UNKNOWN"
	}
}

// ValidStageStateTransition checks if a state transition is valid.
// Valid transitions: PLANNED -> ATTEMPTING -> AWAITING_GROUP -> FINAL
func ValidStageStateTransition(from, to StageState) bool {
	switch from {
	case StageStatePlanned:
		return to == StageStateAttempting || to == StageStateFinal
	case StageStateAttempting:
		return to == StageStateAwaitingGroup || to == StageStateFinal
	case StageStateAwaitingGroup:
		return to == StageStateFinal
	case StageStateFinal:
		return false // No transitions from FINAL
	default:
		return to == StageStatePlanned // Allow setting initial state
	}
}

// AttemptState describes the state of a Stage Attempt.
type AttemptState int

const (
	AttemptStateUnknown    AttemptState = 0
	AttemptStatePending    AttemptState = 10 // Created, not yet scheduled
	AttemptStateScheduled  AttemptState = 20 // Scheduled for execution
	AttemptStateRunning    AttemptState = 30 // Currently executing
	AttemptStateComplete   AttemptState = 40 // Finished successfully
	AttemptStateIncomplete AttemptState = 50 // Failed or timed out
)

func (s AttemptState) String() string {
	switch s {
	case AttemptStatePending:
		return "PENDING"
	case AttemptStateScheduled:
		return "SCHEDULED"
	case AttemptStateRunning:
		return "RUNNING"
	case AttemptStateComplete:
		return "COMPLETE"
	case AttemptStateIncomplete:
		return "INCOMPLETE"
	default:
		return "UNKNOWN"
	}
}

// IsFinal returns true if the attempt is in a terminal state.
func (s AttemptState) IsFinal() bool {
	return s == AttemptStateComplete || s == AttemptStateIncomplete
}

// ValidAttemptStateTransition checks if a state transition is valid.
func ValidAttemptStateTransition(from, to AttemptState) bool {
	switch from {
	case AttemptStatePending:
		return to == AttemptStateScheduled || to == AttemptStateRunning || to == AttemptStateIncomplete
	case AttemptStateScheduled:
		return to == AttemptStateRunning || to == AttemptStateIncomplete
	case AttemptStateRunning:
		return to == AttemptStateComplete || to == AttemptStateIncomplete
	case AttemptStateComplete, AttemptStateIncomplete:
		return false // Terminal states
	default:
		return to == AttemptStatePending // Allow setting initial state
	}
}

// Stage is an executable node in the workflow.
type Stage struct {
	ID            string
	WorkPlanID    string
	State         StageState
	Args          map[string]any
	Attempts      []Attempt
	Assignments   []Assignment
	Dependencies  *DependencyGroup
	ExecutionMode ExecutionMode // Sync or async execution
	RunnerType    string        // Which runner type handles this stage
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Version       int64
}

// NewStage creates a new Stage with the given ID.
func NewStage(workPlanID, id string) *Stage {
	now := time.Now().UTC()
	return &Stage{
		ID:            id,
		WorkPlanID:    workPlanID,
		State:         StageStatePlanned,
		Args:          make(map[string]any),
		Attempts:      nil,
		Assignments:   nil,
		ExecutionMode: ExecutionModeSync, // Default to sync execution
		CreatedAt:     now,
		UpdatedAt:     now,
		Version:       1,
	}
}

// SetState transitions the stage to a new state.
func (s *Stage) SetState(newState StageState) error {
	if !ValidStageStateTransition(s.State, newState) {
		return fmt.Errorf("%w: cannot transition stage from %s to %s",
			ErrInvalidState, s.State, newState)
	}
	s.State = newState
	s.UpdatedAt = time.Now().UTC()
	// Note: Version is managed by the storage layer, not here
	return nil
}

// CreateAttempt creates a new attempt for this stage.
func (s *Stage) CreateAttempt() (*Attempt, error) {
	if s.State != StageStateAttempting {
		return nil, fmt.Errorf("%w: cannot create attempt for stage in state %s",
			ErrInvalidState, s.State)
	}

	// Check if there's already an active attempt
	if len(s.Attempts) > 0 {
		lastAttempt := &s.Attempts[len(s.Attempts)-1]
		if !lastAttempt.State.IsFinal() {
			return nil, fmt.Errorf("%w: previous attempt is still active",
				ErrInvalidState)
		}
	}

	now := time.Now().UTC()
	attempt := Attempt{
		Idx:       len(s.Attempts) + 1,
		State:     AttemptStatePending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.Attempts = append(s.Attempts, attempt)
	s.UpdatedAt = now
	// Note: Version is managed by the storage layer, not here

	return &s.Attempts[len(s.Attempts)-1], nil
}

// CurrentAttempt returns the current (last) attempt, if any.
func (s *Stage) CurrentAttempt() *Attempt {
	if len(s.Attempts) == 0 {
		return nil
	}
	return &s.Attempts[len(s.Attempts)-1]
}

// Attempt represents a single execution attempt of a Stage.
type Attempt struct {
	Idx        int
	State      AttemptState
	ProcessUID string
	Details    map[string]any
	Progress   []ProgressEntry
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Failure    *Failure
}

// SetState transitions the attempt to a new state.
func (a *Attempt) SetState(newState AttemptState) error {
	if !ValidAttemptStateTransition(a.State, newState) {
		return fmt.Errorf("%w: cannot transition attempt from %s to %s",
			ErrInvalidState, a.State, newState)
	}
	a.State = newState
	a.UpdatedAt = time.Now().UTC()
	return nil
}

// Claim claims this attempt for a process.
func (a *Attempt) Claim(processUID string) error {
	if a.State != AttemptStatePending && a.State != AttemptStateScheduled {
		return fmt.Errorf("%w: attempt in state %s cannot be claimed",
			ErrInvalidState, a.State)
	}
	if a.ProcessUID != "" && a.ProcessUID != processUID {
		return fmt.Errorf("%w: claimed by %s", ErrAttemptClaimed, a.ProcessUID)
	}
	a.ProcessUID = processUID
	a.State = AttemptStateRunning
	a.UpdatedAt = time.Now().UTC()
	return nil
}

// AddProgress adds a progress message.
func (a *Attempt) AddProgress(message string, details map[string]any) {
	a.Progress = append(a.Progress, ProgressEntry{
		Message:   message,
		Timestamp: time.Now().UTC(),
		Details:   details,
	})
	a.UpdatedAt = time.Now().UTC()
}

// SetFailure marks the attempt as failed.
func (a *Attempt) SetFailure(message string) {
	now := time.Now().UTC()
	a.Failure = &Failure{
		Message:    message,
		OccurredAt: now,
	}
	a.State = AttemptStateIncomplete
	a.UpdatedAt = now
}

// Complete marks the attempt as complete.
func (a *Attempt) Complete() error {
	return a.SetState(AttemptStateComplete)
}

// ProgressEntry is a progress message from an attempt.
type ProgressEntry struct {
	Message   string
	Timestamp time.Time
	Details   map[string]any
}

// Assignment indicates a stage's responsibility to handle a check.
type Assignment struct {
	TargetCheckID string
	GoalState     CheckState
}

// Failure contains information about a failure.
type Failure struct {
	Message    string
	OccurredAt time.Time
}
