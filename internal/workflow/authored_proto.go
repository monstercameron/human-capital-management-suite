package workflow

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
)

// This file implements WF-EXT-010's lossless mirror: a total bidirectional
// mapping between Definition and the AuthoredWorkflowDefinition proto. The
// mapping is mechanical by design — every field copied verbatim, no
// validation, no normalization beyond one documented rule — so the compiler
// stays the only place that judges a definition.
//
// The one rule: proto repeated/map fields cannot distinguish absent from
// empty, so empty slices and maps normalize to nil on the way back into Go.
// Message presence does distinguish an absent spec pointer from a
// present-but-empty one, and that distinction survives the round trip.

// ToAuthoredProto renders def as its stored authoring form.
func ToAuthoredProto(def Definition) *workflowv1.AuthoredWorkflowDefinition {
	return &workflowv1.AuthoredWorkflowDefinition{
		WorkflowId:            def.WorkflowID,
		Version:               def.Version,
		Name:                  def.Name,
		Catalog:               catalogToProto(def.Catalog),
		IntentType:            def.IntentType,
		MatchPredicate:        def.MatchPredicate,
		InputSchema:           schemaRefToProto(def.InputSchema),
		OutputSchema:          schemaRefToProto(def.OutputSchema),
		VariablesSchema:       schemaRefToProto(def.VariablesSchema),
		Inputs:                fieldsToProto(def.Inputs),
		Outputs:               fieldsToProto(def.Outputs),
		TenantScope:           def.TenantScope,
		OrganizationScope:     def.OrganizationScope,
		RiskClass:             def.RiskClass,
		DeclaredModes:         executionModesToProto(def.DeclaredModes),
		TerminalProfile:       string(def.TerminalProfile),
		StartNodeId:           def.StartNodeID,
		Nodes:                 nodesToProto(def.Nodes),
		Edges:                 edgesToProto(def.Edges),
		ApprovalRequirements:  approvalsToProto(def.ApprovalRequirements),
		Obligations:           obligationsToProto(def.Obligations),
		Limits:                limitsToProto(def.Limits),
		FailurePolicyRef:      def.FailurePolicyRef,
		CancellationPolicyRef: def.CancellationPolicyRef,
		MigrationPolicyRef:    def.MigrationPolicyRef,
		RetentionPolicyRef:    def.RetentionPolicyRef,
	}
}

// FromAuthoredProto renders the stored authoring form back as a Definition.
// Layout and canvas state are editor-owned and stay behind: they are stored
// with the artifact but never enter the definition, so they can never reach
// the compiled plan or its digest.
func FromAuthoredProto(pb *workflowv1.AuthoredWorkflowDefinition) Definition {
	if pb == nil {
		return Definition{}
	}
	return Definition{
		WorkflowID:            pb.GetWorkflowId(),
		Version:               pb.GetVersion(),
		Name:                  pb.GetName(),
		Catalog:               catalogFromProto(pb.GetCatalog()),
		IntentType:            pb.GetIntentType(),
		MatchPredicate:        stringsMapFromProto(pb.GetMatchPredicate()),
		InputSchema:           schemaRefFromProto(pb.GetInputSchema()),
		OutputSchema:          schemaRefFromProto(pb.GetOutputSchema()),
		VariablesSchema:       schemaRefFromProto(pb.GetVariablesSchema()),
		Inputs:                fieldsFromProto(pb.GetInputs()),
		Outputs:               fieldsFromProto(pb.GetOutputs()),
		TenantScope:           pb.GetTenantScope(),
		OrganizationScope:     pb.GetOrganizationScope(),
		RiskClass:             pb.GetRiskClass(),
		DeclaredModes:         executionModesFromProto(pb.GetDeclaredModes()),
		TerminalProfile:       TerminalProfile(pb.GetTerminalProfile()),
		StartNodeID:           pb.GetStartNodeId(),
		Nodes:                 nodesFromProto(pb.GetNodes()),
		Edges:                 edgesFromProto(pb.GetEdges()),
		ApprovalRequirements:  approvalsFromProto(pb.GetApprovalRequirements()),
		Obligations:           obligationsFromProto(pb.GetObligations()),
		Limits:                limitsFromProto(pb.GetLimits()),
		FailurePolicyRef:      pb.GetFailurePolicyRef(),
		CancellationPolicyRef: pb.GetCancellationPolicyRef(),
		MigrationPolicyRef:    pb.GetMigrationPolicyRef(),
		RetentionPolicyRef:    pb.GetRetentionPolicyRef(),
	}
}

func schemaRefToProto(s SchemaRef) *workflowv1.AuthoredSchemaRef {
	return &workflowv1.AuthoredSchemaRef{SchemaId: s.SchemaID, Version: s.Version, ProtobufFullName: s.ProtobufFullName}
}

func schemaRefFromProto(pb *workflowv1.AuthoredSchemaRef) SchemaRef {
	if pb == nil {
		return SchemaRef{}
	}
	return SchemaRef{SchemaID: pb.GetSchemaId(), Version: pb.GetVersion(), ProtobufFullName: pb.GetProtobufFullName()}
}

