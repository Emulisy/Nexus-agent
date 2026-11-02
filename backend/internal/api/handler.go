// HTTP handlers share the service container and request parsing helpers.
// Authentication claims are installed by middleware; business services receive
// the extracted user ID rather than depending on HTTP request objects.
package api

import (
	"net/http"
	"strconv"

	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
	"github.com/Emulisy/SupaAgent/internal/security"
	"github.com/Emulisy/SupaAgent/internal/service"
)

// Handler holds application services used by the registered HTTP endpoints.
type Handler struct {
	svcs *service.Services
}

// New binds handlers to an already-constructed service container.
func New(svcs *service.Services) *Handler { return &Handler{svcs: svcs} }

// pathID parses the conventional {id} route parameter as a signed 64-bit integer.
func pathID(r *http.Request) (int64, error) {
	return parseID(r.PathValue("id"))
}

// parseID translates malformed base-10 IDs into client validation errors.
func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errcode.ErrInvalidParam.WithMsg("Invalid resource ID").WithCause(err)
	}
	return id, nil
}

// userID reads claims populated by Auth and returns zero when claims are absent.
// Protected endpoints rely on the route group's authentication middleware to
// reject unauthenticated requests before this helper is called.
func userID(r *http.Request) int64 {
	if claims, ok := r.Context().Value(claimsKey).(*security.Claims); ok {
		return claims.UserID
	}
	return 0
}

func isAdmin(r *http.Request) bool {
	claims, ok := r.Context().Value(claimsKey).(*security.Claims)
	return ok && claims.Role == "admin"
}
