package telemetry

import (
	"hash/fnv"
	"math"
	"sort"
	"sync"

	operationstelemetry "github.com/monstercameron/human-capital-management-suite/internal/operations/telemetry"
)

// SinkClass names an export destination category eligible to receive
// signals of a given AttributeClass (platform-architecture-catalog.md
// 9.21.8 "Governed Observability": classify -> redact/hash/drop ->
// destination policy).
type SinkClass string

// Published sink classes.
const (
	SinkMetricsBackend SinkClass = "metrics_backend"
	SinkLogBackend     SinkClass = "log_backend"
	SinkTraceBackend   SinkClass = "trace_backend"
)

// ExportPolicy declares, per AttributeClass, which sinks may receive it.
// PROHIBITED never appears as a key with a non-empty sink list: the zero
// value for an unmapped class is "no sinks", so an evaluator bug that fails
// to look up a class fails closed rather than open.
type ExportPolicy struct {
	Version int
	allowed map[AttributeClass][]SinkClass
}

// NewExportPolicy compiles an export policy. An explicit mapping for
// ClassProhibited with at least one sink is rejected: prohibited content
// must never have an export path, under any policy version.
func NewExportPolicy(version int, allowed map[AttributeClass][]SinkClass) (ExportPolicy, error) {
	if sinks := allowed[ClassProhibited]; len(sinks) > 0 {
		return ExportPolicy{}, ErrProhibitedClassExported
	}
	compiled := make(map[AttributeClass][]SinkClass, len(allowed))
	for class, sinks := range allowed {
		cp := append([]SinkClass(nil), sinks...)
		sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
		compiled[class] = cp
	}
	return ExportPolicy{Version: version, allowed: compiled}, nil
}

// ErrProhibitedClassExported is returned by NewExportPolicy when a caller
// tries to map ClassProhibited to any non-empty sink list.
var ErrProhibitedClassExported = newExportPolicyErr()

func newExportPolicyErr() error {
	return &RejectionError{Code: "OBS_004_REJECTED", Field: "export_policy.PROHIBITED", State: "non_empty_sink_list", Version: 0}
}

// DefaultExportPolicy is the OBS-004 GREEN default: public operational
// attributes may reach every sink, restricted operational attributes never
// reach a metrics backend (they may still appear, bounded, in logs and
// traces), and prohibited content reaches nothing.
func DefaultExportPolicy(version int) ExportPolicy {
	p, err := NewExportPolicy(version, map[AttributeClass][]SinkClass{
		ClassOperationalPublic:     {SinkMetricsBackend, SinkLogBackend, SinkTraceBackend},
		ClassOperationalRestricted: {SinkLogBackend, SinkTraceBackend},
	})
	if err != nil {
		// Unreachable: the literal above never maps ClassProhibited.
		panic(err)
	}
	return p
}

// Allows reports whether sink may receive an attribute of class class. An
// unrecognized class (including the zero value) allows nothing.
func (p ExportPolicy) Allows(class AttributeClass, sink SinkClass) bool {
	for _, s := range p.allowed[class] {
		if s == sink {
			return true
		}
	}
	return false
}

// Sinks returns the sink classes p allows for class, sorted, for
// consistency checks against the published YAML contract.
func (p ExportPolicy) Sinks(class AttributeClass) []SinkClass {
	return append([]SinkClass(nil), p.allowed[class]...)
}

// cardinalityOverflowValue is the bucket every value beyond a key's budget
// collapses into, so cardinality stays bounded no matter how many distinct
// values a caller supplies.
const cardinalityOverflowValue = "__overflow__"

// CardinalityGovernor enforces one bounded-distinct-value budget per
// attribute key, sourced from the key's AttributeDefinition.MaxCardinality.
// It is safe for concurrent use.
type CardinalityGovernor struct {
	allow *Allowlist

	mu   sync.Mutex
	seen map[string]map[string]struct{}
}

// NewCardinalityGovernor builds a governor reading budgets from allow.
func NewCardinalityGovernor(allow *Allowlist) *CardinalityGovernor {
	return &CardinalityGovernor{allow: allow, seen: make(map[string]map[string]struct{})}
}

