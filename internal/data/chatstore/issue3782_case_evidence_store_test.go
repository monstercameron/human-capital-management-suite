package chatstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func evidenceExcerpt(id, conversation, caseRef string, bodies ...string) chat.CaseEvidenceExcerpt {
	e := chat.CaseEvidenceExcerpt{ID: id, ConversationID: conversation, CaseRef: caseRef, CaseRevision: "rev-3", Note: "Shows the request", AttachedBy: "hr"}
	for i, body := range bodies {
		e.Items = append(e.Items, chat.CaseEvidenceItem{ID: id + "-" + string(rune('a'+i)), MessageID: "m" + string(rune('1'+i)), SourceRevision: 1, AuthorID: "emp", AuthoredAt: time.Now().UTC().Add(-time.Hour), Body: body, ContentHash: chat.EvidenceContentHash(body)})
	}
	return e
}

func TestIssue3782_CaseEvidenceStore_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	caseSeed(t, s, "host", "thread", "GROUP", "mgr", "hr", "emp")
	caseSeed(t, s, "other", "thread-other", "GROUP", "mgr", "hr")

	// A conversation bound to no case takes no evidence.
	if _, err := s.AttachCaseEvidence(ctx, "host", evidenceExcerpt("x0", "thread", "ER-2041", "hello")); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("attach with no case: %v", err)
	}
	if _, err := s.BindCase(ctx, "host", "b1", chat.CaseBinding{ConversationID: "thread", CaseRef: "ER-2041", OwnerID: "hr", Restricted: true, BoundBy: "admin"}); err != nil {
		t.Fatal(err)
	}
	// Evidence reviewed against another case is refused, nothing is written.
	if _, err := s.AttachCaseEvidence(ctx, "host", evidenceExcerpt("x1", "thread", "ER-9999", "hello")); !errors.Is(err, chat.ErrCaseEvidenceCaseChanged) {
		t.Fatalf("attach against the wrong case: %v", err)
	}
	if got, err := s.CaseEvidence(ctx, "host", "thread", "ER-9999"); err != nil || len(got) != 0 {
		t.Fatalf("a refused attachment left rows: %v %v", got, err)
	}

	saved, err := s.AttachCaseEvidence(ctx, "host", evidenceExcerpt("e1", "thread", "ER-2041", "I asked on Monday", "Approved verbally"))
	if err != nil || len(saved.Items) != 2 || saved.AttachedAt.IsZero() || saved.Items[0].Ordinal != 0 || saved.Items[1].Ordinal != 1 {
		t.Fatalf("attach = %+v err=%v", saved, err)
	}
	if _, err := s.AttachCaseEvidence(ctx, "host", evidenceExcerpt("e2", "thread", "ER-2041", "Later note")); err != nil {
		t.Fatal(err)
	}
	got, err := s.CaseEvidence(ctx, "host", "thread", "ER-2041")
	if err != nil || len(got) != 2 || got[0].ID != "e1" || got[1].ID != "e2" || len(got[0].Items) != 2 {
		t.Fatalf("list = %+v err=%v", got, err)
	}
	first := got[0].Items[0]
	if first.Body != "I asked on Monday" || first.ContentHash != chat.EvidenceContentHash("I asked on Monday") || first.CaseRevision != "rev-3" || first.CaseRef != "ER-2041" || first.AttachedBy != "hr" || first.Note != "Shows the request" || first.MessageID != "m1" {
		t.Fatalf("frozen item = %+v", first)
	}

	// Another workspace neither sees nor writes it.
	if got, err := s.CaseEvidence(ctx, "other", "thread-other", "ER-2041"); err != nil || len(got) != 0 {
		t.Fatalf("evidence leaked across workspaces: %v %v", got, err)
	}
	if got, err := s.CaseEvidence(ctx, "other", "thread", "ER-2041"); err != nil || len(got) != 0 {
		t.Fatalf("evidence read by naming another workspace's conversation: %v %v", got, err)
	}
	if _, err := s.AttachCaseEvidence(ctx, "other", evidenceExcerpt("e3", "thread-other", "ER-2041", "x")); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("cross-tenant attach: %v", err)
	}

	// The copy is append-only.
	err = s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE chat_case_evidence SET body='changed' WHERE tenant_id='host'`)
		return e
	})
	if err == nil {
		t.Fatal("frozen evidence was edited")
	}
	err = s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM chat_case_evidence WHERE tenant_id='host'`)
		return e
	})
	if err == nil {
		t.Fatal("frozen evidence was deleted")
	}

	// Lifting the case or binding another hides the evidence of the old one.
	if _, err := s.BindCase(ctx, "host", "b2", chat.CaseBinding{ConversationID: "thread", CaseRef: "ER-3000", OwnerID: "hr", Restricted: true, BoundBy: "admin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AttachCaseEvidence(ctx, "host", evidenceExcerpt("e4", "thread", "ER-2041", "stale review")); !errors.Is(err, chat.ErrCaseEvidenceCaseChanged) {
		t.Fatalf("attach after the case changed: %v", err)
	}
	if got, _ := s.CaseEvidence(ctx, "host", "thread", "ER-3000"); len(got) != 0 {
		t.Fatalf("evidence of the old case shows under the new one: %v", got)
	}

	// The attachment is a derived copy in the records inventory, and audited.
	var kind, from string
	var audits int
	err = s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT kind,derived_from FROM chat_record_inventory WHERE tenant_id='host' AND record_id='evidence:e1'`).Scan(&kind, &from); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_audit_event WHERE tenant_id='host' AND action='chat.case.evidence' AND actor_id='hr'`).Scan(&audits)
	})
	if err != nil || kind != "DERIVED_COPY" || from != "m1" || audits != 2 {
		t.Fatalf("inventory kind=%q from=%q audits=%d err=%v", kind, from, audits, err)
	}
}

