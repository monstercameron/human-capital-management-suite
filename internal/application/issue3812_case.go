package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3812: a conversation bound to a governed case. The chat service enforces
// the rules, from the binding the store keeps; this file is how a binding is
// made, lifted and read.
//
//   - Reading a binding needs only the right to open the conversation: everyone
//     in it is told it is case-bound, and whether it is restricted.
//   - Making or lifting one is workspace administration, decided by the verified
//     caller's current administrator role, and the administrator must be able to
//     open the conversation too: an administrator who is not in a thread is not
//     told it exists, and cannot bind it.
//
// The case reference is an opaque string from the system that owns the case.

// ChatCasePath serves the get, bind and unbind calls.
const ChatCasePath = "/api/chat/case/v1/"

// ChatCaseView is what one reader may know of a conversation's case.
type ChatCaseView struct {
	Bound      bool   `json:"bound"`
	CaseRef    string `json:"case_ref,omitempty"`
	OwnerID    string `json:"owner_id,omitempty"`
	OwnerIsMe  bool   `json:"owner_is_me,omitempty"`
	Restricted bool   `json:"restricted,omitempty"`
	// CanManage is set for a workspace administrator who can open the
	// conversation: they may bind it, change it and lift it.
	CanManage bool `json:"can_manage"`
}

// ChatCaseService is what the HTTP edge calls.
type ChatCaseService interface {
	Case(ctx context.Context, p chat.Principal, conversation string) (ChatCaseView, error)
	Bind(ctx context.Context, p chat.Principal, conversation, caseRef, owner string, restricted bool) (ChatCaseView, error)
	Unbind(ctx context.Context, p chat.Principal, conversation string) (ChatCaseView, error)
}

// ChatCaseStore keeps the bindings.
type ChatCaseStore interface {
	ActiveCase(ctx context.Context, tenant, conversation string) (chat.CaseBinding, bool, error)
	BindCase(ctx context.Context, tenant, id string, b chat.CaseBinding) (chat.CaseBinding, error)
	UnbindCase(ctx context.Context, tenant, conversation, actor string) error
}

// ChatCaseConversations is the conversation service as the case surface reads it.
type ChatCaseConversations interface {
	GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error)
}

type chatCaseApplication struct {
	Store         ChatCaseStore
	Conversations ChatCaseConversations
	// Admin names the workspace administrator behind the context, or refuses.
	Admin func(ctx context.Context, tenant string) (string, error)
	NewID func() string
}

var _ ChatCaseService = chatCaseApplication{}

// open decides that the caller may open the conversation. Anything short of that
// reads as "not found", so a conversation a person cannot open is never
// confirmed to exist.
func (a chatCaseApplication) open(ctx context.Context, p chat.Principal, conversation string) error {
	if a.Store == nil || a.Conversations == nil {
		return chat.ErrUnavailable
	}
	if p.TenantID == "" || p.SubjectID == "" || strings.TrimSpace(conversation) == "" {
		return chat.ErrNotFound
	}
	_, err := a.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: conversation})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
		return chat.ErrNotFound
	default:
		return chat.ErrUnavailable
	}
}

func (a chatCaseApplication) isAdmin(ctx context.Context, p chat.Principal) bool {
	if a.Admin == nil {
		return false
	}
	actor, err := a.Admin(ctx, p.TenantID)
	return err == nil && actor == p.SubjectID
}

func (a chatCaseApplication) view(ctx context.Context, p chat.Principal, conversation string) (ChatCaseView, error) {
	b, bound, err := a.Store.ActiveCase(ctx, p.TenantID, conversation)
	if err != nil {
		return ChatCaseView{}, chat.ErrUnavailable
	}
	out := ChatCaseView{CanManage: a.isAdmin(ctx, p)}
	if bound {
		out.Bound, out.CaseRef, out.OwnerID, out.Restricted, out.OwnerIsMe = true, b.CaseRef, b.OwnerID, b.Restricted, b.OwnerID == p.SubjectID
	}
	return out, nil
}

func (a chatCaseApplication) Case(ctx context.Context, p chat.Principal, conversation string) (ChatCaseView, error) {
	if err := a.open(ctx, p, conversation); err != nil {
		return ChatCaseView{}, err
	}
	return a.view(ctx, p, conversation)
}

// authorize decides an administrator who can open the conversation.
func (a chatCaseApplication) authorize(ctx context.Context, p chat.Principal, conversation string) error {
	if err := a.open(ctx, p, conversation); err != nil {
		return err
	}
	if !a.isAdmin(ctx, p) {
		return chat.ErrPermissionDenied
	}
	return nil
}

