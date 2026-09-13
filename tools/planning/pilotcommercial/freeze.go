// Package pilotcommercial implements COMMERCIAL-001: the signed pilot
// commercial package that freezes what the ChangeOps Promotion +
// Compensation Change pilot may be sold as, priced as, and held authoritative
// for, exactly the way tools/planning/scopeceiling (PHASE-001),
// tools/planning/pilotjurisdiction (SELECT-001) and tools/planning/pilotprovider
// (SELECT-002) froze the scope ceiling, jurisdiction selection and provider
// selection before it. See doc.go for why this package's promises are
// derived from - never independently asserted alongside - those three
// artifacts and the internal/commercial entitlement/pricing/authority/exit
// registry.
package pilotcommercial

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// PromiseStatus is the closed vocabulary every selection-bound promise in
// this schema (jurisdiction, provider) uses to say how much it may rely on
// a still-maturing selection artifact. It mirrors the two-state honesty
// pattern tools/planning/pilotprovider.SelectionStatus and
// tools/planning/pilotjurisdiction's reviewer-emptiness already established:
// a promise may never silently claim more certainty than its source artifact
// has earned.
type PromiseStatus string

const (
	// PromiseNone means this freeze relies on nothing from the named
	// selection: no jurisdiction, no provider, no SLO is promised. This is
	// the only legal SLOStatus value today (see SLOStatusNone) because
	// PHASE-001's slo selection slot is unfilled.
	PromiseNone PromiseStatus = "NONE"
	// PromiseSelectedPendingReview means a selection exists (SELECT-001's
	// California jurisdiction, or a future non-placeholder provider) but its
	// own review/confirmation status has not cleared the bar that would let
	// this freeze rely on it as settled fact.
	PromiseSelectedPendingReview PromiseStatus = "SELECTED_PENDING_REVIEW"
	// PromiseSelectedConfirmed means the selection artifact itself reports a
	// cleared review/confirmation status. Validate refuses this value unless
	// the freeze also carries the corroborating fields
	// (ReviewStatus/VendorRef) that back the claim.
	PromiseSelectedConfirmed PromiseStatus = "SELECTED_CONFIRMED"
)

var validPromiseStatuses = map[PromiseStatus]bool{
	PromiseNone:                  true,
	PromiseSelectedPendingReview: true,
	PromiseSelectedConfirmed:     true,
}

// SLOStatusNone is the only value EvidenceAndSLO.SLOStatus may carry while
// PHASE-001's phase1-scope-ceiling.yaml carries an unfilled "slo" selection
// slot (RED: "package promises ... an unselected ... SLO"). It is a
// distinct constant from PromiseNone, even though it carries the same wire
// value, because it is validated against a different vocabulary
// (validSLOStatuses) that today deliberately contains nothing else: unlike
// jurisdiction and provider, there is no partially-selected SLO state this
// schema recognizes, because no SLO selection has ever been proposed
// anywhere in this repository.
const SLOStatusNone = "NONE"

var validSLOStatuses = map[string]bool{SLOStatusNone: true}

// Entitlement binds one sold capability to the Phase 1 scope ceiling and to
// internal/commercial's live entitlement registry (GREEN: "maps entitlements
// ... to the Phase 1 digest"). Field names and shape intentionally mirror
// internal/commercial.IntentEntitlement field-for-field so derive.go's
// diffEntitlements can compare them without a lossy translation.
type Entitlement struct {
	IntentID    string   `yaml:"intent_id" json:"intent_id"`
	Modes       []string `yaml:"modes,omitempty" json:"modes,omitempty"`
	EffectClass string   `yaml:"effect_class" json:"effect_class"`
	Disposition string   `yaml:"disposition" json:"disposition"`
}

// IntentPromise is the entitlement half of GREEN's "maps entitlements and
// exclusions to the Phase 1 digest": every sold capability plus every named
// exclusion, pinned to the exact PHASE-001 ceiling digest they were checked
// against.
type IntentPromise struct {
	ScopeCeilingDigest string        `yaml:"scope_ceiling_digest" json:"scope_ceiling_digest"`
	Entitlements       []Entitlement `yaml:"entitlements" json:"entitlements"`
	Exclusions         []string      `yaml:"exclusions" json:"exclusions"`
}

