package runtime

// Effect-role settlement (WF-RUN-037) over
// migrations/00306_workflow_effect_role_settlement.sql. A workflow compiles
// every write-effect node into AUTHORITATIVE_CORE, DOWNSTREAM_EFFECT or
// DERIVED_UPDATE. When a downstream effect or derived update fails, the
// committed core must stay the business outcome: the failure is settled here,
// against the core node executions that had already committed, and routed to
// reconciliation (a downstream effect) or marked rebuildable from the core (a
// derived update). [DeriveEffectRoleSettlement] is the pure derivation from
// the pinned plan and the durable node executions; [EffectRoleSettlementStore]
// writes the sealed row through the caller's advance transaction.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Effect-role settlement refusal codes.
const (
	// CodeEffectRoleNotDependent reports a settlement requested for a node that
	// is not a DOWNSTREAM_EFFECT or DERIVED_UPDATE: the core and a read have
	// nothing to settle against a core.
	CodeEffectRoleNotDependent = "EFFECT_ROLE_NOT_DEPENDENT"
	// CodeEffectRoleNoCore reports a derived update with no committed core to
	// rebuild from.
	CodeEffectRoleNoCore = "EFFECT_ROLE_NO_CORE"
	// CodeEffectRoleSealBroken reports a settlement whose content no longer
	// digests to its seal, whether submitted or read back.
	CodeEffectRoleSealBroken = "EFFECT_ROLE_SEAL_BROKEN"
)

// EffectRoleRoute is where a failed dependent node was settled.
type EffectRoleRoute string

// The two settlement routes, one per dependent role.
const (
	// RouteReconciliation settles a failed DOWNSTREAM_EFFECT: the core stands
	// and the open effect is observed, reconciled and repaired.
	RouteReconciliation EffectRoleRoute = "RECONCILIATION"
	// RouteRebuildFromCore settles a failed DERIVED_UPDATE: the core stands
	// and the derived state is rebuilt from it.
	RouteRebuildFromCore EffectRoleRoute = "REBUILD_FROM_CORE"
)

// CoreOutcome is one committed AUTHORITATIVE_CORE node execution a settlement
// names as the outcome that stands.
type CoreOutcome struct {
	NodeID            string
	NodeExecutionID   uuid.UUID
	OutputArtifactRef string
}

// EffectRoleSettlement is one sealed settlement of a failed dependent node.
type EffectRoleSettlement struct {
	InstanceID   uuid.UUID
	NodeID       string
	Attempt      int
	Role         workflow.EffectRole
	EffectClass  capability.EffectClass
	EffectKey    string
	Route        EffectRoleRoute
	FailureRoute string
	ErrorClass   string
	PlanDigest   string
	Cores        []CoreOutcome
	Digest       string
}

// Verify recomputes the settlement seal.
func (s EffectRoleSettlement) Verify() error {
	if effectRoleSettlementDigest(s) != s.Digest {
		return refuse(CodeEffectRoleSealBroken, s.InstanceID.String(), s.NodeID, "effect role settlement seal broken")
	}
	return nil
}