func versionedRefToProto(r *VersionedRef) *workflowv1.AuthoredVersionedRef {
	if r == nil {
		return nil
	}
	return &workflowv1.AuthoredVersionedRef{Id: r.ID, Version: r.Version}
}

func versionedRefFromProto(pb *workflowv1.AuthoredVersionedRef) *VersionedRef {
	if pb == nil {
		return nil
	}
	return &VersionedRef{ID: pb.GetId(), Version: pb.GetVersion()}
}

func valueTypeToProto(t ValueType) *workflowv1.AuthoredValueType {
	out := &workflowv1.AuthoredValueType{
		Kind:            string(t.Kind),
		Nullable:        t.Nullable,
		Brand:           t.Brand,
		Classification:  string(t.Classification),
		EnumRef:         t.EnumRef,
		MessageRef:      t.MessageRef,
		ReferenceTarget: t.ReferenceTarget,
	}
	if t.Element != nil {
		out.Element = valueTypeToProto(*t.Element)
	}
	return out
}

func valueTypeFromProto(pb *workflowv1.AuthoredValueType) ValueType {
	if pb == nil {
		return ValueType{}
	}
	out := ValueType{
		Kind:            Kind(pb.GetKind()),
		Nullable:        pb.GetNullable(),
		Brand:           pb.GetBrand(),
		Classification:  DataClassification(pb.GetClassification()),
		EnumRef:         pb.GetEnumRef(),
		MessageRef:      pb.GetMessageRef(),
		ReferenceTarget: pb.GetReferenceTarget(),
	}
	if pb.GetElement() != nil {
		e := valueTypeFromProto(pb.GetElement())
		out.Element = &e
	}
	return out
}

func fieldToProto(f Field) *workflowv1.AuthoredField {
	return &workflowv1.AuthoredField{
		Path:              f.Path,
		Type:              valueTypeToProto(f.Type),
		MaxClassification: string(f.MaxClassification),
		Label:             f.Label,
		Description:       f.Description,
		Group:             f.Group,
	}
}

func fieldFromProto(pb *workflowv1.AuthoredField) Field {
	if pb == nil {
		return Field{}
	}
	return Field{
		Path:              pb.GetPath(),
		Type:              valueTypeFromProto(pb.GetType()),
		MaxClassification: DataClassification(pb.GetMaxClassification()),
		Label:             pb.GetLabel(),
		Description:       pb.GetDescription(),
		Group:             pb.GetGroup(),
	}
}

func fieldsToProto(fs []Field) []*workflowv1.AuthoredField {
	if fs == nil {
		return nil
	}
	out := make([]*workflowv1.AuthoredField, len(fs))
	for i, f := range fs {
		out[i] = fieldToProto(f)
	}
	return out
}

func fieldsFromProto(pbs []*workflowv1.AuthoredField) []Field {
	if len(pbs) == 0 {
		return nil
	}
	out := make([]Field, len(pbs))
	for i, pb := range pbs {
		out[i] = fieldFromProto(pb)
	}
	return out
}

func sourceToProto(s Source) *workflowv1.AuthoredSource {
	return &workflowv1.AuthoredSource{
		Kind:          string(s.Kind),
		NodeId:        s.NodeID,
		ContextKind:   s.ContextKind,
		VariableName:  s.VariableName,
		Path:          s.Path,
		Type:          valueTypeToProto(s.Type),
		Constant:      s.Constant,
		ParameterKey:  s.ParameterKey,
		ParameterMode: string(s.ParameterMode),
		MaxAgeSeconds: s.MaxAgeSeconds,
	}
}

func sourceFromProto(pb *workflowv1.AuthoredSource) Source {
	if pb == nil {
		return Source{}
	}
	return Source{
		Kind:          SourceKind(pb.GetKind()),
		NodeID:        pb.GetNodeId(),
		ContextKind:   pb.GetContextKind(),
		VariableName:  pb.GetVariableName(),
		Path:          pb.GetPath(),
		Type:          valueTypeFromProto(pb.GetType()),
		Constant:      pb.GetConstant(),
		ParameterKey:  pb.GetParameterKey(),
		ParameterMode: ParameterBindingMode(pb.GetParameterMode()),
		MaxAgeSeconds: pb.GetMaxAgeSeconds(),
	}
}

func mappingsToProto(ms []Mapping) []*workflowv1.AuthoredMapping {
	if ms == nil {
		return nil
	}
	out := make([]*workflowv1.AuthoredMapping, len(ms))
	for i, m := range ms {
		out[i] = &workflowv1.AuthoredMapping{Target: m.Target, Source: sourceToProto(m.Source), DeclassificationRef: m.DeclassificationRef}
	}
	return out
}

func mappingsFromProto(pbs []*workflowv1.AuthoredMapping) []Mapping {
	if len(pbs) == 0 {
		return nil
	}
	out := make([]Mapping, len(pbs))
	for i, pb := range pbs {
		out[i] = Mapping{Target: pb.GetTarget(), Source: sourceFromProto(pb.GetSource()), DeclassificationRef: pb.GetDeclassificationRef()}
	}
	return out
}

