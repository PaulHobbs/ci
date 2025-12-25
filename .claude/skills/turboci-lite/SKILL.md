---
name: turboci-lite
description: Documentation for the TurboCI-Lite workflow orchestration service. Use when working with turboci-lite code, understanding its architecture, modifying the gRPC service, working with workflow stages/checks, using the pkg/ci DAG API, or debugging the orchestrator.
---

# TurboCI-Lite Service

TurboCI-Lite is a simplified, monolithic gRPC-based workflow orchestration service written in Go. It manages CI/CD pipelines by orchestrating a directed graph of work items called Checks (objectives) and Stages (executable units).

## Architecture Overview

The system uses a **push-based execution model** where the orchestrator dispatches work to registered stage runners via gRPC.

```
┌─────────────────────────────────────────────────────────────┐
│                     Orchestrator                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │ Orchestrator │  │   Runner     │  │    Callback      │   │
│  │   Service    │  │   Service    │  │    Service       │   │
│  └──────┬───────┘  └──────┬───────┘  └────────┬─────────┘   │
│         │                 │                    │             │
│         ▼                 ▼                    ▼             │
│  ┌─────────────────────────────────────────────────────┐    │
│  │              Dispatcher (background)                 │    │
│  │   - Polls execution queue for pending work           │    │
│  │   - Dispatches to registered runners                 │    │
│  │   - Handles retries and timeouts                     │    │
│  └─────────────────────────────────────────────────────┘    │
│                            │                                 │
│                            ▼                                 │
│  ┌─────────────────────────────────────────────────────┐    │
│  │              SQLite Storage                          │    │
│  │   - stage_runners (registrations)                    │    │
│  │   - stage_executions (durable queue)                 │    │
│  └─────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
              │                           ▲
              │ Stage.Run()               │ CompleteExecution()
              │ Stage.RunAsync()          │ UpdateExecution()
              ▼                           │
┌─────────────────────────────────────────────────────────────┐
│                    Stage Runners                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │ build_runner │  │ test_runner  │  │ plan_runner  │       │
│  │   :50052     │  │   :50053     │  │   :50054     │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
└─────────────────────────────────────────────────────────────┘
```

### Layered Architecture

```
Client Requests
    ↓
Transport Layer (gRPC)           → internal/transport/grpc/
    ↓
Endpoint Layer (Validation)      → internal/endpoint/
    ↓
Service Layer (Business Logic)   → internal/service/
    ↓
Storage Layer (Repository)       → internal/storage/
    ↓
SQLite Implementation            → internal/storage/sqlite/
```

## Key Concepts

### WorkPlan
Container for an entire workflow execution. All Checks and Stages belong to a WorkPlan.

### Check
A non-executable node representing an objective or question to be answered. Checks have dependencies and transition through states.

**Check States:**
- `PLANNING` (10) → Initial state
- `PLANNED` (20) → Ready for execution
- `WAITING` (30) → Waiting for dependencies
- `FINAL` (40) → Complete (terminal)

### Stage
An executable unit of work with retry tracking. Stages have Assignments that target Checks.

**Stage States:**
- `PLANNED` (10) → Ready to execute
- `ATTEMPTING` (20) → Currently executing
- `AWAITING_GROUP` (30) → Waiting for related work
- `FINAL` (40) → Complete (terminal)

**Stage Fields:**
- `execution_mode` - SYNC or ASYNC execution
- `runner_type` - Which runner handles this stage (e.g., "build_executor", "test_executor")

### Attempt
A single execution attempt within a Stage.

**Attempt States:**
- `PENDING` (10) → Not started
- `SCHEDULED` (20) → Scheduled for execution
- `RUNNING` (30) → Currently running
- `COMPLETE` (40) → Finished successfully
- `INCOMPLETE` (50) → Failed or cancelled

### Dependencies
Directed edges between nodes with AND/OR predicates:
- **AND**: All dependencies must be satisfied
- **OR**: At least one dependency must be satisfied

### Stage Runners
Long-lived gRPC servers that implement the `StageRunner` service. They register with the orchestrator and receive execution requests.

**Runner Registration:**
- Runners register with `runner_type`, endpoint address, supported modes, and capacity
- Registrations have TTL and require heartbeat renewal
- Orchestrator tracks current load per runner

### Execution Modes
- **SYNC**: Orchestrator calls `Run()`, blocks until completion
- **ASYNC**: Orchestrator calls `RunAsync()`, runner calls back with `CompleteStageExecution()`

