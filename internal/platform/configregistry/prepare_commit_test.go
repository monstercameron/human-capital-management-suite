package configregistry

import (
	"testing"
	"time"
)

func TestTodo_REV_031_01_PrepareCommit(t *testing.T) {
	store := NewRegistry()
	obj := newObj(Scope{TenantID: "rev031-prepare", CellID: "cell-a"}, KindRule, "candidate", 1, `{"enabled":true}`)
	prepared, err := Prepare(store, obj)
	if err != nil {
		t.Fatal(err)
	}
	candidate := prepared.Object()
	if candidate.Digest() == "" {
		t.Fatal("prepare did not mint immutable candidate digest")
	}
	if _, found, err := store.GetObject(candidate.Ref()); err != nil || found {
		t.Fatalf("prepare changed registry: found=%t err=%v", found, err)
	}
	evidence := ActivationEvidence{ActivatedBy: "operator", Authority: "config-control", ActivatedAt: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)}
	record, err := CommitActivation(store, prepared, evidence)
	if err != nil {
		t.Fatal(err)
	}
	active, err := Resolve(store, candidate.Scope, candidate.Kind, candidate.ID)
	if err != nil || active.Digest() != candidate.Digest() || record.ObjectDigest != candidate.Digest() {
		t.Fatalf("atomic activation result=%+v active=%+v err=%v", record, active, err)
	}
	history, err := store.ListActivations(candidate.Scope, candidate.Kind, candidate.ID)
	if err != nil || len(history) != 1 || history[0] != record {
		t.Fatalf("activation history=%+v err=%v", history, err)
	}
}

func TestTodo_REV_031_01_PrepareCommitFault(t *testing.T) {
	store := NewRegistry()
	obj := newObj(Scope{TenantID: "rev031-prepare-fault", CellID: "cell-a"}, KindRule, "candidate", 1, "body")
	prepared, err := Prepare(store, obj)
	if err != nil {
		t.Fatal(err)
	}
	// A corrupted prepared value cannot publish either the object or history.
	prepared.object.Body = []byte("tampered")
	if _, err := CommitActivation(store, prepared, ActivationEvidence{ActivatedBy: "operator", ActivatedAt: time.Now()}); err == nil {
		t.Fatal("tampered prepared candidate committed")
	}
	if _, found, err := store.GetObject(obj.Ref()); err != nil || found {
		t.Fatalf("failed commit left object: found=%t err=%v", found, err)
	}
	if history, err := store.ListActivations(obj.Scope, obj.Kind, obj.ID); err != nil || len(history) != 0 {
		t.Fatalf("failed commit left activation history=%+v err=%v", history, err)
	}
}
