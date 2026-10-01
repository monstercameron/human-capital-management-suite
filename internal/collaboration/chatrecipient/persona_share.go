package chatrecipient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdeliver"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

var (
	// ErrPersonaShareUnavailable means the required server authorities are not composed.
	ErrPersonaShareUnavailable = errors.New("chatrecipient: persona sharing unavailable")
	// ErrPersonaSharePreview means the preview token is expired, consumed, or not bound to the caller.
	ErrPersonaSharePreview = errors.New("chatrecipient: invalid or expired share preview")
	// ErrPersonaShareAudienceChanged asks callers to display a refreshed preview before retrying.
	ErrPersonaShareAudienceChanged = errors.New("chatrecipient: audience changed before share commit")
)

const personaSharePreviewLifetime = 5 * time.Minute

// PrivatePersonaShareSource returns a server-owned projection of one
// invoker-only persona result. Implementations must authorize Principal against
// the ephemeral result on every call and return current conversation and
// persona privacy policy rather than the values from invocation time.
type PrivatePersonaShareSource interface {
	LoadPrivatePersonaResult(context.Context, chat.Principal, string) (PrivatePersonaResult, error)
}

// PrivatePersonaResult is the trusted source for a share preview. The source
// must keep this projection available only to OwnerHomeTenantID/OwnerSubjectID.
type PrivatePersonaResult struct {
	EphemeralID, TenantID, OwnerHomeTenantID, OwnerSubjectID string
	ThreadID, PersonaDisplayName                             string
	Conversation                                             chat.Conversation
	OutputPolicy                                             agentdeliver.Conversation
	AlwaysPrivate                                            bool
	Items                                                    []PrivatePersonaShareItem
}

// PrivatePersonaShareItem is one independently reauthorized part of a private
// result. Text must already satisfy the persona output-link and embed policy.
type PrivatePersonaShareItem struct {
	Output      agentdeliver.ResultItem
	Disclosures []Disclosure
}

// PersonaShareCommitRequest asks a server-owned chat adapter to post the
// invoker's selected content. The adapter must atomically reject the write if
// the destination audience revision differs from ExpectedAudienceRevision.
type PersonaShareCommitRequest struct {
	Principal                chat.Principal
	TenantID, ConversationID string
	ThreadID, Body           string
	ExpectedAudienceRevision uint64
	IdempotencyKey           string
}

// PersonaShareCommitter performs the T2 post as the authenticated invoker and
// compares the audience revision in the same transaction as the post commit.
type PersonaShareCommitter interface {
	CommitPersonaShare(context.Context, PersonaShareCommitRequest) (chat.Post, error)
}

// PersonaSharePreviewRequest selects a private result. Destination and content
// are resolved from the invoker-authorized source, never from client fields.
type PersonaSharePreviewRequest struct {
	Principal         chat.Principal
	EphemeralResultID string
}

// PersonaShareItemPreview is display data for one result item. Dropped items
// contain a coarse reason that does not identify a recipient or policy detail.
type PersonaShareItemPreview struct {
	ID, Text   string
	Kept       bool
	DropReason AudienceFloorReason
}

// PersonaSharePreview is a short-lived, invoker-bound proposal. Its token
// carries no authority by itself; ConfirmPersonaShare reloads the source,
// rechecks the audience, and only then invokes the server committer.
type PersonaSharePreview struct {
	Token, PersonaDisplayName, TenantID, ConversationID, ThreadID string
	AudienceRevision                                              uint64
	ExpiresAt                                                     time.Time
	Items                                                         []PersonaShareItemPreview
}

// PersonaShareConfirmRequest confirms exactly one server-issued preview token.
type PersonaShareConfirmRequest struct {
	Principal chat.Principal
	Token     string
}

// PersonaShareConfirmResult reports a completed share or a regenerated preview.
type PersonaShareConfirmResult struct {
	Shared  bool
	PostID  string
	Preview *PersonaSharePreview
}

type storedPersonaSharePreview struct {
	principal    chat.Principal
	resultID     string
	sourceDigest string
	preview      PersonaSharePreview
	completed    *PersonaShareConfirmResult
	confirming   bool
}

// PersonaShareService owns preview tokens and composes source, audience and
// commit authorities. A client cannot provide item text, disclosures, or a
// destination, and cannot cause a post without confirming a current preview.
type PersonaShareService struct {
	source    PrivatePersonaShareSource
	audience  AudienceFloorAuthority
	committer PersonaShareCommitter
	now       func() time.Time
	mu        sync.Mutex
	previews  map[string]storedPersonaSharePreview
}

