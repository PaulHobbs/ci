package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/turboci-lite/internal/observability"
	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage/sqlite"
)

// testEnv provides a minimal test environment for web tests.
type testEnv struct {
	storage      *sqlite.SQLiteStorage
	orchestrator *service.OrchestratorService
	server       *Server
	dbPath       string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	ctx := context.Background()
	metrics := observability.NewMetrics()

	// Create temp database
	tmpDir := os.TempDir()
	dbPath := filepath.Join(tmpDir, "turboci_web_test_"+t.Name()+".db")
	os.Remove(dbPath)
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")

	storage, err := sqlite.NewWithMetrics(dbPath, metrics)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	if err := storage.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	orchestrator := service.NewOrchestratorWithMetrics(storage, metrics)
	server := NewServer(":0", orchestrator, storage)

	return &testEnv{
		storage:      storage,
		orchestrator: orchestrator,
		server:       server,
		dbPath:       dbPath,
	}
}

func (e *testEnv) cleanup() {
	e.storage.Close()
	if e.dbPath != "" {
		os.Remove(e.dbPath)
		os.Remove(e.dbPath + "-wal")
		os.Remove(e.dbPath + "-shm")
	}
}

// TestAPIRouting verifies that all API routes are correctly matched.
// This is a regression test for a bug where /api/workplans/{id}/timeline
// was falling through to the static file handler due to missing trailing slash.
func TestAPIRouting(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create a work plan to test with
	wp, err := env.orchestrator.CreateWorkPlan(ctx, nil)
	if err != nil {
		t.Fatalf("failed to create work plan: %v", err)
	}

	tests := []struct {
		name           string
		path           string
		wantStatus     int
		wantJSONField  string // field that should exist in JSON response
		allowRedirect  bool   // whether 301 redirect is acceptable
	}{
		{
			name:          "list workplans - trailing slash",
			path:          "/api/workplans/",
			wantStatus:    http.StatusOK,
			wantJSONField: "workPlans",
		},
		{
			name:          "list workplans - no trailing slash redirects",
			path:          "/api/workplans",
			wantStatus:    http.StatusMovedPermanently,
			allowRedirect: true,
		},
		{
			name:          "get workplan by ID",
			path:          "/api/workplans/" + wp.ID,
			wantStatus:    http.StatusOK,
			wantJSONField: "ID",
		},
		{
			name:          "get workplan timeline",
			path:          "/api/workplans/" + wp.ID + "/timeline",
			wantStatus:    http.StatusOK,
			wantJSONField: "workPlanId",
		},
		{
			name:       "get nonexistent workplan",
			path:       "/api/workplans/nonexistent",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "get nonexistent timeline",
			path:       "/api/workplans/nonexistent/timeline",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()

			env.server.Handler().ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				if tt.allowRedirect && rr.Code == http.StatusMovedPermanently {
					// Check redirect location
					loc := rr.Header().Get("Location")
					if loc != tt.path+"/" {
						t.Errorf("redirect to wrong location: got %s, want %s", loc, tt.path+"/")
					}
					return
				}
				t.Errorf("status = %d, want %d; body: %s", rr.Code, tt.wantStatus, rr.Body.String())
				return
			}

			if tt.wantJSONField != "" {
				var result map[string]any
				if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
					t.Errorf("response is not valid JSON: %v; body: %s", err, rr.Body.String())
					return
				}
				if _, ok := result[tt.wantJSONField]; !ok {
					t.Errorf("response missing field %q: %s", tt.wantJSONField, rr.Body.String())
				}
			}
		})
	}
}

// TestAPIRoutingRegression is a specific regression test for the bug where
// requests to /api/workplans/{id}/timeline were served by the static file
// handler instead of the API handler.
func TestAPIRoutingRegression(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()

	wp, err := env.orchestrator.CreateWorkPlan(ctx, nil)
	if err != nil {
		t.Fatalf("failed to create work plan: %v", err)
	}

	// The bug: /api/workplans/{id}/timeline was NOT matched by /api/workplans
	// (no trailing slash), so it fell through to the "/" static file handler.
	// The fix: register /api/workplans/ (with trailing slash) for prefix matching.

	timelinePath := "/api/workplans/" + wp.ID + "/timeline"
	req := httptest.NewRequest(http.MethodGet, timelinePath, nil)
	rr := httptest.NewRecorder()

	env.server.Handler().ServeHTTP(rr, req)

	// Before fix: would return 200 with HTML (index.html) or 404 from static handler
	// After fix: returns 200 with JSON timeline data

	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want %d", timelinePath, rr.Code, http.StatusOK)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("GET %s: Content-Type = %q, want %q", timelinePath, contentType, "application/json")
	}

	var timeline TimelineResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &timeline); err != nil {
		t.Errorf("GET %s: response is not valid TimelineResponse JSON: %v", timelinePath, err)
	}

	if timeline.WorkPlanID != wp.ID {
		t.Errorf("GET %s: workPlanId = %q, want %q", timelinePath, timeline.WorkPlanID, wp.ID)
	}
}
