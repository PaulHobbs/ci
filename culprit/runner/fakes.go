package runner

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/example/turboci-lite/culprit/domain"
)

// FakeCommitMaterializer is a test double for CommitMaterializer.
// It doesn't actually check out any code, just tracks calls.
type FakeCommitMaterializer struct {
	mu sync.RWMutex

	// States tracks all materialized states.
	States []*MaterializedState

	// FailOn causes Materialize to fail when any of these SHAs are included.
	FailOn map[string]error

	// Delay adds artificial delay to Materialize calls.
	Delay time.Duration

	// IDCounter is used to generate unique IDs.
	IDCounter int
}

// NewFakeMaterializer creates a new FakeCommitMaterializer.
func NewFakeMaterializer() *FakeCommitMaterializer {
	return &FakeCommitMaterializer{
		FailOn: make(map[string]error),
	}
}

// Materialize implements CommitMaterializer.
func (m *FakeCommitMaterializer) Materialize(ctx context.Context, baseRef string, commits []domain.Commit) (*MaterializedState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for configured failures
	for _, c := range commits {
		if err, ok := m.FailOn[c.SHA]; ok {
			return nil, err
		}
	}

	// Apply delay
	if m.Delay > 0 {
		select {
		case <-time.After(m.Delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	m.IDCounter++
	state := &MaterializedState{
		ID:        fmt.Sprintf("fake-state-%d", m.IDCounter),
		WorkDir:   fmt.Sprintf("/fake/workdir/%d", m.IDCounter),
		BaseRef:   baseRef,
		Commits:   commits,
		CreatedAt: time.Now().UTC(),
	}

	m.States = append(m.States, state)
	return state, nil
}

// Cleanup implements CommitMaterializer.
func (m *FakeCommitMaterializer) Cleanup(ctx context.Context, state *MaterializedState) error {
	// No-op for fake
	return nil
}

// GetMaterializations returns all states that have been created.
func (m *FakeCommitMaterializer) GetMaterializations() []*MaterializedState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*MaterializedState, len(m.States))
	copy(result, m.States)
	return result
}

// Reset clears all state.
func (m *FakeCommitMaterializer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.States = nil
	m.IDCounter = 0
}

// FakeTestRunner is a test double for TestRunner.
// It simulates test outcomes based on configured culprits and flake rates.
type FakeTestRunner struct {
	mu sync.RWMutex

	// Culprits maps commit SHAs to whether they are culprits.
	// If a commit is a culprit, any test group containing it will fail
	// (unless masked by flakiness).
	Culprits map[string]bool

	// FlakeRate is the probability of a random flake (0-1).
	// Both false positives and false negatives use this rate.
	FlakeRate float64

	// InfraFailureRate is the probability of an infrastructure failure.
	InfraFailureRate float64

	// Results tracks all test results.
	Results []domain.TestGroupResult

	// Delay adds artificial delay to Run calls.
	Delay time.Duration

	// Seed is the random seed for reproducibility (0 for random).
	Seed int64

	rng *rand.Rand
}

// NewFakeTestRunner creates a new FakeTestRunner.
func NewFakeTestRunner() *FakeTestRunner {
	return &FakeTestRunner{
		Culprits: make(map[string]bool),
	}
}

// WithCulprits sets the culprit commits.
func (r *FakeTestRunner) WithCulprits(shas ...string) *FakeTestRunner {
	for _, sha := range shas {
		r.Culprits[sha] = true
	}
	return r
}

// WithFlakeRate sets the flake rate.
func (r *FakeTestRunner) WithFlakeRate(rate float64) *FakeTestRunner {
	r.FlakeRate = rate
	return r
}

// WithSeed sets the random seed for reproducibility.
func (r *FakeTestRunner) WithSeed(seed int64) *FakeTestRunner {
	r.Seed = seed
	return r
}

// WithInfraFailureRate sets the infrastructure failure rate.
func (r *FakeTestRunner) WithInfraFailureRate(rate float64) *FakeTestRunner {
	r.InfraFailureRate = rate
	return r
}

// WithDelay sets an artificial delay for test runs.
func (r *FakeTestRunner) WithDelay(delay time.Duration) *FakeTestRunner {
	r.Delay = delay
	return r
}

// Run implements TestRunner.
func (r *FakeTestRunner) Run(ctx context.Context, state *MaterializedState, config TestConfig) (*domain.TestGroupResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Initialize RNG if needed
	if r.rng == nil {
		if r.Seed == 0 {
			r.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
		} else {
			r.rng = rand.New(rand.NewSource(r.Seed))
		}
	}

	// Apply delay
	if r.Delay > 0 {
		select {
		case <-time.After(r.Delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Check for infrastructure failure
	if r.InfraFailureRate > 0 && r.rng.Float64() < r.InfraFailureRate {
		result := &domain.TestGroupResult{
			GroupID:    state.ID,
			Outcome:    domain.OutcomeInfra,
			Duration:   100 * time.Millisecond,
			Logs:       "Simulated infrastructure failure",
			ExecutedAt: time.Now().UTC(),
		}
		r.Results = append(r.Results, *result)
		return result, nil
	}

	// Determine true outcome based on culprits
	hasCulprit := false
	for _, commit := range state.Commits {
		if r.Culprits[commit.SHA] {
			hasCulprit = true
			break
		}
	}

	// Determine observed outcome (with possible flakes)
	outcome := domain.OutcomePass
	if hasCulprit {
		outcome = domain.OutcomeFail
		// Possible false positive (test passes despite culprit)
		if r.FlakeRate > 0 && r.rng.Float64() < r.FlakeRate {
			outcome = domain.OutcomePass
		}
	} else {
		// Possible false negative (test fails despite no culprit)
		if r.FlakeRate > 0 && r.rng.Float64() < r.FlakeRate {
			outcome = domain.OutcomeFail
		}
	}

	result := &domain.TestGroupResult{
		GroupID:    state.ID,
		Outcome:    outcome,
		Duration:   100 * time.Millisecond,
		ExecutedAt: time.Now().UTC(),
	}

	if outcome == domain.OutcomeFail {
		result.Logs = "Test failed"
	} else {
		result.Logs = "Test passed"
	}

	r.Results = append(r.Results, *result)
	return result, nil
}

// GetResults returns all test results.
func (r *FakeTestRunner) GetResults() []domain.TestGroupResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	results := make([]domain.TestGroupResult, len(r.Results))
	copy(results, r.Results)
	return results
}

// Reset clears all state.
func (r *FakeTestRunner) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Results = nil
	r.rng = nil
}

// FakeIDGenerator generates sequential IDs for testing.
type FakeIDGenerator struct {
	mu      sync.Mutex
	counter int
	prefix  string
}

// NewFakeIDGenerator creates a new FakeIDGenerator.
func NewFakeIDGenerator(prefix string) *FakeIDGenerator {
	return &FakeIDGenerator{prefix: prefix}
}

// Generate returns a new unique ID.
func (g *FakeIDGenerator) Generate() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.counter++
	return fmt.Sprintf("%s-%d", g.prefix, g.counter)
}
