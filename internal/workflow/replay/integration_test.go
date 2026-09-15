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
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
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
	}
	rec := f.load(t, source)

	if rec.CompiledPlanDigest != f.plan.Digest() {
		t.Fatalf("record pins plan %s, want %s", rec.CompiledPlanDigest, f.plan.Digest())
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
	// handler produced, read back out of the continuation ledger rather than
	// recomputed.
	for _, entry := range res.Trace.Entries {
		if want, ok := f.runner.routes[entry.NodeID]; ok && string(want) != entry.RouteKey {
			t.Fatalf("%s replayed route %q, executed %q", entry.NodeID, entry.RouteKey, want)
		}
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

	return &executedRun{
		db: db, conn: conn, tenantID: tenantID, instanceID: result.Start.InstanceID,
		intentID: intentID, plan: plan, runner: runner, result: result,
	}
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
}

var _ execute.StepRunner = (*promotionRunner)(nil)

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