// Cap returns value unchanged while the key's distinct-value budget has
// room, and the overflow bucket once the budget is exhausted — including
// for every subsequent new value, so cardinality is capped rather than
// merely delayed. A key with no metric budget (MaxCardinality == 0) is
// treated as budget-exhausted immediately: Cap must never be asked to pass
// an unbounded label through to a metric.
func (g *CardinalityGovernor) Cap(key, value string) string {
	def, ok := g.allow.Lookup(key)
	if !ok || def.MaxCardinality <= 0 {
		return cardinalityOverflowValue
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	set, ok := g.seen[key]
	if !ok {
		set = make(map[string]struct{})
		g.seen[key] = set
	}
	if _, already := set[value]; already {
		return value
	}
	if len(set) >= def.MaxCardinality {
		return cardinalityOverflowValue
	}
	set[value] = struct{}{}
	return value
}

// DistinctCount returns how many distinct values Cap has admitted for key
// (excluding the overflow bucket), for tests and diagnostics.
func (g *CardinalityGovernor) DistinctCount(key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.seen[key])
}

// RetentionClass names why a signal must always be retained regardless of
// the success sampling rate (structured-logging-and-opentelemetry.md
// "Sampling").
type RetentionClass string

// Published forced-retention classes. RetentionNone means "no forced
// retention reason"; the sampling rate alone decides.
const (
	RetentionNone                     RetentionClass = ""
	RetentionSecurityDenial           RetentionClass = "security_denial"
	RetentionFinancialMutation        RetentionClass = "financial_mutation"
	RetentionIrreversibleEffect       RetentionClass = "irreversible_effect"
	RetentionAmbiguousResult          RetentionClass = "ambiguous_result"
	RetentionCorrectnessFailure       RetentionClass = "correctness_failure"
	RetentionTelemetryPipelineFailure RetentionClass = "telemetry_pipeline_failure"
)

// SamplingDecision is a versioned, deterministic verdict on whether a
// signal tied to one correlation id is retained.
type SamplingDecision struct {
	Retained      bool
	Reason        string
	PolicyVersion int
	Receipt       operationstelemetry.Receipt
}

// SamplingPolicy is a versioned, deterministic head/tail sampling rule.
// Decide never takes a caller-supplied "force retain"/"force drop" flag: an
// untrusted parent's sampling flag has no parameter to arrive through, so
// it structurally cannot force retention or suppress a mandatory trace
// (OBS-004 RED: "untrusted sampling flags force retention or suppress
// mandatory traces"). Only a server-computed RetentionClass can do that.
type SamplingPolicy struct {
	Version           int
	SuccessSampleRate float64 // [0,1]
}

// Decide returns the sampling verdict for correlationID given the
// caller-computed retention class. A non-empty retention class always
// retains, regardless of SuccessSampleRate or the hash of correlationID.
func (p SamplingPolicy) Decide(correlationID string, retention RetentionClass) SamplingDecision {
	if retention != RetentionNone {
		return SamplingDecision{Retained: true, Reason: "forced_retain_" + string(retention), PolicyVersion: p.Version}
	}
	// Comparing as a float ratio (rather than scaling the rate up into a
	// uint64 threshold) avoids relying on how a Go implementation rounds an
	// out-of-range float64->uint64 conversion at the rate==1 boundary,
	// which the language spec leaves architecture-dependent.
	ratio := float64(sampleHash(correlationID, p.Version)) / float64(math.MaxUint64)
	if ratio <= clamp01(p.SuccessSampleRate) {
		return SamplingDecision{Retained: true, Reason: "sampled_below_rate", PolicyVersion: p.Version}
	}
	return SamplingDecision{Retained: false, Reason: "sampled_above_rate", PolicyVersion: p.Version}
}

func clamp01(f float64) float64 {
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	default:
		return f
	}
}

// sampleHash is deterministic: the same (correlationID, version) pair
// always hashes to the same value, so the same signal is never sampled in
// on one export attempt and out on a retry.
func sampleHash(correlationID string, version int) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte{byte(version), byte(version >> 8), byte(version >> 16), byte(version >> 24)})
	_, _ = h.Write([]byte(correlationID))
	return h.Sum64()
}

