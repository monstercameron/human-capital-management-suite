package providerreceipt

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// InboundResponse is the redaction-safe response for an accepted callback.
// Signed payload bytes never cross the inbound route response.
type InboundResponse struct {
	Accepted      bool   `json:"accepted"`
	Provider      string `json:"provider"`
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	PayloadDigest string `json:"payload_digest"`
}

type InboundHandler struct {
	Verifier *Verifier
	Now      func() time.Time
}

func NewInboundHandler(verifier *Verifier, now func() time.Time) http.Handler {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return InboundHandler{Verifier: verifier, Now: now}
}

func (h InboundHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || h.Verifier == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(h.Verifier.ep.MaxBytes)+1))
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	parsed, err := h.Verifier.Parse(r.Header, body, h.Now().UTC())
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		if errors.Is(err, ErrDuplicateDifferent) {
			status = http.StatusConflict
		}
		http.Error(w, "callback rejected", status)
		return
	}
	response := InboundResponse{Accepted: true, Provider: parsed.Provider, EventID: parsed.EventID, EventType: parsed.EventType, PayloadDigest: parsed.PayloadDigest}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(response)
}

// InboundPath returns the canonical connection-scoped route used by the edge.
func InboundPath(connection string) string {
	connection = strings.Trim(connection, "/")
	return "/v1/inbound/" + connection
}
