package managerchange

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/testprofile"
)

// conformanceProfile is the fake-data profile this family loads its
// capability answers from (WF-TEST-003 REFACTOR).
var conformanceProfile = testprofile.ProfileRef{ID: "harborcare-conformance", Version: 1}

// Environment is the fixed, in-memory read projection used by the simulation.
// It has no database, clock, network or HRIS write port. Capability answers
// come from the checked-in conformance profile, never from hand-written
// literals; only input echoes stay explicit in the handlers.
type Environment struct {
	// Outcomes picks the profile sample outcome per node for what-if
	// branches. A node without a pick serves SUCCEEDED.
	Outcomes map[string]workflow.Outcome

	profile testprofile.Profile
	loaded  bool
}

func NewEnvironment() *Environment { return &Environment{Outcomes: map[string]workflow.Outcome{}} }

// loadProfile resolves the family's profile lazily so NewEnvironment keeps
// its signature; a missing profile fails closed at first use.
func (e *Environment) loadProfile() error {
	if e.loaded {
		return nil
	}
	registry, err := testprofile.DefaultRegistry()
	if err != nil {
		return fmt.Errorf("managerchange: load profiles: %w", err)
	}
	p, err := registry.Resolve(conformanceProfile)
	if err != nil {
		return fmt.Errorf("managerchange: resolve %s: %w", conformanceProfile.ID, err)
	}
	e.profile = p
	e.loaded = true
	return nil
}

func (e *Environment) picked(nodeID string) workflow.Outcome {
	if outcome, ok := e.Outcomes[nodeID]; ok {
		return outcome
	}
	return workflow.OutcomeSucceeded
}

// Registry publishes only read-only fixture capabilities.
func (e *Environment) Registry() (*capability.Registry, error) {
	r := capability.NewRegistry()
	if err := r.Register(capabilityDefinition(CapabilityWorker, "people"), e.readWorker); err != nil {
		return nil, err
	}
	if err := r.Register(capabilityDefinition(CapabilityManager, "people"), e.readManager); err != nil {
		return nil, err
	}
	return r, nil
}

func capabilityRequest(payload any) (simulate.CapabilityRequest, error) {
	req, ok := payload.(simulate.CapabilityRequest)
	if !ok {
		return simulate.CapabilityRequest{}, fmt.Errorf("managerchange: capability received %T", payload)
	}
	return req, nil
}

func (e *Environment) readWorker(_ context.Context, payload any) (any, error) {
	req, err := capabilityRequest(payload)
	if err != nil {
		return nil, err
	}
	worker, err := req.Inputs.Get("worker_id")
	if err != nil {
		return nil, err
	}
	response, err := e.profileResponse(NodeReadWorker, req)
	if err != nil {
		return nil, err
	}
	// The subject echo is input-derived, not fake data: it stays explicit.
	response.Outputs["worker_id"] = worker
	return response, nil
}

func (e *Environment) readManager(_ context.Context, payload any) (any, error) {
	req, err := capabilityRequest(payload)
	if err != nil {
		return nil, err
	}
	manager, err := req.Inputs.Get("proposed_manager_id")
	if err != nil {
		return nil, err
	}
	response, err := e.profileResponse(NodeReadManager, req)
	if err != nil {
		return nil, err
	}
	// The subject echo is input-derived, not fake data: it stays explicit.
	response.Outputs["proposed_manager_id"] = manager
	return response, nil
}

// profileResponse serves one capability answer from the family's profile for
// the author's picked outcome.
func (e *Environment) profileResponse(nodeID string, req simulate.CapabilityRequest) (simulate.CapabilityResponse, error) {
	if err := e.loadProfile(); err != nil {
		return simulate.CapabilityResponse{}, err
	}
	response, err := e.profile.SimulateResponse(nodeID, req.Capability.ID, req.Capability.Version, e.picked(nodeID))
	if err != nil {
		return simulate.CapabilityResponse{}, fmt.Errorf("managerchange: node %s: %w", nodeID, err)
	}
	return response, nil
}

// Transforms implements the pure validation and proposal simulation ports.
type Transforms struct{}

