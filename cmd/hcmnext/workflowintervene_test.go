package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var ivFixedNow = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

// ivNode is one node execution a CLI fixture instance starts with: the node
// is recorded READY, then walks the given statuses.
type ivNode struct {
	id     string
	status []runtime.NodeStatus
}

// ivInstance creates a RUNNING promotion instance at frontier on the
// promotion plan the served command resolves, mirroring the controller's own
// fixture shape so the CLI test drives real compiled semantics.
func ivInstance(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, plan *workflow.CompiledWorkflow, frontier []string, nodes ...ivNode) (uuid.UUID, int64) {
	t.Helper()
	ctx := context.Background()
	inst, err := runtime.NewInstance(tenantID, uuid.New(), "cell-local", plan, workflow.ModeExecute, "sha256:input", "corr", ivFixedNow)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	var version int64
	if err := migopTx(conn, tenantID, func(tx dbport.Tx) error {
		store := runtime.Store{}
		stored, err := store.CreateInstance(ctx, tx, inst)
		if err != nil {
			return err
		}
		running, err := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{
			TenantID: tenantID, InstanceID: inst.InstanceID, ExpectedVersion: stored.InstanceVersion,
			Status: runtime.InstanceRunning, CurrentNodeIDs: frontier,
		})
		if err != nil {
			return err
		}
		version = running.InstanceVersion
		for _, n := range nodes {
			cn, ok := plan.Node(n.id)
			if !ok {
				t.Fatalf("promotion plan has no node %q", n.id)
			}
			exec := runtime.NewNodeExecution(tenantID, inst.InstanceID, n.id, 1, cn.Type, runtime.NodeReady)
			exec.RecordedAt = ivFixedNow
			if _, version, err = store.RecordNodeExecution(ctx, tx, exec, version); err != nil {
				return err
			}
			for _, s := range n.status {
				if _, version, err = store.RecordNodeTransition(ctx, tx, runtime.NodeTransition{
					TenantID: tenantID, InstanceID: inst.InstanceID, NodeID: n.id, Attempt: 1,
					ExpectedInstanceVersion: version, Status: s,
				}); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("build instance: %v", err)
	}
	return inst.InstanceID, version
}

// ivGrant records a real JIT grant in the trust store: the served command's
// JIT authority resolves it, the grant's instance-scoped field carries the
// dual-control second approval, and the preflight simulation supplies the
// dry run. Repair kinds ride an integrity-repair grant, override an
// incident-responder one -- the same families the operator policy demands.
func ivGrant(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, grantID, role, capability, instanceID string) {
	t.Helper()
	scope, err := json.Marshal(truststore.JITGrantScope{Role: role, TicketRef: "INC-1", Justification: "stuck promotion",
		Capabilities: []string{capability}, Fields: []string{workflowcontrol.InstanceApprovalField(instanceID)}, Purpose: "workflow control"})
	if err != nil {
		t.Fatalf("marshal grant scope: %v", err)
	}
	err = truststore.New(db.Conn).PutJITGrant(context.Background(), tenantID, truststore.JITGrantRecord{
		TenantID: tenantID, RowID: uuid.New(), GrantID: grantID, Revision: 1, State: "ACTIVE",
		Requester: "operator:ana", Approver: "approver:lead", Scope: scope,
		// The lifetime stays inside the tightest role ceiling
		// (integrity-repair: 4h) so the record restores as authority.
		NotBefore: ivFixedNow.Add(-30 * time.Minute), ExpiresAt: ivFixedNow.Add(3 * time.Hour),
	})
	if err != nil {
		t.Fatalf("put %s: %v", grantID, err)
	}
}

// ivCommand runs the served workflow-intervene command against the embedded
// database and returns its exit code and output.
func ivCommand(t *testing.T, db *pgtest.DB, args ...string) (int, string, string) {
	t.Helper()
	old := openInterventionCell
	openInterventionCell = func(context.Context, string) (dbport.Beginner, func(), error) {
		return migopConn(t, db), func() {}, nil
	}
	defer func() { openInterventionCell = old }()
	var stdout, stderr bytes.Buffer
	code := runWorkflowIntervene(args, &stdout, &stderr, func() time.Time { return ivFixedNow }, openInterventionCell)
	return code, stdout.String(), stderr.String()
}

func ivBase(tenantID, instanceID uuid.UUID, version int64) []string {
	return []string{"-database-url", "postgres://test",
		"-tenant", tenantID.String(), "-instance", instanceID.String(),
		"-version", strconv.FormatInt(version, 10),
		"-evidence", "ticket:INC-7,log:driver-run-3", "-reason", "INC-1", "-operator", "operator:ana"}
}

func ivNodeStatus(t *testing.T, conn *pgxadapter.Conn, tenantID, instanceID uuid.UUID, nodeID string) runtime.NodeStatus {
	t.Helper()
	var out []runtime.NodeExecution
	if err := migopTx(conn, tenantID, func(tx dbport.Tx) error {
		var err error
		out, err = (runtime.Store{}).LoadNodeExecutions(context.Background(), tx, tenantID, instanceID)
		return err
	}); err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	var latest runtime.NodeExecution
	for _, n := range out {
		if n.NodeID == nodeID && n.Attempt >= latest.Attempt {
			latest = n
		}
	}
	return latest.Status
}

func ivInstanceState(t *testing.T, conn *pgxadapter.Conn, tenantID, instanceID uuid.UUID) (runtime.InstanceStatus, int64, []string) {
	t.Helper()
	var inst runtime.Instance
	if err := migopTx(conn, tenantID, func(tx dbport.Tx) error {
		var err error
		inst, err = (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, instanceID)
		return err
	}); err != nil {
		t.Fatalf("load instance: %v", err)
	}
	return inst.RuntimeStatus, inst.InstanceVersion, inst.CurrentNodeIDs
}

// TestTodo_REV_009_02_Integration drives all six previously untriggerable
// intervention kinds through the served hcmnext workflow-intervene command
// against embedded PostgreSQL: real JIT grants, the durable journal, the
// promotion plan set and the runtime's own transitions. Each accepted kind
// leaves its durable state change behind.
func TestTodo_REV_009_02_Integration(t *testing.T) {
	db := pgtest.New(t)
	conn := migopConn(t, db)
	tenantID := migopInsertTenant(t, db, "rev00902-cli")
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("compile promotion plan: %v", err)
	}

	applied := func(what string, code int, out, stderr string) {
		t.Helper()
		if code != 0 {
			t.Fatalf("%s: exit %d, stdout %q, stderr %q", what, code, out, stderr)
		}
		if !strings.Contains(out, "outcome: APPLIED") || !strings.Contains(out, "decision:") {
			t.Fatalf("%s printed %q, want an APPLIED outcome with a recorded decision", what, out)
		}
	}

	t.Run("SKIP", func(t *testing.T) {
		id, v := ivInstance(t, conn, tenantID, plan, []string{"wait_effective_date"},
			ivNode{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
		ivGrant(t, db, tenantID, "rev00902-g-skip", string(jit.RoleIntegrityRepair), "WORKFLOW_SKIP", id.String())
		args := append([]string{"skip", "-node", "wait_effective_date"}, ivBase(tenantID, id, v)...)
		code, out, stderr := ivCommand(t, db, args...)
		applied("skip", code, out, stderr)
		if got := ivNodeStatus(t, conn, tenantID, id, "wait_effective_date"); got != runtime.NodeSkipped {
			t.Fatalf("skipped node = %s, want SKIPPED", got)
		}
	})

	t.Run("SATISFY", func(t *testing.T) {
		id, v := ivInstance(t, conn, tenantID, plan, []string{"wait_effective_date"},
			ivNode{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
		ivGrant(t, db, tenantID, "rev00902-g-satisfy", string(jit.RoleIntegrityRepair), "WORKFLOW_SATISFY", id.String())
		args := append([]string{"satisfy", "-node", "wait_effective_date", "-route", "SUCCEEDED"}, ivBase(tenantID, id, v)...)
		code, out, stderr := ivCommand(t, db, args...)
		applied("satisfy", code, out, stderr)
		if got := ivNodeStatus(t, conn, tenantID, id, "wait_effective_date"); got != runtime.NodeSucceeded {
			t.Fatalf("satisfied node = %s, want SUCCEEDED", got)
		}
	})

	t.Run("OVERRIDE", func(t *testing.T) {
		id, v := ivInstance(t, conn, tenantID, plan, []string{"still_valid"}, ivNode{"still_valid", nil})
		ivGrant(t, db, tenantID, "rev00902-g-override", string(jit.RoleIncidentResponder), "WORKFLOW_OVERRIDE", id.String())
		args := append([]string{"override", "-node", "still_valid", "-route", "VALID"}, ivBase(tenantID, id, v)...)
		code, out, stderr := ivCommand(t, db, args...)
		applied("override", code, out, stderr)
		if got := ivNodeStatus(t, conn, tenantID, id, "still_valid"); got != runtime.NodeOverridden {
			t.Fatalf("overridden node = %s, want OVERRIDDEN", got)
		}
	})

	t.Run("REWIND", func(t *testing.T) {
		id, v := ivInstance(t, conn, tenantID, plan, []string{"still_valid"},
			ivNode{"revalidate", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}}, ivNode{"still_valid", nil})
		ivGrant(t, db, tenantID, "rev00902-g-rewind-pause", string(jit.RoleIntegrityRepair), "WORKFLOW_PAUSE", id.String())
		ivGrant(t, db, tenantID, "rev00902-g-rewind", string(jit.RoleIntegrityRepair), "WORKFLOW_REWIND", id.String())
		code, out, stderr := ivCommand(t, db, append([]string{"pause"}, ivBase(tenantID, id, v)...)...)
		if code != 0 {
			t.Fatalf("pause before rewind: exit %d, stdout %q, stderr %q", code, out, stderr)
		}
		_, pv, _ := ivInstanceState(t, conn, tenantID, id)
		args := append([]string{"rewind", "-target", "revalidate"}, ivBase(tenantID, id, pv)...)
		code, out, stderr = ivCommand(t, db, args...)
		applied("rewind", code, out, stderr)
		_, _, frontier := ivInstanceState(t, conn, tenantID, id)
		if len(frontier) != 1 || frontier[0] != "revalidate" {
			t.Fatalf("rewound frontier = %v, want [revalidate]", frontier)
		}
	})

	t.Run("SUPERSEDE", func(t *testing.T) {
		id, v := ivInstance(t, conn, tenantID, plan, []string{"wait_effective_date"},
			ivNode{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
		replacement, _ := ivInstance(t, conn, tenantID, plan, []string{"snapshot_worker"})
		ivGrant(t, db, tenantID, "rev00902-g-supersede", string(jit.RoleIntegrityRepair), "WORKFLOW_SUPERSEDE", id.String())
		args := append([]string{"supersede", "-replacement", replacement.String()}, ivBase(tenantID, id, v)...)
		code, out, stderr := ivCommand(t, db, args...)
		applied("supersede", code, out, stderr)
		if status, _, _ := ivInstanceState(t, conn, tenantID, id); status != runtime.InstanceSuperseded {
			t.Fatalf("superseded instance = %s, want SUPERSEDED", status)
		}
	})

	t.Run("RECONCILE", func(t *testing.T) {
		id, v := ivInstance(t, conn, tenantID, plan, []string{"execute_promotion"},
			ivNode{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning}})
		ivGrant(t, db, tenantID, "rev00902-g-reconcile-cancel", string(jit.RoleIntegrityRepair), "WORKFLOW_CANCEL", id.String())
		ivGrant(t, db, tenantID, "rev00902-g-reconcile", string(jit.RoleIntegrityRepair), "WORKFLOW_RECONCILE", id.String())
		code, out, stderr := ivCommand(t, db, append([]string{"cancel"}, ivBase(tenantID, id, v)...)...)
		if code != 0 || !strings.Contains(out, "outcome: REPAIR_REQUIRED") {
			t.Fatalf("cancel an in-flight effect: exit %d, stdout %q, stderr %q; want REPAIR_REQUIRED", code, out, stderr)
		}
		_, cv, _ := ivInstanceState(t, conn, tenantID, id)
		args := append([]string{"reconcile", "-node", "execute_promotion", "-observation", "EFFECT_APPLIED"}, ivBase(tenantID, id, cv)...)
		code, out, stderr = ivCommand(t, db, args...)
		applied("reconcile", code, out, stderr)
		if got := ivNodeStatus(t, conn, tenantID, id, "execute_promotion"); got != runtime.NodeSucceeded {
			t.Fatalf("reconciled node = %s, want SUCCEEDED", got)
		}
	})
}

// TestTodo_REV_009_02_Fault proves the served surface refuses instead of
// intervening: unknown kinds and missing flags are usage errors, a missing
// grant denies without touching workflow state, and a malformed replacement
// never reaches the gateway.
func TestTodo_REV_009_02_Fault(t *testing.T) {
	db := pgtest.New(t)
	conn := migopConn(t, db)
	tenantID := migopInsertTenant(t, db, "rev00902-fault")
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("compile promotion plan: %v", err)
	}

	for name, args := range map[string][]string{
		"no kind":            {},
		"compensate refused": {"compensate", "-tenant", tenantID.String()},
		"unknown kind":       {"force", "-tenant", tenantID.String()},
		"missing flags":      {"skip"},
		"bad tenant":         {"skip", "-tenant", "not-a-uuid"},
		"stale version":      {"skip", "-tenant", tenantID.String(), "-instance", uuid.New().String()},
	} {
		all := append([]string{"-database-url", "postgres://test"}, args...)
		if code, _, _ := ivCommand(t, db, all...); code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}

	id, v := ivInstance(t, conn, tenantID, plan, []string{"wait_effective_date"},
		ivNode{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})

	// No grant: the gateway denies and the node keeps waiting.
	code, out, _ := ivCommand(t, db, append([]string{"skip", "-node", "wait_effective_date"}, ivBase(tenantID, id, v)...)...)
	if code != 0 || !strings.Contains(out, "outcome: DENIED") {
		t.Fatalf("skip without a grant: exit %d, stdout %q; want a governed DENIED", code, out)
	}
	if got := ivNodeStatus(t, conn, tenantID, id, "wait_effective_date"); got != runtime.NodeWaiting {
		t.Fatalf("a denied skip moved the node to %s", got)
	}

	// A malformed replacement never reaches the gateway.
	ivGrant(t, db, tenantID, "rev00902-g-fault-supersede", string(jit.RoleIntegrityRepair), "WORKFLOW_SUPERSEDE", id.String())
	bad := append([]string{"supersede", "-replacement", "not-a-uuid"}, ivBase(tenantID, id, v)...)
	if code, _, _ := ivCommand(t, db, bad...); code != 1 {
		t.Fatalf("supersede with a malformed replacement: exit %d, want 1", code)
	}
	if status, _, _ := ivInstanceState(t, conn, tenantID, id); status != runtime.InstanceRunning {
		t.Fatalf("a refused supersede moved the instance to %s", status)
	}
}
