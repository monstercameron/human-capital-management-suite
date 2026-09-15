package workflow

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrCompile is the sentinel every compilation failure unwraps to. Classify
// with [errors.Is] and read [Error.Code] for the exact reason; never match
// message text.
var ErrCompile = errors.New("workflow: compilation rejected")

// Stable diagnostic codes. A transport, a publication pipeline or a test
// matches these; they never change spelling once published.
const (
	// CodeTypeMismatch reports an input mapping, edge or schema binding whose
	// declared types are not assignable (WF-COMP-001 RED).
	CodeTypeMismatch = "TYPE_MISMATCH"
	// CodeUnresolvedRef reports a missing, unknown or retired reference:
	// schema, capability, node, route, context path or mapping target
	// (WF-COMP-001 RED).
	CodeUnresolvedRef = "UNRESOLVED_REF"

	// CodeRetiredReference reports a schema, rule, resolver, timeout-policy or
	// compensation reference whose published target is retired (WF-COMP-007).
	CodeRetiredReference = "RETIRED_REFERENCE"
	// CodeReferenceResolverRequired reports a definition declaring a
	// reference kind that only a [ReferenceResolver] can check, compiled
	// without one (WF-COMP-007). It is never silently accepted.
	CodeReferenceResolverRequired = "REFERENCE_RESOLVER_REQUIRED"

	// CodeInvalidDefinition reports a structurally malformed definition:
	// blank identity, duplicate node id, unknown step type.
	CodeInvalidDefinition = "INVALID_DEFINITION"
	// CodePhaseNotImplemented reports a node whose step type is not
	// implemented in the compiler's declared phase.
	CodePhaseNotImplemented = "PHASE_NOT_IMPLEMENTED"

	// CodeUnreachableNode reports a node no path from the start node reaches.
	CodeUnreachableNode = "UNREACHABLE_NODE"
	// CodeImplicitFirstEdge reports an outgoing edge with no explicit route
	// key. There is no implicit "first edge".
	CodeImplicitFirstEdge = "IMPLICIT_FIRST_EDGE"
	// CodeMissingRoute reports an outcome of a node with no explicit edge,
	// including a missing UNKNOWN or default route.
	CodeMissingRoute = "MISSING_ROUTE"
	// CodeDuplicateRoute reports two outgoing edges claiming one route key.
	CodeDuplicateRoute = "DUPLICATE_ROUTE"
	// CodeUnknownRoute reports an edge whose route key is not an outcome the
	// source node can produce.
	CodeUnknownRoute = "UNKNOWN_ROUTE"
	// CodeInvalidTerminalPath reports a node that cannot reach an END, or an
	// END with outgoing edges.
	CodeInvalidTerminalPath = "INVALID_TERMINAL_PATH"
	// CodeUndeclaredCycle reports a cycle with no declared, guarded bound.
	CodeUndeclaredCycle = "UNDECLARED_CYCLE"
	// CodeUnboundedFanout reports missing or exceeded fan-out/depth limits.
	CodeUnboundedFanout = "UNBOUNDED_FANOUT"
	// CodeSourceNotPredecessor reports a mapping reading a node's output that
	// does not run before the reader on every path.
	CodeSourceNotPredecessor = "SOURCE_NOT_PREDECESSOR"
	// CodeDuplicateMapping reports two mappings writing one input field.
	CodeDuplicateMapping = "DUPLICATE_MAPPING"

	// CodeNonIdempotentRetry reports a retried mutation with no compatible
	// idempotency policy and stable effect key (WF-COMP-003 RED).
	CodeNonIdempotentRetry = "NON_IDEMPOTENT_RETRY"
	// CodeUnobservedEffect reports an external or irreversible effect with no
	// observation, failure or repair route (WF-COMP-003 RED).
	CodeUnobservedEffect = "UNOBSERVED_EFFECT"
	// CodeMutationInSimulation reports a plan claiming SIMULATE support while
	// containing a node whose effect class cannot be suppressed.
	CodeMutationInSimulation = "MUTATION_IN_SIMULATION"
	// CodeEffectDeclarationConflict reports a node declaring an effect class
	// its capability manifest contradicts. The manifest is the source of
	// effect truth.
	CodeEffectDeclarationConflict = "EFFECT_DECLARATION_CONFLICT"
	// CodeWriteEffectRefusedP1A reports a write effect in a plan compiled
	// under the zero-effect P1A requirement.
	CodeWriteEffectRefusedP1A = "WRITE_EFFECT_REFUSED_P1A"

	// CodeConflictingWriteSet reports two concurrent branches of one PARALLEL
	// whose declared write sets intersect, by logical effect key or by data
	// domain (WF-COMP-004 RED).
	CodeConflictingWriteSet = "CONFLICTING_WRITE_SET"
	// CodeUnaccountedBranchEffect reports a concurrent branch that declares a
	// write and reaches no JOIN, so no join contract reconciles that effect
	// with its siblings (WF-COMP-004 RED).
	CodeUnaccountedBranchEffect = "UNACCOUNTED_BRANCH_EFFECT"
	// CodeUnsafeCheckpoint reports an author-requested safe point strictly
	// inside an atomic region, where an intervention would abandon an effect
	// no OBSERVE has confirmed (WF-COMP-004 RED).
	CodeUnsafeCheckpoint = "UNSAFE_CHECKPOINT"

	// CodeMissingGovernanceEvaluation reports a node invoking a capability
	// without the AuthZ, legal, purpose and risk evaluations (WF-COMP-005).
	CodeMissingGovernanceEvaluation = "MISSING_GOVERNANCE_EVALUATION"
	// CodeUnresolvedApprovalScope reports an approval requirement that cannot
	// resolve within the declared scope.
	CodeUnresolvedApprovalScope = "UNRESOLVED_APPROVAL_SCOPE"
	// CodeUnvalidatedAgentOutput reports agent-eligible capability output
	// flowing into an effect without typed output validation.
	CodeUnvalidatedAgentOutput = "UNVALIDATED_AGENT_OUTPUT"
	// CodeUnresolvedObligation reports an obligation reference with no
	// declared insertion point, owner or satisfaction condition.
	CodeUnresolvedObligation = "UNRESOLVED_OBLIGATION"

	// CodeUnauthorizedScope reports a node whose declared authority scope does
	// not cover the capability's required AuthZ scope (WF-STEP-001 RED).
	CodeUnauthorizedScope = "UNAUTHORIZED_SCOPE"
	// CodeUnrestrictedResultCopy reports a mapping copying a whole capability
	// result instead of a declared typed field (WF-STEP-001 REFACTOR).
	CodeUnrestrictedResultCopy = "UNRESTRICTED_RESULT_COPY"
	// CodeMutableDecisionInput reports a DECISION reading unpinned or mutable
	// state instead of a snapshot (WF-STEP-002 RED).
	CodeMutableDecisionInput = "MUTABLE_DECISION_INPUT"
	// CodeNonExclusiveRoutes reports DECISION routes that overlap with no
	// declared precedence (WF-STEP-002 RED).
	CodeNonExclusiveRoutes = "NONEXCLUSIVE_ROUTES"
	// CodeArbitraryCode reports a TRANSFORM carrying inline customer code.
	CodeArbitraryCode = "ARBITRARY_CODE"
	// CodeNondeterministicTransform reports a TRANSFORM reading the clock,
	// randomness or the network (WF-STEP-010 RED).
	CodeNondeterministicTransform = "NONDETERMINISTIC_TRANSFORM"
	// CodeUnpinnedLookup reports a TRANSFORM lookup with no pinned snapshot.
	CodeUnpinnedLookup = "UNPINNED_LOOKUP"
	// CodeResourceLimitExceeded reports a TRANSFORM whose declared resource
	// bounds are absent or above the compiler ceiling.
	CodeResourceLimitExceeded = "RESOURCE_LIMIT_EXCEEDED"
	// CodeUnsatisfiedSanitizer reports tainted input reaching an untainted
	// output with no sanitizer receipt (WF-STEP-010 RED).
	CodeUnsatisfiedSanitizer = "UNSATISFIED_SANITIZER"
	// CodeReceiptIsNotObservation reports an OBSERVE accepting a submission
	// receipt or transport acknowledgement as observed business state.
	CodeReceiptIsNotObservation = "RECEIPT_IS_NOT_OBSERVATION"
	// CodeStaleObservationAccepted reports an OBSERVE with no freshness
	// policy or required source watermark.
	CodeStaleObservationAccepted = "STALE_OBSERVATION_ACCEPTED"
	// CodeDegradedCollapsedToPass reports an UNKNOWN or PARTIAL observation
	// routed to the same continuation as PASS.
	CodeDegradedCollapsedToPass = "DEGRADED_COLLAPSED_TO_PASS"
	// CodeRetryExhaustionFalseCompletion reports retry exhaustion that does
	// not reach a degraded or repair terminal (WF-STEP-014 REFACTOR).
	CodeRetryExhaustionFalseCompletion = "RETRY_EXHAUSTION_FALSE_COMPLETION"

	// CodeMissingDimension reports an END that does not record all five intent
	// lifecycle dimensions.
	CodeMissingDimension = "MISSING_DIMENSION"
	// CodeSixthDimension reports an END writing a dimension outside the fixed
	// five (WF-STEP-017 RED).
	CodeSixthDimension = "SIXTH_DIMENSION"
	// CodeIllegalTerminalTuple reports a terminal tuple the fixed kernel
	// legality rules reject.
	CodeIllegalTerminalTuple = "ILLEGAL_TERMINAL_TUPLE"
	// CodeTerminalNotInProfile reports a terminal outside the workflow's
	// declared terminal profile.
	CodeTerminalNotInProfile = "TERMINAL_NOT_IN_PROFILE"
	// CodeObligationCollapsed reports an END claiming discharged obligations
	// while outstanding obligations remain (WF-STEP-017 RED).
	CodeObligationCollapsed = "OBLIGATION_COLLAPSED"
	// CodeDegradedCollapsedToSuccess reports an END reachable only through a
	// degraded observation that nevertheless claims consistency.
	CodeDegradedCollapsedToSuccess = "DEGRADED_COLLAPSED_TO_SUCCESS"
)

