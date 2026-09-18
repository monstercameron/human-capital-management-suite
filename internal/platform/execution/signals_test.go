package execution

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepsignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

const fixtureSignalNode = "signal_ack_received"

var signalAdapterAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func signalFixturePlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	def, err := workflow.LoadFile(filepath.Join("..", "..", "workflow", "testdata", "wait_signal_fixture.json"))
	if err != nil {
		t.Fatalf("load the WAIT/SIGNAL fixture: %v", err)
	}
	plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatalf("compile the WAIT/SIGNAL fixture: %v", err)
	}
	return plan
}

func signalFixtureRequest(t *testing.T, tenantID, instanceID uuid.UUID) execute.SignalSubscriptionRequest {
	t.Helper()
	plan := signalFixturePlan(t)
	node, ok := plan.Node(fixtureSignalNode)
	if !ok {
		t.Fatalf("fixture has no node %s", fixtureSignalNode)
	}
	return execute.SignalSubscriptionRequest{
		Continuation: runtime.ContinuationRecord{
			TenantID: tenantID, InstanceID: instanceID, SourceNodeID: "wait_for_effective_date", SourceAttempt: 1,
			TargetNodeID: fixtureSignalNode, Kind: frontier.IntentSignalSubscriptionRequired, TargetAttempt: 1,
			RecordedAt: signalAdapterAt,
			Causal: &runtime.CausalMetadata{CorrelationID: "corr:1", CausationID: "cause:1", LogicalOperationID: "op:1", AttemptID: "attempt:1",
				TraceLink: &runtime.TraceLinkMetadata{TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef", TraceFlags: 1}},
		},
		Plan: plan, Node: node, CorrelationID: "corr:1", CreatedAt: signalAdapterAt,
		Proposal:    runtime.ProposalBinding{Revision: intent.ProposalRevision{IntentID: "intent:1", ProposalRevisionID: "proposal:1"}},
		SubjectRefs: []string{"employment:jane", "worker:jane"},
	}
}

func TestDefaultCorrelation_ResolvesOnlyTheClosedVocabulary(t *testing.T) {
	req := signalFixtureRequest(t, uuid.New(), uuid.New())
	signal := *req.Node.Signal
	for expression, want := range map[string]string{
		"workflow.correlation_id": "corr:1",
		"workflow.instance_id":    req.Continuation.InstanceID.String(),
		"proposal.intent_id":      "intent:1",
		"proposal.revision_id":    "proposal:1",
		"subject:employment":      "employment:jane",
	} {
		s := signal
		s.CorrelationKeyExpression = expression
		req.Node.Signal = &s
		got, err := DefaultCorrelation(req)
		if err != nil || got != want {
			t.Fatalf("%s = %q, %v; want %q", expression, got, err, want)
		}
	}
	for name, expression := range map[string]string{
		"a payload-style path":   "worker_id",
		"an absent subject kind": "subject:position",
		"an empty subject kind":  "subject:",
	} {
		s := signal
		s.CorrelationKeyExpression = expression
		req.Node.Signal = &s
		if _, err := DefaultCorrelation(req); !errors.Is(err, ErrUnresolvedCorrelation) {
			t.Fatalf("%s: err = %v, want ErrUnresolvedCorrelation", name, err)
		}
	}
	s := signal
	s.CorrelationKeyExpression = "subject:employment"
	req.Node.Signal = &s
	req.SubjectRefs = []string{"employment:jane", "employment:john"}
	if _, err := DefaultCorrelation(req); !errors.Is(err, ErrUnresolvedCorrelation) {
		t.Fatalf("an ambiguous subject: err = %v, want ErrUnresolvedCorrelation", err)
	}
	req.Node.Signal = nil
	if _, err := DefaultCorrelation(req); !errors.Is(err, ErrUnresolvedCorrelation) {
		t.Fatalf("no signal binding: err = %v, want ErrUnresolvedCorrelation", err)
	}
}

func TestCreateSubscription_RefusesBeforeWritingAnything(t *testing.T) {
	req := signalFixtureRequest(t, uuid.New(), uuid.New())
	adapter := SignalSubscriptions{}
	if _, err := adapter.CreateSubscription(context.Background(), nil, req); !errors.Is(err, ErrUnresolvedCorrelation) {
		t.Fatalf("an unresolvable correlation: err = %v, want ErrUnresolvedCorrelation", err)
	}
	wait, _ := req.Plan.Node("wait_for_effective_date")
	req.Node = wait
	if _, err := adapter.CreateSubscription(context.Background(), nil, req); err == nil {
		t.Fatal("a WAIT node was subscribed as a SIGNAL")
	}
	if stateCausal(nil) != nil || runtimeCausal(nil) != nil {
		t.Fatal("a nil causal identity was invented")
	}
}

func TestSignalSubscriptions_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID, instanceID := uuid.New(), uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'signal adapter', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenantID, "signal-adapter-"+tenantID.String())
	db.Exec(t, `INSERT INTO workflow_instance
		(tenant_id, instance_id, cell_id, workflow_id, workflow_version, compiled_plan_hash,
		 business_subject_refs, execution_mode, runtime_status, completion_dimensions, input_ref,
		 variable_revision_head, current_node_ids, correlation_id, created_at)
		VALUES ($1, $2, 'cell-local', 'fixture', 1, repeat('a', 64), '{}', 'EXECUTE', 'RUNNING', '{}',
		        'input:one', 0, '{signal_ack_received}', 'corr:1', $3)`, tenantID, instanceID, signalAdapterAt)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	inTx := func(fn func(tx dbport.Tx) error) {
		t.Helper()
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			t.Fatal(err)
		}
		if err := fn(tx); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	adapter := SignalSubscriptions{Correlate: func(req execute.SignalSubscriptionRequest) (string, error) {
		return "worker:jane", nil
	}}
	req := signalFixtureRequest(t, tenantID, instanceID)
	inTx(func(tx dbport.Tx) error {
		first, err := adapter.CreateSubscription(ctx, tx, req)
		if err != nil {
			return err
		}
		again, err := adapter.CreateSubscription(ctx, tx, req)
		if err != nil {
			return err
		}
		want := signals.SubscriptionIDFor(tenantID, instanceID, fixtureSignalNode, 1)
		if first.SubscriptionID != want || first.Replay || !again.Replay || again.SubscriptionID != want || first.Attempt != 1 {
			t.Fatalf("subscribe = %+v then %+v; want one deterministic subscription, replayed the second time", first, again)
		}
		if _, err := adapter.LoadMatchedSignal(ctx, tx, execute.MatchedSignalQuery{TenantID: tenantID, SignalID: uuid.New(), SubscriptionID: want}); !errors.Is(err, execute.ErrSignalDrift) {
			t.Fatalf("an unmatched receipt: err = %v, want ErrSignalDrift", err)
		}
		if _, err := adapter.LoadMatchedSignal(ctx, tx, execute.MatchedSignalQuery{}); err == nil || errors.Is(err, execute.ErrSignalDrift) {
			t.Fatalf("an incomplete query: err = %v, want a plain refusal", err)
		}
		return nil
	})
	var closesAt time.Time
	var correlationValue, expected, causation string
	if err := db.QueryRow(ctx, `SELECT expires_at, correlation_value, expected_schema_ref, causation_id FROM workflow_signal_subscription WHERE instance_id = $1`, instanceID).
		Scan(&closesAt, &correlationValue, &expected, &causation); err != nil {
		t.Fatal(err)
	}
	if !closesAt.Equal(signalAdapterAt.Add(24*time.Hour)) || correlationValue != "worker:jane" || expected != "PromotionAckPayload/v1/v1" || causation != "cause:1" {
		t.Fatalf("stored subscription = closes %s, value %q, schema %q, causation %q", closesAt, correlationValue, expected, causation)
	}

	var accepted signals.Receipt
	inTx(func(tx dbport.Tx) error {
		var err error
		accepted, err = (signals.Store{}).Receive(ctx, tx, signals.ReceiveRequest{ReceivedAt: signalAdapterAt.Add(time.Hour), Signal: signalFor(tenantID)}, acceptAll{})
		return err
	})
	inTx(func(tx dbport.Tx) error {
		row, err := adapter.LoadMatchedSignal(ctx, tx, execute.MatchedSignalQuery{
			TenantID: tenantID, SignalID: accepted.SignalID,
			SubscriptionID: signals.SubscriptionIDFor(tenantID, instanceID, fixtureSignalNode, 1),
		})
		if err != nil {
			return err
		}
		if !row.Settled || row.NodeID != fixtureSignalNode || row.InstanceID != instanceID ||
			row.ContinuationRef != signals.ContinuationRef(accepted.SignalID) || row.Causal == nil || row.Causal.TraceLink == nil {
			t.Fatalf("matched signal = %+v, want a settled reference with the subscription's causal identity", row)
		}
		return nil
	})
}

