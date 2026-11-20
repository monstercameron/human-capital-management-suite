package productui

// CanOpenAnalytics says whether the viewer may open the governed analytics
// surface (the report catalog). Chat's Prepare an analysis (issue 3959) hands a
// request there and is offered only to a person the page would not turn away. A
// view with no resolved permissions is not trusted: the answer is no.
func (view View) CanOpenAnalytics() bool {
	return len(view.EffectivePermissions) > 0 && view.Can(PageReportCatalog, "view")
}
