package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/chengpeng-cp/nexus-agent/internal/agent"
	"github.com/chengpeng-cp/nexus-agent/internal/pkg/errcode"
)

const maxJSONBody = 4 << 20

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// decodeJSON accepts exactly one JSON value and bounds request memory consumption.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		return errcode.ErrInvalidParam.WithMsg("Invalid JSON request body").WithCause(err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errcode.ErrInvalidParam.WithMsg("Expected a single JSON value")
	}
	return nil
}

// serveChatEvents keeps the request alive and flushes every complete SSE frame.
// Request cancellation also reaches the model/tool runner through r.Context().
func serveChatEvents(w http.ResponseWriter, r *http.Request, events <-chan agent.Event) {
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	if err := controller.Flush(); err != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			data, err := json.Marshal(ev.Data)
			if err != nil {
				continue
			}
			if _, err := writeSSE(w, ev.Type, data); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}
