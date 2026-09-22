package effects

// REV-101-03: the workflow terminal effect must not own promotion domain
// writes. The promotion settlement capability
// (internal/data/promotioncommit.Settler) owns guard release, budget
// release, payload-schema registration and the outcome payload; the
// terminal writer invokes it and records only its typed result.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// recordingSettler stands in for the promotion settlement capability: it
// records the request the terminal writer built and returns a fixed typed
// result without touching any table.
type recordingSettler struct {
	requests []promotioncommit.SettleRequest
	result   promotioncommit.SettleResult
	err      error
}

func (f *recordingSettler) Settle(_ context.Context, _ dbport.Tx, req promotioncommit.SettleRequest) (promotioncommit.SettleResult, error) {
	f.requests = append(f.requests, req)
	return f.result, f.err
}

func rev10103Tenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'rev10103 terminal', 'ACTIVE', $3)`,
		tenantID, "rev10103-"+tenantID.String()[:8], time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	return tenantID
}

func rev10103Tx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) (dbport.Tx, func()) {
	t.Helper()
	ctx := context.Background()
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatal(err)
	}
	return tx, func() { _ = tx.Rollback(ctx) }
}

func rev10103Proposal(intentID, proposalID uuid.UUID) intent.ProposalRevision {
	intentText, proposalText := intentID.String(), proposalID.String()
	return intent.ProposalRevision{
		IntentID:           intentText,
		ProposalRevisionID: proposalText,
		Revision:           1,
		CreatedBy:          intent.PrincipalReference{PrincipalID: "principal:rev10103", Kind: intent.InitiatorHuman},
		MaterialDigest:     digest.Reference{Digest: "sha256:" + strings.Repeat("d", 64), IntentID: &intentText, ProposalRevisionID: &proposalText},
		Subjects:           []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "employment:rev10103", AuthorityDomain: "PEOPLE"}},
	}
}

func rev10103Request(tenantID, instanceID, intentID, proposalID uuid.UUID, at time.Time) execute.TerminalWriteRequest {
	return execute.TerminalWriteRequest{
		TenantID:        tenantID,
		InstanceID:      instanceID,
		WorkflowID:      "promotion",
		PlanDigest:      "sha256:" + strings.Repeat("p", 64),
		Proposal:        runtime.ProposalBinding{Revision: rev10103Proposal(intentID, proposalID)},
		TerminalCode:    "PROMOTION_COMPLETE",
		CorrelationID:   "corr:rev10103",
		IdempotencyKey:  "terminal:rev10103",
		RecordedAt:      at,
		EndNodeID:       "END",
		EndOutputDigest: "sha256:" + strings.Repeat("e", 64),
	}
}

// TestTodo_REV_101_03 is the PRIMARY case: the terminal writer delegates to
// the promotion settlement capability and records only its typed result. A
// writer that still performed its own guard, budget, schema or ledger writes
// would leave rows behind the fake; here the fake owns every write (none)
// and the returned identity is exactly the capability result's mapping.
func TestTodo_REV_101_03(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := rev10103Tenant(t, db)
	tx, done := rev10103Tx(t, db, tenantID)
	defer done()

	instanceID, intentID, proposalID := uuid.New(), uuid.New(), uuid.New()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	req := rev10103Request(tenantID, instanceID, intentID, proposalID, at)

	fake := &recordingSettler{result: promotioncommit.SettleResult{
		ResultRef: req.TerminalCode,
		EventRef:  StreamKeyFor(req.WorkflowID, instanceID.String()) + "@7",
	}}
	writer := &LedgerTerminalWriter{Settler: fake}
	identity, err := writer.Write(ctx, tx, req)
	if err != nil {
		t.Fatalf("terminal write through the settlement capability: %v", err)
	}
	if identity.ResultRef != fake.result.ResultRef || identity.EventRef != fake.result.EventRef {
		t.Fatalf("terminal identity = %+v, want exactly the capability result %+v", identity, fake.result)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("settlement calls = %d, want exactly 1", len(fake.requests))
	}
	got := fake.requests[0]
	if got.TenantID != tenantID || got.InstanceID != instanceID || got.WorkflowID != req.WorkflowID ||
		got.PlanDigest != req.PlanDigest || got.TerminalCode != req.TerminalCode ||
		got.CorrelationID != req.CorrelationID || got.IdempotencyKey != req.IdempotencyKey ||
		!got.RecordedAt.Equal(at) || got.EndNodeID != req.EndNodeID || got.EndOutputDigest != req.EndOutputDigest {
		t.Fatalf("settlement request routing fields = %+v, want the terminal request's own", got)
	}
	if got.Proposal.ProposalRevisionID != proposalID.String() || got.Proposal.IntentID != intentID.String() ||
		got.Proposal.MaterialDigest.Digest != req.Proposal.Revision.MaterialDigest.Digest ||
		len(got.Proposal.Subjects) != 1 || got.Proposal.Subjects[0].SubjectID != "employment:rev10103" {
		t.Fatalf("settlement proposal = %+v, want the terminal request's revision", got.Proposal)
	}

	// The workflow layer recorded only the typed result: no ledger event,
	// no payload-schema row and no guard or budget write escaped the fake.
	for _, count := range []struct {
		name  string
		query string
		args  []any
	}{
		{"ledger_event", `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, []any{tenantID}},
		{"payload_schema", `SELECT count(*) FROM payload_schema WHERE tenant_id = $1`, []any{tenantID}},
		{"promotion_active_intent_guard", `SELECT count(*) FROM promotion_active_intent_guard WHERE tenant_id = $1`, []any{tenantID}},
	} {
		var rows int
		if err := tx.QueryRow(ctx, count.query, count.args...).Scan(&rows); err != nil {
			t.Fatalf("count %s: %v", count.name, err)
		}
		if rows != 0 {
			t.Fatalf("%s rows = %d behind the fake settler, want 0: the terminal effect must not write domain tables itself", count.name, rows)
		}
	}
}

