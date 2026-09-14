// Package closurewitness emits SLICE-016's canonical per-intent closure
// witness: exactly one digest-backed witness per source-bound intent
// definition, carrying every forward and reverse edge of the chain
// source -> slice -> model/engine/capability -> handler -> endpoint ->
// scenario/test/todo/evidence, together with the definition's phase and
// gate, its endpoint disposition, its test and evidence outputs and the
// expiry of every exception it leans on.
//
// Witnesses are never asserted by hand. [Compile] is a pure function of a
// [Snapshot] whose every row is read from an existing registry by
// [LoadSnapshot]; there is no input field that carries a result, a state
// or a maturity, so a forged "complete" claim has nowhere to enter. Any
// orphan, duplicate, stale, aggregate-only or absent edge yields
// SLICE_CLOSURE_INCOMPLETE naming the exact edge identity, and a registry
// the loader cannot find is reported as an UNSOURCED edge class rather
// than silently treated as empty.
//
// The package is kernel-pure apart from loader.go: no clock (the as-of
// date is an explicit input), no network, no mutable package state.
package closurewitness

// SchemaVersion is the witness report's own format version.
const SchemaVersion = 1

// Result values. A witness or report is COMPLETE only when it carries no
// defect at all; everything else is SLICE_CLOSURE_INCOMPLETE.
const (
	ResultComplete   = "COMPLETE"
	ResultIncomplete = "SLICE_CLOSURE_INCOMPLETE"
)

// EdgeClass names one link class of the closure chain.
type EdgeClass string

// The twelve edge classes, in chain order.
const (
	ClassSource     EdgeClass = "SOURCE"
	ClassPhaseGate  EdgeClass = "PHASE_GATE"
	ClassSlice      EdgeClass = "SLICE"
	ClassModel      EdgeClass = "MODEL"
	ClassEngine     EdgeClass = "ENGINE"
	ClassCapability EdgeClass = "CAPABILITY"
	ClassHandler    EdgeClass = "HANDLER"
	ClassEndpoint   EdgeClass = "ENDPOINT"
	ClassScenario   EdgeClass = "SCENARIO"
	ClassTest       EdgeClass = "TEST"
	ClassTodo       EdgeClass = "TODO"
	ClassEvidence   EdgeClass = "EVIDENCE"
)

// Classes returns every edge class in chain order.
func Classes() []EdgeClass {
	return []EdgeClass{
		ClassSource, ClassPhaseGate, ClassSlice, ClassModel, ClassEngine,
		ClassCapability, ClassHandler, ClassEndpoint, ClassScenario,
		ClassTest, ClassTodo, ClassEvidence,
	}
}

// Valid reports whether c is one of the twelve declared classes.
func (c EdgeClass) Valid() bool { return classRank(c) >= 0 }

func classRank(c EdgeClass) int {
	for i, known := range Classes() {
		if known == c {
			return i
		}
	}
	return -1
}

// EdgeState is the closure state of one edge class on one witness.
type EdgeState string

// Edge states. BOUND and JUSTIFIED_ABSENT carry no defect; every other
// state is backed by at least one [Defect].
const (
	StateBound         EdgeState = "BOUND"
	StateJustified     EdgeState = "JUSTIFIED_ABSENT"
	StateAbsent        EdgeState = "ABSENT"
	StateUnsourced     EdgeState = "UNSOURCED"
	StateDuplicate     EdgeState = "DUPLICATE"
	StateStale         EdgeState = "STALE"
	StateAggregateOnly EdgeState = "AGGREGATE_ONLY"
)

// Direction says which side of an edge asserted it: FORWARD edges are
// declared by the definition-side registry, REVERSE edges by the
// target-side registry pointing back at the definition.
type Direction string

// Edge directions.
const (
	Forward Direction = "FORWARD"
	Reverse Direction = "REVERSE"
)

// Defect codes. Every defect's Result is SLICE_CLOSURE_INCOMPLETE.
const (
	DefectAbsent            = "EDGE_ABSENT"
	DefectReverseAbsent     = "REVERSE_EDGE_ABSENT"
	DefectDuplicate         = "EDGE_DUPLICATE"
	DefectStale             = "EDGE_STALE"
	DefectAggregateOnly     = "EDGE_AGGREGATE_ONLY"
	DefectOrphan            = "EDGE_ORPHAN"
	DefectUnsourced         = "EDGE_CLASS_UNSOURCED"
	DefectMaturityOverclaim = "MATURITY_ADVANCED_WITHOUT_CLOSURE"
)

// defectState maps a defect code to the edge state it forces.
func defectState(code string) EdgeState {
	switch code {
	case DefectAbsent, DefectReverseAbsent:
		return StateAbsent
	case DefectDuplicate:
		return StateDuplicate
	case DefectAggregateOnly:
		return StateAggregateOnly
	case DefectUnsourced:
		return StateUnsourced
	default:
		return StateStale
	}
}

