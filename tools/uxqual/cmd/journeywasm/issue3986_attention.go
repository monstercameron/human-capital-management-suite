package main

import (
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// Issue 3986: the viewer's own work, read for the private "What needs my
// attention?" brief. Responsibility comes from the owning service's
// viewer-specific projection, never from a status string; a journey whose
// responsibility the service did not state is labelled unknown. A journey the
// viewer can merely see, with no relationship to it, is not "my work" and is
// left out, as in My Work (productui.MyWorkItems).

// chatAttentionItems maps one ListJourneys answer onto the brief, leaving out
// journeys that are closed or are not the viewer's own work.
func chatAttentionItems(locale string, journeys []*journeyv1.Journey) []chatui.AttentionItem {
	copy := productui.ResolveProductLocale(locale)
	items := make([]chatui.AttentionItem, 0, len(journeys))
	for _, journey := range journeys {
		if journey == nil || strings.TrimSpace(journey.GetIntentId()) == "" {
			continue
		}
		viewer := journey.GetViewer()
		if viewer.GetClosed() || viewer.GetResponsibility() == journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_CLOSED {
			continue
		}
		if !chatAttentionMine(journey) {
			continue
		}
		stage := strings.TrimPrefix(journey.GetStage().String(), "JOURNEY_STAGE_")
		item := chatui.AttentionItem{
			ID:             journey.GetIntentId(),
			Title:          strings.TrimSpace(journey.GetWorkerName()),
			Source:         chatAttentionSource(locale),
			Status:         copy.Text(productui.JourneyStatusForStage(stage).BadgeKey),
			Href:           chatui.JourneyReferenceURL(journey.GetIntentId()),
			Responsibility: chatAttentionResponsibility(viewer.GetResponsibility()),
		}
		if item.Title == "" {
			item.Title = item.Source
		}
		if due := journey.GetCurrentWorkItem().GetDueAt(); due != nil && due.IsValid() {
			item.Due = due.AsTime()
		}
		items = append(items, item)
	}
	return items
}

// chatAttentionMine mirrors the My Work population: the viewer must act, must
// have started it, or must hold or be able to claim its current work item.
func chatAttentionMine(journey *journeyv1.Journey) bool {
	viewer := journey.GetViewer()
	if viewer.GetResponsibility() == journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED {
		return true
	}
	for _, relationship := range viewer.GetRelationships() {
		if relationship == journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_INITIATOR {
			return true
		}
	}
	switch journey.GetCurrentWorkItem().GetViewerMembership() {
	case "CLAIMANT", "ASSIGNEE", "CANDIDATE":
		return true
	}
	return false
}

func chatAttentionResponsibility(r journeyv1.JourneyViewerResponsibility) string {
	switch r {
	case journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED:
		return chatui.AttentionAction
	case journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_TRACKING,
		journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_OBSERVING:
		return chatui.AttentionWaiting
	}
	return chatui.AttentionUnknown
}

func chatAttentionSource(locale string) string {
	switch {
	case strings.HasPrefix(locale, "de"):
		return "Beförderung"
	case strings.HasPrefix(locale, "ar"):
		return "ترقية"
	}
	return "Promotion"
}
