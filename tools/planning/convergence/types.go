// Package convergence is CLOSE-002's single convergence gate. It runs the
// design, slice and implementation gap compilers that already exist
// (designclosure, intentcoverage, closurewitness, workflowmaturity and
// selectionbind) over the concrete selection facts NEXT-002's signed P1A
// manifest binds, and folds their findings into one register of gaps.
//
// A gap's identity is its semantic owner, contract class and structural
// subject, never the wording a compiler used: two compilers reporting the
// same missing test for the same definition in different sentences are one
// gap with two observations. Every gap whose owner is inside the selected
// scope must resolve to an open existing todo that claims it, a proposed new
// atomic todo carrying owner, phase and oracle, or an explicit rejection
// with a rationale and a decider. Gaps whose owner cannot be placed inside
// or outside the selection, compiler inputs that could not be sourced,
// unfilled selection slots and unmapped finding codes are reported as
// unknowns rather than dropped. A selected fact that no downstream slice,
// model, API, threat or test consumes is itself a selected-scope gap.
//
// The fixed-point property is identity stability, not an empty register:
// [Converge] is a pure function of its [Snapshot], a second pass over an
// unchanged snapshot is byte-identical, and adopting every proposed todo and
// re-running produces no new gap identity and no second proposal for the
// same gap. Today's placeholder selections produce many gaps; that is the
// honest output.
//
// The package is kernel-pure apart from loader.go: no clock (the as-of date
// is an explicit input), no network and no mutable package state.
package convergence

// SchemaVersion is the convergence report's own format version.
const SchemaVersion = 1

// Result values. RESOLVED requires every selected-scope gap to be resolved,
// zero unknowns and zero structural findings.
const (
	ResultResolved   = "RESOLVED"
	ResultUnresolved = "UNRESOLVED"
)

// Compiler names recorded on observations.
const (
	CompilerDesignClosure    = "designclosure"
	CompilerIntentCoverage   = "intentcoverage"
	CompilerClosureWitness   = "closurewitness"
	CompilerWorkflowMaturity = "workflowmaturity"
	CompilerSelectionBind    = "selectionbind"
	CompilerFacts            = "convergence.facts"
)

// Contract is one closed contract class a gap is about.
type Contract string

// The contract vocabulary. Adapters map every compiler finding code onto
// exactly one of these; a code they cannot map carries an empty contract and
// surfaces as an UNMAPPED_FINDING_CODE unknown.
const (
	ContractSource             Contract = "SOURCE"
	ContractOwner              Contract = "OWNER"
	ContractPhaseGate          Contract = "PHASE_GATE"
	ContractSlice              Contract = "SLICE"
	ContractModel              Contract = "MODEL"
	ContractProperty           Contract = "PROPERTY"
	ContractGovernance         Contract = "GOVERNANCE"
	ContractEngine             Contract = "ENGINE"
	ContractCapability         Contract = "CAPABILITY"
	ContractHandler            Contract = "HANDLER"
	ContractEndpoint           Contract = "ENDPOINT"
	ContractWorkflowDesign     Contract = "WORKFLOW_DESIGN"
	ContractScenario           Contract = "SCENARIO"
	ContractDecision           Contract = "DECISION"
	ContractTest               Contract = "TEST"
	ContractTodo               Contract = "TODO"
	ContractEvidence           Contract = "EVIDENCE"
	ContractSelectionGate      Contract = "SELECTION_GATE"
	ContractDownstreamConsumer Contract = "DOWNSTREAM_CONSUMER"
)

// Contracts returns the contract vocabulary in declaration order.
func Contracts() []Contract {
	return []Contract{
		ContractSource, ContractOwner, ContractPhaseGate, ContractSlice,
		ContractModel, ContractProperty, ContractGovernance, ContractEngine,
		ContractCapability, ContractHandler, ContractEndpoint,
		ContractWorkflowDesign, ContractScenario, ContractDecision,
		ContractTest, ContractTodo, ContractEvidence, ContractSelectionGate,
		ContractDownstreamConsumer,
	}
}

