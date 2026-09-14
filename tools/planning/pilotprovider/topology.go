// Package pilotprovider implements SELECT-002: the signed pilot provider
// topology that binds the ChangeOps Promotion + Compensation Change wedge to
// one design-partner system's product, edition, region, API entitlements,
// sandbox evidence, fault behavior, credential model, data-processing terms
// and exit posture, exactly the way tools/planning/pilotjurisdiction's
// JurisdictionProfile did for SELECT-001's jurisdiction slot. See doc.go for
// why the checked-in provider is a structurally-marked placeholder rather
// than a real vendor.
package pilotprovider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// SelectionStatus is the closed vocabulary for whether a ProviderTopology
// names a procured, contracted vendor or an unverified placeholder standing
// in for one. This is the provider-topology analogue of
// definitions/legal/packs/states/us-ca.json's review_status: UNREVIEWED.
type SelectionStatus string

const (
	// StatusPlaceholderUnverified marks a topology whose provider identity
	// is a structural stand-in: no procurement, contract or vendor contact
	// has confirmed it. Every checked-in topology today carries this status.
	StatusPlaceholderUnverified SelectionStatus = "PLACEHOLDER_UNVERIFIED"
	// StatusVendorConfirmed marks a topology a named vendor contact and
	// contract document have actually confirmed. Validate refuses this
	// status unless VendorConfirmation is fully populated and Provider.VendorID
	// no longer carries PlaceholderSuffix (see Validate and
	// SatisfiesRealProviderSelectionGate).
	StatusVendorConfirmed SelectionStatus = "VENDOR_CONFIRMED"
)

// PlaceholderSuffix is the mandatory, Validate-enforced marker every
// placeholder Provider.VendorID and Provider.ContractID in this package must
// carry - the provider-topology analogue of the fifty state rule-packs'
// "-draft" pack_id suffix. It is not decorative: Validate rejects a
// PLACEHOLDER_UNVERIFIED topology whose VendorID lacks it, and
// SatisfiesRealProviderSelectionGate rejects a VENDOR_CONFIRMED topology
// whose VendorID still carries it.
const PlaceholderSuffix = "-PLACEHOLDER-UNVERIFIED"

// Provider names the design-partner system this topology binds to: product,
// edition, region and API version, the exact facts
// competitive-positioning-and-authority-expansion.md's Coexistence
// Architecture section requires ("Connector definitions must preserve
// edition, API/version, tenant configuration, licensed feature...").
type Provider struct {
	VendorID          string `yaml:"vendor_id" json:"vendor_id"`
	VendorDisplayName string `yaml:"vendor_display_name" json:"vendor_display_name"`
	Product           string `yaml:"product" json:"product"`
	Edition           string `yaml:"edition" json:"edition"`
	Region            string `yaml:"region" json:"region"`
	APIVersion        string `yaml:"api_version" json:"api_version"`
}

// APIEntitlement is one named API scope/entitlement this topology relies on
// (RED: "API entitlement").
type APIEntitlement struct {
	Scope       string `yaml:"scope" json:"scope"`
	Description string `yaml:"description" json:"description"`
	Granted     bool   `yaml:"granted" json:"granted"`
}

// Closed sandbox-fidelity vocabulary (RED: "sandbox fidelity").
const (
	SandboxFidelitySyntheticFixtureOnly = "SYNTHETIC_FIXTURE_ONLY"
	SandboxFidelityVendorSandboxTenant  = "VENDOR_SANDBOX_TENANT"
	SandboxFidelityVendorProductionLike = "VENDOR_SANDBOX_TENANT_PRODUCTION_LIKE"
)

var validSandboxFidelities = map[string]bool{
	SandboxFidelitySyntheticFixtureOnly: true,
	SandboxFidelityVendorSandboxTenant:  true,
	SandboxFidelityVendorProductionLike: true,
}

// SandboxEvidence declares how sandbox behavior was actually exercised, and
// where a caller can inspect the evidence (RED: "sandbox fidelity"; GREEN:
// "includes sandbox evidence").
type SandboxEvidence struct {
	Fidelity    string `yaml:"fidelity" json:"fidelity"`
	Description string `yaml:"description" json:"description"`
	EvidenceRef string `yaml:"evidence_ref" json:"evidence_ref"`
	ExecutedAt  string `yaml:"executed_at" json:"executed_at"`
}

// Closed field-authority vocabulary (RED: "authority-by-field").
const (
	AuthorityHCMNext            = "HCM_NEXT"
	AuthorityProvider           = "PROVIDER"
	AuthoritySharedWithConflict = "SHARED_WITH_CONFLICT_POLICY"
)

