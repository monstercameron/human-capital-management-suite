package intent

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

func intent022Request() OperatorActionRequest {
	expiry, err := values.NewInstantFromUnix(1760003600, 0)
	if err != nil {
		panic(err)
	}
	return OperatorActionRequest{
		IntentInstanceID: "intent-op-1", Operation: OperationConnectorRedrive,
		Tenant: "harborcare-demo", Target: "connector-incumbent",
		IdempotencyKey: "op-1", Simulated: true, SimulationRef: "sim-1",
		JITGrant: "jit-grant-1", JITExpires: expiry, Approvers: []string{"operator-1"},
	}
}

// TestTodo_INTENT_022_Race: authorization is pure, so concurrent verdicts
// over a shared request stay race-free and agree.
func TestTodo_INTENT_022_Race(t *testing.T) {
	at := mustInstant(t, 1760000000)
	req := intent022Request()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				rec, err := AuthorizeOperatorAction(req, at)
				if err != nil {
					t.Errorf("AuthorizeOperatorAction: %v", err)
					return
				}
				if rec.Digest == "" {
					t.Error("empty digest")
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestTodo_INTENT_022_Integration: the authorized receipt reserves exactly
// one idempotency slot, so a retried action replays instead of
// re-executing.
func TestTodo_INTENT_022_Integration(t *testing.T) {
	at := mustInstant(t, 1760000000)
	now := time.Unix(1760000000, 0).UTC()
	rec, err := AuthorizeOperatorAction(intent022Request(), at)
	if err != nil {
		t.Fatalf("AuthorizeOperatorAction: %v", err)
	}
	registry := idempotency.NewRegistry()
	identity := idempotency.Identity{Tenant: string(rec.Tenant), Capability: "operator-action", EffectScope: rec.Target, Key: rec.IdempotencyKey}
	reserve := func() idempotency.Resolution {
		res, err := registry.Reserve(idempotency.Request{
			Identity: identity, Layer: "operator",
			Canonical: []byte(rec.Digest),
			Retention: idempotency.RetentionPolicy{ExpiresAt: now.Add(time.Hour), Mode: idempotency.RejectReuse},
			Now:       now,
		})
		if err != nil {
			t.Fatalf("Reserve: %v", err)
		}
		return res
	}
	first := reserve()
	if first.Decision != idempotency.Reserved {
		t.Fatalf("decision=%q, want RESERVED", first.Decision)
	}
	if _, err := registry.Complete(identity, first.Record.RequestDigest, "result-1", "effect-1", now); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	second := reserve()
	if second.Decision != idempotency.Replay {
		t.Fatalf("decision=%q, want REPLAY instead of a second effect", second.Decision)
	}
}

// TestTodo_INTENT_022_Fault: every side door fails closed with a typed
// refusal.
func TestTodo_INTENT_022_Fault(t *testing.T) {
	at := mustInstant(t, 1760000000)
	cases := map[string]func(*OperatorActionRequest){
		"no intent":     func(r *OperatorActionRequest) { r.IntentInstanceID = "" },
		"unknown op":    func(r *OperatorActionRequest) { r.Operation = "BACKUP_RESTORE" },
		"no tenant":     func(r *OperatorActionRequest) { r.Tenant = "" },
		"no target":     func(r *OperatorActionRequest) { r.Target = " " },
		"no idemkey":    func(r *OperatorActionRequest) { r.IdempotencyKey = "" },
		"no grant":      func(r *OperatorActionRequest) { r.JITGrant = "" },
		"no simulation": func(r *OperatorActionRequest) { r.Simulated = false },
		"blank sim ref": func(r *OperatorActionRequest) { r.SimulationRef = "  " },
		"blank emergency reason": func(r *OperatorActionRequest) {
			r.Simulated = false
			expiry, err := values.NewInstantFromUnix(1760086400, 0)
			if err != nil {
				panic(err)
			}
			r.Emergency = &EmergencyBypass{Reason: " ", ReviewBy: expiry}
		},
	}
	for name, mutate := range cases {
		req := intent022Request()
		mutate(&req)
		if _, err := AuthorizeOperatorAction(req, at); !errors.Is(err, ErrOperatorAction) {
			t.Fatalf("%s: err=%v, want ErrOperatorAction", name, err)
		}
	}
	if _, err := AuthorizeOperatorAction(intent022Request(), values.Instant{}); !errors.Is(err, ErrOperatorAction) {
		t.Fatalf("zero instant: %v", err)
	}
	// Solo approver listed twice is still one approver.
	dupe := intent022Request()
	dupe.Operation = OperationFailover
	dupe.Approvers = []string{"operator-1", "operator-1", " "}
	if _, err := AuthorizeOperatorAction(dupe, at); !errors.Is(err, ErrOperatorAction) {
		t.Fatal("duplicated approver satisfied dual control")
	}
}

// TestTodo_INTENT_022_Security: authority binds tenant, target and grant;
// receipts from different scopes never collide, and emergencies stay
// declared.
func TestTodo_INTENT_022_Security(t *testing.T) {
	at := mustInstant(t, 1760000000)
	a, err := AuthorizeOperatorAction(intent022Request(), at)
	if err != nil {
		t.Fatalf("AuthorizeOperatorAction: %v", err)
	}
	other := intent022Request()
	other.Tenant = "other-tenant"
	b, err := AuthorizeOperatorAction(other, at)
	if err != nil {
		t.Fatalf("AuthorizeOperatorAction: %v", err)
	}
	if a.Digest == b.Digest {
		t.Fatal("cross-tenant receipts share a digest")
	}
	moved := intent022Request()
	moved.Target = "connector-other"
	c, err := AuthorizeOperatorAction(moved, at)
	if err != nil {
		t.Fatalf("AuthorizeOperatorAction: %v", err)
	}
	if a.Digest == c.Digest {
		t.Fatal("cross-target receipts share a digest")
	}
	// An emergency without a bypass record is routine work without a
	// simulation: refused either way.
	silent := intent022Request()
	silent.Operation = OperationBreakGlass
	silent.Simulated = false
	silent.Approvers = []string{"operator-1", "operator-2"}
	if _, err := AuthorizeOperatorAction(silent, at); !errors.Is(err, ErrOperatorAction) {
		t.Fatal("undeclared emergency authorized")
	}
}

// TestTodo_INTENT_022_Recovery: emergency receipts carry their mandatory
// review; routine work can never borrow the emergency path.
func TestTodo_INTENT_022_Recovery(t *testing.T) {
	at := mustInstant(t, 1760000000)
	reviewBy := mustInstant(t, 1760086400)
	req := intent022Request()
	req.Operation = OperationFailover
	req.Approvers = []string{"operator-1", "operator-2"}
	req.Simulated = false
	req.Emergency = &EmergencyBypass{Reason: "primary cell dark, fencing restore", ReviewBy: reviewBy}
	rec, err := AuthorizeOperatorAction(req, at)
	if err != nil {
		t.Fatalf("AuthorizeOperatorAction: %v", err)
	}
	if !rec.Emergency || rec.ReviewBy.Compare(reviewBy) != 0 {
		t.Fatalf("receipt=%+v, want declared emergency with review deadline", rec)
	}
	past := req
	past.Emergency = &EmergencyBypass{Reason: "late review", ReviewBy: mustInstant(t, 1759990000)}
	if _, err := AuthorizeOperatorAction(past, at); !errors.Is(err, ErrOperatorAction) {
		t.Fatal("emergency with a past review authorized")
	}
	routine := intent022Request()
	routine.Emergency = &EmergencyBypass{Reason: "shortcut", ReviewBy: reviewBy}
	rec, err = AuthorizeOperatorAction(routine, at)
	if err != nil {
		t.Fatalf("AuthorizeOperatorAction: %v", err)
	}
	if !rec.Emergency {
		t.Fatal("declared bypass must stay declared on the receipt")
	}
}

// TestTodo_INTENT_022_Mutation: grant, review and approver edges resolve
// on the documented side.
func TestTodo_INTENT_022_Mutation(t *testing.T) {
	at := mustInstant(t, 1760000000)
	// Grant expiring exactly at the instant is already lapsed.
	edge := intent022Request()
	edge.JITExpires = at
	if _, err := AuthorizeOperatorAction(edge, at); !errors.Is(err, ErrOperatorAction) {
		t.Fatal("grant expiring at the instant authorized")
	}
	// A non-dual operation passes with one approver; dual needs two.
	single := intent022Request()
	single.Approvers = nil
	if _, err := AuthorizeOperatorAction(single, at); err != nil {
		t.Fatalf("single approver on redrive refused: %v", err)
	}
	dual := single
	dual.Operation = OperationKeyRotation
	if _, err := AuthorizeOperatorAction(dual, at); !errors.Is(err, ErrOperatorAction) {
		t.Fatal("solo key rotation authorized")
	}
	dual.Approvers = []string{"operator-1", "operator-2"}
	if _, err := AuthorizeOperatorAction(dual, at); err != nil {
		t.Fatalf("dual key rotation refused: %v", err)
	}
	// Review exactly at the instant is not a future review.
	review := intent022Request()
	review.Operation = OperationBreakGlass
	review.Simulated = false
	review.Approvers = []string{"operator-1", "operator-2"}
	review.Emergency = &EmergencyBypass{Reason: "edge", ReviewBy: at}
	if _, err := AuthorizeOperatorAction(review, at); !errors.Is(err, ErrOperatorAction) {
		t.Fatal("review due at the instant authorized")
	}
}
