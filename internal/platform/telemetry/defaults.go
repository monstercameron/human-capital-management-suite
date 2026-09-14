package telemetry

// This file is the compiled-in mirror of
// definitions/telemetry/resource-contract.yaml and
// definitions/telemetry/policy.yaml. contract_test.go loads the real
// checked-in YAML files and asserts they compile to exactly these values,
// so the schema and the registry a process actually runs with can never
// silently drift apart (OBS-001 REFACTOR).

// DefaultAllowlistDefinitions is the compiled-in attribute allow-list
// mirroring definitions/telemetry/resource-contract.yaml's
// attribute_allowlist section.
func DefaultAllowlistDefinitions() []AttributeDefinition {
	return []AttributeDefinition{
		{
			Key: "cell_id", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalResource, SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 64,
			Description:    "Bounded cell identifier; never a tenant identity.",
		},
		{
			Key: "environment", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalResource, SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 8,
			Description:    "Deployment environment name.",
		},
		{
			Key: "process_role", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalResource, SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 16,
			Description:    "Runtime role of the emitting process.",
		},
		{
			Key: "tenant_class", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalResource, SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 8,
			Description:    "Bounded tenant tier bucket, never a tenant id.",
		},
		{
			Key: "outcome", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 8,
			Description:    "Typed operation outcome.",
		},
		{
			Key: "capability_id", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 128,
			Description:    "Registry-owned capability identifier.",
		},
		{
			Key: "workflow_node", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 128,
			Description:    "Registry-owned workflow node name.",
		},
		{
			Key: "edge", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 32,
			Description:    "Named replication/consistency edge for parity checks.",
		},
		{
			Key: "operation", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 256,
			Description:    "Bounded registry-named boundary operation (OBS-014 boundary instrumentation).",
		},
		{
			Key: "route", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 256,
			Description:    "Route template of a transport boundary, never a concrete path.",
		},
		{
			Key: "dependency", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 64,
			Description:    "Named external or storage dependency behind a boundary call.",
		},
		{
			Key: "status", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 32,
			Description:    "Bounded status class of a boundary call (HTTP or gRPC status family).",
		},
		{
			Key: "size_class", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 8,
			Description:    "Bucketed payload size class, never a raw byte count.",
		},
		{
			Key: "retry", Class: ClassOperationalPublic,
			Signals:        []SignalKind{SignalLog, SignalSpan, SignalMetric},
			MaxCardinality: 2,
			Description:    "Whether the boundary call was a retry attempt.",
		},
		{
			Key: "error_type", Class: ClassOperationalRestricted,
			Signals:        []SignalKind{SignalLog, SignalSpan},
			MaxCardinality: 0,
			Description:    "Classified error type; unbounded on logs/traces only, never a metric label.",
		},
		{
			Key: "correlation_id", Class: ClassOperationalRestricted,
			Signals:        []SignalKind{SignalLog, SignalSpan},
			MaxCardinality: 0,
			Description:    "Business correlation identifier; never a metric label.",
		},
		{
			Key: "logical_operation_id", Class: ClassOperationalRestricted,
			Signals:        []SignalKind{SignalSpan},
			MaxCardinality: 0,
			Description:    "Bounded durable logical operation identifier; span context only, never authority or a metric label.",
		},
		{
			Key: "attempt_id", Class: ClassOperationalRestricted,
			Signals:        []SignalKind{SignalSpan},
			MaxCardinality: 0,
			Description:    "Bounded durable attempt identifier; span context only, never authority or a metric label.",
		},
		{
			Key: "node_id", Class: ClassOperationalRestricted,
			Signals:        []SignalKind{SignalSpan},
			MaxCardinality: 0,
			Description:    "Workflow node identifier for trace-to-inspector pivoting; span context only, never authority or a metric label.",
		},
		{
			Key: "terminal_code", Class: ClassOperationalRestricted,
			Signals:        []SignalKind{SignalSpan},
			MaxCardinality: 32,
			Description:    "Closed workflow terminal outcome vocabulary; span context only, never payload or authority.",
		},
		{
			Key: "timer_id", Class: ClassOperationalRestricted,
			Signals:        []SignalKind{SignalSpan},
			MaxCardinality: 0,
			Description:    "Durable timer identifier attributing a timer-fire or timer-resume span; span context only, never authority or a metric label.",
		},
		{
			Key: "message_kind", Class: ClassOperationalRestricted,
			Signals:        []SignalKind{SignalSpan},
			MaxCardinality: 16,
			Description:    "Closed asynchronous message-kind vocabulary; span context only, never payload, authority, or metric label.",
		},
	}
}

// DefaultAllowlist compiles DefaultAllowlistDefinitions into an Allowlist.
func DefaultAllowlist() (*Allowlist, error) {
	return NewAllowlist(DefaultAllowlistDefinitions()...)
}

// DefaultPolicyVersion is the compiled-in policy schema version, mirroring
// definitions/telemetry/policy.yaml's top-level version.
const DefaultPolicyVersion = 1

// DefaultSamplingPolicy mirrors definitions/telemetry/policy.yaml's
// sampling section.
func DefaultSamplingPolicy() SamplingPolicy {
	return SamplingPolicy{Version: 1, SuccessSampleRate: 0.1}
}

// DefaultForcedRetentionClasses mirrors
// definitions/telemetry/policy.yaml's sampling.forced_retention_classes.
func DefaultForcedRetentionClasses() []RetentionClass {
	return []RetentionClass{
		RetentionSecurityDenial,
		RetentionFinancialMutation,
		RetentionIrreversibleEffect,
		RetentionAmbiguousResult,
		RetentionCorrectnessFailure,
		RetentionTelemetryPipelineFailure,
	}
}
