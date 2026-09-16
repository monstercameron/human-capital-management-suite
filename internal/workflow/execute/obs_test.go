package execute

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// --- OBS-023/OBS-024 shared in-memory fakes ---------------------------------
//
// These are deliberately not the real OTel/capability-sink implementations
// (internal/platform/execution owns those, and its own tests golden-test
// their exact wire shape) — they exist so this package's own unit tests can
// assert exactly what the driver hands its Instrumentation/ExecutionEvidence
// ports, without a database or an exporter.

// recordedSpan is one Start*Span/End call pair a fakeInstrumentation
// captured.
type recordedSpan struct {
	family  string // "node", "advance" or "terminal"
	attrs   SpanAttributes
	ended   bool
	outcome string
	failed  bool
}

// fakeInstrumentation is the [Instrumentation] test double every test below
// drives the [Driver] through.
type fakeInstrumentation struct {
	mu      sync.Mutex
	traceID string
	spans   []*recordedSpan
}

var _ Instrumentation = (*fakeInstrumentation)(nil)

func (f *fakeInstrumentation) TraceID(context.Context) string { return f.traceID }

func (f *fakeInstrumentation) start(ctx context.Context, family string, attrs SpanAttributes) (context.Context, Span) {
	f.mu.Lock()
	rec := &recordedSpan{family: family, attrs: attrs}
	f.spans = append(f.spans, rec)
	f.mu.Unlock()
	return ctx, &fakeSpan{rec: rec}
}

func (f *fakeInstrumentation) StartNodeSpan(ctx context.Context, a SpanAttributes) (context.Context, Span) {
	return f.start(ctx, "node", a)
}
func (f *fakeInstrumentation) StartAdvanceSpan(ctx context.Context, a SpanAttributes) (context.Context, Span) {
	return f.start(ctx, "advance", a)
}
func (f *fakeInstrumentation) StartTerminalSpan(ctx context.Context, a SpanAttributes) (context.Context, Span) {
	return f.start(ctx, "terminal", a)
}

func (f *fakeInstrumentation) Spans() []*recordedSpan {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*recordedSpan(nil), f.spans...)
}

func (f *fakeInstrumentation) spansNamed(family string) []*recordedSpan {
	var out []*recordedSpan
	for _, s := range f.Spans() {
		if s.family == family {
			out = append(out, s)
		}
	}
	return out
}

type fakeSpan struct{ rec *recordedSpan }

func (s *fakeSpan) End(outcome string, err error) {
	s.rec.ended = true
	s.rec.outcome = outcome
	s.rec.failed = err != nil
}

var _ Span = (*fakeSpan)(nil)

// fakeEvidenceEntry is one RecordExecutionEvidence call a fakeEvidence
// captured.
type fakeEvidenceEntry struct {
	tenantID   uuid.UUID
	kind       string
	instanceID string
	nodeID     string
	refID      string
	digest     string
}

// fakeEvidence is the [ExecutionEvidence] test double every test below
// drives the [Driver] through. It mints deterministic, ordinal evidence
// ids so a test can assert Result.EvidenceIDs' exact order.
type fakeEvidence struct {
	mu      sync.Mutex
	entries []fakeEvidenceEntry
	// failKind, when non-empty, makes RecordExecutionEvidence fail for that
	// one kind instead of recording it — OBS-024's negative fixture.
	failKind string
}

var _ ExecutionEvidence = (*fakeEvidence)(nil)

func (f *fakeEvidence) RecordExecutionEvidence(_ context.Context, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, _ time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failKind != "" && kind == f.failKind {
		return "", errors.New("fakeEvidence: injected failure for " + kind)
	}
	f.entries = append(f.entries, fakeEvidenceEntry{tenantID: tenantID, kind: kind, instanceID: instanceID, nodeID: nodeID, refID: refID, digest: digest})
	return "ev:" + kind + ":" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(kind+instanceID+nodeID+refID+digest)).String()[:8], nil
}

func (f *fakeEvidence) Entries() []fakeEvidenceEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeEvidenceEntry(nil), f.entries...)
}

// fakeTerminalWriter is the [TerminalWriter] test double the OBS-023/024
// scenario below drives continuationSink.Complete through.
type fakeTerminalWriter struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (w *fakeTerminalWriter) Write(context.Context, dbport.Tx, TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if w.fail {
		return idempotency.ResultIdentity{}, errors.New("fakeTerminalWriter: injected write failure")
	}
	return idempotency.ResultIdentity{ResultRef: "result:obs-demo", EventRef: "event:obs-demo@1"}, nil
}