// NewPersonaShareService constructs a server-side private-result sharing
// coordinator. Missing ports leave it unavailable and fail closed.
func NewPersonaShareService(source PrivatePersonaShareSource, audience AudienceFloorAuthority, committer PersonaShareCommitter, clock func() time.Time) *PersonaShareService {
	if clock == nil {
		clock = time.Now
	}
	return &PersonaShareService{source: source, audience: audience, committer: committer, now: clock, previews: make(map[string]storedPersonaSharePreview)}
}

// PreviewPersonaShare computes a current item-by-item preview for one private
// result and stores a short-lived token bound to this invoker.
func (s *PersonaShareService) PreviewPersonaShare(ctx context.Context, request PersonaSharePreviewRequest) (PersonaSharePreview, error) {
	if !s.available() || ctx == nil || !validSharePrincipal(request.Principal) || strings.TrimSpace(request.EphemeralResultID) == "" {
		return PersonaSharePreview{}, ErrPersonaShareUnavailable
	}
	result, err := s.loadSource(ctx, request.Principal, request.EphemeralResultID)
	if err != nil {
		return PersonaSharePreview{}, err
	}
	preview, digest, err := s.buildPreview(ctx, request.Principal, result)
	if err != nil {
		return PersonaSharePreview{}, err
	}
	if err := s.savePreview(storedPersonaSharePreview{principal: request.Principal, resultID: request.EphemeralResultID, sourceDigest: digest, preview: preview}); err != nil {
		return PersonaSharePreview{}, err
	}
	return clonePersonaSharePreview(preview), nil
}

// ConfirmPersonaShare re-resolves the source and audience before committing.
// Any changed source, audience revision, or kept/dropped set replaces the
// preview and returns it without posting; a commit-time audience race does the
// same. The committer must enforce ExpectedAudienceRevision atomically.
func (s *PersonaShareService) ConfirmPersonaShare(ctx context.Context, request PersonaShareConfirmRequest) (PersonaShareConfirmResult, error) {
	if !s.available() || ctx == nil || !validSharePrincipal(request.Principal) || strings.TrimSpace(request.Token) == "" {
		return PersonaShareConfirmResult{}, ErrPersonaSharePreview
	}
	if completed, ok := s.completedConfirm(request.Principal, request.Token); ok {
		return completed, nil
	}
	stored, err := s.beginConfirm(request.Principal, request.Token)
	if err != nil {
		return PersonaShareConfirmResult{}, err
	}
	result, err := s.loadSource(ctx, request.Principal, stored.resultID)
	if err != nil {
		s.releaseConfirm(request.Token)
		return PersonaShareConfirmResult{}, err
	}
	current, digest, err := s.buildPreview(ctx, request.Principal, result)
	if err != nil {
		s.releaseConfirm(request.Token)
		return PersonaShareConfirmResult{}, err
	}
	if digest != stored.sourceDigest || current.AudienceRevision != stored.preview.AudienceRevision || !sameShareItems(current.Items, stored.preview.Items) {
		return s.replacePreview(request.Token, request.Principal, stored.resultID, current, digest)
	}
	body := composePersonaShareBody(result.PersonaDisplayName, current.Items)
	if body == "" {
		s.releaseConfirm(request.Token)
		return PersonaShareConfirmResult{}, ErrPersonaSharePreview
	}
	post, err := s.committer.CommitPersonaShare(ctx, PersonaShareCommitRequest{
		Principal: request.Principal, TenantID: result.TenantID, ConversationID: result.Conversation.ID,
		ThreadID: result.ThreadID, Body: body, ExpectedAudienceRevision: current.AudienceRevision,
		IdempotencyKey: "persona-share:" + request.Token,
	})
	if errors.Is(err, ErrPersonaShareAudienceChanged) {
		fresh, freshDigest, freshErr := s.buildPreview(ctx, request.Principal, result)
		if freshErr != nil {
			s.releaseConfirm(request.Token)
			return PersonaShareConfirmResult{}, freshErr
		}
		return s.replacePreview(request.Token, request.Principal, stored.resultID, fresh, freshDigest)
	}
	if err != nil {
		s.releaseConfirm(request.Token)
		return PersonaShareConfirmResult{}, err
	}
	completed := PersonaShareConfirmResult{Shared: true, PostID: post.ID}
	s.completeConfirm(request.Token, completed)
	return completed, nil
}

