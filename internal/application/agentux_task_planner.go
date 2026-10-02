package application

import (
	"context"
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
)

// AgentTaskPlanningOutput is the planner's declaration of the data a request
// needs. The plan below is assembled from it, so a request that needs no data
// gets no read step.
type AgentTaskPlanningOutput struct {
	NeedsOwnRecord bool `json:"needs_own_record"`
}

// AgentTaskPlanner declares the minimum data needed by a task before any
// capability is called.
type AgentTaskPlanner interface {
	PlanAgentTask(context.Context, string, agentclient.StartMode) (AgentTaskPlanningOutput, error)
}

type deterministicAgentTaskPlanner struct{}

func (deterministicAgentTaskPlanner) PlanAgentTask(_ context.Context, prompt string, _ agentclient.StartMode) (AgentTaskPlanningOutput, error) {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, prompt)
	text := " " + strings.Join(strings.Fields(normalized), " ") + " "
	for _, phrase := range []string{
		" my record ", " my profile ", " my job ", " my title ", " my position ",
		" my department ", " my org ", " my location ", " my worker number ",
		" my employment ", " where do i work ", " who is my ",
	} {
		if strings.Contains(text, phrase) {
			return AgentTaskPlanningOutput{NeedsOwnRecord: true}, nil
		}
	}
	return AgentTaskPlanningOutput{}, nil
}

// agentTaskPlanSteps builds the reviewed plan from the declared data needs:
// the answer step always, and the read of the user's own worker record only
// when the request needs it. A general question therefore causes no personnel
// record read and shows the user no "looked up your record" step.
func agentTaskPlanSteps(output AgentTaskPlanningOutput) []agentrun.PlanStep {
	answer := agentrun.PlanStep{ID: agentSummaryStepID, Type: agentrun.StepAnalyze, SkillID: agentSummarizeSkillID, SkillVersion: agentSkillVersion,
		ExpectedOutput: "a short private answer to the request", Tier: agentrun.TierPrivateDraft}
	if !output.NeedsOwnRecord {
		return []agentrun.PlanStep{answer}
	}
	return []agentrun.PlanStep{
		{ID: agentReadStepID, Type: agentrun.StepRead, SkillID: agentReadSkillID, SkillVersion: agentSkillVersion,
			ExpectedOutput: "the signed-in user's own worker record", Tier: agentrun.TierRead},
		answer,
	}
}
