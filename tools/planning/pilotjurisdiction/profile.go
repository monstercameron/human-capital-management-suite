package pilotjurisdiction

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

// JurisdictionRef is the country/state pair this profile pins, in the same
// shape a checked-in file needs (uppercase ISO country/state codes). It is a
// plain YAML/JSON projection of [legal.Jurisdiction]; see [JurisdictionRef.ToLegal].
type JurisdictionRef struct {
	Country string `yaml:"country" json:"country"`
	State   string `yaml:"state" json:"state"`
}

// ToLegal converts j to the legal package's own jurisdiction fact type.
func (j JurisdictionRef) ToLegal() legal.Jurisdiction {
	return legal.Jurisdiction{Country: j.Country, State: j.State}
}

// SourcePin pins one authoritative source this profile relies on, together
// with the exact version and content digest it was reviewed against, so a
// later silent edit to the source is detectable.
type SourcePin struct {
	// Kind is "RESEARCH_MEMO" (the drafted research narrative) or
	// "RULE_PACK_DEFINITION" (a definitions/legal/packs/... file loadable by
	// legal.LoadPackDefinitionFile).
	Kind string `yaml:"kind" json:"kind"`
	// Path is repository-relative.
	Path string `yaml:"path" json:"path"`
	// PackID, VersionMajor, VersionMinor and VocabularyVersion are required
	// when Kind is RULE_PACK_DEFINITION; they pin the exact release this
	// profile reviewed rather than "whatever is on disk".
	PackID            string `yaml:"pack_id,omitempty" json:"pack_id,omitempty"`
	VersionMajor      uint32 `yaml:"version_major,omitempty" json:"version_major,omitempty"`
	VersionMinor      uint32 `yaml:"version_minor,omitempty" json:"version_minor,omitempty"`
	VocabularyVersion uint32 `yaml:"vocabulary_version,omitempty" json:"vocabulary_version,omitempty"`
	// ReviewStatus is the source's own declared review status (e.g.
	// "UNREVIEWED"), read verbatim from the source rather than asserted here.
	ReviewStatus string `yaml:"review_status" json:"review_status"`
	// ContentDigest is the lowercase hex sha256 of the source file's bytes at
	// the moment this profile pinned it. conformance_test.go recomputes it
	// live against the checked-in file.
	ContentDigest string `yaml:"content_digest" json:"content_digest"`
}

// Reviewer names the qualified legal owner accountable for raising this
// profile's ReviewStatus out of UNREVIEWED. RED requires this element; see
// doc.go for why Name is deliberately left empty on the checked-in profile
// rather than populated with an invented name.
type Reviewer struct {
	Name          string `yaml:"name" json:"name"`
	Qualification string `yaml:"qualification" json:"qualification"`
	AssignedDate  string `yaml:"assigned_date" json:"assigned_date"`
}

// ScopeItem names one Phase 1 business intent this profile's jurisdiction
// review covers.
type ScopeItem struct {
	IntentID    string `yaml:"intent_id" json:"intent_id"`
	Disposition string `yaml:"disposition" json:"disposition"` // INCLUDE
	Rationale   string `yaml:"rationale" json:"rationale"`
}

// ObligationMapping binds one legal.ObligationType this profile's pinned
// pack carries to the Phase 1 intent it governs and the exact evidence path
// a caller can inspect. It deliberately does not restate the lifecycle step,
// binding kind or trigger predicate: those live in exactly one place,
// legal.ObligationKindSpecFor, and [ObligationMapping.KindSpec] reads them
// from there so this profile can never drift into its own hard-coded copy
// of what the obligation vocabulary already says (REFACTOR: never hard-code
// law in workflow branches).
type ObligationMapping struct {
	// Kind is a legal.ObligationType wire token, e.g. "NOTICE".
	Kind string `yaml:"kind" json:"kind"`
	// IntentID is the Phase 1 business intent this obligation binds to.
	IntentID string `yaml:"intent_id" json:"intent_id"`
	// EvidencePath names the exact Go symbol a caller reads to see this
	// obligation's evidence once evaluated, e.g.
	// "legal.EvaluationResult.Obligations[].Binding".
	EvidencePath string `yaml:"evidence_path" json:"evidence_path"`
}

// KindSpec resolves m's obligation type against the legal package's own
// vocabulary table. ok is false when Kind does not parse or carries no spec.
func (m ObligationMapping) KindSpec() (legal.ObligationKindSpec, bool) {
	t, err := legal.ParseObligationType(m.Kind)
	if err != nil {
		return legal.ObligationKindSpec{}, false
	}
	return legal.ObligationKindSpecFor(t)
}

