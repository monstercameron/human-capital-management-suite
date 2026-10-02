package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

type AgentIconMentionProfile struct {
	personachat.Profile
	Icon         agenticon.Value `json:"icon"`
	IconRevision int64           `json:"icon_revision"`
}

type AgentIconPostActor struct {
	personachat.PostActor
	Icon         agenticon.Value `json:"icon"`
	IconRevision int64           `json:"icon_revision"`
}

type AgentIconDirectory struct {
	Personas   []AgentIconMentionProfile `json:"personas"`
	PostActors []AgentIconPostActor      `json:"post_actors"`
}

type AgentIconDirectorySource interface {
	DirectoryAgentIcons(context.Context, string) (AgentIconDirectory, error)
}

// AgentIconDirectoryHandler extends only the admitted directory response.
// Progress, retry and every other chat action keep the existing handler.
type AgentIconDirectoryHandler struct {
	Source   AgentIconDirectorySource
	Fallback http.Handler
}

func (h AgentIconDirectoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != personachat.Path || r.Method != http.MethodGet {
		if h.Fallback != nil {
			h.Fallback.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	if h.serveAgentIconRail(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if h.Source == nil {
		writeAgentIconReply(w, AgentIconReply{}, ErrAgentIconUnavailable)
		return
	}
	directory, err := h.Source.DirectoryAgentIcons(r.Context(), r.URL.Query().Get("conversation_id"))
	if err != nil {
		mapped := ErrAgentIconUnavailable
		switch {
		case errors.Is(err, personachat.ErrUnauthenticated):
			mapped = ErrAgentIconUnauthenticated
		case errors.Is(err, personachat.ErrDenied):
			mapped = ErrAgentIconDenied
		case errors.Is(err, personachat.ErrInvalid):
			mapped = ErrAgentIconInvalid
		}
		writeAgentIconReply(w, AgentIconReply{}, mapped)
		return
	}
	_ = json.NewEncoder(w).Encode(directory)
}
