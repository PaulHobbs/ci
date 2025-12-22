# State Machines Reference

Detailed state machine documentation for TurboCI-Lite entities.

## Check State Machine

```
┌──────────────┐
│   PLANNING   │ (10) - Initial state, check is being defined
└──────┬───────┘
       │
       ▼
┌──────────────┐
│   PLANNED    │ (20) - Check definition complete, ready to wait
└──────┬───────┘
       │
       ▼
┌──────────────┐
│   WAITING    │ (30) - Waiting for dependencies to be satisfied
└──────┬───────┘
       │
       ▼
┌──────────────┐
│    FINAL     │ (40) - Check complete (terminal state)
└──────────────┘
```

### Valid Check Transitions

| From | To | When |
|------|-----|------|
| PLANNING | PLANNED | Check definition is complete |
| PLANNING | FINAL | Check has no dependencies (immediate completion) |
| PLANNED | WAITING | Check has dependencies to wait on |
| PLANNED | FINAL | All dependencies already satisfied |
| WAITING | FINAL | All dependencies satisfied |

### Check State Code

Located in `internal/domain/check.go`:

```go
type CheckState int

const (
    CheckStatePlanning CheckState = 10
    CheckStatePlanned  CheckState = 20
    CheckStateWaiting  CheckState = 30
    CheckStateFinal    CheckState = 40
)

func (s CheckState) CanTransitionTo(target CheckState) bool {
    switch s {
    case CheckStatePlanning:
        return target == CheckStatePlanned || target == CheckStateFinal
    case CheckStatePlanned:
        return target == CheckStateWaiting || target == CheckStateFinal
    case CheckStateWaiting:
        return target == CheckStateFinal
    case CheckStateFinal:
        return false // Terminal state
    }
    return false
}
```

## Stage State Machine

```
┌──────────────┐
│   PLANNED    │ (10) - Stage is planned, ready for execution
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  ATTEMPTING  │ (20) - Stage is currently being executed
└──────┬───────┘
       │
       ▼
┌──────────────┐
│AWAITING_GROUP│ (30) - Waiting for related stages in group
└──────┬───────┘
       │
       ▼
┌──────────────┐
│    FINAL     │ (40) - Stage complete (terminal state)
└──────────────┘
```

### Valid Stage Transitions

| From | To | When |
|------|-----|------|
| PLANNED | ATTEMPTING | Execution begins |
| PLANNED | FINAL | No execution needed (skip) |
| ATTEMPTING | AWAITING_GROUP | Waiting for group members |
| ATTEMPTING | FINAL | Execution complete |
| AWAITING_GROUP | FINAL | Group execution complete |

### Stage State Code

Located in `internal/domain/stage.go`:

```go
type StageState int

const (
    StageStatePlanned       StageState = 10
    StageStateAttempting    StageState = 20
    StageStateAwaitingGroup StageState = 30
    StageStateFinal         StageState = 40
)

func (s StageState) CanTransitionTo(target StageState) bool {
    switch s {
    case StageStatePlanned:
        return target == StageStateAttempting || target == StageStateFinal
    case StageStateAttempting:
        return target == StageStateAwaitingGroup || target == StageStateFinal
    case StageStateAwaitingGroup:
        return target == StageStateFinal
    case StageStateFinal:
        return false // Terminal state
    }
    return false
}
```

## Attempt State Machine

```
┌──────────────┐
│   PENDING    │ (10) - Attempt created, not yet scheduled
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  SCHEDULED   │ (20) - Attempt scheduled for execution
└──────┬───────┘
       │
       ▼
┌──────────────┐
│   RUNNING    │ (30) - Attempt actively executing
└──────┬───────┘
       │
       ├─────────────────┐
       ▼                 ▼
┌──────────────┐  ┌──────────────┐
│   COMPLETE   │  │  INCOMPLETE  │
│     (40)     │  │     (50)     │
└──────────────┘  └──────────────┘
   (success)        (failure)
```

### Valid Attempt Transitions

