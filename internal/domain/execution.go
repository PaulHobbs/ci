package domain

import (
	"time"

	"github.com/example/turboci-lite/pkg/id"
)

// ExecutionState tracks the state of a dispatched execution.
type ExecutionState int

const (
	ExecutionStatePending    ExecutionState = 10 // Queued, not yet dispatched
	ExecutionStateDispatched ExecutionState = 20 // Sent to runner
	ExecutionStateRunning    ExecutionState = 30 // Confirmed running by runner
	ExecutionStateComplete   ExecutionState = 40 // Successfully completed
	ExecutionStateFailed     ExecutionState = 50 // Failed/timed out
	ExecutionStateCancelled  ExecutionState = 60 // Cancelled before completion
)

func (s ExecutionState) String() string {
	switch s {
	case ExecutionStatePending:
		return "PENDING"
	case ExecutionStateDispatched:
		return "DISPATCHED"
	case ExecutionStateRunning:
		return "RUNNING"
	case ExecutionStateComplete:
		return "COMPLETE"
	case ExecutionStateFailed:
		return "FAILED"
	case ExecutionStateCancelled:
		return "CANCELLED"
	default:
		return "UNKNOWN"
	}
}

// IsFinal returns true if the execution is in a terminal state.
func (s ExecutionState) IsFinal() bool {
	return s == ExecutionStateComplete || s == ExecutionStateFailed || s == ExecutionStateCancelled
}

// StageExecution represents a single dispatch of a stage to a runner.
type StageExecution struct {
	ID              string
	WorkPlanID      string
	StageID         string
	AttemptIdx      int
	RunnerType      string
	ExecutionMode   ExecutionMode
	State           ExecutionState
	RunnerID        string     // ID of runner handling this execution
	DispatchedAt    *time.Time // When dispatched to runner
	StartedAt       *time.Time // When runner confirmed start
	CompletedAt     *time.Time // When execution completed
	Deadline        *time.Time // Execution deadline
	LastProgressAt  *time.Time // Last progress update
	ProgressPercent int        // 0-100 progress percentage
	ProgressMessage string     // Latest progress message
	ErrorMessage    string     // Error message if failed
	RetryCount      int        // Number of retry attempts
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewStageExecution creates a new pending execution.
func NewStageExecution(workPlanID, stageID string, attemptIdx int, runnerType string, mode ExecutionMode) *StageExecution {
	now := time.Now().UTC()
	return &StageExecution{
		ID:            id.Generate(),
		WorkPlanID:    workPlanID,
		StageID:       stageID,
		AttemptIdx:    attemptIdx,
		RunnerType:    runnerType,
		ExecutionMode: mode,
		State:         ExecutionStatePending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// MarkDispatched transitions to dispatched state.
func (e *StageExecution) MarkDispatched(runnerID string) error {
	if e.State != ExecutionStatePending {
		return ErrInvalidState
	}
	now := time.Now().UTC()
	e.State = ExecutionStateDispatched
	e.RunnerID = runnerID
	e.DispatchedAt = &now
	e.UpdatedAt = now
	return nil
}

// MarkRunning transitions to running state.
func (e *StageExecution) MarkRunning() error {
	if e.State != ExecutionStateDispatched && e.State != ExecutionStatePending {
		return ErrInvalidState
	}
	now := time.Now().UTC()
	e.State = ExecutionStateRunning
	e.StartedAt = &now
	e.UpdatedAt = now
	return nil
}

// MarkComplete transitions to complete state.
func (e *StageExecution) MarkComplete() error {
	if e.State.IsFinal() {
		return ErrInvalidState
	}
	now := time.Now().UTC()
	e.State = ExecutionStateComplete
	e.CompletedAt = &now
	e.UpdatedAt = now
	return nil
}

// MarkFailed transitions to failed state.
func (e *StageExecution) MarkFailed(errorMsg string) error {
	if e.State.IsFinal() {
		return ErrInvalidState
	}
	now := time.Now().UTC()
	e.State = ExecutionStateFailed
	e.ErrorMessage = errorMsg
	e.CompletedAt = &now
	e.UpdatedAt = now
	return nil
}

// MarkCancelled transitions to cancelled state.
func (e *StageExecution) MarkCancelled() error {
	if e.State.IsFinal() {
		return ErrInvalidState
	}
	now := time.Now().UTC()
	e.State = ExecutionStateCancelled
	e.CompletedAt = &now
	e.UpdatedAt = now
	return nil
}

// UpdateProgress updates progress information.
func (e *StageExecution) UpdateProgress(percent int, message string) {
	now := time.Now().UTC()
	e.ProgressPercent = percent
	e.ProgressMessage = message
	e.LastProgressAt = &now
	e.UpdatedAt = now
}

// IsExpired returns true if the execution has passed its deadline.
func (e *StageExecution) IsExpired() bool {
	if e.Deadline == nil {
		return false
	}
	return time.Now().UTC().After(*e.Deadline)
}

// SetDeadline sets the execution deadline.
func (e *StageExecution) SetDeadline(deadline time.Time) {
	e.Deadline = &deadline
	e.UpdatedAt = time.Now().UTC()
}