func (Transforms) Transform(_ context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	switch req.Transform.TransformRef {
	case TransformValidate:
		worker, err := req.Inputs.Text("worker_id")
		if err != nil {
			return simulate.TransformResult{}, err
		}
		current, err := req.Inputs.Text("current_manager_id")
		if err != nil {
			return simulate.TransformResult{}, err
		}
		proposed, err := req.Inputs.Text("proposed_manager_id")
		if err != nil {
			return simulate.TransformResult{}, err
		}
		active, err := req.Inputs.Get("worker_active")
		if err != nil {
			return simulate.TransformResult{}, err
		}
		workerActive, err := active.Bool()
		if err != nil {
			return simulate.TransformResult{}, err
		}
		proposedActive, err := req.Inputs.Get("proposed_manager_active")
		if err != nil {
			return simulate.TransformResult{}, err
		}
		managerActive, err := proposedActive.Bool()
		if err != nil {
			return simulate.TransformResult{}, err
		}
		eligible, err := req.Inputs.Get("management_eligible")
		if err != nil {
			return simulate.TransformResult{}, err
		}
		managerEligible, err := eligible.Bool()
		if err != nil {
			return simulate.TransformResult{}, err
		}
		if worker == proposed || !workerActive || !managerActive || !managerEligible || current == proposed {
			return simulate.TransformResult{Outcome: workflow.OutcomeFailed, Outputs: simulate.Bag{"validation_status": simulate.NewString("REJECTED")}, Detail: "manager relationship validation rejected"}, nil
		}
		return simulate.TransformResult{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"validation_status": simulate.NewString("VALID")}}, nil
	case TransformSimulate:
		worker, err := req.Inputs.Text("worker_id")
		if err != nil {
			return simulate.TransformResult{}, err
		}
		current, err := req.Inputs.Text("current_manager_id")
		if err != nil {
			return simulate.TransformResult{}, err
		}
		proposed, err := req.Inputs.Text("proposed_manager_id")
		if err != nil {
			return simulate.TransformResult{}, err
		}
		date, err := req.Inputs.Text("effective_date")
		if err != nil {
			return simulate.TransformResult{}, err
		}
		digest := sha256.Sum256([]byte(worker + "\x00" + current + "\x00" + proposed + "\x00" + date))
		return simulate.TransformResult{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{
			"proposal_digest": simulate.NewString("sha256:" + hex.EncodeToString(digest[:])),
			"write_boundary":  simulate.NewString(managerWriteBinding),
		}}, nil
	default:
		return simulate.TransformResult{}, fmt.Errorf("managerchange: unknown transform %q", req.Transform.TransformRef)
	}
}

// Setup is a deterministic, ready-to-run Manager Change simulation.
type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Inputs  simulate.Inputs
	Options simulate.Options
}

func NewSetup() (*Setup, error) {
	env := NewEnvironment()
	registry, err := env.Registry()
	if err != nil {
		return nil, err
	}
	plan, err := Compile(registry)
	if err != nil {
		return nil, fmt.Errorf("managerchange: compile reference: %w", err)
	}
	effective, err := values.ParseLocalDate("2026-09-01")
	if err != nil {
		return nil, err
	}
	return &Setup{
		Plan: plan,
		Inputs: simulate.Inputs{Values: simulate.Bag{
			"worker_id":           simulate.NewBranded("WorkerID", "worker-jane"),
			"current_manager_id":  simulate.NewBranded("WorkerID", "manager-alice"),
			"proposed_manager_id": simulate.NewBranded("WorkerID", "manager-bob"),
			"effective_date":      simulate.NewLocalDate(effective),
		}},
		Options: simulate.Options{
			Capabilities: registry,
			Transforms:   Transforms{},
			SubjectRef:   "principal:hr-partner-9",
			Controls:     []simulate.ControlVersion{{Name: "hcmnext.workflow.conformance.managerchange.environment", Version: "v1"}},
		},
	}, nil
}

