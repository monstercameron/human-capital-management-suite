// Package mobility is CONF-015's proof that mobility, immigration and
// cross-border privacy evidence remain explicit, dated and independently
// governed through the real SIMULATE-mode workflow interpreter.
//
// This is a conformance fixture, not a mobility product. Its capability
// handlers are pinned in-memory observations. The workflow composes dated
// mobility legs, home/host payroll facts, authorization milestones, tax/PE
// uncertainty, a privacy-transfer decision and an immigration-vendor
// observation without turning any observation into production authority.
package mobility

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

const (
	WorkflowID = "hcmnext.workflows.mobility_cross_border_privacy"
	Version    = 1
)

const (
	NodeReadMobilityLegs      = "read_mobility_legs"
	NodeReadWorkAuthorization = "read_work_authorization"
	NodeResolveTaxPE          = "resolve_tax_pe"
	NodeResolvePrivacy        = "resolve_privacy_transfer"
	NodeComputeFootprint      = "compute_mobility_footprint"
	NodeBuildProposal         = "build_mobility_proposal"
	NodeMobilityDecision      = "mobility_decision"
	NodeObserveImmigration    = "observe_immigration_vendor"

	NodeEndPendingApprovals      = "end_pending_approvals"
	NodeEndExpiringAuthorization = "end_expiring_authorization"
	NodeEndAuthorizationBlocked  = "end_authorization_blocked"
	NodeEndMilestoneBlocked      = "end_authorization_milestone_blocked"
	NodeEndOverlappingLegBlocked = "end_overlapping_leg_blocked"
	NodeEndPrivacyBlocked        = "end_privacy_transfer_blocked"
	NodeEndTaxPEUnknown          = "end_tax_pe_unknown"
	NodeEndDegradedRepair        = "end_degraded_repair"
	NodeEndUnknown               = "end_unknown"
)

const (
	RouteAuthorizationBlocked          = "AUTHORIZATION_BLOCKED"
	RouteAuthorizationMilestoneBlocked = "AUTHORIZATION_MILESTONE_INCOMPLETE"
	RouteOverlappingLegBlocked         = "OVERLAPPING_ASSIGNMENT_BLOCKED"
	RoutePrivacyTransferBlocked        = "PRIVACY_TRANSFER_PROHIBITED"
	RouteTaxPEUnknown                  = "TAX_PE_IMPACT_UNKNOWN"
	RouteAuthorizationExpiring         = "AUTHORIZATION_EXPIRING_REVIEW"
	RouteMobilityReady                 = "MOBILITY_READY_PENDING_OBSERVATION"
	RuleMobilityDecision               = "rules.mobility.cross_border_decision/v1"
)

const (
	TransformMobilityFootprint = "transforms.mobility.compute_footprint"
	TransformBuildProposal     = "transforms.mobility.build_proposal"
)

const (
	CapReadMobilityLegs      = "hcmnext.conformance.mobility.read_mobility_legs"
	CapReadWorkAuthorization = "hcmnext.conformance.mobility.read_work_authorization"
	CapResolveTaxPE          = "hcmnext.conformance.mobility.resolve_tax_pe"
	CapResolvePrivacy        = "hcmnext.conformance.mobility.resolve_privacy_transfer"
	CapObserveImmigration    = "hcmnext.conformance.mobility.observe_immigration_vendor"
)

const (
	ApprovalHomePayroll = "approval.mobility.home_payroll"
	ApprovalHostPayroll = "approval.mobility.host_payroll"
	ApprovalImmigration = "approval.mobility.immigration"
	ApprovalPrivacy     = "approval.mobility.privacy"
	ApprovalTaxPEReview = "approval.mobility.tax_pe_review"
)

const (
	ObligationDatedLegEvidence  = "obligation.mobility.dated_leg_evidence"
	ObligationAuthorization     = "obligation.mobility.authorization_milestones"
	ObligationTaxPEReview       = "obligation.mobility.tax_pe_review"
	ObligationPrivacyReview     = "obligation.mobility.privacy_transfer_review"
	ObligationVendorObservation = "obligation.mobility.immigration_observation"
	ObligationRenewalReminder   = "obligation.mobility.authorization_renewal"
	ObligationRecordsRetention  = "obligation.mobility.records_retention"
)