// TestSignalSubscriptions_ExpiryIntegration sweeps a due wait through the
// production adapter: ExpireDue marks it EXPIRED with its timeout
// continuation, and LoadExpiredSubscription hands the driver a settled
// expiry whose reference is the durable timeout namespace. An OPEN wait and
// an unknown subscription are both ErrSignalDrift: neither is evidence a
// node may time out on.
func TestSignalSubscriptions_ExpiryIntegration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'signal expiry adapter', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenantID, "signal-expiry-adapter-"+tenantID.String())
	newRunningInstance := func(key string) uuid.UUID {
		t.Helper()
		instanceID := uuid.New()
		db.Exec(t, `INSERT INTO workflow_instance
			(tenant_id, instance_id, cell_id, workflow_id, workflow_version, compiled_plan_hash,
			 business_subject_refs, execution_mode, runtime_status, completion_dimensions, input_ref,
			 variable_revision_head, current_node_ids, correlation_id, created_at)
			VALUES ($1, $2, 'cell-local', 'fixture', 1, repeat('a', 64), '{}', 'EXECUTE', 'WAITING', '{}',
			        'input:one', 0, '{signal_ack_received}', $3, $4)`, tenantID, instanceID, key, signalAdapterAt)
		return instanceID
	}
	dueInstance, openInstance := newRunningInstance("corr:due"), newRunningInstance("corr:open")
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	inTx := func(fn func(tx dbport.Tx) error) {
		t.Helper()
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			t.Fatal(err)
		}
		if err := fn(tx); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	adapter := SignalSubscriptions{Correlate: func(req execute.SignalSubscriptionRequest) (string, error) {
		return "worker:jane", nil
	}}
	dueSub := signals.SubscriptionIDFor(tenantID, dueInstance, fixtureSignalNode, 1)
	openSub := signals.SubscriptionIDFor(tenantID, openInstance, fixtureSignalNode, 1)
	inTx(func(tx dbport.Tx) error {
		if _, err := adapter.CreateSubscription(ctx, tx, signalFixtureRequest(t, tenantID, dueInstance)); err != nil {
			return err
		}
		// The second wait parks two days later, so its close is still in
		// the future when the sweep runs: exactly one wait is due.
		later := signalFixtureRequest(t, tenantID, openInstance)
		later.CreatedAt = signalAdapterAt.Add(48 * time.Hour)
		_, err := adapter.CreateSubscription(ctx, tx, later)
		return err
	})
	inTx(func(tx dbport.Tx) error {
		expired, err := (signals.Store{}).ExpireDue(ctx, tx, tenantID, signalAdapterAt.Add(25*time.Hour), 8)
		if err != nil {
			return err
		}
		if len(expired) != 1 || expired[0].SubscriptionID != dueSub {
			t.Fatalf("expired = %+v, want exactly the due wait", expired)
		}
		return nil
	})
	inTx(func(tx dbport.Tx) error {
		row, err := adapter.LoadExpiredSubscription(ctx, tx, execute.ExpiredSubscriptionQuery{TenantID: tenantID, SubscriptionID: dueSub})
		if err != nil {
			return err
		}
		if !row.Settled || row.InstanceID != dueInstance || row.NodeID != fixtureSignalNode ||
			row.ContinuationRef != signals.ExpiryContinuationRef(dueSub) {
			t.Fatalf("expired subscription = %+v, want a settled timeout reference for the due wait", row)
		}
		if _, err := adapter.LoadExpiredSubscription(ctx, tx, execute.ExpiredSubscriptionQuery{TenantID: tenantID, SubscriptionID: openSub}); !errors.Is(err, execute.ErrSignalDrift) {
			t.Fatalf("an OPEN wait: err = %v, want ErrSignalDrift", err)
		}
		if _, err := adapter.LoadExpiredSubscription(ctx, tx, execute.ExpiredSubscriptionQuery{TenantID: tenantID, SubscriptionID: uuid.New()}); !errors.Is(err, execute.ErrSignalDrift) {
			t.Fatalf("an unknown subscription: err = %v, want ErrSignalDrift", err)
		}
		return nil
	})
}

