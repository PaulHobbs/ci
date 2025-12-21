# Culprit Finder CLI - Design Document

## Overview

The `culprit-finder` CLI is a user-friendly command-line wrapper around the culprit finder library, designed to make finding breaking commits as easy as using `git bisect`.

## Design Goals

### 1. Excellent Developer Experience

**Git-like Workflow**: Familiar commands similar to `git bisect`:
- `start` - Initialize a search (like `git bisect start`)
- `run` - Execute the search (automated, unlike bisect's manual steps)
- `status` - Check progress (like `git bisect log`)
- `reset` - Clean up (like `git bisect reset`)

**Session Persistence**: All state saved to disk in `.culprit-finder/`:
- Can interrupt and resume at any time
- Survives terminal crashes
- Multiple repos can have independent sessions

**Rich Visual Feedback**:
- Color-coded output with emoji indicators
- Progress bars for long-running operations
- Clear status indicators (✓, ✗, ⚠, ▶)
- Formatted commit displays with highlighting

### 2. Ease of Use

**Minimal Required Arguments**:
```bash
culprit-finder start <good-ref> <bad-ref> -- <test-command>
```

**Sensible Defaults**:
- `max-culprits`: 2 (handle most cases)
- `repetitions`: 3 (good flake robustness)
- `confidence`: 0.8 (high quality results)
- `flake-rate`: 0.05 (typical test flakiness)

**Interactive Mode**: Wizard that asks questions in plain English:
```bash
culprit-finder start -i main HEAD -- make test
# Prompts: "How flaky are your tests?" with options
```

**Helpful Error Messages**:
- Suggests next steps
- Explains what went wrong
- Provides examples of correct usage

### 3. Robustness

**Graceful Interruption**:
- Ctrl+C saves progress and exits cleanly
- Cleanup of git worktrees even on error
- Can resume from where it left off

**Error Recovery**:
- Continue-on-error flag for partial failures
- Clear distinction between test failures and infrastructure failures
- Validation of inputs before starting expensive operations

**Resource Management**:
- Automatic cleanup of temporary worktrees
- Database connection pooling
- Parallel test execution with proper resource limits

## Architecture

### Component Structure

```
cmd/culprit-finder/
├── main.go                          # Entry point
├── internal/
│   ├── cli/                         # Command implementations
│   │   ├── root.go                  # Root command & help
│   │   ├── start.go                 # Initialize search
│   │   ├── run.go                   # Execute search
│   │   ├── status.go                # Show progress
│   │   ├── reset.go                 # Clean up
│   │   ├── config.go                # Show configuration
│   │   ├── list.go                  # List commits
│   │   └── version.go               # Version info
│   ├── session/                     # Session management
│   │   └── session.go               # Persistence layer
│   └── ui/                          # User interface
│       └── ui.go                    # Display helpers
├── README.md                        # User documentation
├── DESIGN.md                        # This file
├── example.sh                       # Usage examples
├── Makefile                         # Build automation
└── .gitignore                       # Ignore patterns
```

### Session Management

**Session File** (`.culprit-finder/session.json`):
```json
{
  "id": "session-1234567890",
  "repository": "/path/to/repo",
  "good_ref": "v1.0",
  "bad_ref": "HEAD",
  "test_command": "make test",
  "test_timeout": "10m",
  "config": {
    "max_culprits": 2,
    "repetitions": 3,
    "confidence_threshold": 0.8,
    "false_positive_rate": 0.05,
    "false_negative_rate": 0.05
  },
  "commits": [...],
  "status": "running",
  "total_tests": 384,
  "completed_tests": 156
}
```

**Database** (`.culprit-finder/culprit.db`):
- SQLite database managed by orchestrator
- Stores work plan, stages, checks, results
- Enables resume capability

### Command Flow

#### Start Command

1. **Validate Environment**:
   - Check we're in a git repository
   - Verify no existing session
   - Validate git refs exist

2. **Get Commit Range**:
   - Run `git log good-ref..bad-ref`
   - Parse commits into structured data
   - Validate minimum commit count (≥2)

3. **Interactive Configuration** (if `-i`):
   - Ask about test flakiness
   - Ask about expected culprit count
   - Set appropriate defaults

4. **Create Session**:
   - Generate session ID
   - Calculate estimated test count
   - Save session file
   - Create database

5. **Display Summary**:
   - Show configuration
   - Show estimated tests
   - Provide next steps

#### Run Command

1. **Load Session**:
   - Read session file
   - Validate status (can resume if interrupted)
   - Initialize database connection

2. **Setup Components**:
   - Create git materializer
   - Create test runner
   - Create culprit finder

3. **Execute Search**:
   - Start or resume work plan
   - Run tests in parallel
   - Show progress bar
   - Save progress continuously

4. **Handle Interruption**:
   - Catch SIGINT/SIGTERM
   - Save current state
   - Clean up resources
   - Allow resume later

5. **Display Results**:
   - Show identified culprits with confidence
   - List innocent commits
   - Note ambiguous commits
   - Provide recommendations

#### Status Command

1. **Load Session**
2. **Display Information**:
   - Session metadata
   - Configuration
   - Progress (if running)
   - Results (if complete)
   - Next steps

#### Reset Command

1. **Confirm** (unless `--force`)
2. **Clean Up**:
   - Remove session directory
   - Delete database
   - Remove any temporary files
3. **Provide Next Steps**

### User Interface Design

#### Color Scheme

- **Blue**: Headers, section titles
- **Green**: Success, checkmarks
- **Red**: Errors, failures, culprits
- **Yellow**: Warnings, highlights
- **Purple**: Questions, prompts
- **Cyan**: Progress indicators
- **Gray**: Metadata, secondary info

#### Output Patterns

**Header**:
```
====================
  Section Title
====================
```

**Step Progress**:
```
▶ Performing action...
✓ Action complete
✗ Action failed
⚠ Warning message
```

**Progress Bar**:
```
Tests: [████████████░░░░░░░░] 156/384 (40.6%)
```

**Commit Display**:
```
  abc12345 Fix authentication bug (alice@example.com)
  def67890 Update database schema (bob@example.com)
```

**Culprit Display**:
```
1. CULPRIT
  Commit:     abc123def456
  Subject:    Refactor authentication logic
  Author:     alice@example.com
  Confidence: 94.2%
  Score:      8.45
```

## User Experience Enhancements

### 1. Automatic Detection

- Git repository root auto-detected
- Test command validated before starting
- Sensible defaults for all parameters

### 2. Progressive Disclosure

- Basic usage is simple: `start` then `run`
- Advanced options available via flags
- Help text progressive: overview → details → examples

### 3. Helpful Guidance

**When stuck**: Next steps always provided:
```
Next Steps:
  Run 'culprit-finder run' to start the search
```

**When errors occur**: Actionable suggestions:
```
Error: No culprits identified
Possible reasons:
  - The test might be flaky (try increasing --repetitions)
  - The failure might not be in this commit range
```

### 4. Safety Features

**Confirmation Prompts**:
- Reset command asks for confirmation
- Override with `--force` flag

**Non-Destructive**:
- Uses git worktrees (doesn't modify working directory)
- All changes isolated to `.culprit-finder/`
- Easy to add to `.gitignore`

**Resumable**:
- Safe to interrupt at any time
- Progress saved continuously
- Clean exit on Ctrl+C

## Advanced Features

### Interactive Mode

Wizard-style configuration for non-experts:

```
Interactive Configuration

This wizard will help you configure the culprit finder.

? How flaky are your tests?
  1. Very stable (< 1% flake rate)
  2. Mostly stable (~ 5% flake rate) [default]
  3. Somewhat flaky (~ 15% flake rate)
  4. Very flaky (> 25% flake rate)

Choice [1-4]: 3

? How many culprits do you expect?
  1. Single culprit (faster)
  2. Up to 2 culprits [default]
  3. Up to 3 culprits (slower)

Choice [1-3]: 2

✓ Configuration: 5 reps, 15% flake rate, max 2 culprits
```

### Session Management

**Independent Sessions**: Each repo has its own session:
```bash
cd repo-a
culprit-finder start ...  # Session for repo-a

cd ../repo-b
culprit-finder start ...  # Different session for repo-b
```

**Resume Capability**: Robust state management:
```bash
culprit-finder run
# Interrupt with Ctrl+C
# Later:
culprit-finder run  # Resumes from where it left off
```

### Rich Output

**Progress Tracking**:
- Real-time progress bar
- Estimated time remaining
- Tests completed / total

**Detailed Results**:
- Confidence scores
- Evidence from test groups
- Innocent vs ambiguous commits

**Verbose Mode**:
```bash
culprit-finder status -v  # Show full commit list
culprit-finder list       # Show all commits in range
```

## Integration Points

### Git Integration

- Uses standard git commands
- Works with any git ref (branches, tags, SHAs)
- Creates temporary worktrees for isolation

### Test Command Integration

- Any command that exits 0 (pass) or non-zero (fail)
- Configurable timeout
- Environment variable support (future)

### CI/CD Integration

Non-interactive usage for automation:
```bash
culprit-finder start \
  --max-culprits 2 \
  --repetitions 5 \
  --timeout 15m \
  v1.0 HEAD -- make ci-test

culprit-finder run --progress=false

# Exit code 0 if culprits found, 1 otherwise
```

## Performance Considerations

### Parallelization

- All test groups run in parallel
- Wall-clock time = test duration × repetitions
- Not sum of all tests

### Resource Usage

- Database is lightweight SQLite
- Worktrees use hard links where possible
- Automatic cleanup on exit

### Scalability

- Tested with 500+ commit ranges
- Logarithmic test count growth
- Progress saved incrementally

## Future Enhancements

### Potential Features

1. **Watch Mode**: Auto-detect new commits and re-run
2. **Remote Execution**: Distribute tests across machines
3. **Result Caching**: Skip tests for known-good commit combinations
4. **Integration**: Native CI/CD platform support
5. **Visualization**: Web UI for viewing results
6. **Export**: JSON/CSV output for tooling integration
7. **Bisect Mode**: Fall back to bisect-like behavior for certain cases
8. **Auto-revert**: Optionally create revert commits

### Extensibility

- Custom materializers for non-git VCS
- Custom test runners for remote execution
- Plugin system for result processing
- Webhook notifications on completion

## Comparison Matrix

| Feature | culprit-finder | git bisect | TurboCI |
|---------|---------------|------------|---------|
| Multiple culprits | ✅ | ❌ | ✅ |
| Flake handling | ✅ | ❌ | ✅ |
| Parallel tests | ✅ | ❌ | ✅ |
| Automatic | ✅ | ❌ | ✅ |
| Interactive | ✅ | ✅ | ❌ |
| CLI | ✅ | ✅ | Partial |
| Resumable | ✅ | ✅ | ✅ |
| Simple setup | ✅ | ✅ | ❌ |

## Testing Strategy

### Unit Tests

- Session persistence
- Git integration helpers
- UI formatting functions
- Configuration validation

### Integration Tests

- Full workflow tests with fake git repos
- Error handling scenarios
- Interruption and resume
- Multiple session scenarios

### Manual Testing

- Real git repositories
- Various flake rates
- Different commit range sizes
- Edge cases (no culprits, all culprits, etc.)

## Documentation

### User Documentation

- **README.md**: Quick start, usage guide, troubleshooting
- **example.sh**: Runnable examples for common scenarios
- **--help**: Comprehensive help text for each command

### Developer Documentation

- **DESIGN.md**: This file - architecture and design decisions
- **Code comments**: Implementation details
- **Algorithm docs**: Reference to main culprit finder docs

## Success Metrics

### User Experience

- Time to first result < 5 minutes for small ranges
- Zero-config works for 80% of use cases
- Error messages lead to resolution in 1-2 steps

### Reliability

- Handles Ctrl+C gracefully 100% of the time
- Resume works across sessions
- No leaked resources (worktrees, processes)

### Performance

- CLI overhead < 1 second
- Parallel execution close to theoretical maximum
- Memory usage scales linearly with commit count

## Conclusion

The `culprit-finder` CLI transforms the sophisticated culprit finding algorithm into a tool that's as easy to use as `git bisect`, while being more powerful and robust. The focus on developer experience, sensible defaults, and helpful guidance makes it accessible to developers of all skill levels.
