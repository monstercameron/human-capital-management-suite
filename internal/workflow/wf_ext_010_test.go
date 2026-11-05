package workflow_test

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// richDefinition exercises every field of workflow.Definition and every node
// spec type, so the PRIMARY test proves the proto carries all of them. It
// deliberately uses only nil-or-populated slices and maps: empty-non-nil
// normalizes to nil on the proto leg by design (see
// TestTodo_WF_EXT_010/EmptyNormalizesToNil).
func richDefinition() workflow.Definition {
	listOfList := workflow.ValueType{
		Kind:           workflow.KindList,
		Nullable:       true,
		Brand:          "B1",
		Classification: workflow.DataConfidential,
		Element: &workflow.ValueType{
			Kind:    workflow.KindList,
			Element: &workflow.ValueType{Kind: workflow.KindString, Brand: "WorkerID"},
		},
	}
	return workflow.Definition{
		WorkflowID: "wf.ext010.rich",
		Version:    7,
		Name:       "EXT-010 rich mirror fixture",
		Catalog: &workflow.CatalogMetadata{
			DisplayName: "Rich", Category: "cat", Description: "desc",
			Keywords: []string{"k1", "k2"}, Icon: "icon", Hidden: true,
		},
		IntentType:      "intent.rich.v1",
		MatchPredicate:  map[string]string{"a.b": "c", "x": "y"},
		InputSchema:     fixtureSchemaRef("RichInput"),
		OutputSchema:    fixtureSchemaRef("RichOutput"),
		VariablesSchema: fixtureSchemaRef("RichVars"),
		Inputs: []workflow.Field{
			{Path: "in1", Type: listOfList, MaxClassification: workflow.DataInternal, Label: "L", Description: "D", Group: "G"},
			{Path: "in2", Type: workflow.ValueType{Kind: workflow.KindEnum, EnumRef: "E1"}},
			{Path: "in3", Type: workflow.ValueType{Kind: workflow.KindMessage, MessageRef: "M1"}},
			{Path: "in4", Type: workflow.ValueType{Kind: workflow.KindReference, ReferenceTarget: "custom:thing"}},
			{Path: "in5", Type: workflow.ValueType{Kind: workflow.KindMoney}},
		},
		Outputs:           []workflow.Field{{Path: "out1", Type: stringType()}},
		TenantScope:       "acme",
		OrganizationScope: "acme/eng",
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate, workflow.ModeExecute},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       "cap",
		Nodes: []workflow.Node{
			{
				ID:           "cap",
				Type:         workflow.StepCapability,
				InputSchema:  capSchema(capReadWorker, "request"),
				OutputSchema: capSchema(capReadWorker, "response"),
				Inputs:       []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				Outputs:      []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: inputSource("worker_id"), DeclassificationRef: "decl/v1"},
					{Target: "ctx", Source: workflow.Source{Kind: workflow.SourceContext, ContextKind: "worker", Path: "id", Type: stringType()}},
					{Target: "cst", Source: workflow.Source{Kind: workflow.SourceConstant, Type: stringType(), Constant: "K"}},
					{Target: "par", Source: workflow.Source{Kind: workflow.SourceParameter, ParameterKey: "pk", ParameterMode: workflow.ParameterLive, MaxAgeSeconds: 60, Type: stringType()}},
					{Target: "var", Source: workflow.Source{Kind: workflow.SourceVariable, VariableName: "v1", Path: "p", Type: stringType()}},
					{Target: "nodal", Source: nodeSource("cap", "worker_id")},
				},
				RequiredContext: []workflow.ContextRequirement{{
					Kind: "worker", FieldPaths: []string{"id", "name"}, Purpose: "p",
					MaximumClassification: "CONFIDENTIAL", MaxAgeSeconds: 30,
					RequiredWatermarks: []string{"w1"}, Pinned: true, MissingBehavior: workflow.MissingFail,
				}},
				Capability: &workflow.CapabilityRef{
					ID: capReadWorker, Version: 3, OperationMode: workflow.ModeExecute,
					AuthorityScopes: []string{"s1", "s2"}, IdempotencyKeyMapping: "worker_id",
					EffectBinding: "bind", ExpectedVersions: []string{"v1"},
				},
				DeclaredEffect:     capability.EffectReadOnly,
				EffectRole:         workflow.RoleDerivedUpdate,
				ResolverRef:        &workflow.VersionedRef{ID: "r1", Version: "v1"},
				TimeoutPolicy:      &workflow.VersionedRef{ID: "t1", Version: "v2"},
				CompensationRef:    &workflow.VersionedRef{ID: "c1", Version: "v3"},
				OutcomeAliases:     map[string]string{"DONE": "SUCCEEDED", "DROP": ""},
				ModeOverlay:        &workflow.ModeOverlay{SimulateEffect: capability.EffectPure},
				Retry:              &workflow.RetryPolicy{MaxAttempts: 5, BackoffRef: "b1"},
				FailureRoute:       "end",
				SafePointRequested: true,
				Governance: workflow.NodeGovernance{
					Purpose: "gp", Classification: "INTERNAL",
					RequiredDecisions:     []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernancePrivacy},
					ObligationRefs:        []string{"o1"},
					ApprovalRequirements:  []string{"a1"},
					RevalidationBoundary:  workflow.RevalidatePreEffect,
					DataAccessManifestRef: "dam/v1",
					OutputValidatorRef:    "ov/v1",
				},
				Metadata: map[string]string{"mk": "mv"},
			},
			{
				ID:           "dec",
				Type:         workflow.StepDecision,
				InputSchema:  capSchema(capReadWorker, "request"),
				OutputSchema: capSchema(capReadWorker, "response"),
				Decision: &workflow.DecisionSpec{
					EvaluatorRef: "ev", EvaluatorVersion: 2, RuleRef: "rule", RuleVersion: "rv1",
					InputDigestProfile: "prof",
					Routes: []workflow.DecisionRoute{
						{Key: "yes", Predicate: "p1", Precedence: 0},
						{Key: "no", Predicate: "p2", Precedence: -7},
					},
					DefaultRoute: "no",
				},
				Governance: governedNode(workflow.RevalidateNone),
			},
			{
				ID:           "xform",
				Type:         workflow.StepTransform,
				InputSchema:  capSchema(capReadWorker, "request"),
				OutputSchema: capSchema(capReadWorker, "response"),
				Transform: &workflow.TransformSpec{
					TransformRef: "tr", Version: 4,
					ProgramRef: &workflow.VersionedRef{ID: "prog", Version: "v9"},
					InlineCode: "return 1",
					UsesClock:  true, UsesRandom: true, UsesNetwork: true,
					Lookups: []workflow.TransformLookup{
						{Ref: "lk1", SnapshotDigest: "sha256:aaa"},
					},
					NormalizationProfile: "np",
					InputTaint: []workflow.TaintInput{
						{Source: "in1", Level: workflow.TaintTainted},
					},
					OutputTaint:         workflow.TaintDerived,
					SanitizerReceiptRef: "san/v1",
					Limits: workflow.TransformLimits{
						MaxInputBytes: 100, MaxOutputBytes: 200, MaxSteps: 300,
					},
				},
				Governance: governedNode(workflow.RevalidateNone),
			},
			{
				ID:           "obs",
				Type:         workflow.StepObserve,
				InputSchema:  capSchema(capReadWorker, "request"),
				OutputSchema: capSchema(capReadWorker, "response"),
				Observe: &workflow.ObserveSpec{
					EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "auth",
					ExpectedStateFields: []string{"f1"}, RequiredWatermarks: []string{"w2"},
					MaxAgeSeconds: 90, ComparisonProfile: "cp", RetryExhaustionRoute: "end",
				},
				Capability: &workflow.CapabilityRef{
					ID: capObservePayrol, Version: 1, OperationMode: workflow.ModeExecute,
					AuthorityScopes: []string{"scope:payroll.read"},
				},
				Governance: governedNode(workflow.RevalidatePreExecution),
			},
			{
				ID:           "wait",
				Type:         workflow.StepWait,
				InputSchema:  capSchema(capReadWorker, "request"),
				OutputSchema: capSchema(capReadWorker, "response"),
				Wait: &workflow.WaitSpec{
					WakeKind: workflow.WaitWakeAtTimeExpr, WakeInstant: "2026-01-01T00:00:00Z",
					WakeLocalDate: "2026-01-02", WakeLocalTime: "03:04:05",
					Disambiguation: "EARLIER", ZoneID: "America/New_York", ZoneTzdbVersion: "2026a",
					CalendarRef: "cal", CalendarVersion: "cv1",
					ReferenceUpdatePolicy: "PIN",
					TimeExpr: &workflow.TimeExprSpec{
						AnchorPath: "a", OffsetKind: "BUSINESS_DAYS", OffsetDays: -3, OffsetSeconds: 3600,
						CalendarRef: "cc", CalendarVersion: "cv2", ZoneID: "UTC", ZoneTzdbVersion: "2026a",
						LocalTime: "09:00:00", Disambiguation: "LATER",
						StatuteRulePack: "srp", StatuteRelease: "rel", Statute: "st",
					},
				},
				Governance: governedNode(workflow.RevalidateNone),
			},
			{
				ID:           "sig",
				Type:         workflow.StepSignal,
				InputSchema:  capSchema(capReadWorker, "request"),
				OutputSchema: capSchema(capReadWorker, "response"),
				Signal: &workflow.SignalSpec{
					EventType: "ev.t", CorrelationKeyExpression: "id",
					ExpectedSchemaRef: fixtureSchemaRef("SigPayload"),
					AcceptedSources:   []string{"src1"},
					Ordering:          workflow.SignalOrderingMonotonicSequence,
					CloseAfterSeconds: 120,
					CloseTimeExpr:     &workflow.TimeExprSpec{AnchorPath: "b", OffsetKind: "DURATION", OffsetSeconds: 5},
					TimeoutDefault:    &workflow.DeadlineDefaultSpec{Schema: "s", CanonicalJSON: `{"a":1}`},
				},
				Governance: governedNode(workflow.RevalidateNone),
			},
			{
				ID:            "end",
				Type:          workflow.StepEnd,
				InputSchema:   capSchema(capReadWorker, "request"),
				OutputSchema:  capSchema(capReadWorker, "response"),
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("DONE"),
				End: &workflow.EndSpec{
					TerminalCode:                   "DONE",
					RuntimeStatus:                  workflow.RuntimeCompleted,
					CompletionMapping:              fixtureCompletion("req", "exe", "biz", "con", "obl"),
					OutstandingObligationRefs:      []string{"oo1"},
					RepairRefs:                     []string{"rr1"},
					IncidentRefs:                   []string{"ir1"},
					CommitReceiptRef:               "cr/v1",
					ApprovalRequired:               true,
					ApprovedMaterialDigest:         "sha256:bbb",
					ClosurePolicyPermitsOpenRepair: true,
				},
				Governance: governedNode(workflow.RevalidatePreClosure),
			},
		},
		Edges: []workflow.Edge{
			{From: "cap", To: "dec", RouteKey: "SUCCEEDED"},
			{From: "dec", To: "end", RouteKey: "no"},
		},
		ApprovalRequirements: []workflow.ApprovalRequirement{{
			ID: "ap1", ResolverExpression: "mgr-of(requestor)", Scope: "acme",
			Quorum: 2, SeparationOfDuties: true, EffectiveAsOfPolicy: "POLL",
		}},
		Obligations: []workflow.ObligationRequirement{{
			ID: "ob1", Authority: "legal", InsertionPoint: workflow.InsertClosure,
			RequiredAction: "file", ResponsibleParty: "hr",
			SatisfactionCondition: "filed", SourceVersion: "sv1",
			ReevaluationPolicy: workflow.ReevalRequireReview, Mandatory: true,
		}},
		Limits: workflow.Limits{
			MaxFanOut: 4, MaxDepth: 10, MaxNodes: 12,
			DeclaredCycles: []workflow.CycleDeclaration{
				{EntryNodeID: "cap", GuardNodeID: "dec", MaxIterations: 3},
			},
		},
		FailurePolicyRef:      "pf/v1",
		CancellationPolicyRef: "pc/v1",
		MigrationPolicyRef:    "pm/v1",
		RetentionPolicyRef:    "pr/v1",
	}
}

