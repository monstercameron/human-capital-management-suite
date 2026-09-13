package pilotblueprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// WorkstreamKind is the closed, ordered vocabulary of pilot workstreams this
// blueprint's RACI and implementation graph must cover exactly once,
// discovery through hypercare (GREEN), matching CUSTOMER-001's own INTENT
// CONTEXT verbatim: "make customer discovery, configuration, data, identity,
// integration, legal review, testing, training, cutover and steady-state
// ownership explicit". WorkstreamHypercare is GREEN's "hypercare": steady
// state ownership after cutover.
type WorkstreamKind string

// The exact, closed set of pilot workstreams, discovery through hypercare.
const (
	WorkstreamDiscovery     WorkstreamKind = "DISCOVERY"
	WorkstreamConfiguration WorkstreamKind = "CONFIGURATION"
	WorkstreamData          WorkstreamKind = "DATA"
	WorkstreamIdentity      WorkstreamKind = "IDENTITY"
	WorkstreamIntegration   WorkstreamKind = "INTEGRATION"
	WorkstreamLegalReview   WorkstreamKind = "LEGAL_REVIEW"
	WorkstreamTesting       WorkstreamKind = "TESTING"
	WorkstreamTraining      WorkstreamKind = "TRAINING"
	WorkstreamCutover       WorkstreamKind = "CUTOVER"
	WorkstreamHypercare     WorkstreamKind = "HYPERCARE"
)

// AllWorkstreamKinds returns the exact, ordered discovery-through-hypercare
// sequence GREEN requires. [Blueprint.Validate] checks Workstreams against
// this slice position-by-position, not merely as an unordered set, so
// reordering or dropping a stage is a structural violation.
func AllWorkstreamKinds() []WorkstreamKind {
	return []WorkstreamKind{
		WorkstreamDiscovery, WorkstreamConfiguration, WorkstreamData, WorkstreamIdentity,
		WorkstreamIntegration, WorkstreamLegalReview, WorkstreamTesting, WorkstreamTraining,
		WorkstreamCutover, WorkstreamHypercare,
	}
}

// Party is the closed vocabulary RED's "customer / HCM Next / provider
// owner" clause names: the design partner (none selected yet - see doc.go),
// the platform itself, and the incumbent system
// tools/planning/pilotprovider topologizes.
type Party string

// The exact, closed set of parties.
const (
	PartyCustomer Party = "CUSTOMER"
	PartyHCMNext  Party = "HCM_NEXT"
	PartyProvider Party = "PROVIDER"
)

// AllParties is the exact, closed set every workstream's Owners must cover
// once each.
func AllParties() []Party { return []Party{PartyCustomer, PartyHCMNext, PartyProvider} }

var validParties = map[Party]bool{PartyCustomer: true, PartyHCMNext: true, PartyProvider: true}

// RACIRole is the classic closed Responsible/Accountable/Consulted/Informed
// vocabulary.
type RACIRole string

// The exact, closed set of RACI roles.
const (
	Responsible RACIRole = "RESPONSIBLE"
	Accountable RACIRole = "ACCOUNTABLE"
	Consulted   RACIRole = "CONSULTED"
	Informed    RACIRole = "INFORMED"
)

var validRACIRoles = map[RACIRole]bool{
	Responsible: true, Accountable: true, Consulted: true, Informed: true,
}

// OwnerRole assigns one party's RACI role and templated role title for one
// workstream (RED: "customer / HCM Next / provider owner"). RoleTitle is a
// role description (e.g. "Design-Partner Executive Sponsor"), never a named
// human: naming a specific human belongs to a tenant instantiation's
// CustomerFacts, never to this reusable template (REFACTOR).
type OwnerRole struct {
	Party     Party    `yaml:"party" json:"party"`
	RACIRole  RACIRole `yaml:"raci_role" json:"raci_role"`
	RoleTitle string   `yaml:"role_title" json:"role_title"`
}

// Artifact names one input or output artifact a workstream consumes or
// produces (RED: "input/output artifact").
type Artifact struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Ref         string `yaml:"ref" json:"ref"`
}

// Stop/go actions: the same closed vocabulary
// tools/planning/pilotjurisdiction and tools/planning/pilotprovider use for
// their own stop/reselect thresholds.
const (
	ActionProceed       = "PROCEED"
	ActionReselectWedge = "RESELECT_WEDGE"
	ActionStop          = "STOP"
)

var validActions = map[string]bool{
	ActionProceed: true, ActionReselectWedge: true, ActionStop: true,
}

// StopGoGate names the measurable condition that turns one Dependency's
// evidence into a proceed/reselect/stop decision (GREEN: "binds every
// dependency to exact evidence and a stop/go gate").
type StopGoGate struct {
	Metric    string `yaml:"metric" json:"metric"`
	Threshold string `yaml:"threshold" json:"threshold"`
	Action    string `yaml:"action" json:"action"`
}