## Directory Structure

```
turboci-lite/
├── cmd/
│   ├── server/main.go          # Orchestrator server entry point
│   └── runner-example/main.go  # Example stage runner implementation
├── internal/
│   ├── domain/                 # Pure domain models
│   │   ├── workplan.go         # WorkPlan entity
│   │   ├── check.go            # Check, CheckResult, CheckState
│   │   ├── stage.go            # Stage, Attempt, AttemptState
│   │   ├── dependency.go       # Dependency, DependencyGroup
│   │   ├── execution.go        # StageExecution (queue entry)
│   │   ├── runner.go           # StageRunner (registration)
│   │   └── errors.go           # Domain errors
│   ├── storage/                # Repository interfaces
│   │   ├── repository.go       # Abstract interfaces
│   │   └── sqlite/             # SQLite implementation
│   │       ├── sqlite.go       # Connection management
│   │       ├── migrations.go   # Schema definitions
│   │       ├── workplan.go     # WorkPlan queries
│   │       ├── check.go        # Check queries
│   │       ├── stage.go        # Stage queries
│   │       ├── dependency.go   # Dependency queries
│   │       ├── runner.go       # StageRunner queries
│   │       └── execution.go    # StageExecution queries
│   ├── service/
│   │   ├── orchestrator.go     # Core business logic
│   │   ├── runner.go           # Runner registration service
│   │   ├── dispatcher.go       # Background work dispatcher
│   │   └── callback.go         # Async completion handling
│   ├── endpoint/
│   │   └── endpoint.go         # Validation, error mapping
│   └── transport/grpc/
│       ├── server.go           # gRPC server setup
│       └── handlers.go         # Proto ↔ Domain conversion
├── proto/turboci/v1/           # Protocol buffer definitions
│   ├── service.proto           # Orchestrator gRPC service
│   ├── stagerunner.proto       # StageRunner gRPC service
│   ├── workplan.proto          # WorkPlan messages
│   ├── check.proto             # Check messages
│   ├── stage.proto             # Stage messages
│   └── common.proto            # Shared enums (includes ExecutionMode)
├── client/
│   └── pipeline.go             # High-level pipeline runner (legacy)
├── pkg/
│   ├── id/
│   │   └── generator.go        # UUID generation
│   └── ci/                     # High-level developer API
│       ├── helpers.go          # Level 1: Thin syntax helpers
│       ├── builders.go         # Level 2: Fluent builders
│       ├── workflow.go         # Level 3: Async futures
│       ├── dag.go              # Level 4: Declarative DAG DSL
│       └── lint/               # Static analysis linter
│           └── analyzer.go
├── go.mod
└── Makefile
```

## gRPC APIs

### TurboCIOrchestrator Service

The main orchestrator service exposes these RPCs:

**Workflow Management:**
```protobuf
rpc CreateWorkPlan(CreateWorkPlanRequest) returns (CreateWorkPlanResponse)
rpc GetWorkPlan(GetWorkPlanRequest) returns (GetWorkPlanResponse)
rpc WriteNodes(WriteNodesRequest) returns (WriteNodesResponse)
rpc QueryNodes(QueryNodesRequest) returns (QueryNodesResponse)
```

**Runner Registration:**
```protobuf
rpc RegisterStageRunner(RegisterStageRunnerRequest) returns (RegisterStageRunnerResponse)
rpc UnregisterStageRunner(UnregisterStageRunnerRequest) returns (UnregisterStageRunnerResponse)
rpc ListStageRunners(ListStageRunnersRequest) returns (ListStageRunnersResponse)
```

**Async Callbacks (called by runners):**
```protobuf
rpc UpdateStageExecution(UpdateStageExecutionRequest) returns (UpdateStageExecutionResponse)
rpc CompleteStageExecution(CompleteStageExecutionRequest) returns (CompleteStageExecutionResponse)
```

### StageRunner Service

The service that stage runners implement:

```protobuf
service StageRunner {
  // Run executes a stage synchronously, blocking until complete
  rpc Run(RunRequest) returns (RunResponse);

  // RunAsync starts async execution, returns immediately
  // Runner must call back to orchestrator with completion
  rpc RunAsync(RunAsyncRequest) returns (RunAsyncResponse);

  // Ping checks if runner is healthy and available
  rpc Ping(PingRequest) returns (PingResponse);
}
```