func TestTodo_WF_EXT_010(t *testing.T) {
	t.Run("MirrorRoundTripIsIdentity", func(t *testing.T) {
		want := richDefinition()
		got := workflow.FromAuthoredProto(workflow.ToAuthoredProto(want))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip mutated the definition:\n%s", definitionDiff(want, got))
		}
	})

	t.Run("PresentEmptySpecsSurvive", func(t *testing.T) {
		def := workflow.Definition{
			WorkflowID: "wf.ext010.presence", Version: 1, StartNodeID: "n",
			Nodes: []workflow.Node{{
				ID: "n", Type: workflow.StepDecision,
				Decision: &workflow.DecisionSpec{},
			}},
		}
		got := workflow.FromAuthoredProto(workflow.ToAuthoredProto(def))
		if got.Nodes[0].Decision == nil {
			t.Fatal("present-but-empty DecisionSpec came back nil")
		}
		if got.Nodes[0].Capability != nil || got.Nodes[0].End != nil {
			t.Fatal("absent specs came back present")
		}
	})

	t.Run("EmptyNormalizesToNil", func(t *testing.T) {
		def := workflow.Definition{
			WorkflowID: "wf.ext010.empty", Version: 1,
			DeclaredModes:  []workflow.ExecutionMode{},
			Inputs:         []workflow.Field{},
			MatchPredicate: map[string]string{},
			Nodes:          []workflow.Node{},
			Edges:          []workflow.Edge{},
		}
		got := workflow.FromAuthoredProto(workflow.ToAuthoredProto(def))
		if got.DeclaredModes != nil || got.Inputs != nil || got.MatchPredicate != nil ||
			got.Nodes != nil || got.Edges != nil {
			t.Fatalf("empty collections did not normalize to nil: %+v", got)
		}
	})

	t.Run("NilIsSafe", func(t *testing.T) {
		if got := workflow.FromAuthoredProto(nil); !reflect.DeepEqual(got, workflow.Definition{}) {
			t.Fatalf("FromAuthoredProto(nil) = %+v", got)
		}
		back := workflow.FromAuthoredProto(workflow.ToAuthoredProto(workflow.Definition{}))
		if !reflect.DeepEqual(back, workflow.Definition{}) {
			t.Fatalf("zero definition round trip = %+v", back)
		}
	})

	t.Run("LoaderReadsProtoJSON", func(t *testing.T) {
		want := richDefinition()
		raw, err := workflow.MarshalProtoJSON(want)
		if err != nil {
			t.Fatalf("MarshalProtoJSON: %v", err)
		}
		got, err := workflow.LoadProtoJSON(raw)
		if err != nil {
			t.Fatalf("LoadProtoJSON: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("protojson leg mutated the definition:\n%s", definitionDiff(want, got))
		}
	})

	t.Run("LoaderIsStrict", func(t *testing.T) {
		raw, err := workflow.MarshalProtoJSON(richDefinition())
		if err != nil {
			t.Fatalf("MarshalProtoJSON: %v", err)
		}
		s := strings.Replace(string(raw), `"workflowId"`, `"workflowIdX"`, 1)
		if _, err := workflow.LoadProtoJSON([]byte(s)); err == nil {
			t.Fatal("unknown protojson field was silently accepted")
		}
		if _, err := workflow.LoadProtoJSON([]byte(`{"workflowId": 42}`)); err == nil {
			t.Fatal("mistyped protojson field was silently accepted")
		}
	})

	t.Run("LayoutStaysOutsideTheDefinition", func(t *testing.T) {
		raw, err := workflow.MarshalProtoJSON(richDefinition())
		if err != nil {
			t.Fatalf("MarshalProtoJSON: %v", err)
		}
		// Splice editor canvas state into the stored artifact.
		trimmed := strings.TrimSuffix(string(raw), "}\n")
		withLayout := trimmed + `, "layout": {"cap": {"x": 10.5, "y": 20.25, "collapsed": true}},` +
			` "canvas": {"zoom": 1.5, "centerX": 1, "centerY": 2}}`
		got, err := workflow.LoadProtoJSON([]byte(withLayout))
		if err != nil {
			t.Fatalf("LoadProtoJSON with layout: %v", err)
		}
		if !reflect.DeepEqual(got, richDefinition()) {
			t.Fatalf("layout leaked into the definition:\n%s", definitionDiff(richDefinition(), got))
		}
	})
}

