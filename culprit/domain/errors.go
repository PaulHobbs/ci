package domain

import "errors"

var (
	// ErrNotFound is returned when a requested entity doesn't exist.
	ErrNotFound = errors.New("not found")

	// ErrInvalidState is returned when a state transition is not allowed.
	ErrInvalidState = errors.New("invalid state transition")

	// ErrInvalidConfig is returned when configuration is invalid.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrTooFewCommits is returned when there are too few commits to search.
	ErrTooFewCommits = errors.New("too few commits to search")

	// ErrNoResults is returned when decoding has no results available.
	ErrNoResults = errors.New("no results available for decoding")

	// ErrMaterializationFailed is returned when commit materialization fails.
	ErrMaterializationFailed = errors.New("materialization failed")

	// ErrTestExecutionFailed is returned when test execution fails due to infra.
	ErrTestExecutionFailed = errors.New("test execution failed")

	// ErrSessionNotFound is returned when a search session doesn't exist.
	ErrSessionNotFound = errors.New("session not found")

	// ErrSessionAlreadyComplete is returned when trying to modify a complete session.
	ErrSessionAlreadyComplete = errors.New("session already complete")
)
