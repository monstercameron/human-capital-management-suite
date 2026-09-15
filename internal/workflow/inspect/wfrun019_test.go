package inspect_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// WF-RUN-019's RED clause has two halves that pull against each other: the
// inspector must not omit the current node, attempt, retry, proposal,
// baseline, policy, effect or repair references, and must not leak protected
// input or output. Its GREEN clause names the traversal an authorized view
// walks. The tests below are that matrix.
//
// Nothing here is a scheduler view. There is no lease holder and no next-retry
// instant to render, because WF-RUN-000 gates the primitives that would
// produce them; what a node carries is its declared retry policy reference and
// its attempt number.

// TestTodo_WF_RUN_019 is the PRIMARY case: an authorized view walks the whole
// declared traversal and omits none of the references the RED clause names.
func TestTodo_WF_RUN_019(t *testing.T) {
	t.Parallel()

	view, err := inspect.Build(inspect.Request{
		Instance:      fixtureInstanceState(),
		Nodes:         fixtureNodes(),
		Authorization: operatorAuth(),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// definition -> instance -> node -> governance -> transaction -> connector
	// -> observation/reconciliation -> trace, in that order.
	wantTraversal := []inspect.Section{
		inspect.SectionDefinition, inspect.SectionInstance, inspect.SectionNode,
		inspect.SectionGovernance, inspect.SectionTransaction, inspect.SectionConnector,
		inspect.SectionObservation, inspect.SectionTrace,
	}
	if len(view.Traversal) != len(wantTraversal) {
		t.Fatalf("traversal has %d stages, want %d", len(view.Traversal), len(wantTraversal))
	}
	for i, want := range wantTraversal {
		if view.Traversal[i] != want {
			t.Errorf("traversal stage %d = %s, want %s", i, view.Traversal[i], want)
		}
	}

	if !view.Definition.Disclosed || view.Definition.CompiledPlanHash == "" {
		t.Errorf("definition stage = %+v", view.Definition)
	}
	if !view.Instance.Disclosed || view.Instance.RuntimeStatus != string(runtime.InstanceRunning) {
		t.Errorf("instance stage = %+v", view.Instance)
	}

	// All five lifecycle dimensions, never collapsed into the runtime status.
	life := view.Instance.Lifecycle
	if life.RequestState == "" || life.ExecutionState == "" || life.BusinessState == "" ||
		life.ConsistencyState == "" || life.ObligationState == "" {
		t.Errorf("lifecycle omits a dimension: %+v", life)
	}
	if life.ObligationState != "PENDING" {
		t.Errorf("obligation state = %q, want PENDING", life.ObligationState)
	}

	// The frontier: the current node, with the attempt standing on it.
	if got := view.FrontierNodeIDs(); len(got) != 1 || got[0] != nodeSync {
		t.Fatalf("frontier = %v, want [%s]", got, nodeSync)
	}
	front := view.Frontier[0]
	if !front.AttemptRecorded || front.Attempt != 3 || front.Status != string(runtime.NodeFailed) {
		t.Errorf("frontier entry = %+v, want attempt 3 in FAILED", front)
	}

	// The node the instance is stopped on carries every reference the RED
	// clause forbids omitting.
	node, ok := view.Node(nodeSync)
	if !ok {
		t.Fatalf("the current node %s is not in the view", nodeSync)
	}
	if !node.Current {
		t.Error("the current node is not marked current")
	}
	if node.Attempt != 3 {
		t.Errorf("attempt = %d, want 3", node.Attempt)
	}
	for _, ref := range []struct {
		name string
		ref  inspect.Ref
	}{
		{"retry policy", node.RetryPolicyRef},
		{"proposal", node.Governance.ProposalRef},
		{"baseline", node.Governance.BaselineRef},
		{"policy", node.Governance.PolicyRef},
		{"repair", node.Observation.RepairRef},
		{"capability execution", node.Connector.CapabilityExecutionID},
		{"trace", node.Trace.TraceID},
	} {
		if v, readable := ref.ref.Get(); !readable || v == "" {
			t.Errorf("%s ref is not disclosed to an authorized operator: state %v", ref.name, ref.ref.State())
		}
	}
	if effects, _ := node.Connector.EffectRefs.Values, node.Connector.EffectRefs.State; len(effects) == 0 {
		t.Error("effect refs are empty for a node that dispatched one")
	}
	if v, _ := node.Observation.ErrorClass.Get(); v != "ADP_TIMEOUT" {
		t.Errorf("error class = %q, want ADP_TIMEOUT", v)
	}

	// A fully authorized view withholds nothing and misses nothing.
	if !view.Completeness.Complete {
		t.Errorf("an authorized view reports itself incomplete: redactions %v, gaps %v",
			view.Completeness.Redactions, view.Completeness.Gaps)
	}
}

// TestTodo_WF_RUN_019_Golden pins the rendered view byte for byte.
//
// A field-by-field assertion proves the fields a test remembered to check. The
// golden proves the whole artifact, so a stage that quietly stops being
// rendered -- the exact RED failure -- shows up in review as removed lines
// rather than as a test that still passes.
func TestTodo_WF_RUN_019_Golden(t *testing.T) {
	t.Parallel()

	view, err := inspect.Build(inspect.Request{
		Instance:      fixtureInstanceState(),
		Nodes:         fixtureNodes(),
		Authorization: operatorAuth(),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	rendered, err := view.JSON()
	if err != nil {
		t.Fatalf("render view: %v", err)
	}
	golden(t, "wfrun019_operator_view.json", rendered)
}

// TestTodo_WF_RUN_019_Race proves the projection is a pure function of its
// inputs: many goroutines projecting the same state produce identical bytes
// and leave the inputs untouched.
//
// It matters because an inspector is the one surface an operator reaches for
// while a workflow is moving, so it will be called concurrently, and a
// projection that mutated the state it was handed would corrupt the runtime
// records a driver is simultaneously reading.
func TestTodo_WF_RUN_019_Race(t *testing.T) {
	t.Parallel()

	inst := fixtureInstanceState()
	nodes := fixtureNodes()
	auth := operatorAuth()

	first, err := inspect.Build(inspect.Request{Instance: inst, Nodes: nodes, Authorization: auth})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want, err := first.JSON()
	if err != nil {
		t.Fatalf("render view: %v", err)
	}

	const readers = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = make([][]byte, 0, readers)
	)
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, buildErr := inspect.Build(inspect.Request{Instance: inst, Nodes: nodes, Authorization: auth})
			if buildErr != nil {
				t.Errorf("concurrent Build: %v", buildErr)
				return
			}
			b, jsonErr := v.JSON()
			if jsonErr != nil {
				t.Errorf("concurrent render: %v", jsonErr)
				return
			}
			mu.Lock()
			results = append(results, b)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(results) != readers {
		t.Fatalf("%d concurrent projections produced %d results", readers, len(results))
	}
	for i, got := range results {
		if string(got) != string(want) {
			t.Fatalf("concurrent projection %d differs from the serial one", i)
		}
	}

	// The inputs are unchanged: the projection sorted a copy, not the caller's
	// slice, and never wrote through the instance it was handed.
	if inst.CurrentNodeIDs[0] != nodeSync || len(nodes) != 3 || nodes[0].NodeID != nodePreflight {
		t.Fatal("Build mutated the state it was given")
	}
}

// TestTodo_WF_RUN_019_Integration walks the real path: state written through
// internal/workflow/runtime into PostgreSQL, loaded back under tenant scope,
// and projected.
//
// The unit fixtures above build records by hand. This one proves the
// projection is over what the store actually persists and returns -- including
// the array and jsonb columns, which are the ones a hand-built fixture is most
// likely to render differently from a real read.
func TestTodo_WF_RUN_019_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun019-integration")
	store := runtime.Store{}
	conn := appConn(t, db)

	instanceID := uuid.New()
	inst := runtime.Instance{
		TenantID:            tenant,
		InstanceID:          instanceID,
		CellID:              "cell-local",
		WorkflowID:          "people.workflows.promote_into_management",
		WorkflowVersion:     17,
		CompiledPlanHash:    fixturePlanHash,
		BusinessSubjectRefs: []string{"person:jane"},
		ExecutionMode:       workflow.ModeExecute,
		RuntimeStatus:       runtime.InstanceCreated,
		InputRef:            "artifact:input/promotion-88191",
		CurrentNodeIDs:      []string{nodeSync},
		EffectiveContextRef: "ctx:legal.us-ca/2026.1",
		InstanceVersion:     1,
		CorrelationID:       "corr-88191",
		CreatedAt:           fixtureCreated,
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.CreateInstance(ctx, tx, inst)
		return err
	})

	node := runtime.NewNodeExecution(tenant, instanceID, nodeSync, 3,
		workflow.StepCapability, runtime.NodeFailed)
	node.InputSnapshotRef = "artifact:input/payroll-sync"
	node.ErrorClass = "ADP_TIMEOUT"
	node.TraceID = "trace:payroll-sync"
	node.CompletedAt = &fixtureEnded
	node.Refs = runtime.GovernanceRefs{
		AuthorizationDecisionID: "authz:payroll-sync",
		CapabilityExecutionID:   "capexec:payroll-sync/3",
		PolicyRef:               "payroll.sync.policy/2.1.0",
		ProposalRef:             "proposal:88191",
		BaselineRef:             "baseline:comp/2026-09-01",
		RepairRef:               "repair:payroll-88191",
		EffectRefs:              []string{"effect:payroll.worker_sync/88191"},
		RetryPolicyRef:          "policy.retry.effect.bounded/v1",
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, _, err := store.RecordNodeExecution(ctx, tx, node, 1)
		return err
	})

	var (
		loaded runtime.Instance
		rows   []runtime.NodeExecution
	)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		loaded, err = store.LoadInstance(ctx, tx, tenant, instanceID)
		if err != nil {
			return err
		}
		rows, err = store.LoadNodeExecutions(ctx, tx, tenant, instanceID)
		return err
	})

	view, err := inspect.Build(inspect.Request{
		Instance:      loaded,
		Nodes:         rows,
		Authorization: operatorAuth(),
	})
	if err != nil {
		t.Fatalf("Build over persisted state: %v", err)
	}
	if got := view.FrontierNodeIDs(); len(got) != 1 || got[0] != nodeSync {
		t.Fatalf("frontier from persisted state = %v", got)
	}
	rendered, ok := view.Node(nodeSync)
	if !ok {
		t.Fatal("the persisted node is not in the view")
	}
	if rendered.Attempt != 3 || rendered.Status != string(runtime.NodeFailed) {
		t.Errorf("node from persisted state = attempt %d / %s", rendered.Attempt, rendered.Status)
	}
	if v, _ := rendered.Governance.ProposalRef.Get(); v != "proposal:88191" {
		t.Errorf("proposal ref from persisted state = %q", v)
	}
	if v, _ := rendered.Observation.RepairRef.Get(); v != "repair:payroll-88191" {
		t.Errorf("repair ref from persisted state = %q", v)
	}
	if effects := rendered.Connector.EffectRefs.Values; len(effects) != 1 {
		t.Errorf("effect refs from persisted state = %v", effects)
	}
	if v, _ := rendered.RetryPolicyRef.Get(); v != "policy.retry.effect.bounded/v1" {
		t.Errorf("retry policy ref from persisted state = %q", v)
	}
	if !view.Completeness.Complete {
		t.Errorf("view over persisted state is incomplete: redactions %v, gaps %v",
			view.Completeness.Redactions, view.Completeness.Gaps)
	}

	// The durable reader: inspect.Load traverses every record family behind
	// this instance from PostgreSQL itself, keeps protected payloads out,
	// answers a foreign tenant exactly as it answers a missing instance, and
	// renders identically for concurrent readers.
	t.Run("durable traversal", func(t *testing.T) {
		assertDurableTraversal(t, db, conn, tenant, instanceID)
	})
	t.Run("tenant isolation", func(t *testing.T) {
		assertTenantIsolation(t, db, tenant, instanceID)
	})
	t.Run("concurrent durable loads", func(t *testing.T) {
		assertConcurrentLoads(t, db, tenant, instanceID)
	})
}

