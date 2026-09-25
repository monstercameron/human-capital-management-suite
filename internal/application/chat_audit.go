package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
)

// auditedChatService records metadata for a chat mutation.
//
// Ordering: the canonical write is the chat database transaction underneath, and
// this decorator runs after that transaction commits. A post-commit audit
// failure therefore cannot be reported as a failed mutation — the post exists.
// So the append is best effort: a failure is queued on this service and retried
// by RetryAudits, and the committed value is returned with a nil error. The
// queue is bounded; once it overflows the oldest entries are dropped and counted
// so the loss is visible rather than silent.
//
// atomicCore is the production composition: the chat transaction writes its own
// audit row, so this decorator only carries the authorization evidence and
// appends nothing.
type auditedChatService struct {
	chatcore.ConversationService
	records    *chatrecords.Service
	atomicCore bool

	mu      sync.Mutex
	pending []pendingChatAudit
	dropped int
}

// maxPendingChatAudits bounds the post-commit retry queue. It is process local:
// a restart loses queued retries, which is why the durable, atomic composition
// (atomicCore) is the production default.
const maxPendingChatAudits = 1024

type pendingChatAudit struct {
	principal chatcore.Principal
	record    chatrecords.Record
	action    string
	authority recordAuthorization
}

// chatRecordLookup is the keyed record read used to settle an idempotent audit
// conflict. A repository that does not offer it falls back to a tenant listing.
type chatRecordLookup interface {
	Record(ctx context.Context, tenantID, recordID string) (chatrecords.Record, bool, error)
}

// ValidateCreate must remain visible through the wrapper so routing rejects
// invalid requests before reserving a route.
func (s *auditedChatService) ValidateCreate(ctx context.Context, r chatcore.CreateConversationRequest) error {
	v, ok := s.ConversationService.(interface {
		ValidateCreate(context.Context, chatcore.CreateConversationRequest) error
	})
	if !ok {
		return chatcore.ErrUnavailable
	}
	return v.ValidateCreate(ctx, r)
}

// recordsReady reports whether an audit record can be written at all.
func (s *auditedChatService) recordsReady() bool {
	return s != nil && s.records != nil && s.records.Repo != nil
}

// authorization is the record-authorization evidence for an entity this service
// has just written or read. The revision comes from that entity, so no second
// GetConversation is needed to prove the same access twice.
func authorization(tenant, actor, conversation string, revision uint64) recordAuthorization {
	return recordAuthorization{tenant: tenant, actor: actor, conversation: conversation, revision: revision}
}

// conversationEvidence loads the conversation revision for a record that has no
// revision of its own. Only share-link creation needs it; every mutation uses
// the revision its own result carries.
func (s *auditedChatService) conversationEvidence(ctx context.Context, p chatcore.Principal, tenant, conversation string) (context.Context, error) {
	if s.records == nil || s.records.Repo == nil {
		return nil, chatcore.ErrUnavailable
	}
	c, err := s.ConversationService.GetConversation(ctx, chatcore.GetConversationRequest{Principal: p, TenantID: tenant, ConversationID: conversation})
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, recordAuthorizationKey{}, authorization(tenant, p.SubjectID, conversation, c.Revision)), nil
}

// audit records one committed mutation. It never converts a durable success into
// a failure: a failed append is queued for RetryAudits.
func (s *auditedChatService) audit(ctx context.Context, p chatcore.Principal, r chatrecords.Record, action string) {
	if s.atomicCore || !s.recordsReady() {
		return
	}
	evidence := authorization(r.TenantID, p.SubjectID, r.ConversationID, r.Revision)
	if err := s.append(context.WithValue(ctx, recordAuthorizationKey{}, evidence), p, r, action); err != nil {
		s.queue(pendingChatAudit{principal: p, record: r, action: action, authority: evidence})
	}
}

func (s *auditedChatService) queue(entry pendingChatAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) >= maxPendingChatAudits {
		s.pending = s.pending[1:]
		s.dropped++
	}
	s.pending = append(s.pending, entry)
}

// PendingAudits reports the queued post-commit audit appends and how many were
// dropped because the queue was full.
func (s *auditedChatService) PendingAudits() (pending, dropped int) {
	if s == nil {
		return 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending), s.dropped
}

// RetryAudits drains the queue, re-appending each record with its original
// authorization evidence. Entries that fail again are re-queued in order, so a
// still-unavailable record store does not lose them.
func (s *auditedChatService) RetryAudits(ctx context.Context) (appended int, err error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	batch := s.pending
	s.pending = nil
	s.mu.Unlock()
	var failed []pendingChatAudit
	for i, entry := range batch {
		appendErr := s.append(context.WithValue(ctx, recordAuthorizationKey{}, entry.authority), entry.principal, entry.record, entry.action)
		if appendErr != nil {
			if err == nil {
				err = appendErr
			}
			failed = append(failed, batch[i:]...)
			break
		}
		appended++
	}
	if len(failed) > 0 {
		s.mu.Lock()
		s.pending = append(failed, s.pending...)
		if len(s.pending) > maxPendingChatAudits {
			s.dropped += len(s.pending) - maxPendingChatAudits
			s.pending = s.pending[len(s.pending)-maxPendingChatAudits:]
		}
		s.mu.Unlock()
	}
	return appended, err
}

