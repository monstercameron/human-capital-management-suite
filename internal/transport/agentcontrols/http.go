// Package agentcontrols is a thin authenticated owner controls boundary.
package agentcontrols

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

const Path = "/api/agent-controls"

var (
	ErrUnauthenticated = errors.New("agent controls: authentication required")
	ErrDenied          = errors.New("agent controls: access denied")
	ErrInvalid         = errors.New("agent controls: invalid request")
	ErrConflict        = errors.New("agent controls: revision conflict")
	ErrUnavailable     = errors.New("agent controls: unavailable")
)

type Reply struct {
	Snapshot productui.AgentControlsSnapshot `json:"snapshot"`
	Export   json.RawMessage                 `json:"export,omitempty"`
}

// Surface derives tenant, actor, grants and allowed actions from verified
// context. Transport never accepts an actor or a tenant in the JSON payload.
type Surface interface {
	Snapshot(context.Context) (Reply, error)
	Control(context.Context, productui.AgentControlsCommand) (Reply, error)
	Draft(context.Context, productui.AgentScheduleDraft) (Reply, error)
	Preview(context.Context, productui.AgentScheduleDraft) (Reply, error)
}

type Handler struct{ Surface Surface }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if h.Surface == nil {
		writeReply(w, Reply{}, ErrUnavailable)
		return
	}
	var reply Reply
	var err error
	switch {
	case r.URL.Path == Path && r.Method == http.MethodGet:
		reply, err = h.Surface.Snapshot(r.Context())
	case r.URL.Path == Path+"/control" && r.Method == http.MethodPost:
		var command productui.AgentControlsCommand
		if err = decodeRequest(w, r, &command); err == nil {
			reply, err = h.Surface.Control(r.Context(), command)
		}
	case (r.URL.Path == Path+"/draft" || r.URL.Path == Path+"/preview") && r.Method == http.MethodPost:
		var draft productui.AgentScheduleDraft
		if err = decodeRequest(w, r, &draft); err == nil {
			if r.URL.Path == Path+"/draft" {
				reply, err = h.Surface.Draft(r.Context(), draft)
			} else {
				reply, err = h.Surface.Preview(r.Context(), draft)
			}
		}
	default:
		http.NotFound(w, r)
		return
	}
	writeReply(w, reply, err)
}

func decodeRequest(w http.ResponseWriter, r *http.Request, out any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return ErrInvalid
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func writeReply(w http.ResponseWriter, reply Reply, err error) {
	if err == nil {
		_ = json.NewEncoder(w).Encode(reply)
		return
	}
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, ErrUnauthenticated):
		status, code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, ErrDenied):
		status, code = http.StatusForbidden, "denied"
	case errors.Is(err, ErrInvalid):
		status, code = http.StatusBadRequest, "invalid"
	case errors.Is(err, ErrConflict):
		status, code = http.StatusConflict, "conflict"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{code})
}