const (
	organizationScope  = "acme/global_mobility"
	purpose            = "SIMULATE_MOBILITY_IMMIGRATION_PRIVACY"
	classification     = "CONFIDENTIAL_HR"
	dataAccessManifest = "dam.mobility.simulation/v1"
	mappingDigest      = "hcmnext.workflow.InputMappingSet/v1"
	repairRef          = "repair.mobility.cross_border_observation/v1"
)

func str(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}
func plainStr() workflow.ValueType  { return workflow.ValueType{Kind: workflow.KindString} }
func boolean() workflow.ValueType   { return workflow.ValueType{Kind: workflow.KindBool} }
func localDate() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindLocalDate} }

func capabilitySchema(id, slot string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: id + "." + slot + "/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}
}
func workflowSchema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: name + "/v1", Version: 1, ProtobufFullName: "hcmnext.workflows.v1." + name}
}
func invocationGovernance(obligations, approvals []string) workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose: purpose, Classification: classification,
		RequiredDecisions: []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk},
		ObligationRefs:    obligations, ApprovalRequirements: approvals,
		RevalidationBoundary: workflow.RevalidatePreExecution, DataAccessManifestRef: dataAccessManifest,
	}
}
func terminalGovernance(obligations, approvals []string) workflow.NodeGovernance {
	return workflow.NodeGovernance{Purpose: purpose, Classification: classification, ObligationRefs: obligations, ApprovalRequirements: approvals, RevalidationBoundary: workflow.RevalidatePreClosure, DataAccessManifestRef: dataAccessManifest}
}

// ReferenceDefinition returns the P1A simulation workflow for CONF-015.
func ReferenceDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID: WorkflowID, Version: Version, Name: "Mobility, immigration and cross-border privacy (simulation)",
		InputSchema: workflowSchema("MobilityCrossBorderInput"), OutputSchema: workflowSchema("MobilityCrossBorderResult"), VariablesSchema: workflowSchema("MobilityCrossBorderVariables"),
		TenantScope: "acme", OrganizationScope: organizationScope, RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeSimulate}, TerminalProfile: workflow.TerminalProfileSimulateOnly, StartNodeID: NodeReadMobilityLegs,
		Inputs:           []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}, {Path: "mobility_id", Type: str("MobilityID")}, {Path: "effective_date", Type: localDate()}},
		Outputs:          []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}, {Path: "terminal_code", Type: plainStr()}},
		Limits:           workflow.Limits{MaxFanOut: 8, MaxDepth: 14, MaxNodes: 24},
		FailurePolicyRef: "policy.workflow.failure.simulation/v1", CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1", MigrationPolicyRef: "policy.workflow.migration.pinned/v1", RetentionPolicyRef: "policy.workflow.retention.hr-simulation/v1",
		ApprovalRequirements: []workflow.ApprovalRequirement{
			{ID: ApprovalHomePayroll, ResolverExpression: "HomePayrollOwnerFor(mobility)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalHostPayroll, ResolverExpression: "HostPayrollOwnerFor(mobility)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalImmigration, ResolverExpression: "ImmigrationReviewerFor(mobility)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalPrivacy, ResolverExpression: "PrivacyReviewerFor(mobility)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
			{ID: ApprovalTaxPEReview, ResolverExpression: "TaxPEReviewerFor(mobility)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: false, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"},
		},
		Obligations: []workflow.ObligationRequirement{
			{ID: ObligationDatedLegEvidence, Authority: "customer.policy.mobility.leg_evidence", InsertionPoint: workflow.InsertSimulation, RequiredAction: "Retain dated origin/destination legs and work-presence evidence", ResponsibleParty: "global_mobility", SatisfactionCondition: "leg evidence digest is bound to the proposal", SourceVersion: RuleMobilityDecision, ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true},
			{ID: ObligationAuthorization, Authority: "immigration.authorization.registry", InsertionPoint: workflow.InsertSimulation, RequiredAction: "Retain authorization status and every required milestone", ResponsibleParty: "immigration.operations", SatisfactionCondition: "authorization milestone evidence is bound to the proposal", SourceVersion: "immigration.authorization/v1", ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true},
			{ID: ObligationTaxPEReview, Authority: "legal.tax.permanent_establishment", InsertionPoint: workflow.InsertApproval, RequiredAction: "Review tax residence and permanent-establishment impact for the dated legs", ResponsibleParty: "mobility.tax", SatisfactionCondition: "tax/PE outcome or explicit unknown repair is recorded", SourceVersion: "mobility.tax_pe/v1", ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true},
			{ID: ObligationPrivacyReview, Authority: "privacy.cross_border_transfer", InsertionPoint: workflow.InsertApproval, RequiredAction: "Review categories, purpose and mechanism before a cross-border transfer", ResponsibleParty: "privacy.office", SatisfactionCondition: "privacy transfer decision is recorded against the proposal", SourceVersion: "privacy.cross_border/v1", ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true},
			{ID: ObligationVendorObservation, Authority: "immigration.vendor.projection", InsertionPoint: workflow.InsertSimulation, RequiredAction: "Retain the immigration vendor result as an observation, not authority", ResponsibleParty: "global_mobility", SatisfactionCondition: "vendor watermark and outcome are recorded", SourceVersion: "immigration.vendor/v1", ReevaluationPolicy: workflow.ReevalRequireReview},
			{ID: ObligationRenewalReminder, Authority: "immigration.authorization.registry", InsertionPoint: workflow.InsertSimulation, RequiredAction: "Schedule a renewal review before authorization expiry", ResponsibleParty: "immigration.operations", SatisfactionCondition: "renewal review is recorded against the expiry", SourceVersion: "immigration.renewal/v1", ReevaluationPolicy: workflow.ReevalReevaluate},
			{ID: ObligationRecordsRetention, Authority: "records.retention", InsertionPoint: workflow.InsertClosure, RequiredAction: "Retain the mobility simulation artifact and evidence references", ResponsibleParty: "operations.records", SatisfactionCondition: "artifact digest is retained with the terminal", SourceVersion: "records.retention.hr-simulation/v1", ReevaluationPolicy: workflow.ReevalPin, Mandatory: true},
		},
		Nodes: nodes(), Edges: edges(),
	}
}

