package productui

import "sync"

var (
	defaultStylesheetOnce sync.Once
	defaultStylesheet     string
	platformStylesOnce    sync.Once
	platformStyles        string
)

// Stylesheet is the fixed platform stylesheet for the first product slice.
// Customer branding resolves semantic custom properties; it cannot replace
// focus, status, high-contrast, print, or responsive safety rules.
func Stylesheet() string {
	defaultStylesheetOnce.Do(func() {
		theme, _ := ResolveTheme(nil)
		defaultStylesheet = stylesheetForTheme(theme)
	})
	return defaultStylesheet
}

// StylesheetForTheme validates customer overrides and returns one complete,
// deterministic sheet suitable for CSP hashing. A later BrandPack service can
// call this boundary without allowing raw tenant CSS into the document.
func StylesheetForTheme(overrides map[string]string) (string, error) {
	theme, err := ResolveTheme(overrides)
	if err != nil {
		return "", err
	}
	return stylesheetForTheme(theme), nil
}

func stylesheetForTheme(theme Theme) string {
	platformStylesOnce.Do(func() {

		platformStyles = baseStylesheet() + SemanticThemeStylesheet() + refinementsStylesheet() + componentRefinementsStylesheet() + responsiveGridFixStylesheet() + responsiveSafetyStylesheet() + collapsibleNavigationStylesheet() + NavigationEnhancementsStylesheet() + liveDataRefinementsStylesheet() + viewportShellStylesheet() + personProfileStylesheet() + peopleDirectoryStylesheet() + workflowHistoryStylesheet() + PhotoStylesheet() + profileDetailStylesheet() + historyTableStylesheet() + JourneyIntegrationStylesheet() + MotionStylesheet() + customerThemeStylesheet() + AppearanceStylesheet() + AppearanceSwatchStylesheet() + CustomerIdentityStylesheet() + brandLogoStylesStylesheet() + compactBrandStylesStylesheet() + AppearanceRobustnessStylesheet() + localeStylesStylesheet() + localePreferenceStylesStylesheet() + accessibilityStylesStylesheet() + accessibilityLayoutStylesStylesheet() + accessibilityReviewStylesStylesheet() + localePreferenceAccessibilityStylesStylesheet() + InteractionMotionStylesheet() + navigationPolishStylesStylesheet() + navigationScrollbarStylesStylesheet() + navigationViewportStylesStylesheet() + navigationViewportXStylesStylesheet() + colorModeControlStylesStylesheet() + surfaceTokenCoverageStylesheet() + legacySurfaceCoverageStylesheet() + loadingProxyStylesStylesheet() + loadingLayoutOffsetsStylesheet() + navigationSearchStylesStylesheet() + peopleSortFilterStylesStylesheet() + peopleQuickActionStylesStylesheet() + collectionControlStylesStylesheet() + uxReviewRefinementsStylesheet() + responsiveComponentStylesStylesheet() + peopleStickyHeaderStylesStylesheet() + dataTableStylesStylesheet() + GlobalSearchStylesheet() + historyNavigationStylesheet() + actionLauncherStylesheet() + PopoverStylesheet() + ViewerProfileStylesheet() + organizationMetadataStylesStylesheet() + organizationHierarchyStylesStylesheet() + organizationDisclosureStylesStylesheet() + organizationVisibilityStylesStylesheet() + roleAccessStylesStylesheet() + workerIDStylesStylesheet() + permissionBoundaryStylesheet() + validationStylesStylesheet() + statusPresentationStylesStylesheet() + provenancePresentationStylesStylesheet() + myselfStylesStylesheet() + networkTransitionStylesStylesheet() + navigationInteractionRefinementsStylesheet() + interactionThemeStylesStylesheet() + visualQARefinementsStylesheet() + peopleActionColumnStylesStylesheet() + BreadcrumbStylesheet() + UtilityDrawerStylesheet() + MobileShellStylesheet() + FederationEntryStylesheet() + SessionWarningStylesheet() + StepUpStylesheet() + AuthorityBannerStylesheet() + BreakGlassStylesheet() + PolicySimulationStylesheet() + SignedOutStylesheet() + DelegationSelectorStylesheet() + ContextSwitcherStylesheet() + uxaudit008TableDensityStylesheet() + scrollRegionStylesheet() + uipolish001TypographyStylesheet() + uipolish002SettingsStylesheet() + UIPolish004ScrollStylesheet() + UIPolish011MotionStylesheet() + uipolish008TableStylesheet() + darkModeStylesStylesheet()
	})
	return theme.CSS() + platformStyles
}

// popoverStyles is the single visual and spatial contract for floating
// surfaces. The transparent bridge covers the trigger-to-panel seam while the
// runtime grace timer covers diagonal pointer travel outside that narrow seam.

// globalSearchStyles is appended after light/dark token resolution so the
