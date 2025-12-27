# Submit Queue Testing Documentation

## Overview

This document describes the testing approach for the submit queue implementation with 8x8 matrix group testing.

## Test Files

### Integration Tests

**Location**: `/workspace/cmd/ci/runners/submit_queue/submit_queue_test.go`

**Test Coverage**:

1. **TestSubmitQueueIntegration** - Full end-to-end workflow test
   - Sets up embedded orchestrator
   - Starts all 7 runner types
   - Executes complete workflow with 64 CLs
   - Verifies final results match expected pattern

2. **TestKickoffRunner** - Unit test for kickoff runner
   - Tests CL processing
   - Verifies creation of config/assignment/planning stages
   - Validates check updates

3. **TestConfigRunner** - Unit test for config runner
   - Tests matrix configuration creation
   - Verifies config data structure

4. **TestTestExecutorRunner** - Tests test execution simulation
   - Verifies row test results (rows 2 and 5 fail)
   - Verifies column test results (columns 3 and 6 fail)
   - Tests both passing and failing scenarios

## Running Tests

### Run All Tests

```bash
cd /workspace
go test -v ./cmd/ci/runners/submit_queue/...
```

### Run Specific Test

```bash
go test -v ./cmd/ci/runners/submit_queue/ -run TestSubmitQueueIntegration
go test -v ./cmd/ci/runners/submit_queue/ -run TestKickoffRunner
go test -v ./cmd/ci/runners/submit_queue/ -run TestConfigRunner
go test -v ./cmd/ci/runners/submit_queue/ -run TestTestExecutorRunner
```

### Run With Race Detection

```bash
go test -race -v ./cmd/ci/runners/submit_queue/...
```

### Run With Coverage

```bash
go test -cover -v ./cmd/ci/runners/submit_queue/...
go test -coverprofile=coverage.out ./cmd/ci/runners/submit_queue/...
go tool cover -html=coverage.out
```

## Test Architecture

### Test Helper Functions

#### setupEmbeddedOrchestrator
Creates a full embedded orchestrator with:
- SQLite storage (temporary database)
- Orchestrator service
- Runner service
- Callback service
- Dispatcher with gRPC client factory
- gRPC server on port 50051

#### setupTestOrchestrator
Creates a minimal orchestrator for unit tests:
- In-memory SQLite storage
- Orchestrator service only
- gRPC server on port 50052

#### startAllRunners
Starts all 7 runner types needed for the workflow:
- kickoff (port 50070)
- config (port 50071)
- assignment (port 50072)
- planning (port 50073)
- test_executor (port 50074)
- minibatch_finished (port 50075)
- batch_finished (port 50076)

#### RunnerProcess
Helper struct that manages runner lifecycle:
- Start: Initializes and registers runner
- Stop: Gracefully shuts down runner
- Uses common.BaseRunner for gRPC handling

### Test Data

All tests use simulated data:
- **64 CLs**: CL-1 through CL-64
- **Row failures**: Rows 2 and 5 (hardcoded in `simulateTest()`)
- **Column failures**: Columns 3 and 6 (hardcoded in `simulateTest()`)
- **Expected culprits**: CL-20, CL-23, CL-44, CL-47

## Expected Test Results

### Integration Test Flow

1. **Kickoff Stage**
   - Receives 64 CLs
   - Creates config, assignment, planning checks/stages
   - Status: FINAL

2. **Config Stage**
   - Stores matrix configuration
   - Total CLs: 64, Matrix size: 8x8
   - Status: FINAL

3. **Assignment Stage**
   - Creates 8 row assignments (8 CLs each)
   - Creates 8 column assignments (8 CLs each)
   - Status: FINAL

4. **Planning Stage**
   - Creates 8 row test stages
   - Creates 8 row finished stages
   - Creates 1 batch finished stage
   - Status: FINAL

5. **Row Test Execution** (8 stages in parallel)
   - Row 0: PASS
   - Row 1: PASS
   - Row 2: FAIL ✗
   - Row 3: PASS
   - Row 4: PASS
   - Row 5: FAIL ✗
   - Row 6: PASS
   - Row 7: PASS

6. **Row Finished Stages**
   - Passed rows: Submit 48 CLs (6 rows × 8 CLs)
   - Failed rows: Mark for column testing

7. **Batch Finished Stage (1st invocation)**
   - Detects 2 failed rows
   - Creates 8 column test stages (all columns need testing)
   - Status: FINAL

