package subscription

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

// replayFixture builds one ACTIVE subscription, its current grant, and the
// recorded outbound request for its first envelope.
func replayFixture(t *testing.T, id string) (EventSubscription, ScopeGrant, DeliveryRequest) {
	t.Helper()
	draft, err := NewDraft(subscriptionRequest(id, "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := draft.Activate("requester-2", "approver-1")
	if err != nil {
		t.Fatal(err)
	}
	envelope := deliveryEnvelope(1)
	request := tenantRequest(id, "tenant-a", 1)
	request.SubscriptionRevision = active.Revision
	request.Envelope = envelope
	return active, subscriptionScopeGrant(active), request
}

func replayInput(t *testing.T, id string) ReplayRequest {
	t.Helper()
	subscription, grant, original := replayFixture(t, id)
	return ReplayRequest{Original: original, Candidate: original.Envelope,
		Subscription: subscription, Grant: grant,
		Epoch: 1, RequestedBy: "op:ann"}
}

func assertRejected(t *testing.T, err error, field string) *ReplayRejection {
	t.Helper()
	if err == nil || !errors.Is(err, ErrReplayRejected) {
		t.Fatalf("expected a replay rejection for %s, got %v", field, err)
	}
	var rejection *ReplayRejection
	if !errors.As(err, &rejection) {
		t.Fatalf("rejection for %s is untyped: %v", field, err)
	}
	if rejection.Code != ReplayRejectionCode || rejection.Field != field || rejection.Version == "" {
		t.Fatalf("rejection = %+v", rejection)
	}
	return rejection
}

// TestTodo_SUB_006 proves replay resends the recorded event without inventing
// business history: the redelivery keeps the original event identity on its
// own lineage with a new attempt and epoch, rechecks current subscription
// AuthZ, and anything that invents identity is rejected with SUB_006_REJECTED
// while persisting nothing and calling no provider.
func TestTodo_SUB_006(t *testing.T) {
	journal := NewDeliveryJournal()
	provider := &deliveryProvider{}
	input := replayInput(t, "sub-replay")

	replay, err := PlanReplay(input)
	if err != nil {
		t.Fatalf("admitted replay = %v", err)
	}
	if replay.Delivery.Envelope.Digest() != input.Original.Envelope.Digest() ||
		replay.Delivery.Envelope.Sequence != 1 || replay.Epoch != 1 ||
		replay.OriginalDigest == "" || !replay.Auth.Allowed {
		t.Fatalf("replay = %+v", replay)
	}
	sent, err := journal.DeliverNow(provider, replay.Delivery)
	if err != nil || sent.Operation.State != OperationAcked || len(sent.Operation.Attempts) != 1 {
		t.Fatalf("replayed delivery = %+v, %v", sent, err)
	}

	// Seeded defects: inventing a new event identity or new business history.
	mutants := map[string]func(*CanonicalEnvelope){
		"sequence":        func(e *CanonicalEnvelope) { e.Sequence = 2 },
		"event_kind":      func(e *CanonicalEnvelope) { e.Kind = EventWorkerCreated },
		"schema_version":  func(e *CanonicalEnvelope) { e.SchemaVersion = 2 },
		"tenant_scope":    func(e *CanonicalEnvelope) { e.Tenant = "tenant-b" },
		"payload_digest":  func(e *CanonicalEnvelope) { e.PayloadDigest = "sha256:forged-history" },
		"provenance_ref":  func(e *CanonicalEnvelope) { e.ProvenanceRef = "event:forged" },
		"subject_refs":    func(e *CanonicalEnvelope) { e.SubjectRefs = []string{"worker:2"} },
		"effective_at":    func(e *CanonicalEnvelope) { e.EffectiveAt = e.EffectiveAt.AddDate(0, 0, 1) },
		"known_at":        func(e *CanonicalEnvelope) { e.KnownAt = e.KnownAt.AddDate(0, 0, 1) },
		"replay_envelope": func(e *CanonicalEnvelope) { *e = CanonicalEnvelope{} },
	}
	for field, mutate := range mutants {
		bad := replayInput(t, "sub-replay")
		mutate(&bad.Candidate)
		err := func() error { _, err := PlanReplay(bad); return err }()
		rejection := assertRejected(t, err, field)
		if !strings.Contains(rejection.Error(), ReplayRejectionCode) {
			t.Fatalf("rejection error does not carry its code: %v", err)
		}
	}
	if len(journal.Operations("sub-replay")) != 1 || len(provider.attempts) != 1 {
		t.Fatalf("rejections journaled operations or called the provider: ops=%d calls=%d",
			len(journal.Operations("sub-replay")), len(provider.attempts))
	}
}

// TestTodo_SUB_006_Golden pins the replay evidence: the redelivery carries
// the recorded lineage key bit for bit, and its safe explanation is stable.
func TestTodo_SUB_006_Golden(t *testing.T) {
	input := replayInput(t, "sub-golden")
	replay, err := PlanReplay(input)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := input.Original.validate()
	if err != nil {
		t.Fatal(err)
	}
	if replay.Delivery.IdempotencyKey == "" || replay.Delivery.IdempotencyKey != recorded.IdempotencyKey {
		t.Fatalf("replay forked a new lineage: %q", replay.Delivery.IdempotencyKey)
	}
	again, err := PlanReplay(input)
	if err != nil || again.Delivery.IdempotencyKey != replay.Delivery.IdempotencyKey {
		t.Fatal("identical replays diverged")
	}
	want := "subscription replay subscription=sub-golden epoch=1 sequence=1 digest=" + replay.OriginalDigest +
		" delivery=" + replay.Delivery.IdempotencyKey + " requested_by=op:ann"
	if replay.Explain() != want {
		t.Fatalf("explain = %q", replay.Explain())
	}
}

// TestTodo_SUB_006_Race proves concurrent replays of one recorded request
// converge on its lineage: exactly one provider call, one journaled attempt.
func TestTodo_SUB_006_Race(t *testing.T) {
	journal := NewDeliveryJournal()
	provider := &deliveryProvider{}
	input := replayInput(t, "sub-race")
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replay, err := PlanReplay(input)
			if err != nil {
				errs <- err
				return
			}
			_, err = journal.DeliverNow(provider, replay.Delivery)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	acked := 0
	for err := range errs {
		if err != nil && !errors.Is(err, ErrDeliveryInFlight) {
			t.Fatalf("concurrent replay error = %v", err)
		}
		if err == nil {
			acked++
		}
	}
	if acked == 0 || len(provider.attempts) != 1 {
		t.Fatalf("acked=%d provider_attempts=%d", acked, len(provider.attempts))
	}
	if operations := journal.Operations("sub-race"); len(operations) != 1 || len(operations[0].Attempts) != 1 {
		t.Fatalf("replay forked lineages: %+v", operations)
	}
}

// TestTodo_SUB_006_Integration proves the recovery loop through the real
// dispatcher, journal, gate and dead-letter queue: exhaustion dead-letters,
// an authorized replay redelivers as the lineage's next attempt with its
// original identity, and the dead letter resolves.
func TestTodo_SUB_006_Integration(t *testing.T) {
	journal := NewDeliveryJournal()
	dlq := NewDeadLetterQueue(24 * time.Hour)
	gate := NewDeliveryGate()
	capacity, err := NewDeliveryCapacity(map[string]operation.ConnectorPolicy{
		"endpoint:webhook": {Quota: operation.ConnectorQuota{Limit: 1000, Window: time.Minute, MaxConcurrent: 32}, PerTenantShare: 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := Dispatcher{Journal: journal, Gate: gate, DLQ: dlq,
		Policy: RetryPolicy{MaxAttempts: 1}, DeadLetterOwner: "team:integrations", Capacity: capacity}
	subscription, grant, original := replayFixture(t, "sub-recover")
	poison := &poisonProvider{err: errors.New("connection refused")}
	if _, err := dispatcher.Dispatch(context.Background(), poison, original); !errors.Is(err, ErrRetryExhausted) {
		t.Fatalf("exhaustion = %v", err)
	}
	// The recorded request carries no explicit key; read the lineage back.
	operations := journal.Operations("sub-recover")
	if len(operations) != 1 {
		t.Fatalf("operations = %+v", operations)
	}
	original = operations[0].Request
	letter, ok := dlq.Get("dlq-" + operations[0].OperationID)
	if !ok || letter.Attempts != 1 {
		t.Fatalf("dead letter = %+v, %t", letter, ok)
	}
	healthy := &deliveryProvider{}
	replay, err := PlanReplay(ReplayRequest{Original: letter.Request, Candidate: letter.Request.Envelope,
		Subscription: subscription, Grant: grant, Epoch: 2, RequestedBy: "op:ann"})
	if err != nil {
		t.Fatalf("replay from dead letter = %v", err)
	}
	delivered, err := journal.DeliverNow(healthy, replay.Delivery)
	if err != nil || delivered.Operation.State != OperationAcked || len(delivered.Operation.Attempts) != 2 {
		t.Fatalf("replayed delivery = %+v, %v", delivered, err)
	}
	if delivered.Operation.Request.Envelope.Digest() != original.Envelope.Digest() {
		t.Fatal("replay changed the event identity")
	}
	if _, err := dlq.Resolve(letter.ID, "op:ann", "replayed-epoch-2"); err != nil {
		t.Fatalf("resolve = %v", err)
	}
}

// TestTodo_SUB_006_Fault proves malformed replays, tampered subscriptions and
// ambiguous redeliveries fail without inventing history or forking lineages.
func TestTodo_SUB_006_Fault(t *testing.T) {
	input := replayInput(t, "sub-fault")
	for field, mutate := range map[string]func(*ReplayRequest){
		"requested_by": func(r *ReplayRequest) { r.RequestedBy = " " },
		"epoch":        func(r *ReplayRequest) { r.Epoch = 0 },
		"subscription": func(r *ReplayRequest) { r.Subscription.Digest = "tampered" },
	} {
		bad := input
		mutate(&bad)
		assertRejected(t, func() error { _, err := PlanReplay(bad); return err }(), field)
	}
	bad := input
	bad.Original.Envelope.Sequence = 0
	assertRejected(t, func() error { _, err := PlanReplay(bad); return err }(), "replay_request")

	journal := NewDeliveryJournal()
	provider := &deliveryProvider{err: ambiguousDeliveryError{}}
	replay, err := PlanReplay(input)
	if err != nil {
		t.Fatal(err)
	}
	result, err := journal.DeliverNow(provider, replay.Delivery)
	if !errors.Is(err, ErrDeliveryAmbiguous) || !result.Operation.Ambiguous {
		t.Fatalf("ambiguous replay = %+v, %v", result, err)
	}
	provider.err = nil
	retry, err := journal.DeliverNow(provider, replay.Delivery)
	if err != nil || len(retry.Operation.Attempts) != 2 ||
		retry.Operation.Request.Envelope.Digest() != input.Original.Envelope.Digest() {
		t.Fatalf("ambiguous replay retry = %+v, %v", retry, err)
	}
}

// TestTodo_SUB_006_Security proves replay rechecks current authorization: a
// paused, revoked or de-scoped subscription is denied without effect, and
// rejections never echo digests or payloads.
func TestTodo_SUB_006_Security(t *testing.T) {
	provider := &deliveryProvider{}
	journal := NewDeliveryJournal()
	input := replayInput(t, "sub-secure")

	paused, err := input.Subscription.Pause("op:ann")
	if err != nil {
		t.Fatal(err)
	}
	denied := input
	denied.Subscription = paused
	assertRejected(t, func() error { _, err := PlanReplay(denied); return err }(), "subscription_state")
	revoked, err := paused.Revoke("op:ann")
	if err != nil {
		t.Fatal(err)
	}
	denied.Subscription = revoked
	rejection := assertRejected(t, func() error { _, err := PlanReplay(denied); return err }(), "subscription_state")
	if rejection.State != string(StateRevoked) {
		t.Fatalf("revocation state = %q", rejection.State)
	}
	narrowed := replayInput(t, "sub-secure")
	narrowed.Grant.Fields[EventWorkerChanged] = []string{"worker.id"}
	assertRejected(t, func() error { _, err := PlanReplay(narrowed); return err }(), "authorization")
	foreign := replayInput(t, "sub-secure")
	foreign.Grant.TenantScope = "tenant-b"
	assertRejected(t, func() error { _, err := PlanReplay(foreign); return err }(), "authorization")
	impostor := replayInput(t, "sub-secure")
	impostor.Grant.PartnerRef = "partner:mallory"
	assertRejected(t, func() error { _, err := PlanReplay(impostor); return err }(), "authorization")
	wrong := replayInput(t, "sub-secure")
	wrong.Subscription = mustActivate(t, "sub-other")
	assertRejected(t, func() error { _, err := PlanReplay(wrong); return err }(), "subscription_id")
	scopeless := replayInput(t, "sub-secure")
	scopeless.Grant.Purpose = ""
	assertRejected(t, func() error { _, err := PlanReplay(scopeless); return err }(), "authorization")

	digestMutant := replayInput(t, "sub-secure")
	digestMutant.Candidate.PayloadDigest = "sha256:forged-history"
	err = func() error { _, err := PlanReplay(digestMutant); return err }()
	if err == nil || strings.Contains(err.Error(), "sha256:forged-history") {
		t.Fatalf("rejection echoed the forged digest: %v", err)
	}
	_ = provider
	_ = journal
	if len(provider.attempts) != 0 || len(journal.Operations("sub-secure")) != 0 {
		t.Fatal("denied replays reached the provider or journal")
	}
}

// TestTodo_SUB_006_Recovery proves a redelivered lineage stays effectively
// once: replaying an acknowledged lineage is a duplicate, never a new event,
// and a new epoch marker never forks the lineage.
func TestTodo_SUB_006_Recovery(t *testing.T) {
	journal := NewDeliveryJournal()
	provider := &deliveryProvider{}
	input := replayInput(t, "sub-recovery")
	first, err := PlanReplay(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.DeliverNow(provider, first.Delivery); err != nil {
		t.Fatal(err)
	}
	duplicate, err := PlanReplay(input)
	if err != nil {
		t.Fatal(err)
	}
	redelivery, err := journal.DeliverNow(provider, duplicate.Delivery)
	if err != nil || !redelivery.Duplicate || len(provider.attempts) != 1 {
		t.Fatalf("duplicate replay = %+v attempts=%d, %v", redelivery, len(provider.attempts), err)
	}
	advanced := input
	advanced.Epoch = 2
	advanced.RequestedBy = "op:bo"
	evolved, err := PlanReplay(advanced)
	if err != nil {
		t.Fatal(err)
	}
	if evolved.Delivery.IdempotencyKey != first.Delivery.IdempotencyKey {
		t.Fatal("a new epoch forked the delivery lineage")
	}
	if operations := journal.Operations("sub-recovery"); len(operations) != 1 {
		t.Fatalf("recovery forked lineages: %d", len(operations))
	}
}

// TestTodo_SUB_006_Mutation proves every single-field identity mutant is
// killed while non-identity mutants (epoch, requester) survive on the same
// lineage.
func TestTodo_SUB_006_Mutation(t *testing.T) {
	input := replayInput(t, "sub-mutation")
	killed := 0
	for field, mutate := range map[string]func(*CanonicalEnvelope){
		"sequence":       func(e *CanonicalEnvelope) { e.Sequence++ },
		"event_kind":     func(e *CanonicalEnvelope) { e.Kind = EventApplicationRevoked },
		"schema_version": func(e *CanonicalEnvelope) { e.SchemaVersion++ },
		"tenant_scope":   func(e *CanonicalEnvelope) { e.Tenant += "-x" },
		"payload_digest": func(e *CanonicalEnvelope) { e.PayloadDigest += "x" },
		"provenance_ref": func(e *CanonicalEnvelope) { e.ProvenanceRef += "x" },
		"subject_refs":   func(e *CanonicalEnvelope) { e.SubjectRefs = append(e.SubjectRefs, "worker:9") },
	} {
		mutant := input
		mutate(&mutant.Candidate)
		assertRejected(t, func() error { _, err := PlanReplay(mutant); return err }(), field)
		killed++
	}
	if killed != 7 {
		t.Fatalf("killed %d mutants", killed)
	}
	for _, mutate := range []func(*ReplayRequest){
		func(r *ReplayRequest) { r.Epoch = 9 },
		func(r *ReplayRequest) { r.RequestedBy = "op:bo" },
	} {
		survivor := input
		mutate(&survivor)
		replay, err := PlanReplay(survivor)
		if err != nil {
			t.Fatalf("non-identity mutant rejected: %v", err)
		}
		original, err := PlanReplay(input)
		if err != nil {
			t.Fatal(err)
		}
		if replay.Delivery.IdempotencyKey != original.Delivery.IdempotencyKey {
			t.Fatal("a non-identity mutant forked the lineage")
		}
	}
}

func mustActivate(t *testing.T, id string) EventSubscription {
	t.Helper()
	draft, err := NewDraft(subscriptionRequest(id, "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := draft.Activate("requester-2", "approver-1")
	if err != nil {
		t.Fatal(err)
	}
	return active
}
