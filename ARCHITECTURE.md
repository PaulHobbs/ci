# TurboCI-Lite Architecture

A simplified, monolithic gRPC Go server implementing core TurboCI workflow orchestration concepts with SQLite storage.

## Overview

TurboCI-Lite is a workflow orchestration system that manages a directed graph of **Checks** (objectives/questions to answer) and **Stages** (executable units that answer those questions). The system tracks dependencies between nodes and coordinates their execution through well-defined state machines.

## Core Concepts

### WorkPlan
A container for an entire workflow execution. All Checks and Stages belong to a single WorkPlan.

### Check (Non-executable Node)
Represents a named objective or question, e.g.:
- "Does source X build for platform Y?"
- "Do these tests pass?"

**State Machine:**
```
PLANNING -> PLANNED -> WAITING -> FINAL
```

| State | Description |
|-------|-------------|
| PLANNING | Check is being defined, options/deps can be edited |
| PLANNED | Check is complete but dependencies unresolved |
| WAITING | Dependencies resolved, waiting for results |
| FINAL | Fully complete and immutable |

### Stage (Executable Node)
Represents a unit of work that can be executed, with retry/attempt tracking.

**State Machine:**
```
PLANNED -> ATTEMPTING -> AWAITING_GROUP -> FINAL
```

| State | Description |
|-------|-------------|
| PLANNED | Stage inserted but dependencies unresolved |
| ATTEMPTING | Dependencies resolved, executing (has active Attempt) |
| AWAITING_GROUP | Execution done, waiting for spawned child stages |
| FINAL | Fully complete |

### Stage Attempt
A single execution attempt within a Stage. Stages can have multiple attempts (retries).

**State Machine:**
```
PENDING -> SCHEDULED -> RUNNING -> COMPLETE/INCOMPLETE
```

### Dependencies
Edges between nodes with boolean logic predicates (AND/OR/threshold).

## Layered Architecture (Goa-inspired)

Following the Goa design pattern, the codebase is organized into distinct layers with clean interfaces between them:

```
┌─────────────────────────────────────────────────────────────┐
│                    Transport Layer                          │
│              (gRPC server, proto handling)                  │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                    Endpoint Layer                           │
│         (Request validation, auth, error mapping)           │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                    Service Layer                            │
│   ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐    │
│   │Orchestrator │  │  Runner     │  │   Callback      │    │
│   │  Service    │  │  Service    │  │   Service       │    │
│   └──────┬──────┘  └──────┬──────┘  └────────┬────────┘    │
│          │                │                   │             │
│          └────────┬───────┴───────────────────┘             │
│                   ▼                                         │
│   ┌─────────────────────────────────────────────────────┐  │
│   │              Dispatcher (background)                 │  │
│   │   - Polls execution queue for pending work           │  │
│   │   - Dispatches to registered runners                 │  │
│   │   - Handles retries and timeouts                     │  │
│   └─────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                    Storage Layer                            │
│               (Repository interfaces)                       │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                   SQLite Implementation                     │
│              (Concrete storage backend)                     │
└─────────────────────────────────────────────────────────────┘
```

## Push-Based Execution Model

TurboCI-Lite uses a push-based execution model where the orchestrator dispatches work to registered stage runners:

```
┌─────────────────────────────────────────────────────────────┐
│                     Orchestrator                            │
│                          │                                  │
│                          ▼                                  │
│   ┌─────────────────────────────────────────────────────┐  │
│   │              stage_executions (queue)                │  │
│   │   - PENDING: waiting for dispatch                    │  │
│   │   - DISPATCHED: sent to runner                       │  │
│   │   - RUNNING: actively executing                      │  │
│   │   - COMPLETE/FAILED: terminal states                 │  │
│   └─────────────────────────────────────────────────────┘  │
│                          │                                  │
│              Dispatcher polls & dispatches                  │
│                          │                                  │
└──────────────────────────┼──────────────────────────────────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
              ▼            ▼            ▼
       ┌──────────┐  ┌──────────┐  ┌──────────┐
       │ Runner A │  │ Runner B │  │ Runner C │
       │ (build)  │  │  (test)  │  │ (deploy) │
       │  :50052  │  │  :50053  │  │  :50054  │
       └──────────┘  └──────────┘  └──────────┘
```

### Stage Runners

Stage runners are long-lived gRPC servers that implement the `StageRunner` service:

