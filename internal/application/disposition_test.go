package application

// RED/GREEN evidence for REV-004-02: governed retention, legal-hold and
// verified-deletion wired into the serve composition root.
//
// The RED was a wiring gap, not a logic gap: internal/governance/records
// (ExecuteDeletion, Simulate) and internal/governance/legalhold
// (DecideDisposition) had zero non-test importers and appeared in no
// cmd/* dependency closure. These tests prove the DispositionGate the serve
// cell now composes calls them on a real request path: retention simulation
// first, legal-hold evaluation second, verified deletion last and only when
// the hold check allows it.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legalhold"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/records"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	rev00402Tenant      = "tenant-a"
	rev00402Compartment = "hr-records"
	rev00402RecordRef   = "rec-001"
)

func rev00402At() time.Time {
	return time.Date(2026, time.September, 21, 12, 0, 0, 0, time.UTC)
}

func rev00402Hold() legalhold.Hold {
	return legalhold.Hold{
		ID:          "hold-1",
		Tenant:      rev00402Tenant,
		Compartment: rev00402Compartment,
		Scope: legalhold.Scope{
			Tenant:      rev00402Tenant,
			Compartment: rev00402Compartment,
			RecordRefs:  []string{rev00402RecordRef},
		},
		Reason:    "litigation hold",
		Authority: "general-counsel",
		CreatedAt: values.NewInstant(rev00402At()),
	}
}

func rev00402Record() legalhold.Record {
	return legalhold.Record{
		Tenant:      rev00402Tenant,
		Compartment: rev00402Compartment,
		Ref:         rev00402RecordRef,
	}
}

func rev00402Deletion() records.DeletionRequest {
	return records.DeletionRequest{
		DeletionID:  "del-1",
		Tenant:      rev00402Tenant,
		RecordID:    rev00402RecordRef,
		RequestedBy: "privacy-officer",
		At:          rev00402At(),
		Copies: []records.DeletableCopy{
			{ID: "copy-canonical", Kind: records.CopyKindCanonical, Tenant: rev00402Tenant},
			{ID: "copy-derived", Kind: records.CopyKindDerived, Tenant: rev00402Tenant},
			{ID: "copy-external", Kind: records.CopyKindExternal, Tenant: rev00402Tenant},
			{ID: "copy-backup", Kind: records.CopyKindBackup, Tenant: rev00402Tenant, ReDeleted: true, ReDeleteRef: "redel-1"},
			{ID: "copy-restored", Kind: records.CopyKindRestored, Tenant: rev00402Tenant, TombstoneDigest: "tomb-1"},
		},
		Tombstones: []records.Tombstone{{CopyID: "copy-restored", Digest: "tomb-1"}},
	}
}

func rev00402Request() VerifiedDeletionRequest {
	return VerifiedDeletionRequest{
		Deletion:   rev00402Deletion(),
		Record:     rev00402Record(),
		EvidenceID: "evidence/rev-004-02-1",
		At:         rev00402At(),
	}
}

func rev00402Simulation() records.SimulationRequest {
	cutoff := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	created := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	return records.SimulationRequest{
		AsOf: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		Copies: []records.Copy{
			{
				ID: trueCopyID(), RecordSeries: "payroll", Custodian: "hr",
				Jurisdiction: "US-CA", CreatedAt: created, CutoffAt: &cutoff,
				ArchiveAcknowledged: true,
			},
		},
		Rules: []records.RetentionRule{
			{RecordSeries: "payroll", Jurisdiction: "US-CA", MinimumDays: 30, AuthorityRef: "schedule-payroll-ca"},
		},
	}
}

func trueCopyID() string { return "copy-payroll-1" }

