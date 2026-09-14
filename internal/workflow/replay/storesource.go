package replay

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Executor is the minimal database capability [StoreSource] needs. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it, and so do
// internal/workflow/runtime's and internal/data/runtimestate's own Executors,
// which are the same two interfaces -- naming it here keeps this package from
// importing one of theirs just to spell a parameter.
//
// It is read-only in practice: nothing in this package issues a write. The
// Execer half is present only because [runtime.Store]'s and
// [runtimestate]'s Executors declare it, so a caller can pass the handle it
// already has.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// StoreSource assembles a [Record] from the durable tables DB-012
// materialized.
//
// It reads and nothing else:
//
//   - workflow_instance and workflow_node_execution, through
//     [runtime.Store]'s own LoadInstance and LoadNodeExecutions, for the
//     instance identity, its pinned plan digest, its status and every node
//     attempt's step type, typed output digest and error class.
//   - workflow_continuation, directly, for the advancement order and the route
//     key each completion took. Those two facts live nowhere else:
//     [runtime.Advance] records the outcome's output digest on the node
//     execution but records the route it took only on the continuations it
//     derived, and [runtime.ContinuationStore] declares no read method
//     (deliberately -- it is an append-only write target), so this package
//     reads the ledger's own columns rather than asking that store to grow a
//     reader for one caller.
//   - workflow_frontier_entry and workflow_checkpoint, through
//     internal/data/runtimestate's own stores.
//
// It deliberately reads no signals and no timers. This runtime raises neither:
// internal/workflow/execute's continuation sink refuses
// SIGNAL_SUBSCRIPTION_REQUIRED and TIMER_REQUIRED as unsupported, so those
// tables hold nothing for an instance this source can find, and returning an
// empty slice is the honest answer rather than a query nobody can make return
// a row. A caller replaying a run that did use them supplies the record
// through a [MemorySource] instead.
type StoreSource struct {
	Executor   Executor
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	// HistoricalIntentID is the intent the recorded run was. It is a
	// parameter rather than a column because workflow_instance stores the
	// correlation id, not the intent envelope's own id, and a replay must
	// name the intent it re-derives rather than guess at it. Empty falls back
	// to the instance's correlation id.
	HistoricalIntentID string
	// TraceDigest, when set, is the digest the replay must reproduce.
	TraceDigest string
	// JoinDeclarations are the declarations the historical start passed to
	// [frontier.Seed].
	JoinDeclarations []frontier.JoinDeclaration
}

var _ Source = StoreSource{}

// Load implements [Source].
func (s StoreSource) Load(ctx context.Context) (ret0 Record, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.replay.load_record")
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if s.Executor == nil {
		return Record{}, refuse(CodeSourceFailed, "", "no executor supplied")
	}
	if s.TenantID == uuid.Nil || s.InstanceID == uuid.Nil {
		return Record{}, refuse(CodeSourceFailed, "", "tenant and instance ids are required")
	}

	inst, err := (runtime.Store{}).LoadInstance(ctx, s.Executor, s.TenantID, s.InstanceID)
	if err != nil {
		return Record{}, wrap(CodeSourceFailed, "", err, "load instance %s", s.InstanceID)
	}
	execs, err := (runtime.Store{}).LoadNodeExecutions(ctx, s.Executor, s.TenantID, s.InstanceID)
	if err != nil {
		return Record{}, wrap(CodeSourceFailed, "", err, "load node executions of %s", s.InstanceID)
	}
	conts, err := s.continuations(ctx)
	if err != nil {
		return Record{}, err
	}

	intentID := s.HistoricalIntentID
	if intentID == "" {
		intentID = inst.CorrelationID
	}
	rec := Record{
		TenantID: inst.TenantID, InstanceID: inst.InstanceID,
		WorkflowID: inst.WorkflowID, WorkflowVersion: inst.WorkflowVersion,
		CompiledPlanDigest: inst.CompiledPlanHash,
		CorrelationID:      inst.CorrelationID, HistoricalIntentID: intentID,
		ExecutionMode: inst.ExecutionMode, FinalStatus: inst.RuntimeStatus,
		InputRef: inst.InputRef, TraceDigest: s.TraceDigest,
		JoinDeclarations: append([]frontier.JoinDeclaration(nil), s.JoinDeclarations...),
	}
	rec.Nodes, rec.TerminalCode = nodeRecords(execs, conts)

	if !inst.RuntimeStatus.Terminal() {
		entries, err := (runtimestate.FrontierStore{}).Open(ctx, s.Executor, s.TenantID, s.InstanceID)
		if err != nil {
			return Record{}, wrap(CodeSourceFailed, "", err, "read frontier of %s", s.InstanceID)
		}
		for _, e := range entries {
			rec.Frontier = append(rec.Frontier, FrontierEntry{
				NodeID: e.NodeID, State: e.State, Sequence: e.Sequence,
				EnteredAt: e.EnteredAt, LeftAt: e.LeftAt,
			})
		}
		if len(rec.Frontier) == 0 {
			// No frontier-entry rows: this driver records the frontier on the
			// instance itself (workflow_instance.current_node_ids) and never
			// writes workflow_frontier_entry. Read it from where it actually
			// is rather than reporting an instance with no frontier at all.
			for i, id := range inst.Frontier() {
				rec.Frontier = append(rec.Frontier, FrontierEntry{
					NodeID: id, State: runtimestate.FrontierReady,
					Sequence: uint64(i + 1), EnteredAt: inst.CreatedAt,
				})
			}
		}
	}

	if cp, err := (runtimestate.CheckpointStore{}).Latest(ctx, s.Executor, s.TenantID, s.InstanceID); err == nil {
		rec.Checkpoints = append(rec.Checkpoints, Checkpoint{
			Sequence: cp.Sequence, Kind: cp.Kind, StateDigest: cp.StateDigest,
			FrontierDigest: cp.FrontierDigest, VariableDigest: cp.VariableDigest,
			InstanceVersion: cp.InstanceVersion, TakenAt: cp.TakenAt,
		})
	} else if !errors.Is(err, runtimestate.ErrNotFound) {
		return Record{}, wrap(CodeSourceFailed, "", err, "read checkpoints of %s", s.InstanceID)
	}

	return rec, nil
}

