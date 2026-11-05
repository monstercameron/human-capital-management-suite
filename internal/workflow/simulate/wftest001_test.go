package simulate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// recordingContract returns the zero-effect-ceiling test contract WF-TEST-001
// records under: SIMULATE in TEST never commits and never sends.
func recordingContract(t *testing.T) intent.ModeContract {
	t.Helper()
	contract, err := intent.ModeContractFor(intent.ModeSimulate, intent.EnvironmentTest)
	if err != nil {
		t.Fatalf("ModeContractFor: %v", err)
	}
	if !contract.ZeroEffect() {
		t.Fatalf("test contract is not zero-effect: %+v", contract)
	}
	return contract
}

// recordingRegistry publishes the fixture capabilities with a read handler
// that returns the mapped values and a mutating handler that fails the test
// if the run ever reaches a live write path.
func recordingRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	defs := []capability.Definition{
		fixtureCapability(capReadWorker, "people", capability.EffectReadOnly),
		fixtureCapability(capSyncPayroll, "payroll", capability.EffectExternalMutation),
		fixtureCapability(capObservePayroll, "payroll", capability.EffectReadOnly),
	}
	for _, def := range defs {
		def := def
		handler := func(_ context.Context, _ any) (any, error) {
			return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{}}, nil
		}
		switch def.ID {
		case capReadWorker:
			handler = func(_ context.Context, _ any) (any, error) {
				return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{
					"worker_id":  simulate.NewBranded("WorkerID", "11111111-1111-4111-8111-111111111111"),
					"effect_key": simulate.NewString("effect-1"),
				}}, nil
			}
		case capSyncPayroll:
			handler = func(_ context.Context, _ any) (any, error) {
				t.Error("recording run reached the mutating handler; writes must be recorded, never invoked")
				return simulate.CapabilityResponse{}, errors.New("live write path reached")
			}
		}
		if err := r.Register(def, handler); err != nil {
			t.Fatalf("publish fixture capability %s: %v", def.Key(), err)
		}
	}
	return r
}

type recordingReads struct{}

func (recordingReads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	// UNKNOWN routes to the repair terminal: a simulation never commits, so
	// a recording run parks at REPAIR_REQUIRED with its captured records
	// rather than minting a COMMITTED execution it did not perform.
	return simulate.Observation{
		Outcome:   workflow.OutcomeUnknown,
		Outputs:   simulate.Bag{"observed_status": simulate.NewString("UNKNOWN"), "source_watermark": simulate.NewString("payroll.provider.acme.cursor@1")},
		Watermark: "payroll.provider.acme.cursor@1",
		Detail:    "observed " + req.Observe.SourceAuthority + ": UNKNOWN (nothing committed)",
	}, nil
}

func recordingInputs() simulate.Inputs {
	return simulate.Inputs{
		Values: simulate.Bag{"worker_id": simulate.NewBranded("WorkerID", "11111111-1111-4111-8111-111111111111")},
	}
}

// TestTodo_WF_TEST_001 proves the recording profile: a plan with a write
// node runs under the zero-effect-ceiling test contract, the would-be
// external call is captured as a typed record with its preview, the handler
// is never invoked, effect counters stay zero, and the receipt verifies.
func TestTodo_WF_TEST_001(t *testing.T) {
	invoked := false
	plan := mustCompileMutatingPlan(t, func() { invoked = true })
	contract := recordingContract(t)
	recording := simulate.NewRecording()
	profile := simulate.RecordingProfile{Contract: contract, Recorder: recording}

	if err := simulate.AdmitWithRecording(plan, profile); err != nil {
		t.Fatalf("AdmitWithRecording: %v", err)
	}
	receipt, err := simulate.Run(context.Background(), plan, recordingInputs(), simulate.Options{
		Capabilities: recordingRegistry(t),
		Reads:        recordingReads{},
		Recording:    &profile,
	})
	if err != nil {
		t.Fatalf("Run with recording: %v", err)
	}
	if invoked {
		t.Fatal("the mutating capability handler was reached; recording must not invoke")
	}
	if receipt.Terminal.NodeID != fxEndRepair {
		t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, fxEndRepair, receipt.NodeIDs())
	}
	effects := recording.Effects()
	if len(effects) != 1 {
		t.Fatalf("recorded %d effects, want 1: %+v", len(effects), effects)
	}
	record := effects[0]
	if record.NodeID != fxSync || record.Capability.ID != capSyncPayroll || record.Kind != simulate.RecordedExternalCall {
		t.Fatalf("record = %+v, want the sync_payroll external call", record)
	}
	if record.EffectClass != capability.EffectExternalMutation || record.EffectBinding != "payroll.worker_sync" {
		t.Fatalf("record class/binding = %q/%q, want the declared mutation", record.EffectClass, record.EffectBinding)
	}
	effectKey, err := record.Inputs.Text("effect_key")
	if err != nil || effectKey != "effect-1" {
		t.Fatalf("recorded effect_key = %q (err %v), want the mapped input", effectKey, err)
	}
	if record.Preview == "" || record.InputDigest == "" {
		t.Fatalf("record has no preview or input digest: %+v", record)
	}
	if len(record.Outputs) != 1 || record.Outputs[0].Path != "submission_id" {
		t.Fatalf("recorded declared outputs = %+v, want [submission_id]", record.Outputs)
	}
	if len(receipt.RecordedEffects) != 1 || receipt.RecordedEffects[0].NodeID != fxSync {
		t.Fatalf("receipt carries %+v, want the recorded sync", receipt.RecordedEffects)
	}
	if counters := receipt.EffectCounters(); !counters.IsZero() {
		t.Fatalf("recording run counted effects: %+v", counters)
	}
	if err := receipt.Verify(); err != nil {
		t.Fatalf("receipt does not verify: %v", err)
	}

	secondRecording := simulate.NewRecording()
	secondProfile := simulate.RecordingProfile{Contract: contract, Recorder: secondRecording}
	second, err := simulate.Run(context.Background(), plan, recordingInputs(), simulate.Options{
		Capabilities: recordingRegistry(t),
		Reads:        recordingReads{},
		Recording:    &secondProfile,
	})
	if err != nil {
		t.Fatalf("second recording run: %v", err)
	}
	if receipt.Digest() != second.Digest() {
		t.Fatal("recording receipt digest drifted between runs")
	}
}

