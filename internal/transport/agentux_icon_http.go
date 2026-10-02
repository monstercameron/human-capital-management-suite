package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

const AgentIconPath = "/api/agent-controls/icons"

var (
	ErrAgentIconUnauthenticated = errors.New("agent icon: authentication required")
	ErrAgentIconDenied          = errors.New("agent icon: access denied")
	ErrAgentIconInvalid         = errors.New("agent icon: invalid request")
	ErrAgentIconConflict        = errors.New("agent icon: revision conflict")
	ErrAgentIconUnavailable     = errors.New("agent icon: unavailable")
)

type AgentIconCommand struct {
	PersonaID        string `json:"persona_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Action           string `json:"action,omitempty"`
}

type AgentIconReply struct {
	Icon     agenticon.Value `json:"icon"`
	Revision int64           `json:"revision"`
}

// AgentIconSurface derives actor and tenant from the verified request context.
type AgentIconSurface interface {
	ChangeAgentIcon(context.Context, AgentIconCommand) (AgentIconReply, error)
	PreviewAgentIcon(context.Context, AgentIconCommand) (AgentIconReply, error)
}

type AgentIconHandler struct{ Surface AgentIconSurface }

func (h AgentIconHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	action := strings.TrimPrefix(r.URL.Path, AgentIconPath+"/")
	if r.URL.Path != AgentIconPath+"/"+action || (action != "regenerate" && action != "shuffle" && action != "reset" && action != "undo" && action != "preview") {
		http.NotFound(w, r)
		return
	}
	if h.Surface == nil {
		writeAgentIconReply(w, AgentIconReply{}, ErrAgentIconUnavailable)
		return
	}
	var command AgentIconCommand
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
		writeAgentIconReply(w, AgentIconReply{}, ErrAgentIconInvalid)
		return
	}
	var reply AgentIconReply
	var err error
	if action == "preview" {
		reply, err = h.Surface.PreviewAgentIcon(r.Context(), command)
	} else {
		if command.Action != "" && command.Action != action {
			writeAgentIconReply(w, AgentIconReply{}, ErrAgentIconInvalid)
			return
		}
		command.Action = action
		reply, err = h.Surface.ChangeAgentIcon(r.Context(), command)
	}
	writeAgentIconReply(w, reply, err)
}

func writeAgentIconReply(w http.ResponseWriter, reply AgentIconReply, err error) {
	if err == nil {
		_ = json.NewEncoder(w).Encode(reply)
		return
	}
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, ErrAgentIconUnauthenticated):
		status, code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, ErrAgentIconDenied):
		status, code = http.StatusForbidden, "denied"
	case errors.Is(err, ErrAgentIconInvalid):
		status, code = http.StatusBadRequest, "invalid"
	case errors.Is(err, ErrAgentIconConflict):
		status, code = http.StatusConflict, "conflict"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
