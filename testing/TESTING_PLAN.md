# TurboCI-Lite Push-Based Execution Testing Plan

This document outlines the integration testing strategy for the push-based gRPC execution system.

## Overview

The push-based execution system involves several components that need to work together:

1. **Orchestrator** - Manages work plans, checks, stages, and dependencies
2. **Runner Service** - Handles runner registration, heartbeat, and selection
3. **Dispatcher** - Background worker that dispatches pending executions to runners
4. **Callback Service** - Handles async execution progress and completion callbacks
5. **Stage Runners** - External gRPC servers that execute stage work

## Test Infrastructure

### Location
```
testing/
├── TESTING_PLAN.md          # This file
└── e2e/
    ├── helpers_test.go      # Test utilities, mock runner, setup helpers
    ├── basic_test.go        # Basic workflow E2E tests
    ├── runner_test.go       # Runner registration and lifecycle tests
    ├── dispatcher_test.go   # Dispatcher behavior tests
    ├── execution_test.go    # Sync/async execution mode tests
    ├── workflow_test.go     # Complex workflow structure tests
    ├── callback_test.go     # Callback and progress update tests
    └── error_test.go        # Error handling and edge cases
```

### Test Helpers

- `TestEnv` - Sets up in-memory SQLite, services, and mock runners
- `MockRunner` - In-process mock that implements StageRunnerClient interface
- `MockRunnerServer` - Optional real gRPC server for more realistic tests
- Helper functions for creating work plans, checks, stages with various configurations

## Test Categories

### 1. Basic E2E Tests (`basic_test.go`)

| Test | Description |
|------|-------------|
| `TestBasicSyncExecution` | Single stage with sync execution completes a check |
| `TestBasicAsyncExecution` | Single stage with async execution and callback |
| `TestStageWithMultipleChecks` | One stage updates multiple assigned checks |
| `TestSequentialStages` | Two stages with dependency, executed in order |
| `TestParallelStages` | Two independent stages execute concurrently |

### 2. Runner Registration Tests (`runner_test.go`)

| Test | Description |
|------|-------------|
| `TestRunnerRegistration` | Register a runner and verify it appears in list |
| `TestRunnerHeartbeat` | Runner re-registration extends TTL |
| `TestRunnerUnregistration` | Explicit unregistration removes runner |
| `TestRunnerExpiration` | Runner expires after TTL without heartbeat |
| `TestMultipleRunnersSameType` | Multiple runners of same type, work distributed |
| `TestMultipleRunnerTypes` | Different runner types handle different stages |
| `TestRunnerMetadata` | Runner metadata is stored and retrievable |

### 3. Dispatcher Tests (`dispatcher_test.go`)

| Test | Description |
|------|-------------|
| `TestDispatcherSelectsRunner` | Dispatcher picks an available runner |
| `TestDispatcherLoadBalancing` | Dispatcher picks least-loaded runner |
| `TestDispatcherNoRunnerAvailable` | Execution stays pending when no runners |
| `TestDispatcherRunnerBecomesAvailable` | Pending execution dispatched when runner registers |
| `TestDispatcherRespectsModeSupport` | Only dispatches to runners supporting the mode |
| `TestDispatcherRespectsCapacity` | Doesn't dispatch to runner at max capacity |

### 4. Execution Mode Tests (`execution_test.go`)

| Test | Description |
|------|-------------|
| `TestSyncExecutionSuccess` | Sync execution completes and updates checks |
| `TestSyncExecutionFailure` | Sync execution failure is recorded |
| `TestAsyncExecutionSuccess` | Async execution with completion callback |
| `TestAsyncExecutionProgress` | Async execution sends progress updates |
| `TestAsyncExecutionFailure` | Async execution reports failure via callback |
| `TestExecutionTimeout` | Execution past deadline is marked timed out |
| `TestExecutionDeadline` | Execution respects deadline passed to runner |

### 5. Workflow Structure Tests (`workflow_test.go`)

