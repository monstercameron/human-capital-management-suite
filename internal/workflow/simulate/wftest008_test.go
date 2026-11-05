package simulate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// cancelContracts declares the reversal contract for the mutating
// fixture's write node: payroll sync compensates through a published
// reversal and commits in the payroll atomic region.
func cancelContracts() []workflow.WriteNode {
	return []workflow.WriteNode{{
		NodeID:       fxSync,
		Effect:       workflow.EffectRecord{ID: "payroll.sync_worker", Compensation: "payroll.reverse_sync"},
		AtomicRegion: "payroll",
	}}
}

func cancelOptions(t *testing.T, cancel *simulate.CancelSpec) simulate.Options {
	t.Helper()
	recording := &simulate.RecordingProfile{Contract: recordingContract(t), Recorder: simulate.NewRecording()}
	return simulate.Options{
		Capabilities: recordingRegistry(t),
		Reads:        recordingReads{},
		Recording:    recording,
		Cancel:       cancel,
	}
}

func cancelSpec(atNode string) *simulate.CancelSpec {
	return &simulate.CancelSpec{AtNodeID: atNode, Contracts: cancelContracts()}
}

// TestTodo_WF_TEST_008 is the PRIMARY contract: the author cancels a test
// run at a node; the receipt records the unwind the compiled plan demands
// (compensations, corrections, human-decision prompts) instead of a
// terminal the run never reached.
func TestTodo_WF_TEST_008(t *testing.T) {
	ctx := context.Background()
	plan := mustCompileMutatingPlan(t, func() {})
	in := recordingInputs()

	t.Run("cancel_at_write_node_unwinds_per_plan", func(t *testing.T) {
		receipt, err := simulate.Run(ctx, plan, in, cancelOptions(t, cancelSpec(fxSync)))
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Terminal.TerminalCode != "CANCELLED" || receipt.Terminal.RuntimeStatus != workflow.RuntimeCancelled {
			t.Fatalf("terminal = %+v, want CANCELLED", receipt.Terminal)
		}
		if receipt.Cancel == nil || receipt.Cancel.AtNodeID != fxSync {
			t.Fatalf("cancel record = %+v, want cancel at %s", receipt.Cancel, fxSync)
		}
		requireCancelMatchesPlan(t, receipt, cancelContracts())
		for _, trace := range receipt.Trace {
			if trace.NodeID == fxObserve {
				t.Fatal("observe ran after the cancel")
			}
		}
	})

	t.Run("cancel_before_any_write_records_empty_unwind", func(t *testing.T) {
		receipt, err := simulate.Run(ctx, plan, in, cancelOptions(t, cancelSpec(fxRead)))
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Cancel == nil || len(receipt.Cancel.Committed) != 0 || len(receipt.Cancel.Unwind) != 0 {
			t.Fatalf("cancel record = %+v, want an empty unwind", receipt.Cancel)
		}
	})

	t.Run("unknown_cancel_point_refused", func(t *testing.T) {
		_, err := simulate.Run(ctx, plan, in, cancelOptions(t, cancelSpec("no_such_node")))
		if err == nil {
			t.Fatal("unknown cancel point ran without error")
		}
		var simErr *simulate.Error
		if !errors.As(err, &simErr) || simErr.Code != simulate.CodeInvalidOptions {
			t.Fatalf("err = %v, want an INVALID_OPTIONS refusal", err)
		}
	})

	t.Run("unreached_cancel_point_refused", func(t *testing.T) {
		_, err := simulate.Run(ctx, plan, in, cancelOptions(t, cancelSpec(fxEndCommit)))
		if err == nil {
			t.Fatal("unreached cancel point ran without error")
		}
		var simErr *simulate.Error
		if !errors.As(err, &simErr) || simErr.Code != simulate.CodeCancelPointUnreached {
			t.Fatalf("err = %v, want a CANCEL_POINT_UNREACHED refusal", err)
		}
	})

	t.Run("write_without_contract_refused", func(t *testing.T) {
		spec := cancelSpec(fxSync)
		spec.Contracts = nil
		if _, err := simulate.Run(ctx, plan, in, cancelOptions(t, spec)); err == nil {
			t.Fatal("contract-free unwind ran without error")
		} else if !errors.Is(err, workflow.ErrNoReversalContract) {
			t.Fatalf("err = %v, want ErrNoReversalContract", err)
		}
	})

	t.Run("correction_contract_records_correct_action", func(t *testing.T) {
		spec := cancelSpec(fxSync)
		spec.Contracts = []workflow.WriteNode{{
			NodeID: fxSync,
			Effect: workflow.EffectRecord{ID: "payroll.sync_worker", Correction: "payroll.fix_sync"},
		}}
		receipt, err := simulate.Run(ctx, plan, in, cancelOptions(t, spec))
		if err != nil {
			t.Fatal(err)
		}
		if len(receipt.Cancel.Unwind) != 1 || receipt.Cancel.Unwind[0].Effects[0].Action != "CORRECT:payroll.fix_sync" {
			t.Fatalf("unwind = %+v, want the payroll correction", receipt.Cancel.Unwind)
		}
	})

	t.Run("cancel_strands_open_approvals_as_prompts", func(t *testing.T) {
		setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
		opts := setup.Options
		opts.Cancel = &simulate.CancelSpec{AtNodeID: workflow.PromotionNodeEndApproval}
		receipt, err := simulate.Run(ctx, setup.Plan, setup.Inputs, opts)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Cancel == nil || len(receipt.Cancel.Prompts) == 0 {
			t.Fatalf("cancel record = %+v, want stranded approval prompts", receipt.Cancel)
		}
		if len(receipt.Cancel.Prompts) != len(receipt.WorkItems) {
			t.Fatalf("%d prompts for %d work items", len(receipt.Cancel.Prompts), len(receipt.WorkItems))
		}
		for i, prompt := range receipt.Cancel.Prompts {
			if prompt.NodeID != receipt.WorkItems[i].NodeID || prompt.RequirementID != receipt.WorkItems[i].RequirementID {
				t.Fatalf("prompt %d = %+v, want the open work item", i, prompt)
			}
		}
	})
}

