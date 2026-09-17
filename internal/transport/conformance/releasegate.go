package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ALIGN-064: the default product-slice release gates on named evidence.
// The gate lists the qualifying checks a release must pass; admitting a
// release requires exactly one evidence digest per required gate. A
// missing gate denies the release by name, evidence for an unrequired
// gate is refused rather than banked, and duplicate evidence for one gate
// is refused — the release decision is fully determined by the evidence
// set, and it verifies by digest.

// Release errors.
var (
	ErrReleaseInvalid = errors.New("transport conformance: release evidence is invalid")
	ErrReleaseDenied  = errors.New("transport conformance: release gate denied")
)

// ReleaseGate names the qualifying checks a release must pass.
type ReleaseGate struct {
	Required []string `json:"required"`
}

// ReleaseEvidence pins one gate's passing evidence by digest.
type ReleaseEvidence struct {
	Gate   string `json:"gate"`
	Digest string `json:"digest"`
}

// ReleaseDecision is the deterministic outcome of gating a release.
type ReleaseDecision struct {
	Admitted bool     `json:"admitted"`
	Missing  []string `json:"missing"`
	Digest   string   `json:"digest"`
}

// DefaultReleaseGate is the gate the default product-slice release must
// pass: parity, transport, authorization, chronology, restart safety,
// concurrency safety, localization, and usability proofs.
func DefaultReleaseGate() ReleaseGate {
	return ReleaseGate{Required: []string{
		"parity.ssr_browser",
		"parity.transport",
		"authorization.noninterference",
		"chronology.postgres",
		"restart.restore",
		"safety.concurrent_actions",
		"localization.a11y",
		"usability.zero_override",
	}}
}

// Admit gates a release on its evidence. Every required gate needs
// exactly one non-empty evidence digest; anything else denies or refuses.
func (g ReleaseGate) Admit(evidence []ReleaseEvidence) (ReleaseDecision, error) {
	if len(g.Required) == 0 {
		return ReleaseDecision{}, fmt.Errorf("%w: gate requires no checks", ErrReleaseInvalid)
	}
	required := make(map[string]bool, len(g.Required))
	for _, gate := range g.Required {
		if gate == "" {
			return ReleaseDecision{}, fmt.Errorf("%w: gate names an empty check", ErrReleaseInvalid)
		}
		if required[gate] {
			return ReleaseDecision{}, fmt.Errorf("%w: gate requires %q twice", ErrReleaseInvalid, gate)
		}
		required[gate] = true
	}
	seen := make(map[string]string, len(evidence))
	for _, item := range evidence {
		if !required[item.Gate] {
			return ReleaseDecision{}, fmt.Errorf("%w: evidence for unrequired gate %q", ErrReleaseInvalid, item.Gate)
		}
		if _, ok := seen[item.Gate]; ok {
			return ReleaseDecision{}, fmt.Errorf("%w: duplicate evidence for gate %q", ErrReleaseInvalid, item.Gate)
		}
		if item.Digest == "" {
			return ReleaseDecision{}, fmt.Errorf("%w: gate %q evidence has no digest", ErrReleaseInvalid, item.Gate)
		}
		seen[item.Gate] = item.Digest
	}
	decision := ReleaseDecision{}
	for _, gate := range g.Required {
		if _, ok := seen[gate]; !ok {
			decision.Missing = append(decision.Missing, gate)
		}
	}
	sort.Strings(decision.Missing)
	if len(decision.Missing) > 0 {
		decision.Digest = decision.computeDigest(g.Required)
		return decision, fmt.Errorf("%w: missing %v", ErrReleaseDenied, decision.Missing)
	}
	decision.Admitted = true
	decision.Digest = decision.computeDigest(g.Required)
	return decision, nil
}

func (d ReleaseDecision) computeDigest(gates []string) string {
	required := append([]string(nil), gates...)
	sort.Strings(required)
	b, err := json.Marshal(struct {
		Admitted bool     `json:"admitted"`
		Missing  []string `json:"missing"`
		Required []string `json:"required"`
	}{d.Admitted, d.Missing, required})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDecision reports whether the decision digest matches its content
// against the gate it was admitted under.
func (d ReleaseDecision) VerifyDecision(gate ReleaseGate) error {
	if d.Digest == "" || d.Digest != d.computeDigest(gate.Required) {
		return fmt.Errorf("%w: release decision digest does not match", ErrReleaseInvalid)
	}
	return nil
}
