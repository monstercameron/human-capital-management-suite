// Package commercial contains the machine-readable commercial boundary for the
// first paid release. It is deliberately separate from runtime and billing:
// this package describes what may be sold, while entitlement and execution
// registries remain the authority for what the product can do.
package commercial

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// P1AManifestDigest is the CanonicalDigest of the signed
// definitions/planning/gates/p1a-manifest.yaml this package sells. It is a
// golden value, not an independent fact: this package is a runtime registry
// and does not read planning definitions, so commercial_test.go's
// TestP1AManifestDigestIsTheLiveSignedManifestDigest recomputes the live
// manifest digest and fails the moment the manifest is re-signed without
// updating this constant (the drift that left it at 3df52c31... while the
// manifest had moved on).
const P1AManifestDigest = "e9d7ffe66f019dc8ea8b4252303cde9dfc6b22cf705a8de5c6a024d1fbcd34dc"

const (
	ReleaseP1A             = "P1A"
	PilotEntitlement       = "entitlement.pilot.p1a.v1"
	PilotWorkflow          = "promotion.preflight-simulate-observe/v1"
	PilotPriceCurrency     = "USD"
	PilotPriceMinimumCents = int64(1500000)
	PilotPriceMaximumCents = int64(4000000)
)

var (
	ErrInvalidPackage = errors.New("commercial: invalid pilot package")
	ErrAuthority      = errors.New("commercial: pilot cannot grant write authority")
)

// IntentEntitlement binds one sold capability to the signed release manifest.
type IntentEntitlement struct {
	ID          string   `json:"id"`
	Modes       []string `json:"modes,omitempty"`
	EffectClass string   `json:"effect_class"`
	Disposition string   `json:"disposition"`
}

// PricingHypothesis is intentionally fixed-price. Usage and provider cost are
// evidence for a later decision, not customer-visible rating in P1A.
type PricingHypothesis struct {
	Currency            string `json:"currency"`
	MinimumCents        int64  `json:"minimum_cents"`
	MaximumCents        int64  `json:"maximum_cents"`
	DurationDaysMin     int    `json:"duration_days_min"`
	DurationDaysMax     int    `json:"duration_days_max"`
	UsageRating         string `json:"usage_rating"`
	ImplementationCost  string `json:"implementation_cost"`
	SupportModel        string `json:"support_model"`
	ProviderPassThrough string `json:"provider_pass_through"`
}

// AuthorityBoundary says precisely what a customer may rely on the pilot to
// do. P1A observes and simulates; it does not become system of record.
type AuthorityBoundary struct {
	Topology          string   `json:"topology"`
	WriteAuthority    bool     `json:"write_authority"`
	OwnedDomains      []string `json:"owned_domains"`
	ObservedDomains   []string `json:"observed_domains"`
	ForbiddenEffects  []string `json:"forbidden_effects"`
	ExpansionRequires string   `json:"expansion_requires"`
}

// ExitTerms make export, retention and termination part of the offer rather
// than an untracked promise in contract prose.
type ExitTerms struct {
	TerminationNoticeDays int      `json:"termination_notice_days"`
	ExportFormats         []string `json:"export_formats"`
	ExportIncludes        []string `json:"export_includes"`
	RetentionDays         int      `json:"retention_days"`
	DeletionCertificate   bool     `json:"deletion_certificate"`
}

// PilotCommercialPackage is the signed, machine-readable P1A offer.
type PilotCommercialPackage struct {
	SchemaVersion       int                 `json:"schema_version"`
	Release             string              `json:"release"`
	ManifestDigest      string              `json:"manifest_digest"`
	EntitlementRef      string              `json:"entitlement_ref"`
	Workflow            string              `json:"workflow"`
	Entitlements        []IntentEntitlement `json:"entitlements"`
	Exclusions          []string            `json:"exclusions"`
	Pricing             PricingHypothesis   `json:"pricing"`
	Authority           AuthorityBoundary   `json:"authority"`
	EvidenceSLO         string              `json:"evidence_slo"`
	Exit                ExitTerms           `json:"exit"`
	RepriceThresholdPct int                 `json:"reprice_threshold_pct"`
	StopThresholdPct    int                 `json:"stop_threshold_pct"`
}

