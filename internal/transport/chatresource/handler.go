// Package chatresource projects the canonical conversation port onto scoped
// resource URLs for installed machine clients.
package chatresource

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatadmission"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const maxBody = 4096

// NewHandler requires a verified machine identity. The chat service then
// checks its active installation, granted scope, channel policy and tenant.
func NewHandler(cfg transport.Config, service chatcore.ConversationService) http.Handler {
	mux := http.NewServeMux()
	h := handler{cfg: cfg, service: service}
	mux.HandleFunc("GET /v1/conversations/{id}", h.get)
	mux.HandleFunc("GET /v1/conversations/{id}/events", h.events)
	mux.HandleFunc("POST /v1/conversations/{id}/posts", h.post)
	mux.HandleFunc("GET /v1/conversations/{id}/posts", h.list)
	return mux
}

func (h handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, p, ok := h.admit(w, r, "/hcmnext.chat.v1.ConversationService/GetConversation")
	if !ok {
		return
	}
	id := r.PathValue("id")
	if id == "" || len(id) > 200 {
		writeError(w, http.StatusBadRequest, "chat.invalid_request")
		return
	}
	v, err := h.service.GetConversation(ctx, chatcore.GetConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: id})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationJSON(v))
}

type conversationResponse struct {
	ID             string                    `json:"id"`
	TenantID       string                    `json:"tenant_id"`
	Kind           chatcore.ConversationKind `json:"kind"`
	Name           string                    `json:"name"`
	OwnerID        string                    `json:"owner_id"`
	Revision       uint64                    `json:"revision"`
	Archived       bool                      `json:"archived"`
	Joined         bool                      `json:"joined"`
	MemberCount    uint32                    `json:"member_count"`
	LastActivityAt *string                   `json:"last_activity_at"`
}

func conversationJSON(v chatcore.Conversation) conversationResponse {
	out := conversationResponse{ID: v.ID, TenantID: v.TenantID, Kind: v.Kind, Name: v.Name, OwnerID: v.OwnerID, Revision: v.Revision, Archived: v.Archived, Joined: v.Joined, MemberCount: v.MemberCount}
	if v.LastActivityAt != nil {
		at := v.LastActivityAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		out.LastActivityAt = &at
	}
	return out
}

type handler struct {
	cfg     transport.Config
	service chatcore.ConversationService
}

func (h handler) admit(w http.ResponseWriter, r *http.Request, method string) (context.Context, chatcore.Principal, bool) {
	if h.service == nil {
		writeError(w, http.StatusServiceUnavailable, "chat.unavailable")
		return nil, chatcore.Principal{}, false
	}
	ctx, _, err := transport.Admit(r.Context(), h.cfg, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: method, Kind: transport.KindHTTPEdge})
	if err != nil {
		writeError(w, http.StatusUnauthorized, "chat.unauthenticated")
		return nil, chatcore.Principal{}, false
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || (p.SubjectKind() != trust.SubjectKindAgent && p.SubjectKind() != trust.SubjectKindIntegration) {
		writeError(w, http.StatusForbidden, "chat.machine_identity_required")
		return nil, chatcore.Principal{}, false
	}
	return ctx, chatcore.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject(), Roles: p.Roles()}, true
}

func (h handler) post(w http.ResponseWriter, r *http.Request) {
	ctx, p, ok := h.admit(w, r, "/hcmnext.chat.v1.ConversationService/SendPost")
	if !ok {
		return
	}
	id := r.PathValue("id")
	key := r.Header.Get("Idempotency-Key")
	if id == "" || len(id) > 200 || strings.TrimSpace(key) == "" || len(key) > 200 {
		writeError(w, http.StatusBadRequest, "chat.invalid_request")
		return
	}
	media, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaErr != nil || media != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "chat.content_type")
		return
	}
	var body struct {
		Body     string `json:"body"`
		ParentID string `json:"parent_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "chat.invalid_json")
		return
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "chat.invalid_json")
		return
	}
	if strings.TrimSpace(body.Body) == "" || len(body.Body) > maxBody || utf8.RuneCountInString(body.Body) > 4000 {
		writeError(w, http.StatusBadRequest, "chat.invalid_body")
		return
	}
	v, err := h.service.SendPost(ctx, chatcore.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: id, Body: body.Body, ParentID: body.ParentID, IdempotencyKey: key})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, postJSON(v))
}

func (h handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, p, ok := h.admit(w, r, "/hcmnext.chat.v1.ConversationService/ListPosts")
	if !ok {
		return
	}
	id := r.PathValue("id")
	if id == "" || len(id) > 200 {
		writeError(w, http.StatusBadRequest, "chat.invalid_request")
		return
	}
	size := 50
	if raw := r.URL.Query().Get("page_size"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 100 {
			writeError(w, http.StatusBadRequest, "chat.invalid_page_size")
			return
		}
		size = v
	}
	if len(r.URL.Query().Get("cursor")) > 2048 {
		writeError(w, http.StatusBadRequest, "chat.invalid_cursor")
		return
	}
	v, err := h.service.ListPosts(ctx, chatcore.ListPostsRequest{Principal: p, TenantID: p.TenantID, ConversationID: id, Page: chatcore.Page{Cursor: r.URL.Query().Get("cursor"), PageSize: uint32(size)}, Descending: true})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	posts := make([]any, 0, len(v.Posts))
	for _, post := range v.Posts {
		posts = append(posts, postJSON(post))
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts, "next_cursor": v.NextCursor})
}

func postJSON(p chatcore.Post) any {
	return map[string]any{"id": p.ID, "conversation_id": p.ConversationID, "author_id": p.AuthorID, "body": p.Body, "sequence": p.Sequence, "revision": p.Revision, "parent_id": p.ParentID, "deleted": p.Deleted, "created_at": p.CreatedAt}
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, chatadmission.ErrOverloaded):
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "chat.rate_limited")
	case errors.Is(err, chatcore.ErrPermissionDenied), errors.Is(err, chatcore.ErrNotFound):
		writeError(w, http.StatusNotFound, "chat.not_found")
	case errors.Is(err, chatcore.ErrInvalidArgument):
		writeError(w, http.StatusBadRequest, "chat.invalid_request")
	case errors.Is(err, chatcore.ErrConflict):
		writeError(w, http.StatusConflict, "chat.conflict")
	default:
		writeError(w, http.StatusServiceUnavailable, "chat.unavailable")
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code}})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
