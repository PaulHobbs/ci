# Culprit Finder

A non-adaptive group testing culprit finder for identifying which commit(s) in a range introduced a failure, with robustness to flaky tests.

## Quick Start

```go
package main

import (
    "context"
    "time"

    "go.chromium.org/luci/turboci-lite/culprit/domain"
    "go.chromium.org/luci/turboci-lite/culprit/runner"
    "go.chromium.org/luci/turboci-lite/internal/storage/sqlite"
    "go.chromium.org/luci/turboci-lite/pkg/id"
)

func main() {
    ctx := context.Background()

    // Setup storage
    storage, _ := sqlite.New("culprit.db")
    defer storage.Close()
    storage.Migrate(ctx)

    // Setup materializer and test runner
    materializer := runner.NewGitMaterializer("/path/to/repo")
    testRunner := runner.NewCommandTestRunner()

    // Create culprit finder
    finder := runner.NewRunner(storage, materializer, testRunner, id.Generate)

    // Define commits to search
    commits := []domain.Commit{
        {SHA: "abc123", Index: 0, Subject: "Commit 1"},
        {SHA: "def456", Index: 1, Subject: "Commit 2"},
        // ... more commits
    }

    // Run search
    result, err := finder.RunSearch(ctx, &runner.SearchRequest{
        CommitRange: domain.CommitRange{
            Repository: "myrepo",
            BaseRef:    "known-good-ref",
            Commits:    commits,
        },
        TestCommand: "make test",
        TestTimeout: 10 * time.Minute,
        Config: domain.CulpritFinderConfig{
            MaxCulprits: 2,
            Repetitions: 3,
        },
    })

    // Process results
    for _, culprit := range result.IdentifiedCulprits {
        fmt.Printf("Culprit: %s (confidence: %.2f)\n",
            culprit.Commit.SHA, culprit.Confidence)
    }
}
```

## Configuration

### CulpritFinderConfig

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `MaxCulprits` | int | 2 | Maximum expected number of simultaneous culprits |
| `Repetitions` | int | 3 | Number of times to run each test group |
| `ConfidenceThreshold` | float64 | 0.8 | Minimum confidence (0-1) to report a culprit |
| `FalsePositiveRate` | float64 | 0.05 | Expected probability of flaky pass |
| `FalseNegativeRate` | float64 | 0.10 | Expected probability of flaky fail |
| `RandomSeed` | int64 | 0 | Seed for reproducibility (0 = random) |

### Tuning Guidelines

**For larger commit ranges (n > 100):**
```go
config := domain.CulpritFinderConfig{
    MaxCulprits: 2,
    Repetitions: 5,  // More repetitions for accuracy
}
```

**For known-flaky tests:**
```go
config := domain.CulpritFinderConfig{
    Repetitions:       7,    // More repetitions
    FalsePositiveRate: 0.15, // Match actual flake rate
    FalseNegativeRate: 0.15,
}
```

**For speed over accuracy:**
```go
config := domain.CulpritFinderConfig{
    MaxCulprits: 1,   // Assume single culprit
    Repetitions: 1,   // No repetitions
}
```

## Plugging in Custom Components

### Custom CommitMaterializer

Implement the `CommitMaterializer` interface to support different VCS or build systems:

```go
type CommitMaterializer interface {
    Materialize(ctx context.Context, baseRef string, commits []domain.Commit) (*MaterializedState, error)
    Cleanup(ctx context.Context, state *MaterializedState) error
}

// Example: Gerrit-based materializer
type GerritMaterializer struct {
    client *gerrit.Client
}

func (m *GerritMaterializer) Materialize(ctx context.Context, baseRef string, commits []domain.Commit) (*MaterializedState, error) {
    // Cherry-pick CLs from Gerrit
    // ...
}
```

### Custom TestRunner

Implement the `TestRunner` interface for custom test execution:

```go
type TestRunner interface {
    Run(ctx context.Context, state *MaterializedState, config TestConfig) (*domain.TestGroupResult, error)
}

// Example: Remote test runner
type RemoteTestRunner struct {
    buildbot *buildbot.Client
}

func (r *RemoteTestRunner) Run(ctx context.Context, state *MaterializedState, config TestConfig) (*domain.TestGroupResult, error) {
    // Submit job to buildbot
    // Wait for result
    // ...
}
```

## Testing with Fakes

Use the provided fakes for unit and integration testing:

