package manifest

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
)

// EndpointDispositionCategory is the ENDPOINT-009 exact-join category every
// contracted intent definition and every published capability must map to.
// planning/specs/http-grpc-endpoint-contract.md#expansion-rule.
type EndpointDispositionCategory string

const (
	// CategoryTypedPublicMethod means a dedicated RPC method exists for
	// exactly this definition or capability (no other definition or
	// capability shares the route).
	CategoryTypedPublicMethod EndpointDispositionCategory = "TYPED_PUBLIC_METHOD"
	// CategoryGenericIntentLifecycleOnly means the definition or capability
	// is reachable only through IntentService's generic lifecycle methods
	// (CreateIntent, GetIntent, ListIntents, SimulateIntent, ExplainIntent,
	// ListIntentTimeline), which dispatch by DefinitionReference/capability
	// ID carried in the request rather than binding one RPC per definition.
	CategoryGenericIntentLifecycleOnly EndpointDispositionCategory = "GENERIC_INTENT_LIFECYCLE_ONLY"
	// CategoryInternalCapabilityOnly means the capability is invoked only
	// by trusted internal callers (never dispatched from a public request),
	// so no public route names it at all.
	CategoryInternalCapabilityOnly EndpointDispositionCategory = "INTERNAL_CAPABILITY_ONLY"
	// CategoryEventOrScheduleOnly means the only reachable initiators are
	// SCHEDULE/SYSTEM_EVENT, never a public request/response RPC.
	CategoryEventOrScheduleOnly EndpointDispositionCategory = "EVENT_OR_SCHEDULE_ONLY"
	// CategoryNoEndpointWithJustification means nothing currently exposes
	// this definition or capability, and Justification records why that is
	// a reviewed decision rather than an oversight.
	CategoryNoEndpointWithJustification EndpointDispositionCategory = "NO_ENDPOINT_WITH_JUSTIFICATION"
)

// Valid reports whether c is one of the five declared categories.
func (c EndpointDispositionCategory) Valid() bool {
	switch c {
	case CategoryTypedPublicMethod, CategoryGenericIntentLifecycleOnly,
		CategoryInternalCapabilityOnly, CategoryEventOrScheduleOnly,
		CategoryNoEndpointWithJustification:
		return true
	default:
		return false
	}
}

// IntentDisposition is one intent definition's ENDPOINT-009 mapping.
type IntentDisposition struct {
	DefinitionRef    string                      `json:"definition_ref"` // "<intent_type_id>/v<version>"
	DisplayName      string                      `json:"display_name"`
	Release          string                      `json:"release"`
	Category         EndpointDispositionCategory `json:"category"`
	Justification    string                      `json:"justification"`
	ServingEndpoints []string                    `json:"serving_endpoints"` // sorted EndpointDefinition.EndpointID values, when applicable
}

// CapabilityDisposition is one BOOTSTRAP capability's ENDPOINT-009 mapping.
type CapabilityDisposition struct {
	CapabilityID     string                      `json:"capability_id"`
	Version          uint32                      `json:"version"`
	Category         EndpointDispositionCategory `json:"category"`
	Justification    string                      `json:"justification"`
	ServingEndpoints []string                    `json:"serving_endpoints"`
}

// DispositionReport is the total ENDPOINT-009 cross-join: every contracted
// intent definition and every published capability, each with exactly one
// category.
type DispositionReport struct {
	Intents      []IntentDisposition     `json:"intents"`      // sorted by DefinitionRef
	Capabilities []CapabilityDisposition `json:"capabilities"` // sorted by CapabilityID, then Version
}

// genericLifecycleEndpointIDs is the sorted set of IntentService methods
// that dispatch generically by DefinitionReference/capability ID, rather
// than binding one dedicated route per definition or capability. It is the
// ServingEndpoints value for every CategoryGenericIntentLifecycleOnly row.
func genericLifecycleEndpointIDs() []string {
	return sortStrings([]string{
		"hcmnext.intents.v1.IntentService/CreateIntent",
		"hcmnext.intents.v1.IntentService/GetIntent",
		"hcmnext.intents.v1.IntentService/ListIntents",
		"hcmnext.intents.v1.IntentService/SimulateIntent",
		"hcmnext.intents.v1.IntentService/ExplainIntent",
		"hcmnext.intents.v1.IntentService/ListIntentTimeline",
	})
}

