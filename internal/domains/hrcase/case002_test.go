package hrcase

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func case002Access() CaseAccess {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return CaseAccess{
		CaseID: "case-001",
		Participants: []Participant{
			{Role: "CASE_MANAGER", Principal: "principal:manager"},
			{Role: "SUBJECT", Principal: "worker:subject"},
			{Role: "WITNESS", Principal: "escrow:witness-1"},
			{Role: "INVESTIGATOR", Principal: "principal:investigator"},
		},
		Compartments: []Compartment{
			{ID: "general", Classification: "GENERAL", AllowedRoles: []string{"CASE_MANAGER", "SUBJECT", "INVESTIGATOR", "WITNESS"}, Purpose: "case handling", EffectiveFrom: at},
			{ID: "medical", Classification: "MEDICAL", AllowedRoles: []string{"CASE_MANAGER"}, Purpose: "accommodation review", EffectiveFrom: at},
			{ID: "investigation", Classification: "INVESTIGATION", AllowedRoles: []string{"CASE_MANAGER", "INVESTIGATOR"}, Purpose: "fact finding", EffectiveFrom: at},
		},
	}
}

func case002At() time.Time { return time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) }

// TestCaseCompartmentAuthorizationPreventsParticipantNoteAndEvidenceLeakage
// is the RED contract: every leak shape stays denied with a non-disclosing
// refusal.
func TestCaseCompartmentAuthorizationPreventsParticipantNoteAndEvidenceLeakage(t *testing.T) {
	access := case002Access()
	at := case002At()

	// Manager reads a general note: allowed, with a disclosure receipt.
	note := ConfidentialNote{ID: "note-1", CaseID: "case-001", CompartmentID: "general", Classification: "GENERAL", AuthorRole: "CASE_MANAGER", Author: "principal:manager", Purpose: "case handling", BodyDigest: "sha256:note"}
	stored, receipt, err := access.StoreNote(note)
	if err != nil {
		t.Fatalf("StoreNote: %v", err)
	}
	if receipt.CaseID != "case-001" || receipt.CompartmentID != "general" || receipt.Digest() == "" {
		t.Fatalf("receipt = %+v", receipt)
	}
	_ = stored

	// Manager sees a medical artifact: denied.
	if err := access.CanView("principal:manager", "CASE_MANAGER", "medical", "case handling", at); err == nil {
		t.Fatal("manager viewed medical under the wrong purpose")
	} else if !errors.Is(err, ErrCaseDenied) {
		t.Fatalf("err=%v, want ErrCaseDenied", err)
	}

	// Subject infers the confidential witness: denied from investigation.
	if err := access.CanView("worker:subject", "SUBJECT", "investigation", "fact finding", at); !errors.Is(err, ErrCaseDenied) {
		t.Fatalf("subject view err=%v, want denied", err)
	}

	// Note classification lowered by the caller: refused.
	lowered := ConfidentialNote{ID: "note-2", CaseID: "case-001", CompartmentID: "medical", Classification: "GENERAL", AuthorRole: "CASE_MANAGER", Author: "principal:manager", Purpose: "accommodation review", BodyDigest: "sha256:note-2"}
	if _, _, err := access.StoreNote(lowered); !errors.Is(err, ErrCaseInvalid) {
		t.Fatalf("lowered classification err=%v, want invalid", err)
	}

	// Escrowed reporter is correlated: no API resolves escrow identity.
	if ResolveEscrowed("escrow:witness-1") != "" {
		t.Fatal("escrow identity resolved")
	}

	// Removed participant retains no cached access.
	access.RevokeParticipant("worker:subject")
	if err := access.CanView("worker:subject", "SUBJECT", "general", "case handling", at); !errors.Is(err, ErrCaseDenied) {
		t.Fatalf("revoked view err=%v, want denied", err)
	}

	// Search and count reveal nothing hidden.
	visible := access.VisibleCases("worker:subject", []string{"case-001", "case-002"})
	for _, id := range visible {
		if id == "case-001" {
			t.Fatal("revoked subject still sees the case")
		}
	}
	if got := access.CountVisible("worker:subject", []string{"case-001", "case-002"}); got != len(visible) {
		t.Fatalf("count=%d visible=%d", got, len(visible))
	}
}