// Decision is the exact policy receipt (OBS-004 GREEN: "emits an exact
// policy receipt/drop reason") for one candidate attribute on one signal
// kind.
type Decision struct {
	Key        string
	Value      string
	Class      AttributeClass
	Kept       bool
	DropReason string
}

// Evaluator is the one policy evaluator that governs logs, spans, metrics
// and exemplars (OBS-004 REFACTOR): a signal adapter must redact through it
// rather than inventing a weaker rule of its own.
type Evaluator struct {
	Allow       *Allowlist
	Cardinality *CardinalityGovernor
	Export      ExportPolicy
	Sampling    SamplingPolicy
	// OperationsPolicy is the shared OPS-002 contract. Keeping it on the
	// live evaluator prevents the platform path from silently diverging from
	// the operational policy package.
	OperationsPolicy operationstelemetry.Policy
}

// NewEvaluator builds an Evaluator whose CardinalityGovernor reads budgets
// from allow.
func NewEvaluator(allow *Allowlist, export ExportPolicy, sampling SamplingPolicy) *Evaluator {
	policy := operationstelemetry.DefaultPolicy()
	return &Evaluator{Allow: allow, Cardinality: NewCardinalityGovernor(allow), Export: export, Sampling: sampling, OperationsPolicy: policy}
}

// EvaluateAttribute classifies key, and — only once the key is known,
// registered for kind and not prohibited — caps its value's cardinality
// when kind is SignalMetric. A misconfigured Evaluator (nil Allow) fails
// closed: every attribute is dropped, never kept
// (structured-logging-and-opentelemetry.md "Export, failure and shutdown":
// "Privacy-gateway failure fails telemetry export closed").
func (e *Evaluator) EvaluateAttribute(kind SignalKind, key, value string) Decision {
	if e == nil || e.Allow == nil {
		return Decision{Key: key, Class: ClassProhibited, Kept: false, DropReason: "evaluator_unconfigured"}
	}
	def, ok := e.Allow.Lookup(key)
	if !ok || def.Class == ClassProhibited {
		return Decision{Key: key, Class: ClassProhibited, Kept: false, DropReason: "unknown_or_prohibited_key"}
	}
	allowedForKind := false
	for _, s := range def.Signals {
		if s == kind {
			allowedForKind = true
			break
		}
	}
	if !allowedForKind {
		return Decision{Key: key, Class: def.Class, Kept: false, DropReason: "signal_not_allowed_for_key"}
	}
	out := value
	if kind == SignalMetric {
		if def.MaxCardinality > e.OperationsPolicy.MetricCardinalityLimit {
			return Decision{Key: key, Class: def.Class, Kept: false, DropReason: "policy_cardinality_budget_exceeded"}
		}
		if e.Cardinality == nil {
			return Decision{Key: key, Class: def.Class, Kept: false, DropReason: "cardinality_governor_unconfigured"}
		}
		out = e.Cardinality.Cap(key, value)
	}
	return Decision{Key: key, Value: out, Class: def.Class, Kept: true}
}

// EvaluateExport reports whether class may reach sink under e's export
// policy. A misconfigured Evaluator fails closed.
func (e *Evaluator) EvaluateExport(class AttributeClass, sink SinkClass) bool {
	if e == nil {
		return false
	}
	return e.Export.Allows(class, sink)
}

// Decide reports the sampling verdict for correlationID under e's sampling
// policy.
func (e *Evaluator) Decide(correlationID string, retention RetentionClass) SamplingDecision {
	if e == nil {
		return SamplingDecision{Retained: true, Reason: "forced_retain_evaluator_unconfigured"}
	}
	decision := e.Sampling.Decide(correlationID, retention)
	if e.OperationsPolicy.RequiresRetention(string(retention)) {
		decision.Retained = true
		decision.Reason = "forced_retain_" + string(retention)
	}
	decision.Receipt = e.OperationsPolicy.Receipt(decision.Reason, operationstelemetry.CriticalFailure(retention), decision.Retained)
	return decision
}

// PolicyReceipt returns a deterministic receipt for the evaluator's active
// policy. It contains no customer, tenant, correlation or payload data.
func (e *Evaluator) PolicyReceipt() operationstelemetry.Receipt {
	if e == nil {
		return operationstelemetry.Receipt{}
	}
	return e.OperationsPolicy.Receipt("evaluator_ready", "", true)
}
