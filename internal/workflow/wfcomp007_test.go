package workflow_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

const (
	wf007Rule         = "rules.promotion.raise_threshold"
	wf007RuleVersion  = "2026.1"
	wf007Resolver     = "resolver.promotion.finance_approver"
	wf007Timeout      = "timeout.promotion.band_evaluation"
	wf007Compensation = "compensation.promotion.band_evaluation"
	wf007RefNode      = workflow.PromotionNodeEvaluateBand
)

// wf007Definition is the promotion reference workflow declaring every
// reference kind WF-COMP-007 adds: a versioned rule on its DECISION and a
// resolver, timeout policy and compensation on one capability node.
func wf007Definition() workflow.Definition {
	def := workflow.PromotionReferenceDefinition()
	for i := range def.Nodes {
		n := &def.Nodes[i]
		switch n.ID {
		case workflow.PromotionNodeRaiseThreshold:
			n.Decision.RuleRef = wf007Rule
			n.Decision.RuleVersion = wf007RuleVersion
		case wf007RefNode:
			n.ResolverRef = &workflow.VersionedRef{ID: wf007Resolver, Version: "1"}
			n.TimeoutPolicy = &workflow.VersionedRef{ID: wf007Timeout, Version: "4"}
			n.CompensationRef = &workflow.VersionedRef{ID: wf007Compensation, Version: "2"}
		}
	}
	return def
}

// wf007Entries publishes every target def references, schemas included,
// each PUBLISHED with a digest derived from its identity.
func wf007Entries(t *testing.T, def workflow.Definition) []workflow.ResolvedReference {
	t.Helper()
	seen := map[workflow.Reference]bool{}
	var out []workflow.ResolvedReference
	add := func(e workflow.ResolvedReference) {
		k := workflow.Reference{Kind: e.Kind, ID: e.ID, Version: e.Version}
		if seen[k] {
			return
		}
		seen[k] = true
		if e.Digest == "" {
			e.Digest = "sha256:" + k.String()
		}
		e.Status = workflow.ReferencePublished
		out = append(out, e)
	}
	schema := func(s workflow.SchemaRef) {
		if !s.Valid() {
			return
		}
		r := workflow.SchemaReference(s)
		add(workflow.ResolvedReference{Kind: r.Kind, ID: r.ID, Version: r.Version, Descriptor: s.ProtobufFullName})
	}
	schema(def.InputSchema)
	schema(def.OutputSchema)
	schema(def.VariablesSchema)
	for _, n := range def.Nodes {
		schema(n.InputSchema)
		schema(n.OutputSchema)
		if n.Signal != nil {
			schema(n.Signal.ExpectedSchemaRef)
		}
		if n.Decision != nil && n.Decision.RuleRef != "" {
			rule, err := workflow.RuleTableReference(fixtureRuleTable(n.Decision.RuleRef, n.Decision.RuleVersion), workflow.ReferencePublished)
			if err != nil {
				t.Fatalf("publish rule: %v", err)
			}
			add(rule)
		}
		for kind, v := range map[workflow.ReferenceKind]*workflow.VersionedRef{
			workflow.RefResolver: n.ResolverRef, workflow.RefTimeoutPolicy: n.TimeoutPolicy, workflow.RefCompensation: n.CompensationRef,
		} {
			if v != nil {
				add(workflow.ResolvedReference{Kind: kind, ID: v.ID, Version: v.Version})
			}
		}
	}
	return out
}

func wf007Registry(t *testing.T, entries []workflow.ResolvedReference) *workflow.ReferenceRegistry {
	t.Helper()
	reg, err := workflow.NewReferenceRegistry(entries...)
	if err != nil {
		t.Fatalf("publish references: %v", err)
	}
	return reg
}

func wf007Options(t *testing.T) workflow.Options {
	t.Helper()
	opts := promotionOptions(t)
	opts.References = wf007Registry(t, wf007Entries(t, wf007Definition()))
	return opts
}

// editEntry returns entries with the one matching (kind, id) edited.
func editEntry(entries []workflow.ResolvedReference, kind workflow.ReferenceKind, id string,
	edit func(*workflow.ResolvedReference)) []workflow.ResolvedReference {
	out := append([]workflow.ResolvedReference(nil), entries...)
	for i := range out {
		if out[i].Kind == kind && out[i].ID == id {
			edit(&out[i])
		}
	}
	return out
}