```protobuf
service StageRunner {
    rpc Run(RunRequest) returns (RunResponse);      // Sync execution
    rpc RunAsync(RunAsyncRequest) returns (RunAsyncResponse);  // Async execution
    rpc Ping(PingRequest) returns (PingResponse);   // Health check
}
```

Runners register with the orchestrator via `RegisterStageRunner` and must periodically re-register (heartbeat) to stay active. Each runner specifies:
- **runner_type**: What kind of work it handles (e.g., "build", "test")
- **supported_modes**: SYNC, ASYNC, or both
- **max_concurrent**: Capacity limit for load balancing
- **ttl**: Time-to-live before registration expires

### Execution Modes

**Sync Execution**: Dispatcher calls `runner.Run()`, blocks until completion, applies check updates immediately.

**Async Execution**: Dispatcher calls `runner.RunAsync()` which returns immediately. Runner later calls back via:
- `UpdateExecution()` - Progress updates
- `CompleteExecution()` - Final completion with check updates

## Directory Structure

```
turboci-lite/
├── cmd/
│   └── server/
│       └── main.go              # Server entry point
├── proto/
│   └── turboci/
│       └── v1/
│           ├── service.proto    # gRPC service definitions
│           ├── stagerunner.proto # StageRunner service (for runners to implement)
│           ├── workplan.proto   # WorkPlan messages
│           ├── check.proto      # Check messages
│           ├── stage.proto      # Stage messages
│           └── common.proto     # Shared types (states, deps, etc.)
├── gen/
│   └── proto/
│       └── turboci/
│           └── v1/              # Generated Go protobuf code
├── internal/
│   ├── transport/
│   │   └── grpc/
│   │       ├── server.go        # gRPC server setup
│   │       └── handlers.go      # Proto <-> endpoint adapters
│   ├── endpoint/
│   │   ├── endpoint.go          # Endpoint definitions
│   │   ├── workplan.go          # WorkPlan endpoints
│   │   ├── check.go             # Check endpoints
│   │   ├── stage.go             # Stage endpoints
│   │   └── validation.go        # Request validation
│   ├── service/
│   │   ├── orchestrator.go      # Core orchestration service
│   │   ├── runner.go            # Runner registration service
│   │   ├── dispatcher.go        # Background dispatcher (polls & dispatches)
│   │   ├── callback.go          # Async callback handling
│   │   ├── workplan.go          # WorkPlan business logic
│   │   ├── check.go             # Check business logic
│   │   ├── stage.go             # Stage business logic
│   │   └── dependency.go        # Dependency resolution logic
│   ├── storage/
│   │   ├── repository.go        # Repository interfaces
│   │   ├── models.go            # Domain models
│   │   └── sqlite/
│   │       ├── sqlite.go        # SQLite implementation
│   │       ├── workplan.go      # WorkPlan queries
│   │       ├── check.go         # Check queries
│   │       ├── stage.go         # Stage queries
│   │       ├── runner.go        # StageRunner queries
│   │       ├── execution.go     # StageExecution queries
│   │       └── migrations.go    # Schema migrations
│   └── domain/
│       ├── workplan.go          # WorkPlan domain types
│       ├── check.go             # Check domain types
│       ├── stage.go             # Stage domain types
│       ├── runner.go            # StageRunner domain types
│       ├── execution.go         # StageExecution domain types
│       └── dependency.go        # Dependency domain types
├── testing/
│   ├── TESTING_PLAN.md          # Integration test plan
│   └── e2e/                     # End-to-end tests
│       ├── helpers_test.go      # Test infrastructure (TestEnv, MockRunner)
│       ├── basic_test.go        # Basic execution tests
│       ├── runner_test.go       # Runner registration tests
│       ├── workflow_test.go     # Complex workflow tests
│       └── error_test.go        # Error handling tests
└── pkg/
    └── id/
        └── generator.go         # ID generation utilities
```

## Module Breakdown

### 1. Proto Layer (`proto/`)
Simplified protobuf definitions based on the original TurboCI protos.

**Key Simplifications:**
- No realm-based security (single security context)
- Simplified dependency predicates (simple AND/OR, no complex thresholds)
- No separate Datum/Value types (inline JSON for options/results)
- No edit history tracking
- Push-based execution via registered stage runners

### 2. Transport Layer (`internal/transport/grpc/`)
Handles gRPC server setup and proto message conversion.

