package managerchange

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// TestTodo_PROMO_006 is the primary Manager Change reference fixture test.
func TestTodo_PROMO_006(t *testing.T) {
	setup, err := NewSetup()
	if err != nil {
		t.Fatalf("NewSetup: %v", err)
	}
	if setup.Plan.Phase != workflow.PhaseP1A || setup.Plan.TerminalProfile != workflow.TerminalProfileSimulateOnly {
		t.Fatalf("plan phase/profile = %s/%s, want P1A/SIMULATE_ONLY", setup.Plan.Phase, setup.Plan.TerminalProfile)
	}
	if setup.Plan.Effects.ZeroEffect == false {
		t.Fatal("Manager Change plan is not zero-effect")
	}
	if got := setup.Plan.Digest(); got != "5a41efd841bb457f48b5c28de1d2c1f446ea20edd88db230ad702b2e394245e3" {
		t.Fatalf("plan digest = %s, want checked-in golden", got)
	}
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("simulate.Run: %v", err)
	}
	if receipt.Mode != workflow.ModeSimulate {
		t.Fatalf("mode = %s, want %s", receipt.Mode, workflow.ModeSimulate)
	}
	counters := receipt.EffectCounters()
	if counters.DomainWrites != 0 || counters.Reservations != 0 || counters.WorkItems != 0 || counters.Timers != 0 || counters.Messages != 0 || counters.OutboxEntries != 0 || counters.ProviderCalls != 0 || counters.ApprovalBindings != 0 {
		t.Fatalf("effect counters = %+v, want zero", counters)
	}
	if err := receipt.ZeroEffect.Validate(); err != nil {
		t.Fatalf("zero-effect receipt: %v", err)
	}
	contract, err := SimulationContract()
	if err != nil {
		t.Fatalf("SimulationContract: %v", err)
	}
	if len(contract.Writes) != 1 || len(contract.SideEffects) != 1 {
		t.Fatalf("contract shape writes/effects = %d/%d, want 1/1", len(contract.Writes), len(contract.SideEffects))
	}
	if contract.SideEffects[0].Status() != simcontract.EffectSimulatedNotExecuted {
		t.Fatalf("side effect status = %s, want %s", contract.SideEffects[0].Status(), simcontract.EffectSimulatedNotExecuted)
	}
}

// TestTodo_PROMO_006_Conformance proves shared PROMO-004 shape, single-domain
// boundaries, P1A refusal of added effects, and absence of EXECUTE activation.
func TestTodo_PROMO_006_Conformance(t *testing.T) {
	contract, err := SimulationContract()
	if err != nil {
		t.Fatalf("SimulationContract: %v", err)
	}
	if reflect.TypeOf(contract) != reflect.TypeOf(simcontract.SimulationResult{}) {
		t.Fatal("Manager Change does not use the shared WorkflowSimulationContract type")
	}
	if len(simcontract.Sections()) != 12 || contract.Reads == nil || contract.Writes == nil || contract.Streams == nil || contract.Conflicts == nil || contract.Approvals == nil || contract.Authority == nil || contract.LegalObligations == nil || contract.SideEffects == nil || contract.Repair == nil {
		t.Fatal("Manager Change contract does not state all shared contract sections")
	}
	if err := contract.Validate(); err != nil {
		t.Fatalf("shared contract validation: %v", err)
	}
	if strings.Contains(contract.SideEffects[0].EffectID, "compensation") || strings.Contains(contract.SideEffects[0].EffectID, "payroll") || strings.Contains(contract.SideEffects[0].EffectID, "access") {
		t.Fatalf("non-HRIS effect leaked into Manager Change: %s", contract.SideEffects[0].EffectID)
	}

	in, err := ContractInput()
	if err != nil {
		t.Fatalf("ContractInput: %v", err)
	}
	in.SideEffects = append(in.SideEffects, in.SideEffects[0])
	if _, err := CompileSimulationContract(in); err == nil {
		t.Fatal("adding a second effect set compiled successfully")
	}
	in, err = ContractInput()
	if err != nil {
		t.Fatalf("ContractInput: %v", err)
	}
	in.Writes = append(in.Writes, in.Writes[0])
	if _, err := CompileSimulationContract(in); err == nil {
		t.Fatal("adding a second write set compiled successfully")
	}

	def := ReferenceDefinition()
	def.Nodes[3].DeclaredEffect = capability.EffectExternalMutation
	if _, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: mustRegistry(t)}); err == nil {
		t.Fatal("adding an effect to the simulation transform compiled successfully")
	}
	assertNotActivatedForExecute(t)
}

func mustRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	r, err := NewEnvironment().Registry()
	if err != nil {
		t.Fatalf("Registry: %v", err)
	}
	return r
}

func assertNotActivatedForExecute(t *testing.T) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate managerchange test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	for _, rel := range []string{"internal/platform/execution", "internal/application"} {
		err := filepath.WalkDir(filepath.Join(root, rel), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := strings.ToLower(string(body))
			managerChangeRef := strings.Contains(text, "managerchange") || strings.Contains(text, "manager_change") || strings.Contains(text, "change_manager")
			if managerChangeRef && strings.Contains(text, "version.activate") {
				return &activationScanError{path: path}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("EXECUTE activation scan: %v", err)
		}
	}
}

type activationScanError struct{ path string }

func (e *activationScanError) Error() string {
	return "Manager Change is activated for EXECUTE in " + e.path
}
