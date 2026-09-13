package pilotprovider

import "testing"

// TestPilotProviderSelectionProvesEditionAuthorityOperationsAndExitViability
// is SELECT-002's PRIMARY test. It loads the real, checked-in
// definitions/planning/gates/select-002-provider-topology.yaml and proves
// every RED element is structurally present (product/edition/region/API
// entitlement, sandbox fidelity, authority-by-field, webhook/polling
// behavior, quotas, timeout ambiguity, credential model, data-processing
// terms, fallback/exit, commercial limit), that GREEN's exact-coverage
// invariants hold (every operation verb and fault class exactly once, one
// independently observed cross-system outcome, defined stop/reselect
// conditions), and that the topology is honestly marked as a placeholder
// that cannot authorize a real provider selection.
func TestPilotProviderSelectionProvesEditionAuthorityOperationsAndExitViability(t *testing.T) {
	topology := mustLoadTopology(t)

	violations := topology.Validate()
	if len(violations) != 0 {
		t.Fatalf("the checked-in topology must validate as a structurally complete placeholder, got: %v", violations)
	}

	// --- RED: product/edition/region/API entitlement ("Edition") ---
	if topology.Provider.Product == "" || topology.Provider.Edition == "" || topology.Provider.Region == "" || topology.Provider.APIVersion == "" {
		t.Fatalf("topology is missing product/edition/region/api_version: %+v", topology.Provider)
	}
	if len(topology.APIEntitlements) == 0 {
		t.Fatal("topology names no API entitlements")
	}

	// --- RED: authority-by-field ("Authority") ---
	if len(topology.FieldAuthorities) == 0 {
		t.Fatal("topology declares authority for no field")
	}
	for _, f := range topology.FieldAuthorities {
		if !validFieldAuthorities[f.Authority] {
			t.Errorf("field_authorities names unknown authority %q for field %q", f.Authority, f.Field)
		}
	}

	// --- RED/GREEN: webhook/polling, quotas, timeout ambiguity, credential
	// model, data-processing terms, sandbox fidelity, exact operation and
	// fault coverage, one independently observed cross-system outcome
	// ("Operations") ---
	if !validObservationMechanisms[topology.Observation.Mechanism] {
		t.Errorf("observation.mechanism %q is not a declared mechanism", topology.Observation.Mechanism)
	}
	if topology.Quota.RequestLimit <= 0 || topology.Quota.ConcurrencyLimit <= 0 {
		t.Fatalf("topology is missing quotas: %+v", topology.Quota)
	}
	if topology.Timeout.AmbiguousOutcomeAction != AmbiguousOutcomeObserveBeforeRetry {
		t.Errorf("timeout.ambiguous_outcome_action = %q, want %s", topology.Timeout.AmbiguousOutcomeAction, AmbiguousOutcomeObserveBeforeRetry)
	}
	if !validCredentialSchemes[topology.Credential.Scheme] {
		t.Errorf("credential.scheme %q is not a declared scheme", topology.Credential.Scheme)
	}
	if topology.DataProcessing.ResidencyRegion == "" || topology.DataProcessing.DPAReference == "" {
		t.Fatalf("topology is missing data-processing terms: %+v", topology.DataProcessing)
	}
	if !validSandboxFidelities[topology.Sandbox.Fidelity] {
		t.Errorf("sandbox.fidelity %q is not a declared fidelity level", topology.Sandbox.Fidelity)
	}

	seenVerb := map[string]bool{}
	for _, o := range topology.Operations {
		if o.ContractID == "" || o.ContractVersion == "" || o.Owner == "" {
			t.Errorf("operation %+v is missing an exact contract, version or owner", o)
		}
		seenVerb[o.Verb] = true
	}
	for _, verb := range allOperationVerbs {
		if !seenVerb[verb] {
			t.Errorf("topology's operations do not cover verb %s", verb)
		}
	}

	seenFault := map[string]bool{}
	for _, f := range topology.Faults {
		seenFault[f.Class] = true
	}
	for _, class := range AllFaultClasses() {
		if !seenFault[class] {
			t.Errorf("topology's fault matrix does not cover class %s", class)
		}
	}

	if !topology.CrossSystemObservation.ProvesIndependentOfWriteAcknowledgement {
		t.Error("topology's cross_system_observation does not prove independence from the write acknowledgement")
	}
	if topology.CrossSystemObservation.Description == "" || topology.CrossSystemObservation.EvidenceRef == "" {
		t.Errorf("topology's cross_system_observation is incomplete: %+v", topology.CrossSystemObservation)
	}

	// --- RED/GREEN: fallback/exit, commercial limit, stop/reselect
	// conditions ("ExitViability") ---
	exit := topology.Exit
	if exit.StopNewWork == "" || exit.PreserveOrReconcileInFlight == "" || exit.RevokeCredentials == "" ||
		exit.ActivateFallback == "" || exit.DrainRollbackReopen == "" || exit.ExceptionExpiry == "" {
		t.Fatalf("topology's exit plan is incomplete: %+v", exit)
	}
	if topology.Commercial.Metric == "" || topology.Commercial.Limit == "" || topology.Commercial.OveragePolicy == "" {
		t.Fatalf("topology is missing a commercial limit: %+v", topology.Commercial)
	}
	if len(topology.StopReselectThresholds) == 0 {
		t.Fatal("topology names no stop/reselect thresholds")
	}
	for _, th := range topology.StopReselectThresholds {
		if !validActions[th.Action] {
			t.Errorf("stop_reselect_thresholds names unknown action %q", th.Action)
		}
	}

	// --- The placeholder is honestly marked and cannot authorize a real
	// selection: this is what keeps ExitViability honest - a pilot cannot
	// be said to have a viable exit from a vendor nobody has actually
	// signed with. ---
	if topology.SelectionStatus != StatusPlaceholderUnverified {
		t.Fatalf("checked-in topology's selection_status = %q, want %s", topology.SelectionStatus, StatusPlaceholderUnverified)
	}
	satisfiesRealGate, gateViolations := topology.SatisfiesRealProviderSelectionGate()
	if satisfiesRealGate {
		t.Fatal("the checked-in placeholder topology must not satisfy a real provider selection gate")
	}
	if len(gateViolations) == 0 {
		t.Fatal("SatisfiesRealProviderSelectionGate reported false but named no reasons")
	}

	// Signature verifies.
	ok, err := VerifyTopologySignature(topology)
	if err != nil {
		t.Fatalf("VerifyTopologySignature: %v", err)
	}
	if !ok {
		t.Fatal("the checked-in topology must verify against its own signature")
	}
}