// TestTodo_WF_TEST_001_Security proves a test run cannot reach a live write
// path: recording demands the zero-effect ceiling and a recorder, and the
// bare interpreter still refuses write plans outright.
func TestTodo_WF_TEST_001_Security(t *testing.T) {
	invoked := false
	plan := mustCompileMutatingPlan(t, func() { invoked = true })

	if err := simulate.Admit(plan); err == nil {
		t.Fatal("Admit accepted a write plan without a recording profile")
	}
	if _, err := simulate.Run(context.Background(), plan, recordingInputs(), simulate.Options{
		Capabilities: recordingRegistry(t),
		Reads:        recordingReads{},
	}); err == nil {
		t.Fatal("Run executed a write plan without a recording profile")
	}

	executing, err := intent.ModeContractFor(intent.ModeExecute, intent.EnvironmentProduction)
	if err != nil {
		t.Fatalf("ModeContractFor: %v", err)
	}
	if err := simulate.AdmitWithRecording(plan, simulate.RecordingProfile{Contract: executing, Recorder: simulate.NewRecording()}); err == nil {
		t.Fatal("recording admitted a write plan under a committing contract")
	}
	contract := recordingContract(t)
	if err := simulate.AdmitWithRecording(plan, simulate.RecordingProfile{Contract: contract}); err == nil {
		t.Fatal("recording admitted a write plan with no recorder to capture it")
	}
	if invoked {
		t.Fatal("a refused run reached the mutating handler")
	}
}

// TestTodo_WF_TEST_001_Property proves the effect sinks are unchanged after
// any recording run: across every write class and repeated runs, no handler
// behind a ledger, outbox or connector double is ever invoked, while every
// would-be effect is captured.
func TestTodo_WF_TEST_001_Property(t *testing.T) {
	for _, class := range []capability.EffectClass{
		capability.EffectInternalMutation,
		capability.EffectExternalMutation,
		capability.EffectIrreversibleExternalMutation,
	} {
		class := class
		t.Run(string(class), func(t *testing.T) {
			var calls int
			def := propertyDefinition(class)
			plan, err := workflow.Compile(def, workflow.Options{
				Phase:        workflow.PhaseP1B,
				Capabilities: propertyRegistry(t, class, &calls),
			})
			if err != nil {
				t.Fatalf("compile %s plan: %v", class, err)
			}
			contract := recordingContract(t)
			for i := 0; i < 3; i++ {
				recording := simulate.NewRecording()
				profile := simulate.RecordingProfile{Contract: contract, Recorder: recording}
				receipt, err := simulate.Run(context.Background(), plan, recordingInputs(), simulate.Options{
					Capabilities: propertyRegistry(t, class, &calls),
					Reads:        recordingReads{},
					Recording:    &profile,
				})
				if err != nil {
					t.Fatalf("recording run %d: %v", i, err)
				}
				if len(recording.Effects()) != 1 {
					t.Fatalf("run %d recorded %d effects, want 1", i, len(recording.Effects()))
				}
				if counters := receipt.EffectCounters(); !counters.IsZero() {
					t.Fatalf("run %d counted effects: %+v", i, counters)
				}
			}
			if calls != 0 {
				t.Fatalf("%s handler invoked %d times across recording runs; sinks must be unchanged", class, calls)
			}
		})
	}
}

// propertyDefinition mirrors mutatingDefinition with the sync node shaped for
// the given write class: an internal mutation commits as its own
// authoritative core, and an irreversible mutation carries no automatic
// retry. The recorded node is the same sync in every case.
func propertyDefinition(class capability.EffectClass) workflow.Definition {
	def := mutatingDefinition()
	for i := range def.Nodes {
		if def.Nodes[i].ID != fxSync {
			continue
		}
		switch class {
		case capability.EffectInternalMutation:
			def.Nodes[i].EffectRole = workflow.RoleAuthoritativeCore
		case capability.EffectIrreversibleExternalMutation:
			def.Nodes[i].Retry = nil
		}
	}
	return def
}

// propertyRegistry mirrors mutatingRegistry with the sync capability bound at
// the given write class and every handler counted.
func propertyRegistry(t *testing.T, class capability.EffectClass, calls *int) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	for _, def := range []capability.Definition{
		fixtureCapability(capReadWorker, "people", capability.EffectReadOnly),
		fixtureCapability(capSyncPayroll, "payroll", class),
		fixtureCapability(capObservePayroll, "payroll", capability.EffectReadOnly),
	} {
		def := def
		handler := func(_ context.Context, _ any) (any, error) {
			return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{
				"worker_id":  simulate.NewBranded("WorkerID", "11111111-1111-4111-8111-111111111111"),
				"effect_key": simulate.NewString("effect-1"),
			}}, nil
		}
		if def.EffectClass.IsWrite() {
			handler = func(_ context.Context, _ any) (any, error) {
				*calls++
				return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{}}, nil
			}
		}
		if err := r.Register(def, handler); err != nil {
			t.Fatalf("publish fixture capability %s: %v", def.Key(), err)
		}
	}
	return r
}
