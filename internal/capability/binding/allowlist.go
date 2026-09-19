package binding

import (
	"fmt"
	"sort"
	"strings"
)

// AllowlistEntry records one gap that exists in the tree today, who owns
// closing it, and why it is a known state rather than an unnoticed one. It
// is keyed by [Gap.ID], so re-wording a gap's Detail can never silently
// re-open or re-close it, and a gap that grows a new candidate route
// changes its ID and falls out of the allowlist by construction.
type AllowlistEntry struct {
	GapID string
	// OwnerTodo is the planning/todos.md id that closes this gap.
	OwnerTodo string
	Rationale string
}

// Allowlist is the set of binding gaps the live tree has as of BIND-001's
// implementation. It is a ratchet, not an excuse: [ConformanceReport] fails
// on any gap that is not listed here, and equally on any listed gap that no
// longer exists, so the list can only shrink without someone editing it.
//
// The shape of today's state, in one sentence: no published capability has
// exactly one wire descriptor and exactly one typed Go handler, because the
// eight P1A domain capabilities are dispatched generically by
// DefinitionReference across six IntentService methods (with three of them
// additionally answerable through a dedicated admin/journey route), and the
// two registry self-description capabilities are served by a *pair* of
// RegistryService methods each with no arm in
// internal/intent/app.handlerFor at all.
func Allowlist() []AllowlistEntry {
	entries := []AllowlistEntry{}
	entries = append(entries, genericLifecycleAllowlist()...)
	entries = append(entries, registryCapabilityAllowlist()...)
	entries = append(entries, sharedHandlerAllowlist()...)
	entries = append(entries, unboundWireAllowlist()...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].GapID < entries[j].GapID })
	return entries
}

// AllowlistByGapID returns [Allowlist] keyed by GapID.
func AllowlistByGapID() map[string]AllowlistEntry {
	all := Allowlist()
	out := make(map[string]AllowlistEntry, len(all))
	for _, e := range all {
		out[e.GapID] = e
	}
	return out
}

const (
	// ownerBind001 owns turning the generic lifecycle dispatch into one
	// descriptor per capability, which is the remaining half of BIND-001
	// itself: this package proves the hole is total and named, and closing
	// it is a Protobuf and transport change no single lane may make alone.
	ownerBind001 = "BIND-001"
	// ownerProto010 owns descriptor-level RPC exposure disposition. Its
	// governedServices list covers only the intents and registry services,
	// which is why every admin and journey method is currently unbound.
	ownerProto010 = "PROTO-010"
	// ownerEndpoint009 owns the endpoint/capability cross-join whose
	// GENERIC_INTENT_LIFECYCLE_ONLY category is the source of the
	// ambiguity recorded here.
	ownerEndpoint009 = "ENDPOINT-009"
)

// genericLifecycleGapID is the AMBIGUOUS_WIRE_METHOD gap id for a
// capability reachable through the six generic lifecycle methods plus the
// optional extra dedicated routes given.
func genericLifecycleGapID(capabilityID string, extra ...string) string {
	refs := append(genericIntentLifecycleMethods(), extra...)
	sort.Strings(refs)
	return Gap{Kind: GapAmbiguousWireMethod, Capability: capabilityID, Subject: strings.Join(refs, ",")}.ID()
}

