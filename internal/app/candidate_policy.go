package app

import "fmt"

const (
	minCombinedResultLimit = 1
	maxCombinedResultLimit = 10
	maxCandidateLimit      = 20
)

// CandidateLimit returns the explicit candidate count or the automatic oversampling count.
func CandidateLimit(resultLimit, explicit int) (int, error) {
	if resultLimit < minCombinedResultLimit || resultLimit > maxCombinedResultLimit {
		return 0, fmt.Errorf("result limit must be between %d and %d", minCombinedResultLimit, maxCombinedResultLimit)
	}
	if explicit != 0 {
		if explicit < resultLimit || explicit > maxCandidateLimit {
			return 0, fmt.Errorf("candidate limit must be between %d and %d", resultLimit, maxCandidateLimit)
		}
		return explicit, nil
	}
	candidates := 2*resultLimit + 2
	if candidates > maxCandidateLimit {
		candidates = maxCandidateLimit
	}
	return candidates, nil
}
