package replay

import (
	"context"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Options are everything a [Replayer] needs. Every field is a value or a port
// the caller owns; this package opens no connection, reads no configuration
// and consults no ambient state.
type Options struct {
	// Plan is the compiled definition to re-execute. Its digest must be the
	// one [Record.CompiledPlanDigest] pins.
	Plan *workflow.CompiledWorkflow
	// Source hands over the durable record.
	Source Source

	// Contract, Definition and Instance are the REPLAY admission triple. See
	// [Admit] for what each of them is checked for.
	Contract   intent.ModeContract
	Definition intent.Definition
	Instance   intent.Instance

	// Adapter is the live adapter set the historical run was composed with.
	// A replay never invokes it: it is held only so the composition under
	// replay is the same composition that ran, and every attempt through
	// [Replayer.EffectGate] is refused before it would delegate. Nil is
	// ordinary and means the caller composed no adapters at all.
	Adapter NodeAdapter

	// MaxSteps bounds one replay. Zero uses a plan-sized bound, which is what
	// keeps a record that declares a cycle from replaying forever without a
	// timer anyone has to own.
	MaxSteps int
}

// Result is everything one [Replayer.Replay] call produced.
//
// It is always populated, including on a refusal: a replay that stopped at a
// divergence has re-derived everything up to that point, and throwing that
// away would leave an investigator with an error string instead of the walk
// that led to it.
type Result struct {
	// Trace is the replayed walk. On a divergence it holds the entries
	// produced before the divergence was found.
	Trace Trace
	// Status restates [Trace.Status] so a caller that keeps only the result
	// still has it.
	Status Status
	// Divergence is the first place the replay and the record disagreed, or
	// nil when they did not.
	Divergence *Divergence
	// Refusals is every adapter attempt this replayer's [Replayer.EffectGate]
	// has turned away, in attempt order. It is empty for an ordinary replay,
	// because an ordinary replay attempts nothing: the replayer itself never
	// reaches for an adapter, so a non-empty Refusals means a composed
	// handler tried to and was stopped.
	Refusals []Refusal
	// RecordedTraceDigest is what the record pinned, and DigestMatches
	// reports whether the replay reproduced it. DigestMatches is false when
	// the record pinned nothing, so a caller must read both.
	RecordedTraceDigest string
	DigestMatches       bool
	// Record is the record the replay actually consumed, as loaded. It is
	// carried so a report names its own inputs.
	Record Record
}

// Replayer re-executes one compiled definition from one instance's durable
// record.
//
// It is built once and may be replayed repeatedly: [Replayer.Replay] holds no
// state across calls beyond the ports it was constructed with, which is what
// makes "replaying twice is byte-identical" a property rather than a hope.
type Replayer struct {
	plan     *workflow.CompiledWorkflow
	source   Source
	contract intent.ModeContract
	def      intent.Definition
	inst     intent.Instance
	adapter  NodeAdapter
	maxSteps int
	// gate is the one [Recorder] every [Replayer.EffectGate] attempt goes
	// through, so a refusal a composed handler provoked is still visible in
	// the next [Result] rather than lost with the gate that produced it.
	gate *Recorder
}

// New validates the wiring and the REPLAY admission triple. A replayer that
// exists is one that has already been admitted: [Admit] runs here, before any
// record is read, so a contract that would permit an effect is refused without
// ever touching history.
func New(opts Options) (*Replayer, error) {
	if opts.Plan == nil {
		return nil, refuse(CodeInvalidOptions, "", "no compiled plan supplied")
	}
	if opts.Source == nil {
		return nil, refuse(CodeInvalidOptions, "", "no record source supplied")
	}
	if err := Admit(opts.Contract, opts.Definition, opts.Instance); err != nil {
		return nil, err
	}
	max := opts.MaxSteps
	if max <= 0 {
		max = 4*len(opts.Plan.Nodes) + 8
	}
	return &Replayer{
		plan: opts.Plan, source: opts.Source, contract: opts.Contract,
		def: opts.Definition, inst: opts.Instance, adapter: opts.Adapter, maxSteps: max,
		gate: NewRecorder(Record{}),
	}, nil
}

// EffectGate returns the only adapter surface a composed handler may reach
// during a replay.
//
// Every attempt through it other than a governed read is
// [CodeEffectForbidden], naming the node that tried, and the caller's own
// adapter is never delegated to -- not on a refusal, and not on the governed
// read either, which is answered from the record. A caller wiring a replay
// hands this gate to its handlers instead of the live adapter set.
func (r *Replayer) EffectGate() NodeAdapter {
	return guardedAdapter{recorder: r.gate, inner: r.adapter}
}

// Refusals is every attempt [Replayer.EffectGate] has turned away since this
// replayer was built, in attempt order.
func (r *Replayer) Refusals() []Refusal { return r.gate.Refusals() }

// Contract returns the mode contract this replayer was admitted under.
func (r *Replayer) Contract() intent.ModeContract { return r.contract }

// Replay re-derives the recorded run.
//
// It returns a fully populated [Result] in every case. The error is nil only
// for a clean replay -- one that reached the record's terminal, stopped at a
// paused instance's frontier, or exhausted the recorded attempts with the
// instance still open. Every other outcome carries a typed [Error]:
// [CodeArtifactUnavailable] when the record is missing material the walk
// reached for, [CodeDivergence] when the replay and the record disagree about
// something the record does hold, and the structural codes for a record or
// plan that could not be replayed at all. A divergence is reported both ways
// on purpose: WF-RUN-013's GREEN clause names a code and its FAULT clause asks
// for a divergence value rather than a crash, and a caller should not have to
// choose which of the two it gets.
func (r *Replayer) Replay(ctx context.Context) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.replay.replay")
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	rec, err := r.source.Load(ctx)
	if err != nil {
		return Result{}, wrap(CodeSourceFailed, "", err, "load the durable record")
	}
	if err := rec.Validate(); err != nil {
		return Result{Record: rec}, err
	}
	if rec.CompiledPlanDigest != r.plan.Digest() {
		return Result{Record: rec}, refuse(CodePlanMismatch, "",
			"the record pins compiled plan %s; replay was called with a plan digesting to %s",
			rec.CompiledPlanDigest, r.plan.Digest())
	}
	if rec.WorkflowID != "" && rec.WorkflowID != r.plan.WorkflowID {
		return Result{Record: rec}, refuse(CodePlanMismatch, "",
			"the record is of workflow %s; replay was called with %s", rec.WorkflowID, r.plan.WorkflowID)
	}

	run := &walk{
		plan:     r.plan,
		rec:      rec,
		recorder: NewRecorder(rec),
		gate:     r.gate,
		maxSteps: r.maxSteps,
	}
	return run.execute()
}