// continuationRow is one workflow_continuation row, read raw.
type continuationRow struct {
	sourceNodeID  string
	sourceAttempt int
	targetNodeID  string
	kind          frontier.IntentKind
	routeKey      string
	ref           string
	terminalCode  string
	recordedAt    time.Time
}

// continuations reads the instance's continuation ledger in recorded order.
func (s StoreSource) continuations(ctx context.Context) ([]continuationRow, error) {
	rows, err := s.Executor.Query(ctx, `
		SELECT source_node_id, source_attempt, target_node_id, kind,
			COALESCE(route_key, ''), COALESCE(ref, ''), COALESCE(terminal_code, ''), recorded_at
		FROM workflow_continuation
		WHERE tenant_id = $1 AND instance_id = $2
		ORDER BY recorded_at, source_node_id, source_attempt, target_node_id, kind`,
		s.TenantID, s.InstanceID)
	if err != nil {
		return nil, wrap(CodeSourceFailed, "", err, "read continuations of %s", s.InstanceID)
	}
	defer rows.Close()

	var out []continuationRow
	for rows.Next() {
		var (
			r    continuationRow
			kind string
		)
		if err := rows.Scan(&r.sourceNodeID, &r.sourceAttempt, &r.targetNodeID, &kind,
			&r.routeKey, &r.ref, &r.terminalCode, &r.recordedAt); err != nil {
			return nil, wrap(CodeSourceFailed, "", err, "scan continuation of %s", s.InstanceID)
		}
		r.kind = frontier.IntentKind(kind)
		r.recordedAt = r.recordedAt.UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeSourceFailed, "", err, "read continuations of %s", s.InstanceID)
	}
	return out, nil
}

// nodeRecords reconstructs the recorded attempts, in advancement order, from
// the node executions and the continuation ledger.
//
// The ledger, not the execution table, defines the order and the membership:
// an advancement is exactly a (source node, source attempt) pair that raised
// continuations, so a node execution the frontier settled without driving --
// a SKIPPED branch, a successor created READY and never run -- is correctly
// absent.
//
// The order is causal, not chronological. Continuation rows carry the instant
// the advancement was recorded at, and a driver running a whole synchronous
// drain against one clock stamps every one of them identically, so ordering by
// recorded_at alone would fall back to whatever the tie-break happened to be
// and replay the graph in alphabetical order. What the ledger does encode
// exactly is causation: advancement N raises the continuations whose target
// nodes advancement N+1 is the source of. [causalOrder] reads that, and uses
// the timestamps only to break a genuine tie between two independent branches.
func nodeRecords(execs []runtime.NodeExecution, conts []continuationRow) ([]NodeRecord, string) {
	byNode := map[string]runtime.NodeExecution{}
	for _, e := range execs {
		byNode[e.NodeID+"\x00"+itoa(e.Attempt)] = e
	}

	groups, keys := causalOrder(conts)

	var (
		out          []NodeRecord
		terminalCode string
	)
	for i, key := range keys {
		g := groups[key]
		row := g.rows[0]
		exec := byNode[key]
		nr := NodeRecord{
			Sequence: i + 1, NodeID: row.sourceNodeID, Attempt: row.sourceAttempt,
			StepType: exec.StepType, OutputDigest: exec.OutputArtifactRef,
			ErrorClass: exec.ErrorClass, RecordedAt: g.first,
		}
		if exec.Status == runtime.NodeFailed {
			nr.Failed = true
		}
		for _, r := range g.rows {
			switch {
			case r.kind == frontier.IntentComplete:
				terminalCode = r.terminalCode
			case r.targetNodeID == r.sourceNodeID && r.kind != frontier.IntentReady:
				// The advancement raised an await on the node itself.
				nr.Await = awaitOf(r.kind)
				nr.AwaitRef = r.ref
			case r.routeKey != "" && nr.RouteKey == "":
				nr.RouteKey = r.routeKey
			}
		}
		if nr.RecordedAt.IsZero() && exec.CompletedAt != nil {
			nr.RecordedAt = exec.CompletedAt.UTC()
		}
		out = append(out, nr)
	}
	return out, terminalCode
}

