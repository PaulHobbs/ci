// Package main implements the TriggerBuilds runner.
// This runner detects changed packages via git and creates checks for affected packages/binaries.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/cmd/ci/runners/common"
)

var (
	runnerID         = flag.String("runner-id", "trigger-runner-1", "Runner ID")
	listenAddr       = flag.String("listen", ":50061", "Address to listen on")
	orchestratorAddr = flag.String("orchestrator", "localhost:50051", "Orchestrator address")
	repoRoot         = flag.String("repo-root", ".", "Repository root directory")
)

func main() {
	flag.Parse()

	runner := common.NewBaseRunner(*runnerID, "trigger_builds", *listenAddr, *orchestratorAddr, 1)

	trigger := &TriggerRunner{
		BaseRunner: runner,
		repoRoot:   *repoRoot,
	}
	runner.SetRunHandler(trigger.HandleRun)

	// Start gRPC server
	go func() {
		if err := runner.StartServer(); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	ctx := context.Background()
	if err := runner.Register(ctx); err != nil {
		log.Fatalf("Failed to register: %v", err)
	}

	go runner.HeartbeatLoop(ctx, 60*time.Second)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down trigger runner...")
	runner.Shutdown(ctx)
}

// TriggerRunner handles detecting changes and creating checks.
type TriggerRunner struct {
	*common.BaseRunner
	repoRoot string
}

// AffectedPackage represents a Go package that needs testing.
type AffectedPackage struct {
	Path   string `json:"path"`
	Module string `json:"module"`
	Dir    string `json:"dir"`
}

// AffectedBinary represents a binary that needs building.
type AffectedBinary struct {
	Name    string `json:"name"`
	Path    string `json:"path"`    // import path
	Dir     string `json:"dir"`     // directory
	GoMod   string `json:"go_mod"`  // which go.mod it belongs to
}

// E2ETest represents an E2E test suite.
type E2ETest struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	DependsOn []string `json:"depends_on"` // Check IDs this depends on
}

// AnalysisResult contains all affected packages and binaries.
type AnalysisResult struct {
	Packages  []AffectedPackage `json:"packages"`
	Binaries  []AffectedBinary  `json:"binaries"`
	E2ETests  []E2ETest         `json:"e2e_tests"`
	BaseRef   string            `json:"base_ref"`
	HeadRef   string            `json:"head_ref"`
}

// HandleRun processes a trigger builds request.
func (t *TriggerRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	log.Printf("[trigger] Starting analysis for work_plan=%s", req.WorkPlanId)

	// Get refs from args
	baseRef := "origin/main"
	headRef := "HEAD"
	if req.Args != nil {
		if v := req.Args.Fields["base_ref"]; v != nil {
			baseRef = v.GetStringValue()
		}
		if v := req.Args.Fields["head_ref"]; v != nil {
			headRef = v.GetStringValue()
		}
	}

	// Detect changes and analyze affected packages
	analysis, err := t.analyzeChanges(ctx, baseRef, headRef)
	if err != nil {
		log.Printf("[trigger] Analysis failed: %v", err)
		return t.createFailureResponse(req, fmt.Sprintf("analysis failed: %v", err))
	}

	log.Printf("[trigger] Found %d packages, %d binaries, %d e2e tests",
		len(analysis.Packages), len(analysis.Binaries), len(analysis.E2ETests))

	// Create checks via WriteNodes
	if err := t.createChecks(ctx, req.WorkPlanId, analysis); err != nil {
		log.Printf("[trigger] Failed to create checks: %v", err)
		return t.createFailureResponse(req, fmt.Sprintf("failed to create checks: %v", err))
	}

	// Create result using ResponseBuilder
	return common.NewResponse().
		FinalizeCheck(req.AssignedCheckIds[0], map[string]any{
			"packages_count":  len(analysis.Packages),
			"binaries_count":  len(analysis.Binaries),
			"e2e_tests_count": len(analysis.E2ETests),
			"base_ref":        baseRef,
			"head_ref":        headRef,
		}).
		Build(), nil
}

func (t *TriggerRunner) createFailureResponse(req *pb.RunRequest, msg string) (*pb.RunResponse, error) {
	return common.NewResponse().
		FailCheck(req.AssignedCheckIds[0], msg, nil).
		Build(), nil
}