// ContractInput is the one-write Manager Change simulation contract. The
// write and side effect are planned facts, never an operation performed here.
func ContractInput() (simcontract.AssembleInput, error) {
	worker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		return simcontract.AssembleInput{}, err
	}
	relationship, err := values.NewResourceKey(fixtures.Tenant, values.Kind("manager_relationship"), worker.Id)
	if err != nil {
		return simcontract.AssembleInput{}, err
	}
	revision, err := values.NewSequenceRevision("org.relationship."+worker.Id, 4)
	if err != nil {
		return simcontract.AssembleInput{}, err
	}
	effectID := "org.manager_relationship.change@2026-09-01#omar-reyes-001"
	effect, err := simcontract.NewSideEffect(effectID, string(simassign.EffectManagerRelationship), "org.manager_relationship", "org.manager_relationship/"+worker.Id, string(simassign.Reversible), "org.manager_relationship.supersede", "observe.org.manager_chain_current")
	if err != nil {
		return simcontract.AssembleInput{}, err
	}
	return simcontract.AssembleInput{
		Intent:                  simcontract.IntentRef{IntentID: "intent_manager_change_omar_reyes_001", IntentType: "hcmnext.people.change_manager", IntentVersion: "v1"},
		Snapshot:                simcontract.SnapshotRef{SnapshotDigest: "sha256:fixture-manager-change-input-snapshot-omar-reyes-2026-09-01", Tenant: fixtures.Tenant, Subject: worker},
		ProposalCandidateDigest: "sha256:fixture-manager-change-candidate-omar-reyes-2026-09-01",
		Reads:                   []intent.PlannedRead{{ResourceKey: relationship, ExpectedRevision: revision}},
		Writes: []intent.PlannedWrite{{
			Subject: intent.SubjectReference{Kind: "worker", SubjectID: worker.Id, AuthorityDomain: "org"}, ResourceKey: relationship,
			FieldPath: "org.manager_relationship.manager_id", CurrentCanonicalText: "44444444-4444-4444-8444-444444444444", ProposedCanonicalText: "77777777-7777-4777-8777-777777777777",
			SourceAuthorityDecision: "ORG_MANAGER_RELATIONSHIP_WRITE_ALLOWED", ExpectedRevision: revision,
		}},
		Streams:   []intent.PlanParticipant{{ParticipantID: "org.manager_relationship", StreamID: "org.relationship." + worker.Id, StorageClass: "LOCAL_EVENT_STREAM", Local: true}},
		Conflicts: []conflict.Candidate{}, Approvals: []decision.ApprovalRequirement{}, LegalObligations: []decision.Obligation{},
		Authority:   []evidence.SourceAuthority{{Kind: evidence.AuthorityLocal, System: "hcmnext.org", PolicyRef: "org.manager_relationship.write/2026.1"}},
		SideEffects: []simcontract.SideEffect{effect}, Repair: []intent.CompensationBinding{{EffectID: effectID, Strategy: "SUPERSEDING_REVISION", RepairPlanID: "org.manager_relationship.supersede"}},
		Cost: simcontract.Cost{State: simcontract.CostNone}, Completion: simcontract.Completion{State: simcontract.CompletionReady, Detail: "one manager relationship is the only planned HRIS write boundary"},
		Revalidation: simcontract.Revalidation{Rules: []string{"people.worker.active/v1", "people.manager.eligible/v1", "org.manager_relationship.no_cycle/v1", "org.manager_relationship.conflict_watermark/v1"}, ControlSnapshotDigest: "sha256:fixture-manager-change-control-snapshot-2026-08-01"},
	}, nil
}

// CompileSimulationContract enforces the fixture's single-boundary shape
// before delegating to the shared PROMO-004 contract constructor.
func CompileSimulationContract(in simcontract.AssembleInput) (simcontract.SimulationResult, error) {
	if len(in.Writes) != 1 {
		return simcontract.SimulationResult{}, fmt.Errorf("managerchange: exactly one manager relationship write set is required, got %d", len(in.Writes))
	}
	if len(in.SideEffects) != 1 {
		return simcontract.SimulationResult{}, fmt.Errorf("managerchange: exactly one manager relationship effect is required, got %d", len(in.SideEffects))
	}
	return simcontract.Assemble(in)
}

// SimulationContract assembles the immutable shared contract shape.
func SimulationContract() (simcontract.SimulationResult, error) {
	in, err := ContractInput()
	if err != nil {
		return simcontract.SimulationResult{}, err
	}
	return CompileSimulationContract(in)
}
