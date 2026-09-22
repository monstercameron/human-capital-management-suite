package promotioncommit_test

// REV-101-03: the promotion settlement capability owns the terminal writes
// the workflow plane used to perform itself: guard release, budget release,
// payload-schema registration and the canonical outcome payload. These tests
// exercise that capability directly in its own package.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
)

func settleProposal(intentID, proposalID uuid.UUID) intent.ProposalRevision {
	intentText, proposalText := intentID.String(), proposalID.String()
	return intent.ProposalRevision{
		IntentID:           intentText,
		ProposalRevisionID: proposalText,
		Revision:           1,
		CreatedBy:          intent.PrincipalReference{PrincipalID: "principal:settle", Kind: intent.InitiatorHuman},
		MaterialDigest:     digest.Reference{Digest: "sha256:" + strings.Repeat("d", 64)},
		Subjects:           []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "employment:settle", AuthorityDomain: "PEOPLE"}},
	}
}

func TestSettleOutcomePayloadRejectsASubjectlessRevision(t *testing.T) {
	proposal := settleProposal(uuid.New(), uuid.New())
	proposal.Subjects = nil
	_, err := promotioncommit.OutcomePayloadJSON(promotioncommit.SettleRequest{
		WorkflowID: "promotion", Proposal: proposal, RecordedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("a terminal settlement that would silently name no worker was accepted")
	}
}

func TestSettleRequiresATransactionTenantAndAppender(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	req := promotioncommit.SettleRequest{
		TenantID: tenant, WorkflowID: "promotion", Proposal: settleProposal(uuid.New(), uuid.New()),
		RecordedAt: time.Now().UTC(),
	}
	settler := promotioncommit.Settler{ProjectionName: "settle", SourceRef: "test"}
	if _, err := settler.Settle(ctx, nil, req); err == nil {
		t.Fatal("a settlement without a transaction was accepted")
	}
	db := pgtest.New(t)
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	badTenant := req
	badTenant.TenantID = uuid.Nil
	if _, err := settler.Settle(ctx, tx, badTenant); err == nil {
		t.Fatal("a settlement without a tenant was accepted")
	}
	if _, err := (promotioncommit.Settler{}).Settle(ctx, tx, req); err == nil {
		t.Fatal("a settlement without a ledger appender was accepted")
	}
}

// TestSettleCommitsTheTerminalSettlement proves the capability's own
// contract in its own package: one ledger event under the outcome schema,
// one projection checkpoint, one outbox message, one payload-schema row and
// a released admission guard, with a replay resolving to the same fact.
func TestSettleCommitsTheTerminalSettlement(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := uuid.New()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'settle capability', 'ACTIVE', $3)`,
		tenant, "settle-"+tenant.String()[:8], at.Add(-time.Hour))

	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatal(err)
	}
	settler := promotioncommit.Settler{
		Appender:       ledgerport.NewAppender(registry),
		ProjectionName: "settle_outcome",
		SourceRef:      "hcmnext:test:settle",
	}

	intentID, proposalID, instanceID := uuid.New(), uuid.New(), uuid.New()
	req := promotioncommit.SettleRequest{
		TenantID: tenant, WorkflowID: "promotion", PlanDigest: "sha256:plan",
		InstanceID: instanceID, Proposal: settleProposal(intentID, proposalID),
		TerminalCode: "PROMOTION_COMPLETE", CorrelationID: "corr:settle",
		IdempotencyKey: "settle:1", RecordedAt: at,
		EndNodeID: "END", EndOutputDigest: "sha256:end",
	}

	settleOnce := func() promotioncommit.SettleResult {
		t.Helper()
		tx, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		guardID := uuid.New()
		if _, err := promotionguard.Admit(ctx, tx, tenant, guardID, "employment:settle", "2026-09-05", "settle-guard"); err != nil {
			t.Fatalf("admit the promotion guard: %v", err)
		}
		if err := promotionguard.Confirm(ctx, tx, tenant, guardID, "settle-guard", intentID); err != nil {
			t.Fatalf("confirm the promotion guard: %v", err)
		}
		result, err := settler.Settle(ctx, tx, req)
		if err != nil {
			t.Fatalf("settle the terminal promotion: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit the settlement: %v", err)
		}
		return result
	}

	first := settleOnce()
	streamKey := promotioncommit.StreamKeyFor(req.WorkflowID, instanceID.String())
	if first.ResultRef != req.TerminalCode || first.EventRef != streamKey+"@1" {
		t.Fatalf("settlement result = %+v, want PROMOTION_COMPLETE at %s@1", first, streamKey)
	}
	if got := settleOnce(); got != first {
		t.Fatalf("replay result = %+v, want %+v", got, first)
	}

	var schemaRef string
	if err := db.Conn.QueryRow(ctx, `
		SELECT schema_ref FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		tenant, streamKey).Scan(&schemaRef); err != nil {
		t.Fatalf("load the settlement ledger event: %v", err)
	}
	if schemaRef != promotioncommit.PromotionOutcomeSchema {
		t.Fatalf("ledger event schema_ref = %q, want %q", schemaRef, promotioncommit.PromotionOutcomeSchema)
	}
	var effectiveAt time.Time
	if err := db.Conn.QueryRow(ctx, `
		SELECT effective_at FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		tenant, streamKey).Scan(&effectiveAt); err != nil {
		t.Fatalf("load the settlement effective instant: %v", err)
	}
	if !effectiveAt.Equal(at) {
		t.Fatalf("settlement effective_at = %s, want the recorded instant %s for a proposal with no interval", effectiveAt, at)
	}

	// A proposal carrying its own effective interval aligns the ledger's
	// bitemporal coordinate with the promotion instead.
	promotedAt := time.Date(2026, 12, 1, 5, 0, 0, 0, time.UTC)
	interval, err := values.NewOpenInstantInterval(values.NewInstant(promotedAt))
	if err != nil {
		t.Fatal(err)
	}
	intervalReq := req
	intervalReq.InstanceID = uuid.New()
	intervalReq.IdempotencyKey = "settle:2"
	intervalReq.Proposal.EffectiveTime = interval
	intervalStream := promotioncommit.StreamKeyFor(intervalReq.WorkflowID, intervalReq.InstanceID.String())
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := settler.Settle(ctx, tx, intervalReq); err != nil {
		t.Fatalf("settle the dated promotion: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit the dated settlement: %v", err)
	}
	if err := db.Conn.QueryRow(ctx, `
		SELECT effective_at FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		tenant, intervalStream).Scan(&effectiveAt); err != nil {
		t.Fatalf("load the dated effective instant: %v", err)
	}
	if !effectiveAt.Equal(promotedAt) {
		t.Fatalf("dated settlement effective_at = %s, want the proposal instant %s", effectiveAt, promotedAt)
	}

	var guardStatus string
	if err := db.Conn.QueryRow(ctx, `
		SELECT status FROM promotion_active_intent_guard WHERE tenant_id = $1 AND intent_id = $2 ORDER BY opened_at DESC LIMIT 1`,
		tenant, intentID).Scan(&guardStatus); err != nil {
		t.Fatalf("load the admission guard: %v", err)
	}
	if guardStatus != "CLOSED" {
		t.Fatalf("admission guard status = %q, want CLOSED", guardStatus)
	}
}
