package gateevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// P1BTemplate is the schema for definitions/planning/gates/p1b-template.yaml
// (NEXT-002's "separately signed P1B template"): the six P1B candidate
// write/approval contracts, named only - never activatable from this
// document alone. It binds the exact P1A manifest it follows by path and
// CanonicalDigest, so the P1B template goes stale the moment P1A (or any
// selection P1A binds) changes.
type P1BTemplate struct {
	SchemaVersion         int                `yaml:"schema_version" json:"schema_version"`
	Release               string             `yaml:"release" json:"release"`
	TodoID                string             `yaml:"todo_id" json:"todo_id"`
	P1AManifest           P1AManifestBinding `yaml:"p1a_manifest" json:"p1a_manifest"`
	Contracts             []Contract         `yaml:"contracts" json:"contracts"`
	RequiresGateADecision bool               `yaml:"requires_gate_a_decision" json:"requires_gate_a_decision"`
	GateADecision         string             `yaml:"gate_a_decision" json:"gate_a_decision"`
	AuthorityDigest       string             `yaml:"authority_digest" json:"authority_digest"`
	Signature             *Signature         `yaml:"signature,omitempty" json:"signature,omitempty"`
}

// P1AManifestBinding pins the P1A manifest a P1B template amends.
type P1AManifestBinding struct {
	Path   string `yaml:"path" json:"path"`
	Digest string `yaml:"digest" json:"digest"`
}

// P1AManifestPath is the repository-relative path of the signed P1A manifest.
const P1AManifestPath = "definitions/planning/gates/p1a-manifest.yaml"

// Contract is one P1B candidate write/approval contract.
type Contract struct {
	Order            int    `yaml:"order" json:"order"`
	ID               string `yaml:"id" json:"id"`
	Mode             string `yaml:"mode,omitempty" json:"mode,omitempty"`
	ActivationStatus string `yaml:"activation_status" json:"activation_status"`
}

// ActivationBlocked is the only ActivationStatus a P1B template contract may
// carry before a signed Gate A PROCEED decision exists.
const ActivationBlocked = "BLOCKED_PENDING_GATE_A"

// P1BCandidateContracts is next-steps.md's exact, ordered six-contract P1B
// list. Only promote_worker carries a mode, EXECUTE, which P1A never grants.
var P1BCandidateContracts = []Contract{
	{Order: 1, ID: "promote_worker", Mode: "EXECUTE"},
	{Order: 2, ID: "change_base_pay"},
	{Order: 3, ID: "reserve_compensation_budget"},
	{Order: 4, ID: "release_compensation_budget"},
	{Order: 5, ID: "approve_proposal"},
	{Order: 6, ID: "reject_proposal"},
}

// LoadP1BTemplate reads and parses a P1B template YAML file.
func LoadP1BTemplate(path string) (*P1BTemplate, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var t P1BTemplate
	if err := yaml.Unmarshal(content, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &t, nil
}

// ActivationBlockers names every reason t cannot activate. Activation needs
// a recorded Gate A PROCEED decision plus a NEW authority digest: a
// well-formed sha256 that is not merely the P1A manifest's own identity
// re-presented as authority.
func (t P1BTemplate) ActivationBlockers() []string {
	var blockers []string
	if !t.RequiresGateADecision {
		blockers = append(blockers, "requires_gate_a_decision is false - a template that does not require Gate A can never be activated")
	}
	if t.GateADecision != "PROCEED" {
		blockers = append(blockers, fmt.Sprintf("gate_a_decision is %q, not a signed Gate A PROCEED", t.GateADecision))
	}
	switch {
	case !isHex64(t.AuthorityDigest):
		blockers = append(blockers, "authority_digest is not a new 64-hex-character authority digest")
	case t.AuthorityDigest == t.P1AManifest.Digest:
		blockers = append(blockers, "authority_digest reuses the P1A manifest digest; P1B requires a new authority digest")
	}
	return blockers
}

// CanActivate reports whether t could be activated: only when
// ActivationBlockers is empty. Neither checked-in document ever populates
// the Gate A decision or authority digest; this proves the template cannot
// self-activate, matching next-steps.md P1B: "cannot activate without Gate A
// plus a new authority digest."
func (t P1BTemplate) CanActivate() bool {
	return len(t.ActivationBlockers()) == 0
}

// Validate returns every structural violation on t.
func (t P1BTemplate) Validate() []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if t.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	if t.Release != "P1B" {
		add("release", fmt.Sprintf("must be P1B, got %q", t.Release))
	}
	if t.TodoID != "NEXT-002" {
		add("todo_id", fmt.Sprintf("must be NEXT-002, got %q", t.TodoID))
	}
	if t.P1AManifest.Path != P1AManifestPath {
		add("p1a_manifest.path", fmt.Sprintf("must be %s, got %q", P1AManifestPath, t.P1AManifest.Path))
	}
	if !isHex64(t.P1AManifest.Digest) {
		add("p1a_manifest.digest", "missing or not a lowercase 64-hex-character sha256 digest")
	}
	if len(t.Contracts) == 0 {
		add("contracts", "missing")
	}
	if len(t.Contracts) != len(P1BCandidateContracts) {
		add("contracts", fmt.Sprintf("got %d contracts, want exactly the %d P1B candidates", len(t.Contracts), len(P1BCandidateContracts)))
	}
	for i, c := range t.Contracts {
		if i < len(P1BCandidateContracts) {
			want := P1BCandidateContracts[i]
			if c.Order != want.Order || c.ID != want.ID || c.Mode != want.Mode {
				add(fmt.Sprintf("contracts[%d]", i), fmt.Sprintf("got %d/%s/%q, want %d/%s/%q", c.Order, c.ID, c.Mode, want.Order, want.ID, want.Mode))
			}
		}
		if c.ActivationStatus != ActivationBlocked {
			add("contracts", fmt.Sprintf("%s: activation_status %q must be %s before Gate A", c.ID, c.ActivationStatus, ActivationBlocked))
		}
	}
	if !t.RequiresGateADecision {
		add("requires_gate_a_decision", "must be true - a P1B template that does not require Gate A is not a template")
	}
	if t.CanActivate() {
		add("gate_a_decision/authority_digest", "template is activatable; a checked-in P1B template must never be")
	}
	if t.Signature == nil {
		add("signature", "missing - the P1B template must be separately signed")
	} else if t.Signature.Algorithm != "ed25519" || t.Signature.PublicKey == "" || t.Signature.Value == "" {
		add("signature", "incomplete ed25519 signature")
	}

	return violations
}