func TestTodo_REV_004_02(t *testing.T) {
	t.Parallel()

	t.Run("retention simulation runs through the gate", func(t *testing.T) {
		t.Parallel()
		gate := NewDispositionGate()
		report, err := gate.EvaluateRetention(rev00402Simulation())
		if err != nil {
			t.Fatalf("EvaluateRetention: %v", err)
		}
		if report.Status != records.Eligible {
			t.Fatalf("retention status = %q, want %q (blockers: %+v)", report.Status, records.Eligible, report.Blockers)
		}
		if len(report.Copies) != 1 || report.Copies[0].CopyID != "copy-payroll-1" {
			t.Fatalf("retention copies = %+v, want the one simulated copy", report.Copies)
		}
		if report.Digest == "" {
			t.Fatal("retention report carries no digest")
		}
		if report.DeletionCount != 0 || report.DeletedBytes != 0 {
			t.Fatalf("simulation authorizes deletion: count=%d bytes=%d", report.DeletionCount, report.DeletedBytes)
		}

		held := rev00402Simulation()
		held.Copies[0].ActiveHoldRefs = []string{"hold-1"}
		blocked, err := gate.EvaluateRetention(held)
		if err != nil {
			t.Fatalf("EvaluateRetention(held): %v", err)
		}
		if blocked.Status != records.BlockedWithReasons {
			t.Fatalf("held retention status = %q, want BLOCKED_WITH_REASONS", blocked.Status)
		}
	})

	t.Run("a hold blocks verified deletion and persists the finding", func(t *testing.T) {
		t.Parallel()
		gate := NewDispositionGate()
		if err := gate.CreateHold(rev00402Hold()); err != nil {
			t.Fatalf("CreateHold: %v", err)
		}
		result, err := gate.ExecuteVerifiedDeletion(rev00402Request())
		if !errors.Is(err, legalhold.ErrHoldBlocked) {
			t.Fatalf("ExecuteVerifiedDeletion(held) = %v, want HOLD_BLOCKED", err)
		}
		if result.Certificate.Complete || result.Certificate.Digest != "" {
			t.Fatalf("held deletion produced a certificate: %+v", result.Certificate)
		}
		if result.Decision.Code != "HOLD_BLOCKED" || result.Decision.HoldID != "hold-1" {
			t.Fatalf("held decision = %+v, want HOLD_BLOCKED naming hold-1", result.Decision)
		}
		found := false
		for _, e := range gate.HoldEvidence() {
			if e.Code == "HOLD_BLOCKED" && e.RecordRef == rev00402RecordRef && e.ID == "evidence/rev-004-02-1" {
				found = true
			}
		}
		if !found {
			t.Fatalf("hold evidence log has no persisted HOLD_BLOCKED finding: %+v", gate.HoldEvidence())
		}
	})

	t.Run("release lets the same deletion complete with a certificate", func(t *testing.T) {
		t.Parallel()
		gate := NewDispositionGate()
		if err := gate.CreateHold(rev00402Hold()); err != nil {
			t.Fatalf("CreateHold: %v", err)
		}
		if _, err := gate.ExecuteVerifiedDeletion(rev00402Request()); !errors.Is(err, legalhold.ErrHoldBlocked) {
			t.Fatalf("first attempt = %v, want HOLD_BLOCKED", err)
		}
		if err := gate.ReleaseHold("hold-1", rev00402At()); err != nil {
			t.Fatalf("ReleaseHold: %v", err)
		}
		result, err := gate.ExecuteVerifiedDeletion(rev00402Request())
		if err != nil {
			t.Fatalf("ExecuteVerifiedDeletion(released): %v", err)
		}
		if !result.Decision.Allowed || result.Decision.Code != "DISPOSITION_ALLOWED" {
			t.Fatalf("released decision = %+v, want DISPOSITION_ALLOWED", result.Decision)
		}
		cert := result.Certificate
		if !cert.Complete || len(cert.Outcomes) != 5 || cert.Digest == "" {
			t.Fatalf("released certificate = %+v, want a complete 5-outcome certificate", cert)
		}
		if cert.Tenant != rev00402Tenant || cert.RecordID != rev00402RecordRef {
			t.Fatalf("certificate binds the wrong subject: %+v", cert)
		}
	})

	t.Run("malformed deletions are errors, never partial certificates", func(t *testing.T) {
		t.Parallel()
		gate := NewDispositionGate()
		bad := rev00402Request()
		bad.Deletion.DeletionID = ""
		if _, err := gate.ExecuteVerifiedDeletion(bad); err == nil {
			t.Fatal("ExecuteVerifiedDeletion without a deletion id succeeded")
		}
		missing := rev00402Request()
		missing.EvidenceID = ""
		if _, err := gate.ExecuteVerifiedDeletion(missing); err == nil {
			t.Fatal("ExecuteVerifiedDeletion without an evidence identity succeeded")
		}
		var uncomposed *DispositionGate
		if _, err := uncomposed.ExecuteVerifiedDeletion(rev00402Request()); err == nil {
			t.Fatal("uncomposed gate executed a deletion")
		}
		if _, err := uncomposed.EvaluateRetention(rev00402Simulation()); err == nil {
			t.Fatal("uncomposed gate simulated retention")
		}
		if got := uncomposed.DecideDisposition(rev00402Record(), "e", rev00402At()); got.Code != "GATE_UNCOMPOSED" {
			t.Fatalf("uncomposed gate decision = %+v, want GATE_UNCOMPOSED", got)
		}
	})
}