// TestTodo_REV_101_03_Integration runs the production composition
// (LedgerTerminalWriter over the real promotion settlement capability) and
// proves the whole terminal settlement commits once: one ledger event, one
// projection checkpoint, one outbox message, one payload-schema row and a
// released admission guard. A replay with the same idempotency key returns
// the same event reference without appending a second fact.
func TestTodo_REV_101_03_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := rev10103Tenant(t, db)

	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatal(err)
	}
	writer := &LedgerTerminalWriter{
		Appender:       ledgerport.NewAppender(registry),
		ProjectionName: "rev10103_outcome",
		SourceRef:      "hcmnext:test:rev10103",
	}

	instanceID, intentID, proposalID := uuid.New(), uuid.New(), uuid.New()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	req := rev10103Request(tenantID, instanceID, intentID, proposalID, at)
	streamKey := StreamKeyFor(req.WorkflowID, instanceID.String())

	seedGuard := func(tx dbport.Tx) {
		t.Helper()
		guardID := uuid.New()
		if _, err := promotionguard.Admit(ctx, tx, tenantID, guardID, "employment:rev10103", "2026-09-05", "rev10103-guard"); err != nil {
			t.Fatalf("admit the promotion guard: %v", err)
		}
		if err := promotionguard.Confirm(ctx, tx, tenantID, guardID, "rev10103-guard", intentID); err != nil {
			t.Fatalf("confirm the promotion guard: %v", err)
		}
	}
	commitWrite := func() (string, string) {
		t.Helper()
		tx, done := rev10103Tx(t, db, tenantID)
		defer done()
		seedGuard(tx)
		identity, err := writer.Write(ctx, tx, req)
		if err != nil {
			t.Fatalf("terminal write: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit the terminal settlement: %v", err)
		}
		return identity.ResultRef, identity.EventRef
	}

	resultRef, eventRef := commitWrite()
	if resultRef != req.TerminalCode {
		t.Fatalf("terminal result ref = %q, want %q", resultRef, req.TerminalCode)
	}
	if eventRef != streamKey+"@1" {
		t.Fatalf("terminal event ref = %q, want %q", eventRef, streamKey+"@1")
	}

	var schemaRef string
	var payloadRaw []byte
	if err := db.Conn.QueryRow(ctx, `
		SELECT schema_ref, payload FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		tenantID, streamKey).Scan(&schemaRef, &payloadRaw); err != nil {
		t.Fatalf("load the terminal ledger event: %v", err)
	}
	if schemaRef != PromotionOutcomeSchema {
		t.Fatalf("ledger event schema_ref = %q, want %q", schemaRef, PromotionOutcomeSchema)
	}
	var payload struct {
		WorkerRef           string   `json:"worker_ref"`
		ApprovalDecisionIDs []string `json:"approval_decision_ids"`
	}
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		t.Fatalf("unmarshal the terminal payload: %v", err)
	}
	if payload.WorkerRef != "employment:rev10103" {
		t.Fatalf("payload worker_ref = %q, want %q", payload.WorkerRef, "employment:rev10103")
	}

	var outboxSchema string
	var outboxPayload []byte
	if err := db.Conn.QueryRow(ctx, `
		SELECT schema_ref, payload FROM outbox WHERE tenant_id = $1 AND ordering_key = $2`,
		tenantID, streamKey).Scan(&outboxSchema, &outboxPayload); err != nil {
		t.Fatalf("load the terminal outbox message: %v", err)
	}
	if outboxSchema != PromotionOutcomeSchema || string(outboxPayload) != string(payloadRaw) {
		t.Fatal("the outbox message does not carry the ledger event's own schema and payload")
	}

	var checkpoint int64
	if err := db.Conn.QueryRow(ctx, `
		SELECT last_applied_sequence FROM projection_checkpoint
		WHERE tenant_id = $1 AND projection_name = $2 AND stream_key = $3`,
		tenantID, writer.ProjectionName, streamKey).Scan(&checkpoint); err != nil {
		t.Fatalf("load the projection checkpoint: %v", err)
	}
	if checkpoint != 1 {
		t.Fatalf("projection checkpoint sequence = %d, want 1", checkpoint)
	}

	var schemas int
	if err := db.Conn.QueryRow(ctx, `
		SELECT count(*) FROM payload_schema WHERE tenant_id = $1 AND schema_ref = $2`,
		tenantID, PromotionOutcomeSchema).Scan(&schemas); err != nil {
		t.Fatalf("count payload schemas: %v", err)
	}
	if schemas != 1 {
		t.Fatalf("payload_schema rows = %d, want exactly 1", schemas)
	}

	var guardStatus string
	if err := db.Conn.QueryRow(ctx, `
		SELECT status FROM promotion_active_intent_guard WHERE tenant_id = $1 AND intent_id = $2`,
		tenantID, intentID).Scan(&guardStatus); err != nil {
		t.Fatalf("load the admission guard: %v", err)
	}
	if guardStatus != "CLOSED" {
		t.Fatalf("admission guard status = %q, want CLOSED", guardStatus)
	}

	// A replay of the same terminal write resolves to the same fact.
	replayResult, replayEvent := commitWrite()
	if replayResult != resultRef || replayEvent != eventRef {
		t.Fatalf("replay identity = %q/%q, want %q/%q", replayResult, replayEvent, resultRef, eventRef)
	}
	var events int
	if err := db.Conn.QueryRow(ctx, `
		SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		tenantID, streamKey).Scan(&events); err != nil {
		t.Fatalf("count ledger events after replay: %v", err)
	}
	if events != 1 {
		t.Fatalf("ledger events after replay = %d, want 1", events)
	}
}

