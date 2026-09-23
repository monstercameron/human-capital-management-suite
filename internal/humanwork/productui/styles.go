package productui

import (
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

var (
	defaultStylesheetOnce sync.Once
	defaultStylesheet     string
	platformStylesOnce    sync.Once
	platformStyles        string
	platformDarkStyles    string
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

// StylesheetForCustomerTheme renders the complete, CSP-hashable customer
// BrandPack, including independently admitted dark colors when configured.
func StylesheetForCustomerTheme(customer CustomerTheme) (string, error) {
	if err := ValidateCustomerTheme(customer); err != nil {
		return "", err
	}
	if customer.Palette != "custom" {
		return Stylesheet(), nil
	}
	modes, err := ResolveThemeModesWithDark(customer.TokenOverrides, customer.DarkTokenOverrides)
	if err != nil {
		return "", err
	}
	platformStylesOnce.Do(initPlatformStyles)
	return modes[ThemeModeLight].CSS() + platformStyles + customAppearancePreviewStylesheetForModes(modes) + darkModeStylesStylesheetForCustomer(modes[ThemeModeLight], modes[ThemeModeDark]), nil
}

func stylesheetForTheme(theme Theme) string {
	platformStylesOnce.Do(initPlatformStyles)
	return theme.CSS() + platformStyles + customAppearancePreviewStylesheet(theme) + platformDarkStyles
}

func initPlatformStyles() {
	platformStyles = baseStylesheet() + SemanticThemeStylesheet() + refinementsStylesheet() + componentRefinementsStylesheet() + responsiveGridFixStylesheet() + responsiveSafetyStylesheet() + collapsibleNavigationStylesheet() + NavigationEnhancementsStylesheet() + liveDataRefinementsStylesheet() + viewportShellStylesheet() + personProfileStylesheet() + peopleDirectoryStylesheet() + workflowHistoryStylesheet() + PhotoStylesheet() + profileDetailStylesheet() + historyTableStylesheet() + JourneyIntegrationStylesheet() + MotionStylesheet() + customerThemeStylesheet() + AppearanceStylesheet() + AppearanceSwatchStylesheet() + CustomerIdentityStylesheet() + brandLogoStylesStylesheet() + compactBrandStylesStylesheet() + AppearanceRobustnessStylesheet() + localeStylesStylesheet() + localePreferenceStylesStylesheet() + accessibilityStylesStylesheet() + accessibilityLayoutStylesStylesheet() + accessibilityReviewStylesStylesheet() + localePreferenceAccessibilityStylesStylesheet() + InteractionMotionStylesheet() + navigationPolishStylesStylesheet() + navigationScrollbarStylesStylesheet() + navigationViewportStylesStylesheet() + navigationViewportXStylesStylesheet() + colorModeControlStylesStylesheet() + surfaceTokenCoverageStylesheet() + legacySurfaceCoverageStylesheet() + loadingProxyStylesStylesheet() + loadingLayoutOffsetsStylesheet() + navigationSearchStylesStylesheet() + peopleSortFilterStylesStylesheet() + peopleQuickActionStylesStylesheet() + collectionControlStylesStylesheet() + uxReviewRefinementsStylesheet() + responsiveComponentStylesStylesheet() + peopleStickyHeaderStylesStylesheet() + dataTableStylesStylesheet() + GlobalSearchStylesheet() + historyNavigationStylesheet() + actionLauncherStylesheet() + PopoverStylesheet() + ViewerProfileStylesheet() + organizationMetadataStylesStylesheet() + organizationHierarchyStylesStylesheet() + organizationDisclosureStylesStylesheet() + organizationVisibilityStylesStylesheet() + roleAccessStylesStylesheet() + workerIDStylesStylesheet() + permissionBoundaryStylesheet() + validationStylesStylesheet() + statusPresentationStylesStylesheet() + provenancePresentationStylesStylesheet() + myselfStylesStylesheet() + networkTransitionStylesStylesheet() + navigationInteractionRefinementsStylesheet() + interactionThemeStylesStylesheet() + visualQARefinementsStylesheet() + peopleActionColumnStylesStylesheet() + BreadcrumbStylesheet() + UtilityDrawerStylesheet() + MobileShellStylesheet() + FederationEntryStylesheet() + SessionWarningStylesheet() + StepUpStylesheet() + AuthorityBannerStylesheet() + BreakGlassStylesheet() + PolicySimulationStylesheet() + SignedOutStylesheet() + DelegationSelectorStylesheet() + ContextSwitcherStylesheet() + uxaudit008TableDensityStylesheet() + scrollRegionStylesheet() + uipolish001TypographyStylesheet() + uipolish002SettingsStylesheet() + UIPolish004ScrollStylesheet() + UIPolish011MotionStylesheet() + uipolish008TableStylesheet() + peopleRowStateStylesheet() + workFilterStripStylesheet() + homeOperationalStylesheet() + asyncRegionFailureStylesheet() + roleAccessPreviewStylesheet()
	platformStyles += workflowViewerStylesheet()
	platformStyles += chatui.ScopedStylesheet() + chatShellStylesheet()
	platformStyles += ChatRetentionStylesheet()
	platformStyles += workflowDesignerStylesheet()
	platformStyles += workflowEditorStylesheet()
	platformStyles += columnChooserStylesheet()
	platformStyles += docsStylesheet()
	platformStyles += workflowNotificationStylesheet()
	platformDarkStyles = darkModeStylesStylesheet()
}

// popoverStyles is the single visual and spatial contract for floating
// surfaces. The transparent bridge covers the trigger-to-panel seam while the
// runtime grace timer covers diagonal pointer travel outside that narrow seam.

// globalSearchStyles is appended after light/dark token resolution so the
