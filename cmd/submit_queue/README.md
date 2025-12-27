# Submit Queue with 8x8 Matrix Group Testing

This implements a CI submit queue using non-adaptive group testing with an 8x8 matrix design.

## Overview

The submit queue processes 64 CLs (Change Lists) using a two-phase group testing approach:

1. **Phase 1 - Row Tests**: Test 8 rows (8 CLs each)
   - If a row passes, submit those 8 CLs immediately
   - If a row fails, mark its columns for testing

2. **Phase 2 - Column Tests**: Test columns that had failures
   - Only test columns where at least one row failed
   - Use row + column results to identify specific failed CLs (culprit finding)

## Architecture

### Stage Workflow

```
Kickoff Stage
    ↓
Config Stage (creates matrix configuration)
    ↓
Assignment Stage (assigns CLs to rows/columns)
    ↓
Planning Stage (creates execution stages)
    ↓
    ├─→ Row 0 Test → Row 0 Finished ──┐
    ├─→ Row 1 Test → Row 1 Finished ──┤
    ├─→ Row 2 Test → Row 2 Finished ──┤
    ├─→ Row 3 Test → Row 3 Finished ──┤
    ├─→ Row 4 Test → Row 4 Finished ──┼─→ Batch Finished
    ├─→ Row 5 Test → Row 5 Finished ──┤      ↓
    ├─→ Row 6 Test → Row 6 Finished ──┤   (If failures)
    └─→ Row 7 Test → Row 7 Finished ──┘      ↓
                                        ┌─────────────┐
                                        │ Create      │
                                        │ Column      │
                                        │ Tests       │
                                        └─────────────┘
                                              ↓
                    ┌─→ Col N Test → Col N Finished ──┐
                    ├─→ Col M Test → Col M Finished ──┼─→ Decode Finished
                    └─→ ...                           │    (Final Results)
```

### Runner Types

The implementation includes 7 runner types, all in `cmd/ci/runners/submit_queue/main.go`:

1. **kickoff** - Receives 64 CLs and creates initial workflow stages
2. **config** - Creates matrix configuration
3. **assignment** - Assigns CLs to row/column minibatches
4. **planning** - Creates row test execution stages
5. **test_executor** - Executes tests for a minibatch (row or column)
6. **minibatch_finished** - Handles minibatch completion and CL submission
7. **batch_finished** - Creates column tests and performs final decoding

## Usage

### 1. Start the Orchestrator

First, start the orchestrator and required runners:

```bash
# Start orchestrator
./bin/orchestrator --port 50051

# Start all submit queue runners (in separate terminals)
./bin/submit_queue_runner --type kickoff --listen :50070
./bin/submit_queue_runner --type config --listen :50071
./bin/submit_queue_runner --type assignment --listen :50072
./bin/submit_queue_runner --type planning --listen :50073
./bin/submit_queue_runner --type test_executor --listen :50074 --runner-id test-1
./bin/submit_queue_runner --type test_executor --listen :50075 --runner-id test-2
./bin/submit_queue_runner --type minibatch_finished --listen :50076
./bin/submit_queue_runner --type batch_finished --listen :50077
```

### 2. Run the Submit Queue Client

```bash
# Using embedded orchestrator (easiest for testing)
./bin/submit_queue --cls 64

# Or connect to existing orchestrator
./bin/submit_queue --orchestrator localhost:50051 --cls 64

# With verbose output
./bin/submit_queue --verbose
```

### 3. Build Commands

```bash
# Build the runner
go build -o bin/submit_queue_runner ./cmd/ci/runners/submit_queue

# Build the client
go build -o bin/submit_queue ./cmd/submit_queue
```

## Implementation Details

### Matrix Layout

CLs are arranged in an 8x8 matrix:

```
        Col 0   Col 1   Col 2   Col 3   Col 4   Col 5   Col 6   Col 7
Row 0:  CL-1    CL-2    CL-3    CL-4    CL-5    CL-6    CL-7    CL-8
Row 1:  CL-9    CL-10   CL-11   CL-12   CL-13   CL-14   CL-15   CL-16
Row 2:  CL-17   CL-18   CL-19   CL-20   CL-21   CL-22   CL-23   CL-24
Row 3:  CL-25   CL-26   CL-27   CL-28   CL-29   CL-30   CL-31   CL-32
Row 4:  CL-33   CL-34   CL-35   CL-36   CL-37   CL-38   CL-39   CL-40
Row 5:  CL-41   CL-42   CL-43   CL-44   CL-45   CL-46   CL-47   CL-48
Row 6:  CL-49   CL-50   CL-51   CL-52   CL-53   CL-54   CL-55   CL-56
Row 7:  CL-57   CL-58   CL-59   CL-60   CL-61   CL-62   CL-63   CL-64
```

### Test Simulation

The current implementation simulates test results for demonstration:
- Row 2 and Row 5 fail
- Column 3 and Column 6 fail
- This identifies culprits: CL-20, CL-24, CL-44, CL-48

To customize test logic, modify the `simulateTest()` function in `cmd/ci/runners/submit_queue/main.go`.

### Submission Logic

When a row test passes:
1. The minibatch-finished stage extracts CLs from the test result
2. Marks them as submitted (logged)
3. In a real implementation, this would integrate with your VCS/CI system

### Culprit Finding

When failures occur:
1. Batch-finished stage identifies which rows failed
2. Creates column test stages for ALL columns (since any column could contain a culprit)
3. Column tests run
4. Culprits are identified at the intersection of failed rows and failed columns

For example:
- If Row 2 fails and Column 3 fails
- The culprit is CL-20 (at position [2,3])

## API Usage

The client demonstrates the DAG (Declarative) API pattern:

```go
// Build workflow using DAG API
dag := ci.NewDAG()

// Create kickoff check and stage
kickoffCheck := dag.Check("sq:kickoff").Kind("sq_kickoff")
dag.Stage("stage:sq:kickoff").
    Runner("kickoff").
    Sync().
    Arg("cls", cls).  // Pass 64 CLs
    Assigns(kickoffCheck)

// Execute the DAG
exec, err := dag.Execute(ctx, orchestrator)
```

The kickoff stage then dynamically creates all subsequent stages using the orchestrator's `WriteNodes` API.

## Extension Points

### Custom Test Execution

Replace `simulateTest()` in `test_executor` runner with real test logic:

```go
func executeRealTests(cls []string) (bool, error) {
    // Run actual builds/tests for the CLs
    // Return true if all pass, false if any fail
}
```

### Custom Submission

Replace the submission logic in `minibatch_finished` runner:

```go
func submitCLs(cls []string) error {
    // Integrate with your VCS system
    // e.g., call Gerrit API, GitHub API, etc.
}
```

### Adaptive Group Testing

To implement adaptive group testing (dynamic minibatch sizing):
1. Modify `planning` stage to create variable-sized minibatches
2. Modify `batch_finished` to create second-level tests based on first-level results
3. Add additional decoding stages

## Performance

For 64 CLs with the 8x8 matrix:
- **Best case**: 8 row tests (all pass) = 8 test executions
- **Worst case**: 8 row tests + 8 column tests = 16 test executions
- **Compared to sequential**: 64 test executions

This provides ~4-8x speedup for the submit queue.

## References

- **Non-adaptive Group Testing**: Fixed test design determined upfront
- **Adaptive Group Testing**: Test design changes based on intermediate results
- **Matrix Design**: Rows and columns provide two independent groupings
- **Culprit Finding**: Intersection of failed groups identifies specific failures
