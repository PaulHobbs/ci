# TurboCI-Lite Implementation Plan

This document outlines the step-by-step implementation plan for building TurboCI-Lite.

## Phase 1: Project Setup and Proto Definitions

### 1.1 Initialize Go Module
- Create `go.mod` with module path `github.com/example/turboci-lite`
- Add dependencies:
  - `google.golang.org/grpc`
  - `google.golang.org/protobuf`
  - `github.com/mattn/go-sqlite3`
  - `github.com/google/uuid`

### 1.2 Define Proto Files

**`proto/turboci/v1/common.proto`**
- CheckState enum (PLANNING, PLANNED, WAITING, FINAL)
- StageState enum (PLANNED, ATTEMPTING, AWAITING_GROUP, FINAL)
- AttemptState enum (PENDING, SCHEDULED, RUNNING, COMPLETE, INCOMPLETE)
- Timestamp message
- Dependency message with simple predicate (AND/OR)

**`proto/turboci/v1/workplan.proto`**
- WorkPlan message (id, created_at, version)
- CreateWorkPlanRequest/Response

**`proto/turboci/v1/check.proto`**
- Check message (id, work_plan_id, state, kind, options, results, dependencies)
- CheckResult message (id, owner_id, data, created_at, finalized_at, failure)
- CheckWrite message for WriteNodes

**`proto/turboci/v1/stage.proto`**
- Stage message (id, work_plan_id, state, args, attempts, assignments, dependencies)
- Attempt message (idx, state, process_uid, details, progress, failure)
- Assignment message (target_check_id, goal_state)
- StageWrite message for WriteNodes

**`proto/turboci/v1/service.proto`**
- TurboCIOrchestrator service definition
  - CreateWorkPlan
  - WriteNodes
  - QueryNodes
  - GetWorkPlan
- WriteNodesRequest/Response
- QueryNodesRequest/Response

### 1.3 Generate Go Code
- Set up `buf.gen.yaml` or Makefile for protoc generation
- Generate Go code to `gen/proto/turboci/v1/`

---

## Phase 2: Domain Layer

### 2.1 Domain Types (`internal/domain/`)

**`workplan.go`**
```go
type WorkPlan struct {
    ID        string
    CreatedAt time.Time
    UpdatedAt time.Time
    Version   int64
}
```

**`check.go`**
```go
type CheckState int

type Check struct {
    ID          string
    WorkPlanID  string
    State       CheckState
    Kind        string
    Options     map[string]any  // JSON-compatible
    Results     []CheckResult
    Dependencies []Dependency
    CreatedAt   time.Time
    UpdatedAt   time.Time
    Version     int64
}

type CheckResult struct {
    ID          int64
    OwnerType   string  // "stage_attempt"
    OwnerID     string  // "workplan:stage:attempt_idx"
    Data        map[string]any
    CreatedAt   time.Time
    FinalizedAt *time.Time
    Failure     *string
}
```

**`stage.go`**
```go
type StageState int
type AttemptState int

type Stage struct {
    ID          string
    WorkPlanID  string
    State       StageState
    Args        map[string]any
    Attempts    []Attempt
    Assignments []Assignment
    Dependencies []Dependency
    CreatedAt   time.Time
    UpdatedAt   time.Time
    Version     int64
}

type Attempt struct {
    Idx         int
    State       AttemptState
    ProcessUID  string
    Details     map[string]any
    Progress    []ProgressEntry
    CreatedAt   time.Time
    UpdatedAt   time.Time
    Failure     *string
}

type ProgressEntry struct {
    Message   string
    Timestamp time.Time
    Details   map[string]any
}

type Assignment struct {
    TargetCheckID string
    GoalState     CheckState
}
```

**`dependency.go`**
```go
type NodeType string
const (
    NodeTypeCheck NodeType = "check"
    NodeTypeStage NodeType = "stage"
)

type PredicateType string
const (
    PredicateAND PredicateType = "AND"
    PredicateOR  PredicateType = "OR"
)

type Dependency struct {
    ID            int64
    WorkPlanID    string
    SourceType    NodeType
    SourceID      string
    TargetType    NodeType
    TargetID      string
    PredicateType PredicateType
    Resolved      bool
    Satisfied     *bool
    ResolvedAt    *time.Time
}
```

### 2.2 Domain Errors
```go
var (
    ErrNotFound           = errors.New("not found")
    ErrInvalidState       = errors.New("invalid state transition")
    ErrConcurrentModify   = errors.New("concurrent modification")
    ErrInvalidDependency  = errors.New("invalid dependency")
    ErrCyclicDependency   = errors.New("cyclic dependency detected")
)
```

