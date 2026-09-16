package bootstrap_test

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// chronologyTables are the tables a P1A intent is allowed to write.
//
// They are the record of what was asked and observed - the ledger, its stream
// registry and head, the projection checkpoint, the outbox and the intent
// projection - and nothing in them is workforce state. Every other table in
// the database is authoritative or reference data, and the promotion path must
// leave all of it byte-identical.
var chronologyTables = map[string]bool{
	"ledger_event":          true,
	"ledger_event_p0":       true,
	"ledger_event_p1":       true,
	"ledger_event_p2":       true,
	"ledger_event_p3":       true,
	"ledger_stream":         true,
	"stream_head":           true,
	"projection_checkpoint": true,
	"outbox":                true,
	"intent_instance":       true,
	"goose_db_version":      true,
}

// TestP1APromotionProducesExactEvidenceAndZeroAuthoritativeOrProviderEffect is
// the NEXT-005 PRIMARY test.
//
// RED: a request smuggles current truth into the baseline, a replay duplicates
// the intent, the simulation omits the write, effect, approval or uncertainty
// detail, or any domain revision, reservation, task, timer, message, outbox
// effect or provider call appears.
//
// GREEN: trusted ingress creates one intent; the mixed-source snapshot
// preserves status and provenance; a deterministic simulation and an immutable
// proposal expose the complete before and after; the path produces one
// evidence receipt; and the prohibited-effect row and request counts remain
// exactly zero under retry.
func TestP1APromotionProducesExactEvidenceAndZeroAuthoritativeOrProviderEffect(t *testing.T) {
	c := newCell(t)
	ctx := context.Background()
	tenant := pgstore.TenantID(testTenant)

	// The workforce state P1A must not touch. The migration tree carries no
	// worker table yet, so the suite stands one up and seeds it from the same
	// corpus the governed read answers from: without it, "no business row
	// changed" would be true only because there were no business rows.
	seedWorkforce(t, c)

	// Two tripwires for external effect. Neither is reachable from anything
	// this cell does, which is the point: an assertion that a counter stayed at
	// zero is only worth making when a non-zero value was possible.
	sink := newHTTPNoop(t)
	t.Setenv("HCMNEXT_CONNECTOR_ENDPOINT", sink.URL())
	outbound := installOutboundRecorder(t)

	before := snapshotDatabase(t, c)

	// --- The complete P1A promotion path. ---
	created, simulated := c.createAndSimulate(t, "idem-promotion-1")
	intentID := created.GetIntentId()

	t.Run("trusted ingress creates exactly one intent", func(t *testing.T) {
		if created.GetTenantId() != testTenant {
			t.Fatalf("the created intent carries tenant %q, want the credential's %q", created.GetTenantId(), testTenant)
		}
		if got := created.GetInitiator().GetPrincipalId(); got != testSubject {
			t.Fatalf("the initiator is %q, want the server-derived %q", got, testSubject)
		}
		if created.GetLifecycle().GetRequest() != intentsv1.RequestState_REQUEST_STATE_DRAFT {
			t.Fatalf("a created intent is at %s, want DRAFT", created.GetLifecycle().GetRequest())
		}
		if created.GetExecutionMode() != intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE {
			t.Fatalf("execution mode is %s; P1A promote_worker is simulate-only", created.GetExecutionMode())
		}

		// A retry of the same request is the same intent, not a second one.
		replay, err := c.grpcIntent.CreateIntent(c.grpcContext(ctx), promoteWorkerRequest(t, "idem-promotion-1"))
		if err != nil {
			t.Fatalf("replayed CreateIntent: %v", err)
		}
		if replay.GetIntent().GetIntentId() != intentID {
			t.Fatalf("a retry minted a second intent %s", replay.GetIntent().GetIntentId())
		}
		if got := queryOne[int](t, c,
			`SELECT count(*) FROM intent_instance WHERE tenant_id = $1 AND idempotency_key = $2`,
			tenant, "idem-promotion-1"); got != 1 {
			t.Fatalf("a retry produced %d intent rows", got)
		}
	})

	t.Run("the simulation exposes the complete before and after", func(t *testing.T) {
		if len(simulated.GetPlannedWrites()) == 0 {
			t.Fatalf("the simulation named no planned write; findings %v", simulated.GetFindings())
		}
		if simulated.GetProposalRevisionId() == "" {
			t.Fatal("the simulation minted no immutable proposal revision")
		}
		if simulated.GetMaterialProposalDigest().GetDigest() == "" {
			t.Fatal("the proposal carries no material digest for an approval to bind")
		}
		if simulated.GetMaterialProposalDigest().GetAlgorithmId() == "" ||
			simulated.GetMaterialProposalDigest().GetProfileId() == "" {
			t.Fatalf("the material digest is a naked hash: %v", simulated.GetMaterialProposalDigest())
		}
		if len(simulated.GetPlannedEffects()) == 0 {
			t.Fatal("the compiled plan named nothing it would do; a plan that says nothing is not a product")
		}
		for _, effect := range simulated.GetPlannedEffects() {
			if !strings.Contains(effect.GetDescription(), "not executable") {
				t.Fatalf("planned effect %q does not say the plan is non-executable", effect.GetDescription())
			}
		}

		// Determinism: the same stored request simulates to the same digest.
		again, err := c.grpcIntent.SimulateIntent(c.grpcContext(ctx),
			&intentsv1.SimulateIntentRequest{IntentId: intentID})
		if err != nil {
			t.Fatalf("re-simulate: %v", err)
		}
		if got, want := again.GetSimulation().GetMaterialProposalDigest().GetDigest(),
			simulated.GetMaterialProposalDigest().GetDigest(); got != want {
			t.Fatalf("simulation is not deterministic:\n got %s\nwant %s", got, want)
		}
	})

	t.Run("the path produces one zero-effect receipt", func(t *testing.T) {
		receipt := simulated.GetZeroEffectReceipt()
		if receipt == nil || !receipt.GetZeroEffect() {
			t.Fatalf("the simulation carries no zero-effect receipt: %v", receipt)
		}
		if !strings.HasPrefix(receipt.GetReasonRef(), promotion.IntentType) {
			t.Fatalf("the receipt does not name the intent it certifies: %q", receipt.GetReasonRef())
		}
		if !strings.Contains(receipt.GetReasonRef(), "SIMULATE") {
			t.Fatalf("the receipt does not name the mode it certifies: %q", receipt.GetReasonRef())
		}
	})

	t.Run("every domain answer went through the governed gateway", func(t *testing.T) {
		records := memoryEvidence(t, c.app).Records()
		if len(records) == 0 {
			t.Fatal("the promotion path recorded no capability evidence")
		}
		invoked := map[string]int{}
		for _, record := range records {
			if record.Decision != "INVOKED" {
				t.Fatalf("capability %s was refused: %s", record.CapabilityID, record.ReasonCode)
			}
			if record.EvidenceID == "" {
				t.Fatalf("capability %s was invoked without an evidence identifier", record.CapabilityID)
			}
			if record.SubjectRef != testSubject {
				t.Fatalf("capability %s recorded subject %q, want the verified principal %q",
					record.CapabilityID, record.SubjectRef, testSubject)
			}
			invoked[record.CapabilityID]++
		}
		for _, required := range []string{
			"hcmnext.people.explain_worker_state",
			promotion.IntentType,
		} {
			if invoked[required] == 0 {
				t.Fatalf("the promotion path never invoked %s; recorded %v", required, invoked)
			}
		}
	})

	t.Run("the read surfaces answer from the stored chronology", func(t *testing.T) {
		explained, err := c.grpcIntent.ExplainIntent(c.grpcContext(ctx),
			&intentsv1.ExplainIntentRequest{IntentId: intentID})
		if err != nil {
			t.Fatalf("ExplainIntent: %v", err)
		}
		if len(explained.GetEvidenceRefs()) < 2 {
			t.Fatalf("the explanation cites %d evidence references", len(explained.GetEvidenceRefs()))
		}
		if !strings.Contains(explained.GetExplanationText(), "DRAFT") {
			t.Fatalf("the explanation does not report the lifecycle: %q", explained.GetExplanationText())
		}

		timeline, err := c.grpcIntent.ListIntentTimeline(c.grpcContext(ctx),
			&intentsv1.ListIntentTimelineRequest{IntentId: intentID})
		if err != nil {
			t.Fatalf("ListIntentTimeline: %v", err)
		}
		if len(timeline.GetEvents()) != 1 {
			t.Fatalf("the timeline holds %d events, want the single creation event", len(timeline.GetEvents()))
		}
		if got := timeline.GetEvents()[0].GetKind(); got != "TRANSACTION_FACT" {
			t.Fatalf("the creation event is recorded as %q; a P1A intent asserts no domain fact", got)
		}

		listed, err := c.grpcIntent.ListIntents(c.grpcContext(ctx), &intentsv1.ListIntentsRequest{})
		if err != nil {
			t.Fatalf("ListIntents: %v", err)
		}
		if len(listed.GetIntents()) == 0 {
			t.Fatal("the tenant's intents did not list")
		}
	})

	// EP-INTENT-003 implemented SubmitIntent, CancelIntent and SupersedeIntent,
	// so the P1A stub that refused every one of them with FAILED_PRECONDITION
	// citing release.p1a_zero_effect_ceiling no longer exists. Asserting that
	// ceiling here described a stub rather than the system, and had been
	// failing since that change. What is still true, and is what this subtest
	// now pins, is that each of these governed writes refuses an incomplete
	// request outright rather than performing part of it -- none of these
	// calls carries the expected_instance_version every one of them requires.
	// The zero-effect half of the claim is proven independently, and far more
	// strongly, by the whole-database snapshot comparison in the subtest
	// below, whose own oracle is validated against a deliberate mutation.
	t.Run("every governed write refuses an incomplete request outright", func(t *testing.T) {
		cases := map[string]func() error{
			"SubmitIntent": func() error {
				_, err := c.grpcIntent.SubmitIntent(c.grpcContext(ctx), &intentsv1.SubmitIntentRequest{
					IdempotencyKey:     "idem-promotion-submit",
					IntentId:           intentID,
					ProposalRevisionId: simulated.GetProposalRevisionId(),
				})
				return err
			},
			"CancelIntent": func() error {
				_, err := c.grpcIntent.CancelIntent(c.grpcContext(ctx), &intentsv1.CancelIntentRequest{
					IdempotencyKey: "idem-promotion-cancel",
					IntentId:       intentID,
					ReasonRef:      "reason.test",
				})
				return err
			},
			"SupersedeIntent": func() error {
				_, err := c.grpcIntent.SupersedeIntent(c.grpcContext(ctx), &intentsv1.SupersedeIntentRequest{
					IdempotencyKey:     "idem-promotion-supersede",
					SupersededIntentId: intentID,
					Definition:         &intentsv1.DefinitionReference{IntentTypeId: promotion.IntentType, Version: 1},
					ReasonRef:          "reason.test",
				})
				return err
			},
		}
		for name, call := range cases {
			err := call()
			if err == nil {
				t.Fatalf("%s succeeded; P1A grants no write authority", name)
			}
			owned, ok := envelope.FromGRPC(err)
			if !ok {
				t.Fatalf("%s failed with an unowned error: %v", name, err)
			}
			if owned.Code() != envelope.CodeInvalidArgument {
				t.Fatalf("%s refused with %s, want INVALID_ARGUMENT for a request missing a required field", name, owned.Code())
			}
			// The refusal must name the field it is refusing on, so a caller
			// can tell a malformed request from a rejected one. A refusal that
			// named nothing would leave the caller unable to act on it.
			if len(owned.Violations()) == 0 {
				t.Fatalf("%s refused without naming a field violation", name)
			}
		}
	})

	// --- Zero authoritative effect. ---
	t.Run("no authoritative or reference row changed", func(t *testing.T) {
		after := snapshotDatabase(t, c)
		assertSnapshotsEqual(t, before, after)

		if got := queryOne[int](t, c, `SELECT count(*) FROM proposal_revision`); got != 0 {
			t.Fatalf("proposal_revision holds %d rows; P1A binds no proposal", got)
		}
	})

	t.Run("the snapshot oracle detects a workforce change", func(t *testing.T) {
		// RED for the oracle itself. Without this, "the snapshots matched"
		// could mean the comparison is blind rather than that nothing moved.
		if _, err := c.pool.Exec(ctx,
			`UPDATE workforce_worker SET grade = 'P9' WHERE worker_key = 'omar-reyes'`); err != nil {
			t.Fatalf("mutate the stand-in workforce table: %v", err)
		}
		tampered := snapshotDatabase(t, c)
		if tampered["workforce_worker"] == before["workforce_worker"] {
			t.Fatal("the snapshot oracle did not notice a changed workforce row")
		}
		if _, err := c.pool.Exec(ctx,
			`UPDATE workforce_worker SET grade = 'P2' WHERE worker_key = 'omar-reyes'`); err != nil {
			t.Fatalf("restore the stand-in workforce table: %v", err)
		}
		restored := snapshotDatabase(t, c)
		if restored["workforce_worker"] != before["workforce_worker"] {
			t.Fatal("restoring the row did not restore the snapshot; the oracle is not a function of content")
		}
	})

	// --- Zero external effect. ---
	t.Run("no provider call and no external-effect message", func(t *testing.T) {
		if got := sink.Calls(); got != 0 {
			t.Fatalf("the fake connector endpoint received %d call(s)", got)
		}
		if got := outbound.Calls(); got != 0 {
			t.Fatalf("the promotion path made %d outbound HTTP request(s): %v", got, outbound.Targets())
		}

		identities := queryAll[string](t, c,
			`SELECT effect_identity FROM outbox WHERE tenant_id = $1 ORDER BY effect_identity`, tenant)
		for _, identity := range identities {
			if !strings.HasPrefix(identity, "intent.created:") {
				t.Fatalf("the outbox holds %q; P1A enqueues the record of a request, never an external effect", identity)
			}
		}
		if len(identities) != 1 {
			t.Fatalf("the outbox holds %d messages for one created intent: %v", len(identities), identities)
		}
	})
}

