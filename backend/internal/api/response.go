// Package api adapts HTTP requests to application services using net/http.
// JSON endpoints share a response envelope; chat writes service events as SSE.
// Handlers bind inputs and extract identity, while services own business rules,
// storage orchestration, and conversion to application error codes.
package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Emulisy/SupaAgent/internal/logging"
	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
	"github.com/Emulisy/SupaAgent/internal/pkg/pagination"
)

// envelope carries a stable application code alongside the HTTP status.
// Code zero means success. Data is omitted when no result payload is present.
type envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// OK writes a successful HTTP 200 envelope, omitting data when it is nil.
func OK(w http.ResponseWriter, data any) {
	writeJSON(w, 200, envelope{Code: 0, Message: "ok", Data: data})
}

// parsePagination delegates query parsing and page limits to the pagination package.
func parsePagination(r *http.Request) pagination.Query {
	return pagination.Parse(r)
}

// forbidden writes the ownership-denial response used by session handlers.
func forbidden(w http.ResponseWriter) {
	writeJSON(w, 403, envelope{Code: 20201, Message: "Access denied"})
}

// Fail preserves errcode.Error status and code values, including wrapped errors.
// Unknown errors become internal failures. Underlying causes are logged but are
// excluded from the client response so storage or provider details stay internal.
func Fail(w http.ResponseWriter, err error) {
	var be *errcode.Error
	if !errors.As(err, &be) {
		be = errcode.ErrInternal.WithCause(err)
	}
	if be.Cause() != nil {
		// Keep internal error details in logs.
		logging.LogError("request failed", "code", strconv.Itoa(be.Code), "msg", be.Message, "cause", be.Cause().Error())
	}
	writeJSON(w, be.HTTPStatus, envelope{Code: be.Code, Message: be.Message})
}