func contextReqsToProto(rs []ContextRequirement) []*workflowv1.AuthoredContextRequirement {
	if rs == nil {
		return nil
	}
	out := make([]*workflowv1.AuthoredContextRequirement, len(rs))
	for i, r := range rs {
		out[i] = &workflowv1.AuthoredContextRequirement{
			Kind: r.Kind, FieldPaths: r.FieldPaths, Purpose: r.Purpose,
			MaximumClassification: r.MaximumClassification, MaxAgeSeconds: r.MaxAgeSeconds,
			RequiredWatermarks: r.RequiredWatermarks, Pinned: r.Pinned, MissingBehavior: r.MissingBehavior,
		}
	}
	return out
}

func contextReqsFromProto(pbs []*workflowv1.AuthoredContextRequirement) []ContextRequirement {
	if len(pbs) == 0 {
		return nil
	}
	out := make([]ContextRequirement, len(pbs))
	for i, pb := range pbs {
		out[i] = ContextRequirement{
			Kind: pb.GetKind(), FieldPaths: stringsFromProto(pb.GetFieldPaths()), Purpose: pb.GetPurpose(),
			MaximumClassification: pb.GetMaximumClassification(), MaxAgeSeconds: pb.GetMaxAgeSeconds(),
			RequiredWatermarks: stringsFromProto(pb.GetRequiredWatermarks()),
			Pinned:             pb.GetPinned(), MissingBehavior: pb.GetMissingBehavior(),
		}
	}
	return out
}

func capabilityRefToProto(c *CapabilityRef) *workflowv1.AuthoredCapabilityRef {
	if c == nil {
		return nil
	}
	return &workflowv1.AuthoredCapabilityRef{
		Id: c.ID, Version: c.Version, OperationMode: string(c.OperationMode),
		AuthorityScopes: c.AuthorityScopes, IdempotencyKeyMapping: c.IdempotencyKeyMapping,
		EffectBinding: c.EffectBinding, ExpectedVersions: c.ExpectedVersions,
	}
}

func capabilityRefFromProto(pb *workflowv1.AuthoredCapabilityRef) *CapabilityRef {
	if pb == nil {
		return nil
	}
	return &CapabilityRef{
		ID: pb.GetId(), Version: pb.GetVersion(), OperationMode: ExecutionMode(pb.GetOperationMode()),
		AuthorityScopes:       stringsFromProto(pb.GetAuthorityScopes()),
		IdempotencyKeyMapping: pb.GetIdempotencyKeyMapping(),
		EffectBinding:         pb.GetEffectBinding(),
		ExpectedVersions:      stringsFromProto(pb.GetExpectedVersions()),
	}
}

func decisionToProto(d *DecisionSpec) *workflowv1.AuthoredDecisionSpec {
	if d == nil {
		return nil
	}
	var routes []*workflowv1.AuthoredDecisionRoute
	if d.Routes != nil {
		routes = make([]*workflowv1.AuthoredDecisionRoute, len(d.Routes))
		for i, r := range d.Routes {
			routes[i] = &workflowv1.AuthoredDecisionRoute{Key: r.Key, Predicate: r.Predicate, Precedence: int64(r.Precedence)}
		}
	}
	return &workflowv1.AuthoredDecisionSpec{
		EvaluatorRef: d.EvaluatorRef, EvaluatorVersion: d.EvaluatorVersion,
		RuleRef: d.RuleRef, RuleVersion: d.RuleVersion,
		InputDigestProfile: d.InputDigestProfile, Routes: routes, DefaultRoute: d.DefaultRoute,
	}
}

func decisionFromProto(pb *workflowv1.AuthoredDecisionSpec) *DecisionSpec {
	if pb == nil {
		return nil
	}
	var routes []DecisionRoute
	if len(pb.GetRoutes()) > 0 {
		routes = make([]DecisionRoute, len(pb.GetRoutes()))
		for i, r := range pb.GetRoutes() {
			routes[i] = DecisionRoute{Key: r.GetKey(), Predicate: r.GetPredicate(), Precedence: int(r.GetPrecedence())}
		}
	}
	return &DecisionSpec{
		EvaluatorRef: pb.GetEvaluatorRef(), EvaluatorVersion: pb.GetEvaluatorVersion(),
		RuleRef: pb.GetRuleRef(), RuleVersion: pb.GetRuleVersion(),
		InputDigestProfile: pb.GetInputDigestProfile(), Routes: routes, DefaultRoute: pb.GetDefaultRoute(),
	}
}

