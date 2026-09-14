package pilotprovider

import "testing"

// repoRoot is the path from this package's directory to the repository
// root: tools/planning/pilotprovider is three levels below root, exactly
// like tools/planning/pilotjurisdiction and tools/planning/scopeceiling.
const repoRoot = "../../.."

const topologyPath = repoRoot + "/definitions/planning/gates/select-002-provider-topology.yaml"

// fixtureVendorID is the placeholder vendor identifier validFixture() uses.
// It intentionally matches the checked-in topology's real vendor_id so
// refactor_test.go's live scan and the unit fixtures agree on what a
// "leaked" identifier looks like.
const fixtureVendorID = "legacyhcm-incumbent-PLACEHOLDER-UNVERIFIED"

// mustLoadTopology loads the real, checked-in SELECT-002 topology. It does
// not assert Validate() is clean: by design the checked-in topology is a
// PLACEHOLDER_UNVERIFIED selection, and topology_test.go's PRIMARY test
// asserts its Validate() result explicitly (structurally clean as a
// placeholder, but never able to satisfy SatisfiesRealProviderSelectionGate).
func mustLoadTopology(t *testing.T) ProviderTopology {
	t.Helper()
	tp, err := LoadTopology(topologyPath)
	if err != nil {
		t.Fatalf("LoadTopology: %v", err)
	}
	return *tp
}

// validFixture returns a minimal, structurally complete PLACEHOLDER_UNVERIFIED
// ProviderTopology: every RED element is present, every operation verb and
// fault class is covered exactly once, and Validate() reports zero
// violations. property_test.go and mutation_test.go exercise Validate
// itself against this fixture rather than the one real checked-in file.
func validFixture() ProviderTopology {
	return ProviderTopology{
		SchemaVersion:   1,
		TodoID:          "SELECT-002",
		SignedDate:      "2026-09-13",
		SelectionStatus: StatusPlaceholderUnverified,
		Provider: Provider{
			VendorID:          fixtureVendorID,
			VendorDisplayName: "fixture vendor display name",
			Product:           "fixture-product",
			Edition:           "fixture-edition",
			Region:            "fixture-region",
			APIVersion:        "v1-fixture",
		},
		VendorConfirmation: VendorConfirmation{},
		APIEntitlements: []APIEntitlement{
			{Scope: "worker.read", Description: "fixture read entitlement", Granted: true},
			{Scope: "worker.compensation.change", Description: "fixture write entitlement", Granted: true},
		},
		Sandbox: SandboxEvidence{
			Fidelity:    SandboxFidelitySyntheticFixtureOnly,
			Description: "fixture sandbox description",
			EvidenceRef: "fixture_sandbox_evidence_ref",
			ExecutedAt:  "2026-09-13T00:00:00Z",
		},
		FieldAuthorities: []FieldAuthority{
			{Field: "compensation.base", Authority: AuthorityHCMNext, Rationale: "fixture rationale"},
			{Field: "person.name.given", Authority: AuthorityProvider, Rationale: "fixture rationale"},
		},
		Observation: ObservationPath{
			Mechanism:              ObservationBoth,
			PollIntervalSeconds:    300,
			WebhookSignatureScheme: "HMAC_SHA256",
			IndependentOfWriteAck:  true,
		},
		Quota: Quota{
			LimitWindow:      "1m",
			RequestLimit:     100,
			ConcurrencyLimit: 5,
			BurstPolicy:      "fixture burst policy",
		},
		Timeout: TimeoutPolicy{
			RequestTimeoutSeconds:  30,
			AmbiguousOutcomeAction: AmbiguousOutcomeObserveBeforeRetry,
		},
		Credential: CredentialModel{
			Scheme:       CredentialOAuth2ClientCredentials,
			RotationDays: 90,
			ScopeGranted: []string{"worker.read", "worker.compensation.change"},
		},
		DataProcessing: DataProcessingTerms{
			ResidencyRegion:        "US",
			RetentionDays:          365,
			SubprocessorDisclosure: "fixture subprocessor disclosure",
			DPAReference:           "fixture: no DPA executed",
		},
		Operations: []OperationMapping{
			{Verb: VerbRead, CapabilityOrIntentID: "worker.read", ContractID: "fixture-contract", ContractVersion: "1.0.0", Owner: "fixture-owner"},
			{Verb: VerbWrite, CapabilityOrIntentID: "worker.compensation.change", ContractID: "fixture-contract", ContractVersion: "1.0.0", Owner: "fixture-owner"},
			{Verb: VerbSubscribe, CapabilityOrIntentID: "worker.change.events", ContractID: "fixture-contract", ContractVersion: "1.0.0", Owner: "fixture-owner"},
			{Verb: VerbObserve, CapabilityOrIntentID: "worker.compensation.observe", ContractID: "fixture-contract", ContractVersion: "1.0.0", Owner: "fixture-owner"},
			{Verb: VerbReconcile, CapabilityOrIntentID: "worker.compensation.reconcile", ContractID: "fixture-contract", ContractVersion: "1.0.0", Owner: "fixture-owner"},
		},
		Faults: fixtureFaults(),
		CrossSystemObservation: CrossSystemObservation{
			Description:                             "fixture cross-system observation",
			Method:                                  ObservationPolling,
			ProvesIndependentOfWriteAcknowledgement: true,
			EvidenceRef:                             "fixture_cross_system_evidence_ref",
		},
		Exit: ExitPlan{
			StopNewWork:                 "fixture stop new work",
			PreserveOrReconcileInFlight: "fixture preserve/reconcile",
			RevokeCredentials:           "fixture revoke credentials",
			ActivateFallback:            "fixture activate fallback",
			DrainRollbackReopen:         "fixture drain/rollback/reopen",
			ExceptionExpiry:             "fixture exception expiry",
		},
		Commercial: CommercialLimit{
			Metric:        "fixture metric",
			Limit:         "fixture limit",
			OveragePolicy: "fixture overage policy",
		},
		StopReselectThresholds: []StopReselectThreshold{
			{Metric: "fixture_metric", Threshold: "fixture_threshold", Action: ActionProceed},
		},
		Signature: &Signature{
			Algorithm: "ed25519", PublicKey: "fixture-public-key", Value: "fixture-signature-value",
			KeyFixture: "tools/planning/gateevidence/testdata/dev-signing-key.yaml",
		},
	}
}