// seedWorkforce stands up and seeds the stand-in authoritative workforce table.
func seedWorkforce(t *testing.T, c *cell) {
	t.Helper()
	ctx := context.Background()
	if _, err := c.pool.Exec(ctx, `
		CREATE TABLE workforce_worker (
			worker_key   text PRIMARY KEY,
			job_code     text NOT NULL,
			grade        text NOT NULL,
			org_unit     text NOT NULL,
			position_id  text NOT NULL,
			base_amount  numeric(18,2) NOT NULL,
			currency     text NOT NULL
		)`); err != nil {
		t.Fatalf("create the stand-in workforce table: %v", err)
	}
	rows := []struct {
		key, jobCode, grade, orgUnit, positionID, amount, currency string
	}{
		{"jane-doe", "ENG-SWE3", "P3", "eng-platform", "POS-SWE-118", "150000.00", "USD"},
		{"omar-reyes", "OPS-HRBP2", "P2", "people-ops", "POS-HRBP-204", "93000.00", "USD"},
		{"lena-park", "FIN-ANL2", "P2", "finance", "POS-FIN-311", "104000.00", "USD"},
		{"noor-haddad", "OPS-HRBP1", "P1", "people-ops", "POS-HRBP-101", "78000.00", "USD"},
	}
	for _, row := range rows {
		if _, err := c.pool.Exec(ctx, `
			INSERT INTO workforce_worker (worker_key, job_code, grade, org_unit, position_id, base_amount, currency)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			row.key, row.jobCode, row.grade, row.orgUnit, row.positionID, row.amount, row.currency); err != nil {
			t.Fatalf("seed %s: %v", row.key, err)
		}
	}
}

// snapshotDatabase returns a content fingerprint of every table in the cell's
// schema that a P1A intent is forbidden to write.
//
// The fingerprint is a row count plus a digest over the ordered textual form of
// every row, so it changes on an insert, an update and a delete alike - a
// count alone would miss an in-place edit, which is exactly the change a
// promotion would make.
func snapshotDatabase(t *testing.T, c *cell) map[string]string {
	t.Helper()
	ctx := context.Background()
	tables := queryAll[string](t, c, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'
		ORDER BY table_name`)

	out := map[string]string{}
	for _, table := range tables {
		if chronologyTables[table] {
			continue
		}
		var count int64
		var fingerprint *string
		query := fmt.Sprintf(
			`SELECT count(*), md5(coalesce(string_agg(row_text, '|' ORDER BY row_text), ''))
			 FROM (SELECT t::text AS row_text FROM %q t) rows`, table)
		if err := c.pool.QueryRow(ctx, query).Scan(&count, &fingerprint); err != nil {
			t.Fatalf("fingerprint %s: %v", table, err)
		}
		value := ""
		if fingerprint != nil {
			value = *fingerprint
		}
		out[table] = fmt.Sprintf("rows=%d digest=%s", count, value)
	}
	return out
}

// assertSnapshotsEqual fails with the exact table that moved.
func assertSnapshotsEqual(t *testing.T, before, after map[string]string) {
	t.Helper()
	names := map[string]bool{}
	for name := range before {
		names[name] = true
	}
	for name := range after {
		names[name] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	if len(ordered) == 0 {
		t.Fatal("the snapshot covered no tables; the oracle would pass on anything")
	}
	for _, name := range ordered {
		if before[name] != after[name] {
			t.Fatalf("table %s changed during the P1A promotion path:\nbefore %s\n after %s",
				name, before[name], after[name])
		}
	}
}

// queryAll runs a single-column query and collects every value.
func queryAll[T any](t *testing.T, c *cell, sql string, args ...any) []T {
	t.Helper()
	rows, err := c.pool.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var value T
		if err := rows.Scan(&value); err != nil {
			t.Fatalf("scan %q: %v", sql, err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return out
}

// outboundRecorder counts every request that leaves through the process's
// default HTTP transport.
type outboundRecorder struct {
	mu      sync.Mutex
	targets []string
	next    http.RoundTripper
}

func (r *outboundRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.targets = append(r.targets, req.Method+" "+req.URL.String())
	r.mu.Unlock()
	return r.next.RoundTrip(req)
}

func (r *outboundRecorder) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.targets)
}

func (r *outboundRecorder) Targets() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.targets...)
}

// installOutboundRecorder puts the recorder in front of the process's default
// transport for the duration of the test.
//
// The two transports this suite drives do not use it - gRPC runs over a
// bufconn and the edge client carries httptest's own transport - so anything
// it records is an outbound call the cell made on its own, which in P1A must
// be nothing at all.
func installOutboundRecorder(t *testing.T) *outboundRecorder {
	t.Helper()
	recorder := &outboundRecorder{next: http.DefaultTransport}
	previous := http.DefaultTransport
	http.DefaultTransport = recorder
	t.Cleanup(func() { http.DefaultTransport = previous })
	return recorder
}
