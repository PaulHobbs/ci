# culprit-finder

A command-line tool for finding culprit commits using flake-aware non-adaptive group testing.

## Overview

`culprit-finder` helps you identify which commit(s) introduced a test failure. It's similar to `git bisect`, but with key advantages:

- **Finds multiple culprits**: Can identify up to `d` simultaneous breaking commits
- **Handles flaky tests**: Built-in robustness through repetitions and probabilistic decoding
- **Efficient**: Uses logarithmic number of tests (vs linear for bisect)
- **Parallel execution**: Runs all tests in parallel for maximum speed
- **Resumable**: Can pause and resume searches

## Installation

```bash
cd cmd/culprit-finder
go build -o culprit-finder
sudo mv culprit-finder /usr/local/bin/
```

Or run directly:
```bash
go run . --help
```

## Quick Start

The workflow is similar to `git bisect`:

```bash
# 1. Start a search from a known-good commit to a bad one
culprit-finder start v1.0 HEAD -- make test

# 2. Run the search (this may take a while)
culprit-finder run

# 3. Review results
culprit-finder status

# 4. Clean up when done
culprit-finder reset
```

## Usage

### Starting a Search

```bash
culprit-finder start <good-ref> <bad-ref> -- <test-command>
```

**Examples:**

```bash
# Basic usage
culprit-finder start main HEAD -- go test ./...

# With custom test timeout
culprit-finder start --timeout 15m v2.0 HEAD -- npm test

# For flaky tests
culprit-finder start --repetitions 5 --flake-rate 0.15 main HEAD -- pytest

# Expect multiple culprits
culprit-finder start --max-culprits 3 v1.0 HEAD -- cargo test

# Interactive mode (prompts for settings)
culprit-finder start -i main HEAD -- make test
```

**Options:**

- `--max-culprits <n>` - Maximum expected culprits (default: 2)
- `--repetitions <r>` - Test repetitions for flake robustness (default: 3)
- `--confidence <c>` - Minimum confidence threshold 0-1 (default: 0.8)
- `--flake-rate <f>` - Expected test flake rate 0-1 (default: 0.05)
- `--timeout <d>` - Test command timeout (default: 10m)
- `--seed <s>` - Random seed for reproducibility
- `-i, --interactive` - Interactive configuration wizard

### Running the Search

```bash
culprit-finder run
```

This executes the search, creating git worktrees and running tests in parallel.

**Features:**
- Progress bar shows test completion
- Automatically saves progress
- Can be interrupted with Ctrl+C and resumed
- Results displayed when complete

**Options:**
- `--progress` - Show progress updates (default: true)
- `--continue-on-error` - Continue even if some tests fail to execute

### Checking Status

```bash
culprit-finder status
```

Shows current progress, configuration, and results (if complete).

**Options:**
- `-v, --verbose` - Show detailed information including commit list

### Viewing Configuration

```bash
culprit-finder config
```

Displays all configuration parameters and estimated test count.

### Listing Commits

```bash
culprit-finder list
```

Shows all commits in the search range, highlighting any identified culprits.

**Options:**
- `-n, --limit <n>` - Limit number of commits shown (0 = all)

### Resetting

```bash
culprit-finder reset
```

Cleans up the session and all data. Use this to start fresh or after completion.

**Options:**
- `-f, --force` - Skip confirmation prompt

## How It Works

### Algorithm

The tool uses **d-disjunct matrices** for non-adaptive group testing:

1. **Matrix Construction**: Creates a test matrix where each column represents a commit and each row represents a test group
2. **Materialization**: For each test group, creates a git worktree with the specified commits cherry-picked
3. **Test Execution**: Runs the test command in parallel across all worktrees
4. **Repetition**: Repeats each test group multiple times to handle flakes
5. **Majority Voting**: Determines the "true" outcome from repeated results
6. **Decoding**: Uses log-likelihood scoring to identify culprits

### Test Count

The number of tests is approximately:

```
tests ≈ 4 × d² × log₂(n) × r
```

Where:
- `d` = max-culprits
- `n` = number of commits
- `r` = repetitions

**Examples:**

| Commits | max-culprits | repetitions | Total Tests |
|---------|--------------|-------------|-------------|
| 20      | 1            | 3           | ~60         |
| 50      | 2            | 3           | ~320        |
| 100     | 2            | 3           | ~400        |
| 500     | 2            | 5           | ~920        |

### Parallelization

All test groups run in parallel. Wall-clock time is:

```
time ≈ max(test_duration) × repetitions
```

Not `sum(all_test_durations)`.

## Configuration Guide

### For Stable Tests

```bash
culprit-finder start \
  --max-culprits 1 \
  --repetitions 1 \
  main HEAD -- make test
```

Fastest option when you know tests are stable.

### For Flaky Tests

```bash
culprit-finder start \
  --repetitions 7 \
  --flake-rate 0.15 \
  main HEAD -- npm test
```

More repetitions and appropriate flake rate improve robustness.

### For Large Commit Ranges

```bash
culprit-finder start \
  --max-culprits 2 \
  --repetitions 5 \
  v1.0 HEAD -- go test ./...
```

More repetitions ensure accuracy across many commits.

### For Multiple Expected Culprits

```bash
culprit-finder start \
  --max-culprits 3 \
  --repetitions 3 \
  main HEAD -- cargo test
```