```go
package mytest

import (
    "testing"
    "go.chromium.org/luci/turboci-lite/culprit/runner"
)

func TestMyCulpritLogic(t *testing.T) {
    // Create fake materializer
    materializer := runner.NewFakeMaterializer()

    // Create fake test runner with configured culprits
    testRunner := runner.NewFakeTestRunner().
        WithCulprits("bad-commit-sha").
        WithFlakeRate(0.1).
        WithSeed(42)

    // Use in tests...
}
```

### FakeTestRunner Options

```go
testRunner := runner.NewFakeTestRunner().
    WithCulprits("sha1", "sha2").  // Commits that cause failures
    WithFlakeRate(0.1).             // 10% flake rate
    WithSeed(42)                    // For reproducibility
```

### FakeMaterializer Options

```go
materializer := runner.NewFakeMaterializer()
materializer.Delay = 100 * time.Millisecond  // Simulate latency
materializer.FailOn["bad-sha"] = errors.New("conflict")  // Inject failures
```

## Understanding Results

### DecodingResult

```go
type DecodingResult struct {
    IdentifiedCulprits []CulpritCandidate  // High-confidence culprits
    Confidence         float64              // Overall result confidence
    InnocentCommits    []string             // Definitely not culprits
    AmbiguousCommits   []string             // Could not determine
    GroupOutcomes      []AggregatedGroupOutcome  // Per-group results
}
```

### CulpritCandidate

```go
type CulpritCandidate struct {
    Commit     Commit    // The suspected commit
    Score      float64   // Log-likelihood ratio (higher = more evidence)
    Confidence float64   // Probability of being culprit (0-1)
    Evidence   []Evidence  // Which test groups contributed
}
```

### Interpreting Confidence

| Confidence | Interpretation |
|------------|----------------|
| > 0.95 | Very high confidence, almost certainly a culprit |
| 0.8 - 0.95 | High confidence, likely a culprit |
| 0.6 - 0.8 | Moderate confidence, probably a culprit |
| 0.4 - 0.6 | Ambiguous, cannot determine |
| < 0.4 | Probably not a culprit |

## Algorithm Details

See [DESIGN.md](DESIGN.md) for detailed algorithm description including:
- d-disjunct matrix construction
- Majority voting for flake robustness
- Log-likelihood scoring
- Complexity analysis

## Package Structure

```
culprit/
├── domain/          # Domain types
│   ├── commit.go    # Commit, CommitRange
│   ├── matrix.go    # TestMatrix, TestGroup
│   ├── result.go    # TestGroupResult, DecodingResult
│   ├── session.go   # SearchSession
│   └── config.go    # CulpritFinderConfig
├── matrix/          # Matrix construction
│   ├── builder.go   # MatrixBuilder
│   └── disjunct.go  # d-disjunct algorithm
├── decoder/         # Result decoding
│   ├── decoder.go   # Decoder
│   ├── majority.go  # Majority voting
│   └── likelihood.go # Scoring
└── runner/          # Execution
    ├── interfaces.go # CommitMaterializer, TestRunner
    ├── fakes.go     # Test doubles
    ├── git.go       # Git materializer
    └── runner.go    # Orchestrator integration
```

## Error Handling

| Error | Cause | Resolution |
|-------|-------|------------|
| `ErrTooFewCommits` | Need at least 2 commits | Provide larger commit range |
| `ErrInvalidConfig` | Invalid configuration | Check config parameters |
| `ErrMaterializationFailed` | Could not prepare codebase | Check for merge conflicts |
| `ErrNoResults` | No test results available | Ensure tests ran successfully |
| `ErrSessionNotFound` | Unknown session ID | Check session ID |

## Performance Considerations

### Test Count

The number of tests is approximately:
```
tests = 4 * d² * log₂(n) * r
```

Where:
- `d` = MaxCulprits
- `n` = number of commits
- `r` = Repetitions

### Examples

| Commits | MaxCulprits | Repetitions | Total Tests |
|---------|-------------|-------------|-------------|
| 20 | 1 | 3 | ~60 |
| 50 | 2 | 3 | ~320 |
| 100 | 2 | 3 | ~400 |
| 500 | 2 | 5 | ~920 |

### Parallelization

All test groups run in parallel. With sufficient resources, the wall-clock time equals:
```
time = max(test_duration) * repetitions
```

Not:
```
time = sum(test_durations)  // This would be sequential
```
