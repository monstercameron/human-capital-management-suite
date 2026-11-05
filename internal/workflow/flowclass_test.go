// TestTodo_WF_DATA_004 proves field types carry their classification, node
// inputs declare the highest classification they accept, and the compiler
// rejects any mapping routing a more restricted value into a
// less-protected destination unless a reviewed declassification is
// referenced. Capability inputs, message template slots, document bindings
// and connector requests all arrive at the compiler as node input fields,
// so the tests pin caps on node inputs standing for each.
package workflow_test

import (
	"math/rand"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func wfd4LabelInput(t *testing.T, def *workflow.Definition, path string, label workflow.DataClassification) {
	t.Helper()
	for i := range def.Inputs {
		if def.Inputs[i].Path == path {
			def.Inputs[i].Type.Classification = label
			return
		}
	}
	t.Fatalf("definition declares no input %q", path)
}

func wfd4LabelOutput(t *testing.T, def *workflow.Definition, nodeID, path string, label workflow.DataClassification) {
	t.Helper()
	n := nodeRef(t, def, nodeID)
	for i := range n.Outputs {
		if n.Outputs[i].Path == path {
			n.Outputs[i].Type.Classification = label
			return
		}
	}
	t.Fatalf("node %s declares no output %q", nodeID, path)
}

func wfd4CapInput(t *testing.T, def *workflow.Definition, nodeID, path string, max workflow.DataClassification) {
	t.Helper()
	n := nodeRef(t, def, nodeID)
	for i := range n.Inputs {
		if n.Inputs[i].Path == path {
			n.Inputs[i].MaxClassification = max
			return
		}
	}
	t.Fatalf("node %s declares no input %q", nodeID, path)
}

func wfd4Declassify(t *testing.T, def *workflow.Definition, nodeID, target, ref string) {
	t.Helper()
	n := nodeRef(t, def, nodeID)
	for i := range n.InputMappings {
		if n.InputMappings[i].Target == target {
			n.InputMappings[i].DeclassificationRef = ref
			return
		}
	}
	t.Fatalf("node %s binds no mapping for %q", nodeID, target)
}

func wfd4CompiledMapping(t *testing.T, plan *workflow.CompiledWorkflow, nodeID, target string) workflow.CompiledMapping {
	t.Helper()
	n, ok := plan.Node(nodeID)
	if !ok {
		t.Fatalf("plan has no node %s", nodeID)
	}
	for _, m := range n.Mappings {
		if m.Target == target {
			return m
		}
	}
	t.Fatalf("plan node %s has no compiled mapping for %q", nodeID, target)
	return workflow.CompiledMapping{}
}

func TestTodo_WF_DATA_004(t *testing.T) {
	t.Run("lattice_orders_parse_and_dominates", func(t *testing.T) {
		lattice := []workflow.DataClassification{
			workflow.DataPublic, workflow.DataInternal,
			workflow.DataConfidential, workflow.DataRestricted,
		}
		for i, lo := range lattice {
			if !lo.Valid() {
				t.Fatalf("lattice member %q must be valid", lo)
			}
			for j, hi := range lattice {
				if got, want := hi.Dominates(lo), j >= i; got != want {
					t.Fatalf("Dominates(%s, %s) = %v, want %v", hi, lo, got, want)
				}
			}
			parsed, err := workflow.ParseDataClassification(string(lo))
			if err != nil || parsed != lo {
				t.Fatalf("ParseDataClassification(%q) = %v, %v", lo, parsed, err)
			}
		}
		for _, raw := range []string{"", "public", "SECRET", "CONFIDENTIAL_HR", "RESTRICTED "} {
			if raw == "RESTRICTED " {
				if _, err := workflow.ParseDataClassification(raw); err != nil {
					t.Fatalf("surrounding whitespace is tolerated: %v", err)
				}
				continue
			}
			if _, err := workflow.ParseDataClassification(raw); err == nil {
				t.Fatalf("ParseDataClassification(%q) must fail closed", raw)
			}
		}
		if workflow.DataRestricted.Dominates("SECRET") || workflow.DataClassification("SECRET").Dominates(workflow.DataPublic) {
			t.Fatal("unknown labels dominate nothing and are dominated by nothing")
		}
	})

	t.Run("legacy_unlabeled_plans_compile_unchanged", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		n, ok := plan.Node(workflow.PromotionNodeSimulateComp)
		if !ok {
			t.Fatal("reference plan lost its simulate node")
		}
		for _, m := range n.Mappings {
			if m.SourceType.Classification != "" || m.MaxClassification != "" || m.DeclassificationRef != "" {
				t.Fatalf("legacy compiled mapping carries flow provenance: %+v", m)
			}
		}
	})

	t.Run("labeled_source_to_generous_cap_compiles_with_provenance", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &def, "proposed_base_pay", workflow.DataRestricted)
		wfd4CapInput(t, &def, workflow.PromotionNodeSimulateComp, "proposed_base_pay", workflow.DataRestricted)
		plan, err := workflow.Compile(def, promotionOptions(t))
		if err != nil {
			t.Fatalf("labeled flow within cap must compile: %v", err)
		}
		m := wfd4CompiledMapping(t, plan, workflow.PromotionNodeSimulateComp, "proposed_base_pay")
		if m.SourceType.Classification != workflow.DataRestricted || m.MaxClassification != workflow.DataRestricted {
			t.Fatalf("compiled mapping lost its flow labels: %+v", m)
		}
	})

	t.Run("restricted_source_to_internal_cap_is_rejected", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &def, "proposed_base_pay", workflow.DataRestricted)
		wfd4CapInput(t, &def, workflow.PromotionNodeSimulateComp, "proposed_base_pay", workflow.DataInternal)
		d := mustReject(t, def, promotionOptions(t), workflow.CodeRestrictedFlow)
		if !d.HasAt(workflow.CodeRestrictedFlow, workflow.PromotionNodeSimulateComp) {
			t.Fatalf("diagnostic must name the violating node, got %v", d.Errors)
		}
	})

	t.Run("node_output_sources_are_checked", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		wfd4LabelOutput(t, &def, workflow.PromotionNodeSnapshotWorker, "worker_id", workflow.DataConfidential)
		wfd4CapInput(t, &def, workflow.PromotionNodeSimulateComp, "worker_id", workflow.DataPublic)
		mustReject(t, def, promotionOptions(t), workflow.CodeRestrictedFlow)
	})

	t.Run("declassification_excepts_and_is_recorded", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &def, "proposed_base_pay", workflow.DataRestricted)
		wfd4CapInput(t, &def, workflow.PromotionNodeSimulateComp, "proposed_base_pay", workflow.DataInternal)
		wfd4Declassify(t, &def, workflow.PromotionNodeSimulateComp, "proposed_base_pay", "review/DEC-1042")
		plan, err := workflow.Compile(def, promotionOptions(t))
		if err != nil {
			t.Fatalf("referenced declassification must except the flow: %v", err)
		}
		m := wfd4CompiledMapping(t, plan, workflow.PromotionNodeSimulateComp, "proposed_base_pay")
		if m.DeclassificationRef != "review/DEC-1042" {
			t.Fatalf("compiled mapping lost the exception: %+v", m)
		}
		blank := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &blank, "proposed_base_pay", workflow.DataRestricted)
		wfd4CapInput(t, &blank, workflow.PromotionNodeSimulateComp, "proposed_base_pay", workflow.DataInternal)
		wfd4Declassify(t, &blank, workflow.PromotionNodeSimulateComp, "proposed_base_pay", "  ")
		mustReject(t, blank, promotionOptions(t), workflow.CodeRestrictedFlow)
	})

	t.Run("unknown_labels_fail_closed", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &def, "worker_id", "SECRET")
		mustReject(t, def, promotionOptions(t), workflow.CodeInvalidDefinition)

		capped := workflow.PromotionReferenceDefinition()
		wfd4CapInput(t, &capped, workflow.PromotionNodeSimulateComp, "worker_id", "SECRET")
		mustReject(t, capped, promotionOptions(t), workflow.CodeInvalidDefinition)

		hr := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &hr, "worker_id", "CONFIDENTIAL_HR")
		mustReject(t, hr, promotionOptions(t), workflow.CodeInvalidDefinition)

		incoherent := workflow.PromotionReferenceDefinition()
		n := nodeRef(t, &incoherent, workflow.PromotionNodeSimulateComp)
		for i := range n.Inputs {
			if n.Inputs[i].Path == "worker_id" {
				n.Inputs[i].Type.Classification = workflow.DataConfidential
				n.Inputs[i].MaxClassification = workflow.DataInternal
			}
		}
		mustReject(t, incoherent, promotionOptions(t), workflow.CodeInvalidDefinition)
	})
}

