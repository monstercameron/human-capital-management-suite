package chatstore

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3782: frozen conversation evidence attached to the case a conversation
// is bound to. An attachment is written in one tenant transaction that also
// re-reads the binding (so evidence is never filed against a case the
// conversation is no longer bound to), adds the copy to the records inventory
// as a DERIVED_COPY (so retention and legal holds see it), and appends an audit
// event. The rows are append-only.

var _ chat.CaseEvidencePort = (*Store)(nil)

var evidenceHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// maxEvidenceBodyBytes bounds one frozen message.
const maxEvidenceBodyBytes = 64 << 10

func validEvidence(e chat.CaseEvidenceExcerpt) bool {
	e.CaseRef = strings.TrimSpace(e.CaseRef)
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.ConversationID) == "" || e.CaseRef == "" || utf8.RuneCountInString(e.CaseRef) > MaxCaseRefRunes ||
		strings.TrimSpace(e.AttachedBy) == "" || len(e.Items) == 0 || len(e.Items) > chat.MaxEvidenceMessages ||
		utf8.RuneCountInString(e.Note) > chat.MaxEvidenceNoteRunes || utf8.RuneCountInString(e.CaseRevision) > chat.MaxEvidenceRevisionRunes {
		return false
	}
	seen := map[string]bool{}
	for _, item := range e.Items {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.MessageID) == "" || strings.TrimSpace(item.AuthorID) == "" || item.AuthoredAt.IsZero() ||
			!evidenceHashPattern.MatchString(item.ContentHash) || len(item.Body) > maxEvidenceBodyBytes || seen[item.MessageID] {
			return false
		}
		seen[item.MessageID] = true
	}
	return true
}

// AttachCaseEvidence freezes the items against the case the conversation is
// bound to. It returns chat.ErrNotFound when the conversation has no case in the
// tenant, and chat.ErrCaseEvidenceCaseChanged when the case in force is not the
// one the evidence was reviewed against.
func (s *Store) AttachCaseEvidence(ctx context.Context, tenantID string, e chat.CaseEvidenceExcerpt) (chat.CaseEvidenceExcerpt, error) {
	e.CaseRef = strings.TrimSpace(e.CaseRef)
	if strings.TrimSpace(tenantID) == "" || !validEvidence(e) {
		return chat.CaseEvidenceExcerpt{}, chat.ErrInvalidArgument
	}
	var attachedAt time.Time
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		bound, found, err := activeCaseIn(ctx, tx, tenantID, e.ConversationID)
		if err != nil {
			return err
		}
		if !found {
			return chat.ErrNotFound
		}
		if bound.CaseRef != e.CaseRef {
			return chat.ErrCaseEvidenceCaseChanged
		}
		for i, item := range e.Items {
			var at time.Time
			if err := tx.QueryRow(ctx, `INSERT INTO chat_case_evidence(tenant_id,id,excerpt_id,ordinal,conversation_id,case_ref,case_revision,message_id,source_revision,author_id,authored_at,body,content_hash,note,attached_by)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING attached_at`,
				tenantID, item.ID, e.ID, i, e.ConversationID, e.CaseRef, e.CaseRevision, item.MessageID, int64(item.SourceRevision), item.AuthorID, item.AuthoredAt.UTC(), item.Body, item.ContentHash, e.Note, e.AttachedBy).Scan(&at); err != nil {
				return err
			}
			attachedAt = at
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at,derived_from) VALUES($1,$2,$3,'DERIVED_COPY',$4,1,$5,$6)`,
			tenantID, "evidence:"+e.ID, e.ConversationID, e.ID, attachedAt, e.Items[0].MessageID); err != nil {
			return err
		}
		return caseAudit(ctx, tx, tenantID, e.AttachedBy, e.ConversationID, "chat.case.evidence", e.CaseRef)
	})
	if err != nil {
		return chat.CaseEvidenceExcerpt{}, err
	}
	e.TenantID, e.AttachedAt = tenantID, attachedAt.UTC()
	items := make([]chat.CaseEvidenceItem, len(e.Items))
	for i, item := range e.Items {
		item.ExcerptID, item.Ordinal, item.TenantID, item.ConversationID = e.ID, i, tenantID, e.ConversationID
		item.CaseRef, item.CaseRevision, item.Note, item.AttachedBy, item.AttachedAt = e.CaseRef, e.CaseRevision, e.Note, e.AttachedBy, e.AttachedAt
		items[i] = item
	}
	e.Items = items
	return e, nil
}

// maxEvidenceRows bounds one listing.
const maxEvidenceRows = 1000

// CaseEvidence lists what was attached to caseRef from the conversation, oldest
// first. It reads rows only; whether the caller may open the conversation is
// decided before it is called.
func (s *Store) CaseEvidence(ctx context.Context, tenantID, conversationID, caseRef string) ([]chat.CaseEvidenceExcerpt, error) {
	caseRef = strings.TrimSpace(caseRef)
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || caseRef == "" {
		return nil, chat.ErrInvalidArgument
	}
	out := []chat.CaseEvidenceExcerpt{}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,excerpt_id,ordinal,case_revision,message_id,source_revision,author_id,authored_at,body,content_hash,note,attached_by,attached_at
FROM chat_case_evidence WHERE tenant_id=$1 AND conversation_id=$2 AND case_ref=$3 ORDER BY attached_at, excerpt_id, ordinal LIMIT $4`, tenantID, conversationID, caseRef, maxEvidenceRows)
		if err != nil {
			return err
		}
		defer rows.Close()
		index := map[string]int{}
		for rows.Next() {
			var item chat.CaseEvidenceItem
			var revision int64
			if err := rows.Scan(&item.ID, &item.ExcerptID, &item.Ordinal, &item.CaseRevision, &item.MessageID, &revision, &item.AuthorID, &item.AuthoredAt, &item.Body, &item.ContentHash, &item.Note, &item.AttachedBy, &item.AttachedAt); err != nil {
				return err
			}
			item.TenantID, item.ConversationID, item.CaseRef, item.SourceRevision = tenantID, conversationID, caseRef, uint64(revision)
			item.AuthoredAt, item.AttachedAt = item.AuthoredAt.UTC(), item.AttachedAt.UTC()
			at, seen := index[item.ExcerptID]
			if !seen {
				at = len(out)
				index[item.ExcerptID] = at
				out = append(out, chat.CaseEvidenceExcerpt{ID: item.ExcerptID, TenantID: tenantID, ConversationID: conversationID, CaseRef: caseRef, CaseRevision: item.CaseRevision, Note: item.Note, AttachedBy: item.AttachedBy, AttachedAt: item.AttachedAt})
			}
			out[at].Items = append(out[at].Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