func (s *auditedChatService) append(ctx context.Context, p chatcore.Principal, r chatrecords.Record, action string) error {
	_, err := s.records.AppendRecord(ctx, p.SubjectID, r, action, action)
	if !errors.Is(err, chatrecords.ErrConflict) {
		return err
	}
	// A conflict is either an idempotent replay of this exact record or a real
	// collision. Resolve it with a keyed read; only a repository without one
	// falls back to listing the tenant.
	if keyed, ok := s.records.Repo.(chatRecordLookup); ok {
		old, found, readErr := keyed.Record(ctx, r.TenantID, r.RecordID)
		if readErr != nil {
			return readErr
		}
		if found && sameAuditRecord(old, r) {
			return nil
		}
		return err
	}
	rows, readErr := s.records.Repo.List(ctx, r.TenantID)
	if readErr != nil {
		return readErr
	}
	for _, old := range rows {
		if sameAuditRecord(old, r) {
			return nil
		}
	}
	return err
}

func sameAuditRecord(old, r chatrecords.Record) bool {
	return old.RecordID == r.RecordID && old.Revision == r.Revision && old.Kind == r.Kind && old.SourceID == r.SourceID
}

func (s *auditedChatService) CreateConversation(ctx context.Context, r chatcore.CreateConversationRequest) (chatcore.Conversation, error) {
	if !s.recordsReady() {
		return chatcore.Conversation{}, chatcore.ErrUnavailable
	}
	c, err := s.ConversationService.CreateConversation(ctx, r)
	if err != nil {
		return c, err
	}
	s.audit(ctx, r.Principal, chatrecords.Record{TenantID: c.TenantID, ConversationID: c.ID, RecordID: "conversation:" + c.ID, Kind: chatrecords.KindConversation, SourceID: c.ID, Revision: c.Revision}, "chat.conversation.create")
	return c, nil
}
func (s *auditedChatService) UpdateConversation(ctx context.Context, r chatcore.UpdateConversationRequest) (chatcore.Conversation, error) {
	if !s.recordsReady() {
		return chatcore.Conversation{}, chatcore.ErrUnavailable
	}
	c, err := s.ConversationService.UpdateConversation(ctx, r)
	if err != nil {
		return c, err
	}
	s.audit(ctx, r.Principal, chatrecords.Record{TenantID: c.TenantID, ConversationID: c.ID, RecordID: "conversation:" + c.ID, Kind: chatrecords.KindConversation, SourceID: c.ID, Revision: c.Revision}, "chat.conversation.update")
	return c, nil
}
func (s *auditedChatService) AddMembership(ctx context.Context, r chatcore.AddMembershipRequest) (chatcore.Membership, error) {
	if !s.recordsReady() {
		return chatcore.Membership{}, chatcore.ErrUnavailable
	}
	m, err := s.ConversationService.AddMembership(ctx, r)
	if err != nil {
		return m, err
	}
	s.audit(ctx, r.Principal, membershipRecord(m), "chat.membership.add")
	return m, nil
}
func (s *auditedChatService) RemoveMembership(ctx context.Context, r chatcore.RemoveMembershipRequest) (chatcore.Membership, error) {
	if !s.recordsReady() {
		return chatcore.Membership{}, chatcore.ErrUnavailable
	}
	m, err := s.ConversationService.RemoveMembership(ctx, r)
	if err != nil {
		return m, err
	}
	s.audit(ctx, r.Principal, membershipRecord(m), "chat.membership.remove")
	return m, nil
}
func (s *auditedChatService) SendPost(ctx context.Context, r chatcore.SendPostRequest) (chatcore.Post, error) {
	if !s.recordsReady() {
		return chatcore.Post{}, chatcore.ErrUnavailable
	}
	p, err := s.ConversationService.SendPost(ctx, r)
	if err != nil {
		return p, err
	}
	s.audit(ctx, r.Principal, postRecord(p, chatrecords.KindPost), "chat.post.send")
	return p, nil
}
func (s *auditedChatService) EditPost(ctx context.Context, r chatcore.EditPostRequest) (chatcore.Post, error) {
	if !s.recordsReady() {
		return chatcore.Post{}, chatcore.ErrUnavailable
	}
	p, err := s.ConversationService.EditPost(ctx, r)
	if err != nil {
		return p, err
	}
	s.audit(ctx, r.Principal, postRecord(p, chatrecords.KindEdit), "chat.post.edit")
	return p, nil
}
func (s *auditedChatService) DeletePost(ctx context.Context, r chatcore.DeletePostRequest) (chatcore.Post, error) {
	if !s.recordsReady() {
		return chatcore.Post{}, chatcore.ErrUnavailable
	}
	p, err := s.ConversationService.DeletePost(ctx, r)
	if err != nil {
		return p, err
	}
	s.audit(ctx, r.Principal, postRecord(p, chatrecords.KindTombstone), "chat.post.delete")
	return p, nil
}
func postRecord(p chatcore.Post, k chatrecords.Kind) chatrecords.Record {
	return chatrecords.Record{TenantID: p.TenantID, ConversationID: p.ConversationID, RecordID: "post:" + p.ID, Kind: k, SourceID: p.ID, Revision: p.Revision, CreatedAt: p.CreatedAt}
}
func membershipRecord(m chatcore.Membership) chatrecords.Record {
	return chatrecords.Record{TenantID: m.TenantID, ConversationID: m.ConversationID, RecordID: "membership:" + m.ConversationID + ":" + m.HomeTenantID + ":" + m.SubjectID, Kind: chatrecords.KindMembership, SourceID: m.SubjectID, Revision: m.Revision}
}

