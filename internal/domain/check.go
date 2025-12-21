package domain

import (
	"fmt"
	"time"
)

// CheckState describes the current state of a Check.
type CheckState int

const (
	CheckStateUnknown  CheckState = 0
	CheckStatePlanning CheckState = 10 // Check is being defined
	CheckStatePlanned  CheckState = 20 // Check complete but deps unresolved
	CheckStateWaiting  CheckState = 30 // Deps resolved, waiting for results
	CheckStateFinal    CheckState = 40 // Fully complete and immutable
)

func (s CheckState) String() string {
	switch s {
	case CheckStatePlanning:
		return "PLANNING"
	case CheckStatePlanned:
		return "PLANNED"
	case CheckStateWaiting:
		return "WAITING"
	case CheckStateFinal:
		return "FINAL"
	default:
		return "UNKNOWN"
	}
}

// ValidCheckStateTransition checks if a state transition is valid.
// Valid transitions: PLANNING -> PLANNED -> WAITING -> FINAL
func ValidCheckStateTransition(from, to CheckState) bool {
	switch from {
	case CheckStatePlanning:
		return to == CheckStatePlanned || to == CheckStateFinal
	case CheckStatePlanned:
		return to == CheckStateWaiting || to == CheckStateFinal
	case CheckStateWaiting:
		return to == CheckStateFinal
	case CheckStateFinal:
		return false // No transitions from FINAL
	default:
		return to == CheckStatePlanning // Allow setting initial state
	}
}

// Check is a non-executable node representing an objective/question.
type Check struct {
	ID           string
	WorkPlanID   string
	State        CheckState
	Kind         string
	Options      map[string]any
	Results      []CheckResult
	Dependencies *DependencyGroup
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Version      int64
}

// NewCheck creates a new Check with the given ID.
func NewCheck(workPlanID, id string) *Check {
	now := time.Now().UTC()
	return &Check{
		ID:         id,
		WorkPlanID: workPlanID,
		State:      CheckStatePlanning,
		Options:    make(map[string]any),
		Results:    nil,
		CreatedAt:  now,
		UpdatedAt:  now,
		Version:    1,
	}
}

// SetState transitions the check to a new state.
func (c *Check) SetState(newState CheckState) error {
	if !ValidCheckStateTransition(c.State, newState) {
		return fmt.Errorf("%w: cannot transition check from %s to %s",
			ErrInvalidState, c.State, newState)
	}
	c.State = newState
	c.UpdatedAt = time.Now().UTC()
	// Note: Version is managed by the storage layer, not here
	return nil
}

// AddResult adds a result to the check.
func (c *Check) AddResult(result CheckResult) error {
	if c.State != CheckStateWaiting && c.State != CheckStatePlanning {
		return fmt.Errorf("%w: cannot add result to check in state %s",
			ErrInvalidState, c.State)
	}
	result.CreatedAt = time.Now().UTC()
	c.Results = append(c.Results, result)
	c.UpdatedAt = time.Now().UTC()
	// Note: Version is managed by the storage layer, not here
	return nil
}

// CheckResult contains results from a stage attempt for a check.
type CheckResult struct {
	ID          int64
	OwnerType   string         // e.g., "stage_attempt"
	OwnerID     string         // e.g., "stage_id:attempt_idx"
	Data        map[string]any // JSON-compatible
	CreatedAt   time.Time
	FinalizedAt *time.Time
	Failure     *Failure
}

// NewCheckResult creates a new CheckResult.
func NewCheckResult(ownerType, ownerID string, data map[string]any) CheckResult {
	return CheckResult{
		OwnerType: ownerType,
		OwnerID:   ownerID,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	}
}

// Finalize marks the result as finalized.
func (r *CheckResult) Finalize() {
	now := time.Now().UTC()
	r.FinalizedAt = &now
}

// SetFailure sets a failure on the result.
func (r *CheckResult) SetFailure(message string) {
	now := time.Now().UTC()
	r.Failure = &Failure{
		Message:    message,
		OccurredAt: now,
	}
	r.FinalizedAt = &now
}
