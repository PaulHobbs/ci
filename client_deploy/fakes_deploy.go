// Package client_deploy provides a client for a deployment pipeline.
package client_deploy

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// --- Interfaces for External Services ---

// Artifact represents a deployable software artifact.
type Artifact struct {
	ID      string // e.g., "my-service:v1.2.3"
	URL     string // e.g., "gs://my-bucket/my-service-v1.2.3.tar.gz"
	SHA256  string
	BuiltAt time.Time
}

// ArtifactRegistry provides access to software artifacts.
type ArtifactRegistry interface {
	GetArtifact(ctx context.Context, id string) (*Artifact, error)
}

// Deployment represents a deployment to an environment.
type Deployment struct {
	ID          string
	ArtifactID  string
	Environment string // "canary" or "production"
	Status      string // "SUCCESS", "FAILED"
}

// DeploymentService manages deployments to different environments.
type DeploymentService interface {
	Deploy(ctx context.Context, artifactID, environment string) (*Deployment, error)
	Rollback(ctx context.Context, deploymentID string) error
}

// CanaryMetrics represents health metrics from a canary deployment.
type CanaryMetrics struct {
	ErrorRate   float64
	LatencyP99  time.Duration
	Healthy     bool
	FailureReason string
}

// MonitoringService checks the health of a canary deployment.
type MonitoringService interface {
	CheckCanaryHealth(ctx context.Context, deploymentID string) (*CanaryMetrics, error)
}

// NotificationService sends notifications about deployment status.
type NotificationService interface {
	Send(ctx context.Context, channel, message string) error
}


// --- Fake Implementations for Testing ---

// FakeArtifactRegistry simulates an artifact registry.
type FakeArtifactRegistry struct {
	mu        sync.RWMutex
	artifacts map[string]*Artifact
}

func NewFakeArtifactRegistry() *FakeArtifactRegistry {
	return &FakeArtifactRegistry{artifacts: make(map[string]*Artifact)}
}

func (r *FakeArtifactRegistry) AddArtifact(artifact *Artifact) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.artifacts[artifact.ID] = artifact
}

func (r *FakeArtifactRegistry) GetArtifact(ctx context.Context, id string) (*Artifact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if artifact, ok := r.artifacts[id]; ok {
		return artifact, nil
	}
	return nil, fmt.Errorf("artifact %q not found", id)
}

// FakeDeploymentService simulates a deployment service.
type FakeDeploymentService struct {
	mu           sync.RWMutex
	deployments  map[string]*Deployment
	nextDeployID int
	Failures     map[string]error // Keyed by artifactID:environment
	ExecutedDeployments []string // Records calls to Deploy
	RolledBackDeployments []string // Records calls to Rollback
}

func NewFakeDeploymentService() *FakeDeploymentService {
	return &FakeDeploymentService{
		deployments:  make(map[string]*Deployment),
		nextDeployID: 1,
		Failures:     make(map[string]error),
	}
}

func (s *FakeDeploymentService) Deploy(ctx context.Context, artifactID, environment string) (*Deployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ExecutedDeployments = append(s.ExecutedDeployments, fmt.Sprintf("%s:%s", artifactID, environment))

	failureKey := fmt.Sprintf("%s:%s", artifactID, environment)
	if err, shouldFail := s.Failures[failureKey]; shouldFail {
		return nil, err
	}

	dep := &Deployment{
		ID:          fmt.Sprintf("deploy-%d", s.nextDeployID),
		ArtifactID:  artifactID,
		Environment: environment,
		Status:      "SUCCESS",
	}
	s.deployments[dep.ID] = dep
	s.nextDeployID++
	return dep, nil
}

func (s *FakeDeploymentService) Rollback(ctx context.Context, deploymentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.deployments[deploymentID]; !ok {
		return fmt.Errorf("deployment %q not found", deploymentID)
	}
	s.deployments[deploymentID].Status = "ROLLED_BACK"
	s.RolledBackDeployments = append(s.RolledBackDeployments, deploymentID)
	return nil
}

// FakeMonitoringService simulates a monitoring service.
type FakeMonitoringService struct {
	mu      sync.RWMutex
	metrics map[string]*CanaryMetrics // Keyed by deployment ID
}

func NewFakeMonitoringService() *FakeMonitoringService {
	return &FakeMonitoringService{metrics: make(map[string]*CanaryMetrics)}
}

func (s *FakeMonitoringService) SetCanaryResult(deploymentID string, metrics *CanaryMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics[deploymentID] = metrics
}

func (s *FakeMonitoringService) CheckCanaryHealth(ctx context.Context, deploymentID string) (*CanaryMetrics, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if metrics, ok := s.metrics[deploymentID]; ok {
		return metrics, nil
	}
	// Default healthy result
	return &CanaryMetrics{
		ErrorRate:  0.001,
		LatencyP99: 120 * time.Millisecond,
		Healthy:    true,
	}, nil
}

// FakeNotificationService simulates a notification service.
type FakeNotificationService struct {
	mu       sync.RWMutex
	Messages map[string][]string // Keyed by channel
}

func NewFakeNotificationService() *FakeNotificationService {
	return &FakeNotificationService{Messages: make(map[string][]string)}
}

func (s *FakeNotificationService) Send(ctx context.Context, channel, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages[channel] = append(s.Messages[channel], message)
	return nil
}

func (s *FakeNotificationService) GetMessages(channel string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Messages[channel]
}
