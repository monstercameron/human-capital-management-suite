package application

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const ChannelStatusPath = "/api/chat/v1/channel-status/"

func OverlayChannelStatus(next http.Handler, service chat.ChannelStatusService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChannelStatusPath) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatstateHTTPError(w, "unauthenticated", denied.HTTPStatus())
			return
		}
		ChannelStatusHandler{Service: service}.ServeHTTP(w, r.WithContext(ctx))
	})
}

type ChannelStatusHandler struct{ Service chat.ChannelStatusService }

func chatstateHTTPError(w http.ResponseWriter, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code string `json:"code"`
	}{code})
}

func (h ChannelStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := trust.FromContext(r.Context())
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || !time.Now().Before(p.ExpiresAt()) {
		chatstateHTTPError(w, "unauthenticated", 401)
		return
	}
	if h.Service == nil {
		chatstateHTTPError(w, "unavailable", 503)
		return
	}
	if !strings.HasPrefix(r.URL.Path, ChannelStatusPath) {
		chatstateHTTPError(w, "not_found", 404)
		return
	}
	if r.URL.Path == ChannelStatusPath && r.Method == http.MethodGet && r.URL.Query().Has(channelStatusBatchParam) {
		// CHATBUG-014: the statuses of several conversations in one request
		// (chatperf2_chatstate_batch.go).
		h.serveBatch(w, r, chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()})
		return
	}
	if r.URL.Path == ChannelStatusPath && r.Method == http.MethodGet {
		search, ok := h.Service.(chat.ChannelStatusSearcher)
		if !ok {
			chatstateHTTPError(w, "unavailable", 503)
			return
		}
		archived := r.URL.Query().Get("archived")
		if archived != "true" && archived != "false" {
			chatstateHTTPError(w, "invalid_argument", 400)
			return
		}
		p := chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}
		rows, err := search.SearchChannelStatuses(r.Context(), p, p.TenantID, r.URL.Query().Get("query"), archived == "true")
		if err != nil {
			chatstateHTTPError(w, "unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(rows)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, ChannelStatusPath), "/")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] != "transitions") {
		chatstateHTTPError(w, "not_found", 404)
		return
	}
	principal := chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}
	request := chat.GetConversationRequest{Principal: principal, TenantID: principal.TenantID, ConversationID: parts[0]}
	var value any
	var err error
	switch {
	case r.Method == http.MethodGet && len(parts) == 2:
		value, err = h.Service.AllowedStatusTransitions(r.Context(), request)
	case r.Method == http.MethodGet:
		if snapshots, ok := h.Service.(chat.ChannelStatusSnapshotService); ok {
			value, err = snapshots.GetChannelStatusSnapshot(r.Context(), request)
		} else {
			value, err = h.Service.GetChannelStatus(r.Context(), request)
		}
	case r.Method == http.MethodPost && len(parts) == 1:
		var change chat.ChangeChannelStatusRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&change) != nil || decoder.Decode(&struct{}{}) != io.EOF || (change.TenantID != "" && change.TenantID != principal.TenantID) || (change.ConversationID != "" && change.ConversationID != parts[0]) {
			chatstateHTTPError(w, "invalid_argument", 400)
			return
		}
		change.Principal, change.TenantID, change.ConversationID = principal, principal.TenantID, parts[0]
		value, err = h.Service.ChangeChannelStatus(r.Context(), change)
	default:
		w.Header().Set("Allow", "GET, POST")
		chatstateHTTPError(w, "method_not_allowed", 405)
		return
	}
	if err != nil {
		code, status := chatstateRefusal(err)
		chatstateHTTPError(w, code, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}

// chatstateRefusal is the code and HTTP status a service refusal is answered
// with.
func chatstateRefusal(err error) (code string, status int) {
	code, status = "unavailable", 503
	switch {
	case errors.Is(err, chat.ErrInvalidArgument):
		code, status = "invalid_argument", 400
	case errors.Is(err, chat.ErrUnauthenticated):
		code, status = "unauthenticated", 401
	case errors.Is(err, chat.ErrPermissionDenied):
		code, status = "permission_denied", 403
	case errors.Is(err, chat.ErrNotFound):
		code, status = "not_found", 404
	case errors.Is(err, chat.ErrConflict):
		code, status = "revision_conflict", 409
	case errors.Is(err, chat.ErrChannelHeld):
		code, status = "channel_held", 409
	case errors.Is(err, chat.ErrLastReopener):
		code, status = "last_reopener", 409
	case errors.Is(err, chat.ErrChannelStatus):
		code, status = "channel_status", 409
	}
	return code, status
}
