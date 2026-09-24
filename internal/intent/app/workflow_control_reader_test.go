package app

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
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// This package has no pgtest harness of its own yet (every other *_test.go
// file here runs against fakes or the corpus fixtures), so this file is what
// gives internal/intent/app.TestMain its one required call. If a later file
// in this package also needs embedded PostgreSQL, it must not declare a
// second TestMain; it should reuse this one.
func TestMain(m *testing.M) { pgtest.RunMain(m) }

// wfInspectorClock is the fixed instant every fixture below stamps. Neither
// the runtime store, the work item store nor the reader under test reads a
// wall clock, so a deterministic test has to say which one.
var wfInspectorClock = time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)

// wfInspectorTenant registers one active tenant as the migration/admin role,
// matching the pattern internal/workflow/runtime and internal/humanwork/workitem's
// own pgtest fixtures use: the pgtest connection is a superuser and is
// therefore never itself subject to row level security.
func wfInspectorTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// wfInspectorConn opens a connection on db's schema and assumes the
// least-privilege app role, the only way a test observes the row level
// security policies migrations 00016 and 00017 declare. The same connection
// is used both to write the fixture (inside its own tenant-scoped
// transactions, see [wfInspectorTx]) and, handed to [NewWorkflowControlReader]
// as the [dbport.Beginner] it opens its own read transaction on, to prove the
// reader's reads are themselves RLS-scoped rather than relying on a
// superuser connection to see everything.
func wfInspectorConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(t.Context(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// wfInspectorTx runs fn inside its own tenant-scoped transaction on conn and
// commits it, the same shape every store write below needs: two statements
// (a compare-and-set plus an append-only evidence row) that must land
// together.
func wfInspectorTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := t.Context()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatalf("transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// wfInspectorHex64 builds a 64-character hex string, the shape migration
// 00002's content_digest domain requires, from a single repeated digit.
func wfInspectorHex64(digit string) string {
	return strings.Repeat(digit, 64)
}

// wfInspectorInstance builds a storable, non-terminal RUNNING instance for
// tenant, parked at nodeID. It is built by hand rather than through
// [runtime.NewInstance] because that constructor demands a compiled
// [workflow.CompiledWorkflow], and this reader neither reads nor cares about
// one -- it only projects the durable rows the store already holds.
func wfInspectorInstance(tenant, instanceID uuid.UUID, nodeID string) runtime.Instance {
	return runtime.Instance{
		TenantID:         tenant,
		InstanceID:       instanceID,
		CellID:           "cell-local",
		WorkflowID:       "wf.inspector-test",
		WorkflowVersion:  1,
		CompiledPlanHash: wfInspectorHex64("a"),
		ExecutionMode:    workflow.ModeSimulate,
		RuntimeStatus:    runtime.InstanceRunning,
		InputRef:         "input-snapshot-ref",
		CurrentNodeIDs:   []string{nodeID},
		InstanceVersion:  1,
		CorrelationID:    "corr-" + instanceID.String(),
		CreatedAt:        wfInspectorClock,
	}
}

// wfInspectorTaskInput builds a well-formed [workitem.NewWorkItemInput] for a
// plain TASK item awaiting instance's nodeID.
func wfInspectorTaskInput(tenant, instance uuid.UUID, nodeID string) workitem.NewWorkItemInput {
	return workitem.NewWorkItemInput{
		TenantID:            tenant,
		Kind:                workitem.KindTask,
		WorkType:            "worktype.inspector.review/v1",
		CorrelationID:       "corr-" + instance.String(),
		WorkflowInstanceID:  instance,
		NodeID:              nodeID,
		SubjectRefs:         []string{"worker:jane"},
		PolicyRouteRef:      "route.inspector.current_manager/v1",
		Visibility:          workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: "org:acme-eu:engineering",
		DeadlineAt:          wfInspectorClock.Add(48 * time.Hour),
		CreatedAt:           wfInspectorClock,
	}
}

// wfInspectorMeta builds transition evidence naming a fixed test actor.
func wfInspectorMeta(reason string) workitem.TransitionMeta {
	return workitem.TransitionMeta{
		ActorPrincipalID: "principal:test-actor",
		Reason:           reason,
		At:               wfInspectorClock,
	}
}

// wfInspectorSingleCandidate resolves to exactly one candidate, which is what
// routes a [workitem.Store.Route] call to ASSIGNED rather than AVAILABLE or
// ESCALATED.
func wfInspectorSingleCandidate(principal string) humanwork.Resolution {
	return humanwork.Resolution{
		RequirementID:     "req.inspector-test/v1",
		Outcome:           humanwork.OutcomeResolved,
		Candidates:        []humanwork.Candidate{{PrincipalID: principal, Via: humanwork.SourceDirect, TermRef: "term.inspector-test"}},
		ResolvedAt:        values.NewInstant(wfInspectorClock),
		EffectiveAt:       values.NewInstant(wfInspectorClock),
		DirectoryVersion:  "directory.inspector-test/1",
		ExpressionDigest:  "sha256:" + wfInspectorHex64("1"),
		RequirementDigest: "sha256:" + wfInspectorHex64("2"),
		QuorumRequired:    1,
	}
}

// TestWorkflowInspectorRefusesWhenTheReaderIsNotConfigured pins the
// documented refusal: a reader built with no database, no tenant map, or
// neither, answers [ErrWorkflowControlReaderUnconfigured] rather than
// touching anything.
func TestWorkflowInspectorRefusesWhenTheReaderIsNotConfigured(t *testing.T) {
	always := func(values.TenantId) uuid.UUID { return uuid.New() }
	cases := map[string]WorkflowControlReader{
		"no database and no tenant map": NewWorkflowControlReader(nil, nil),
		"no database":                   NewWorkflowControlReader(nil, always),
		"no tenant map":                 NewWorkflowControlReader(fakeUnconfiguredBeginner{}, nil),
	}
	for name, reader := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := reader.ReadWorkflowControlRecord(t.Context(), values.TenantId("tenant-x"), uuid.New())
			if !errors.Is(err, ErrWorkflowControlReaderUnconfigured) {
				t.Fatalf("ReadWorkflowControlRecord = %v, want ErrWorkflowControlReaderUnconfigured", err)
			}
		})
	}
}