func (a chatCaseApplication) Bind(ctx context.Context, p chat.Principal, conversation, caseRef, owner string, restricted bool) (ChatCaseView, error) {
	if err := a.authorize(ctx, p, conversation); err != nil {
		return ChatCaseView{}, err
	}
	caseRef, owner = strings.TrimSpace(caseRef), strings.TrimSpace(owner)
	if caseRef == "" || utf8.RuneCountInString(caseRef) > chatstore.MaxCaseRefRunes || owner == "" {
		return ChatCaseView{}, chat.ErrInvalidArgument
	}
	id := uuid.NewString()
	if a.NewID != nil {
		id = a.NewID()
	}
	if _, err := a.Store.BindCase(ctx, p.TenantID, id, chat.CaseBinding{ConversationID: conversation, CaseRef: caseRef, OwnerID: owner, Restricted: restricted, BoundBy: p.SubjectID}); err != nil {
		return ChatCaseView{}, err
	}
	return a.view(ctx, p, conversation)
}

func (a chatCaseApplication) Unbind(ctx context.Context, p chat.Principal, conversation string) (ChatCaseView, error) {
	if err := a.authorize(ctx, p, conversation); err != nil {
		return ChatCaseView{}, err
	}
	if err := a.Store.UnbindCase(ctx, p.TenantID, conversation, p.SubjectID); err != nil {
		return ChatCaseView{}, err
	}
	return a.view(ctx, p, conversation)
}

// newChatCaseApplication composes the surface from the chat store, the chat
// service and the workspace administrator check.
func newChatCaseApplication(store *chatstore.Store, conversations chat.ConversationService, grants *ChatCompanyGrants) ChatCaseService {
	if store == nil || conversations == nil || grants == nil {
		return nil
	}
	return chatCaseApplication{Store: store, Conversations: conversations, Admin: func(ctx context.Context, tenant string) (string, error) {
		return grants.admin(ctx, tenant, time.Now().UTC())
	}}
}

// ChatCaseHTTP answers get, bind and unbind. All three are POST so a case
// reference never sits in a page address.
type ChatCaseHTTP struct{ Service ChatCaseService }

type chatCaseRequest struct {
	ConversationID string `json:"conversation_id"`
	CaseRef        string `json:"case_ref"`
	OwnerID        string `json:"owner_id"`
	Restricted     bool   `json:"restricted"`
}

type chatCaseCode struct {
	Code string `json:"code"`
}

func chatCaseJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func chatCaseError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, chat.ErrPermissionDenied):
		status, code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, chat.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, chat.ErrInvalidArgument):
		status, code = http.StatusBadRequest, "invalid_request"
	}
	chatCaseJSON(w, status, chatCaseCode{code})
}

func (h ChatCaseHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor, ok := trust.FromContext(r.Context())
	_, admitted := transport.InvocationFromContext(r.Context())
	if !ok || actor == nil || !admitted {
		chatCaseError(w, chat.ErrPermissionDenied)
		return
	}
	// A case is a person's business; an agent never reads or changes one.
	if actor.SubjectKind() != trust.SubjectKindHuman {
		chatCaseError(w, chat.ErrPermissionDenied)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		chatCaseJSON(w, http.StatusMethodNotAllowed, chatCaseCode{"method_not_allowed"})
		return
	}
	action := strings.TrimPrefix(r.URL.Path, ChatCasePath)
	if action != "get" && action != "bind" && action != "unbind" {
		chatCaseJSON(w, http.StatusNotFound, chatCaseCode{"not_found"})
		return
	}
	if h.Service == nil {
		chatCaseError(w, chat.ErrUnavailable)
		return
	}
	var q chatCaseRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF {
		chatCaseError(w, chat.ErrInvalidArgument)
		return
	}
	p := chat.Principal{TenantID: actor.Tenant().String(), SubjectID: actor.Subject()}
	var (
		view ChatCaseView
		err  error
	)
	switch action {
	case "get":
		view, err = h.Service.Case(r.Context(), p, q.ConversationID)
	case "bind":
		view, err = h.Service.Bind(r.Context(), p, q.ConversationID, q.CaseRef, q.OwnerID, q.Restricted)
	default:
		view, err = h.Service.Unbind(r.Context(), p, q.ConversationID)
	}
	if err != nil {
		chatCaseError(w, err)
		return
	}
	chatCaseJSON(w, http.StatusOK, view)
}

// OverlayChatCase serves the case calls beside the other chat overlays.
func OverlayChatCase(next http.Handler, service ChatCaseService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatCasePath) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatCaseJSON(w, denied.HTTPStatus(), chatCaseCode{"permission_denied"})
			return
		}
		ChatCaseHTTP{Service: service}.ServeHTTP(w, r.WithContext(ctx))
	})
}
