// Package httpx contains transport-level types shared by the public HTTP apps.
package httpx

import (
	"crypto/rand"
	"encoding/hex"
)

// Problem is the stable RFC 9457-style error envelope used by both apps.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Detail    string `json:"detail"`
	RequestID string `json:"request_id"`
	Retryable bool   `json:"retryable"`
	Parameter string `json:"parameter,omitempty"`
}

// NewRequestID returns a collision-resistant request correlation identifier.
func NewRequestID() string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return "req_" + hex.EncodeToString(bytes)
}