// Dependency is one prerequisite a workstream cannot proceed without (RED:
// "prerequisite"), bound to the exact party who owes it, the exact evidence
// a caller can inspect, and a stop/go gate (GREEN: "binds every dependency
// to exact evidence and a stop/go gate").
type Dependency struct {
	Description string     `yaml:"description" json:"description"`
	Party       Party      `yaml:"party" json:"party"`
	EvidenceRef string     `yaml:"evidence_ref" json:"evidence_ref"`
	Gate        StopGoGate `yaml:"gate" json:"gate"`
}

// Timing declares one workstream's due date and staleness expiry as
// relative offsets from pilot kickoff (RED: "due/expiry"). Relative offsets,
// not absolute dates, are what keep this schema reusable across tenants
// (REFACTOR): a tenant instantiation binds these offsets to its own kickoff
// date; it never edits them.
type Timing struct {
	DueOffsetDays    int `yaml:"due_offset_days" json:"due_offset_days"`
	ExpiryOffsetDays int `yaml:"expiry_offset_days" json:"expiry_offset_days"`
}

// Workstream is one discovery-through-hypercare stage of the pilot
// implementation graph.
type Workstream struct {
	Kind     WorkstreamKind `yaml:"kind" json:"kind"`
	Sequence int            `yaml:"sequence" json:"sequence"`

	Owners []OwnerRole `yaml:"owners" json:"owners"`

	Prerequisites []Dependency `yaml:"prerequisites" json:"prerequisites"`

	InputArtifacts  []Artifact `yaml:"input_artifacts" json:"input_artifacts"`
	OutputArtifacts []Artifact `yaml:"output_artifacts" json:"output_artifacts"`

	Timing Timing `yaml:"timing" json:"timing"`

	AcceptanceOracle string `yaml:"acceptance_oracle" json:"acceptance_oracle"`

	DataProcessingBoundary string `yaml:"data_processing_boundary" json:"data_processing_boundary"`

	Escalation string `yaml:"escalation" json:"escalation"`
	Fallback   string `yaml:"fallback" json:"fallback"`
}

// hasPartyDependency reports whether w names at least one prerequisite owed
// by party.
func (w Workstream) hasPartyDependency(party Party) bool {
	for _, d := range w.Prerequisites {
		if d.Party == party {
			return true
		}
	}
	return false
}

// Signature is an ed25519 signature over a Blueprint's CanonicalDigest,
// structurally identical to tools/planning/pilotprovider.Signature and
// tools/planning/pilotjurisdiction.Signature.
type Signature struct {
	Algorithm  string `yaml:"algorithm" json:"algorithm"`
	PublicKey  string `yaml:"public_key" json:"public_key"`
	Value      string `yaml:"value" json:"value"`
	KeyFixture string `yaml:"key_fixture" json:"key_fixture"`
}

// Blueprint is CUSTOMER-001's signed, reusable design-partner implementation
// blueprint (definitions/planning/gates/customer-001-pilot-blueprint.yaml).
// It is a TEMPLATE (REFACTOR): every field below is a role title, a relative
// offset, or a described procedure - never a named human, a real date, or a
// real vendor fact. [Instantiate] binds it to one tenant's real
// [CustomerFacts], tools/planning/pilotprovider.ProviderTopology and
// tools/planning/pilotjurisdiction.JurisdictionProfile without ever
// mutating it (see doc.go and refactor_test.go).
type Blueprint struct {
	SchemaVersion   int    `yaml:"schema_version" json:"schema_version"`
	TodoID          string `yaml:"todo_id" json:"todo_id"`
	SignedDate      string `yaml:"signed_date" json:"signed_date"`
	TemplateVersion string `yaml:"template_version" json:"template_version"`

	// ProviderTopologyRef and JurisdictionProfileRef bind this blueprint's
	// provider- and jurisdiction-side dependencies to the exact signed
	// artifacts that resolve them, rather than restating provider or
	// jurisdiction facts here.
	ProviderTopologyRef    string `yaml:"provider_topology_ref" json:"provider_topology_ref"`
	JurisdictionProfileRef string `yaml:"jurisdiction_profile_ref" json:"jurisdiction_profile_ref"`
	ScopeCeilingRef        string `yaml:"scope_ceiling_ref" json:"scope_ceiling_ref"`

	Workstreams []Workstream `yaml:"workstreams" json:"workstreams"`

	Signature *Signature `yaml:"signature,omitempty" json:"signature,omitempty"`
}

// LoadBlueprint reads and parses a Blueprint YAML file.
func LoadBlueprint(path string) (*Blueprint, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var b Blueprint
	if err := yaml.Unmarshal(content, &b); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &b, nil
}