func (s *PersonaShareService) available() bool {
	return s != nil && !isNilSharePort(s.source) && !isNilAuthority(s.audience) && !isNilSharePort(s.committer) && s.now != nil
}

func (s *PersonaShareService) loadSource(ctx context.Context, principal chat.Principal, id string) (PrivatePersonaResult, error) {
	result, err := s.source.LoadPrivatePersonaResult(ctx, principal, id)
	if err != nil {
		return PrivatePersonaResult{}, chat.ErrPermissionDenied
	}
	if result.EphemeralID != id || result.TenantID != result.Conversation.TenantID || result.OwnerHomeTenantID != principal.TenantID || result.OwnerSubjectID != principal.SubjectID || result.Conversation.ID == "" || result.ThreadID == "" {
		return PrivatePersonaResult{}, chat.ErrPermissionDenied
	}
	return result, nil
}

func (s *PersonaShareService) buildPreview(ctx context.Context, principal chat.Principal, result PrivatePersonaResult) (PersonaSharePreview, string, error) {
	if !validShareSource(result) {
		return PersonaSharePreview{}, "", chat.ErrInvalidArgument
	}
	items, revision := evaluatePersonaShareItems(ctx, result, s.audience)
	items = fitPersonaShareItems(result.PersonaDisplayName, items)
	token, err := newPersonaShareToken()
	if err != nil {
		return PersonaSharePreview{}, "", fmt.Errorf("create share preview token: %w", err)
	}
	preview := PersonaSharePreview{
		Token: token, PersonaDisplayName: result.PersonaDisplayName, TenantID: result.TenantID,
		ConversationID: result.Conversation.ID, ThreadID: result.ThreadID,
		AudienceRevision: revision, ExpiresAt: s.now().Add(personaSharePreviewLifetime), Items: items,
	}
	return preview, personaShareSourceDigest(result), nil
}

func (s *PersonaShareService) savePreview(preview storedPersonaSharePreview) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prunePreviewsLocked()
	if len(s.previews) >= 1024 {
		return ErrPersonaShareUnavailable
	}
	s.previews[preview.preview.Token] = preview
	return nil
}

func (s *PersonaShareService) beginConfirm(principal chat.Principal, token string) (storedPersonaSharePreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prunePreviewsLocked()
	stored, ok := s.previews[token]
	if !ok || stored.principal.TenantID != principal.TenantID || stored.principal.SubjectID != principal.SubjectID || stored.confirming {
		return storedPersonaSharePreview{}, ErrPersonaSharePreview
	}
	stored.confirming = true
	s.previews[token] = stored
	return stored, nil
}

func (s *PersonaShareService) releaseConfirm(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.previews[token]
	if !ok {
		return
	}
	stored.confirming = false
	s.previews[token] = stored
}

func (s *PersonaShareService) completedConfirm(principal chat.Principal, token string) (PersonaShareConfirmResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.previews[token]
	if !ok || stored.principal.TenantID != principal.TenantID || stored.principal.SubjectID != principal.SubjectID || stored.completed == nil {
		return PersonaShareConfirmResult{}, false
	}
	return *stored.completed, true
}

func (s *PersonaShareService) completeConfirm(token string, result PersonaShareConfirmResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.previews[token]
	if !ok {
		return
	}
	stored.confirming = false
	stored.completed = &result
	s.previews[token] = stored
}

func (s *PersonaShareService) replacePreview(oldToken string, principal chat.Principal, resultID string, preview PersonaSharePreview, digest string) (PersonaShareConfirmResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.previews, oldToken)
	s.prunePreviewsLocked()
	if len(s.previews) >= 1024 {
		return PersonaShareConfirmResult{}, ErrPersonaShareUnavailable
	}
	s.previews[preview.Token] = storedPersonaSharePreview{principal: principal, resultID: resultID, sourceDigest: digest, preview: preview}
	return PersonaShareConfirmResult{Preview: ptrPersonaSharePreview(clonePersonaSharePreview(preview))}, nil
}

func (s *PersonaShareService) prunePreviewsLocked() {
	now := s.now()
	for token, preview := range s.previews {
		if !preview.preview.ExpiresAt.After(now) && !preview.confirming {
			delete(s.previews, token)
		}
	}
}