// walk is one replay in flight. It is a struct rather than a long function so
// that each phase of WF-RUN-013's contract -- seed, step, settle, compare --
// reads as its own method.
type walk struct {
	plan     *workflow.CompiledWorkflow
	rec      Record
	recorder *Recorder
	gate     *Recorder
	maxSteps int

	state   frontier.InstanceState
	entries []TraceEntry
}

func (w *walk) execute() (Result, error) {
	state, err := frontier.Seed(w.plan, w.rec.InstanceID.String(), w.rec.JoinDeclarations...)
	if err != nil {
		return w.result(StatusDiverged, "", nil), wrap(CodeRecordInvalid, "", err,
			"seed the instance state the record was produced from")
	}
	w.state = state

	ordered := w.rec.Ordered()
	if len(ordered) > w.maxSteps {
		return w.result(StatusDiverged, "", nil), refuse(CodeStepBudgetExceeded, "",
			"the record holds %d attempts; this replay is bounded at %d", len(ordered), w.maxSteps)
	}

	var (
		complete     bool
		terminalCode string
	)
	for _, nr := range ordered {
		div, err := w.step(nr)
		if div != nil {
			return w.result(StatusDiverged, terminalCode, div), err
		}
		if w.state.Completed {
			complete = true
			terminalCode = w.state.Terminal.TerminalCode
			break
		}
	}
	return w.settle(complete, terminalCode)
}

