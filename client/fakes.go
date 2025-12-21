// Package client provides a basic CI pipeline client for turboci-lite.
// It uses fakes for external resources to enable integration testing.
package client

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// BuildConfig represents a build configuration (e.g., from a project config).
type BuildConfig struct {
	ID       string
	Name     string
	Target   string
	Platform string
	Priority int
}

// TestConfig represents a test configuration.
type TestConfig struct {
	ID       string
	Name     string
	Suite    string
	Platform string
	// DependsOnBuild indicates which build this test depends on
	DependsOnBuild string
}

// ChangeInfo represents a Gerrit change.
type ChangeInfo struct {
	ID      string
	Project string
	Branch  string
	Subject string
	Owner   string
}

// ConfigProvider provides build and test configurations.
type ConfigProvider interface {
	// GetBuildConfigs returns all build configurations for a change.
	GetBuildConfigs(ctx context.Context, change *ChangeInfo) ([]*BuildConfig, error)

	// GetTestConfigs returns all test configurations for a change.
	GetTestConfigs(ctx context.Context, change *ChangeInfo) ([]*TestConfig, error)
}

// BuildResult represents the result of a build execution.
type BuildResult struct {
	BuildID  string
	Success  bool
	Duration time.Duration
	Logs     string
	Artifacts []string
}

// TestResult represents the result of a test execution.
type TestResult struct {
	TestID   string
	Success  bool
	Duration time.Duration
	Logs     string
	Passed   int
	Failed   int
	Skipped  int
}

// Executor executes builds and tests.
type Executor interface {
	// ExecuteBuild runs a build and returns the result.
	ExecuteBuild(ctx context.Context, config *BuildConfig, change *ChangeInfo) (*BuildResult, error)

	// ExecuteTest runs a test suite and returns the result.
	ExecuteTest(ctx context.Context, config *TestConfig, change *ChangeInfo, buildResult *BuildResult) (*TestResult, error)
}

// GerritClient interacts with Gerrit for external state changes.
type GerritClient interface {
	// PostComment posts a comment on a change.
	PostComment(ctx context.Context, changeID string, message string) error

	// SetVerified sets the verified label on a change.
	SetVerified(ctx context.Context, changeID string, value int, message string) error

	// GetChange retrieves change information.
	GetChange(ctx context.Context, changeID string) (*ChangeInfo, error)
}

// --- Fake Implementations ---

// FakeConfigProvider provides fake build/test configurations for testing.
type FakeConfigProvider struct {
	mu           sync.RWMutex
	buildConfigs map[string][]*BuildConfig // keyed by change ID
	testConfigs  map[string][]*TestConfig  // keyed by change ID
}

// NewFakeConfigProvider creates a new FakeConfigProvider.
func NewFakeConfigProvider() *FakeConfigProvider {
	return &FakeConfigProvider{
		buildConfigs: make(map[string][]*BuildConfig),
		testConfigs:  make(map[string][]*TestConfig),
	}
}

// AddBuildConfig adds a build config for a change.
func (p *FakeConfigProvider) AddBuildConfig(changeID string, config *BuildConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buildConfigs[changeID] = append(p.buildConfigs[changeID], config)
}

// AddTestConfig adds a test config for a change.
func (p *FakeConfigProvider) AddTestConfig(changeID string, config *TestConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.testConfigs[changeID] = append(p.testConfigs[changeID], config)
}

func (p *FakeConfigProvider) GetBuildConfigs(ctx context.Context, change *ChangeInfo) ([]*BuildConfig, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	configs := p.buildConfigs[change.ID]
	if configs == nil {
		return []*BuildConfig{}, nil
	}
	return configs, nil
}

func (p *FakeConfigProvider) GetTestConfigs(ctx context.Context, change *ChangeInfo) ([]*TestConfig, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	configs := p.testConfigs[change.ID]
	if configs == nil {
		return []*TestConfig{}, nil
	}
	return configs, nil
}

// FakeExecutor simulates build and test execution.
type FakeExecutor struct {
	mu           sync.RWMutex
	buildResults map[string]*BuildResult // keyed by build config ID
	testResults  map[string]*TestResult  // keyed by test config ID

	// Callbacks for custom behavior
	OnBuild func(config *BuildConfig, change *ChangeInfo) (*BuildResult, error)
	OnTest  func(config *TestConfig, change *ChangeInfo, buildResult *BuildResult) (*TestResult, error)

	// For tracking execution order
	ExecutedBuilds []string
	ExecutedTests  []string
}

