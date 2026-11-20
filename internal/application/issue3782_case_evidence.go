package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3782: reviewed conversation evidence attached to a governed case.
//
//   - Attaching needs the right to open the conversation and to be the owner of
//     the case it is bound to, or a workspace administrator who can open it.
//     The page sends the ids of the messages the person reviewed and the case
//     they reviewed them against, never text: the server reads every message
//     itself, with the caller's own authority, and freezes what it read.
//   - Reading needs only the right to open the conversation, and every read
//     reads the originals again with the reader's own authority: the frozen text
//     is shown only to a reader who can still open the original, and a source
//     that was edited or is gone is marked. A reader who cannot open the
//     original is not shown its text, author or fingerprint.
//   - Nothing here is a decision. The view has no approval, policy version or
//     outcome: those come from the service that owns the case, and a reaction
//     or acknowledgement in a conversation proves none of them.

// ChatCaseEvidencePath serves the list and attach calls.
const ChatCaseEvidencePath = "/api/chat/case-evidence/v1/"

// ChatCaseEvidenceAttach is one reviewed attachment.
type ChatCaseEvidenceAttach struct {
	ConversationID string
	// CaseRef is the case the person reviewed the messages against; the call is
	// refused if the conversation is bound to another one by now.
	CaseRef string
	// CaseRevision is the revision of the case's proposal, as the case system
	// names it. Optional, and opaque to Chat.
	CaseRevision string
	Note         string
	MessageIDs   []string
}

