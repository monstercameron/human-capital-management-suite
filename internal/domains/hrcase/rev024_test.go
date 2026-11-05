package hrcase

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/documents/evidence"
)

// rev024Envelope seals a governed evidence envelope carrying one item per
// id/digest pair. The envelope is Complete: every policy reference the
// pipeline requires is present.
func rev024Envelope(t *testing.T, items ...evidence.EvidenceItem) evidence.Package {
	t.Helper()
	sealed, err := evidence.New(evidence.Package{
		Issuer: "evidence-authority/v1", Freshness: "fresh:2026-04-04",
		Signature: "sig:case-004", Classification: "INVESTIGATION",
		Purpose: "case.review", Retention: "retain:7y", Hold: "hold:hold-7",
		Method: "sealed-export", Schema: "evidence/v1", Rule: "case004-export",
		Redaction: "redaction-policy/v1", VerifierAuthority: "evidence-authority/v1",
		Items: items,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sealed.Verify().Status != evidence.Complete {
		t.Fatalf("fixture envelope status = %s, want COMPLETE", sealed.Verify().Status)
	}
	return sealed
}

// TestTodo_REV_024_02_Integration is the REV-024-02 integration: the case
// evidence-package export consumes DOC-EVIDENCE-001 envelopes. Every
// artifact carries a sealed, Complete envelope and its EvidenceID and
// digest must resolve into that envelope; bare colon-delimited strings
// with no envelope, unknown IDs, mismatched digests and incomplete
// envelopes are all refused with zero effect.
func TestTodo_REV_024_02_Integration(t *testing.T) {
	first := rev024Envelope(t, evidence.EvidenceItem{ID: "ev-1", Kind: "document", Digest: "sha256:artifact-1", Sequence: 1})
	second := rev024Envelope(t, evidence.EvidenceItem{ID: "ev-2", Kind: "document", Digest: "sha256:artifact-2", Sequence: 2})
	governed := []ExportArtifact{
		{EvidenceID: "ev-1", ArtifactDigest: "sha256:artifact-1", CompartmentID: "general", RetentionDecision: "retain:7y", Evidence: first},
		{EvidenceID: "ev-2", ArtifactDigest: "sha256:artifact-2", CompartmentID: "investigation", RedactionNote: "witness name redacted", LegalHold: true, HoldRef: "hold-7", RetentionDecision: "retain:7y", CorrectionOf: "ev-2-v1", Evidence: second},
	}
	pkg, err := ExportEvidence("case-004", []string{"general", "investigation"}, case004Events(), governed, case004Decisions())
	if err != nil {
		t.Fatalf("governed export refused: %v", err)
	}
	if len(pkg.Artifacts) != 2 || pkg.Digest == "" {
		t.Fatalf("governed package = %+v", pkg)
	}
	if pkg.Artifacts[0].Evidence.Verify().Status != evidence.Complete {
		t.Fatal("exported artifact lost its governed envelope")
	}

	bare := []ExportArtifact{
		{EvidenceID: "ev-1", ArtifactDigest: "sha256:artifact-1", CompartmentID: "general", RetentionDecision: "retain:7y"},
	}
	if _, err := ExportEvidence("case-004", []string{"general"}, case004Events(), bare, case004Decisions()); !errors.Is(err, ErrExportInvalid) {
		t.Fatalf("bare-string artifact err = %v, want ErrExportInvalid", err)
	}

	unknown := []ExportArtifact{
		{EvidenceID: "ev-9", ArtifactDigest: "sha256:artifact-9", CompartmentID: "general", RetentionDecision: "retain:7y", Evidence: first},
	}
	if _, err := ExportEvidence("case-004", []string{"general"}, case004Events(), unknown, case004Decisions()); !errors.Is(err, ErrExportInvalid) {
		t.Fatalf("unknown evidence id err = %v, want ErrExportInvalid", err)
	}

	mismatched := []ExportArtifact{
		{EvidenceID: "ev-1", ArtifactDigest: "sha256:forged", CompartmentID: "general", RetentionDecision: "retain:7y", Evidence: first},
	}
	if _, err := ExportEvidence("case-004", []string{"general"}, case004Events(), mismatched, case004Decisions()); !errors.Is(err, ErrExportInvalid) {
		t.Fatalf("mismatched digest err = %v, want ErrExportInvalid", err)
	}

	partial, err := evidence.New(evidence.Package{
		Issuer: "evidence-authority/v1", Items: []evidence.EvidenceItem{{ID: "ev-1", Kind: "document", Digest: "sha256:artifact-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	incomplete := []ExportArtifact{
		{EvidenceID: "ev-1", ArtifactDigest: "sha256:artifact-1", CompartmentID: "general", RetentionDecision: "retain:7y", Evidence: partial},
	}
	if _, err := ExportEvidence("case-004", []string{"general"}, case004Events(), incomplete, case004Decisions()); !errors.Is(err, ErrExportInvalid) {
		t.Fatalf("partial envelope err = %v, want ErrExportInvalid", err)
	}
}
