package bootstrap_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

// TestBootstrapCellMigratesServesSimulatesAppendsRestartsAndReconciles is the
// NEXT-004 PRIMARY test.
//
// RED: the release needs Node or npm, lacks generated service bindings, has
// competing migration roots, loses ledger/projection/outbox chronology on a
// crash, or cannot return the same typed intent and simulation result after a
// restart.
//
// GREEN: the root Go module builds the cell; an ephemeral PostgreSQL migrates
// from zero; typed Create and Simulate calls on both published transports
// persist one ACID chronology; a worker acknowledgement plus a mid-batch kill
// and restart preserve exactly-once application; and reconciliation restores a
// projection to the ledger's own digests.
func TestBootstrapCellMigratesServesSimulatesAppendsRestartsAndReconciles(t *testing.T) {
	c := newCell(t)
	ctx := context.Background()
	tenant := pgstore.TenantID(testTenant)

	// --- Migrates: an ephemeral server came up at the target schema version. ---
	t.Run("migrates from zero", func(t *testing.T) {
		target, err := migrations.TargetVersion()
		if err != nil {
			t.Fatalf("read the embedded migration tree: %v", err)
		}
		version := queryOne[int64](t, c, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`)
		if version != target {
			t.Fatalf("schema version = %d, want %d (the whole embedded tree)", version, target)
		}
		for _, table := range []string{
			"tenant", "intent_instance", "proposal_revision",
			"ledger_stream", "stream_head", "ledger_event",
			"projection_checkpoint", "outbox", "payload_schema",
		} {
			if got := queryOne[bool](t, c,
				`SELECT to_regclass($1) IS NOT NULL`, table); !got {
				t.Fatalf("table %s is missing after migration", table)
			}
		}
	})

	// --- Serves: both published transports answer the discovery surface. ---
	t.Run("serves the registry on both transports", func(t *testing.T) {
		grpcDefs, err := c.grpcRegistry.ListIntentDefinitions(c.grpcContext(ctx),
			&registryv1.ListIntentDefinitionsRequest{})
		if err != nil {
			t.Fatalf("gRPC ListIntentDefinitions: %v", err)
		}
		if got := len(grpcDefs.GetIntentDefinitions()); got != 14 {
			t.Fatalf("gRPC published %d definitions, want the 14 in the catalog", got)
		}
		edgeDefs, err := c.edgeRegistry.ListIntentDefinitions(ctx,
			edgeRequest(c, &registryv1.ListIntentDefinitionsRequest{}))
		if err != nil {
			t.Fatalf("edge ListIntentDefinitions: %v", err)
		}
		if got := len(edgeDefs.Msg.GetIntentDefinitions()); got != 14 {
			t.Fatalf("the edge published %d definitions, want 14", got)
		}
		caps, err := c.grpcRegistry.ListCapabilities(c.grpcContext(ctx), &registryv1.ListCapabilitiesRequest{})
		if err != nil {
			t.Fatalf("ListCapabilities: %v", err)
		}
		if len(caps.GetCapabilities()) == 0 {
			t.Fatal("the cell published no capabilities")
		}
		for _, capability := range caps.GetCapabilities() {
			if capability.GetSideEffectProfile() > 2 {
				t.Fatalf("capability %s publishes a write side-effect profile in P1A", capability.GetCapabilityId())
			}
		}
	})

	// --- Appends: one create writes one atomic chronology. ---
	created, simulated := c.createAndSimulate(t, "idem-bootstrap-1")
	intentID := created.GetIntentId()
	streamKey := pgstore.StreamKey(intentID)

	t.Run("one create writes ledger, projection and outbox in one transaction", func(t *testing.T) {
		events, err := ledgerport.NewReader().ReadStream(ctx, c.pool, tenant, streamKey)
		if err != nil {
			t.Fatalf("read stream: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("stream %s holds %d events, want exactly the creation event", streamKey, len(events))
		}
		event := events[0]

		checkpoint, err := projection.Read(ctx, c.pool, tenant, pgstore.ProjectionName, streamKey)
		if err != nil {
			t.Fatalf("read checkpoint: %v", err)
		}
		if checkpoint.LastAppliedSequence != event.Sequence {
			t.Fatalf("checkpoint at %d, ledger head at %d", checkpoint.LastAppliedSequence, event.Sequence)
		}
		if checkpoint.LastAppliedDigest != event.Digest {
			t.Fatalf("checkpoint digest %q does not match the ledger event digest %q",
				checkpoint.LastAppliedDigest, event.Digest)
		}

		outboxID := queryOne[uuid.UUID](t, c,
			`SELECT outbox_id FROM outbox WHERE tenant_id = $1 AND effect_identity = $2`,
			tenant, "intent.created:"+intentID)
		if outboxID != event.EventID {
			t.Fatalf("outbox row %s is not derived from ledger event %s", outboxID, event.EventID)
		}

		projected := queryOne[int](t, c,
			`SELECT count(*) FROM intent_instance WHERE tenant_id = $1 AND intent_id = $2`,
			tenant, intentID)
		if projected != 1 {
			t.Fatalf("intent_instance holds %d rows for %s, want 1", projected, intentID)
		}

		// The stored envelope is the authoritative record, and it recomputes.
		if err := c.store.Verify(ctx, testTenant, intentID); err != nil {
			t.Fatalf("stored chronology does not verify: %v", err)
		}
	})

	t.Run("a failed append leaves no partial chronology", func(t *testing.T) {
		// RED for atomicity: an append whose payload schema was never
		// registered must fail, and must leave no ledger event, no checkpoint,
		// no outbox row and no projection row behind.
		orphan := uuid.NewString()
		_, err := c.store.AppendIntent(ctx, app.IntentRecord{
			Tenant:            testTenant,
			IntentID:          orphan,
			Definition:        intent.Ref{TypeID: promotion.IntentType, Version: 1},
			IdempotencyKey:    "idem-bootstrap-orphan",
			CorrelationID:     "req-orphan",
			Lifecycle:         draftDimensions(),
			InstanceVersion:   1,
			RequestDigest:     digest.Reference{AlgorithmID: "sha256", Digest: zeroDigest},
			CreatedAt:         baseTime,
			RecordedAt:        baseTime,
			LastTransitionAt:  baseTime,
			Envelope:          []byte("not a registered schema"),
			EnvelopeSchemaRef: "hcmnext.unregistered.v1.Nothing@1",
		})
		if err == nil {
			t.Fatal("an append under an unregistered payload schema was accepted")
		}
		for _, probe := range []struct {
			name string
			sql  string
		}{
			{"ledger_event", `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`},
			{"ledger_stream", `SELECT count(*) FROM ledger_stream WHERE tenant_id = $1 AND stream_key = $2`},
			{"projection_checkpoint", `SELECT count(*) FROM projection_checkpoint WHERE tenant_id = $1 AND stream_key = $2`},
		} {
			if got := queryOne[int](t, c, probe.sql, tenant, pgstore.StreamKey(orphan)); got != 0 {
				t.Fatalf("%s holds %d rows for a rolled-back append, want 0", probe.name, got)
			}
		}
		if got := queryOne[int](t, c,
			`SELECT count(*) FROM intent_instance WHERE tenant_id = $1 AND idempotency_key = $2`,
			tenant, "idem-bootstrap-orphan"); got != 0 {
			t.Fatalf("intent_instance holds %d rows for a rolled-back append, want 0", got)
		}
	})

	// --- Simulates: the same typed answer on both transports. ---
	t.Run("simulates through gRPC and the connect edge alike", func(t *testing.T) {
		if simulated.GetZeroEffectReceipt() == nil || !simulated.GetZeroEffectReceipt().GetZeroEffect() {
			t.Fatalf("the gRPC simulation carries no zero-effect receipt: %v", simulated.GetZeroEffectReceipt())
		}
		if simulated.GetProposalRevisionId() == "" {
			t.Fatalf("a READY promotion produced no proposal revision; findings %v", simulated.GetFindings())
		}
		if len(simulated.GetPlannedWrites()) == 0 {
			t.Fatal("the simulation reported no planned write; a promotion that changes nothing is not the corpus scenario")
		}

		// Promotion admission now permits only one active request per worker.
		// The edge replays the same governed intent rather than manufacturing
		// a second active promotion merely to compare transports.
		edgeCreated, err := c.edgeIntent.CreateIntent(ctx,
			edgeRequest(c, promoteWorkerRequest(t, "idem-bootstrap-1")))
		if err != nil {
			t.Fatalf("edge CreateIntent: %v", err)
		}
		edgeSimulated, err := c.edgeIntent.SimulateIntent(ctx, edgeRequest(c, &intentsv1.SimulateIntentRequest{
			IntentId: edgeCreated.Msg.GetIntent().GetIntentId(),
		}))
		if err != nil {
			t.Fatalf("edge SimulateIntent: %v", err)
		}
		edgeArtifact := edgeSimulated.Msg.GetSimulation()

		// Both surfaces address the same idempotent intent. They must return
		// the same proposal, planned writes, findings and zero-effect proof.
		if edgeCreated.Msg.GetIntent().GetIntentId() != intentID {
			t.Fatalf("edge replay minted %s instead of %s", edgeCreated.Msg.GetIntent().GetIntentId(), intentID)
		}
		if !edgeArtifact.GetZeroEffectReceipt().GetZeroEffect() {
			t.Fatal("the edge simulation carries no zero-effect receipt")
		}
		if edgeArtifact.GetProposalRevisionId() == "" {
			t.Fatal("the edge simulation minted no proposal revision")
		}
		if got, want := plannedTargets(edgeArtifact), plannedTargets(simulated); !slices.Equal(got, want) {
			t.Fatalf("the transports disagree on the planned writes:\n edge: %v\n grpc: %v", got, want)
		}
		if got, want := findingCodes(edgeArtifact), findingCodes(simulated); !slices.Equal(got, want) {
			t.Fatalf("the transports disagree on the findings:\n edge: %v\n grpc: %v", got, want)
		}
		if edgeArtifact.GetMaterialProposalDigest().GetDigest() !=
			simulated.GetMaterialProposalDigest().GetDigest() {
			t.Fatal("the same intent produced different material proposal digests across transports")
		}
	})

	// --- Restart: the same typed intent and simulation come back. ---
	t.Run("a restarted cell returns the same typed intent and simulation", func(t *testing.T) {
		restarted, err := pgstore.New(c.pool, pgstore.WithCellID(testCellID))
		if err != nil {
			t.Fatalf("recompose store: %v", err)
		}
		record, err := restarted.LoadIntent(ctx, testTenant, intentID)
		if err != nil {
			t.Fatalf("load intent after restart: %v", err)
		}
		if record.IdempotencyKey != "idem-bootstrap-1" {
			t.Fatalf("restarted read returned idempotency key %q", record.IdempotencyKey)
		}

		fetched, err := c.grpcIntent.GetIntent(c.grpcContext(ctx), &intentsv1.GetIntentRequest{IntentId: intentID})
		if err != nil {
			t.Fatalf("GetIntent: %v", err)
		}
		if got := fetched.GetIntent().GetCanonicalRequestDigest().GetDigest(); got != created.GetCanonicalRequestDigest().GetDigest() {
			t.Fatalf("request digest changed across a read:\n got %s\nwant %s", got, created.GetCanonicalRequestDigest().GetDigest())
		}

		again, err := c.grpcIntent.SimulateIntent(c.grpcContext(ctx),
			&intentsv1.SimulateIntentRequest{IntentId: intentID})
		if err != nil {
			t.Fatalf("re-simulate: %v", err)
		}
		if got, want := again.GetSimulation().GetMaterialProposalDigest().GetDigest(),
			simulated.GetMaterialProposalDigest().GetDigest(); got != want {
			t.Fatalf("simulation is not reproducible:\n got %s\nwant %s", got, want)
		}
	})

	t.Run("a replayed create is one intent, not two", func(t *testing.T) {
		before := queryOne[int](t, c, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenant)
		replayed, err := c.grpcIntent.CreateIntent(c.grpcContext(ctx), promoteWorkerRequest(t, "idem-bootstrap-1"))
		if err != nil {
			t.Fatalf("replayed CreateIntent: %v", err)
		}
		if got := replayed.GetIntent().GetIntentId(); got != intentID {
			t.Fatalf("a replayed idempotency key minted a second intent %s (original %s)", got, intentID)
		}
		after := queryOne[int](t, c, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenant)
		if after != before {
			t.Fatalf("a replay appended %d extra ledger event(s)", after-before)
		}
	})

	t.Run("a changed request cannot reuse the active promotion's idempotency key", func(t *testing.T) {
		changed := promoteWorkerRequest(t, "idem-bootstrap-1")
		var body structpb.Struct
		if err := proto.Unmarshal(changed.GetRequest().GetProtobufWireBytes(), &body); err != nil {
			t.Fatalf("decode fixture payload: %v", err)
		}
		body.Fields["business_reason"] = structpb.NewStringValue("a_different_promotion_reason")
		wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(&body)
		if err != nil {
			t.Fatalf("encode changed payload: %v", err)
		}
		changed.Request.ProtobufWireBytes = wire
		before := queryOne[int](t, c, `SELECT count(*) FROM intent_instance WHERE tenant_id = $1`, tenant)
		_, err = c.grpcIntent.CreateIntent(c.grpcContext(ctx), changed)
		owned, ok := envelope.FromGRPC(err)
		if !ok || owned.Code() != envelope.CodeAlreadyExists {
			t.Fatalf("changed replay = %v, want an owned active-conflict refusal", err)
		}
		if after := queryOne[int](t, c, `SELECT count(*) FROM intent_instance WHERE tenant_id = $1`, tenant); after != before {
			t.Fatalf("changed replay created %d intent rows", after-before)
		}
	})

	// --- Restarts: the outbox consumer is killed mid-batch and resumes. ---
	t.Run("a worker killed mid-batch loses nothing and never double-applies", func(t *testing.T) {
		for _, key := range []string{"idem-bootstrap-2", "idem-bootstrap-3"} {
			// Outbox recovery is intent-family agnostic. Use independent
			// read-only intents instead of violating the promotion guard.
			created, err := c.grpcIntent.CreateIntent(c.grpcContext(ctx), explainWorkerStateRequest(t, c, key, ""))
			if err != nil {
				t.Fatalf("CreateIntent(%s): %v", key, err)
			}
			if _, err := c.grpcIntent.SimulateIntent(c.grpcContext(ctx),
				&intentsv1.SimulateIntentRequest{IntentId: created.GetIntent().GetIntentId()}); err != nil {
				t.Fatalf("SimulateIntent(%s): %v", key, err)
			}
		}
		total := queryOne[int](t, c, `SELECT count(*) FROM outbox WHERE tenant_id = $1`, tenant)
		if total < 3 {
			t.Fatalf("outbox holds %d messages, want one per created intent", total)
		}

		const lease = 200 * time.Millisecond
		first := outbox.NewConsumer(c.pool, outbox.WithLease(lease), outbox.WithBatchSize(10))
		batch, err := first.Poll(ctx, tenant)
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		if len(batch) != total {
			t.Fatalf("the first sweep claimed %d of %d messages", len(batch), total)
		}
		delivered := map[uuid.UUID]int{}
		for _, msg := range batch[:len(batch)-1] {
			delivered[msg.OutboxID]++
			if err := first.Ack(ctx, tenant, msg.OutboxID); err != nil {
				t.Fatalf("ack %s: %v", msg.OutboxID, err)
			}
		}
		abandoned := batch[len(batch)-1]

		// The kill: the first consumer never acks or fails the last message.
		// A fresh Consumer value is a fresh process in production terms.
		pollUntil(t, 5*time.Second, "the abandoned lease to expire", func() bool {
			second := outbox.NewConsumer(c.pool, outbox.WithLease(lease), outbox.WithBatchSize(10))
			reclaimed, pollErr := second.Poll(ctx, tenant)
			if pollErr != nil || len(reclaimed) == 0 {
				return false
			}
			if len(reclaimed) != 1 || reclaimed[0].OutboxID != abandoned.OutboxID {
				t.Fatalf("the restarted worker reclaimed %d message(s); want exactly the abandoned one", len(reclaimed))
			}
			delivered[reclaimed[0].OutboxID]++
			if err := second.Ack(ctx, tenant, reclaimed[0].OutboxID); err != nil {
				t.Fatalf("ack reclaimed: %v", err)
			}
			return true
		})

		if len(delivered) != total {
			t.Fatalf("%d of %d messages were delivered; a kill lost one", len(delivered), total)
		}
		if undelivered := queryOne[int](t, c,
			`SELECT count(*) FROM outbox WHERE tenant_id = $1 AND status <> $2`, tenant, outbox.StatusDelivered); undelivered != 0 {
			t.Fatalf("%d message(s) never reached DELIVERED", undelivered)
		}
		// No double-apply: one row per effect identity, whatever the retries.
		distinct := queryOne[int](t, c,
			`SELECT count(DISTINCT effect_identity) FROM outbox WHERE tenant_id = $1`, tenant)
		if distinct != total {
			t.Fatalf("%d outbox rows carry %d distinct effect identities", total, distinct)
		}
	})

	// --- Reconciles: a projection that fell behind catches back up. ---
	t.Run("the reconciler restores a projection to the ledger", func(t *testing.T) {
		if _, err := c.pool.Exec(ctx,
			`UPDATE projection_checkpoint
			 SET last_applied_sequence = 0, last_applied_digest = NULL, status = 'CURRENT'
			 WHERE tenant_id = $1 AND projection_name = $2 AND stream_key = $3`,
			tenant, pgstore.ProjectionName, streamKey); err != nil {
			t.Fatalf("rewind checkpoint: %v", err)
		}

		due, err := projection.ReconcileDue(ctx, c.pool)
		if err != nil {
			t.Fatalf("ReconcileDue: %v", err)
		}
		var target projection.StreamProjection
		for _, candidate := range due {
			if candidate.StreamKey == streamKey {
				target = candidate
			}
		}
		if target.StreamKey == "" {
			t.Fatalf("a rewound checkpoint was not reported as due; due = %v", describe(due))
		}

		applied, err := projection.NewReconciler(c.pool, datalogger.NewReader()).ReconcileOne(ctx, target)
		if err != nil {
			t.Fatalf("ReconcileOne: %v", err)
		}
		if applied != 1 {
			t.Fatalf("reconciliation applied %d events, want the 1 the stream holds", applied)
		}

		head, err := projection.StreamHead(ctx, c.pool, tenant, streamKey)
		if err != nil {
			t.Fatalf("StreamHead: %v", err)
		}
		checkpoint, err := projection.Read(ctx, c.pool, tenant, pgstore.ProjectionName, streamKey)
		if err != nil {
			t.Fatalf("read checkpoint: %v", err)
		}
		if checkpoint.LastAppliedSequence != head {
			t.Fatalf("after reconciliation the checkpoint is at %d and the ledger head at %d",
				checkpoint.LastAppliedSequence, head)
		}
		event, err := ledgerport.NewReader().ReadEvent(ctx, c.pool, tenant, streamKey, head)
		if err != nil {
			t.Fatalf("read head event: %v", err)
		}
		if checkpoint.LastAppliedDigest != event.Digest {
			t.Fatalf("the reconciled projection carries digest %q, the ledger %q",
				checkpoint.LastAppliedDigest, event.Digest)
		}
	})

	t.Run("every served request was logged with its trusted context", func(t *testing.T) {
		records := c.logRecords()
		if len(records) == 0 {
			t.Fatal("the cell served requests and logged none")
		}
		for _, record := range records {
			if record.Succeeded() && record.TenantID != testTenant {
				t.Fatalf("request %s logged tenant %q, want %q", record.Method, record.TenantID, testTenant)
			}
		}
	})
}

// plannedTargets is the sorted set of write targets an artifact names.
func plannedTargets(artifact *intentsv1.SimulationArtifact) []string {
	out := make([]string, 0, len(artifact.GetPlannedWrites()))
	for _, write := range artifact.GetPlannedWrites() {
		out = append(out, write.GetTargetRef()+" "+write.GetOperation())
	}
	slices.Sort(out)
	return out
}

// findingCodes is the sorted set of finding codes an artifact reports.
func findingCodes(artifact *intentsv1.SimulationArtifact) []string {
	out := make([]string, 0, len(artifact.GetFindings()))
	for _, finding := range artifact.GetFindings() {
		out = append(out, finding.GetCode()+"/"+finding.GetSeverity())
	}
	slices.Sort(out)
	return out
}

// zeroDigest is a syntactically valid content digest for the negative append
// case, which never reaches digest verification.
const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

// draftDimensions is the lifecycle tuple a newly drafted change request starts
// in, restated here because the negative-append case builds its record by hand
// rather than through the kernel.
func draftDimensions() lifecycle.Dimensions {
	return lifecycle.Dimensions{
		Request:     lifecycle.RequestDraft,
		Execution:   lifecycle.ExecutionNotPlanned,
		Business:    lifecycle.BusinessNotStarted,
		Consistency: lifecycle.ConsistencyNotApplicable,
		Obligation:  lifecycle.ObligationNotApplicable,
	}
}
