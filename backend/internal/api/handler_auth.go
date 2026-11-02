// Authentication endpoints translate request bodies to the auth service.
// Passwords and token validation stay in the service/security layers; user
// responses are projected explicitly rather than serializing credential fields.
package api

import (
	"net/http"

	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
	"github.com/Emulisy/SupaAgent/internal/service"
)

// RegisterReq carries account credentials validated by AuthService.Register.
type RegisterReq struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Register handles POST /api/v1/auth/register and returns the account identity and role.
// It does not log in the new account or include its password hash in the response.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req RegisterReq
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	u, err := h.svcs.Auth.Register(ctx, &service.RegisterInput{
		Username: req.Username, Email: req.Email, Password: req.Password,
	})
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"id": u.ID, "username": u.Username, "role": u.Role})
}

// LoginReq supplies a username and password for issuing an access/refresh pair.
type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login rejects missing credentials before asking the auth service to issue tokens.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req LoginReq
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	if req.Username == "" || req.Password == "" {
		Fail(w, errcode.ErrMissingParam.WithMsg("Username and password are required"))
		return
	}
	pair, err := h.svcs.Auth.Login(ctx, req.Username, req.Password)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, pair)
}

// RefreshReq supplies the allowlisted refresh token to rotate.
type RefreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh returns a newly rotated token pair or the auth service's credential error.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req RefreshReq
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	pair, err := h.svcs.Auth.Refresh(ctx, req.RefreshToken)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, pair)
}

// Logout revokes the signed-in user's refresh-token set and returns an empty success result.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.svcs.Auth.Logout(ctx, userID(r)); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// Me returns the current account profile from GET /api/v1/me using verified claims.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, err := h.svcs.Repos.User.GetByID(ctx, userID(r))
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"id": u.ID, "username": u.Username, "email": u.Email, "role": u.Role})
}