func TestTodo_REV_004_02_Golden(t *testing.T) {
	t.Parallel()

	gate := NewDispositionGate()
	result, err := gate.ExecuteVerifiedDeletion(rev00402Request())
	if err != nil {
		t.Fatalf("ExecuteVerifiedDeletion: %v", err)
	}
	explain := result.Certificate.Explain()
	if !strings.HasPrefix(explain, "verified deletion v1 id=del-1 tenant=tenant-a record=rec-001 complete=true outcomes=5 digest=") {
		t.Fatalf("certificate explanation = %q", explain)
	}
	// The certificate digest pins every outcome: rerunning the identical
	// envelope must reproduce the identical digest byte for byte.
	again, err := gate.ExecuteVerifiedDeletion(rev00402Request())
	if err != nil {
		t.Fatalf("ExecuteVerifiedDeletion again: %v", err)
	}
	if again.Certificate.Digest != result.Certificate.Digest {
		t.Fatalf("certificate digest drifted: %q then %q", result.Certificate.Digest, again.Certificate.Digest)
	}
	if !strings.HasPrefix(result.Certificate.Digest, "sha256:") || len(result.Certificate.Digest) != len("sha256:")+64 {
		t.Fatalf("certificate digest = %q, want sha256: plus 64 hex characters", result.Certificate.Digest)
	}

	report, err := gate.EvaluateRetention(rev00402Simulation())
	if err != nil {
		t.Fatalf("EvaluateRetention: %v", err)
	}
	if report.Digest == "" {
		t.Fatal("retention report carries no digest")
	}
	rerun, err := gate.EvaluateRetention(rev00402Simulation())
	if err != nil {
		t.Fatalf("EvaluateRetention again: %v", err)
	}
	if rerun.Digest != report.Digest {
		t.Fatalf("retention digest drifted: %q then %q", report.Digest, rerun.Digest)
	}
}