// advancementGroup is every continuation row one advancement raised.
type advancementGroup struct {
	sourceNodeID  string
	sourceAttempt int
	rows          []continuationRow
	first         time.Time
	ledgerOrder   int
}

// causalOrder groups the continuation ledger by advancement and returns the
// groups in the order they must have happened.
//
// It is Kahn's algorithm over the causation the ledger records: a group may be
// emitted once every group that named its source node as a target has been
// emitted. A self-target -- the await an APPROVAL raises on itself, or the
// COMPLETE a terminal raises -- is not a dependency, because a node cannot be
// waiting for itself to have run. Ties between genuinely independent groups
// (parallel branches, which no plan this phase compiles has) fall back to the
// recorded instant and then to the ledger's own row order, so the answer is
// deterministic either way.
//
// A cycle would leave groups unemitted; they are appended in ledger order
// afterwards rather than dropped, so a record this function cannot fully order
// still replays and diverges visibly at the first node out of place, instead
// of silently losing an attempt.
func causalOrder(conts []continuationRow) (map[string]*advancementGroup, []string) {
	groups := map[string]*advancementGroup{}
	var order []string
	for i, row := range conts {
		key := row.sourceNodeID + "\x00" + itoa(row.sourceAttempt)
		g, ok := groups[key]
		if !ok {
			g = &advancementGroup{
				sourceNodeID: row.sourceNodeID, sourceAttempt: row.sourceAttempt,
				first: row.recordedAt, ledgerOrder: i,
			}
			groups[key] = g
			order = append(order, key)
		}
		g.rows = append(g.rows, row)
	}

	// pending counts, per group, how many other groups activated its source
	// node and have not been emitted yet.
	bySourceNode := map[string][]string{}
	for _, key := range order {
		g := groups[key]
		bySourceNode[g.sourceNodeID] = append(bySourceNode[g.sourceNodeID], key)
	}
	activatedBy := map[string]map[string]bool{}
	for _, key := range order {
		g := groups[key]
		for _, row := range g.rows {
			if row.targetNodeID == g.sourceNodeID {
				continue
			}
			for _, target := range bySourceNode[row.targetNodeID] {
				if activatedBy[target] == nil {
					activatedBy[target] = map[string]bool{}
				}
				activatedBy[target][key] = true
			}
		}
	}

	emitted := map[string]bool{}
	out := make([]string, 0, len(order))
	for len(out) < len(order) {
		var ready []string
		for _, key := range order {
			if emitted[key] {
				continue
			}
			blocked := false
			for cause := range activatedBy[key] {
				if !emitted[cause] {
					blocked = true
					break
				}
			}
			if !blocked {
				ready = append(ready, key)
			}
		}
		if len(ready) == 0 {
			// A cycle in the recorded causation. Emit what is left in ledger
			// order rather than dropping it.
			for _, key := range order {
				if !emitted[key] {
					emitted[key] = true
					out = append(out, key)
				}
			}
			break
		}
		sort.SliceStable(ready, func(i, j int) bool {
			a, b := groups[ready[i]], groups[ready[j]]
			if !a.first.Equal(b.first) {
				return a.first.Before(b.first)
			}
			return a.ledgerOrder < b.ledgerOrder
		})
		next := ready[0]
		emitted[next] = true
		out = append(out, next)
	}
	return groups, out
}

// awaitOf maps a raised scheduling intent back to the awaiting-work marker the
// handler must have returned to raise it.
func awaitOf(kind frontier.IntentKind) frontier.AwaitKind {
	switch kind {
	case frontier.IntentWorkItemRequired:
		return frontier.AwaitWorkItem
	case frontier.IntentSignalSubscriptionRequired:
		return frontier.AwaitSignal
	case frontier.IntentTimerRequired:
		return frontier.AwaitTimer
	default:
		return frontier.AwaitNone
	}
}