// NewFakeExecutor creates a new FakeExecutor.
func NewFakeExecutor() *FakeExecutor {
	return &FakeExecutor{
		buildResults: make(map[string]*BuildResult),
		testResults:  make(map[string]*TestResult),
	}
}

// SetBuildResult sets a predetermined result for a build.
func (e *FakeExecutor) SetBuildResult(configID string, result *BuildResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.buildResults[configID] = result
}

// SetTestResult sets a predetermined result for a test.
func (e *FakeExecutor) SetTestResult(configID string, result *TestResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.testResults[configID] = result
}

func (e *FakeExecutor) ExecuteBuild(ctx context.Context, config *BuildConfig, change *ChangeInfo) (*BuildResult, error) {
	e.mu.Lock()
	e.ExecutedBuilds = append(e.ExecutedBuilds, config.ID)
	e.mu.Unlock()

	// Check for custom callback
	if e.OnBuild != nil {
		return e.OnBuild(config, change)
	}

	// Check for predetermined result
	e.mu.RLock()
	if result, ok := e.buildResults[config.ID]; ok {
		e.mu.RUnlock()
		return result, nil
	}
	e.mu.RUnlock()

	// Default: successful build
	return &BuildResult{
		BuildID:  fmt.Sprintf("build-%s-%s", config.ID, change.ID),
		Success:  true,
		Duration: 100 * time.Millisecond,
		Logs:     fmt.Sprintf("Building %s for %s... SUCCESS", config.Name, config.Platform),
		Artifacts: []string{
			fmt.Sprintf("out/%s/%s.bin", config.Platform, config.Target),
		},
	}, nil
}

func (e *FakeExecutor) ExecuteTest(ctx context.Context, config *TestConfig, change *ChangeInfo, buildResult *BuildResult) (*TestResult, error) {
	e.mu.Lock()
	e.ExecutedTests = append(e.ExecutedTests, config.ID)
	e.mu.Unlock()

	// Check for custom callback
	if e.OnTest != nil {
		return e.OnTest(config, change, buildResult)
	}

	// Check for predetermined result
	e.mu.RLock()
	if result, ok := e.testResults[config.ID]; ok {
		e.mu.RUnlock()
		return result, nil
	}
	e.mu.RUnlock()

	// Default: successful test
	return &TestResult{
		TestID:   fmt.Sprintf("test-%s-%s", config.ID, change.ID),
		Success:  true,
		Duration: 50 * time.Millisecond,
		Logs:     fmt.Sprintf("Running %s... PASSED", config.Suite),
		Passed:   10,
		Failed:   0,
		Skipped:  0,
	}, nil
}

// FakeGerritClient simulates Gerrit interactions.
type FakeGerritClient struct {
	mu       sync.RWMutex
	changes  map[string]*ChangeInfo
	comments map[string][]string   // keyed by change ID
	verified map[string]int        // keyed by change ID
	messages map[string]string     // last message for each change
}

// NewFakeGerritClient creates a new FakeGerritClient.
func NewFakeGerritClient() *FakeGerritClient {
	return &FakeGerritClient{
		changes:  make(map[string]*ChangeInfo),
		comments: make(map[string][]string),
		verified: make(map[string]int),
		messages: make(map[string]string),
	}
}

// AddChange adds a change to the fake Gerrit.
func (g *FakeGerritClient) AddChange(change *ChangeInfo) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.changes[change.ID] = change
}

// GetComments returns all comments posted on a change.
func (g *FakeGerritClient) GetComments(changeID string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.comments[changeID]
}

// GetVerified returns the verified label value for a change.
func (g *FakeGerritClient) GetVerified(changeID string) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.verified[changeID]
}

// GetLastMessage returns the last verified message for a change.
func (g *FakeGerritClient) GetLastMessage(changeID string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.messages[changeID]
}

func (g *FakeGerritClient) PostComment(ctx context.Context, changeID string, message string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.comments[changeID] = append(g.comments[changeID], message)
	return nil
}

func (g *FakeGerritClient) SetVerified(ctx context.Context, changeID string, value int, message string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.verified[changeID] = value
	g.messages[changeID] = message
	return nil
}

func (g *FakeGerritClient) GetChange(ctx context.Context, changeID string) (*ChangeInfo, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if change, ok := g.changes[changeID]; ok {
		return change, nil
	}
	return nil, fmt.Errorf("change %s not found", changeID)
}
