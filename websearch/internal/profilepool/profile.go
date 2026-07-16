// Package profilepool manages provider-specific search identities and leases.
package profilepool

import (
	"context"
	"time"

	"web-search-backend/websearch/internal/domain"
)

// State identifies one profile's scheduling lifecycle state.
type State string

const (
	StateWarming     State = "warming"
	StateProbation   State = "probation"
	StateTrusted     State = "trusted"
	StateDegraded    State = "degraded"
	StateQuarantined State = "quarantined"
	StateDraining    State = "draining"
	StateRetired     State = "retired"
)

// Profile owns one complete provider-specific search identity and session.
type Profile interface {
	ID() string
	Provider() domain.ProviderName
	Capacity() int
	Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error)
	Close() error
}

// Result reports one leased execution back to the pool.
type Result struct {
	Succeeded      bool
	Classification domain.Classification
	FinishedAt     time.Time
}

// Snapshot is a concurrency-safe view of one profile.
type Snapshot struct {
	ID                  string
	Provider            domain.ProviderName
	State               State
	Capacity            int
	InFlight            int
	EffectiveSamples    int
	SuccessWeight       float64
	FailureWeight       float64
	RecentEWMA          float64
	ConsecutiveFailures int
	Generation          uint64
}

// ProviderSnapshot summarizes one provider pool for routing and diagnostics.
type ProviderSnapshot struct {
	Provider       domain.ProviderName
	ProfileCount   int
	ServingCount   int
	AvailableSlots int
	InFlight       int
	Profiles       []Snapshot
}
