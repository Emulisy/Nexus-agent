// Middleware provides recovery, CORS, and authentication using net/http.
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Emulisy/SupaAgent/internal/logging"
	"github.com/Emulisy/SupaAgent/internal/security"
)

type contextKey struct{}

var claimsKey contextKey

func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if failure := recover(); failure != nil {
				if failure == http.ErrAbortHandler {
					panic(failure)
				}
				logging.LogError("panic recovered", "path", r.URL.Path, "err", fmt.Sprint(failure))
				writeJSON(w, http.StatusInternalServerError, envelope{Code: 90001, Message: "Internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Auth accepts access tokens only from the Authorization header.
func Auth(jwt *security.JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") == "" {
				writeJSON(w, http.StatusUnauthorized, envelope{Code: 20101, Message: "Authentication required"})
				return
			}
			claims, err := jwt.Parse(strings.TrimPrefix(auth, "Bearer "), false)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, envelope{Code: 20103, Message: "Invalid credentials"})
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
		})
	}
}

func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}