type acceptAll struct{}

func (acceptAll) Verify(stepsignal.Signal) error { return nil }

func signalFor(tenantID uuid.UUID) stepsignal.Signal {
	at := signalAdapterAt.Add(time.Hour)
	return stepsignal.Signal{
		Tenant: values.TenantId(tenantID.String()), Source: "hcmnext.integrations.hris",
		EventType: "hcmnext.events.promotion_ack", SchemaRef: "PromotionAckPayload/v1/v1",
		CorrelationKey: "worker_id", CorrelationValue: "worker:jane", IdempotencyKey: "ack-1",
		Payload: []byte(`{"acknowledged":true}`), ReceivedAt: values.NewInstant(at),
	}
}

func TestExecuteDriverAdapterResumeSignal_PropagatesTheDriverRefusal(t *testing.T) {
	driver, err := execute.New(execute.Options{DB: stubBeginner{}, Steps: promotionStepRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	var executor app.SignalResumeExecutor = executeDriverAdapter{driver: driver}
	if _, err := executor.ResumeSignal(context.Background(), app.ExecutionSignalResumeRequest{}); !errors.Is(err, execute.ErrInvalidConfiguration) {
		t.Fatalf("resume with no SignalReader: err = %v, want ErrInvalidConfiguration", err)
	}
}
