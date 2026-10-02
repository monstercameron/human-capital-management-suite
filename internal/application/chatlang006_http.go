package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

// ChatlangPath is the translation administration API.
const ChatlangPath = "/api/chat/translation/v1"

// ChatlangSurface is the administration of translation: the workspace's
// settings, a channel's, and the glossary. Every operation authorizes the
// caller against current authority before it reads or writes.
type ChatlangSurface interface {
	View(context.Context, string) (ChatlangView, error)
	PutWorkspace(context.Context, chatlang.Workspace) (chatlang.Workspace, error)
	PutChannel(context.Context, string, chatlang.Channel) error
	AddTerm(context.Context, chatlang.Term) (string, error)
	RemoveTerm(context.Context, string) error
}

var _ ChatlangSurface = (*ChatlangGovernance)(nil)

// ChatlangHandler serves ChatlangPath.
type ChatlangHandler struct{ Surface ChatlangSurface }

type chatlangHTTPError struct {
	Code string `json:"code"`
}

func chatlangJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func chatlangFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, chat.ErrPermissionDenied):
		chatlangJSON(w, http.StatusForbidden, chatlangHTTPError{"denied"})
	case errors.Is(err, chatlang.ErrInvalid):
		chatlangJSON(w, http.StatusBadRequest, chatlangHTTPError{"invalid"})
	default:
		chatlangJSON(w, http.StatusServiceUnavailable, chatlangHTTPError{"unavailable"})
	}
}

func (h ChatlangHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, ChatlangPath+"/")
	method := http.MethodPost
	switch action {
	case "settings":
		method = http.MethodGet
	case "workspace", "channel", "glossary/add", "glossary/remove":
	default:
		http.NotFound(w, r)
		return
	}
	if isNilPersonaOutputPort(h.Surface) {
		if method == http.MethodGet {
			chatFeatureUnavailableWrite(w, "translation_not_composed")
			return
		}
		chatlangFailure(w, chatlang.ErrUnavailable)
		return
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		chatlangJSON(w, http.StatusMethodNotAllowed, chatlangHTTPError{"method"})
		return
	}
	var input struct {
		Workspace    chatlang.Workspace `json:"workspace"`
		Conversation string             `json:"conversation"`
		Translation  chatlang.Switch    `json:"translation"`
		External     chatlang.External  `json:"external"`
		Term         chatlang.Term      `json:"term"`
		ID           string             `json:"id"`
	}
	if method == http.MethodPost {
		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || contentType != "application/json" {
			chatlangFailure(w, chatlang.ErrInvalid)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			chatlangFailure(w, chatlang.ErrInvalid)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			chatlangFailure(w, chatlang.ErrInvalid)
			return
		}
	}
	conversation := r.URL.Query().Get("conversation")
	var err error
	switch action {
	case "workspace":
		_, err = h.Surface.PutWorkspace(r.Context(), input.Workspace)
	case "channel":
		conversation = input.Conversation
		err = h.Surface.PutChannel(r.Context(), conversation, chatlang.Channel{Translation: input.Translation, External: input.External})
	case "glossary/add":
		_, err = h.Surface.AddTerm(r.Context(), input.Term)
	case "glossary/remove":
		err = h.Surface.RemoveTerm(r.Context(), input.ID)
	}
	if err != nil {
		chatlangFailure(w, err)
		return
	}
	view, err := h.Surface.View(r.Context(), conversation)
	if err != nil {
		chatlangFailure(w, err)
		return
	}
	chatlangJSON(w, http.StatusOK, view)
}

// OverlayChatTranslation mounts the translation administration API.
func OverlayChatTranslation(next http.Handler, surface ChatlangSurface, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatlangHandler{Surface: surface}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatlangPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatlangJSON(w, denied.HTTPStatus(), chatlangHTTPError{"denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

// translationSurface is the administration surface of the composed renderings,
// or nil when translation is not composed.
func (s *agentServedAssembly) translationSurface() ChatlangSurface {
	if surface, ok := s.Renderings.(*ChatRenderingSurface); ok && surface != nil && surface.Languages != nil {
		return surface.Languages
	}
	return nil
}

// translationReady reports whether an engine is composed, which is when the
// administration section is offered.
func (s *agentServedAssembly) translationReady() bool {
	if surface, ok := s.Renderings.(*ChatRenderingSurface); ok && surface != nil && surface.Languages != nil {
		return surface.Languages.Engine().Ready
	}
	return false
}
