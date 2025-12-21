package domain

import "fmt"

// CulpritFinderConfig holds configuration for the culprit finder.
type CulpritFinderConfig struct {
	// MaxCulprits is the maximum expected number of culprits (d).
	// This affects the test matrix size. Higher values require more tests
	// but can identify more simultaneous culprits.
	// Default: 2
	MaxCulprits int

	// Repetitions is the number of times to repeat each test group (r).
	// Higher values improve flake robustness but increase test count.
	// Default: 3
	Repetitions int

	// ConfidenceThreshold is the minimum confidence score (0-1) to report a culprit.
	// Lower values report more candidates but with higher false positive risk.
	// Default: 0.8
	ConfidenceThreshold float64

	// FalsePositiveRate is the expected probability that a test passes
	// when it should fail (due to flakiness).
	// Used in the probabilistic decoder.
	// Default: 0.05
	FalsePositiveRate float64

	// FalseNegativeRate is the expected probability that a test fails
	// when it should pass (due to flakiness).
	// Default: 0.10
	FalseNegativeRate float64

	// RandomSeed is the seed for random matrix generation.
	// Use 0 for random seed, or a specific value for reproducibility.
	// Default: 0
	RandomSeed int64
}

// DefaultConfig returns the default configuration.
func DefaultConfig() CulpritFinderConfig {
	return CulpritFinderConfig{
		MaxCulprits:         2,
		Repetitions:         3,
		ConfidenceThreshold: 0.8,
		FalsePositiveRate:   0.05,
		FalseNegativeRate:   0.10,
		RandomSeed:          0,
	}
}

// Validate checks that the configuration is valid.
func (c *CulpritFinderConfig) Validate() error {
	if c.MaxCulprits < 1 {
		return fmt.Errorf("%w: MaxCulprits must be at least 1, got %d",
			ErrInvalidConfig, c.MaxCulprits)
	}
	if c.MaxCulprits > 10 {
		return fmt.Errorf("%w: MaxCulprits must be at most 10, got %d",
			ErrInvalidConfig, c.MaxCulprits)
	}
	if c.Repetitions < 1 {
		return fmt.Errorf("%w: Repetitions must be at least 1, got %d",
			ErrInvalidConfig, c.Repetitions)
	}
	if c.Repetitions > 20 {
		return fmt.Errorf("%w: Repetitions must be at most 20, got %d",
			ErrInvalidConfig, c.Repetitions)
	}
	if c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 1 {
		return fmt.Errorf("%w: ConfidenceThreshold must be between 0 and 1, got %f",
			ErrInvalidConfig, c.ConfidenceThreshold)
	}
	if c.FalsePositiveRate < 0 || c.FalsePositiveRate > 1 {
		return fmt.Errorf("%w: FalsePositiveRate must be between 0 and 1, got %f",
			ErrInvalidConfig, c.FalsePositiveRate)
	}
	if c.FalseNegativeRate < 0 || c.FalseNegativeRate > 1 {
		return fmt.Errorf("%w: FalseNegativeRate must be between 0 and 1, got %f",
			ErrInvalidConfig, c.FalseNegativeRate)
	}
	return nil
}

// WithDefaults returns a new config with defaults applied for zero values.
func (c CulpritFinderConfig) WithDefaults() CulpritFinderConfig {
	defaults := DefaultConfig()
	if c.MaxCulprits == 0 {
		c.MaxCulprits = defaults.MaxCulprits
	}
	if c.Repetitions == 0 {
		c.Repetitions = defaults.Repetitions
	}
	if c.ConfidenceThreshold == 0 {
		c.ConfidenceThreshold = defaults.ConfidenceThreshold
	}
	if c.FalsePositiveRate == 0 {
		c.FalsePositiveRate = defaults.FalsePositiveRate
	}
	if c.FalseNegativeRate == 0 {
		c.FalseNegativeRate = defaults.FalseNegativeRate
	}
	return c
}