8. **Column Test Execution** (8 stages in parallel)
   - Column 0: PASS
   - Column 1: PASS
   - Column 2: PASS
   - Column 3: FAIL ✗
   - Column 4: PASS
   - Column 5: PASS
   - Column 6: FAIL ✗
   - Column 7: PASS

9. **Column Finished Stages**
   - Process column results
   - Status: FINAL

10. **Decode Stage**
    - Identifies culprits:
      - Row 2 ∩ Column 3 = CL-20
      - Row 2 ∩ Column 6 = CL-23
      - Row 5 ∩ Column 3 = CL-44
      - Row 5 ∩ Column 6 = CL-47
    - Status: FINAL

### Test Assertions

```go
// TestSubmitQueueIntegration assertions
- Workflow executes without errors
- Batch finished check reaches FINAL state
- All 8 row finished checks are FINAL
- 6 rows pass, 2 rows fail

// TestKickoffRunner assertions
- Returns FINAL stage state
- Creates check updates

// TestConfigRunner assertions
- Returns FINAL stage state
- Creates config check update

// TestTestExecutorRunner assertions
- Row 0 passes
- Row 2 fails
- Row 5 fails
- Column 3 fails
- Column 0 passes
```

## Manual Testing

### End-to-End Manual Test

```bash
# Terminal 1: Start orchestrator
go run ./cmd/server --port 50051

# Terminal 2-8: Start runners
go run ./cmd/ci/runners/submit_queue --type kickoff --listen :50070 &
go run ./cmd/ci/runners/submit_queue --type config --listen :50071 &
go run ./cmd/ci/runners/submit_queue --type assignment --listen :50072 &
go run ./cmd/ci/runners/submit_queue --type planning --listen :50073 &
go run ./cmd/ci/runners/submit_queue --type test_executor --listen :50074 &
go run ./cmd/ci/runners/submit_queue --type minibatch_finished --listen :50075 &
go run ./cmd/ci/runners/submit_queue --type batch_finished --listen :50076 &

# Terminal 9: Run client
go run ./cmd/submit_queue --orchestrator localhost:50051 --verbose
```

### Using Helper Script

```bash
# Embedded mode (easiest)
./run_submit_queue.sh embedded --verbose

# Distributed mode (for debugging)
./run_submit_queue.sh distributed
# Then follow instructions to start runners manually
```

## Troubleshooting Tests

### Test Timeout

If tests timeout, increase the timeout:
```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
```

### Port Conflicts

If ports are in use, change them in the test:
```go
// In startAllRunners
{"kickoff", 50170},  // Changed from 50070
{"config", 50171},   // Changed from 50071
// etc.
```

### Database Locks

If you see "database is locked" errors:
- Tests use temporary databases
- Each test cleans up after itself
- Run tests sequentially with `-p 1`:
  ```bash
  go test -v -p 1 ./cmd/ci/runners/submit_queue/...
  ```

### Runner Registration Failures

If runners fail to register:
- Increase wait time after starting orchestrator
- Check orchestrator is listening on correct port
- Verify no firewall blocking local connections

## Test Performance

### Expected Execution Time

- **Unit tests**: < 1 second each
- **Integration test**: 5-10 seconds
- **Full suite**: 10-15 seconds

### Optimization Tips

1. **Reduce polling intervals** in dispatcher config
2. **Use in-memory database** for faster I/O
3. **Run tests in parallel** when possible
4. **Skip cleanup** for faster iteration (not recommended for CI)

## Continuous Integration

### CI Configuration Example

```yaml
name: Submit Queue Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - uses: actions/setup-go@v2
        with:
          go-version: '1.21'

      - name: Run tests
        run: |
          go test -v -race -coverprofile=coverage.out ./cmd/ci/runners/submit_queue/...

      - name: Upload coverage
        uses: codecov/codecov-action@v2
        with:
          files: ./coverage.out
```

## Future Test Improvements

1. **Property-based testing**: Use QuickCheck-style testing for matrix operations
2. **Chaos testing**: Inject failures during execution
3. **Load testing**: Test with larger batches (128, 256 CLs)
4. **Benchmark tests**: Measure performance characteristics
5. **Integration with real VCS**: Test actual CL submission
6. **Network partition testing**: Test resilience to network issues
7. **State machine verification**: Formal verification of state transitions

## Test Metrics

Track these metrics over time:
- Test execution time
- Code coverage percentage
- Number of flaky tests
- Average time to detect regressions
- Test maintenance cost

## References

- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [Table-Driven Tests in Go](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests)
- [Integration Testing Best Practices](https://martinfowler.com/bliki/IntegrationTest.html)
