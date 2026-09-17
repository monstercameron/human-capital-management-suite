package leavereturn

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// EffectiveDateText is the reference scenario's fixed effective date: a
// conformance fixture never derives its own scenario date from the wall
// clock.
const EffectiveDateText = "2026-10-01"

// Setup is a ready-to-run simulation of the leave-and-return reference
// workflow: the compiled plan, the declared inputs and the wired
// zero-effect environment behind every port.
type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  simulate.Inputs
	Options simulate.Options
}

// Params overrides the parts of the workflow's own declared inputs a
// CONF-004 scenario needs to vary. Zero value is the golden scenario:
// legal context supplied, a sealed leave-administrator review, readiness
// READY and a requested return.
type Params struct {
	// OmitLegalContext leaves out the LegalContext snapshot the program
	// resolution node's declared requirement expects.
	OmitLegalContext bool
	// ReviewerRole overrides the workflow's reviewer_role input.
	ReviewerRole string
	// MedicalSealed overrides the workflow's medical_sealed input.
	MedicalSealed *bool
	// ManagerDetermined overrides the workflow's
	// manager_determined_eligibility input.
	ManagerDetermined *bool
	// ReadinessState overrides the environment's readiness answer.
	ReadinessState string
	// ReturnRequested overrides the workflow's return_requested input.
	ReturnRequested *bool
}

// NewSetup wires env against the compiled reference workflow with golden
// Params.
func NewSetup(env *Environment) (*Setup, error) { return newSetup(env, Params{}) }

// NewSetupWithParams wires env with an explicit override of the scenario
// parameters CONF-004's RED/GREEN cases vary.
func NewSetupWithParams(env *Environment, params Params) (*Setup, error) {
	return newSetup(env, params)
}

func boolOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func newSetup(env *Environment, params Params) (*Setup, error) {
	registry, err := env.Registry()
	if err != nil {
		return nil, err
	}
	plan, err := Compile(registry)
	if err != nil {
		return nil, fmt.Errorf("leavereturn: reference must compile: %w", err)
	}
	effective, err := values.ParseLocalDate(EffectiveDateText)
	if err != nil {
		return nil, fmt.Errorf("leavereturn: effective date: %w", err)
	}
	if params.ReadinessState != "" {
		env.ReadinessState = params.ReadinessState
	}
	reviewer := params.ReviewerRole
	if reviewer == "" {
		reviewer = env.ReviewerRole
	}
	managerDetermined := params.ManagerDetermined != nil && *params.ManagerDetermined
	if env.ReviewerIsManager {
		managerDetermined = true
	}
	inputs := simulate.Inputs{
		Values: simulate.Bag{
			"worker_id":                      simulate.NewBranded("WorkerID", "22222222-2222-4222-8222-222222222222"),
			"employment_id":                  simulate.NewBranded("EmploymentID", "EMP-9001"),
			"requester_id":                   simulate.NewBranded("WorkerID", "principal:leave-admin-7"),
			"leave_type":                     simulate.NewString("MEDICAL"),
			"effective_date":                 simulate.NewLocalDate(effective),
			"return_requested":               simulate.NewBool(boolOr(params.ReturnRequested, true)),
			"reviewer_role":                  simulate.NewString(reviewer),
			"medical_sealed":                 simulate.NewBool(boolOr(params.MedicalSealed, env.MedicalSealed)),
			"manager_determined_eligibility": simulate.NewBool(managerDetermined),
		},
	}
	if !params.OmitLegalContext {
		inputs.Context = map[string]simulate.Bag{
			"LegalContext": {
				"jurisdiction":             simulate.NewString("hypothetical-fmla"),
				"applicable_rule_versions": simulate.NewString("legal.leave.hypothetical-fmla/2026.1"),
			},
		}
	}

	return &Setup{
		Plan:   plan,
		Env:    env,
		Inputs: inputs,
		Options: simulate.Options{
			Capabilities: registry,
			SubjectRef:   "principal:leave-admin-7",
			Decisions:    Decisions{},
			Transforms:   Transforms{},
			Reads:        Reads{Env: env},
			Approvals:    Approvals{},
			Controls: []simulate.ControlVersion{
				{Name: "hcmnext.workflow.conformance.leavereturn.environment", Version: "v1"},
			},
		},
	}, nil
}
