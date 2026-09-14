package pilotprovider

import (
	"strings"
	"testing"
)

// TestTodo_SELECT_002_Property is SELECT-002's PROPERTY test. It proves the
// GREEN invariant - every RED element Validate checks for actually causes a
// violation naming the exact broken field when removed, and a structurally
// complete fixture with none of those defects validates clean - holds
// across the whole schema, not just the one checked-in file. This mirrors
// tools/planning/pilotjurisdiction's TestTodo_SELECT_001_Property.
func TestTodo_SELECT_002_Property(t *testing.T) {
	base := validFixture()
	if v := base.Validate(); len(v) != 0 {
		t.Fatalf("validFixture() must validate clean, got: %v", v)
	}

	cases := []struct {
		name    string
		mutate  func(*ProviderTopology)
		wantHit string
	}{
		{"wrong todo id", func(p *ProviderTopology) { p.TodoID = "SELECT-001" }, "todo_id"},
		{"missing signed date", func(p *ProviderTopology) { p.SignedDate = "" }, "signed_date"},
		{"unknown selection status", func(p *ProviderTopology) { p.SelectionStatus = "MADE_UP" }, "selection_status"},

		{"placeholder vendor id lacks placeholder suffix", func(p *ProviderTopology) { p.Provider.VendorID = "some-real-looking-vendor" }, "provider.vendor_id"},
		{"placeholder status but vendor confirmation partially populated", func(p *ProviderTopology) {
			p.VendorConfirmation.ConfirmedByContact = "someone"
		}, "vendor_confirmation"},
		{"missing vendor display name", func(p *ProviderTopology) { p.Provider.VendorDisplayName = "" }, "provider.vendor_display_name"},
		{"missing product", func(p *ProviderTopology) { p.Provider.Product = "" }, "provider.product"},
		{"missing edition", func(p *ProviderTopology) { p.Provider.Edition = "" }, "provider.edition"},
		{"missing region", func(p *ProviderTopology) { p.Provider.Region = "" }, "provider.region"},
		{"missing api version", func(p *ProviderTopology) { p.Provider.APIVersion = "" }, "provider.api_version"},

		{"no api entitlements", func(p *ProviderTopology) { p.APIEntitlements = nil }, "api_entitlements"},
		{"entitlement missing scope", func(p *ProviderTopology) { p.APIEntitlements[0].Scope = "" }, "api_entitlements[0].scope"},
		{"entitlement missing description", func(p *ProviderTopology) { p.APIEntitlements[0].Description = "" }, "api_entitlements[0].description"},

		{"unknown sandbox fidelity", func(p *ProviderTopology) { p.Sandbox.Fidelity = "MADE_UP" }, "sandbox.fidelity"},
		{"sandbox missing description", func(p *ProviderTopology) { p.Sandbox.Description = "" }, "sandbox.description"},
		{"sandbox missing evidence ref", func(p *ProviderTopology) { p.Sandbox.EvidenceRef = "" }, "sandbox.evidence_ref"},
		{"sandbox missing executed at", func(p *ProviderTopology) { p.Sandbox.ExecutedAt = "" }, "sandbox.executed_at"},

		{"no field authorities", func(p *ProviderTopology) { p.FieldAuthorities = nil }, "field_authorities"},
		{"field authority missing field", func(p *ProviderTopology) { p.FieldAuthorities[0].Field = "" }, "field_authorities[0].field"},
		{"field authority unknown authority", func(p *ProviderTopology) { p.FieldAuthorities[0].Authority = "MADE_UP" }, "field_authorities[0].authority"},
		{"field authority missing rationale", func(p *ProviderTopology) { p.FieldAuthorities[0].Rationale = "" }, "field_authorities[0].rationale"},

		{"unknown observation mechanism", func(p *ProviderTopology) { p.Observation.Mechanism = "CARRIER_PIGEON" }, "observation.mechanism"},
		{"polling with no interval", func(p *ProviderTopology) {
			p.Observation.Mechanism = ObservationPolling
			p.Observation.PollIntervalSeconds = 0
		}, "observation.poll_interval_seconds"},
		{"webhook with no signature scheme", func(p *ProviderTopology) {
			p.Observation.Mechanism = ObservationWebhook
			p.Observation.WebhookSignatureScheme = ""
		}, "observation.webhook_signature_scheme"},

		{"quota missing limit window", func(p *ProviderTopology) { p.Quota.LimitWindow = "" }, "quota.limit_window"},
		{"quota non-positive request limit", func(p *ProviderTopology) { p.Quota.RequestLimit = 0 }, "quota.request_limit"},
		{"quota non-positive concurrency limit", func(p *ProviderTopology) { p.Quota.ConcurrencyLimit = 0 }, "quota.concurrency_limit"},
		{"quota missing burst policy", func(p *ProviderTopology) { p.Quota.BurstPolicy = "" }, "quota.burst_policy"},

		{"timeout non-positive", func(p *ProviderTopology) { p.Timeout.RequestTimeoutSeconds = 0 }, "timeout.request_timeout_seconds"},
		{"timeout ambiguous action not observe-before-retry", func(p *ProviderTopology) { p.Timeout.AmbiguousOutcomeAction = "RETRY_BLINDLY" }, "timeout.ambiguous_outcome_action"},

		{"unknown credential scheme", func(p *ProviderTopology) { p.Credential.Scheme = "CARRIER_PIGEON" }, "credential.scheme"},
		{"credential non-positive rotation", func(p *ProviderTopology) { p.Credential.RotationDays = 0 }, "credential.rotation_days"},
		{"credential no scope granted", func(p *ProviderTopology) { p.Credential.ScopeGranted = nil }, "credential.scope_granted"},

		{"data processing missing residency", func(p *ProviderTopology) { p.DataProcessing.ResidencyRegion = "" }, "data_processing.residency_region"},
		{"data processing non-positive retention", func(p *ProviderTopology) { p.DataProcessing.RetentionDays = 0 }, "data_processing.retention_days"},
		{"data processing missing subprocessor disclosure", func(p *ProviderTopology) { p.DataProcessing.SubprocessorDisclosure = "" }, "data_processing.subprocessor_disclosure"},
		{"data processing missing dpa reference", func(p *ProviderTopology) { p.DataProcessing.DPAReference = "" }, "data_processing.dpa_reference"},

		{"no operations", func(p *ProviderTopology) { p.Operations = nil }, "operations"},
		{"operation unknown verb", func(p *ProviderTopology) { p.Operations[0].Verb = "DELETE" }, "operations[0].verb"},
		{"operation missing capability id", func(p *ProviderTopology) { p.Operations[0].CapabilityOrIntentID = "" }, "operations[0].capability_or_intent_id"},
		{"operation missing contract id", func(p *ProviderTopology) { p.Operations[0].ContractID = "" }, "operations[0].contract_id"},
		{"operation missing contract version", func(p *ProviderTopology) { p.Operations[0].ContractVersion = "" }, "operations[0].contract_version"},
		{"operation missing owner", func(p *ProviderTopology) { p.Operations[0].Owner = "" }, "operations[0].owner"},
		{"operation verb coverage incomplete", func(p *ProviderTopology) { p.Operations = p.Operations[1:] }, "missing an operation mapping for verb"},

		{"no faults", func(p *ProviderTopology) { p.Faults = nil }, "faults"},
		{"fault unknown class", func(p *ProviderTopology) { p.Faults[0].Class = "MADE_UP" }, "faults[0].class"},
		{"fault missing example response", func(p *ProviderTopology) { p.Faults[0].ExampleResponse = "" }, "faults[0].example_response"},
		{"fault missing default action", func(p *ProviderTopology) { p.Faults[0].DefaultAction = "" }, "faults[0].default_action"},
		{"fault missing evidence ref", func(p *ProviderTopology) { p.Faults[0].EvidenceRef = "" }, "faults[0].evidence_ref"},
		{"fault duplicate class", func(p *ProviderTopology) { p.Faults[1].Class = p.Faults[0].Class }, "duplicate fault class"},
		{"fault class coverage incomplete", func(p *ProviderTopology) { p.Faults = p.Faults[1:] }, "missing required fault class"},

		{"cross-system observation missing description", func(p *ProviderTopology) { p.CrossSystemObservation.Description = "" }, "cross_system_observation.description"},
		{"cross-system observation invalid method BOTH", func(p *ProviderTopology) { p.CrossSystemObservation.Method = ObservationBoth }, "cross_system_observation.method"},
		{"cross-system observation not independent", func(p *ProviderTopology) {
			p.CrossSystemObservation.ProvesIndependentOfWriteAcknowledgement = false
		}, "proves_independent_of_write_acknowledgement"},
		{"cross-system observation missing evidence ref", func(p *ProviderTopology) { p.CrossSystemObservation.EvidenceRef = "" }, "cross_system_observation.evidence_ref"},

		{"exit missing stop new work", func(p *ProviderTopology) { p.Exit.StopNewWork = "" }, "exit.stop_new_work"},
		{"exit missing preserve/reconcile", func(p *ProviderTopology) { p.Exit.PreserveOrReconcileInFlight = "" }, "exit.preserve_or_reconcile_in_flight"},
		{"exit missing revoke credentials", func(p *ProviderTopology) { p.Exit.RevokeCredentials = "" }, "exit.revoke_credentials"},
		{"exit missing activate fallback", func(p *ProviderTopology) { p.Exit.ActivateFallback = "" }, "exit.activate_fallback"},
		{"exit missing drain/rollback/reopen", func(p *ProviderTopology) { p.Exit.DrainRollbackReopen = "" }, "exit.drain_rollback_reopen"},
		{"exit missing exception expiry", func(p *ProviderTopology) { p.Exit.ExceptionExpiry = "" }, "exit.exception_expiry"},

		{"commercial missing metric", func(p *ProviderTopology) { p.Commercial.Metric = "" }, "commercial.metric"},
		{"commercial missing limit", func(p *ProviderTopology) { p.Commercial.Limit = "" }, "commercial.limit"},
		{"commercial missing overage policy", func(p *ProviderTopology) { p.Commercial.OveragePolicy = "" }, "commercial.overage_policy"},

		{"no stop/reselect thresholds", func(p *ProviderTopology) { p.StopReselectThresholds = nil }, "stop_reselect_thresholds"},
		{"stop/reselect threshold unknown action", func(p *ProviderTopology) { p.StopReselectThresholds[0].Action = "MADE_UP" }, "stop_reselect_thresholds[0].action"},

		{"no signature", func(p *ProviderTopology) { p.Signature = nil }, "signature"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := deepCopy(base)
			tc.mutate(&p)
			violations := p.Validate()
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

// TestTodo_SELECT_002_Property_VendorConfirmedFlowValidatesCleanOnlyWhenComplete
// proves the other direction of the selection-status consistency check: a
// topology that flips to VENDOR_CONFIRMED validates clean only once the
// vendor id drops the placeholder suffix AND vendor_confirmation is fully
// populated - not on either alone. This is what lets doc.go claim swapping
// in a real vendor is a pure data change: the schema already supports it,
// gated by the same Validate function.
func TestTodo_SELECT_002_Property_VendorConfirmedFlowValidatesCleanOnlyWhenComplete(t *testing.T) {
	confirmed := deepCopy(validFixture())
	confirmed.SelectionStatus = StatusVendorConfirmed
	// Flag flipped alone: still invalid on both counts.
	violations := confirmed.Validate()
	if len(violations) == 0 {
		t.Fatal("expected violations when selection_status flips without fixing vendor id or vendor_confirmation")
	}

	// Fix only the vendor id: vendor_confirmation still empty.
	partial := deepCopy(confirmed)
	partial.Provider.VendorID = "acme-hcm-production"
	violations = partial.Validate()
	found := false
	for _, v := range violations {
		if strings.Contains(v.String(), "vendor_confirmation") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a vendor_confirmation violation when only the vendor id was fixed, got %v", violations)
	}

	// Fix both: now it validates clean.
	complete := deepCopy(partial)
	complete.VendorConfirmation = VendorConfirmation{
		ConfirmedByContact:  "Jane Procurement",
		ContractDocumentRef: "contracts/acme-hcm-msa-2027.pdf",
		SignedEffectiveDate: "2027-01-01",
	}
	if v := complete.Validate(); len(v) != 0 {
		t.Fatalf("expected a clean validation once vendor id and vendor_confirmation both reflect a real selection, got %v", v)
	}
}