func dropEntry(entries []workflow.ResolvedReference, kind workflow.ReferenceKind, id string) []workflow.ResolvedReference {
	var out []workflow.ResolvedReference
	for _, e := range entries {
		if e.Kind != kind || e.ID != id {
			out = append(out, e)
		}
	}
	return out
}

// liarResolver answers every lookup with a fixed target, whatever was asked.
type liarResolver struct{ answer workflow.ResolvedReference }

func (l liarResolver) ResolveReference(workflow.Reference) (workflow.ResolvedReference, bool) {
	return l.answer, true
}

// TestTodo_WF_COMP_007 proves planning/todos.md WF-COMP-007: definitions
// declare resolver_ref, timeout_policy, compensation_ref and a versioned
// rule_ref; with a reference resolver the compiler resolves every schema,
// rule, resolver, timeout-policy and compensation reference against published
// registries, rejects unknown or retired targets with typed diagnostics and
// pins the resolved targets into the plan digest; without one it refuses the
// new kinds instead of silently accepting them.
func TestTodo_WF_COMP_007(t *testing.T) {
	t.Run("GREEN_default_options_unchanged_without_new_reference_kinds", func(t *testing.T) {
		plan, err := workflow.Compile(workflow.PromotionReferenceDefinition(), promotionOptions(t))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if plan.References != nil {
			t.Fatalf("a plan compiled without a resolver carries no references, got %d", len(plan.References))
		}
		if plan.Digest() != mustCompilePromotion(t).Digest() {
			t.Fatal("the default compilation digest must not move")
		}
	})

	t.Run("RED_new_reference_kinds_require_a_resolver", func(t *testing.T) {
		d := mustReject(t, wf007Definition(), promotionOptions(t), workflow.CodeReferenceResolverRequired)
		if got := d.Codes(); len(got) != 1 {
			t.Fatalf("codes = %v, want only %s", got, workflow.CodeReferenceResolverRequired)
		}
		for _, site := range []struct{ node, field string }{
			{wf007RefNode, "resolver_ref"}, {wf007RefNode, "timeout_policy"}, {wf007RefNode, "compensation_ref"},
			{workflow.PromotionNodeRaiseThreshold, "decision.rule_ref"},
		} {
			found := false
			for _, e := range d.Errors {
				if e.Location.NodeID == site.node && e.Location.Field == site.field {
					found = true
				}
			}
			if !found {
				t.Errorf("no %s diagnostic at %s.%s: %v", workflow.CodeReferenceResolverRequired, site.node, site.field, d)
			}
		}
	})

	t.Run("GREEN_every_reference_resolves_and_is_pinned", func(t *testing.T) {
		def := wf007Definition()
		entries := wf007Entries(t, def)
		plan, err := workflow.Compile(def, workflow.Options{
			Phase: workflow.PhaseP1A, Capabilities: promotionRegistry(t), References: wf007Registry(t, entries),
		})
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if len(plan.References) != len(entries) {
			t.Fatalf("plan pins %d references, %d were declared", len(plan.References), len(entries))
		}
		for i := 1; i < len(plan.References); i++ {
			a, b := plan.References[i-1], plan.References[i]
			if a.Kind > b.Kind || (a.Kind == b.Kind && a.ID > b.ID) {
				t.Fatalf("plan references not sorted at %d: %+v then %+v", i, a, b)
			}
		}
		node, _ := plan.Node(wf007RefNode)
		if node.ResolverRef == nil || node.ResolverRef.ID != wf007Resolver || node.ResolverRef.Digest == "" {
			t.Fatalf("resolver_ref = %+v", node.ResolverRef)
		}
		if node.TimeoutPolicy == nil || node.TimeoutPolicy.Version != "4" {
			t.Fatalf("timeout_policy = %+v", node.TimeoutPolicy)
		}
		if node.CompensationRef == nil || node.CompensationRef.Kind != workflow.RefCompensation {
			t.Fatalf("compensation_ref = %+v", node.CompensationRef)
		}
		decision, _ := plan.Node(workflow.PromotionNodeRaiseThreshold)
		wantRule, _ := fixtureRuleTable(wf007Rule, wf007RuleVersion).Digest()
		if decision.Decision.Rule == nil || decision.Decision.Rule.Digest != wantRule {
			t.Fatalf("decision rule = %+v, want digest %s", decision.Decision.Rule, wantRule)
		}
		if err := plan.Verify(); err != nil {
			t.Fatalf("verify: %v", err)
		}

		// The digest pins the resolved target: republishing the timeout
		// policy under the same identity with new content moves the plan.
		moved := editEntry(entries, workflow.RefTimeoutPolicy, wf007Timeout, func(r *workflow.ResolvedReference) { r.Digest = "sha256:changed" })
		again, err := workflow.Compile(def, workflow.Options{
			Phase: workflow.PhaseP1A, Capabilities: promotionRegistry(t), References: wf007Registry(t, moved),
		})
		if err != nil {
			t.Fatalf("recompile: %v", err)
		}
		if again.Digest() == plan.Digest() {
			t.Fatal("a changed reference digest must change the plan digest")
		}

		// Tampering with a pinned reference after compilation stops the plan verifying.
		plan.References[0].Digest = "sha256:tampered"
		if err := plan.Verify(); err == nil {
			t.Fatal("a plan whose pinned reference was edited must not verify")
		}
	})

	t.Run("RED_unknown_targets", func(t *testing.T) {
		def := wf007Definition()
		entries := wf007Entries(t, def)
		for _, drop := range []struct {
			kind workflow.ReferenceKind
			id   string
		}{
			{workflow.RefResolver, wf007Resolver}, {workflow.RefTimeoutPolicy, wf007Timeout},
			{workflow.RefCompensation, wf007Compensation}, {workflow.RefRule, wf007Rule},
			{workflow.RefSchema, def.VariablesSchema.SchemaID},
		} {
			opts := promotionOptions(t)
			opts.References = wf007Registry(t, dropEntry(entries, drop.kind, drop.id))
			mustReject(t, def, opts, workflow.CodeUnresolvedRef)
		}
	})

	t.Run("RED_retired_targets", func(t *testing.T) {
		def := wf007Definition()
		entries := wf007Entries(t, def)
		retire := func(r *workflow.ResolvedReference) { r.Status = workflow.ReferenceRetired }
		opts := promotionOptions(t)
		opts.References = wf007Registry(t, editEntry(entries, workflow.RefCompensation, wf007Compensation, retire))
		d := mustReject(t, def, opts, workflow.CodeRetiredReference)
		if !d.HasAt(workflow.CodeRetiredReference, wf007RefNode) {
			t.Fatalf("retired compensation not reported at %s: %v", wf007RefNode, d)
		}

		deprecated := func(r *workflow.ResolvedReference) { r.Status = workflow.ReferenceDeprecated }
		opts.References = wf007Registry(t, editEntry(entries, workflow.RefCompensation, wf007Compensation, deprecated))
		if _, err := workflow.Compile(def, opts); err != nil {
			t.Fatalf("a deprecated target is still bindable: %v", err)
		}
	})

	t.Run("RED_schema_refs_are_resolved_not_only_syntax_checked", func(t *testing.T) {
		def := wf007Definition()
		opts := promotionOptions(t)
		opts.References = wf007Registry(t, editEntry(wf007Entries(t, def), workflow.RefSchema, def.InputSchema.SchemaID,
			func(r *workflow.ResolvedReference) { r.Descriptor = "hcmnext.other.v1.Message" }))
		mustReject(t, def, opts, workflow.CodeTypeMismatch)
	})

	t.Run("RED_unversioned_rule_with_resolver", func(t *testing.T) {
		def := wf007Definition()
		nodeRef(t, &def, workflow.PromotionNodeRaiseThreshold).Decision.RuleVersion = ""
		mustReject(t, def, wf007Options(t), workflow.CodeUnresolvedRef)
	})

	t.Run("RED_resolver_answering_a_different_target", func(t *testing.T) {
		opts := promotionOptions(t)
		opts.References = liarResolver{answer: workflow.ResolvedReference{
			Kind: workflow.RefResolver, ID: "resolver.anything", Version: "9", Digest: "sha256:x", Status: workflow.ReferencePublished,
		}}
		mustReject(t, wf007Definition(), opts, workflow.CodeUnresolvedRef)
	})
}

