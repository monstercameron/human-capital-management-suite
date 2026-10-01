package application

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const AgentStudioPath = "/api/agent-studio/"

var ErrAgentStudioHTTPUnavailable = errors.New("application: agent studio http surface unavailable")

// AgentStudioActorSource supplies the server-derived audit chain for model
// generation. It is never accepted from the browser request.
type AgentStudioActorSource interface {
	AgentStudioActor(*http.Request, *trust.Principal) (agentmodel.ActorChain, error)
}

type AgentStudioHTTPConfig struct {
	Drafts    AgentStudioDraftSource
	Generator *AgentStudioModelGenerator
	Apply     *AgentStudioSuggestionService
	Actors    AgentStudioActorSource
}

type AgentStudioHandler struct {
	drafts    AgentStudioDraftSource
	generator *AgentStudioModelGenerator
	apply     *AgentStudioSuggestionService
	actors    AgentStudioActorSource
}

func NewAgentStudioHandler(config AgentStudioHTTPConfig) (http.Handler, error) {
	if config.Drafts == nil || config.Generator == nil || config.Apply == nil || config.Actors == nil {
		return nil, ErrAgentStudioHTTPUnavailable
	}
	return &AgentStudioHandler{drafts: config.Drafts, generator: config.Generator, apply: config.Apply, actors: config.Actors}, nil
}

type agentStudioSuggestRequest struct {
	PersonaID string `json:"persona_id"`
	Goal      string `json:"goal"`
}

func (h *AgentStudioHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || r == nil || !strings.HasPrefix(r.URL.Path, AgentStudioPath) {
		http.NotFound(w, r)
		return
	}
	principal, ok := trust.FromContext(r.Context())
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		http.Error(w, "agent studio unavailable", http.StatusForbidden)
		return
	}
	switch strings.TrimPrefix(r.URL.Path, AgentStudioPath) {
	case "suggest":
		h.suggest(w, r, principal)
	case "apply":
		h.applySuggestion(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *AgentStudioHandler) suggest(w http.ResponseWriter, r *http.Request, principal *trust.Principal) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var request agentStudioSuggestRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || strings.TrimSpace(request.PersonaID) != request.PersonaID || request.PersonaID == "" || strings.TrimSpace(request.Goal) != request.Goal || request.Goal == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if _, err := h.apply.AuthorizeSuggestion(r.Context(), request.PersonaID); err != nil {
		http.Error(w, "agent studio unavailable", http.StatusForbidden)
		return
	}
	draft, err := h.drafts.CurrentAgentStudioDraft(r.Context(), request.PersonaID)
	if err != nil {
		http.Error(w, "draft unavailable", http.StatusConflict)
		return
	}
	actor, err := h.actors.AgentStudioActor(r, principal)
	if err != nil {
		http.Error(w, "agent studio unavailable", http.StatusForbidden)
		return
	}
	suggestion, result, err := h.generator.Suggest(r.Context(), principal.Tenant().String(), actor, draft, request.Goal)
	if err != nil {
		http.Error(w, "suggestion unavailable", http.StatusBadGateway)
		return
	}
	agentStudioWriteJSON(w, http.StatusOK, struct {
		Suggestion   AgentStudioSuggestion `json:"suggestion"`
		PromptDigest string                `json:"prompt_digest"`
		OutputDigest string                `json:"output_digest"`
	}{suggestion, result.PromptDigest, result.OutputDigest})
}

func (h *AgentStudioHandler) applySuggestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	var suggestion AgentStudioSuggestion
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&suggestion) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	receipt, err := h.apply.ApplySelected(r.Context(), suggestion)
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, ErrAgentStudioSuggestionInvalid) || errors.Is(err, ErrAgentStudioSuggestionUnavailable) {
			status = http.StatusBadRequest
		}
		http.Error(w, "suggestion rejected", status)
		return
	}
	agentStudioWriteJSON(w, http.StatusCreated, receipt)
}

func agentStudioWriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
