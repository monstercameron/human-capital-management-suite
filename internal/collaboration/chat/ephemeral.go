package chat

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

var (
	ErrEphemeralUnavailable = errors.New("chat: ephemeral posts unavailable")
	ErrEphemeralExpired     = errors.New("chat: ephemeral post expired")
)

const EphemeralLifetime = 24 * time.Hour

// EphemeralPost is a recipient-scoped delivery record. It is intentionally
// not a Post: conversation history, search, unread/mention projections,
// exports and shared outbox consumers operate only on Post rows.
type EphemeralPost struct {
	ID, TenantID, ConversationID, ThreadID string
	RecipientHomeTenantID                  string
	RecipientSubjectID                     string
	Body                                   string
	OnlyVisibleToYou                       bool
	CreatedAt, ExpiresAt                   time.Time
	DurableCopyConversationID              string
	DurableCopyPostID                      string
	ThreadLink                             string
	Sequence                               uint64
}

type SendEphemeralPostRequest struct {
	Principal                                Principal
	TenantID, ConversationID, ThreadID, Body string
	QuestionPostID                           string
	DurableCopyConversationID                string
	// AuthorAsAgent asks the service to author the durable copy as the other
	// active member of the canonical persona direct conversation. The caller
	// never supplies or chooses that identity.
	AuthorAsAgent  bool
	IdempotencyKey string
	ExpiresAt      time.Time
}

type ListEphemeralPostsRequest struct {
	Principal                Principal
	TenantID, ConversationID string
	AfterSequence            uint64
	PageSize                 int
}

// EphemeralStore is deliberately an optional extension to Store. Existing
// chat stores and test doubles therefore cannot accidentally make ephemeral
// content part of their ordinary Post implementation.
type EphemeralStore interface {
	PutEphemeral(context.Context, EphemeralPost) (EphemeralPost, error)
	ListEphemeral(context.Context, Principal, string, string, uint64, int) ([]EphemeralPost, uint64, error)
}

type EphemeralService interface {
	SendEphemeralPost(context.Context, SendEphemeralPostRequest) (EphemeralPost, error)
}

// PersonaDMResolver is the server-side identity seam for the invoker's
// persona DM. The request may carry a routing hint, but this resolver remains
// authoritative; callers cannot redirect private content to an arbitrary DM.
type PersonaDMResolver interface {
	ResolvePersonaDM(context.Context, Principal, string) (string, error)
}

// MemoryEphemeralStore is a bounded test/local-development store. Production
// composition should provide a durable implementation; the interface keeps
// the privacy boundary explicit and makes the stream behavior testable without
// a second database.
type MemoryEphemeralStore struct {
	mu    sync.Mutex
	clock func() time.Time
	posts map[string][]EphemeralPost
}

func NewMemoryEphemeralStore(clock func() time.Time) *MemoryEphemeralStore {
	if clock == nil {
		clock = time.Now
	}
	return &MemoryEphemeralStore{clock: clock, posts: make(map[string][]EphemeralPost)}
}