**Responsibilities:**
- gRPC server initialization and shutdown
- Proto message marshaling/unmarshaling
- HTTP/2 transport configuration
- Interceptor chain (logging, recovery, etc.)

### 3. Endpoint Layer (`internal/endpoint/`)
Bridges transport and service layers with request/response handling.

**Responsibilities:**
- Request validation (required fields, valid states, etc.)
- Error mapping to gRPC status codes
- Rate limiting (optional)
- Request/response logging

**Key Interface:**
```go
type Endpoint func(ctx context.Context, request any) (response any, err error)
```

### 4. Service Layer (`internal/service/`)
Core business logic, orchestration, and execution dispatch.

**OrchestratorService:**
- `CreateWorkPlan(ctx, req) -> WorkPlan`
- `WriteNodes(ctx, req) -> WriteNodesResponse`
- `QueryNodes(ctx, req) -> QueryNodesResponse`
- Dependency resolution and state advancement
- Creates `StageExecution` when stages advance to ATTEMPTING

**RunnerService:**
- `RegisterRunner(ctx, req) -> RegistrationResponse` - Register or re-register (heartbeat)
- `UnregisterRunner(ctx, registrationID) -> error` - Explicit unregistration
- `ListRunners(ctx, filter) -> []StageRunner` - List active runners
- `SelectRunner(ctx, runnerType, mode) -> *StageRunner` - Pick least-loaded runner

**CallbackService:**
- `UpdateExecution(ctx, req) -> error` - Progress updates from async runners
- `CompleteExecution(ctx, req) -> error` - Final completion with check updates
- `GetExecution(ctx, executionID) -> *StageExecution`

**Dispatcher (Background Worker):**
- Polls `stage_executions` table for PENDING work
- Selects appropriate runner based on type and mode
- Dispatches sync executions (blocks, applies results)
- Dispatches async executions (returns immediately, runner calls back)
- Handles expired runner cleanup
- Handles stale/timed-out execution detection

### 5. Storage Layer (`internal/storage/`)
Abstract repository interfaces for persistence.

**Key Interfaces:**
```go
type WorkPlanRepository interface {
    Create(ctx context.Context, wp *domain.WorkPlan) error
    Get(ctx context.Context, id string) (*domain.WorkPlan, error)
    Update(ctx context.Context, wp *domain.WorkPlan) error
}

type CheckRepository interface {
    Create(ctx context.Context, check *domain.Check) error
    Get(ctx context.Context, workPlanID, checkID string) (*domain.Check, error)
    Update(ctx context.Context, check *domain.Check) error
    List(ctx context.Context, workPlanID string, filter CheckFilter) ([]*domain.Check, error)
    UpdateState(ctx context.Context, workPlanID, checkID string, state CheckState) error
}

type StageRepository interface {
    Create(ctx context.Context, stage *domain.Stage) error
    Get(ctx context.Context, workPlanID, stageID string) (*domain.Stage, error)
    Update(ctx context.Context, stage *domain.Stage) error
    List(ctx context.Context, workPlanID string, filter StageFilter) ([]*domain.Stage, error)
    UpdateState(ctx context.Context, workPlanID, stageID string, state StageState) error
    AddAttempt(ctx context.Context, workPlanID, stageID string, attempt *domain.Attempt) error
}

type DependencyRepository interface {
    Create(ctx context.Context, dep *domain.Dependency) error
    GetBySource(ctx context.Context, sourceType, sourceID string) ([]*domain.Dependency, error)
    GetByTarget(ctx context.Context, targetType, targetID string) ([]*domain.Dependency, error)
    MarkResolved(ctx context.Context, depID string, satisfied bool) error
}

type StageRunnerRepository interface {
    Create(ctx context.Context, runner *domain.StageRunner) error
    Get(ctx context.Context, registrationID string) (*domain.StageRunner, error)
    Delete(ctx context.Context, registrationID string) error
    List(ctx context.Context, runnerType string) ([]*domain.StageRunner, error)
    IncrementLoad(ctx context.Context, registrationID string) error
    DecrementLoad(ctx context.Context, registrationID string) error
    CleanupExpired(ctx context.Context) (int64, error)
}

type StageExecutionRepository interface {
    Create(ctx context.Context, exec *domain.StageExecution) error
    Get(ctx context.Context, executionID string) (*domain.StageExecution, error)
    GetByStageAttempt(ctx context.Context, workPlanID, stageID string, attemptIdx int) (*domain.StageExecution, error)
    ListPending(ctx context.Context, limit int) ([]*domain.StageExecution, error)
    MarkDispatched(ctx context.Context, execID, runnerID string) error
    MarkRunning(ctx context.Context, execID string) error
    MarkComplete(ctx context.Context, execID string) error
    MarkFailed(ctx context.Context, execID, errorMessage string) error
    UpdateProgress(ctx context.Context, execID string, percent int, message string) error
}

// UnitOfWork for transactional operations
type UnitOfWork interface {
    WorkPlans() WorkPlanRepository
    Checks() CheckRepository
    Stages() StageRepository
    Dependencies() DependencyRepository
    StageRunners() StageRunnerRepository
    StageExecutions() StageExecutionRepository
    Commit() error
    Rollback() error
}

type Storage interface {
    Begin(ctx context.Context) (UnitOfWork, error)
    Close() error
}
```

