package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// AgentIconRailQuery selects the viewer's agent list from the persona route:
// GET /api/chat/personas?rail=direct.
const AgentIconRailQuery = "rail"

// AgentIconRailEntry is one of the viewer's direct conversations with an agent:
// the agent's name, its own description and its stored icon. The conversation
// list reads it beside the list, so the sidebar draws every agent with its own
// icon on first paint.
type AgentIconRailEntry struct {
	ConversationID string          `json:"conversation_id"`
	AgentID        string          `json:"agent_id"`
	Name           string          `json:"name"`
	Purpose        string          `json:"purpose"`
	Icon           agenticon.Value `json:"icon"`
	IconRevision   int64           `json:"icon_revision"`
}

type AgentIconRail struct {
	Agents []AgentIconRailEntry `json:"agents"`
}

// AgentIconRailSource lists the caller's own direct conversations with agents.
// It never widens visibility: each entry is a conversation the caller is in,
// with an agent that conversation's directory admits.
type AgentIconRailSource interface {
	RailAgentIcons(context.Context) (AgentIconRail, error)
}

// serveAgentIconRail answers the rail query. It reports false when the request
// is not one, so the directory handler carries on with its own work.
func (h AgentIconDirectoryHandler) serveAgentIconRail(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != personachat.Path || r.Method != http.MethodGet || r.URL.Query().Get(AgentIconRailQuery) != "direct" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	source, ok := h.Source.(AgentIconRailSource)
	if !ok {
		writeAgentIconReply(w, AgentIconReply{}, ErrAgentIconUnavailable)
		return true
	}
	rail, err := source.RailAgentIcons(r.Context())
	if err != nil {
		mapped := ErrAgentIconUnavailable
		switch {
		case errors.Is(err, personachat.ErrUnauthenticated):
			mapped = ErrAgentIconUnauthenticated
		case errors.Is(err, personachat.ErrDenied):
			mapped = ErrAgentIconDenied
		}
		writeAgentIconReply(w, AgentIconReply{}, mapped)
		return true
	}
	if rail.Agents == nil {
		rail.Agents = []AgentIconRailEntry{}
	}
	_ = json.NewEncoder(w).Encode(rail)
	return true
}