func (t *TriggerRunner) analyzeChanges(ctx context.Context, baseRef, headRef string) (*AnalysisResult, error) {
	start := time.Now()
	result := &AnalysisResult{
		BaseRef: baseRef,
		HeadRef: headRef,
	}

	// Get changed files via git diff
	changedFiles, err := t.getChangedFiles(ctx, baseRef, headRef)
	if err != nil {
		// If git diff fails (no remote), analyze everything
		log.Printf("[trigger] Git diff failed, analyzing all packages: %v", err)
		return t.analyzeAllPackages(ctx)
	}
	log.Printf("[trigger] Git diff took %v", time.Since(start))

	if len(changedFiles) == 0 {
		log.Printf("[trigger] No changes detected between %s and %s", baseRef, headRef)
		return result, nil
	}

	log.Printf("[trigger] Changed files: %d", len(changedFiles))

	// Find affected packages
	affectedDirs := make(map[string]bool)
	for _, file := range changedFiles {
		if strings.HasSuffix(file, ".go") || file == "go.mod" || file == "go.sum" {
			dir := filepath.Dir(file)
			affectedDirs[dir] = true
		}
	}

	// Get all packages and filter to affected ones
	listStart := time.Now()
	allPackages, err := t.listPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list packages: %w", err)
	}
	log.Printf("[trigger] listPackages took %v (found %d total)", time.Since(listStart), len(allPackages))

	for _, pkg := range allPackages {
		relDir, _ := filepath.Rel(t.repoRoot, pkg.Dir)
		if affectedDirs[relDir] || affectedDirs["."] {
			result.Packages = append(result.Packages, pkg)
		}
	}

	// Find binaries
	binStart := time.Now()
	result.Binaries, err = t.findBinaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find binaries: %w", err)
	}
	log.Printf("[trigger] findBinaries took %v", time.Since(binStart))

	// Find E2E tests
	result.E2ETests = t.findE2ETests(result.Packages)

	log.Printf("[trigger] analyzeChanges total took %v", time.Since(start))
	return result, nil
}

func (t *TriggerRunner) analyzeAllPackages(ctx context.Context) (*AnalysisResult, error) {
	result := &AnalysisResult{
		BaseRef: "N/A",
		HeadRef: "HEAD",
	}

	packages, err := t.listPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list packages: %w", err)
	}
	result.Packages = packages

	binaries, err := t.findBinaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find binaries: %w", err)
	}
	result.Binaries = binaries

	result.E2ETests = t.findE2ETests(packages)

	return result, nil
}

func (t *TriggerRunner) getChangedFiles(ctx context.Context, baseRef, headRef string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", fmt.Sprintf("%s...%s", baseRef, headRef))
	cmd.Dir = t.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []string
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func (t *TriggerRunner) listPackages(ctx context.Context) ([]AffectedPackage, error) {
	// Find all go.mod files and list packages for each
	var packages []AffectedPackage

	// List packages from root module
	pkgs, err := t.listModulePackages(ctx, t.repoRoot)
	if err != nil {
		return nil, err
	}
	packages = append(packages, pkgs...)

	// Check for nested modules in cmd/
	cmdDir := filepath.Join(t.repoRoot, "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				modPath := filepath.Join(cmdDir, entry.Name(), "go.mod")
				if _, err := os.Stat(modPath); err == nil {
					pkgs, err := t.listModulePackages(ctx, filepath.Join(cmdDir, entry.Name()))
					if err != nil {
						log.Printf("[trigger] Warning: failed to list packages for %s: %v", entry.Name(), err)
						continue
					}
					packages = append(packages, pkgs...)
				}
			}
		}
	}

	return packages, nil
}

func (t *TriggerRunner) listModulePackages(ctx context.Context, modDir string) ([]AffectedPackage, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	cmd.Dir = modDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list failed in %s: %w", modDir, err)
	}
	log.Printf("[trigger] go list in %s took %v", modDir, time.Since(start))

	var packages []AffectedPackage
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var pkg struct {
			ImportPath string `json:"ImportPath"`
			Dir        string `json:"Dir"`
			Module     struct {
				Path string `json:"Path"`
			} `json:"Module"`
		}
		if err := decoder.Decode(&pkg); err != nil {
			break
		}

		packages = append(packages, AffectedPackage{
			Path:   pkg.ImportPath,
			Module: pkg.Module.Path,
			Dir:    pkg.Dir,
		})
	}

	return packages, nil
}

