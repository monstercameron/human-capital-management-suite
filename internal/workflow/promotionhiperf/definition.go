package promotionhiperf

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

const (
	// WorkflowID is the published identity of the high-performer variant.
	WorkflowID = "hcmnext.workflows.promotion.high_performer"
	// Version is the immutable definition version of the variant graph.
	Version = 1
	// SemanticVersion is the human-facing identity [Definition] publishes.
	SemanticVersion = "1.0.0"

	// NodeFetchMarketRate reads the market anchor the variant's raise floor
	// is computed from.
	NodeFetchMarketRate = "fetch_market_rate"

	// CapabilityMarketRate is the market-rate read the variant's fetch node
	// invokes (HIPERF-001's hcmnext.rewards.market_rate/v1 port).
	CapabilityMarketRate = "hcmnext.rewards.market_rate"
)

// Definition returns the high-performer variant graph: the shared execute
// node set (no copied node literal) under the variant identity, plus the
// fetch_market_rate node between snapshot_worker and
// simulate_compensation. The fetch's anchor digest feeds
// simulate_compensation and the resulting market floor feeds
// raise_threshold; every other node, edge, approval and policy is the
// execute graph unchanged.
func Definition() workflow.Definition {
	def := promotionexec.Definition()
	def.WorkflowID = WorkflowID
	def.Version = Version
	def.Name = "High-performer promotion"
	def.InputSchema = schema("HighPerformerPromotionInput")
	def.Inputs = append(def.Inputs,
		workflow.Field{Path: "target_grade", Type: plainString()},
		workflow.Field{Path: "market_pay_zone", Type: plainString()},
		workflow.Field{Path: "market_currency", Type: plainString()},
		workflow.Field{Path: "market_as_of", Type: localDate()},
	)
	withMarketInputs(&def)
	def.Nodes = append(def.Nodes, fetchMarketRateNode())
	def.Edges = variantEdges(def.Edges)
	return def
}

// withMarketInputs adds the market dataflow to the shared simulate and
// threshold nodes: simulate takes the fetch's anchor digest and reports the
// market floor, and the threshold decides over that floor next to the raise
// ratio.
func withMarketInputs(def *workflow.Definition) {
	for i := range def.Nodes {
		switch def.Nodes[i].ID {
		case promotionexec.NodeSimulateCompensation:
			def.Nodes[i].Inputs = append(def.Nodes[i].Inputs,
				workflow.Field{Path: "market_anchor", Type: plainString()})
			def.Nodes[i].Outputs = append(def.Nodes[i].Outputs,
				workflow.Field{Path: "market_floor", Type: money()})
			def.Nodes[i].InputMappings = append(def.Nodes[i].InputMappings,
				workflow.Mapping{Target: "market_anchor", Source: output(NodeFetchMarketRate, "anchor_digest")})
		case promotionexec.NodeRaiseThreshold:
			def.Nodes[i].Inputs = append(def.Nodes[i].Inputs,
				workflow.Field{Path: "market_floor", Type: money()},
				workflow.Field{Path: "market_anchor", Type: plainString()})
			def.Nodes[i].InputMappings = append(def.Nodes[i].InputMappings,
				workflow.Mapping{Target: "market_floor", Source: output(promotionexec.NodeSimulateCompensation, "market_floor")},
				workflow.Mapping{Target: "market_anchor", Source: output(NodeFetchMarketRate, "anchor_digest")})
		}
	}
}

