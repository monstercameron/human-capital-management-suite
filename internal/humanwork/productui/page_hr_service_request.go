package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func hrServiceRequestPage(view View) ui.Node {
	escalation := SupportDestinationProps{}
	if view.Allows(PageConfidentialCase, "view") {
		definition, ok := LookupPage(PageConfidentialCase)
		if ok {
			escalation = SupportDestinationProps{
				Category: "Need a confidential route?",
				Title:    view.Locale.Text(definition.TitleKey), Description: view.Locale.Text(definition.SubtitleKey),
				ActionLabel: view.Locale.Text("help.open"), Href: statefulHref(view, PageConfidentialCase), Navigate: view.Navigate,
			}
		}
	}
	return ui.CreateElement(HRServiceRequestPage, HRServiceRequestPageProps{
		Action:        statefulHref(view, PageHRServiceRequest),
		CategoryLabel: view.Locale.Text("hr_request.category_label"), DetailsLabel: view.Locale.Text("hr_request.details_label"),
		CategoryPlaceholder: view.Locale.Text("hr_request.category_placeholder"), DetailsPlaceholder: view.Locale.Text("hr_request.details_placeholder"),
		SubmitLabel: view.Locale.Text("hr_request.submit"), SubmitDisabled: !view.Allows(PageHRServiceRequest, "create"), Escalation: escalation,
		Categories: []SupportRequestCategory{
			{Value: "pay-benefits", Label: view.Locale.Text("hr_request.pay_benefits")},
			{Value: "time-leave", Label: view.Locale.Text("hr_request.time_leave")},
			{Value: "workplace-access", Label: view.Locale.Text("hr_request.workplace_access")},
			{Value: "other", Label: view.Locale.Text("hr_request.other")},
		},
		Unavailable: view.Locale.Text("hr_service_request.unavailable_detail"),
	})
}
