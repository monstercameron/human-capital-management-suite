package agentmodel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// CallStage is where in a run a model call was made. AGENTCOST-001 measures
// spend by stage so the share that is "choose one of a few options" can be
// counted instead of guessed.
type CallStage string

const (
	CallStageAdmission        CallStage = "admission"
	CallStageAnswer           CallStage = "answer"
	CallStageToolContinuation CallStage = "tool_continuation"
	CallStageValidation       CallStage = "validation"
	CallStageEvaluation       CallStage = "evaluation"
	CallStageAnnouncement     CallStage = "announcement"
	CallStageScreening        CallStage = "screening"
)

// CallKind says what a model call was for: text a person reads, structured
// output drawn from content, or one of a bounded set of answers.
type CallKind string

const (
	CallKindGeneration CallKind = "generation"
	CallKindExtraction CallKind = "extraction"
	CallKindDecision   CallKind = "decision"
)

// CostBasis says whether a line's cost came from the provider's own figure or
// was estimated from the schedule. An estimate is shown as one, never as zero
// and never as measured.
type CostBasis string

const (
	CostMeasured  CostBasis = "measured"
	CostEstimated CostBasis = "estimated"
)

// CallLine is one model call's ledger line: tokens in and out, cost and
// latency, classified by stage and kind. It carries identifiers and digests,
// never prompts or answers.
type CallLine struct {
	TenantID          string    `json:"tenant_id"`
	AgentID           string    `json:"agent_id"`
	TriggerKind       string    `json:"trigger_kind"`
	RunID             string    `json:"run_id"`
	QuestionID        string    `json:"question_id"`
	CallSite          string    `json:"call_site"`
	Stage             CallStage `json:"stage"`
	Kind              CallKind  `json:"kind"`
	ProviderID        string    `json:"provider_id"`
	ModelID           string    `json:"model_id"`
	ModelVersion      string    `json:"model_version"`
	InputTokens       int64     `json:"input_tokens"`
	OutputTokens      int64     `json:"output_tokens"`
	CachedInputTokens int64     `json:"cached_input_tokens"`
	CostMicros        int64     `json:"cost_micros"`
	Basis             CostBasis `json:"cost_basis"`
	LatencyMillis     int64     `json:"latency_ms"`
	StartedAt         time.Time `json:"started_at"`
	PromptDigest      string    `json:"prompt_digest"`
	ScheduleVersion   string    `json:"schedule_version"`
}

// ErrInvalidCallLine marks a ledger line that must not be recorded.
var ErrInvalidCallLine = errors.New("agentmodel: invalid call ledger line")

// Validate refuses a line that would make the spend figures wrong.
func (l CallLine) Validate() error {
	switch l.Stage {
	case CallStageAdmission, CallStageAnswer, CallStageToolContinuation, CallStageValidation, CallStageEvaluation, CallStageAnnouncement, CallStageScreening:
	default:
		return fmt.Errorf("%w: unknown stage %q", ErrInvalidCallLine, l.Stage)
	}
	switch l.Kind {
	case CallKindGeneration, CallKindExtraction, CallKindDecision:
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidCallLine, l.Kind)
	}
	switch l.Basis {
	case CostMeasured, CostEstimated:
	default:
		return fmt.Errorf("%w: unknown cost basis %q", ErrInvalidCallLine, l.Basis)
	}
	if strings.TrimSpace(l.TenantID) == "" || strings.TrimSpace(l.AgentID) == "" || strings.TrimSpace(l.RunID) == "" || strings.TrimSpace(l.CallSite) == "" {
		return fmt.Errorf("%w: tenant, agent, run and call site are required", ErrInvalidCallLine)
	}
	if l.InputTokens < 0 || l.OutputTokens < 0 || l.CachedInputTokens < 0 || l.CachedInputTokens > l.InputTokens || l.CostMicros < 0 || l.LatencyMillis < 0 {
		return fmt.Errorf("%w: tokens, cost and latency cannot be negative and cached input cannot exceed input", ErrInvalidCallLine)
	}
	if l.StartedAt.IsZero() {
		return fmt.Errorf("%w: start time is required", ErrInvalidCallLine)
	}
	return nil
}

// ClassifyTaskProfile maps a task profile to what its calls are for. Profiles
// that ask a bounded question are decisions; profiles that fill a structure
// from content are extractions; anything unrecognised is generation, so new
// work is never silently counted as cheap.
func ClassifyTaskProfile(profile string) CallKind {
	lower := strings.ToLower(profile)
	for _, marker := range []string{"screen", "decide", "decision", "choose", "choice", "classify", "triage", "route", "validate", "verify", "moderate", "detect_audience", "audience"} {
		if strings.Contains(lower, marker) {
			return CallKindDecision
		}
	}
	for _, marker := range []string{"extract", "slot", "parse", "transcribe", "summarise_slots", "summarize_slots"} {
		if strings.Contains(lower, marker) {
			return CallKindExtraction
		}
	}
	return CallKindGeneration
}