func TestTodo_REV_004_02_Security(t *testing.T) {
	t.Parallel()

	t.Run("one tenant cannot hold or delete another tenant", func(t *testing.T) {
		t.Parallel()
		gate := NewDispositionGate()
		other := rev00402Hold()
		other.ID = "hold-other"
		other.Tenant = "tenant-b"
		other.Scope.Tenant = "tenant-b"
		if err := gate.CreateHold(other); err != nil {
			t.Fatalf("CreateHold(other tenant): %v", err)
		}
		result, err := gate.ExecuteVerifiedDeletion(rev00402Request())
		if err != nil {
			t.Fatalf("tenant-b hold blocked tenant-a deletion: %v", err)
		}
		if !result.Certificate.Complete {
			t.Fatalf("tenant-a certificate is incomplete: %+v", result.Certificate)
		}
		mismatched := rev00402Request()
		mismatched.Record.Tenant = "tenant-b"
		if _, err := gate.ExecuteVerifiedDeletion(mismatched); err == nil {
			t.Fatal("deletion executed against a hold check for another tenant")
		}
		foreign := rev00402Deletion()
		foreign.Copies = append(foreign.Copies, records.DeletableCopy{ID: "copy-foreign", Kind: records.CopyKindDerived, Tenant: "tenant-b"})
		smuggled := rev00402Request()
		smuggled.Deletion = foreign
		if _, err := gate.ExecuteVerifiedDeletion(smuggled); err == nil {
			t.Fatal("deletion carrying another tenant's copy succeeded")
		}
	})

	t.Run("hold visibility needs authorization and compartment", func(t *testing.T) {
		t.Parallel()
		gate := NewDispositionGate()
		if err := gate.CreateHold(rev00402Hold()); err != nil {
			t.Fatalf("CreateHold: %v", err)
		}
		if _, err := gate.Holds(rev00402Tenant, rev00402Compartment, false); !errors.Is(err, legalhold.ErrUnauthorized) {
			t.Fatalf("unauthorized hold query = %v, want ErrUnauthorized", err)
		}
		if _, err := gate.Holds("", "", true); !errors.Is(err, legalhold.ErrCompartmentDenied) {
			t.Fatalf("empty-scope hold query = %v, want ErrCompartmentDenied", err)
		}
		visible, err := gate.Holds(rev00402Tenant, rev00402Compartment, true)
		if err != nil || len(visible) != 1 || visible[0].ID != "hold-1" {
			t.Fatalf("authorized hold query = %+v, %v; want hold-1", visible, err)
		}
		other, err := gate.Holds("tenant-b", rev00402Compartment, true)
		if err != nil || len(other) != 0 {
			t.Fatalf("cross-tenant hold query = %+v, %v; want none", other, err)
		}
	})

	t.Run("duplicate and unknown holds fail closed", func(t *testing.T) {
		t.Parallel()
		gate := NewDispositionGate()
		if err := gate.CreateHold(rev00402Hold()); err != nil {
			t.Fatalf("CreateHold: %v", err)
		}
		if err := gate.CreateHold(rev00402Hold()); !errors.Is(err, legalhold.ErrInvalidHold) {
			t.Fatalf("duplicate hold = %v, want ErrInvalidHold", err)
		}
		if err := gate.ReleaseHold("hold-unknown", rev00402At()); !errors.Is(err, legalhold.ErrUnknownHold) {
			t.Fatalf("unknown release = %v, want ErrUnknownHold", err)
		}
	})
}