// JurisdictionPromise is bound to SELECT-001's jurisdiction profile (RED:
// "promises an unselected ... jurisdiction"). Country/State/ReviewStatus are
// deliberately named to mirror SELECT-001's own JurisdictionRef and
// ReviewStatus fields rather than inventing new vocabulary, so derive.go's
// live cross-check is a direct field comparison, not a translation.
type JurisdictionPromise struct {
	Status       PromiseStatus `yaml:"status" json:"status"`
	Country      string        `yaml:"country,omitempty" json:"country,omitempty"`
	State        string        `yaml:"state,omitempty" json:"state,omitempty"`
	ReviewStatus string        `yaml:"review_status,omitempty" json:"review_status,omitempty"`
	Caveat       string        `yaml:"caveat" json:"caveat"`
}

// ProviderPromise is bound to SELECT-002's provider topology (RED: "promises
// an unselected ... provider"). VendorRef is deliberately empty while Status
// is PromiseNone: SELECT-002's checked-in topology is a structurally-marked
// placeholder that cannot satisfy a real provider-selection gate, so this
// freeze must not name any provider-dependent entitlement beyond what that
// placeholder can back.
type ProviderPromise struct {
	Status    PromiseStatus `yaml:"status" json:"status"`
	VendorRef string        `yaml:"vendor_ref,omitempty" json:"vendor_ref,omitempty"`
	Caveat    string        `yaml:"caveat" json:"caveat"`
}

// EvidenceAndSLO folds RED's SLO clause and GREEN's "evidence and SLOs"
// clause into one structure: what evidence every pilot result carries, and
// the explicit, closed statement that no availability or response-time
// commitment is promised while PHASE-001's slo selection slot is unfilled.
type EvidenceAndSLO struct {
	SLOStatus        string   `yaml:"slo_status" json:"slo_status"`
	SLOStatement     string   `yaml:"slo_statement" json:"slo_statement"`
	EvidenceCaptured []string `yaml:"evidence_captured" json:"evidence_captured"`
}

// PricingHypothesis mirrors internal/commercial.PricingHypothesis
// field-for-field (GREEN: "declares price/usage/support/implementation
// assumptions") plus IsHypothesis, which RED's "hides ... hypothesis" clause
// requires this freeze assert explicitly rather than let a reader mistake a
// price range for an agreed rate card.
type PricingHypothesis struct {
	Currency            string `yaml:"currency" json:"currency"`
	MinimumCents        int64  `yaml:"minimum_cents" json:"minimum_cents"`
	MaximumCents        int64  `yaml:"maximum_cents" json:"maximum_cents"`
	DurationDaysMin     int    `yaml:"duration_days_min" json:"duration_days_min"`
	DurationDaysMax     int    `yaml:"duration_days_max" json:"duration_days_max"`
	UsageRating         string `yaml:"usage_rating" json:"usage_rating"`
	ImplementationCost  string `yaml:"implementation_cost" json:"implementation_cost"`
	SupportModel        string `yaml:"support_model" json:"support_model"`
	ProviderPassThrough string `yaml:"provider_pass_through" json:"provider_pass_through"`
	IsHypothesis        bool   `yaml:"is_hypothesis" json:"is_hypothesis"`
}

// Closed authority-topology vocabulary (RED: "confuses overlay authority
// with system-of-record ownership").
const (
	TopologyExternalObservation = "EXTERNAL_OBSERVATION"
	TopologySystemOfRecord      = "SYSTEM_OF_RECORD"
)

var validAuthorityTopologies = map[string]bool{
	TopologyExternalObservation: true,
	TopologySystemOfRecord:      true,
}

