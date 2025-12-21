package id

import (
	"github.com/google/uuid"
)

// Generate generates a new unique ID.
func Generate() string {
	return uuid.New().String()
}

// GenerateShort generates a shorter unique ID (first 8 chars of UUID).
func GenerateShort() string {
	return uuid.New().String()[:8]
}