---

## Phase 3: Storage Layer

### 3.1 Repository Interfaces (`internal/storage/repository.go`)

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
    List(ctx context.Context, workPlanID string, opts ListOptions) ([]*domain.Check, error)
    AddResult(ctx context.Context, workPlanID, checkID string, result *domain.CheckResult) error
}

type StageRepository interface {
    Create(ctx context.Context, stage *domain.Stage) error
    Get(ctx context.Context, workPlanID, stageID string) (*domain.Stage, error)
    Update(ctx context.Context, stage *domain.Stage) error
    List(ctx context.Context, workPlanID string, opts ListOptions) ([]*domain.Stage, error)
    AddAttempt(ctx context.Context, workPlanID, stageID string, attempt *domain.Attempt) error
    UpdateAttempt(ctx context.Context, workPlanID, stageID string, idx int, attempt *domain.Attempt) error
}

type DependencyRepository interface {
    Create(ctx context.Context, dep *domain.Dependency) error
    GetBySource(ctx context.Context, workPlanID string, sourceType domain.NodeType, sourceID string) ([]*domain.Dependency, error)
    GetByTarget(ctx context.Context, workPlanID string, targetType domain.NodeType, targetID string) ([]*domain.Dependency, error)
    MarkResolved(ctx context.Context, id int64, satisfied bool) error
}

type UnitOfWork interface {
    WorkPlans() WorkPlanRepository
    Checks() CheckRepository
    Stages() StageRepository
    Dependencies() DependencyRepository
    Commit() error
    Rollback() error
}

type Storage interface {
    Begin(ctx context.Context) (UnitOfWork, error)
    Close() error
    Migrate(ctx context.Context) error
}
```

### 3.2 SQLite Implementation (`internal/storage/sqlite/`)

**`sqlite.go`** - Storage factory and connection management
```go
type SQLiteStorage struct {
    db *sql.DB
}

func New(path string) (*SQLiteStorage, error)
func (s *SQLiteStorage) Begin(ctx context.Context) (storage.UnitOfWork, error)
func (s *SQLiteStorage) Close() error
func (s *SQLiteStorage) Migrate(ctx context.Context) error
```

**`migrations.go`** - Schema creation
- Table definitions as per ARCHITECTURE.md
- Index creation
- Version tracking

**`workplan.go`** - WorkPlan queries
**`check.go`** - Check queries with JSON handling
**`stage.go`** - Stage and Attempt queries
**`dependency.go`** - Dependency queries

**`unit_of_work.go`** - Transaction wrapper
```go
type unitOfWork struct {
    tx          *sql.Tx
    workPlans   *workPlanRepo
    checks      *checkRepo
    stages      *stageRepo
    dependencies *dependencyRepo
}
```

---

## Phase 4: Service Layer

### 4.1 Orchestrator Service (`internal/service/orchestrator.go`)

```go
type OrchestratorService struct {
    storage storage.Storage
}

func NewOrchestrator(storage storage.Storage) *OrchestratorService

// Main operations
func (s *OrchestratorService) CreateWorkPlan(ctx context.Context, req *CreateWorkPlanRequest) (*WorkPlan, error)
func (s *OrchestratorService) WriteNodes(ctx context.Context, req *WriteNodesRequest) (*WriteNodesResponse, error)
func (s *OrchestratorService) QueryNodes(ctx context.Context, req *QueryNodesRequest) (*QueryNodesResponse, error)
func (s *OrchestratorService) GetWorkPlan(ctx context.Context, id string) (*WorkPlan, error)
```

### 4.2 Check Logic (`internal/service/check.go`)

```go
// Check state machine validation
func (s *OrchestratorService) validateCheckStateTransition(current, next domain.CheckState) error
func (s *OrchestratorService) advanceCheckState(ctx context.Context, uow storage.UnitOfWork, check *domain.Check) error
func (s *OrchestratorService) createCheck(ctx context.Context, uow storage.UnitOfWork, write *CheckWrite) error
func (s *OrchestratorService) updateCheck(ctx context.Context, uow storage.UnitOfWork, write *CheckWrite) error
```

### 4.3 Stage Logic (`internal/service/stage.go`)

```go
// Stage state machine
func (s *OrchestratorService) validateStageStateTransition(current, next domain.StageState) error
func (s *OrchestratorService) advanceStageState(ctx context.Context, uow storage.UnitOfWork, stage *domain.Stage) error
func (s *OrchestratorService) createStage(ctx context.Context, uow storage.UnitOfWork, write *StageWrite) error
func (s *OrchestratorService) updateStage(ctx context.Context, uow storage.UnitOfWork, write *StageWrite) error
func (s *OrchestratorService) createAttempt(ctx context.Context, uow storage.UnitOfWork, stage *domain.Stage) error
```

### 4.4 Dependency Resolution (`internal/service/dependency.go`)

```go
// Core dependency resolution
func (s *OrchestratorService) resolveDependencies(ctx context.Context, uow storage.UnitOfWork, workPlanID string) error
func (s *OrchestratorService) checkDependencySatisfied(deps []*domain.Dependency) (satisfied bool, resolved bool)
func (s *OrchestratorService) detectCycles(ctx context.Context, uow storage.UnitOfWork, workPlanID string) error
func (s *OrchestratorService) propagateResolution(ctx context.Context, uow storage.UnitOfWork, node domain.NodeType, nodeID string) error
```

---

## Phase 5: Endpoint Layer

### 5.1 Endpoint Definitions (`internal/endpoint/endpoint.go`)

```go
type Endpoint func(ctx context.Context, request any) (response any, err error)