// rev10103GoldenRequest is the fixed promotion the golden bytes pin.
func rev10103GoldenRequest(t *testing.T) promotioncommit.SettleRequest {
	t.Helper()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	effective := time.Date(2026, 12, 1, 5, 0, 0, 0, time.UTC)
	interval, err := values.NewOpenInstantInterval(values.NewInstant(effective))
	if err != nil {
		t.Fatal(err)
	}
	tenant := values.TenantId("rev10103-tenant")
	firstKey, err := values.NewResourceKey(tenant, values.Kind("assignment"), "position_id")
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := values.NewResourceKey(tenant, values.Kind("assignment"), "grade")
	if err != nil {
		t.Fatal(err)
	}
	// ProposedState is deliberately unsorted: the golden bytes prove the
	// capability renders the placement summary in stable sorted order.
	proposal := rev10103Proposal(
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
	)
	proposal.Subjects = []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "employment:golden", AuthorityDomain: "PEOPLE"}}
	proposal.ProposedState = []intent.StateAssertion{
		{ResourceKey: firstKey, FieldPath: "position_id", CanonicalText: "POS-2"},
		{ResourceKey: secondKey, FieldPath: "grade", CanonicalText: "M1"},
	}
	proposal.EffectiveTime = interval
	proposal.MaterialDigest = digest.Reference{Digest: "sha256:" + strings.Repeat("d", 64)}

	return promotioncommit.SettleRequest{
		TenantID:            uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		WorkflowID:          "promotion",
		PlanDigest:          "sha256:" + strings.Repeat("p", 64),
		InstanceID:          uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		Proposal:            proposal,
		TerminalCode:        "PROMOTION_COMPLETE",
		CorrelationID:       "corr:golden",
		IdempotencyKey:      "terminal:golden",
		RecordedAt:          at,
		EndNodeID:           "END",
		EndOutputDigest:     "sha256:" + strings.Repeat("e", 64),
		ApprovalDecisionIDs: []string{"55555555-5555-5555-5555-555555555555"},
		TaskSubmissionIDs:   []string{"66666666-6666-6666-6666-666666666666"},
	}
}

// TestTodo_REV_101_03_Golden pins the exact bytes the settlement
// capability records for a fixed promotion: field set, field order and the
// sorted placement summary. Any drift in the canonical outcome payload
// fails here before it can reach the ledger.
func TestTodo_REV_101_03_Golden(t *testing.T) {
	got, err := promotioncommit.OutcomePayloadJSON(rev10103GoldenRequest(t))
	if err != nil {
		t.Fatalf("render the golden outcome payload: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "rev10103_promotion_outcome.golden.json"))
	if err != nil {
		t.Fatalf("read the golden outcome payload: %v", err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("outcome payload drift:\n got: %s\nwant: %s", got, want)
	}
}