// CallContext is what the caller knows about a model call that the usage
// figures alone cannot say. It travels on the request context.
type CallContext struct {
	TenantID     string
	AgentID      string
	TriggerKind  string
	RunID        string
	QuestionID   string
	CallSite     string
	Stage        CallStage
	Kind         CallKind
	PromptDigest string
}

type callContextKey struct{}

// WithCallContext attaches the call's identity for the recording adapter.
func WithCallContext(ctx context.Context, call CallContext) context.Context {
	return context.WithValue(ctx, callContextKey{}, call)
}

type callLedgerKey struct{}

// WithCallLedger carries the run's ledger for the recording adapter. The
// composed gateway wraps its adapters once with no ledger of their own; each
// run carries its ledger on the call context so one shared adapter records
// into the calling run's ledger.
func WithCallLedger(ctx context.Context, ledger *CallLedger) context.Context {
	return context.WithValue(ctx, callLedgerKey{}, ledger)
}

// CallLedgerFromContext returns the ledger carried on ctx, if any.
func CallLedgerFromContext(ctx context.Context) *CallLedger {
	if ctx == nil {
		return nil
	}
	ledger, _ := ctx.Value(callLedgerKey{}).(*CallLedger)
	return ledger
}

// Pricer prices usage without a provider figure: the cost and whether it is
// measured. A nil pricer leaves unmeasured usage at zero cost, estimated.
type Pricer func(ModelIdentity, ModelUsage) (costMicros int64, measured bool)

// RecordingAdapter is a ModelAdapter that appends one ledger line per call to
// the ledger and otherwise passes the call through untouched.
type RecordingAdapter struct {
	next            ModelAdapter
	ledger          *CallLedger
	pricer          Pricer
	now             func() time.Time
	scheduleVersion string
}

// NewRecordingAdapter wraps next, recording into ledger. A nil ledger records
// nowhere; a nil pricer estimates unmeasured usage at zero.
func NewRecordingAdapter(next ModelAdapter, ledger *CallLedger, pricer Pricer) *RecordingAdapter {
	return &RecordingAdapter{next: next, ledger: ledger, pricer: pricer, now: time.Now}
}

// WithScheduleVersion returns a recorder that stamps lines with the price
// schedule version their costs were computed at.
func (a *RecordingAdapter) WithScheduleVersion(version string) *RecordingAdapter {
	if a == nil {
		return nil
	}
	out := *a
	out.scheduleVersion = version
	return &out
}

var _ ModelAdapter = (*RecordingAdapter)(nil)

// Capabilities advertises the wrapped adapter's contract.
func (a *RecordingAdapter) Capabilities() AdapterCapabilities {
	if a == nil || a.next == nil {
		return AdapterCapabilities{}
	}
	return a.next.Capabilities()
}

// Identity advertises the wrapped adapter's pinned identity so a composed
// gateway still sees its selection through the recorder. An inner adapter
// without an identity fails closed as the zero identity.
func (a *RecordingAdapter) Identity() ModelIdentity {
	if a == nil || a.next == nil {
		return ModelIdentity{}
	}
	identified, ok := a.next.(interface{ Identity() ModelIdentity })
	if !ok {
		return ModelIdentity{}
	}
	return identified.Identity()
}

// Invoke runs the call and records its ledger line. A call without a call
// context is still run; it is recorded under the unknown call site so the
// totals stay complete and the gap stays visible. The run's own ledger rides
// on the context and wins over the constructed one.
func (a *RecordingAdapter) Invoke(ctx context.Context, req ModelRequest) (ModelResult, error) {
	if a == nil || a.next == nil {
		return ModelResult{}, errors.New("agentmodel: recording adapter has no wrapped adapter")
	}
	now := a.now
	if now == nil {
		now = time.Now
	}
	start := now()
	result, err := a.next.Invoke(ctx, req)
	elapsed := now().Sub(start)
	ledger := CallLedgerFromContext(ctx)
	if ledger == nil {
		ledger = a.ledger
	}
	if ledger == nil {
		return result, err
	}
	call, _ := ctx.Value(callContextKey{}).(CallContext)
	if strings.TrimSpace(call.TenantID) == "" {
		call.TenantID = "unknown"
	}
	if strings.TrimSpace(call.AgentID) == "" {
		call.AgentID = "unknown"
	}
	if strings.TrimSpace(call.RunID) == "" {
		call.RunID = "unknown"
	}
	if strings.TrimSpace(call.CallSite) == "" {
		call.CallSite = "unknown"
	}
	if call.Stage == "" {
		call.Stage = CallStageAnswer
	}
	if call.Kind == "" {
		call.Kind = ClassifyTaskProfile(req.TaskProfile)
	}
	cost, measured := result.Usage.CostMicros, result.Usage.CostMicros > 0
	if !measured && a.pricer != nil {
		cost, measured = a.pricer(result.Provider, result.Usage)
	}
	if cost < 0 {
		cost = 0
	}
	basis := CostEstimated
	if measured {
		basis = CostMeasured
	}
	_ = ledger.Append(CallLine{
		TenantID: call.TenantID, AgentID: call.AgentID, TriggerKind: call.TriggerKind, RunID: call.RunID,
		QuestionID: call.QuestionID, CallSite: call.CallSite, Stage: call.Stage, Kind: call.Kind,
		ProviderID: result.Provider.ProviderID, ModelID: result.Provider.ModelID, ModelVersion: result.Provider.Version,
		InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens,
		CachedInputTokens: result.Usage.CachedInputTokens, CostMicros: cost, Basis: basis,
		LatencyMillis: elapsed.Milliseconds(), StartedAt: start.UTC(), PromptDigest: call.PromptDigest,
		ScheduleVersion: a.scheduleVersion,
	})
	return result, err
}