// Violation names one structural defect. Field is dotted for list entries so
// a test can name exactly what a mutation broke.
type Violation struct {
	Field string
	Issue string
}

func (v Violation) String() string { return fmt.Sprintf("%s: %s", v.Field, v.Issue) }

// Validate returns every structural violation on b. It performs no file I/O
// and never consults tenant facts: it checks that the blueprint is a
// structurally complete, internally consistent TEMPLATE covering discovery
// through hypercare, with every RED element present for every workstream.
// Whether any real tenant is actually ready to proceed is [Instantiate]'s
// question, not Validate's.
func (b Blueprint) Validate() []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if b.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	if b.TodoID != "CUSTOMER-001" {
		add("todo_id", "must be CUSTOMER-001")
	}
	if strings.TrimSpace(b.SignedDate) == "" {
		add("signed_date", "missing")
	}
	if strings.TrimSpace(b.TemplateVersion) == "" {
		add("template_version", "missing - a reusable template must be versioned")
	}
	if strings.TrimSpace(b.ProviderTopologyRef) == "" {
		add("provider_topology_ref", "missing - provider-side dependencies must bind to a real signed topology, not restate provider facts")
	}
	if strings.TrimSpace(b.JurisdictionProfileRef) == "" {
		add("jurisdiction_profile_ref", "missing - the legal-review workstream must bind to a real signed jurisdiction profile")
	}
	if strings.TrimSpace(b.ScopeCeilingRef) == "" {
		add("scope_ceiling_ref", "missing")
	}

	// --- GREEN: exact, ordered discovery-through-hypercare coverage ---
	want := AllWorkstreamKinds()
	if len(b.Workstreams) != len(want) {
		add("workstreams", fmt.Sprintf("has %d entries, want exactly %d (discovery through hypercare)", len(b.Workstreams), len(want)))
	}
	seenKind := map[WorkstreamKind]bool{}
	for i, w := range b.Workstreams {
		field := fmt.Sprintf("workstreams[%d]", i)
		if seenKind[w.Kind] {
			add(field+".kind", fmt.Sprintf("duplicate workstream kind %s", w.Kind))
		}
		seenKind[w.Kind] = true
		if i < len(want) {
			if w.Kind != want[i] {
				add(field+".kind", fmt.Sprintf("position %d must be %s (discovery-through-hypercare order), got %s", i, want[i], w.Kind))
			}
			if w.Sequence != i+1 {
				add(field+".sequence", fmt.Sprintf("must equal position %d, got %d", i+1, w.Sequence))
			}
		}
		violations = append(violations, validateWorkstream(field, w)...)
	}
	for _, k := range want {
		if !seenKind[k] {
			add("workstreams", fmt.Sprintf("missing required workstream %s", k))
		}
	}

	if b.Signature == nil {
		add("signature", "missing - a pilot blueprint must be signed")
	} else {
		if b.Signature.Algorithm != "ed25519" {
			add("signature.algorithm", fmt.Sprintf("unsupported algorithm %q", b.Signature.Algorithm))
		}
		if b.Signature.PublicKey == "" {
			add("signature.public_key", "missing")
		}
		if b.Signature.Value == "" {
			add("signature.value", "missing")
		}
	}

	return violations
}

