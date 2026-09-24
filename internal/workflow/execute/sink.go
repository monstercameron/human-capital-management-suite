package execute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type continuationSink struct {
	tx dbport.Tx

	durable   runtime.ContinuationSink
	factory   WorkItemFactory
	timers    TimerFactory
	signals   SignalSubscriber
	terminal  TerminalWriter
	terminals TerminalRegistry
	repair    RepairRequester
	guard     idempotency.Store
	policy    idempotency.RetentionPolicy

	// plan is the exact compiled plan the advancement is pinned to. A
	// TIMER_REQUIRED continuation names a node, and the wake condition lives
	// on that node, so the sink needs the plan to hand the compiled WAIT node
	// to its TimerFactory rather than making the adapter re-resolve it.
	plan *workflow.CompiledWorkflow

	workflowID    string
	planDigest    string
	proposal      runtime.ProposalBinding
	source        runtime.StartSource
	cellID        string
	correlationID string
	startKey      string
	subjectRefs   []string

	// endNodeID and endOutputDigest are the END node's outcome (WF-RUN-030),
	// set by the driver from the exact frontier.NodeOutcome it ran before
	// calling Advance -- the COMPLETE intent's TargetNodeID is always that
	// same node, so this sink never has to re-derive which node they belong
	// to from the continuation record.
	endNodeID       string
	endOutputDigest string

	// instrumentation and evidence are OBS-023/OBS-024's ports, set by the
	// driver from its own Options. Both are always non-nil once a Driver is
	// built through New (which defaults them to their Noop implementation);
	// a nil value here is still tolerated defensively (see Complete) for a
	// continuationSink built outside that path.
	instrumentation Instrumentation
	evidence        ExecutionEvidence

	created []workitem.WorkItem
	// timersCreated collects every durable timer this advancement's
	// TIMER_REQUIRED continuations produced, in dispatch order.
	timersCreated []TimerHandle
	// evidenceIDs collects every OBS-024 evidence id this sink recorded
	// (TERMINAL_WRITTEN today), in recording order.
	evidenceIDs []string
}

func (s *continuationSink) RequireWorkItem(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execute.require_work_item")
	defer func() { observe.DoneWith(obsOp, retErr) }()
	if err := s.durable.RequireWorkItem(ctx, ex, rec); err != nil {
		return err
	}
	if s.factory == nil {
		return invalid("WORK_ITEM_REQUIRED for node %s but no WorkItemFactory is configured", rec.TargetNodeID)
	}
	id := runtime.ContinuationID(rec.TenantID, rec.InstanceID, rec.SourceNodeID, rec.SourceAttempt, rec.TargetNodeID, rec.Kind)
	item, err := s.factory.CreateAndRoute(ctx, ex, WorkItemRequest{
		WorkItemID: id, Continuation: rec, Proposal: s.proposal,
		CellID: s.cellID, CorrelationID: s.correlationID,
		SubjectRefs: append([]string(nil), s.subjectRefs...), CreatedAt: rec.RecordedAt,
	})
	if err != nil {
		return err
	}
	if item.WorkItemID != id {
		return invalid("WorkItemFactory returned id %s for continuation-derived id %s", item.WorkItemID, id)
	}
	s.created = append(s.created, item)
	return nil
}

func (s *continuationSink) RequireSignalSubscription(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execute.require_signal_subscription")
	defer func() { observe.DoneWith(obsOp, retErr) }()
	if err := s.durable.RequireSignalSubscription(ctx, ex, rec); err != nil {
		return err
	}
	return s.requireSignalSubscription(ctx, ex, rec)
}

func (s *continuationSink) RequireTimer(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execute.require_timer")
	defer func() { observe.DoneWith(obsOp, retErr) }()
	if err := s.durable.RequireTimer(ctx, ex, rec); err != nil {
		return err
	}
	// Without a TimerFactory this driver has no durable store for a timer and
	// must not pretend otherwise -- the pre-WF-RUN-004 behaviour, unchanged.
	if s.timers == nil {
		return unsupported("TIMER_REQUIRED", rec.TargetNodeID)
	}
	if s.plan == nil {
		return invalid("TIMER_REQUIRED for node %s but the sink was built without a pinned plan", rec.TargetNodeID)
	}
	node, ok := s.plan.Node(rec.TargetNodeID)
	if !ok {
		return invalid("TIMER_REQUIRED names node %s, which the pinned plan does not declare", rec.TargetNodeID)
	}
	handle, err := s.timers.CreateTimer(ctx, ex, TimerRequest{
		Continuation: rec, Plan: s.plan, Node: node, CreatedAt: rec.RecordedAt, Proposal: s.proposal,
	})
	if err != nil {
		return err
	}
	if handle.NodeID != "" && handle.NodeID != rec.TargetNodeID {
		return invalid("TimerFactory returned a timer for node %s while scheduling %s", handle.NodeID, rec.TargetNodeID)
	}
	handle.NodeID = rec.TargetNodeID
	s.timersCreated = append(s.timersCreated, handle)
	return nil
}

func (s *continuationSink) MarkReady(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execute.mark_ready")
	defer func() { observe.DoneWith(obsOp, retErr) }()
	return s.durable.MarkReady(ctx, ex, rec)
}