// TestTodo_CASE_002_Property proves authorization is total: every
// unknown principal, role, compartment or purpose is denied, and every
// declared edge is allowed, deterministically.
func TestTodo_CASE_002_Property(t *testing.T) {
	access := case002Access()
	at := case002At()
	allowed := [][4]string{
		{"principal:manager", "CASE_MANAGER", "general", "case handling"},
		{"principal:manager", "CASE_MANAGER", "medical", "accommodation review"},
		{"principal:investigator", "INVESTIGATOR", "investigation", "fact finding"},
		{"escrow:witness-1", "WITNESS", "general", "case handling"},
	}
	for _, a := range allowed {
		first, err := access.CanViewReceipt(a[0], a[1], a[2], a[3], at)
		if err != nil {
			t.Fatalf("allowed edge %v: %v", a, err)
		}
		second, err := access.CanViewReceipt(a[0], a[1], a[2], a[3], at)
		if err != nil || first.Digest() != second.Digest() {
			t.Fatalf("allowed edge %v unstable", a)
		}
	}
	denied := [][4]string{
		{"principal:nobody", "CASE_MANAGER", "general", "case handling"},
		{"principal:manager", "WITNESS", "general", "case handling"},
		{"principal:manager", "CASE_MANAGER", "vault", "case handling"},
		{"principal:manager", "CASE_MANAGER", "general", "curiosity"},
		{"", "CASE_MANAGER", "general", "case handling"},
	}
	for _, d := range denied {
		if err := access.CanView(d[0], d[1], d[2], d[3], at); !errors.Is(err, ErrCaseDenied) {
			t.Fatalf("edge %v err=%v, want denied", d, err)
		}
	}
	// Revocation is idempotent and total: no compartment survives it.
	access.RevokeParticipant("principal:manager")
	access.RevokeParticipant("principal:manager")
	for _, a := range allowed {
		if err := access.CanView(a[0], a[1], a[2], a[3], at); a[0] == "principal:manager" && !errors.Is(err, ErrCaseDenied) {
			t.Fatalf("revoked edge %v err=%v, want denied", a, err)
		}
	}
	if got := access.VisibleCases("principal:manager", []string{"case-001"}); len(got) != 0 {
		t.Fatalf("revoked listing = %v", got)
	}
	// An expired compartment denies even a fresh participant.
	expired := case002Access()
	expired.Compartments[0].EffectiveTo = case002At().Add(-time.Hour)
	if err := expired.CanView("worker:subject", "SUBJECT", "general", "case handling", at); !errors.Is(err, ErrCaseDenied) {
		t.Fatalf("expired edge err=%v, want denied", err)
	}
}

// TestTodo_CASE_002_Golden pins the canonical disclosure receipt.
func TestTodo_CASE_002_Golden(t *testing.T) {
	access := case002Access()
	first, err := access.CanViewReceipt("principal:manager", "CASE_MANAGER", "general", "case handling", case002At())
	if err != nil {
		t.Fatal(err)
	}
	second, err := access.CanViewReceipt("principal:manager", "CASE_MANAGER", "general", "case handling", case002At())
	if err != nil || first.Digest() != second.Digest() {
		t.Fatal("receipt digest unstable")
	}
	if !strings.HasPrefix(first.Digest(), "sha256:") || first.Viewer != "principal:manager" || first.CompartmentID != "general" {
		t.Fatalf("receipt = %+v", first)
	}
	// An escrowed viewer is redacted in derived receipts.
	escrowed, err := access.CanViewReceipt("escrow:witness-1", "WITNESS", "general", "case handling", case002At())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(escrowed.Viewer, "witness-1") {
		t.Fatalf("escrow leaked into receipt: %+v", escrowed)
	}
}

