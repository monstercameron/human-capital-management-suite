package bootstrap_test

import (
	"context"
	"sync"
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestTodo_NEXT_005_Race proves concurrent retries of one trusted ingress
// request converge on one durable intent and one creation outbox record.
func TestTodo_NEXT_005_Race(t *testing.T) {
	c := newCell(t)
	const workers = 8
	req := promoteWorkerRequest(t, "next005-race-key")
	type answer struct {
		id  string
		err error
	}
	answers := make(chan answer, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, err := c.grpcIntent.CreateIntent(c.grpcContext(context.Background()), req)
			if err != nil {
				answers <- answer{err: err}
				return
			}
			answers <- answer{id: created.GetIntent().GetIntentId()}
		}()
	}
	wg.Wait()
	close(answers)

	var intentID string
	for got := range answers {
		if got.err != nil {
			t.Fatalf("concurrent CreateIntent: %v", got.err)
		}
		if intentID == "" {
			intentID = got.id
		} else if got.id != intentID {
			t.Fatalf("concurrent retries returned different intent IDs %q and %q", intentID, got.id)
		}
	}
	if intentID == "" {
		t.Fatal("no concurrent request returned an intent")
	}
	if got := queryOne[int](t, c,
		`SELECT count(*) FROM intent_instance WHERE tenant_id = $1 AND idempotency_key = $2`,
		pgstore.TenantID(testTenant), "next005-race-key"); got != 1 {
		t.Fatalf("concurrent retry left %d intent records, want one", got)
	}
	if got := queryOne[int](t, c,
		`SELECT count(*) FROM outbox WHERE tenant_id = $1 AND effect_identity = $2`,
		pgstore.TenantID(testTenant), "intent.created:"+intentID); got != 1 {
		t.Fatalf("concurrent retry left %d creation outbox records, want one", got)
	}
}

// TestTodo_NEXT_005_Integration drives the same P1A request through both
// published transports and checks that the resulting simulation is bound to
// the same persisted intent and material proposal digest.
func TestTodo_NEXT_005_Integration(t *testing.T) {
	c := newCell(t)
	req := promoteWorkerRequest(t, "next005-transport-parity")
	grpcCreated, err := c.grpcIntent.CreateIntent(c.grpcContext(context.Background()), req)
	if err != nil {
		t.Fatalf("gRPC CreateIntent: %v", err)
	}
	edgeCreated, err := c.edgeIntent.CreateIntent(context.Background(), edgeRequest(c, req))
	if err != nil {
		t.Fatalf("Connect CreateIntent retry: %v", err)
	}
	intentID := grpcCreated.GetIntent().GetIntentId()
	if edgeCreated.Msg.GetIntent().GetIntentId() != intentID {
		t.Fatalf("gRPC and Connect created %q and %q", intentID, edgeCreated.Msg.GetIntent().GetIntentId())
	}
	grpcSimulation, err := c.grpcIntent.SimulateIntent(c.grpcContext(context.Background()), &intentsv1.SimulateIntentRequest{IntentId: intentID})
	if err != nil {
		t.Fatalf("gRPC SimulateIntent: %v", err)
	}
	edgeSimulation, err := c.edgeIntent.SimulateIntent(context.Background(), edgeRequest(c, &intentsv1.SimulateIntentRequest{IntentId: intentID}))
	if err != nil {
		t.Fatalf("Connect SimulateIntent: %v", err)
	}
	left, right := grpcSimulation.GetSimulation(), edgeSimulation.Msg.GetSimulation()
	if left.GetIntentId() != intentID || right.GetIntentId() != intentID {
		t.Fatalf("transport results escaped their intent: gRPC=%q Connect=%q want %q", left.GetIntentId(), right.GetIntentId(), intentID)
	}
	if left.GetMaterialProposalDigest().GetDigest() == "" || left.GetMaterialProposalDigest().GetDigest() != right.GetMaterialProposalDigest().GetDigest() {
		t.Fatalf("transport proposal digests differ or are empty: gRPC=%q Connect=%q",
			left.GetMaterialProposalDigest().GetDigest(), right.GetMaterialProposalDigest().GetDigest())
	}
	if left.GetZeroEffectReceipt() == nil || right.GetZeroEffectReceipt() == nil ||
		!left.GetZeroEffectReceipt().GetZeroEffect() || !right.GetZeroEffectReceipt().GetZeroEffect() {
		t.Fatalf("one transport omitted the zero-effect receipt: gRPC=%v Connect=%v", left.GetZeroEffectReceipt(), right.GetZeroEffectReceipt())
	}
}

// TestTodo_NEXT_005_Fault injects a malformed typed request at the served
// boundary and proves rejection leaves no partial chronology or business row.
func TestTodo_NEXT_005_Fault(t *testing.T) {
	c := newCell(t)
	seedWorkforce(t, c)
	before := snapshotDatabase(t, c)
	req := promoteWorkerRequest(t, "next005-malformed-payload")
	req.Request.ProtobufWireBytes = []byte{0x0a, 0xff}
	created, err := c.grpcIntent.CreateIntent(c.grpcContext(context.Background()), req)
	if err != nil {
		t.Fatalf("CreateIntent should durably record a request before simulation: %v", err)
	}
	afterCreate := snapshotDatabase(t, c)
	if _, err := c.grpcIntent.SimulateIntent(c.grpcContext(context.Background()), &intentsv1.SimulateIntentRequest{IntentId: created.GetIntent().GetIntentId()}); err == nil {
		t.Fatal("malformed typed payload reached a successful simulation")
	}
	assertSnapshotsEqual(t, afterCreate, snapshotDatabase(t, c))
	if before["workforce_worker"] != afterCreate["workforce_worker"] {
		t.Fatal("creating the malformed draft changed workforce state")
	}
	if got := queryOne[int](t, c,
		`SELECT count(*) FROM intent_instance WHERE tenant_id = $1 AND idempotency_key = $2`,
		pgstore.TenantID(testTenant), "next005-malformed-payload"); got != 1 {
		t.Fatalf("faulted simulation has %d intent_instance rows, want the single durable draft", got)
	}
	if got := queryOne[int](t, c,
		`SELECT count(*) FROM outbox WHERE tenant_id = $1 AND effect_identity LIKE 'intent.created:%'`,
		pgstore.TenantID(testTenant)); got != 1 {
		t.Fatalf("faulted simulation has %d intent.created rows, want one draft event", got)
	}
}

