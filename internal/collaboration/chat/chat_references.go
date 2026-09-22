package chat

// This file owns the semantic reference layer for chat. References are
// resolved against current membership and authority at suggestion time and
// again at commit time; display text is never an authorization input.

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type ReferenceKind string

const (
	PersonMention       ReferenceKind = "PERSON_MENTION"
	AgentMention        ReferenceKind = "AGENT_MENTION"
	ConversationMention ReferenceKind = "CONVERSATION_REFERENCE"
	// MediaAttachment references a chatmedia artifact. The post carries the
	// artifact identifier and a rendering snapshot; the bytes stay in the media
	// service and a reader fetches them through its grant and download flow, so
	// scanning, quarantine and per-read authorization are never bypassed by
	// putting content in a message.
	MediaAttachment ReferenceKind = "MEDIA"
)

// Reference is the durable, typed form of an @ or # token. Display is a
// snapshot for rendering only and must never be used to resolve authority.
type Reference struct {
	Kind           ReferenceKind
	TenantID       string
	ID             string
	Display        string
	ConversationID string
	// ContentType, ByteSize, Width and Height describe a MediaAttachment and are
	// zero on every other kind. Like Display they are a snapshot for rendering:
	// the media service stays the authority, and a client that trusted these to
	// decide access would be trusting the message instead of the artifact.
	ContentType   string
	ByteSize      uint64
	Width, Height uint32
}

type ReferenceCandidate struct {
	Reference
	Eligible bool
}

// ReferenceDirectory is deliberately a read-only port. Implementations must
// return candidates from the caller's tenant directory; the service applies
// conversation membership and current conversation authorization as well.
type ReferenceDirectory interface {
	People(context.Context, Principal, string, string, string) ([]ReferenceCandidate, error)
	Agents(context.Context, Principal, string, string, string) ([]ReferenceCandidate, error)
	Conversations(context.Context, Principal, string, string, string) ([]ReferenceCandidate, error)
}

// AgentReferenceDirectory optionally supplies app installation membership for
// agent mentions. Agents are visible participants without human membership
// rows, so the chat service never treats an installation ID as a person.
type AgentReferenceDirectory interface {
	AgentEligible(context.Context, string, string, string) bool
}

type SuggestReferencesRequest struct {
	Principal      Principal
	TenantID       string
	ConversationID string
	Kind           ReferenceKind
	Query          string
}

type SendPostWithReferencesRequest struct {
	SendPostRequest
	References []Reference
}

type SourceAttribution struct {
	TenantID, ConversationID, PostID string
	PostRevision                     uint64
	OriginalAuthorID                 string
}

type ForwardPostRequest struct {
	Principal Principal
	SourceAttribution
	SourceTenantID, SourceConversationID, SourcePostID string
	DestinationTenantID, DestinationConversationID     string
	IdempotencyKey                                     string
}

type DisclosureInput struct {
	Principal   Principal
	Source      Conversation
	SourcePost  Post
	Destination Conversation
	At          time.Time
}

// DisclosureChecker owns classification, DLP, residency and cross-company
// policy. A nil checker permits only same-tenant forwarding.
type DisclosureChecker interface {
	Check(context.Context, DisclosureInput) error
}

// LinkCodec gives deployments a durable, signed locator format. A link is
// always a locator: ResolveConversationLink still performs current authz.
type LinkCodec interface {
	Encode(string, string, string) (string, error)
	Decode(string) (tenant, conversation, post string, err error)
}

// OpaqueLocator is a small dependency-free codec useful for tests and local
// composition. It grants no access and contains no message body.
type OpaqueLocator struct{}

func (OpaqueLocator) Encode(tenant, conversation, post string) (string, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(conversation) == "" {
		return "", ErrInvalidArgument
	}
	raw := tenant + "\x00" + conversation + "\x00" + post
	return base64.RawURLEncoding.EncodeToString([]byte(raw)), nil
}

func (OpaqueLocator) Decode(token string) (string, string, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", "", "", ErrNotFound
	}
	parts := strings.Split(string(b), "\x00")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return "", "", "", ErrNotFound
	}
	return parts[0], parts[1], parts[2], nil
}

type ConversationLink struct {
	URL, TenantID, ConversationID, PostID string
}

