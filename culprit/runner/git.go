package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/turboci-lite/culprit/domain"
)

// GitCommitMaterializer implements CommitMaterializer using git worktrees.
// It creates worktrees and cherry-picks commits onto the base ref.
type GitCommitMaterializer struct {
	mu sync.Mutex

	// RepoPath is the path to the git repository.
	RepoPath string

	// WorktreeBaseDir is the base directory for creating worktrees.
	// If empty, os.TempDir() is used.
	WorktreeBaseDir string

	// idCounter is used to generate unique worktree names.
	idCounter int

	// activeWorktrees tracks worktrees that haven't been cleaned up.
	activeWorktrees map[string]string // ID -> worktree path
}

// NewGitMaterializer creates a new GitCommitMaterializer.
func NewGitMaterializer(repoPath string) *GitCommitMaterializer {
	return &GitCommitMaterializer{
		RepoPath:        repoPath,
		activeWorktrees: make(map[string]string),
	}
}

// WithWorktreeBaseDir sets the base directory for worktrees.
func (m *GitCommitMaterializer) WithWorktreeBaseDir(dir string) *GitCommitMaterializer {
	m.WorktreeBaseDir = dir
	return m
}

// Materialize creates a git worktree at baseRef and cherry-picks the given commits.
func (m *GitCommitMaterializer) Materialize(ctx context.Context, baseRef string, commits []domain.Commit) (*MaterializedState, error) {
	m.mu.Lock()
	m.idCounter++
	id := fmt.Sprintf("culprit-worktree-%d-%d", time.Now().Unix(), m.idCounter)
	m.mu.Unlock()

	// Determine worktree directory
	baseDir := m.WorktreeBaseDir
	if baseDir == "" {
		baseDir = os.TempDir()
	}
	worktreePath := filepath.Join(baseDir, id)

	// Create the worktree
	if err := m.createWorktree(ctx, worktreePath, baseRef); err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	// Sort commits by index to ensure correct order
	sortedCommits := make([]domain.Commit, len(commits))
	copy(sortedCommits, commits)
	sort.Slice(sortedCommits, func(i, j int) bool {
		return sortedCommits[i].Index < sortedCommits[j].Index
	})

	// Cherry-pick each commit
	for _, commit := range sortedCommits {
		if err := m.cherryPick(ctx, worktreePath, commit.SHA); err != nil {
			// Clean up on failure
			_ = m.removeWorktree(ctx, worktreePath)
			return nil, fmt.Errorf("failed to cherry-pick %s: %w", commit.SHA, err)
		}
	}

	m.mu.Lock()
	m.activeWorktrees[id] = worktreePath
	m.mu.Unlock()

	return &MaterializedState{
		ID:        id,
		WorkDir:   worktreePath,
		BaseRef:   baseRef,
		Commits:   commits,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// Cleanup removes the worktree for a materialized state.
func (m *GitCommitMaterializer) Cleanup(ctx context.Context, state *MaterializedState) error {
	m.mu.Lock()
	worktreePath, exists := m.activeWorktrees[state.ID]
	if exists {
		delete(m.activeWorktrees, state.ID)
	}
	m.mu.Unlock()

	if !exists {
		// Already cleaned up
		return nil
	}

	return m.removeWorktree(ctx, worktreePath)
}

// CleanupAll removes all active worktrees.
func (m *GitCommitMaterializer) CleanupAll(ctx context.Context) error {
	m.mu.Lock()
	worktrees := make(map[string]string)
	for k, v := range m.activeWorktrees {
		worktrees[k] = v
	}
	m.activeWorktrees = make(map[string]string)
	m.mu.Unlock()

	var lastErr error
	for _, path := range worktrees {
		if err := m.removeWorktree(ctx, path); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// createWorktree creates a new git worktree at the given path.
func (m *GitCommitMaterializer) createWorktree(ctx context.Context, path, ref string) error {
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", path, ref)
	cmd.Dir = m.RepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add failed: %w\noutput: %s", err, string(output))
	}
	return nil
}

// removeWorktree removes a git worktree.
func (m *GitCommitMaterializer) removeWorktree(ctx context.Context, path string) error {
	// First, remove the directory
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("failed to remove worktree directory: %w", err)
	}

	// Then, prune the worktree from git's tracking
	cmd := exec.CommandContext(ctx, "git", "worktree", "prune")
	cmd.Dir = m.RepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree prune failed: %w\noutput: %s", err, string(output))
	}
	return nil
}

// cherryPick cherry-picks a single commit into the worktree.
func (m *GitCommitMaterializer) cherryPick(ctx context.Context, worktreePath, sha string) error {
	// Use --no-commit to stage the changes without creating a commit
	// This allows testing the combined state of multiple commits
	cmd := exec.CommandContext(ctx, "git", "cherry-pick", "--no-commit", sha)
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's a merge conflict
		if strings.Contains(string(output), "conflict") {
			return fmt.Errorf("%w: cherry-pick conflict for %s: %s",
				domain.ErrMaterializationFailed, sha, string(output))
		}
		return fmt.Errorf("cherry-pick failed for %s: %w\noutput: %s", sha, err, string(output))
	}
	return nil
}

// CommandTestRunner implements TestRunner by executing a shell command.
type CommandTestRunner struct {
	// Shell is the shell to use for executing commands.
	// Defaults to "/bin/sh".
	Shell string

	// ShellArg is the argument to pass to the shell before the command.
	// Defaults to "-c".
	ShellArg string
}

// NewCommandTestRunner creates a new CommandTestRunner.
func NewCommandTestRunner() *CommandTestRunner {
	return &CommandTestRunner{
		Shell:    "/bin/sh",
		ShellArg: "-c",
	}
}

// Run executes the test command in the materialized state's working directory.
func (r *CommandTestRunner) Run(ctx context.Context, state *MaterializedState, config TestConfig) (*domain.TestGroupResult, error) {
	// Determine working directory
	workDir := config.WorkDir
	if workDir == "" {
		workDir = state.WorkDir
	}

	// Set up context with timeout
	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	// Create command
	shell := r.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	shellArg := r.ShellArg
	if shellArg == "" {
		shellArg = "-c"
	}

	cmd := exec.CommandContext(ctx, shell, shellArg, config.Command)
	cmd.Dir = workDir

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range config.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Run command and capture output
	start := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	result := &domain.TestGroupResult{
		GroupID:    state.ID,
		Duration:   duration,
		Logs:       string(output),
		ExecutedAt: time.Now().UTC(),
	}

	// Determine outcome
	if ctx.Err() != nil {
		// Context cancelled or timed out - treat as infra failure
		result.Outcome = domain.OutcomeInfra
		result.Logs = fmt.Sprintf("Test cancelled or timed out: %v\n%s", ctx.Err(), result.Logs)
	} else if err != nil {
		// Command failed - test failure
		result.Outcome = domain.OutcomeFail
	} else {
		// Command succeeded - test pass
		result.Outcome = domain.OutcomePass
	}

	return result, nil
}
