package client_deploy

import (
	"context"
	"fmt"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/pkg/id"
)

// DeploymentRequest is the input for triggering a deployment pipeline.
type DeploymentRequest struct {
	ArtifactID string // The artifact to deploy, e.g., "my-service:v1.2.3"
	DeployGoal string // e.g., "production"
	TriggeredBy string
	Channel    string // Notification channel
}

// DeployPipelineRunner orchestrates a deployment workflow using turboci-lite.
type DeployPipelineRunner struct {
	orchestrator *service.OrchestratorService
	artifacts    ArtifactRegistry
	deployer     DeploymentService
	monitor      MonitoringService
	notifier     NotificationService
}

// NewDeployPipelineRunner creates a new runner.
func NewDeployPipelineRunner(
	orchestrator *service.OrchestratorService,
	artifacts ArtifactRegistry,
	deployer DeploymentService,
	monitor MonitoringService,
	notifier NotificationService,
) *DeployPipelineRunner {
	return &DeployPipelineRunner{
		orchestrator: orchestrator,
		artifacts:    artifacts,
		deployer:     deployer,
		monitor:      monitor,
		notifier:     notifier,
	}
}

// Run executes the deployment pipeline.
func (r *DeployPipelineRunner) Run(ctx context.Context, req *DeploymentRequest) (string, error) {
	// 1. Fetch artifact info
	artifact, err := r.artifacts.GetArtifact(ctx, req.ArtifactID)
	if err != nil {
		r.notifier.Send(ctx, req.Channel, fmt.Sprintf("Deployment failed: artifact %q not found.", req.ArtifactID))
		return "", fmt.Errorf("failed to get artifact: %w", err)
	}

	// 2. Create a WorkPlan
	wpRes, err := r.orchestrator.CreateWorkPlan(ctx, &service.CreateWorkPlanRequest{})
	if err != nil {
		return "", fmt.Errorf("failed to create workplan: %w", err)
	}
	workPlanID := wpRes.ID
	fmt.Printf("Created WorkPlan: %s\n", workPlanID)

	// 3. Define the graph of Checks and Stages
	if err := r.defineGraph(ctx, workPlanID, artifact); err != nil {
		return "", fmt.Errorf("failed to define graph: %w", err)
	}

	// 4. Execute the pipeline stages
	if err := r.executePipeline(ctx, workPlanID, artifact, req.Channel); err != nil {
		// The pipeline itself failed. The specific error should have been notified by executePipeline.
		return workPlanID, err
	}

	return workPlanID, nil
}

// defineGraph creates all the necessary Checks and Stages for the deployment.
func (r *DeployPipelineRunner) defineGraph(ctx context.Context, wpID string, artifact *Artifact) error {
	// Define checks (the goals)
	checks := []*service.CheckWrite{
		{ID: "artifact-fetched", Kind: "verification"},
		{ID: "canary-deployed", Kind: "deployment"},
		{ID: "canary-healthy", Kind: "verification"},
		{ID: "production-deployed", Kind: "deployment"},
	}

	// Define stages (the actions)
	stages := []*service.StageWrite{
		// Stage 1: Deploy to Canary
		{
			ID:          "deploy-canary",
			Assignments: []domain.Assignment{{TargetCheckID: "canary-deployed"}},
			Dependencies: &domain.DependencyGroup{
				Predicate:    domain.PredicateAND,
				Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeCheck, TargetID: "artifact-fetched"}},
			},
		},
		// Stage 2: Monitor Canary
		{
			ID:          "monitor-canary",
			Assignments: []domain.Assignment{{TargetCheckID: "canary-healthy"}},
			Dependencies: &domain.DependencyGroup{
				Predicate:    domain.PredicateAND,
				Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeCheck, TargetID: "canary-deployed"}},
			},
		},
		// Stage 3: Promote to Production
		{
			ID:          "promote-to-prod",
			Assignments: []domain.Assignment{{TargetCheckID: "production-deployed"}},
			Dependencies: &domain.DependencyGroup{
				Predicate:    domain.PredicateAND,
				Dependencies: []domain.DependencyRef{{TargetType: domain.NodeTypeCheck, TargetID: "canary-healthy"}},
			},
		},
	}

	_, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: wpID,
		Checks:     checks,
		Stages:     stages,
	})
	return err
}