// ChatCaseEvidenceItemView is one frozen message as one reader may see it.
type ChatCaseEvidenceItemView struct {
	ID string `json:"id"`
	// Status is current, edited or gone (see chat.EvidenceSource*).
	Status      string `json:"status"`
	MessageID   string `json:"message_id,omitempty"`
	AuthorID    string `json:"author_id,omitempty"`
	AuthoredAt  string `json:"authored_at,omitempty"`
	Body        string `json:"body,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// ChatCaseEvidenceExcerptView is the messages attached together.
type ChatCaseEvidenceExcerptView struct {
	ID           string                     `json:"id"`
	CaseRevision string                     `json:"case_revision,omitempty"`
	Note         string                     `json:"note,omitempty"`
	AttachedBy   string                     `json:"attached_by"`
	AttachedAt   string                     `json:"attached_at"`
	Items        []ChatCaseEvidenceItemView `json:"items"`
}

// ChatCaseEvidenceView is the evidence of a conversation's case as one reader
// may see it.
type ChatCaseEvidenceView struct {
	Bound   bool   `json:"bound"`
	CaseRef string `json:"case_ref,omitempty"`
	// CanAttach is set for the case owner and for a workspace administrator who
	// can open the conversation.
	CanAttach bool                          `json:"can_attach"`
	Excerpts  []ChatCaseEvidenceExcerptView `json:"excerpts"`
}

// ChatCaseEvidenceService is what the HTTP edge calls.
type ChatCaseEvidenceService interface {
	Evidence(ctx context.Context, p chat.Principal, conversation string) (ChatCaseEvidenceView, error)
	Attach(ctx context.Context, p chat.Principal, in ChatCaseEvidenceAttach) (ChatCaseEvidenceView, error)
}

// ChatCaseEvidenceStore keeps the bindings and the frozen evidence.
type ChatCaseEvidenceStore interface {
	ActiveCase(ctx context.Context, tenant, conversation string) (chat.CaseBinding, bool, error)
	chat.CaseEvidencePort
}

type chatCaseEvidenceApplication struct {
	Store ChatCaseEvidenceStore
	// Reader reads a conversation, or one message of it, with the caller's own
	// authority.
	Reader chat.AuthorizedReferenceReader
	// Admin names the workspace administrator behind the context, or refuses.
	Admin func(ctx context.Context, tenant string) (string, error)
	NewID func() string
}

var _ ChatCaseEvidenceService = chatCaseEvidenceApplication{}

func (a chatCaseEvidenceApplication) id() string {
	if a.NewID != nil {
		return a.NewID()
	}
	return uuid.NewString()
}

// read reads the conversation (message empty) or one message of it as the
// caller. A refusal and an absence read the same.
func (a chatCaseEvidenceApplication) read(ctx context.Context, p chat.Principal, conversation, message string) (*chat.Post, error) {
	if a.Store == nil || a.Reader == nil {
		return nil, chat.ErrUnavailable
	}
	if p.TenantID == "" || p.SubjectID == "" || strings.TrimSpace(conversation) == "" {
		return nil, chat.ErrNotFound
	}
	_, post, err := a.Reader.ReadAuthorizedReference(ctx, p, p.TenantID, conversation, message)
	switch {
	case err == nil:
		if message != "" && (post == nil || post.ID != message || post.ConversationID != conversation) {
			return nil, chat.ErrNotFound
		}
		return post, nil
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
		return nil, chat.ErrNotFound
	default:
		return nil, chat.ErrUnavailable
	}
}

func (a chatCaseEvidenceApplication) isAdmin(ctx context.Context, p chat.Principal) bool {
	if a.Admin == nil {
		return false
	}
	actor, err := a.Admin(ctx, p.TenantID)
	return err == nil && actor == p.SubjectID
}

// canAttach is the authority to file evidence: the owner of the bound case, or
// a workspace administrator (who has already shown they can open it).
func (a chatCaseEvidenceApplication) canAttach(ctx context.Context, p chat.Principal, b chat.CaseBinding, bound bool) bool {
	return (bound && b.OwnerID == p.SubjectID) || a.isAdmin(ctx, p)
}

func (a chatCaseEvidenceApplication) view(ctx context.Context, p chat.Principal, conversation string) (ChatCaseEvidenceView, error) {
	b, bound, err := a.Store.ActiveCase(ctx, p.TenantID, conversation)
	if err != nil {
		return ChatCaseEvidenceView{}, chat.ErrUnavailable
	}
	out := ChatCaseEvidenceView{Excerpts: []ChatCaseEvidenceExcerptView{}}
	if !bound {
		return out, nil
	}
	out.Bound, out.CaseRef, out.CanAttach = true, b.CaseRef, a.canAttach(ctx, p, b, bound)
	excerpts, err := a.Store.CaseEvidence(ctx, p.TenantID, conversation, b.CaseRef)
	if err != nil {
		return ChatCaseEvidenceView{}, chat.ErrUnavailable
	}
	// Each original is read once, as this reader.
	sources := map[string]*chat.Post{}
	for _, e := range excerpts {
		ev := ChatCaseEvidenceExcerptView{ID: e.ID, CaseRevision: e.CaseRevision, Note: e.Note, AttachedBy: e.AttachedBy, AttachedAt: e.AttachedAt.UTC().Format(time.RFC3339), Items: []ChatCaseEvidenceItemView{}}
		for _, item := range e.Items {
			current, seen := sources[item.MessageID]
			if !seen {
				post, err := a.read(ctx, p, conversation, item.MessageID)
				switch {
				case err == nil:
					current = post
				case errors.Is(err, chat.ErrNotFound):
					current = nil
				default:
					return ChatCaseEvidenceView{}, err
				}
				sources[item.MessageID] = current
			}
			ev.Items = append(ev.Items, evidenceItemView(item, current))
		}
		out.Excerpts = append(out.Excerpts, ev)
	}
	return out, nil
}

// evidenceItemView shows what the reader may see of one frozen message. When
// the original is open to them and unchanged, they see it as they read it now;
// when it was edited they see the text that was frozen, marked; when it is gone
// or not open to them they see that it is gone and nothing else.
func evidenceItemView(item chat.CaseEvidenceItem, current *chat.Post) ChatCaseEvidenceItemView {
	status := chat.EvidenceSourceState(item, current)
	v := ChatCaseEvidenceItemView{ID: item.ID, Status: status}
	if status == chat.EvidenceSourceGone {
		return v
	}
	v.MessageID, v.AuthorID, v.AuthoredAt, v.Body = item.MessageID, item.AuthorID, item.AuthoredAt.UTC().Format(time.RFC3339), item.Body
	if status == chat.EvidenceSourceCurrent && current != nil {
		v.Body = current.Body
	}
	if len(item.ContentHash) >= 12 {
		v.Fingerprint = item.ContentHash[:12]
	}
	return v
}

func (a chatCaseEvidenceApplication) Evidence(ctx context.Context, p chat.Principal, conversation string) (ChatCaseEvidenceView, error) {
	if _, err := a.read(ctx, p, conversation, ""); err != nil {
		return ChatCaseEvidenceView{}, err
	}
	return a.view(ctx, p, conversation)
}

func (a chatCaseEvidenceApplication) Attach(ctx context.Context, p chat.Principal, in ChatCaseEvidenceAttach) (ChatCaseEvidenceView, error) {
	conversation := in.ConversationID
	if _, err := a.read(ctx, p, conversation, ""); err != nil {
		return ChatCaseEvidenceView{}, err
	}
	b, bound, err := a.Store.ActiveCase(ctx, p.TenantID, conversation)
	if err != nil {
		return ChatCaseEvidenceView{}, chat.ErrUnavailable
	}
	if !a.canAttach(ctx, p, b, bound) {
		return ChatCaseEvidenceView{}, chat.ErrPermissionDenied
	}
	caseRef, revision, note := strings.TrimSpace(in.CaseRef), strings.TrimSpace(in.CaseRevision), strings.TrimSpace(in.Note)
	if caseRef == "" || utf8.RuneCountInString(caseRef) > chatstore.MaxCaseRefRunes || utf8.RuneCountInString(revision) > chat.MaxEvidenceRevisionRunes ||
		utf8.RuneCountInString(note) > chat.MaxEvidenceNoteRunes || len(in.MessageIDs) == 0 || len(in.MessageIDs) > chat.MaxEvidenceMessages {
		return ChatCaseEvidenceView{}, chat.ErrInvalidArgument
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(in.MessageIDs))
	for _, raw := range in.MessageIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			return ChatCaseEvidenceView{}, chat.ErrInvalidArgument
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if !bound || b.CaseRef != caseRef {
		return ChatCaseEvidenceView{}, chat.ErrCaseEvidenceCaseChanged
	}
	// Every message is read as the caller, so nothing they cannot open is frozen,
	// and a message they cannot open reads as absent.
	posts := make([]*chat.Post, 0, len(ids))
	for _, id := range ids {
		post, err := a.read(ctx, p, conversation, id)
		if err != nil {
			return ChatCaseEvidenceView{}, err
		}
		if post.Deleted {
			return ChatCaseEvidenceView{}, chat.ErrNotFound
		}
		posts = append(posts, post)
	}
	sort.SliceStable(posts, func(i, j int) bool { return posts[i].Sequence < posts[j].Sequence })
	excerpt := chat.CaseEvidenceExcerpt{ID: a.id(), ConversationID: conversation, CaseRef: caseRef, CaseRevision: revision, Note: note, AttachedBy: p.SubjectID}
	for _, post := range posts {
		excerpt.Items = append(excerpt.Items, chat.CaseEvidenceItem{ID: a.id(), MessageID: post.ID, SourceRevision: post.Revision, AuthorID: post.AuthorID, AuthoredAt: post.CreatedAt.UTC(), Body: post.Body, ContentHash: chat.EvidenceContentHash(post.Body)})
	}
	if _, err := a.Store.AttachCaseEvidence(ctx, p.TenantID, excerpt); err != nil {
		return ChatCaseEvidenceView{}, err
	}
	return a.view(ctx, p, conversation)
}

// newChatCaseEvidenceApplication composes the surface from the chat store, the
// chat service and the workspace administrator check.
func newChatCaseEvidenceApplication(store *chatstore.Store, reader chat.AuthorizedReferenceReader, grants *ChatCompanyGrants) ChatCaseEvidenceService {
	if store == nil || reader == nil || grants == nil {
		return nil
	}
	return chatCaseEvidenceApplication{Store: store, Reader: reader, Admin: func(ctx context.Context, tenant string) (string, error) {
		return grants.admin(ctx, tenant, time.Now().UTC())
	}}
}

// ChatCaseEvidenceHTTP answers list and attach. Both are POST so a case
// reference never sits in a page address.
type ChatCaseEvidenceHTTP struct{ Service ChatCaseEvidenceService }

type chatCaseEvidenceRequest struct {
	ConversationID string   `json:"conversation_id"`
	CaseRef        string   `json:"case_ref"`
	CaseRevision   string   `json:"case_revision"`
	Note           string   `json:"note"`
	MessageIDs     []string `json:"message_ids"`
}

func chatCaseEvidenceError(w http.ResponseWriter, err error) {
	if errors.Is(err, chat.ErrCaseEvidenceCaseChanged) {
		chatCaseJSON(w, http.StatusConflict, chatCaseCode{"case_changed"})
		return
	}
	chatCaseError(w, err)
}

func (h ChatCaseEvidenceHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	action := strings.TrimPrefix(r.URL.Path, ChatCaseEvidencePath)
	if action != "list" && action != "attach" {
		chatCaseJSON(w, http.StatusNotFound, chatCaseCode{"not_found"})
		return
	}
	if h.Service == nil {
		chatCaseError(w, chat.ErrUnavailable)
		return
	}
	var q chatCaseEvidenceRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF {
		chatCaseError(w, chat.ErrInvalidArgument)
		return
	}
	p := chat.Principal{TenantID: actor.Tenant().String(), SubjectID: actor.Subject()}
	var (
		view ChatCaseEvidenceView
		err  error
	)
	if action == "list" {
		view, err = h.Service.Evidence(r.Context(), p, q.ConversationID)
	} else {
		view, err = h.Service.Attach(r.Context(), p, ChatCaseEvidenceAttach{ConversationID: q.ConversationID, CaseRef: q.CaseRef, CaseRevision: q.CaseRevision, Note: q.Note, MessageIDs: q.MessageIDs})
	}
	if err != nil {
		chatCaseEvidenceError(w, err)
		return
	}
	chatCaseJSON(w, http.StatusOK, view)
}

// OverlayChatCaseEvidence serves the evidence calls beside the other chat overlays.
func OverlayChatCaseEvidence(next http.Handler, service ChatCaseEvidenceService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatCaseEvidencePath) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatCaseJSON(w, denied.HTTPStatus(), chatCaseCode{"permission_denied"})
			return
		}
		ChatCaseEvidenceHTTP{Service: service}.ServeHTTP(w, r.WithContext(ctx))
	})
}

// composeChatCaseEvidence builds the surface over the served conversation
// service, which reads a message as the person asking. A service that cannot
// read as a person leaves the surface off rather than reading as anyone else.
func composeChatCaseEvidence(store *chatstore.Store, conversations chat.ConversationService, grants *ChatCompanyGrants) ChatCaseEvidenceService {
	reader, ok := conversations.(chat.AuthorizedReferenceReader)
	if !ok {
		return nil
	}
	return newChatCaseEvidenceApplication(store, reader, grants)
}
