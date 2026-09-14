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

// P1AManifest is the schema for definitions/planning/gates/p1a-manifest.yaml
// (NEXT-002). Field order is fixed by struct declaration order, which is
// also the order json.Marshal emits them in - that fixed order is what
// makes CanonicalDigest stable across re-serialization.
type P1AManifest struct {
	SchemaVersion           int                     `yaml:"schema_version" json:"schema_version"`
	Release                 string                  `yaml:"release" json:"release"`
	TodoID                  string                  `yaml:"todo_id" json:"todo_id"`
	SignedDate              string                  `yaml:"signed_date" json:"signed_date"`
	FreshnessWindowDays     int                     `yaml:"freshness_window_days" json:"freshness_window_days"`
	Workflow                string                  `yaml:"workflow" json:"workflow"`
	Intents                 []Intent                `yaml:"intents" json:"intents"`
	EffectCeiling           []string                `yaml:"effect_ceiling" json:"effect_ceiling"`
	Capabilities            []Capability            `yaml:"capabilities" json:"capabilities"`
	Commands                []Command               `yaml:"commands" json:"commands"`
	Migrations              Migrations              `yaml:"migrations" json:"migrations"`
	QualificationDecisions  []QualificationDecision `yaml:"qualification_decisions" json:"qualification_decisions"`
	Evidence                []EvidenceEntry         `yaml:"evidence" json:"evidence"`
	ForbiddenImportPrefixes []string                `yaml:"forbidden_import_prefixes" json:"forbidden_import_prefixes"`
	// ForbiddenEffects is the machine-readable form of EffectCeiling: the
	// closed effect vocabulary internal/commercial.AuthorityBoundary also
	// forbids, so the commercial view and the release manifest share one
	// identity (see P1AForbiddenEffects).
	ForbiddenEffects []string `yaml:"forbidden_effects" json:"forbidden_effects"`
	// SelectionBindings pins every selecting and readiness artifact this
	// release depends on by path and digest (see RequiredSelectionBindingTodoIDs).
	// The bindings are signed with the rest of the manifest; whether the
	// selections they pin are actually complete is computed from those
	// artifacts' own gates by tools/planning/gateevidence/selectionbind, never
	// asserted here.
	SelectionBindings []SelectionBinding `yaml:"selection_bindings" json:"selection_bindings"`
	Signature         *Signature         `yaml:"signature,omitempty" json:"signature,omitempty"`
}

// Digest kinds a SelectionBinding may use. CANONICAL_JSON is the artifact's
// own CanonicalDigest (the JSON projection its signature covers, so a
// comment-only edit does not stale the binding); FILE_SHA256 hashes the raw
// file bytes, for artifacts that have no canonical projection.
const (
	DigestKindCanonicalJSON = "CANONICAL_JSON"
	DigestKindFileSHA256    = "FILE_SHA256"
)

// SelectionBinding binds one selecting artifact by repository-relative path
// and digest.
type SelectionBinding struct {
	TodoID     string `yaml:"todo_id" json:"todo_id"`
	Path       string `yaml:"path" json:"path"`
	DigestKind string `yaml:"digest_kind" json:"digest_kind"`
	Digest     string `yaml:"digest" json:"digest"`
}

// RequiredSelectionBindingTodoIDs is the exact, ordered set of todos a P1A
// manifest must bind: NEXT-002's Depends in their declared order, then
// THREAT-001, whose release decision blocks any Phase 1 release.
var RequiredSelectionBindingTodoIDs = []string{
	"PHASE-001", "SELECT-001", "SELECT-002", "CUSTOMER-001", "TOPOLOGY-001", "COMMERCIAL-001", "THREAT-001",
}

// P1AZeroEffectCeiling is next-steps.md's P1A effect ceiling, verbatim.
var P1AZeroEffectCeiling = []string{
	"zero worker, employment, assignment, organization, position, compensation or budget mutations",
	"zero reservations, WorkItems and timers",
	"zero committed external effects, provider writes and MessageIntents",
}