### 6. SQLite Implementation (`internal/storage/sqlite/`)
Concrete SQLite-based storage implementation.

**Schema Overview:**
```sql
-- Work Plans
CREATE TABLE work_plans (
    id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    version INTEGER NOT NULL DEFAULT 1
);

-- Checks
CREATE TABLE checks (
    id TEXT NOT NULL,
    work_plan_id TEXT NOT NULL REFERENCES work_plans(id),
    state INTEGER NOT NULL DEFAULT 10,  -- PLANNING
    kind TEXT,
    options_json TEXT,  -- JSON blob for simplicity
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (work_plan_id, id)
);

-- Check Results
CREATE TABLE check_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_plan_id TEXT NOT NULL,
    check_id TEXT NOT NULL,
    owner_type TEXT NOT NULL,  -- 'stage_attempt'
    owner_id TEXT NOT NULL,
    data_json TEXT,
    created_at DATETIME NOT NULL,
    finalized_at DATETIME,
    failure_message TEXT,
    FOREIGN KEY (work_plan_id, check_id) REFERENCES checks(work_plan_id, id)
);

-- Stages
CREATE TABLE stages (
    id TEXT NOT NULL,
    work_plan_id TEXT NOT NULL REFERENCES work_plans(id),
    state INTEGER NOT NULL DEFAULT 10,  -- PLANNED
    args_json TEXT,  -- Stage arguments as JSON
    execution_mode INTEGER DEFAULT 1,  -- 1=SYNC, 2=ASYNC
    runner_type TEXT,  -- Type of runner to dispatch to
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (work_plan_id, id)
);

-- Stage Attempts
CREATE TABLE stage_attempts (
    idx INTEGER NOT NULL,
    work_plan_id TEXT NOT NULL,
    stage_id TEXT NOT NULL,
    state INTEGER NOT NULL DEFAULT 0,  -- PENDING
    process_uid TEXT,
    details_json TEXT,
    progress_json TEXT,  -- Array of progress messages
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    failure_message TEXT,
    PRIMARY KEY (work_plan_id, stage_id, idx),
    FOREIGN KEY (work_plan_id, stage_id) REFERENCES stages(work_plan_id, id)
);

-- Stage Assignments (stage -> check responsibility)
CREATE TABLE stage_assignments (
    work_plan_id TEXT NOT NULL,
    stage_id TEXT NOT NULL,
    target_check_id TEXT NOT NULL,
    goal_state INTEGER NOT NULL,
    PRIMARY KEY (work_plan_id, stage_id, target_check_id),
    FOREIGN KEY (work_plan_id, stage_id) REFERENCES stages(work_plan_id, id),
    FOREIGN KEY (work_plan_id, target_check_id) REFERENCES checks(work_plan_id, id)
);

-- Dependencies (edges between nodes)
CREATE TABLE dependencies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_plan_id TEXT NOT NULL,
    source_type TEXT NOT NULL,  -- 'check' or 'stage'
    source_id TEXT NOT NULL,
    target_type TEXT NOT NULL,  -- 'check' or 'stage'
    target_id TEXT NOT NULL,
    predicate_type TEXT NOT NULL DEFAULT 'AND',  -- 'AND' or 'OR'
    resolved BOOLEAN DEFAULT FALSE,
    satisfied BOOLEAN,
    resolved_at DATETIME,
    UNIQUE(work_plan_id, source_type, source_id, target_type, target_id)
);

-- Stage Runners (registered execution workers)
CREATE TABLE stage_runners (
    registration_id TEXT PRIMARY KEY,
    id TEXT NOT NULL,  -- Runner's self-assigned ID
    runner_type TEXT NOT NULL,
    address TEXT NOT NULL,
    supported_modes_json TEXT NOT NULL,  -- JSON array of execution modes
    max_concurrent INTEGER DEFAULT 0,
    current_load INTEGER DEFAULT 0,
    metadata_json TEXT,
    registered_at DATETIME NOT NULL,
    last_heartbeat DATETIME NOT NULL,
    expires_at DATETIME NOT NULL
);

-- Stage Executions (execution queue)
CREATE TABLE stage_executions (
    id TEXT PRIMARY KEY,
    work_plan_id TEXT NOT NULL,
    stage_id TEXT NOT NULL,
    attempt_idx INTEGER NOT NULL,
    runner_type TEXT NOT NULL,
    execution_mode INTEGER DEFAULT 1,  -- 1=SYNC, 2=ASYNC
    state INTEGER DEFAULT 10,  -- 10=PENDING, 20=DISPATCHED, 30=RUNNING, 40=COMPLETE, 50=FAILED
    runner_id TEXT,  -- registration_id of assigned runner
    dispatched_at DATETIME,
    started_at DATETIME,
    completed_at DATETIME,
    deadline DATETIME,
    progress_percent INTEGER DEFAULT 0,
    progress_message TEXT,
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    FOREIGN KEY (work_plan_id) REFERENCES work_plans(id)
);

-- Indexes
CREATE INDEX idx_checks_work_plan ON checks(work_plan_id);
CREATE INDEX idx_checks_state ON checks(work_plan_id, state);
CREATE INDEX idx_stages_work_plan ON stages(work_plan_id);
CREATE INDEX idx_stages_state ON stages(work_plan_id, state);
CREATE INDEX idx_executions_pending ON stage_executions(state, runner_type) WHERE state = 10;
CREATE INDEX idx_runners_type ON stage_runners(runner_type);
CREATE INDEX idx_deps_source ON dependencies(work_plan_id, source_type, source_id);
CREATE INDEX idx_deps_target ON dependencies(work_plan_id, target_type, target_id);
```

