package release

// REV-088-01: the default product-slice release gate (ALIGN-064) decides a
// real release. Build collects the gate's eight evidence digests as required
// bundle inputs and refuses a bundle missing one; the admission decision is
// pinned in the manifest and VerifyBundle re-checks it, so a bundle can
// never reflect a release the gate would deny.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
)

var (
	// ErrProductGateEvidence reports a bundle whose product-gate evidence
	// set the release gate refuses: a missing gate denies by name.
	ErrProductGateEvidence = errors.New("release: product-gate evidence is incomplete")
	// ErrProductGateDecision reports a manifest whose pinned gate decision
	// does not verify against its recorded evidence.
	ErrProductGateDecision = errors.New("release: product-gate decision does not verify")
)

// ProductGateFileName is the gate-evidence report copied into every bundle
// under policy/, alongside the four CICD-001 policy reports.
const ProductGateFileName = "policy/productgate.json"

// ProductGateRecord pins the release-gate decision a bundle was admitted
// under: the required gates, one evidence digest per gate, and the decision
// digest Admit computed for exactly that set.
type ProductGateRecord struct {
	Required []string                      `json:"required"`
	Evidence []conformance.ReleaseEvidence `json:"evidence"`
	Decision string                        `json:"decision"`
}

// ProductGateGates returns the eight ALIGN-064 gates every bundle must prove,
// in their canonical order.
func ProductGateGates() []string {
	return conformance.DefaultReleaseGate().Required
}

// AdmitProductGate runs the default product-slice release gate over the
// supplied gate-to-digest evidence. A gate without a digest is absent, so
// Admit denies it by name; the denial (or refusal) is wrapped so callers can
// distinguish a gate denial from any other build failure with errors.Is
// against conformance.ErrReleaseDenied.
func AdmitProductGate(evidence map[string]string) (conformance.ReleaseDecision, []conformance.ReleaseEvidence, error) {
	gate := conformance.DefaultReleaseGate()
	// Pass the complete caller-supplied set through the conformance gate so
	// evidence for an unrequired name cannot be silently banked or ignored.
	// Sorting keeps the sealed record deterministic despite Go map iteration.
	names := make([]string, 0, len(evidence))
	for name := range evidence {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]conformance.ReleaseEvidence, 0, len(names))
	for _, name := range names {
		items = append(items, conformance.ReleaseEvidence{Gate: name, Digest: strings.TrimSpace(evidence[name])})
	}
	decision, err := gate.Admit(items)
	if err != nil {
		return conformance.ReleaseDecision{}, nil, fmt.Errorf("%w: %w", ErrProductGateEvidence, err)
	}
	return decision, items, nil
}

// SealProductGate pins an admitted decision for the manifest.
func SealProductGate(decision conformance.ReleaseDecision, evidence []conformance.ReleaseEvidence) ProductGateRecord {
	required := ProductGateGates()
	items := append([]conformance.ReleaseEvidence(nil), evidence...)
	sort.Slice(items, func(i, j int) bool { return items[i].Gate < items[j].Gate })
	return ProductGateRecord{Required: required, Evidence: items, Decision: decision.Digest}
}

// MarshalProductGate renders the canonical policy/productgate.json bytes for
// a sealed record.
func MarshalProductGate(record ProductGateRecord) ([]byte, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("release: marshal product-gate record: %w", err)
	}
	return append(data, '\n'), nil
}

// VerifyProductGateRecord re-checks a manifest's pinned gate decision: the
// required set must be exactly the default product-slice gate, the recorded
// evidence must admit, and the admitted digest must equal the pinned one. A
// bundle whose gate proof is missing, narrowed, or tampered with is refused.
func VerifyProductGateRecord(record *ProductGateRecord) error {
	if record == nil {
		return fmt.Errorf("%w: bundle carries no product-gate record", ErrProductGateDecision)
	}
	want := ProductGateGates()
	if len(record.Required) != len(want) {
		return fmt.Errorf("%w: record requires %d gates, the product gate requires %d", ErrProductGateDecision, len(record.Required), len(want))
	}
	for i := range want {
		if record.Required[i] != want[i] {
			return fmt.Errorf("%w: record gate %d is %q, want %q", ErrProductGateDecision, i, record.Required[i], want[i])
		}
	}
	decision, err := conformance.DefaultReleaseGate().Admit(record.Evidence)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrProductGateDecision, err)
	}
	if !decision.Admitted {
		return fmt.Errorf("%w: recorded evidence does not admit", ErrProductGateDecision)
	}
	if decision.Digest != record.Decision {
		return fmt.Errorf("%w: decision digest mismatch", ErrProductGateDecision)
	}
	return nil
}
