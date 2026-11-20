package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3779, slice 1: legal holds on a conversation, from the Chat settings
// page. Placing, releasing and listing holds is workspace administration,
// decided by the verified caller's current administrator role and never by a
// field of the request. A hold names a conversation the administrator can open:
// a conversation they cannot open is answered as not found, so the hold form
// never confirms that it exists. Every place and release is audited by the
// records service in the same transaction as the hold itself.

// ChatHoldsPath serves list, place, release and the per-conversation check.
const ChatHoldsPath = "/api/chat/holds/v1/"

// chatHoldChoiceLimit bounds the conversations offered to the place form.
const chatHoldChoiceLimit = 100

// ChatHoldView is one hold as the page shows it. It carries no message text and
// no person ids: who acted is only "you" or someone else.
type ChatHoldView struct {
	ID               string `json:"id"`
	ConversationID   string `json:"conversation_id"`
	ConversationName string `json:"conversation_name,omitempty"`
	Matter           string `json:"matter,omitempty"`
	Reason           string `json:"reason"`
	PlacedAt         string `json:"placed_at"`
	PlacedByMe       bool   `json:"placed_by_me"`
	Active           bool   `json:"active"`
	ReleasedAt       string `json:"released_at,omitempty"`
	ReleasedByMe     bool   `json:"released_by_me,omitempty"`
	ReleaseReason    string `json:"release_reason,omitempty"`
}

// ChatHoldChoice is a conversation the administrator may place a hold on.
type ChatHoldChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// ChatHoldsList is the page: every hold, and the conversations the form offers.
type ChatHoldsList struct {
	Holds         []ChatHoldView   `json:"holds"`
	Conversations []ChatHoldChoice `json:"conversations"`
}

// ChatHoldState answers whether one conversation is under an active hold.
type ChatHoldState struct {
	Held  bool `json:"held"`
	Count int  `json:"count"`
}

// ChatHoldsService is what the HTTP edge calls.
type ChatHoldsService interface {
	ListHolds(context.Context, chatcore.Principal) (ChatHoldsList, error)
	PlaceHold(ctx context.Context, p chatcore.Principal, conversation, matter, reason string) (ChatHoldView, error)
	ReleaseHold(ctx context.Context, p chatcore.Principal, holdID, reason string) (ChatHoldView, error)
	ConversationHold(ctx context.Context, p chatcore.Principal, conversation string) (ChatHoldState, error)
}

var _ ChatHoldsService = (*ChatExtensions)(nil)

func holdView(h chatrecords.Hold, p chatcore.Principal, names map[string]string) ChatHoldView {
	v := ChatHoldView{ID: h.HoldID, ConversationID: h.ConversationID, ConversationName: names[h.ConversationID], Matter: h.MatterRef, Reason: h.Reason, PlacedAt: h.PlacedAt.UTC().Format(time.RFC3339), PlacedByMe: h.PlacedBy == p.SubjectID, Active: h.Active()}
	if h.ReleasedAt != nil {
		v.ReleasedAt, v.ReleasedByMe, v.ReleaseReason = h.ReleasedAt.UTC().Format(time.RFC3339), h.ReleasedBy == p.SubjectID, h.ReleaseReason
	}
	return v
}

// openConversation decides that the caller may open the conversation. Anything
// short of that reads as not found.
func (s *ChatExtensions) openHoldConversation(ctx context.Context, p chatcore.Principal, conversation string) (chatcore.Conversation, error) {
	if s.Conversations == nil {
		return chatcore.Conversation{}, chatcore.ErrUnavailable
	}
	if strings.TrimSpace(conversation) == "" {
		return chatcore.Conversation{}, chatcore.ErrNotFound
	}
	c, err := s.Conversations.GetConversation(ctx, chatcore.GetConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: conversation})
	switch {
	case err == nil:
		return c, nil
	case errors.Is(err, chatcore.ErrNotFound), errors.Is(err, chatcore.ErrPermissionDenied):
		return chatcore.Conversation{}, chatcore.ErrNotFound
	default:
		return chatcore.Conversation{}, chatcore.ErrUnavailable
	}
}

// ListHolds returns the workspace's holds and the conversations the caller can
// place one on, for a workspace administrator.
func (s *ChatExtensions) ListHolds(ctx context.Context, p chatcore.Principal) (ChatHoldsList, error) {
	if s == nil {
		return ChatHoldsList{}, chatcore.ErrUnavailable
	}
	ctx, err := s.retentionContext(ctx, p)
	if err != nil {
		return ChatHoldsList{}, err
	}
	holds, err := s.Records.ListHolds(ctx, p.SubjectID, p.TenantID)
	if err != nil {
		return ChatHoldsList{}, err
	}
	out := ChatHoldsList{Holds: []ChatHoldView{}, Conversations: []ChatHoldChoice{}}
	names := map[string]string{}
	if s.Conversations != nil {
		// Only conversations the caller can open are named or offered.
		page, err := s.Conversations.ListConversations(ctx, chatcore.ListConversationsRequest{Principal: p, TenantID: p.TenantID, Page: chatcore.Page{PageSize: chatHoldChoiceLimit}})
		if err != nil && !errors.Is(err, chatcore.ErrPermissionDenied) && !errors.Is(err, chatcore.ErrNotFound) {
			return ChatHoldsList{}, chatcore.ErrUnavailable
		}
		for _, c := range page.Conversations {
			if c.Archived || c.ID == "" {
				continue
			}
			names[c.ID] = c.Name
			out.Conversations = append(out.Conversations, ChatHoldChoice{ID: c.ID, Name: c.Name, Kind: string(c.Kind)})
		}
	}
	for _, h := range holds {
		out.Holds = append(out.Holds, holdView(h, p, names))
	}
	return out, nil
}

