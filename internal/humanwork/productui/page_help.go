package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func helpPage(view View) ui.Node {
	actions := make([]ActionLinkProps, 0, 6)
	for _, task := range []struct {
		page PageID
		key  string
	}{
		{PageMyself, "help.my_profile"},
		{PageOrganization, "help.organization"},
		{PagePeople, "help.choose_employee"},
		{PageJourneys, "help.review_requests"},
		{PageHistory, "help.past_decisions"},
		{PageSettings, "help.account_settings"},
	} {
		if view.Allows(task.page, "view") {
			actions = append(actions, ActionLinkProps{Label: view.Locale.Text(task.key), Href: statefulHref(view, task.page), Navigate: view.Navigate})
		}
	}
	description := trimRetiredSupportBoundary(view.Locale.Text("help.access_detail"))
	support := InformationalPanelProps{Title: view.Locale.Text("help.access_title"), Class: "support-summary", Description: description, Destinations: authorizedSupportDestinations(view)}
	if view.Allows(PageJourneys, "create") {
		support.Title = view.Locale.Text("help.promotion_title")
		support.Description = trimRetiredSupportBoundary(view.Locale.Text("help.promotion_detail"))
	}
	var search *SupportDestinationProps
	if view.Allows(PageKnowledgeSearch, "view") {
		if definition, ok := LookupPage(PageKnowledgeSearch); ok {
			search = &SupportDestinationProps{
				Category:    view.Locale.Text("help.category_answer"),
				Title:       view.Locale.Text(definition.TitleKey),
				Description: view.Locale.Text(definition.SubtitleKey),
				// The category is also the outcome-oriented action label. A
				// generic "Open" makes a support destination ambiguous for
				// keyboard and screen-reader users.
				ActionLabel: view.Locale.Text("help.category_answer"),
				Href:        statefulHref(view, PageKnowledgeSearch),
				Navigate:    view.Navigate,
			}
		}
	}
	return ui.CreateElement(HelpPage, HelpPageProps{
		Guidance: QuickActionsProps{Title: view.Locale.Text("help.tasks"), Actions: actions},
		Support:  support, SearchDestination: search, Escalation: description,
	})
}

// Older Help copy said that support tickets could not be submitted from the
// workspace. The authorized destinations below supersede that limitation;
// trim only that retired sentence while retaining the localized guidance
// that remains useful for the live promotion workflow.
func trimRetiredSupportBoundary(detail string) string {
	for _, marker := range []string{" Support tickets cannot", " Supportanfragen können", " لا يمكن إرسال تذاكر الدعم"} {
		if index := strings.Index(detail, marker); index >= 0 {
			return strings.TrimSpace(detail[:index])
		}
	}
	return detail
}

// authorizedSupportDestinations is intentionally assembled from the current
// permission projection. Help is a directory, not an authority boundary, so
// it never exposes a route that the server has not already admitted.
func authorizedSupportDestinations(view View) []SupportDestinationProps {
	type destination struct {
		page     PageID
		category string
	}
	available := []destination{
		{PageKnowledgeSearch, view.Locale.Text("help.category_answer")},
		{PageHRServiceRequest, view.Locale.Text("help.category_hr")},
		{PageConfidentialCase, view.Locale.Text("help.category_confidential")},
		{PageCaseStatus, view.Locale.Text("help.category_track")},
	}
	result := make([]SupportDestinationProps, 0, len(available))
	for _, item := range available {
		if !view.Allows(item.page, "view") {
			continue
		}
		definition, ok := LookupPage(item.page)
		if !ok {
			continue
		}
		result = append(result, SupportDestinationProps{
			Category: item.category, Title: view.Locale.Text(definition.TitleKey),
			Description: view.Locale.Text(definition.SubtitleKey), ActionLabel: item.category, Href: statefulHref(view, item.page), Navigate: view.Navigate,
		})
	}
	return result
}