// fakeUnconfiguredBeginner is a [dbport.Beginner] that must never be called:
// every case in TestWorkflowInspectorRefusesWhenTheReaderIsNotConfigured
// that supplies one pairs it with a nil tenant map, so the reader must refuse
// before ever asking it to begin a transaction.
type fakeUnconfiguredBeginner struct{}

func (fakeUnconfiguredBeginner) Begin(_ context.Context) (dbport.Tx, error) {
	panic("Begin must not be called on an unconfigured reader")
}

// TestWorkflowInspectorReportsNotFoundForAnUnknownInstance proves the
// documented not-found path: an instance id nothing has ever written comes
// back as [runtime.CodeInstanceNotFound], readable through [runtime.CodeOf]
// and still wrapping [runtime.ErrRuntime], not a second sentinel this package
// invented for the same fact.
func TestWorkflowInspectorReportsNotFoundForAnUnknownInstance(t *testing.T) {
	db := pgtest.New(t)
	tenant := wfInspectorTenant(t, db, "wf-inspector-not-found")
	conn := wfInspectorConn(t, db)

	reader := NewWorkflowControlReader(conn, func(values.TenantId) uuid.UUID { return tenant })
	_, err := reader.ReadWorkflowControlRecord(t.Context(), values.TenantId("tenant-not-found"), uuid.New())
	if err == nil {
		t.Fatal("ReadWorkflowControlRecord on an unknown instance id must fail")
	}
	if code := runtime.CodeOf(err); code != runtime.CodeInstanceNotFound {
		t.Fatalf("runtime.CodeOf(err) = %q, want %q (err: %v)", code, runtime.CodeInstanceNotFound, err)
	}
	if !errors.Is(err, runtime.ErrRuntime) {
		t.Fatalf("err = %v, want it to still unwrap to runtime.ErrRuntime", err)
	}
}

