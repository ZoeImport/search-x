package profilepool

import "time"

// TrustConfig controls probation promotion and recent-result weighting.
type TrustConfig struct {
	MinSamples             int
	MinSuccessRate         float64
	MinRecentEWMA          float64
	MaxConsecutiveFailures int
	RecentAlpha            float64
	DecayHalfLife          time.Duration
}

func (config TrustConfig) normalized() TrustConfig {
	if config.MinSamples <= 0 {
		config.MinSamples = 20
	}
	if config.MinSuccessRate <= 0 || config.MinSuccessRate > 1 {
		config.MinSuccessRate = 0.90
	}
	if config.MinRecentEWMA <= 0 || config.MinRecentEWMA > 1 {
		config.MinRecentEWMA = 0.90
	}
	if config.MaxConsecutiveFailures <= 0 {
		config.MaxConsecutiveFailures = 3
	}
	if config.RecentAlpha <= 0 || config.RecentAlpha > 1 {
		config.RecentAlpha = 0.20
	}
	if config.DecayHalfLife <= 0 {
		config.DecayHalfLife = 7 * 24 * time.Hour
	}
	return config
}

func successRate(success, failure float64) float64 {
	const alpha = 1.0
	const beta = 1.0
	return (success + alpha) / (success + failure + alpha + beta)
}
