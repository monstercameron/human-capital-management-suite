package application

import (
	"context"
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

const ChatmapPath = "/api/chat/locations/v1"

type ChatmapSurface interface {
	Attach(context.Context, chat.AttachLocationRequest) (chat.LocationShare, error)
	Read(context.Context, chat.Principal, chat.LocationKey) (chat.LocationShare, error)
	End(context.Context, chat.Principal, chat.LocationKey) error
}
type ChatmapPictures interface {
	Picture(context.Context, chat.Principal, chat.LocationKey, int, chat.MapSize, chat.MapTheme) (chat.MapPicture, error)
}
type ChatmapHTTP struct {
	Surface  ChatmapSurface
	Pictures ChatmapPictures
}
type chatmapBody struct {
	TenantID, ConversationID, PostID, ID string
	PostRevision                         uint64
	Place                                chat.LocationPlace
	ExpiresAt                            *time.Time
	Zoom                                 int
	Size                                 chat.MapSize
	Theme                                chat.MapTheme
	Query                                string
	// Live sharing and settings (CHATMAP-005, CHATMAP-006).
	Live                bool
	LiveIntervalSeconds int
	Policy              chatmapPolicyWire
	Country             string
	Enabled             bool
	Basis               string
	Reason              string
}

func OverlayChatmap(next http.Handler, surface ChatmapSurface, pictures ChatmapPictures, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	h := ChatmapHTTP{Surface: surface, Pictures: pictures}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatmapPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatmapError(w, denied.HTTPStatus(), "request_denied")
			return
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}
func chatmapError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code string `json:"code"`
	}{code})
}
func (h ChatmapHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor, ok := trust.FromContext(r.Context())
	if !ok || actor == nil || !time.Now().Before(actor.ExpiresAt()) {
		chatmapError(w, http.StatusUnauthorized, "session_required")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		chatmapError(w, 405, "method_not_allowed")
		return
	}
	if h.Surface == nil {
		chatmapError(w, 503, "location_unavailable")
		return
	}
	// Coordinates can only appear in the request body, never a page address.
	if r.URL.RawQuery != "" {
		chatmapError(w, 400, "invalid_request")
		return
	}
	var body chatmapBody
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		chatmapError(w, 400, "invalid_request")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		chatmapError(w, 400, "invalid_request")
		return
	}
	p := chat.Principal{TenantID: actor.Tenant().String(), SubjectID: actor.Subject()}
	k := chat.LocationKey{TenantID: body.TenantID, ConversationID: body.ConversationID, PostID: body.PostID, ID: body.ID}
	var result any
	var err error
	switch r.URL.Path {
	case ChatmapPath + "/message":
		messages, ok := h.Surface.(interface {
			MessageLocations(context.Context, chat.Principal, string, string, string) ([]chat.LocationShare, error)
		})
		if !ok {
			err = chat.ErrLocationUnavailable
		} else {
			result, err = messages.MessageLocations(r.Context(), p, k.TenantID, k.ConversationID, k.PostID)
		}
	case ChatmapPath + "/sharing":
		shares, ok := h.Surface.(interface {
			SharingNow(context.Context, chat.Principal, string, string) ([]chat.LocationShare, error)
		})
		if !ok {
			err = chat.ErrLocationUnavailable
		} else {
			result, err = shares.SharingNow(r.Context(), p, k.TenantID, k.ConversationID)
		}
	case ChatmapPath + "/sites":
		sites, ok := h.Surface.(interface {
			SearchLocationSites(context.Context, chat.Principal, string, string, string) ([]chat.LocationSite, error)
		})
		if !ok {
			err = chat.ErrLocationUnavailable
		} else {
			result, err = sites.SearchLocationSites(r.Context(), p, k.TenantID, k.ConversationID, body.Query)
		}
	case ChatmapPath + "/lookup":
		lookup, ok := h.Surface.(interface {
			LookupAddress(context.Context, chat.Principal, string, string, string) ([]chat.LocationPlace, error)
		})
		if !ok {
			err = chat.ErrLocationUnavailable
		} else {
			result, err = lookup.LookupAddress(r.Context(), p, k.TenantID, k.ConversationID, body.Query)
		}
	case ChatmapPath + "/attach":
		result, err = h.Surface.Attach(r.Context(), chat.AttachLocationRequest{Principal: p, TenantID: k.TenantID, ConversationID: k.ConversationID, PostID: k.PostID, PostRevision: body.PostRevision, Place: body.Place, ExpiresAt: body.ExpiresAt, Live: body.Live, LiveInterval: time.Duration(body.LiveIntervalSeconds) * time.Second})
	case ChatmapPath + "/update", ChatmapPath + "/mine", ChatmapPath + "/endmine", ChatmapPath + "/map", ChatmapPath + "/policy", ChatmapPath + "/setpolicy", ChatmapPath + "/setjurisdiction", ChatmapPath + "/settings":
		result, err = h.live(r.Context(), r.URL.Path, p, k, body)
	case ChatmapPath + "/read":
		result, err = h.Surface.Read(r.Context(), p, k)
	case ChatmapPath + "/end":
		err = h.Surface.End(r.Context(), p, k)
		result = struct{ Ended bool }{true}
	case ChatmapPath + "/picture":
		if h.Pictures == nil {
			chatmapError(w, 503, "map_unavailable")
			return
		}
		var image chat.MapPicture
		image, err = h.Pictures.Picture(r.Context(), p, k, body.Zoom, body.Size, body.Theme)
		if err == nil {
			w.Header().Set("Content-Type", image.ContentType)
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
			_, _ = w.Write(image.Image)
			return
		}
	default:
		chatmapError(w, 404, "not_found")
		return
	}
	if err != nil {
		status, code := 503, "location_unavailable"
		switch {
		case errors.Is(err, chat.ErrPermissionDenied):
			status, code = 403, "permission_denied"
		case errors.Is(err, chat.ErrNotFound):
			status, code = 404, "not_found"
		case errors.Is(err, chat.ErrInvalidArgument):
			status, code = 400, "invalid_request"
		case errors.Is(err, chat.ErrConflict):
			status, code = 409, "conflict"
		}
		chatmapError(w, status, code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(result)
}
