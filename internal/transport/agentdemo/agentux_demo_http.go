// Package agentdemo exposes the owner-only support inbox simulation boundary.
package agentdemo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const Path = "/api/agent-controls/support-inbox/simulate"

var (
	ErrInvalid     = errors.New("agentdemo: invalid request")
	ErrDenied      = errors.New("agentdemo: owner access required")
	ErrConflict    = errors.New("agentdemo: idempotency conflict")
	ErrUnavailable = errors.New("agentdemo: inbox unavailable")
)

// Email has no tenant or sender-verification field. Actor and tenant come
// exclusively from the authenticated context in the application surface.
type Email struct {
	From           string `json:"from"`
	Subject        string `json:"subject"`
	Body           string `json:"body"`
	IdempotencyKey string `json:"idempotency_key"`
}
type Receipt struct {
	MessageID string `json:"message_id"`
	State     string `json:"state"`
	Replayed  bool   `json:"replayed"`
}
type Surface interface {
	SimulateCustomerEmail(context.Context, Email) (Receipt, error)
}
type Handler struct{ Surface Surface }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path != Path {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.Surface == nil {
		agentuxDemoWriteError(w, ErrUnavailable)
		return
	}
	var input Email
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 72*1024))
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil {
		agentuxDemoWriteError(w, ErrInvalid)
		return
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) {
		agentuxDemoWriteError(w, ErrInvalid)
		return
	}
	receipt, err := h.Surface.SimulateCustomerEmail(r.Context(), input)
	if err != nil {
		agentuxDemoWriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(receipt)
}

func agentuxDemoWriteError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, ErrInvalid):
		status, code = http.StatusBadRequest, "invalid"
	case errors.Is(err, ErrDenied):
		status, code = http.StatusForbidden, "denied"
	case errors.Is(err, ErrConflict):
		status, code = http.StatusConflict, "conflict"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{code})
}