// typedRegistryEndpointIDs names the dedicated RPCs a
// CategoryTypedPublicMethod registry capability is served by.
func typedRegistryEndpointIDs(capabilityID string) ([]string, error) {
	switch capabilityID {
	case "hcmnext.registry.resolve_capability":
		// resolve_capability answers "what exists" — the two List methods.
		return []string{
			"hcmnext.registry.v1.RegistryService/ListIntentDefinitions",
			"hcmnext.registry.v1.RegistryService/ListCapabilities",
		}, nil
	case "hcmnext.registry.explain_capability":
		// explain_capability answers "explain this one" — the two Get methods.
		return []string{
			"hcmnext.registry.v1.RegistryService/GetIntentDefinition",
			"hcmnext.registry.v1.RegistryService/GetCapability",
		}, nil
	default:
		return nil, fmt.Errorf("manifest: %s has no typed registry endpoint binding", capabilityID)
	}
}

// p1bJustification is the ENDPOINT-009 justification shared by every P1B
// intent definition: next-steps.md gates the entire write path behind a
// signed Gate A PROCEED, so "no endpoint yet" is the reviewed decision, not
// an oversight.
const p1bJustification = "gated behind a signed Gate A PROCEED (planning/next-steps.md \"P1B exists only after a signed Gate A PROCEED\"); SubmitIntent/CancelIntent/SupersedeIntent exist on the wire (DispositionRefusedP1A) but no capability implements EXECUTE mode yet"

// conformanceJustification is the justification for a ReleaseConformance
// definition: it is a design fixture no release schedules.
const conformanceJustification = "ReleaseConformance: a design/conformance fixture (\"DESIGN_CONFORMANCE_ONLY\" phase depth) that no release schedules for execution; it has no capability binding to expose"

// BuildDispositionReport computes the ENDPOINT-009 total cross-join from an
// explicit set of intent definitions and capability records, so the
// business rule is testable against a fixture without touching the compiled
// BOOTSTRAP tables. See [BuildDefaultDispositionReport] for the production
// wiring against internal/intent/definitions and internal/capability.
func BuildDispositionReport(defs []intent.Definition, records []capability.Record) (*DispositionReport, error) {
	report := &DispositionReport{}

	for _, d := range defs {
		row := IntentDisposition{
			DefinitionRef: d.Ref.String(),
			DisplayName:   d.DisplayName,
			Release:       d.Release.String(),
		}
		switch d.Release {
		case intent.ReleaseP1A:
			row.Category = CategoryGenericIntentLifecycleOnly
			row.Justification = "one of the eight P1A executable intent contracts (planning/next-steps.md); reachable only through IntentService's generic lifecycle methods"
			row.ServingEndpoints = genericLifecycleEndpointIDs()
		case intent.ReleaseP1B:
			row.Category = CategoryNoEndpointWithJustification
			row.Justification = p1bJustification
		case intent.ReleaseConformance:
			row.Category = CategoryNoEndpointWithJustification
			row.Justification = conformanceJustification
		default:
			return nil, fmt.Errorf("manifest: intent definition %s has an unscheduled release %s", d.Ref.String(), d.Release.String())
		}
		if !row.Category.Valid() {
			return nil, fmt.Errorf("manifest: intent definition %s produced an invalid category", d.Ref.String())
		}
		report.Intents = append(report.Intents, row)
	}

	for _, rec := range records {
		row := CapabilityDisposition{
			CapabilityID: rec.Definition.ID,
			Version:      rec.Definition.Version,
		}
		switch {
		case rec.Definition.ID == "hcmnext.registry.resolve_capability" || rec.Definition.ID == "hcmnext.registry.explain_capability":
			endpoints, err := typedRegistryEndpointIDs(rec.Definition.ID)
			if err != nil {
				return nil, err
			}
			row.Category = CategoryTypedPublicMethod
			row.Justification = "backs a dedicated RegistryService RPC pair, not the generic IntentService lifecycle"
			row.ServingEndpoints = sortStrings(endpoints)
		default:
			// Every other BOOTSTRAP capability ID is a P1A domain capability
			// (the eight intent contracts plus the independently exposed
			// effective-date debugger; see bootstrap.go bootstrapDefinitions),
			// dispatched only by the generic IntentService lifecycle through
			// the DefinitionReference in the request.
			row.Category = CategoryGenericIntentLifecycleOnly
			row.Justification = "a P1A domain capability invoked only by IntentService's generic lifecycle dispatch, never by a dedicated typed route"
			row.ServingEndpoints = genericLifecycleEndpointIDs()
		}
		if !row.Category.Valid() {
			return nil, fmt.Errorf("manifest: capability %s produced an invalid category", rec.Definition.ID)
		}
		report.Capabilities = append(report.Capabilities, row)
	}

	sort.Slice(report.Intents, func(i, j int) bool { return report.Intents[i].DefinitionRef < report.Intents[j].DefinitionRef })
	sort.Slice(report.Capabilities, func(i, j int) bool {
		if report.Capabilities[i].CapabilityID != report.Capabilities[j].CapabilityID {
			return report.Capabilities[i].CapabilityID < report.Capabilities[j].CapabilityID
		}
		return report.Capabilities[i].Version < report.Capabilities[j].Version
	})
	return report, nil
}