// AuthorityBoundary mirrors internal/commercial.AuthorityBoundary
// field-for-field (GREEN: "legal and authority boundaries") and adds the
// structural invariant Validate enforces: a domain cannot be both owned
// (system-of-record authority) and merely observed (overlay authority) at
// once, and only a topology holding real write authority may claim
// ownership of any domain. This is what keeps RED's "confuses overlay
// authority with system-of-record ownership" a checked property instead of
// a sentence in a comment.
type AuthorityBoundary struct {
	Topology          string   `yaml:"topology" json:"topology"`
	WriteAuthority    bool     `yaml:"write_authority" json:"write_authority"`
	OwnedDomains      []string `yaml:"owned_domains" json:"owned_domains"`
	ObservedDomains   []string `yaml:"observed_domains" json:"observed_domains"`
	ForbiddenEffects  []string `yaml:"forbidden_effects" json:"forbidden_effects"`
	ExpansionRequires string   `yaml:"expansion_requires" json:"expansion_requires"`
}

// ProviderPassThrough declares whether, and under what policy, provider
// costs are ever disclosed or passed through to the customer (GREEN:
// "provider pass-throughs").
type ProviderPassThrough struct {
	Disclosed                bool   `yaml:"disclosed" json:"disclosed"`
	Policy                   string `yaml:"policy" json:"policy"`
	RequiresCustomerApproval bool   `yaml:"requires_customer_approval" json:"requires_customer_approval"`
}

// ExitTerms mirrors internal/commercial.ExitTerms field-for-field (GREEN:
// "termination and export obligations"; RED: "lacks ... exit terms").
type ExitTerms struct {
	TerminationNoticeDays int      `yaml:"termination_notice_days" json:"termination_notice_days"`
	ExportFormats         []string `yaml:"export_formats" json:"export_formats"`
	ExportIncludes        []string `yaml:"export_includes" json:"export_includes"`
	RetentionDays         int      `yaml:"retention_days" json:"retention_days"`
	DeletionCertificate   bool     `yaml:"deletion_certificate" json:"deletion_certificate"`
}

// DataProcessingTerms names the data-processing/retention facts RED
// requires ("lacks data-processing, retention or exit terms") beyond the
// customer-visible ExitTerms.RetentionDays: residency, subprocessor
// disclosure and the DPA reference. Field names mirror
// tools/planning/pilotprovider.DataProcessingTerms.
type DataProcessingTerms struct {
	ResidencyRegion        string `yaml:"residency_region" json:"residency_region"`
	SubprocessorDisclosure string `yaml:"subprocessor_disclosure" json:"subprocessor_disclosure"`
	DPAReference           string `yaml:"dpa_reference" json:"dpa_reference"`
}

// BillingPolicy is the declared half of RED's "bills replay or repair
// duplicates" clause; billing.go's ComputeBillableTotal is the enforced
// half. ReplayBillable and RepairBillable are closed to false: this pilot's
// fixed-price model (PricingHypothesis.UsageRating) never meters individual
// transactions, so a replay or a repair of one is never a second billable
// event, and Validate refuses a freeze that claims otherwise.
type BillingPolicy struct {
	Model            string `yaml:"model" json:"model"`
	ReplayBillable   bool   `yaml:"replay_billable" json:"replay_billable"`
	RepairBillable   bool   `yaml:"repair_billable" json:"repair_billable"`
	IdempotencyBasis string `yaml:"idempotency_basis" json:"idempotency_basis"`
}

// RepriceStopCriteria mirrors internal/commercial's
// RepriceThresholdPct/StopThresholdPct (GREEN: "quantitative reprice/stop
// criteria").
type RepriceStopCriteria struct {
	RepriceThresholdPct int `yaml:"reprice_threshold_pct" json:"reprice_threshold_pct"`
	StopThresholdPct    int `yaml:"stop_threshold_pct" json:"stop_threshold_pct"`
}

// Signature is an ed25519 signature over a PilotCommercialFreeze's
// CanonicalDigest, structurally identical to
// tools/planning/pilotprovider.Signature and tools/planning/gateevidence.Signature.
type Signature struct {
	Algorithm  string `yaml:"algorithm" json:"algorithm"`
	PublicKey  string `yaml:"public_key" json:"public_key"`
	Value      string `yaml:"value" json:"value"`
	KeyFixture string `yaml:"key_fixture" json:"key_fixture"`
}