// Valid reports whether c belongs to the vocabulary.
func (c Contract) Valid() bool {
	for _, known := range Contracts() {
		if known == c {
			return true
		}
	}
	return false
}

// Scope places a gap relative to the concrete selection.
type Scope string

// Scopes. UNKNOWN is never silently treated as out of scope.
const (
	ScopeSelected       Scope = "SELECTED"
	ScopeOutOfSelection Scope = "OUT_OF_SELECTION"
	ScopeUnknown        Scope = "UNKNOWN"
)

// ResolutionKind is how a gap is closed.
type ResolutionKind string

// Resolution kinds. NOT_REQUIRED applies only to out-of-selection gaps.
const (
	ResolutionExistingTodo ResolutionKind = "EXISTING_TODO"
	ResolutionProposedTodo ResolutionKind = "PROPOSED_TODO"
	ResolutionRejected     ResolutionKind = "REJECTED"
	ResolutionNotRequired  ResolutionKind = "NOT_REQUIRED"
	ResolutionUnresolved   ResolutionKind = "UNRESOLVED"
)

// Selected owner kinds.
const (
	OwnerKindIntent    = "INTENT"
	OwnerKindSelection = "SELECTION"
)

// Consumer layers a selected fact may change. IMPLEMENTATION is product
// source outside the transport and test layers.
const (
	LayerSlice          = "SLICE"
	LayerModel          = "MODEL"
	LayerAPI            = "API"
	LayerThreat         = "THREAT"
	LayerTest           = "TEST"
	LayerImplementation = "IMPLEMENTATION"
)

func validLayer(layer string) bool {
	switch layer {
	case LayerSlice, LayerModel, LayerAPI, LayerThreat, LayerTest, LayerImplementation:
		return true
	}
	return false
}

// Unknown codes.
const (
	UnknownOwnerless       = "OWNERLESS_GAP"
	UnknownUnscopedOwner   = "UNSCOPED_OWNER"
	UnknownUnmappedCode    = "UNMAPPED_FINDING_CODE"
	UnknownMalformedOwner  = "MALFORMED_OWNER"
	UnknownInputUnsourced  = "COMPILER_INPUT_UNSOURCED"
	UnknownSlotUnfilled    = "SELECTION_SLOT_UNFILLED"
	UnknownDuplicateFact   = "DUPLICATE_FACT"
	UnknownInvalidConsumer = "INVALID_CONSUMER_LAYER"
)

// Finding codes for structural defects in resolution inputs.
const (
	FindingInvalidRejection   = "INVALID_REJECTION"
	FindingStaleRejection     = "STALE_REJECTION"
	FindingClaimTodoMissing   = "CLAIM_TODO_MISSING"
	FindingClaimTodoClosed    = "CLAIM_TODO_NOT_OPEN"
	FindingAmbiguousClaim     = "AMBIGUOUS_CLAIM"
	FindingProposalIncomplete = "PROPOSAL_INCOMPLETE"
	FindingProposalCollision  = "PROPOSAL_ID_COLLISION"
)

// InertFactCode is the observation code for a selected fact nothing
// downstream consumes.
const InertFactCode = "INERT_SELECTED_FACT"

// Observation is one compiler finding reduced to owner, contract and
// subject. Detail is the compiler's own wording and never part of identity.
type Observation struct {
	Compiler string   `json:"compiler"`
	Code     string   `json:"code"`
	Owner    string   `json:"-"`
	Contract Contract `json:"-"`
	Subject  string   `json:"-"`
	Detail   string   `json:"detail"`
}

// SelectedOwner is one owner inside the concrete selection: a P1A intent
// or a selecting todo, with the phase a proposed todo for it belongs to.
type SelectedOwner struct {
	Owner string
	Kind  string
	Phase string
}

// Fact is one concrete selected fact and the exact tokens a downstream
// consumer would have to carry to be changed by it.
type Fact struct {
	ID     string
	Kind   string
	Owner  string
	Tokens []string
}

// ConsumerRef is one downstream artifact and the fact tokens it carries.
type ConsumerRef struct {
	Layer  string
	Ref    string
	Tokens []string
}