// TestWorkflowInspectorLoadsTheCompleteDurableRecordOnAHappyPath is the
// primary case: a tenant's instance, one node execution and one work item
// that walked CREATED -> ROUTED -> ASSIGNED -> CLAIMED (four recorded
// transitions) all come back from one call, and the transitions are keyed by
// the work item id's own string form exactly as [WorkflowControlRecord]
// documents.
func TestWorkflowInspectorLoadsTheCompleteDurableRecordOnAHappyPath(t *testing.T) {
	db := pgtest.New(t)
	tenant := wfInspectorTenant(t, db, "wf-inspector-happy-path")
	conn := wfInspectorConn(t, db)
	rtStore := runtime.Store{}
	wiStore := workitem.Store{}
	ctx := t.Context()

	instanceID := uuid.New()
	instance := wfInspectorInstance(tenant, instanceID, "approval_node")
	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		instance, err = rtStore.CreateInstance(ctx, tx, instance)
		return err
	})

	nodeStarted := wfInspectorClock
	nodeCompleted := wfInspectorClock.Add(time.Minute)
	node := runtime.NewNodeExecution(tenant, instanceID, "approval_node", 1, workflow.StepApproval, runtime.NodeSucceeded)
	node.StartedAt = &nodeStarted
	node.CompletedAt = &nodeCompleted
	node.TraceID = "trace-inspector-1"
	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, _, err := rtStore.RecordNodeExecution(ctx, tx, node, instance.InstanceVersion)
		return err
	})

	in, err := workitem.NewWorkItem(wfInspectorTaskInput(tenant, instanceID, "approval_node"))
	if err != nil {
		t.Fatalf("NewWorkItem: %v", err)
	}
	var created workitem.WorkItem
	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		created, err = wiStore.Create(ctx, tx, in, wfInspectorMeta(workitem.ReasonCreated))
		return err
	})

	assignment := workitem.Assignment{
		Resolution: wfInspectorSingleCandidate("principal:manager-1"),
		Trigger:    workitem.TriggerInitialRouting,
	}
	var routed workitem.WorkItem
	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		routed, err = wiStore.Route(ctx, tx, tenant, created.WorkItemID, created.ItemVersion, assignment, wfInspectorMeta("workitem.routed"))
		return err
	})
	if routed.Status != workitem.StatusAssigned {
		t.Fatalf("fixture setup: routed status = %s, want ASSIGNED", routed.Status)
	}

	var claimed workitem.WorkItem
	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		claimed, err = wiStore.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: tenant, WorkItemID: routed.WorkItemID, ExpectedVersion: routed.ItemVersion,
			ClaimantPrincipalID: "principal:manager-1",
			ClaimExpiresAt:      wfInspectorClock.Add(2 * time.Hour),
			Now:                 wfInspectorClock,
			Meta:                wfInspectorMeta("workitem.claimed"),
		})
		return err
	})
	if claimed.Status != workitem.StatusClaimed {
		t.Fatalf("fixture setup: claimed status = %s, want CLAIMED", claimed.Status)
	}

	reader := NewWorkflowControlReader(conn, func(values.TenantId) uuid.UUID { return tenant })
	rec, err := reader.ReadWorkflowControlRecord(ctx, values.TenantId("tenant-happy-path"), instanceID)
	if err != nil {
		t.Fatalf("ReadWorkflowControlRecord: %v", err)
	}

	if rec.Instance.InstanceID != instanceID || rec.Instance.WorkflowID != "wf.inspector-test" ||
		rec.Instance.RuntimeStatus != runtime.InstanceRunning {
		t.Fatalf("loaded instance = %+v, want id %s / wf.inspector-test / RUNNING", rec.Instance, instanceID)
	}

	if len(rec.Nodes) != 1 {
		t.Fatalf("loaded %d node executions, want 1", len(rec.Nodes))
	}
	if got := rec.Nodes[0]; got.NodeID != "approval_node" || got.Status != runtime.NodeSucceeded || got.TraceID != "trace-inspector-1" {
		t.Fatalf("loaded node execution = %+v, want approval_node/SUCCEEDED/trace-inspector-1", got)
	}

	if len(rec.WorkItems) != 1 {
		t.Fatalf("loaded %d work items, want 1", len(rec.WorkItems))
	}
	if got := rec.WorkItems[0]; got.WorkItemID != created.WorkItemID || got.Status != workitem.StatusClaimed {
		t.Fatalf("loaded work item = %+v, want %s/CLAIMED", got, created.WorkItemID)
	}

	if len(rec.Transitions) != 1 {
		t.Fatalf("loaded transitions for %d work items, want 1", len(rec.Transitions))
	}
	trail, ok := rec.Transitions[created.WorkItemID.String()]
	if !ok {
		t.Fatalf("transitions are not keyed by the work item id's string form; keys: %v", mapKeys(rec.Transitions))
	}
	wantEdges := [][2]workitem.Status{
		{"", workitem.StatusCreated},
		{workitem.StatusCreated, workitem.StatusRouted},
		{workitem.StatusRouted, workitem.StatusAssigned},
		{workitem.StatusAssigned, workitem.StatusClaimed},
	}
	if len(trail) != len(wantEdges) {
		t.Fatalf("loaded %d transitions for the work item, want %d: %+v", len(trail), len(wantEdges), trail)
	}
	for i, edge := range wantEdges {
		if trail[i].FromStatus != edge[0] || trail[i].ToStatus != edge[1] {
			t.Errorf("transition %d = %s -> %s, want %s -> %s", i, trail[i].FromStatus, trail[i].ToStatus, edge[0], edge[1])
		}
	}
}

