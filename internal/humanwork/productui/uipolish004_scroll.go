package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// UIPolish004ScrollStylesheet is the typed scroll-ownership contract for
// production shell regions. The composition root should append it after the
// existing component sheets (the insertion point is stylesheetForTheme in
// styles.go); keeping this function separate lets the integration preserve
// existing scrollbar refinements without creating a second owner.
func UIPolish004ScrollStylesheet() string {
	return buildTypedSheet(declareUIPolish004ScrollStyles)
}

func declareUIPolish004ScrollStyles() {
	// Existing shell/table rules own overflow and scrollbar geometry. Only add
	// focus and gesture containment here so those owners remain noncompeting.
	declareGlobal(".primary-nav,.data-table-scroll",
		gwccss.Raw("overscroll-behavior-inline", "contain"),
	)
	// navigationViewportXStylesheet historically emits an unconditional
	// overflow-x:hidden rule after the mobile overflow owner. Re-state the
	// narrow navigation owner here, after the complete shared sheet cascade,
	// so long labels remain reachable by touch, keyboard and RTL scrolling.
	declareGlobal(".primary-nav",
		mediaRule(gwccss.MaxW(760),
			gwccss.Raw("overflow-x", "auto"),
			gwccss.Raw("overflow-y", "visible"),
			gwccss.Raw("overscroll-behavior-inline", "contain"),
			gwccss.Raw("scrollbar-width", "auto"),
			gwccss.Raw("scrollbar-color", "auto"),
			gwccss.Raw("scrollbar-gutter", "auto"),
		),
	)
	declareGlobal(":where(.main-scroll,.primary-nav,.data-table-scroll,#people-directory-table-viewport) :where(a[href],button,input,select,textarea,[tabindex]):focus-visible",
		gwccss.Raw("scroll-margin-block", "var(--hcm-space-3,1.5rem)"),
		gwccss.Raw("scroll-margin-inline", "var(--hcm-space-2,1rem)"),
	)
	// Name the production overlay surfaces as well as their semantic role. A
	// workflow popover is not a dialog, and a future role change must not make
	// the launcher or utility drawer hand wheel/touch gestures to the page.
	declareGlobal(":where(.nav-drawer,.popover-panel,.modal-dialog,.action-launcher-dialog,.utility-drawer-dialog,.people-workflow-options,[role=dialog])",
		gwccss.Raw("overscroll-behavior", "contain"),
	)
}