// PilotCommercialFreeze is COMMERCIAL-001's signed pilot commercial package
// (definitions/planning/gates/commercial-001-pilot-package.yaml).
type PilotCommercialFreeze struct {
	SchemaVersion int    `yaml:"schema_version" json:"schema_version"`
	TodoID        string `yaml:"todo_id" json:"todo_id"`
	SignedDate    string `yaml:"signed_date" json:"signed_date"`

	Intent       IntentPromise       `yaml:"intent" json:"intent"`
	Jurisdiction JurisdictionPromise `yaml:"jurisdiction" json:"jurisdiction"`
	Provider     ProviderPromise     `yaml:"provider" json:"provider"`
	Evidence     EvidenceAndSLO      `yaml:"evidence" json:"evidence"`

	Pricing             PricingHypothesis   `yaml:"pricing" json:"pricing"`
	Authority           AuthorityBoundary   `yaml:"authority" json:"authority"`
	ProviderPassThrough ProviderPassThrough `yaml:"provider_pass_through" json:"provider_pass_through"`
	Exit                ExitTerms           `yaml:"exit" json:"exit"`
	DataProcessing      DataProcessingTerms `yaml:"data_processing" json:"data_processing"`
	Billing             BillingPolicy       `yaml:"billing" json:"billing"`
	RepriceStop         RepriceStopCriteria `yaml:"reprice_stop" json:"reprice_stop"`

	Signature *Signature `yaml:"signature,omitempty" json:"signature,omitempty"`
}