// p1bDigestPayload is the canonical projection hashed by
// P1BTemplate.CanonicalDigest: every field except the signature.
type p1bDigestPayload struct {
	SchemaVersion         int                `json:"schema_version"`
	Release               string             `json:"release"`
	TodoID                string             `json:"todo_id"`
	P1AManifest           P1AManifestBinding `json:"p1a_manifest"`
	Contracts             []Contract         `json:"contracts"`
	RequiresGateADecision bool               `json:"requires_gate_a_decision"`
	GateADecision         string             `json:"gate_a_decision"`
	AuthorityDigest       string             `json:"authority_digest"`
}

// CanonicalDigest returns the hex sha256 of t's canonical JSON projection
// (every field except Signature).
func (t P1BTemplate) CanonicalDigest() (string, error) {
	b, err := json.Marshal(p1bDigestPayload{
		SchemaVersion:         t.SchemaVersion,
		Release:               t.Release,
		TodoID:                t.TodoID,
		P1AManifest:           t.P1AManifest,
		Contracts:             t.Contracts,
		RequiresGateADecision: t.RequiresGateADecision,
		GateADecision:         t.GateADecision,
		AuthorityDigest:       t.AuthorityDigest,
	})
	if err != nil {
		return "", fmt.Errorf("marshal canonical P1B template: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// VerifyP1BTemplateSignature recomputes t's CanonicalDigest and checks it
// against t.Signature, with the same error/false semantics as
// VerifyManifestSignature.
func VerifyP1BTemplateSignature(t P1BTemplate) (bool, error) {
	if t.Signature == nil {
		return false, fmt.Errorf("P1B template has no signature")
	}
	if t.Signature.Algorithm != "ed25519" {
		return false, fmt.Errorf("unsupported signature algorithm %q", t.Signature.Algorithm)
	}
	digest, err := t.CanonicalDigest()
	if err != nil {
		return false, err
	}
	return VerifyDigestSignature(t.Signature.PublicKey, digest, t.Signature.Value)
}

// capabilityBase returns the short contract name of a P1A intent or
// capability id: "hcmnext.people.promote_worker/v1" -> "promote_worker".
func capabilityBase(id string) string {
	base := id
	if i := strings.Index(base, "/"); i >= 0 {
		base = base[:i]
	}
	return base[strings.LastIndex(base, ".")+1:]
}

// CheckDisjoint returns every way p1a and p1b overlap: a P1B (contract,
// mode) tuple P1A also grants, a P1B contract P1A lists as a capability, or
// a template bound to a different P1A manifest than p1a. Only promote_worker
// is named by both, and only in disjoint modes.
func CheckDisjoint(p1a P1AManifest, p1b P1BTemplate) []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	type tuple struct{ id, mode string }
	granted := map[tuple]bool{}
	for _, intent := range p1a.Intents {
		base := capabilityBase(intent.ID)
		if len(intent.Modes) == 0 {
			granted[tuple{base, ""}] = true
		}
		for _, mode := range intent.Modes {
			granted[tuple{base, mode}] = true
		}
	}
	capabilities := map[string]bool{}
	for _, c := range p1a.Capabilities {
		capabilities[capabilityBase(c.ID)] = true
	}
	for _, c := range p1b.Contracts {
		if granted[tuple{c.ID, c.Mode}] {
			add("contracts", fmt.Sprintf("%s (mode %q) is granted by both P1A and P1B - not disjoint", c.ID, c.Mode))
		}
		if c.Mode == "" && capabilities[c.ID] {
			add("contracts", fmt.Sprintf("%s is a P1B write contract but P1A lists it as a capability", c.ID))
		}
	}
	digest, err := p1a.CanonicalDigest()
	if err != nil {
		add("p1a_manifest.digest", err.Error())
	} else if p1b.P1AManifest.Digest != digest {
		add("p1a_manifest.digest", fmt.Sprintf("P1B template binds P1A digest %q, but the P1A manifest digests to %q - stale ordering", p1b.P1AManifest.Digest, digest))
	}
	return violations
}
