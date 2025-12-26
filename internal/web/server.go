package web

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/example/turboci-lite/internal/service"
	"github.com/example/turboci-lite/internal/storage"
)

//go:embed static/*
var staticFiles embed.FS

// Server is the web HTTP server
type Server struct {
	addr     string
	handlers *Handlers
	mux      *http.ServeMux
}

// NewServer creates a new web server
func NewServer(addr string, orchestrator *service.OrchestratorService, store storage.Storage) *Server {
	s := &Server{
		addr:     addr,
		handlers: NewHandlers(orchestrator, store),
		mux:      http.NewServeMux(),
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// API routes - trailing slash enables prefix matching for all /api/workplans/* paths
	s.mux.HandleFunc("/api/workplans/", s.corsMiddleware(s.routeWorkPlans))

	// Serve static files for the React app
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Printf("Warning: failed to load static files: %v", err)
		// Create a fallback handler that returns a development message
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" || r.URL.Path == "/index.html" {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(devHTML))
				return
			}
			http.NotFound(w, r)
		})
		return
	}

	fileServer := http.FileServer(http.FS(staticFS))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Add CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// For SPA routing: serve index.html for non-file paths
		path := r.URL.Path
		if path != "/" && !strings.HasPrefix(path, "/api/") {
			// Check if the file exists
			if _, err := fs.Stat(staticFS, strings.TrimPrefix(path, "/")); err != nil {
				// File doesn't exist, serve index.html for client-side routing
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

// routeWorkPlans routes requests to the appropriate handler based on the path
func (s *Server) routeWorkPlans(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/workplans")

	switch {
	case path == "" || path == "/":
		// GET /api/workplans - list all work plans
		if r.Method == http.MethodGet {
			s.handlers.ListWorkPlans(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}

	case strings.HasSuffix(path, "/timeline"):
		// GET /api/workplans/:id/timeline - get timeline for a work plan
		if r.Method == http.MethodGet {
			s.handlers.GetTimeline(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}

	default:
		// GET /api/workplans/:id - get a specific work plan
		if r.Method == http.MethodGet {
			s.handlers.GetWorkPlan(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// corsMiddleware adds CORS headers to responses
func (s *Server) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Printf("Starting web server on %s", s.addr)
	return http.ListenAndServe(s.addr, s.mux)
}

// Handler returns the HTTP handler for the server
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Development HTML shown when static files are not embedded
const devHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>TurboCI-Lite - Development Mode</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            max-width: 800px;
            margin: 100px auto;
            padding: 20px;
            text-align: center;
            color: #333;
        }
        h1 { color: #2563eb; }
        code {
            background: #f3f4f6;
            padding: 2px 8px;
            border-radius: 4px;
            font-size: 0.9em;
        }
        .instructions {
            text-align: left;
            background: #f9fafb;
            padding: 20px;
            border-radius: 8px;
            margin-top: 30px;
        }
        .instructions pre {
            background: #1f2937;
            color: #f9fafb;
            padding: 15px;
            border-radius: 4px;
            overflow-x: auto;
        }
    </style>
</head>
<body>
    <h1>TurboCI-Lite Web UI</h1>
    <p>The web frontend is not built yet.</p>
    <div class="instructions">
        <h3>To build the frontend:</h3>
        <pre>cd web && npm install && npm run build</pre>
        <h3>For development with hot reload:</h3>
        <pre>cd web && npm install && npm run dev</pre>
        <p>The dev server will proxy API requests to this server.</p>
    </div>
</body>
</html>
`
