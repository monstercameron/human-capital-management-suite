package workspace

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

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
	OK    bool                        `json:"ok,omitempty"`
	Error *personaAdminCommandProblem `json:"error,omitempty"`
}

type personaAdminCommandProblem struct {
	Code string `json:"code"`
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
	if err := h.personaAdminCommands.ExecutePersonaAdminCommand(admitted.Context(), request); err != nil {
		h.writePersonaAdminCommandFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(personaAdminCommandResponse{OK: true})
}

func personaAdminJSONRequest(r *http.Request) bool {
	return r != nil && strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]), "application/json")
}

func decodePersonaAdminCommand(r *http.Request) (productui.PersonaAdminCommandRequest, error) {
	if r == nil || r.Body == nil {
		return productui.PersonaAdminCommandRequest{}, errors.New("persona admin command body missing")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, personaAdminCommandBodyLimit))
	decoder.DisallowUnknownFields()
	var request productui.PersonaAdminCommandRequest
	if err := decoder.Decode(&request); err != nil {
		return productui.PersonaAdminCommandRequest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return productui.PersonaAdminCommandRequest{}, errors.New("persona admin command must contain one JSON value")
	}
	return request, nil
}

func validPersonaAdminCommandAction(action string) bool {
	switch action {
	case "CREATE_DRAFT", "CREATE_VERSION", "REQUEST_REVIEW", "REVIEW", "PUBLISH", "ROLLBACK", "INSTALL", "SUSPEND", "RETIRE":
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
		}
	}
	h.writePersonaAdminCommandError(w, status, code)
}

func (h *Handler) writePersonaAdminCommandError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(personaAdminCommandResponse{Error: &personaAdminCommandProblem{Code: code}})
}
