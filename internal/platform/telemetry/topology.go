package telemetry

import (
	"errors"
	"sort"
	"strings"
)

// SpanTopologyVersion is incremented when a published span name or its
// meaning changes.  Names are an operational API, so callers must select a
// definition from this registry rather than constructing names from input.
const SpanTopologyVersion = 1

const maxSpanAttributeValue = 256

// SpanName is the complete, bounded name of a semantic span.  It contains no
// tenant, worker, URL, error, or attempt value.
type SpanName string

const (
	SpanHTTPServer         SpanName = "hcmnext.http.server"
	SpanGRPCServer         SpanName = "hcmnext.grpc.server"
	SpanCapabilityInvoke   SpanName = "hcmnext.capability.invoke"
	SpanIntentCreate       SpanName = "hcmnext.intent.create"
	SpanIntentAdvance      SpanName = "hcmnext.intent.advance"
	SpanWorkflowStart      SpanName = "hcmnext.workflow.start"
	SpanWorkflowNode       SpanName = "hcmnext.workflow.node"
	SpanTransactionPrepare SpanName = "hcmnext.transaction.prepare"
	SpanTransactionCommit  SpanName = "hcmnext.transaction.commit"
	SpanDBOperation        SpanName = "hcmnext.db.operation"
	SpanOutboxPublish      SpanName = "hcmnext.outbox.publish"
	SpanQueueDeliver       SpanName = "hcmnext.queue.deliver"
	SpanConnectorDispatch  SpanName = "hcmnext.connector.dispatch"
	SpanProviderCall       SpanName = "hcmnext.provider.call"
	SpanObservation        SpanName = "hcmnext.observation"
	SpanReconciliation     SpanName = "hcmnext.reconciliation"
	SpanRepair             SpanName = "hcmnext.repair"
	SpanProjectorApply     SpanName = "hcmnext.projector.apply"
	SpanJobPartition       SpanName = "hcmnext.job.partition"
)

type SpanFamily string

const (
	FamilyTransport      SpanFamily = "transport"
	FamilyCapability     SpanFamily = "capability"
	FamilyIntent         SpanFamily = "intent"
	FamilyWorkflow       SpanFamily = "workflow"
	FamilyTransaction    SpanFamily = "transaction"
	FamilyDatabase       SpanFamily = "database"
	FamilyMessaging      SpanFamily = "messaging"
	FamilyConnector      SpanFamily = "connector"
	FamilyReconciliation SpanFamily = "reconciliation"
	FamilyProjection     SpanFamily = "projection"
	FamilyJob            SpanFamily = "job"
)

// SpanAttribute is a registered, low-cardinality attribute key.
type SpanAttribute struct {
	Key      string
	Required bool
}

// SpanDefinition describes the topology contract for one operation.
type SpanDefinition struct {
	Name            SpanName
	Family          SpanFamily
	Version         int
	DurableBoundary bool
	HasAttempt      bool
	Attributes      []SpanAttribute
}

// CanonicalSpanTopology returns a defensive copy of the published registry.
func CanonicalSpanTopology() []SpanDefinition {
	defs := append([]SpanDefinition(nil), canonicalSpanTopology...)
	for i := range defs {
		defs[i].Attributes = append([]SpanAttribute(nil), defs[i].Attributes...)
	}
	return defs
}

// SpanDefinitionFor looks up a published semantic span name.
func SpanDefinitionFor(name SpanName) (SpanDefinition, bool) {
	for _, d := range canonicalSpanTopology {
		if d.Name == name {
			d.Attributes = append([]SpanAttribute(nil), d.Attributes...)
			return d, true
		}
	}
	return SpanDefinition{}, false
}

// Status is the status of this span's operation, not of a parent or a
// business object observed by it.
type SpanStatus string

const (
	SpanStatusOK    SpanStatus = "OK"
	SpanStatusError SpanStatus = "ERROR"
)

// StatusFor applies the stable status rule. Denied and cancelled are
// intentional business results; provider acceptance is success only for the
// provider operation and never implies transaction commit.
func StatusFor(outcome Outcome) SpanStatus {
	switch outcome {
	case OutcomeSuccess, OutcomeDenied, OutcomeCancelled:
		return SpanStatusOK
	default:
		return SpanStatusError
	}
}

