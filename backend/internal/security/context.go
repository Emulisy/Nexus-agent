// Execution-context helpers carry user identity outside HTTP request objects.
// Chat and workflow services attach an ID before tool/KB callbacks run; those
// callbacks can then apply owner-filtered storage queries without HTTP dependencies.
package security

import "context"

// userIDContextKey is private to avoid collisions with other context values.
type userIDContextKey struct{}

// WithUserID derives a context carrying the supplied identity; callers establish authentication before attaching it.
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDContextKey{}, userID)
}

// UserIDFromContext accepts only a stored int64 greater than zero and reports absence otherwise.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(userIDContextKey{}).(int64)
	return userID, ok && userID > 0
}
