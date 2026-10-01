package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

func TestTodo_UXBLIND_062(t *testing.T) {
	events := timelineLocale("en-US", []*journeyv1.TimelineEvent{
		{Kind: eventWorkItem, Title: "Finance review approved", Actor: "Loretta"},
		{Kind: eventWorkItem, Title: "Manager review assigned", Detail: "assigned_to:Morgan"},
		{Kind: eventWorkItem, Title: "Finance review approved", Detail: "decision_reason:Budget confirmed"},
	})
	if len(events) != 3 {
		t.Fatalf("timeline events = %+v", events)
	}
	foundApprover, foundAssignment, foundReason := false, false, false
	for _, event := range events {
		foundApprover = foundApprover || (strings.Contains(event.Title, "approved") && event.Actor == "Loretta")
		foundAssignment = foundAssignment || event.Detail == "Assigned to Morgan"
		foundReason = foundReason || event.Detail == "Reason: Budget confirmed"
	}
	if !foundApprover || !foundAssignment || !foundReason {
		t.Errorf("approver/outcome/details = %+v", events)
	}
}

func TestTodo_UXBLIND_062_Browser(t *testing.T) {
	for _, tc := range []struct {
		locale string
		want   []string
	}{
		{"de-DE", []string{"genehmigt", "Zugewiesen an Loretta", "Begründung: Budget"}},
		{"ar", []string{"تمت الموافقة", "أُسندت إلى Loretta", "السبب: Budget"}},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			if title := timelineTitleLocale(tc.locale, &journeyv1.TimelineEvent{Kind: eventWorkItem, Title: "Finance review approved"}); !strings.Contains(title, tc.want[0]) {
				t.Errorf("localized title = %q", title)
			}
			if got := timelineDetailLocale(tc.locale, &journeyv1.TimelineEvent{Kind: eventWorkItem, Detail: "assigned_to:Loretta"}); got != tc.want[1] {
				t.Errorf("localized assignee = %q", got)
			}
			if got := timelineDetailLocale(tc.locale, &journeyv1.TimelineEvent{Kind: eventWorkItem, Detail: "decision_reason:Budget"}); got != tc.want[2] {
				t.Errorf("localized reason = %q", got)
			}
		})
	}
}

func TestTodo_UXBLIND_062_Golden(t *testing.T) {
	title := timelineTitleLocale("en-US", &journeyv1.TimelineEvent{Kind: eventWorkItem, Title: "Manager review rejected"})
	detail := timelineDetailLocale("en-US", &journeyv1.TimelineEvent{Kind: eventWorkItem, Detail: "decision_reason:Evidence was incomplete"})
	if got, want := title+"|"+detail, "Manager review: rejected|Reason: Evidence was incomplete"; got != want {
		t.Fatalf("golden localized history = %q, want %q", got, want)
	}
}