func (m *MemoryEphemeralStore) PutEphemeral(_ context.Context, p EphemeralPost) (EphemeralPost, error) {
	if m == nil || p.TenantID == "" || p.ConversationID == "" || p.RecipientSubjectID == "" || !p.OnlyVisibleToYou || p.ExpiresAt.IsZero() {
		return EphemeralPost{}, ErrInvalidArgument
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.posts[ephemeralKey(p.TenantID, p.ConversationID)] {
		if existing.ID == p.ID {
			return cloneEphemeral(existing), nil
		}
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Sequence == 0 {
		p.Sequence = uint64(len(m.posts[ephemeralKey(p.TenantID, p.ConversationID)]) + 1)
	}
	m.posts[ephemeralKey(p.TenantID, p.ConversationID)] = append(m.posts[ephemeralKey(p.TenantID, p.ConversationID)], cloneEphemeral(p))
	return cloneEphemeral(p), nil
}

func (m *MemoryEphemeralStore) ListEphemeral(_ context.Context, p Principal, tenant, conversation string, after uint64, limit int) ([]EphemeralPost, uint64, error) {
	if m == nil || p.SubjectID == "" || p.TenantID == "" || tenant == "" || conversation == "" {
		return nil, after, ErrInvalidArgument
	}
	if limit <= 0 {
		limit = 100
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []EphemeralPost
	next := after
	for _, item := range m.posts[ephemeralKey(tenant, conversation)] {
		if item.Sequence <= after || item.RecipientSubjectID != p.SubjectID || item.RecipientHomeTenantID != p.TenantID || !m.clock().Before(item.ExpiresAt) {
			if item.Sequence > next {
				next = item.Sequence
			}
			continue
		}
		out = append(out, cloneEphemeral(item))
		if item.Sequence > next {
			next = item.Sequence
		}
		if len(out) == limit {
			break
		}
	}
	return out, next, nil
}

func ephemeralKey(tenant, conversation string) string { return tenant + "\x00" + conversation }
func cloneEphemeral(p EphemeralPost) EphemeralPost    { return p }

func (s *Service) SetEphemeralStore(store EphemeralStore) { s.ephemeral = store }

func (s *Service) SetPersonaDMResolver(resolver PersonaDMResolver) { s.personaDM = resolver }

func (s *Service) SendEphemeralPost(ctx context.Context, r SendEphemeralPostRequest) (EphemeralPost, error) {
	if s == nil || s.store == nil || s.ephemeral == nil || s.personaDM == nil {
		return EphemeralPost{}, ErrEphemeralUnavailable
	}
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil {
		return EphemeralPost{}, err
	}
	if strings.TrimSpace(r.ConversationID) == "" || strings.TrimSpace(r.ThreadID) == "" || strings.TrimSpace(r.Body) == "" || strings.TrimSpace(r.IdempotencyKey) == "" {
		return EphemeralPost{}, ErrInvalidArgument
	}
	if len(r.Body) > 4000 {
		return EphemeralPost{}, ErrInvalidArgument
	}
	now := s.now()
	expires := r.ExpiresAt
	if expires.IsZero() {
		expires = now.Add(EphemeralLifetime)
	}
	if !expires.After(now) || expires.After(now.Add(EphemeralLifetime)) {
		return EphemeralPost{}, ErrInvalidArgument
	}
	conversation, err := s.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return EphemeralPost{}, err
	}
	if err = s.authorize(ctx, r.Principal, conversation, chatpolicy.ActionPost); err != nil {
		return EphemeralPost{}, err
	}
	durableConversationID, err := s.personaDM.ResolvePersonaDM(ctx, r.Principal, r.TenantID)
	if err != nil || strings.TrimSpace(durableConversationID) == "" {
		return EphemeralPost{}, ErrPermissionDenied
	}
	if r.DurableCopyConversationID != "" && r.DurableCopyConversationID != durableConversationID {
		return EphemeralPost{}, ErrPermissionDenied
	}
	var agentAuthor Principal
	if r.AuthorAsAgent {
		agentAuthor, err = s.resolvePersonaDirectAuthor(ctx, r, durableConversationID)
		if err != nil {
			return EphemeralPost{}, err
		}
	}
	if r.ThreadID != r.ConversationID {
		parent, parentErr := s.store.GetPost(ctx, r.TenantID, r.ConversationID, r.ThreadID)
		if parentErr != nil || parent.ConversationID != r.ConversationID || parent.Deleted {
			return EphemeralPost{}, ErrInvalidArgument
		}
	}
	question := Post{}
	questionPostID := strings.TrimSpace(r.QuestionPostID)
	if questionPostID == "" && r.ThreadID != r.ConversationID {
		questionPostID = r.ThreadID
	}
	if questionPostID != "" {
		question, err = s.store.GetPost(ctx, r.TenantID, r.ConversationID, questionPostID)
		if err != nil || question.ID != questionPostID || question.TenantID != r.TenantID || question.ConversationID != r.ConversationID || question.AuthorID != r.Principal.SubjectID || question.AuthorHomeTenantID != r.Principal.TenantID || question.Deleted || strings.TrimSpace(question.Body) == "" || question.CreatedAt.IsZero() {
			return EphemeralPost{}, ErrPermissionDenied
		}
	}
	threadLink := ""
	if durableConversationID != r.ConversationID || !r.AuthorAsAgent {
		linkPostID := questionPostID
		link, linkErr := s.CreateConversationLink(ctx, r.Principal, r.TenantID, r.ConversationID, linkPostID)
		if linkErr != nil {
			return EphemeralPost{}, linkErr
		}
		threadLink = link.URL
	}
	// Keep the signed, current-authority Chat locator beside the durable copy.
	// The private answer remains only in the invoker's persona DM; the link has
	// identifiers and a locator token, never answer data.
	sourceLabel := strings.TrimSpace(conversation.Name)
	if conversation.Kind == PublicChannel || conversation.Kind == PrivateChannel {
		sourceLabel = "#" + strings.TrimPrefix(sourceLabel, "#")
	}
	projectedThreadLink := ephemeralQuestionThreadLink(threadLink, sourceLabel, question)
	// The line saying why an answer is private belongs to the card, not to the
	// copy saved in the asker's own conversation with the agent.
	durableText, _ := SplitPrivateReason(r.Body)
	durableBody := strings.TrimSpace(durableText)
	if durableConversationID != r.ConversationID {
		durableBody = durableEphemeralBodyFromQuestion(durableText, projectedThreadLink, sourceLabel, question)
	} else if !r.AuthorAsAgent {
		durableBody = durableEphemeralBody(durableText, threadLink)
	}
	if err = s.checkContent(ctx, ContentInput{Principal: r.Principal, Conversation: conversation, Body: durableBody}); err != nil {
		return EphemeralPost{}, err
	}
	var durable Post
	if !r.AuthorAsAgent {
		durable, err = s.SendPost(ctx, SendPostRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: durableConversationID, Body: durableBody, IdempotencyKey: r.IdempotencyKey + ":dm"})
	} else {
		durableRequest := SendPostRequest{Principal: agentAuthor, TenantID: r.TenantID, ConversationID: durableConversationID, Body: durableBody, IdempotencyKey: r.IdempotencyKey + ":dm"}
		durable = Post{ID: uuid.NewString(), TenantID: r.TenantID, ConversationID: durableConversationID, AuthorID: agentAuthor.SubjectID, AuthorHomeTenantID: agentAuthor.TenantID, Body: durableBody, Revision: 1, CreatedAt: now}
		durable, err = s.store.SendPost(ctx, durableRequest, durable)
	}
	if err != nil {
		return EphemeralPost{}, err
	}
	// In the agent's own direct conversation the durable answer is the only
	// answer. Returning its ID keeps the delivery receipt idempotent without
	// creating a second recipient-only envelope in the same timeline.
	if r.AuthorAsAgent && durableConversationID == r.ConversationID {
		return EphemeralPost{ID: durable.ID, TenantID: r.TenantID, ConversationID: r.ConversationID, ThreadID: r.ThreadID, RecipientHomeTenantID: r.Principal.TenantID, RecipientSubjectID: r.Principal.SubjectID, Body: strings.TrimSpace(r.Body), DurableCopyConversationID: durable.ConversationID, DurableCopyPostID: durable.ID, CreatedAt: durable.CreatedAt}, nil
	}
	p := EphemeralPost{ID: ephemeralID(r.TenantID, r.ConversationID, r.Principal, r.IdempotencyKey), TenantID: r.TenantID, ConversationID: r.ConversationID, ThreadID: r.ThreadID, RecipientHomeTenantID: r.Principal.TenantID, RecipientSubjectID: r.Principal.SubjectID, Body: strings.TrimSpace(r.Body), OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: expires, DurableCopyConversationID: durable.ConversationID, DurableCopyPostID: durable.ID, ThreadLink: projectedThreadLink}
	return s.ephemeral.PutEphemeral(ctx, p)
}

func (s *Service) resolvePersonaDirectAuthor(ctx context.Context, r SendEphemeralPostRequest, conversationID string) (Principal, error) {
	conversation, err := s.store.GetConversation(ctx, r.TenantID, conversationID)
	if err != nil || conversation.Kind != Direct || conversation.Archived {
		return Principal{}, ErrPermissionDenied
	}
	members, err := s.store.ListMemberships(ctx, r.TenantID, conversationID, Page{PageSize: 3})
	if err != nil || members.NextCursor != "" || len(members.Memberships) != 2 {
		return Principal{}, ErrPermissionDenied
	}
	var foundInvoker bool
	var author Principal
	for _, member := range members.Memberships {
		if member.HomeTenantID == r.Principal.TenantID && member.SubjectID == r.Principal.SubjectID {
			foundInvoker = true
			continue
		}
		if strings.TrimSpace(member.HomeTenantID) == "" || strings.TrimSpace(member.SubjectID) == "" || author.SubjectID != "" {
			return Principal{}, ErrPermissionDenied
		}
		author = Principal{TenantID: member.HomeTenantID, SubjectID: member.SubjectID}
	}
	if !foundInvoker || author.SubjectID == "" || (author.TenantID == r.Principal.TenantID && author.SubjectID == r.Principal.SubjectID) {
		return Principal{}, ErrPermissionDenied
	}
	return author, nil
}

func durableEphemeralBody(body, threadLink string) string {
	return strings.TrimSpace(body) + "\n\n[Open the original message](" + strings.TrimSpace(threadLink) + ")"
}

func durableEphemeralBodyFrom(body, threadLink, sourceLabel string) string {
	if strings.TrimSpace(sourceLabel) == "" {
		return durableEphemeralBody(body, threadLink)
	}
	return strings.TrimSpace(body) + "\n\n[chat-agent-question:" + strings.ReplaceAll(strings.TrimSpace(sourceLabel), "]", "") + "](" + strings.TrimSpace(threadLink) + ")"
}

func durableEphemeralBodyFromQuestion(body, threadLink, sourceLabel string, question Post) string {
	encoded := ephemeralQuestionContext(sourceLabel, question)
	if encoded == "" {
		return durableEphemeralBodyFrom(body, threadLink, sourceLabel)
	}
	return strings.TrimSpace(body) + "\n\n[chat-agent-question-context:" + encoded + "](" + strings.TrimSpace(threadLink) + ")"
}

func ephemeralQuestionThreadLink(threadLink, sourceLabel string, question Post) string {
	encoded := ephemeralQuestionContext(sourceLabel, question)
	if encoded == "" {
		return strings.TrimSpace(threadLink)
	}
	return strings.TrimSpace(threadLink) + "#hcm-question=" + encoded
}

func ephemeralQuestionContext(sourceLabel string, question Post) string {
	if question.ID == "" || strings.TrimSpace(question.Body) == "" || question.CreatedAt.IsZero() {
		return ""
	}
	context, err := json.Marshal(struct {
		Label string    `json:"label"`
		Text  string    `json:"text"`
		At    time.Time `json:"at"`
	}{Label: strings.TrimSpace(sourceLabel), Text: strings.TrimSpace(question.Body), At: question.CreatedAt.UTC()})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(context)
}

func (s *Service) ListEphemeralPosts(ctx context.Context, r ListEphemeralPostsRequest) ([]EphemeralPost, uint64, error) {
	if s == nil || s.ephemeral == nil {
		return nil, 0, ErrEphemeralUnavailable
	}
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || strings.TrimSpace(r.ConversationID) == "" {
		return nil, 0, errOr(err, ErrInvalidArgument)
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID}); err != nil {
		return nil, 0, err
	}
	if r.PageSize < 0 || r.PageSize > 100 {
		return nil, 0, ErrInvalidArgument
	}
	posts, next, err := s.ephemeral.ListEphemeral(ctx, r.Principal, r.TenantID, r.ConversationID, r.AfterSequence, r.PageSize)
	for i := range posts {
		posts[i].Body = s.projectAgentSources(ctx, r.Principal, r.TenantID, r.ConversationID, posts[i].Body)
	}
	return posts, next, err
}

func ephemeralID(tenant, conversation string, p Principal, key string) string {
	h := sha256.Sum256([]byte(tenant + "\x00" + conversation + "\x00" + p.TenantID + "\x00" + p.SubjectID + "\x00" + key))
	return "ephemeral-" + hex.EncodeToString(h[:])
}

var _ EphemeralService = (*Service)(nil)