// ReferenceService is the optional extension carried alongside the canonical
// ConversationService. Routing and stream wrappers explicitly forward it so
// embedding the base interface cannot erase mention/share capabilities.
type ReferenceService interface {
	SuggestReferences(context.Context, SuggestReferencesRequest) ([]ReferenceCandidate, error)
	SendPostWithReferences(context.Context, SendPostWithReferencesRequest) (Post, error)
	CreateShareLink(context.Context, Principal, string, string, string) (ConversationLink, error)
	ResolveShareLink(context.Context, Principal, string) (Conversation, *Post, error)
	ForwardPost(context.Context, ForwardPostRequest) (Post, error)
}

func (s *Service) SetReferenceDirectory(d ReferenceDirectory) { s.referenceDirectory = d }
func (s *Service) SetDisclosureChecker(c DisclosureChecker)   { s.disclosureChecker = c }
func (s *Service) SetLinkCodec(c LinkCodec)                   { s.linkCodec = c }

func (s *Service) SuggestReferences(ctx context.Context, r SuggestReferencesRequest) ([]ReferenceCandidate, error) {
	if s == nil || s.store == nil || r.Principal.SubjectID == "" || r.Principal.TenantID == "" || r.TenantID == "" || r.ConversationID == "" {
		return nil, ErrInvalidArgument
	}
	c, err := s.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil || s.authorize(ctx, r.Principal, c, chatpolicy.ActionRead) != nil {
		// Autocomplete must not disclose whether a private conversation exists.
		return []ReferenceCandidate{}, nil
	}
	if s.referenceDirectory == nil {
		return nil, ErrUnavailable
	}
	q := strings.TrimSpace(r.Query)
	var xs []ReferenceCandidate
	switch r.Kind {
	case PersonMention:
		xs, err = s.referenceDirectory.People(ctx, r.Principal, r.TenantID, r.ConversationID, q)
	case AgentMention:
		xs, err = s.referenceDirectory.Agents(ctx, r.Principal, r.TenantID, r.ConversationID, q)
	case ConversationMention:
		xs, err = s.referenceDirectory.Conversations(ctx, r.Principal, r.TenantID, r.ConversationID, q)
	default:
		return nil, ErrInvalidArgument
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	out := make([]ReferenceCandidate, 0, len(xs))
	for _, x := range xs {
		if !x.Eligible || !validReference(x.Reference, r.Kind) {
			continue
		}
		if r.Kind == ConversationMention {
			target, e := s.store.GetConversation(ctx, x.TenantID, x.ConversationID)
			if e != nil || s.authorize(ctx, r.Principal, target, chatpolicy.ActionRead) != nil {
				continue
			}
		} else {
			if r.Kind == AgentMention {
				if ad, ok := s.referenceDirectory.(AgentReferenceDirectory); !ok || !ad.AgentEligible(ctx, r.TenantID, r.ConversationID, x.ID) {
					continue
				}
			} else {
				m, e := s.store.GetMembership(ctx, r.TenantID, r.ConversationID, x.TenantID, x.ID)
				if e != nil || m.LeftAt != nil || m.SubjectID != x.ID {
					continue
				}
			}
		}
		out = append(out, x)
	}
	return out, nil
}

func (s *Service) SendPostWithReferences(ctx context.Context, r SendPostWithReferencesRequest) (Post, error) {
	if len(r.References) > 128 {
		return Post{}, ErrInvalidArgument
	}
	for _, ref := range r.References {
		if err := s.validateReference(ctx, r.Principal, r.TenantID, r.ConversationID, ref); err != nil {
			return Post{}, err
		}
	}
	r.SendPostRequest.References = append([]Reference(nil), r.References...)
	return s.SendPost(ctx, r.SendPostRequest)
}

func (s *Service) validateReference(ctx context.Context, p Principal, tenant, conversation string, ref Reference) error {
	if !validReference(ref, ref.Kind) || ref.TenantID == "" || ref.ID == "" {
		return ErrInvalidArgument
	}
	if ref.Kind == ConversationMention {
		c, err := s.store.GetConversation(ctx, ref.TenantID, ref.ConversationID)
		if err != nil || s.authorize(ctx, p, c, chatpolicy.ActionRead) != nil {
			return ErrPermissionDenied
		}
		return nil
	}
	if ref.Kind == MediaAttachment {
		return s.validateMediaReference(ctx, tenant, conversation, ref)
	}
	if ref.Kind == AgentMention {
		if ad, ok := s.referenceDirectory.(AgentReferenceDirectory); ok && ad.AgentEligible(ctx, tenant, conversation, ref.ID) {
			return nil
		}
	}
	m, err := s.store.GetMembership(ctx, tenant, conversation, ref.TenantID, ref.ID)
	if err != nil || m.LeftAt != nil || m.SubjectID != ref.ID {
		return ErrPermissionDenied
	}
	return nil
}

func (s *Service) CreateConversationLink(ctx context.Context, p Principal, tenant, conversation, post string) (ConversationLink, error) {
	if p.TenantID == "" || p.TenantID != tenant || conversation == "" {
		return ConversationLink{}, ErrInvalidArgument
	}
	c, err := s.store.GetConversation(ctx, tenant, conversation)
	if err != nil || s.authorize(ctx, p, c, chatpolicy.ActionRead) != nil {
		return ConversationLink{}, ErrPermissionDenied
	}
	if post != "" {
		v, e := s.store.GetPost(ctx, tenant, conversation, post)
		if e != nil || v.Deleted || !s.postVisibleTo(ctx, p, c, v) {
			return ConversationLink{}, ErrNotFound
		}
	}
	codec := s.linkCodec
	if codec == nil {
		codec = OpaqueLocator{}
	}
	token, err := codec.Encode(tenant, conversation, post)
	if err != nil {
		return ConversationLink{}, err
	}
	return ConversationLink{URL: "/chat/share/" + url.PathEscape(token), TenantID: tenant, ConversationID: conversation, PostID: post}, nil
}

// CreateShareLink is the product-facing name for a conversation locator.
func (s *Service) CreateShareLink(ctx context.Context, p Principal, tenant, conversation, post string) (ConversationLink, error) {
	return s.CreateConversationLink(ctx, p, tenant, conversation, post)
}

func (s *Service) ResolveConversationLink(ctx context.Context, p Principal, token string) (Conversation, *Post, error) {
	if strings.TrimSpace(token) == "" {
		return Conversation{}, nil, ErrNotFound
	}
	codec := s.linkCodec
	if codec == nil {
		codec = OpaqueLocator{}
	}
	t, cID, postID, err := codec.Decode(token)
	if err != nil {
		return Conversation{}, nil, ErrNotFound
	}
	c, err := s.store.GetConversation(ctx, t, cID)
	if err != nil || s.authorize(ctx, p, c, chatpolicy.ActionRead) != nil {
		return Conversation{}, nil, ErrNotFound
	}
	if postID == "" {
		return c, nil, nil
	}
	post, err := s.store.GetPost(ctx, t, cID, postID)
	if err != nil || post.Deleted || !s.postVisibleTo(ctx, p, c, post) {
		return Conversation{}, nil, ErrNotFound
	}
	return c, &post, nil
}

// ResolveShareLink preserves the same current-authorization semantics under
// the shorter transport name.
func (s *Service) ResolveShareLink(ctx context.Context, p Principal, token string) (Conversation, *Post, error) {
	return s.ResolveConversationLink(ctx, p, token)
}

func (s *Service) ForwardPost(ctx context.Context, r ForwardPostRequest) (Post, error) {
	sourceTenant, sourceConversation, sourcePost := r.TenantID, r.ConversationID, r.PostID
	if r.SourceTenantID != "" {
		sourceTenant = r.SourceTenantID
	}
	if r.SourceConversationID != "" {
		sourceConversation = r.SourceConversationID
	}
	if r.SourcePostID != "" {
		sourcePost = r.SourcePostID
	}
	if r.Principal.SubjectID == "" || sourceTenant == "" || sourceConversation == "" || sourcePost == "" || r.DestinationConversationID == "" || r.IdempotencyKey == "" {
		return Post{}, ErrInvalidArgument
	}
	src, err := s.store.GetConversation(ctx, sourceTenant, sourceConversation)
	if err != nil || s.authorize(ctx, r.Principal, src, chatpolicy.ActionRead) != nil {
		return Post{}, ErrPermissionDenied
	}
	post, err := s.store.GetPost(ctx, sourceTenant, sourceConversation, sourcePost)
	if err != nil || post.Deleted || !s.postVisibleTo(ctx, r.Principal, src, post) {
		return Post{}, ErrPermissionDenied
	}
	if r.PostRevision != 0 && post.Revision != r.PostRevision {
		return Post{}, ErrConflict
	}
	dstTenant := r.DestinationTenantID
	if dstTenant == "" {
		dstTenant = r.Principal.TenantID
	}
	dst, err := s.store.GetConversation(ctx, dstTenant, r.DestinationConversationID)
	if err != nil || s.authorize(ctx, r.Principal, dst, chatpolicy.ActionPost) != nil {
		return Post{}, ErrPermissionDenied
	}
	if s.disclosureChecker != nil {
		if err := s.disclosureChecker.Check(ctx, DisclosureInput{Principal: r.Principal, Source: src, SourcePost: post, Destination: dst, At: s.now()}); err != nil {
			return Post{}, ErrPermissionDenied
		}
	} else if src.TenantID != dst.TenantID {
		return Post{}, ErrPermissionDenied
	}
	attr := SourceAttribution{TenantID: post.TenantID, ConversationID: post.ConversationID, PostID: post.ID, PostRevision: post.Revision, OriginalAuthorID: post.AuthorID}
	return s.sendPost(ctx, SendPostRequest{Principal: r.Principal, TenantID: dstTenant, ConversationID: dst.ID, Body: post.Body, IdempotencyKey: r.IdempotencyKey, SourceAttribution: &attr}, true)
}

// postVisibleTo applies history visibility after conversation authorization.
// Knowing a post ID or holding a valid locator never bypasses FROM_JOIN/NONE.
func (s *Service) postVisibleTo(ctx context.Context, p Principal, c Conversation, post Post) bool {
	m, err := s.store.GetMembership(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
	if err != nil {
		return c.Kind == PublicChannel
	}
	if m.LeftAt != nil || m.HistoryVisibility == NoHistory {
		return false
	}
	if m.HistoryVisibility == FromJoin {
		return m.JoinedAt != nil && !post.CreatedAt.Before(*m.JoinedAt)
	}
	return true
}

func validReference(r Reference, want ReferenceKind) bool {
	return r.Kind == want && strings.TrimSpace(r.ID) != "" && strings.TrimSpace(r.TenantID) != "" && (r.Kind != ConversationMention || strings.TrimSpace(r.ConversationID) != "")
}

// MediaDirectory is the read-only port the media service satisfies. It answers
// one question: does this artifact exist, admitted, in this tenant and this
// conversation. A post may not attach another conversation's media, and a
// reference to an artifact that does not exist is refused at commit rather than
// rendered as a broken attachment forever.
type MediaDirectory interface {
	MediaArtifact(ctx context.Context, tenantID, conversationID, artifactID string) (MediaFacts, error)
}

// MediaFacts is what the media service knows about an artifact. The service
// compares it against the reference the caller supplied.
type MediaFacts struct {
	ContentType string
	ByteSize    uint64
	Admitted    bool
}

// SetMediaDirectory installs the media authority used to validate media
// attachments. Without it a media reference is refused, which is the correct
// fail-closed answer for a composition with no media service.
func (s *Service) SetMediaDirectory(d MediaDirectory) { s.mediaDirectory = d }

// validateMediaReference is the commit-time check for an attachment.
func (s *Service) validateMediaReference(ctx context.Context, tenant, conversation string, ref Reference) error {
	if ref.TenantID != tenant {
		return ErrPermissionDenied
	}
	if ref.ConversationID != "" && ref.ConversationID != conversation {
		return ErrPermissionDenied
	}
	if s.mediaDirectory == nil {
		return ErrUnavailable
	}
	facts, err := s.mediaDirectory.MediaArtifact(ctx, tenant, conversation, ref.ID)
	if err != nil || !facts.Admitted {
		return ErrNotFound
	}
	// The wire values are a snapshot, so a caller that disagrees with the media
	// service about what it is attaching is refused rather than quietly
	// corrected: the two would then disagree for the life of the post.
	if ref.ContentType != "" && ref.ContentType != facts.ContentType {
		return ErrInvalidArgument
	}
	if ref.ByteSize != 0 && ref.ByteSize != facts.ByteSize {
		return ErrInvalidArgument
	}
	return nil
}