func transformToProto(t *TransformSpec) *workflowv1.AuthoredTransformSpec {
	if t == nil {
		return nil
	}
	var lookups []*workflowv1.AuthoredTransformLookup
	if t.Lookups != nil {
		lookups = make([]*workflowv1.AuthoredTransformLookup, len(t.Lookups))
		for i, l := range t.Lookups {
			lookups[i] = &workflowv1.AuthoredTransformLookup{Ref: l.Ref, SnapshotDigest: l.SnapshotDigest}
		}
	}
	var taint []*workflowv1.AuthoredTaintInput
	if t.InputTaint != nil {
		taint = make([]*workflowv1.AuthoredTaintInput, len(t.InputTaint))
		for i, in := range t.InputTaint {
			taint[i] = &workflowv1.AuthoredTaintInput{Source: in.Source, Level: string(in.Level)}
		}
	}
	return &workflowv1.AuthoredTransformSpec{
		TransformRef: t.TransformRef, Version: t.Version, ProgramRef: versionedRefToProto(t.ProgramRef),
		InlineCode: t.InlineCode, UsesClock: t.UsesClock, UsesRandom: t.UsesRandom, UsesNetwork: t.UsesNetwork,
		Lookups: lookups, NormalizationProfile: t.NormalizationProfile, InputTaint: taint,
		OutputTaint: string(t.OutputTaint), SanitizerReceiptRef: t.SanitizerReceiptRef,
		Limits: &workflowv1.AuthoredTransformLimits{
			MaxInputBytes: t.Limits.MaxInputBytes, MaxOutputBytes: t.Limits.MaxOutputBytes, MaxSteps: t.Limits.MaxSteps,
		},
	}
}

func transformFromProto(pb *workflowv1.AuthoredTransformSpec) *TransformSpec {
	if pb == nil {
		return nil
	}
	var lookups []TransformLookup
	if len(pb.GetLookups()) > 0 {
		lookups = make([]TransformLookup, len(pb.GetLookups()))
		for i, l := range pb.GetLookups() {
			lookups[i] = TransformLookup{Ref: l.GetRef(), SnapshotDigest: l.GetSnapshotDigest()}
		}
	}
	var taint []TaintInput
	if len(pb.GetInputTaint()) > 0 {
		taint = make([]TaintInput, len(pb.GetInputTaint()))
		for i, in := range pb.GetInputTaint() {
			taint[i] = TaintInput{Source: in.GetSource(), Level: TaintLevel(in.GetLevel())}
		}
	}
	lim := pb.GetLimits()
	return &TransformSpec{
		TransformRef: pb.GetTransformRef(), Version: pb.GetVersion(), ProgramRef: versionedRefFromProto(pb.GetProgramRef()),
		InlineCode: pb.GetInlineCode(), UsesClock: pb.GetUsesClock(), UsesRandom: pb.GetUsesRandom(), UsesNetwork: pb.GetUsesNetwork(),
		Lookups: lookups, NormalizationProfile: pb.GetNormalizationProfile(), InputTaint: taint,
		OutputTaint: TaintLevel(pb.GetOutputTaint()), SanitizerReceiptRef: pb.GetSanitizerReceiptRef(),
		Limits: TransformLimits{MaxInputBytes: lim.GetMaxInputBytes(), MaxOutputBytes: lim.GetMaxOutputBytes(), MaxSteps: lim.GetMaxSteps()},
	}
}

func observeToProto(o *ObserveSpec) *workflowv1.AuthoredObserveSpec {
	if o == nil {
		return nil
	}
	return &workflowv1.AuthoredObserveSpec{
		EvidenceKind: string(o.EvidenceKind), SourceAuthority: o.SourceAuthority,
		ExpectedStateFields: o.ExpectedStateFields, RequiredWatermarks: o.RequiredWatermarks,
		MaxAgeSeconds: o.MaxAgeSeconds, ComparisonProfile: o.ComparisonProfile,
		RetryExhaustionRoute: o.RetryExhaustionRoute,
	}
}

func observeFromProto(pb *workflowv1.AuthoredObserveSpec) *ObserveSpec {
	if pb == nil {
		return nil
	}
	return &ObserveSpec{
		EvidenceKind: ObservationEvidenceKind(pb.GetEvidenceKind()), SourceAuthority: pb.GetSourceAuthority(),
		ExpectedStateFields:  stringsFromProto(pb.GetExpectedStateFields()),
		RequiredWatermarks:   stringsFromProto(pb.GetRequiredWatermarks()),
		MaxAgeSeconds:        pb.GetMaxAgeSeconds(),
		ComparisonProfile:    pb.GetComparisonProfile(),
		RetryExhaustionRoute: pb.GetRetryExhaustionRoute(),
	}
}

func timeExprToProto(t *TimeExprSpec) *workflowv1.AuthoredTimeExprSpec {
	if t == nil {
		return nil
	}
	return &workflowv1.AuthoredTimeExprSpec{
		AnchorPath: t.AnchorPath, OffsetKind: t.OffsetKind,
		OffsetDays: int64(t.OffsetDays), OffsetSeconds: t.OffsetSeconds,
		CalendarRef: t.CalendarRef, CalendarVersion: t.CalendarVersion,
		ZoneId: t.ZoneID, ZoneTzdbVersion: t.ZoneTzdbVersion,
		LocalTime: t.LocalTime, Disambiguation: t.Disambiguation,
		StatuteRulePack: t.StatuteRulePack, StatuteRelease: t.StatuteRelease, Statute: t.Statute,
	}
}

