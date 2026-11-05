package agentcapability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/triage"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/capabilityrunner"
)

// TriageFixture is the REFACTOR clause of WF-CTRL-005: the triage conformance
// fixture - the only agent capability the engine had run, limited to read,
// analyse and draft - becomes one registered agent capability among others.
// It serves through the same handler, ledger and outcome mapping as every
// published agent.
func TriageFixture(proposer triage.Proposer, telemetry *triage.TelemetrySink, ledger RunLedger) (PublishedAgent, Deps) {
	agent := PublishedAgent{
		AgentID:   "conformance-triage",
		Version:   1,
		Status:    AgentStatusActive,
		Evaluated: true,
		InputSchema: capability.SchemaRef{
			SchemaID: "agent.conformance-triage.input", Version: 1,
			ProtobufFullName: "hcmnext.agents.conformancetriage.v1.Input",
		},
		OutputSchema: capability.SchemaRef{
			SchemaID: "agent.conformance-triage.output", Version: 1,
			ProtobufFullName: "hcmnext.agents.conformancetriage.v1.Output",
		},
		DataScope: capability.DataDomainFieldSet{
			DataDomains: []string{"hr.cases"},
			FieldPaths:  []string{"hr.cases.subject", "hr.cases.messages"},
		},
		Tools:            []string{"cases.read", "taxonomy.cite"},
		ServicePrincipal: "service:agent/conformance-triage",
		OutputFields: []workflow.Field{
			{Path: "route", Type: workflow.ValueType{Kind: workflow.KindString}},
			{Path: "verdict_digest", Type: workflow.ValueType{Kind: workflow.KindString}},
		},
	}
	return agent, Deps{
		Runner: NewTriageRunner(proposer, telemetry),
		Ledger: ledger,
	}
}

// TriageRunner adapts triage.Triage to the AgentRunner port. The fixture is
// synchronous and deterministic, so Attach re-derives the recorded run from
// its inputs.
type TriageRunner struct {
	proposer  triage.Proposer
	telemetry *triage.TelemetrySink

	mu       sync.Mutex
	recorded map[string]map[string]string
}

// NewTriageRunner builds the fixture runner. A nil proposer exercises the
// deterministic no-proposer fallback, exactly as the serving contract does.
func NewTriageRunner(proposer triage.Proposer, telemetry *triage.TelemetrySink) *TriageRunner {
	return &TriageRunner{proposer: proposer, telemetry: telemetry, recorded: map[string]map[string]string{}}
}

// Start runs the guarded triage pipeline over the node's mapped inputs.
func (r *TriageRunner) Start(_ context.Context, req AgentRunRequest) (AgentRunResult, error) {
	inputs := canonicalInputs(req.Inputs)
	runID := "triage:" + digestInputs(inputs)
	r.mu.Lock()
	r.recorded[runID] = inputs
	r.mu.Unlock()
	return r.run(runID, req.NodeID, inputs)
}

// Attach re-derives a recorded run. An unrecorded run id fails closed: the
// fixture never invents a verdict it did not compute.
func (r *TriageRunner) Attach(_ context.Context, runID string) (AgentRunResult, error) {
	r.mu.Lock()
	inputs, ok := r.recorded[runID]
	r.mu.Unlock()
	if !ok {
		return AgentRunResult{}, fmt.Errorf("agentcapability: triage run %s was not recorded", runID)
	}
	return r.run(runID, "", inputs)
}

func (r *TriageRunner) run(runID, nodeID string, inputs map[string]string) (AgentRunResult, error) {
	caseID := inputs["case_id"]
	if caseID == "" {
		caseID = nodeID
	}
	if caseID == "" {
		caseID = runID
	}
	request := triage.CaseRequest{
		ID:       caseID,
		Subject:  inputs["subject"],
		Taxonomy: inputs["taxonomy"],
		Messages: []triage.Message{{Role: "user", Text: inputs["message"]}},
	}
	verdict, err := triage.Triage(request, r.proposer, r.telemetry)
	if err != nil {
		return AgentRunResult{}, fmt.Errorf("agentcapability: triage fixture: %w", err)
	}
	str := workflow.ValueType{Kind: workflow.KindString}
	return AgentRunResult{
		RunID:   runID,
		Verdict: triageVerdict(verdict.Route),
		Outputs: map[string]TypedText{
			"route":          {Type: str, Text: verdict.Route},
			"verdict_digest": {Type: str, Text: verdict.Digest},
		},
		Reads: []string{"case:" + caseID},
		Cost:  "fixture:no-charge",
	}, nil
}

// triageVerdict maps the fixture's closed routing vocabulary to agent
// verdicts: the governed auto path succeeds, a specialist routing refuses the
// auto path, and hold or manual fallbacks are unknown to the auto path.
func triageVerdict(route string) AgentVerdict {
	switch route {
	case triage.RouteGovernedCreate:
		return VerdictSucceeded
	case triage.RouteSpecialist:
		return VerdictRejected
	default:
		return VerdictUnknown
	}
}

func canonicalInputs(inputs map[string]capabilityrunner.ResolvedValue) map[string]string {
	out := make(map[string]string, len(inputs))
	for path, v := range inputs {
		out[path] = v.Text
	}
	return out
}

func digestInputs(inputs map[string]string) string {
	paths := make([]string, 0, len(inputs))
	for path := range inputs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		h.Write([]byte(path))
		h.Write([]byte{0})
		h.Write([]byte(inputs[path]))
		h.Write([]byte{0})
	}
	sum := hex.EncodeToString(h.Sum(nil))
	return sum[:16]
}