### 7. Domain Types (`internal/domain/`)
Pure domain models independent of storage or transport.

```go
// Simplified state enums
type CheckState int
const (
    CheckStatePlanning CheckState = 10
    CheckStatePlanned  CheckState = 20
    CheckStateWaiting  CheckState = 30
    CheckStateFinal    CheckState = 40
)

type StageState int
const (
    StagePlanned       StageState = 10
    StageAttempting    StageState = 20
    StageAwaitingGroup StageState = 30
    StageFinal         StageState = 40
)

type AttemptState int
const (
    AttemptPending    AttemptState = 0
    AttemptScheduled  AttemptState = 10
    AttemptRunning    AttemptState = 20
    AttemptComplete   AttemptState = 30
    AttemptIncomplete AttemptState = 40
)
```

## Key Simplifications from Original TurboCI

| Feature | Original | TurboCI-Lite |
|---------|----------|--------------|
| Security | Realm-based, per-node | None (single context) |
| Dependencies | Complex predicates with thresholds | Simple AND/OR |
| Data storage | Typed Datum/Value with Any | JSON blobs |
| Versioning | Revision with timestamps | Simple integer version |
| Edit history | Full audit trail | None |
| Executors | Complex scheduling | Push-based dispatch to registered runners |
| Scaling | Distributed (Spanner/etc.) | Single SQLite instance |

## gRPC Service Definition (Simplified)

