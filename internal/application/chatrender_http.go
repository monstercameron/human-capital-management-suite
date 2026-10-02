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
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const ChatRenderingPath = "/api/chat/renderings/v1"

// ChatRenderingPort requires current channel policy authorization before every
// operation. Admission supplies identity; membership alone is not policy.
type ChatRenderingPort interface {
	AuthorizeRendering(context.Context, chatstore.RenderingScope, string) error
	ListRenderings(context.Context, chatstore.RenderingScope, []string) ([]chatrender.Rendering, error)
	RequestRendering(context.Context, chatstore.RenderingScope, chatrender.Rendering) error
	LanguageSettings(context.Context, chatstore.RenderingScope, string) (chatrender.Preference, error)
	PutLanguageSettings(context.Context, chatstore.RenderingScope, chatrender.Preference) error
	ReportRendering(context.Context, chatstore.RenderingScope, chatrender.Rendering, string) error
	CorrectRevisionLanguage(context.Context, chatstore.RenderingScope, string, uint64, string) error
	ConversationLanguages(context.Context, chatstore.RenderingScope) (map[string]int, error)
	ReadRenderingSelection(context.Context, chatstore.RenderingScope, string) (chatrender.Rendering, chatrender.Mark, error)
}
type ChatRenderingHandler struct{ Port ChatRenderingPort }
type chatrenderHTTPError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func chatrenderJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func chatrenderHTTPFailure(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, chatrender.ErrDenied), errors.Is(err, chat.ErrPermissionDenied):
		status, code = http.StatusForbidden, "denied"
	case errors.Is(err, chatrender.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid"
	}
	chatrenderJSON(w, status, chatrenderHTTPError{code, code})
}
func (h ChatRenderingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := trust.FromContext(r.Context())
	if !ok || p == nil {
		chatrenderHTTPFailure(w, chatrender.ErrDenied)
		return
	}
	if isNilPersonaOutputPort(h.Port) {
		// A read of a feature that is not composed is a typed "not available"
		// answer, not an error; writes still fail.
		switch strings.TrimPrefix(r.URL.Path, ChatRenderingPath+"/") {
		case "list", "settings", "languages", "selection", "reader", "search", "search-language", "audience":
			if r.Method == http.MethodGet {
				chatFeatureUnavailableWrite(w, "renderings_not_composed")
				return
			}
		}
		chatrenderHTTPFailure(w, chatrender.ErrUnavailable)
		return
	}
	action := strings.TrimPrefix(r.URL.Path, ChatRenderingPath+"/")
	method := http.MethodGet
	switch action {
	case "list", "settings", "languages", "selection", "reader", "search", "search-language", "audience":
	case "request", "report", "correct-language":
		method = http.MethodPost
	default:
		http.NotFound(w, r)
		return
	}
	if action == "settings" && r.Method == http.MethodPut {
		method = http.MethodPut
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		chatrenderJSON(w, http.StatusMethodNotAllowed, chatrenderHTTPError{"method", "method not allowed"})
		return
	}
	scope := chatstore.RenderingScope{Principal: chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}, Tenant: p.Tenant().String(), Conversation: r.URL.Query().Get("conversation")}
	if host := r.URL.Query().Get("tenant"); host != "" {
		scope.Tenant = host
	}
	if action != "settings" && scope.Conversation == "" {
		chatrenderHTTPFailure(w, chatrender.ErrInvalid)
		return
	}
	if err := h.Port.AuthorizeRendering(r.Context(), scope, action); err != nil {
		chatrenderHTTPFailure(w, err)
		return
	}
	var input struct {
		Rendering chatrender.Rendering  `json:"rendering"`
		Settings  chatrender.Preference `json:"settings"`
		Reason    string                `json:"reason"`
		Message   string                `json:"message"`
		Revision  uint64                `json:"revision"`
		Language  string                `json:"language"`
	}
	if method != http.MethodGet {
		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || contentType != "application/json" {
			chatrenderHTTPFailure(w, chatrender.ErrInvalid)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			chatrenderHTTPFailure(w, chatrender.ErrInvalid)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			chatrenderHTTPFailure(w, chatrender.ErrInvalid)
			return
		}
		input.Rendering.Tenant = scope.Tenant
	}
	var out any
	var err error
	switch action {
	case "reader":
		ids := r.URL.Query()["message"]
		if len(ids) == 0 || len(ids) > 100 {
			chatrenderHTTPFailure(w, chatrender.ErrInvalid)
			return
		}
		selections := map[string]chatui.ReaderSelection{}
		for _, id := range ids {
			var selected chatui.ReaderSelection
			selected.Rendering, selected.Mark, err = h.Port.ReadRenderingSelection(r.Context(), scope, id)
			if err != nil {
				break
			}
			selections[id] = selected
		}
		out = selections
	case "search":
		if source, ok := h.Port.(interface {
			SearchRenderings(context.Context, chatstore.RenderingScope, string) ([]chatrender.Rendering, error)
		}); ok {
			out, err = source.SearchRenderings(r.Context(), scope, r.URL.Query().Get("query"))
		} else {
			err = chatrender.ErrUnavailable
		}
	case "search-language":
		if source, ok := h.Port.(interface {
			SearchMessageLanguages(context.Context, chatstore.RenderingScope, string) ([]chatstore.MessageLanguage, error)
		}); ok {
			out, err = source.SearchMessageLanguages(r.Context(), scope, r.URL.Query().Get("language"))
		} else {
			err = chatrender.ErrUnavailable
		}
	case "list":
		out, err = h.Port.ListRenderings(r.Context(), scope, r.URL.Query()["message"])
	case "request":
		err = h.Port.RequestRendering(r.Context(), scope, input.Rendering)
	case "report":
		err = h.Port.ReportRendering(r.Context(), scope, input.Rendering, input.Reason)
	case "correct-language":
		err = h.Port.CorrectRevisionLanguage(r.Context(), scope, input.Message, input.Revision, input.Language)
	case "settings":
		if method == http.MethodPut {
			err = h.Port.PutLanguageSettings(r.Context(), scope, input.Settings)
		} else {
			locale := r.Header.Get("Accept-Language")
			out, err = h.Port.LanguageSettings(r.Context(), scope, locale)
			if recorder, ok := h.Port.(chatlangLocalePort); ok && err == nil && locale != "" {
				// CHATLANG-002: the language the page was opened in is the default
				// reading language of a person who has not chosen one. Best effort.
				_ = recorder.RecordInterfaceLocale(r.Context(), scope, locale)
			}
		}
	case "audience":
		if source, ok := h.Port.(chatlangAudiencePort); ok {
			out, err = source.AudienceView(r.Context(), scope, r.URL.Query().Get("language"))
		} else {
			err = chatrender.ErrUnavailable
		}
	case "languages":
		out, err = h.Port.ConversationLanguages(r.Context(), scope)
	case "selection":
		var rendering chatrender.Rendering
		var mark chatrender.Mark
		rendering, mark, err = h.Port.ReadRenderingSelection(r.Context(), scope, r.URL.Query().Get("message"))
		out = struct {
			Rendering chatrender.Rendering `json:"rendering"`
			Mark      chatrender.Mark      `json:"mark"`
		}{rendering, mark}
	}
	if err != nil {
		chatrenderHTTPFailure(w, err)
		return
	}
	if out == nil {
		out = struct {
			OK bool `json:"ok"`
		}{true}
	}
	chatrenderJSON(w, http.StatusOK, out)
}
func OverlayChatRenderings(next http.Handler, port ChatRenderingPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatRenderingHandler{Port: port}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatRenderingPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatrenderJSON(w, denied.HTTPStatus(), chatrenderHTTPError{"denied", "request denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
