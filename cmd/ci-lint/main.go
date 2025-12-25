// Command ci-lint runs static analysis on ci API usage.
//
// Usage:
//
//	ci-lint ./...
//
// This tool detects common mistakes when using the ci package:
//   - Empty string literals passed to NewCheck(), NewStage(), etc.
//   - Missing RunnerType() calls before Build() on StageBuilder
//   - Potential dependency cycles
//
// For integration with golangci-lint, see pkg/ci/lint documentation.
package main

import (
	"github.com/example/turboci-lite/pkg/ci/lint"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(lint.Analyzer)
}
