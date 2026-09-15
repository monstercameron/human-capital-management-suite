package replay

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// TestTodo_WF_RUN_013_Integration is the ticket's end-to-end case: a real
// Promotion run is executed through internal/workflow/execute against a real
// PostgreSQL, and then replayed from nothing but the rows that run left
// behind.
//
// Everything the replay consumes is read back out of the database by
// [StoreSource] -- the instance, its node executions, its continuation ledger
// and its frontier -- so this is the assertion that the record shape this
// package defines is one the durable runtime actually produces, rather than
// one only its own fixtures can satisfy. The replay must reach the same
// terminal the execution wrote, on the same nodes, in the same order, without
// re-running a single step handler.
func TestTodo_WF_RUN_013_Integration(t *testing.T) {
	f := newExecutedRun(t)

	// The execution really happened: it ran every node of the plan and closed
	// on the escalated terminal.
	if f.result.Status != execute.StatusComplete {
		t.Fatalf("execution status = %s, want %s", f.result.Status, execute.StatusComplete)
	}
	if f.runner.ran() != len(f.executedNodes()) {
		t.Fatalf("step runner ran %d nodes, execution advanced %d", f.runner.ran(), len(f.executedNodes()))
	}

	source := StoreSource{
		Executor: f.conn, TenantID: f.tenantID, InstanceID: f.instanceID,
		HistoricalIntentID: f.intentID,
		Versions:           workflowversionstore.Store{},
	}
	rec := f.load(t, source)

	if rec.CompiledPlanDigest != f.plan.Digest() {
		t.Fatalf("record pins plan %s, want %s", rec.CompiledPlanDigest, f.plan.Digest())
	}
	// Every pin the replay consumes came out of durable evidence: the
	// execution context row, the registry's published version, and the
	// decision's input artifact recorded in its advancement transaction.
	if rec.ExecutionContextDigest == "" || rec.ExecutionContextDigest != f.result.Start.ExecutionContext.Digest() {
		t.Fatalf("record context %q, execution pinned %q", rec.ExecutionContextDigest, f.result.Start.ExecutionContext.Digest())
	}
	if rec.PinnedVersion == nil || rec.PinnedVersion.CompiledPlanDigest != f.plan.Digest() || rec.PinnedVersion.SemanticVersion != "1.0.0" {
		t.Fatalf("record pinned version = %+v", rec.PinnedVersion)
	}
	decisionInputs, ok := rec.NodeInput(workflow.PromotionNodeRaiseThreshold, 1)
	if !ok || len(rec.NodeInputs) != 1 || decisionInputs.ExecutionContextDigest != rec.ExecutionContextDigest {
		t.Fatalf("record node inputs = %+v", rec.NodeInputs)
	}
	if rec.FinalStatus != runtime.InstanceCompleted {
		t.Fatalf("record status = %s, want %s", rec.FinalStatus, runtime.InstanceCompleted)
	}
	if got, want := len(rec.Nodes), len(f.executedNodes()); got != want {
		t.Fatalf("record holds %d attempts, execution advanced %d", got, want)
	}
	for i, want := range f.executedNodes() {
		if rec.Nodes[i].NodeID != want {
			t.Fatalf("recorded attempt %d is %s, execution advanced %s", i, rec.Nodes[i].NodeID, want)
		}
		if rec.Nodes[i].Sequence != i+1 {
			t.Fatalf("recorded attempt %d carries sequence %d", i, rec.Nodes[i].Sequence)
		}
	}

	res, err := f.replay(t, source, f.plan, &reachingAdapter{})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Status != StatusComplete {
		t.Fatalf("replay status = %s, want %s (divergence %v)", res.Status, StatusComplete, res.Divergence)
	}
	if res.Trace.TerminalCode != promotionEscalatedTerminal {
		t.Fatalf("replayed terminal = %q, want %q", res.Trace.TerminalCode, promotionEscalatedTerminal)
	}
	if got := nodeIDs(res.Trace); !equalStrings(got, f.executedNodes()) {
		t.Fatalf("replayed %v, executed %v", got, f.executedNodes())
	}
	if len(res.Refusals) != 0 {
		t.Fatalf("replay of a real run attempted effects: %+v", res.Refusals)
	}
	// The replay ran no step handler: the recorded outputs were consumed, and
	// the runner's counter has not moved since the execution.
	if f.runner.ran() != len(f.executedNodes()) {
		t.Fatalf("replay re-ran %d step handlers", f.runner.ran()-len(f.executedNodes()))
	}

	// The route keys the replay used are the ones the execution's own step
	// handler produced. The DECISION's was recomputed by evaluating the pinned
	// rule table against its pinned inputs and matched the production port's
	// recorded route and output digest; the others were read back out of the
	// continuation ledger.
	for _, entry := range res.Trace.Entries {
		if want, ok := f.runner.routes[entry.NodeID]; ok && string(want) != entry.RouteKey {
			t.Fatalf("%s replayed route %q, executed %q", entry.NodeID, entry.RouteKey, want)
		}
		wantSource := SourceRecordedOutput
		if entry.NodeID == workflow.PromotionNodeRaiseThreshold {
			wantSource = SourceRecomputed
			if entry.InputDigest != decisionInputs.Digest() || entry.OutputDigest != f.runner.decisionDigest {
				t.Fatalf("decision entry = %+v, want inputs %s and production output %s",
					entry, decisionInputs.Digest(), f.runner.decisionDigest)
			}
		}
		if entry.Source != wantSource {
			t.Fatalf("%s source = %s, want %s", entry.NodeID, entry.Source, wantSource)
		}
	}

	// A candidate that disagrees with the production implementation is caught
	// at the decision on the real rows too.
	divergent, err := f.replayWith(t, source, Candidates{ByNode: map[string]Candidate{
		workflow.PromotionNodeRaiseThreshold: divergingCandidate{route: "WITHIN_THRESHOLD", digest: f.runner.decisionDigest},
	}})
	if CodeOf(err) != CodeDivergence || divergent.Divergence == nil || divergent.Divergence.NodeID != workflow.PromotionNodeRaiseThreshold {
		t.Fatalf("diverging candidate on durable rows: err=%v divergence=%v", err, divergent.Divergence)
	}

	// Pinning the trace digest and replaying again reproduces it exactly: the
	// database path is as deterministic as the in-memory one.
	pinned := source
	pinned.TraceDigest = res.Trace.Digest()
	second, err := f.replay(t, pinned, f.plan, nil)
	if err != nil {
		t.Fatalf("second replay: %v", err)
	}
	if !second.DigestMatches {
		t.Fatalf("second replay digest %s does not match the pinned %s",
			second.Trace.Digest(), res.Trace.Digest())
	}

	// And a replay of the same rows against a different compiled plan is
	// refused rather than quietly re-derived under a migration.
	if _, err := f.replay(t, source, promotionPlanWithoutDrift(t), nil); CodeOf(err) != CodePlanMismatch {
		t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodePlanMismatch)
	}

	// A registry that never published the pinned plan cannot vouch for it.
	unpublished := f.load(t, StoreSource{Executor: f.conn, TenantID: f.tenantID, InstanceID: f.instanceID, HistoricalIntentID: f.intentID})
	if unpublished.PinnedVersion != nil {
		t.Fatalf("a source with no registry produced a pinned version")
	}
	err = inTenantTxErr(f.conn, f.tenantID, func(tx dbport.Tx) error {
		_, err := StoreSource{Executor: tx, TenantID: f.tenantID, InstanceID: f.instanceID,
			Versions: version.NewRegistry()}.Load(context.Background())
		return err
	})
	if CodeOf(err) != CodeArtifactUnavailable {
		t.Fatalf("empty registry: code = %q (%v), want %s", CodeOf(err), err, CodeArtifactUnavailable)
	}

	// Replay wrote nothing: the historical evidence it read is unchanged.
	var inputRows int
	var inputDigest string
	if err := f.db.QueryRow(context.Background(), `SELECT count(*), max(input_digest) FROM workflow_node_input_artifact WHERE tenant_id = $1`,
		f.tenantID).Scan(&inputRows, &inputDigest); err != nil {
		t.Fatalf("read input artifacts: %v", err)
	}
	if inputRows != 1 || inputDigest != decisionInputs.Digest() {
		t.Fatalf("input artifacts after replay: %d rows, digest %s", inputRows, inputDigest)
	}
}