// Exclusion declares one thing this profile's reviewed scope explicitly does
// not cover, with why. Kind is one of ExclusionKindObligation,
// ExclusionKindJurisdiction or ExclusionKindLocality.
type Exclusion struct {
	Kind   string `yaml:"kind" json:"kind"`
	Value  string `yaml:"value" json:"value"`
	Reason string `yaml:"reason" json:"reason"`
}

// Exclusion kinds.
const (
	ExclusionKindObligation   = "OBLIGATION_KIND"
	ExclusionKindJurisdiction = "JURISDICTION"
	ExclusionKindLocality     = "LOCALITY"
)

// EffectiveWindow is the profile's own effective and known-at interval: the
// calendar date the pinned rule packs' obligations start applying, and the
// instant these facts (the research, the extraction, the pack) were first
// knowable to HCM Next.
type EffectiveWindow struct {
	EffectiveStart string `yaml:"effective_start" json:"effective_start"`
	KnownAtStart   string `yaml:"known_at_start" json:"known_at_start"`
}

// CollectiveInteraction declares how this profile's reviewed scope treats
// collective-bargaining and multi-legal-entity facts, per RED's
// "collective/company interaction" requirement.
type CollectiveInteraction struct {
	CBAAssumption       string `yaml:"cba_assumption" json:"cba_assumption"`
	MultiEntityHandling string `yaml:"multi_entity_handling" json:"multi_entity_handling"`
}

// Uncertainty statuses this profile is allowed to return. Both are the
// vocabulary the todo's GREEN clause names verbatim.
const (
	UncertaintyUnknown             = "UNKNOWN"
	UncertaintyHumanReviewRequired = "HUMAN_REVIEW_REQUIRED"
)

// UncertaintyPolicy declares what this profile returns, never guesses, when
// facts do not resolve inside its reviewed scope.
type UncertaintyPolicy struct {
	AmbiguousInputStatus string `yaml:"ambiguous_input_status" json:"ambiguous_input_status"`
	OutOfScopeStatus     string `yaml:"out_of_scope_status" json:"out_of_scope_status"`
}

// UpdateSLA declares how often and on what trigger this profile must be
// re-reviewed.
type UpdateSLA struct {
	ReviewCadenceDays int      `yaml:"review_cadence_days" json:"review_cadence_days"`
	TriggerEvents     []string `yaml:"trigger_events" json:"trigger_events"`
}

// Stop/reselect actions.
const (
	ActionProceed       = "PROCEED"
	ActionReselectWedge = "RESELECT_WEDGE"
	ActionStop          = "STOP"
)

// StopReselectThreshold is one numeric or named threshold that maps measured
// pilot evidence to a proceed/reselect/stop action for this jurisdiction
// selection.
type StopReselectThreshold struct {
	Metric    string `yaml:"metric" json:"metric"`
	Threshold string `yaml:"threshold" json:"threshold"`
	Action    string `yaml:"action" json:"action"`
}

// Signature is an ed25519 signature over a JurisdictionProfile's
// CanonicalDigest, structurally identical to
// tools/planning/scopeceiling.Signature and
// tools/planning/gateevidence.Signature.
type Signature struct {
	Algorithm  string `yaml:"algorithm" json:"algorithm"`
	PublicKey  string `yaml:"public_key" json:"public_key"`
	Value      string `yaml:"value" json:"value"`
	KeyFixture string `yaml:"key_fixture" json:"key_fixture"`
}

// JurisdictionProfile is SELECT-001's signed pilot jurisdiction profile
// (definitions/planning/gates/select-001-jurisdiction-profile.yaml).
type JurisdictionProfile struct {
	SchemaVersion int             `yaml:"schema_version" json:"schema_version"`
	TodoID        string          `yaml:"todo_id" json:"todo_id"`
	SignedDate    string          `yaml:"signed_date" json:"signed_date"`
	Jurisdiction  JurisdictionRef `yaml:"jurisdiction" json:"jurisdiction"`
	ReviewStatus  string          `yaml:"review_status" json:"review_status"`

	Sources  []SourcePin `yaml:"sources" json:"sources"`
	Reviewer Reviewer    `yaml:"reviewer" json:"reviewer"`

	ScopeItems         []ScopeItem         `yaml:"scope_items" json:"scope_items"`
	ObligationMappings []ObligationMapping `yaml:"obligation_mappings" json:"obligation_mappings"`
	Exclusions         []Exclusion         `yaml:"exclusions" json:"exclusions"`

	Window      EffectiveWindow       `yaml:"window" json:"window"`
	Collective  CollectiveInteraction `yaml:"collective" json:"collective"`
	Uncertainty UncertaintyPolicy     `yaml:"uncertainty" json:"uncertainty"`
	UpdateSLA   UpdateSLA             `yaml:"update_sla" json:"update_sla"`

	LegalAdviceDisclaimer  string                  `yaml:"legal_advice_disclaimer" json:"legal_advice_disclaimer"`
	StopReselectThresholds []StopReselectThreshold `yaml:"stop_reselect_thresholds" json:"stop_reselect_thresholds"`

	Signature *Signature `yaml:"signature,omitempty" json:"signature,omitempty"`
}

