package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

const AgentActionsPath = "/v1/agent-actions/"

// AgentActionCommands is the transport port; its implementation owns all
// identity, catalog, approval and workflow decisions.
type AgentActionCommands interface {
	Compile(context.Context, AgentActionCompileRequest) (AgentActionState, error)
	Submit(context.Context, AgentActionSubmitRequest) (AgentActionState, error)
	Observe(context.Context, string) (AgentActionState, error)
}

func NewAgentActionHandler(commands AgentActionCommands) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if commands == nil {
			http.Error(w, `{"error":"agent_actions_unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		var state AgentActionState
		var err error
		switch {
		case r.Method == http.MethodPost && r.URL.Path == AgentActionsPath+"compile-run":
			var req struct {
				RunID string `json:"run_id"`
			}
			if !decodeAgentAction(w, r, &req) {
				return
			}
			source, ok := commands.(interface {
				CompileRun(context.Context, string) (AgentActionState, error)
			})
			if !ok {
				http.Error(w, `{"error":"agent_actions_unavailable"}`, http.StatusServiceUnavailable)
				return
			}
			state, err = source.CompileRun(r.Context(), req.RunID)
		case r.Method == http.MethodPost && r.URL.Path == AgentActionsPath+"compile":
			var req AgentActionCompileRequest
			if !decodeAgentAction(w, r, &req) {
				return
			}
			state, err = commands.Compile(r.Context(), req)
		case r.Method == http.MethodPost && r.URL.Path == AgentActionsPath+"submit":
			var req AgentActionSubmitRequest
			if !decodeAgentAction(w, r, &req) {
				return
			}
			state, err = commands.Submit(r.Context(), req)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, AgentActionsPath) && len(strings.TrimPrefix(r.URL.Path, AgentActionsPath)) > 0 && !strings.Contains(strings.TrimPrefix(r.URL.Path, AgentActionsPath), "/"):
			state, err = commands.Observe(r.Context(), strings.TrimPrefix(r.URL.Path, AgentActionsPath))
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			status := http.StatusConflict
			switch {
			case errors.Is(err, ErrAgentActionInput), errors.Is(err, app.ErrAgentActionDraft), errors.Is(err, app.ErrAgentActionDefinition):
				status = http.StatusBadRequest
			case errors.Is(err, ErrAgentActionApproval):
				status = http.StatusForbidden
			case errors.Is(err, app.ErrLifecycleWritesUnavailable):
				status = http.StatusServiceUnavailable
			default:
				var owned *envelope.Error
				if errors.As(err, &owned) {
					switch owned.Code() {
					case envelope.CodeUnauthenticated:
						status = http.StatusUnauthorized
					case envelope.CodePermissionDenied:
						status = http.StatusForbidden
					case envelope.CodeNotFound:
						status = http.StatusNotFound
					case envelope.CodeInvalidArgument:
						status = http.StatusBadRequest
					case envelope.CodeUnavailable:
						status = http.StatusServiceUnavailable
					}
				}
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(struct {
				Error string           `json:"error"`
				State AgentActionState `json:"state"`
			}{"agent_action_refused", state})
			return
		}
		_ = json.NewEncoder(w).Encode(state)
	})
}

func decodeAgentAction(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		http.Error(w, `{"error":"invalid_agent_action"}`, http.StatusBadRequest)
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		http.Error(w, `{"error":"invalid_agent_action"}`, http.StatusBadRequest)
		return false
	}
	return true
}