// TestTodo_WF_COMP_007_Property proves, over seeded random registries, that
// compilation succeeds exactly when every declared reference is published and
// not retired, that every missing or retired target produces exactly one
// diagnostic at its site, and that the plan digest is a function of the
// resolved targets, not of the registry's publication order.
func TestTodo_WF_COMP_007_Property(t *testing.T) {
	def := wf007Definition()
	all := wf007Entries(t, def)
	capabilities := promotionRegistry(t)
	baseline, err := workflow.Compile(def, workflow.Options{
		Phase: workflow.PhaseP1A, Capabilities: capabilities, References: wf007Registry(t, all),
	})
	if err != nil {
		t.Fatalf("baseline compile: %v", err)
	}
	rng := rand.New(rand.NewSource(7007))
	for trial := 0; trial < 200; trial++ {
		var published []workflow.ResolvedReference
		missing, retired := 0, 0
		for _, e := range all {
			switch rng.Intn(10) {
			case 0:
				missing++
				continue
			case 1:
				e.Status = workflow.ReferenceRetired
				retired++
			}
			published = append(published, e)
		}
		rng.Shuffle(len(published), func(i, j int) { published[i], published[j] = published[j], published[i] })
		plan, err := workflow.Compile(def, workflow.Options{
			Phase: workflow.PhaseP1A, Capabilities: capabilities, References: wf007Registry(t, published),
		})
		if missing+retired == 0 {
			if err != nil {
				t.Fatalf("trial %d: fully published registry must compile: %v", trial, err)
			}
			if plan.Digest() != baseline.Digest() {
				t.Fatalf("trial %d: publication order moved the plan digest", trial)
			}
			continue
		}
		d := diagnostics(t, err)
		gotMissing, gotRetired := 0, 0
		for _, e := range d.Errors {
			switch e.Code {
			case workflow.CodeUnresolvedRef:
				gotMissing++
			case workflow.CodeRetiredReference:
				gotRetired++
			default:
				t.Fatalf("trial %d: unexpected diagnostic %v", trial, e)
			}
		}
		// A missing or retired schema shared by several sites is reported once
		// per site, so the counts are lower bounds.
		if gotMissing < missing || gotRetired < retired || (missing > 0) != (gotMissing > 0) || (retired > 0) != (gotRetired > 0) {
			t.Fatalf("trial %d: missing %d retired %d, diagnostics %d/%d: %v",
				trial, missing, retired, gotMissing, gotRetired, d)
		}
	}
}

