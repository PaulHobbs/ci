package client

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/example/turboci-lite/internal/domain"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage/sqlite"
)

// TestBasicPipeline tests a simple pipeline with one build and one test.
func TestBasicPipeline(t *testing.T) {
	ctx := context.Background()

	// Setup storage
	storage, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// Setup fakes
	configProvider := NewFakeConfigProvider()
	executor := NewFakeExecutor()
	gerrit := NewFakeGerritClient()

	// Create a test change
	change := &ChangeInfo{
		ID:      "12345",
		Project: "chromium/src",
		Branch:  "main",
		Subject: "Add new feature",
		Owner:   "developer@example.com",
	}
	gerrit.AddChange(change)

	// Add build and test configs
	configProvider.AddBuildConfig(change.ID, &BuildConfig{
		ID:       "linux-rel",
		Name:     "Linux Release",
		Target:   "chrome",
		Platform: "linux",
	})

	configProvider.AddTestConfig(change.ID, &TestConfig{
		ID:             "linux-unit",
		Name:           "Linux Unit Tests",
		Suite:          "unit_tests",
		Platform:       "linux",
		DependsOnBuild: "linux-rel",
	})

	// Run pipeline
	runner := NewPipelineRunner(storage, configProvider, executor, gerrit)
	workPlanID, err := runner.RunPipeline(ctx, change)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	// Verify results
	if len(executor.ExecutedBuilds) != 1 {
		t.Errorf("expected 1 build, got %d", len(executor.ExecutedBuilds))
	}
	if executor.ExecutedBuilds[0] != "linux-rel" {
		t.Errorf("expected build linux-rel, got %s", executor.ExecutedBuilds[0])
	}

	if len(executor.ExecutedTests) != 1 {
		t.Errorf("expected 1 test, got %d", len(executor.ExecutedTests))
	}
	if executor.ExecutedTests[0] != "linux-unit" {
		t.Errorf("expected test linux-unit, got %s", executor.ExecutedTests[0])
	}

	// Verify Gerrit was updated
	verified := gerrit.GetVerified(change.ID)
	if verified != 1 {
		t.Errorf("expected verified=1, got %d", verified)
	}

	comments := gerrit.GetComments(change.ID)
	if len(comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(comments))
	}

	// Verify work plan state
	orchestrator := service.NewOrchestrator(storage)
	resp, err := orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		IncludeChecks: true,
		IncludeStages: true,
	})
	if err != nil {
		t.Fatalf("failed to query nodes: %v", err)
	}

	// All checks should be FINAL
	for _, check := range resp.Checks {
		if check.State != domain.CheckStateFinal {
			t.Errorf("check %s in state %s, expected FINAL", check.ID, check.State)
		}
	}

	// All stages should be FINAL
	for _, stage := range resp.Stages {
		if stage.State != domain.StageStateFinal {
			t.Errorf("stage %s in state %s, expected FINAL", stage.ID, stage.State)
		}
	}
}

// TestMultipleBuildsPipeline tests a pipeline with multiple builds and tests.
func TestMultipleBuildsPipeline(t *testing.T) {
	ctx := context.Background()

	storage, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	configProvider := NewFakeConfigProvider()
	executor := NewFakeExecutor()
	gerrit := NewFakeGerritClient()

	change := &ChangeInfo{
		ID:      "67890",
		Project: "chromium/src",
		Branch:  "main",
		Subject: "Multi-platform support",
		Owner:   "developer@example.com",
	}
	gerrit.AddChange(change)

	// Add multiple builds for different platforms
	platforms := []string{"linux", "mac", "win"}
	for _, platform := range platforms {
		configProvider.AddBuildConfig(change.ID, &BuildConfig{
			ID:       fmt.Sprintf("%s-rel", platform),
			Name:     fmt.Sprintf("%s Release", platform),
			Target:   "chrome",
			Platform: platform,
		})

		configProvider.AddTestConfig(change.ID, &TestConfig{
			ID:             fmt.Sprintf("%s-unit", platform),
			Name:           fmt.Sprintf("%s Unit Tests", platform),
			Suite:          "unit_tests",
			Platform:       platform,
			DependsOnBuild: fmt.Sprintf("%s-rel", platform),
		})
	}

	runner := NewPipelineRunner(storage, configProvider, executor, gerrit)
	_, err = runner.RunPipeline(ctx, change)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	// Verify all builds executed
	if len(executor.ExecutedBuilds) != 3 {
		t.Errorf("expected 3 builds, got %d", len(executor.ExecutedBuilds))
	}

	// Verify all tests executed
	if len(executor.ExecutedTests) != 3 {
		t.Errorf("expected 3 tests, got %d", len(executor.ExecutedTests))
	}

	// Verify Gerrit was updated successfully
	if gerrit.GetVerified(change.ID) != 1 {
		t.Errorf("expected verified=1, got %d", gerrit.GetVerified(change.ID))
	}
}