func timeExprFromProto(pb *workflowv1.AuthoredTimeExprSpec) *TimeExprSpec {
	if pb == nil {
		return nil
	}
	return &TimeExprSpec{
		AnchorPath: pb.GetAnchorPath(), OffsetKind: pb.GetOffsetKind(),
		OffsetDays: int(pb.GetOffsetDays()), OffsetSeconds: pb.GetOffsetSeconds(),
		CalendarRef: pb.GetCalendarRef(), CalendarVersion: pb.GetCalendarVersion(),
		ZoneID: pb.GetZoneId(), ZoneTzdbVersion: pb.GetZoneTzdbVersion(),
		LocalTime: pb.GetLocalTime(), Disambiguation: pb.GetDisambiguation(),
		StatuteRulePack: pb.GetStatuteRulePack(), StatuteRelease: pb.GetStatuteRelease(), Statute: pb.GetStatute(),
	}
}

func deadlineDefaultToProto(d *DeadlineDefaultSpec) *workflowv1.AuthoredDeadlineDefaultSpec {
	if d == nil {
		return nil
	}
	return &workflowv1.AuthoredDeadlineDefaultSpec{Schema: d.Schema, CanonicalJson: d.CanonicalJSON}
}

func deadlineDefaultFromProto(pb *workflowv1.AuthoredDeadlineDefaultSpec) *DeadlineDefaultSpec {
	if pb == nil {
		return nil
	}
	return &DeadlineDefaultSpec{Schema: pb.GetSchema(), CanonicalJSON: pb.GetCanonicalJson()}
}

func waitToProto(w *WaitSpec) *workflowv1.AuthoredWaitSpec {
	if w == nil {
		return nil
	}
	return &workflowv1.AuthoredWaitSpec{
		WakeKind: string(w.WakeKind), WakeInstant: w.WakeInstant,
		WakeLocalDate: w.WakeLocalDate, WakeLocalTime: w.WakeLocalTime,
		Disambiguation: w.Disambiguation, ZoneId: w.ZoneID, ZoneTzdbVersion: w.ZoneTzdbVersion,
		CalendarRef: w.CalendarRef, CalendarVersion: w.CalendarVersion,
		ReferenceUpdatePolicy: w.ReferenceUpdatePolicy, TimeExpr: timeExprToProto(w.TimeExpr),
	}
}

func waitFromProto(pb *workflowv1.AuthoredWaitSpec) *WaitSpec {
	if pb == nil {
		return nil
	}
	return &WaitSpec{
		WakeKind: WaitWakeKind(pb.GetWakeKind()), WakeInstant: pb.GetWakeInstant(),
		WakeLocalDate: pb.GetWakeLocalDate(), WakeLocalTime: pb.GetWakeLocalTime(),
		Disambiguation: pb.GetDisambiguation(), ZoneID: pb.GetZoneId(), ZoneTzdbVersion: pb.GetZoneTzdbVersion(),
		CalendarRef: pb.GetCalendarRef(), CalendarVersion: pb.GetCalendarVersion(),
		ReferenceUpdatePolicy: pb.GetReferenceUpdatePolicy(), TimeExpr: timeExprFromProto(pb.GetTimeExpr()),
	}
}

func signalToProto(s *SignalSpec) *workflowv1.AuthoredSignalSpec {
	if s == nil {
		return nil
	}
	return &workflowv1.AuthoredSignalSpec{
		EventType: s.EventType, CorrelationKeyExpression: s.CorrelationKeyExpression,
		ExpectedSchemaRef: schemaRefToProto(s.ExpectedSchemaRef), AcceptedSources: s.AcceptedSources,
		Ordering: string(s.Ordering), CloseAfterSeconds: s.CloseAfterSeconds,
		CloseTimeExpr: timeExprToProto(s.CloseTimeExpr), TimeoutDefault: deadlineDefaultToProto(s.TimeoutDefault),
	}
}

func signalFromProto(pb *workflowv1.AuthoredSignalSpec) *SignalSpec {
	if pb == nil {
		return nil
	}
	return &SignalSpec{
		EventType: pb.GetEventType(), CorrelationKeyExpression: pb.GetCorrelationKeyExpression(),
		ExpectedSchemaRef: schemaRefFromProto(pb.GetExpectedSchemaRef()),
		AcceptedSources:   stringsFromProto(pb.GetAcceptedSources()),
		Ordering:          SignalOrdering(pb.GetOrdering()),
		CloseAfterSeconds: pb.GetCloseAfterSeconds(),
		CloseTimeExpr:     timeExprFromProto(pb.GetCloseTimeExpr()),
		TimeoutDefault:    deadlineDefaultFromProto(pb.GetTimeoutDefault()),
	}
}

