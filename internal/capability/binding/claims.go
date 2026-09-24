package binding

import "sort"

// This file states, for each published capability, what the live tree
// actually offers it — not what a finished design would offer it. Every
// entry cites where the claim was read from, and a claim naming two wire
// methods or two handler symbols is left naming two: collapsing it to one
// would be inventing the very binding BIND-001 exists to demand.
//
// Three sources of evidence feed the table:
//
//  1. internal/transport/manifest/dispositions.go, which records that the
//     nine P1A domain capabilities are CategoryGenericIntentLifecycleOnly
//     (reachable only through IntentService's six generic lifecycle
//     methods, dispatched by the DefinitionReference in the request) and
//     that the two registry capabilities are CategoryTypedPublicMethod
//     served by a *pair* of RegistryService methods each.
//  2. internal/intent/app/capabilities.go's handlerFor switch, the one
//     place a published capability id is bound to a typed Go method. Its
//     default arm returns ErrCapabilityUnbound, which is why the two
//     registry capabilities have no handler there.
//  3. The dedicated typed routes in internal/transport/admin and
//     internal/transport/journey that answer the same domain question. Two
//     of them (GetWorkerState, ExplainTransaction) call the domain package
//     directly and never pass through the capability gateway; they are
//     listed because a caller can reach that answer through them, which is
//     precisely what makes "the" binding for those capabilities ambiguous.

// genericIntentLifecycleMethods are the six IntentService methods that
// dispatch by DefinitionReference rather than binding one route per
// capability (internal/transport/manifest/dispositions.go
// genericLifecycleEndpointIDs). A capability reachable only through them
// has no single wire descriptor.
func genericIntentLifecycleMethods() []string {
	return []string{
		"hcmnext.intents.v1.IntentService/CreateIntent",
		"hcmnext.intents.v1.IntentService/ExplainIntent",
		"hcmnext.intents.v1.IntentService/GetIntent",
		"hcmnext.intents.v1.IntentService/ListIntentTimeline",
		"hcmnext.intents.v1.IntentService/ListIntents",
		"hcmnext.intents.v1.IntentService/SimulateIntent",
	}
}

// appHandler names one method on internal/intent/app's domainHandlers, the
// composition cell that republishes the BOOTSTRAP capability table with
// real handlers bound to it.
func appHandler(name string) HandlerSymbol {
	return HandlerSymbol{PackagePath: "internal/intent/app", Receiver: "*domainHandlers", Name: name}
}

// appRegistryHandler names one method on internal/intent/app's
// IntentService, which serves the four RegistryService discovery RPCs.
func appRegistryHandler(name string) HandlerSymbol {
	return HandlerSymbol{PackagePath: "internal/intent/app", Receiver: "*IntentService", Name: name}
}

const (
	genericLifecycleRationale = "internal/transport/manifest/dispositions.go classifies this capability GENERIC_INTENT_LIFECYCLE_ONLY: it is dispatched by the DefinitionReference in the request, so six IntentService methods can carry it and none of them is its descriptor"
	registryPairRationale     = "internal/transport/manifest/dispositions.go typedRegistryEndpointIDs serves this capability from a pair of RegistryService methods, and internal/intent/app/capabilities.go handlerFor has no arm for it (its default returns ErrCapabilityUnbound), so the registry publishes it with no typed handler bound"
)

