---
name: turboci-lite
description: Documentation for the TurboCI-Lite workflow orchestration service. Use when working with turboci-lite code, understanding its architecture, modifying the gRPC service, working with workflow stages/checks, or debugging the orchestrator.
---

# TurboCI-Lite Service

TurboCI-Lite is a simplified, monolithic gRPC-based workflow orchestration service written in Go. It manages CI/CD pipelines by orchestrating a directed graph of work items called Checks (objectives) and Stages (executable units).

## Architecture Overview

The system follows a layered architecture (Goa-inspired):

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

## Directory Structure

```
turboci-lite/
├── cmd/server/main.go          # Server entry point
├── internal/
│   ├── domain/                 # Pure domain models
│   │   ├── workplan.go         # WorkPlan entity
│   │   ├── check.go            # Check, CheckResult, CheckState
│   │   ├── stage.go            # Stage, Attempt, AttemptState
│   │   ├── dependency.go       # Dependency, DependencyGroup
│   │   └── errors.go           # Domain errors
│   ├── storage/                # Repository interfaces
│   │   ├── repository.go       # Abstract interfaces
│   │   └── sqlite/             # SQLite implementation
│   │       ├── sqlite.go       # Connection management
│   │       ├── migrations.go   # Schema definitions
│   │       ├── workplan.go     # WorkPlan queries
│   │       ├── check.go        # Check queries
│   │       ├── stage.go        # Stage queries
│   │       └── dependency.go   # Dependency queries
│   ├── service/
│   │   └── orchestrator.go     # Core business logic
│   ├── endpoint/
│   │   └── endpoint.go         # Validation, error mapping
│   └── transport/grpc/
│       ├── server.go           # gRPC server setup
│       └── handlers.go         # Proto ↔ Domain conversion
├── proto/turboci/v1/           # Protocol buffer definitions
│   ├── service.proto           # gRPC service definition
│   ├── workplan.proto          # WorkPlan messages
│   ├── check.proto             # Check messages
│   ├── stage.proto             # Stage messages
│   └── common.proto            # Shared enums
├── client/
│   └── pipeline.go             # High-level pipeline runner
├── pkg/id/
│   └── generator.go            # UUID generation
├── go.mod
└── Makefile
```

## gRPC API

The service exposes 4 RPCs:

### CreateWorkPlan
Creates a new empty WorkPlan.

```protobuf
rpc CreateWorkPlan(CreateWorkPlanRequest) returns (CreateWorkPlanResponse)
```

### GetWorkPlan
Retrieves a WorkPlan by ID.

```protobuf
rpc GetWorkPlan(GetWorkPlanRequest) returns (GetWorkPlanResponse)
```

### WriteNodes
Atomically writes/updates multiple Checks and Stages. This is the primary method for building and modifying workflows.

```protobuf
rpc WriteNodes(WriteNodesRequest) returns (WriteNodesResponse)
```

### QueryNodes
Queries nodes with filtering by state, type, etc.

```protobuf
rpc QueryNodes(QueryNodesRequest) returns (QueryNodesResponse)
```

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

## Example: Creating a Pipeline

```go
// Create work plan
wp, _ := orchestrator.CreateWorkPlan(ctx, &CreateWorkPlanRequest{})

// Write planning check and stage
orchestrator.WriteNodes(ctx, &WriteNodesRequest{
    WorkPlanID: wp.ID,
    Checks: []*CheckWrite{{
        ID:    "plan-check",
        State: CheckStatePlanning,
    }},
    Stages: []*StageWrite{{
        ID: "plan-stage",
        Assignments: []Assignment{{
            TargetCheckID: "plan-check",
            GoalState:     CheckStateFinal,
        }},
    }},
})

// Write build check with dependency on planning
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
        ID: "build-stage",
        Assignments: []Assignment{{
            TargetCheckID: "build-check",
            GoalState:     CheckStateFinal,
        }},
    }},
})
```

## Testing with grpcurl

```bash
# List services
grpcurl -plaintext localhost:50051 list

# Create a work plan
grpcurl -plaintext -d '{}' localhost:50051 turboci.v1.TurboCIOrchestrator/CreateWorkPlan

# Get a work plan
grpcurl -plaintext -d '{"id":"<work-plan-id>"}' localhost:50051 turboci.v1.TurboCIOrchestrator/GetWorkPlan
```

## Error Handling

Domain errors are mapped to gRPC status codes:
- `ErrNotFound` → `codes.NotFound`
- `ErrInvalidState` → `codes.FailedPrecondition`
- `ErrConcurrentModify` → `codes.Aborted`
- `ErrInvalidArgument` → `codes.InvalidArgument`
- `ErrAlreadyExists` → `codes.AlreadyExists`

## Database Schema

Core tables (in `internal/storage/sqlite/migrations.go`):
- `work_plans` - Workflow containers
- `checks` - Non-executable objectives
- `stages` - Executable work units
- `stage_attempts` - Execution attempts
- `check_results` - Results from attempts
- `dependencies` - Graph edges between nodes

SQLite is configured with WAL mode for better concurrency.

## Design Decisions

**Simplifications from full TurboCI:**
- No realm-based security (single security context)
- Simple AND/OR predicates (no complex thresholds)
- JSON storage for options/results
- No edit history tracking
- Single SQLite instance (not distributed)
- In-process execution via PipelineRunner

**Trade-offs:**
- Easy to deploy and understand
- Single-machine scalability limit
- No multi-tenant isolation
- No audit trail
