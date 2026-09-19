package telemetry

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// PromotionTopologyVersion is the compatibility version of the complete
// promotion execution topology. A name or parent change requires a new
// version; adding a row is compatible and remains observable through the
// registry digest.
const PromotionTopologyVersion = 1

const (
	SpanGatewayDecision  SpanName = "hcmnext.gateway.decision"
	SpanApprovalComplete SpanName = "hcmnext.approval.complete"
	SpanTimerFire        SpanName = "hcmnext.timer.fire"
	SpanTimerResume      SpanName = "hcmnext.timer.resume"
	SpanLedgerAppend     SpanName = "hcmnext.ledger.append"
	SpanOutboxEnqueue    SpanName = "hcmnext.outbox.enqueue"
	SpanSchedulerTick    SpanName = "hcmnext.scheduler.tick"
	SpanWorkflowAdvance  SpanName = "hcmnext.workflow.advance"
	SpanWorkflowTerminal SpanName = "hcmnext.workflow.terminal"
)

// TopologyRow is the complete semantic contract for one published span. The
// parent is a logical parent, never a database row id or an attempt id.
type TopologyRow struct {
	Name            SpanName
	Parent          SpanName
	Family          SpanFamily
	Version         int
	DurableBoundary bool
	HasAttempt      bool
	Attributes      []SpanAttribute
}

// TopologyRegistry is an immutable, validated view of the published rows.
type TopologyRegistry struct {
	version int
	rows    map[SpanName]TopologyRow
}

var (
	ErrTopologyDuplicate = errors.New("telemetry: duplicate topology row")
	ErrTopologyParent    = errors.New("telemetry: topology parent is not registered")
	ErrTopologyVersion   = errors.New("telemetry: topology version mismatch")
	ErrTopologyEmission  = errors.New("telemetry: span emission is not registered")
)

// CanonicalPromotionTopology returns a defensive copy of the end-to-end
// Promotion path. The root edge request may be HTTP or gRPC; the remaining
// rows are the shared logical path beneath the admitted request.
func CanonicalPromotionTopology() []TopologyRow {
	rows := []TopologyRow{
		{SpanHTTPServer, "", FamilyTransport, 1, false, false, topologyAttrs("correlation_id", "logical_operation_id")},
		{SpanGRPCServer, "", FamilyTransport, 1, false, false, topologyAttrs("correlation_id", "logical_operation_id")},
		{SpanGatewayDecision, SpanHTTPServer, FamilyTransport, 1, false, false, topologyAttrs("logical_operation_id", "decision")},
		{SpanCapabilityInvoke, SpanGatewayDecision, FamilyCapability, 1, false, true, topologyAttrs("logical_operation_id", "attempt_id", "capability")},
		{SpanIntentCreate, SpanCapabilityInvoke, FamilyIntent, 1, false, true, topologyAttrs("logical_operation_id", "attempt_id")},
		{SpanIntentAdvance, SpanIntentCreate, FamilyIntent, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id")},
		{SpanWorkflowStart, SpanIntentAdvance, FamilyWorkflow, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "workflow_id")},
		{SpanWorkflowNode, SpanWorkflowStart, FamilyWorkflow, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "workflow_id", "node_id")},
		{SpanApprovalComplete, SpanWorkflowNode, FamilyWorkflow, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "node_id")},
		{SpanTimerFire, SpanWorkflowNode, FamilyJob, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "timer_id")},
		{SpanTimerResume, SpanTimerFire, FamilyJob, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "timer_id")},
		{SpanTransactionPrepare, SpanWorkflowNode, FamilyTransaction, 1, false, true, topologyAttrs("logical_operation_id", "attempt_id")},
		{SpanTransactionCommit, SpanTransactionPrepare, FamilyTransaction, 1, false, true, topologyAttrs("logical_operation_id", "attempt_id", "commit_state")},
		{SpanDBOperation, SpanTransactionCommit, FamilyDatabase, 1, false, true, topologyAttrs("logical_operation_id", "attempt_id", "db_operation")},
		{SpanLedgerAppend, SpanTransactionCommit, FamilyTransaction, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "stream_kind")},
		{SpanOutboxEnqueue, SpanTransactionCommit, FamilyMessaging, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "message_kind")},
		{SpanOutboxPublish, SpanOutboxEnqueue, FamilyMessaging, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "message_kind")},
		{SpanSchedulerTick, "", FamilyJob, 1, false, true, topologyAttrs("logical_operation_id", "attempt_id", "scheduler")},
		{SpanQueueDeliver, SpanSchedulerTick, FamilyMessaging, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "message_kind")},
		{SpanConnectorDispatch, SpanWorkflowNode, FamilyConnector, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "connector")},
		{SpanProviderCall, SpanConnectorDispatch, FamilyConnector, 1, false, true, topologyAttrs("logical_operation_id", "attempt_id", "provider_operation",
			"provider", "outcome_class", "change_ref", "correlation_id", "outcome", "status", "error_type", "retry_delay_ms", "breaker_state", "from_state", "to_state", "secret_index", "secret_slot", "event_id", "result")},
		{SpanObservation, SpanWorkflowNode, FamilyReconciliation, 1, false, true, topologyAttrs("logical_operation_id", "attempt_id", "observation_kind")},
		{SpanReconciliation, SpanObservation, FamilyReconciliation, 1, false, true, topologyAttrs("logical_operation_id", "attempt_id", "consistency_state")},
		{SpanRepair, SpanReconciliation, FamilyReconciliation, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "repair_kind")},
		{SpanProjectorApply, SpanLedgerAppend, FamilyProjection, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "projection")},
		{SpanJobPartition, SpanSchedulerTick, FamilyJob, 1, true, true, topologyAttrs("logical_operation_id", "attempt_id", "partition")},
		{SpanWorkflowAdvance, SpanWorkflowNode, FamilyWorkflow, 1, false, true, topologyAttrs("logical_operation_id", "attempt_id")},
		{SpanWorkflowTerminal, SpanTransactionCommit, FamilyWorkflow, 1, false, true, topologyAttrs("logical_operation_id", "attempt_id", "terminal_code")},
	}
	return cloneTopologyRows(rows)
}