// TestTodo_NEXT_005_Security proves a caller without a verified credential
// cannot use a valid promotion payload to create an intent or outbox event.
func TestTodo_NEXT_005_Security(t *testing.T) {
	c := newCell(t)
	forged := promoteWorkerRequest(t, "next005-forged-trusted-fields")
	forged.Initiator = &intentsv1.PrincipalReference{
		PrincipalId: "principal-victim",
		Kind:        intentsv1.InitiatorKind_INITIATOR_KIND_AGENT,
	}
	created, err := c.grpcIntent.CreateIntent(c.grpcContext(context.Background()), forged)
	if err != nil {
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("forged initiator refusal = %v, want InvalidArgument", err)
		}
		if got := queryOne[int](t, c,
			`SELECT count(*) FROM intent_instance WHERE tenant_id = $1 AND idempotency_key = $2`,
			pgstore.TenantID(testTenant), "next005-forged-trusted-fields"); got != 0 {
			t.Fatalf("forged initiator refusal wrote %d intent rows", got)
		}
		if got := queryOne[int](t, c,
			`SELECT count(*) FROM outbox WHERE tenant_id = $1 AND effect_identity LIKE 'intent.created:%'`,
			pgstore.TenantID(testTenant)); got != 0 {
			t.Fatalf("forged initiator refusal wrote %d creation outbox rows", got)
		}
	} else {
		if got := created.GetIntent().GetTenantId(); got != testTenant {
			t.Fatalf("request selected tenant %q instead of the credential tenant %q", got, testTenant)
		}
		if got := created.GetIntent().GetInitiator().GetPrincipalId(); got != testSubject {
			t.Fatalf("request selected initiator %q instead of the credential principal %q", got, testSubject)
		}
		if got := created.GetIntent().GetInitiator().GetKind(); got != intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN {
			t.Fatalf("request selected initiator kind %s, want credential-derived HUMAN", got)
		}
	}

	req := promoteWorkerRequest(t, "next005-unauthenticated")
	if _, err := c.grpcIntent.CreateIntent(context.Background(), req); err == nil {
		t.Fatal("unauthenticated CreateIntent succeeded")
	}
	if got := queryOne[int](t, c,
		`SELECT count(*) FROM intent_instance WHERE tenant_id = $1 AND idempotency_key = $2`,
		pgstore.TenantID(testTenant), "next005-unauthenticated"); got != 0 {
		t.Fatalf("unauthenticated request created %d intents", got)
	}
}

// TestTodo_NEXT_005_Conformance proves the promotion simulation's release
// boundary: the stored request stays SIMULATE/NOT_PLANNED and only the
// intent-created chronology record is emitted.
func TestTodo_NEXT_005_Conformance(t *testing.T) {
	c := newCell(t)
	seedWorkforce(t, c)
	before := snapshotDatabase(t, c)
	created, simulated := c.createAndSimulate(t, "next005-conformance")
	if created.GetExecutionMode() != intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE ||
		created.GetLifecycle().GetExecution() != intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED {
		t.Fatalf("P1A created intent has mode/state %s/%s", created.GetExecutionMode(), created.GetLifecycle().GetExecution())
	}
	if receipt := simulated.GetZeroEffectReceipt(); receipt == nil || !receipt.GetZeroEffect() {
		t.Fatalf("simulation omitted its zero-effect receipt: %v", receipt)
	}
	assertSnapshotsEqual(t, before, snapshotDatabase(t, c))
	identities := queryAll[string](t, c,
		`SELECT effect_identity FROM outbox WHERE tenant_id = $1 ORDER BY effect_identity`, pgstore.TenantID(testTenant))
	if len(identities) != 1 || identities[0] != "intent.created:"+created.GetIntentId() {
		t.Fatalf("P1A emitted non-creation effects: %v", identities)
	}
}

// TestTodo_NEXT_005_Mutation validates the zero-effect test oracle by applying
// a controlled write to a seeded workforce row and requiring its fingerprint
// to change. This prevents the conformance proof from passing on a blind scan.
func TestTodo_NEXT_005_Mutation(t *testing.T) {
	c := newCell(t)
	seedWorkforce(t, c)
	before := snapshotDatabase(t, c)
	if _, err := c.pool.Exec(context.Background(),
		`UPDATE workforce_worker SET grade = 'P9' WHERE worker_key = 'omar-reyes'`); err != nil {
		t.Fatalf("apply controlled mutation: %v", err)
	}
	mutated := snapshotDatabase(t, c)
	if mutated["workforce_worker"] == before["workforce_worker"] {
		t.Fatal("the P1A oracle failed to detect a changed workforce row")
	}
}