// LoadFreeze reads and parses a PilotCommercialFreeze YAML file.
func LoadFreeze(path string) (*PilotCommercialFreeze, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f PilotCommercialFreeze
	if err := yaml.Unmarshal(content, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &f, nil
}

// Violation names one structural defect. Field is dotted for list entries so
// a test can name exactly what a mutation broke.
type Violation struct {
	Field string
	Issue string
}

func (v Violation) String() string { return fmt.Sprintf("%s: %s", v.Field, v.Issue) }

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// Validate returns every structural violation on f. It performs no file I/O
// and never consults PHASE-001's ceiling, SELECT-001's profile,
// SELECT-002's topology or internal/commercial's live registry directly:
// those cross-checks belong to ConformsToLiveRegistries (derive.go), which a
// CONFORMANCE test runs against the real checked-in files. Validate only
// checks that f is internally well-formed and that its own promise fields
// agree with each other.
func (f PilotCommercialFreeze) Validate() []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if f.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	if f.TodoID != "COMMERCIAL-001" {
		add("todo_id", "must be COMMERCIAL-001")
	}
	if strings.TrimSpace(f.SignedDate) == "" {
		add("signed_date", "missing")
	}

	// --- GREEN: "maps entitlements and exclusions to the Phase 1 digest" ---
	if !isHex64(f.Intent.ScopeCeilingDigest) {
		add("intent.scope_ceiling_digest", "missing or not a 64-hex-character sha256 digest")
	}
	if len(f.Intent.Entitlements) == 0 {
		add("intent.entitlements", "missing - the package sells no entitlement")
	}
	seenIntent := map[string]bool{}
	for i, e := range f.Intent.Entitlements {
		field := fmt.Sprintf("intent.entitlements[%d]", i)
		if strings.TrimSpace(e.IntentID) == "" {
			add(field+".intent_id", "missing")
		} else if seenIntent[e.IntentID] {
			add(field+".intent_id", fmt.Sprintf("duplicate entitlement %q", e.IntentID))
		} else {
			seenIntent[e.IntentID] = true
		}
		if strings.TrimSpace(e.EffectClass) == "" {
			add(field+".effect_class", "missing")
		}
		if strings.TrimSpace(e.Disposition) == "" {
			add(field+".disposition", "missing")
		}
		// RED: "confuses overlay authority with system-of-record ownership".
		// An overlay without write authority cannot sell a non-read-only
		// entitlement; that would promise more than AuthorityBoundary grants.
		if !f.Authority.WriteAuthority && e.EffectClass != "" && e.EffectClass != "READ_ONLY" {
			add(field+".effect_class", fmt.Sprintf("is %q but authority.write_authority is false - an overlay without write authority cannot sell a non-read-only entitlement", e.EffectClass))
		}
	}
	if len(f.Intent.Exclusions) < 4 {
		add("intent.exclusions", "fewer than four exclusions named - RED requires the package name what it excludes, not merely what it includes")
	}

	// --- RED: "promises an unselected ... jurisdiction" ---
	if !validPromiseStatuses[f.Jurisdiction.Status] {
		add("jurisdiction.status", fmt.Sprintf("must be one of %v, got %q", []PromiseStatus{PromiseNone, PromiseSelectedPendingReview, PromiseSelectedConfirmed}, f.Jurisdiction.Status))
	}
	switch f.Jurisdiction.Status {
	case PromiseNone:
		if f.Jurisdiction.Country != "" || f.Jurisdiction.State != "" || f.Jurisdiction.ReviewStatus != "" {
			add("jurisdiction", "status is NONE but country/state/review_status is populated - NONE must not smuggle in a jurisdiction claim")
		}
	case PromiseSelectedPendingReview, PromiseSelectedConfirmed:
		if strings.TrimSpace(f.Jurisdiction.Country) == "" || strings.TrimSpace(f.Jurisdiction.State) == "" {
			add("jurisdiction", "status names a selection but country/state is missing")
		}
		if strings.TrimSpace(f.Jurisdiction.ReviewStatus) == "" {
			add("jurisdiction.review_status", "missing")
		}
	}
	if strings.TrimSpace(f.Jurisdiction.Caveat) == "" {
		add("jurisdiction.caveat", "missing")
	}

	// --- RED: "promises an unselected ... provider" ---
	if !validPromiseStatuses[f.Provider.Status] {
		add("provider.status", fmt.Sprintf("must be one of %v, got %q", []PromiseStatus{PromiseNone, PromiseSelectedPendingReview, PromiseSelectedConfirmed}, f.Provider.Status))
	}
	switch f.Provider.Status {
	case PromiseNone:
		if f.Provider.VendorRef != "" {
			add("provider.vendor_ref", "status is NONE but vendor_ref is populated - NONE must not smuggle in a provider claim")
		}
	case PromiseSelectedPendingReview, PromiseSelectedConfirmed:
		if strings.TrimSpace(f.Provider.VendorRef) == "" {
			add("provider.vendor_ref", "status names a selection but vendor_ref is missing")
		}
	}
	if strings.TrimSpace(f.Provider.Caveat) == "" {
		add("provider.caveat", "missing")
	}

	// --- RED: "promises an unselected ... SLO"; GREEN: "evidence and SLOs" ---
	if !validSLOStatuses[f.Evidence.SLOStatus] {
		add("evidence.slo_status", fmt.Sprintf("must be %s while no SLO selection exists, got %q", SLOStatusNone, f.Evidence.SLOStatus))
	}
	if strings.TrimSpace(f.Evidence.SLOStatement) == "" {
		add("evidence.slo_statement", "missing")
	}
	if len(f.Evidence.EvidenceCaptured) == 0 {
		add("evidence.evidence_captured", "missing - GREEN requires the package declare what evidence every result carries")
	}

	// --- GREEN: "declares price/usage/support/implementation assumptions";
	// RED: "hides hypothesis/stop threshold" ---
	if strings.TrimSpace(f.Pricing.Currency) == "" {
		add("pricing.currency", "missing")
	}
	if f.Pricing.MinimumCents <= 0 || f.Pricing.MaximumCents <= 0 || f.Pricing.MaximumCents < f.Pricing.MinimumCents {
		add("pricing.minimum_cents", "pricing range is missing or inverted")
	}
	if f.Pricing.DurationDaysMin <= 0 || f.Pricing.DurationDaysMax < f.Pricing.DurationDaysMin {
		add("pricing.duration_days_min", "duration range is missing or inverted")
	}
	if strings.TrimSpace(f.Pricing.UsageRating) == "" {
		add("pricing.usage_rating", "missing")
	}
	if strings.TrimSpace(f.Pricing.ImplementationCost) == "" {
		add("pricing.implementation_cost", "missing - RED forbids omitting implementation cost")
	}
	if strings.TrimSpace(f.Pricing.SupportModel) == "" {
		add("pricing.support_model", "missing - RED forbids omitting support cost")
	}
	if strings.TrimSpace(f.Pricing.ProviderPassThrough) == "" {
		add("pricing.provider_pass_through", "missing - RED forbids omitting provider cost")
	}
	if !f.Pricing.IsHypothesis {
		add("pricing.is_hypothesis", "must be true - RED forbids hiding that pricing is a hypothesis, not an agreed rate card")
	}

	// --- RED: "confuses overlay authority with system-of-record ownership";
	// GREEN: "legal and authority boundaries" ---
	if !validAuthorityTopologies[f.Authority.Topology] {
		add("authority.topology", fmt.Sprintf("unknown topology %q", f.Authority.Topology))
	}
	owned := map[string]bool{}
	for _, d := range f.Authority.OwnedDomains {
		owned[d] = true
	}
	for _, d := range f.Authority.ObservedDomains {
		if owned[d] {
			add("authority", fmt.Sprintf("domain %q is claimed as both owned and observed - conflates overlay authority with system-of-record ownership", d))
		}
	}
	if !f.Authority.WriteAuthority && len(f.Authority.OwnedDomains) != 0 {
		add("authority.owned_domains", "authority.write_authority is false but owned_domains is non-empty - an overlay without write authority cannot claim system-of-record ownership")
	}
	if f.Authority.Topology == TopologyExternalObservation {
		if f.Authority.WriteAuthority {
			add("authority.write_authority", "topology is EXTERNAL_OBSERVATION but write_authority is true")
		}
		if len(f.Authority.OwnedDomains) != 0 {
			add("authority.owned_domains", "topology is EXTERNAL_OBSERVATION but owned_domains is non-empty")
		}
	}
	if f.Authority.Topology == TopologySystemOfRecord {
		if !f.Authority.WriteAuthority {
			add("authority.write_authority", "topology is SYSTEM_OF_RECORD but write_authority is false")
		}
		if len(f.Authority.OwnedDomains) == 0 {
			add("authority.owned_domains", "topology is SYSTEM_OF_RECORD but owned_domains is empty")
		}
	}
	if len(f.Authority.ObservedDomains) == 0 && len(f.Authority.OwnedDomains) == 0 {
		add("authority", "neither owned_domains nor observed_domains names any domain")
	}
	if strings.TrimSpace(f.Authority.ExpansionRequires) == "" {
		add("authority.expansion_requires", "missing")
	}

	// --- GREEN: "provider pass-throughs" ---
	if strings.TrimSpace(f.ProviderPassThrough.Policy) == "" {
		add("provider_pass_through.policy", "missing")
	}
	if f.ProviderPassThrough.Disclosed && !f.ProviderPassThrough.RequiresCustomerApproval {
		add("provider_pass_through.requires_customer_approval", "disclosed pass-through costs require customer approval before being charged")
	}

	// --- RED: "lacks data-processing, retention or exit terms"; GREEN:
	// "termination and export obligations" ---
	if f.Exit.TerminationNoticeDays <= 0 {
		add("exit.termination_notice_days", "must be positive")
	}
	if len(f.Exit.ExportFormats) == 0 {
		add("exit.export_formats", "missing")
	}
	if len(f.Exit.ExportIncludes) == 0 {
		add("exit.export_includes", "missing")
	}
	if f.Exit.RetentionDays <= 0 {
		add("exit.retention_days", "must be positive")
	}
	if !f.Exit.DeletionCertificate {
		add("exit.deletion_certificate", "must be true")
	}
	if strings.TrimSpace(f.DataProcessing.ResidencyRegion) == "" {
		add("data_processing.residency_region", "missing")
	}
	if strings.TrimSpace(f.DataProcessing.SubprocessorDisclosure) == "" {
		add("data_processing.subprocessor_disclosure", "missing")
	}
	if strings.TrimSpace(f.DataProcessing.DPAReference) == "" {
		add("data_processing.dpa_reference", "missing")
	}

	// --- RED: "bills replay or repair duplicates" ---
	if strings.TrimSpace(f.Billing.Model) == "" {
		add("billing.model", "missing")
	}
	if f.Billing.ReplayBillable {
		add("billing.replay_billable", "must be false - a replay is never a second billable event")
	}
	if f.Billing.RepairBillable {
		add("billing.repair_billable", "must be false - a repair is never a second billable event")
	}
	if strings.TrimSpace(f.Billing.IdempotencyBasis) == "" {
		add("billing.idempotency_basis", "missing")
	}

	// --- GREEN: "quantitative reprice/stop criteria" ---
	if f.RepriceStop.RepriceThresholdPct <= 0 {
		add("reprice_stop.reprice_threshold_pct", "must be positive")
	}
	if f.RepriceStop.StopThresholdPct <= f.RepriceStop.RepriceThresholdPct {
		add("reprice_stop.stop_threshold_pct", "must exceed reprice_threshold_pct")
	}

	if f.Signature == nil {
		add("signature", "missing - a commercial freeze must be signed")
	} else {
		if f.Signature.Algorithm != "ed25519" {
			add("signature.algorithm", fmt.Sprintf("unsupported algorithm %q", f.Signature.Algorithm))
		}
		if f.Signature.PublicKey == "" {
			add("signature.public_key", "missing")
		}
		if f.Signature.Value == "" {
			add("signature.value", "missing")
		}
	}

	return violations
}