var (
	ErrUnknownSpan          = errors.New("telemetry: span is not in canonical topology")
	ErrDynamicSpanAttribute = errors.New("telemetry: dynamic span attribute")
	ErrSpanAttributeValue   = errors.New("telemetry: span attribute value exceeds bound")
)

// ValidateSpanAttributes verifies keys and bounds without retaining caller
// data. It deliberately accepts only registered keys.
func ValidateSpanAttributes(name SpanName, attrs map[string]string) error {
	d, ok := SpanDefinitionFor(name)
	if !ok {
		return ErrUnknownSpan
	}
	allowed := make(map[string]bool, len(d.Attributes))
	for _, a := range d.Attributes {
		allowed[a.Key] = true
	}
	for k, v := range attrs {
		if !allowed[k] || strings.TrimSpace(k) == "" {
			return ErrDynamicSpanAttribute
		}
		if len(v) > maxSpanAttributeValue {
			return ErrSpanAttributeValue
		}
	}
	return nil
}

var canonicalSpanTopology = []SpanDefinition{
	{SpanHTTPServer, FamilyTransport, 1, false, false, attrs("correlation_id", "logical_operation_id")},
	{SpanGRPCServer, FamilyTransport, 1, false, false, attrs("correlation_id", "logical_operation_id")},
	{SpanCapabilityInvoke, FamilyCapability, 1, false, true, attrs("logical_operation_id", "attempt_id", "capability")},
	{SpanIntentCreate, FamilyIntent, 1, false, true, attrs("logical_operation_id", "attempt_id")},
	{SpanIntentAdvance, FamilyIntent, 1, true, true, attrs("logical_operation_id", "attempt_id")},
	{SpanWorkflowStart, FamilyWorkflow, 1, true, true, attrs("logical_operation_id", "attempt_id", "workflow_id")},
	{SpanWorkflowNode, FamilyWorkflow, 1, true, true, attrs("logical_operation_id", "attempt_id", "workflow_id", "node_id")},
	{SpanTransactionPrepare, FamilyTransaction, 1, false, true, attrs("logical_operation_id", "attempt_id")},
	{SpanTransactionCommit, FamilyTransaction, 1, false, true, attrs("logical_operation_id", "attempt_id", "commit_state")},
	{SpanDBOperation, FamilyDatabase, 1, false, true, attrs("logical_operation_id", "attempt_id", "db_operation")},
	{SpanOutboxPublish, FamilyMessaging, 1, true, true, attrs("logical_operation_id", "attempt_id", "message_kind")},
	{SpanQueueDeliver, FamilyMessaging, 1, true, true, attrs("logical_operation_id", "attempt_id", "message_kind")},
	{SpanConnectorDispatch, FamilyConnector, 1, true, true, attrs("logical_operation_id", "attempt_id", "connector")},
	{SpanProviderCall, FamilyConnector, 1, false, true, attrs("logical_operation_id", "attempt_id", "provider_operation",
		"provider", "outcome_class", "change_ref", "correlation_id", "outcome", "status", "error_type", "retry_delay_ms", "breaker_state", "from_state", "to_state", "secret_index", "secret_slot", "event_id", "result")},
	{SpanObservation, FamilyReconciliation, 1, false, true, attrs("logical_operation_id", "attempt_id", "observation_kind")},
	{SpanReconciliation, FamilyReconciliation, 1, false, true, attrs("logical_operation_id", "attempt_id", "consistency_state")},
	{SpanRepair, FamilyReconciliation, 1, true, true, attrs("logical_operation_id", "attempt_id", "repair_kind")},
	{SpanProjectorApply, FamilyProjection, 1, true, true, attrs("logical_operation_id", "attempt_id", "projection")},
	{SpanJobPartition, FamilyJob, 1, true, true, attrs("logical_operation_id", "attempt_id", "partition")},
}

func attrs(keys ...string) []SpanAttribute {
	out := make([]SpanAttribute, len(keys))
	for i, k := range keys {
		out[i] = SpanAttribute{Key: k}
	}
	return out
}

// CanonicalSpanNames is useful to deterministic linters and golden tests.
func CanonicalSpanNames() []SpanName {
	out := make([]SpanName, len(canonicalSpanTopology))
	for i, d := range canonicalSpanTopology {
		out[i] = d.Name
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