func genericLifecycleAllowlist() []AllowlistEntry {
	type row struct {
		capabilityID string
		extraWire    []string
		extraHandler []HandlerSymbol
		handler      HandlerSymbol
	}
	rows := []row{
		{
			capabilityID: "hcmnext.people.explain_worker_state",
			extraWire:    []string{"hcmnext.admin.v1.AdminService/GetWorkerState"},
			handler:      appHandler("explainWorkerState"),
			extraHandler: []HandlerSymbol{{PackagePath: "internal/transport/admin", Receiver: "*server", Name: "GetWorkerState"}},
		},
		{
			capabilityID: "hcmnext.people.promote_worker",
			extraWire:    []string{"hcmnext.journey.v1.JourneyService/ProposePromotion"},
			handler:      appHandler("promoteWorker"),
			extraHandler: []HandlerSymbol{{PackagePath: "internal/transport/journey", Receiver: "*server", Name: "ProposePromotion"}},
		},
		{
			capabilityID: "hcmnext.intelligence.explain_transaction",
			extraWire:    []string{"hcmnext.admin.v1.AdminService/ExplainTransaction"},
			handler:      appHandler("explainTransaction"),
			extraHandler: []HandlerSymbol{{PackagePath: "internal/transport/admin", Receiver: "*server", Name: "ExplainTransaction"}},
		},
		{capabilityID: "hcmnext.rewards.simulate_compensation", handler: appHandler("simulateCompensation")},
		{capabilityID: "hcmnext.rewards.evaluate_pay_band_position", handler: appHandler("evaluatePayBandPosition")},
		{capabilityID: "hcmnext.operations.detect_drift", handler: appHandler("detectDrift")},
		{capabilityID: "hcmnext.operations.create_repair_plan", handler: appHandler("createRepairPlan")},
		{capabilityID: "hcmnext.operations.simulate_repair", handler: appHandler("simulateRepair")},
	}

	out := make([]AllowlistEntry, 0, len(rows)*2)
	for _, r := range rows {
		out = append(out, AllowlistEntry{
			GapID:     genericLifecycleGapID(r.capabilityID, r.extraWire...),
			OwnerTodo: ownerEndpoint009,
			Rationale: "GENERIC_INTENT_LIFECYCLE_ONLY: the capability is dispatched by the DefinitionReference in the request across the six IntentService lifecycle methods, so no single descriptor is its own; closing this needs a per-capability typed method in the .proto",
		})
		if len(r.extraHandler) == 0 {
			continue
		}
		refs := []string{r.handler.Ref()}
		for _, h := range r.extraHandler {
			refs = append(refs, h.Ref())
		}
		sort.Strings(refs)
		out = append(out, AllowlistEntry{
			GapID:     Gap{Kind: GapAmbiguousHandler, Capability: r.capabilityID, Subject: strings.Join(refs, ",")}.ID(),
			OwnerTodo: ownerBind001,
			Rationale: "a dedicated admin/journey route answers the same domain question as the capability's composition-cell handler; two of the three (GetWorkerState, ExplainTransaction) reach the domain package without the capability gateway at all",
		})
	}
	return out
}

func registryCapabilityAllowlist() []AllowlistEntry {
	type row struct {
		capabilityID string
		wire         []string
		handlers     []HandlerSymbol
	}
	rows := []row{
		{
			capabilityID: "hcmnext.registry.resolve_capability",
			wire: []string{
				"hcmnext.registry.v1.RegistryService/ListCapabilities",
				"hcmnext.registry.v1.RegistryService/ListIntentDefinitions",
			},
			handlers: []HandlerSymbol{appRegistryHandler("ListCapabilities"), appRegistryHandler("ListIntentDefinitions")},
		},
		{
			capabilityID: "hcmnext.registry.explain_capability",
			wire: []string{
				"hcmnext.registry.v1.RegistryService/GetCapability",
				"hcmnext.registry.v1.RegistryService/GetIntentDefinition",
			},
			handlers: []HandlerSymbol{appRegistryHandler("GetCapability"), appRegistryHandler("GetIntentDefinition")},
		},
	}

	out := make([]AllowlistEntry, 0, len(rows)*3)
	for _, r := range rows {
		wire := append([]string(nil), r.wire...)
		sort.Strings(wire)
		out = append(out, AllowlistEntry{
			GapID:     Gap{Kind: GapAmbiguousWireMethod, Capability: r.capabilityID, Subject: strings.Join(wire, ",")}.ID(),
			OwnerTodo: ownerEndpoint009,
			Rationale: "typedRegistryEndpointIDs deliberately serves each registry self-description capability from a pair of methods (list-shaped and get-shaped); splitting the capability in two, or merging the routes, is an ENDPOINT-009 decision",
		})
		refs := make([]string, 0, len(r.handlers))
		for _, h := range r.handlers {
			refs = append(refs, h.Ref())
		}
		sort.Strings(refs)
		out = append(out, AllowlistEntry{
			GapID:     Gap{Kind: GapAmbiguousHandler, Capability: r.capabilityID, Subject: strings.Join(refs, ",")}.ID(),
			OwnerTodo: ownerBind001,
			Rationale: "the paired routes have one typed method each on internal/intent/app.IntentService, and internal/intent/app.handlerFor has no arm for this capability id at all (its default returns ErrCapabilityUnbound), so nothing the gateway can invoke implements it",
		})
		out = append(out, AllowlistEntry{
			GapID:     Gap{Kind: GapNoModelBinding, Capability: r.capabilityID}.ID(),
			OwnerTodo: ownerBind001,
			Rationale: "the registry self-description capabilities read the capability registry itself, which is platform metadata rather than a drafted BusinessIntent definition, so no generated model entity backs them; either a definition is drafted or the model footprint is declared empty by review",
		})
	}
	return out
}

