// Package main implements the GoBuilder runner.
// This runner builds Go binaries.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/cmd/ci/runners/common"
)

var (
	runnerID         = flag.String("runner-id", "builder-runner-1", "Runner ID")
	listenAddr       = flag.String("listen", ":50063", "Address to listen on")
	orchestratorAddr = flag.String("orchestrator", "localhost:50051", "Orchestrator address")
	maxConcurrent    = flag.Int("max-concurrent", 4, "Maximum concurrent builds")
	repoRoot         = flag.String("repo-root", ".", "Repository root directory")
	outputDir        = flag.String("output-dir", "bin", "Output directory for binaries")
)

func main() {
	flag.Parse()

	runner := common.NewBaseRunner(*runnerID, "go_builder", *listenAddr, *orchestratorAddr, *maxConcurrent)

	builder := &BuilderRunner{
		BaseRunner: runner,
		repoRoot:   *repoRoot,
		outputDir:  *outputDir,
	}
	runner.SetRunHandler(builder.HandleRun)

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

	log.Println("Shutting down builder runner...")
	runner.Shutdown(ctx)
}

// BuilderRunner handles building Go binaries.
type BuilderRunner struct {
	*common.BaseRunner
	repoRoot  string
	outputDir string
}

// HandleRun processes a build request.
func (b *BuilderRunner) HandleRun(ctx context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	if len(req.AssignedCheckIds) == 0 {
		return nil, fmt.Errorf("no assigned check IDs")
	}

	checkID := req.AssignedCheckIds[0]
	log.Printf("[builder] Starting build for check=%s", checkID)

	// Extract build parameters from args
	var binaryPath, outputName, buildDir, goMod string
	if req.Args != nil {
		if v := req.Args.Fields["binary"]; v != nil {
			binaryPath = v.GetStringValue()
		}
		if v := req.Args.Fields["output"]; v != nil {
			outputName = v.GetStringValue()
		}
		if v := req.Args.Fields["dir"]; v != nil {
			buildDir = v.GetStringValue()
		}
		if v := req.Args.Fields["go_mod"]; v != nil {
			goMod = v.GetStringValue()
		}
	}

	if binaryPath == "" || outputName == "" {
		return b.createFailureResponse(req, "missing binary or output parameter")
	}

	// Determine working directory
	workDir := b.repoRoot
	if goMod != "" {
		// Build from the module directory
		workDir = filepath.Dir(goMod)
		// Adjust binary path to be relative to module
		binaryPath = "."
	} else if buildDir != "" {
		workDir = buildDir
		binaryPath = "."
	}

	// Ensure output directory exists
	outputPath := filepath.Join(b.repoRoot, b.outputDir, outputName)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return b.createFailureResponse(req, fmt.Sprintf("failed to create output directory: %v", err))
	}

	// Build the binary
	startTime := time.Now()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", outputPath, binaryPath)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	// Build result data
	resultData := map[string]any{
		"binary":      binaryPath,
		"output":      outputName,
		"output_path": outputPath,
		"work_dir":    workDir,
		"build_log":   string(output),
		"success":     err == nil,
		"exit_code":   exitCode,
		"duration_ms": duration.Milliseconds(),
	}

	// Check if build succeeded
	if err != nil {
		log.Printf("[builder] Build failed for %s: %v", outputName, err)
		return common.NewResponse().
			FailCheck(checkID, fmt.Sprintf("build failed: %v", err), resultData).
			Build(), nil
	}

	log.Printf("[builder] Build succeeded for %s in %v", outputName, duration)
	return common.NewResponse().
		FinalizeCheck(checkID, resultData).
		Build(), nil
}

func (b *BuilderRunner) createFailureResponse(req *pb.RunRequest, msg string) (*pb.RunResponse, error) {
	log.Printf("[builder] Error: %s", msg)
	return common.NewResponse().
		FailCheck(req.AssignedCheckIds[0], msg, nil).
		Build(), nil
}
