package productui

// UXBLIND-073 keeps employee navigation aligned with the authorized data
// projection. An empty journey answer is not useful Insights content, while
// an employee's Organization page remains a stable place for their admitted
// team and reporting-line context.
func contentAwareNavigation(view View) View {
	if view.Loading || view.ContentLoading || view.Refreshing || view.LoadError != "" {
		return view
	}
	view.Navigation = filterContentAwareNavigation(view.Navigation, view)
	view.NavigationSupport = filterContentAwareNavigation(view.NavigationSupport, view)
	return view
}

func filterContentAwareNavigation(items []NavItem, view View) []NavItem {
	filtered := make([]NavItem, 0, len(items))
	for _, item := range items {
		if item.Page == PageInsights && !insightsHasContent(view) {
			continue
		}
		item.Children = filterContentAwareNavigation(item.Children, view)
		filtered = append(filtered, item)
	}
	return filtered
}

func insightsHasContent(view View) bool {
	if view.JourneyPopulation == nil && view.Work == nil {
		// Nil projections are unresolved in isolated previews and loading
		// shells; retaining the route avoids hiding it before the answer arrives.
		return true
	}
	totals, _ := journeyTotals(view)
	return totals.Total > 0
}

func employeeOrganizationMetadataTitle(view View, population []Person) string {
	if len(population) == 0 && view.Can(PageMyself, "view") && !view.Can(PagePeople, "view") {
		return ""
	}
	return view.Locale.Text("organization.metadata_title")
}