func (s *auditedChatService) references() (chatcore.ReferenceService, error) {
	v, ok := s.ConversationService.(chatcore.ReferenceService)
	if !ok {
		return nil, chatcore.ErrUnavailable
	}
	return v, nil
}
func (s *auditedChatService) SuggestReferences(ctx context.Context, r chatcore.SuggestReferencesRequest) ([]chatcore.ReferenceCandidate, error) {
	v, e := s.references()
	if e != nil {
		return nil, e
	}
	return v.SuggestReferences(ctx, r)
}
func (s *auditedChatService) ResolveShareLink(ctx context.Context, p chatcore.Principal, token string) (chatcore.Conversation, *chatcore.Post, error) {
	v, e := s.references()
	if e != nil {
		return chatcore.Conversation{}, nil, e
	}
	return v.ResolveShareLink(ctx, p, token)
}

// ReadAuthorizedReference is a read-only project-link lookup. It deliberately
// bypasses CreateShareLink so opening a preview cannot emit a share-created
// audit event.
func (s *auditedChatService) ReadAuthorizedReference(ctx context.Context, p chatcore.Principal, tenant, conversation, post string) (chatcore.Conversation, *chatcore.Post, error) {
	v, ok := s.ConversationService.(chatcore.AuthorizedReferenceReader)
	if !ok {
		return chatcore.Conversation{}, nil, chatcore.ErrUnavailable
	}
	return v.ReadAuthorizedReference(ctx, p, tenant, conversation, post)
}
func (s *auditedChatService) SendPostWithReferences(ctx context.Context, r chatcore.SendPostWithReferencesRequest) (chatcore.Post, error) {
	if !s.recordsReady() {
		return chatcore.Post{}, chatcore.ErrUnavailable
	}
	v, e := s.references()
	if e != nil {
		return chatcore.Post{}, e
	}
	p, e := v.SendPostWithReferences(ctx, r)
	if e != nil {
		return p, e
	}
	s.audit(ctx, r.Principal, postRecord(p, chatrecords.KindPost), "chat.post.send")
	return p, nil
}
func (s *auditedChatService) CreateShareLink(ctx context.Context, p chatcore.Principal, tenant, conversation, post string) (chatcore.ConversationLink, error) {
	// A share link carries no revision of its own, so this is the one record
	// whose authorization evidence needs the conversation revision read.
	ctx, e := s.conversationEvidence(ctx, p, tenant, conversation)
	if e != nil {
		return chatcore.ConversationLink{}, e
	}
	v, e := s.references()
	if e != nil {
		return chatcore.ConversationLink{}, e
	}
	link, e := v.CreateShareLink(ctx, p, tenant, conversation, post)
	if e != nil {
		return link, e
	}
	digest := sha256.Sum256([]byte(link.URL))
	record := chatrecords.Record{TenantID: tenant, ConversationID: conversation, RecordID: "share:" + hex.EncodeToString(digest[:]), Kind: chatrecords.KindShareLink, SourceID: post, Revision: 1}
	if err := s.append(ctx, p, record, "chat.share.create"); err != nil {
		s.queue(pendingChatAudit{principal: p, record: record, action: "chat.share.create", authority: ctx.Value(recordAuthorizationKey{}).(recordAuthorization)})
	}
	return link, nil
}
func (s *auditedChatService) ForwardPost(ctx context.Context, r chatcore.ForwardPostRequest) (chatcore.Post, error) {
	if !s.recordsReady() {
		return chatcore.Post{}, chatcore.ErrUnavailable
	}
	v, e := s.references()
	if e != nil {
		return chatcore.Post{}, e
	}
	p, e := v.ForwardPost(ctx, r)
	if e != nil {
		return p, e
	}
	s.audit(ctx, r.Principal, postRecord(p, chatrecords.KindDerived), "chat.post.forward")
	return p, nil
}

var _ chatcore.ConversationService = (*auditedChatService)(nil)
var _ chatcore.ReferenceService = (*auditedChatService)(nil)