// sharedHandlerAllowlist covers the typed symbols claimed by more than one
// capability. There are none today — every claimed symbol answers exactly
// one capability id — and the empty list is stated rather than omitted so
// the conformance check proves it stays empty.
func sharedHandlerAllowlist() []AllowlistEntry { return nil }

// unboundWireAllowlist covers every method of a registered service that no
// published capability claims. Three groups, three different reasons.
func unboundWireAllowlist() []AllowlistEntry {
	groups := []struct {
		owner     string
		rationale string
		refs      []string
	}{
		{
			owner:     ownerProto010,
			rationale: "REFUSED_P1A: the method exists in the descriptor so the P1B write path needs no breaking wire change, but every invocation is refused for the duration of P1A and no capability implements EXECUTE mode yet (planning/next-steps.md)",
			refs: []string{
				"hcmnext.intents.v1.IntentService/CancelIntent",
				"hcmnext.intents.v1.IntentService/ExecuteIntent",
				"hcmnext.intents.v1.IntentService/SubmitIntent",
				"hcmnext.intents.v1.IntentService/SupersedeIntent",
			},
		},
		{
			owner:     ownerProto010,
			rationale: "the AdminService is outside internal/transport/manifest's governedServices list, so no endpoint manifest row binds it to a capability; the operator surface reaches the domain packages directly",
			refs: []string{
				"hcmnext.admin.v1.AdminService/GetReleaseManifest",
				"hcmnext.admin.v1.AdminService/GetWorkflowInstance",
				"hcmnext.admin.v1.AdminService/ListCapabilityProfiles",
				"hcmnext.admin.v1.AdminService/ListIntents",
			},
		},
		{
			owner:     ownerProto010,
			rationale: "the JourneyService is outside internal/transport/manifest's governedServices list; its journey/worker routes are an experience surface with no published capability standing behind them",
			refs: []string{
				"hcmnext.journey.v1.JourneyService/AcknowledgeJourney",
				"hcmnext.journey.v1.JourneyService/AddJourneyNote",
				"hcmnext.journey.v1.JourneyService/CreateWorker",
				"hcmnext.journey.v1.JourneyService/DecideJourney",
				"hcmnext.journey.v1.JourneyService/EditProposal",
				"hcmnext.journey.v1.JourneyService/ExecuteJourney",
				"hcmnext.journey.v1.JourneyService/GetProductPreferences",
				"hcmnext.journey.v1.JourneyService/GetRoleAccess",
				"hcmnext.journey.v1.JourneyService/GetWorkerIDPolicy",
				"hcmnext.journey.v1.JourneyService/InspectJourney",
				"hcmnext.journey.v1.JourneyService/ListJourneys",
				"hcmnext.journey.v1.JourneyService/ListWorkers",
				"hcmnext.journey.v1.JourneyService/PreviewJourneyIntervention",
				"hcmnext.journey.v1.JourneyService/ProposeJourney",
				"hcmnext.journey.v1.JourneyService/RecordWorkflowUse",
				"hcmnext.journey.v1.JourneyService/RequestJourneyIntervention",
				"hcmnext.journey.v1.JourneyService/SaveAccessRole",
				"hcmnext.journey.v1.JourneyService/SaveOrganizationVisibility",
				"hcmnext.journey.v1.JourneyService/SaveRoleFeaturePermission",
				"hcmnext.journey.v1.JourneyService/SaveRoleOrganizationVisibility",
				"hcmnext.journey.v1.JourneyService/SaveRolePagePermission",
				"hcmnext.journey.v1.JourneyService/SaveTenantAppearance",
				"hcmnext.journey.v1.JourneyService/SaveUserPreferences",
				"hcmnext.journey.v1.JourneyService/SaveWorkerIDPolicy",
				"hcmnext.journey.v1.JourneyService/SaveWorkerRoleAssignment",
				"hcmnext.journey.v1.JourneyService/WatchJourney",
			},
		},
	}

	var out []AllowlistEntry
	for _, g := range groups {
		for _, ref := range g.refs {
			out = append(out, AllowlistEntry{
				GapID:     Gap{Kind: GapWireMethodUnbound, Subject: ref}.ID(),
				OwnerTodo: g.owner,
				Rationale: g.rationale,
			})
		}
	}
	return out
}

