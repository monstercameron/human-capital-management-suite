package manifest

import (
	"fmt"
	"sort"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
)

// canonicalErrorCodes is the fixed set of hcmnext.common.v1.ErrorCode names
// every governed method may return, per the canonical error projection
// table (planning/specs/http-grpc-endpoint-contract.md). It excludes only
// ERROR_CODE_UNSPECIFIED, which is never a valid wire assertion.
func canonicalErrorCodes() []string {
	codes := commonv1.ErrorCode(0).Descriptor().Values()
	out := make([]string, 0, codes.Len())
	for i := 0; i < codes.Len(); i++ {
		v := codes.Get(i)
		if v.Number() == 0 {
			continue
		}
		out = append(out, string(v.Name()))
	}
	return sortStrings(out)
}

// interactiveReadDeadlineMillis matches bootstrap.go's compiled-in SLO class
// for every BOOTSTRAP capability: "slo.interactive.p95-2s.v1".
const interactiveReadDeadlineMillis = 2000

// rule is the hand-reviewed, per-method policy this manifest publishes. It
// is deliberately not "generated" the way [DiscoverRPCs] and
// [RequiredFieldPaths] are: gRPC service reflection cannot recover HTTP
// binding, disposition, ownership or idempotency classification, so those
// remain reviewed domain facts, exactly as
// planning/specs/http-grpc-endpoint-contract.md describes ("The manifest is
// generated from reviewed Protobuf, capability and BusinessIntent
// metadata"). [Build] is what is generated: the deterministic assembly of
// this table against the live descriptor set, catching drift the moment a
// method is added, renamed or removed.
type rule struct {
	owner               string
	behavior            IntentBehavior
	disposition         Disposition
	dispositionReason   string
	httpMethod          string
	httpPath            string
	httpBody            string
	authzAction         string
	classificationRef   string
	idempotencyClass    IdempotencyClass
	idempotencyKeySrc   string
	revisionPolicy      string
	retryPolicy         string
	paginationPolicy    string
	orderingPolicy      string
	compatibilityStatus string
	phase               string
	// genericLifecycle marks a method whose capability/intent bindings come
	// from the live P1A capability/definition set (dispositions.go) rather
	// than one fixed capability. Registry discovery methods leave this
	// false and set capabilityRefs/acceptedRefs directly.
	genericLifecycle bool
	capabilityRefs   []string // used only when !genericLifecycle
	acceptedAll      bool     // registry discovery: accepts every catalog definition_ref
}

const (
	servedReason = "one of the eight P1A executable intent contracts, or the RegistryService discovery surface that publishes them (planning/next-steps.md)"
	// servedWriteReason marks the governed writes that left the P1A refusal
	// behind. A method carrying it accepts real requests and persists real
	// state, so discovery must not keep telling callers it is refused.
	servedWriteReason = "a governed intent-lifecycle write served from Gate B onward (EP-INTENT-003): the request binds an exact revision, is deduplicated by idempotency key, and reports its outcome truthfully rather than refusing"
	refusedReason     = "exists in the compiled service descriptor so the P1B write path needs no breaking wire change, but every invocation is refused for the duration of P1A (planning/next-steps.md: \"must persist zero worker, employment, assignment, organization, position, compensation or budget mutations\")"
)

// authnAssuranceSubstantial is the uniform minimum session assurance level
// (internal/trust.AssuranceSubstantial) every governed method requires
// today. No current method needs AssuranceHigh; nothing here is exempt down
// to AssuranceLow.
const authnAssuranceSubstantial = "SUBSTANTIAL"