// NewTopologyRegistry validates and compiles rows into a lookup registry.
func NewTopologyRegistry(rows []TopologyRow, version int) (*TopologyRegistry, error) {
	if version <= 0 {
		return nil, ErrTopologyVersion
	}
	compiled := make(map[SpanName]TopologyRow, len(rows))
	for _, row := range rows {
		if row.Name == "" || row.Version != version || row.Family == "" {
			return nil, fmt.Errorf("%w: row %q", ErrTopologyVersion, row.Name)
		}
		if _, exists := compiled[row.Name]; exists {
			return nil, fmt.Errorf("%w: %q", ErrTopologyDuplicate, row.Name)
		}
		if row.Parent == row.Name {
			return nil, fmt.Errorf("%w: self-parent %q", ErrTopologyParent, row.Name)
		}
		row.Attributes = append([]SpanAttribute(nil), row.Attributes...)
		compiled[row.Name] = row
	}
	for _, row := range compiled {
		if row.Parent != "" {
			if _, ok := compiled[row.Parent]; !ok {
				return nil, fmt.Errorf("%w: %q -> %q", ErrTopologyParent, row.Name, row.Parent)
			}
		}
	}
	return &TopologyRegistry{version: version, rows: compiled}, nil
}

// CanonicalPromotionRegistry returns the validated promotion registry.
func CanonicalPromotionRegistry() *TopologyRegistry {
	r, err := NewTopologyRegistry(CanonicalPromotionTopology(), PromotionTopologyVersion)
	if err != nil {
		panic(err)
	}
	return r
}

// Version reports the registry version.
func (r *TopologyRegistry) Version() int {
	if r == nil {
		return 0
	}
	return r.version
}

// Lookup returns a defensive copy of a topology row.
func (r *TopologyRegistry) Lookup(name SpanName) (TopologyRow, bool) {
	if r == nil {
		return TopologyRow{}, false
	}
	row, ok := r.rows[name]
	row.Attributes = append([]SpanAttribute(nil), row.Attributes...)
	return row, ok
}

// Rows returns all rows in stable name order.
func (r *TopologyRegistry) Rows() []TopologyRow {
	if r == nil {
		return nil
	}
	names := make([]SpanName, 0, len(r.rows))
	for name := range r.rows {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	rows := make([]TopologyRow, 0, len(names))
	for _, name := range names {
		row, _ := r.Lookup(name)
		rows = append(rows, row)
	}
	return rows
}

// ValidateEmission checks a span name and its attributes against the
// registry. Transport middleware is allowed to use a manifest procedure as
// its mechanical name; it is still classified as the registered edge span.
func (r *TopologyRegistry) ValidateEmission(name string, attributes map[string]string) error {
	semanticName := SpanName(name)
	if strings.HasPrefix(name, "/") {
		semanticName = SpanHTTPServer
	}
	row, ok := r.Lookup(semanticName)
	if !ok {
		return fmt.Errorf("%w: %q", ErrTopologyEmission, name)
	}
	allowed := make(map[string]bool, len(row.Attributes))
	for _, attr := range row.Attributes {
		allowed[attr.Key] = true
	}
	for key, value := range attributes {
		if !allowed[key] || strings.TrimSpace(key) == "" || len(value) > maxSpanAttributeValue {
			return fmt.Errorf("%w: %q", ErrTopologyEmission, key)
		}
	}
	return nil
}

// ExplainTopology is a compact, stable explanation for diagnostics and
// policy tooling; it contains names and parents only, never caller values.
func (r *TopologyRegistry) ExplainTopology() string {
	rows := r.Rows()
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, string(row.Name)+"<-"+string(row.Parent))
	}
	return strings.Join(parts, ",")
}

func topologyAttrs(keys ...string) []SpanAttribute {
	out := make([]SpanAttribute, len(keys))
	for i, key := range keys {
		out[i] = SpanAttribute{Key: key}
	}
	return out
}

func cloneTopologyRows(rows []TopologyRow) []TopologyRow {
	out := make([]TopologyRow, len(rows))
	for i, row := range rows {
		row.Attributes = append([]SpanAttribute(nil), row.Attributes...)
		out[i] = row
	}
	return out
}