// Location identifies exactly where a diagnostic was found. Empty components
// are omitted from the rendered form, so a graph diagnostic names its node and
// edge while a mapping diagnostic names its field path (WF-COMP-002 REFACTOR).
type Location struct {
	NodeID   string
	EdgeFrom string
	EdgeTo   string
	RouteKey string
	Field    string
	Ref      string
}

func (l Location) String() string {
	var parts []string
	if l.NodeID != "" {
		parts = append(parts, "node="+l.NodeID)
	}
	if l.EdgeFrom != "" || l.EdgeTo != "" {
		parts = append(parts, fmt.Sprintf("edge=%s->%s", l.EdgeFrom, l.EdgeTo))
	}
	if l.RouteKey != "" {
		parts = append(parts, "route="+l.RouteKey)
	}
	if l.Field != "" {
		parts = append(parts, "field="+l.Field)
	}
	if l.Ref != "" {
		parts = append(parts, "ref="+l.Ref)
	}
	return strings.Join(parts, " ")
}

// Error is one compile diagnostic: a stable code, the source location and a
// human-readable detail. Detail is for a person; Code is for a program.
type Error struct {
	Code     string
	Location Location
	Detail   string
}

func (e Error) Error() string {
	loc := e.Location.String()
	if loc == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Detail)
	}
	return fmt.Sprintf("%s [%s]: %s", e.Code, loc, e.Detail)
}

