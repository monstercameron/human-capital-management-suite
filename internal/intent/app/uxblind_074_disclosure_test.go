package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_UXBLIND_074 proves the recorded decision at the server projection
// boundary: the promotion subject has no journey projection in any in-flight
// state, while an unrelated viewer and the subject after recording retain the
// ordinary shared stage projection.
func TestTodo_UXBLIND_074(t *testing.T) {
	zero := workspace.JourneyViewerProjection{}
	for _, stage := range []workspace.JourneyStage{
		workspace.JourneyStageProposed,
		workspace.JourneyStageAwaitingApproval,
		workspace.JourneyStageManagerApproval,
		workspace.JourneyStageFinanceApproval,
		workspace.JourneyStageWaitingEffectiveDate,
		workspace.JourneyStageExecuted,
		workspace.JourneyStageObservingEffects,
	} {
		t.Run(string(stage), func(t *testing.T) {
			got := journeyViewerDisclosureProjection(stage, false, true, nil)
			if !reflect.DeepEqual(got, zero) {
				t.Fatalf("subject projection for %s = %+v, want withheld zero projection", stage, got)
			}
		})
	}

	worker := values.EntityRef{Tenant: values.TenantId("harborcare-demo"), Kind: "worker", Id: "worker-ana"}
	for _, tc := range []struct {
		name, viewer, workerRef string
		want                    bool
	}{
		{name: "corpus stable key", viewer: "ana-flores", workerRef: "ana-flores", want: true},
		{name: "durable entity id", viewer: "worker-ana", workerRef: "ana-flores", want: true},
		{name: "different viewer", viewer: "manager-1", workerRef: "ana-flores", want: false},
		{name: "empty viewer", viewer: "", workerRef: "ana-flores", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := journeySubjectIsViewerRef(tc.viewer, worker, tc.workerRef); got != tc.want {
				t.Fatalf("journeySubjectIsViewerRef(%q, %q) = %t, want %t", tc.viewer, tc.workerRef, got, tc.want)
			}
		})
	}

	if got := journeyViewerDisclosureProjection(workspace.JourneyStageManagerApproval, false, false, nil); got.Responsibility != workspace.JourneyResponsibilityObserving || got.NextStep != workspace.JourneyNextStepManagerDecision {
		t.Fatalf("unrelated viewer projection = %+v, want ordinary observing projection", got)
	}
	recorded := journeyViewerDisclosureProjection(workspace.JourneyStageRecorded, false, true, nil)
	if recorded.Responsibility != workspace.JourneyResponsibilityClosed || !recorded.Closed {
		t.Fatalf("recorded subject projection = %+v, want the recorded result", recorded)
	}
}

// TestTodo_UXBLIND_074_Golden pins the disclosure projection's wire-shaped
// bytes. A withheld subject receives no stage, relationship, action, or
// closure signal that a client could turn into a card, pay row, or notification
// link; the recorded result remains the normal terminal projection.
func TestTodo_UXBLIND_074_Golden(t *testing.T) {
	type goldenRow struct {
		Stage      workspace.JourneyStage            `json:"stage"`
		Projection workspace.JourneyViewerProjection `json:"projection"`
	}
	rows := []goldenRow{
		{Stage: workspace.JourneyStageProposed, Projection: journeyViewerDisclosureProjection(workspace.JourneyStageProposed, false, true, nil)},
		{Stage: workspace.JourneyStageManagerApproval, Projection: journeyViewerDisclosureProjection(workspace.JourneyStageManagerApproval, false, true, nil)},
		{Stage: workspace.JourneyStageWaitingEffectiveDate, Projection: journeyViewerDisclosureProjection(workspace.JourneyStageWaitingEffectiveDate, false, true, nil)},
		{Stage: workspace.JourneyStageRecorded, Projection: journeyViewerDisclosureProjection(workspace.JourneyStageRecorded, false, true, nil)},
	}
	got, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal disclosure golden: %v", err)
	}
	const want = `[{"stage":"PROPOSED","projection":{"Relationships":null,"Responsibility":"","NextStep":"","NextStepOwner":"","AwaitsPerson":false,"Closed":false}},{"stage":"MANAGER_APPROVAL","projection":{"Relationships":null,"Responsibility":"","NextStep":"","NextStepOwner":"","AwaitsPerson":false,"Closed":false}},{"stage":"WAITING_EFFECTIVE_DATE","projection":{"Relationships":null,"Responsibility":"","NextStep":"","NextStepOwner":"","AwaitsPerson":false,"Closed":false}},{"stage":"RECORDED","projection":{"Relationships":null,"Responsibility":"CLOSED","NextStep":"","NextStepOwner":"","AwaitsPerson":false,"Closed":true}}]`
	if string(got) != want {
		t.Fatalf("disclosure golden = %s, want %s", got, want)
	}
}