| Test | Description |
|------|-------------|
| `TestDiamondDependency` | A→B, A→C, B→D, C→D pattern |
| `TestLongChain` | Sequential chain of 5+ stages |
| `TestFanOutFanIn` | One stage fans out to many, then converges |
| `TestParallelBranches` | Multiple independent parallel branches |
| `TestCIStylePipeline` | Plan→Build→Test→Deploy pattern |
| `TestMultiPlatformBuild` | Same stage type runs for multiple platforms |
| `TestConditionalExecution` | Stages with OR dependencies |
| `TestLargeWorkflow` | 50+ stages to test scalability |

### 6. Callback Tests (`callback_test.go`)

| Test | Description |
|------|-------------|
| `TestProgressUpdatesRecorded` | Progress updates stored in execution |
| `TestProgressUpdatesMultiple` | Multiple progress updates in sequence |
| `TestCheckUpdatesFromCallback` | Check state and results updated from callback |
| `TestMultipleCheckUpdates` | Single callback updates multiple checks |
| `TestCallbackTriggersDownstream` | Completion triggers dependent stage execution |
| `TestCallbackWithFailure` | Failure in callback properly recorded |

### 7. Error Handling Tests (`error_test.go`)

| Test | Description |
|------|-------------|
| `TestRunnerDisconnect` | Runner disconnects mid-execution |
| `TestStaleExecution` | Execution with no progress marked stale |
| `TestExecutionRetry` | Failed execution can be retried with new attempt |
| `TestPartialWorkflowFailure` | Some stages fail, others complete |
| `TestInvalidCallback` | Callback for unknown execution is rejected |
| `TestDuplicateCompletion` | Second completion callback is ignored |
| `TestRunnerRejectsWork` | Async runner returns accepted=false |
| `TestConcurrentModification` | Optimistic locking prevents conflicts |

## Test Implementation Strategy

### Phase 1: Infrastructure
1. Create `helpers_test.go` with TestEnv and MockRunner
2. Verify mock runner can be injected into dispatcher

### Phase 2: Basic Tests
1. Implement `TestBasicSyncExecution` first
2. Debug and fix any issues in the system
3. Add remaining basic tests

### Phase 3: Component Tests
1. Runner registration tests
2. Dispatcher behavior tests
3. Execution mode tests

### Phase 4: Complex Workflows
1. Workflow structure tests
2. Callback tests
3. Error handling tests

## Mock Runner Behavior

The MockRunner should support:

```go
type MockRunner struct {
    // Configuration
    RunnerID      string
    RunnerType    string
    SupportedModes []domain.ExecutionMode
    MaxConcurrent int

    // Behavior hooks
    OnRun      func(req *RunRequest) (*RunResponse, error)
    OnRunAsync func(req *RunAsyncRequest) (*RunAsyncResponse, error)

    // State tracking
    ReceivedRequests []*RunRequest
    CurrentLoad      int

    // For async simulation
    PendingExecutions map[string]*RunAsyncRequest
}
```

Default behavior:
- Sync: Return success with all assigned checks finalized
- Async: Accept, simulate progress, then callback completion

## Assertions

Tests should verify:

1. **State transitions** - Stages move through PLANNED → ATTEMPTING → FINAL
2. **Check updates** - Checks receive results and reach FINAL state
3. **Execution records** - StageExecution entries have correct states/timestamps
4. **Runner load** - Load incremented/decremented correctly
5. **Dependencies** - Downstream stages only execute after dependencies complete

## Running Tests

```bash
# Run all E2E tests
go test ./testing/e2e/...

# Run specific test file
go test ./testing/e2e/ -run TestBasic

# Run with verbose output
go test ./testing/e2e/... -v

# Run with race detector
go test ./testing/e2e/... -race
```

## Success Criteria

- All tests pass consistently (no flaky tests)
- Tests complete in under 30 seconds total
- Code coverage of new push-based code > 80%
- Tests catch regressions when implementation changes