// TestTodo_WF_EXT_010_Property proves the mirror laws hold across randomized
// shapes: Go to proto to Go is identity, the protojson leg preserves it too,
// and compiling the original and the round-tripped definition yields the
// same plan digest.
func TestTodo_WF_EXT_010_Property(t *testing.T) {
	t.Run("RandomizedRoundTripIsIdentity", func(t *testing.T) {
		rng := rand.New(rand.NewSource(0xE710))
		for i := 0; i < 300; i++ {
			want := randomDefinition(rng, i)
			if got := workflow.FromAuthoredProto(workflow.ToAuthoredProto(want)); !reflect.DeepEqual(got, want) {
				t.Fatalf("seed %d: round trip mutated the definition:\n%s", i, definitionDiff(want, got))
			}
			raw, err := workflow.MarshalProtoJSON(want)
			if err != nil {
				t.Fatalf("seed %d: MarshalProtoJSON: %v", i, err)
			}
			if got, err := workflow.LoadProtoJSON(raw); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("seed %d: protojson leg failed: err=%v\n%s", i, err, definitionDiff(want, got))
			}
		}
	})

	t.Run("RoundTripPreservesPlanDigest", func(t *testing.T) {
		promo := workflow.PromotionReferenceDefinition()
		promoBack := workflow.FromAuthoredProto(workflow.ToAuthoredProto(promo))
		if !reflect.DeepEqual(promoBack, promo) {
			t.Fatalf("promotion reference mutated:\n%s", definitionDiff(promo, promoBack))
		}
		planA, err := workflow.Compile(promo, promotionOptions(t))
		if err != nil {
			t.Fatalf("compile original: %v", err)
		}
		planB, err := workflow.Compile(promoBack, promotionOptions(t))
		if err != nil {
			t.Fatalf("compile round-tripped: %v", err)
		}
		if planA.Digest() != planB.Digest() {
			t.Fatalf("digest changed: %s vs %s", planA.Digest(), planB.Digest())
		}

		fx := effectsDefinition()
		fxBack := workflow.FromAuthoredProto(workflow.ToAuthoredProto(fx))
		if !reflect.DeepEqual(fxBack, fx) {
			t.Fatalf("effects fixture mutated:\n%s", definitionDiff(fx, fxBack))
		}
		planC, err := workflow.Compile(fx, effectsOptions(t))
		if err != nil {
			t.Fatalf("compile effects original: %v", err)
		}
		planD, err := workflow.Compile(fxBack, effectsOptions(t))
		if err != nil {
			t.Fatalf("compile effects round-tripped: %v", err)
		}
		if planC.Digest() != planD.Digest() {
			t.Fatalf("effects digest changed: %s vs %s", planC.Digest(), planD.Digest())
		}
	})
}