var validFieldAuthorities = map[string]bool{
	AuthorityHCMNext:            true,
	AuthorityProvider:           true,
	AuthoritySharedWithConflict: true,
}

// FieldAuthority declares which system is authoritative for one field this
// topology's operations touch (RED: "authority-by-field").
type FieldAuthority struct {
	Field     string `yaml:"field" json:"field"`
	Authority string `yaml:"authority" json:"authority"`
	Rationale string `yaml:"rationale" json:"rationale"`
}

// Closed observation-mechanism vocabulary (RED: "webhook/polling behavior").
const (
	ObservationWebhook = "WEBHOOK"
	ObservationPolling = "POLLING"
	ObservationBoth    = "BOTH"
)

var validObservationMechanisms = map[string]bool{
	ObservationWebhook: true,
	ObservationPolling: true,
	ObservationBoth:    true,
}

// ObservationPath declares how this topology observes external state
// (RED: "webhook/polling behavior"), and whether that observation is
// independent of any write acknowledgement - the fact GREEN's "proves one
// cross-system independently observed outcome" depends on.
type ObservationPath struct {
	Mechanism              string `yaml:"mechanism" json:"mechanism"`
	PollIntervalSeconds    int    `yaml:"poll_interval_seconds,omitempty" json:"poll_interval_seconds,omitempty"`
	WebhookSignatureScheme string `yaml:"webhook_signature_scheme,omitempty" json:"webhook_signature_scheme,omitempty"`
	IndependentOfWriteAck  bool   `yaml:"independent_of_write_ack" json:"independent_of_write_ack"`
}

// Quota declares this connection's vendor-side rate/concurrency limits
// (RED: "quotas"), matching integration-platform.md's ExternalCapacity
// shape.
type Quota struct {
	LimitWindow      string `yaml:"limit_window" json:"limit_window"`
	RequestLimit     int    `yaml:"request_limit" json:"request_limit"`
	ConcurrencyLimit int    `yaml:"concurrency_limit" json:"concurrency_limit"`
	BurstPolicy      string `yaml:"burst_policy" json:"burst_policy"`
}

// AmbiguousOutcomeObserveBeforeRetry is the only accepted
// TimeoutPolicy.AmbiguousOutcomeAction value: integration-platform.md's
// Error Taxonomy names "Observe before retry" as the default action for a
// "timeout after request may have applied" ambiguous outcome, and this
// package never lets a topology declare a different, unreviewed policy
// (RED: "timeout ambiguity").
const AmbiguousOutcomeObserveBeforeRetry = "OBSERVE_BEFORE_RETRY"

// TimeoutPolicy declares this connection's request timeout and what happens
// when a timeout leaves the outcome ambiguous (RED: "timeout ambiguity").
type TimeoutPolicy struct {
	RequestTimeoutSeconds  int    `yaml:"request_timeout_seconds" json:"request_timeout_seconds"`
	AmbiguousOutcomeAction string `yaml:"ambiguous_outcome_action" json:"ambiguous_outcome_action"`
}

// Closed credential-scheme vocabulary (RED: "credential model"), drawn
// directly from integration-platform.md's Authentication section.
const (
	CredentialOAuth2ClientCredentials = "OAUTH2_CLIENT_CREDENTIALS"
	CredentialAPIKey                  = "API_KEY"
	CredentialMTLS                    = "MTLS"
	CredentialSignedWebhookSecret     = "SIGNED_WEBHOOK_SECRET"
)

var validCredentialSchemes = map[string]bool{
	CredentialOAuth2ClientCredentials: true,
	CredentialAPIKey:                  true,
	CredentialMTLS:                    true,
	CredentialSignedWebhookSecret:     true,
}

// CredentialModel declares how this connection authenticates and how often
// its credential is rotated (RED: "credential model").
type CredentialModel struct {
	Scheme       string   `yaml:"scheme" json:"scheme"`
	RotationDays int      `yaml:"rotation_days" json:"rotation_days"`
	ScopeGranted []string `yaml:"scope_granted" json:"scope_granted"`
}

// DataProcessingTerms declares this connection's data-residency, retention
// and subprocessor posture (RED: "data-processing terms"). On the checked-in
// placeholder topology, DPAReference deliberately states that no data
// processing agreement has actually been executed rather than fabricating a
// reference number - see doc.go.
type DataProcessingTerms struct {
	ResidencyRegion        string `yaml:"residency_region" json:"residency_region"`
	RetentionDays          int    `yaml:"retention_days" json:"retention_days"`
	SubprocessorDisclosure string `yaml:"subprocessor_disclosure" json:"subprocessor_disclosure"`
	DPAReference           string `yaml:"dpa_reference" json:"dpa_reference"`
}