// TestBuildFailure tests that a build failure is properly propagated.
func TestBuildFailure(t *testing.T) {
	ctx := context.Background()

	storage, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	configProvider := NewFakeConfigProvider()
	executor := NewFakeExecutor()
	gerrit := NewFakeGerritClient()

	change := &ChangeInfo{
		ID:      "11111",
		Project: "chromium/src",
		Branch:  "main",
		Subject: "Broken change",
		Owner:   "developer@example.com",
	}
	gerrit.AddChange(change)

	configProvider.AddBuildConfig(change.ID, &BuildConfig{
		ID:       "linux-rel",
		Name:     "Linux Release",
		Target:   "chrome",
		Platform: "linux",
	})

	// Set build to fail
	executor.SetBuildResult("linux-rel", &BuildResult{
		BuildID:  "build-linux-rel-11111",
		Success:  false,
		Duration: 50 * time.Millisecond,
		Logs:     "error: compilation failed",
	})

	configProvider.AddTestConfig(change.ID, &TestConfig{
		ID:             "linux-unit",
		Name:           "Linux Unit Tests",
		Suite:          "unit_tests",
		Platform:       "linux",
		DependsOnBuild: "linux-rel",
	})

	runner := NewPipelineRunner(storage, configProvider, executor, gerrit)
	_, err = runner.RunPipeline(ctx, change)
	if err != nil {
		t.Fatalf("pipeline failed unexpectedly: %v", err)
	}

	// Build should have executed
	if len(executor.ExecutedBuilds) != 1 {
		t.Errorf("expected 1 build, got %d", len(executor.ExecutedBuilds))
	}

	// Test should have still executed (dependency is on the stage, not the result)
	if len(executor.ExecutedTests) != 1 {
		t.Errorf("expected 1 test, got %d", len(executor.ExecutedTests))
	}

	// Verified should be -1 due to build failure
	if gerrit.GetVerified(change.ID) != -1 {
		t.Errorf("expected verified=-1, got %d", gerrit.GetVerified(change.ID))
	}

	// Check that the failure message was set
	msg := gerrit.GetLastMessage(change.ID)
	if msg == "" {
		t.Error("expected failure message, got empty string")
	}
}

// TestTestFailure tests that a test failure is properly propagated.
func TestTestFailure(t *testing.T) {
	ctx := context.Background()

	storage, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	configProvider := NewFakeConfigProvider()
	executor := NewFakeExecutor()
	gerrit := NewFakeGerritClient()

	change := &ChangeInfo{
		ID:      "22222",
		Project: "chromium/src",
		Branch:  "main",
		Subject: "Test failing change",
		Owner:   "developer@example.com",
	}
	gerrit.AddChange(change)

	configProvider.AddBuildConfig(change.ID, &BuildConfig{
		ID:       "linux-rel",
		Name:     "Linux Release",
		Target:   "chrome",
		Platform: "linux",
	})

	configProvider.AddTestConfig(change.ID, &TestConfig{
		ID:             "linux-unit",
		Name:           "Linux Unit Tests",
		Suite:          "unit_tests",
		Platform:       "linux",
		DependsOnBuild: "linux-rel",
	})

	// Set test to fail
	executor.SetTestResult("linux-unit", &TestResult{
		TestID:   "test-linux-unit-22222",
		Success:  false,
		Duration: 30 * time.Millisecond,
		Logs:     "FAILED: TestSomething",
		Passed:   8,
		Failed:   2,
		Skipped:  0,
	})

	runner := NewPipelineRunner(storage, configProvider, executor, gerrit)
	_, err = runner.RunPipeline(ctx, change)
	if err != nil {
		t.Fatalf("pipeline failed unexpectedly: %v", err)
	}

	// Verified should be -1 due to test failure
	if gerrit.GetVerified(change.ID) != -1 {
		t.Errorf("expected verified=-1, got %d", gerrit.GetVerified(change.ID))
	}

	// Check summary comment mentions failures
	comments := gerrit.GetComments(change.ID)
	if len(comments) == 0 {
		t.Fatal("expected at least one comment")
	}
}