| From | To | When |
|------|-----|------|
| PENDING | SCHEDULED | Attempt is scheduled |
| PENDING | RUNNING | Immediate execution (skip scheduling) |
| PENDING | INCOMPLETE | Cancelled before start |
| SCHEDULED | RUNNING | Execution begins |
| SCHEDULED | INCOMPLETE | Cancelled or failed to start |
| RUNNING | COMPLETE | Execution succeeded |
| RUNNING | INCOMPLETE | Execution failed or cancelled |

### Attempt State Code

Located in `internal/domain/stage.go`:

```go
type AttemptState int

const (
    AttemptStatePending    AttemptState = 10
    AttemptStateScheduled  AttemptState = 20
    AttemptStateRunning    AttemptState = 30
    AttemptStateComplete   AttemptState = 40
    AttemptStateIncomplete AttemptState = 50
)

func (s AttemptState) CanTransitionTo(target AttemptState) bool {
    switch s {
    case AttemptStatePending:
        return target == AttemptStateScheduled ||
               target == AttemptStateRunning ||
               target == AttemptStateIncomplete
    case AttemptStateScheduled:
        return target == AttemptStateRunning ||
               target == AttemptStateIncomplete
    case AttemptStateRunning:
        return target == AttemptStateComplete ||
               target == AttemptStateIncomplete
    case AttemptStateComplete, AttemptStateIncomplete:
        return false // Terminal states
    }
    return false
}
```

## Execution State Machine

Stage executions represent dispatched work to stage runners.

```
┌──────────────┐
│   PENDING    │ (10) - Execution created, waiting for dispatch
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  DISPATCHED  │ (20) - Sent to runner, awaiting response
└──────┬───────┘
       │
       ▼
┌──────────────┐
│   RUNNING    │ (30) - Actively executing on runner
└──────┬───────┘
       │
       ├─────────────────┐
       ▼                 ▼
┌──────────────┐  ┌──────────────┐
│   COMPLETE   │  │    FAILED    │
│     (40)     │  │     (50)     │
└──────────────┘  └──────────────┘
   (success)        (failure)
```

### Valid Execution Transitions

| From | To | When |
|------|-----|------|
| PENDING | DISPATCHED | Runner selected, request sent |
| DISPATCHED | RUNNING | Runner acknowledges (async) or starts (sync) |
| DISPATCHED | FAILED | Runner unavailable or dispatch failed |
| RUNNING | COMPLETE | Execution succeeded |
| RUNNING | FAILED | Execution failed or timed out |

### Execution State Code

Located in `internal/domain/execution.go`:

```go
type ExecutionState int

const (
    ExecutionStatePending    ExecutionState = 10
    ExecutionStateDispatched ExecutionState = 20
    ExecutionStateRunning    ExecutionState = 30
    ExecutionStateComplete   ExecutionState = 40
    ExecutionStateFailed     ExecutionState = 50
)

func (s ExecutionState) IsFinal() bool {
    return s == ExecutionStateComplete || s == ExecutionStateFailed
}
```

### Execution Modes

```go
type ExecutionMode int

const (
    ExecutionModeSync  ExecutionMode = 1  // Dispatcher blocks until complete
    ExecutionModeAsync ExecutionMode = 2  // Runner calls back when done
)
```

## Dependency Resolution

When a node reaches FINAL state:
1. Find all dependencies where this node is the target
2. Mark those dependency edges as resolved
3. For each source node of those dependencies:
   - Evaluate if all dependencies are now satisfied (AND) or any (OR)
   - If satisfied, advance the source node's state

### Predicate Types

```go
type PredicateType int

const (
    PredicateAND PredicateType = 1  // All must be satisfied
    PredicateOR  PredicateType = 2  // At least one must be satisfied
)
```

### Satisfaction Logic

Located in `internal/domain/dependency.go`:

```go
func (g *DependencyGroup) IsSatisfied() bool {
    if len(g.Dependencies) == 0 {
        return true
    }

    switch g.Predicate {
    case PredicateAND:
        for _, dep := range g.Dependencies {
            if !dep.Resolved {
                return false
            }
        }
        return true
    case PredicateOR:
        for _, dep := range g.Dependencies {
            if dep.Resolved {
                return true
            }
        }
        return false
    }
    return false
}
```
