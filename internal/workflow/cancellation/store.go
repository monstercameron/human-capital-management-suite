package cancellation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// decisionNamespace derives a decision's identity from the instance state it
// decided, so one observed state names exactly one decision.
var decisionNamespace = uuid.MustParse("6b0f3f5e-3c9a-4f51-9d2e-0b5d2f7a1c10")

func decisionID(inst runtime.Instance) uuid.UUID {
	return uuid.NewSHA1(decisionNamespace, []byte(inst.TenantID.String()+"|"+inst.InstanceID.String()+"|"+strconv.FormatInt(inst.InstanceVersion, 10)))
}

// lockInstance locks the instance row for the rest of the transaction and
// reads it. Every writer that advances an instance updates that row, so no
// node execution or frontier change can commit between the facts this
// package reads and the decision it records.
func lockInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (runtime.Instance, error) {
	var version int64
	if err := ex.QueryRow(ctx, `
		SELECT instance_version FROM workflow_instance
		WHERE tenant_id = $1 AND instance_id = $2
		FOR UPDATE`, tenantID, instanceID).Scan(&version); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return (runtime.Store{}).LoadInstance(ctx, ex, tenantID, instanceID)
		}
		return runtime.Instance{}, fmt.Errorf("workflow cancellation: lock instance %s: %w", instanceID, err)
	}
	return (runtime.Store{}).LoadInstance(ctx, ex, tenantID, instanceID)
}

// evidenceDocument is the decision row's evidence column.
type evidenceDocument struct {
	Outcome  workflow.CancellationOutcome `json:"outcome"`
	Children []ChildDecision              `json:"children"`
}

const decisionColumns = `decision_id, decision, evidence, reasons, compensation_refs,
	status_before, instance_version_before`

func scanDecision(row dbport.Row, inst runtime.Instance) (Outcome, error) {
	var (
		out          Outcome
		decision     string
		evidence     []byte
		reasons      []string
		compensation []string
		statusBefore string
	)
	if err := row.Scan(&out.DecisionID, &decision, &evidence, &reasons, &compensation, &statusBefore, &out.VersionBefore); err != nil {
		return Outcome{}, err
	}
	var doc evidenceDocument
	if err := json.Unmarshal(evidence, &doc); err != nil {
		return Outcome{}, fmt.Errorf("workflow cancellation: decode decision %s evidence: %w", out.DecisionID, err)
	}
	out.Decision = workflow.CancellationDecision(decision)
	out.Evidence, out.Children = doc.Outcome, doc.Children
	for _, r := range reasons {
		out.Reasons = append(out.Reasons, decodeReason(r))
	}
	out.CompensationRefs = compensation
	out.StatusBefore = runtime.InstanceStatus(statusBefore)
	out.Instance = inst
	return out, nil
}

// loadCurrentDecision returns the decision already recorded for inst's
// current state, if any.
func loadCurrentDecision(ctx context.Context, ex Executor, inst runtime.Instance) (Outcome, bool, error) {
	out, err := scanDecision(ex.QueryRow(ctx, `
		SELECT `+decisionColumns+`
		FROM workflow_cancellation_decision
		WHERE tenant_id = $1 AND instance_id = $2 AND instance_version_after = $3
		ORDER BY instance_version_before DESC
		LIMIT 1`, inst.TenantID, inst.InstanceID, inst.InstanceVersion), inst)
	if errors.Is(err, dbport.ErrNoRows) {
		return Outcome{}, false, nil
	}
	if err != nil {
		return Outcome{}, false, fmt.Errorf("workflow cancellation: read current decision of %s: %w", inst.InstanceID, err)
	}
	out.Replayed = true
	return out, true, nil
}

