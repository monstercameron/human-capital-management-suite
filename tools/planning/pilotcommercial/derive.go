package pilotcommercial

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
)

// RegistryMismatch names one place a PilotCommercialFreeze disagrees with a
// live registry or selection artifact it must derive from rather than
// restate independently. This is REFACTOR's enforcement mechanism ("contract
// prose cannot independently enable capability"), not merely its assertion:
// every field ConformsToLiveRegistries checks is read fresh from disk or
// from internal/commercial's exported constructor on every call, never from
// this package's own bookkeeping.
type RegistryMismatch struct {
	Field string
	Issue string
}

func (m RegistryMismatch) String() string { return fmt.Sprintf("%s: %s", m.Field, m.Issue) }

// ConformsToLiveRegistries cross-checks f against the four live sources of
// truth COMMERCIAL-001's GREEN and REFACTOR require it derive from:
// PHASE-001's scope ceiling, SELECT-001's jurisdiction profile, SELECT-002's
// provider topology, and internal/commercial's entitlement/pricing/
// authority/exit registry (DefaultPilotCommercialPackage). It performs real
// file reads and a real function call - this is what makes "the package is
// a view over the registries rather than an independent source of truth" a
// checked property instead of a comment.
func ConformsToLiveRegistries(f PilotCommercialFreeze, ceilingPath, jurisdictionPath, providerPath string) ([]RegistryMismatch, error) {
	var mismatches []RegistryMismatch
	add := func(field, issue string) {
		mismatches = append(mismatches, RegistryMismatch{Field: field, Issue: issue})
	}

	ceiling, err := scopeceiling.LoadManifest(ceilingPath)
	if err != nil {
		return nil, fmt.Errorf("load scope ceiling: %w", err)
	}
	jurisdictionProfile, err := pilotjurisdiction.LoadProfile(jurisdictionPath)
	if err != nil {
		return nil, fmt.Errorf("load jurisdiction profile: %w", err)
	}
	providerTopology, err := pilotprovider.LoadTopology(providerPath)
	if err != nil {
		return nil, fmt.Errorf("load provider topology: %w", err)
	}
	registry := commercial.DefaultPilotCommercialPackage()

	// --- RED: "promises an unselected ... intent": every promised
	// entitlement must be an INCLUDE-disposed intent in the live ceiling,
	// pinned to the ceiling's own current digest. ---
	ceilingDigest, err := ceiling.CanonicalDigest()
	if err != nil {
		return nil, fmt.Errorf("ceiling canonical digest: %w", err)
	}
	if f.Intent.ScopeCeilingDigest != ceilingDigest {
		add("intent.scope_ceiling_digest", fmt.Sprintf("freeze pins %q, live ceiling digest is %q", f.Intent.ScopeCeilingDigest, ceilingDigest))
	}
	ceilingIntents := make(map[string]scopeceiling.IntentItem, len(ceiling.Intents))
	for _, it := range ceiling.Intents {
		ceilingIntents[it.ID] = it
	}
	for i, e := range f.Intent.Entitlements {
		it, ok := ceilingIntents[e.IntentID]
		if !ok {
			add(fmt.Sprintf("intent.entitlements[%d].intent_id", i), fmt.Sprintf("%q is not present in the live Phase 1 scope ceiling - an unselected intent", e.IntentID))
			continue
		}
		if it.Disposition != scopeceiling.Include {
			add(fmt.Sprintf("intent.entitlements[%d].intent_id", i), fmt.Sprintf("%q has ceiling disposition %s, not INCLUDE", e.IntentID, it.Disposition))
		}
	}

	// --- REFACTOR: "commercial views consume entitlement, usage, cost and
	// release registries; contract prose cannot independently enable
	// capability." Entitlements, exclusions, pricing, authority and exit
	// terms must equal internal/commercial's live registry exactly. ---
	if mismatch := diffEntitlements(f.Intent.Entitlements, registry.Entitlements); mismatch != "" {
		add("intent.entitlements", mismatch)
	}
	if !equalStringSets(f.Intent.Exclusions, registry.Exclusions) {
		add("intent.exclusions", fmt.Sprintf("freeze exclusions %v do not match the live commercial registry's %v", f.Intent.Exclusions, registry.Exclusions))
	}
	if mismatch := diffPricing(f.Pricing, registry.Pricing); mismatch != "" {
		add("pricing", mismatch)
	}
	if mismatch := diffAuthority(f.Authority, registry.Authority); mismatch != "" {
		add("authority", mismatch)
	}
	if mismatch := diffExit(f.Exit, registry.Exit); mismatch != "" {
		add("exit", mismatch)
	}
	if f.RepriceStop.RepriceThresholdPct != registry.RepriceThresholdPct {
		add("reprice_stop.reprice_threshold_pct", fmt.Sprintf("freeze has %d, live commercial registry has %d", f.RepriceStop.RepriceThresholdPct, registry.RepriceThresholdPct))
	}
	if f.RepriceStop.StopThresholdPct != registry.StopThresholdPct {
		add("reprice_stop.stop_threshold_pct", fmt.Sprintf("freeze has %d, live commercial registry has %d", f.RepriceStop.StopThresholdPct, registry.StopThresholdPct))
	}

	// --- PHASE-001's slo selection slot must remain unfilled and must name
	// COMMERCIAL-001 among its fillers; this freeze must promise no SLO
	// while that holds. ---
	sloFound := false
	for _, s := range ceiling.SelectionSlots {
		if s.Name != "slo" {
			continue
		}
		sloFound = true
		if s.Filled || s.Value != "" {
			add("evidence.slo_status", "the live ceiling's slo slot is now filled - this freeze's NONE claim needs re-review, not a silent pass")
		}
		if !strings.Contains(s.FillingTodoID, "COMMERCIAL-001") {
			add("evidence.slo_status", fmt.Sprintf("ceiling's slo slot filling_todo_id %q does not name COMMERCIAL-001", s.FillingTodoID))
		}
	}
	if !sloFound {
		add("evidence.slo_status", "live ceiling carries no slo selection slot")
	}
	if f.Evidence.SLOStatus != SLOStatusNone {
		add("evidence.slo_status", fmt.Sprintf("must be %s while the ceiling's slo slot is unfilled, got %q", SLOStatusNone, f.Evidence.SLOStatus))
	}

	// --- RED: "promises an unselected ... jurisdiction": bound to
	// SELECT-001's live profile, including its review status. ---
	if f.Jurisdiction.Country != jurisdictionProfile.Jurisdiction.Country || f.Jurisdiction.State != jurisdictionProfile.Jurisdiction.State {
		add("jurisdiction", fmt.Sprintf("freeze names %s/%s, live SELECT-001 profile names %s/%s", f.Jurisdiction.Country, f.Jurisdiction.State, jurisdictionProfile.Jurisdiction.Country, jurisdictionProfile.Jurisdiction.State))
	}
	if f.Jurisdiction.ReviewStatus != jurisdictionProfile.ReviewStatus {
		add("jurisdiction.review_status", fmt.Sprintf("freeze claims %q, live SELECT-001 profile is %q", f.Jurisdiction.ReviewStatus, jurisdictionProfile.ReviewStatus))
	}
	reviewStatus, rsErr := legal.ParseReviewStatus(jurisdictionProfile.ReviewStatus)
	jurisdictionReleasable := rsErr == nil && reviewStatus.Releasable()
	if jurisdictionReleasable {
		if f.Jurisdiction.Status != PromiseSelectedConfirmed {
			add("jurisdiction.status", "live SELECT-001 profile has cleared review, but freeze still claims SELECTED_PENDING_REVIEW rather than SELECTED_CONFIRMED")
		}
	} else {
		if f.Jurisdiction.Status != PromiseSelectedPendingReview {
			add("jurisdiction.status", fmt.Sprintf("live SELECT-001 profile's review_status %q has not cleared review, so this freeze may not claim %q", jurisdictionProfile.ReviewStatus, f.Jurisdiction.Status))
		}
	}

	// --- RED: "promises an unselected ... provider": bound to SELECT-002's
	// live topology, which must remain a structurally-marked placeholder
	// incapable of satisfying a real provider-selection gate. ---
	satisfiesRealGate, _ := providerTopology.SatisfiesRealProviderSelectionGate()
	if satisfiesRealGate {
		if f.Provider.Status != PromiseSelectedConfirmed {
			add("provider.status", "live SELECT-002 topology now satisfies a real provider-selection gate, but freeze still claims NONE")
		}
	} else {
		if f.Provider.Status != PromiseNone {
			add("provider.status", fmt.Sprintf("live SELECT-002 topology does not satisfy a real provider-selection gate (selection_status=%s), so this freeze may not claim %q", providerTopology.SelectionStatus, f.Provider.Status))
		}
	}

	return mismatches, nil
}

