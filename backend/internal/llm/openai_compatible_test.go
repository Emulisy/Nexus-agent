package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chengpeng-cp/nexus-agent/internal/pkg/errcode"
)

func TestModelTimeoutIsNotInternalError(t *testing.T) {
	err := ModelError(fmt.Errorf("stream: %w", context.DeadlineExceeded))
	if err.Code != errcode.ErrModelTimeout.Code || err.HTTPStatus != http.StatusGatewayTimeout {
		t.Fatalf("unexpected timeout error: %v", err)
	}
	if got := ModelError(parseAPIError(429, []byte(`{"error":{"code":"insufficient_quota"}}`))); got.Code != errcode.ErrModelQuota.Code {
		t.Fatalf("lost quota classification: %v", got)
	}
}

func TestProviderErrorsHaveActionablePublicMessages(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		code      int
		retryable bool
	}{
		{"nested quota", 429, `{"error":{"message":"No credits remaining: private-detail","type":"insufficient_quota","code":"credit_balance_exhausted"}}`, 50005, false},
		{"flat balance", 402, `{"message":"Insufficient Balance: private-detail"}`, 50005, false},
		{"authentication", 401, `{"error":{"message":"private-detail","code":"invalid_api_key"}}`, 50006, false},
		{"invalid model", 404, `{"error":{"message":"private-detail"}}`, 50007, false},
		{"numeric provider code", 400, `{"error":{"code":400,"message":"private-detail"}}`, 50007, false},
		{"rate limit", 429, `{"error":{"type":"rate_limit_exceeded","message":"private-detail"}}`, 40001, true},
		{"upstream failure", 503, `private-detail`, 50001, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseAPIError(tt.status, []byte(tt.body))
			public := errcode.From(fmt.Errorf("request: %w", err))
			if public.Code != tt.code || public.HTTPStatus == http.StatusInternalServerError {
				t.Fatalf("unexpected public error: %+v", public)
			}
			if strings.Contains(public.Message, "private-detail") {
				t.Fatal("public error leaked upstream details")
			}
			if !strings.Contains(public.Cause().Error(), "private-detail") {
				t.Fatal("diagnostic cause was lost")
			}
			if IsRetryable(err) != tt.retryable {
				t.Fatalf("retryable = %v, want %v", IsRetryable(err), tt.retryable)
			}
			var appError *errcode.Error
			if !errors.As(err, &appError) {
				t.Fatal("provider error does not retain its application code")
			}
		})
	}
}
