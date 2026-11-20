package application

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

type issue3807Allowance struct {
	pct             int
	known           bool
	tenant, persona string
	conversation    string
	asked           int
}

func (a *issue3807Allowance) AllowanceRemaining(tenant, persona, conversation string) (int, bool) {
	a.asked++
	a.tenant, a.persona, a.conversation = tenant, persona, conversation
	return a.pct, a.known
}

func issue3807Line(run, agent string) agentmodel.CallLine {
	return agentmodel.CallLine{TenantID: "tenant-a", AgentID: agent, RunID: run, CallSite: "answer", Stage: agentmodel.CallStageAnswer, Kind: agentmodel.CallKindGeneration, Basis: agentmodel.CostMeasured, StartedAt: time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC), CostMicros: 5_000}
}

func TestIssue3807_ProgressCarriesOwnUsedUnitsAndAllowance(t *testing.T) {
	s, ctx, _, invocations, execution := personaSurfaceFixture(t)
	agent := invocations.rows[0].PersonaID
	execution.run.AgentID = agent
	execution.run.Calls = []agentmodel.CallLine{issue3807Line(execution.run.ID, agent), issue3807Line(execution.run.ID, agent)}
	allowance := &issue3807Allowance{pct: 88, known: true}
	s.Allowance = allowance
	progress, err := s.Progress(ctx, "channel-a")
	if err != nil || len(progress.Invocations) != 1 {
		t.Fatalf("progress=%+v %v", progress, err)
	}
	row := progress.Invocations[0]
	if row.UsedUnits != 2 || row.AllowanceRemainingPct == nil || *row.AllowanceRemainingPct != 88 {
		t.Fatalf("used=%d allowance=%v", row.UsedUnits, row.AllowanceRemainingPct)
	}
	if allowance.tenant != "tenant-a" || allowance.persona != agent || allowance.conversation != "channel-a" {
		t.Fatalf("allowance asked for %+v", allowance)
	}
}

func TestIssue3807_UnknownAllowanceAndForeignLinesAreLeftOut(t *testing.T) {
	s, ctx, _, invocations, execution := personaSurfaceFixture(t)
	agent := invocations.rows[0].PersonaID
	execution.run.AgentID = agent
	// A line sealed under another run is not this run's spend: nothing is shown.
	execution.run.Calls = []agentmodel.CallLine{issue3807Line("someone-elses-run", agent)}
	s.Allowance = &issue3807Allowance{known: false}
	progress, err := s.Progress(ctx, "channel-a")
	if err != nil {
		t.Fatal(err)
	}
	row := progress.Invocations[0]
	if row.UsedUnits != 0 || row.AllowanceRemainingPct != nil {
		t.Fatalf("foreign spend or unknown allowance shown: %+v", row)
	}
}

func TestIssue3807_NoAllowanceSourceStillAnswers(t *testing.T) {
	s, ctx, _, _, _ := personaSurfaceFixture(t)
	progress, err := s.Progress(ctx, "channel-a")
	if err != nil || progress.Invocations[0].AllowanceRemainingPct != nil || progress.Invocations[0].UsedUnits != 0 {
		t.Fatalf("progress=%+v %v", progress, err)
	}
}