// diffEntitlements reports a human-readable difference between the freeze's
// entitlements and internal/commercial's live registry entitlements, or ""
// if they carry the same (intent_id, modes, effect_class, disposition)
// tuples regardless of order.
func diffEntitlements(freeze []Entitlement, live []commercial.IntentEntitlement) string {
	if len(freeze) != len(live) {
		return fmt.Sprintf("freeze names %d entitlements, live commercial registry names %d", len(freeze), len(live))
	}
	freezeByID := make(map[string]Entitlement, len(freeze))
	for _, e := range freeze {
		freezeByID[e.IntentID] = e
	}
	for _, want := range live {
		got, ok := freezeByID[want.ID]
		if !ok {
			return fmt.Sprintf("live commercial registry entitles %q, which the freeze does not name", want.ID)
		}
		if got.EffectClass != want.EffectClass {
			return fmt.Sprintf("entitlement %q: freeze effect_class %q, live registry %q", want.ID, got.EffectClass, want.EffectClass)
		}
		if got.Disposition != want.Disposition {
			return fmt.Sprintf("entitlement %q: freeze disposition %q, live registry %q", want.ID, got.Disposition, want.Disposition)
		}
		if !equalStringSlicesInOrder(got.Modes, want.Modes) {
			return fmt.Sprintf("entitlement %q: freeze modes %v, live registry %v", want.ID, got.Modes, want.Modes)
		}
	}
	return ""
}