// TestTodo_WF_DATA_004_Security proves restricted values cannot reach a
// message slot, document binding, connector request or log sink capped
// below them, that context reads face the same comparison, and that only a
// referenced declassification excepts a flow.
func TestTodo_WF_DATA_004_Security(t *testing.T) {
	t.Run("message_slot_capped_internal_refuses_restricted", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &def, "proposed_base_pay", workflow.DataRestricted)
		// The capability input stands for the message template slot the
		// messaging builder declares at INTERNAL.
		wfd4CapInput(t, &def, workflow.PromotionNodeEvaluateBand, "proposed_base_pay", workflow.DataInternal)
		mustReject(t, def, promotionOptions(t), workflow.CodeRestrictedFlow)
	})

	t.Run("document_binding_honors_its_cap", func(t *testing.T) {
		over := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &over, "proposed_base_pay", workflow.DataRestricted)
		wfd4CapInput(t, &over, workflow.PromotionNodeEvaluateBand, "proposed_base_pay", workflow.DataConfidential)
		mustReject(t, over, promotionOptions(t), workflow.CodeRestrictedFlow)

		within := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &within, "proposed_base_pay", workflow.DataConfidential)
		wfd4CapInput(t, &within, workflow.PromotionNodeEvaluateBand, "proposed_base_pay", workflow.DataConfidential)
		if _, err := workflow.Compile(within, promotionOptions(t)); err != nil {
			t.Fatalf("flow within the document cap must compile: %v", err)
		}
	})

	t.Run("connector_request_capped_internal_refuses_confidential", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &def, "worker_id", workflow.DataConfidential)
		wfd4CapInput(t, &def, workflow.PromotionNodeSnapshotWorker, "worker_id", workflow.DataInternal)
		mustReject(t, def, promotionOptions(t), workflow.CodeRestrictedFlow)
	})

	t.Run("log_sink_capped_public_refuses_internal", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &def, "target_job_id", workflow.DataInternal)
		wfd4CapInput(t, &def, workflow.PromotionNodeSimulateComp, "target_job_id", workflow.DataPublic)
		mustReject(t, def, promotionOptions(t), workflow.CodeRestrictedFlow)
	})

	t.Run("context_reads_face_the_same_comparison", func(t *testing.T) {
		rewire := func(t *testing.T, label, cap workflow.DataClassification) workflow.Definition {
			t.Helper()
			def := workflow.PromotionReferenceDefinition()
			n := nodeRef(t, &def, workflow.PromotionNodeSimulateComp)
			n.RequiredContext = []workflow.ContextRequirement{{
				Kind: "LegalContext", FieldPaths: []string{"jurisdiction"},
				Purpose: "promotion.simulation.reads", MaximumClassification: "CONFIDENTIAL_HR",
				MaxAgeSeconds: 3600, Pinned: true, MissingBehavior: workflow.MissingUnknown,
			}}
			for i := range n.InputMappings {
				if n.InputMappings[i].Target == "worker_id" {
					n.InputMappings[i].Source = workflow.Source{
						Kind:        workflow.SourceContext,
						ContextKind: "LegalContext",
						Path:        "jurisdiction",
						Type:        workflow.ValueType{Kind: workflow.KindString, Brand: "WorkerID", Classification: label},
					}
				}
			}
			for i := range n.Inputs {
				if n.Inputs[i].Path == "worker_id" {
					n.Inputs[i].MaxClassification = cap
				}
			}
			return def
		}
		mustReject(t, rewire(t, workflow.DataRestricted, workflow.DataInternal), promotionOptions(t), workflow.CodeRestrictedFlow)
		if _, err := workflow.Compile(rewire(t, workflow.DataRestricted, workflow.DataRestricted), promotionOptions(t)); err != nil {
			t.Fatalf("context flow within cap must compile: %v", err)
		}
	})

	t.Run("declassification_excepts_with_provenance", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		wfd4LabelInput(t, &def, "proposed_base_pay", workflow.DataRestricted)
		wfd4CapInput(t, &def, workflow.PromotionNodeEvaluateBand, "proposed_base_pay", workflow.DataInternal)
		wfd4Declassify(t, &def, workflow.PromotionNodeEvaluateBand, "proposed_base_pay", "review/DEC-2048")
		plan, err := workflow.Compile(def, promotionOptions(t))
		if err != nil {
			t.Fatalf("referenced declassification must except the flow: %v", err)
		}
		m := wfd4CompiledMapping(t, plan, workflow.PromotionNodeEvaluateBand, "proposed_base_pay")
		if m.DeclassificationRef != "review/DEC-2048" || m.SourceType.Classification != workflow.DataRestricted {
			t.Fatalf("compiled mapping lost the exception provenance: %+v", m)
		}
	})

	t.Run("terminal_output_caps_are_enforced", func(t *testing.T) {
		endID := ""
		def := workflow.PromotionReferenceDefinition()
		for _, n := range def.Nodes {
			if n.Type == workflow.StepEnd {
				endID = n.ID
			}
		}
		if endID == "" {
			t.Fatal("reference definition has no terminal")
		}
		capOutput := func(t *testing.T, ref string) workflow.Definition {
			t.Helper()
			d := workflow.PromotionReferenceDefinition()
			for i := range d.Outputs {
				if d.Outputs[i].Path == "worker_id" {
					d.Outputs[i].MaxClassification = workflow.DataInternal
				}
			}
			n := nodeRef(t, &d, endID)
			for i := range n.Inputs {
				if n.Inputs[i].Path == "worker_id" {
					n.Inputs[i].Type.Classification = workflow.DataRestricted
				}
			}
			if ref != "" {
				for i := range n.InputMappings {
					if n.InputMappings[i].Target == "worker_id" {
						n.InputMappings[i].DeclassificationRef = ref
					}
				}
			}
			return d
		}
		mustReject(t, capOutput(t, ""), promotionOptions(t), workflow.CodeRestrictedFlow)
		if _, err := workflow.Compile(capOutput(t, "review/DEC-4096"), promotionOptions(t)); err != nil {
			t.Fatalf("terminal mapping exception must except the output flow: %v", err)
		}
	})
}