// PlaceHold places an active hold on a conversation the caller can open.
func (s *ChatExtensions) PlaceHold(ctx context.Context, p chatcore.Principal, conversation, matter, reason string) (ChatHoldView, error) {
	if s == nil {
		return ChatHoldView{}, chatcore.ErrUnavailable
	}
	ctx, err := s.retentionContext(ctx, p)
	if err != nil {
		return ChatHoldView{}, err
	}
	c, err := s.openHoldConversation(ctx, p, conversation)
	if err != nil {
		return ChatHoldView{}, err
	}
	hold, err := s.Records.PlaceConversationHold(ctx, p.SubjectID, p.TenantID, c.ID, uuid.NewString(), matter, reason)
	if err != nil {
		return ChatHoldView{}, err
	}
	return holdView(hold, p, map[string]string{c.ID: c.Name}), nil
}

// ReleaseHold lifts a hold. Releasing needs the administrator role and a
// reason; it does not need to open the conversation, which may have been
// archived or left since.
func (s *ChatExtensions) ReleaseHold(ctx context.Context, p chatcore.Principal, holdID, reason string) (ChatHoldView, error) {
	if s == nil {
		return ChatHoldView{}, chatcore.ErrUnavailable
	}
	ctx, err := s.retentionContext(ctx, p)
	if err != nil {
		return ChatHoldView{}, err
	}
	hold, err := s.Records.ReleaseConversationHold(ctx, p.SubjectID, p.TenantID, holdID, reason)
	if err != nil {
		return ChatHoldView{}, err
	}
	return holdView(hold, p, nil), nil
}

// ConversationHold reports whether the conversation is under an active hold, for
// the badge in its details. It answers only a workspace administrator who can
// open the conversation, so a member never learns that a hold exists.
func (s *ChatExtensions) ConversationHold(ctx context.Context, p chatcore.Principal, conversation string) (ChatHoldState, error) {
	if s == nil {
		return ChatHoldState{}, chatcore.ErrUnavailable
	}
	ctx, err := s.retentionContext(ctx, p)
	if err != nil {
		return ChatHoldState{}, err
	}
	if _, err = s.openHoldConversation(ctx, p, conversation); err != nil {
		return ChatHoldState{}, err
	}
	held, err := s.Records.ActiveConversationHolds(ctx, p.SubjectID, p.TenantID, conversation)
	if err != nil {
		return ChatHoldState{}, err
	}
	return ChatHoldState{Held: len(held) > 0, Count: len(held)}, nil
}

// ChatHoldsHTTP answers the hold calls. Every call is a POST so nothing sits in
// a page address.
type ChatHoldsHTTP struct{ Service ChatHoldsService }

type chatHoldRequest struct {
	ConversationID string `json:"conversation_id"`
	ID             string `json:"id"`
	Matter         string `json:"matter"`
	Reason         string `json:"reason"`
}

func chatHoldError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, chatcore.ErrPermissionDenied), errors.Is(err, chatrecords.ErrUnauthorized):
		status, code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, chatcore.ErrNotFound), errors.Is(err, chatrecords.ErrHoldNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, chatcore.ErrInvalidArgument), errors.Is(err, chatrecords.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, chatrecords.ErrConflict), errors.Is(err, chatcore.ErrConflict):
		status, code = http.StatusConflict, "already_done"
	}
	chatAuditJSON(w, status, chatAuditCode{code})
}

func (h ChatHoldsHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := trust.FromContext(r.Context())
	_, admitted := transport.InvocationFromContext(r.Context())
	if !ok || p == nil || !admitted {
		chatHoldError(w, chatcore.ErrPermissionDenied)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		chatAuditJSON(w, http.StatusMethodNotAllowed, chatAuditCode{"method_not_allowed"})
		return
	}
	action := strings.TrimPrefix(r.URL.Path, ChatHoldsPath)
	switch action {
	case "list", "place", "release", "conversation":
	default:
		chatAuditJSON(w, http.StatusNotFound, chatAuditCode{"not_found"})
		return
	}
	if h.Service == nil {
		chatHoldError(w, chatcore.ErrUnavailable)
		return
	}
	var q chatHoldRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF {
		chatHoldError(w, chatcore.ErrInvalidArgument)
		return
	}
	principal := chatcore.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}
	var (
		out any
		err error
	)
	switch action {
	case "list":
		out, err = h.Service.ListHolds(r.Context(), principal)
	case "place":
		out, err = h.Service.PlaceHold(r.Context(), principal, q.ConversationID, q.Matter, q.Reason)
	case "release":
		out, err = h.Service.ReleaseHold(r.Context(), principal, q.ID, q.Reason)
	case "conversation":
		out, err = h.Service.ConversationHold(r.Context(), principal, q.ConversationID)
	}
	if err != nil {
		chatHoldError(w, err)
		return
	}
	chatAuditJSON(w, http.StatusOK, out)
}

// OverlayChatHolds serves the hold calls beside the other chat overlays.
func OverlayChatHolds(next http.Handler, service ChatHoldsService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatHoldsPath) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatAuditJSON(w, denied.HTTPStatus(), chatAuditCode{"permission_denied"})
			return
		}
		ChatHoldsHTTP{Service: service}.ServeHTTP(w, r.WithContext(ctx))
	})
}
