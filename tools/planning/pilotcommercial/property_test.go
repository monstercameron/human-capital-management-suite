package pilotcommercial

import (
	"strings"
	"testing"
)

// TestTodo_COMMERCIAL_001_Property is COMMERCIAL-001's PROPERTY test. It
// proves the GREEN invariant - every RED element Validate checks for
// actually causes a violation naming the exact broken field when removed,
// and a structurally complete fixture with none of those defects validates
// clean - holds across the whole schema, not just the one checked-in file.
// This mirrors tools/planning/pilotprovider's TestTodo_SELECT_002_Property.
func TestTodo_COMMERCIAL_001_Property(t *testing.T) {
	base := validFixture()
	if v := base.Validate(); len(v) != 0 {
		t.Fatalf("validFixture() must validate clean, got: %v", v)
	}

	cases := []struct {
		name    string
		mutate  func(*PilotCommercialFreeze)
		wantHit string
	}{
		{"wrong todo id", func(f *PilotCommercialFreeze) { f.TodoID = "SELECT-002" }, "todo_id"},
		{"missing signed date", func(f *PilotCommercialFreeze) { f.SignedDate = "" }, "signed_date"},

		{"missing scope ceiling digest", func(f *PilotCommercialFreeze) { f.Intent.ScopeCeilingDigest = "" }, "intent.scope_ceiling_digest"},
		{"scope ceiling digest not 64 hex chars", func(f *PilotCommercialFreeze) { f.Intent.ScopeCeilingDigest = "not-hex" }, "intent.scope_ceiling_digest"},
		{"no entitlements", func(f *PilotCommercialFreeze) { f.Intent.Entitlements = nil }, "intent.entitlements"},
		{"entitlement missing intent id", func(f *PilotCommercialFreeze) { f.Intent.Entitlements[0].IntentID = "" }, "intent_id"},
		{"duplicate entitlement", func(f *PilotCommercialFreeze) { f.Intent.Entitlements[1].IntentID = f.Intent.Entitlements[0].IntentID }, "duplicate entitlement"},
		{"entitlement missing effect class", func(f *PilotCommercialFreeze) { f.Intent.Entitlements[0].EffectClass = "" }, "effect_class"},
		{"entitlement missing disposition", func(f *PilotCommercialFreeze) { f.Intent.Entitlements[0].Disposition = "" }, "disposition"},
		{"entitlement sells a write beyond authority", func(f *PilotCommercialFreeze) {
			f.Authority.WriteAuthority = false
			f.Intent.Entitlements[0].EffectClass = "EXTERNAL_PROVIDER_WRITE"
		}, "authority.write_authority is false"},
		{"fewer than four exclusions", func(f *PilotCommercialFreeze) { f.Intent.Exclusions = f.Intent.Exclusions[:1] }, "intent.exclusions"},

		{"unknown jurisdiction status", func(f *PilotCommercialFreeze) { f.Jurisdiction.Status = "MADE_UP" }, "jurisdiction.status"},
		{"jurisdiction NONE but country populated", func(f *PilotCommercialFreeze) {
			f.Jurisdiction.Status = PromiseNone
			f.Jurisdiction.ReviewStatus = ""
		}, "jurisdiction"},
		{"jurisdiction selected but country missing", func(f *PilotCommercialFreeze) { f.Jurisdiction.Country = "" }, "jurisdiction"},
		{"jurisdiction selected but review status missing", func(f *PilotCommercialFreeze) { f.Jurisdiction.ReviewStatus = "" }, "jurisdiction.review_status"},
		{"jurisdiction missing caveat", func(f *PilotCommercialFreeze) { f.Jurisdiction.Caveat = "" }, "jurisdiction.caveat"},

		{"unknown provider status", func(f *PilotCommercialFreeze) { f.Provider.Status = "MADE_UP" }, "provider.status"},
		{"provider NONE but vendor ref populated", func(f *PilotCommercialFreeze) { f.Provider.VendorRef = "acme" }, "provider.vendor_ref"},
		{"provider selected but vendor ref missing", func(f *PilotCommercialFreeze) { f.Provider.Status = PromiseSelectedPendingReview }, "provider.vendor_ref"},
		{"provider missing caveat", func(f *PilotCommercialFreeze) { f.Provider.Caveat = "" }, "provider.caveat"},

		{"slo status not NONE", func(f *PilotCommercialFreeze) { f.Evidence.SLOStatus = "PROMISED" }, "evidence.slo_status"},
		{"slo statement missing", func(f *PilotCommercialFreeze) { f.Evidence.SLOStatement = "" }, "evidence.slo_statement"},
		{"no evidence captured", func(f *PilotCommercialFreeze) { f.Evidence.EvidenceCaptured = nil }, "evidence.evidence_captured"},

		{"pricing missing currency", func(f *PilotCommercialFreeze) { f.Pricing.Currency = "" }, "pricing.currency"},
		{"pricing inverted range", func(f *PilotCommercialFreeze) { f.Pricing.MaximumCents = 1 }, "pricing.minimum_cents"},
		{"pricing inverted duration", func(f *PilotCommercialFreeze) { f.Pricing.DurationDaysMax = 0 }, "pricing.duration_days_min"},
		{"pricing missing usage rating", func(f *PilotCommercialFreeze) { f.Pricing.UsageRating = "" }, "pricing.usage_rating"},
		{"pricing missing implementation cost", func(f *PilotCommercialFreeze) { f.Pricing.ImplementationCost = "" }, "pricing.implementation_cost"},
		{"pricing missing support model", func(f *PilotCommercialFreeze) { f.Pricing.SupportModel = "" }, "pricing.support_model"},
		{"pricing missing provider pass-through", func(f *PilotCommercialFreeze) { f.Pricing.ProviderPassThrough = "" }, "pricing.provider_pass_through"},
		{"pricing hides hypothesis", func(f *PilotCommercialFreeze) { f.Pricing.IsHypothesis = false }, "pricing.is_hypothesis"},

		{"unknown authority topology", func(f *PilotCommercialFreeze) { f.Authority.Topology = "MADE_UP" }, "authority.topology"},
		{"domain both owned and observed", func(f *PilotCommercialFreeze) {
			f.Authority.WriteAuthority = true
			f.Authority.OwnedDomains = []string{"worker"}
			f.Authority.ObservedDomains = []string{"worker"}
		}, "conflates overlay authority"},
		{"owned domain without write authority", func(f *PilotCommercialFreeze) {
			f.Authority.WriteAuthority = false
			f.Authority.OwnedDomains = []string{"worker"}
			f.Authority.ObservedDomains = nil
		}, "cannot claim system-of-record ownership"},
		{"external observation topology claims write authority", func(f *PilotCommercialFreeze) { f.Authority.WriteAuthority = true }, "authority.write_authority"},
		{"system of record topology has no owned domains", func(f *PilotCommercialFreeze) {
			f.Authority.Topology = TopologySystemOfRecord
			f.Authority.WriteAuthority = true
		}, "owned_domains is empty"},
		{"authority names no domain at all", func(f *PilotCommercialFreeze) { f.Authority.ObservedDomains = nil }, "neither owned_domains nor observed_domains"},
		{"authority missing expansion requirement", func(f *PilotCommercialFreeze) { f.Authority.ExpansionRequires = "" }, "authority.expansion_requires"},

		{"provider pass-through missing policy", func(f *PilotCommercialFreeze) { f.ProviderPassThrough.Policy = "" }, "provider_pass_through.policy"},
		{"disclosed pass-through without customer approval", func(f *PilotCommercialFreeze) {
			f.ProviderPassThrough.Disclosed = true
			f.ProviderPassThrough.RequiresCustomerApproval = false
		}, "require customer approval"},

		{"exit missing termination notice", func(f *PilotCommercialFreeze) { f.Exit.TerminationNoticeDays = 0 }, "exit.termination_notice_days"},
		{"exit missing export formats", func(f *PilotCommercialFreeze) { f.Exit.ExportFormats = nil }, "exit.export_formats"},
		{"exit missing export includes", func(f *PilotCommercialFreeze) { f.Exit.ExportIncludes = nil }, "exit.export_includes"},
		{"exit missing retention", func(f *PilotCommercialFreeze) { f.Exit.RetentionDays = 0 }, "exit.retention_days"},
		{"exit missing deletion certificate", func(f *PilotCommercialFreeze) { f.Exit.DeletionCertificate = false }, "exit.deletion_certificate"},
		{"data processing missing residency", func(f *PilotCommercialFreeze) { f.DataProcessing.ResidencyRegion = "" }, "data_processing.residency_region"},
		{"data processing missing subprocessor disclosure", func(f *PilotCommercialFreeze) { f.DataProcessing.SubprocessorDisclosure = "" }, "data_processing.subprocessor_disclosure"},
		{"data processing missing dpa reference", func(f *PilotCommercialFreeze) { f.DataProcessing.DPAReference = "" }, "data_processing.dpa_reference"},

		{"billing missing model", func(f *PilotCommercialFreeze) { f.Billing.Model = "" }, "billing.model"},
		{"replay billable", func(f *PilotCommercialFreeze) { f.Billing.ReplayBillable = true }, "billing.replay_billable"},
		{"repair billable", func(f *PilotCommercialFreeze) { f.Billing.RepairBillable = true }, "billing.repair_billable"},
		{"billing missing idempotency basis", func(f *PilotCommercialFreeze) { f.Billing.IdempotencyBasis = "" }, "billing.idempotency_basis"},

		{"reprice threshold non-positive", func(f *PilotCommercialFreeze) { f.RepriceStop.RepriceThresholdPct = 0 }, "reprice_stop.reprice_threshold_pct"},
		{"stop threshold not above reprice threshold", func(f *PilotCommercialFreeze) { f.RepriceStop.StopThresholdPct = f.RepriceStop.RepriceThresholdPct }, "reprice_stop.stop_threshold_pct"},

		{"no signature", func(f *PilotCommercialFreeze) { f.Signature = nil }, "signature"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := deepCopy(base)
			tc.mutate(&f)
			violations := f.Validate()
			if len(violations) == 0 {
				t.Fatalf("expected a violation for %q, got none", tc.name)
			}
			found := false
			for _, v := range violations {
				if strings.Contains(v.String(), tc.wantHit) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a violation containing %q, got %v", tc.wantHit, violations)
			}
		})
	}
}