// ConformanceReport is the difference between the gaps the live tree has
// and the gaps [Allowlist] admits to.
type ConformanceReport struct {
	// NewGaps are gaps present in the table and absent from the allowlist:
	// a regression, and the reason this check exists.
	NewGaps []Gap
	// StaleAllowlist are allowlist entries whose gap no longer exists: the
	// ratchet turning, and a required edit rather than a free pass.
	StaleAllowlist []AllowlistEntry
	// Accepted are the gaps that matched an allowlist entry.
	Accepted []Gap
}

// OK reports whether the table matches the allowlist exactly.
func (r ConformanceReport) OK() bool { return len(r.NewGaps) == 0 && len(r.StaleAllowlist) == 0 }

// Explain renders the report deterministically.
func (r ConformanceReport) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "binding conformance: %d accepted, %d new, %d stale\n",
		len(r.Accepted), len(r.NewGaps), len(r.StaleAllowlist))
	for _, g := range r.NewGaps {
		fmt.Fprintf(&b, "NEW   %s: %s\n", g.ID(), g.Detail)
	}
	for _, e := range r.StaleAllowlist {
		fmt.Fprintf(&b, "STALE %s (owner %s)\n", e.GapID, e.OwnerTodo)
	}
	return b.String()
}

// CheckConformance compares a table's gaps against the allowlist. It is
// pure: the allowlist is compiled in and the table is supplied.
func CheckConformance(t Table, allowlist []AllowlistEntry) ConformanceReport {
	allowed := make(map[string]AllowlistEntry, len(allowlist))
	for _, e := range allowlist {
		allowed[e.GapID] = e
	}
	seen := map[string]bool{}

	var report ConformanceReport
	for _, g := range t.Gaps {
		if _, ok := allowed[g.ID()]; ok {
			seen[g.ID()] = true
			report.Accepted = append(report.Accepted, g)
			continue
		}
		report.NewGaps = append(report.NewGaps, g)
	}
	for _, e := range allowlist {
		if !seen[e.GapID] {
			report.StaleAllowlist = append(report.StaleAllowlist, e)
		}
	}
	sort.Slice(report.StaleAllowlist, func(i, j int) bool {
		return report.StaleAllowlist[i].GapID < report.StaleAllowlist[j].GapID
	})
	return report
}
