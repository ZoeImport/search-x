package searchtrace

import "time"

// Event is one immutable, ordered observation in a search request lifecycle.
type Event struct {
	TraceID           string         `json:"trace_id"`
	RequestID         string         `json:"request_id"`
	Sequence          uint64         `json:"event_sequence"`
	Type              string         `json:"event_type"`
	OccurredAt        time.Time      `json:"occurred_at"`
	RequestedProvider string         `json:"requested_provider,omitempty"`
	Provider          string         `json:"provider,omitempty"`
	ProfileID         string         `json:"profile_id,omitempty"`
	LeaseID           string         `json:"lease_id,omitempty"`
	Classification    string         `json:"classification,omitempty"`
	QueryHash         string         `json:"query_hash,omitempty"`
	QueryLength       int            `json:"query_length,omitempty"`
	QueryPreview      string         `json:"query_preview,omitempty"`
	Query             string         `json:"query,omitempty"`
	Fields            map[string]any `json:"fields,omitempty"`
}