// wfd4Rank is the property oracle's independent copy of the lattice order.
func wfd4Rank(label workflow.DataClassification) int {
	switch label {
	case "", workflow.DataPublic:
		return 0
	case workflow.DataInternal:
		return 1
	case workflow.DataConfidential:
		return 2
	case workflow.DataRestricted:
		return 3
	default:
		return -1
	}
}

// TestTodo_WF_DATA_004_Property proves no compiled plan contains a
// restricted flow: across random labelings, compilation succeeds exactly
// when no mapping or terminal output routes a more restricted value into a
// less-protected destination without a referenced declassification, and
// every compiled mapping carries faithful labels.
func TestTodo_WF_DATA_004_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	labels := []workflow.DataClassification{"", workflow.DataPublic, workflow.DataInternal, workflow.DataConfidential, workflow.DataRestricted}
	pick := func() workflow.DataClassification { return labels[rng.Intn(len(labels))] }

	inputsByPath := func(def *workflow.Definition) map[string]workflow.ValueType {
		out := map[string]workflow.ValueType{}
		for _, f := range def.Inputs {
			out[f.Path] = f.Type
		}
		return out
	}
	nodesByID := func(def *workflow.Definition) map[string]*workflow.Node {
		out := map[string]*workflow.Node{}
		for i := range def.Nodes {
			out[def.Nodes[i].ID] = &def.Nodes[i]
		}
		return out
	}
	sourceLabel := func(t *testing.T, def *workflow.Definition, nodes map[string]*workflow.Node, src workflow.Source) workflow.DataClassification {
		t.Helper()
		switch src.Kind {
		case workflow.SourceWorkflowInput:
			return inputsByPath(def)[src.Path].Classification
		case workflow.SourceNodeOutput:
			producer := nodes[src.NodeID]
			for _, f := range producer.Outputs {
				if f.Path == src.Path {
					return f.Type.Classification
				}
			}
			t.Fatalf("oracle: producer %s declares no output %s", src.NodeID, src.Path)
		case workflow.SourceConstant:
			return workflow.DataPublic
		default:
			t.Fatalf("oracle: unhandled source kind %q", src.Kind)
		}
		return ""
	}

	for i := 0; i < 200; i++ {
		def := workflow.PromotionReferenceDefinition()
		for j := range def.Inputs {
			def.Inputs[j].Type.Classification = pick()
		}
		for j := range def.Outputs {
			def.Outputs[j].Type.Classification = pick()
			def.Outputs[j].MaxClassification = pick()
		}
		for j := range def.Nodes {
			n := &def.Nodes[j]
			for k := range n.Inputs {
				n.Inputs[k].Type.Classification = pick()
				n.Inputs[k].MaxClassification = pick()
			}
			for k := range n.Outputs {
				n.Outputs[k].Type.Classification = pick()
			}
			for k := range n.InputMappings {
				if rng.Intn(4) == 0 {
					n.InputMappings[k].DeclassificationRef = "review/DEC-PROP"
				}
			}
		}
		nodes := nodesByID(&def)

		incoherent := false
		coherence := func(fields []workflow.Field) {
			for _, f := range fields {
				if f.MaxClassification != "" && f.Type.Classification != "" &&
					wfd4Rank(f.MaxClassification) < wfd4Rank(f.Type.Classification) {
					incoherent = true
				}
			}
		}
		coherence(def.Inputs)
		coherence(def.Outputs)
		for _, n := range nodes {
			coherence(n.Inputs)
			coherence(n.Outputs)
		}
		violated := false
		for _, n := range nodes {
			for _, m := range n.InputMappings {
				var cap workflow.DataClassification
				for _, f := range n.Inputs {
					if f.Path == m.Target {
						cap = f.MaxClassification
					}
				}
				label := sourceLabel(t, &def, nodes, m.Source)
				if cap != "" && wfd4Rank(cap) < wfd4Rank(label) && m.DeclassificationRef == "" {
					violated = true
				}
			}
		}
		for _, n := range nodes {
			if n.Type != workflow.StepEnd {
				continue
			}
			for _, out := range def.Outputs {
				var got workflow.ValueType
				for _, f := range n.Inputs {
					if f.Path == out.Path {
						got = f.Type
					}
				}
				ref := ""
				for _, m := range n.InputMappings {
					if m.Target == out.Path {
						ref = m.DeclassificationRef
					}
				}
				if out.MaxClassification != "" && wfd4Rank(out.MaxClassification) < wfd4Rank(got.Classification) && ref == "" {
					violated = true
				}
			}
		}

		plan, err := workflow.Compile(def, promotionOptions(t))
		switch {
		case incoherent:
			if err == nil {
				t.Fatalf("case %d: incoherent field caps compiled", i)
			}
			if d := diagnostics(t, err); !d.Has(workflow.CodeInvalidDefinition) {
				t.Fatalf("case %d: expected %s, got %v", i, workflow.CodeInvalidDefinition, d.Codes())
			}
		case violated:
			if err == nil {
				t.Fatalf("case %d: restricted flow compiled", i)
			}
			d := diagnostics(t, err)
			if !d.Has(workflow.CodeRestrictedFlow) {
				t.Fatalf("case %d: expected %s, got %v", i, workflow.CodeRestrictedFlow, d.Codes())
			}
		default:
			if err != nil {
				t.Fatalf("case %d: clean labeling rejected: %v", i, err)
			}
			for _, cn := range plan.Nodes {
				dn := nodes[cn.ID]
				for _, cm := range cn.Mappings {
					var dm workflow.Mapping
					for _, m := range dn.InputMappings {
						if m.Target == cm.Target {
							dm = m
						}
					}
					var cap workflow.DataClassification
					for _, f := range dn.Inputs {
						if f.Path == cm.Target {
							cap = f.MaxClassification
						}
					}
					label := sourceLabel(t, &def, nodes, dm.Source)
					if cm.SourceType.Classification != label || cm.MaxClassification != cap || cm.DeclassificationRef != dm.DeclassificationRef {
						t.Fatalf("case %d: compiled mapping %s.%s unfaithful: %+v", i, cn.ID, cm.Target, cm)
					}
					if cm.MaxClassification != "" &&
						wfd4Rank(cm.MaxClassification) < wfd4Rank(cm.SourceType.Classification) &&
						cm.DeclassificationRef == "" {
						t.Fatalf("case %d: compiled plan contains a restricted flow: %+v", i, cm)
					}
				}
			}
		}
	}
}