// fetchMarketRateNode is the variant's one new node: a read-only capability
// call resolving the market anchor for the target scope.
func fetchMarketRateNode() workflow.Node {
	return workflow.Node{
		ID: NodeFetchMarketRate, Type: workflow.StepCapability,
		InputSchema: capabilitySchema(CapabilityMarketRate, "request"), OutputSchema: capabilitySchema(CapabilityMarketRate, "response"),
		Inputs: []workflow.Field{
			{Path: "job_code", Type: brandedString("JobID")},
			{Path: "grade", Type: plainString()},
			{Path: "pay_zone", Type: plainString()},
			{Path: "currency", Type: plainString()},
			{Path: "as_of", Type: localDate()},
		},
		Outputs: []workflow.Field{
			{Path: "anchor_p25", Type: money()},
			{Path: "anchor_p50", Type: money()},
			{Path: "anchor_p75", Type: money()},
			{Path: "anchor_as_of", Type: localDate()},
			{Path: "anchor_digest", Type: plainString()},
			{Path: "source_version", Type: plainString()},
		},
		InputMappings: []workflow.Mapping{
			{Target: "job_code", Source: input("target_job_id")},
			{Target: "grade", Source: input("target_grade")},
			{Target: "pay_zone", Source: input("market_pay_zone")},
			{Target: "currency", Source: input("market_currency")},
			{Target: "as_of", Source: input("market_as_of")},
		},
		Capability: &workflow.CapabilityRef{ID: CapabilityMarketRate, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:rewards.read"}},
		Governance: invocation(),
	}
}

// variantEdges reroutes the snapshot's success through the fetch: the fetch
// carries the snapshot's own outcome vocabulary, and the simulation now
// runs on the fetch's success.
func variantEdges(edges []workflow.Edge) []workflow.Edge {
	out := make([]workflow.Edge, 0, len(edges)+4)
	for _, edge := range edges {
		if edge.From == promotionexec.NodeSnapshotWorker && edge.To == promotionexec.NodeSimulateCompensation && edge.RouteKey == "SUCCEEDED" {
			continue
		}
		out = append(out, edge)
	}
	return append(out,
		workflow.Edge{From: promotionexec.NodeSnapshotWorker, To: NodeFetchMarketRate, RouteKey: "SUCCEEDED"},
		workflow.Edge{From: NodeFetchMarketRate, To: promotionexec.NodeSimulateCompensation, RouteKey: "SUCCEEDED"},
		workflow.Edge{From: NodeFetchMarketRate, To: promotionexec.NodeEndRejected, RouteKey: "REJECTED"},
		workflow.Edge{From: NodeFetchMarketRate, To: promotionexec.NodeEndInvalidated, RouteKey: "UNKNOWN"},
		workflow.Edge{From: NodeFetchMarketRate, To: promotionexec.NodeEndInvalidated, RouteKey: "AMBIGUOUS"},
	)
}

func schema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         "hcmnext.workflows.promotion.high_performer." + name + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.workflows.promotion.high_performer." + name,
	}
}

func capabilitySchema(id, slot string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func brandedString(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}

func plainString() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }
func money() workflow.ValueType       { return workflow.ValueType{Kind: workflow.KindMoney} }
func localDate() workflow.ValueType   { return workflow.ValueType{Kind: workflow.KindLocalDate} }

func input(path string) workflow.Source {
	return workflow.Source{Kind: workflow.SourceWorkflowInput, Path: path}
}

func output(node, path string) workflow.Source {
	return workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: node, Path: path}
}

func invocation() workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:               "PROMOTION_EXECUTION",
		Classification:        "CONFIDENTIAL_HR",
		RequiredDecisions:     []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk},
		RevalidationBoundary:  workflow.RevalidatePreExecution,
		DataAccessManifestRef: "data-access.promotion.execution/v1",
	}
}

// Compile compiles the EXECUTE projection of the variant graph.
func Compile(definitions ...workflow.Definition) (*workflow.CompiledWorkflow, error) {
	def := Definition()
	if len(definitions) == 1 {
		def = definitions[0]
	}
	return workflow.Compile(promotionexec.ProjectMode(def, workflow.ModeExecute), workflow.Options{Phase: workflow.PhaseP1B, Capabilities: capabilities(workflow.ModeExecute)})
}

// CompileSimulation compiles the zero-effect SIMULATE projection of the
// variant graph.
func CompileSimulation(definitions ...workflow.Definition) (*workflow.CompiledWorkflow, error) {
	def := Definition()
	if len(definitions) == 1 {
		def = definitions[0]
	}
	return workflow.Compile(promotionexec.ProjectMode(def, workflow.ModeSimulate), workflow.Options{Phase: workflow.PhaseP1B, Capabilities: capabilities(workflow.ModeSimulate)})
}

// CapabilityIDs returns the exact capability identities the variant graph
// invokes: the execute set plus the market-rate read.
func CapabilityIDs() []string {
	ids := append([]string(nil), promotionexec.CapabilityIDs()...)
	return append(ids, CapabilityMarketRate)
}