```protobuf
// Orchestrator service - main API for workflow management
service TurboCIOrchestrator {
    // Create a new empty WorkPlan
    rpc CreateWorkPlan(CreateWorkPlanRequest) returns (CreateWorkPlanResponse);

    // Atomically write/update multiple nodes
    rpc WriteNodes(WriteNodesRequest) returns (WriteNodesResponse);

    // Query nodes in the graph
    rpc QueryNodes(QueryNodesRequest) returns (QueryNodesResponse);

    // Get a specific WorkPlan
    rpc GetWorkPlan(GetWorkPlanRequest) returns (GetWorkPlanResponse);

    // Runner registration (runners call these)
    rpc RegisterStageRunner(RegisterRunnerRequest) returns (RegisterRunnerResponse);
    rpc UnregisterStageRunner(UnregisterRunnerRequest) returns (UnregisterRunnerResponse);
    rpc ListStageRunners(ListRunnersRequest) returns (ListRunnersResponse);

    // Async execution callbacks (runners call these)
    rpc UpdateStageExecution(UpdateExecutionRequest) returns (UpdateExecutionResponse);
    rpc CompleteStageExecution(CompleteExecutionRequest) returns (CompleteExecutionResponse);
}

// StageRunner service - implemented by stage runners
service StageRunner {
    // Synchronous execution - blocks until complete
    rpc Run(RunRequest) returns (RunResponse);

    // Asynchronous execution - returns immediately, runner calls back
    rpc RunAsync(RunAsyncRequest) returns (RunAsyncResponse);

    // Health check
    rpc Ping(PingRequest) returns (PingResponse);
}
```

## Data Flow

### Creating a WorkPlan with Checks and Stages

```
Client                    Endpoint              Service               Storage
   │                         │                     │                     │
   │ CreateWorkPlan ─────────►│                     │                     │
   │                         │ validate ───────────►│                     │
   │                         │                     │ Begin() ────────────►│
   │                         │                     │                     │ tx
   │                         │                     │ Create WorkPlan ────►│
   │                         │                     │◄────────────────────│
   │                         │                     │ Commit() ───────────►│
   │◄─────────────────────────│◄────────────────────│                     │
   │                         │                     │                     │
   │ WriteNodes(checks,stages)►│                     │                     │
   │                         │ validate ───────────►│                     │
   │                         │                     │ Begin() ────────────►│
   │                         │                     │ Create Checks ──────►│
   │                         │                     │ Create Stages ──────►│
   │                         │                     │ Create Dependencies ►│
   │                         │                     │ ResolveDeps() ───────│
   │                         │                     │ AdvanceStates() ─────│
   │                         │                     │ Commit() ───────────►│
   │◄─────────────────────────│◄────────────────────│                     │
```

### Dependency Resolution Flow

When a Check or Stage reaches FINAL state, the orchestrator:

1. Finds all dependencies where this node is the target
2. Marks those dependency edges as resolved
3. For each source node, checks if all dependencies are now satisfied
4. If satisfied, advances the source node's state (PLANNED->WAITING for Checks, PLANNED->ATTEMPTING for Stages)

## Error Handling

Errors are mapped to gRPC status codes at the endpoint layer:

| Error Type | gRPC Code |
|------------|-----------|
| Not found | NOT_FOUND |
| Invalid argument | INVALID_ARGUMENT |
| State transition error | FAILED_PRECONDITION |
| Concurrent modification | ABORTED |
| Internal error | INTERNAL |

## Configuration

```go
type Config struct {
    // Server
    GRPCPort int    `env:"GRPC_PORT" default:"50051"`

    // Storage
    SQLitePath string `env:"SQLITE_PATH" default:"turboci.db"`

    // Execution
    MaxAttempts     int           `env:"MAX_ATTEMPTS" default:"3"`
    AttemptTimeout  time.Duration `env:"ATTEMPT_TIMEOUT" default:"5m"`
}

type DispatcherConfig struct {
    PollInterval       time.Duration  // How often to poll for pending executions
    CleanupInterval    time.Duration  // How often to cleanup expired registrations
    StaleCheckInterval time.Duration  // How often to check for stale executions
    StaleDuration      time.Duration  // Time after which dispatched execution is stale
    DefaultTimeout     time.Duration  // Default execution timeout
    CallbackAddress    string         // Address runners use for callbacks
}
```

## Testing Strategy

1. **Unit Tests**: Each layer tested in isolation with mocks
2. **Integration Tests**: Full stack tests with file-based SQLite (WAL mode)
3. **End-to-End Tests** (`testing/e2e/`): Complete execution flow tests with MockRunner
   - Basic sync/async execution
   - Runner registration and lifecycle
   - Complex workflow patterns (diamond, fan-out/fan-in, chains)
   - Error handling (runner failures, timeouts, partial failures)
4. **Contract Tests**: Proto compatibility validation

## Future Extensibility

The storage layer abstraction allows for:
- PostgreSQL implementation for multi-node deployment
- Spanner implementation for global scale
- Redis-backed caching layer

The endpoint layer abstraction allows for:
- HTTP/REST gateway addition
- GraphQL endpoint
- WebSocket subscriptions for real-time updates