type Endpoints struct {
    CreateWorkPlan Endpoint
    WriteNodes     Endpoint
    QueryNodes     Endpoint
    GetWorkPlan    Endpoint
}

func MakeEndpoints(svc *service.OrchestratorService) Endpoints
```

### 5.2 Validation (`internal/endpoint/validation.go`)

```go
func ValidateCreateWorkPlanRequest(req *CreateWorkPlanRequest) error
func ValidateWriteNodesRequest(req *WriteNodesRequest) error
func ValidateQueryNodesRequest(req *QueryNodesRequest) error
func ValidateCheckWrite(write *CheckWrite) error
func ValidateStageWrite(write *StageWrite) error
```

### 5.3 Error Mapping

```go
func MapErrorToStatus(err error) *status.Status {
    switch {
    case errors.Is(err, domain.ErrNotFound):
        return status.New(codes.NotFound, err.Error())
    case errors.Is(err, domain.ErrInvalidState):
        return status.New(codes.FailedPrecondition, err.Error())
    case errors.Is(err, domain.ErrConcurrentModify):
        return status.New(codes.Aborted, err.Error())
    default:
        return status.New(codes.Internal, "internal error")
    }
}
```

---

## Phase 6: Transport Layer

### 6.1 gRPC Server (`internal/transport/grpc/server.go`)

```go
type Server struct {
    pb.UnimplementedTurboCIOrchestratorServer
    endpoints endpoint.Endpoints
}

func NewServer(endpoints endpoint.Endpoints) *Server
func (s *Server) Serve(addr string) error
func (s *Server) GracefulStop()
```

### 6.2 Handlers (`internal/transport/grpc/handlers.go`)

```go
func (s *Server) CreateWorkPlan(ctx context.Context, req *pb.CreateWorkPlanRequest) (*pb.CreateWorkPlanResponse, error)
func (s *Server) WriteNodes(ctx context.Context, req *pb.WriteNodesRequest) (*pb.WriteNodesResponse, error)
func (s *Server) QueryNodes(ctx context.Context, req *pb.QueryNodesRequest) (*pb.QueryNodesResponse, error)
func (s *Server) GetWorkPlan(ctx context.Context, req *pb.GetWorkPlanRequest) (*pb.GetWorkPlanResponse, error)

