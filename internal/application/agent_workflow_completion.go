package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/workflowbridge"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

// AgentWorkflowEvidence reloads tenant-scoped immutable admission and durable
// execution; it must never use caller-supplied snapshots as completion evidence.
type AgentWorkflowEvidence interface {
	GetAdmission(context.Context, string, string) (agentrun.Record, error)
	GetRun(context.Context, string, string) (runstate.Run, error)
}

// AgentWorkflowCompletionBridge delivers across the isolated agent/core store
// boundary. A retry derives identical bytes; receipt and wakeup commit together.
type AgentWorkflowCompletionBridge struct {
	Evidence      AgentWorkflowEvidence
	Current       runstate.AdmissionRechecker
	Core          dbport.Beginner
	ResolveTenant func(string) uuid.UUID
	Now           func() time.Time
}

func (b AgentWorkflowCompletionBridge) Deliver(ctx context.Context, tenant, runID string) (signals.Receipt, error) {
	if b.Evidence == nil || b.Core == nil || b.ResolveTenant == nil || b.Now == nil || ctx == nil || runID == "" {
		return signals.Receipt{}, ErrAgentWorkflowInput
	}
	tid := b.ResolveTenant(tenant)
	if tid == uuid.Nil {
		return signals.Receipt{}, ErrAgentWorkflowInput
	}
	record, err := b.Evidence.GetAdmission(ctx, tenant, runID)
	if err != nil {
		return signals.Receipt{}, err
	}
	if record.ID != runID || record.Request.Source.TenantID != tenant {
		return signals.Receipt{}, workflowbridge.ErrEvidence
	}
	var run runstate.Run
	if record.Decision == agentrun.DecisionAccepted {
		run, err = b.Evidence.GetRun(ctx, tenant, runID)
		if err != nil {
			return signals.Receipt{}, err
		}
	}
	result, err := workflowbridge.Derive(record, run)
	if err != nil {
		return signals.Receipt{}, err
	}
	if result.Outcome == workflowbridge.Succeeded {
		if b.Current == nil {
			return signals.Receipt{}, ErrAgentWorkflowInput
		}
		if err = b.Current.Recheck(ctx, tenant, runID); err != nil {
			return signals.Receipt{}, err
		}
	}
	parts := strings.Split(record.Request.Source.Ref, ":")
	if len(parts) != 3 || parts[0] != "workflow" {
		return signals.Receipt{}, workflowbridge.ErrEvidence
	}
	instanceID, err := uuid.Parse(parts[1])
	if err != nil || instanceID == uuid.Nil {
		return signals.Receipt{}, workflowbridge.ErrEvidence
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return signals.Receipt{}, err
	}
	at := b.Now().UTC()
	if at.IsZero() {
		return signals.Receipt{}, ErrAgentWorkflowInput
	}
	sig := stepSignal.Signal{Tenant: values.TenantId(tid.String()), Source: workflowbridge.Source, EventType: workflowbridge.EventType,
		SchemaRef: workflowbridge.SchemaRef, CorrelationKey: workflowbridge.CorrelationKey, CorrelationValue: runID,
		IdempotencyKey: runID, Payload: payload, ReceivedAt: values.NewInstant(at)}
	tx, err := b.Core.Begin(ctx)
	if err != nil {
		return signals.Receipt{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tid); err != nil {
		return signals.Receipt{}, err
	}
	var foreign int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM workflow_signal_subscription WHERE tenant_id=$1 AND event_type=$2 AND correlation_key=$3 AND correlation_value=$4 AND instance_id<>$5`, tid, workflowbridge.EventType, workflowbridge.CorrelationKey, runID, instanceID).Scan(&foreign); err != nil {
		return signals.Receipt{}, err
	}
	if foreign != 0 {
		return signals.Receipt{}, workflowbridge.ErrEvidence
	}
	// This verifier is private to this producer invocation and compares the full
	// derived signal envelope. There is no external accepting-verifier endpoint.
	receipt, err := (signals.Store{}).Receive(ctx, tx, signals.ReceiveRequest{Signal: sig, ReceivedAt: at}, agentWorkflowSignalVerifier{expected: sig})
	if err != nil {
		return signals.Receipt{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return signals.Receipt{}, err
	}
	return receipt, nil
}

type agentWorkflowSignalVerifier struct{ expected stepSignal.Signal }

func (v agentWorkflowSignalVerifier) Verify(signal stepSignal.Signal) error {
	expected := v.expected
	if signal.Tenant != expected.Tenant || signal.Source != expected.Source || signal.EventType != expected.EventType ||
		signal.SchemaRef != expected.SchemaRef || signal.CorrelationKey != expected.CorrelationKey || signal.CorrelationValue != expected.CorrelationValue ||
		signal.IdempotencyKey != expected.IdempotencyKey || !agentWorkflowSamePayload(signal.Payload, expected.Payload) {
		return errors.New("agent workflow: signal is not derived from durable completion")
	}
	return nil
}

// agentWorkflowSamePayload compares two completion payloads as JSON values.
// A first delivery is verified against the bytes just derived. A retry is
// verified against the stored signal, and the signal store keeps the payload
// as jsonb: PostgreSQL returns the same value with its own key order and
// spacing, so a byte comparison refused every retry. The store has already
// refused a retry whose bytes differ from the first (by payload digest); what
// this check owns is that the stored value is the one the durable completion
// derives now.
func agentWorkflowSamePayload(got, want []byte) bool {
	if bytes.Equal(got, want) {
		return true
	}
	canonical := func(raw []byte) ([]byte, bool) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) != nil || decoder.More() {
			return nil, false
		}
		// Marshal writes object keys in sorted order, which makes the encoding
		// independent of the order either side used.
		encoded, err := json.Marshal(value)
		return encoded, err == nil
	}
	a, okA := canonical(got)
	b, okB := canonical(want)
	return okA && okB && bytes.Equal(a, b)
}
