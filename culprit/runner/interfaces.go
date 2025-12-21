package runner

import (
	"context"
	"time"

	"github.com/example/turboci-lite/culprit/domain"
)

// CommitMaterializer prepares the codebase state for testing a group of commits.
// It abstracts away the details of how commits are applied (git, gerrit, etc.).
type CommitMaterializer interface {
	// Materialize prepares a codebase state with the given commits applied.
	// The commits are applied on top of the baseRef in order.
	// Returns a MaterializedState that can be used for testing.
	Materialize(ctx context.Context, baseRef string, commits []domain.Commit) (*MaterializedState, error)

	// Cleanup releases resources associated with a materialized state.
	Cleanup(ctx context.Context, state *MaterializedState) error
}

// MaterializedState represents a prepared codebase state ready for testing.
type MaterializedState struct {
	// ID is a unique identifier for this state.
	ID string

	// WorkDir is the directory containing the materialized codebase.
	WorkDir string

	// BaseRef is the reference the commits were applied onto.
	BaseRef string

	// Commits are the commits that were applied.
	Commits []domain.Commit

	// CreatedAt is when this state was created.
	CreatedAt time.Time
}

// TestRunner executes tests on a materialized codebase state.
type TestRunner interface {
	// Run executes the test on the given materialized state.
	// Returns the test outcome and any logs.
	Run(ctx context.Context, state *MaterializedState, config TestConfig) (*domain.TestGroupResult, error)
}

// TestConfig specifies how to run a test.
type TestConfig struct {
	// Command is the test command to execute.
	Command string

	// Timeout is the maximum time to wait for the test.
	Timeout time.Duration

	// Environment contains additional environment variables.
	Environment map[string]string

	// WorkDir overrides the working directory (default is MaterializedState.WorkDir).
	WorkDir string
}

// CulpritFinder is the main interface for culprit finding.
type CulpritFinder interface {
	// StartSearch initiates a culprit search for a commit range.
	StartSearch(ctx context.Context, req *SearchRequest) (*domain.SearchSession, error)

	// GetSession retrieves an existing search session.
	GetSession(ctx context.Context, sessionID string) (*domain.SearchSession, error)

	// GetResults retrieves the current results (may be partial if still running).
	GetResults(ctx context.Context, sessionID string) (*domain.DecodingResult, error)

	// WaitForCompletion waits for a search to complete.
	WaitForCompletion(ctx context.Context, sessionID string) (*domain.DecodingResult, error)
}

// SearchRequest contains all parameters needed to start a culprit search.
type SearchRequest struct {
	// CommitRange is the range of commits to search.
	CommitRange domain.CommitRange

	// TestCommand is the command to run for testing.
	TestCommand string

	// TestTimeout is the timeout for each test execution.
	TestTimeout time.Duration

	// Config is the culprit finder configuration.
	Config domain.CulpritFinderConfig
}

// Validate checks that the search request is valid.
func (r *SearchRequest) Validate() error {
	if len(r.CommitRange.Commits) < 2 {
		return domain.ErrTooFewCommits
	}
	if r.TestCommand == "" {
		return domain.ErrInvalidConfig
	}
	if r.TestTimeout <= 0 {
		r.TestTimeout = 10 * time.Minute
	}
	return r.Config.Validate()
}