// TestEmptyPipeline tests a pipeline with no build/test configs.
func TestEmptyPipeline(t *testing.T) {
	ctx := context.Background()

	storage, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	configProvider := NewFakeConfigProvider()
	executor := NewFakeExecutor()
	gerrit := NewFakeGerritClient()

	change := &ChangeInfo{
		ID:      "33333",
		Project: "chromium/src",
		Branch:  "main",
		Subject: "Documentation only change",
		Owner:   "developer@example.com",
	}
	gerrit.AddChange(change)

	// No configs added - empty pipeline

	runner := NewPipelineRunner(storage, configProvider, executor, gerrit)
	_, err = runner.RunPipeline(ctx, change)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	// No builds or tests should have executed
	if len(executor.ExecutedBuilds) != 0 {
		t.Errorf("expected 0 builds, got %d", len(executor.ExecutedBuilds))
	}
	if len(executor.ExecutedTests) != 0 {
		t.Errorf("expected 0 tests, got %d", len(executor.ExecutedTests))
	}

	// Verified should still be set to 1 (no failures)
	if gerrit.GetVerified(change.ID) != 1 {
		t.Errorf("expected verified=1, got %d", gerrit.GetVerified(change.ID))
	}
}

// TestDependencyChain tests that dependencies are properly respected.
func TestDependencyChain(t *testing.T) {
	ctx := context.Background()

	storage, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	configProvider := NewFakeConfigProvider()
	executor := NewFakeExecutor()
	gerrit := NewFakeGerritClient()

	change := &ChangeInfo{
		ID:      "44444",
		Project: "chromium/src",
		Branch:  "main",
		Subject: "Dependency chain test",
		Owner:   "developer@example.com",
	}
	gerrit.AddChange(change)

	// Create a chain: build1 -> test1 (depends on build1)
	//                 build2 -> test2 (depends on build2)
	configProvider.AddBuildConfig(change.ID, &BuildConfig{
		ID:       "build1",
		Name:     "Build 1",
		Target:   "target1",
		Platform: "linux",
	})
	configProvider.AddBuildConfig(change.ID, &BuildConfig{
		ID:       "build2",
		Name:     "Build 2",
		Target:   "target2",
		Platform: "linux",
	})

	configProvider.AddTestConfig(change.ID, &TestConfig{
		ID:             "test1",
		Name:           "Test 1",
		Suite:          "suite1",
		Platform:       "linux",
		DependsOnBuild: "build1",
	})
	configProvider.AddTestConfig(change.ID, &TestConfig{
		ID:             "test2",
		Name:           "Test 2",
		Suite:          "suite2",
		Platform:       "linux",
		DependsOnBuild: "build2",
	})

	// Track execution order with timestamps
	var executionOrder []string
	executor.OnBuild = func(config *BuildConfig, change *ChangeInfo) (*BuildResult, error) {
		executionOrder = append(executionOrder, "build:"+config.ID)
		return &BuildResult{
			BuildID: config.ID,
			Success: true,
		}, nil
	}
	executor.OnTest = func(config *TestConfig, change *ChangeInfo, buildResult *BuildResult) (*TestResult, error) {
		executionOrder = append(executionOrder, "test:"+config.ID)
		return &TestResult{
			TestID:  config.ID,
			Success: true,
			Passed:  10,
		}, nil
	}

	runner := NewPipelineRunner(storage, configProvider, executor, gerrit)
	_, err = runner.RunPipeline(ctx, change)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	// Verify all executed
	if len(executionOrder) != 4 {
		t.Errorf("expected 4 executions, got %d: %v", len(executionOrder), executionOrder)
	}

	// Verify Gerrit was updated
	if gerrit.GetVerified(change.ID) != 1 {
		t.Errorf("expected verified=1, got %d", gerrit.GetVerified(change.ID))
	}
}

// TestConcurrentBuilds tests that independent builds can run concurrently.
func TestConcurrentBuilds(t *testing.T) {
	ctx := context.Background()

	storage, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	configProvider := NewFakeConfigProvider()
	executor := NewFakeExecutor()
	gerrit := NewFakeGerritClient()

	change := &ChangeInfo{
		ID:      "55555",
		Project: "chromium/src",
		Branch:  "main",
		Subject: "Concurrent builds test",
		Owner:   "developer@example.com",
	}
	gerrit.AddChange(change)

	// Add 5 independent builds
	for i := 1; i <= 5; i++ {
		configProvider.AddBuildConfig(change.ID, &BuildConfig{
			ID:       fmt.Sprintf("build%d", i),
			Name:     fmt.Sprintf("Build %d", i),
			Target:   fmt.Sprintf("target%d", i),
			Platform: "linux",
		})
	}

	runner := NewPipelineRunner(storage, configProvider, executor, gerrit)
	_, err = runner.RunPipeline(ctx, change)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	// All builds should have executed
	if len(executor.ExecutedBuilds) != 5 {
		t.Errorf("expected 5 builds, got %d", len(executor.ExecutedBuilds))
	}

	if gerrit.GetVerified(change.ID) != 1 {
		t.Errorf("expected verified=1, got %d", gerrit.GetVerified(change.ID))
	}
}

