package recruit

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// StartDateText is the reference scenario's fixed start date, in the same
// spirit as the transfer reference's pinned effective date: a conformance
// fixture never derives its own scenario date from the wall clock.
const StartDateText = "2026-12-01"

// Setup is a ready-to-run simulation of the Recruit/Hire/Onboard reference
// workflow: the compiled plan, the declared inputs and the wired zero-effect
// environment behind every port.
type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  simulate.Inputs
	Options simulate.Options
}

// NewSetup wires env against the compiled reference workflow.
func NewSetup(env *Environment) (*Setup, error) {
	registry, err := env.Registry()
	if err != nil {
		return nil, err
	}
	plan, err := Compile(registry)
	if err != nil {
		return nil, fmt.Errorf("recruit: reference must compile: %w", err)
	}
	start, err := values.ParseLocalDate(StartDateText)
	if err != nil {
		return nil, fmt.Errorf("recruit: start date: %w", err)
	}
	inputs := simulate.Inputs{
		Values: simulate.Bag{
			"candidate_id":       simulate.NewBranded("CandidateID", env.CandidateID),
			"offer_id":           simulate.NewBranded("OfferID", env.OfferID),
			"target_position_id": simulate.NewBranded("PositionID", env.PositionID),
			"start_date":         simulate.NewLocalDate(start),
		},
	}
	return &Setup{
		Plan:   plan,
		Env:    env,
		Inputs: inputs,
		Options: simulate.Options{
			Capabilities: registry,
			SubjectRef:   "principal:recruiting-coordinator-3",
			Decisions:    Decisions{},
			Transforms:   Transforms{},
			Reads:        Reads{Env: env},
			Approvals:    Approvals{},
			Controls: []simulate.ControlVersion{
				{Name: "hcmnext.workflow.conformance.recruit.environment", Version: "v1"},
			},
		},
	}, nil
}
