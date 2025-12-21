package domain

import (
	"fmt"
	"time"
)

// SearchStatus represents the current state of a culprit search.
type SearchStatus int

const (
	StatusPending  SearchStatus = iota // Search not yet started
	StatusRunning                      // Tests are being executed
	StatusDecoding                     // All tests complete, decoding in progress
	StatusComplete                     // Search complete with results
	StatusFailed                       // Search failed (infrastructure error)
)

func (s SearchStatus) String() string {
	switch s {
	case StatusPending:
		return "PENDING"
	case StatusRunning:
		return "RUNNING"
	case StatusDecoding:
		return "DECODING"
	case StatusComplete:
		return "COMPLETE"
	case StatusFailed:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

// IsTerminal returns true if this is a final status.
func (s SearchStatus) IsTerminal() bool {
	return s == StatusComplete || s == StatusFailed
}

// SearchSession tracks an ongoing culprit search.
type SearchSession struct {
	// ID is the unique identifier for this session.
	ID string

	// CommitRange is the range of commits being searched.
	CommitRange CommitRange

	// TestCommand is the command to run for testing.
	TestCommand string

	// TestTimeout is the timeout for each test execution.
	TestTimeout time.Duration

	// Config is the culprit finder configuration.
	Config CulpritFinderConfig

	// Matrix is the test matrix for this search.
	Matrix *TestMatrix

	// Status is the current search status.
	Status SearchStatus

	// Progress tracks test execution progress.
	Progress SearchProgress

	// Result contains the decoding result when complete.
	Result *DecodingResult

	// FailureReason contains the failure reason if Status is Failed.
	FailureReason string

	// WorkPlanID is the orchestrator WorkPlan ID (if using orchestrator).
	WorkPlanID string

	// CreatedAt is when the session was created.
	CreatedAt time.Time

	// UpdatedAt is when the session was last updated.
	UpdatedAt time.Time

	// CompletedAt is when the session completed (if complete).
	CompletedAt *time.Time
}

// SearchProgress tracks the progress of test execution.
type SearchProgress struct {
	// TotalGroups is the total number of test group instances.
	TotalGroups int

	// CompletedGroups is the number of completed test groups.
	CompletedGroups int

	// PassedGroups is the number of groups that passed.
	PassedGroups int

	// FailedGroups is the number of groups that failed.
	FailedGroups int

	// InfraFailedGroups is the number of groups with infra failures.
	InfraFailedGroups int
}

// PendingGroups returns the number of groups not yet complete.
func (p SearchProgress) PendingGroups() int {
	return p.TotalGroups - p.CompletedGroups
}

// PercentComplete returns the completion percentage.
func (p SearchProgress) PercentComplete() float64 {
	if p.TotalGroups == 0 {
		return 100.0
	}
	return float64(p.CompletedGroups) * 100.0 / float64(p.TotalGroups)
}

// NewSearchSession creates a new search session.
func NewSearchSession(id string, commitRange CommitRange, testCommand string, testTimeout time.Duration, config CulpritFinderConfig) *SearchSession {
	now := time.Now().UTC()
	return &SearchSession{
		ID:          id,
		CommitRange: commitRange,
		TestCommand: testCommand,
		TestTimeout: testTimeout,
		Config:      config.WithDefaults(),
		Status:      StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// SetMatrix sets the test matrix for this session.
func (s *SearchSession) SetMatrix(matrix *TestMatrix) {
	s.Matrix = matrix
	s.Progress.TotalGroups = matrix.TotalTests()
	s.UpdatedAt = time.Now().UTC()
}

// SetStatus transitions the session to a new status.
func (s *SearchSession) SetStatus(newStatus SearchStatus) error {
	if s.Status.IsTerminal() {
		return fmt.Errorf("%w: cannot transition from terminal status %s",
			ErrInvalidState, s.Status)
	}
	s.Status = newStatus
	s.UpdatedAt = time.Now().UTC()
	if newStatus.IsTerminal() {
		now := time.Now().UTC()
		s.CompletedAt = &now
	}
	return nil
}

// RecordResult records a test group result and updates progress.
func (s *SearchSession) RecordResult(result TestGroupResult) {
	s.Progress.CompletedGroups++
	switch result.Outcome {
	case OutcomePass:
		s.Progress.PassedGroups++
	case OutcomeFail:
		s.Progress.FailedGroups++
	case OutcomeInfra:
		s.Progress.InfraFailedGroups++
	}
	s.UpdatedAt = time.Now().UTC()
}

// SetResult sets the decoding result.
func (s *SearchSession) SetResult(result *DecodingResult) {
	s.Result = result
	s.UpdatedAt = time.Now().UTC()
}

// SetFailed marks the session as failed with a reason.
func (s *SearchSession) SetFailed(reason string) {
	s.Status = StatusFailed
	s.FailureReason = reason
	now := time.Now().UTC()
	s.CompletedAt = &now
	s.UpdatedAt = now
}

// IsComplete returns true if the session has a final result.
func (s *SearchSession) IsComplete() bool {
	return s.Status.IsTerminal()
}

// AllTestsComplete returns true if all test groups have results.
func (s *SearchSession) AllTestsComplete() bool {
	return s.Progress.CompletedGroups >= s.Progress.TotalGroups
}
