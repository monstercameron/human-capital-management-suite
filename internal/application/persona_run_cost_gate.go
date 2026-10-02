package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentcost"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
)

// ErrPersonaRunDailyLimit is the refusal of a run whose agent has used today's
// limit. It is not retryable: the day has to end, or the owner has to raise the
// limit. The asker sees the "limit" card with it.
var ErrPersonaRunDailyLimit = errors.New("application: the agent reached today's limit")

// PersonaRunCostGate is the single place a persona run's spend is held to the
// owner's limits (AGENTCOST-006): Start asks it before the run is admitted, and
// the model call reports to it afterwards. The cost lives in the agent
// database through the gate and the ledger the meter holds.
type PersonaRunCostGate struct {
	Meter  agentcost.Meter
	Owners interface {
		BusinessOwner(tenant, agentID string) string
	}
	// EstimateMicros is the most one model call is expected to cost. It is
	// compared to a money limit before the call.
	EstimateMicros int64
	Now            func() time.Time
	Logger         agentLogger
}

type personaRunCostBinding struct {
	gate atomic.Pointer[PersonaRunCostGate]
}

// BindCostGate makes the worker hold every run to the gate. It is bound after
// the agent access services are composed, because the gate needs the agent
// database; a worker with no gate runs without limits, as it did before.
func (w *PersonaRunModelWorker) BindCostGate(gate *PersonaRunCostGate) {
	if w != nil {
		w.cost.gate.Store(gate)
	}
}

func personaRunCostSubject(request agentinvoke.RunRequest, estimate int64) agentcost.Subject {
	return agentcost.Subject{TenantID: request.TenantID, AgentID: request.PersonaID, AgentName: request.PersonaID, ConversationID: request.ConversationID, EstimateMicros: estimate}
}

// admit refuses a run that would pass a limit. A gate that cannot read its
// limits does not admit the run and does not claim a limit was reached.
func (g *PersonaRunCostGate) admit(request agentinvoke.RunRequest) error {
	if g == nil {
		return nil
	}
	decision := g.Meter.Admit(personaRunCostSubject(request, g.EstimateMicros))
	switch {
	case decision.Allowed:
		return nil
	case decision.Unavailable:
		return ErrPersonaRunExecutorUnavailable
	}
	return &PersonaRunFailure{Code: "DAILY_LIMIT_REACHED", kind: ErrPersonaRunDailyLimit}
}

type personaRunMeteredModel struct {
	inner interface {
		Execute(context.Context, AgentModelExecutorRequest) (AgentModelExecutorResult, error)
	}
	gate    *PersonaRunCostGate
	request agentinvoke.RunRequest
}

// Execute makes the call and then records what it cost under the agent's
// limits and in the cost ledger. A call that failed before it was billed costs
// nothing and is not counted.
func (m personaRunMeteredModel) Execute(ctx context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	result, err := m.inner.Execute(ctx, request)
	if err != nil {
		return result, err
	}
	spend := result.Result.Usage.CostMicros
	if spend < 0 {
		spend = 0
	}
	run := agentcost.Run{
		TenantID: m.request.TenantID, AgentID: m.request.PersonaID, ConversationID: m.request.ConversationID,
		RunID: strings.Join([]string{request.Task.TaskID, request.StepID}, "/"), Kind: agentcost.KindAnswer, SpendMicros: spend,
		Answered: strings.TrimSpace(result.Result.Text) != "" || len(result.Result.Structured) > 0,
	}
	run.At = m.gate.now()
	owner := ""
	if m.gate.Owners != nil {
		owner = m.gate.Owners.BusinessOwner(m.request.TenantID, m.request.PersonaID)
	}
	if finishErr := m.gate.Meter.Finish(personaRunCostSubject(m.request, 0), run, owner); finishErr != nil && m.gate.Logger != nil {
		m.gate.Logger.Error("hcmnext.agent_cost_record_failed", "tenant", m.request.TenantID, "agent", m.request.PersonaID, "error_type", fmt.Sprintf("%T", finishErr))
	}
	return result, nil
}

func (g *PersonaRunCostGate) now() time.Time {
	if g.Now != nil {
		return g.Now().UTC()
	}
	return time.Now().UTC()
}

// meter wraps the model executor of one run so its cost is recorded.
func (g *PersonaRunCostGate) meter(inner interface {
	Execute(context.Context, AgentModelExecutorRequest) (AgentModelExecutorResult, error)
}, request agentinvoke.RunRequest) personaRunMeteredModel {
	return personaRunMeteredModel{inner: inner, gate: g, request: request}
}