Higher `max-culprits` allows finding more simultaneous breakages.

## Understanding Results

### Confidence Levels

| Confidence | Meaning |
|------------|---------|
| > 95%      | Very high confidence, almost certainly a culprit |
| 80-95%     | High confidence, likely a culprit (default threshold) |
| 60-80%     | Moderate confidence, probably a culprit |
| 40-60%     | Ambiguous, cannot determine |
| < 40%      | Probably not a culprit |

### Result Categories

- **Identified Culprits**: Commits above the confidence threshold that likely introduced the failure
- **Innocent Commits**: Commits proven not to be culprits
- **Ambiguous Commits**: Commits that couldn't be determined (usually due to insufficient data or high flake rate)

### Example Output

```
====================
  Results
====================

  Time elapsed: 5m 32s
  Tests run: 384
  Overall confidence: 0.92

✓ Found 2 culprit(s):

1. CULPRIT
  Commit:     abc123def456
  Subject:    Refactor authentication logic
  Author:     alice@example.com
  Confidence: 94.2%
  Score:      8.45

2. CULPRIT
  Commit:     def456789abc
  Subject:    Update database schema
  Author:     bob@example.com
  Confidence: 87.3%
  Score:      6.12

  Review these commits for the introduced failure.

✓ 45 commits cleared (not culprits)
⚠ 3 commits ambiguous (could not determine)
```

## Tips and Best Practices

### 1. Verify Your Test Command

Before starting, make sure your test command:
- Exits with code 0 on pass
- Exits with non-zero on fail
- Runs the failing test consistently

Test it manually:
```bash
cd /your/repo
make test  # or whatever your command is
echo $?    # should be non-zero if test fails
```

### 2. Choose the Right Configuration

- **Stable test, single culprit expected**: `--max-culprits 1 --repetitions 1`
- **Slightly flaky test**: Use default settings
- **Very flaky test**: `--repetitions 7 --flake-rate 0.2`
- **Multiple culprits expected**: `--max-culprits 3`

### 3. Monitor Progress

The search can take a while for large ranges. Use:
```bash
culprit-finder status
```

in another terminal to check progress.

### 4. Interrupt and Resume

You can safely interrupt (Ctrl+C) and resume:
```bash
culprit-finder run  # start
^C                   # interrupt
culprit-finder run  # resume from where you left off
```

### 5. Check Results Carefully

- Review commits with confidence > 80%
- Be cautious with commits in 60-80% range
- Consider re-running with more repetitions if many commits are ambiguous

### 6. Optimize for Your Use Case

**Fast Iteration (Development)**:
```bash
--max-culprits 1 --repetitions 1
```

**Production/CI (Accuracy)**:
```bash
--max-culprits 2 --repetitions 5 --confidence 0.85
```

## Session Management

### Session Storage

Sessions are stored in `.culprit-finder/` in your repository:
```
your-repo/
  .culprit-finder/
    session.json      # Session metadata
    culprit.db        # SQLite database with results
```

**Add to .gitignore:**
```bash
echo ".culprit-finder/" >> .gitignore
```

### Multiple Repositories

Each repository has its own independent session. You can run searches in different repos simultaneously.

## Troubleshooting

### "No active session found"

Start a new search with `culprit-finder start`.

### "Session already active"

Either continue with `culprit-finder run` or reset with `culprit-finder reset --force`.

### "Test command failed"

Make sure:
1. The test command is correct
2. The command runs successfully in the good-ref
3. The command fails in the bad-ref
4. Test timeout is sufficient

### "No culprits identified"

Possible causes:
1. Test is too flaky - increase `--repetitions` and `--flake-rate`
2. Failure not in this commit range - check your refs
3. Test command issues - verify it works correctly

### "Too many ambiguous commits"

Increase `--repetitions` for better accuracy:
```bash
culprit-finder reset
culprit-finder start --repetitions 7 ... -- <test-command>
culprit-finder run
```

## Comparison with git bisect

| Feature | culprit-finder | git bisect |
|---------|---------------|------------|
| Multiple culprits | ✅ Yes | ❌ No (finds first only) |
| Flake handling | ✅ Built-in | ❌ Manual |
| Test count | O(log n) | O(log n) |
| Parallel execution | ✅ Yes | ❌ No |
| Resumable | ✅ Yes | ✅ Yes |
| Interactive | ✅ Automatic | ✅ Manual steps |

Use **culprit-finder** when:
- You suspect multiple commits might be causing failures
- Your tests are flaky
- You want faster results via parallelization
- You want automatic execution

Use **git bisect** when:
- You need manual inspection at each step
- The failure requires human judgment
- You're comfortable with the manual process

## Advanced Usage

### Custom Worktree Directory

```bash
culprit-finder start --worktree-dir /fast/ssd/path main HEAD -- make test
```

### Reproducible Results

```bash
culprit-finder start --seed 42 main HEAD -- make test
```

### Very Large Ranges

For 500+ commits, consider:
```bash
culprit-finder start \
  --max-culprits 2 \
  --repetitions 5 \
  --timeout 20m \
  v1.0 HEAD -- <test-command>
```

## Contributing

See the main [culprit README](../../culprit/README.md) for details on the algorithm and implementation.

## License

Part of the turboci-lite project.