// DefaultPilotCommercialPackage returns the frozen P1A hypothesis.
func DefaultPilotCommercialPackage() PilotCommercialPackage {
	ids := []string{
		"hcmnext.people.explain_worker_state/v1",
		"hcmnext.people.promote_worker/v1",
		"hcmnext.rewards.simulate_compensation/v1",
		"hcmnext.rewards.evaluate_pay_band_position/v1",
		"hcmnext.intelligence.explain_transaction/v1",
		"hcmnext.operations.detect_drift/v1",
		"hcmnext.operations.create_repair_plan/v1",
		"hcmnext.operations.simulate_repair/v1",
	}
	e := make([]IntentEntitlement, len(ids))
	for i, id := range ids {
		e[i] = IntentEntitlement{ID: id, EffectClass: "READ_ONLY", Disposition: "INCLUDED"}
	}
	e[1].Modes = []string{"DRAFT", "PREFLIGHT", "SIMULATE"}
	e[6].Modes = []string{"RECOMMENDATION_ONLY"}
	return PilotCommercialPackage{
		SchemaVersion: 1, Release: ReleaseP1A, ManifestDigest: P1AManifestDigest,
		EntitlementRef: PilotEntitlement, Workflow: PilotWorkflow, Entitlements: e,
		Exclusions:          []string{"worker/employment/assignment/organization/position/compensation/budget mutations", "reservations, WorkItems, timers", "provider writes, external effects, MessageIntents", "granular customer usage rating, tax, payment processing"},
		Pricing:             PricingHypothesis{Currency: PilotPriceCurrency, MinimumCents: PilotPriceMinimumCents, MaximumCents: PilotPriceMaximumCents, DurationDaysMin: 60, DurationDaysMax: 90, UsageRating: "FIXED_PRICE_NO_CUSTOMER_USAGE_RATING", ImplementationCost: "included_and_tracked_as_pilot_evidence", SupportModel: "named_pilot_owner_and_operational_escalation", ProviderPassThrough: "disclosed_separately_only_with_customer_approval"},
		Authority:           AuthorityBoundary{Topology: "EXTERNAL_OBSERVATION", WriteAuthority: false, OwnedDomains: []string{}, ObservedDomains: []string{"worker", "employment", "position", "compensation", "budget", "projection", "ledger"}, ForbiddenEffects: []string{"domain_mutation", "reservation", "work_item", "timer", "message", "outbox", "provider_write"}, ExpansionRequires: "signed Gate A PROCEED followed by a separate P1B authority amendment"},
		EvidenceSLO:         "interactive p95 <= 2s; every result carries authority, provenance, freshness and zero-effect evidence",
		Exit:                ExitTerms{TerminationNoticeDays: 30, ExportFormats: []string{"JSON", "CSV"}, ExportIncludes: []string{"inputs", "results", "observations", "reconciliation", "evidence"}, RetentionDays: 30, DeletionCertificate: true},
		RepriceThresholdPct: 25, StopThresholdPct: 40,
	}
}

// Validate enforces the commercial ceiling and rejects contracts that could
// accidentally be interpreted as granting product authority.
func (p PilotCommercialPackage) Validate() error {
	if p.SchemaVersion != 1 || p.Release != ReleaseP1A || p.ManifestDigest != P1AManifestDigest || p.EntitlementRef != PilotEntitlement || p.Workflow != PilotWorkflow {
		return fmt.Errorf("%w: release identity", ErrInvalidPackage)
	}
	if len(p.Entitlements) != 8 {
		return fmt.Errorf("%w: exactly eight P1A entitlements required", ErrInvalidPackage)
	}
	seen := map[string]bool{}
	for _, e := range p.Entitlements {
		if e.ID == "" || seen[e.ID] || e.EffectClass != "READ_ONLY" || e.Disposition != "INCLUDED" {
			return fmt.Errorf("%w: entitlement %q", ErrInvalidPackage, e.ID)
		}
		seen[e.ID] = true
	}
	if p.Pricing.Currency != PilotPriceCurrency || p.Pricing.MinimumCents != PilotPriceMinimumCents || p.Pricing.MaximumCents != PilotPriceMaximumCents || p.Pricing.DurationDaysMin != 60 || p.Pricing.DurationDaysMax != 90 || p.Pricing.UsageRating != "FIXED_PRICE_NO_CUSTOMER_USAGE_RATING" {
		return fmt.Errorf("%w: pricing hypothesis", ErrInvalidPackage)
	}
	if p.Authority.Topology != "EXTERNAL_OBSERVATION" || p.Authority.WriteAuthority || len(p.Authority.OwnedDomains) != 0 || p.Authority.ExpansionRequires == "" {
		return fmt.Errorf("%w: %w", ErrInvalidPackage, ErrAuthority)
	}
	if len(p.Exclusions) < 4 || p.EvidenceSLO == "" || p.Exit.RetentionDays <= 0 || !p.Exit.DeletionCertificate || p.RepriceThresholdPct <= 0 || p.StopThresholdPct <= p.RepriceThresholdPct {
		return fmt.Errorf("%w: evidence and exit terms", ErrInvalidPackage)
	}
	return nil
}

// Canonical returns deterministic JSON suitable for signing or registry storage.
func (p PilotCommercialPackage) Canonical() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	q := p
	q.Exclusions = append([]string(nil), p.Exclusions...)
	sort.Strings(q.Exclusions)
	return json.Marshal(q)
}

// Parse validates machine-readable package bytes before returning them.
func Parse(data []byte) (PilotCommercialPackage, error) {
	var p PilotCommercialPackage
	if err := json.Unmarshal(data, &p); err != nil {
		return PilotCommercialPackage{}, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	if err := p.Validate(); err != nil {
		return PilotCommercialPackage{}, err
	}
	canonical, _ := p.Canonical()
	if !bytes.Equal(bytes.TrimSpace(data), canonical) {
		return PilotCommercialPackage{}, fmt.Errorf("%w: non-canonical encoding", ErrInvalidPackage)
	}
	return p, nil
}