// TodoRef is one backlog todo's lifecycle state.
type TodoRef struct {
	ID      string
	Phase   string
	Done    bool
	Retired bool
}

// Claim records that a todo owns a gap key. An empty Subject claims every
// subject of the owner and contract.
type Claim struct {
	TodoID   string
	Owner    string
	Contract Contract
	Subject  string
	Source   string
}

// Rejection is an explicit decision not to close a gap key.
type Rejection struct {
	Owner     string
	Contract  Contract
	Subject   string
	Rationale string
	DecidedBy string
}

// Snapshot is the complete typed input [Converge] reads. No field carries a
// resolution or a result.
type Snapshot struct {
	Selected     []SelectedOwner
	KnownOwners  []string
	Facts        []Fact
	Consumers    []ConsumerRef
	Observations []Observation
	Todos        []TodoRef
	Claims       []Claim
	Rejections   []Rejection
	Unknowns     []Unknown
}

// Unknown is one selected-scope question the gate cannot answer.
type Unknown struct {
	Code   string `json:"code"`
	Ref    string `json:"ref"`
	Detail string `json:"detail"`
}

// Finding is one structural defect in the resolution inputs.
type Finding struct {
	Code   string `json:"code"`
	Ref    string `json:"ref"`
	Detail string `json:"detail"`
}

// Resolution is how one gap is closed.
type Resolution struct {
	Kind      ResolutionKind `json:"kind"`
	TodoID    string         `json:"todo_id,omitempty"`
	Rationale string         `json:"rationale,omitempty"`
	DecidedBy string         `json:"decided_by,omitempty"`
}

// Gap is one deduplicated gap identity.
type Gap struct {
	Identity     string        `json:"identity"`
	Owner        string        `json:"owner"`
	Contract     Contract      `json:"contract"`
	Subject      string        `json:"subject,omitempty"`
	Scope        Scope         `json:"scope"`
	Compilers    []string      `json:"compilers"`
	Observations []Observation `json:"observations"`
	Resolution   Resolution    `json:"resolution"`
}

// ProposedTodo is a new atomic todo the gate proposes for one gap. It is
// structured output for the orchestrator; the gate never writes the backlog.
type ProposedTodo struct {
	ID          string   `json:"id"`
	GapIdentity string   `json:"gap_identity"`
	Title       string   `json:"title"`
	Owner       string   `json:"owner"`
	Contract    Contract `json:"contract"`
	Subject     string   `json:"subject,omitempty"`
	Phase       string   `json:"phase"`
	Oracle      string   `json:"oracle"`
}

// ConsumerHit is one downstream consumer of a fact.
type ConsumerHit struct {
	Layer string `json:"layer"`
	Ref   string `json:"ref"`
}

// FactResult is one selected fact with its downstream consumers.
type FactResult struct {
	ID        string        `json:"id"`
	Kind      string        `json:"kind"`
	Owner     string        `json:"owner"`
	Consumers []ConsumerHit `json:"consumers"`
	Inert     bool          `json:"inert"`
}

// Totals reconcile the report.
type Totals struct {
	Observations       int                    `json:"observations"`
	Gaps               int                    `json:"gaps"`
	Deduplicated       int                    `json:"deduplicated_observations"`
	ByScope            map[Scope]int          `json:"by_scope"`
	ByResolution       map[ResolutionKind]int `json:"by_resolution"`
	SelectedUnresolved int                    `json:"selected_unresolved"`
	Facts              int                    `json:"facts"`
	InertFacts         int                    `json:"inert_facts"`
	Proposals          int                    `json:"proposals"`
	Unknowns           int                    `json:"unknowns"`
	Findings           int                    `json:"findings"`
}

// Report is the compiled convergence register.
type Report struct {
	SchemaVersion int            `json:"schema_version"`
	Result        string         `json:"result"`
	Totals        Totals         `json:"totals"`
	Facts         []FactResult   `json:"facts"`
	Gaps          []Gap          `json:"gaps"`
	Proposals     []ProposedTodo `json:"proposals"`
	Unknowns      []Unknown      `json:"unknowns"`
	Findings      []Finding      `json:"findings"`
	Digest        string         `json:"digest"`
}