func TestTodo_REV_004_02_Integration(t *testing.T) {
	composed, _, _ := composeStub(t, stubServeConfig())

	component, ok := composed.Graph().Component(ComponentDispositionGate)
	if !ok {
		t.Fatalf("the served graph names no %q", ComponentDispositionGate)
	}
	if component.Kind != KindAdapter {
		t.Fatalf("disposition gate kind = %q, want %q", component.Kind, KindAdapter)
	}
	if !strings.Contains(component.Impl, "DispositionGate") {
		t.Fatalf("disposition gate impl = %q, want the composed gate", component.Impl)
	}
	gate := composed.Disposition()
	if gate == nil {
		t.Fatal("the served composition exposes no disposition gate")
	}

	// The served cell's own gate evaluates the hold, refuses the deletion,
	// and persists the finding.
	if err := gate.CreateHold(rev00402Hold()); err != nil {
		t.Fatalf("CreateHold through the served gate: %v", err)
	}
	if _, err := gate.ExecuteVerifiedDeletion(rev00402Request()); !errors.Is(err, legalhold.ErrHoldBlocked) {
		t.Fatalf("served deletion under hold = %v, want HOLD_BLOCKED", err)
	}
	if len(gate.HoldEvidence()) == 0 {
		t.Fatal("the served gate persisted no hold finding")
	}

	// Recovery through the same served gate: release, then the identical
	// request completes with a certificate.
	if err := gate.ReleaseHold("hold-1", rev00402At()); err != nil {
		t.Fatalf("ReleaseHold through the served gate: %v", err)
	}
	result, err := gate.ExecuteVerifiedDeletion(rev00402Request())
	if err != nil {
		t.Fatalf("served deletion after release: %v", err)
	}
	if !result.Certificate.Complete {
		t.Fatalf("served certificate is incomplete: %+v", result.Certificate)
	}
	report, err := gate.EvaluateRetention(rev00402Simulation())
	if err != nil {
		t.Fatalf("served retention simulation: %v", err)
	}
	if report.Status != records.Eligible {
		t.Fatalf("served retention status = %q, want ELIGIBLE", report.Status)
	}
}

func TestTodo_REV_004_02_Recovery(t *testing.T) {
	t.Parallel()

	t.Run("a restored copy without its tombstone keeps the certificate incomplete", func(t *testing.T) {
		t.Parallel()
		gate := NewDispositionGate()
		req := rev00402Request()
		req.Deletion.Tombstones = nil
		result, err := gate.ExecuteVerifiedDeletion(req)
		if err != nil {
			t.Fatalf("ExecuteVerifiedDeletion without tombstone: %v", err)
		}
		if result.Certificate.Complete {
			t.Fatal("deletion completed while a restored copy serves without its tombstone")
		}
		for _, outcome := range result.Certificate.Outcomes {
			if outcome.CopyID == "copy-restored" && outcome.Outcome != records.OutcomeUnspecified {
				t.Fatalf("restored outcome = %q, want OUTCOME_UNSPECIFIED", outcome.Outcome)
			}
		}
		// Reapplying the tombstone recovers the same envelope to complete.
		recovered, err := gate.ExecuteVerifiedDeletion(rev00402Request())
		if err != nil {
			t.Fatalf("ExecuteVerifiedDeletion with tombstone: %v", err)
		}
		if !recovered.Certificate.Complete {
			t.Fatalf("recovered certificate is incomplete: %+v", recovered.Certificate)
		}
	})

	t.Run("a backup without re-delete evidence keeps the certificate incomplete", func(t *testing.T) {
		t.Parallel()
		gate := NewDispositionGate()
		req := rev00402Request()
		for i := range req.Deletion.Copies {
			if req.Deletion.Copies[i].Kind == records.CopyKindBackup {
				req.Deletion.Copies[i].ReDeleted = false
				req.Deletion.Copies[i].ReDeleteRef = ""
			}
		}
		result, err := gate.ExecuteVerifiedDeletion(req)
		if err != nil {
			t.Fatalf("ExecuteVerifiedDeletion without re-delete: %v", err)
		}
		if result.Certificate.Complete {
			t.Fatal("deletion completed while a backup lacks re-delete evidence")
		}
	})

	t.Run("retention not yet met blocks while the schedule still applies", func(t *testing.T) {
		t.Parallel()
		gate := NewDispositionGate()
		early := rev00402Simulation()
		early.AsOf = time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
		report, err := gate.EvaluateRetention(early)
		if err != nil {
			t.Fatalf("EvaluateRetention(early): %v", err)
		}
		if report.Status == records.Eligible {
			t.Fatal("retention eligible before the 30-day minimum elapsed")
		}
		met := false
		for _, blocker := range report.Blockers {
			if blocker.Code == "RETENTION_NOT_MET" {
				met = true
			}
		}
		if !met {
			t.Fatalf("retention blockers = %+v, want RETENTION_NOT_MET", report.Blockers)
		}
	})
}