## Execution Flow

### Sync Execution
1. Stage advances to ATTEMPTING → creates `StageExecution` (PENDING)
2. Dispatcher picks up pending execution, selects available runner
3. Dispatcher calls `runner.Run()` (blocks)
4. Runner executes, returns `RunResponse` with check updates
5. Dispatcher applies updates via orchestrator, marks execution COMPLETE

### Async Execution
1. Stage advances to ATTEMPTING → creates `StageExecution` (PENDING)
2. Dispatcher picks up pending execution, selects available runner
3. Dispatcher calls `runner.RunAsync()` (returns immediately)
4. Runner acknowledges, starts background work
5. Runner calls `orchestrator.UpdateStageExecution()` for progress
6. Runner calls `orchestrator.CompleteStageExecution()` when done
7. Callback service applies updates, marks execution COMPLETE

## Configuration

Environment variables:
- `GRPC_PORT` - TCP port for gRPC server (default: 50051)
- `SQLITE_PATH` - Path to SQLite database file (default: turboci.db)

## Building and Running

```bash
# Install protoc plugins
make install-tools

# Generate proto code
make proto

# Build server
make build

# Run server
make run

# Run tests
make test
```

## Example: Creating a Pipeline with Push Execution

```go
// Create work plan
wp, _ := orchestrator.CreateWorkPlan(ctx, &CreateWorkPlanRequest{})

// Write planning stage with runner type
orchestrator.WriteNodes(ctx, &WriteNodesRequest{
    WorkPlanID: wp.ID,
    Checks: []*CheckWrite{{
        ID:    "plan-check",
        State: CheckStatePlanning,
    }},
    Stages: []*StageWrite{{
        ID:            "plan-stage",
        RunnerType:    "planning",
        ExecutionMode: ExecutionModeSync,
        Assignments: []Assignment{{
            TargetCheckID: "plan-check",
            GoalState:     CheckStateFinal,
        }},
    }},
})

// Write build stage with async execution
orchestrator.WriteNodes(ctx, &WriteNodesRequest{
    WorkPlanID: wp.ID,
    Checks: []*CheckWrite{{
        ID: "build-check",
        Dependencies: &DependencyGroup{
            Predicate: PredicateAND,
            Dependencies: []DependencyRef{{
                TargetType: NodeTypeCheck,
                TargetID:   "plan-check",
            }},
        },
    }},
    Stages: []*StageWrite{{
        ID:            "build-stage",
        RunnerType:    "build_executor",
        ExecutionMode: ExecutionModeAsync,
        Assignments: []Assignment{{
            TargetCheckID: "build-check",
            GoalState:     CheckStateFinal,
        }},
    }},
})
```

## Example: Implementing a Stage Runner

```go
type BuildRunner struct {
    pb.UnimplementedStageRunnerServer
    orchestratorClient pb.TurboCIOrchestratorClient
}

func (r *BuildRunner) Run(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
    // Execute build synchronously
    result := executeBuild(req.Args)

    return &pb.RunResponse{
        StageState: pb.StageState_STAGE_STATE_FINAL,
        CheckUpdates: []*pb.CheckUpdate{{
            CheckId:    req.AssignedCheckIds[0],
            State:      pb.CheckState_CHECK_STATE_FINAL,
            ResultData: result,
            Finalize:   true,
        }},
    }, nil
}

func (r *BuildRunner) RunAsync(ctx context.Context, req *pb.RunAsyncRequest) (*pb.RunAsyncResponse, error) {
    // Start async execution
    go func() {
        result := executeBuild(req.Args)

        // Report completion via callback
        r.orchestratorClient.CompleteStageExecution(ctx, &pb.CompleteStageExecutionRequest{
            ExecutionId: req.ExecutionId,
            StageState:  pb.StageState_STAGE_STATE_FINAL,
            CheckUpdates: []*pb.CheckUpdate{{
                CheckId:    req.AssignedCheckIds[0],
                State:      pb.CheckState_CHECK_STATE_FINAL,
                ResultData: result,
                Finalize:   true,
            }},
        })
    }()

    return &pb.RunAsyncResponse{Accepted: true}, nil
}

func main() {
    // Register with orchestrator
    orchestratorConn, _ := grpc.Dial("localhost:50051", grpc.WithInsecure())
    client := pb.NewTurboCIOrchestratorClient(orchestratorConn)

    resp, _ := client.RegisterStageRunner(ctx, &pb.RegisterStageRunnerRequest{
        RunnerId:       "build-runner-1",
        RunnerType:     "build_executor",
        Address:        "localhost:50052",
        SupportedModes: []pb.ExecutionMode{pb.EXECUTION_MODE_SYNC, pb.EXECUTION_MODE_ASYNC},
        MaxConcurrent:  5,
        TtlSeconds:     300,
    })

    // Start gRPC server
    lis, _ := net.Listen("tcp", ":50052")
    server := grpc.NewServer()
    pb.RegisterStageRunnerServer(server, &BuildRunner{orchestratorClient: client})
    server.Serve(lis)
}
```