// --- the executed run ------------------------------------------------------

// promotionEscalatedTerminal is the terminal a grade-changing management
// promotion reaches: the reference threshold table escalates it to a pending
// Finance Partner approval.
const promotionEscalatedTerminal = "SIMULATION_APPROVAL_REQUIRED"

// executedRun is one real Promotion execution, committed to PostgreSQL.
type executedRun struct {
	db         *pgtest.DB
	conn       *pgxadapter.Conn
	tenantID   uuid.UUID
	instanceID uuid.UUID
	intentID   string
	plan       *workflow.CompiledWorkflow
	runner     *promotionRunner
	result     execute.Result
}

func newExecutedRun(t *testing.T) *executedRun {
	t.Helper()
	db := pgtest.New(t)
	tenantID := uuid.New()
	at := time.Date(2026, 9, 3, 15, 0, 0, 0, time.UTC)
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'WF-RUN-013 tenant', 'ACTIVE', $3)`,
		tenantID, "wfrun013-"+tenantID.String(), at.Add(-time.Hour))
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}

	plan := promotionPlan(t)
	const (
		intentID   = "intent:wfrun013:promotion:1"
		revisionID = "proposal:wfrun013:promotion:1"
		subject    = "worker:jane-doe"
	)
	proposal := intent.ProposalRevision{
		IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1,
		CreatedBy: intent.PrincipalReference{PrincipalID: "principal:test-initiator", Kind: intent.InitiatorHuman},
		Subjects: []intent.SubjectReference{{
			Kind: "WORKER", SubjectID: subject, AuthorityDomain: "people",
		}},
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1,
			SchemaID: "hcmnext.intent.ProposalRevision", SchemaVersion: 1,
			AlgorithmID: "sha256", CanonicalLength: 42,
			Digest:             "sha256:" + strings.Repeat("a", 64),
			ScopeBindingDigest: "sha256:" + strings.Repeat("b", 64),
		},
	}

	runner := newPromotionRunner()
	driver, err := execute.New(execute.Options{
		DB: conn, Steps: runner, Terminal: recordingTerminal{},
		Guard:     idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 24 * time.Hour, RetryWindow: time.Hour},
		Clock:     func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}

	result, err := driver.Execute(context.Background(), execute.ExecuteRequest{
		Start: runtime.StartRequest{
			TenantID: tenantID, CellID: "cell-local",
			StartIdempotencyKey: "start:wfrun013:promotion:1",
			Resolver: fixedSelection{selection: runtime.WorkflowSelection{
				WorkflowID: plan.WorkflowID, Pin: version.Pin{SemanticVersion: "1.0.0"}, Plan: plan,
			}},
			Versions: fixedVersions{record: version.CompiledVersion{
				WorkflowID: plan.WorkflowID, SemanticVersion: "1.0.0",
				CompiledPlanDigest: plan.Digest(), Status: version.StatusActive,
			}},
			Proposal: runtime.ProposalBinding{
				Revision: proposal, ApprovalRef: "approval:wfrun013:manager",
			},
			ProposalFacts:       runtime.MemoryProposalFacts{},
			ApprovalFacts:       runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{proposal.ProposalRevisionID: {{DecisionID: "approval:wfrun013:manager", Outcome: runtime.ApprovalOutcomeApproved, ProposalDigest: proposal.MaterialDigest.Digest}}}},
			BusinessSubjectRefs: []string{subject},
			ResolvedContext:     map[string]string{"LegalContext": "legal-context:wfrun013/v1"},
			ExecutionMode:       workflow.ModeExecute,
			CorrelationID:       "correlation:wfrun013:promotion",
			CreatedAt:           at,
		},
	})
	if err != nil {
		t.Fatalf("execute the Promotion run: %v", err)
	}

	// Publish the plan to the durable version registry, as a real deployment
	// would have before starting an instance on it.
	withTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		registry, err := newBootstrapRegistry()
		if err != nil {
			return err
		}
		_, err = version.Publish(workflowversionstore.Store{}.BindTx(context.Background(), tx),
			workflow.PromotionReferenceDefinition(), plan,
			workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry},
			version.PublishMeta{SemanticVersion: "1.0.0", PublishedAt: at, PublishedBy: "principal:wfrun013-publisher"})
		return err
	})

	return &executedRun{
		db: db, conn: conn, tenantID: tenantID, instanceID: result.Start.InstanceID,
		intentID: intentID, plan: plan, runner: runner, result: result,
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	return fn(tx)
}

// replayWith runs one replay with caller-supplied candidates inside a
// tenant-scoped transaction.
func (f *executedRun) replayWith(t *testing.T, source StoreSource, candidates Candidates) (Result, error) {
	t.Helper()
	var (
		res  Result
		rerr error
	)
	err := inTenantTxErr(f.conn, f.tenantID, func(tx dbport.Tx) error {
		scoped := source
		scoped.Executor = tx
		r, err := New(Options{
			Plan: f.plan, Source: scoped, Contract: replayContract(t),
			Definition: replayDefinition(), Instance: f.replayIdentity(), Candidates: candidates,
		})
		if err != nil {
			return err
		}
		res, rerr = r.Replay(context.Background())
		return nil
	})
	if err != nil {
		t.Fatalf("replay with candidates: %v", err)
	}
	return res, rerr
}

// executedNodes is the node order the execution actually advanced, taken from
// its own advancement receipts.
func (f *executedRun) executedNodes() []string {
	out := make([]string, 0, len(f.result.Advances))
	for _, a := range f.result.Advances {
		out = append(out, a.NodeID)
	}
	return out
}

// replayIdentity is the replay's own intent: a new one that names the executed
// run as its cause.
func (f *executedRun) replayIdentity() intent.Instance {
	cause := f.intentID
	return intent.Instance{
		IntentID:      "intent:wfrun013:replay:1",
		CausationID:   &cause,
		ExecutionMode: intent.ModeReplay,
	}
}

// load reads the record inside a tenant-scoped transaction, which is what the
// row-level security policies on the runtime tables require of any reader.
func (f *executedRun) load(t *testing.T, source StoreSource) Record {
	t.Helper()
	var rec Record
	withTenantTx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		scoped := source
		scoped.Executor = tx
		var err error
		rec, err = scoped.Load(context.Background())
		return err
	})
	return rec
}

// replay runs one replay inside a tenant-scoped transaction, which is what
// the row-level security policies on the runtime tables require of any reader:
// internal/data/tenancy.WithTenant sets the tenant with set_config(..., true),
// and a local setting only exists inside a transaction.
func (f *executedRun) replay(
	t *testing.T,
	source StoreSource,
	plan *workflow.CompiledWorkflow,
	adapter NodeAdapter,
) (Result, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, f.tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	scoped := source
	scoped.Executor = tx
	r, err := New(Options{
		Plan: plan, Source: scoped, Contract: replayContract(t),
		Definition: replayDefinition(), Instance: f.replayIdentity(), Adapter: adapter,
	})
	if err != nil {
		t.Fatalf("New replayer: %v", err)
	}
	return r.Replay(ctx)
}

func withTenantTx(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatalf("transaction body: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// promotionRunner is the execution's step handler. It produces a fixed outcome
// per node -- the grade-changing promotion that escalates past the finance
// threshold -- and counts how many times it was called, so the replay can be
// asserted never to have called it.
type promotionRunner struct {
	routes map[string]workflow.Outcome
	calls  int
	// decisionDigest is the output digest the production threshold port
	// produced for the DECISION, recorded when it ran.
	decisionDigest string
}

var _ execute.TransactionalStepRunner = (*promotionRunner)(nil)

// RunsInTransaction runs the DECISION inside the advancement transaction, so
// the pinned inputs it records commit atomically with its outcome.
func (p *promotionRunner) RunsInTransaction(node workflow.CompiledNode) bool {
	return node.Type == workflow.StepDecision
}

// RunInTx evaluates the raise threshold through the production RULE-003 port
// and records the exact inputs and table version it evaluated as the node's
// pinned input artifact.
func (p *promotionRunner) RunInTx(ctx context.Context, ex runtime.Executor, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	p.calls++
	in := fixtureDecisionInput()
	port := promotionsteps.RulesThresholdPort{
		Inputs: func(context.Context, execute.StepRequest) (rules.PromotionApprovalInput, error) { return in, nil },
	}
	res, err := port.RaiseThreshold(ctx, req)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	route := workflow.Outcome("WITHIN_THRESHOLD")
	if res.Tier == rules.ApprovalTierFinanceRequired || res.Tier == rules.ApprovalTierExecutiveRequired {
		route = "EXCEEDS_THRESHOLD"
	}
	table := rules.PromotionApprovalThresholdTable()
	tableDigest, err := table.Digest()
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	if _, err := runtime.RecordNodeInputs(ctx, ex, runtime.NodeInputArtifact{
		TenantID: req.TenantID, InstanceID: req.InstanceID, NodeID: req.Node.ID, Attempt: req.Attempt,
		StepType: req.Node.Type, PlanDigest: req.Plan.Digest(),
		Inputs: []runtime.NodeInputValue{
			{Path: rules.ColumnIncreasePercent, Value: in.IncreasePercent.String()},
			{Path: rules.ColumnBandPosition, Value: string(in.BandPosition)},
			{Path: rules.ColumnBudgetAuthority, Value: string(in.BudgetAuthority)},
			{Path: rules.ColumnGradeChange, Value: "true"},
		},
		Versions: []runtime.PinnedArtifactVersion{{
			Kind: runtime.PinnedVersionRuleTable, Ref: table.ID, Version: table.Version, Digest: tableDigest,
		}},
		RecordedAt: req.RecordedAt,
	}); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	p.decisionDigest = res.OutputDigest
	return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: route, OutputDigest: res.OutputDigest}, res.Refs, nil
}

func newPromotionRunner() *promotionRunner {
	return &promotionRunner{routes: map[string]workflow.Outcome{
		workflow.PromotionNodeSnapshotWorker: workflow.OutcomeSucceeded,
		workflow.PromotionNodeSimulateComp:   workflow.OutcomeSucceeded,
		workflow.PromotionNodeEvaluateBand:   workflow.OutcomeSucceeded,
		workflow.PromotionNodeBuildProposal:  workflow.OutcomeSucceeded,
		workflow.PromotionNodeRaiseThreshold: "EXCEEDS_THRESHOLD",
	}}
}

func (p *promotionRunner) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	p.calls++
	out := frontier.NodeOutcome{NodeID: req.Node.ID, OutputDigest: digestOf(req.Node.ID)}
	if req.Node.Type == workflow.StepEnd {
		return out, runtime.GovernanceRefs{}, nil
	}
	route, ok := p.routes[req.Node.ID]
	if !ok {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{},
			errors.New("no recorded outcome for node " + req.Node.ID)
	}
	out.Outcome = route
	return out, runtime.GovernanceRefs{}, nil
}

func (p *promotionRunner) ran() int { return p.calls }

// recordingTerminal is the governed terminal write. It performs none: this
// test is about the runtime rows, and a business write is another ticket's
// subject.
type recordingTerminal struct{}

var _ execute.TerminalWriter = recordingTerminal{}

func (recordingTerminal) Write(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	return idempotency.ResultIdentity{ResultRef: "result:wfrun013:promotion"}, nil
}

type fixedSelection struct{ selection runtime.WorkflowSelection }

func (r fixedSelection) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return r.selection, nil
}

type fixedVersions struct{ record version.CompiledVersion }

func (s fixedVersions) Put(version.CompiledVersion) error { return nil }
func (s fixedVersions) GetByDigest(string) (version.CompiledVersion, bool, error) {
	return s.record, true, nil
}
func (s fixedVersions) GetActiveForWorkflow(string) (version.CompiledVersion, bool, error) {
	return s.record, true, nil
}
func (s fixedVersions) List(string) ([]version.CompiledVersion, error) {
	return []version.CompiledVersion{s.record}, nil
}

// promotionPlanWithoutDrift compiles a second, materially different Promotion
// plan so the plan-mismatch refusal is proved against a real alternative
// rather than against a corrupted digest string.
func promotionPlanWithoutDrift(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan := promotionPlan(t)
	// Recompiling the same definition at a bumped version is the smallest
	// honest "different plan": same graph, different identity, different
	// digest.
	def := workflow.PromotionReferenceDefinition()
	def.Version++
	registry, err := newBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap capability registry: %v", err)
	}
	other, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry})
	if err != nil {
		t.Fatalf("compile the alternative plan: %v", err)
	}
	if other.Digest() == plan.Digest() {
		t.Fatalf("the alternative plan digests identically to the original")
	}
	return other
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
