package resilience

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"
)

type Jitter struct {
	min    time.Duration
	max    time.Duration
	random func(int64) (int64, error)
}

func NewJitter(minimum, maximum time.Duration) (*Jitter, error) {
	return newJitter(minimum, maximum, func(upperExclusive int64) (int64, error) {
		value, err := rand.Int(rand.Reader, big.NewInt(upperExclusive))
		if err != nil {
			return 0, err
		}
		return value.Int64(), nil
	})
}

func newJitter(minimum, maximum time.Duration, random func(int64) (int64, error)) (*Jitter, error) {
	if minimum < 0 || maximum < minimum {
		return nil, fmt.Errorf("invalid jitter range: min=%s max=%s", minimum, maximum)
	}
	if random == nil {
		return nil, fmt.Errorf("jitter random source is nil")
	}
	return &Jitter{min: minimum, max: maximum, random: random}, nil
}

func (j *Jitter) Wait(ctx context.Context) error {
	delay := j.min
	if span := j.max - j.min; span > 0 {
		offset, err := j.random(int64(span) + 1)
		if err != nil {
			return fmt.Errorf("generate jitter: %w", err)
		}
		delay += time.Duration(offset)
	}
	if delay == 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
