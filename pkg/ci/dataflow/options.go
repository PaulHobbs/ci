package dataflow

// Option configures a Workflow.
type Option func(*workflowConfig)

type workflowConfig struct {
	maxConcurrency int
	// Future: orchestrator for distributed mode
}

// WithMaxConcurrency sets the maximum number of concurrent tasks.
// Defaults to unlimited (0).
func WithMaxConcurrency(n int) Option {
	return func(c *workflowConfig) {
		c.maxConcurrency = n
	}
}

// RunOption configures a single Run() call.
type RunOption func(*runConfig)

type runConfig struct {
	name         string
	explicitDeps []string // Additional explicit dependencies
}

// WithName sets a human-readable name for the task (for debugging).
func WithName(name string) RunOption {
	return func(c *runConfig) {
		c.name = name
	}
}

// DependsOn adds explicit dependencies that aren't captured through data flow.
// Use this when a task needs to wait for another but doesn't use its output.
func DependsOn(deps ...*Future) RunOption {
	return func(c *runConfig) {
		for _, d := range deps {
			c.explicitDeps = append(c.explicitDeps, d.taskID)
		}
	}
}
