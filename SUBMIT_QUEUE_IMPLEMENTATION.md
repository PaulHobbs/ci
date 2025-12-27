# Submit Queue Implementation - 8x8 Matrix Group Testing

## Overview

This document describes the implementation of a CI submit queue using **non-adaptive group testing** with an 8x8 matrix design.

## Design Requirements

From the specification:
- 64 CLs arrive and are arranged in an 8x8 matrix
- 8 CLs are tested at a time (rows first)
- If a row passes, submit those 8 CLs immediately
- After all row tests, test columns for failed rows (culprit finding)
- Only test columns that had at least one failure
- Use the fluent API (`pkg/ci/dataflow`) when possible
- Use the wrapper (`pkg/ci`) for stage runner code

## Implementation Structure

### Files Created

1. **`cmd/ci/runners/submit_queue/main.go`** (673 lines)
   - All 7 runner implementations in one file
   - Runner types: kickoff, config, assignment, planning, test_executor, minibatch_finished, batch_finished

2. **`cmd/submit_queue/main.go`** (297 lines)
   - Client application demonstrating submit queue usage
   - Uses the DAG API from `pkg/ci`
   - Can run with embedded or remote orchestrator

3. **`cmd/submit_queue/README.md`**
   - User-facing documentation
   - Usage instructions and examples

4. **`run_submit_queue.sh`**
   - Helper script to build and run the submit queue
   - Supports embedded and distributed modes

## Architecture

### Stage Flow Diagram

```
┌─────────────────┐
│ Kickoff Stage   │ Receives 64 CLs
└────────┬────────┘
         │
         ↓
┌─────────────────┐
│ Config Stage    │ Creates matrix configuration
└────────┬────────┘
         │
         ↓
┌─────────────────┐
│ Assignment      │ Assigns CLs to rows/columns
│ Stage           │
└────────┬────────┘
         │
         ↓
┌─────────────────┐
│ Planning Stage  │ Creates row test stages
└────────┬────────┘
         │
         ↓
    ┌────┴────┐
    │ Row     │ × 8 (parallel)
    │ Tests   │ Each tests 8 CLs
    └────┬────┘
         │
         ↓
    ┌────┴────────┐
    │ Row         │ × 8
    │ Finished    │ Submits if passed
    └────┬────────┘
         │
         ↓
┌─────────────────┐
│ Batch Finished  │ Checks for failures
└────────┬────────┘
         │
         ↓ (if failures)
    ┌────┴────┐
    │ Column  │ × N (only failed columns)
    │ Tests   │
    └────┬────┘
         │
         ↓
    ┌────┴────────┐
    │ Column      │ × N
    │ Finished    │
    └────┬────────┘
         │
         ↓
┌─────────────────┐
│ Decode Finished │ Final culprit identification
└─────────────────┘
```

### Stage Responsibilities

#### 1. Kickoff Stage (`kickoff` runner)
- **Input**: 64 CLs from client
- **Validation**: Ensures exactly 64 CLs
- **Output**: Creates 3 checks and 3 stages:
  - `sq:config` check + `stage:sq:config` stage
  - `sq:assignment` check + `stage:sq:assignment` stage
  - `sq:planning` check + `stage:sq:planning` stage
- **API Used**: `common.NewCheck()`, `common.NewStage()`, `orchestrator.WriteNodes()`

#### 2. Config Stage (`config` runner)
- **Input**: Queries kickoff stage for CLs
- **Processing**: Stores matrix configuration
- **Output**: Config check with:
  - `matrix_size`: 8
  - `total_cls`: 64
  - `cls`: array of CL IDs
  - `timestamp`: creation time

#### 3. Assignment Stage (`assignment` runner)
- **Input**: Queries config check for CLs
- **Processing**: Creates row and column assignments
- **Output**: Assignment check with:
  - `rows`: 8 arrays of 8 CLs each (rows)
  - `columns`: 8 arrays of 8 CLs each (columns)

**Example assignments:**
```
rows[0] = [CL-1, CL-2, CL-3, CL-4, CL-5, CL-6, CL-7, CL-8]
rows[1] = [CL-9, CL-10, ..., CL-16]
...
columns[0] = [CL-1, CL-9, CL-17, CL-25, CL-33, CL-41, CL-49, CL-57]
columns[1] = [CL-2, CL-10, CL-18, ..., CL-58]
...
```

#### 4. Planning Stage (`planning` runner)
- **Input**: Queries assignment check for row assignments
- **Processing**: Creates execution stages for all 8 rows
- **Output**: For each row (0-7):
  - Row test check: `sq:row:N:test`
  - Row test stage: `stage:sq:row:N:test`
  - Row finished check: `sq:row:N:finished`
  - Row finished stage: `stage:sq:row:N:finished`