func recordDecision(ctx context.Context, ex Executor, out Outcome, planHash string, parentID uuid.UUID, req Request) error {
	evidence, err := json.Marshal(evidenceDocument{Outcome: out.Evidence, Children: out.Children})
	if err != nil {
		return fmt.Errorf("workflow cancellation: encode evidence: %w", err)
	}
	reasons := make([]string, 0, len(out.Reasons))
	for _, r := range out.Reasons {
		reasons = append(reasons, r.encode())
	}
	var parent any
	if parentID != uuid.Nil {
		parent = parentID
	}
	compensation := out.CompensationRefs
	if compensation == nil {
		compensation = []string{}
	}
	if _, err := ex.Exec(ctx, `
		INSERT INTO workflow_cancellation_decision (
			tenant_id, decision_id, instance_id, parent_decision_id, decision, phase,
			compiled_plan_hash, status_before, status_after, instance_version_before,
			instance_version_after, evidence, reasons, compensation_refs, reason_ref,
			requested_by, outcome_digest, decided_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		out.Instance.TenantID, out.DecisionID, out.Instance.InstanceID, parent, string(out.Decision), out.Evidence.Phase,
		planHash, string(out.StatusBefore), string(out.Instance.RuntimeStatus), out.VersionBefore,
		out.Instance.InstanceVersion, evidence, reasons, compensation, req.Reason,
		req.RequestedBy, out.Evidence.Digest, req.RecordedAt.UTC()); err != nil {
		return fmt.Errorf("workflow cancellation: record %s decision for %s: %w", out.Decision, out.Instance.InstanceID, err)
	}
	return nil
}

// Record is one stored decision row, as an inspector reads it.
type Record struct {
	Outcome
	InstanceID       uuid.UUID
	ParentDecisionID *uuid.UUID
	Phase            string
	StatusAfter      runtime.InstanceStatus
	VersionAfter     int64
	ReasonRef        string
	RequestedBy      string
	DecidedAt        time.Time
}

// Decisions returns every decision recorded for an instance, oldest first.
func Decisions(ctx context.Context, ex dbport.Querier, tenantID, instanceID uuid.UUID) (ret0 []Record, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.cancellation.decisions",
		observe.Attrs{observe.KeyTenant: tenantID.String()}, observe.Attrs{observe.KeyInstance: instanceID.String()})
	defer func() { observe.DoneWith(obsOp, retErr, len(ret0)) }()
	rows, err := ex.Query(ctx, `
		SELECT `+decisionColumns+`, instance_id, parent_decision_id, phase, status_after,
			instance_version_after, reason_ref, requested_by, decided_at
		FROM workflow_cancellation_decision
		WHERE tenant_id = $1 AND instance_id = $2
		ORDER BY instance_version_before, decided_at`, tenantID, instanceID)
	if err != nil {
		return nil, fmt.Errorf("workflow cancellation: read decisions of %s: %w", instanceID, err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var (
			rec          Record
			decision     string
			evidence     []byte
			reasons      []string
			compensation []string
			statusBefore string
			statusAfter  string
		)
		if err := rows.Scan(&rec.DecisionID, &decision, &evidence, &reasons, &compensation, &statusBefore, &rec.VersionBefore,
			&rec.InstanceID, &rec.ParentDecisionID, &rec.Phase, &statusAfter, &rec.VersionAfter,
			&rec.ReasonRef, &rec.RequestedBy, &rec.DecidedAt); err != nil {
			return nil, fmt.Errorf("workflow cancellation: scan decision: %w", err)
		}
		var doc evidenceDocument
		if err := json.Unmarshal(evidence, &doc); err != nil {
			return nil, fmt.Errorf("workflow cancellation: decode decision %s evidence: %w", rec.DecisionID, err)
		}
		rec.Decision = workflow.CancellationDecision(decision)
		rec.Evidence, rec.Children = doc.Outcome, doc.Children
		for _, r := range reasons {
			rec.Reasons = append(rec.Reasons, decodeReason(r))
		}
		rec.CompensationRefs = compensation
		rec.StatusBefore = runtime.InstanceStatus(statusBefore)
		rec.StatusAfter = runtime.InstanceStatus(statusAfter)
		rec.DecidedAt = rec.DecidedAt.UTC()
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("workflow cancellation: read decisions of %s: %w", instanceID, err)
	}
	return out, nil
}