// rules is keyed by gRPC procedure path (identical to [RPCDescriptor.GRPCProcedure]).
func rules() map[string]rule {
	return map[string]rule{
		"/hcmnext.intents.v1.IntentService/CreateIntent": {
			owner: "GOVERNANCE", behavior: IntentBehaviorCreates,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "POST", httpPath: "/v1/intents", httpBody: "*",
			authzAction: "hcmnext.intents.create", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyKey, idempotencyKeySrc: "request.idempotency_key",
			revisionPolicy: "NOT_APPLICABLE", retryPolicy: "IDEMPOTENCY_KEY_DEDUPED",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/GetIntent": {
			owner: "GOVERNANCE", behavior: IntentBehaviorObserves,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "GET", httpPath: "/v1/intents/{intent}", httpBody: "",
			authzAction: "hcmnext.intents.get", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "RETURNS_ETAG", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/ListIntents": {
			owner: "GOVERNANCE", behavior: IntentBehaviorObserves,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "GET", httpPath: "/v1/intents", httpBody: "",
			authzAction: "hcmnext.intents.list", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "NOT_APPLICABLE", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "OPAQUE_CURSOR_BOUNDED", orderingPolicy: "STABLE_SNAPSHOT_ORDER",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/SimulateIntent": {
			owner: "GOVERNANCE", behavior: IntentBehaviorNonMaterial,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "POST", httpPath: "/v1/intents/{intent}:simulate", httpBody: "*",
			authzAction: "hcmnext.intents.simulate", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "REQUIRED_EXACT_MATCH", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/ExecuteIntent": {
			owner: "GOVERNANCE", behavior: IntentBehaviorConsumes,
			disposition: DispositionRefusedP1A, dispositionReason: refusedReason,
			httpMethod: "POST", httpPath: "/v1/intents/{intent}:execute", httpBody: "*",
			authzAction: "hcmnext.intents.execute", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyKey, idempotencyKeySrc: "request.idempotency_key",
			revisionPolicy: "REQUIRED_EXACT_MATCH", retryPolicy: "IDEMPOTENCY_KEY_DEDUPED",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_B",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/SubmitIntent": {
			owner: "GOVERNANCE", behavior: IntentBehaviorConsumes,
			disposition: DispositionServed, dispositionReason: servedWriteReason,
			httpMethod: "POST", httpPath: "/v1/intents/{intent}:submit", httpBody: "*",
			authzAction: "hcmnext.intents.submit", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyKey, idempotencyKeySrc: "request.idempotency_key",
			revisionPolicy: "REQUIRED_EXACT_MATCH", retryPolicy: "IDEMPOTENCY_KEY_DEDUPED",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_B",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/CancelIntent": {
			owner: "GOVERNANCE", behavior: IntentBehaviorConsumes,
			disposition: DispositionServed, dispositionReason: servedWriteReason,
			httpMethod: "POST", httpPath: "/v1/intents/{intent}:cancel", httpBody: "*",
			authzAction: "hcmnext.intents.cancel", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyKey, idempotencyKeySrc: "request.idempotency_key",
			revisionPolicy: "REQUIRED_EXACT_MATCH", retryPolicy: "IDEMPOTENCY_KEY_DEDUPED",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_B",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/SupersedeIntent": {
			owner: "GOVERNANCE", behavior: IntentBehaviorEmits,
			disposition: DispositionServed, dispositionReason: servedWriteReason,
			httpMethod: "POST", httpPath: "/v1/intents/{intent}:supersede", httpBody: "*",
			authzAction: "hcmnext.intents.supersede", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyKey, idempotencyKeySrc: "request.idempotency_key",
			revisionPolicy: "REQUIRED_EXACT_MATCH", retryPolicy: "IDEMPOTENCY_KEY_DEDUPED",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_B",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/ExplainIntent": {
			owner: "GOVERNANCE", behavior: IntentBehaviorObserves,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "GET", httpPath: "/v1/intents/{intent}/explanation", httpBody: "",
			authzAction: "hcmnext.intents.explain", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "NOT_APPLICABLE", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/ListIntentTimeline": {
			owner: "GOVERNANCE", behavior: IntentBehaviorObserves,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "GET", httpPath: "/v1/intents/{intent}/timeline", httpBody: "",
			authzAction: "hcmnext.intents.timeline", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "NOT_APPLICABLE", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "OPAQUE_CURSOR_BOUNDED", orderingPolicy: "STABLE_CHRONOLOGICAL_ORDER",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/RecommendIntentAction": {
			owner: "GOVERNANCE", behavior: IntentBehaviorNonMaterial,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "POST", httpPath: "/hcmnext.intents.v1.IntentService/RecommendIntentAction", httpBody: "*",
			authzAction: "hcmnext.intents.recommend", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "NOT_APPLICABLE", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/GetIntentDeepLink": {
			owner: "GOVERNANCE", behavior: IntentBehaviorObserves,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "POST", httpPath: "/hcmnext.intents.v1.IntentService/GetIntentDeepLink", httpBody: "*",
			authzAction: "hcmnext.intents.deeplink", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "RETURNS_ETAG", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/InspectIntentFields": {
			owner: "GOVERNANCE", behavior: IntentBehaviorObserves,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "POST", httpPath: "/hcmnext.intents.v1.IntentService/InspectIntentFields", httpBody: "*",
			authzAction: "hcmnext.intents.inspect", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "RETURNS_ETAG", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			genericLifecycle: true,
		},
		"/hcmnext.intents.v1.IntentService/ExportIntentFields": {
			owner: "GOVERNANCE", behavior: IntentBehaviorObserves,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "POST", httpPath: "/hcmnext.intents.v1.IntentService/ExportIntentFields", httpBody: "*",
			authzAction: "hcmnext.intents.export", classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "RETURNS_ETAG", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			genericLifecycle: true,
		},

		"/hcmnext.registry.v1.RegistryService/ListIntentDefinitions": {
			owner: "REGISTRY", behavior: IntentBehaviorNonMaterial,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "GET", httpPath: "/v1/intent-definitions", httpBody: "",
			authzAction: "hcmnext.registry.intent_definitions.list", classificationRef: "INTERNAL_METADATA",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "NOT_APPLICABLE", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "OPAQUE_CURSOR_BOUNDED", orderingPolicy: "STABLE_CATALOG_ORDER",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			capabilityRefs: []string{"hcmnext.registry.resolve_capability"}, acceptedAll: true,
		},
		"/hcmnext.registry.v1.RegistryService/GetIntentDefinition": {
			owner: "REGISTRY", behavior: IntentBehaviorNonMaterial,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "GET", httpPath: "/v1/intent-definitions/{intent_definition}", httpBody: "",
			authzAction: "hcmnext.registry.intent_definitions.get", classificationRef: "INTERNAL_METADATA",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "NOT_APPLICABLE", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			capabilityRefs: []string{"hcmnext.registry.explain_capability"}, acceptedAll: true,
		},
		"/hcmnext.registry.v1.RegistryService/ListCapabilities": {
			owner: "REGISTRY", behavior: IntentBehaviorNonMaterial,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "GET", httpPath: "/v1/capabilities", httpBody: "",
			authzAction: "hcmnext.registry.capabilities.list", classificationRef: "INTERNAL_METADATA",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "NOT_APPLICABLE", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "OPAQUE_CURSOR_BOUNDED", orderingPolicy: "STABLE_CATALOG_ORDER",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			capabilityRefs: []string{"hcmnext.registry.resolve_capability"},
		},
		"/hcmnext.registry.v1.RegistryService/GetCapability": {
			owner: "REGISTRY", behavior: IntentBehaviorNonMaterial,
			disposition: DispositionServed, dispositionReason: servedReason,
			httpMethod: "GET", httpPath: "/v1/capabilities/{capability}", httpBody: "",
			authzAction: "hcmnext.registry.capabilities.get", classificationRef: "INTERNAL_METADATA",
			idempotencyClass: IdempotencyReadSafe,
			revisionPolicy:   "NOT_APPLICABLE", retryPolicy: "CLIENT_MAY_RETRY",
			paginationPolicy: "NOT_APPLICABLE", orderingPolicy: "NOT_APPLICABLE",
			compatibilityStatus: "ACTIVE", phase: "GATE_A",
			capabilityRefs: []string{"hcmnext.registry.explain_capability"},
		},
	}
}

// Build assembles the total EndpointManifest from the live Protobuf service
// descriptors (via [DiscoverRPCs]), the reviewed per-method [rule] table,
// [RequiredFieldPaths], and the live P1A capability/intent sets computed
// from internal/capability and internal/intent/definitions. It fails closed
// — rejecting an unowned, unbound or handwritten route — rather than
// returning a partial manifest:
//
//   - a descriptor method with no rule entry ("handwritten route": present
//     on the wire, absent from review) is an error;
//   - a rule entry naming a method absent from the descriptor set (a stale
//     manifest row) is an error;
//   - any row with an empty owner, empty capability set or invalid
//     disposition/behavior/idempotency-class value is an error.
func Build() (*EndpointManifest, error) {
	rpcs, err := DiscoverRPCs()
	if err != nil {
		return nil, err
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		return nil, fmt.Errorf("manifest: building BOOTSTRAP capability registry: %w", err)
	}
	return build(rpcs, rules(), registry.List(), definitions.All())
}

// build is [Build]'s pure core: every input is explicit, so tests can prove
// the totality/ownership/binding checks against a controlled fixture
// without depending on the compiled BOOTSTRAP tables.
func build(rpcs []RPCDescriptor, ruleTable map[string]rule, records []capability.Record, defs []intent.Definition) (*EndpointManifest, error) {
	p1aCaps := P1ACapabilityIDs(records)
	p1aIntents := P1AIntentRefs(defs)
	allIntents := AllIntentRefs(defs)
	errCodes := canonicalErrorCodes()

	seen := make(map[string]bool, len(rpcs))
	out := make([]EndpointDefinition, 0, len(rpcs))

	for _, d := range rpcs {
		procedure := d.GRPCProcedure()
		seen[procedure] = true

		r, ok := ruleTable[procedure]
		if !ok {
			return nil, fmt.Errorf("manifest: %s is declared in the service descriptor but has no reviewed rule entry (handwritten/unreviewed route)", procedure)
		}
		if r.owner == "" {
			return nil, fmt.Errorf("manifest: %s has no owner", procedure)
		}
		if !r.disposition.Valid() {
			return nil, fmt.Errorf("manifest: %s has an invalid disposition %q", procedure, r.disposition)
		}
		if !r.behavior.Valid() {
			return nil, fmt.Errorf("manifest: %s has an invalid intent behavior %q", procedure, r.behavior)
		}
		if !r.idempotencyClass.Valid() {
			return nil, fmt.Errorf("manifest: %s has an invalid idempotency class %q", procedure, r.idempotencyClass)
		}

		var capRefs []string
		var acceptedRefs []string
		switch {
		case r.genericLifecycle:
			capRefs = p1aCaps
			if r.disposition == DispositionServed {
				acceptedRefs = p1aIntents
			}
		case r.acceptedAll:
			capRefs = sortUniqueStrings(r.capabilityRefs)
			acceptedRefs = allIntents
		default:
			capRefs = sortUniqueStrings(r.capabilityRefs)
		}
		if len(capRefs) == 0 {
			return nil, fmt.Errorf("manifest: %s is bound to zero capabilities (unbound route)", procedure)
		}

		required, ok := RequiredFieldPaths(procedure)
		if !ok {
			return nil, fmt.Errorf("manifest: %s has no request presence rule entry", procedure)
		}

		hedging := "NOT_APPLICABLE_P1A"
		fieldMask := "NOT_APPLICABLE"
		tenantScope := "REQUEST_SCOPE_CONTEXT"
		purposePolicy := "purpose.scope_context_check/v1"
		rateBudget := "budget.standard.v1"
		evidencePolicy := "intent_lifecycle_evidence/v1"
		if r.owner == "REGISTRY" {
			evidencePolicy = "registry_read_audit/v1"
		}

		out = append(out, EndpointDefinition{
			EndpointID:                   d.EndpointID(),
			ServiceFullName:              d.ServiceFullName,
			MethodName:                   d.MethodName,
			OwnerDomain:                  r.owner,
			CapabilityRefs:               capRefs,
			IntentBehavior:               r.behavior,
			AcceptedIntentDefinitionRefs: acceptedRefs,
			RequestType:                  d.RequestType,
			ResponseType:                 d.ResponseType,
			ErrorCodes:                   errCodes,
			GRPCProcedure:                procedure,
			HTTPMethod:                   r.httpMethod,
			HTTPPathTemplate:             r.httpPath,
			HTTPBodyBinding:              r.httpBody,
			Disposition:                  r.disposition,
			DispositionReason:            r.dispositionReason,
			AuthnAssurance:               authnAssuranceSubstantial,
			AuthzAction:                  r.authzAction,
			PurposePolicyRef:             purposePolicy,
			TenantScopeDerivation:        tenantScope,
			ClassificationRef:            r.classificationRef,
			RequiredFieldPaths:           required,
			IdempotencyClass:             r.idempotencyClass,
			IdempotencyKeySource:         r.idempotencyKeySrc,
			RevisionPolicy:               r.revisionPolicy,
			DeadlineBudgetMillis:         interactiveReadDeadlineMillis,
			RetryPolicy:                  r.retryPolicy,
			HedgingPolicy:                hedging,
			PaginationPolicy:             r.paginationPolicy,
			FieldMaskPolicy:              fieldMask,
			OrderingPolicy:               r.orderingPolicy,
			RateBudgetRef:                rateBudget,
			EvidencePolicyRef:            evidencePolicy,
			CompatibilityStatus:          r.compatibilityStatus,
			Phase:                        r.phase,
		})
	}

	for procedure := range ruleTable {
		if !seen[procedure] {
			return nil, fmt.Errorf("manifest: rule table names %s, which no longer exists in the service descriptor (stale manifest row)", procedure)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].EndpointID < out[j].EndpointID })
	return &EndpointManifest{SchemaVersion: 1, Endpoints: out}, nil
}
