package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PathPersonaAdminCommand is the authenticated command endpoint for persona
// administration. The request carries action data only; identity comes from
// the admitted bearer principal.
const PathPersonaAdminCommand = "/workspace/persona-admin/commands"

const personaAdminCommandBodyLimit = 32 << 10

type personaAdminCommandError interface {
	PersonaAdminCommandCode() string
}

type personaAdminCommandResponse struct {
	OK         bool                                    `json:"ok,omitempty"`
	Evaluation *productui.PersonaAdminEvaluationResult `json:"evaluation,omitempty"`
	Error      *personaAdminCommandProblem             `json:"error,omitempty"`
}

type personaAdminCommandProblem struct {
	Code string `json:"code"`
}

type personaAdminDocumentCommandTransport interface {
	ExecutePersonaAdminCommandWithDocumentReferences(context.Context, productui.PersonaAdminCommandRequest, []agentdocref.Reference) error
}

type personaAdminResultCommandTransport interface {
	ExecutePersonaAdminCommandWithResult(context.Context, productui.PersonaAdminCommandRequest) (productui.PersonaAdminEvaluationResult, error)
}

type personaAdminCommandRequest struct {
	productui.PersonaAdminCommandRequest
	DocumentReferences []agentdocref.Reference `json:"document_references"`
}

func (h *Handler) servePersonaAdminCommand(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	principal, _ := trust.FromContext(admitted.Context())
	if principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		h.writePersonaAdminCommandError(w, http.StatusForbidden, "forbidden")
		return
	}
	access, err := h.resolveProductAccess(admitted.Context(), principal)
	if err != nil {
		h.writePersonaAdminCommandError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if !access.can(productui.PagePersonaAdmin, roleaccess.ActionView) {
		h.writePersonaAdminCommandError(w, http.StatusForbidden, "forbidden")
		return
	}
	if h.personaAdminCommands == nil {
		h.writePersonaAdminCommandError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if r.Method != http.MethodPost || !personaAdminJSONRequest(r) {
		h.writePersonaAdminCommandError(w, http.StatusUnsupportedMediaType, "invalid")
		return
	}
	request, err := decodePersonaAdminCommand(r)
	if err != nil || !validPersonaAdminCommandAction(request.Action) {
		h.writePersonaAdminCommandError(w, http.StatusBadRequest, "invalid")
		return
	}
	if !access.configured || !access.can(productui.PagePersonaAdmin, personaAdminCommandPageAction(request.Action)) {
		h.writePersonaAdminCommandError(w, http.StatusForbidden, "forbidden")
		return
	}
	var commandErr error
	var evaluation *productui.PersonaAdminEvaluationResult
	if request.Action == "RUN_EVALUATION" {
		transport, supported := h.personaAdminCommands.(personaAdminResultCommandTransport)
		if !supported {
			h.writePersonaAdminCommandError(w, http.StatusServiceUnavailable, "evaluation_unavailable")
			return
		}
		result, resultErr := transport.ExecutePersonaAdminCommandWithResult(admitted.Context(), request.PersonaAdminCommandRequest)
		commandErr = resultErr
		if resultErr == nil {
			evaluation = &result
		}
	} else if request.DocumentReferences != nil {
		transport, supported := h.personaAdminCommands.(personaAdminDocumentCommandTransport)
		if !supported {
			h.writePersonaAdminCommandError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		commandErr = transport.ExecutePersonaAdminCommandWithDocumentReferences(admitted.Context(), request.PersonaAdminCommandRequest, request.DocumentReferences)
	} else {
		commandErr = h.personaAdminCommands.ExecutePersonaAdminCommand(admitted.Context(), request.PersonaAdminCommandRequest)
	}
	if commandErr != nil {
		h.writePersonaAdminCommandFailure(w, commandErr)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(personaAdminCommandResponse{OK: true, Evaluation: evaluation})
}

func personaAdminJSONRequest(r *http.Request) bool {
	return r != nil && strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]), "application/json")
}

func decodePersonaAdminCommand(r *http.Request) (personaAdminCommandRequest, error) {
	if r == nil || r.Body == nil {
		return personaAdminCommandRequest{}, errors.New("persona admin command body missing")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, personaAdminCommandBodyLimit))
	decoder.DisallowUnknownFields()
	var request personaAdminCommandRequest
	if err := decoder.Decode(&request); err != nil {
		return personaAdminCommandRequest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return personaAdminCommandRequest{}, errors.New("persona admin command must contain one JSON value")
	}
	return request, nil
}

func validPersonaAdminCommandAction(action string) bool {
	switch action {
	case "CREATE_DRAFT", "CREATE_VERSION", "REQUEST_REVIEW", "REVIEW", "RUN_EVALUATION", "PUBLISH", "ROLLBACK", "INSTALL", "UNINSTALL", "REINSTALL", "SUSPEND", "RETIRE", "SET_REACTIONS":
		return true
	default:
		return false
	}
}

func personaAdminCommandPageAction(action string) string {
	switch action {
	case "CREATE_DRAFT":
		return roleaccess.ActionCreate
	case "REVIEW":
		return roleaccess.ActionView
	case "RETIRE":
		return roleaccess.ActionDelete
	default:
		return roleaccess.ActionUpdate
	}
}

func (h *Handler) writePersonaAdminCommandFailure(w http.ResponseWriter, err error) {
	code := "unavailable"
	status := http.StatusServiceUnavailable
	var typed personaAdminCommandError
	if errors.As(err, &typed) {
		switch typed.PersonaAdminCommandCode() {
		case "forbidden":
			code, status = "forbidden", http.StatusForbidden
		case "invalid":
			code, status = "invalid", http.StatusUnprocessableEntity
		case "conflict":
			code, status = "conflict", http.StatusConflict
		case "document_unreadable":
			code, status = "document_unreadable", http.StatusForbidden
		case "evaluation_unavailable":
			code, status = "evaluation_unavailable", http.StatusServiceUnavailable
		case "runtime_unavailable":
			code, status = "runtime_unavailable", http.StatusServiceUnavailable
		}
	}
	// The response carries only the code. The cause goes to the server log so
	// an operator can tell which check refused; without it every refusal of an
	// administration command looked the same.
	if err != nil {
		slog.Warn("hcmnext.persona_admin_command_failed", "code", code, "cause", err.Error())
	}
	h.writePersonaAdminCommandError(w, status, code)
}

func (h *Handler) writePersonaAdminCommandError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(personaAdminCommandResponse{Error: &personaAdminCommandProblem{Code: code}})
}
