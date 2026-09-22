package execution

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

func downstreamTestConsumer(t *testing.T) *DownstreamSyncConsumer {
	t.Helper()
	consumer, err := NewDownstreamSyncConsumer(outbox.GroupPolicy{
		Group:       "promotion-downstream",
		MaxAttempts: 3,
		PoisonOwner: "promotion-owners",
		PoisonTTL:   time.Hour,
		RepairRoute: "promotion/repair",
	}, outbox.NewMemoryCheckpointStore())
	if err != nil {
		t.Fatalf("NewDownstreamSyncConsumer: %v", err)
	}
	return consumer
}

func downstreamLeg(tenant uuid.UUID, effect, ordering string) outbox.Record {
	return outbox.Record{
		Tenant:         tenant,
		OutboxID:       uuid.New(),
		EffectIdentity: effect,
		OrderingKey:    ordering,
		Criticality:    "P1",
		SchemaRef:      "hcmnext.payroll/v1",
		Payload:        []byte("leg:" + effect),
	}
}

func downstreamConsume(t *testing.T, consumer *DownstreamSyncConsumer, rec outbox.Record) outbox.ApplyOutcome {
	t.Helper()
	recorder := &observetest.Recorder{}
	outcome, err := consumer.Consume(recorder.Context(context.Background()), rec)
	if err != nil {
		t.Fatalf("Consume %s: %v", rec.EffectIdentity, err)
	}
	ops := recorder.Named("workflow.downstream.consume")
	if len(ops) != 1 || ops[0].Ended != 1 || ops[0].Outcome != observe.OutcomeSuccess || ops[0].Attrs[observe.KeyTenant] != rec.Tenant.String() {
		t.Fatalf("downstream telemetry: %+v", ops)
	}
	return outcome
}

// TestDownstreamSyncConsumerHandlesCompensation proves the served
// promotion's downstream legs (WF-REV-013, execution plane): a payroll
// compensation delivered twice before its leg waits, then reverses it
// exactly once when the leg arrives, while the IAM leg stays applied and
// unreversed; redeliveries change nothing.
func TestDownstreamSyncConsumerHandlesCompensation(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	consumer := downstreamTestConsumer(t)
	proposal := "proposal-rev013"
	payroll, iam := "payroll:"+proposal, "iam:"+proposal
	ordering := "downstream:" + proposal

	if outcome := downstreamConsume(t, consumer, downstreamLeg(tenant, "reversal:"+payroll, ordering)); outcome != outbox.ApplyCompensationHeld {
		t.Fatalf("early payroll compensation = %s, want COMPENSATION_HELD", outcome)
	}
	if outcome := downstreamConsume(t, consumer, downstreamLeg(tenant, "reversal:"+payroll, ordering)); outcome != outbox.ApplyCompensationHeld {
		t.Fatalf("duplicate early payroll compensation = %s, want COMPENSATION_HELD", outcome)
	}
	if held := consumer.Held(tenant, payroll); len(held) != 1 {
		t.Fatalf("held = %+v, want the one payroll compensation", held)
	}
	if _, reversed := consumer.Reversed(payroll); reversed {
		t.Fatal("payroll reversed before its leg applied")
	}

	if outcome := downstreamConsume(t, consumer, downstreamLeg(tenant, payroll, ordering)); outcome != outbox.ApplyApplied {
		t.Fatalf("payroll leg = %s, want APPLIED", outcome)
	}
	if outcome := downstreamConsume(t, consumer, downstreamLeg(tenant, iam, ordering)); outcome != outbox.ApplyApplied {
		t.Fatalf("iam leg = %s, want APPLIED", outcome)
	}
	compensation, reversed := consumer.Reversed(payroll)
	if !reversed || compensation != "reversal:"+payroll {
		t.Fatalf("payroll reversal = %q, %v; want the held compensation applied once the leg arrived", compensation, reversed)
	}
	if _, reversed := consumer.Reversed(iam); reversed {
		t.Fatal("iam leg reversed without a compensation")
	}
	for _, leg := range []string{payroll, iam} {
		if _, ok := consumer.Applied(leg); !ok {
			t.Fatalf("leg %s not recorded as applied", leg)
		}
	}
	if _, ok := consumer.Applied("payroll:unknown"); ok {
		t.Fatal("unknown leg reported as applied")
	}
	wantSequence := []string{"apply:" + payroll, "reverse:" + payroll, "apply:" + iam}
	if sequence := consumer.Sequence(); len(sequence) != len(wantSequence) {
		t.Fatalf("sequence = %v, want %v", sequence, wantSequence)
	} else {
		for i := range wantSequence {
			if sequence[i] != wantSequence[i] {
				t.Fatalf("sequence = %v, want %v", sequence, wantSequence)
			}
		}
	}
	if held := consumer.Held(tenant, payroll); len(held) != 0 {
		t.Fatalf("held after drain = %+v, want empty", held)
	}

	for _, rec := range []outbox.Record{
		downstreamLeg(tenant, payroll, ordering),
		downstreamLeg(tenant, iam, ordering),
		downstreamLeg(tenant, "reversal:"+payroll, ordering),
	} {
		if outcome := downstreamConsume(t, consumer, rec); outcome != outbox.ApplyDuplicateFenced {
			t.Fatalf("redelivery of %s = %s, want DUPLICATE_FENCED", rec.EffectIdentity, outcome)
		}
	}
	if sequence := consumer.Sequence(); len(sequence) != len(wantSequence) {
		t.Fatalf("sequence after redelivery = %v, want no second application", sequence)
	}
	if err := consumer.Group().CommitCheckpoint(ctx, ordering, tenant, "reversal:"+payroll); err != nil {
		t.Fatalf("CommitCheckpoint for the applied compensation: %v", err)
	}

	// The projection fails closed: reversals without a leg, second
	// applications and fenced runs are errors, never silent effects.
	fresh := downstreamTestConsumer(t)
	orphan := downstreamLeg(tenant, "reversal:payroll:unknown", ordering)
	if err := fresh.handle(ctx, orphan, outbox.Fencing{AllowExternalEffects: true}); err == nil {
		t.Fatal("reversal without an applied leg accepted")
	}
	leg := downstreamLeg(tenant, payroll, ordering)
	if err := fresh.handle(ctx, leg, outbox.Fencing{AllowExternalEffects: true}); err != nil {
		t.Fatalf("first application: %v", err)
	}
	if err := fresh.handle(ctx, leg, outbox.Fencing{AllowExternalEffects: true}); err == nil {
		t.Fatal("second application of one leg accepted")
	}
	reversal := downstreamLeg(tenant, "reversal:"+payroll, ordering)
	if err := fresh.handle(ctx, reversal, outbox.Fencing{AllowExternalEffects: true}); err != nil {
		t.Fatalf("first reversal: %v", err)
	}
	if err := fresh.handle(ctx, reversal, outbox.Fencing{AllowExternalEffects: true}); err == nil {
		t.Fatal("second reversal of one leg accepted")
	}
	if err := fresh.handle(ctx, leg, outbox.Fencing{}); err == nil {
		t.Fatal("fenced run accepted")
	}
	if _, err := NewDownstreamSyncConsumer(outbox.GroupPolicy{}, outbox.NewMemoryCheckpointStore()); err == nil {
		t.Fatal("empty group policy accepted")
	}
	if _, err := NewDownstreamSyncConsumer(outbox.GroupPolicy{
		Group: "promotion-downstream", MaxAttempts: 1,
		PoisonOwner: "o", PoisonTTL: time.Hour, RepairRoute: "r",
	}, nil); err == nil {
		t.Fatal("storeless downstream consumer accepted")
	}
}
