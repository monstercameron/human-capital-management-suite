package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// Issue 3782: reviewed conversation evidence attached to a governed case.
//
// A person who owns the case (or a workspace administrator who can open the
// conversation) picks messages of a case-bound conversation, reviews them, and
// Chat freezes a copy of exactly those messages against the case the
// conversation is bound to, and optionally against the revision of the case's
// proposal that the case system names. The copy is supporting evidence and
// nothing more: Chat never records an approval, a policy version or an outcome.
// Those belong to the service that owns the case, and a reaction, a vote or an
// acknowledgement in a conversation is not proof of any of them.
//
// What is stored is the frozen text, who wrote it and when, the revision it had
// when it was frozen, and a hash of it. Every time the evidence is read, the
// source message is read again with the reader's own authority, so the evidence
// can say the original was edited since, or is deleted or no longer open to the
// reader, and shows the frozen text only to someone who can still open the
// original.

// Limits of one attachment.
const (
	MaxEvidenceMessages      = 20
	MaxEvidenceNoteRunes     = 500
	MaxEvidenceRevisionRunes = 80
)

// ErrCaseEvidenceCaseChanged is returned when the conversation is no longer
// bound to the case the evidence was reviewed against. The reviewer sees the
// current binding and decides again.
var ErrCaseEvidenceCaseChanged = errors.New("chat: the conversation is bound to a different case")

// Evidence source states, as the reader's own read of the original decides.
const (
	// EvidenceSourceCurrent: the original is open to the reader and unchanged.
	EvidenceSourceCurrent = "current"
	// EvidenceSourceEdited: the original is open to the reader and has been
	// edited since it was frozen.
	EvidenceSourceEdited = "edited"
	// EvidenceSourceGone: the original is deleted, or is not open to the reader.
	// The two read the same on purpose: the reader is not told which.
	EvidenceSourceGone = "gone"
)

// CaseEvidenceItem is one frozen message of an attachment.
type CaseEvidenceItem struct {
	ID             string
	ExcerptID      string
	Ordinal        int
	TenantID       string
	ConversationID string
	CaseRef        string
	// CaseRevision is the revision of the case's proposal as the case system
	// names it; empty when the person did not name one. It is opaque to Chat.
	CaseRevision   string
	MessageID      string
	SourceRevision uint64
	AuthorID       string
	AuthoredAt     time.Time
	Body           string
	ContentHash    string
	Note           string
	AttachedBy     string
	AttachedAt     time.Time
}

// CaseEvidenceExcerpt is the messages one person attached together.
type CaseEvidenceExcerpt struct {
	ID             string
	TenantID       string
	ConversationID string
	CaseRef        string
	CaseRevision   string
	Note           string
	AttachedBy     string
	AttachedAt     time.Time
	Items          []CaseEvidenceItem
}

// CaseEvidencePort keeps the frozen evidence.
type CaseEvidencePort interface {
	// AttachCaseEvidence writes the attachment if the conversation is still
	// bound to e.CaseRef, else it returns ErrCaseEvidenceCaseChanged (or
	// ErrNotFound when it is bound to no case).
	AttachCaseEvidence(ctx context.Context, tenant string, e CaseEvidenceExcerpt) (CaseEvidenceExcerpt, error)
	// CaseEvidence lists what was attached to caseRef from the conversation,
	// oldest first.
	CaseEvidence(ctx context.Context, tenant, conversation, caseRef string) ([]CaseEvidenceExcerpt, error)
}

// EvidenceContentHash is the hash recorded beside the frozen text.
func EvidenceContentHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// EvidenceSourceState compares a frozen item with the original as one reader
// reads it now. current is nil when the original cannot be read.
func EvidenceSourceState(item CaseEvidenceItem, current *Post) string {
	switch {
	case current == nil || current.Deleted:
		return EvidenceSourceGone
	case current.Revision != item.SourceRevision:
		return EvidenceSourceEdited
	}
	return EvidenceSourceCurrent
}