func (s *continuationSink) Complete(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execute.complete_continuation")
	defer func() { observe.DoneWith(obsOp, retErr) }()
	if err := s.durable.Complete(ctx, ex, rec); err != nil {
		return err
	}
	terminal := s.terminal
	if s.terminals != nil {
		if registered, ok := s.terminals.ResolveTerminal(s.workflowID, rec.TerminalCode); ok {
			terminal = registered
		} else {
			terminal = nil
		}
	}
	if terminal == nil || s.guard == nil {
		return invalid("COMPLETE for node %s requires TerminalWriter and idempotency Store", rec.TargetNodeID)
	}
	instrumentation := s.instrumentation
	if instrumentation == nil {
		instrumentation = NoopInstrumentation{}
	}
	termCtx, termSpan := instrumentation.StartTerminalSpan(ctx, SpanAttributes{
		InstanceID: rec.InstanceID.String(), NodeID: rec.TargetNodeID, TerminalCode: rec.TerminalCode,
	})

	digest := terminalDigest(s.planDigest, s.source, rec)
	effectRef := s.proposal.Revision.ProposalRevisionID
	if effectRef == "" {
		sourceBytes, _ := json.Marshal(s.source)
		effectRef = fmt.Sprintf("%s:%x", s.source.Kind, sha256.Sum256(sourceBytes))
	}
	scope := idempotency.Scope{
		Tenant: rec.TenantID, Capability: s.workflowID,
		EffectScope: "workflow-terminal:" + effectRef,
		Key:         s.startKey,
	}
	identity, err := idempotency.Guard(termCtx, s.tx, s.guard, scope, digest, s.policy, rec.RecordedAt,
		func(ctx context.Context, tx dbport.Tx) (idempotency.ResultIdentity, error) {
			return terminal.Write(ctx, tx, TerminalWriteRequest{
				TenantID: rec.TenantID, InstanceID: rec.InstanceID,
				WorkflowID: s.workflowID, PlanDigest: s.planDigest,
				Proposal: s.proposal, Source: s.source, TerminalCode: rec.TerminalCode,
				CorrelationID: s.correlationID, IdempotencyKey: s.startKey,
				RecordedAt:      rec.RecordedAt,
				EndNodeID:       s.endNodeID,
				EndOutputDigest: s.endOutputDigest,
			})
		})
	if err != nil {
		termSpan.End(OutcomeFailure, err)
		return err
	}
	termSpan.End(OutcomeSuccess, nil)
	if s.repair != nil && s.source.Proposal != nil && strings.Contains(rec.TerminalCode, "REPAIR") {
		if err := s.repair.Request(ctx, s.tx, RepairRequest{
			TenantID: rec.TenantID, InstanceID: rec.InstanceID, WorkflowID: s.workflowID,
			PlanDigest: s.planDigest, Proposal: s.proposal,
			EffectRef: "workflow:" + s.workflowID + ":" + rec.InstanceID.String(),
			PolicyRef: "repair:" + s.workflowID, IntendedRef: s.proposal.Revision.MaterialDigest.Digest,
			RepairPolicy: rec.TerminalCode, RequestedAt: rec.RecordedAt,
		}); err != nil {
			return fmt.Errorf("workflow execute: request repair: %w", err)
		}
	}

	// OBS-024: the TERMINAL_WRITTEN entry joins the terminal write on this
	// same transaction: the governed write and its evidence commit together
	// or roll back together.
	evidence := s.evidence
	if evidence == nil {
		evidence = NoopExecutionEvidence{}
	}
	evidenceID, evErr := recordTxEvidence(ctx, s.tx, evidence, rec.TenantID, EvidenceKindTerminalWritten,
		rec.InstanceID.String(), rec.TargetNodeID, identity.Identity.EventRef, digest, rec.RecordedAt)
	if evErr != nil {
		return fmt.Errorf("workflow execute: record TERMINAL_WRITTEN evidence: %w", evErr)
	}
	if evidenceID != "" {
		s.evidenceIDs = append(s.evidenceIDs, evidenceID)
	}
	return nil
}

func terminalDigest(planDigest string, source runtime.StartSource, rec runtime.ContinuationRecord) string {
	var revisionID, proposalDigest string
	if source.Proposal != nil {
		revisionID = source.Proposal.Revision.ProposalRevisionID
		proposalDigest = source.Proposal.Revision.MaterialDigest.Digest
	}
	material := struct {
		PlanDigest         string              `json:"plan_digest"`
		ProposalRevisionID string              `json:"proposal_revision_id"`
		ProposalDigest     string              `json:"proposal_digest"`
		Source             runtime.StartSource `json:"source"`
		InstanceID         string              `json:"instance_id"`
		TerminalCode       string              `json:"terminal_code"`
	}{}
	var b []byte
	var err error
	if source.Proposal != nil {
		// Keep Promotion's pre-WF-EXT-007 replay identity byte-for-byte stable.
		b, err = json.Marshal(struct {
			PlanDigest         string `json:"plan_digest"`
			ProposalRevisionID string `json:"proposal_revision_id"`
			ProposalDigest     string `json:"proposal_digest"`
			InstanceID         string `json:"instance_id"`
			TerminalCode       string `json:"terminal_code"`
		}{planDigest, revisionID, proposalDigest, rec.InstanceID.String(), rec.TerminalCode})
	} else {
		material.PlanDigest, material.Source = planDigest, source
		material.InstanceID, material.TerminalCode = rec.InstanceID.String(), rec.TerminalCode
		b, err = json.Marshal(material)
	}
	if err != nil {
		b = []byte(fmt.Sprintf("unencodable:%v", err))
	}
	h := sha256.New()
	h.Write([]byte("hcmnext.workflow.execute.TerminalWrite/v1"))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

var _ runtime.ContinuationSink = (*continuationSink)(nil)