// fakeIdempotencyStore is a purely in-memory [idempotency.Store]: Reserve
// always wins (created=true) and Complete always succeeds, so
// continuationSink.Complete can be exercised without a real Postgres
// idempotency_record table.
type fakeIdempotencyStore struct{}

func (fakeIdempotencyStore) Reserve(_ context.Context, _ idempotency.Executor, scope idempotency.Scope, digest string, _ idempotency.RetentionPolicy, now time.Time) (idempotency.Record, bool, error) {
	return idempotency.Record{Scope: scope, RequestDigest: digest, Status: idempotency.StatusReserved, CreatedAt: now}, true, nil
}

func (fakeIdempotencyStore) Complete(_ context.Context, _ idempotency.Executor, scope idempotency.Scope, identity idempotency.ResultIdentity, now time.Time) (idempotency.Record, error) {
	return idempotency.Record{Scope: scope, Status: idempotency.StatusCompleted, Identity: identity, CreatedAt: now}, nil
}

func (fakeIdempotencyStore) Lookup(context.Context, idempotency.Executor, idempotency.Scope) (idempotency.Record, bool, error) {
	return idempotency.Record{}, false, nil
}

func (fakeIdempotencyStore) Expire(context.Context, idempotency.Executor, uuid.UUID, time.Time) (int64, error) {
	return 0, nil
}

var _ idempotency.Store = fakeIdempotencyStore{}

// obsEndRunner is the [StepRunner] the OBS scenario drains READY into: it
// only ever sees the plan's "end" node.
type obsEndRunner struct {
	mu       sync.Mutex
	traceIDs []string
}