## Testing with grpcurl

```bash
# List services
grpcurl -plaintext localhost:50051 list

# Create a work plan
grpcurl -plaintext -d '{}' localhost:50051 turboci.v1.TurboCIOrchestrator/CreateWorkPlan

# Register a stage runner
grpcurl -plaintext -d '{
  "runner_id": "test-runner",
  "runner_type": "build_executor",
  "address": "localhost:50052",
  "supported_modes": ["EXECUTION_MODE_SYNC"],
  "max_concurrent": 5,
  "ttl_seconds": 300
}' localhost:50051 turboci.v1.TurboCIOrchestrator/RegisterStageRunner

# List registered runners
grpcurl -plaintext -d '{}' localhost:50051 turboci.v1.TurboCIOrchestrator/ListStageRunners
```

## Error Handling

Domain errors are mapped to gRPC status codes:
- `ErrNotFound` → `codes.NotFound`
- `ErrInvalidState` → `codes.FailedPrecondition`
- `ErrConcurrentModify` → `codes.Aborted`
- `ErrInvalidArgument` → `codes.InvalidArgument`
- `ErrAlreadyExists` → `codes.AlreadyExists`
- `ErrRunnerNotFound` → `codes.NotFound`
- `ErrRunnerUnavailable` → `codes.Unavailable`
- `ErrExecutionNotFound` → `codes.NotFound`

## Database Schema

Core tables (in `internal/storage/sqlite/migrations.go`):
- `work_plans` - Workflow containers
- `checks` - Non-executable objectives
- `stages` - Executable work units (includes `execution_mode`, `runner_type`)
- `stage_attempts` - Execution attempts
- `check_results` - Results from attempts
- `dependencies` - Graph edges between nodes
- `stage_runners` - Runner registrations with TTL
- `stage_executions` - Durable execution queue

SQLite is configured with WAL mode for better concurrency.

## Design Decisions

**Push-based execution model:**
- Orchestrator dispatches work to registered runners via gRPC
- Runners implement the `StageRunner` service
- Supports both sync (blocking) and async (callback) execution modes
- SQLite-backed execution queue for durability across restarts

**Simplifications from full TurboCI:**
- No realm-based security (single security context)
- Simple AND/OR predicates (no complex thresholds)
- JSON storage for options/results
- No edit history tracking
- Single SQLite instance (not distributed)

**Trade-offs:**
- Easy to deploy and understand
- Single-machine scalability limit
- No multi-tenant isolation
- No audit trail

## Testing

**E2E Test Infrastructure** (`testing/e2e/`):
- `TestEnv` - Complete test environment with all services
- `MockRunner` - In-process mock implementing `StageRunnerClient`
- File-based SQLite with WAL mode for concurrent access
- 22 comprehensive tests covering:
  - Basic sync/async execution
  - Runner registration and lifecycle
  - Complex workflows (diamond deps, fan-out, chains)
  - Error handling (runner failures, timeouts)

```bash
# Run E2E tests
cd testing/e2e && go test -v

# Run specific test
go test -v -run TestBasicSyncExecution
```

---

## pkg/ci - High-Level Developer API

The `pkg/ci` package provides a simplified developer API for TurboCI-Lite workflows. It offers four abstraction levels that reduce boilerplate while maintaining full control when needed.

```
pkg/ci/
├── helpers.go      # Level 1: Thin syntax helpers
├── builders.go     # Level 2: Fluent builders
├── workflow.go     # Level 3: Async futures with streaming
├── dag.go          # Level 4: Declarative DAG DSL
└── lint/           # Static analysis linter
```

### Abstraction Levels Comparison

