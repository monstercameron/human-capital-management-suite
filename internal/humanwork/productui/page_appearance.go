package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func appearancePage(view View) ui.Node {
	editable := len(view.EffectivePermissions) == 0 || view.Can(PageAppearance, "update")
	previewPages := make([]AppearancePreviewPage, 0, 4)
	for _, page := range []PageID{PageHome, PagePeople, PageWork, PageOrganization} {
		if !view.Allows(page, "view") {
			continue
		}
		definition, ok := LookupPage(page)
		if !ok {
			continue
		}
		previewPages = append(previewPages, AppearancePreviewPage{ID: page, Label: view.Locale.Text(definition.LabelKey)})
	}
	var approved []BrandAssetOption
	if view.Tenant == "Harborcare Demo" || view.Tenant == "harborcare-demo" {
		approved = []BrandAssetOption{{Label: view.Locale.Text("appearance.use_sample_logo"), URL: "/workspace/assets/harborcare-logo.svg"}}
	}
	return ui.CreateElement(AppearancePage, AppearancePageProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Theme:     view.Appearance, ColorModes: localizedColorModeOptions(view.Locale), Palettes: PaletteOptions(), Shapes: ShapeOptions(),
		Densities: DensityOptions(), Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(),
		Navigation: NavigationOptions(), Motions: MotionOptions(),
		Editable: editable, OnPreview: view.PreviewTheme, OnSave: view.SaveTheme, OnReset: view.ResetTheme,
		PreviewPages: previewPages, PreviewTenant: view.Tenant, ApprovedLogos: approved,
		RenderPreview: func(page PageID) ui.Node {
			if !view.Allows(page, "view") {
				return unavailablePanel(view.Locale.Text("shell.page_unavailable"), view.Locale.Text("shell.page_recovery"))
			}
			return BuildPageContent(appearancePreviewView(view, page))
		},
	})
}

// A theme preview starts from a representative authorized page rather than
// inheriting an unrelated search or selection left in the live workspace.
func appearancePreviewView(view View, page PageID) View {
	view.Page = page
	view.Navigate = nil
	view.Query = ""
	view.PeoplePage = 0
	view.PeopleTeam = ""
	view.PeopleLocation = ""
	view.PeopleEligibleOnly = false
	view.WorkFilter = ""
	view.SelectedWork = ""
	return view
}

func localizedColorModeOptions(locale LocaleContext) []AppearanceOption {
	options := ColorModeOptions()
	for index := range options {
		options[index].Label = locale.Text("appearance.color_mode_" + options[index].ID)
		options[index].Description = locale.Text("appearance.color_mode_" + options[index].ID + "_help")
	}
	return options
}