// digestPayload is the canonical projection hashed by CanonicalDigest and
// signed by Sign/Verify: every freeze field except the signature itself.
type digestPayload struct {
	SchemaVersion       int                 `json:"schema_version"`
	TodoID              string              `json:"todo_id"`
	SignedDate          string              `json:"signed_date"`
	Intent              IntentPromise       `json:"intent"`
	Jurisdiction        JurisdictionPromise `json:"jurisdiction"`
	Provider            ProviderPromise     `json:"provider"`
	Evidence            EvidenceAndSLO      `json:"evidence"`
	Pricing             PricingHypothesis   `json:"pricing"`
	Authority           AuthorityBoundary   `json:"authority"`
	ProviderPassThrough ProviderPassThrough `json:"provider_pass_through"`
	Exit                ExitTerms           `json:"exit"`
	DataProcessing      DataProcessingTerms `json:"data_processing"`
	Billing             BillingPolicy       `json:"billing"`
	RepriceStop         RepriceStopCriteria `json:"reprice_stop"`
}

func (f PilotCommercialFreeze) payload() digestPayload {
	return digestPayload{
		SchemaVersion:       f.SchemaVersion,
		TodoID:              f.TodoID,
		SignedDate:          f.SignedDate,
		Intent:              f.Intent,
		Jurisdiction:        f.Jurisdiction,
		Provider:            f.Provider,
		Evidence:            f.Evidence,
		Pricing:             f.Pricing,
		Authority:           f.Authority,
		ProviderPassThrough: f.ProviderPassThrough,
		Exit:                f.Exit,
		DataProcessing:      f.DataProcessing,
		Billing:             f.Billing,
		RepriceStop:         f.RepriceStop,
	}
}

// CanonicalDigest returns the hex-encoded sha256 digest of f's canonical
// JSON projection (every field except Signature).
func (f PilotCommercialFreeze) CanonicalDigest() (string, error) {
	b, err := json.Marshal(f.payload())
	if err != nil {
		return "", fmt.Errorf("marshal canonical freeze: %w", err)
	}
	sum := sha256.Sum256(b)
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