// executePipeline simulates the executor loop that picks up and runs stages.
func (r *DeployPipelineRunner) executePipeline(ctx context.Context, wpID string, artifact *Artifact, channel string) error {
	// In a real system, this would be a loop in a separate worker process.
	// Here, we simulate it linearly for testing.

	// Mark artifact as "fetched" to kick things off.
	if err := r.updateCheck(ctx, wpID, "artifact-fetched", true, nil); err != nil {
		return err
	}

	// --- Canary Deployment ---
	canaryDeployment, err := r.executeCanaryDeployment(ctx, wpID, artifact)
	if err != nil {
		msg := fmt.Sprintf("Deployment of %s to canary FAILED. Rolling back.", artifact.ID)
		r.notifier.Send(ctx, channel, msg)
		// In a real system, a rollback stage would be triggered.
		// For this simulation, we just notify and stop.
		return err
	}

	// --- Canary Monitoring ---
	err = r.executeCanaryMonitoring(ctx, wpID, canaryDeployment)
	if err != nil {
		msg := fmt.Sprintf("Canary of %s FAILED health checks. Rolling back.", artifact.ID)
		r.notifier.Send(ctx, channel, msg)
		if rbErr := r.deployer.Rollback(ctx, canaryDeployment.ID); rbErr != nil {
			r.notifier.Send(ctx, channel, fmt.Sprintf("ALERT: Failed to roll back canary deployment %s", canaryDeployment.ID))
		}
		return err
	}
	r.notifier.Send(ctx, channel, fmt.Sprintf("Canary deployment of %s is healthy. Promoting to production.", artifact.ID))

	// --- Production Deployment ---
	_, err = r.executeProductionDeployment(ctx, wpID, artifact)
	if err != nil {
		msg := fmt.Sprintf("Promotion of %s to production FAILED.", artifact.ID)
		r.notifier.Send(ctx, channel, msg)
		return err
	}

	r.notifier.Send(ctx, channel, fmt.Sprintf("Successfully deployed %s to production!", artifact.ID))
	return nil
}

func (r *DeployPipelineRunner) executeCanaryDeployment(ctx context.Context, wpID string, artifact *Artifact) (*Deployment, error) {
	// Simulate executor picking up 'deploy-canary'
	deployment, err := r.deployer.Deploy(ctx, artifact.ID, "canary")
	if err != nil {
		r.updateCheck(ctx, wpID, "canary-deployed", false, err)
		return nil, err
	}
	r.updateCheck(ctx, wpID, "canary-deployed", true, nil)
	return deployment, nil
}

func (r *DeployPipelineRunner) executeCanaryMonitoring(ctx context.Context, wpID string, deployment *Deployment) error {
	// Simulate executor picking up 'monitor-canary'
	metrics, err := r.monitor.CheckCanaryHealth(ctx, deployment.ID)
	if err != nil {
		r.updateCheck(ctx, wpID, "canary-healthy", false, err)
		return err
	}
	if !metrics.Healthy {
		err = fmt.Errorf("canary unhealthy: %s", metrics.FailureReason)
		r.updateCheck(ctx, wpID, "canary-healthy", false, err)
		return err
	}
	r.updateCheck(ctx, wpID, "canary-healthy", true, nil)
	return nil
}

func (r *DeployPipelineRunner) executeProductionDeployment(ctx context.Context, wpID string, artifact *Artifact) (*Deployment, error) {
	// Simulate executor picking up 'promote-to-prod'
	deployment, err := r.deployer.Deploy(ctx, artifact.ID, "production")
	if err != nil {
		r.updateCheck(ctx, wpID, "production-deployed", false, err)
		return nil, err
	}
	r.updateCheck(ctx, wpID, "production-deployed", true, nil)
	return deployment, nil
}

// updateCheck is a helper to write a result for a check.
func (r *DeployPipelineRunner) updateCheck(ctx context.Context, wpID, checkID string, success bool, checkErr error) error {
	var failure *domain.Failure
	if !success {
		msg := "failed"
		if checkErr != nil {
			msg = checkErr.Error()
		}
		failure = &domain.Failure{Message: msg}
	}

	// In a real executor, this would be a more complex stage attempt update.
	// For this simulation, we write a result directly to the check.
	_, err := r.orchestrator.WriteNodes(ctx, &service.WriteNodesRequest{
		WorkPlanID: wpID,
		Checks: []*service.CheckWrite{
			{
				ID: checkID,
				Result: &service.CheckResultWrite{
					OwnerType: "executor",
					OwnerID:   id.Generate(),
					Finalize:  true,
					Failure:   failure,
					Data:      map[string]any{"success": success},
				},
			},
		},
	})
	return err
}