func effectRoleSettlementDigest(s EffectRoleSettlement) string {
	parts := []string{
		"effect-role-settlement/v1", s.InstanceID.String(), s.NodeID, fmt.Sprintf("%d", s.Attempt),
		string(s.Role), string(s.EffectClass), s.EffectKey, string(s.Route), s.FailureRoute, s.ErrorClass, s.PlanDigest,
	}
	for _, c := range s.Cores {
		parts = append(parts, "core", c.NodeID, c.NodeExecutionID.String(), c.OutputArtifactRef)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// routeForRole returns the settlement route of a dependent role.
func routeForRole(role workflow.EffectRole) (EffectRoleRoute, bool) {
	switch role {
	case workflow.RoleDownstreamEffect:
		return RouteReconciliation, true
	case workflow.RoleDerivedUpdate:
		return RouteRebuildFromCore, true
	default:
		return "", false
	}
}

// DeriveEffectRoleSettlement settles one failed attempt of a DOWNSTREAM_EFFECT
// or DERIVED_UPDATE node from the pinned plan and the instance's durable node
// executions. The cores it names are the plan's AUTHORITATIVE_CORE nodes whose
// latest attempt SUCCEEDED -- committed in their own earlier advancement, so
// nothing this settlement's transaction does can roll them back. A derived
// update with no committed core is refused: there is nothing to rebuild from.
func DeriveEffectRoleSettlement(
	plan *workflow.CompiledWorkflow, instanceID uuid.UUID, nodeID string, attempt int, errorClass string, executions []NodeExecution,
) (EffectRoleSettlement, error) {
	id := instanceID.String()
	if plan == nil {
		return EffectRoleSettlement{}, refuse(CodeInvalidRecord, id, nodeID, "settlement needs the pinned plan")
	}
	node, ok := plan.Node(nodeID)
	if !ok {
		return EffectRoleSettlement{}, refuse(CodeInvalidRecord, id, nodeID, "node is absent from the pinned plan")
	}
	route, ok := routeForRole(node.EffectRole)
	if !ok {
		return EffectRoleSettlement{}, refuse(CodeEffectRoleNotDependent, id, nodeID,
			"node role %q is neither DOWNSTREAM_EFFECT nor DERIVED_UPDATE", node.EffectRole)
	}
	if attempt < 1 || strings.TrimSpace(errorClass) == "" || node.FailureRoute == "" {
		return EffectRoleSettlement{}, refuse(CodeInvalidRecord, id, nodeID,
			"settlement needs a positive attempt, an error class and the compiled failure route")
	}
	latest := map[string]NodeExecution{}
	for _, e := range executions {
		if e.InstanceID != instanceID {
			continue
		}
		if cur, seen := latest[e.NodeID]; !seen || e.Attempt > cur.Attempt {
			latest[e.NodeID] = e
		}
	}
	s := EffectRoleSettlement{
		InstanceID: instanceID, NodeID: nodeID, Attempt: attempt, Role: node.EffectRole,
		EffectClass: node.EffectClass, EffectKey: node.EffectKey, Route: route,
		FailureRoute: node.FailureRoute, ErrorClass: errorClass, PlanDigest: plan.Digest(),
		Cores: []CoreOutcome{},
	}
	for _, coreID := range plan.NodesWithRole(workflow.RoleAuthoritativeCore) {
		e, ok := latest[coreID]
		if !ok || e.Status != NodeSucceeded {
			continue
		}
		s.Cores = append(s.Cores, CoreOutcome{NodeID: coreID, NodeExecutionID: e.NodeExecutionID, OutputArtifactRef: e.OutputArtifactRef})
	}
	if node.EffectRole == workflow.RoleDerivedUpdate && len(s.Cores) == 0 {
		return EffectRoleSettlement{}, refuse(CodeEffectRoleNoCore, id, nodeID,
			"derived update has no committed AUTHORITATIVE_CORE to rebuild from")
	}
	s.Digest = effectRoleSettlementDigest(s)
	return s, nil
}

// EffectRoleSettlementStore persists [EffectRoleSettlement]. It holds no
// state; the caller's transaction must already carry the tenant scope.
type EffectRoleSettlementStore struct{}

const effectRoleSettlementColumns = `instance_id, node_id, attempt, effect_role, effect_class, effect_key, route,
	failure_route, error_class, plan_digest, core_node_ids, core_execution_ids, core_output_refs, record_digest`

// Record inserts one sealed settlement idempotently by (instance, node,
// attempt): a replay of the same settlement returns the stored row, and a
// different settlement for an attempt already settled is refused as a broken
// seal rather than overwriting the evidence.
func (EffectRoleSettlementStore) Record(ctx context.Context, ex Executor, tenantID uuid.UUID, s EffectRoleSettlement, recordedAt time.Time) (ret0 EffectRoleSettlement, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.record_effect_role_settlement", tenantID, s.InstanceID, s.NodeID, s.Attempt)
	defer func() { observe.DoneWith(obsOp, retErr, ret0.Digest) }()
	id := s.InstanceID.String()
	switch {
	case tenantID == uuid.Nil || s.InstanceID == uuid.Nil:
		return EffectRoleSettlement{}, refuse(CodeInvalidRecord, id, s.NodeID, "tenant and instance ids must not be the nil UUID")
	case recordedAt.IsZero():
		return EffectRoleSettlement{}, refuse(CodeInvalidRecord, id, s.NodeID, "recorded_at must be supplied; this package never reads a wall clock")
	}
	if want, ok := routeForRole(s.Role); !ok || want != s.Route {
		return EffectRoleSettlement{}, refuse(CodeEffectRoleNotDependent, id, s.NodeID, "role %q does not settle on route %q", s.Role, s.Route)
	}
	if err := s.Verify(); err != nil {
		return EffectRoleSettlement{}, err
	}
	nodeIDs := make([]string, len(s.Cores))
	execIDs := make([]string, len(s.Cores))
	outputs := make([]string, len(s.Cores))
	for i, c := range s.Cores {
		nodeIDs[i], execIDs[i], outputs[i] = c.NodeID, c.NodeExecutionID.String(), c.OutputArtifactRef
	}
	if _, err := ex.Exec(ctx, `INSERT INTO workflow_effect_role_settlement (tenant_id, recorded_at, `+effectRoleSettlementColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (tenant_id, instance_id, node_id, attempt) DO NOTHING`,
		tenantID, recordedAt.UTC(), s.InstanceID, s.NodeID, int32(s.Attempt), string(s.Role), string(s.EffectClass), s.EffectKey,
		string(s.Route), s.FailureRoute, s.ErrorClass, s.PlanDigest, nodeIDs, execIDs, outputs, s.Digest); err != nil {
		return EffectRoleSettlement{}, wrap(CodeStorageFailed, id, s.NodeID, err, "record effect role settlement")
	}
	stored, err := scanEffectRoleSettlement(ex.QueryRow(ctx, `SELECT `+effectRoleSettlementColumns+` FROM workflow_effect_role_settlement
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND attempt = $4`, tenantID, s.InstanceID, s.NodeID, int32(s.Attempt)))
	if err != nil {
		return EffectRoleSettlement{}, err
	}
	if stored.Digest != s.Digest {
		return EffectRoleSettlement{}, refuse(CodeEffectRoleSealBroken, id, s.NodeID,
			"attempt %d is already settled as %s; refusing a different settlement %s", s.Attempt, stored.Digest, s.Digest)
	}
	return stored, nil
}

// ListForInstance returns every settlement recorded for one instance, ordered
// by node and attempt, each seal verified.
func (EffectRoleSettlementStore) ListForInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (ret0 []EffectRoleSettlement, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.list_effect_role_settlements", tenantID, instanceID)
	defer func() { observe.DoneWith(obsOp, retErr) }()
	rows, err := ex.Query(ctx, `SELECT `+effectRoleSettlementColumns+` FROM workflow_effect_role_settlement
		WHERE tenant_id = $1 AND instance_id = $2 ORDER BY node_id, attempt`, tenantID, instanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "list effect role settlements")
	}
	defer rows.Close()
	out := []EffectRoleSettlement{}
	for rows.Next() {
		s, err := scanEffectRoleSettlement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "list effect role settlements")
	}
	return out, nil
}

func scanEffectRoleSettlement(row dbport.Row) (EffectRoleSettlement, error) {
	var (
		s                         EffectRoleSettlement
		attempt                   int32
		role, class, route        string
		nodeIDs, execIDs, outputs []string
	)
	if err := row.Scan(&s.InstanceID, &s.NodeID, &attempt, &role, &class, &s.EffectKey, &route,
		&s.FailureRoute, &s.ErrorClass, &s.PlanDigest, &nodeIDs, &execIDs, &outputs, &s.Digest); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return EffectRoleSettlement{}, refuse(CodeStorageFailed, "", "", "effect role settlement is absent after insert")
		}
		return EffectRoleSettlement{}, wrap(CodeStorageFailed, "", "", err, "read effect role settlement")
	}
	s.Attempt, s.Role, s.EffectClass, s.Route = int(attempt), workflow.EffectRole(role), capability.EffectClass(class), EffectRoleRoute(route)
	if len(nodeIDs) != len(execIDs) || len(nodeIDs) != len(outputs) {
		return EffectRoleSettlement{}, refuse(CodeEffectRoleSealBroken, s.InstanceID.String(), s.NodeID, "stored core outcome columns disagree in length")
	}
	s.Cores = make([]CoreOutcome, len(nodeIDs))
	for i := range nodeIDs {
		execID, err := uuid.Parse(execIDs[i])
		if err != nil {
			return EffectRoleSettlement{}, wrap(CodeEffectRoleSealBroken, s.InstanceID.String(), s.NodeID, err, "stored core execution id")
		}
		s.Cores[i] = CoreOutcome{NodeID: nodeIDs[i], NodeExecutionID: execID, OutputArtifactRef: outputs[i]}
	}
	if err := s.Verify(); err != nil {
		return EffectRoleSettlement{}, err
	}
	return s, nil
}