// cancelFixture is one plan CONFORMANCE cancels at each safe point of,
// with its recording options and reversal contracts.
type cancelFixture struct {
	name      string
	plan      *workflow.CompiledWorkflow
	contracts []workflow.WriteNode
	inputs    simulate.Inputs
	options   func(t *testing.T, cancel *simulate.CancelSpec) simulate.Options
}

// cancelFixtures covers the mutating read/sync/observe plan and one
// single-write plan per effect class. Contracts declare walk-consistent
// commit order.
func cancelFixtures(t *testing.T) []cancelFixture {
	t.Helper()
	out := []cancelFixture{{
		name:      "mutating",
		plan:      mustCompileMutatingPlan(t, func() {}),
		contracts: cancelContracts(),
		inputs:    recordingInputs(),
		options:   cancelOptions,
	}}
	for _, class := range []capability.EffectClass{
		capability.EffectInternalMutation,
		capability.EffectExternalMutation,
		capability.EffectIrreversibleExternalMutation,
	} {
		class := class
		def := propertyDefinition(class)
		plan, err := workflow.Compile(def, workflow.Options{
			Phase:        workflow.PhaseP1B,
			Capabilities: propertyRegistry(t, class, new(int)),
		})
		if err != nil {
			t.Fatalf("compile %s plan: %v", class, err)
		}
		out = append(out, cancelFixture{
			name:      "property-" + string(class),
			plan:      plan,
			contracts: propertyCancelContracts(plan),
			inputs:    recordingInputs(),
			options: func(t *testing.T, cancel *simulate.CancelSpec) simulate.Options {
				t.Helper()
				recording := &simulate.RecordingProfile{Contract: recordingContract(t), Recorder: simulate.NewRecording()}
				return simulate.Options{
					Capabilities: propertyRegistry(t, class, new(int)),
					Reads:        recordingReads{},
					Recording:    recording,
					Cancel:       cancel,
				}
			},
		})
	}
	return out
}

