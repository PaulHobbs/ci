package domain

import "errors"

var (
	// ErrNotFound is returned when a requested entity doesn't exist.
	ErrNotFound = errors.New("not found")

	// ErrInvalidState is returned when a state transition is not allowed.
	ErrInvalidState = errors.New("invalid state transition")

	// ErrConcurrentModify is returned when optimistic locking fails.
	ErrConcurrentModify = errors.New("concurrent modification")

	// ErrInvalidDependency is returned when a dependency is malformed.
	ErrInvalidDependency = errors.New("invalid dependency")

	// ErrCyclicDependency is returned when a dependency cycle is detected.
	ErrCyclicDependency = errors.New("cyclic dependency detected")

	// ErrInvalidArgument is returned when an argument is invalid.
	ErrInvalidArgument = errors.New("invalid argument")

	// ErrAlreadyExists is returned when trying to create a duplicate entity.
	ErrAlreadyExists = errors.New("already exists")

	// ErrAttemptClaimed is returned when a stage attempt is already claimed.
	ErrAttemptClaimed = errors.New("attempt already claimed by another process")

	// ErrRunnerNotFound is returned when no runner is registered for a type.
	ErrRunnerNotFound = errors.New("no runner registered for stage type")

	// ErrRunnerUnavailable is returned when runner cannot accept work.
	ErrRunnerUnavailable = errors.New("runner unavailable or at capacity")

	// ErrExecutionNotFound is returned when an execution ID is not found.
	ErrExecutionNotFound = errors.New("execution not found")

	// ErrExecutionExpired is returned when an execution has timed out.
	ErrExecutionExpired = errors.New("execution expired")

	// ErrInvalidCallback is returned when callback data is invalid.
	ErrInvalidCallback = errors.New("invalid callback data")
)
