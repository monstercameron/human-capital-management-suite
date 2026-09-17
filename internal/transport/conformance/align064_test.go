package conformance_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
)

func align064Evidence() []conformance.ReleaseEvidence {
	gate := conformance.DefaultReleaseGate()
	evidence := make([]conformance.ReleaseEvidence, 0, len(gate.Required))
	for _, name := range gate.Required {
		evidence = append(evidence, conformance.ReleaseEvidence{Gate: name, Digest: "sha256:evidence-" + name})
	}
	return evidence
}

// TestTodo_ALIGN_064 proves the default product-slice release gates on
// named evidence: full evidence admits, anything missing denies by name,
// and foreign or duplicate evidence is refused.
func TestTodo_ALIGN_064(t *testing.T) {
	gate := conformance.DefaultReleaseGate()
	decision, err := gate.Admit(align064Evidence())
	if err != nil {
		t.Fatalf("Admit(full): %v", err)
	}
	if !decision.Admitted || len(decision.Missing) != 0 || decision.Digest == "" {
		t.Fatalf("decision = %+v", decision)
	}
	if err := decision.VerifyDecision(gate); err != nil {
		t.Fatalf("VerifyDecision: %v", err)
	}
	// A missing gate denies the release by name.
	partial := align064Evidence()[:len(align064Evidence())-1]
	denied, err := gate.Admit(partial)
	if !errors.Is(err, conformance.ErrReleaseDenied) {
		t.Fatalf("Admit(partial) = %+v, %v, want ErrReleaseDenied", denied, err)
	}
	if len(denied.Missing) != 1 || denied.Admitted {
		t.Fatalf("denied decision = %+v", denied)
	}
}

func TestTodo_ALIGN_064_Property(t *testing.T) {
	gate := conformance.DefaultReleaseGate()
	evidence := align064Evidence()
	first, err := gate.Admit(evidence)
	if err != nil {
		t.Fatal(err)
	}
	// Evidence order never changes the decision.
	reversed := append([]conformance.ReleaseEvidence(nil), evidence...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	second, err := gate.Admit(reversed)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("release decision is order-dependent: %s != %s", first.Digest, second.Digest)
	}
}

func TestTodo_ALIGN_064_Golden(t *testing.T) {
	gate := conformance.DefaultReleaseGate()
	decision, err := gate.Admit(align064Evidence())
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	const wantDigest = "sha256:f921a03c44f1bdfe508b10145f2fdc534088d6b6ebf7b53df76b5dd9cb6df57e"
	if decision.Digest != wantDigest {
		t.Fatalf("release digest=%q want=%q", decision.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_064_Security(t *testing.T) {
	gate := conformance.DefaultReleaseGate()
	// Evidence for an unrequired gate is refused, never banked.
	foreign := append(align064Evidence(), conformance.ReleaseEvidence{Gate: "evil.backdoor", Digest: "sha256:evil"})
	if _, err := gate.Admit(foreign); !errors.Is(err, conformance.ErrReleaseInvalid) {
		t.Fatalf("Admit(foreign) = %v, want ErrReleaseInvalid", err)
	}
	// Duplicate evidence for one gate is refused.
	duplicated := append(align064Evidence(), align064Evidence()[0])
	if _, err := gate.Admit(duplicated); !errors.Is(err, conformance.ErrReleaseInvalid) {
		t.Fatalf("Admit(duplicate) = %v, want ErrReleaseInvalid", err)
	}
	// Empty digests are not evidence.
	empty := align064Evidence()
	empty[0].Digest = ""
	if _, err := gate.Admit(empty); !errors.Is(err, conformance.ErrReleaseInvalid) {
		t.Fatalf("Admit(empty digest) = %v, want ErrReleaseInvalid", err)
	}
}

func TestTodo_ALIGN_064_Integration(t *testing.T) {
	gate := conformance.DefaultReleaseGate()
	// Release evidence binds real qualification artifacts: the string
	// catalog digest and a live semantic digest stand in for two gates.
	evidence := align064Evidence()
	evidence[0].Digest = conformance.CatalogDigest()
	evidence[1].Digest = execute(t, false).SemanticDigest
	decision, err := gate.Admit(evidence)
	if err != nil {
		t.Fatalf("Admit(bound artifacts): %v", err)
	}
	if !decision.Admitted {
		t.Fatalf("decision = %+v", decision)
	}
	if err := decision.VerifyDecision(gate); err != nil {
		t.Fatalf("VerifyDecision(bound): %v", err)
	}
}

func TestTodo_ALIGN_064_Fault(t *testing.T) {
	// An empty gate admits nothing: a release with no required checks is
	// invalid, not vacuously admitted.
	empty := conformance.ReleaseGate{}
	if _, err := empty.Admit(nil); !errors.Is(err, conformance.ErrReleaseInvalid) {
		t.Fatalf("Admit(empty gate) = %v, want ErrReleaseInvalid", err)
	}
	// A gate naming one check twice is invalid.
	doubled := conformance.ReleaseGate{Required: []string{"parity.transport", "parity.transport"}}
	if _, err := doubled.Admit(nil); !errors.Is(err, conformance.ErrReleaseInvalid) {
		t.Fatalf("Admit(doubled gate) = %v, want ErrReleaseInvalid", err)
	}
	// A forged decision digest does not verify.
	gate := conformance.DefaultReleaseGate()
	decision, err := gate.Admit(align064Evidence())
	if err != nil {
		t.Fatal(err)
	}
	decision.Digest = "sha256:forged"
	if err := decision.VerifyDecision(gate); !errors.Is(err, conformance.ErrReleaseInvalid) {
		t.Fatalf("VerifyDecision(forged) = %v, want ErrReleaseInvalid", err)
	}
}

func TestTodo_ALIGN_064_Conformance(t *testing.T) {
	gate := conformance.DefaultReleaseGate()
	// The default gate requires the eight qualifying proofs: parity,
	// transport, authorization, chronology, restart, concurrency safety,
	// localization, and usability.
	want := map[string]bool{
		"parity.ssr_browser": true, "parity.transport": true,
		"authorization.noninterference": true, "chronology.postgres": true,
		"restart.restore": true, "safety.concurrent_actions": true,
		"localization.a11y": true, "usability.zero_override": true,
	}
	if len(gate.Required) != len(want) {
		t.Fatalf("default gate requires %v", gate.Required)
	}
	for _, name := range gate.Required {
		if !want[name] {
			t.Fatalf("default gate requires unexpected %q", name)
		}
	}
	decision, err := gate.Admit(align064Evidence())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Admitted || len(decision.Missing) != 0 {
		t.Fatalf("full evidence did not admit: %+v", decision)
	}
}

func FuzzTodo_ALIGN_064_Fuzz(f *testing.F) {
	f.Add("parity.transport", "sha256:evidence-parity.transport")
	f.Fuzz(func(t *testing.T, gateName, digest string) {
		gate := conformance.DefaultReleaseGate()
		evidence := align064Evidence()
		evidence[0] = conformance.ReleaseEvidence{Gate: gateName, Digest: digest}
		first, firstErr := gate.Admit(evidence)
		second, secondErr := gate.Admit(evidence)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("admission is not deterministic for %q", gateName)
		}
		if firstErr == nil && first.Digest != second.Digest {
			t.Fatalf("release digest is not deterministic for %q", gateName)
		}
	})
}