// propertyCancelContracts declares a reversible contract for every
// write-class node of a property plan; single-write plans unwind trivially.
func propertyCancelContracts(plan *workflow.CompiledWorkflow) []workflow.WriteNode {
	var out []workflow.WriteNode
	for _, node := range plan.Nodes {
		if node.EffectClass.IsWrite() {
			out = append(out, workflow.WriteNode{
				NodeID: node.ID,
				Effect: workflow.EffectRecord{ID: "test." + node.ID, Reversible: true},
			})
		}
	}
	return out
}

// dispatchedSafePoints runs the plan to completion under recording and
// returns the safe points the walk actually reached, in walk order.
func dispatchedSafePoints(t *testing.T, plan *workflow.CompiledWorkflow, in simulate.Inputs, opts simulate.Options) []string {
	t.Helper()
	opts.Cancel = nil
	full, err := simulate.Run(context.Background(), plan, in, opts)
	if err != nil {
		t.Fatalf("full run: %v", err)
	}
	safe := map[string]bool{}
	for _, node := range plan.Nodes {
		if node.SafePoint {
			safe[node.ID] = true
		}
	}
	var points []string
	for _, trace := range full.Trace {
		if safe[trace.NodeID] {
			points = append(points, trace.NodeID)
		}
	}
	return points
}

// TestTodo_WF_TEST_008_Conformance cancels at every safe point each
// fixture run reaches and proves every receipt follows the compiled plan.
func TestTodo_WF_TEST_008_Conformance(t *testing.T) {
	for _, fx := range cancelFixtures(t) {
		fx := fx
		t.Run(fx.name, func(t *testing.T) {
			points := dispatchedSafePoints(t, fx.plan, fx.inputs, fx.options(t, nil))
			if len(points) == 0 {
				t.Fatal("fixture run reached no safe point")
			}
			for _, point := range points {
				receipt, err := simulate.Run(context.Background(), fx.plan, fx.inputs,
					fx.options(t, &simulate.CancelSpec{AtNodeID: point, Contracts: fx.contracts}))
				if err != nil {
					t.Fatalf("cancel at %s: %v", point, err)
				}
				requireCancelMatchesPlan(t, receipt, fx.contracts)
			}
		})
	}
}

// TestTodo_WF_TEST_008_Property proves the recorded settlement always
// matches the compiled unwind plan: committed effects equal the recorded
// set, every action follows its contract, unwind order is the reverse
// commit order with regions grouped, and reruns are digest-stable.
func TestTodo_WF_TEST_008_Property(t *testing.T) {
	for _, fx := range cancelFixtures(t) {
		fx := fx
		t.Run(fx.name, func(t *testing.T) {
			points := dispatchedSafePoints(t, fx.plan, fx.inputs, fx.options(t, nil))
			for _, point := range points {
				first, err := simulate.Run(context.Background(), fx.plan, fx.inputs,
					fx.options(t, &simulate.CancelSpec{AtNodeID: point, Contracts: fx.contracts}))
				if err != nil {
					t.Fatalf("cancel at %s: %v", point, err)
				}
				second, err := simulate.Run(context.Background(), fx.plan, fx.inputs,
					fx.options(t, &simulate.CancelSpec{AtNodeID: point, Contracts: fx.contracts}))
				if err != nil {
					t.Fatalf("rerun cancel at %s: %v", point, err)
				}
				if first.Digest() != second.Digest() {
					t.Fatalf("cancel at %s is digest-unstable", point)
				}
				requireCancelMatchesPlan(t, first, fx.contracts)
			}
		})
	}
}