// step re-derives one recorded attempt. It returns the divergence it found, or
// nil and one appended trace entry.
func (w *walk) step(nr NodeRecord) (*Divergence, error) {
	node, ok := w.plan.Node(nr.NodeID)
	if !ok {
		d := &Divergence{
			Sequence: nr.Sequence, NodeID: nr.NodeID, Attempt: nr.Attempt, Field: FieldFrontier,
			Recorded: nr.NodeID, Replayed: "",
			Detail: "the compiled plan holds no such node",
		}
		return d, refuse(CodeDivergence, nr.NodeID,
			"the record ran a node the compiled plan does not declare")
	}
	if !onFrontier(w.state, nr.NodeID) {
		d := &Divergence{
			Sequence: nr.Sequence, NodeID: nr.NodeID, Attempt: nr.Attempt, Field: FieldFrontier,
			Recorded: nr.NodeID, Replayed: joinIDs(w.state.Frontier),
			Detail: "the record ran this node next; re-derivation does not place it on the frontier",
		}
		return d, refuse(CodeDivergence, nr.NodeID, "recorded node is not on the replayed frontier")
	}
	if node.Type != nr.StepType {
		d := &Divergence{
			Sequence: nr.Sequence, NodeID: nr.NodeID, Attempt: nr.Attempt, Field: FieldFrontier,
			Recorded: string(nr.StepType), Replayed: string(node.Type),
			Detail: "the record and the compiled plan disagree about this node's step type",
		}
		return d, refuse(CodeDivergence, nr.NodeID, "recorded step type is not the compiled one")
	}

	source, div, err := w.resolveSource(nr)
	if div != nil {
		return div, err
	}

	tr, aerr := frontier.Advance(w.plan, w.state, outcomeOf(nr))
	if aerr != nil {
		d := &Divergence{
			Sequence: nr.Sequence, NodeID: nr.NodeID, Attempt: nr.Attempt, Field: FieldRoute,
			Recorded: nr.RouteKey, Replayed: "",
			Detail: "the compiled plan cannot route the recorded outcome",
		}
		return d, wrap(CodeDivergence, nr.NodeID, aerr, "advance on the recorded outcome")
	}

	w.entries = append(w.entries, TraceEntry{
		Sequence: nr.Sequence, NodeID: nr.NodeID, Attempt: nr.Attempt, StepType: nr.StepType,
		Source: source, RouteKey: tr.RouteKey, OutputDigest: tr.OutputDigest,
		CompletedState: string(tr.CompletedState), Frontier: append([]string(nil), tr.Frontier...),
		TransitionDigest: tr.Digest(), ObservedAt: nr.RecordedAt.UTC(),
	})
	w.state = tr.Next
	return nil, nil
}

// resolveSource says where this attempt's inputs came from, refusing when the
// record names a signal or timer it does not itself hold. It is the point at
// which "take every input from the record" stops being a comment and becomes a
// check.
func (w *walk) resolveSource(nr NodeRecord) (InputSource, *Divergence, error) {
	switch {
	case nr.SignalID != "":
		if _, err := w.recorder.Signal(nr.NodeID, nr.SignalID); err != nil {
			d := &Divergence{
				Sequence: nr.Sequence, NodeID: nr.NodeID, Attempt: nr.Attempt, Field: FieldArtifact,
				Recorded: nr.SignalID, Replayed: "",
				Detail: "the attempt names a signal delivery the record does not hold",
			}
			return "", d, err
		}
		return SourceRecordedSignal, nil, nil
	case nr.TimerID != "":
		if _, err := w.recorder.Timer(nr.NodeID, nr.TimerID); err != nil {
			d := &Divergence{
				Sequence: nr.Sequence, NodeID: nr.NodeID, Attempt: nr.Attempt, Field: FieldArtifact,
				Recorded: nr.TimerID, Replayed: "",
				Detail: "the attempt names a timer settlement the record does not hold",
			}
			return "", d, err
		}
		return SourceRecordedTimer, nil, nil
	default:
		return SourceRecordedOutput, nil, nil
	}
}

// settle decides the status of a walk that ran out of recorded attempts (or
// reached a terminal), and runs the two whole-run comparisons: the open
// frontier and the pinned trace digest.
func (w *walk) settle(complete bool, terminalCode string) (Result, error) {
	if complete {
		if want := w.rec.TerminalCode; want != "" && want != terminalCode {
			d := &Divergence{
				Sequence: len(w.entries), NodeID: w.state.Terminal.NodeID, Field: FieldTerminal,
				Recorded: want, Replayed: terminalCode,
				Detail: "the replay reached a different terminal than the record",
			}
			return w.result(StatusDiverged, terminalCode, d),
				refuse(CodeDivergence, w.state.Terminal.NodeID, "terminal does not match the record")
		}
		return w.compare(w.result(StatusComplete, terminalCode, nil))
	}

	// The record says the instance ended, and the recorded attempts did not
	// get there. That is a missing artifact, not a shorter run.
	if w.rec.FinalStatus.Terminal() {
		node := firstOpen(w.state)
		d := &Divergence{
			Sequence: len(w.entries), NodeID: node, Field: FieldArtifact,
			Recorded: string(w.rec.FinalStatus), Replayed: joinIDs(w.state.Frontier),
			Detail: "the record ends the instance but holds no attempt that reaches its terminal",
		}
		return w.result(StatusDiverged, "", d), refuse(CodeArtifactUnavailable, node,
			"the record ends %s with %d attempts, none of which reaches a terminal",
			w.rec.FinalStatus, len(w.rec.Nodes))
	}

	status := StatusOpenFrontier
	if w.rec.Paused() {
		status = StatusPausedAtFrontier
	}
	if want := w.rec.OpenFrontier(); len(want) > 0 && joinIDs(want) != joinIDs(w.state.Frontier) {
		d := &Divergence{
			Sequence: len(w.entries), NodeID: firstDifferent(want, w.state.Frontier), Field: FieldOpenFrontier,
			Recorded: joinIDs(want), Replayed: joinIDs(w.state.Frontier),
			Detail: "the replay stopped at a different open frontier than the record holds",
		}
		return w.result(StatusDiverged, "", d), refuse(CodeDivergence, d.NodeID,
			"the replayed frontier is not the recorded one")
	}
	return w.compare(w.result(status, "", nil))
}

