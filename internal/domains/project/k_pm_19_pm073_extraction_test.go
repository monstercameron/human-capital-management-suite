package project

import (
	"context"
	"errors"
	"testing"
)

var pm073DrainErr = errors.New("drain failed")

type pm073Extractor struct {
	manifest    ExtractionManifest
	phases      []ExtractionPhase
	fail        ExtractionPhase
	restore     RestoreEvidence
	restoreCall bool
}

func (e *pm073Extractor) Copy(_ context.Context, _ ExtractionPlan) (ExtractionManifest, error) {
	e.phases = append(e.phases, ExtractionCopy)
	if e.fail == ExtractionCopy {
		return ExtractionManifest{}, errors.New("copy failed")
	}
	return e.manifest, nil
}
func (e *pm073Extractor) Verify(_ context.Context, _ ExtractionPlan, _ ExtractionManifest) error {
	e.phases = append(e.phases, ExtractionVerify)
	if e.fail == ExtractionVerify {
		return errors.New("verify failed")
	}
	return nil
}
func (e *pm073Extractor) Fence(_ context.Context, _ ExtractionPlan) error {
	e.phases = append(e.phases, ExtractionFence)
	if e.fail == ExtractionFence {
		return errors.New("fence failed")
	}
	return nil
}
func (e *pm073Extractor) Cutover(_ context.Context, _ ExtractionPlan, _ ExtractionManifest) error {
	e.phases = append(e.phases, ExtractionCutover)
	if e.fail == ExtractionCutover {
		return errors.New("cutover failed")
	}
	return nil
}
func (e *pm073Extractor) Drain(_ context.Context, _ ExtractionPlan) error {
	e.phases = append(e.phases, ExtractionDrain)
	if e.fail == ExtractionDrain {
		return pm073DrainErr
	}
	return nil
}
func (e *pm073Extractor) Restore(_ context.Context, _ ExtractionPlan, _ ExtractionManifest, _ string) (RestoreEvidence, error) {
	e.restoreCall = true
	e.phases = append(e.phases, ExtractionRestored)
	return e.restore, nil
}

func pm073Plan() ExtractionPlan {
	snapshot := ExtractionSnapshot{
		TenantID: "tenant-a", ProjectID: "project-a", ProjectRevision: 12, EventCursor: 44,
		Tasks:       []TaskCheckpoint{{ID: "task-2", Revision: 9}, {ID: "task-1", Revision: 3}},
		Memberships: []MembershipCheckpoint{{UserID: "member-2", Revision: 5}, {UserID: "member-1", Revision: 7}},
	}
	return ExtractionPlan{Snapshot: snapshot, SourcePlacement: "core-postgres", TargetPlacement: "project-postgres", APIContractDigest: "sha256:project-api-v1", Pilot: PilotGate{MixedLoadWithinSLO: true, SeparateCapacityReady: true, RehearsalApproved: true}}
}

func TestTodo_PM_073(t *testing.T) {
	plan := pm073Plan()
	extractor := &pm073Extractor{manifest: ExtractionManifest{Snapshot: plan.Snapshot, APIContractDigest: plan.APIContractDigest}}
	result, err := RehearseExtraction(context.Background(), extractor, plan)
	if err != nil {
		t.Fatal(err)
	}
	want := []ExtractionPhase{ExtractionCopy, ExtractionVerify, ExtractionFence, ExtractionCutover, ExtractionDrain}
	if len(result.Phases) != len(want) || result.Cutover != true || result.Drained != true || !result.APIContractPreserved {
		t.Fatalf("rehearsal result = %s", result.String())
	}
	for i := range want {
		if result.Phases[i] != want[i] {
			t.Fatalf("phase %d = %q, want %q", i, result.Phases[i], want[i])
		}
	}
	if result.Manifest.Snapshot.EventCursor != 44 || len(result.Manifest.Snapshot.Tasks) != 2 || len(result.Manifest.Snapshot.Memberships) != 2 {
		t.Fatalf("manifest dropped project state: %+v", result.Manifest)
	}
	if extractor.restoreCall {
		t.Fatal("successful cutover requested restore")
	}
}

func TestTodo_PM_073_Recovery(t *testing.T) {
	plan := pm073Plan()
	extractor := &pm073Extractor{
		manifest: ExtractionManifest{Snapshot: plan.Snapshot, APIContractDigest: plan.APIContractDigest},
		fail:     ExtractionDrain,
		restore:  RestoreEvidence{OperationID: "restore-1", Source: plan.SourcePlacement, Target: plan.TargetPlacement, Reason: "drain failed"},
	}
	result, err := RehearseExtraction(context.Background(), extractor, plan)
	if err == nil || !errors.Is(err, pm073DrainErr) {
		if err == nil {
			t.Fatal("drain fault unexpectedly succeeded")
		}
	}
	if !extractor.restoreCall || len(result.Phases) != 5 || result.Restore.OperationID != "restore-1" || result.Drained {
		t.Fatalf("recovery evidence = %+v restoreCalled=%v", result, extractor.restoreCall)
	}
}

func TestTodo_PM_073_Fault(t *testing.T) {
	plan := pm073Plan()
	plan.Pilot = PilotGate{MixedLoadWithinSLO: false, SeparateCapacityReady: false, RehearsalApproved: true}
	extractor := &pm073Extractor{}
	result, err := RehearseExtraction(context.Background(), extractor, plan)
	if !errors.Is(err, ErrPilotGateFailed) || result.RequiredAction != "SEPARATE_PROJECT_DATABASE_CAPACITY_AND_RERUN" {
		t.Fatalf("failed gate result=%+v err=%v", result, err)
	}
	if len(extractor.phases) != 0 {
		t.Fatalf("failed pilot gate performed storage work: %v", extractor.phases)
	}
	plan = pm073Plan()
	extractor = &pm073Extractor{manifest: ExtractionManifest{Snapshot: plan.Snapshot, APIContractDigest: "wrong"}, restore: RestoreEvidence{OperationID: "restore-manifest", Source: plan.SourcePlacement, Target: plan.TargetPlacement, Reason: "manifest mismatch"}}
	result, err = RehearseExtraction(context.Background(), extractor, plan)
	if !errors.Is(err, ErrExtractionManifest) || len(result.Phases) != 2 || !extractor.restoreCall || result.Restore.OperationID != "restore-manifest" {
		t.Fatalf("manifest fault result=%+v err=%v", result, err)
	}
}