// Closed operation-verb vocabulary: integration-platform.md's five
// Architectural Role verbs.
const (
	VerbRead      = "READ"
	VerbWrite     = "WRITE"
	VerbSubscribe = "SUBSCRIBE"
	VerbObserve   = "OBSERVE"
	VerbReconcile = "RECONCILE"
)

var allOperationVerbs = []string{VerbRead, VerbWrite, VerbSubscribe, VerbObserve, VerbReconcile}

var validOperationVerbs = map[string]bool{
	VerbRead: true, VerbWrite: true, VerbSubscribe: true, VerbObserve: true, VerbReconcile: true,
}

// OperationMapping binds one Phase 1 read/write/effect/observation verb to
// the exact contract, version and owner this topology relies on (GREEN:
// "maps each Phase 1 read/write/effect/observation to exact contract and
// version and owner").
type OperationMapping struct {
	Verb                 string `yaml:"verb" json:"verb"`
	CapabilityOrIntentID string `yaml:"capability_or_intent_id" json:"capability_or_intent_id"`
	ContractID           string `yaml:"contract_id" json:"contract_id"`
	ContractVersion      string `yaml:"contract_version" json:"contract_version"`
	Owner                string `yaml:"owner" json:"owner"`
}

// Closed fault-class vocabulary: the exact nine rows of
// integration-platform.md's Error Taxonomy and Redrive table. Every one of
// these must appear in Faults exactly once (see Validate) - the fault-matrix
// analogue of pilotjurisdiction's exact obligation-kind coverage.
const (
	FaultAuthentication        = "AUTHENTICATION"
	FaultAuthorizationScope    = "AUTHORIZATION_SCOPE"
	FaultValidation            = "VALIDATION"
	FaultConflict              = "CONFLICT"
	FaultRateLimited           = "RATE_LIMITED"
	FaultTransientDependency   = "TRANSIENT_DEPENDENCY"
	FaultAmbiguousOutcome      = "AMBIGUOUS_OUTCOME"
	FaultSchemaIncompatibility = "SCHEMA_INCOMPATIBILITY"
	FaultFinalBusinessReject   = "FINAL_BUSINESS_REJECT"
)

// AllFaultClasses is the exact, closed set Faults must cover once each.
func AllFaultClasses() []string {
	return []string{
		FaultAuthentication, FaultAuthorizationScope, FaultValidation, FaultConflict,
		FaultRateLimited, FaultTransientDependency, FaultAmbiguousOutcome,
		FaultSchemaIncompatibility, FaultFinalBusinessReject,
	}
}

var validFaultClasses = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range AllFaultClasses() {
		m[c] = true
	}
	return m
}()

// FaultCase is one row of this topology's fault matrix (GREEN: "includes
// ... fault matrix").
type FaultCase struct {
	Class              string `yaml:"class" json:"class"`
	ExampleResponse    string `yaml:"example_response" json:"example_response"`
	DefaultAction      string `yaml:"default_action" json:"default_action"`
	SimulatedInSandbox bool   `yaml:"simulated_in_sandbox" json:"simulated_in_sandbox"`
	EvidenceRef        string `yaml:"evidence_ref" json:"evidence_ref"`
}

// CrossSystemObservation declares the one cross-system, independently
// observed outcome GREEN requires ("proves one cross-system independently
// observed outcome") - an observation that does not merely trust a write
// acknowledgement as proof the external state changed.
type CrossSystemObservation struct {
	Description                             string `yaml:"description" json:"description"`
	Method                                  string `yaml:"method" json:"method"`
	ProvesIndependentOfWriteAcknowledgement bool   `yaml:"proves_independent_of_write_acknowledgement" json:"proves_independent_of_write_acknowledgement"`
	EvidenceRef                             string `yaml:"evidence_ref" json:"evidence_ref"`
}

// ExitPlan declares this topology's fallback/exit posture across the exact
// six dimensions OPS-009 rehearses for vendor continuity (RED: "fallback/
// exit"): stop new work, preserve or reconcile in-flight effects, revoke
// credentials, activate fallback, drain/rollback/reopen and expire
// exceptions with evidence.
type ExitPlan struct {
	StopNewWork                 string `yaml:"stop_new_work" json:"stop_new_work"`
	PreserveOrReconcileInFlight string `yaml:"preserve_or_reconcile_in_flight" json:"preserve_or_reconcile_in_flight"`
	RevokeCredentials           string `yaml:"revoke_credentials" json:"revoke_credentials"`
	ActivateFallback            string `yaml:"activate_fallback" json:"activate_fallback"`
	DrainRollbackReopen         string `yaml:"drain_rollback_reopen" json:"drain_rollback_reopen"`
	ExceptionExpiry             string `yaml:"exception_expiry" json:"exception_expiry"`
}

