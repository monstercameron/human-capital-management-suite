package application

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChatAgentReactionPath serves the settings pages' reads and writes of the
// workspace and conversation reaction switches (AGENTUX-052).
const ChatAgentReactionPath = "/api/chat/agent-reactions/"

// AgentReactionHTTP answers the reaction switches: policy and settings read
// them, switch stores one.
type AgentReactionHTTP struct{ Service AgentReactionService }

type agentReactionHTTPError struct {
	Code string `json:"code"`
}

func agentReactionJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func agentReactionError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, chatcore.ErrPermissionDenied):
		status, code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, chatcore.ErrInvalidArgument):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, chatcore.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, chatcore.ErrAgentReactionsDisabled):
		status, code = http.StatusConflict, "disabled"
	}
	agentReactionJSON(w, status, agentReactionHTTPError{Code: code})
}

func (h AgentReactionHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := trust.FromContext(r.Context())
	_, admitted := transport.InvocationFromContext(r.Context())
	if !ok || p == nil || !admitted {
		agentReactionError(w, chatcore.ErrPermissionDenied)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		agentReactionJSON(w, http.StatusMethodNotAllowed, agentReactionHTTPError{Code: "method_not_allowed"})
		return
	}
	principal := chatcore.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}
	decode := func(value any) bool {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		d.DisallowUnknownFields()
		if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
			agentReactionError(w, chatcore.ErrInvalidArgument)
			return false
		}
		return true
	}
	switch strings.TrimPrefix(r.URL.Path, ChatAgentReactionPath) {
	case "policy":
		var input struct{ TenantID, ConversationID string }
		if !decode(&input) {
			return
		}
		out, err := h.Service.Policy(r.Context(), principal, input.TenantID, input.ConversationID)
		if err != nil {
			agentReactionError(w, err)
			return
		}
		agentReactionJSON(w, http.StatusOK, out)
	case "settings":
		var input struct{ TenantID string }
		if !decode(&input) {
			return
		}
		out, err := h.Service.Settings(r.Context(), principal, input.TenantID)
		if err != nil {
			agentReactionError(w, err)
			return
		}
		agentReactionJSON(w, http.StatusOK, out)
	case "switch":
		var input AgentReactionSwitchRequest
		if !decode(&input) {
			return
		}
		if err := h.Service.SetSwitch(r.Context(), principal, input); err != nil {
			agentReactionError(w, err)
			return
		}
		agentReactionJSON(w, http.StatusOK, struct {
			Saved bool `json:"saved"`
		}{true})
	default:
		agentReactionJSON(w, http.StatusNotFound, agentReactionHTTPError{Code: "not_found"})
	}
}

// agentReactionServiceFor composes the switch service over the served chat: the
// store holds the rows, the renderings' admins decide who may change them, and
// the service proves membership for a conversation read.
func agentReactionServiceFor(runtime composedChat) AgentReactionService {
	service := AgentReactionService{Conversations: runtime.service}
	if runtime.store != nil {
		service.Store = runtime.store
	}
	if runtime.renderings != nil && runtime.renderings.Languages != nil {
		service.Admin = runtime.renderings.Languages.Admins
	}
	return service
}

// OverlayAgentReactions serves the reaction switches beside the other chat
// overlays.
func OverlayAgentReactions(next http.Handler, service AgentReactionService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatAgentReactionPath) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, err := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if err != nil {
			agentReactionJSON(w, err.HTTPStatus(), agentReactionHTTPError{Code: "permission_denied"})
			return
		}
		AgentReactionHTTP{Service: service}.ServeHTTP(w, r.WithContext(ctx))
	})
}