// LoadProfile reads and parses a JurisdictionProfile YAML file.
func LoadProfile(path string) (*JurisdictionProfile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var p JurisdictionProfile
	if err := yaml.Unmarshal(content, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &p, nil
}

// Violation names one structural defect. Field is dotted for list entries so
// a test can name exactly what a mutation broke.
type Violation struct {
	Field string
	Issue string
}

func (v Violation) String() string { return fmt.Sprintf("%s: %s", v.Field, v.Issue) }

var validExclusionKinds = map[string]bool{
	ExclusionKindObligation:   true,
	ExclusionKindJurisdiction: true,
	ExclusionKindLocality:     true,
}

var validUncertaintyStatuses = map[string]bool{
	UncertaintyUnknown:             true,
	UncertaintyHumanReviewRequired: true,
}

var validActions = map[string]bool{
	ActionProceed:       true,
	ActionReselectWedge: true,
	ActionStop:          true,
}

// Validate returns every structural violation on p. It performs no file I/O:
// it checks that the profile is internally well-formed and that its
// obligation-kind coverage is exact against legal.AllObligationTypes, which
// requires no file access because that list is a compiled-in vocabulary.
// Cross-checking Sources against the actual files on disk is
// conformance_test.go's job, not Validate's.
func (p JurisdictionProfile) Validate() []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if p.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	if p.TodoID != "SELECT-001" {
		add("todo_id", "must be SELECT-001")
	}
	if strings.TrimSpace(p.SignedDate) == "" {
		add("signed_date", "missing")
	}
	if err := p.Jurisdiction.ToLegal().Validate(); err != nil {
		add("jurisdiction", fmt.Sprintf("invalid: %v", err))
	}
	if _, err := legal.ParseReviewStatus(p.ReviewStatus); err != nil {
		add("review_status", fmt.Sprintf("must be one of legal's own review-status wire tokens: %v", err))
	}

	// --- RED: authoritative source citations ---
	if len(p.Sources) == 0 {
		add("sources", "missing - the profile carries no authoritative source citations")
	}
	for i, s := range p.Sources {
		field := fmt.Sprintf("sources[%d]", i)
		if s.Kind != "RESEARCH_MEMO" && s.Kind != "RULE_PACK_DEFINITION" {
			add(field+".kind", fmt.Sprintf("unknown source kind %q", s.Kind))
		}
		if strings.TrimSpace(s.Path) == "" {
			add(field+".path", "missing")
		}
		if strings.TrimSpace(s.ReviewStatus) == "" {
			add(field+".review_status", "missing")
		}
		if strings.TrimSpace(s.ContentDigest) == "" {
			add(field+".content_digest", "missing - a source without a pinned digest is not a pinned version")
		}
		if s.Kind == "RULE_PACK_DEFINITION" {
			if strings.TrimSpace(s.PackID) == "" {
				add(field+".pack_id", "missing for a RULE_PACK_DEFINITION source")
			}
			if s.VersionMajor == 0 {
				add(field+".version_major", "missing for a RULE_PACK_DEFINITION source")
			}
			if s.VocabularyVersion == 0 {
				add(field+".vocabulary_version", "missing for a RULE_PACK_DEFINITION source")
			}
		}
	}

	// --- RED: qualified legal owner/reviewer ---
	if strings.TrimSpace(p.Reviewer.Name) == "" {
		add("reviewer.name", "missing - a qualified legal owner/reviewer is required; this profile is HUMAN_REVIEW_REQUIRED pending a named reviewer")
	} else if strings.TrimSpace(p.Reviewer.Qualification) == "" {
		add("reviewer.qualification", "missing - a named reviewer must also state the qualification that makes them qualified")
	}

	// --- RED: included intent/rule/filing set ---
	if len(p.ScopeItems) == 0 {
		add("scope_items", "missing - the profile names no included intent")
	}
	seenIntent := map[string]bool{}
	for i, s := range p.ScopeItems {
		field := fmt.Sprintf("scope_items[%d]", i)
		if strings.TrimSpace(s.IntentID) == "" {
			add(field+".intent_id", "missing")
		} else if seenIntent[s.IntentID] {
			add(field+".intent_id", "duplicate scope item")
		}
		seenIntent[s.IntentID] = true
		if s.Disposition != "INCLUDE" {
			add(field+".disposition", fmt.Sprintf("unexpected disposition %q", s.Disposition))
		}
		if strings.TrimSpace(s.Rationale) == "" {
			add(field+".rationale", "missing")
		}
	}

	seenMappedKinds := map[legal.ObligationType]bool{}
	for i, m := range p.ObligationMappings {
		field := fmt.Sprintf("obligation_mappings[%d]", i)
		t, err := legal.ParseObligationType(m.Kind)
		if err != nil {
			add(field+".kind", fmt.Sprintf("unknown obligation kind %q", m.Kind))
		} else {
			if seenMappedKinds[t] {
				add(field+".kind", fmt.Sprintf("duplicate mapping for kind %s", m.Kind))
			}
			seenMappedKinds[t] = true
			if _, ok := legal.ObligationKindSpecFor(t); !ok {
				add(field+".kind", fmt.Sprintf("legal.ObligationKindSpecFor has no row for %s", m.Kind))
			}
		}
		if strings.TrimSpace(m.IntentID) == "" {
			add(field+".intent_id", "missing")
		}
		if strings.TrimSpace(m.EvidencePath) == "" {
			add(field+".evidence_path", "missing")
		}
	}

	// --- RED/GREEN: exact exclusions, obligation-kind completeness ---
	if len(p.Exclusions) == 0 {
		add("exclusions", "missing - the profile declares no exact exclusions")
	}
	seenExcludedKinds := map[legal.ObligationType]bool{}
	hasJurisdictionExclusion := false
	for i, e := range p.Exclusions {
		field := fmt.Sprintf("exclusions[%d]", i)
		if !validExclusionKinds[e.Kind] {
			add(field+".kind", fmt.Sprintf("unknown exclusion kind %q", e.Kind))
		}
		if strings.TrimSpace(e.Value) == "" {
			add(field+".value", "missing")
		}
		if strings.TrimSpace(e.Reason) == "" {
			add(field+".reason", "missing")
		}
		switch e.Kind {
		case ExclusionKindJurisdiction:
			hasJurisdictionExclusion = true
		case ExclusionKindObligation:
			t, err := legal.ParseObligationType(e.Value)
			if err != nil {
				add(field+".value", fmt.Sprintf("unknown obligation kind %q", e.Value))
				break
			}
			if seenExcludedKinds[t] {
				add(field+".value", fmt.Sprintf("duplicate exclusion for kind %s", e.Value))
			}
			seenExcludedKinds[t] = true
			if seenMappedKinds[t] {
				add(field+".value", fmt.Sprintf("kind %s is declared both mapped (in scope) and excluded (out of scope)", e.Value))
			}
		}
	}
	if !hasJurisdictionExclusion {
		add("exclusions", "missing a JURISDICTION exclusion stating every jurisdiction outside this profile's pinned one is out of reviewed scope")
	}
	for _, t := range legal.AllObligationTypes() {
		if !seenMappedKinds[t] && !seenExcludedKinds[t] {
			add("obligation_coverage", fmt.Sprintf("obligation kind %s is neither mapped nor excluded - undeclared scope", t))
		}
	}

	// --- RED: effective and known-at interval ---
	if strings.TrimSpace(p.Window.EffectiveStart) == "" {
		add("window.effective_start", "missing")
	}
	if strings.TrimSpace(p.Window.KnownAtStart) == "" {
		add("window.known_at_start", "missing")
	}

	// --- RED: collective/company interaction ---
	if strings.TrimSpace(p.Collective.CBAAssumption) == "" {
		add("collective.cba_assumption", "missing")
	}
	if strings.TrimSpace(p.Collective.MultiEntityHandling) == "" {
		add("collective.multi_entity_handling", "missing")
	}

	// --- RED/GREEN: uncertainty behavior ---
	if !validUncertaintyStatuses[p.Uncertainty.AmbiguousInputStatus] {
		add("uncertainty.ambiguous_input_status", fmt.Sprintf("must be UNKNOWN or HUMAN_REVIEW_REQUIRED, got %q", p.Uncertainty.AmbiguousInputStatus))
	}
	if !validUncertaintyStatuses[p.Uncertainty.OutOfScopeStatus] {
		add("uncertainty.out_of_scope_status", fmt.Sprintf("must be UNKNOWN or HUMAN_REVIEW_REQUIRED, got %q", p.Uncertainty.OutOfScopeStatus))
	}

	// --- RED: update SLA ---
	if p.UpdateSLA.ReviewCadenceDays <= 0 {
		add("update_sla.review_cadence_days", "must be positive")
	}
	if len(p.UpdateSLA.TriggerEvents) == 0 {
		add("update_sla.trigger_events", "missing")
	}

	// --- RED: prohibited legal-advice boundary ---
	if !strings.Contains(strings.ToLower(p.LegalAdviceDisclaimer), "not legal advice") {
		add("legal_advice_disclaimer", `missing or does not contain "not legal advice"`)
	}

	// --- RED: stop/reselect threshold ---
	if len(p.StopReselectThresholds) == 0 {
		add("stop_reselect_thresholds", "missing")
	}
	for i, th := range p.StopReselectThresholds {
		field := fmt.Sprintf("stop_reselect_thresholds[%d]", i)
		if strings.TrimSpace(th.Metric) == "" {
			add(field+".metric", "missing")
		}
		if strings.TrimSpace(th.Threshold) == "" {
			add(field+".threshold", "missing")
		}
		if !validActions[th.Action] {
			add(field+".action", fmt.Sprintf("unknown action %q", th.Action))
		}
	}

	if p.Signature == nil {
		add("signature", "missing - a jurisdiction profile must be signed")
	} else {
		if p.Signature.Algorithm != "ed25519" {
			add("signature.algorithm", fmt.Sprintf("unsupported algorithm %q", p.Signature.Algorithm))
		}
		if p.Signature.PublicKey == "" {
			add("signature.public_key", "missing")
		}
		if p.Signature.Value == "" {
			add("signature.value", "missing")
		}
	}

	return violations
}