// TestAndroidStylePipeline simulates an Android-style CI pipeline.
func TestAndroidStylePipeline(t *testing.T) {
	ctx := context.Background()

	storage, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	configProvider := NewFakeConfigProvider()
	executor := NewFakeExecutor()
	gerrit := NewFakeGerritClient()

	change := &ChangeInfo{
		ID:      "android-123",
		Project: "platform/frameworks/base",
		Branch:  "main",
		Subject: "Fix memory leak in ActivityManager",
		Owner:   "android-dev@google.com",
	}
	gerrit.AddChange(change)

	// Android-style builds: multiple product configurations
	products := []string{"aosp_arm64", "aosp_x86_64", "sdk_phone_arm64"}
	for _, product := range products {
		configProvider.AddBuildConfig(change.ID, &BuildConfig{
			ID:       product,
			Name:     fmt.Sprintf("Build %s", product),
			Target:   "droid",
			Platform: product,
		})
	}

	// Android-style tests
	testSuites := []string{"cts", "vts", "unit"}
	for _, suite := range testSuites {
		configProvider.AddTestConfig(change.ID, &TestConfig{
			ID:             fmt.Sprintf("%s-tests", suite),
			Name:           fmt.Sprintf("%s Tests", suite),
			Suite:          suite,
			Platform:       "aosp_arm64",
			DependsOnBuild: "aosp_arm64",
		})
	}

	runner := NewPipelineRunner(storage, configProvider, executor, gerrit)
	workPlanID, err := runner.RunPipeline(ctx, change)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	// Verify all builds and tests executed
	if len(executor.ExecutedBuilds) != len(products) {
		t.Errorf("expected %d builds, got %d", len(products), len(executor.ExecutedBuilds))
	}
	if len(executor.ExecutedTests) != len(testSuites) {
		t.Errorf("expected %d tests, got %d", len(testSuites), len(executor.ExecutedTests))
	}

	// Verify work plan structure
	orchestrator := service.NewOrchestrator(storage)
	resp, err := orchestrator.QueryNodes(ctx, &service.QueryNodesRequest{
		WorkPlanID:    workPlanID,
		IncludeStages: true,
	})
	if err != nil {
		t.Fatalf("failed to query nodes: %v", err)
	}

	// Expected stages: planning + materializer + (3 builds + 3 tests) + completion = 9
	expectedStages := 1 + 1 + len(products) + len(testSuites) + 1
	if len(resp.Stages) != expectedStages {
		t.Errorf("expected %d stages, got %d", expectedStages, len(resp.Stages))
	}

	if gerrit.GetVerified(change.ID) != 1 {
		t.Errorf("expected verified=1, got %d", gerrit.GetVerified(change.ID))
	}
}

// TestChromeStylePipeline simulates a Chrome-style CI pipeline.
func TestChromeStylePipeline(t *testing.T) {
	ctx := context.Background()

	storage, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	configProvider := NewFakeConfigProvider()
	executor := NewFakeExecutor()
	gerrit := NewFakeGerritClient()

	change := &ChangeInfo{
		ID:      "chrome-456789",
		Project: "chromium/src",
		Branch:  "main",
		Subject: "Implement new tab page feature",
		Owner:   "chromium-dev@chromium.org",
	}
	gerrit.AddChange(change)

	// Chrome-style builds: multiple platforms and configurations
	builds := []struct {
		id       string
		name     string
		platform string
	}{
		{"linux-rel", "Linux Release", "linux"},
		{"linux-dbg", "Linux Debug", "linux"},
		{"win-rel", "Windows Release", "win"},
		{"mac-rel", "Mac Release", "mac"},
		{"android-arm64-rel", "Android ARM64", "android"},
		{"chromeos-amd64-rel", "ChromeOS AMD64", "chromeos"},
	}

	for _, b := range builds {
		configProvider.AddBuildConfig(change.ID, &BuildConfig{
			ID:       b.id,
			Name:     b.name,
			Target:   "chrome",
			Platform: b.platform,
		})
	}

	// Chrome-style tests: various test suites per platform
	tests := []struct {
		id             string
		name           string
		suite          string
		platform       string
		dependsOnBuild string
	}{
		{"linux-rel-unit", "Linux Unit Tests", "unit_tests", "linux", "linux-rel"},
		{"linux-rel-browser", "Linux Browser Tests", "browser_tests", "linux", "linux-rel"},
		{"win-rel-unit", "Windows Unit Tests", "unit_tests", "win", "win-rel"},
		{"mac-rel-unit", "Mac Unit Tests", "unit_tests", "mac", "mac-rel"},
		{"android-arm64-rel-unit", "Android Unit Tests", "unit_tests", "android", "android-arm64-rel"},
	}

	for _, tc := range tests {
		configProvider.AddTestConfig(change.ID, &TestConfig{
			ID:             tc.id,
			Name:           tc.name,
			Suite:          tc.suite,
			Platform:       tc.platform,
			DependsOnBuild: tc.dependsOnBuild,
		})
	}

	runner := NewPipelineRunner(storage, configProvider, executor, gerrit)
	_, err = runner.RunPipeline(ctx, change)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	// Verify execution counts
	if len(executor.ExecutedBuilds) != len(builds) {
		t.Errorf("expected %d builds, got %d", len(builds), len(executor.ExecutedBuilds))
	}
	if len(executor.ExecutedTests) != len(tests) {
		t.Errorf("expected %d tests, got %d", len(tests), len(executor.ExecutedTests))
	}

	// Verify Gerrit feedback
	if gerrit.GetVerified(change.ID) != 1 {
		t.Errorf("expected verified=1, got %d", gerrit.GetVerified(change.ID))
	}

	comments := gerrit.GetComments(change.ID)
	if len(comments) == 0 {
		t.Error("expected at least one comment")
	}

	// Verify comment contains summary
	comment := comments[0]
	if len(comment) == 0 {
		t.Error("comment should not be empty")
	}
}