- **Also creates**:
  - Batch finished check: `sq:batch:finished`
  - Batch finished stage: `stage:sq:batch:finished`
- **Total nodes created**: 16 checks + 17 stages (8 test + 8 finished + 1 batch)

#### 5. Test Executor Stage (`test_executor` runner)
- **Input**: Row or column of CLs to test
- **Processing**: Executes tests on the minibatch
- **Simulation**: Currently simulates results:
  - Row 2 and Row 5 fail
  - Column 3 and Column 6 fail
- **Output**: Test result with:
  - `type`: "row" or "column"
  - `batch_id`: row/column number
  - `cls`: CLs tested
  - `passed`: true/false
  - `tested_at`: timestamp
- **Failure handling**: Uses `FailCheck()` if test fails

#### 6. Minibatch Finished Stage (`minibatch_finished` runner)
- **Input**: Test check result
- **Processing**:
  - Queries test check to see if it passed
  - If row test passed: logs CL submission
  - If test failed: marks for column testing
- **Output**: Finished check with:
  - `type`: "row" or "column"
  - `batch_id`: row/column number
  - `passed`: true/false
  - `action`: "submitted" or "marked_for_column_test"
  - `submitted_cls`: array (if submitted)

#### 7. Batch Finished Stage (`batch_finished` runner)

**First invocation (after row tests):**
- **Input**: All 8 row finished checks
- **Processing**:
  - Queries all row finished checks
  - Identifies which rows failed
  - If any row failed, identifies all columns needing tests
  - Creates column test stages
- **Output**:
  - If no failures: Finalizes with `all_passed: true`
  - If failures: Creates column test stages and decode check

**Second invocation (after column tests):**
- **Input**: All column finished checks
- **Processing**: Final decoding (identifies specific failed CLs)
- **Output**: Decode check with culprit information

### Data Flow Example

**Scenario**: Row 2 and Row 5 fail; Column 3 and Column 6 fail

1. **Kickoff**: Receives CL-1 through CL-64
2. **Config**: Stores matrix config
3. **Assignment**: Creates row/column assignments
4. **Planning**: Creates 8 row test stages
5. **Row Tests**:
   - Row 0: PASS → Submit CL-1 to CL-8
   - Row 1: PASS → Submit CL-9 to CL-16
   - Row 2: FAIL → Mark for column testing
   - Row 3: PASS → Submit CL-25 to CL-32
   - Row 4: PASS → Submit CL-33 to CL-40
   - Row 5: FAIL → Mark for column testing
   - Row 6: PASS → Submit CL-49 to CL-56
   - Row 7: PASS → Submit CL-57 to CL-64
6. **Batch Finished (1st)**: Detects failures, creates 8 column test stages
7. **Column Tests**:
   - Column 0: PASS
   - Column 1: PASS
   - Column 2: PASS
   - Column 3: FAIL
   - Column 4: PASS
   - Column 5: PASS
   - Column 6: FAIL
   - Column 7: PASS
8. **Decode**: Identifies culprits:
   - Row 2 ∩ Column 3 = CL-20 (position [2,3])
   - Row 2 ∩ Column 6 = CL-23 (position [2,6])
   - Row 5 ∩ Column 3 = CL-44 (position [5,3])
   - Row 5 ∩ Column 6 = CL-47 (position [5,6])

## API Usage

### Client Code (DAG API)

```go
dag := ci.NewDAG()

// Create kickoff check and stage
kickoffCheck := dag.Check("sq:kickoff").Kind("sq_kickoff")
dag.Stage("stage:sq:kickoff").
    Runner("kickoff").
    Sync().
    Arg("cls", cls).  // 64 CLs
    Assigns(kickoffCheck)

// Execute
exec, err := dag.Execute(ctx, orchestrator)
```

### Runner Code (WriteNodes API)

```go
// Create checks and stages
checks := []*pb.CheckWrite{
    common.NewCheck("check:id").Kind("kind").State(PLANNED).Build(),
}

stages := []*pb.StageWrite{
    common.NewStage("stage:id").
        RunnerType("runner_type").
        Sync().
        Args(map[string]any{"key": "value"}).
        Assigns("check:id").
        DependsOnStages("stage:dep").
        Build(),
}

// Write to orchestrator
_, err = orchestrator.WriteNodes(ctx, &pb.WriteNodesRequest{
    WorkPlanId: workPlanId,
    Checks:     checks,
    Stages:     stages,
})
```

## Key Design Decisions

### 1. Single Runner Binary