func endToProto(e *EndSpec) *workflowv1.AuthoredEndSpec {
	if e == nil {
		return nil
	}
	return &workflowv1.AuthoredEndSpec{
		TerminalCode: e.TerminalCode, RuntimeStatus: string(e.RuntimeStatus),
		CompletionMapping: e.CompletionMapping, OutstandingObligationRefs: e.OutstandingObligationRefs,
		RepairRefs: e.RepairRefs, IncidentRefs: e.IncidentRefs,
		CommitReceiptRef: e.CommitReceiptRef, ApprovalRequired: e.ApprovalRequired,
		ApprovedMaterialDigest:         e.ApprovedMaterialDigest,
		ClosurePolicyPermitsOpenRepair: e.ClosurePolicyPermitsOpenRepair,
	}
}

func endFromProto(pb *workflowv1.AuthoredEndSpec) *EndSpec {
	if pb == nil {
		return nil
	}
	return &EndSpec{
		TerminalCode: pb.GetTerminalCode(), RuntimeStatus: RuntimeStatus(pb.GetRuntimeStatus()),
		CompletionMapping:         stringsMapFromProto(pb.GetCompletionMapping()),
		OutstandingObligationRefs: stringsFromProto(pb.GetOutstandingObligationRefs()),
		RepairRefs:                stringsFromProto(pb.GetRepairRefs()),
		IncidentRefs:              stringsFromProto(pb.GetIncidentRefs()),
		CommitReceiptRef:          pb.GetCommitReceiptRef(), ApprovalRequired: pb.GetApprovalRequired(),
		ApprovedMaterialDigest:         pb.GetApprovedMaterialDigest(),
		ClosurePolicyPermitsOpenRepair: pb.GetClosurePolicyPermitsOpenRepair(),
	}
}

func governanceToProto(g NodeGovernance) *workflowv1.AuthoredNodeGovernance {
	var decisions []string
	if g.RequiredDecisions != nil {
		decisions = make([]string, len(g.RequiredDecisions))
		for i, d := range g.RequiredDecisions {
			decisions[i] = string(d)
		}
	}
	return &workflowv1.AuthoredNodeGovernance{
		Purpose: g.Purpose, Classification: g.Classification, RequiredDecisions: decisions,
		ObligationRefs: g.ObligationRefs, ApprovalRequirements: g.ApprovalRequirements,
		RevalidationBoundary:  string(g.RevalidationBoundary),
		DataAccessManifestRef: g.DataAccessManifestRef, OutputValidatorRef: g.OutputValidatorRef,
	}
}

func governanceFromProto(pb *workflowv1.AuthoredNodeGovernance) NodeGovernance {
	if pb == nil {
		return NodeGovernance{}
	}
	var decisions []GovernanceKind
	if len(pb.GetRequiredDecisions()) > 0 {
		decisions = make([]GovernanceKind, len(pb.GetRequiredDecisions()))
		for i, d := range pb.GetRequiredDecisions() {
			decisions[i] = GovernanceKind(d)
		}
	}
	return NodeGovernance{
		Purpose: pb.GetPurpose(), Classification: pb.GetClassification(), RequiredDecisions: decisions,
		ObligationRefs:        stringsFromProto(pb.GetObligationRefs()),
		ApprovalRequirements:  stringsFromProto(pb.GetApprovalRequirements()),
		RevalidationBoundary:  RevalidationBoundary(pb.GetRevalidationBoundary()),
		DataAccessManifestRef: pb.GetDataAccessManifestRef(), OutputValidatorRef: pb.GetOutputValidatorRef(),
	}
}

func retryToProto(r *RetryPolicy) *workflowv1.AuthoredRetryPolicy {
	if r == nil {
		return nil
	}
	return &workflowv1.AuthoredRetryPolicy{MaxAttempts: r.MaxAttempts, BackoffRef: r.BackoffRef}
}

func retryFromProto(pb *workflowv1.AuthoredRetryPolicy) *RetryPolicy {
	if pb == nil {
		return nil
	}
	return &RetryPolicy{MaxAttempts: pb.GetMaxAttempts(), BackoffRef: pb.GetBackoffRef()}
}

func modeOverlayToProto(o *ModeOverlay) *workflowv1.AuthoredModeOverlay {
	if o == nil {
		return nil
	}
	return &workflowv1.AuthoredModeOverlay{SimulateEffect: string(o.SimulateEffect)}
}

func modeOverlayFromProto(pb *workflowv1.AuthoredModeOverlay) *ModeOverlay {
	if pb == nil {
		return nil
	}
	return &ModeOverlay{SimulateEffect: capability.EffectClass(pb.GetSimulateEffect())}
}

