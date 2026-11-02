// Package errcode defines application failures with stable numeric codes and HTTP statuses.
// Public messages are separate from wrapped diagnostic causes so the API can return
// a useful error without exposing underlying infrastructure details.
package errcode

import (
	"errors"
	"fmt"
	"net/http"
)

// Error carries a transport status, application code, public message, and optional
// cause. Shared package definitions are templates; WithCause and WithMsg return
// copies so one request cannot mutate the message or cause seen by another.
type Error struct {
	HTTPStatus int
	Code       int
	Message    string
	cause      error
}

// Error renders the code and public message, appending the cause when present.
// Use this diagnostic form in logs; HTTP adapters expose Message separately.
func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// Unwrap supports errors.Is/errors.As traversal of the underlying diagnostic cause.
func (e *Error) Unwrap() error { return e.cause }

// Cause exposes the wrapped failure for structured server-side logging.
func (e *Error) Cause() error { return e.cause }

// WithCause copies the template and attaches a diagnostic error while preserving
// its numeric code, HTTP status, and public message.
func (e *Error) WithCause(err error) *Error {
	cp := *e
	cp.cause = err
	return &cp
}

// WithMsg copies the template and formats a request-specific public message.
// Arguments included here may reach clients; internal causes belong in WithCause.
func (e *Error) WithMsg(format string, args ...any) *Error {
	cp := *e
	cp.Message = fmt.Sprintf(format, args...)
	return &cp
}

// Error codes are grouped by domain: 10xxx validation, 20xxx authentication and
// authorization, 30xxx Agents/workflows, 40xxx throttling, 50xxx model services,
// 60xxx documents/memory, 70xxx tools, 80xxx integrations, and 90xxx internal failures.
// Existing codes remain stable even when a particular feature has no active caller.
var (
	ErrInvalidParam = &Error{http.StatusBadRequest, 10001, "Invalid parameters", nil}
	ErrMissingParam = &Error{http.StatusBadRequest, 10002, "Missing required parameters", nil}
	ErrFileTooLarge = &Error{http.StatusBadRequest, 10003, "File exceeds the size limit", nil}
	ErrFileType     = &Error{http.StatusBadRequest, 10004, "Unsupported file type", nil}

	ErrUnauthorized = &Error{http.StatusUnauthorized, 20101, "Authentication required", nil}
	ErrTokenExpired = &Error{http.StatusUnauthorized, 20102, "Your session has expired", nil}
	ErrTokenInvalid = &Error{http.StatusUnauthorized, 20103, "Invalid credentials", nil}
	ErrForbidden    = &Error{http.StatusForbidden, 20201, "Access denied", nil}
	ErrLoginFailed  = &Error{http.StatusUnauthorized, 20202, "Invalid username or password", nil}
	ErrUserLocked   = &Error{http.StatusTooManyRequests, 20203, "Too many failed login attempts. Please try again later.", nil}
	ErrUserExists   = &Error{http.StatusConflict, 20204, "Username or email already exists", nil}

	ErrAgentNotFound = &Error{http.StatusNotFound, 30001, "Agent not found", nil}
	ErrMaxIteration  = &Error{http.StatusInternalServerError, 30002, "Agent exceeded the maximum number of iterations", nil}
	ErrSessionNotFnd = &Error{http.StatusNotFound, 30003, "Session not found", nil}
	ErrRunNotFound   = &Error{http.StatusNotFound, 30004, "Run not found", nil}
	ErrWorkflowDSL   = &Error{http.StatusBadRequest, 30005, "Invalid workflow definition", nil}
	ErrBudget        = &Error{http.StatusInternalServerError, 30006, "Agent token budget exceeded", nil}

	ErrRateLimited = &Error{http.StatusTooManyRequests, 40001, "Too many requests. Please try again later.", nil}

	ErrModelUnavail     = &Error{http.StatusBadGateway, 50001, "Model temporarily unavailable. Please try again later.", nil}
	ErrModelTimeout     = &Error{http.StatusGatewayTimeout, 50002, "Upstream model request timed out", nil}
	ErrProviderNotFound = &Error{http.StatusNotFound, 50003, "Model configuration not found", nil}
	ErrEmbedFailed      = &Error{http.StatusBadGateway, 50004, "Embedding service failed", nil}
	ErrModelQuota       = &Error{http.StatusBadGateway, 50005, "The model provider has insufficient API credits or quota. Choose another model or add credits to the provider account.", nil}
	ErrModelAuth        = &Error{http.StatusBadGateway, 50006, "The model provider rejected its API credentials. Check the provider API key in Models.", nil}
	ErrModelRequest     = &Error{http.StatusBadGateway, 50007, "The model provider rejected the request. Check the model name, parameters, and tool support in Models.", nil}

	ErrParseFailed    = &Error{http.StatusInternalServerError, 60002, "Document parsing failed", nil}
	ErrKBNotFound     = &Error{http.StatusNotFound, 60003, "Knowledge base not found", nil}
	ErrDocNotFound    = &Error{http.StatusNotFound, 60004, "Document not found", nil}
	ErrMemoryNotFound = &Error{http.StatusNotFound, 60005, "Memory not found", nil}

	ErrToolNotFound   = &Error{http.StatusNotFound, 70001, "Tool not found", nil}
	ErrToolFailed     = &Error{http.StatusInternalServerError, 70002, "Tool execution failed", nil}
	ErrToolTimeout    = &Error{http.StatusGatewayTimeout, 70003, "Tool execution timed out", nil}
	ErrSandboxTimeout = &Error{http.StatusGatewayTimeout, 70004, "Sandbox execution timed out", nil}
	ErrSQLForbidden   = &Error{http.StatusForbidden, 70005, "Only read-only SELECT queries are allowed", nil}
	ErrDomainNotAllow = &Error{http.StatusForbidden, 70006, "Target domain is not in the allowlist", nil}

	ErrMCPConnFail  = &Error{http.StatusBadGateway, 80001, "MCP connection failed", nil}
	ErrMCPToolFail  = &Error{http.StatusBadGateway, 80002, "MCP tool call failed", nil}
	ErrA2ATaskFail  = &Error{http.StatusBadGateway, 80003, "A2A task failed", nil}
	ErrA2ACardFetch = &Error{http.StatusBadGateway, 80004, "Failed to fetch the Agent Card", nil}

	ErrInternal     = &Error{http.StatusInternalServerError, 90001, "Internal server error", nil}
	ErrNotImplement = &Error{http.StatusNotImplemented, 90002, "Feature not implemented", nil}
)

// From preserves an application Error found through wrapping. Other failures
// become internal errors with their original cause available for diagnostics.
func From(err error) *Error {
	var be *Error
	if errors.As(err, &be) {
		return be
	}
	return ErrInternal.WithCause(err)
}
