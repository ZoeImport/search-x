package baidu

import (
	"context"
	"fmt"

	"web-search-backend/websearch/internal/domain"
	"web-search-backend/websearch/internal/transport"
)

// StrategyStep is one ordered Baidu implementation inside the internal strategy chain.
type StrategyStep struct {
	Name       domain.BaiduStrategyName
	Transport  transport.SearchTransport
	Waiters    []Waiter
	UseBreaker bool
}

// StrategyFallbackPolicy decides whether another Baidu strategy may be attempted.
type StrategyFallbackPolicy interface {
	Allows(domain.Classification) bool
}

// ConservativeStrategyFallback prevents immediate repeated Baidu requests after risk-control responses.
type ConservativeStrategyFallback struct{}

// Allows returns true only for failures where another implementation can plausibly help without escalating risk controls.
func (ConservativeStrategyFallback) Allows(classification domain.Classification) bool {
	switch classification {
	case domain.ClassificationNetworkError,
		domain.ClassificationTimeout,
		domain.ClassificationParseChanged:
		return true
	default:
		return false
	}
}

// StrategyChain stores an immutable ordered set of Baidu strategy steps.
type StrategyChain struct {
	steps  []StrategyStep
	policy StrategyFallbackPolicy
}

// NewStrategyChain validates and creates a Baidu strategy chain.
func NewStrategyChain(policy StrategyFallbackPolicy, steps ...StrategyStep) (*StrategyChain, error) {
	if len(steps) == 0 {
		return nil, fmt.Errorf("Baidu strategy chain is empty")
	}
	if policy == nil {
		policy = ConservativeStrategyFallback{}
	}
	cloned := make([]StrategyStep, len(steps))
	for index, step := range steps {
		if step.Name == "" {
			return nil, fmt.Errorf("Baidu strategy step %d name is empty", index)
		}
		if step.Transport == nil {
			return nil, fmt.Errorf("Baidu strategy step %d transport is nil", index)
		}
		step.Waiters = append([]Waiter(nil), step.Waiters...)
		for waiterIndex, waiter := range step.Waiters {
			if waiter == nil {
				return nil, fmt.Errorf("Baidu strategy step %d waiter %d is nil", index, waiterIndex)
			}
		}
		cloned[index] = step
	}
	return &StrategyChain{steps: cloned, policy: policy}, nil
}

func (chain *StrategyChain) stepsCopy() []StrategyStep {
	if chain == nil {
		return nil
	}
	steps := make([]StrategyStep, len(chain.steps))
	copy(steps, chain.steps)
	return steps
}

func waitForStrategy(ctx context.Context, step StrategyStep) error {
	for _, waiter := range step.Waiters {
		if err := waiter.Wait(ctx); err != nil {
			return err
		}
	}
	return nil
}