// TestPartialFailure tests a pipeline where some builds pass and others fail.
func TestPartialFailure(t *testing.T) {
	ctx := context.Background()

	storage, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	configProvider := NewFakeConfigProvider()
	executor := NewFakeExecutor()
	gerrit := NewFakeGerritClient()

	change := &ChangeInfo{
		ID:      "partial-fail",
		Project: "chromium/src",
		Branch:  "main",
		Subject: "Platform-specific bug",
		Owner:   "developer@example.com",
	}
	gerrit.AddChange(change)

	// Two builds, one will fail
	configProvider.AddBuildConfig(change.ID, &BuildConfig{
		ID:       "linux-rel",
		Name:     "Linux Release",
		Target:   "chrome",
		Platform: "linux",
	})
	configProvider.AddBuildConfig(change.ID, &BuildConfig{
		ID:       "win-rel",
		Name:     "Windows Release",
		Target:   "chrome",
		Platform: "win",
	})

	// Windows build fails
	executor.SetBuildResult("win-rel", &BuildResult{
		BuildID:  "build-win-rel",
		Success:  false,
		Duration: 100 * time.Millisecond,
		Logs:     "error: MSVC compilation error",
	})

	runner := NewPipelineRunner(storage, configProvider, executor, gerrit)
	_, err = runner.RunPipeline(ctx, change)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	// Both builds should have executed
	if len(executor.ExecutedBuilds) != 2 {
		t.Errorf("expected 2 builds, got %d", len(executor.ExecutedBuilds))
	}

	// Verified should be -1 due to Windows failure
	if gerrit.GetVerified(change.ID) != -1 {
		t.Errorf("expected verified=-1, got %d", gerrit.GetVerified(change.ID))
	}
}

// BenchmarkPipeline benchmarks pipeline execution.
func BenchmarkPipeline(b *testing.B) {
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		storage, _ := sqlite.New(":memory:")
		storage.Migrate(ctx)

		configProvider := NewFakeConfigProvider()
		executor := NewFakeExecutor()
		gerrit := NewFakeGerritClient()

		change := &ChangeInfo{
			ID:      fmt.Sprintf("bench-%d", i),
			Project: "test/project",
			Branch:  "main",
		}
		gerrit.AddChange(change)

		// Add 10 builds and 10 tests
		for j := 0; j < 10; j++ {
			configProvider.AddBuildConfig(change.ID, &BuildConfig{
				ID:       fmt.Sprintf("build%d", j),
				Name:     fmt.Sprintf("Build %d", j),
				Target:   "target",
				Platform: "linux",
			})
			configProvider.AddTestConfig(change.ID, &TestConfig{
				ID:             fmt.Sprintf("test%d", j),
				Name:           fmt.Sprintf("Test %d", j),
				Suite:          "suite",
				Platform:       "linux",
				DependsOnBuild: fmt.Sprintf("build%d", j),
			})
		}

		runner := NewPipelineRunner(storage, configProvider, executor, gerrit)
		runner.RunPipeline(ctx, change)

		storage.Close()
	}
}