// CallLedger keeps per-call lines for one measurement window.
type CallLedger struct {
	mu    sync.Mutex
	lines []CallLine
}

// Append records one validated line.
func (l *CallLedger) Append(line CallLine) error {
	if err := line.Validate(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
	return nil
}

// Lines returns the recorded lines oldest first.
func (l *CallLedger) Lines() []CallLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]CallLine(nil), l.lines...)
}

// CallTotals sums one slice of calls.
type CallTotals struct {
	Calls         int64
	TokensIn      int64
	TokensOut     int64
	CostMicros    int64
	LatencyMillis int64
}

// TotalsByAgent sums tokens, cost and latency per agent.
func (l *CallLedger) TotalsByAgent() map[string]CallTotals {
	out := map[string]CallTotals{}
	for _, line := range l.Lines() {
		totals := out[line.AgentID]
		totals.Calls++
		totals.TokensIn += line.InputTokens
		totals.TokensOut += line.OutputTokens
		totals.CostMicros += line.CostMicros
		totals.LatencyMillis += line.LatencyMillis
		out[line.AgentID] = totals
	}
	return out
}

// TotalsByTrigger sums tokens, cost and latency per trigger kind.
func (l *CallLedger) TotalsByTrigger() map[string]CallTotals {
	out := map[string]CallTotals{}
	for _, line := range l.Lines() {
		totals := out[line.TriggerKind]
		totals.Calls++
		totals.TokensIn += line.InputTokens
		totals.TokensOut += line.OutputTokens
		totals.CostMicros += line.CostMicros
		totals.LatencyMillis += line.LatencyMillis
		out[line.TriggerKind] = totals
	}
	return out
}

// TotalsByQuestion sums tokens, cost and latency per answered question. Lines
// without a question are counted under the empty question so the sum still
// reconciles with the other totals.
func (l *CallLedger) TotalsByQuestion() map[string]CallTotals {
	out := map[string]CallTotals{}
	for _, line := range l.Lines() {
		totals := out[line.QuestionID]
		totals.Calls++
		totals.TokensIn += line.InputTokens
		totals.TokensOut += line.OutputTokens
		totals.CostMicros += line.CostMicros
		totals.LatencyMillis += line.LatencyMillis
		out[line.QuestionID] = totals
	}
	return out
}

// CallSiteCost is one call site's summed cost.
type CallSiteCost struct {
	Site   string
	Totals CallTotals
}

// TopCallSites returns the n most expensive call sites, most expensive first.
func (l *CallLedger) TopCallSites(n int) []CallSiteCost {
	bySite := map[string]CallTotals{}
	for _, line := range l.Lines() {
		totals := bySite[line.CallSite]
		totals.Calls++
		totals.TokensIn += line.InputTokens
		totals.TokensOut += line.OutputTokens
		totals.CostMicros += line.CostMicros
		totals.LatencyMillis += line.LatencyMillis
		bySite[line.CallSite] = totals
	}
	var out []CallSiteCost
	for site, totals := range bySite {
		out = append(out, CallSiteCost{Site: site, Totals: totals})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Totals.CostMicros != out[j].Totals.CostMicros {
			return out[i].Totals.CostMicros > out[j].Totals.CostMicros
		}
		return out[i].Site < out[j].Site
	})
	if n < 0 {
		n = 0
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// DecisionSharePercent is the whole-percent share of spend on calls classified
// as decisions rather than generation or extraction.
func (l *CallLedger) DecisionSharePercent() int {
	var decision, total int64
	for _, line := range l.Lines() {
		total += line.CostMicros
		if line.Kind == CallKindDecision {
			decision += line.CostMicros
		}
	}
	if total <= 0 {
		return 0
	}
	return int((decision*100 + total/2) / total)
}