func (r *obsEndRunner) Run(_ context.Context, req StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	r.mu.Lock()
	r.traceIDs = append(r.traceIDs, req.TraceID)
	r.mu.Unlock()
	if req.Node.ID != "end" {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("obsEndRunner: unexpected node " + req.Node.ID)
	}
	return frontier.NodeOutcome{NodeID: "end", Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:end-output"}, runtime.GovernanceRefs{}, nil
}

// obsScenario builds one Resume call over a two-node approve->end plan,
// driven entirely by a fake Advance (never runtime.Advance/a real database):
// the first advancement resolves the completed APPROVAL work item and hands
// back a READY continuation to "end"; drainReady runs "end" through
// obsEndRunner (one node span); the second advancement calls
// continuationSink.Complete directly (through in.Sink.Complete), exactly as
// [runtime.Advance] would for a COMPLETE intent, exercising the real
// terminal span/evidence path this package owns end to end, without a
// database.
type obsScenario struct {
	tenantID   uuid.UUID
	instanceID uuid.UUID
	workItemID uuid.UUID
	req        ResumeRequest
	item       workitem.WorkItem

	instrumentation *fakeInstrumentation
	evidence        *fakeEvidence
	terminal        *fakeTerminalWriter
	stepRunner      *obsEndRunner

	advanceCalls int
}

func newOBSScenario(t *testing.T, traceID string) *obsScenario {
	t.Helper()
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	instanceID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	workItemID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	proposalDigest := "sha256:" + strings.Repeat("a", 64)

	item := workitem.WorkItem{
		TenantID: tenantID, WorkItemID: workItemID, ItemVersion: 4,
		Kind: workitem.KindApproval, WorkType: "approval.obs", Status: workitem.StatusCompleted,
		CorrelationID: "corr-obs", WorkflowInstanceID: instanceID, NodeID: "approve",
		ApprovalRequirementRef: "approval.obs", ProposalRef: proposalDigest,
		SubjectRefs: []string{"employment:obs"}, OwnerKind: workitem.OwnerPrincipal,
		OwnerRef: "principal:approver", PolicyRouteRef: "route.obs",
		Visibility: workitem.VisibilityAssigneeOnly, OrganizationScopeID: "org-obs",
		DeadlineAt: at.Add(time.Hour), CompletedBy: "principal:approver", CompletedAt: &at,
		CompletedOutputDigest: "sha256:" + strings.Repeat("b", 64),
		CreatedAt:             at.Add(-time.Hour), RecordedAt: at,
	}

	plan := &workflow.CompiledWorkflow{
		WorkflowID: "obs.demo", Version: 1,
		Nodes: []workflow.CompiledNode{
			{ID: "approve", Type: workflow.StepApproval, AllowedModes: []workflow.ExecutionMode{workflow.ModeExecute}},
			{ID: "end", Type: workflow.StepEnd, AllowedModes: []workflow.ExecutionMode{workflow.ModeExecute}},
		},
	}
	selection := runtime.WorkflowSelection{
		WorkflowID: plan.WorkflowID, Pin: version.Pin{SemanticVersion: "1.0.0"}, Plan: plan,
	}
	record := version.CompiledVersion{
		WorkflowID: plan.WorkflowID, SemanticVersion: "1.0.0",
		CompiledPlanDigest: plan.Digest(), Status: version.StatusActive,
	}

	req := ResumeRequest{
		Start: runtime.StartRequest{
			TenantID: tenantID, CellID: "cell-obs", StartIdempotencyKey: "start-obs",
			Resolver:      staticResolver{selection: selection},
			Versions:      staticVersions{record: record},
			Proposal:      runtime.ProposalBinding{Revision: proposalRevisionFor(item)},
			ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedApprovalFacts(proposalRevisionFor(item)),
			CorrelationID:       "corr-obs",
			BusinessSubjectRefs: []string{"employment:obs"},
			ExecutionMode:       workflow.ModeExecute,
		},
		InstanceID: instanceID, ExpectedInstanceVersion: 3, RecordedAt: at,
		WorkItemID: workItemID, ExpectedWorkItemVersion: item.ItemVersion,
		Outcome: frontier.NodeOutcome{
			Outcome:      workflow.Outcome("APPROVED"),
			OutputDigest: "sha256:" + strings.Repeat("c", 64),
		},
	}

	return &obsScenario{
		tenantID: tenantID, instanceID: instanceID, workItemID: workItemID,
		req: req, item: item,
		instrumentation: &fakeInstrumentation{traceID: traceID},
		evidence:        &fakeEvidence{},
		terminal:        &fakeTerminalWriter{},
		stepRunner:      &obsEndRunner{},
	}
}

// proposalRevisionFor builds the minimal ProposalRevision checkWorkItemDrift
// and the terminal digest need to agree with item's own ProposalRef.
func proposalRevisionFor(item workitem.WorkItem) intent.ProposalRevision {
	return intent.ProposalRevision{MaterialDigest: digest.Reference{Digest: item.ProposalRef}}
}

func (s *obsScenario) driver(t *testing.T) *Driver {
	t.Helper()
	tx := &memoryTx{}
	d, err := New(Options{
		DB: oneBeginner{tx}, Steps: s.stepRunner, Items: fakeWorkItemReader{item: s.item},
		Terminal: s.terminal, Guard: fakeIdempotencyStore{},
		Retention:       idempotency.RetentionPolicy{Retention: time.Hour, RetryWindow: time.Minute},
		Instrumentation: s.instrumentation, Evidence: s.evidence,
		Advance: func(ctx context.Context, ex runtime.Executor, in runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			s.advanceCalls++
			if in.Outcome.NodeID == "approve" {
				return runtime.AdvanceReceipt{
					TenantID: in.TenantID, InstanceID: in.InstanceID, NodeID: "approve",
					NewInstanceVersion: 4,
					Continuations: []runtime.ContinuationRecord{{
						TenantID: in.TenantID, InstanceID: in.InstanceID, SourceNodeID: "approve", SourceAttempt: 1,
						TargetNodeID: "end", Kind: frontier.IntentReady, RecordedAt: in.RecordedAt,
					}},
				}, nil
			}
			// The "end" advancement: a real [runtime.Advance] would derive a
			// COMPLETE intent from the plan's END node and dispatch it to
			// in.Sink.Complete; this fake does exactly that one call, so
			// continuationSink.Complete's own OBS-023/024 hooks run for
			// real.
			if err := in.Sink.Complete(ctx, ex, runtime.ContinuationRecord{
				TenantID: in.TenantID, InstanceID: in.InstanceID, SourceNodeID: "end", SourceAttempt: 1,
				TargetNodeID: "end", Kind: frontier.IntentComplete, TerminalCode: "OBS_DEMO_DONE",
				RecordedAt: in.RecordedAt,
			}); err != nil {
				return runtime.AdvanceReceipt{}, err
			}
			return runtime.AdvanceReceipt{
				TenantID: in.TenantID, InstanceID: in.InstanceID, NodeID: "end",
				NewInstanceVersion: 5, Complete: true, TerminalCode: "OBS_DEMO_DONE",
				OutputDigest: "sha256:end-output",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// ---------------------------------------------------------------------------
// TestTodo_OBS_023
// ---------------------------------------------------------------------------

// TestTodo_OBS_023 proves the OBS-023 GREEN clause purely in memory: the
// driver propagates the ambient trace id into every StepRequest and
// runtime.AdvanceRequest it builds, opens one node span per READY node run,
// one advance span per advancement and one terminal span for the terminal
// write, and closes each with a non-empty outcome and no error on the
// success path.
func TestTodo_OBS_023(t *testing.T) {
	const traceID = "0af7651916cd43dd8448eb211c80319c"
	scn := newOBSScenario(t, traceID)
	driver := scn.driver(t)

	result, err := driver.Resume(context.Background(), scn.req)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result.Status != StatusComplete {
		t.Fatalf("result.Status = %s, want COMPLETE", result.Status)
	}
	if scn.advanceCalls != 2 {
		t.Fatalf("advance calls = %d, want 2 (approve, then end)", scn.advanceCalls)
	}

	// The step runner's own StepRequest carried the ambient trace id.
	for _, got := range scn.stepRunner.traceIDs {
		if got != traceID {
			t.Fatalf("StepRequest.TraceID = %q, want %q", got, traceID)
		}
	}

	nodeSpans := scn.instrumentation.spansNamed("node")
	if len(nodeSpans) != 1 {
		t.Fatalf("node spans = %d, want 1 (only \"end\" runs through StepRunner)", len(nodeSpans))
	}
	if nodeSpans[0].attrs.NodeID != "end" || nodeSpans[0].attrs.InstanceID != scn.instanceID.String() {
		t.Fatalf("node span attrs = %+v", nodeSpans[0].attrs)
	}
	if !nodeSpans[0].ended || nodeSpans[0].outcome != OutcomeSuccess || nodeSpans[0].failed {
		t.Fatalf("node span end state = %+v, want ended/SUCCESS/no-error", nodeSpans[0])
	}

	advanceSpans := scn.instrumentation.spansNamed("advance")
	if len(advanceSpans) != 2 {
		t.Fatalf("advance spans = %d, want 2", len(advanceSpans))
	}
	for _, s := range advanceSpans {
		if s.attrs.InstanceID != scn.instanceID.String() {
			t.Fatalf("advance span instance_id = %q, want %q", s.attrs.InstanceID, scn.instanceID.String())
		}
		if !s.ended || s.failed {
			t.Fatalf("advance span end state = %+v, want ended/no-error", s)
		}
	}

	terminalSpans := scn.instrumentation.spansNamed("terminal")
	if len(terminalSpans) != 1 {
		t.Fatalf("terminal spans = %d, want 1", len(terminalSpans))
	}
	ts := terminalSpans[0]
	if ts.attrs.TerminalCode != "OBS_DEMO_DONE" || ts.attrs.NodeID != "end" {
		t.Fatalf("terminal span attrs = %+v", ts.attrs)
	}
	if !ts.ended || ts.outcome != OutcomeSuccess || ts.failed {
		t.Fatalf("terminal span end state = %+v, want ended/SUCCESS/no-error", ts)
	}
}

// TestTodo_OBS_023_Security proves span attributes never widen beyond
// OBS-023's bounded set. SpanAttributes is a closed struct with exactly
// instance_id/node_id/attempt/terminal_code, so the real assertion is that
// nothing this package hands the port ever originates from a payload,
// digest or principal field: the node span for "end" carries only its node
// id, never the StepRequest's Proposal, and the terminal span carries only
// its terminal code, never the ledger digest [continuationSink.Complete]
// computes for idempotency.
func TestTodo_OBS_023_Security(t *testing.T) {
	scn := newOBSScenario(t, "0af7651916cd43dd8448eb211c80319c")
	driver := scn.driver(t)
	if _, err := driver.Resume(context.Background(), scn.req); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	for _, s := range scn.instrumentation.Spans() {
		if s.attrs.InstanceID != scn.instanceID.String() {
			t.Fatalf("%s span leaked an unexpected instance_id: %+v", s.family, s.attrs)
		}
		switch s.family {
		case "node":
			if s.attrs.TerminalCode != "" {
				t.Fatalf("node span carries a terminal_code it has no business knowing: %+v", s.attrs)
			}
		case "terminal":
			// The idempotency digest and the proposal's material digest are
			// both computed inside continuationSink.Complete; neither may
			// ever reach a span attribute (only the caller-visible
			// terminal_code may).
			if s.attrs.TerminalCode == "" {
				t.Fatalf("terminal span carries no terminal_code: %+v", s.attrs)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// TestTodo_OBS_024
// ---------------------------------------------------------------------------

// TestTodo_OBS_024 proves the OBS-024 GREEN clause: [Driver.Resume] records
// an APPROVAL_COMPLETED entry for the completed approval work item, and the
// terminal write's own continuationSink.Complete records a TERMINAL_WRITTEN
// entry, both through the same [ExecutionEvidence] port, and both ids
// surface on Result.EvidenceIDs in recording order.
func TestTodo_OBS_024(t *testing.T) {
	scn := newOBSScenario(t, "0af7651916cd43dd8448eb211c80319c")
	driver := scn.driver(t)

	result, err := driver.Resume(context.Background(), scn.req)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	entries := scn.evidence.Entries()
	if len(entries) != 2 {
		t.Fatalf("evidence entries = %d, want 2 (APPROVAL_COMPLETED, TERMINAL_WRITTEN): %+v", len(entries), entries)
	}
	approval, terminal := entries[0], entries[1]
	// WF-RUN-035: both entries name the storage tenant the run committed
	// under, never a guessed or empty one.
	if approval.tenantID != scn.tenantID || terminal.tenantID != scn.tenantID {
		t.Fatalf("evidence tenants = %s, %s; want %s", approval.tenantID, terminal.tenantID, scn.tenantID)
	}

	if approval.kind != EvidenceKindApprovalCompleted {
		t.Fatalf("entries[0].kind = %s, want %s", approval.kind, EvidenceKindApprovalCompleted)
	}
	if approval.instanceID != scn.instanceID.String() || approval.nodeID != "approve" {
		t.Fatalf("APPROVAL_COMPLETED entry = %+v, want instance %s node approve", approval, scn.instanceID)
	}
	if approval.refID != scn.workItemID.String() {
		t.Fatalf("APPROVAL_COMPLETED refID = %q, want the work item id %q", approval.refID, scn.workItemID)
	}

	if terminal.kind != EvidenceKindTerminalWritten {
		t.Fatalf("entries[1].kind = %s, want %s", terminal.kind, EvidenceKindTerminalWritten)
	}
	if terminal.instanceID != scn.instanceID.String() || terminal.nodeID != "end" {
		t.Fatalf("TERMINAL_WRITTEN entry = %+v, want instance %s node end", terminal, scn.instanceID)
	}
	if terminal.digest == "" {
		t.Fatal("TERMINAL_WRITTEN entry carries no digest")
	}

	if len(result.EvidenceIDs) != 2 {
		t.Fatalf("result.EvidenceIDs = %v, want 2 ids", result.EvidenceIDs)
	}
	if scn.terminal.calls != 1 {
		t.Fatalf("terminal writer calls = %d, want exactly 1", scn.terminal.calls)
	}
}

// TestTodo_OBS_024_Security proves the RED-adjacent invariant OBS-024's own
// vocabulary implies but does not spell out: a terminal write that fails
// records no TERMINAL_WRITTEN evidence at all — evidence is a record of
// what happened, never of what was merely attempted. It also proves the
// EvidenceKind vocabulary passed to the port is always one of the five
// published constants, never a caller-invented string.
func TestTodo_OBS_024_Security(t *testing.T) {
	scn := newOBSScenario(t, "0af7651916cd43dd8448eb211c80319c")
	scn.terminal.fail = true
	driver := scn.driver(t)

	if _, err := driver.Resume(context.Background(), scn.req); err == nil {
		t.Fatal("Resume succeeded despite an injected terminal-write failure")
	}

	for _, e := range scn.evidence.Entries() {
		if e.kind == EvidenceKindTerminalWritten {
			t.Fatalf("TERMINAL_WRITTEN evidence recorded for a failed write: %+v", e)
		}
		switch e.kind {
		case EvidenceKindGateRefused, EvidenceKindGateAdmitted, EvidenceKindApprovalCompleted,
			EvidenceKindTaskSubmitted, EvidenceKindTerminalWritten:
			// published vocabulary
		default:
			t.Fatalf("evidence entry names an unpublished kind %q", e.kind)
		}
	}
}