// compare checks the replayed trace against the digest the record pinned. A
// record that pins nothing is not a divergence -- it is a record taken before
// its trace was minted -- but it never reports a match either.
func (w *walk) compare(res Result) (Result, error) {
	if res.RecordedTraceDigest == "" {
		return res, nil
	}
	if res.RecordedTraceDigest == res.Trace.Digest() {
		res.DigestMatches = true
		return res, nil
	}
	node := ""
	if len(w.entries) > 0 {
		node = w.entries[len(w.entries)-1].NodeID
	}
	res.Status = StatusDiverged
	res.Trace.Status = StatusDiverged
	res.Trace.digest = computeTraceDigest(res.Trace)
	res.Divergence = &Divergence{
		Sequence: len(w.entries), NodeID: node, Field: FieldTraceDigest,
		Recorded: res.RecordedTraceDigest, Replayed: res.Trace.Digest(),
		Detail: "the replayed trace does not reproduce the recorded digest",
	}
	return res, refuse(CodeDivergence, node, "the replayed trace digest is not the recorded one")
}

// result assembles the trace and the result. It is the one place the trace
// digest is minted, so a trace that reached a caller is always a trace whose
// digest covers exactly the content it carries.
func (w *walk) result(status Status, terminalCode string, div *Divergence) Result {
	tr := Trace{
		TenantID: w.rec.TenantID.String(), InstanceID: w.rec.InstanceID.String(),
		WorkflowID: w.plan.WorkflowID, Version: w.plan.Version, PlanDigest: w.plan.Digest(),
		HistoricalIntentID: w.rec.HistoricalIntentID,
		Entries:            append([]TraceEntry(nil), w.entries...),
		Status:             status, TerminalCode: terminalCode,
		Frontier: append([]string(nil), w.state.Frontier...),
	}
	if tr.Entries == nil {
		tr.Entries = []TraceEntry{}
	}
	if tr.Frontier == nil {
		tr.Frontier = []string{}
	}
	tr.digest = computeTraceDigest(tr)
	return Result{
		Trace: tr, Status: status, Divergence: div,
		Refusals:            append(w.gate.Refusals(), w.recorder.Refusals()...),
		RecordedTraceDigest: w.rec.TraceDigest,
		Record:              w.rec,
	}
}

// outcomeOf builds the advancement input from the record, verbatim. Every
// field is copied; none is derived, defaulted or recomputed, which is the
// whole of "take every node input from the recorded outputs".
func outcomeOf(nr NodeRecord) frontier.NodeOutcome {
	return frontier.NodeOutcome{
		NodeID:       nr.NodeID,
		Outcome:      workflow.Outcome(nr.RouteKey),
		OutputDigest: nr.OutputDigest,
		Await:        nr.Await,
		AwaitRef:     nr.AwaitRef,
		Failed:       nr.Failed,
		ErrorClass:   nr.ErrorClass,
		Terminal:     nr.Terminal,
	}
}

func onFrontier(state frontier.InstanceState, nodeID string) bool {
	for _, id := range state.Frontier {
		if id == nodeID {
			return true
		}
	}
	return false
}

func firstOpen(state frontier.InstanceState) string {
	if len(state.Frontier) == 0 {
		return ""
	}
	return state.Frontier[0]
}

// firstDifferent names the first node the two sorted sets disagree about, so a
// frontier divergence points at one node rather than at two lists.
func firstDifferent(recorded, replayed []string) string {
	have := map[string]bool{}
	for _, id := range replayed {
		have[id] = true
	}
	for _, id := range recorded {
		if !have[id] {
			return id
		}
	}
	want := map[string]bool{}
	for _, id := range recorded {
		want[id] = true
	}
	for _, id := range replayed {
		if !want[id] {
			return id
		}
	}
	return ""
}

// joinIDs renders a node id set for a divergence's two sides. It sorts a copy,
// so the rendering is stable regardless of the order it was handed.
func joinIDs(ids []string) string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	s := ""
	for i, id := range out {
		if i > 0 {
			s += ","
		}
		s += id
	}
	return s
}