// mapKeys is a small diagnostic helper for a failed assertion message; it
// carries no test logic of its own.
func mapKeys(m map[string][]workitem.TransitionRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestWorkflowInspectorIsolatesTenants proves the same fact
// [runtime.Store.LoadInstance] documents at the store layer holds through
// this port too: an instance id that exists for one tenant loads nothing for
// another, reported identically to an instance that was never created at
// all.
func TestWorkflowInspectorIsolatesTenants(t *testing.T) {
	db := pgtest.New(t)
	owner := wfInspectorTenant(t, db, "wf-inspector-isolation-owner")
	stranger := wfInspectorTenant(t, db, "wf-inspector-isolation-stranger")
	conn := wfInspectorConn(t, db)
	ctx := t.Context()

	instanceID := uuid.New()
	instance := wfInspectorInstance(owner, instanceID, "approval_node")
	wfInspectorTx(t, conn, owner, func(tx dbport.Tx) error {
		var err error
		instance, err = runtime.Store{}.CreateInstance(ctx, tx, instance)
		return err
	})

	tenantOf := map[values.TenantId]uuid.UUID{"owner": owner, "stranger": stranger}
	reader := NewWorkflowControlReader(conn, func(id values.TenantId) uuid.UUID { return tenantOf[id] })

	if _, err := reader.ReadWorkflowControlRecord(ctx, "owner", instanceID); err != nil {
		t.Fatalf("the owning tenant must load its own instance: %v", err)
	}

	_, err := reader.ReadWorkflowControlRecord(ctx, "stranger", instanceID)
	if err == nil {
		t.Fatal("another tenant reading the same instance id must not succeed")
	}
	if code := runtime.CodeOf(err); code != runtime.CodeInstanceNotFound {
		t.Fatalf("cross-tenant read runtime.CodeOf(err) = %q, want %q (err: %v)", code, runtime.CodeInstanceNotFound, err)
	}
}