| Feature | L1 Helpers | L2 Builder | L3 Futures | L4 DAG |
|---------|------------|------------|------------|--------|
| Boilerplate Reduction | 10% | 40% | 60% | 80% |
| Learning Curve | None | Low | Medium | Medium |
| Control Retained | 100% | 100% | 90% | 70% |
| Dynamic Node Creation | Yes | Yes | Yes | Yes (hybrid) |
| Async/Await Style | No | No | Yes | Yes |

All levels are **composable** - you can mix and match, escaping to lower levels when needed.

---

### Level 1: Helpers (helpers.go)

Thin syntax sugar that reduces noise while keeping the same mental model.

```go
import "github.com/example/turboci-lite/pkg/ci"

// Before: Verbose dependency construction
deps := &domain.DependencyGroup{
    Predicate: domain.PredicateAND,
    Dependencies: []domain.DependencyRef{
        {TargetType: domain.NodeTypeCheck, TargetID: "build:main"},
    },
}

// After: Clean helper functions
deps := ci.DependsOn(ci.Check("build:main"))
```

**Available Helpers:**

| Function | Description |
|----------|-------------|
| `Ptr[T](v T) *T` | Generic pointer helper |
| `Check(id string) NodeRef` | Reference to a check node |
| `Stage(id string) NodeRef` | Reference to a stage node |
| `DependsOn(refs ...NodeRef) *DependencyGroup` | AND dependency group |
| `DependsOnAny(refs ...NodeRef) *DependencyGroup` | OR dependency group |
| `DependsOnChecks(ids ...string) *DependencyGroup` | AND deps from check IDs |
| `DependsOnStages(ids ...string) *DependencyGroup` | AND deps from stage IDs |
| `Assigns(checkID string) Assignment` | Assignment with FINAL goal |
| `AssignsWithGoal(checkID string, goal CheckState) Assignment` | Custom goal state |
| `AssignmentList(assignments ...Assignment) []Assignment` | Build assignment slice |

---

### Level 2: Builders (builders.go)

Fluent builder pattern for constructing CheckWrite and StageWrite objects with validation.

#### CheckBuilder

```go
import "github.com/example/turboci-lite/pkg/ci"

// Build a check with all options
check := ci.NewCheck("build:main").
    Kind("build").
    State(domain.CheckStatePlanning).
    Option("target", "//pkg/...").
    DependsOn("plan:main").
    Build()

// Use in WriteNodes request
req.Checks = append(req.Checks, check)
```

**CheckBuilder Methods:**

| Method | Description |
|--------|-------------|
| `NewCheck(id string) *CheckBuilder` | Create builder (panics if empty) |
| `Kind(kind string)` | Set check kind |
| `State(state CheckState)` | Set initial state |
| `Planning()` / `Planned()` / `Waiting()` / `Final()` | State shortcuts |
| `Options(opts map[string]any)` | Set all options |
| `Option(key string, value any)` | Set single option |
| `DependsOn(checkIDs ...string)` | AND dependencies on checks |
| `DependsOnAny(checkIDs ...string)` | OR dependencies on checks |
| `Build() *service.CheckWrite` | Build final object |

#### StageBuilder

```go
// Build a stage with runner and assignments
stage := ci.NewStage("exec:build").
    RunnerType("builder").
    Sync().
    Arg("target", "//pkg/...").
    Assigns("build:main").
    DependsOnStages("exec:plan").
    Build()
```

**StageBuilder Methods:**

| Method | Description |
|--------|-------------|
| `NewStage(id string) *StageBuilder` | Create builder (panics if empty) |
| `RunnerType(rt string)` | Set runner type (required) |
| `Sync()` / `Async()` | Set execution mode |
| `State(state StageState)` | Set initial state |
| `Args(args map[string]any)` | Set all args |
| `Arg(key string, value any)` | Set single arg |
| `Assigns(checkID string)` | Assign check with FINAL goal |
| `AssignsWithGoal(checkID string, goal CheckState)` | Custom goal |
| `AssignsAll(assignments ...Assignment)` | Multiple assignments |
| `DependsOnStages(stageIDs ...string)` | Depend on stages |
| `DependsOnChecks(checkIDs ...string)` | Depend on checks |
| `Build() *service.StageWrite` | Build final object |

#### AttemptBuilder

```go
// Build an attempt update
attempt := ci.NewAttempt("attempt-1").
    Running().
    Progress(50, "Building...").
    Build()
```

---

### Level 3: Workflow Context (workflow.go)

Imperative async programming with futures. Hides polling behind clean Wait APIs.