// requireCancelMatchesPlan proves a cancel receipt follows the compiled
// unwind plan, from first principles rather than implementation helpers:
// the committed set equals the recorded set, every action follows its
// declared contract, the unwind is the reverse commit order with regions
// grouped, and the whole record equals a fresh planner compilation.
func requireCancelMatchesPlan(t *testing.T, receipt simulate.Receipt, contracts []workflow.WriteNode) {
	t.Helper()
	if receipt.Cancel == nil {
		t.Fatal("receipt carries no cancel record")
	}
	recorded := map[string]bool{}
	for _, rec := range receipt.RecordedEffects {
		recorded[rec.NodeID] = true
	}
	if len(receipt.Cancel.Committed) != len(recorded) {
		t.Fatalf("committed %d effects, recorded %d", len(receipt.Cancel.Committed), len(recorded))
	}
	byNode := map[string]workflow.WriteNode{}
	for _, c := range contracts {
		byNode[c.NodeID] = c
	}
	for _, eff := range receipt.Cancel.Committed {
		if !recorded[eff.NodeID] {
			t.Fatalf("committed unrecorded effect %s", eff.NodeID)
		}
		contract, ok := byNode[eff.NodeID]
		if !ok {
			t.Fatalf("committed effect %s has no declared contract", eff.NodeID)
		}
		if want := cancelActionFor(contract.Effect); eff.Action != want {
			t.Fatalf("effect %s action = %s, want %s", eff.NodeID, eff.Action, want)
		}
		if eff.Region != contract.AtomicRegion {
			t.Fatalf("effect %s region = %q, want %q", eff.NodeID, eff.Region, contract.AtomicRegion)
		}
	}
	// The flattened unwind is the reverse commit order.
	var flat []string
	for _, group := range receipt.Cancel.Unwind {
		for _, eff := range group.Effects {
			flat = append(flat, eff.NodeID)
		}
	}
	if len(flat) != len(receipt.Cancel.Committed) {
		t.Fatalf("unwind covers %d effects, committed %d", len(flat), len(receipt.Cancel.Committed))
	}
	for i, eff := range receipt.Cancel.Committed {
		if flat[len(flat)-1-i] != eff.NodeID {
			t.Fatalf("unwind is not the reverse commit order: %v", flat)
		}
	}
	// Regions group contiguously.
	for _, group := range receipt.Cancel.Unwind {
		for _, eff := range group.Effects {
			if eff.Region != group.Region {
				t.Fatalf("group %q carries foreign effect %+v", group.Region, eff)
			}
		}
		if group.Region == "" && len(group.Effects) != 1 {
			t.Fatal("regionless group is not a singleton")
		}
	}
	// The whole record equals a fresh planner compilation over the
	// committed contracts in walk order.
	var nodes []workflow.WriteNode
	seen := map[string]bool{}
	for _, trace := range receipt.Trace {
		contract, ok := byNode[trace.NodeID]
		if !ok || seen[trace.NodeID] {
			continue
		}
		seen[trace.NodeID] = true
		contract.After = nil
		if len(nodes) > 0 {
			contract.After = []string{nodes[len(nodes)-1].NodeID}
		}
		if recorded[trace.NodeID] {
			nodes = append(nodes, contract)
		}
	}
	if len(nodes) == 0 {
		if len(receipt.Cancel.Unwind) != 0 {
			t.Fatal("empty commit set unwinds non-empty")
		}
		return
	}
	fresh, err := workflow.CompileUnwindPlan(nodes, []workflow.SafePoint{{ID: "check", ThroughNode: nodes[len(nodes)-1].NodeID}})
	if err != nil {
		t.Fatalf("fresh plan: %v", err)
	}
	want := fresh.Points[0]
	if len(want.Committed) != len(receipt.Cancel.Committed) || len(want.Unwind) != len(receipt.Cancel.Unwind) {
		t.Fatalf("receipt diverges from a fresh plan: %+v vs %+v", receipt.Cancel, want)
	}
	for i := range want.Committed {
		if want.Committed[i] != receipt.Cancel.Committed[i] {
			t.Fatalf("committed[%d] = %+v, want %+v", i, receipt.Cancel.Committed[i], want.Committed[i])
		}
	}
	for i := range want.Unwind {
		if len(want.Unwind[i].Effects) != len(receipt.Cancel.Unwind[i].Effects) || want.Unwind[i].Region != receipt.Cancel.Unwind[i].Region {
			t.Fatalf("unwind[%d] diverges from a fresh plan", i)
		}
		for j := range want.Unwind[i].Effects {
			if want.Unwind[i].Effects[j] != receipt.Cancel.Unwind[i].Effects[j] {
				t.Fatalf("unwind[%d][%d] diverges from a fresh plan", i, j)
			}
		}
	}
}

func cancelActionFor(effect workflow.EffectRecord) string {
	if effect.Reversible {
		return "REVERT"
	}
	if effect.Compensation != "" {
		return "COMPENSATE:" + effect.Compensation
	}
	return "CORRECT:" + effect.Correction
}
