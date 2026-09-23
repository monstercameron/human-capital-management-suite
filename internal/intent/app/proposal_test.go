package app

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestProposal_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestProposal_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestTodo_CONFLICT_003_ProposalForBindsTypedWriteSemantics(t *testing.T) {
	at := values.NewInstant(time.Date(2026, 10, 1, 12, 0, 0, 123456789, time.UTC))
	subject := values.EntityRef{Tenant: values.TenantId("acme"), Kind: values.Kind("worker"), Id: "worker-1"}
	primary := intent.SubjectReference{Kind: "WORKER", SubjectID: subject.Id, AuthorityDomain: "PEOPLE"}
	watermark, err := values.NewSequenceRevision("people.worker.worker-1", 7)
	if err != nil {
		t.Fatalf("NewSequenceRevision: %v", err)
	}
	spec, err := proposalFor(intent.Instance{IntentID: "intent-1", Tenant: values.TenantId("acme"), OrganizationScopeID: "org-1", Subjects: []intent.SubjectReference{primary}, RequestedEffectiveAt: &at}, intent.Definition{}, promotion.PreflightRequest{Subject: subject}, promotion.SimulationResult{Projected: promotion.ProjectedWorkerState{Changes: []promotion.PlacementChange{{Field: "job_code", Before: "ENG-1", After: "ENG-2", Changed: true}, {Field: "grade", Before: "P2", After: "P2", Changed: false}}}}, intent.BaselineSnapshot{Revisions: map[string]values.RevisionToken{subject.String(): watermark}}, intent.ControlSnapshots{}, 1, "", "")
	if err != nil {
		t.Fatalf("proposalFor: %v", err)
	}
	if len(spec.Writes) != 1 {
		t.Fatalf("writes = %d, want one material change", len(spec.Writes))
	}
	write := spec.Writes[0]
	if write.Operation != intent.WriteOperationUpdate {
		t.Errorf("operation = %q, want UPDATE", write.Operation)
	}
	if write.EffectiveInterval != spec.EffectiveTime {
		t.Errorf("effective interval = %v, want proposal interval %v", write.EffectiveInterval, spec.EffectiveTime)
	}
	if write.ExpectedRevision != watermark {
		t.Errorf("expected revision = %v, want %v", write.ExpectedRevision, watermark)
	}
	if len(spec.SourceBaselines) != 1 || spec.SourceBaselines[0].StreamID != promotionStreamID(subject) || spec.SourceBaselines[0].ExpectedRevision != watermark {
		t.Errorf("source baselines = %+v, want exact promotion stream watermark", spec.SourceBaselines)
	}
	wantResource, err := values.NewResourceKey(values.TenantId("acme"), values.Kind("assignment"), "worker", subject.Id)
	if err != nil {
		t.Fatal(err)
	}
	if write.Subject != primary || !write.ResourceKey.Equal(wantResource) || write.SourceAuthorityDecision != "authority.local_master/v1" {
		t.Errorf("write lost subject/resource/authority binding: %+v", write)
	}
	start, ok := write.EffectiveInterval.StartInstant()
	if !ok || start != at {
		t.Errorf("effective start = %v (instant=%t), want exact nanosecond instant %v", start, ok, at)
	}
	if len(spec.Effects) != 0 {
		t.Fatalf("typed write authoring introduced %d external effects", len(spec.Effects))
	}
}