func diffPricing(freeze PricingHypothesis, live commercial.PricingHypothesis) string {
	switch {
	case freeze.Currency != live.Currency:
		return fmt.Sprintf("currency: freeze %q, live %q", freeze.Currency, live.Currency)
	case freeze.MinimumCents != live.MinimumCents:
		return fmt.Sprintf("minimum_cents: freeze %d, live %d", freeze.MinimumCents, live.MinimumCents)
	case freeze.MaximumCents != live.MaximumCents:
		return fmt.Sprintf("maximum_cents: freeze %d, live %d", freeze.MaximumCents, live.MaximumCents)
	case freeze.DurationDaysMin != live.DurationDaysMin:
		return fmt.Sprintf("duration_days_min: freeze %d, live %d", freeze.DurationDaysMin, live.DurationDaysMin)
	case freeze.DurationDaysMax != live.DurationDaysMax:
		return fmt.Sprintf("duration_days_max: freeze %d, live %d", freeze.DurationDaysMax, live.DurationDaysMax)
	case freeze.UsageRating != live.UsageRating:
		return fmt.Sprintf("usage_rating: freeze %q, live %q", freeze.UsageRating, live.UsageRating)
	case freeze.ImplementationCost != live.ImplementationCost:
		return fmt.Sprintf("implementation_cost: freeze %q, live %q", freeze.ImplementationCost, live.ImplementationCost)
	case freeze.SupportModel != live.SupportModel:
		return fmt.Sprintf("support_model: freeze %q, live %q", freeze.SupportModel, live.SupportModel)
	case freeze.ProviderPassThrough != live.ProviderPassThrough:
		return fmt.Sprintf("provider_pass_through: freeze %q, live %q", freeze.ProviderPassThrough, live.ProviderPassThrough)
	}
	return ""
}

func diffAuthority(freeze AuthorityBoundary, live commercial.AuthorityBoundary) string {
	switch {
	case freeze.Topology != live.Topology:
		return fmt.Sprintf("topology: freeze %q, live %q", freeze.Topology, live.Topology)
	case freeze.WriteAuthority != live.WriteAuthority:
		return fmt.Sprintf("write_authority: freeze %v, live %v", freeze.WriteAuthority, live.WriteAuthority)
	case !equalStringSets(freeze.OwnedDomains, live.OwnedDomains):
		return fmt.Sprintf("owned_domains: freeze %v, live %v", freeze.OwnedDomains, live.OwnedDomains)
	case !equalStringSets(freeze.ObservedDomains, live.ObservedDomains):
		return fmt.Sprintf("observed_domains: freeze %v, live %v", freeze.ObservedDomains, live.ObservedDomains)
	case !equalStringSets(freeze.ForbiddenEffects, live.ForbiddenEffects):
		return fmt.Sprintf("forbidden_effects: freeze %v, live %v", freeze.ForbiddenEffects, live.ForbiddenEffects)
	case freeze.ExpansionRequires != live.ExpansionRequires:
		return fmt.Sprintf("expansion_requires: freeze %q, live %q", freeze.ExpansionRequires, live.ExpansionRequires)
	}
	return ""
}

func diffExit(freeze ExitTerms, live commercial.ExitTerms) string {
	switch {
	case freeze.TerminationNoticeDays != live.TerminationNoticeDays:
		return fmt.Sprintf("termination_notice_days: freeze %d, live %d", freeze.TerminationNoticeDays, live.TerminationNoticeDays)
	case !equalStringSlicesInOrder(freeze.ExportFormats, live.ExportFormats):
		return fmt.Sprintf("export_formats: freeze %v, live %v", freeze.ExportFormats, live.ExportFormats)
	case !equalStringSlicesInOrder(freeze.ExportIncludes, live.ExportIncludes):
		return fmt.Sprintf("export_includes: freeze %v, live %v", freeze.ExportIncludes, live.ExportIncludes)
	case freeze.RetentionDays != live.RetentionDays:
		return fmt.Sprintf("retention_days: freeze %d, live %d", freeze.RetentionDays, live.RetentionDays)
	case freeze.DeletionCertificate != live.DeletionCertificate:
		return fmt.Sprintf("deletion_certificate: freeze %v, live %v", freeze.DeletionCertificate, live.DeletionCertificate)
	}
	return ""
}

func equalStringSlicesInOrder(a, b []string) bool {
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

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := append([]string(nil), a...)
	sb := append([]string(nil), b...)
	sort.Strings(sa)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}