// Proto <-> Domain conversions
func checkToProto(check *domain.Check) *pb.Check
func checkFromProto(pb *pb.Check) *domain.Check
func stageToProto(stage *domain.Stage) *pb.Stage
func stageFromProto(pb *pb.Stage) *domain.Stage
```

### 6.3 Interceptors

```go
func LoggingInterceptor() grpc.UnaryServerInterceptor
func RecoveryInterceptor() grpc.UnaryServerInterceptor
```

---

## Phase 7: Main Entry Point

### 7.1 Server Main (`cmd/server/main.go`)

```go
func main() {
    // Parse config from env
    cfg := loadConfig()

    // Initialize storage
    store, err := sqlite.New(cfg.SQLitePath)
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()

    // Run migrations
    if err := store.Migrate(context.Background()); err != nil {
        log.Fatal(err)
    }

    // Create service
    svc := service.NewOrchestrator(store)

    // Create endpoints
    endpoints := endpoint.MakeEndpoints(svc)

    // Create and start gRPC server
    server := grpc.NewServer(endpoints)
    log.Printf("Starting gRPC server on :%d", cfg.GRPCPort)
    if err := server.Serve(fmt.Sprintf(":%d", cfg.GRPCPort)); err != nil {
        log.Fatal(err)
    }
}
```

---

## Phase 8: Testing

### 8.1 Unit Tests

**Domain tests:**
- State machine transition validation
- Dependency predicate evaluation

**Service tests:**
- Mock storage layer
- Test WriteNodes with various check/stage combinations
- Test dependency resolution scenarios

**Endpoint tests:**
- Validation logic
- Error mapping

### 8.2 Integration Tests

**Full stack tests with in-memory SQLite:**
- Create WorkPlan -> WriteNodes -> QueryNodes workflow
- Multi-stage dependency chains
- Concurrent modification scenarios

```go
func TestCreateWorkPlanFlow(t *testing.T) {
    store, _ := sqlite.New(":memory:")
    store.Migrate(context.Background())
    svc := service.NewOrchestrator(store)

    // Create workplan
    wp, err := svc.CreateWorkPlan(ctx, &CreateWorkPlanRequest{})
    require.NoError(t, err)

    // Write checks and stages
    _, err = svc.WriteNodes(ctx, &WriteNodesRequest{
        WorkPlanID: wp.ID,
        Checks: []*CheckWrite{{
            ID:    "build-check",
            State: domain.CheckStatePlanning,
        }},
        Stages: []*StageWrite{{
            ID: "build-stage",
            Assignments: []*Assignment{{
                TargetCheckID: "build-check",
                GoalState:     domain.CheckStateFinal,
            }},
        }},
    })
    require.NoError(t, err)

    // Query and verify
    result, err := svc.QueryNodes(ctx, &QueryNodesRequest{
        WorkPlanID: wp.ID,
    })
    require.NoError(t, err)
    require.Len(t, result.Checks, 1)
    require.Len(t, result.Stages, 1)
}
```

### 8.3 Benchmark Tests

- Measure WriteNodes throughput
- Measure dependency resolution performance with large graphs

---

## Implementation Order Summary

| Phase | Description | Dependencies |
|-------|-------------|--------------|
| 1 | Project setup, proto definitions | None |
| 2 | Domain types and errors | Phase 1 |
| 3 | Storage interfaces and SQLite impl | Phase 2 |
| 4 | Service layer (orchestration logic) | Phase 2, 3 |
| 5 | Endpoint layer (validation, error mapping) | Phase 4 |
| 6 | Transport layer (gRPC server) | Phase 1, 5 |
| 7 | Main entry point | All above |
| 8 | Testing | All above |

## File Creation Checklist

```
[ ] go.mod
[ ] buf.gen.yaml (or Makefile for protoc)
[ ] proto/turboci/v1/common.proto
[ ] proto/turboci/v1/workplan.proto
[ ] proto/turboci/v1/check.proto
[ ] proto/turboci/v1/stage.proto
[ ] proto/turboci/v1/service.proto
[ ] internal/domain/workplan.go
[ ] internal/domain/check.go
[ ] internal/domain/stage.go
[ ] internal/domain/dependency.go
[ ] internal/domain/errors.go
[ ] internal/storage/repository.go
[ ] internal/storage/sqlite/sqlite.go
[ ] internal/storage/sqlite/migrations.go
[ ] internal/storage/sqlite/workplan.go
[ ] internal/storage/sqlite/check.go
[ ] internal/storage/sqlite/stage.go
[ ] internal/storage/sqlite/dependency.go
[ ] internal/storage/sqlite/unit_of_work.go
[ ] internal/service/orchestrator.go
[ ] internal/service/check.go
[ ] internal/service/stage.go
[ ] internal/service/dependency.go
[ ] internal/endpoint/endpoint.go
[ ] internal/endpoint/validation.go
[ ] internal/endpoint/workplan.go
[ ] internal/endpoint/check.go
[ ] internal/endpoint/stage.go
[ ] internal/transport/grpc/server.go
[ ] internal/transport/grpc/handlers.go
[ ] internal/transport/grpc/interceptors.go
[ ] cmd/server/main.go
[ ] pkg/id/generator.go
```

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Complex dependency resolution bugs | Extensive unit tests, visualization tools |
| SQLite concurrency limits | Use WAL mode, serialize writes, document limitations |
| Proto compatibility issues | Keep protos simple, avoid oneofs initially |
| State machine edge cases | Comprehensive state transition matrix tests |
