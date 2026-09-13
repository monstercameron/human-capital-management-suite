package app

import (
	"reflect"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// TestTodo_PROMOUX_012 is the server half of the PRIMARY: the engine resolves
// each journey's relationship to the viewer, what it asks of them and its next
// transition, so every surface reads one answer.
func TestTodo_PROMOUX_012(t *testing.T) {
	t.Run("every stage maps to one next transition and closure agrees with the intervention rule", func(t *testing.T) {
		for value, name := range journeyv1.JourneyStage_name {
			if value == 0 {
				continue
			}
			stage := workspace.JourneyStage(strings.TrimPrefix(name, "JOURNEY_STAGE_"))
			transition := journeyStageTransition(stage)
			_, unavailable := interventionUnavailableAtStage(workspace.JourneyInterventionCancel, stage)
			reason, _ := interventionUnavailableAtStage(workspace.JourneyInterventionCancel, stage)
			terminal := unavailable && reason == reasonInterventionAlreadyTerminal
			if journeyStageClosed(stage) != terminal {
				t.Errorf("%s: closed = %t, the intervention rule's terminal = %t", stage, journeyStageClosed(stage), terminal)
			}
			if journeyStageClosed(stage) {
				if transition != (journeyTransition{}) {
					t.Errorf("closed %s names a next transition %+v", stage, transition)
				}
				continue
			}
			if transition.step == "" {
				t.Errorf("open %s has no next step", stage)
			}
			if transition.awaitsPerson == (transition.owner == workspace.JourneyStepOwnerSystem) {
				t.Errorf("%s: awaitsPerson %t contradicts owner %q", stage, transition.awaitsPerson, transition.owner)
			}
		}
		if journeyStageTransition("SOMETHING_NEW") != (journeyTransition{}) || journeyStageClosed("SOMETHING_NEW") {
			t.Fatal("an unknown stage was given a guessed transition or closure")
		}
	})

	holds := &workspace.JourneyWorkItemSummary{ViewerMembership: "ASSIGNEE", ViewerPermittedActions: []string{"claim"}}
	claimed := &workspace.JourneyWorkItemSummary{ViewerMembership: "CLAIMANT", ViewerPermittedActions: []string{"complete", "decide_approval", "release"}}
	candidate := &workspace.JourneyWorkItemSummary{ViewerMembership: "CANDIDATE", ViewerPermittedActions: []string{"claim"}}
	someoneElses := &workspace.JourneyWorkItemSummary{ViewerMembership: "NONE", AssigneePrincipalID: "principal:finance"}
	rel := func(r ...workspace.JourneyViewerRelationship) []workspace.JourneyViewerRelationship { return r }

	cases := []struct {
		name      string
		stage     workspace.JourneyStage
		initiator bool
		work      *workspace.JourneyWorkItemSummary
		want      workspace.JourneyViewerProjection
	}{
		{"a passive wait the viewer proposed asks nothing of them", workspace.JourneyStageWaitingEffectiveDate, true, nil,
			workspace.JourneyViewerProjection{Relationships: rel(workspace.JourneyViewerInitiator), Responsibility: workspace.JourneyResponsibilityTracking,
				NextStep: workspace.JourneyNextStepAwaitEffectiveDate, NextStepOwner: workspace.JourneyStepOwnerSystem}},
		{"a passive wait nobody relates to is observed", workspace.JourneyStageWaitingEffectiveDate, false, nil,
			workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityObserving,
				NextStep: workspace.JourneyNextStepAwaitEffectiveDate, NextStepOwner: workspace.JourneyStepOwnerSystem}},
		{"an approval the viewer initiated but someone else holds is tracked", workspace.JourneyStageManagerApproval, true, someoneElses,
			workspace.JourneyViewerProjection{Relationships: rel(workspace.JourneyViewerInitiator), Responsibility: workspace.JourneyResponsibilityTracking,
				NextStep: workspace.JourneyNextStepManagerDecision, NextStepOwner: workspace.JourneyStepOwnerManager, AwaitsPerson: true}},
		{"a finance approval assigned to the viewer is their action", workspace.JourneyStageFinanceApproval, false, holds,
			workspace.JourneyViewerProjection{Relationships: rel(workspace.JourneyViewerAssignee), Responsibility: workspace.JourneyResponsibilityActionRequired,
				NextStep: workspace.JourneyNextStepFinanceDecision, NextStepOwner: workspace.JourneyStepOwnerFinance, AwaitsPerson: true}},
		{"a claimed item is held", workspace.JourneyStageAwaitingApproval, false, claimed,
			workspace.JourneyViewerProjection{Relationships: rel(workspace.JourneyViewerAssignee), Responsibility: workspace.JourneyResponsibilityActionRequired,
				NextStep: workspace.JourneyNextStepApprovalDecision, NextStepOwner: workspace.JourneyStepOwnerApprover, AwaitsPerson: true}},
		{"a claimable item is the viewer's action", workspace.JourneyStageManagerApproval, false, candidate,
			workspace.JourneyViewerProjection{Relationships: rel(workspace.JourneyViewerCandidate), Responsibility: workspace.JourneyResponsibilityActionRequired,
				NextStep: workspace.JourneyNextStepManagerDecision, NextStepOwner: workspace.JourneyStepOwnerManager, AwaitsPerson: true}},
		{"the viewer's own blocked proposal is theirs to correct", workspace.JourneyStageBlocked, true, nil,
			workspace.JourneyViewerProjection{Relationships: rel(workspace.JourneyViewerInitiator), Responsibility: workspace.JourneyResponsibilityActionRequired,
				NextStep: workspace.JourneyNextStepCorrectProposal, NextStepOwner: workspace.JourneyStepOwnerProposer, AwaitsPerson: true}},
		{"someone else's blocked proposal is not", workspace.JourneyStageBlocked, false, nil,
			workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityObserving,
				NextStep: workspace.JourneyNextStepCorrectProposal, NextStepOwner: workspace.JourneyStepOwnerProposer, AwaitsPerson: true}},
		{"repair names no owner, so an initiator only tracks it", workspace.JourneyStageRepairRequired, true, nil,
			workspace.JourneyViewerProjection{Relationships: rel(workspace.JourneyViewerInitiator), Responsibility: workspace.JourneyResponsibilityTracking,
				NextStep: workspace.JourneyNextStepRepair, AwaitsPerson: true}},
		{"initiator and assignee at once, relationships sorted", workspace.JourneyStageReapproval, true, holds,
			workspace.JourneyViewerProjection{Relationships: rel(workspace.JourneyViewerAssignee, workspace.JourneyViewerInitiator), Responsibility: workspace.JourneyResponsibilityActionRequired,
				NextStep: workspace.JourneyNextStepReapprovalDecision, NextStepOwner: workspace.JourneyStepOwnerApprover, AwaitsPerson: true}},
		{"a recorded journey is closed even for its assignee and initiator", workspace.JourneyStageRecorded, true, holds,
			workspace.JourneyViewerProjection{Relationships: rel(workspace.JourneyViewerAssignee, workspace.JourneyViewerInitiator), Responsibility: workspace.JourneyResponsibilityClosed, Closed: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := journeyViewerProjection(c.stage, c.initiator, c.work); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("projection = %+v, want %+v", got, c.want)
			}
		})
	}

	t.Run("a viewer the work item rules do not admit gains no relationship from it", func(t *testing.T) {
		item := summaryItem(workitem.StatusAssigned, workitem.VisibilityAssigneeOnly, summaryClock)
		work := journeyWorkItemSummary([]workitem.WorkItem{item}, "principal:outsider", "org-elsewhere", summaryClock, nil)
		got := journeyViewerProjection(workspace.JourneyStageFinanceApproval, false, work)
		if len(got.Relationships) != 0 || got.Responsibility != workspace.JourneyResponsibilityObserving {
			t.Fatalf("an outsider to an ASSIGNEE_ONLY item received %+v", got)
		}
		assignee := journeyWorkItemSummary([]workitem.WorkItem{item}, item.OwnerRef, "org-elsewhere", summaryClock, nil)
		if got := journeyViewerProjection(workspace.JourneyStageFinanceApproval, false, assignee); got.Responsibility != workspace.JourneyResponsibilityActionRequired {
			t.Fatalf("the routed assignee received %+v", got)
		}
	})

	t.Run("initiator identity is an exact, non-empty match", func(t *testing.T) {
		for _, c := range []struct {
			initiator, viewer string
			want              bool
		}{
			{"principal:a", "principal:a", true},
			{" principal:a ", "principal:a", true},
			{"principal:a", "principal:ab", false},
			{"", "", false},
			{"", "principal:a", false},
			{"principal:a", "", false},
		} {
			if got := isJourneyInitiator(c.initiator, c.viewer); got != c.want {
				t.Errorf("isJourneyInitiator(%q, %q) = %t, want %t", c.initiator, c.viewer, got, c.want)
			}
		}
	})
}