// stateRank orders states worst-first so a class with several defects
// reports one deterministic state.
func stateRank(s EdgeState) int {
	switch s {
	case StateAbsent:
		return 0
	case StateUnsourced:
		return 1
	case StateDuplicate:
		return 2
	case StateStale:
		return 3
	case StateAggregateOnly:
		return 4
	case StateJustified:
		return 5
	default:
		return 6
	}
}

// Edge is one exact link a registry asserts.
type Edge struct {
	Class     EdgeClass `json:"class"`
	Direction Direction `json:"direction"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Registry  string    `json:"registry"`
}

// Defect is one reason a witness or the report is incomplete. Identity is
// the exact, stable edge identity a reviewer repairs.
type Defect struct {
	Result   string    `json:"result"`
	Code     string    `json:"code"`
	Class    EdgeClass `json:"class"`
	Identity string    `json:"identity"`
	Detail   string    `json:"detail"`
}

// ClassStatus is one edge class's state on a witness.
type ClassStatus struct {
	Class EdgeClass `json:"class"`
	State EdgeState `json:"state"`
}

// SourceBinding pins the authoritative source row a witness is bound to.
type SourceBinding struct {
	Registry  string `json:"registry"`
	RowDigest string `json:"row_digest"`
}

// PhaseBinding carries the definition's phase and gate as every registry
// states them, plus the maturity the coverage registry claims.
type PhaseBinding struct {
	DescriptorPhase    string `json:"descriptor_phase"`
	Release            string `json:"release"`
	CeilingGate        string `json:"ceiling_gate"`
	CeilingDisposition string `json:"ceiling_disposition"`
	ClaimedMaturity    string `json:"claimed_maturity"`
}

// EndpointDisposition is the ENDPOINT-009 disposition of the definition.
type EndpointDisposition struct {
	Category         string   `json:"category"`
	Justification    string   `json:"justification"`
	ServingEndpoints []string `json:"serving_endpoints"`
}

// EvidenceRef is one bound todo's evidence output.
type EvidenceRef struct {
	Todo   string   `json:"todo"`
	Digest string   `json:"digest"`
	Dates  []string `json:"dates"`
	Tests  []string `json:"tests"`
	Expiry string   `json:"expiry"`
}

// Witness is the one closure witness for one source-bound definition.
type Witness struct {
	Definition          string              `json:"definition"`
	DisplayName         string              `json:"display_name"`
	Source              SourceBinding       `json:"source"`
	Phase               PhaseBinding        `json:"phase"`
	EndpointDisposition EndpointDisposition `json:"endpoint_disposition"`
	Classes             []ClassStatus       `json:"classes"`
	Edges               []Edge              `json:"edges"`
	Tests               []string            `json:"tests"`
	Evidence            []EvidenceRef       `json:"evidence"`
	Expiry              string              `json:"expiry"`
	Result              string              `json:"result"`
	Defects             []Defect            `json:"defects"`
	Digest              string              `json:"digest"`
}

// Totals reconcile the report: every witness is counted once and every
// class state is counted once per witness.
type Totals struct {
	Definitions  int                             `json:"definitions"`
	Complete     int                             `json:"complete"`
	Incomplete   int                             `json:"incomplete"`
	Orphans      int                             `json:"orphans"`
	ByDefectCode map[string]int                  `json:"by_defect_code"`
	ByClassState map[EdgeClass]map[EdgeState]int `json:"by_class_state"`
	Incompletes  []string                        `json:"incomplete_definitions"`
}

// Report is the compiled witness set.
type Report struct {
	SchemaVersion int       `json:"schema_version"`
	AsOf          string    `json:"as_of"`
	Result        string    `json:"result"`
	Totals        Totals    `json:"totals"`
	Witnesses     []Witness `json:"witnesses"`
	Orphans       []Defect  `json:"orphans"`
	Digest        string    `json:"digest"`
}

// NoWaiver is the expiry value of evidence that leans on no waiver.
const NoWaiver = "NO_WAIVER"

// Registry path labels used in edges and defects.
const (
	RegistryDescriptors  = "definitions/governance/intent-conformance-descriptors.yaml"
	RegistryCatalog      = "internal/intent/definitions"
	RegistryCeiling      = "definitions/planning/gates/phase1-scope-ceiling.yaml"
	RegistrySlices       = "definitions/planning/product-slices.yaml"
	RegistryModel        = "internal/intent/modelbinding"
	RegistryEngine       = "tools/policy/enginecoverage"
	RegistryClaims       = "internal/capability/binding.Claims"
	RegistryBootstrap    = "internal/capability.NewBootstrapRegistry"
	RegistryBinding      = "internal/capability/binding.Build"
	RegistryDisposition  = "internal/transport/manifest.BuildDefaultDispositionReport"
	RegistryEndpoints    = "internal/transport/manifest.Build"
	RegistryScenarios    = "tools/planning/scenariomatrix"
	RegistryTodos        = "planning/todos.md"
	RegistryCoverage     = "definitions/planning/capability-coverage.yaml"
	RegistryTestSources  = "cmd|gen|internal|test|tools *_test.go"
	RegistryKnownDefects = "definitions/planning/known-defects.yaml"
)

// Snapshot is the complete typed input [Compile] reads. Every row is
// registry data; none carries a result.
type Snapshot struct {
	// AsOf is the YYYY-MM-DD date evidence waivers are judged against.
	AsOf string

	Descriptors        []DescriptorRow
	Catalog            []CatalogRow
	Ceiling            []CeilingRow
	Slices             []SliceRow
	ModelBindings      []ModelBindingRow
	ModelGaps          []ModelGapRow
	Engines            []EngineRow
	Capabilities       []CapabilityRow
	Claims             []ClaimRow
	BindingEntries     []BindingEntryRow
	BindingGaps        []BindingGapRow
	IntentDispositions []IntentDispositionRow
	Endpoints          []EndpointRow
	Scenarios          []ScenarioRow
	Todos              []TodoRow
	TodoClaims         []TodoClaimRow
	ContextTokens      []ContextTokenRow
	Coverage           []CoverageRow
	TestExists         map[string]bool
	Waivers            []WaiverRow

	// Unsourced lists edge classes whose registry could not be found.
	Unsourced []EdgeClass
}

// DescriptorRow is one accepted intent-conformance descriptor.
type DescriptorRow struct {
	Definition  string
	DisplayName string
	Phase       string
	RowDigest   string
}

// CatalogRow is one compiled Go intent definition.
type CatalogRow struct {
	Definition  string
	DisplayName string
	Release     string
}

// CeilingRow is one PHASE-001 scope-ceiling intent row.
type CeilingRow struct {
	Definition  string
	Gate        string
	Disposition string
}

// SliceRow is one product slice.
type SliceRow struct {
	SliceID        string
	Version        int
	Intents        []string
	Capabilities   []string
	DigestVerified bool
}

// ModelBindingRow is one resolved definition-to-model binding.
type ModelBindingRow struct {
	Definition string
	Entities   []string
}

// ModelGapRow is one unresolved model-binding element.
type ModelGapRow struct {
	Definition string
	Element    string
	Detail     string
}

// EngineRow is one reusable-computation responsibility with the engine
// coverage findings that name it.
type EngineRow struct {
	Intent      string
	Computation string
	Package     string
	Findings    []string
}

// CapabilityRow is one published BOOTSTRAP capability.
type CapabilityRow struct {
	CapabilityID string
	Version      uint32
}

// ClaimRow is one reviewed capability binding claim.
type ClaimRow struct {
	CapabilityID  string
	Version       uint32
	DefinitionRef string
}

// BindingEntryRow is one fully bound capability.
type BindingEntryRow struct {
	CapabilityID string
	Wire         string
	Handler      string
}

// BindingGapRow is one BIND-001 binding gap.
type BindingGapRow struct {
	Kind         string
	CapabilityID string
	Subject      string
	OwnerTodo    string
}

// IntentDispositionRow is one ENDPOINT-009 intent disposition.
type IntentDispositionRow struct {
	Definition       string
	Category         string
	Justification    string
	ServingEndpoints []string
}

// EndpointRow is one endpoint-manifest row.
type EndpointRow struct {
	EndpointID          string
	Disposition         string
	AcceptedDefinitions []string
}

// ScenarioRow is one generated adversarial scenario matrix.
type ScenarioRow struct {
	Definition    string
	ScenarioIDs   []string
	NotApplicable int
	Findings      []string
}

// TodoRow is one backlog todo.
type TodoRow struct {
	ID             string
	Done           bool
	Retired        bool
	PrimaryTest    string
	EvidenceTests  []string
	EvidenceDigest string
	EvidenceDates  []string
}

// TodoClaimRow is one todo's claim on a bare intent id.
type TodoClaimRow struct {
	Intent string
	TodoID string
	Kind   string
}

// ContextTokenRow is one DIRECT or INTENTS token a todo's INTENT CONTEXT
// names.
type ContextTokenRow struct {
	TodoID string
	Field  string
	Token  string
}

// CoverageRow is one checked-in capability-coverage.yaml intent item.
type CoverageRow struct {
	Intent      string
	State       string
	DirectTodos []string
	Tests       []string
}

// WaiverRow is one evidence-freshness waiver.
type WaiverRow struct {
	TodoID string
	Issue  string
	Expiry string
}

// Todo claim kinds, mirroring tools/planning/coveragematrix.
const (
	ClaimDirect = "DIRECT"
	ClaimDomain = "DOMAIN"
)