// P1AForbiddenEffects is the closed effect vocabulary P1A may never produce:
// workforce mutation, reservation, WorkItem, timer, message, outbox effect
// and provider write. It is the same vocabulary, in the same order, as
// internal/commercial.DefaultPilotCommercialPackage().Authority.ForbiddenEffects.
var P1AForbiddenEffects = []string{
	"domain_mutation", "reservation", "work_item", "timer", "message", "outbox", "provider_write",
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ValidBindingPath reports whether p is a clean repository-relative path:
// forward slashes, no leading slash or drive, no "." or ".." segment. A
// binding path that can escape the repository root is refused rather than
// resolved.
func ValidBindingPath(p string) bool {
	if p == "" || strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.Contains(p, ":") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func validateSelectionBindings(bindings []SelectionBinding, add func(field, issue string)) {
	if len(bindings) != len(RequiredSelectionBindingTodoIDs) {
		add("selection_bindings", fmt.Sprintf("got %d bindings, want exactly %d (%s)", len(bindings), len(RequiredSelectionBindingTodoIDs), strings.Join(RequiredSelectionBindingTodoIDs, ", ")))
	}
	for i, b := range bindings {
		field := fmt.Sprintf("selection_bindings[%d]", i)
		if i < len(RequiredSelectionBindingTodoIDs) && b.TodoID != RequiredSelectionBindingTodoIDs[i] {
			add(field+".todo_id", fmt.Sprintf("got %q, want %q - bindings are ordered and none may be omitted or substituted", b.TodoID, RequiredSelectionBindingTodoIDs[i]))
		}
		if !ValidBindingPath(b.Path) {
			add(field+".path", fmt.Sprintf("%q is not a clean repository-relative path", b.Path))
		}
		if b.DigestKind != DigestKindCanonicalJSON && b.DigestKind != DigestKindFileSHA256 {
			add(field+".digest_kind", fmt.Sprintf("unknown digest kind %q", b.DigestKind))
		}
		if !isHex64(b.Digest) {
			add(field+".digest", "missing or not a lowercase 64-hex-character sha256 digest")
		}
	}
}

// Intent is one of the eight P1A executable intent contracts
// (next-steps.md "P1A - paid observation, preflight and simulation").
type Intent struct {
	Order       int      `yaml:"order" json:"order"`
	ID          string   `yaml:"id" json:"id"`
	Modes       []string `yaml:"modes,omitempty" json:"modes,omitempty"`
	Disposition string   `yaml:"disposition" json:"disposition"`
}

// Capability is one bootstrap capability-registry entry
// (internal/capability/bootstrap.go bootstrapDefinitions, cross-checked by
// TOOL-004 against tools/gen/schemaflux/testdata/capability_manifest.yaml).
type Capability struct {
	ID          string `yaml:"id" json:"id"`
	Version     int    `yaml:"version" json:"version"`
	OwnerDomain string `yaml:"owner_domain" json:"owner_domain"`
	EffectClass string `yaml:"effect_class" json:"effect_class"`
	TestRef     string `yaml:"test_ref" json:"test_ref"`
}

// Command is one of the four P1A composition-root binaries (NEXT-004).
type Command struct {
	Name    string `yaml:"name" json:"name"`
	Package string `yaml:"package" json:"package"`
	Purpose string `yaml:"purpose" json:"purpose"`
}

// MigrationFile is one embedded Goose migration (migrations/migrations.go
// Files()), named and checksummed exactly as that function computes it.
type MigrationFile struct {
	Version  int64  `yaml:"version" json:"version"`
	Name     string `yaml:"name" json:"name"`
	Checksum string `yaml:"checksum" json:"checksum"`
}

// MigrationGap records a skipped version number in the migration sequence,
// so an unexplained gap is a recorded fact rather than a silent omission.
type MigrationGap struct {
	Version int64  `yaml:"version" json:"version"`
	Reason  string `yaml:"reason" json:"reason"`
}

// Migrations is the P1A manifest's migration closure.
type Migrations struct {
	Dialect string          `yaml:"dialect" json:"dialect"`
	Files   []MigrationFile `yaml:"files" json:"files"`
	Gaps    []MigrationGap  `yaml:"gaps,omitempty" json:"gaps,omitempty"`
}

// QualificationDecision is one toolchain qualification decision the P1A
// manifest depends on (TOOL-004, TOOL-008, UX-QUAL-001, WF-RUN-000).
type QualificationDecision struct {
	TodoID   string `yaml:"todo_id" json:"todo_id"`
	Decision string `yaml:"decision" json:"decision"`
	Record   string `yaml:"record" json:"record"`
}

// EvidenceEntry names exactly one Test function, in exactly one package,
// that substantiates exactly one todo's manifest inclusion. Compile
// resolves each entry to a Verdict.
type EvidenceEntry struct {
	TodoID  string `yaml:"todo_id" json:"todo_id"`
	Test    string `yaml:"test" json:"test"`
	Package string `yaml:"package" json:"package"`
}

// Signature is an Ed25519 signature over a manifest's CanonicalDigest.
type Signature struct {
	Algorithm  string `yaml:"algorithm" json:"algorithm"`
	PublicKey  string `yaml:"public_key" json:"public_key"`
	Value      string `yaml:"value" json:"value"`
	KeyFixture string `yaml:"key_fixture" json:"key_fixture"`
}

// LoadP1AManifest reads and parses a P1A manifest YAML file.
func LoadP1AManifest(path string) (*P1AManifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m P1AManifest
	if err := yaml.Unmarshal(content, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &m, nil
}

// Violation names one manifest defect.
type Violation struct {
	Field string
	Issue string
}

func (v Violation) String() string { return fmt.Sprintf("%s: %s", v.Field, v.Issue) }

// Validate returns every structural violation on m. It does not verify the
// signature (see Verify) or evidence freshness (see Compile).
func (m P1AManifest) Validate() []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if m.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	if m.Release != "P1A" {
		add("release", fmt.Sprintf("must be P1A, got %q - one blended release manifest is refused", m.Release))
	}
	if m.TodoID == "" {
		add("todo_id", "missing")
	}
	if m.SignedDate == "" {
		add("signed_date", "missing")
	}
	if m.FreshnessWindowDays <= 0 {
		add("freshness_window_days", "must be positive")
	}
	if len(m.Intents) == 0 {
		add("intents", "missing")
	}
	if len(m.EffectCeiling) == 0 {
		add("effect_ceiling", "missing - a P1A manifest must state its zero-effect ceiling")
	} else if !equalStrings(m.EffectCeiling, P1AZeroEffectCeiling) {
		add("effect_ceiling", "differs from next-steps.md's P1A zero-effect ceiling")
	}
	if !equalStrings(m.ForbiddenEffects, P1AForbiddenEffects) {
		add("forbidden_effects", fmt.Sprintf("got %v, want exactly %v", m.ForbiddenEffects, P1AForbiddenEffects))
	}
	for _, intent := range m.Intents {
		for _, mode := range intent.Modes {
			if mode == "EXECUTE" {
				add("intents", fmt.Sprintf("%s grants EXECUTE; P1A grants no write mode before Gate A", intent.ID))
			}
		}
		if intent.Disposition != "INCLUDED" {
			add("intents", fmt.Sprintf("%s has disposition %q; a P1A manifest includes no unbound or deferred intent", intent.ID, intent.Disposition))
		}
	}
	validateSelectionBindings(m.SelectionBindings, add)
	if len(m.Capabilities) == 0 {
		add("capabilities", "missing")
	}
	for _, c := range m.Capabilities {
		if c.EffectClass != "READ_ONLY" {
			add("capabilities", fmt.Sprintf("%s: effect_class %q is not READ_ONLY; P1A grants no other effect class", c.ID, c.EffectClass))
		}
	}
	if len(m.Commands) == 0 {
		add("commands", "missing")
	}
	if len(m.Migrations.Files) == 0 {
		add("migrations", "missing")
	}
	if len(m.QualificationDecisions) == 0 {
		add("qualification_decisions", "missing")
	}
	if len(m.Evidence) == 0 {
		add("evidence", "missing")
	}
	if m.Signature == nil {
		add("signature", "missing - a P1A manifest must be signed")
	} else {
		if m.Signature.Algorithm != "ed25519" {
			add("signature.algorithm", fmt.Sprintf("unsupported algorithm %q", m.Signature.Algorithm))
		}
		if m.Signature.PublicKey == "" {
			add("signature.public_key", "missing")
		}
		if m.Signature.Value == "" {
			add("signature.value", "missing")
		}
	}

	return violations
}

// digestPayload is the canonical projection hashed by CanonicalDigest and
// signed by Sign/Verify: every manifest field except the signature itself,
// so a signature can never cover its own bytes.
type digestPayload struct {
	SchemaVersion           int                     `json:"schema_version"`
	Release                 string                  `json:"release"`
	TodoID                  string                  `json:"todo_id"`
	SignedDate              string                  `json:"signed_date"`
	FreshnessWindowDays     int                     `json:"freshness_window_days"`
	Workflow                string                  `json:"workflow"`
	Intents                 []Intent                `json:"intents"`
	EffectCeiling           []string                `json:"effect_ceiling"`
	Capabilities            []Capability            `json:"capabilities"`
	Commands                []Command               `json:"commands"`
	Migrations              Migrations              `json:"migrations"`
	QualificationDecisions  []QualificationDecision `json:"qualification_decisions"`
	Evidence                []EvidenceEntry         `json:"evidence"`
	ForbiddenImportPrefixes []string                `json:"forbidden_import_prefixes"`
	ForbiddenEffects        []string                `json:"forbidden_effects"`
	SelectionBindings       []SelectionBinding      `json:"selection_bindings"`
}

func (m P1AManifest) payload() digestPayload {
	return digestPayload{
		SchemaVersion:           m.SchemaVersion,
		Release:                 m.Release,
		TodoID:                  m.TodoID,
		SignedDate:              m.SignedDate,
		FreshnessWindowDays:     m.FreshnessWindowDays,
		Workflow:                m.Workflow,
		Intents:                 m.Intents,
		EffectCeiling:           m.EffectCeiling,
		Capabilities:            m.Capabilities,
		Commands:                m.Commands,
		Migrations:              m.Migrations,
		QualificationDecisions:  m.QualificationDecisions,
		Evidence:                m.Evidence,
		ForbiddenImportPrefixes: m.ForbiddenImportPrefixes,
		ForbiddenEffects:        m.ForbiddenEffects,
		SelectionBindings:       m.SelectionBindings,
	}
}

// CanonicalDigest returns the hex-encoded sha256 digest of m's canonical
// JSON projection (every field except Signature). Two manifests with
// identical content but a different (or absent) signature digest
// identically; this is exactly the value Sign covers and Verify checks.
func (m P1AManifest) CanonicalDigest() (string, error) {
	b, err := json.Marshal(m.payload())
	if err != nil {
		return "", fmt.Errorf("marshal canonical manifest: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
