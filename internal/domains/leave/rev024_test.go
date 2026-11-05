package leave

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/documents/intake"
	"github.com/monstercameron/human-capital-management-suite/internal/documentsecurity"
)

type rev024Scanner struct{ state documentsecurity.State }

func (s rev024Scanner) Scan(_ context.Context, _ documentsecurity.ScanInput, _ documentsecurity.Limits) (documentsecurity.Verdict, error) {
	return documentsecurity.Verdict{State: s.state, Scanner: "test-av", ScannerVersion: "1", Derivative: []byte("%PDF-1.7\nsafe derivative")}, nil
}

// rev024MintService scans one fixture upload and returns the governed
// release service plus its registry so tests can revoke beneath a ref.
func rev024MintService(t *testing.T) (*intake.Service, *documentsecurity.Registry) {
	t.Helper()
	registry := documentsecurity.NewRegistry()
	upload := documentsecurity.Upload{ID: "upload-1", Name: "medical.pdf", ContentType: "application/pdf", Bytes: []byte("bytes")}
	limits := documentsecurity.Limits{MaxBytes: 100, MaxDerivativeBytes: 100, MaxCompressionRatio: 10}
	if _, err := registry.Scan(context.Background(), upload, limits, rev024Scanner{documentsecurity.Safe}); err != nil {
		t.Fatal(err)
	}
	return intake.NewService(registry), registry
}

// rev024Release mints one governed evidence reference. Every release from
// one service carries a distinct sequence identity.
func rev024Release(t *testing.T, svc *intake.Service) intake.EvidenceRef {
	t.Helper()
	pkg, err := svc.Release(intake.Request{
		ArtifactID: "upload-1", Purpose: "leave.review",
		SubjectID: "worker-1", RequestedBy: "leave-snapshot-fixture",
		Now: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Evidence
}

// rev024Entries binds every required input to a governed reference issued
// by svc.
func rev024Entries(t *testing.T, svc *intake.Service) []InputEntry {
	t.Helper()
	entries := make([]InputEntry, 0, len(requiredLeaveInputs))
	for _, input := range requiredLeaveInputs {
		entries = append(entries, InputEntry{
			Input: input, Revision: "rev-7", Watermark: "wm-2026-09-01",
			Status: InputReady, EvidenceRef: rev024Release(t, svc),
			Authority: "source:" + input, Effective: "2026-09-01", Known: "2026-09-02",
		})
	}
	return entries
}

// TestTodo_REV_024_02 is the REV-024-02 primary: LeaveInputSnapshot
// evidence entries carry intake.EvidenceRef values produced by the governed
// DOC-INTAKE-001 pipeline, never bare colon-delimited strings. Only a
// usable, issuer-bound reference builds; zero, tampered and revoked
// references all refuse, and the snapshot seal binds the reference
// identity.
func TestTodo_REV_024_02(t *testing.T) {
	svc, registry := rev024MintService(t)
	built, err := BuildSnapshot(rev024Entries(t, svc))
	if err != nil {
		t.Fatalf("governed references refused: %v", err)
	}
	if !built.Satisfied() {
		t.Fatal("all-ready governed snapshot is not satisfied")
	}
	if err := built.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if built.Entries[0].EvidenceRef.ID == "" || built.Entries[0].EvidenceRef.ArtifactDigest == "" {
		t.Fatalf("entry carries no governed identity: %+v", built.Entries[0].EvidenceRef)
	}

	zero := rev024Entries(t, svc)
	zero[0].EvidenceRef = intake.EvidenceRef{}
	if _, err := BuildSnapshot(zero); !errors.Is(err, intake.ErrInvalid) {
		t.Fatalf("zero reference err = %v, want intake.ErrInvalid", err)
	}

	// A reference minted by a different service instance is still a
	// genuine governed issue: it carries its own issuer binding and its
	// own registry verdict, so it builds.
	foreignSvc, _ := rev024MintService(t)
	foreign := rev024Entries(t, svc)
	foreign[0].EvidenceRef = rev024Release(t, foreignSvc)
	if _, err := BuildSnapshot(foreign); err != nil {
		t.Fatalf("genuine second-service reference refused: %v", err)
	}

	tampered := rev024Entries(t, svc)
	ref := tampered[0].EvidenceRef
	ref.Purpose = "forged.purpose"
	tampered[0].EvidenceRef = ref
	if _, err := BuildSnapshot(tampered); !errors.Is(err, intake.ErrInvalid) {
		t.Fatalf("tampered reference err = %v, want intake.ErrInvalid", err)
	}

	revokedSvc, revokedRegistry := rev024MintService(t)
	revoked := rev024Entries(t, revokedSvc)
	if !revokedRegistry.Revoke("upload-1", "superseded scan") {
		t.Fatal("fixture revoke failed")
	}
	if _, err := BuildSnapshot(revoked); !errors.Is(err, intake.ErrRevoked) {
		t.Fatalf("revoked reference err = %v, want intake.ErrRevoked", err)
	}
	_ = registry

	// The seal binds reference identity: swapping one entry's reference
	// for a distinct issuance changes the digest.
	swapSvc, _ := rev024MintService(t)
	first, err := BuildSnapshot(rev024Entries(t, swapSvc))
	if err != nil {
		t.Fatal(err)
	}
	swapped := rev024Entries(t, swapSvc)
	swapped[0].EvidenceRef = rev024Release(t, swapSvc)
	second, err := BuildSnapshot(swapped)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == second.Digest {
		t.Fatal("reference swap kept the snapshot digest")
	}
	if err := second.Verify(); err != nil {
		t.Fatalf("swapped Verify: %v", err)
	}
}
