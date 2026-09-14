package pilotcommercial

import "testing"

// repoRoot is the path from this package's directory to the repository
// root: tools/planning/pilotcommercial is three levels below root, exactly
// like tools/planning/pilotprovider, tools/planning/pilotjurisdiction and
// tools/planning/scopeceiling.
const repoRoot = "../../.."

const (
	freezePath       = repoRoot + "/definitions/planning/gates/commercial-001-pilot-package.yaml"
	ceilingPath      = repoRoot + "/definitions/planning/gates/phase1-scope-ceiling.yaml"
	jurisdictionPath = repoRoot + "/definitions/planning/gates/select-001-jurisdiction-profile.yaml"
	providerPath     = repoRoot + "/definitions/planning/gates/select-002-provider-topology.yaml"
)

// mustLoadFreeze loads the real, checked-in COMMERCIAL-001 freeze.
func mustLoadFreeze(t *testing.T) PilotCommercialFreeze {
	t.Helper()
	f, err := LoadFreeze(freezePath)
	if err != nil {
		t.Fatalf("LoadFreeze: %v", err)
	}
	return *f
}

// validFixture returns a minimal, structurally complete
// PilotCommercialFreeze: every RED element is present and every GREEN
// invariant holds, so Validate() reports zero violations. It is not bound
// to the live registries (ConformsToLiveRegistries is a separate,
// registry-only check exercised by conformance_test.go and
// refactor_test.go against the real checked-in file); property_test.go and
// mutation_test.go exercise Validate itself against this fixture.
func validFixture() PilotCommercialFreeze {
	return PilotCommercialFreeze{
		SchemaVersion: 1,
		TodoID:        "COMMERCIAL-001",
		SignedDate:    "2026-09-13",
		Intent: IntentPromise{
			ScopeCeilingDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Entitlements: []Entitlement{
				{IntentID: "hcmnext.fixture.read_only/v1", EffectClass: "READ_ONLY", Disposition: "INCLUDED"},
				{IntentID: "hcmnext.fixture.other_read_only/v1", Modes: []string{"SIMULATE"}, EffectClass: "READ_ONLY", Disposition: "INCLUDED"},
			},
			Exclusions: []string{"fixture exclusion one", "fixture exclusion two", "fixture exclusion three", "fixture exclusion four"},
		},
		Jurisdiction: JurisdictionPromise{
			Status:       PromiseSelectedPendingReview,
			Country:      "US",
			State:        "CA",
			ReviewStatus: "UNREVIEWED",
			Caveat:       "fixture jurisdiction caveat",
		},
		Provider: ProviderPromise{
			Status: PromiseNone,
			Caveat: "fixture provider caveat",
		},
		Evidence: EvidenceAndSLO{
			SLOStatus:        SLOStatusNone,
			SLOStatement:     "fixture slo statement",
			EvidenceCaptured: []string{"authority", "provenance"},
		},
		Pricing: PricingHypothesis{
			Currency:            "USD",
			MinimumCents:        100,
			MaximumCents:        200,
			DurationDaysMin:     30,
			DurationDaysMax:     60,
			UsageRating:         "FIXED_PRICE_NO_CUSTOMER_USAGE_RATING",
			ImplementationCost:  "fixture implementation cost",
			SupportModel:        "fixture support model",
			ProviderPassThrough: "fixture provider pass-through",
			IsHypothesis:        true,
		},
		Authority: AuthorityBoundary{
			Topology:          TopologyExternalObservation,
			WriteAuthority:    false,
			OwnedDomains:      nil,
			ObservedDomains:   []string{"worker", "compensation"},
			ForbiddenEffects:  []string{"domain_mutation"},
			ExpansionRequires: "fixture expansion requirement",
		},
		ProviderPassThrough: ProviderPassThrough{
			Disclosed:                false,
			Policy:                   "fixture pass-through policy",
			RequiresCustomerApproval: true,
		},
		Exit: ExitTerms{
			TerminationNoticeDays: 30,
			ExportFormats:         []string{"JSON"},
			ExportIncludes:        []string{"inputs"},
			RetentionDays:         30,
			DeletionCertificate:   true,
		},
		DataProcessing: DataProcessingTerms{
			ResidencyRegion:        "fixture residency region",
			SubprocessorDisclosure: "fixture subprocessor disclosure",
			DPAReference:           "fixture: no DPA executed",
		},
		Billing: BillingPolicy{
			Model:            "FIXED_PRICE_NO_PER_TRANSACTION_METERING",
			ReplayBillable:   false,
			RepairBillable:   false,
			IdempotencyBasis: "fixture idempotency basis",
		},
		RepriceStop: RepriceStopCriteria{
			RepriceThresholdPct: 25,
			StopThresholdPct:    40,
		},
		Signature: &Signature{
			Algorithm: "ed25519", PublicKey: "fixture-public-key", Value: "fixture-signature-value",
			KeyFixture: "tools/planning/gateevidence/testdata/dev-signing-key.yaml",
		},
	}
}

// deepCopy returns an independent copy of f so a mutation in one subtest
// cannot leak into another (slices in Go share backing arrays on a plain
// struct copy).
func deepCopy(f PilotCommercialFreeze) PilotCommercialFreeze {
	c := f
	c.Intent.Entitlements = append([]Entitlement(nil), f.Intent.Entitlements...)
	for i := range c.Intent.Entitlements {
		c.Intent.Entitlements[i].Modes = append([]string(nil), f.Intent.Entitlements[i].Modes...)
	}
	c.Intent.Exclusions = append([]string(nil), f.Intent.Exclusions...)
	c.Evidence.EvidenceCaptured = append([]string(nil), f.Evidence.EvidenceCaptured...)
	c.Authority.OwnedDomains = append([]string(nil), f.Authority.OwnedDomains...)
	c.Authority.ObservedDomains = append([]string(nil), f.Authority.ObservedDomains...)
	c.Authority.ForbiddenEffects = append([]string(nil), f.Authority.ForbiddenEffects...)
	c.Exit.ExportFormats = append([]string(nil), f.Exit.ExportFormats...)
	c.Exit.ExportIncludes = append([]string(nil), f.Exit.ExportIncludes...)
	if f.Signature != nil {
		sig := *f.Signature
		c.Signature = &sig
	}
	return c
}