All 7 runner types are in one binary (`cmd/ci/runners/submit_queue/main.go`) with the `--type` flag selecting which runner to instantiate. This:
- Simplifies deployment
- Shares common code
- Reduces build complexity

**Alternative considered**: Separate binary per runner (more distributed but more complex)

### 2. Dynamic Stage Creation

The workflow uses a **materialize pattern** where stages dynamically create subsequent stages:
- Kickoff creates config/assignment/planning stages
- Planning creates row test stages
- Batch-finished creates column test stages (if needed)

This allows the workflow to adapt based on test results (e.g., only create column tests if needed).

### 3. Two-Level Batch Finished

The batch-finished runner is called twice:
1. After row tests: Creates column tests if needed
2. After column tests: Performs final decoding

This is implemented by having batch-finished create a decode check that depends on column finished checks.

### 4. Check Dependencies

The implementation uses check dependencies to enforce ordering:
```
sq:config
    ↓
sq:assignment (depends on sq:config)
    ↓
sq:planning (depends on sq:assignment)
    ↓
sq:batch:finished (depends on all sq:row:N:finished)
    ↓
sq:decode:finished (depends on all sq:col:N:finished)
```

### 5. Submission in Minibatch-Finished

CL submission happens in the minibatch-finished stage rather than the test executor stage because:
- Separates concerns (testing vs. submission)
- Allows for retry logic
- Enables auditing/logging of submissions

## Performance Characteristics

### Test Execution Count

- **All rows pass**: 8 tests
- **One row fails**: 8 rows + 8 columns = 16 tests
- **Multiple rows fail**: 8 rows + 8 columns = 16 tests (same)
- **Compared to sequential**: 64 tests

**Speedup**: 4x to 8x depending on failure rate

### Parallelism

- **Row tests**: All 8 run in parallel
- **Column tests**: All 8 run in parallel (if needed)
- **Total parallel batches**: 2 (rows, then columns)

### Latency

- **Best case**: 1 batch (rows only)
- **Worst case**: 2 batches (rows + columns)

## Extension Points

### 1. Custom Test Execution

Replace `simulateTest()` in test_executor:

```go
func executeRealTests(testType string, cls []string) (bool, error) {
    // Build all CLs together
    // Run test suite
    // Return true if all pass
}
```

### 2. Real CL Submission

Replace submission logging in minibatch_finished:

```go
func submitCLs(cls []string) error {
    for _, cl := range cls {
        // Call Gerrit/GitHub API to submit
    }
    return nil
}
```

### 3. Adaptive Group Testing

To make it adaptive:
1. Modify planning to create variable-sized minibatches
2. Add intermediate decoding stages
3. Create additional test rounds based on results

### 4. Different Matrix Sizes

To support NxN matrices:
1. Parameterize matrix size in config stage
2. Update planning stage to create N row tests
3. Update batch-finished to create N column tests

## Testing the Implementation

### Unit Testing

Test each runner independently:

```go
func TestKickoffRunner(t *testing.T) {
    runner := NewKickoffRunner()

    req := &pb.RunRequest{
        WorkPlanId: "test-wp",
        Args: /* 64 CLs */,
    }

    resp, err := runner.HandleRun(ctx, req)
    // Assert response
}
```

### Integration Testing

Run full workflow:

```bash
./run_submit_queue.sh embedded --verbose
```

Expected output:
- 8 row tests complete
- Rows 2 and 5 fail (simulated)
- 8 column tests created and run
- Columns 3 and 6 fail (simulated)
- Culprits identified: CL-20, CL-23, CL-44, CL-47

## Troubleshooting

### Check Work Plan Status

```go
exec.GetCheckResult(ctx, "sq:batch:finished")
```

### Query All Checks

```go
orchestrator.QueryNodes(ctx, &pb.QueryNodesRequest{
    WorkPlanId: workPlanId,
    IncludeChecks: true,
})
```

### Enable Verbose Logging

Run with `--verbose` flag to see detailed execution logs.

## Future Improvements

1. **Retry Logic**: Add automatic retry for transient test failures
2. **Priority Queuing**: Process high-priority CLs first
3. **Dynamic Batch Sizing**: Adjust batch size based on failure rate
4. **Metrics Collection**: Track test execution time, failure rates, etc.
5. **Partial Submission**: Submit passed CLs from failed rows after column tests
6. **Multi-Level Testing**: Add more than 2 levels of group testing
7. **Incremental Results**: Stream results as rows complete

## References

- Group Testing: https://en.wikipedia.org/wiki/Group_testing
- TurboCI-Lite API: `/workspace/pkg/ci/`
- Runner Examples: `/workspace/cmd/ci/runners/`