```go
import "github.com/example/turboci-lite/pkg/ci"

// Create workflow context (connects to orchestrator)
wf, err := ci.NewWorkflowContext(ctx, orchestrator, grpcClient, workPlanID)

// Define checks - returns handles for async waiting
buildCheck, _ := wf.DefineCheck(ctx, "build:main",
    ci.WithKind("build"),
    ci.WithCheckState(domain.CheckStatePlanning),
)

testCheck, _ := wf.DefineCheck(ctx, "test:unit",
    ci.WithKind("test"),
    ci.WithCheckDeps("build:main"),
)

// Define stages
buildStage, _ := wf.DefineStage(ctx, "exec:build",
    ci.WithRunner("builder"),
    ci.WithSync(),
    ci.AssignsTo("build:main"),
)

// Wait for completion (uses streaming with polling fallback)
result, err := buildCheck.Wait(ctx)

// Or wait with timeout
result, err := buildCheck.WaitWithTimeout(ctx, 5*time.Minute)

// Parallel waiting
err := ci.WaitAllChecks(ctx, buildCheck, testCheck)
err := ci.WaitAllStages(ctx, buildStage, testStage)

// Race - wait for first completion
check, err := ci.WaitAnyCheck(ctx, check1, check2, check3)
```

**Functional Options:**

| Option | Description |
|--------|-------------|
| `WithKind(kind string)` | Set check kind |
| `WithCheckState(state CheckState)` | Set check state |
| `WithCheckDeps(checkIDs ...string)` | Check dependencies |
| `WithRunner(runnerType string)` | Set runner type |
| `WithSync()` / `WithAsync()` | Execution mode |
| `WithStageState(state StageState)` | Set stage state |
| `WithStageDeps(stageIDs ...string)` | Stage dependencies |
| `AssignsTo(checkIDs ...string)` | Assign to checks |

---

### Level 4: DAG Builder (dag.go)

Declarative workflow definition with hybrid dynamic mode. Define your workflow as a graph, then execute it.

#### Static Workflow (Define Before Execute)

```go
import "github.com/example/turboci-lite/pkg/ci"

// Create DAG
dag := ci.NewDAG()

// Define checks (returns nodes for dependency references)
planCheck := dag.Check("plan").Kind("plan")
buildCheck := dag.Check("build").Kind("build").DependsOn(planCheck)
testCheck := dag.Check("test").Kind("test").DependsOn(buildCheck)

// Define stages
planStage := dag.Stage("plan-stage").Runner("planner").Assigns(planCheck)
buildStage := dag.Stage("build-stage").Runner("builder").Assigns(buildCheck).After(planStage)
testStage := dag.Stage("test-stage").Runner("tester").Assigns(testCheck).After(buildStage)

// Execute the DAG
exec, err := dag.Execute(ctx, orchestrator)
if err != nil {
    return err
}

// Wait for completion
err = exec.WaitForCompletion(ctx)

// Or wait for specific nodes
check, err := exec.WaitForCheck(ctx, "build")
stage, err := exec.WaitForStage(ctx, "build-stage")
```

#### Dynamic Workflow (Add Nodes During Execution)

The DAG supports **hybrid mode** - add nodes dynamically after execution starts:

```go
dag := ci.NewDAG()

// Initial planning phase
planCheck := dag.Check("plan").Kind("plan")
planStage := dag.Stage("plan").Runner("planner").Assigns(planCheck)

// Start execution
exec, _ := dag.Execute(ctx, orchestrator)

// Wait for planning to complete
planResult, _ := exec.WaitForCheck(ctx, "plan")

// Dynamically add build checks based on plan result
targets := planResult.Options["targets"].([]string)
for _, target := range targets {
    // AddCheck works after Execute - writes immediately to orchestrator
    buildCheck, _ := dag.AddCheck(ctx, "build:"+target)
    buildCheck.Kind("build").DependsOn(planCheck)

    buildStage, _ := dag.AddStage(ctx, "exec:build:"+target)
    buildStage.Runner("builder").Assigns(buildCheck).After(planStage)
}

// Wait for all dynamic work to complete
exec.WaitForCompletion(ctx)
```

#### CheckNode Methods

| Method | Description |
|--------|-------------|
| `dag.Check(id string) *CheckNode` | Create check node |
| `Kind(kind string) *CheckNode` | Set kind |
| `State(state CheckState) *CheckNode` | Set state |
| `Option(key string, value any) *CheckNode` | Set option |
| `DependsOn(checks ...*CheckNode) *CheckNode` | Add check dependencies |