func nodeToProto(n Node) *workflowv1.AuthoredNodeDefinition {
	return &workflowv1.AuthoredNodeDefinition{
		Id: n.ID, Type: string(n.Type),
		InputSchema: schemaRefToProto(n.InputSchema), OutputSchema: schemaRefToProto(n.OutputSchema),
		Inputs: fieldsToProto(n.Inputs), Outputs: fieldsToProto(n.Outputs),
		InputMappings: mappingsToProto(n.InputMappings), RequiredContext: contextReqsToProto(n.RequiredContext),
		Capability: capabilityRefToProto(n.Capability), Decision: decisionToProto(n.Decision),
		Transform: transformToProto(n.Transform), Observe: observeToProto(n.Observe),
		End: endToProto(n.End), Wait: waitToProto(n.Wait), Signal: signalToProto(n.Signal),
		DeclaredEffect: string(n.DeclaredEffect), EffectRole: string(n.EffectRole),
		ResolverRef: versionedRefToProto(n.ResolverRef), TimeoutPolicy: versionedRefToProto(n.TimeoutPolicy),
		CompensationRef: versionedRefToProto(n.CompensationRef),
		OutcomeAliases:  n.OutcomeAliases, ModeOverlay: modeOverlayToProto(n.ModeOverlay),
		Retry: retryToProto(n.Retry), FailureRoute: n.FailureRoute,
		SafePointRequested: n.SafePointRequested, Governance: governanceToProto(n.Governance),
		Metadata: n.Metadata,
	}
}

func nodeFromProto(pb *workflowv1.AuthoredNodeDefinition) Node {
	if pb == nil {
		return Node{}
	}
	return Node{
		ID: pb.GetId(), Type: StepType(pb.GetType()),
		InputSchema: schemaRefFromProto(pb.GetInputSchema()), OutputSchema: schemaRefFromProto(pb.GetOutputSchema()),
		Inputs: fieldsFromProto(pb.GetInputs()), Outputs: fieldsFromProto(pb.GetOutputs()),
		InputMappings: mappingsFromProto(pb.GetInputMappings()), RequiredContext: contextReqsFromProto(pb.GetRequiredContext()),
		Capability: capabilityRefFromProto(pb.GetCapability()), Decision: decisionFromProto(pb.GetDecision()),
		Transform: transformFromProto(pb.GetTransform()), Observe: observeFromProto(pb.GetObserve()),
		End: endFromProto(pb.GetEnd()), Wait: waitFromProto(pb.GetWait()), Signal: signalFromProto(pb.GetSignal()),
		DeclaredEffect: capability.EffectClass(pb.GetDeclaredEffect()), EffectRole: EffectRole(pb.GetEffectRole()),
		ResolverRef: versionedRefFromProto(pb.GetResolverRef()), TimeoutPolicy: versionedRefFromProto(pb.GetTimeoutPolicy()),
		CompensationRef: versionedRefFromProto(pb.GetCompensationRef()),
		OutcomeAliases:  stringsMapFromProto(pb.GetOutcomeAliases()), ModeOverlay: modeOverlayFromProto(pb.GetModeOverlay()),
		Retry: retryFromProto(pb.GetRetry()), FailureRoute: pb.GetFailureRoute(),
		SafePointRequested: pb.GetSafePointRequested(), Governance: governanceFromProto(pb.GetGovernance()),
		Metadata: stringsMapFromProto(pb.GetMetadata()),
	}
}

func nodesToProto(ns []Node) []*workflowv1.AuthoredNodeDefinition {
	if ns == nil {
		return nil
	}
	out := make([]*workflowv1.AuthoredNodeDefinition, len(ns))
	for i, n := range ns {
		out[i] = nodeToProto(n)
	}
	return out
}

func nodesFromProto(pbs []*workflowv1.AuthoredNodeDefinition) []Node {
	if len(pbs) == 0 {
		return nil
	}
	out := make([]Node, len(pbs))
	for i, pb := range pbs {
		out[i] = nodeFromProto(pb)
	}
	return out
}

func approvalsToProto(as []ApprovalRequirement) []*workflowv1.AuthoredApprovalRequirement {
	if as == nil {
		return nil
	}
	out := make([]*workflowv1.AuthoredApprovalRequirement, len(as))
	for i, a := range as {
		out[i] = &workflowv1.AuthoredApprovalRequirement{
			Id: a.ID, ResolverExpression: a.ResolverExpression, Scope: a.Scope,
			Quorum: int64(a.Quorum), SeparationOfDuties: a.SeparationOfDuties,
			EffectiveAsOfPolicy: a.EffectiveAsOfPolicy,
		}
	}
	return out
}

func approvalsFromProto(pbs []*workflowv1.AuthoredApprovalRequirement) []ApprovalRequirement {
	if len(pbs) == 0 {
		return nil
	}
	out := make([]ApprovalRequirement, len(pbs))
	for i, pb := range pbs {
		out[i] = ApprovalRequirement{
			ID: pb.GetId(), ResolverExpression: pb.GetResolverExpression(), Scope: pb.GetScope(),
			Quorum: int(pb.GetQuorum()), SeparationOfDuties: pb.GetSeparationOfDuties(),
			EffectiveAsOfPolicy: pb.GetEffectiveAsOfPolicy(),
		}
	}
	return out
}

func obligationsToProto(os []ObligationRequirement) []*workflowv1.AuthoredObligationRequirement {
	if os == nil {
		return nil
	}
	out := make([]*workflowv1.AuthoredObligationRequirement, len(os))
	for i, o := range os {
		out[i] = &workflowv1.AuthoredObligationRequirement{
			Id: o.ID, Authority: o.Authority, InsertionPoint: string(o.InsertionPoint),
			RequiredAction: o.RequiredAction, ResponsibleParty: o.ResponsibleParty,
			SatisfactionCondition: o.SatisfactionCondition, SourceVersion: o.SourceVersion,
			ReevaluationPolicy: string(o.ReevaluationPolicy), Mandatory: o.Mandatory,
		}
	}
	return out
}

