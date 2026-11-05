package testprofile

import (
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// SimulateResponse resolves the author's picked outcome for one node into the
// capability answer the simulator serves. It is the WF-TEST-003 REFACTOR
// seam: a conformance family loads its answers from its profile instead of
// hand-written literals, and the author picks the outcome per node for
// what-if branches. An outcome the profile does not declare for the node
// fails closed with ErrResponseAbsent: the branch was never authored, so the
// run refuses rather than invent data.
func (p Profile) SimulateResponse(nodeID, capabilityID string, capabilityVersion uint32, outcome workflow.Outcome) (simulate.CapabilityResponse, error) {
	response, err := p.ResponseFor(nodeID, capabilityID, capabilityVersion, outcome)
	if err != nil {
		return simulate.CapabilityResponse{}, err
	}
	outputs := make(simulate.Bag, len(response.Fields))
	for _, field := range response.Fields {
		outputs[field.Path] = simulate.Value{Type: field.Type, Text: field.Value}
	}
	return simulate.CapabilityResponse{
		Outcome: response.Outcome,
		Outputs: outputs,
		Detail:  response.Detail,
	}, nil
}
