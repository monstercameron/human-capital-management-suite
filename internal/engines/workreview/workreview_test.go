package workreview_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/workreview"
)

func testFinding() workreview.Finding {
	return workreview.Finding{
		TaskID:             "task:leave-1",
		Verdict:            workreview.ReviewMoreInfo,
		RequirementID:      "req:evidence",
		RequirementVersion: "v3",
		ArtifactID:         "art:scan-9",
		ArtifactVersion:    "v3",
		Scope:              []string{"medical-document", "diagnosis"},
		Reason:             "evidence-partial",
		ExpiresTick:        42,
		EvidenceReceipt:    "receipt:quarantine-7",
	}
}

func TestWorkreviewSealIsDeterministicAndOrderIndependent(t *testing.T) {
	a := workreview.Seal(testFinding())
	if err := a.Verify(); err != nil {
		t.Fatalf("sealed finding does not verify: %v", err)
	}
	swapped := testFinding()
	swapped.Scope = []string{"diagnosis", "medical-document"}
	b := workreview.Seal(swapped)
	if a.Digest != b.Digest {
		t.Fatalf("scope order changed the seal: %q vs %q", a.Digest, b.Digest)
	}
	if resealed := workreview.Seal(a); resealed.Digest != a.Digest {
		t.Fatalf("sealing is not idempotent: %q vs %q", a.Digest, resealed.Digest)
	}
	changed := testFinding()
	changed.Reason = "evidence-clear"
	if c := workreview.Seal(changed); c.Digest == a.Digest {
		t.Fatal("a changed reason left the seal unchanged")
	}
	if !strings.HasPrefix(a.Digest, "sha256:") {
		t.Fatalf("seal %q is not a sha256 digest reference", a.Digest)
	}
}

func TestWorkreviewVerifyFailsClosed(t *testing.T) {
	var zero workreview.Finding
	if err := zero.Verify(); err == nil {
		t.Fatal("zero finding verifies: nothing must verify without a seal")
	}
	forged := workreview.Seal(testFinding())
	forged.Reason = "evidence-clear"
	if err := forged.Verify(); err == nil {
		t.Fatal("tampered finding verifies: the seal must bind every field")
	}
	stripped := workreview.Seal(testFinding())
	stripped.Digest = ""
	if err := stripped.Verify(); err == nil {
		t.Fatal("unsealed finding verifies: an empty digest is never valid")
	}
}

func TestWorkreviewClosedVocabulary(t *testing.T) {
	if workreview.ReviewSufficient != "SUFFICIENT" ||
		workreview.ReviewInsufficient != "INSUFFICIENT" ||
		workreview.ReviewMoreInfo != "MORE_INFORMATION_REQUIRED" ||
		workreview.ReviewUnknown != "UNKNOWN" {
		t.Fatal("verdict constants changed value: consumers match on these strings")
	}
	for _, v := range []string{workreview.ReviewSufficient, workreview.ReviewInsufficient, workreview.ReviewMoreInfo, workreview.ReviewUnknown} {
		if !workreview.ValidVerdict(v) {
			t.Errorf("ValidVerdict(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "APPROVED", "more_information_required"} {
		if workreview.ValidVerdict(v) {
			t.Errorf("ValidVerdict(%q) = true, want false", v)
		}
	}
	for _, r := range []string{"evidence-clear", "evidence-contradictory", "evidence-partial", "evidence-unreadable"} {
		if !workreview.ValidReason(r) {
			t.Errorf("ValidReason(%q) = false, want true", r)
		}
	}
	for _, r := range []string{"", "doctor-note", "Evidence-Clear"} {
		if workreview.ValidReason(r) {
			t.Errorf("ValidReason(%q) = true, want false", r)
		}
	}
}
