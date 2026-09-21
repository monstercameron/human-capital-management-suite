package promotion

import (
	"errors"
	"testing"
	"time"
)

func TestRedriveRefreshesSimulationWithoutChangingLifecycle(t *testing.T) {
	r := NewRegistry()
	p := testPackage(t, "pkg-redrive", "production")
	if err := r.Put(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Redrive(p.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("redrive before simulation error = %v, want invalid transition", err)
	}
	before, err := r.Simulate(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := r.Redrive(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Digest != before.Digest || !after.Passed {
		t.Fatalf("redriven simulation = %+v, want digest %q and passed", after, before.Digest)
	}
	record, ok := r.Get(p.ID)
	if !ok || record.Status != StatusSimulated {
		t.Fatalf("record after redrive = %+v, found=%v", record, ok)
	}

	if _, err := r.Approve(p.ID, "operator", time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Redrive(p.ID); err != nil {
		t.Fatalf("redrive approved package: %v", err)
	}
	if _, err := r.Activate(p.ID, time.Unix(21, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Redrive(p.ID); err != nil {
		t.Fatalf("redrive active package: %v", err)
	}
}

func TestRedriveUnknownPackageIsNotFound(t *testing.T) {
	if _, err := NewRegistry().Redrive("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("redrive missing package error = %v, want not found", err)
	}
}