// validateWorkstream checks every RED element on one workstream: a
// customer/HCM Next/provider owner, at least one prerequisite bound to
// evidence and a stop/go gate, at least one input and one output artifact,
// a positive due/expiry, an acceptance oracle, a data-processing boundary,
// an escalation path and a fallback.
func validateWorkstream(field string, w Workstream) []Violation {
	var violations []Violation
	add := func(subfield, issue string) {
		violations = append(violations, Violation{Field: field + "." + subfield, Issue: issue})
	}

	// RED: customer / HCM Next / provider owner - each party exactly once,
	// with a non-empty role title and exactly one ACCOUNTABLE owner overall.
	seenParty := map[Party]bool{}
	accountableCount := 0
	for i, o := range w.Owners {
		ofield := fmt.Sprintf("owners[%d]", i)
		if !validParties[o.Party] {
			add(ofield+".party", fmt.Sprintf("unknown party %q", o.Party))
		} else if seenParty[o.Party] {
			add(ofield+".party", fmt.Sprintf("duplicate owner for party %s", o.Party))
		}
		seenParty[o.Party] = true
		if !validRACIRoles[o.RACIRole] {
			add(ofield+".raci_role", fmt.Sprintf("unknown RACI role %q", o.RACIRole))
		} else if o.RACIRole == Accountable {
			accountableCount++
		}
		if strings.TrimSpace(o.RoleTitle) == "" {
			add(ofield+".role_title", "missing")
		}
	}
	for _, party := range AllParties() {
		if !seenParty[party] {
			add("owners", fmt.Sprintf("missing a %s owner", party))
		}
	}
	if accountableCount != 1 {
		add("owners", fmt.Sprintf("must name exactly one ACCOUNTABLE owner, found %d", accountableCount))
	}

	// RED: prerequisite, bound to exact evidence and a stop/go gate (GREEN).
	if len(w.Prerequisites) == 0 {
		add("prerequisites", "missing - the workstream names no prerequisite")
	}
	for i, d := range w.Prerequisites {
		dfield := fmt.Sprintf("prerequisites[%d]", i)
		if strings.TrimSpace(d.Description) == "" {
			add(dfield+".description", "missing")
		}
		if !validParties[d.Party] {
			add(dfield+".party", fmt.Sprintf("unknown party %q", d.Party))
		}
		if strings.TrimSpace(d.EvidenceRef) == "" {
			add(dfield+".evidence_ref", "missing - a prerequisite without exact evidence cannot be checked")
		}
		if strings.TrimSpace(d.Gate.Metric) == "" {
			add(dfield+".gate.metric", "missing")
		}
		if strings.TrimSpace(d.Gate.Threshold) == "" {
			add(dfield+".gate.threshold", "missing")
		}
		if !validActions[d.Gate.Action] {
			add(dfield+".gate.action", fmt.Sprintf("unknown action %q", d.Gate.Action))
		}
	}

	// RED: input/output artifact.
	if len(w.InputArtifacts) == 0 {
		add("input_artifacts", "missing - the workstream names no input artifact")
	}
	for i, a := range w.InputArtifacts {
		if strings.TrimSpace(a.Name) == "" {
			add(fmt.Sprintf("input_artifacts[%d].name", i), "missing")
		}
	}
	if len(w.OutputArtifacts) == 0 {
		add("output_artifacts", "missing - the workstream names no output artifact")
	}
	for i, a := range w.OutputArtifacts {
		if strings.TrimSpace(a.Name) == "" {
			add(fmt.Sprintf("output_artifacts[%d].name", i), "missing")
		}
	}

	// RED: due/expiry.
	if w.Timing.DueOffsetDays <= 0 {
		add("timing.due_offset_days", "must be positive")
	}
	if w.Timing.ExpiryOffsetDays <= 0 {
		add("timing.expiry_offset_days", "must be positive")
	} else if w.Timing.ExpiryOffsetDays < w.Timing.DueOffsetDays {
		add("timing.expiry_offset_days", "must be at least due_offset_days - evidence cannot expire before it is due")
	}

	// RED: acceptance oracle.
	if strings.TrimSpace(w.AcceptanceOracle) == "" {
		add("acceptance_oracle", "missing")
	}

	// RED: data-processing boundary.
	if strings.TrimSpace(w.DataProcessingBoundary) == "" {
		add("data_processing_boundary", "missing")
	}

	// RED: escalation.
	if strings.TrimSpace(w.Escalation) == "" {
		add("escalation", "missing")
	}

	// RED: fallback.
	if strings.TrimSpace(w.Fallback) == "" {
		add("fallback", "missing")
	}

	return violations
}

// digestPayload is the canonical projection hashed by CanonicalDigest and
// signed by Sign/Verify: every blueprint field except the signature itself.
type digestPayload struct {
	SchemaVersion          int          `json:"schema_version"`
	TodoID                 string       `json:"todo_id"`
	SignedDate             string       `json:"signed_date"`
	TemplateVersion        string       `json:"template_version"`
	ProviderTopologyRef    string       `json:"provider_topology_ref"`
	JurisdictionProfileRef string       `json:"jurisdiction_profile_ref"`
	ScopeCeilingRef        string       `json:"scope_ceiling_ref"`
	Workstreams            []Workstream `json:"workstreams"`
}

func (b Blueprint) payload() digestPayload {
	return digestPayload{
		SchemaVersion:          b.SchemaVersion,
		TodoID:                 b.TodoID,
		SignedDate:             b.SignedDate,
		TemplateVersion:        b.TemplateVersion,
		ProviderTopologyRef:    b.ProviderTopologyRef,
		JurisdictionProfileRef: b.JurisdictionProfileRef,
		ScopeCeilingRef:        b.ScopeCeilingRef,
		Workstreams:            b.Workstreams,
	}
}

// CanonicalDigest returns the hex-encoded sha256 digest of b's canonical
// JSON projection (every field except Signature).
func (b Blueprint) CanonicalDigest() (string, error) {
	bts, err := json.Marshal(b.payload())
	if err != nil {
		return "", fmt.Errorf("marshal canonical blueprint: %w", err)
	}
	sum := sha256.Sum256(bts)
	return hex.EncodeToString(sum[:]), nil
}

// FileDigest returns the lowercase hex sha256 digest of the file at path.
func FileDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
