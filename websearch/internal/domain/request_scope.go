package domain

import "context"

type requestScopeKey struct{}

// RequestScope is the immutable correlation identity carried across the search chain.
type RequestScope struct {
	TraceID   string
	RequestID string
}

func WithRequestScope(ctx context.Context, scope RequestScope) context.Context {
	return context.WithValue(ctx, requestScopeKey{}, scope)
}

func RequestScopeFrom(ctx context.Context) (RequestScope, bool) {
	scope, ok := ctx.Value(requestScopeKey{}).(RequestScope)
	return scope, ok
}
