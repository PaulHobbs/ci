// Package ci is a stub for testing the ci linter.
// This package provides minimal type stubs so the linter can analyze
// code that imports the real ci package.
package ci

// NodeRef represents a reference to a check or stage node.
type NodeRef struct{}

// DependencyGroup represents a group of dependencies.
type DependencyGroup struct{}

// NewCheck creates a new check builder. Panics if id is empty.
func NewCheck(id string) interface{} { return nil }

// NewStage creates a new stage builder. Panics if id is empty.
func NewStage(id string) interface{} { return nil }

// Check returns a NodeRef for a check with the given ID.
func Check(id string) NodeRef { return NodeRef{} }

// Stage returns a NodeRef for a stage with the given ID.
func Stage(id string) NodeRef { return NodeRef{} }

// DependsOn creates an AND dependency group.
func DependsOn(refs ...string) *DependencyGroup { return nil }

// DependsOnAny creates an OR dependency group.
func DependsOnAny(refs ...string) *DependencyGroup { return nil }

// DependsOnChecks creates an AND dependency group from check IDs.
func DependsOnChecks(ids ...string) *DependencyGroup { return nil }

// DependsOnStages creates an AND dependency group from stage IDs.
func DependsOnStages(ids ...string) *DependencyGroup { return nil }
