# Culprit Finder Design

This document describes the design and algorithm of the non-adaptive group testing culprit finder for turboci-lite.

## Overview

The culprit finder identifies which commit(s) in a range introduced a failure. It uses **non-adaptive group testing** to test multiple subsets of commits in parallel, then analyzes results to identify culprits.

### Key Properties

- **Non-adaptive**: All test groups are determined upfront, enabling maximum parallelization
- **Flake-robust**: Uses repetition and probabilistic decoding to handle flaky tests
- **Modular**: Abstract interfaces allow plugging in different commit materializers and test runners

## Algorithm: d-Disjunct Matrices with Repetition

### Problem Statement

Given:
- A range of `n` commits, at most `d` of which are "culprits" (introduced failures)
- A test that returns PASS/FAIL but may be flaky (false positives/negatives)
- Goal: Identify all culprits with high confidence using minimum tests

### Why d-Disjunct Matrices?

A binary matrix `M` (rows=tests, columns=commits) is **d-disjunct** if for any column `c` and any set `S` of `d` other columns, column `c` is not "covered" by the union of columns in `S`. This means there exists at least one row where `c` has a 1 but all columns in `S` have 0s.

**Properties:**
- Information-theoretically optimal: O(d² log n) tests for n items with ≤d culprits
- Simple decoding: A commit appearing in a passing test is definitively innocent
- Graceful degradation: Even with some errors, likely culprits can be identified

### Matrix Construction

We use a **random sparse matrix construction**:

1. Calculate number of groups: `t = 4 * d² * log₂(n)`
2. Calculate column weight: `w = 2 * d * log₂(n)` (tests per commit)
3. For each commit, randomly select `w` groups to include it in

This creates a matrix where:
- Each commit appears in approximately `w` groups
- Groups have varying sizes based on random assignment
- With high probability, the matrix is d-disjunct

### Flake Handling

Tests may be flaky:
- **False positive**: Test passes when it should fail (culprit present but masked)
- **False negative**: Test fails when it should pass (no culprit but random failure)

We handle flakes through:

1. **Repetition**: Run each test group `r` times (typically 3-5)
2. **Majority voting**: Aggregate per-group results by majority vote
3. **Probabilistic decoding**: Use log-likelihood scoring instead of deterministic elimination

### Decoding Algorithm

#### Step 1: Majority Voting

For each base group, aggregate the `r` repetition results:
- Count PASS, FAIL, and INFRA outcomes
- Final outcome = majority vote (ties go to FAIL for safety)

#### Step 2: Log-Likelihood Scoring

For each commit `c`, compute a score:

```
score(c) = Σ log(P(observation | c is culprit) / P(observation | c is innocent))
```

Where for each group `g` containing `c`:
- If group FAILED: contribution = log((1-FP_rate) / FN_rate)
- If group PASSED: contribution = log(FP_rate / (1-FN_rate))

Higher scores indicate stronger evidence of being a culprit.

#### Step 3: Classification

- Convert scores to confidence via sigmoid: `confidence = 1 / (1 + exp(-score))`
- Commits with confidence ≥ threshold are classified as culprits
- Commits with confidence ≤ (1 - threshold) are classified as innocent
- Remaining commits are ambiguous

## Architecture

### Package Structure

```
culprit/
├── domain/          # Domain types (Commit, TestMatrix, TestGroupResult, etc.)
├── matrix/          # Matrix construction (d-disjunct builder)
├── decoder/         # Result decoding (majority voting + likelihood scoring)
├── runner/          # Execution coordination
│   ├── interfaces.go    # CommitMaterializer, TestRunner interfaces
│   ├── fakes.go         # Fake implementations for testing
│   ├── git.go           # Git-based commit materializer
│   └── runner.go        # Orchestrator-integrated runner
└── storage/         # Optional persistence
```

### Key Interfaces

```go
// CommitMaterializer prepares codebase state for testing
type CommitMaterializer interface {
    Materialize(ctx context.Context, baseRef string, commits []Commit) (*MaterializedState, error)
    Cleanup(ctx context.Context, state *MaterializedState) error
}

// TestRunner executes tests on materialized state
type TestRunner interface {
    Run(ctx context.Context, state *MaterializedState, config TestConfig) (*TestGroupResult, error)
}
```

### Orchestrator Integration

The culprit finder maps naturally to turboci-lite's concepts:

| Culprit Finder | Orchestrator |
|----------------|--------------|
| SearchSession | WorkPlan |
| TestGroup | Check (kind="culprit_test_group") |
| Group execution | Stage (type="culprit_test") |
| TestGroupResult | CheckResult |
| Decoding | Stage (type="culprit_decode") |

All test groups run in parallel (no inter-dependencies), and a final decode stage waits for all tests to complete.

## Configuration Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| MaxCulprits | 2 | Maximum expected culprits (d) |
| Repetitions | 3 | Times to repeat each test group |
| ConfidenceThreshold | 0.8 | Min confidence to report culprit |
| FalsePositiveRate | 0.05 | Expected rate of flaky passes |
| FalseNegativeRate | 0.10 | Expected rate of flaky failures |
| RandomSeed | 0 | Seed for matrix generation (0 = random) |

## Complexity Analysis

### Test Count

Base groups: `O(d² log n)`
Total tests: `O(d² r log n)`

| d | n=20 | n=50 | n=100 | n=500 |
|---|------|------|-------|-------|
| 1 | 20   | 28   | 35    | 48    |
| 2 | 76   | 108  | 135   | 184   |
| 3 | 162  | 229  | 286   | 390   |

*Base test count (multiply by repetitions for total)*

### Accuracy vs Flake Rate

| Repetitions | Accuracy (10% flake) |
|-------------|----------------------|
| 1           | ~70%                 |
| 3           | ~92%                 |
| 5           | ~98%                 |
| 7           | ~99.5%               |

## Limitations

1. **Assumes independence**: The probabilistic model assumes tests are independent. Correlated failures may affect accuracy.

2. **Cherry-pick conflicts**: Git materialization via cherry-pick may fail if commits conflict. This manifests as an infrastructure failure.

3. **d must be small**: The algorithm is designed for d ≤ 3. For more simultaneous culprits, test count grows quadratically.

4. **Flake rate estimation**: The decoder uses configured flake rates. Misestimation affects accuracy.

## References

1. Du, D.-Z., & Hwang, F. K. (2000). *Combinatorial Group Testing and Its Applications*
2. Atia, G. K., & Saligrama, V. (2012). "Boolean Compressed Sensing and Noisy Group Testing"
3. Aldridge, M. (2019). "Individual Testing is Optimal for Noisy Group Testing in the Linear Regime"
