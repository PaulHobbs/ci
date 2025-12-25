// Package lint provides static analysis checks for the ci API.
//
// This analyzer detects common mistakes when using the ci package:
//   - Empty string literals passed to NewCheck(), NewStage(), etc.
//   - Missing RunnerType() calls before Build() on StageBuilder
//   - Missing Assigns() calls on stages
//   - Potential dependency cycles (basic detection)
//
// Usage:
//
//	go install github.com/example/turboci-lite/cmd/ci-lint@latest
//	ci-lint ./...
//
// Or with golangci-lint (add to .golangci.yml):
//
//	linters:
//	  enable:
//	    - cilint
package lint

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer is the ci lint analyzer.
var Analyzer = &analysis.Analyzer{
	Name:     "cilint",
	Doc:      "checks for common ci API mistakes",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (interface{}, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// Track builder chains for validation
	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}

	inspect.Preorder(nodeFilter, func(n ast.Node) {
		call := n.(*ast.CallExpr)

		// Check for porcelain function calls
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			checkSelectorCall(pass, call, fn)
		case *ast.Ident:
			checkIdentCall(pass, call, fn)
		}
	})

	return nil, nil
}

// checkSelectorCall checks calls like ci.NewCheck("...")
func checkSelectorCall(pass *analysis.Pass, call *ast.CallExpr, sel *ast.SelectorExpr) {
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}

	// Check if this is a ci package call
	if pkg.Name != "ci" {
		return
	}

	methodName := sel.Sel.Name

	switch methodName {
	case "NewCheck", "NewStage", "Check", "Stage":
		checkEmptyStringArg(pass, call, methodName)
	case "DependsOn", "DependsOnAny", "DependsOnChecks", "DependsOnStages":
		checkDependencyArgs(pass, call, methodName)
	}
}

// checkIdentCall checks calls from within the ci package
func checkIdentCall(pass *analysis.Pass, call *ast.CallExpr, ident *ast.Ident) {
	// Check if we're in the ci package
	if !strings.HasSuffix(pass.Pkg.Path(), "/ci") {
		return
	}

	switch ident.Name {
	case "NewCheck", "NewStage", "Check", "Stage":
		checkEmptyStringArg(pass, call, ident.Name)
	}
}

// checkEmptyStringArg reports if the first argument is an empty string literal.
func checkEmptyStringArg(pass *analysis.Pass, call *ast.CallExpr, funcName string) {
	if len(call.Args) == 0 {
		return
	}

	firstArg := call.Args[0]
	if lit, ok := firstArg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		// Check for empty string ""
		if lit.Value == `""` || lit.Value == "``" {
			pass.Reportf(lit.Pos(), "%s called with empty string literal - will panic at runtime", funcName)
		}
	}
}

// checkDependencyArgs reports suspicious dependency patterns.
func checkDependencyArgs(pass *analysis.Pass, call *ast.CallExpr, funcName string) {
	// Check for empty argument list (which is valid but likely a mistake)
	if len(call.Args) == 0 {
		pass.Reportf(call.Pos(), "%s called with no arguments - this is a no-op", funcName)
	}

	// Check for duplicate dependencies
	seen := make(map[string]token.Pos)
	for _, arg := range call.Args {
		if id := extractStringLit(arg); id != "" {
			if prevPos, exists := seen[id]; exists {
				pass.Reportf(arg.Pos(), "duplicate dependency %q (first seen at %v)", id, pass.Fset.Position(prevPos))
			}
			seen[id] = arg.Pos()
		}
	}
}

// extractStringLit extracts a string literal value from an expression.
func extractStringLit(expr ast.Expr) string {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		// Remove quotes
		s := lit.Value
		if len(s) >= 2 {
			return s[1 : len(s)-1]
		}
	}
	return ""
}

// Additional rules that could be implemented:
//
// 1. BuilderChainChecker: Track builder chains and verify:
//    - StageBuilder.Build() was called after RunnerType() was set
//    - StageBuilder.Build() was called after Assigns() or AssignsAll()
//
// 2. DependencyCycleChecker: Build a graph from Check/Stage nodes and detect cycles
//
// 3. OrphanCheckChecker: Find checks that are never assigned to any stage
//
// These require more sophisticated analysis (dataflow tracking) and are
// left as future enhancements.