// NodeOrder returns the deterministic documented order used by the
// package's golden test: the execute order with the market-rate fetch
// between the snapshot and the compensation simulation.
func NodeOrder() []string {
	order := []string{promotionexec.NodeSnapshotWorker, NodeFetchMarketRate}
	return append(order, promotionexec.NodeOrder()[1:]...)
}

type staticCapabilities map[capability.Key]capability.Record

func (r staticCapabilities) Lookup(key capability.Key) (capability.Record, bool) {
	record, ok := r[key]
	return record, ok
}

func capabilityRecord(id, owner string, effect capability.EffectClass, scope string) capability.Record {
	return capability.Record{Definition: capability.Definition{ID: id, Version: 1, OwnerDomain: owner, RequestSchema: capability.SchemaRef{SchemaID: id + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}, ResponseSchema: capability.SchemaRef{SchemaID: id + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}, ErrorSchema: capability.SchemaRef{SchemaID: id + ".error/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}, EffectClass: effect, IdempotencyPolicyRef: "idempotency.promotion." + owner + ".v1", AuthZScopeRef: scope, LegalBasisRef: "legal.promotion.execution/v1", EntitlementRef: "entitlement.promotion.execution/v1", SLOClassRef: "slo.promotion.execution/v1", TestRef: "conformance:" + id + "/v1"}, Status: capability.StatusActive, Digest: "sha256:promotionhiperf-" + owner}
}

func capabilities(mode workflow.ExecutionMode) workflow.CapabilityResolver {
	promotionEffect := capability.EffectInternalMutation
	releaseEffect := capability.EffectInternalMutation
	if mode == workflow.ModeSimulate {
		promotionEffect = capability.EffectReadOnly
		releaseEffect = capability.EffectReadOnly
	}
	return staticCapabilities{
		{ID: "hcmnext.people.explain_worker_state", Version: 1}:         capabilityRecord("hcmnext.people.explain_worker_state", "people", capability.EffectReadOnly, "scope:people.read"),
		{ID: "hcmnext.rewards.simulate_compensation", Version: 1}:       capabilityRecord("hcmnext.rewards.simulate_compensation", "rewards", capability.EffectReadOnly, "scope:rewards.read"),
		{ID: "hcmnext.rewards.evaluate_pay_band_position", Version: 1}:  capabilityRecord("hcmnext.rewards.evaluate_pay_band_position", "rewards", capability.EffectReadOnly, "scope:rewards.read"),
		{ID: CapabilityMarketRate, Version: 1}:                          capabilityRecord(CapabilityMarketRate, "rewards", capability.EffectReadOnly, "scope:rewards.read"),
		{ID: "internal/governance/revalidate", Version: 1}:              capabilityRecord("internal/governance/revalidate", "governance", capability.EffectReadOnly, "scope:governance.read"),
		{ID: "hcmnext.people.promote_worker", Version: 1}:               capabilityRecord("hcmnext.people.promote_worker", "people", promotionEffect, "scope:people.write"),
		{ID: "hcmnext.payroll.observe_promotion", Version: 1}:           capabilityRecord("hcmnext.payroll.observe_promotion", "payroll", capability.EffectReadOnly, "scope:observation.read"),
		{ID: "hcmnext.access.observe_promotion", Version: 1}:            capabilityRecord("hcmnext.access.observe_promotion", "access", capability.EffectReadOnly, "scope:observation.read"),
		{ID: "hcmnext.reconciliation.observe_promotion", Version: 1}:    capabilityRecord("hcmnext.reconciliation.observe_promotion", "reconciliation", capability.EffectReadOnly, "scope:observation.read"),
		{ID: "hcmnext.rewards.release_compensation_budget", Version: 1}: capabilityRecord("hcmnext.rewards.release_compensation_budget", "rewards", releaseEffect, "scope:rewards.write"),
	}
}

// HasMarketRate reports whether plan carries the variant's market-rate
// capability binding. A nil plan has none.
func HasMarketRate(plan *workflow.CompiledWorkflow) bool {
	if plan == nil {
		return false
	}
	_, ok := plan.Node(NodeFetchMarketRate)
	return ok
}