func TestIssue3782_CaseEvidenceStoreRefusesBadInput_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	caseSeed(t, s, "host", "thread", "GROUP", "mgr", "hr")
	if _, err := s.BindCase(ctx, "host", "b1", chat.CaseBinding{ConversationID: "thread", CaseRef: "ER-2041", OwnerID: "hr", BoundBy: "admin"}); err != nil {
		t.Fatal(err)
	}
	bad := map[string]chat.CaseEvidenceExcerpt{
		"no items": evidenceExcerpt("b1", "thread", "ER-2041"),
		"no id":    evidenceExcerpt("", "thread", "ER-2041", "x"),
		"long note": func() chat.CaseEvidenceExcerpt {
			e := evidenceExcerpt("b2", "thread", "ER-2041", "x")
			e.Note = strings.Repeat("n", 501)
			return e
		}(),
		"long rev": func() chat.CaseEvidenceExcerpt {
			e := evidenceExcerpt("b3", "thread", "ER-2041", "x")
			e.CaseRevision = strings.Repeat("r", 81)
			return e
		}(),
		"no attacher": func() chat.CaseEvidenceExcerpt {
			e := evidenceExcerpt("b4", "thread", "ER-2041", "x")
			e.AttachedBy = ""
			return e
		}(),
		"too many": evidenceExcerpt("b5", "thread", "ER-2041", make([]string, chat.MaxEvidenceMessages+1)...),
		"bad hash": func() chat.CaseEvidenceExcerpt {
			e := evidenceExcerpt("b6", "thread", "ER-2041", "x")
			e.Items[0].ContentHash = "nope"
			return e
		}(),
		"no message": func() chat.CaseEvidenceExcerpt {
			e := evidenceExcerpt("b7", "thread", "ER-2041", "x")
			e.Items[0].MessageID = ""
			return e
		}(),
		"no author": func() chat.CaseEvidenceExcerpt {
			e := evidenceExcerpt("b8", "thread", "ER-2041", "x")
			e.Items[0].AuthorID = ""
			return e
		}(),
		"unknown room": evidenceExcerpt("b9", "missing", "ER-2041", "x"),
	}
	for name, e := range bad {
		if _, err := s.AttachCaseEvidence(ctx, "host", e); !errors.Is(err, chat.ErrInvalidArgument) && !errors.Is(err, chat.ErrNotFound) {
			t.Errorf("%s: err = %v, want invalid or not found", name, err)
		}
	}
	if got, _ := s.CaseEvidence(ctx, "host", "thread", "ER-2041"); len(got) != 0 {
		t.Fatalf("refused attachments left evidence: %v", got)
	}
	if _, err := s.CaseEvidence(ctx, "", "thread", "ER-2041"); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no tenant: %v", err)
	}
}