#### StageNode Methods

| Method | Description |
|--------|-------------|
| `dag.Stage(id string) *StageNode` | Create stage node |
| `Runner(runnerType string) *StageNode` | Set runner (required) |
| `Sync() / Async() *StageNode` | Execution mode |
| `Arg(key string, value any) *StageNode` | Set arg |
| `Assigns(checks ...*CheckNode) *StageNode` | Assign to checks |
| `After(stages ...*StageNode) *StageNode` | Depend on stages |
| `AfterChecks(checks ...*CheckNode) *StageNode` | Depend on checks |

#### WorkflowExecution Methods

| Method | Description |
|--------|-------------|
| `WaitForCompletion(ctx) error` | Wait for all nodes |
| `WaitForCheck(ctx, id) (*domain.Check, error)` | Wait for specific check |
| `WaitForStage(ctx, id) (*domain.Stage, error)` | Wait for specific stage |

---

### Static Analysis Linter (pkg/ci/lint)

Catch common mistakes at build time with the `cilint` analyzer.

**Detected Issues:**

| Rule | Detects |
|------|---------|
| Empty ID | `ci.NewCheck("")` - will panic at runtime |
| Empty dependencies | `ci.DependsOn()` - no-op call |
| Duplicate dependencies | `ci.DependsOn("a", "b", "a")` |

**Installation and Usage:**

```bash
# Install the linter
go install github.com/example/turboci-lite/cmd/ci-lint@latest

# Run on your code
ci-lint ./...

# Or with golangci-lint (.golangci.yml)
linters:
  enable:
    - cilint
```

**Example Output:**

```
pkg/myworkflow/pipeline.go:42:15: NewCheck called with empty string literal - will panic at runtime
pkg/myworkflow/pipeline.go:58:2: DependsOn called with no arguments - this is a no-op
```

---

### Complete Example: CI Pipeline with DAG

```go
package main

import (
    "context"
    "log"

    "github.com/example/turboci-lite/pkg/ci"
    "github.com/example/turboci-lite/internal/service"
)

func RunCIPipeline(ctx context.Context, orch *service.OrchestratorService) error {
    dag := ci.NewDAG()

    // Phase 1: Planning
    planCheck := dag.Check("plan").Kind("plan")
    planStage := dag.Stage("plan").
        Runner("planner").
        Sync().
        Assigns(planCheck)

    // Phase 2: Build (depends on plan)
    buildCheck := dag.Check("build").Kind("build").DependsOn(planCheck)
    buildStage := dag.Stage("build").
        Runner("builder").
        Async().
        Arg("parallelism", 4).
        Assigns(buildCheck).
        After(planStage)

    // Phase 3: Test (depends on build)
    testCheck := dag.Check("test").Kind("test").DependsOn(buildCheck)
    testStage := dag.Stage("test").
        Runner("tester").
        Async().
        Assigns(testCheck).
        After(buildStage)

    // Phase 4: Deploy (depends on test)
    deployCheck := dag.Check("deploy").Kind("deploy").DependsOn(testCheck)
    deployStage := dag.Stage("deploy").
        Runner("deployer").
        Sync().
        Assigns(deployCheck).
        After(testStage)

    // Execute
    exec, err := dag.Execute(ctx, orch)
    if err != nil {
        return err
    }

    // Wait for completion
    if err := exec.WaitForCompletion(ctx); err != nil {
        return err
    }

    // Check results
    deployResult, _ := exec.WaitForCheck(ctx, "deploy")
    log.Printf("Deploy completed: %v", deployResult.State)

    return nil
}
```

### Mixing Abstraction Levels

You can escape to lower levels when needed:

```go
// Use DAG for structure
dag := ci.NewDAG()
planCheck := dag.Check("plan").Kind("plan")

// Execute DAG
exec, _ := dag.Execute(ctx, orch)

// Escape to Level 2 builders for custom nodes
customCheck := ci.NewCheck("custom:special").
    Kind("custom").
    Option("special_config", map[string]any{"key": "value"}).
    DependsOn("plan").
    Build()

// Write directly via orchestrator
orch.WriteNodes(ctx, &service.WriteNodesRequest{
    WorkPlanID: exec.WorkPlanID(),
    Checks:     []*service.CheckWrite{customCheck},
})
```