// FuzzTodo_CASE_002 proves authorization never panics and never grants on an
// empty principal, whatever strings arrive.
func FuzzTodo_CASE_002(f *testing.F) {
	access := case002Access()
	at := case002At()
	f.Add("principal:manager", "CASE_MANAGER", "general", "case handling")
	f.Add("", "", "", "")
	f.Add("escrow:witness-1", "WITNESS", "investigation", "fact finding")
	f.Fuzz(func(t *testing.T, principal, role, compartment, purpose string) {
		err := access.CanView(principal, role, compartment, purpose, at)
		if principal == "" && err == nil {
			t.Fatal("empty principal granted")
		}
		if err != nil && !errors.Is(err, ErrCaseDenied) && !errors.Is(err, ErrCaseInvalid) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestTodo_CASE_002_Security proves denied reads are non-disclosing:
// a missing case and a forbidden case refuse identically.
func TestTodo_CASE_002_Security(t *testing.T) {
	access := case002Access()
	at := case002At()
	missing := access.CanView("worker:subject", "SUBJECT", "vault", "case handling", at)
	forbidden := access.CanView("worker:subject", "SUBJECT", "investigation", "fact finding", at)
	if missing == nil || forbidden == nil || missing.Error() != forbidden.Error() {
		t.Fatalf("disclosing refusal: %v vs %v", missing, forbidden)
	}
	item := EvidenceItem{ID: "ev-1", CaseID: "case-001", CompartmentID: "medical", ArtifactDigest: "sha256:artifact", Custody: []string{"custody:intake"}}
	if err := access.CheckEvidence("principal:manager", "CASE_MANAGER", item, "case handling", at); err != nil {
		t.Fatalf("manager medical custody check: %v", err)
	}
	if err := access.CheckEvidence("worker:subject", "SUBJECT", item, "case handling", at); !errors.Is(err, ErrCaseDenied) {
		t.Fatalf("subject medical evidence err=%v, want denied", err)
	}
	uncustodied := item
	uncustodied.Custody = nil
	if err := access.CheckEvidence("principal:manager", "CASE_MANAGER", uncustodied, "accommodation review", at); !errors.Is(err, ErrCaseInvalid) {
		t.Fatalf("uncustodied evidence err=%v, want invalid", err)
	}
}

// TestTodo_CASE_002_Mutation proves tampered compartments, forged notes and
// replayed receipts cannot slip through.
func TestTodo_CASE_002_Mutation(t *testing.T) {
	access := case002Access()
	at := case002At()
	// A receipt issued under the original policy dies when the policy
	// changes underneath it.
	original, err := access.CanViewReceipt("principal:investigator", "INVESTIGATOR", "investigation", "fact finding", at)
	if err != nil {
		t.Fatal(err)
	}
	tampered := case002Access()
	tampered.Compartments[2].Purpose = "rewritten purpose"
	if err := original.Verify(tampered); !errors.Is(err, ErrCaseDenied) {
		t.Fatalf("receipt survived a policy change: %v", err)
	}
	if err := original.Verify(access); err != nil {
		t.Fatalf("receipt died under its own policy: %v", err)
	}
	forged := ConfidentialNote{ID: "note-9", CaseID: "case-001", CompartmentID: "general", Classification: "GENERAL", AuthorRole: "SUBJECT", Author: "principal:manager", Purpose: "case handling", BodyDigest: "sha256:x"}
	if _, _, err := access.StoreNote(forged); !errors.Is(err, ErrCaseDenied) {
		t.Fatalf("forged author err=%v, want denied", err)
	}
	receipt, err := access.CanViewReceipt("principal:manager", "CASE_MANAGER", "general", "case handling", at)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Purpose = "curiosity"
	if receipt.Verify(access) == nil {
		t.Fatal("tampered receipt verified")
	}
	if err := receipt.Verify(access); !errors.Is(err, ErrCaseDenied) {
		t.Fatalf("tampered receipt err=%v, want denied", err)
	}
}