// TestTodo_WF_RUN_019_Fault is the RED case in both directions: protected
// input and output never leak, and a view that is missing something says so
// instead of rendering a confident blank.
func TestTodo_WF_RUN_019_Fault(t *testing.T) {
	t.Parallel()

	t.Run("a caller who may not see the instance is told nothing about it", func(t *testing.T) {
		auth := operatorAuth()
		auth.InstanceDisclosable = false
		auth.DenialReason = "NOT_IN_SCOPE"

		_, err := inspect.Build(inspect.Request{
			Instance: fixtureInstanceState(), Nodes: fixtureNodes(), Authorization: auth,
		})
		if !errors.Is(err, inspect.ErrNotDisclosable) {
			t.Fatalf("error = %v, want ErrNotDisclosable", err)
		}
		// The refusal names the policy token, not the workflow, the subject or
		// the instance id: the existence of the execution is itself
		// information.
		for _, leak := range []string{
			fixtureInstance.String(), "promote_into_management", "jane", "88191",
		} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("the refusal message discloses %q: %v", leak, err)
			}
		}
	})

	t.Run("protected input and output are redacted, not blanked", func(t *testing.T) {
		auth := operatorAuth()
		auth.Fields[inspect.FieldNodeInput] = inspect.Ruling{
			Effect: inspect.EffectDeny, Reason: "CLASSIFICATION_CONFIDENTIAL_HR",
		}
		auth.Fields[inspect.FieldNodeOutput] = inspect.Ruling{
			Effect: inspect.EffectDeny, Reason: "CLASSIFICATION_CONFIDENTIAL_HR",
		}
		auth.Fields[inspect.FieldInstanceInput] = inspect.Ruling{
			Effect: inspect.EffectDeny, Reason: "CLASSIFICATION_CONFIDENTIAL_HR",
		}

		view, err := inspect.Build(inspect.Request{
			Instance: fixtureInstanceState(), Nodes: fixtureNodes(), Authorization: auth,
		})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}

		if !view.Instance.InputRef.IsRedacted() {
			t.Error("the instance input ref was disclosed to a caller denied it")
		}
		for _, n := range view.Nodes {
			if !n.InputSnapshotRef.IsRedacted() || !n.OutputArtifactRef.IsRedacted() {
				t.Errorf("node %s disclosed a protected artifact reference", n.NodeID)
			}
			if n.InputSnapshotRef.Reason() != "CLASSIFICATION_CONFIDENTIAL_HR" {
				t.Errorf("node %s redaction reason = %q", n.NodeID, n.InputSnapshotRef.Reason())
			}
			if v, readable := n.InputSnapshotRef.Get(); readable || v != "" {
				t.Errorf("node %s redacted reference still carries %q", n.NodeID, v)
			}
		}

		// The bytes are the real test: a Ref that redacted the accessor but
		// still marshalled its value would pass every assertion above.
		rendered, err := view.JSON()
		if err != nil {
			t.Fatalf("render view: %v", err)
		}
		for _, secret := range []string{
			"artifact:input/payroll-sync", "artifact:output/preflight",
			"artifact:input/promotion-88191",
		} {
			if strings.Contains(string(rendered), secret) {
				t.Errorf("the rendered view leaks the protected reference %q", secret)
			}
		}

		// And the redaction is reported rather than silent.
		if view.Completeness.Complete {
			t.Error("a redacted view reports itself complete")
		}
		if len(view.Completeness.Redactions) != 3 {
			t.Errorf("redactions = %v, want the three denied fields", view.Completeness.Redactions)
		}
	})

	t.Run("a denied stage is named, not dropped", func(t *testing.T) {
		auth := operatorAuth()
		auth.Sections[inspect.SectionGovernance] = inspect.Ruling{
			Effect: inspect.EffectDeny, Reason: "NO_GOVERNANCE_SCOPE",
		}

		view, err := inspect.Build(inspect.Request{
			Instance: fixtureInstanceState(), Nodes: fixtureNodes(), Authorization: auth,
		})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if len(view.Traversal) != len(inspect.TraversalOrder()) {
			t.Fatal("a denied stage was removed from the traversal")
		}
		node, ok := view.Node(nodeSync)
		if !ok {
			t.Fatal("the current node vanished with its governance stage")
		}
		if node.Governance.Disclosed {
			t.Error("a denied governance stage was disclosed")
		}
		if node.Governance.DeniedReason != "NO_GOVERNANCE_SCOPE" {
			t.Errorf("denied reason = %q", node.Governance.DeniedReason)
		}
		if node.Governance.ProposalRef.State() != values.PresenceRedacted {
			t.Error("a denied stage's references are absent rather than redacted, which reads as 'nothing there'")
		}
		if !contains(view.Completeness.Redactions, string(inspect.SectionGovernance)) {
			t.Errorf("redactions = %v, want the denied stage named", view.Completeness.Redactions)
		}
	})

	t.Run("a frontier node with no recorded execution is a reported gap", func(t *testing.T) {
		inst := fixtureInstanceState()
		inst.CurrentNodeIDs = []string{nodeSync, "never_recorded"}

		view, err := inspect.Build(inspect.Request{
			Instance: inst, Nodes: fixtureNodes(), Authorization: operatorAuth(),
		})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if got := view.FrontierNodeIDs(); len(got) != 2 {
			t.Fatalf("frontier = %v; the unexplained node was dropped", got)
		}
		var found bool
		for _, entry := range view.Frontier {
			if entry.NodeID == "never_recorded" {
				found = true
				if entry.AttemptRecorded {
					t.Error("an unrecorded frontier node claims a recorded attempt")
				}
			}
		}
		if !found {
			t.Fatal("the unexplained frontier node is not rendered")
		}
		if view.Completeness.Complete {
			t.Error("a view with an unexplained frontier node reports itself complete")
		}
		var gapNamed bool
		for _, gap := range view.Completeness.Gaps {
			if strings.Contains(gap, "never_recorded") {
				gapNamed = true
			}
		}
		if !gapNamed {
			t.Errorf("gaps = %v, want the unexplained frontier node named", view.Completeness.Gaps)
		}
	})

	t.Run("an authorization decision that is not usable is refused", func(t *testing.T) {
		cases := []struct {
			name string
			auth inspect.Authorization
		}{
			{"no policy version", inspect.Authorization{Purpose: "OPERATIONS", InstanceDisclosable: true}},
			{"no purpose", inspect.Authorization{PolicyVersion: "p/1", InstanceDisclosable: true}},
			{"denial with no reason", inspect.Authorization{PolicyVersion: "p/1", Purpose: "OPERATIONS"}},
			{"denial with no token", func() inspect.Authorization {
				a := operatorAuth()
				a.Sections[inspect.SectionNode] = inspect.Ruling{Effect: inspect.EffectDeny}
				return a
			}()},
			{"unknown redactable field", func() inspect.Authorization {
				a := operatorAuth()
				a.Fields["node.secret_sauce"] = inspect.Ruling{Effect: inspect.EffectAllow}
				return a
			}()},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := inspect.Build(inspect.Request{
					Instance: fixtureInstanceState(), Nodes: fixtureNodes(), Authorization: tc.auth,
				})
				if !errors.Is(err, inspect.ErrAuthorizationInvalid) && !errors.Is(err, inspect.ErrNotDisclosable) {
					t.Fatalf("error = %v, want an authorization refusal", err)
				}
			})
		}
	})

	t.Run("a durable load honors a non-disclosable decision before reading anything", func(t *testing.T) {
		auth := operatorAuth()
		auth.InstanceDisclosable = false
		auth.DenialReason = "NOT_IN_SCOPE"
		// No executor at all: a Load that reached for the database would
		// fail differently, so ErrNotDisclosable proves nothing was read.
		_, err := inspect.Load(context.Background(), nil, inspect.LoadRequest{
			TenantID: fixtureTenant, InstanceID: fixtureInstance, Authorization: auth,
		})
		if !errors.Is(err, inspect.ErrNotDisclosable) {
			t.Fatalf("error = %v, want ErrNotDisclosable", err)
		}
		if strings.Contains(err.Error(), fixtureInstance.String()) {
			t.Errorf("the refusal discloses the instance id: %v", err)
		}
	})

	t.Run("a durable load refuses an unusable request", func(t *testing.T) {
		if _, err := inspect.Load(context.Background(), nil, inspect.LoadRequest{
			TenantID: fixtureTenant, InstanceID: fixtureInstance, Authorization: inspect.Authorization{},
		}); !errors.Is(err, inspect.ErrAuthorizationInvalid) {
			t.Fatalf("invalid authorization: error = %v, want ErrAuthorizationInvalid", err)
		}
		for name, req := range map[string]inspect.LoadRequest{
			"no executor": {TenantID: fixtureTenant, InstanceID: fixtureInstance},
			"no tenant":   {InstanceID: fixtureInstance},
			"no instance": {TenantID: fixtureTenant},
		} {
			req.Authorization = operatorAuth()
			if _, err := inspect.Load(context.Background(), nil, req); !errors.Is(err, inspect.ErrInvalidRequest) {
				t.Errorf("%s: error = %v, want ErrInvalidRequest", name, err)
			}
		}
	})

	t.Run("node executions from another instance are refused", func(t *testing.T) {
		nodes := fixtureNodes()
		nodes[0].InstanceID = uuid.New()

		_, err := inspect.Build(inspect.Request{
			Instance: fixtureInstanceState(), Nodes: nodes, Authorization: operatorAuth(),
		})
		if !errors.Is(err, inspect.ErrInvalidRequest) {
			t.Fatalf("error = %v, want ErrInvalidRequest", err)
		}
	})
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// insertTenant registers one active tenant as the admin role.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// appConn opens a connection on db's schema under the least-privilege role, so
// the row level security policies migration 00016 declares actually apply.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// inTenantTx runs fn in its own transaction, scoped to tenant by its first
// statement.
func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("scope transaction to tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