func (t *TriggerRunner) findBinaries(ctx context.Context) ([]AffectedBinary, error) {
	var binaries []AffectedBinary

	// Find all directories under cmd/ that have a main.go
	cmdDir := filepath.Join(t.repoRoot, "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		return binaries, nil // No cmd directory
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		binDir := filepath.Join(cmdDir, entry.Name())
		mainGo := filepath.Join(binDir, "main.go")

		// Check for main.go directly or in subdirectories
		if _, err := os.Stat(mainGo); err == nil {
			// Check if this is a separate module
			goMod := ""
			if _, err := os.Stat(filepath.Join(binDir, "go.mod")); err == nil {
				goMod = filepath.Join(binDir, "go.mod")
			}

			binaries = append(binaries, AffectedBinary{
				Name:  entry.Name(),
				Path:  "./cmd/" + entry.Name(),
				Dir:   binDir,
				GoMod: goMod,
			})
		}
	}

	return binaries, nil
}

func (t *TriggerRunner) findE2ETests(packages []AffectedPackage) []E2ETest {
	var tests []E2ETest

	for _, pkg := range packages {
		// E2E tests are in testing/e2e or have "e2e" in the path
		if strings.Contains(pkg.Path, "/testing/e2e") || strings.Contains(pkg.Path, "/e2e") {
			// Determine dependencies based on what the E2E tests cover
			var deps []string
			// E2E tests depend on all unit tests completing
			for _, p := range packages {
				if !strings.Contains(p.Path, "/testing/") && !strings.Contains(p.Path, "/e2e") {
					deps = append(deps, fmt.Sprintf("test:%s", p.Path))
				}
			}

			tests = append(tests, E2ETest{
				Name:      filepath.Base(pkg.Path),
				Path:      pkg.Path,
				DependsOn: deps,
			})
		}
	}

	return tests
}

func (t *TriggerRunner) createChecks(ctx context.Context, workPlanID string, analysis *AnalysisResult) error {
	var checks []*pb.CheckWrite

	// Create build checks using the fluent builder
	for _, bin := range analysis.Binaries {
		checks = append(checks, common.NewCheck(fmt.Sprintf("build:%s", bin.Name)).
			Kind("build").
			Planning().
			Options(map[string]any{
				"binary": bin.Path,
				"output": bin.Name,
				"dir":    bin.Dir,
				"go_mod": bin.GoMod,
			}).
			Build())
	}

	// Create unit test checks (depend on relevant builds)
	for _, pkg := range analysis.Packages {
		// Skip E2E test packages
		if strings.Contains(pkg.Path, "/testing/e2e") || strings.Contains(pkg.Path, "/e2e") {
			continue
		}

		// Find build dependencies for this package
		var buildDeps []string
		for _, bin := range analysis.Binaries {
			if strings.HasPrefix(pkg.Dir, bin.Dir) || pkg.Module == bin.GoMod {
				buildDeps = append(buildDeps, fmt.Sprintf("build:%s", bin.Name))
			}
		}

		checks = append(checks, common.NewCheck(fmt.Sprintf("test:%s", pkg.Path)).
			Kind("unit_test").
			Planning().
			Options(map[string]any{
				"package": pkg.Path,
				"module":  pkg.Module,
				"dir":     pkg.Dir,
			}).
			Dependencies(common.DepsAND(buildDeps...)).
			Build())
	}

	// Create E2E test checks
	for _, e2e := range analysis.E2ETests {
		checks = append(checks, common.NewCheck(fmt.Sprintf("e2e:%s", e2e.Name)).
			Kind("e2e_test").
			Planning().
			Options(map[string]any{
				"test_path": e2e.Path,
				"name":      e2e.Name,
			}).
			Dependencies(common.DepsAND(e2e.DependsOn...)).
			Build())
	}

	if len(checks) == 0 {
		log.Printf("[trigger] No checks to create")
		return nil
	}

	log.Printf("[trigger] Creating %d checks", len(checks))

	_, err := t.Orchestrator.WriteNodes(ctx, &pb.WriteNodesRequest{
		WorkPlanId: workPlanID,
		Checks:     checks,
	})
	return err
}