func obligationsFromProto(pbs []*workflowv1.AuthoredObligationRequirement) []ObligationRequirement {
	if len(pbs) == 0 {
		return nil
	}
	out := make([]ObligationRequirement, len(pbs))
	for i, pb := range pbs {
		out[i] = ObligationRequirement{
			ID: pb.GetId(), Authority: pb.GetAuthority(), InsertionPoint: InsertionPoint(pb.GetInsertionPoint()),
			RequiredAction: pb.GetRequiredAction(), ResponsibleParty: pb.GetResponsibleParty(),
			SatisfactionCondition: pb.GetSatisfactionCondition(), SourceVersion: pb.GetSourceVersion(),
			ReevaluationPolicy: ReevaluationPolicy(pb.GetReevaluationPolicy()), Mandatory: pb.GetMandatory(),
		}
	}
	return out
}

func limitsToProto(l Limits) *workflowv1.AuthoredLimits {
	var cycles []*workflowv1.AuthoredCycleDeclaration
	if l.DeclaredCycles != nil {
		cycles = make([]*workflowv1.AuthoredCycleDeclaration, len(l.DeclaredCycles))
		for i, c := range l.DeclaredCycles {
			cycles[i] = &workflowv1.AuthoredCycleDeclaration{
				EntryNodeId: c.EntryNodeID, GuardNodeId: c.GuardNodeID, MaxIterations: c.MaxIterations,
			}
		}
	}
	return &workflowv1.AuthoredLimits{
		MaxFanOut: l.MaxFanOut, MaxDepth: l.MaxDepth, MaxNodes: l.MaxNodes, DeclaredCycles: cycles,
	}
}

func limitsFromProto(pb *workflowv1.AuthoredLimits) Limits {
	if pb == nil {
		return Limits{}
	}
	var cycles []CycleDeclaration
	if len(pb.GetDeclaredCycles()) > 0 {
		cycles = make([]CycleDeclaration, len(pb.GetDeclaredCycles()))
		for i, c := range pb.GetDeclaredCycles() {
			cycles[i] = CycleDeclaration{
				EntryNodeID: c.GetEntryNodeId(), GuardNodeID: c.GetGuardNodeId(), MaxIterations: c.GetMaxIterations(),
			}
		}
	}
	return Limits{
		MaxFanOut: pb.GetMaxFanOut(), MaxDepth: pb.GetMaxDepth(), MaxNodes: pb.GetMaxNodes(), DeclaredCycles: cycles,
	}
}

func edgesToProto(es []Edge) []*workflowv1.AuthoredEdge {
	if es == nil {
		return nil
	}
	out := make([]*workflowv1.AuthoredEdge, len(es))
	for i, e := range es {
		out[i] = &workflowv1.AuthoredEdge{From: e.From, To: e.To, RouteKey: e.RouteKey}
	}
	return out
}

func edgesFromProto(pbs []*workflowv1.AuthoredEdge) []Edge {
	if len(pbs) == 0 {
		return nil
	}
	out := make([]Edge, len(pbs))
	for i, pb := range pbs {
		out[i] = Edge{From: pb.GetFrom(), To: pb.GetTo(), RouteKey: pb.GetRouteKey()}
	}
	return out
}

func catalogToProto(c *CatalogMetadata) *workflowv1.AuthoredCatalogMetadata {
	if c == nil {
		return nil
	}
	return &workflowv1.AuthoredCatalogMetadata{
		DisplayName: c.DisplayName, Category: c.Category, Description: c.Description,
		Keywords: c.Keywords, Icon: c.Icon, Hidden: c.Hidden,
	}
}

func catalogFromProto(pb *workflowv1.AuthoredCatalogMetadata) *CatalogMetadata {
	if pb == nil {
		return nil
	}
	return &CatalogMetadata{
		DisplayName: pb.GetDisplayName(), Category: pb.GetCategory(), Description: pb.GetDescription(),
		Keywords: stringsFromProto(pb.GetKeywords()), Icon: pb.GetIcon(), Hidden: pb.GetHidden(),
	}
}

func executionModesToProto(ms []ExecutionMode) []string {
	if ms == nil {
		return nil
	}
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = string(m)
	}
	return out
}

func executionModesFromProto(ss []string) []ExecutionMode {
	if len(ss) == 0 {
		return nil
	}
	out := make([]ExecutionMode, len(ss))
	for i, s := range ss {
		out[i] = ExecutionMode(s)
	}
	return out
}

// stringsFromProto copies a repeated string, normalizing empty to nil.
func stringsFromProto(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	out := make([]string, len(ss))
	copy(out, ss)
	return out
}

// stringsMapFromProto copies a string map, normalizing empty to nil.
func stringsMapFromProto(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