// digestPayload is the canonical projection hashed by CanonicalDigest and
// signed by Sign/Verify: every profile field except the signature itself.
type digestPayload struct {
	SchemaVersion          int                     `json:"schema_version"`
	TodoID                 string                  `json:"todo_id"`
	SignedDate             string                  `json:"signed_date"`
	Jurisdiction           JurisdictionRef         `json:"jurisdiction"`
	ReviewStatus           string                  `json:"review_status"`
	Sources                []SourcePin             `json:"sources"`
	Reviewer               Reviewer                `json:"reviewer"`
	ScopeItems             []ScopeItem             `json:"scope_items"`
	ObligationMappings     []ObligationMapping     `json:"obligation_mappings"`
	Exclusions             []Exclusion             `json:"exclusions"`
	Window                 EffectiveWindow         `json:"window"`
	Collective             CollectiveInteraction   `json:"collective"`
	Uncertainty            UncertaintyPolicy       `json:"uncertainty"`
	UpdateSLA              UpdateSLA               `json:"update_sla"`
	LegalAdviceDisclaimer  string                  `json:"legal_advice_disclaimer"`
	StopReselectThresholds []StopReselectThreshold `json:"stop_reselect_thresholds"`
}

func (p JurisdictionProfile) payload() digestPayload {
	return digestPayload{
		SchemaVersion:          p.SchemaVersion,
		TodoID:                 p.TodoID,
		SignedDate:             p.SignedDate,
		Jurisdiction:           p.Jurisdiction,
		ReviewStatus:           p.ReviewStatus,
		Sources:                p.Sources,
		Reviewer:               p.Reviewer,
		ScopeItems:             p.ScopeItems,
		ObligationMappings:     p.ObligationMappings,
		Exclusions:             p.Exclusions,
		Window:                 p.Window,
		Collective:             p.Collective,
		Uncertainty:            p.Uncertainty,
		UpdateSLA:              p.UpdateSLA,
		LegalAdviceDisclaimer:  p.LegalAdviceDisclaimer,
		StopReselectThresholds: p.StopReselectThresholds,
	}
}

// CanonicalDigest returns the hex-encoded sha256 digest of p's canonical JSON
// projection (every field except Signature).
func (p JurisdictionProfile) CanonicalDigest() (string, error) {
	b, err := json.Marshal(p.payload())
	if err != nil {
		return "", fmt.Errorf("marshal canonical profile: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// FileDigest returns the lowercase hex sha256 digest of the file at path. It
// is exported so both the conformance test and the manifest-signing tool use
// exactly one digest computation for pinning a source's content.
func FileDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