func nodes() []workflow.Node {
	return []workflow.Node{
		{
			ID: NodeReadMobilityLegs, Type: workflow.StepCapability, InputSchema: capabilitySchema(CapReadMobilityLegs, "request"), OutputSchema: capabilitySchema(CapReadMobilityLegs, "response"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}, {Path: "mobility_id", Type: str("MobilityID")}, {Path: "effective_date", Type: localDate()}},
			Outputs:       []workflow.Field{{Path: "mobility_active", Type: boolean()}, {Path: "overlap_detected", Type: boolean()}, {Path: "source_jurisdiction", Type: plainStr()}, {Path: "destination_jurisdiction", Type: plainStr()}, {Path: "home_payroll_group", Type: plainStr()}, {Path: "host_payroll_group", Type: plainStr()}, {Path: "payroll_model", Type: plainStr()}, {Path: "leg_start", Type: localDate()}, {Path: "leg_end", Type: localDate()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: builders.FromInput("worker_id")}, {Target: "mobility_id", Source: builders.FromInput("mobility_id")}, {Target: "effective_date", Source: builders.FromInput("effective_date")}},
			Capability:    &workflow.CapabilityRef{ID: CapReadMobilityLegs, Version: 1, OperationMode: workflow.ModeSimulate, AuthorityScopes: []string{"scope:mobility.legs.read"}}, Governance: invocationGovernance(nil, nil),
		},
		{
			ID: NodeReadWorkAuthorization, Type: workflow.StepCapability, InputSchema: capabilitySchema(CapReadWorkAuthorization, "request"), OutputSchema: capabilitySchema(CapReadWorkAuthorization, "response"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}, {Path: "mobility_id", Type: str("MobilityID")}, {Path: "effective_date", Type: localDate()}},
			Outputs:       []workflow.Field{{Path: "authorization_valid", Type: boolean()}, {Path: "authorization_expiring", Type: boolean()}, {Path: "authorization_milestone_complete", Type: boolean()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: builders.FromInput("worker_id")}, {Target: "mobility_id", Source: builders.FromInput("mobility_id")}, {Target: "effective_date", Source: builders.FromInput("effective_date")}},
			Capability:    &workflow.CapabilityRef{ID: CapReadWorkAuthorization, Version: 1, OperationMode: workflow.ModeSimulate, AuthorityScopes: []string{"scope:mobility.authorization.read"}}, Governance: invocationGovernance(nil, nil),
		},
		{
			ID: NodeResolveTaxPE, Type: workflow.StepCapability, InputSchema: capabilitySchema(CapResolveTaxPE, "request"), OutputSchema: capabilitySchema(CapResolveTaxPE, "response"),
			Inputs:          []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}, {Path: "mobility_id", Type: str("MobilityID")}, {Path: "effective_date", Type: localDate()}},
			Outputs:         []workflow.Field{{Path: "tax_impact_unknown", Type: boolean()}, {Path: "pe_impact_unknown", Type: boolean()}, {Path: "tax_rule_version", Type: plainStr()}},
			InputMappings:   []workflow.Mapping{{Target: "worker_id", Source: builders.FromInput("worker_id")}, {Target: "mobility_id", Source: builders.FromInput("mobility_id")}, {Target: "effective_date", Source: builders.FromInput("effective_date")}},
			RequiredContext: []workflow.ContextRequirement{{Kind: "LegalContext", FieldPaths: []string{"jurisdiction", "applicable_rule_versions"}, Purpose: purpose, MaximumClassification: classification, MaxAgeSeconds: 3600, RequiredWatermarks: []string{"legal.policy.version"}, Pinned: true, MissingBehavior: workflow.MissingUnknown}},
			Capability:      &workflow.CapabilityRef{ID: CapResolveTaxPE, Version: 1, OperationMode: workflow.ModeSimulate, AuthorityScopes: []string{"scope:mobility.tax.read"}}, Governance: invocationGovernance(nil, nil),
		},
		{
			ID: NodeResolvePrivacy, Type: workflow.StepCapability, InputSchema: capabilitySchema(CapResolvePrivacy, "request"), OutputSchema: capabilitySchema(CapResolvePrivacy, "response"),
			Inputs:          []workflow.Field{{Path: "mobility_id", Type: str("MobilityID")}, {Path: "source_jurisdiction", Type: plainStr()}, {Path: "destination_jurisdiction", Type: plainStr()}},
			Outputs:         []workflow.Field{{Path: "privacy_transfer_allowed", Type: boolean()}, {Path: "privacy_mechanism", Type: plainStr()}},
			InputMappings:   []workflow.Mapping{{Target: "mobility_id", Source: builders.FromInput("mobility_id")}, {Target: "source_jurisdiction", Source: builders.FromNode(NodeReadMobilityLegs, "source_jurisdiction")}, {Target: "destination_jurisdiction", Source: builders.FromNode(NodeReadMobilityLegs, "destination_jurisdiction")}},
			RequiredContext: []workflow.ContextRequirement{{Kind: "PrivacyContext", FieldPaths: []string{"transfer_mechanism", "data_categories", "policy_version"}, Purpose: purpose, MaximumClassification: classification, MaxAgeSeconds: 3600, RequiredWatermarks: []string{"privacy.policy.version"}, Pinned: true, MissingBehavior: workflow.MissingUnknown}},
			Capability:      &workflow.CapabilityRef{ID: CapResolvePrivacy, Version: 1, OperationMode: workflow.ModeSimulate, AuthorityScopes: []string{"scope:privacy.transfer.read"}}, Governance: invocationGovernance(nil, nil),
		},
		{
			ID: NodeComputeFootprint, Type: workflow.StepTransform, InputSchema: workflowSchema("MobilityFootprintDraft"), OutputSchema: workflowSchema("MobilityFootprint"),
			Inputs:        []workflow.Field{{Path: "mobility_active", Type: boolean()}, {Path: "overlap_detected", Type: boolean()}, {Path: "source_jurisdiction", Type: plainStr()}, {Path: "destination_jurisdiction", Type: plainStr()}, {Path: "home_payroll_group", Type: plainStr()}, {Path: "host_payroll_group", Type: plainStr()}, {Path: "payroll_model", Type: plainStr()}, {Path: "leg_start", Type: localDate()}, {Path: "leg_end", Type: localDate()}, {Path: "authorization_valid", Type: boolean()}, {Path: "authorization_milestone_complete", Type: boolean()}},
			Outputs:       []workflow.Field{{Path: "overlapping_assignment", Type: boolean()}, {Path: "authorization_blocked", Type: boolean()}, {Path: "authorization_milestone_incomplete", Type: boolean()}, {Path: "payroll_cross_border", Type: boolean()}, {Path: "footprint_digest", Type: plainStr()}},
			InputMappings: []workflow.Mapping{{Target: "mobility_active", Source: builders.FromNode(NodeReadMobilityLegs, "mobility_active")}, {Target: "overlap_detected", Source: builders.FromNode(NodeReadMobilityLegs, "overlap_detected")}, {Target: "source_jurisdiction", Source: builders.FromNode(NodeReadMobilityLegs, "source_jurisdiction")}, {Target: "destination_jurisdiction", Source: builders.FromNode(NodeReadMobilityLegs, "destination_jurisdiction")}, {Target: "home_payroll_group", Source: builders.FromNode(NodeReadMobilityLegs, "home_payroll_group")}, {Target: "host_payroll_group", Source: builders.FromNode(NodeReadMobilityLegs, "host_payroll_group")}, {Target: "payroll_model", Source: builders.FromNode(NodeReadMobilityLegs, "payroll_model")}, {Target: "leg_start", Source: builders.FromNode(NodeReadMobilityLegs, "leg_start")}, {Target: "leg_end", Source: builders.FromNode(NodeReadMobilityLegs, "leg_end")}, {Target: "authorization_valid", Source: builders.FromNode(NodeReadWorkAuthorization, "authorization_valid")}, {Target: "authorization_milestone_complete", Source: builders.FromNode(NodeReadWorkAuthorization, "authorization_milestone_complete")}},
			Transform:     &workflow.TransformSpec{TransformRef: TransformMobilityFootprint, Version: 1, NormalizationProfile: "hcmnext.canonical.mobility_footprint/v1", InputTaint: []workflow.TaintInput{{Source: "source_jurisdiction", Level: workflow.TaintTrusted}, {Source: "destination_jurisdiction", Level: workflow.TaintTrusted}, {Source: "leg_start", Level: workflow.TaintTrusted}, {Source: "leg_end", Level: workflow.TaintTrusted}}, OutputTaint: workflow.TaintTrusted, Limits: workflow.TransformLimits{MaxInputBytes: 64 * 1024, MaxOutputBytes: 64 * 1024, MaxSteps: 5000}},
			Governance:    workflow.NodeGovernance{Purpose: purpose, Classification: classification, RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest},
		},
		{
			ID: NodeBuildProposal, Type: workflow.StepTransform, InputSchema: workflowSchema("MobilityProposalDraft"), OutputSchema: workflowSchema("MobilityProposal"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}, {Path: "mobility_id", Type: str("MobilityID")}, {Path: "footprint_digest", Type: plainStr()}, {Path: "home_payroll_group", Type: plainStr()}, {Path: "host_payroll_group", Type: plainStr()}, {Path: "payroll_model", Type: plainStr()}, {Path: "effective_date", Type: localDate()}},
			Outputs:       []workflow.Field{{Path: "proposal_digest", Type: plainStr()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: builders.FromInput("worker_id")}, {Target: "mobility_id", Source: builders.FromInput("mobility_id")}, {Target: "footprint_digest", Source: builders.FromNode(NodeComputeFootprint, "footprint_digest")}, {Target: "home_payroll_group", Source: builders.FromNode(NodeReadMobilityLegs, "home_payroll_group")}, {Target: "host_payroll_group", Source: builders.FromNode(NodeReadMobilityLegs, "host_payroll_group")}, {Target: "payroll_model", Source: builders.FromNode(NodeReadMobilityLegs, "payroll_model")}, {Target: "effective_date", Source: builders.FromInput("effective_date")}},
			Transform:     &workflow.TransformSpec{TransformRef: TransformBuildProposal, Version: 1, NormalizationProfile: "hcmnext.canonical.mobility_proposal/v1", InputTaint: []workflow.TaintInput{{Source: "worker_id", Level: workflow.TaintTrusted}, {Source: "mobility_id", Level: workflow.TaintTrusted}, {Source: "footprint_digest", Level: workflow.TaintTrusted}}, OutputTaint: workflow.TaintTrusted, Limits: workflow.TransformLimits{MaxInputBytes: 64 * 1024, MaxOutputBytes: 64 * 1024, MaxSteps: 5000}},
			Governance:    workflow.NodeGovernance{Purpose: purpose, Classification: classification, RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest},
		},
		{
			ID: NodeMobilityDecision, Type: workflow.StepDecision, InputSchema: workflowSchema("MobilityDecisionInput"), OutputSchema: workflowSchema("MobilityDecisionResult"),
			Inputs:        []workflow.Field{{Path: "mobility_active", Type: boolean()}, {Path: "authorization_blocked", Type: boolean()}, {Path: "authorization_milestone_incomplete", Type: boolean()}, {Path: "authorization_expiring", Type: boolean()}, {Path: "overlapping_assignment", Type: boolean()}, {Path: "privacy_transfer_allowed", Type: boolean()}, {Path: "tax_impact_unknown", Type: boolean()}, {Path: "pe_impact_unknown", Type: boolean()}},
			Outputs:       []workflow.Field{{Path: "route_key", Type: plainStr()}},
			InputMappings: []workflow.Mapping{{Target: "mobility_active", Source: builders.FromNode(NodeReadMobilityLegs, "mobility_active")}, {Target: "authorization_blocked", Source: builders.FromNode(NodeComputeFootprint, "authorization_blocked")}, {Target: "authorization_milestone_incomplete", Source: builders.FromNode(NodeComputeFootprint, "authorization_milestone_incomplete")}, {Target: "authorization_expiring", Source: builders.FromNode(NodeReadWorkAuthorization, "authorization_expiring")}, {Target: "overlapping_assignment", Source: builders.FromNode(NodeComputeFootprint, "overlapping_assignment")}, {Target: "privacy_transfer_allowed", Source: builders.FromNode(NodeResolvePrivacy, "privacy_transfer_allowed")}, {Target: "tax_impact_unknown", Source: builders.FromNode(NodeResolveTaxPE, "tax_impact_unknown")}, {Target: "pe_impact_unknown", Source: builders.FromNode(NodeResolveTaxPE, "pe_impact_unknown")}},
			Decision:      &workflow.DecisionSpec{EvaluatorRef: "engines.rules.mobility_cross_border", EvaluatorVersion: 1, RuleRef: RuleMobilityDecision, InputDigestProfile: mappingDigest, Routes: []workflow.DecisionRoute{{Key: RouteAuthorizationBlocked, Predicate: "authorization_invalid_or_expired", Precedence: 10}, {Key: RouteAuthorizationMilestoneBlocked, Predicate: "authorization_milestone_incomplete", Precedence: 20}, {Key: RouteOverlappingLegBlocked, Predicate: "overlapping_assignment", Precedence: 30}, {Key: RoutePrivacyTransferBlocked, Predicate: "privacy_transfer_prohibited", Precedence: 40}, {Key: RouteTaxPEUnknown, Predicate: "tax_or_pe_impact_unknown", Precedence: 50}, {Key: RouteAuthorizationExpiring, Predicate: "authorization_expiring", Precedence: 60}, {Key: RouteMobilityReady, Predicate: "dated_mobility_ready", Precedence: 70}}, DefaultRoute: RouteMobilityReady},
			Governance:    workflow.NodeGovernance{Purpose: purpose, Classification: classification, RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: dataAccessManifest},
		},
		{
			ID: NodeObserveImmigration, Type: workflow.StepObserve, InputSchema: capabilitySchema(CapObserveImmigration, "request"), OutputSchema: capabilitySchema(CapObserveImmigration, "response"), Inputs: []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}, {Path: "mobility_id", Type: str("MobilityID")}, {Path: "effective_date", Type: localDate()}}, Outputs: []workflow.Field{{Path: "vendor_state", Type: plainStr()}}, InputMappings: []workflow.Mapping{{Target: "worker_id", Source: builders.FromInput("worker_id")}, {Target: "mobility_id", Source: builders.FromInput("mobility_id")}, {Target: "effective_date", Source: builders.FromInput("effective_date")}},
			Capability: &workflow.CapabilityRef{ID: CapObserveImmigration, Version: 1, OperationMode: workflow.ModeSimulate, AuthorityScopes: []string{"scope:mobility.immigration.read"}}, Observe: &workflow.ObserveSpec{EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "immigration.vendor.projection", ExpectedStateFields: []string{"mobility_id"}, RequiredWatermarks: []string{"immigration.vendor.stream_head"}, MaxAgeSeconds: 300, ComparisonProfile: "comparison.mobility.immigration/v1", RetryExhaustionRoute: NodeEndDegradedRepair}, Retry: &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"}, Governance: invocationGovernance(nil, nil),
		},
		end(NodeEndPendingApprovals, "MOBILITY_PENDING_APPROVALS", workflow.RuntimeCompleted, builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"), []string{ObligationDatedLegEvidence, ObligationAuthorization, ObligationTaxPEReview, ObligationPrivacyReview, ObligationVendorObservation, ObligationRecordsRetention}, []string{ApprovalHomePayroll, ApprovalHostPayroll, ApprovalImmigration, ApprovalPrivacy, ApprovalTaxPEReview}),
		end(NodeEndExpiringAuthorization, "MOBILITY_AUTHORIZATION_EXPIRING_REVIEW", workflow.RuntimeCompleted, builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING"), []string{ObligationDatedLegEvidence, ObligationAuthorization, ObligationTaxPEReview, ObligationPrivacyReview, ObligationVendorObservation, ObligationRenewalReminder, ObligationRecordsRetention}, []string{ApprovalHomePayroll, ApprovalHostPayroll, ApprovalImmigration, ApprovalPrivacy, ApprovalTaxPEReview}),
		end(NodeEndAuthorizationBlocked, "MOBILITY_AUTHORIZATION_BLOCKED", workflow.RuntimeCompleted, builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"), []string{ObligationAuthorization, ObligationRecordsRetention}, nil),
		end(NodeEndMilestoneBlocked, "MOBILITY_AUTHORIZATION_MILESTONE_INCOMPLETE", workflow.RuntimeCompleted, builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"), []string{ObligationAuthorization, ObligationRecordsRetention}, nil),
		end(NodeEndOverlappingLegBlocked, "MOBILITY_OVERLAPPING_ASSIGNMENT_BLOCKED", workflow.RuntimeCompleted, builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"), []string{ObligationDatedLegEvidence, ObligationRecordsRetention}, nil),
		end(NodeEndPrivacyBlocked, "MOBILITY_PRIVACY_TRANSFER_BLOCKED", workflow.RuntimeCompleted, builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "PENDING"), []string{ObligationPrivacyReview, ObligationRecordsRetention}, nil),
		end(NodeEndTaxPEUnknown, "MOBILITY_TAX_PE_IMPACT_UNKNOWN", workflow.RuntimeBlocked, builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"), []string{ObligationTaxPEReview, ObligationRecordsRetention}, nil),
		end(NodeEndDegradedRepair, "MOBILITY_SIMULATION_DEGRADED", workflow.RuntimeBlocked, builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"), []string{ObligationVendorObservation, ObligationRecordsRetention}, nil),
		end(NodeEndUnknown, "MOBILITY_SIMULATION_UNKNOWN", workflow.RuntimeBlocked, builders.Completion("SIMULATED", "BLOCKED", "UNKNOWN", "UNKNOWN", "PENDING"), []string{ObligationRecordsRetention}, nil),
	}
}

func end(id, code string, status workflow.RuntimeStatus, mapping map[string]string, obligations, approvals []string) workflow.Node {
	return workflow.Node{ID: id, Type: workflow.StepEnd, Inputs: builders.TerminalInputs("worker_id", "WorkerID"), InputMappings: builders.TerminalMappings("worker_id", code), End: &workflow.EndSpec{TerminalCode: code, RuntimeStatus: status, CompletionMapping: mapping, OutstandingObligationRefs: obligations, RepairRefs: repairFor(id)}, Governance: terminalGovernance(obligations, approvals)}
}
func repairFor(id string) []string {
	if id == NodeEndTaxPEUnknown || id == NodeEndDegradedRepair {
		return []string{repairRef}
	}
	return nil
}

func edges() []workflow.Edge {
	capabilityRoutes := func(from, success string) []workflow.Edge {
		return []workflow.Edge{{From: from, To: success, RouteKey: string(workflow.OutcomeSucceeded)}, {From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeRejected)}, {From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)}, {From: from, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeAmbiguous)}}
	}
	out := capabilityRoutes(NodeReadMobilityLegs, NodeReadWorkAuthorization)
	out = append(out, capabilityRoutes(NodeReadWorkAuthorization, NodeResolveTaxPE)...)
	out = append(out, capabilityRoutes(NodeResolveTaxPE, NodeResolvePrivacy)...)
	out = append(out, capabilityRoutes(NodeResolvePrivacy, NodeComputeFootprint)...)
	out = append(out, workflow.Edge{From: NodeComputeFootprint, To: NodeBuildProposal, RouteKey: string(workflow.OutcomeSucceeded)}, workflow.Edge{From: NodeComputeFootprint, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)}, workflow.Edge{From: NodeBuildProposal, To: NodeMobilityDecision, RouteKey: string(workflow.OutcomeSucceeded)}, workflow.Edge{From: NodeBuildProposal, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeFailed)})
	out = append(out,
		workflow.Edge{From: NodeMobilityDecision, To: NodeEndAuthorizationBlocked, RouteKey: RouteAuthorizationBlocked},
		workflow.Edge{From: NodeMobilityDecision, To: NodeEndMilestoneBlocked, RouteKey: RouteAuthorizationMilestoneBlocked},
		workflow.Edge{From: NodeMobilityDecision, To: NodeEndOverlappingLegBlocked, RouteKey: RouteOverlappingLegBlocked},
		workflow.Edge{From: NodeMobilityDecision, To: NodeEndPrivacyBlocked, RouteKey: RoutePrivacyTransferBlocked},
		workflow.Edge{From: NodeMobilityDecision, To: NodeEndTaxPEUnknown, RouteKey: RouteTaxPEUnknown},
		workflow.Edge{From: NodeMobilityDecision, To: NodeEndExpiringAuthorization, RouteKey: RouteAuthorizationExpiring},
		workflow.Edge{From: NodeMobilityDecision, To: NodeObserveImmigration, RouteKey: RouteMobilityReady},
		workflow.Edge{From: NodeMobilityDecision, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
		workflow.Edge{From: NodeObserveImmigration, To: NodeEndPendingApprovals, RouteKey: string(workflow.OutcomePass)},
		workflow.Edge{From: NodeObserveImmigration, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomeFail)},
		workflow.Edge{From: NodeObserveImmigration, To: NodeEndDegradedRepair, RouteKey: string(workflow.OutcomePartial)},
		workflow.Edge{From: NodeObserveImmigration, To: NodeEndUnknown, RouteKey: string(workflow.OutcomeUnknown)},
	)
	return out
}

func Compile(registry workflow.CapabilityResolver) (*workflow.CompiledWorkflow, error) {
	return workflow.Compile(ReferenceDefinition(), workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry})
}
func CapabilityIDs() []capability.Key {
	return []capability.Key{{ID: CapReadMobilityLegs, Version: 1}, {ID: CapReadWorkAuthorization, Version: 1}, {ID: CapResolveTaxPE, Version: 1}, {ID: CapResolvePrivacy, Version: 1}, {ID: CapObserveImmigration, Version: 1}}
}
