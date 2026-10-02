package application

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const ChatFiltersPath = "/api/chat/filters/v1"

type ChatFilterHandler struct{ Service *chatfilter.Service }
type chatFilterRequest struct {
	Definition chatfilter.Definition
	Enablement chatfilter.Enablement
	Sample     string
	DryRun     bool
	Channel    string
}
type ChatFilterError struct {
	Code     string           `json:"code"`
	RuleName string           `json:"rule_name,omitempty"`
	Span     *chatfilter.Span `json:"span,omitempty"`
}

func ChatFilterErrorBody(err error) (int, ChatFilterError) {
	var blocked *chatfilter.BlockedError
	switch {
	case errors.As(err, &blocked):
		return http.StatusUnprocessableEntity, ChatFilterError{Code: "content_blocked", RuleName: blocked.RuleName, Span: &blocked.Span}
	case errors.Is(err, chatfilter.ErrDenied):
		return http.StatusForbidden, ChatFilterError{Code: "permission_denied"}
	case errors.Is(err, chatfilter.ErrInvalid):
		return http.StatusBadRequest, ChatFilterError{Code: "invalid_filter"}
	case errors.Is(err, chatfilter.ErrConflict):
		return http.StatusConflict, ChatFilterError{Code: "version_conflict"}
	default:
		return http.StatusServiceUnavailable, ChatFilterError{Code: "filters_unavailable"}
	}
}
func chatFilterJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (h ChatFilterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := trust.FromContext(r.Context())
	if !ok {
		chatFilterJSON(w, http.StatusUnauthorized, ChatFilterError{Code: "unauthenticated"})
		return
	}
	if h.Service == nil {
		chatFilterJSON(w, http.StatusServiceUnavailable, ChatFilterError{Code: "filters_unavailable"})
		return
	}
	a := chatfilter.Actor{Tenant: string(p.Tenant()), Subject: p.Subject(), Channel: r.URL.Query().Get("channel")}
	endpoint := strings.TrimPrefix(r.URL.Path, ChatFiltersPath)
	var result any
	var err error
	if r.Method == http.MethodGet {
		switch endpoint {
		case "", "/":
			var defs []chatfilter.Definition
			defs, err = h.Service.SearchFilters(r.Context(), a, r.URL.Query().Get("q"))
			result = chatmod002PublicDefinitions(defs)
		case "/hits":
			before := int64(0)
			if raw := r.URL.Query().Get("before"); raw != "" {
				before, err = strconv.ParseInt(raw, 10, 64)
				if err != nil || before < 0 {
					chatFilterJSON(w, 400, ChatFilterError{Code: "invalid_cursor"})
					return
				}
			}
			result, err = h.Service.ReadHitsBefore(r.Context(), a, r.URL.Query().Get("q"), before)
		case "/enablements":
			result, err = h.Service.ListEnablements(r.Context(), a)
		default:
			http.NotFound(w, r)
			return
		}
	} else if r.Method == http.MethodPost {
		if endpoint != "/versions" && endpoint != "/enable" && endpoint != "/disable" && endpoint != "/try" {
			http.NotFound(w, r)
			return
		}
		var req chatFilterRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24<<10))
		decoder.DisallowUnknownFields()
		if decodeErr := decoder.Decode(&req); decodeErr != nil {
			chatFilterJSON(w, 400, ChatFilterError{Code: "invalid_request"})
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			chatFilterJSON(w, 400, ChatFilterError{Code: "invalid_request"})
			return
		}
		switch endpoint {
		case "/versions":
			err = h.Service.CreateVersion(r.Context(), a, req.Definition)
		case "/enable", "/disable":
			req.Enablement.Enabled = endpoint == "/enable"
			err = h.Service.Enable(r.Context(), a, req.Enablement, req.DryRun)
		case "/try":
			result, err = h.Service.Try(r.Context(), a, req.Definition, chatfilter.Input{Tenant: a.Tenant, Channel: req.Channel, Subject: a.Subject, Body: req.Sample})
		}
		if result == nil {
			result = map[string]string{"version": chatfilter.APIVersion}
		}
	} else {
		w.Header().Set("Allow", "GET, POST")
		chatFilterJSON(w, 405, ChatFilterError{Code: "method_not_allowed"})
		return
	}
	if err != nil {
		status, body := ChatFilterErrorBody(err)
		chatFilterJSON(w, status, body)
		return
	}
	chatFilterJSON(w, http.StatusOK, result)
}
func OverlayChatFilters(next http.Handler, service *chatfilter.Service, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatFilterHandler{Service: service}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatFiltersPath && !strings.HasPrefix(r.URL.Path, ChatFiltersPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatFilterJSON(w, denied.HTTPStatus(), ChatFilterError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