// randomDefinition builds a definition exercising a random subset of the
// mirror surface. It emits only nil-or-populated collections so identity is
// exact; the empty-normalizes-to-nil rule is covered separately.
func randomDefinition(rng *rand.Rand, seed int) workflow.Definition {
	pick := func(ss ...string) string { return ss[rng.Intn(len(ss))] }
	maybe := func(p int) bool { return rng.Intn(100) < p }
	strs := func() []string {
		if !maybe(70) {
			return nil
		}
		n := 1 + rng.Intn(3)
		out := make([]string, n)
		for i := range out {
			out[i] = pick("", "a", "Z", "0", "with space", "uni-Ω", `q"uote`)
		}
		return out
	}
	strmap := func() map[string]string {
		if !maybe(60) {
			return nil
		}
		return map[string]string{"k1": pick("", "v"), "k2": pick("w", "")}
	}
	var vtype func(depth int) workflow.ValueType
	vtype = func(depth int) workflow.ValueType {
		vt := workflow.ValueType{
			Kind:           workflow.Kind(pick("STRING", "INTEGER", "DECIMAL", "BOOL", "INSTANT", "LOCAL_DATE", "MONEY", "ENUM", "MESSAGE", "LIST", "DURATION", "RATE", "REFERENCE")),
			Nullable:       maybe(30),
			Brand:          pick("", "B"),
			Classification: workflow.DataClassification(pick("", "PUBLIC", "INTERNAL", "CONFIDENTIAL", "RESTRICTED")),
			EnumRef:        pick("", "E"), MessageRef: pick("", "M"), ReferenceTarget: pick("", "custom:k"),
		}
		if vt.Kind == workflow.KindList && depth < 2 && maybe(80) {
			e := vtype(depth + 1)
			vt.Element = &e
		}
		return vt
	}
	field := func() workflow.Field {
		return workflow.Field{
			Path: pick("p", "a.b", ""), Type: vtype(0),
			MaxClassification: workflow.DataClassification(pick("", "PUBLIC", "RESTRICTED")),
			Label:             pick("", "L"), Description: pick("", "D"), Group: pick("", "G"),
		}
	}
	source := func() workflow.Source {
		return workflow.Source{
			Kind:   workflow.SourceKind(pick("WORKFLOW_INPUT", "NODE_OUTPUT", "CONTEXT", "CONSTANT", "PARAMETER", "VARIABLE")),
			NodeID: pick("", "n0"), ContextKind: pick("", "ck"), VariableName: pick("", "vv"),
			Path: pick("", "p"), Type: vtype(0), Constant: pick("", "K"),
			ParameterKey:  pick("", "pk"),
			ParameterMode: workflow.ParameterBindingMode(pick("", "PINNED_AT_PUBLISH", "PINNED_AT_START", "LIVE")),
			MaxAgeSeconds: uint64(rng.Intn(1000)),
		}
	}
	spec := rng.Intn(7)
	node := workflow.Node{
		ID:           "n0",
		Type:         workflow.StepType(pick("CAPABILITY", "DECISION", "TRANSFORM", "OBSERVE", "END", "WAIT", "SIGNAL")),
		InputSchema:  workflow.SchemaRef{SchemaID: "s", Version: uint32(rng.Intn(5)), ProtobufFullName: "f"},
		OutputSchema: workflow.SchemaRef{SchemaID: "s", Version: 1, ProtobufFullName: "f"},
		Inputs:       []workflow.Field{field()},
		Outputs:      []workflow.Field{field()},
		InputMappings: []workflow.Mapping{
			{Target: "t", Source: source(), DeclassificationRef: pick("", "d")},
		},
		RequiredContext: []workflow.ContextRequirement{{
			Kind: pick("", "k"), FieldPaths: strs(), Purpose: pick("", "p"),
			MaximumClassification: pick("", "PUBLIC"), MaxAgeSeconds: uint64(rng.Intn(50)),
			RequiredWatermarks: strs(), Pinned: maybe(50), MissingBehavior: pick("FAIL", "UNKNOWN", ""),
		}},
		DeclaredEffect:     capability.EffectClass(pick("PURE", "READ_ONLY", "INTERNAL_MUTATION", "EXTERNAL_MUTATION", "IRREVERSIBLE_EXTERNAL_MUTATION", "")),
		EffectRole:         workflow.EffectRole(pick("", "AUTHORITATIVE_CORE", "DOWNSTREAM_EFFECT", "DERIVED_UPDATE")),
		OutcomeAliases:     strmap(),
		Retry:              &workflow.RetryPolicy{MaxAttempts: uint32(rng.Intn(9)), BackoffRef: pick("", "b")},
		FailureRoute:       pick("", "n1"),
		SafePointRequested: maybe(20),
		Governance: workflow.NodeGovernance{
			Purpose: pick("", "g"), Classification: pick("", "INTERNAL"),
			RequiredDecisions: []workflow.GovernanceKind{
				workflow.GovernanceKind(pick("AUTHZ", "LEGAL", "PRIVACY", "PURPOSE", "RISK", "ENTITLEMENT")),
			},
			ObligationRefs:        strs(),
			ApprovalRequirements:  strs(),
			RevalidationBoundary:  workflow.RevalidationBoundary(pick("NONE", "PRE_EXECUTION", "PRE_EFFECT", "PRE_CLOSURE", "")),
			DataAccessManifestRef: pick("", "dam"),
			OutputValidatorRef:    pick("", "ov"),
		},
		Metadata: strmap(),
	}
	switch spec {
	case 0:
		node.Capability = &workflow.CapabilityRef{
			ID: pick("c", ""), Version: uint32(rng.Intn(4)),
			OperationMode:         workflow.ExecutionMode(pick("SIMULATE", "EXECUTE", "REPLAY", "REPAIR", "SHADOW", "")),
			AuthorityScopes:       strs(),
			IdempotencyKeyMapping: pick("", "ik"),
			EffectBinding:         pick("", "eb"),
			ExpectedVersions:      strs(),
		}
	case 1:
		node.Decision = &workflow.DecisionSpec{
			EvaluatorRef: pick("", "ev"), EvaluatorVersion: uint32(rng.Intn(4)),
			RuleRef: pick("", "r"), RuleVersion: pick("", "rv"),
			InputDigestProfile: pick("", "ip"),
			Routes: []workflow.DecisionRoute{
				{Key: "k", Predicate: "p", Precedence: rng.Intn(200) - 100},
			},
			DefaultRoute: pick("", "k"),
		}
	case 2:
		node.Transform = &workflow.TransformSpec{
			TransformRef: pick("", "t"), Version: uint32(rng.Intn(4)),
			ProgramRef: &workflow.VersionedRef{ID: pick("", "p"), Version: pick("", "v")},
			InlineCode: pick("", "code"),
			UsesClock:  maybe(20), UsesRandom: maybe(20), UsesNetwork: maybe(20),
			Lookups: []workflow.TransformLookup{
				{Ref: pick("", "l"), SnapshotDigest: pick("", "sha256:x")},
			},
			NormalizationProfile: pick("", "np"),
			InputTaint: []workflow.TaintInput{
				{Source: "s", Level: workflow.TaintLevel(pick("TRUSTED", "DERIVED", "TAINTED", ""))},
			},
			OutputTaint:         workflow.TaintLevel(pick("TRUSTED", "DERIVED", "TAINTED", "")),
			SanitizerReceiptRef: pick("", "san"),
			Limits: workflow.TransformLimits{
				MaxInputBytes: uint64(rng.Intn(100)), MaxOutputBytes: uint64(rng.Intn(100)), MaxSteps: uint64(rng.Intn(100)),
			},
		}
	case 3:
		node.Observe = &workflow.ObserveSpec{
			EvidenceKind:    workflow.ObservationEvidenceKind(pick("AUTHORITATIVE_READ", "SUBMISSION_RECEIPT", "TRANSPORT_ACK", "")),
			SourceAuthority: pick("", "sa"), ExpectedStateFields: strs(), RequiredWatermarks: strs(),
			MaxAgeSeconds: uint64(rng.Intn(100)), ComparisonProfile: pick("", "cp"),
			RetryExhaustionRoute: pick("", "n1"),
		}
	case 4:
		node.Wait = &workflow.WaitSpec{
			WakeKind:    workflow.WaitWakeKind(pick("AT_INSTANT", "AT_LOCAL_DATE", "AT_LOCAL_DATETIME", "AT_TIME_EXPR", "")),
			WakeInstant: pick("", "2026-01-01T00:00:00Z"), WakeLocalDate: pick("", "2026-01-02"),
			WakeLocalTime: pick("", "03:04:05"), Disambiguation: pick("", "EARLIER"),
			ZoneID: pick("", "UTC"), ZoneTzdbVersion: pick("", "2026a"),
			CalendarRef: pick("", "cal"), CalendarVersion: pick("", "cv"),
			ReferenceUpdatePolicy: pick("", "PIN"),
			TimeExpr: &workflow.TimeExprSpec{
				AnchorPath: pick("", "a"), OffsetKind: pick("", "ok"),
				OffsetDays: rng.Intn(200) - 100, OffsetSeconds: rng.Int63n(7200) - 3600,
				CalendarRef: pick("", "c"), CalendarVersion: pick("", "cv"),
				ZoneID: pick("", "z"), ZoneTzdbVersion: pick("", "tz"),
				LocalTime: pick("", "lt"), Disambiguation: pick("", "d"),
				StatuteRulePack: pick("", "srp"), StatuteRelease: pick("", "sr"), Statute: pick("", "s"),
			},
		}
	case 5:
		node.Signal = &workflow.SignalSpec{
			EventType: pick("", "e"), CorrelationKeyExpression: pick("", "c"),
			ExpectedSchemaRef: workflow.SchemaRef{SchemaID: "s", Version: 1, ProtobufFullName: "f"},
			AcceptedSources:   strs(),
			Ordering:          workflow.SignalOrdering(pick("NONE", "MONOTONIC_SEQUENCE", "")),
			CloseAfterSeconds: uint64(rng.Intn(100)),
			CloseTimeExpr:     &workflow.TimeExprSpec{AnchorPath: "a", OffsetKind: "ok"},
			TimeoutDefault:    &workflow.DeadlineDefaultSpec{Schema: "s", CanonicalJSON: `{"a":1}`},
		}
	case 6:
		node.End = &workflow.EndSpec{
			TerminalCode: pick("", "T"), RuntimeStatus: workflow.RuntimeStatus(pick("COMPLETED", "CANCELLED", "")),
			CompletionMapping: strmap(), OutstandingObligationRefs: strs(),
			RepairRefs: strs(), IncidentRefs: strs(),
			CommitReceiptRef: pick("", "cr"), ApprovalRequired: maybe(30),
			ApprovedMaterialDigest:         pick("", "sha256:y"),
			ClosurePolicyPermitsOpenRepair: maybe(30),
		}
	}
	if maybe(50) {
		node.ResolverRef = &workflow.VersionedRef{ID: "r", Version: "v"}
	}
	if maybe(50) {
		node.TimeoutPolicy = &workflow.VersionedRef{ID: "t", Version: "v"}
	}
	if maybe(50) {
		node.CompensationRef = &workflow.VersionedRef{ID: "c", Version: "v"}
	}
	if maybe(50) {
		node.ModeOverlay = &workflow.ModeOverlay{}
	}
	_ = seed
	return workflow.Definition{
		WorkflowID: pick("wf", ""), Version: uint32(rng.Intn(9)),
		Name:            pick("n", ""),
		Catalog:         &workflow.CatalogMetadata{DisplayName: pick("", "d"), Category: pick("", "c"), Description: pick("", "dd"), Keywords: strs(), Icon: pick("", "i"), Hidden: maybe(30)},
		IntentType:      pick("", "it"),
		MatchPredicate:  strmap(),
		InputSchema:     workflow.SchemaRef{SchemaID: "s", Version: 1, ProtobufFullName: "f"},
		OutputSchema:    workflow.SchemaRef{SchemaID: "s", Version: 1, ProtobufFullName: "f"},
		VariablesSchema: workflow.SchemaRef{SchemaID: "s", Version: 1, ProtobufFullName: "f"},
		Inputs:          []workflow.Field{field()},
		Outputs:         []workflow.Field{field()},
		TenantScope:     pick("", "t"), OrganizationScope: pick("", "o"), RiskClass: pick("", "r"),
		DeclaredModes: []workflow.ExecutionMode{
			workflow.ExecutionMode(pick("SIMULATE", "EXECUTE")),
		},
		TerminalProfile: workflow.TerminalProfile(pick("SIMULATE_ONLY", "EXECUTE", "")),
		StartNodeID:     "n0",
		Nodes:           []workflow.Node{node},
		Edges:           []workflow.Edge{{From: "n0", To: "n0", RouteKey: pick("", "r")}},
		ApprovalRequirements: []workflow.ApprovalRequirement{{
			ID: pick("", "a"), ResolverExpression: pick("", "re"), Scope: pick("", "s"),
			Quorum: rng.Intn(10) - 2, SeparationOfDuties: maybe(40),
			EffectiveAsOfPolicy: pick("", "e"),
		}},
		Obligations: []workflow.ObligationRequirement{{
			ID: pick("", "o"), Authority: pick("", "a"),
			InsertionPoint: workflow.InsertionPoint(pick("DEFINITION_PUBLICATION", "CLOSURE", "")),
			RequiredAction: pick("", "ra"), ResponsibleParty: pick("", "rp"),
			SatisfactionCondition: pick("", "sc"), SourceVersion: pick("", "sv"),
			ReevaluationPolicy: workflow.ReevaluationPolicy(pick("PIN", "BLOCK", "")),
			Mandatory:          maybe(40),
		}},
		Limits: workflow.Limits{
			MaxFanOut: uint32(rng.Intn(9)), MaxDepth: uint32(rng.Intn(9)), MaxNodes: uint32(rng.Intn(9)),
			DeclaredCycles: []workflow.CycleDeclaration{
				{EntryNodeID: "n0", GuardNodeID: "n0", MaxIterations: uint32(rng.Intn(9))},
			},
		},
		FailurePolicyRef: pick("", "f"), CancellationPolicyRef: pick("", "c"),
		MigrationPolicyRef: pick("", "m"), RetentionPolicyRef: pick("", "r"),
	}
}

// definitionDiff renders a compact structural diff for round-trip failures.
func definitionDiff(want, got workflow.Definition) string {
	wb, _ := workflow.Marshal(want)
	gb, _ := workflow.Marshal(got)
	wlines := strings.Split(string(wb), "\n")
	glines := strings.Split(string(gb), "\n")
	var sb strings.Builder
	for i := 0; i < len(wlines) || i < len(glines); i++ {
		var w, g string
		if i < len(wlines) {
			w = wlines[i]
		}
		if i < len(glines) {
			g = glines[i]
		}
		if w != g {
			sb.WriteString("line " + strings.TrimSpace(w) + "\n  want: " + w + "\n  got:  " + g + "\n")
		}
		if sb.Len() > 4000 {
			sb.WriteString("... (truncated)\n")
			break
		}
	}
	return sb.String()
}
