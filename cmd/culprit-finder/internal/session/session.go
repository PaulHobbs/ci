package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/example/turboci-lite/culprit/domain"
)

// Session represents a persistent culprit finder session
type Session struct {
	ID             string                      `json:"id"`
	Repository     string                      `json:"repository"`
	GoodRef        string                      `json:"good_ref"`
	BadRef         string                      `json:"bad_ref"`
	TestCommand    string                      `json:"test_command"`
	TestTimeout    time.Duration               `json:"test_timeout"`
	Config         domain.CulpritFinderConfig  `json:"config"`
	Commits        []domain.Commit             `json:"commits"`
	WorktreeDir    string                      `json:"worktree_dir"`
	DatabasePath   string                      `json:"database_path"`
	Status         string                      `json:"status"` // "initialized", "running", "completed", "failed"
	CreatedAt      time.Time                   `json:"created_at"`
	UpdatedAt      time.Time                   `json:"updated_at"`
	WorkPlanID     string                      `json:"work_plan_id,omitempty"`
	Result         *domain.DecodingResult      `json:"result,omitempty"`
	TotalTests     int                         `json:"total_tests"`
	CompletedTests int                         `json:"completed_tests"`
}

const (
	sessionDir  = ".culprit-finder"
	sessionFile = "session.json"
)

// GetSessionDir returns the session directory for the current repo
func GetSessionDir(repoPath string) string {
	return filepath.Join(repoPath, sessionDir)
}

// GetSessionPath returns the path to the session file
func GetSessionPath(repoPath string) string {
	return filepath.Join(GetSessionDir(repoPath), sessionFile)
}

// Load loads the session from disk
func Load(repoPath string) (*Session, error) {
	path := GetSessionPath(repoPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no active session found (use 'culprit-finder start' to begin)")
		}
		return nil, fmt.Errorf("failed to read session: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to parse session: %w", err)
	}

	return &session, nil
}

// Save saves the session to disk
func (s *Session) Save(repoPath string) error {
	s.UpdatedAt = time.Now()

	sessionDir := GetSessionDir(repoPath)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	path := GetSessionPath(repoPath)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write session: %w", err)
	}

	return nil
}

// Delete removes the session from disk
func Delete(repoPath string) error {
	sessionDir := GetSessionDir(repoPath)
	if err := os.RemoveAll(sessionDir); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove session: %w", err)
		}
	}
	return nil
}

// Exists checks if a session exists
func Exists(repoPath string) bool {
	_, err := os.Stat(GetSessionPath(repoPath))
	return err == nil
}

// New creates a new session
func New(
	repoPath string,
	goodRef string,
	badRef string,
	testCommand string,
	testTimeout time.Duration,
	config domain.CulpritFinderConfig,
	commits []domain.Commit,
) *Session {
	now := time.Now()
	id := fmt.Sprintf("session-%d", now.Unix())

	return &Session{
		ID:          id,
		Repository:  repoPath,
		GoodRef:     goodRef,
		BadRef:      badRef,
		TestCommand: testCommand,
		TestTimeout: testTimeout,
		Config:      config,
		Commits:     commits,
		WorktreeDir: filepath.Join(os.TempDir(), "culprit-finder-worktrees"),
		DatabasePath: filepath.Join(GetSessionDir(repoPath), "culprit.db"),
		Status:      "initialized",
		CreatedAt:   now,
		UpdatedAt:   now,
		TotalTests:  calculateTotalTests(len(commits), config),
	}
}

// calculateTotalTests estimates the total number of tests
func calculateTotalTests(n int, config domain.CulpritFinderConfig) int {
	if n <= 0 {
		return 0
	}
	d := config.MaxCulprits
	r := config.Repetitions

	// Formula: 4 * d² * log₂(n) * r
	// Using approximation: log₂(n) ≈ log(n) / log(2)
	log2n := 0
	temp := n
	for temp > 1 {
		temp /= 2
		log2n++
	}

	return 4 * d * d * log2n * r
}