// Claims returns the reviewed claim table for the eleven BOOTSTRAP
// capabilities, sorted by capability id.
func Claims() []Claim {
	claims := []Claim{
		{
			CapabilityID:      "hcmnext.people.explain_worker_state",
			CapabilityVersion: 1,
			DefinitionRef:     "hcmnext.people.explain_worker_state/v1",
			WireMethods: append(genericIntentLifecycleMethods(),
				"hcmnext.admin.v1.AdminService/GetWorkerState"),
			Handlers: []HandlerSymbol{
				appHandler("explainWorkerState"),
				{PackagePath: "internal/transport/admin", Receiver: "*server", Name: "GetWorkerState"},
			},
			Rationale: genericLifecycleRationale + "; internal/transport/admin/worker_state.go additionally answers the same question as a direct passthrough to internal/domains/people.ExplainWorkerState, without the capability gateway",
		},
		{
			CapabilityID:      "hcmnext.people.promote_worker",
			CapabilityVersion: 1,
			DefinitionRef:     "hcmnext.people.promote_worker/v1",
			WireMethods: append(genericIntentLifecycleMethods(),
				"hcmnext.journey.v1.JourneyService/ProposePromotion"),
			Handlers: []HandlerSymbol{
				appHandler("promoteWorker"),
				{PackagePath: "internal/transport/journey", Receiver: "*server", Name: "ProposePromotion"},
			},
			Rationale: genericLifecycleRationale + "; internal/transport/journey/promotionpropose.go additionally forwards to the one Promotion application service, which does run the capability gateway",
		},
		{
			CapabilityID:      "hcmnext.rewards.simulate_compensation",
			CapabilityVersion: 1,
			DefinitionRef:     "hcmnext.rewards.simulate_compensation/v1",
			WireMethods:       genericIntentLifecycleMethods(),
			Handlers:          []HandlerSymbol{appHandler("simulateCompensation")},
			Rationale:         genericLifecycleRationale,
		},
		{
			CapabilityID:      "hcmnext.rewards.evaluate_pay_band_position",
			CapabilityVersion: 1,
			DefinitionRef:     "hcmnext.rewards.evaluate_pay_band_position/v1",
			WireMethods:       genericIntentLifecycleMethods(),
			Handlers:          []HandlerSymbol{appHandler("evaluatePayBandPosition")},
			Rationale:         genericLifecycleRationale,
		},
		{
			CapabilityID:      "hcmnext.intelligence.explain_transaction",
			CapabilityVersion: 1,
			DefinitionRef:     "hcmnext.intelligence.explain_transaction/v1",
			WireMethods: append(genericIntentLifecycleMethods(),
				"hcmnext.admin.v1.AdminService/ExplainTransaction"),
			Handlers: []HandlerSymbol{
				appHandler("explainTransaction"),
				{PackagePath: "internal/transport/admin", Receiver: "*server", Name: "ExplainTransaction"},
			},
			Rationale: genericLifecycleRationale + "; internal/transport/admin/explain_transaction.go additionally answers the same question as a direct passthrough to internal/domains/intelligence.ExplainTransaction, without the capability gateway",
		},
		{
			CapabilityID:      "hcmnext.operations.detect_drift",
			CapabilityVersion: 1,
			DefinitionRef:     "hcmnext.operations.detect_drift/v1",
			WireMethods:       genericIntentLifecycleMethods(),
			Handlers:          []HandlerSymbol{appHandler("detectDrift")},
			Rationale:         genericLifecycleRationale,
		},
		{
			CapabilityID:      "hcmnext.operations.create_repair_plan",
			CapabilityVersion: 1,
			DefinitionRef:     "hcmnext.operations.create_repair_plan/v1",
			WireMethods:       genericIntentLifecycleMethods(),
			Handlers:          []HandlerSymbol{appHandler("createRepairPlan")},
			Rationale:         genericLifecycleRationale,
		},
		{
			CapabilityID:      "hcmnext.operations.simulate_repair",
			CapabilityVersion: 1,
			DefinitionRef:     "hcmnext.operations.simulate_repair/v1",
			WireMethods:       genericIntentLifecycleMethods(),
			Handlers:          []HandlerSymbol{appHandler("simulateRepair")},
			Rationale:         genericLifecycleRationale,
		},
		{
			CapabilityID:      "hcmnext.dataops.explain_field_history",
			CapabilityVersion: 1,
			// This diagnostic capability has a live handler in the composition
			// cell, but no drafted intent definition or generated model binding.
			DefinitionRef: "",
			WireMethods:   genericIntentLifecycleMethods(),
			Handlers:      []HandlerSymbol{appHandler("explainFieldHistory")},
			Rationale:     genericLifecycleRationale + "; internal/intent/app/diagnostics.go binds the dataops handler, while the intent catalog has no drafted definition for this capability",
		},
		{
			CapabilityID:      "hcmnext.registry.resolve_capability",
			CapabilityVersion: 1,
			// The registry's self-description capabilities answer "what
			// exists"; no drafted BusinessIntent definition covers them, so
			// there is no model binding to resolve.
			DefinitionRef: "",
			WireMethods: []string{
				"hcmnext.registry.v1.RegistryService/ListCapabilities",
				"hcmnext.registry.v1.RegistryService/ListIntentDefinitions",
			},
			Handlers: []HandlerSymbol{
				appRegistryHandler("ListCapabilities"),
				appRegistryHandler("ListIntentDefinitions"),
			},
			Rationale: registryPairRationale,
		},
		{
			CapabilityID:      "hcmnext.registry.explain_capability",
			CapabilityVersion: 1,
			DefinitionRef:     "",
			WireMethods: []string{
				"hcmnext.registry.v1.RegistryService/GetCapability",
				"hcmnext.registry.v1.RegistryService/GetIntentDefinition",
			},
			Handlers: []HandlerSymbol{
				appRegistryHandler("GetCapability"),
				appRegistryHandler("GetIntentDefinition"),
			},
			Rationale: registryPairRationale,
		},
	}
	for i := range claims {
		sort.Strings(claims[i].WireMethods)
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].CapabilityID < claims[j].CapabilityID })
	return claims
}

// ClaimsByCapability returns [Claims] keyed by capability id.
func ClaimsByCapability() map[string]Claim {
	all := Claims()
	out := make(map[string]Claim, len(all))
	for _, c := range all {
		out[c.CapabilityID] = c
	}
	return out
}