// fixtureFaults returns exactly one FaultCase per AllFaultClasses() entry,
// covering the closed fault-class vocabulary exactly once, the way a valid
// topology's fault matrix must.
func fixtureFaults() []FaultCase {
	var faults []FaultCase
	for _, class := range AllFaultClasses() {
		faults = append(faults, FaultCase{
			Class:              class,
			ExampleResponse:    "fixture example response for " + class,
			DefaultAction:      "fixture default action for " + class,
			SimulatedInSandbox: true,
			EvidenceRef:        "fixture_fault_evidence_ref",
		})
	}
	return faults
}

// deepCopy returns an independent copy of t so a mutation in one subtest
// cannot leak into another (slices in Go share backing arrays on a plain
// struct copy).
func deepCopy(t ProviderTopology) ProviderTopology {
	c := t
	c.APIEntitlements = append([]APIEntitlement(nil), t.APIEntitlements...)
	c.FieldAuthorities = append([]FieldAuthority(nil), t.FieldAuthorities...)
	c.Credential.ScopeGranted = append([]string(nil), t.Credential.ScopeGranted...)
	c.Operations = append([]OperationMapping(nil), t.Operations...)
	c.Faults = append([]FaultCase(nil), t.Faults...)
	c.StopReselectThresholds = append([]StopReselectThreshold(nil), t.StopReselectThresholds...)
	if t.Signature != nil {
		sig := *t.Signature
		c.Signature = &sig
	}
	return c
}
