package matrix

import (
	"fmt"

	"github.com/example/turboci-lite/culprit/domain"
)

// Builder constructs test matrices for group testing.
type Builder interface {
	// Build creates a test matrix for the given commits and configuration.
	Build(commits []domain.Commit, config domain.CulpritFinderConfig) (*domain.TestMatrix, error)
}

// DisjunctBuilder builds d-disjunct matrices for group testing.
// A d-disjunct matrix guarantees that any set of at most d columns (culprits)
// can be uniquely identified from the test outcomes.
type DisjunctBuilder struct {
	idGenerator func() string
}

// NewBuilder creates a new DisjunctBuilder.
func NewBuilder(idGenerator func() string) *DisjunctBuilder {
	return &DisjunctBuilder{
		idGenerator: idGenerator,
	}
}

// Build creates a test matrix for the given commits.
// The matrix is designed to identify up to config.MaxCulprits culprits.
func (b *DisjunctBuilder) Build(commits []domain.Commit, config domain.CulpritFinderConfig) (*domain.TestMatrix, error) {
	config = config.WithDefaults()
	if err := config.Validate(); err != nil {
		return nil, err
	}

	n := len(commits)
	if n < 2 {
		return nil, fmt.Errorf("%w: need at least 2 commits, got %d",
			domain.ErrTooFewCommits, n)
	}

	d := config.MaxCulprits

	// Calculate number of test groups needed
	// For d-disjunct matrix, we need O(d^2 * log n) groups
	numGroups := calculateNumGroups(n, d)

	// Generate the d-disjunct matrix using random construction
	matrix := generateRandomDisjunctMatrix(n, d, numGroups, config.RandomSeed)

	// Convert to domain types
	groups := make([]domain.TestGroup, len(matrix))
	for i, row := range matrix {
		groups[i] = domain.TestGroup{
			ID:            fmt.Sprintf("group-%d", i),
			Index:         i,
			CommitIndices: row,
		}
	}

	return &domain.TestMatrix{
		ID:          b.idGenerator(),
		NumCommits:  n,
		Groups:      groups,
		Repetitions: config.Repetitions,
		Config:      config,
	}, nil
}
