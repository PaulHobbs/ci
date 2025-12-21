# Design of the Deployment Pipeline Client

This document outlines the design and purpose of the `client_deploy/` directory and the Critical User Journeys (CUJs) covered by its integration tests.

## 1. Overview

The `client_deploy/` directory contains a client implementation for a **deployment pipeline**. Its purpose is to demonstrate a different workflow pattern using the same underlying `turboci-lite` orchestration engine, contrasting it with the pre-submit CI/testing workflow found in the main `client/` directory.

This client models a typical **canary release process**:
1.  An artifact is deployed to a small "canary" environment.
2.  The canary deployment is monitored for health and correctness.
3.  If the canary is deemed healthy, the deployment is promoted to the main "production" environment.

## 2. Architecture & Components

The client is designed to be self-contained and testable, with clear separation between the pipeline logic and external services.

- **`deploy_pipeline.go`**: Contains the core logic for the `DeployPipelineRunner`. This component is responsible for receiving a deployment request, creating a `WorkPlan` in `turboci-lite`, defining the graph of stages and dependencies, and executing the workflow.

- **`fakes_deploy.go`**: Provides in-memory fake implementations of all external services required by the pipeline. This enables robust integration testing without any real external dependencies. The fakes include:
    - `FakeArtifactRegistry`: Simulates fetching deployable artifacts.
    - `FakeDeploymentService`: Simulates deploying to canary/production and handling rollbacks.
    - `FakeMonitoringService`: Simulates checking canary health metrics.
    - `FakeNotificationService`: Simulates sending status updates to a chat channel.

- **`deploy_pipeline_test.go`**: Contains the integration tests that verify the CUJs listed below. These tests run the `DeployPipelineRunner` against the fake services and a real in-memory `turboci-lite` instance (using SQLite).

## 3. Pipeline Workflow

The deployment pipeline is executed as a `WorkPlan` within `turboci-lite`. The graph is defined with `Checks` (goals) and `Stages` (actions) with dependencies ensuring they run in the correct order.

1.  **Trigger**: The pipeline is initiated by a `DeploymentRequest`, which specifies the artifact to deploy.
2.  **Artifact Fetch**: The pipeline's first action is to verify the existence of the requested artifact.
3.  **Canary Deployment**: A stage deploys the artifact to the "canary" environment.
4.  **Canary Monitoring**: A subsequent stage, dependent on the successful canary deployment, runs health checks.
5.  **Promotion or Rollback**:
    - If the canary is healthy, a final stage promotes the artifact to the "production" environment.
    - If the canary is unhealthy, the pipeline stops, a rollback is initiated, and a failure is reported.
6.  **Notification**: The pipeline sends notifications to a specified channel upon success or failure.

## 4. Tested Critical User Journeys (CUJs)

The integration tests in `deploy_pipeline_test.go` cover the following critical user journeys.

### CUJ 1: Successful End-to-End Deployment (Happy Path)
- **Description**: A new, valid artifact version is deployed successfully through the canary and production environments. This represents the ideal workflow.
- **Covered by**: `TestSuccessfulDeployment`
- **Key Verifications**:
    - The artifact is correctly fetched.
    - The artifact is deployed to the `canary` environment.
    - The canary health checks pass.
    - The artifact is subsequently promoted to the `production` environment.
    - Final success notifications are sent.

### CUJ 2: Canary Health Check Failure and Rollback
- **Description**: An artifact is deployed to canary but fails its health checks, triggering an automatic rollback and a clear failure notification. This tests the pipeline's primary safety mechanism.
- **Covered by**: `TestCanaryHealthFailure`
- **Key Verifications**:
    - The canary deployment succeeds initially.
    - The monitoring stage reports the canary as unhealthy.
    - The pipeline halts and does **not** promote to production.
    - A rollback is initiated on the failed canary deployment.
    - A specific failure notification is sent.

### CUJ 3: Initial Deployment Failure
- **Description**: The pipeline fails during the initial deployment to the canary environment (e.g., due to infrastructure capacity issues). This tests the pipeline's ability to handle early-stage failures.
- **Covered by**: `TestCanaryDeployFailure`
- **Key Verifications**:
    - The `Deploy` action for the canary environment fails.
    - The pipeline halts immediately.
    - No subsequent stages (monitoring, promotion) are attempted.
    - A specific failure notification is sent.

### CUJ 4: Invalid Deployment Request (Bad Artifact)
- **Description**: The pipeline is triggered with a request for an artifact that does not exist. This tests the pipeline's input validation and resilience to invalid requests.
- **Covered by**: `TestArtifactNotFound`
- **Key Verifications**:
    - The pipeline fails immediately during the initial artifact fetch phase.
    - No `WorkPlan` is executed, and no deployments are attempted.
    - A clear failure notification is sent, indicating the artifact was not found.