// CommercialLimit names one measurable commercial ceiling this topology's
// pilot use must respect (RED: "commercial limit").
type CommercialLimit struct {
	Metric        string `yaml:"metric" json:"metric"`
	Limit         string `yaml:"limit" json:"limit"`
	OveragePolicy string `yaml:"overage_policy" json:"overage_policy"`
}

// Stop/reselect actions, identical vocabulary to
// tools/planning/pilotjurisdiction's.
const (
	ActionProceed       = "PROCEED"
	ActionReselectWedge = "RESELECT_WEDGE"
	ActionStop          = "STOP"
)

var validActions = map[string]bool{
	ActionProceed:       true,
	ActionReselectWedge: true,
	ActionStop:          true,
}

// StopReselectThreshold is one numeric or named threshold that maps
// measured pilot evidence to a proceed/reselect/stop action for this
// provider selection (GREEN: "defines stop/reselect conditions").
type StopReselectThreshold struct {
	Metric    string `yaml:"metric" json:"metric"`
	Threshold string `yaml:"threshold" json:"threshold"`
	Action    string `yaml:"action" json:"action"`
}

// VendorConfirmation names the human contact, contract document and
// effective date that actually confirmed this provider selection. It is
// deliberately empty on the checked-in placeholder topology - see doc.go -
// exactly as tools/planning/pilotjurisdiction.Reviewer is deliberately
// empty on SELECT-001's checked-in profile.
type VendorConfirmation struct {
	ConfirmedByContact  string `yaml:"confirmed_by_contact" json:"confirmed_by_contact"`
	ContractDocumentRef string `yaml:"contract_document_ref" json:"contract_document_ref"`
	SignedEffectiveDate string `yaml:"signed_effective_date" json:"signed_effective_date"`
}

// VendorConfirmation reports whether c carries any populated field.
func (c VendorConfirmation) anyPopulated() bool {
	return strings.TrimSpace(c.ConfirmedByContact) != "" ||
		strings.TrimSpace(c.ContractDocumentRef) != "" ||
		strings.TrimSpace(c.SignedEffectiveDate) != ""
}

// VendorConfirmation reports whether c carries every field.
func (c VendorConfirmation) allPopulated() bool {
	return strings.TrimSpace(c.ConfirmedByContact) != "" &&
		strings.TrimSpace(c.ContractDocumentRef) != "" &&
		strings.TrimSpace(c.SignedEffectiveDate) != ""
}

// Signature is an ed25519 signature over a ProviderTopology's
// CanonicalDigest, structurally identical to
// tools/planning/pilotjurisdiction.Signature and
// tools/planning/gateevidence.Signature.
type Signature struct {
	Algorithm  string `yaml:"algorithm" json:"algorithm"`
	PublicKey  string `yaml:"public_key" json:"public_key"`
	Value      string `yaml:"value" json:"value"`
	KeyFixture string `yaml:"key_fixture" json:"key_fixture"`
}

// ProviderTopology is SELECT-002's signed pilot provider topology
// (definitions/planning/gates/select-002-provider-topology.yaml).
type ProviderTopology struct {
	SchemaVersion   int             `yaml:"schema_version" json:"schema_version"`
	TodoID          string          `yaml:"todo_id" json:"todo_id"`
	SignedDate      string          `yaml:"signed_date" json:"signed_date"`
	SelectionStatus SelectionStatus `yaml:"selection_status" json:"selection_status"`

	Provider           Provider           `yaml:"provider" json:"provider"`
	VendorConfirmation VendorConfirmation `yaml:"vendor_confirmation" json:"vendor_confirmation"`

	APIEntitlements  []APIEntitlement    `yaml:"api_entitlements" json:"api_entitlements"`
	Sandbox          SandboxEvidence     `yaml:"sandbox" json:"sandbox"`
	FieldAuthorities []FieldAuthority    `yaml:"field_authorities" json:"field_authorities"`
	Observation      ObservationPath     `yaml:"observation" json:"observation"`
	Quota            Quota               `yaml:"quota" json:"quota"`
	Timeout          TimeoutPolicy       `yaml:"timeout" json:"timeout"`
	Credential       CredentialModel     `yaml:"credential" json:"credential"`
	DataProcessing   DataProcessingTerms `yaml:"data_processing" json:"data_processing"`

	Operations             []OperationMapping      `yaml:"operations" json:"operations"`
	Faults                 []FaultCase             `yaml:"faults" json:"faults"`
	CrossSystemObservation CrossSystemObservation  `yaml:"cross_system_observation" json:"cross_system_observation"`
	Exit                   ExitPlan                `yaml:"exit" json:"exit"`
	Commercial             CommercialLimit         `yaml:"commercial" json:"commercial"`
	StopReselectThresholds []StopReselectThreshold `yaml:"stop_reselect_thresholds" json:"stop_reselect_thresholds"`

	Signature *Signature `yaml:"signature,omitempty" json:"signature,omitempty"`
}

