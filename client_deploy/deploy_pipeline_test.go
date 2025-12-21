package client_deploy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage/sqlite"
)

func setupTestEnv(t *testing.T) (context.Context, *service.OrchestratorService, *FakeArtifactRegistry, *FakeDeploymentService, *FakeMonitoringService, *FakeNotificationService) {

	t.Helper()

	ctx := context.Background()



	storage, err := sqlite.New(":memory:")

	if err != nil {

		t.Fatalf("failed to create storage: %v", err)

	}

	t.Cleanup(func() { storage.Close() })



	if err := storage.Migrate(ctx); err != nil {

		t.Fatalf("failed to migrate: %v", err)

	}



	orchestrator := service.NewOrchestrator(storage)

	artifacts := NewFakeArtifactRegistry()

	deployer := NewFakeDeploymentService()

	monitor := NewFakeMonitoringService()

	notifier := NewFakeNotificationService()



	return ctx, orchestrator, artifacts, deployer, monitor, notifier

}

func TestSuccessfulDeployment(t *testing.T) {
	ctx, orchestrator, artifacts, deployer, monitor, notifier := setupTestEnv(t)

	// Setup fakes
	artifact := &Artifact{ID: "my-service:v1.2.3", URL: "gs://bucket/v1.2.3.tar.gz"}
	artifacts.AddArtifact(artifact)

	// Create and run pipeline
	runner := NewDeployPipelineRunner(orchestrator, artifacts, deployer, monitor, notifier)
	req := &DeploymentRequest{
		ArtifactID: artifact.ID,
		DeployGoal: "production",
		Channel:    "#deployments",
	}
	_, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Pipeline.Run() failed unexpectedly: %v", err)
	}

	// Verify deployments
	if len(deployer.ExecutedDeployments) != 2 {
		t.Errorf("expected 2 deployments, got %d", len(deployer.ExecutedDeployments))
	}
	if deployer.ExecutedDeployments[0] != "my-service:v1.2.3:canary" {
		t.Errorf("unexpected canary deployment: %s", deployer.ExecutedDeployments[0])
	}
	if deployer.ExecutedDeployments[1] != "my-service:v1.2.3:production" {
		t.Errorf("unexpected production deployment: %s", deployer.ExecutedDeployments[1])
	}

	// Verify notifications
	messages := notifier.GetMessages("#deployments")
	if len(messages) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(messages))
	}
	if !strings.Contains(messages[0], "Canary deployment of my-service:v1.2.3 is healthy") {
		t.Errorf("unexpected notification: %s", messages[0])
	}
	if !strings.Contains(messages[1], "Successfully deployed my-service:v1.2.3 to production") {
		t.Errorf("unexpected notification: %s", messages[1])
	}
}

func TestCanaryHealthFailure(t *testing.T) {
	ctx, orchestrator, artifacts, deployer, monitor, notifier := setupTestEnv(t)

	// Setup fakes
	artifact := &Artifact{ID: "my-service:v1.2.4", URL: "gs://bucket/v1.2.4.tar.gz"}
	artifacts.AddArtifact(artifact)

	// Canary deployment will succeed, but monitoring will fail
	canaryDeploymentID := "deploy-1" // The fake deployer will generate this ID
	monitor.SetCanaryResult(canaryDeploymentID, &CanaryMetrics{
		Healthy:       false,
		FailureReason: "error rate spiked to 25%",
	})

	// Create and run pipeline
	runner := NewDeployPipelineRunner(orchestrator, artifacts, deployer, monitor, notifier)
	req := &DeploymentRequest{
		ArtifactID: artifact.ID,
		DeployGoal: "production",
		Channel:    "#deployments",
	}
	_, err := runner.Run(ctx, req)
	if err == nil {
		t.Fatal("Pipeline.Run() should have failed but didn't")
	}

	// Verify deployments
	if len(deployer.ExecutedDeployments) != 1 {
		t.Errorf("expected 1 deployment (canary), got %d", len(deployer.ExecutedDeployments))
	}
	if deployer.ExecutedDeployments[0] != "my-service:v1.2.4:canary" {
		t.Errorf("unexpected canary deployment: %s", deployer.ExecutedDeployments[0])
	}

	// Verify rollback was called
	if len(deployer.RolledBackDeployments) != 1 {
		t.Errorf("expected 1 rollback, got %d", len(deployer.RolledBackDeployments))
	}
	if deployer.RolledBackDeployments[0] != canaryDeploymentID {
		t.Errorf("unexpected rollback ID: got %s, want %s", deployer.RolledBackDeployments[0], canaryDeploymentID)
	}

	// Verify notifications
	messages := notifier.GetMessages("#deployments")
	if len(messages) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(messages))
	}
	if !strings.Contains(messages[0], "Canary of my-service:v1.2.4 FAILED health checks") {
		t.Errorf("unexpected notification: %s", messages[0])
	}
}

func TestArtifactNotFound(t *testing.T) {
	ctx, orchestrator, artifacts, deployer, monitor, notifier := setupTestEnv(t)

	// No artifact is added to the registry

	// Create and run pipeline
	runner := NewDeployPipelineRunner(orchestrator, artifacts, deployer, monitor, notifier)
	req := &DeploymentRequest{
		ArtifactID: "non-existent:v0.0.1",
		DeployGoal: "production",
		Channel:    "#deployments",
	}
	_, err := runner.Run(ctx, req)
	if err == nil {
		t.Fatal("Pipeline.Run() should have failed but didn't")
	}

	// Verify no deployments were attempted
	if len(deployer.ExecutedDeployments) != 0 {
		t.Errorf("expected 0 deployments, got %d", len(deployer.ExecutedDeployments))
	}

	// Verify notification
	messages := notifier.GetMessages("#deployments")
	if len(messages) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(messages))
	}
	if !strings.Contains(messages[0], "Deployment failed: artifact \"non-existent:v0.0.1\" not found") {
		t.Errorf("unexpected notification: %s", messages[0])
	}
}

func TestCanaryDeployFailure(t *testing.T) {
	ctx, orchestrator, artifacts, deployer, monitor, notifier := setupTestEnv(t)

	// Setup fakes
	artifact := &Artifact{ID: "my-service:v1.2.5", URL: "gs://bucket/v1.2.5.tar.gz"}
	artifacts.AddArtifact(artifact)

	// Make the canary deployment fail
	deployer.Failures["my-service:v1.2.5:canary"] = fmt.Errorf("no capacity in canary cluster")

	// Create and run pipeline
	runner := NewDeployPipelineRunner(orchestrator, artifacts, deployer, monitor, notifier)
	req := &DeploymentRequest{
		ArtifactID: artifact.ID,
		DeployGoal: "production",
		Channel:    "#deployments",
	}
	_, err := runner.Run(ctx, req)
	if err == nil {
		t.Fatal("Pipeline.Run() should have failed but didn't")
	}

	// Verify deployments
	if len(deployer.ExecutedDeployments) != 1 {
		t.Errorf("expected 1 deployment attempt, got %d", len(deployer.ExecutedDeployments))
	}

	// Verify no rollbacks (since nothing was successfully deployed)
	if len(deployer.RolledBackDeployments) != 0 {
		t.Errorf("expected 0 rollbacks, got %d", len(deployer.RolledBackDeployments))
	}

	// Verify notifications
	messages := notifier.GetMessages("#deployments")
	if len(messages) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(messages))
	}
	if !strings.Contains(messages[0], "Deployment of my-service:v1.2.5 to canary FAILED") {
		t.Errorf("unexpected notification: %s", messages[0])
	}
}
