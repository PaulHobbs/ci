// Package a is a test package for the ci linter.
package a

import "ci"

// Test cases

func emptyNewCheck() {
	ci.NewCheck("") // want "NewCheck called with empty string literal"
}

func emptyNewStage() {
	ci.NewStage("") // want "NewStage called with empty string literal"
}

func emptyCheck() {
	ci.Check("") // want "Check called with empty string literal"
}

func emptyStage() {
	ci.Stage("") // want "Stage called with empty string literal"
}

func emptyDependsOn() {
	ci.DependsOn() // want "DependsOn called with no arguments"
}

func emptyDependsOnAny() {
	ci.DependsOnAny() // want "DependsOnAny called with no arguments"
}

func duplicateDeps() {
	ci.DependsOn("a", "b", "a") // want `duplicate dependency "a"`
}

// Valid cases - should NOT produce warnings

func validNewCheck() {
	ci.NewCheck("my-check")
}

func validNewStage() {
	ci.NewStage("my-stage")
}

func validDependsOn() {
	ci.DependsOn("a", "b", "c")
}
