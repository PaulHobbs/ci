# TurboCI-Lite

TurboCI-Lite is a simplified, monolithic gRPC Go server that implements the core workflow orchestration concepts of the TurboCI system. It is designed to manage directed graphs of **Checks** (objectives) and **Stages** (executable units), tracking dependencies and coordinating execution through defined state machines.

Unlike distributed CI systems, TurboCI-Lite uses a **push-based execution model** where a central orchestrator dispatches work to registered stage runners, backed by a single **SQLite** database for persistence.

## Key Features

- **Workflow Orchestration**: Manage complex dependency graphs with `WorkPlans`.
- **State Management**: Robust state machines for Checks (`PLANNING` -> `PLANNED` -> `WAITING` -> `FINAL`) and Stages (`PLANNED` -> `ATTEMPTING` -> `FINAL`).
- **Push-Based Dispatch**: A background dispatcher sends work to registered long-lived runners (supporting both Sync and Async execution).
- **Flexible Dependencies**: Support for AND/OR logic predicates between nodes.
- **Layered Architecture**: Clean separation of concerns (Transport, Endpoint, Service, Storage) inspired by Goa.
- **SQLite Persistence**: Simple, portable, and transactional storage.
- **Culprit Finder**: Includes a specialized tool for identifying breaking commits using non-adaptive group testing.

## Components

### 1. Orchestrator Service
The core service that manages the lifecycle of:
- **WorkPlan**: The container for a workflow.
- **Check**: A named objective (e.g., "Tests Passed").
- **Stage**: An executable unit (e.g., "Run Unit Tests").

### 2. Stage Runners
Workers that register with the Orchestrator to execute Stages. They can be:
- **Sync**: Block until completion.
- **Async**: Return immediately and provide progress/completion updates via callbacks.

### 3. Culprit Finder
Located in `culprit/` and `cmd/culprit-finder/`, this is a standalone tool that uses **d-disjunct matrices** and **majority voting** to efficiently find multiple culprit commits in a range, even in the presence of flaky tests.

### 4. Deployment Pipeline Client
Located in `client_deploy/`, this component demonstrates a "Canary Release" workflow (Artifact Verification -> Canary Deploy -> Monitor -> Production Promote) using the TurboCI-Lite engine.

## Architecture

TurboCI-Lite follows a clean, layered architecture:

```
┌─────────────┐       ┌─────────────┐       ┌─────────────┐
│  Transport  │ ◄───► │  Endpoint   │ ◄───► │   Service   │
│   (gRPC)    │       │ (Validation)│       │   (Logic)   │
└─────────────┘       └─────────────┘       └──────┬──────┘
                                                   │
                                            ┌──────▼──────┐
                                            │   Storage   │
                                            │   (SQLite)  │
                                            └─────────────┘
```

See [ARCHITECTURE.md](ARCHITECTURE.md) for a deep dive into the system design, state machines, and data flow.

## Directory Structure

```
turboci-lite/
├── cmd/
│   ├── server/          # Main Orchestrator server entry point
│   └── culprit-finder/  # CLI tool for finding breaking commits
├── internal/
│   ├── service/         # Business logic (Orchestrator, Dispatcher, Runner)
│   ├── storage/         # SQLite implementation
│   ├── endpoint/        # Request/Response handling
│   └── transport/       # gRPC server definition
├── proto/               # Protobuf definitions
├── culprit/             # Culprit finder core logic
├── client_deploy/       # Example deployment pipeline client
└── testing/             # E2E and integration tests
```

## Getting Started

### Prerequisites
- Go 1.21+
- Make
- Protoc (for generating code)

### Running the Server

```bash
# Start the TurboCI-Lite server
go run cmd/server/main.go
```

The server will initialize a SQLite database (`turboci.db` by default) and listen on port `50051`.

### Running Tests

TurboCI-Lite has a comprehensive test suite including E2E integration tests.

```bash
# Run all tests
go test ./...

# Run E2E tests specifically
go test ./testing/e2e/...
```

## Documentation

- **[ARCHITECTURE.md](ARCHITECTURE.md)**: Detailed system architecture, data models, and state diagrams.
- **[IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md)**: Step-by-step implementation history and roadmap.
- **[testing/TESTING_PLAN.md](testing/TESTING_PLAN.md)**: Strategy for E2E and integration testing.
- **[culprit/README.md](culprit/README.md)**: Documentation for the Culprit Finder algorithm and library.
- **[cmd/culprit-finder/README.md](cmd/culprit-finder/README.md)**: User guide for the Culprit Finder CLI tool.
- **[client_deploy/DESIGN.md](client_deploy/DESIGN.md)**: Design of the example deployment pipeline.