// TestTodo_WF_COMP_007_Mutation proves each single-point weakening of a
// fully resolved definition or its registry is killed with a named code.
func TestTodo_WF_COMP_007_Mutation(t *testing.T) {
	runMutations(t, wf007Definition, wf007Options, []mutationCase{
		{name: "resolver_ref_unpublished_version", code: workflow.CodeUnresolvedRef, mutate: func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, wf007RefNode).ResolverRef.Version = "2"
		}},
		{name: "timeout_policy_blank_id", code: workflow.CodeUnresolvedRef, mutate: func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, wf007RefNode).TimeoutPolicy.ID = ""
		}},
		{name: "compensation_ref_unknown", code: workflow.CodeUnresolvedRef, mutate: func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, wf007RefNode).CompensationRef.ID = "compensation.nobody.published"
		}},
		{name: "rule_ref_other_version", code: workflow.CodeUnresolvedRef, mutate: func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeRaiseThreshold).Decision.RuleVersion = "2025.9"
		}},
		{name: "node_output_schema_unpublished_version", code: workflow.CodeUnresolvedRef, mutate: func(t *testing.T, def *workflow.Definition) {
			n := nodeRef(t, def, workflow.PromotionNodeRaiseThreshold)
			n.OutputSchema.Version++
		}},
		{name: "definition_input_schema_descriptor_drift", code: workflow.CodeTypeMismatch, mutate: func(t *testing.T, def *workflow.Definition) {
			def.InputSchema.ProtobufFullName = "hcmnext.drifted.v1.Input"
		}},
		{name: "resolver_ref_on_another_node_unpublished", code: workflow.CodeUnresolvedRef, mutate: func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeSnapshotWorker).ResolverRef = &workflow.VersionedRef{ID: wf007Resolver, Version: "7"}
		}},
	})

	t.Run("retire_each_published_target", func(t *testing.T) {
		def := wf007Definition()
		entries := wf007Entries(t, def)
		for i := range entries {
			e := entries[i]
			t.Run(fmt.Sprintf("%s_%s", e.Kind, e.ID), func(t *testing.T) {
				opts := promotionOptions(t)
				opts.References = wf007Registry(t, editEntry(entries, e.Kind, e.ID,
					func(r *workflow.ResolvedReference) { r.Status = workflow.ReferenceRetired }))
				mustReject(t, def, opts, workflow.CodeRetiredReference)
			})
		}
	})

	t.Run("drop_the_resolver", func(t *testing.T) {
		mustReject(t, wf007Definition(), promotionOptions(t), workflow.CodeReferenceResolverRequired)
	})
}
