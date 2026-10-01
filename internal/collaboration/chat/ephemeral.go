package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	DurableCopyConversationID                string
	IdempotencyKey                           string
	ExpiresAt                                time.Time
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
	if r.ThreadID != r.ConversationID {
		parent, parentErr := s.store.GetPost(ctx, r.TenantID, r.ConversationID, r.ThreadID)
		if parentErr != nil || parent.ConversationID != r.ConversationID || parent.Deleted {
			return EphemeralPost{}, ErrInvalidArgument
		}
	}
	linkPostID := ""
	if r.ThreadID != r.ConversationID {
		linkPostID = r.ThreadID
	}
	link, err := s.CreateConversationLink(ctx, r.Principal, r.TenantID, r.ConversationID, linkPostID)
	if err != nil {
		return EphemeralPost{}, err
	}
	threadLink := link.URL
	// Keep the signed, current-authority Chat locator beside the durable copy.
	// The private answer remains only in the invoker's persona DM; the link has
	// identifiers and a locator token, never answer data.
	durableBody := strings.TrimSpace(r.Body) + "\n\nOpen the source conversation: " + threadLink
	durable, err := s.SendPost(ctx, SendPostRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: durableConversationID, Body: durableBody, IdempotencyKey: r.IdempotencyKey + ":dm"})
	if err != nil {
		return EphemeralPost{}, err
	}
	p := EphemeralPost{ID: ephemeralID(r.TenantID, r.ConversationID, r.Principal, r.IdempotencyKey), TenantID: r.TenantID, ConversationID: r.ConversationID, ThreadID: r.ThreadID, RecipientHomeTenantID: r.Principal.TenantID, RecipientSubjectID: r.Principal.SubjectID, Body: strings.TrimSpace(r.Body), OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: expires, DurableCopyConversationID: durable.ConversationID, DurableCopyPostID: durable.ID, ThreadLink: threadLink}
	return s.ephemeral.PutEphemeral(ctx, p)
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
	return s.ephemeral.ListEphemeral(ctx, r.Principal, r.TenantID, r.ConversationID, r.AfterSequence, r.PageSize)
}

func ephemeralID(tenant, conversation string, p Principal, key string) string {
	h := sha256.Sum256([]byte(tenant + "\x00" + conversation + "\x00" + p.TenantID + "\x00" + p.SubjectID + "\x00" + key))
	return "ephemeral-" + hex.EncodeToString(h[:])
}

var _ EphemeralService = (*Service)(nil)
