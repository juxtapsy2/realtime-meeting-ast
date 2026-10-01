package meetings

import "context"

// ownerEmailCtxKey carries the authenticated session email into the meeting
// service. It is set by the HTTP layer and read when a meeting is created, so
// the meeting remembers who owns it (and therefore whose providers apply).
type ownerEmailCtxKey struct{}

// WithOwnerEmail returns a context carrying the authenticated owner's email.
func WithOwnerEmail(ctx context.Context, email string) context.Context {
	return context.WithValue(ctx, ownerEmailCtxKey{}, email)
}

// OwnerEmailFromContext returns the authenticated owner email, or "" when the
// request is unauthenticated (for example with the allowlist gate disabled).
func OwnerEmailFromContext(ctx context.Context) string {
	email, _ := ctx.Value(ownerEmailCtxKey{}).(string)
	return email
}