// BuildDefaultDispositionReport is [BuildDispositionReport] wired to the
// production sources ENDPOINT-009 names: the fourteen definitions from
// internal/intent/definitions and the BOOTSTRAP capabilities from
// internal/capability.
func BuildDefaultDispositionReport() (*DispositionReport, error) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		return nil, fmt.Errorf("manifest: building BOOTSTRAP capability registry: %w", err)
	}
	records := registry.List()
	defs := definitions.All()
	return BuildDispositionReport(defs, records)
}

// P1AIntentRefs returns the sorted definition_ref strings
// ("<intent_type_id>/v<version>") of every ReleaseP1A definition in defs.
// [Build] uses this — sourced from the same internal/intent/definitions
// table [BuildDefaultDispositionReport] reads — as the
// AcceptedIntentDefinitionRefs value for every generic IntentService
// lifecycle method, so the two ENDPOINT-001 and ENDPOINT-009 views can never
// name a different set of currently-servable definitions.
func P1AIntentRefs(defs []intent.Definition) []string {
	var out []string
	for _, d := range defs {
		if d.Release == intent.ReleaseP1A {
			out = append(out, d.Ref.String())
		}
	}
	return sortStrings(out)
}

// AllIntentRefs returns the sorted definition_ref strings of every
// definition in defs, regardless of release. RegistryService's discovery
// methods use this: the BOOTSTRAP registry resolves every catalog
// definition, not only the ones a caller may currently act on.
func AllIntentRefs(defs []intent.Definition) []string {
	var out []string
	for _, d := range defs {
		out = append(out, d.Ref.String())
	}
	return sortStrings(out)
}

// P1ACapabilityIDs returns the sorted capability IDs of every BOOTSTRAP
// record other than the two registry-only capabilities (which back
// RegistryService's own typed methods, not the generic IntentService
// lifecycle). [Build] uses this as the CapabilityRefs value for every
// generic IntentService lifecycle method.
func P1ACapabilityIDs(records []capability.Record) []string {
	var out []string
	for _, rec := range records {
		if rec.Definition.ID == "hcmnext.registry.resolve_capability" || rec.Definition.ID == "hcmnext.registry.explain_capability" {
			continue
		}
		out = append(out, rec.Definition.ID)
	}
	return sortUniqueStrings(out)
}