// LoadTopology reads and parses a ProviderTopology YAML file.
func LoadTopology(path string) (*ProviderTopology, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var t ProviderTopology
	if err := yaml.Unmarshal(content, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &t, nil
}

// Violation names one structural defect. Field is dotted for list entries so
// a test can name exactly what a mutation broke.
type Violation struct {
	Field string
	Issue string
}

func (v Violation) String() string { return fmt.Sprintf("%s: %s", v.Field, v.Issue) }

// Validate returns every structural violation on t. It performs no file I/O
// and never consults the vendor's real-world identity: it checks that the
// topology is internally well-formed, that its selection-status claim is
// consistent with its own vendor-identity and vendor-confirmation fields,
// and that its fault-matrix coverage is exact.
func (t ProviderTopology) Validate() []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if t.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	if t.TodoID != "SELECT-002" {
		add("todo_id", "must be SELECT-002")
	}
	if strings.TrimSpace(t.SignedDate) == "" {
		add("signed_date", "missing")
	}

	// --- RED: generic provider name is not accepted; product/edition/region/
	// API version must all be present, and the selection-status claim must
	// be structurally consistent with the vendor identity and confirmation.
	switch t.SelectionStatus {
	case StatusPlaceholderUnverified:
		if !strings.HasSuffix(t.Provider.VendorID, PlaceholderSuffix) {
			add("provider.vendor_id", fmt.Sprintf("selection_status is %s but vendor_id %q does not carry the required placeholder suffix %q", t.SelectionStatus, t.Provider.VendorID, PlaceholderSuffix))
		}
		if t.VendorConfirmation.anyPopulated() {
			add("vendor_confirmation", "selection_status is PLACEHOLDER_UNVERIFIED but vendor_confirmation carries a populated field - a placeholder must not claim partial vendor confirmation")
		}
	case StatusVendorConfirmed:
		if strings.HasSuffix(t.Provider.VendorID, PlaceholderSuffix) {
			add("provider.vendor_id", "selection_status is VENDOR_CONFIRMED but vendor_id still carries the placeholder suffix")
		}
		if !t.VendorConfirmation.allPopulated() {
			add("vendor_confirmation", "selection_status is VENDOR_CONFIRMED but vendor_confirmation is not fully populated")
		}
	default:
		add("selection_status", fmt.Sprintf("must be %s or %s, got %q", StatusPlaceholderUnverified, StatusVendorConfirmed, t.SelectionStatus))
	}

	if strings.TrimSpace(t.Provider.VendorID) == "" {
		add("provider.vendor_id", "missing")
	}
	if strings.TrimSpace(t.Provider.VendorDisplayName) == "" {
		add("provider.vendor_display_name", "missing")
	}
	if strings.TrimSpace(t.Provider.Product) == "" {
		add("provider.product", "missing - a generic provider name without a product is not accepted")
	}
	if strings.TrimSpace(t.Provider.Edition) == "" {
		add("provider.edition", "missing - a generic provider name without an edition is not accepted")
	}
	if strings.TrimSpace(t.Provider.Region) == "" {
		add("provider.region", "missing - a generic provider name without a region is not accepted")
	}
	if strings.TrimSpace(t.Provider.APIVersion) == "" {
		add("provider.api_version", "missing - a generic provider name without an API version is not accepted")
	}

	// --- RED: API entitlements ---
	if len(t.APIEntitlements) == 0 {
		add("api_entitlements", "missing - the topology names no API entitlements")
	}
	for i, e := range t.APIEntitlements {
		field := fmt.Sprintf("api_entitlements[%d]", i)
		if strings.TrimSpace(e.Scope) == "" {
			add(field+".scope", "missing")
		}
		if strings.TrimSpace(e.Description) == "" {
			add(field+".description", "missing")
		}
	}

	// --- RED: sandbox fidelity ---
	if !validSandboxFidelities[t.Sandbox.Fidelity] {
		add("sandbox.fidelity", fmt.Sprintf("unknown sandbox fidelity %q", t.Sandbox.Fidelity))
	}
	if strings.TrimSpace(t.Sandbox.Description) == "" {
		add("sandbox.description", "missing")
	}
	if strings.TrimSpace(t.Sandbox.EvidenceRef) == "" {
		add("sandbox.evidence_ref", "missing")
	}
	if strings.TrimSpace(t.Sandbox.ExecutedAt) == "" {
		add("sandbox.executed_at", "missing")
	}

	// --- RED: authority-by-field ---
	if len(t.FieldAuthorities) == 0 {
		add("field_authorities", "missing - the topology declares authority for no field")
	}
	for i, f := range t.FieldAuthorities {
		field := fmt.Sprintf("field_authorities[%d]", i)
		if strings.TrimSpace(f.Field) == "" {
			add(field+".field", "missing")
		}
		if !validFieldAuthorities[f.Authority] {
			add(field+".authority", fmt.Sprintf("unknown authority %q", f.Authority))
		}
		if strings.TrimSpace(f.Rationale) == "" {
			add(field+".rationale", "missing")
		}
	}

	// --- RED: webhook/polling behavior ---
	if !validObservationMechanisms[t.Observation.Mechanism] {
		add("observation.mechanism", fmt.Sprintf("unknown mechanism %q", t.Observation.Mechanism))
	}
	if t.Observation.Mechanism == ObservationPolling || t.Observation.Mechanism == ObservationBoth {
		if t.Observation.PollIntervalSeconds <= 0 {
			add("observation.poll_interval_seconds", "must be positive when polling is used")
		}
	}
	if t.Observation.Mechanism == ObservationWebhook || t.Observation.Mechanism == ObservationBoth {
		if strings.TrimSpace(t.Observation.WebhookSignatureScheme) == "" {
			add("observation.webhook_signature_scheme", "missing when webhook delivery is used")
		}
	}

	// --- RED: quotas ---
	if strings.TrimSpace(t.Quota.LimitWindow) == "" {
		add("quota.limit_window", "missing")
	}
	if t.Quota.RequestLimit <= 0 {
		add("quota.request_limit", "must be positive")
	}
	if t.Quota.ConcurrencyLimit <= 0 {
		add("quota.concurrency_limit", "must be positive")
	}
	if strings.TrimSpace(t.Quota.BurstPolicy) == "" {
		add("quota.burst_policy", "missing")
	}

	// --- RED: timeout ambiguity ---
	if t.Timeout.RequestTimeoutSeconds <= 0 {
		add("timeout.request_timeout_seconds", "must be positive")
	}
	if t.Timeout.AmbiguousOutcomeAction != AmbiguousOutcomeObserveBeforeRetry {
		add("timeout.ambiguous_outcome_action", fmt.Sprintf("must be %s, got %q", AmbiguousOutcomeObserveBeforeRetry, t.Timeout.AmbiguousOutcomeAction))
	}

	// --- RED: credential model ---
	if !validCredentialSchemes[t.Credential.Scheme] {
		add("credential.scheme", fmt.Sprintf("unknown credential scheme %q", t.Credential.Scheme))
	}
	if t.Credential.RotationDays <= 0 {
		add("credential.rotation_days", "must be positive")
	}
	if len(t.Credential.ScopeGranted) == 0 {
		add("credential.scope_granted", "missing")
	}

	// --- RED: data-processing terms ---
	if strings.TrimSpace(t.DataProcessing.ResidencyRegion) == "" {
		add("data_processing.residency_region", "missing")
	}
	if t.DataProcessing.RetentionDays <= 0 {
		add("data_processing.retention_days", "must be positive")
	}
	if strings.TrimSpace(t.DataProcessing.SubprocessorDisclosure) == "" {
		add("data_processing.subprocessor_disclosure", "missing")
	}
	if strings.TrimSpace(t.DataProcessing.DPAReference) == "" {
		add("data_processing.dpa_reference", "missing")
	}

	// --- GREEN: exact operation-verb coverage, each with contract/version/owner ---
	if len(t.Operations) == 0 {
		add("operations", "missing - the topology maps no Phase 1 read/write/effect/observation")
	}
	seenVerb := map[string]bool{}
	for i, o := range t.Operations {
		field := fmt.Sprintf("operations[%d]", i)
		if !validOperationVerbs[o.Verb] {
			add(field+".verb", fmt.Sprintf("unknown verb %q", o.Verb))
		} else {
			seenVerb[o.Verb] = true
		}
		if strings.TrimSpace(o.CapabilityOrIntentID) == "" {
			add(field+".capability_or_intent_id", "missing")
		}
		if strings.TrimSpace(o.ContractID) == "" {
			add(field+".contract_id", "missing - exact contract required")
		}
		if strings.TrimSpace(o.ContractVersion) == "" {
			add(field+".contract_version", "missing - exact version required")
		}
		if strings.TrimSpace(o.Owner) == "" {
			add(field+".owner", "missing - exact owner required")
		}
	}
	for _, verb := range allOperationVerbs {
		if !seenVerb[verb] {
			add("operations", fmt.Sprintf("missing an operation mapping for verb %s", verb))
		}
	}

	// --- GREEN: exact fault-matrix coverage, present and exact ---
	if len(t.Faults) == 0 {
		add("faults", "missing - the topology carries no fault matrix")
	}
	seenFault := map[string]bool{}
	for i, f := range t.Faults {
		field := fmt.Sprintf("faults[%d]", i)
		if !validFaultClasses[f.Class] {
			add(field+".class", fmt.Sprintf("unknown fault class %q", f.Class))
		} else if seenFault[f.Class] {
			add(field+".class", fmt.Sprintf("duplicate fault class %s", f.Class))
		} else {
			seenFault[f.Class] = true
		}
		if strings.TrimSpace(f.ExampleResponse) == "" {
			add(field+".example_response", "missing")
		}
		if strings.TrimSpace(f.DefaultAction) == "" {
			add(field+".default_action", "missing")
		}
		if strings.TrimSpace(f.EvidenceRef) == "" {
			add(field+".evidence_ref", "missing")
		}
	}
	for _, class := range AllFaultClasses() {
		if !seenFault[class] {
			add("faults", fmt.Sprintf("missing required fault class %s", class))
		}
	}

	// --- GREEN: one cross-system independently observed outcome ---
	if strings.TrimSpace(t.CrossSystemObservation.Description) == "" {
		add("cross_system_observation.description", "missing")
	}
	if !validObservationMechanisms[t.CrossSystemObservation.Method] || t.CrossSystemObservation.Method == ObservationBoth {
		add("cross_system_observation.method", fmt.Sprintf("must be %s or %s, got %q", ObservationWebhook, ObservationPolling, t.CrossSystemObservation.Method))
	}
	if !t.CrossSystemObservation.ProvesIndependentOfWriteAcknowledgement {
		add("cross_system_observation.proves_independent_of_write_acknowledgement", "must be true - an observation that only trusts the write acknowledgement proves nothing cross-system")
	}
	if strings.TrimSpace(t.CrossSystemObservation.EvidenceRef) == "" {
		add("cross_system_observation.evidence_ref", "missing")
	}

	// --- RED: fallback/exit ---
	if strings.TrimSpace(t.Exit.StopNewWork) == "" {
		add("exit.stop_new_work", "missing")
	}
	if strings.TrimSpace(t.Exit.PreserveOrReconcileInFlight) == "" {
		add("exit.preserve_or_reconcile_in_flight", "missing")
	}
	if strings.TrimSpace(t.Exit.RevokeCredentials) == "" {
		add("exit.revoke_credentials", "missing")
	}
	if strings.TrimSpace(t.Exit.ActivateFallback) == "" {
		add("exit.activate_fallback", "missing")
	}
	if strings.TrimSpace(t.Exit.DrainRollbackReopen) == "" {
		add("exit.drain_rollback_reopen", "missing")
	}
	if strings.TrimSpace(t.Exit.ExceptionExpiry) == "" {
		add("exit.exception_expiry", "missing")
	}

	// --- RED: commercial limit ---
	if strings.TrimSpace(t.Commercial.Metric) == "" {
		add("commercial.metric", "missing")
	}
	if strings.TrimSpace(t.Commercial.Limit) == "" {
		add("commercial.limit", "missing")
	}
	if strings.TrimSpace(t.Commercial.OveragePolicy) == "" {
		add("commercial.overage_policy", "missing")
	}

	// --- GREEN: stop/reselect conditions ---
	if len(t.StopReselectThresholds) == 0 {
		add("stop_reselect_thresholds", "missing")
	}
	for i, th := range t.StopReselectThresholds {
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

	if t.Signature == nil {
		add("signature", "missing - a provider topology must be signed")
	} else {
		if t.Signature.Algorithm != "ed25519" {
			add("signature.algorithm", fmt.Sprintf("unsupported algorithm %q", t.Signature.Algorithm))
		}
		if t.Signature.PublicKey == "" {
			add("signature.public_key", "missing")
		}
		if t.Signature.Value == "" {
			add("signature.value", "missing")
		}
	}

	return violations
}

// SatisfiesRealProviderSelectionGate reports whether t is capable of
// authorizing a real EXTERNAL_PROVIDER_WRITE effect - the check any
// downstream gate (NEXT-002 or a future release gate) must run before
// treating a ProviderTopology as a procured, contracted selection rather
// than this package's structural placeholder. It is deliberately
// independent of Validate: Validate only checks internal consistency of
// whatever selection_status a topology claims, while this function checks
// the substantive claim a real gate cares about, by value - a topology
// cannot pass it merely by setting SelectionStatus to StatusVendorConfirmed
// (a flag) while its Provider.VendorID still carries the placeholder suffix
// or its VendorConfirmation remains unpopulated (the values that actually
// back up the claim).
func (t ProviderTopology) SatisfiesRealProviderSelectionGate() (bool, []Violation) {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if t.SelectionStatus != StatusVendorConfirmed {
		add("selection_status", fmt.Sprintf("a real provider selection gate requires %s, got %q", StatusVendorConfirmed, t.SelectionStatus))
	}
	if strings.HasSuffix(t.Provider.VendorID, PlaceholderSuffix) {
		add("provider.vendor_id", fmt.Sprintf("carries the placeholder suffix %q - a real selection gate cannot accept a placeholder-marked vendor identifier", PlaceholderSuffix))
	}
	if strings.TrimSpace(t.Provider.VendorID) == "" {
		add("provider.vendor_id", "missing")
	}
	if strings.TrimSpace(t.VendorConfirmation.ConfirmedByContact) == "" {
		add("vendor_confirmation.confirmed_by_contact", "missing - a real selection gate requires a named confirming contact")
	}
	if strings.TrimSpace(t.VendorConfirmation.ContractDocumentRef) == "" {
		add("vendor_confirmation.contract_document_ref", "missing - a real selection gate requires a contract document reference")
	}
	if strings.TrimSpace(t.VendorConfirmation.SignedEffectiveDate) == "" {
		add("vendor_confirmation.signed_effective_date", "missing - a real selection gate requires a signed effective date")
	}

	return len(violations) == 0, violations
}

// digestPayload is the canonical projection hashed by CanonicalDigest and
// signed by Sign/Verify: every topology field except the signature itself.
type digestPayload struct {
	SchemaVersion          int                     `json:"schema_version"`
	TodoID                 string                  `json:"todo_id"`
	SignedDate             string                  `json:"signed_date"`
	SelectionStatus        SelectionStatus         `json:"selection_status"`
	Provider               Provider                `json:"provider"`
	VendorConfirmation     VendorConfirmation      `json:"vendor_confirmation"`
	APIEntitlements        []APIEntitlement        `json:"api_entitlements"`
	Sandbox                SandboxEvidence         `json:"sandbox"`
	FieldAuthorities       []FieldAuthority        `json:"field_authorities"`
	Observation            ObservationPath         `json:"observation"`
	Quota                  Quota                   `json:"quota"`
	Timeout                TimeoutPolicy           `json:"timeout"`
	Credential             CredentialModel         `json:"credential"`
	DataProcessing         DataProcessingTerms     `json:"data_processing"`
	Operations             []OperationMapping      `json:"operations"`
	Faults                 []FaultCase             `json:"faults"`
	CrossSystemObservation CrossSystemObservation  `json:"cross_system_observation"`
	Exit                   ExitPlan                `json:"exit"`
	Commercial             CommercialLimit         `json:"commercial"`
	StopReselectThresholds []StopReselectThreshold `json:"stop_reselect_thresholds"`
}

func (t ProviderTopology) payload() digestPayload {
	return digestPayload{
		SchemaVersion:          t.SchemaVersion,
		TodoID:                 t.TodoID,
		SignedDate:             t.SignedDate,
		SelectionStatus:        t.SelectionStatus,
		Provider:               t.Provider,
		VendorConfirmation:     t.VendorConfirmation,
		APIEntitlements:        t.APIEntitlements,
		Sandbox:                t.Sandbox,
		FieldAuthorities:       t.FieldAuthorities,
		Observation:            t.Observation,
		Quota:                  t.Quota,
		Timeout:                t.Timeout,
		Credential:             t.Credential,
		DataProcessing:         t.DataProcessing,
		Operations:             t.Operations,
		Faults:                 t.Faults,
		CrossSystemObservation: t.CrossSystemObservation,
		Exit:                   t.Exit,
		Commercial:             t.Commercial,
		StopReselectThresholds: t.StopReselectThresholds,
	}
}

// CanonicalDigest returns the hex-encoded sha256 digest of t's canonical
// JSON projection (every field except Signature).
func (t ProviderTopology) CanonicalDigest() (string, error) {
	b, err := json.Marshal(t.payload())
	if err != nil {
		return "", fmt.Errorf("marshal canonical topology: %w", err)
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
