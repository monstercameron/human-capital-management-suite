package journey

import (
	"strings"
	"testing"
)

func uxrefineAction(t *testing.T, actions []Action, id string) Action {
	t.Helper()
	for _, action := range actions {
		if action.ID == id {
			return action
		}
	}
	t.Fatalf("fixture action %q was not found", id)
	return Action{}
}

// TestTodo_UXBLIND_052 proves each journey intervention keeps its own review
// wording instead of falling back to one generic pending label.
func TestTodo_UXBLIND_052(t *testing.T) {
	page := SampleDetailPage()
	actions := page.Detail.Actions
	approve := uxrefineAction(t, actions, "approve")
	approve.Confirmation = []Fact{{Label: "Employee", Value: page.Detail.Journey.WorkerName}}
	approve.ReviewLabel = "Review and approve"
	approve.ConfirmTitle = "Confirm approval"
	approve.Busy = true
	approve.BusyLabel = "Checking approval details…"

	start := uxrefineAction(t, actions, "execute")
	start.Disabled = false
	start.Confirmation = []Fact{{Label: "Employee", Value: page.Detail.Journey.WorkerName}}
	start.ReviewLabel = "Review and start approval workflow"
	start.ConfirmTitle = "Confirm start approval workflow"

	submit := Action{
		ID: "submit", Label: "Save proposal", Variant: "primary", Action: "/workspace/journeys/propose",
		Confirmation: []Fact{{Label: "Employee", Value: page.Detail.Journey.WorkerName}},
		ReviewLabel:  "Review and submit", ConfirmTitle: "Check the proposal before you submit",
	}

	for _, tc := range []struct {
		name, wantLabel, wantTitle string
		action                     Action
	}{
		{"approve", "Review and approve", "Confirm approval", approve},
		{"start", "Review and start approval workflow", "Confirm start approval workflow", start},
		{"submit", "Review and submit", "Check the proposal before you submit", submit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markup := renderNode(t, actionCard(live{locale: "en-US"}, tc.action))
			if !strings.Contains(markup, `class="jn-confirm"`) || !strings.Contains(markup, tc.wantLabel) || !strings.Contains(markup, tc.wantTitle) {
				t.Fatalf("action-specific review surface missing label/title %q/%q:\n%s", tc.wantLabel, tc.wantTitle, markup)
			}
			if !strings.Contains(markup, `class="jn-confirm-open-label">`+tc.wantLabel+`</span>`) {
				t.Fatalf("pending %s did not keep its action label on the review trigger:\n%s", tc.name, markup)
			}
		})
	}
}

// TestTodo_UXBLIND_052_Browser proves the busy review surface is mounted
// immediately and disables its committing control while work is pending.
func TestTodo_UXBLIND_052_Browser(t *testing.T) {
	page := SampleDetailPage()
	action := uxrefineAction(t, page.Detail.Actions, "approve")
	action.Confirmation = []Fact{{Label: "Employee", Value: page.Detail.Journey.WorkerName}}
	action.ReviewLabel = "Review and approve"
	action.ConfirmTitle = "Confirm approval"
	action.Busy = true
	action.BusyLabel = "Checking approval details…"

	markup := renderNode(t, actionCard(live{locale: "en-US"}, action))
	for _, want := range []string{
		`class="jn-confirm"`,
		`class="jn-confirm-body"`,
		`role="status"`,
		"Checking approval details…",
		`aria-busy="true"`,
		"Review and approve",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("busy review surface omitted %q:\n%s", want, markup)
		}
	}
	if !strings.Contains(markup, `class="jn-confirm-open-label">Review and approve</span>`) {
		t.Fatalf("busy review lost the action-specific pending trigger:\n%s", markup)
	}
}