// Diagnostics is the complete diagnostic set one compilation produced. A
// compiler reports everything it found rather than stopping at the first
// problem, so one publication attempt tells an author the whole story.
type Diagnostics struct {
	Errors []Error
}

func (d *Diagnostics) Error() string {
	parts := make([]string, 0, len(d.Errors))
	for _, e := range d.Errors {
		parts = append(parts, e.Error())
	}
	return ErrCompile.Error() + ": " + strings.Join(parts, "; ")
}

// Unwrap exposes the sentinel cause to [errors.Is].
func (d *Diagnostics) Unwrap() error { return ErrCompile }

// Has reports whether the set contains at least one diagnostic with code.
func (d *Diagnostics) Has(code string) bool {
	for _, e := range d.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
}

// HasAt reports whether the set contains a diagnostic with code at nodeID.
func (d *Diagnostics) HasAt(code, nodeID string) bool {
	for _, e := range d.Errors {
		if e.Code == code && e.Location.NodeID == nodeID {
			return true
		}
	}
	return false
}

// Codes lists the distinct codes present, sorted, so a test can assert the
// complete diagnostic set rather than only the code it expected.
func (d *Diagnostics) Codes() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(d.Errors))
	for _, e := range d.Errors {
		if !seen[e.Code] {
			seen[e.Code] = true
			out = append(out, e.Code)
		}
	}
	sort.Strings(out)
	return out
}

// collector accumulates diagnostics during one compilation.
type collector struct {
	errs []Error
}

func (c *collector) add(code string, loc Location, format string, args ...any) {
	c.errs = append(c.errs, Error{Code: code, Location: loc, Detail: fmt.Sprintf(format, args...)})
}

// result returns the accumulated diagnostics in a deterministic order: by node
// id, then code, then detail. Compiling the same definition twice reports the
// same list in the same order.
func (c *collector) result() *Diagnostics {
	if len(c.errs) == 0 {
		return nil
	}
	out := make([]Error, len(c.errs))
	copy(out, c.errs)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Location.NodeID != b.Location.NodeID {
			return a.Location.NodeID < b.Location.NodeID
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Location.Field != b.Location.Field {
			return a.Location.Field < b.Location.Field
		}
		return a.Detail < b.Detail
	})
	return &Diagnostics{Errors: out}
}

// ErrorCode reports the error's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e Error) ErrorCode() string { return e.Code }
