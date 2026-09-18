package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AppearancePageProps keeps the customer editor independent from storage and
// routing. The browser adapter supplies preview behavior and saves through the
// BrandPack service can supply the same callbacks without changing the page.
type AppearancePageProps struct {
	I18nProps
	Theme                CustomerTheme
	ColorModes           []AppearanceOption
	Palettes             []AppearanceOption
	Shapes               []AppearanceOption
	Densities            []AppearanceOption
	Glyphs               []AppearanceOption
	Typefaces            []AppearanceOption
	Navigation           []AppearanceOption
	Motions              []AppearanceOption
	Editable             bool
	OnPreview            func(CustomerTheme)
	OnSave               func(CustomerTheme)
	OnReset              func()
	BrandAssetStatus     string
	ApprovedLogos        []BrandAssetOption
	OnUploadBrandAsset   func(string)
	OnPreviewBrandAsset  func(string)
	OnRemoveBrandAsset   func()
	OnRollbackBrandAsset func()
	PreviewPages         []AppearancePreviewPage
	RenderPreview        func(PageID) ui.Node
	PreviewTenant        string
}

// AppearancePreviewPage is an already-authorized destination exposed to the
// read-only modal. The adapter supplies its real page composition separately.
type AppearancePreviewPage struct {
	ID    PageID
	Label string
}

// AppearancePage is the composed administration surface for tenant branding.
func AppearancePage(props AppearancePageProps) ui.Node {
	draft := NormalizeCustomerTheme(props.Theme)
	var removeLogo, rollbackLogo func()
	if props.OnPreview != nil {
		removeLogo = func() {
			draft.BrandLogoURL = ""
			previewAppearance(props.OnPreview, draft)
		}
		rollbackLogo = func() {
			draft.BrandLogoURL = props.Theme.BrandLogoURL
			previewAppearance(props.OnPreview, draft)
		}
	}
	if props.OnRemoveBrandAsset != nil {
		removeLogo = props.OnRemoveBrandAsset
	}
	if props.OnRollbackBrandAsset != nil {
		rollbackLogo = props.OnRollbackBrandAsset
	}
	previewProps := props
	previewProps.OnReset = func() {
		resetAppearanceDraft(&draft, props.OnReset)
	}
	return html.Div(html.Props{Class: "appearance-page", Data: map[string]string{"hcm-appearance-scope": "tenant"}},
		html.Section(html.Props{Class: "surface appearance-intro"},
			html.Div(html.Props{Class: "appearance-intro-copy"},
				html.H2(html.Props{}, ui.Text(props.Text("appearance.intro_title"))),
				html.P(html.Props{Class: "muted"}, ui.Text(props.Text("appearance.intro_detail"))),
			),
			html.Div(html.Props{Class: "appearance-intro-actions"},
				html.Span(html.Props{Class: "appearance-badge"}, navIcon("palette"), ui.Text(props.Text("appearance.tenant"))),
				ui.CreateElement(appearancePreviewLauncher, appearancePreviewProps{AppearancePageProps: previewProps, Draft: &draft}),
			),
		),
		appearanceScopeGuidance(props.I18nProps),
		appearanceSectionNavigation(props.I18nProps),
		html.Form(html.Props{Class: "appearance-form", OnSubmit: preventFormSubmit(props.OnSave, &draft)},
			html.Fieldset(html.Props{Class: "appearance-edit-boundary", Disabled: !props.Editable},
				html.Div(html.Props{Class: "appearance-controls"},
					appearanceBrandSignature(props.I18nProps, draft, props.PreviewTenant, func(value string) {
						draft.BrandName = value
						previewAppearance(props.OnPreview, draft)
					}, func(value string) {
						draft.BrandMark = value
						previewAppearance(props.OnPreview, draft)
					}, func(value string) {
						draft.BrandLogoURL = value
						previewAppearance(props.OnPreview, draft)
					}, BrandAssetPickerProps{
						I18nProps: props.I18nProps, Name: draft.BrandName, Mark: draft.BrandMark, LogoURL: draft.BrandLogoURL,
						Editable: props.Editable, Status: props.BrandAssetStatus, Approved: props.ApprovedLogos,
						OnUpload:   props.OnUploadBrandAsset,
						OnPreview:  props.OnPreviewBrandAsset,
						OnRemove:   removeLogo,
						OnRollback: rollbackLogo,
					}),
					appearanceChoices(props.Text("appearance.color_mode"), props.Text("appearance.color_mode_help"), "color_mode", draft.ColorMode, props.ColorModes, "color-mode-choices", func(value string) {
						draft.ColorMode = value
						previewAppearance(props.OnPreview, draft)
					}),
					appearanceChoices(props.Text("appearance.palette"), props.Text("appearance.palette_help"), "palette", draft.Palette, props.Palettes, "palette-choices", func(value string) {
						if value == "custom" && draft.Palette != "custom" {
							draft.TokenOverrides = paletteColorOverrides(draft.Palette)
							draft.DarkTokenOverrides = nil
						} else if value != "custom" {
							draft.TokenOverrides = nil
							draft.DarkTokenOverrides = nil
						}
						draft.Palette = value
						previewAppearance(props.OnPreview, draft)
					}),
					appearanceCustomColors(props.I18nProps, draft, ThemeModeLight, func(name, value string) {
						if draft.Palette != "custom" {
							draft.TokenOverrides = paletteColorOverrides(draft.Palette)
						}
						draft.Palette = "custom"
						draft.TokenOverrides[name] = value
						previewAppearance(props.OnPreview, draft)
					}),
					appearanceCustomColors(props.I18nProps, draft, ThemeModeDark, func(name, value string) {
						if draft.Palette != "custom" {
							draft.TokenOverrides = paletteColorOverrides(draft.Palette)
						}
						if draft.DarkTokenOverrides == nil {
							draft.DarkTokenOverrides = make(map[string]string)
						}
						draft.Palette = "custom"
						draft.DarkTokenOverrides[name] = value
						previewAppearance(props.OnPreview, draft)
					}),
					appearanceChoices(props.Text("appearance.shape"), props.Text("appearance.shape_help"), "shape", draft.Shape, props.Shapes, "", func(value string) {
						draft.Shape = value
						previewAppearance(props.OnPreview, draft)
					}),
					appearanceChoices(props.Text("appearance.density"), props.Text("appearance.density_help"), "density", draft.Density, props.Densities, "", func(value string) {
						draft.Density = value
						previewAppearance(props.OnPreview, draft)
					}),
					appearanceChoices(props.Text("appearance.glyphs"), props.Text("appearance.glyphs_help"), "glyphs", draft.Glyphs, props.Glyphs, "", func(value string) {
						draft.Glyphs = value
						previewAppearance(props.OnPreview, draft)
					}),
					appearanceChoices(props.Text("appearance.typeface"), props.Text("appearance.typeface_help"), "typeface", draft.Typeface, props.Typefaces, "", func(value string) {
						draft.Typeface = value
						previewAppearance(props.OnPreview, draft)
					}),
					appearanceChoices(props.Text("appearance.navigation"), props.Text("appearance.navigation_help"), "navigation", draft.Navigation, props.Navigation, "", func(value string) {
						draft.Navigation = value
						previewAppearance(props.OnPreview, draft)
					}),
					appearanceChoices(props.Text("appearance.motion"), props.Text("appearance.motion_help"), "motion", draft.Motion, props.Motions, "", func(value string) {
						draft.Motion = value
						previewAppearance(props.OnPreview, draft)
					}),
					appearanceEditActions(previewProps),
				),
				ui.CreateElement(appearancePreview, appearancePreviewProps{AppearancePageProps: previewProps, Draft: &draft}),
			),
		),
	)
}

func resetAppearanceDraft(draft *CustomerTheme, onReset func()) {
	if draft != nil {
		*draft = DefaultCustomerTheme()
	}
	if onReset != nil {
		onReset()
	}
}

// appearanceBrandSignature edits the organization's brand identity. It also
// says what the header shows right now: while the theme still holds the
// product default, HeaderBrandIdentity falls back to the admitted tenant
// name, so presenting the stored default alone told an administrator the
// header said something it did not (UXLIVE-008).
func appearanceBrandSignature(i18n I18nProps, theme CustomerTheme, tenant string, onName, onMark, onLogo func(string), picker ...BrandAssetPickerProps) ui.Node {
	nameHelp, markHelp := i18n.Text("appearance.workspace_name_help"), i18n.Text("appearance.short_mark_help")
	if headerName, headerMark := HeaderBrandIdentity(theme, tenant); headerName != theme.BrandName || headerMark != theme.BrandMark {
		if headerName != theme.BrandName {
			nameHelp = i18n.Text("appearance.workspace_name_default_help", map[string]string{"shown": headerName})
		}
		if headerMark != theme.BrandMark {
			markHelp = i18n.Text("appearance.short_mark_default_help", map[string]string{"shown": headerMark})
		}
	}
	name := html.Props{ID: "appearance-brand-name", Type: "text", Name: "brand_name", Value: theme.BrandName, MaxLength: 40, AutoComplete: "off"}
	mark := html.Props{ID: "appearance-brand-mark", Type: "text", Name: "brand_mark", Value: theme.BrandMark, MaxLength: 3, AutoComplete: "off"}
	if onName != nil {
		name.OnInput = ui.UseEvent(func(event ui.InputEvent) { onName(event.GetValue()) })
	}
	if onMark != nil {
		mark.OnInput = ui.UseEvent(func(event ui.InputEvent) { onMark(event.GetValue()) })
	}
	assetProps := BrandAssetPickerProps{I18nProps: i18n, Name: theme.BrandName, Mark: theme.BrandMark, LogoURL: theme.BrandLogoURL, Editable: true, OnChange: onLogo}
	if len(picker) > 0 {
		assetProps = picker[0]
		assetProps.I18nProps = i18n
		assetProps.Name, assetProps.Mark, assetProps.LogoURL = theme.BrandName, theme.BrandMark, theme.BrandLogoURL
		assetProps.OnChange = onLogo
	}
	return html.Fieldset(html.Props{ID: "appearance-section-brand", Class: "surface appearance-group"},
		html.Legend(html.Props{}, ui.Text(i18n.Text("appearance.brand_signature"))),
		html.P(html.Props{Class: "muted appearance-group-help"}, ui.Text(i18n.Text("appearance.brand_help"))),
		html.Div(html.Props{Class: "appearance-brand-fields"},
			ui.CreateElement(LabeledControl, LabeledControlProps{For: name.ID, Label: i18n.Text("appearance.workspace_name"), Control: html.Input(name), Help: nameHelp}),
			ui.CreateElement(LabeledControl, LabeledControlProps{For: mark.ID, Label: i18n.Text("appearance.short_mark"), Control: html.Input(mark), Help: markHelp}),
			html.Div(html.Props{Class: "appearance-brand-logo-field"}, ui.CreateElement(BrandAssetPicker, assetProps)),
		),
	)
}

func appearanceScopeGuidance(i18n I18nProps) ui.Node {
	return html.Section(html.Props{Class: "surface appearance-scope-guidance", Aria: map[string]string{"labelledby": "appearance-scope-title"}},
		html.H2(html.Props{ID: "appearance-scope-title"}, ui.Text(appearanceCopy(i18n, "appearance.scope_title", "Organization-wide appearance"))),
		html.P(html.Props{Class: "muted"}, ui.Text(appearanceCopy(i18n, "appearance.scope_detail", "Saved appearance settings apply to this organization and every signed-in user. Use system setting lets each device resolve light or dark from its own system preference; Light and Dark are organization-wide choices."))),
	)
}

func appearanceCopy(i18n I18nProps, key, fallback string) string {
	value := i18n.Text(key)
	if strings.HasPrefix(value, "⟦") && strings.HasSuffix(value, "⟧") {
		return fallback
	}
	return value
}

func appearanceChoices(title, help, name, selected string, options []AppearanceOption, class string, onChange func(string)) ui.Node {
	choices := make([]ui.Node, 0, len(options))
	for _, option := range options {
		option := option
		input := html.Props{Type: "radio", Name: name, Value: option.ID, Checked: option.ID == selected, Aria: map[string]string{"label": option.Label}}
		if onChange != nil {
			input.OnChange = ui.UseEvent(func(ui.InputEvent) { onChange(option.ID) })
		}
		content := []ui.Node{html.Input(input), html.Strong(html.Props{}, ui.Text(option.Label)), html.Small(html.Props{}, ui.Text(option.Description))}
		if name == "glyphs" {
			content = append(content, html.Span(html.Props{Class: "appearance-glyph-sample", Data: map[string]string{"hcm-glyph-sample": option.ID}, Aria: map[string]string{"hidden": "true"}}, navIcon("home"), navIcon("people"), navIcon("journeys")))
		}
		if len(option.Swatches) > 0 {
			swatches := make([]ui.Node, 0, len(option.Swatches))
			for index := range option.Swatches {
				swatches = append(swatches, html.Span(html.Props{Class: fmt.Sprintf("appearance-swatch swatch-%s-%d", option.ID, index+1), Aria: map[string]string{"hidden": "true"}}))
			}
			content = append(content, html.Span(html.Props{Class: "appearance-swatches", Aria: map[string]string{"hidden": "true"}}, swatches...))
		}
		choices = append(choices, html.Label(html.Props{Class: "appearance-choice appearance-choice-" + name + "-" + option.ID}, content...))
	}
	choiceClass := "appearance-choices"
	if class != "" {
		choiceClass += " " + class
	}
	return html.Fieldset(html.Props{ID: "appearance-section-" + name, Class: "surface appearance-group"},
		html.Legend(html.Props{}, ui.Text(title)),
		html.P(html.Props{Class: "muted appearance-group-help"}, ui.Text(help)),
		html.Div(html.Props{Class: choiceClass}, choices...),
	)
}

type appearancePreviewProps struct {
	AppearancePageProps
	Draft *CustomerTheme
}

func appearancePreview(props appearancePreviewProps) ui.Node {
	theme := NormalizeCustomerTheme(props.Theme)
	return html.Aside(html.Props{Class: "surface appearance-preview", Data: map[string]string{"hcm-preview-surface": "appearance"}, Aria: map[string]string{"label": props.Text("appearance.preview_aria")}},
		html.Div(html.Props{}, html.H2(html.Props{}, ui.Text(props.Text("appearance.preview"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("appearance.preview_help")))),
		html.Div(html.Props{Class: "appearance-preview-grid"},
			appearancePreviewWindow(props.AppearancePageProps, theme, "light", appearancePreviewLabel(props.Locale, "light"), "appearance-preview-light"),
			appearancePreviewWindow(props.AppearancePageProps, theme, "dark", appearancePreviewLabel(props.Locale, "dark"), "appearance-preview-dark"),
			appearancePreviewWindow(props.AppearancePageProps, theme, "compact", appearancePreviewLabel(props.Locale, "compact"), "appearance-preview-compact"),
		),
		html.P(html.Props{Class: "callout"}, ui.Text(props.Text("appearance.protected"))),
	)
}

// AppearanceThemeSummary uses the same admitted option IDs as the editor.
// The summary is presentation only; Save still validates the complete theme.
func AppearanceThemeSummary(locale LocaleContext, theme CustomerTheme) string {
	theme = NormalizeCustomerTheme(theme)
	palette := theme.Palette
	for _, option := range PaletteOptions() {
		if option.ID == theme.Palette {
			palette = option.Label
			break
		}
	}
	return palette + " · " + locale.Text("appearance.color_mode_"+theme.ColorMode)
}

func appearanceSectionNavigation(props I18nProps) ui.Node {
	links := []struct{ id, label string }{
		{"brand", props.Text("appearance.section_brand")},
		{"color_mode", props.Text("appearance.section_color")},
		{"shape", props.Text("appearance.section_layout")},
	}
	items := make([]ui.Node, 0, len(links))
	for _, link := range links {
		items = append(items, html.A(html.Props{Href: "#appearance-section-" + link.id, Class: "appearance-section-link"}, ui.Text(link.label)))
	}
	return html.Nav(html.Props{Class: "appearance-section-nav surface", Aria: map[string]string{"label": props.Text("appearance.section_navigation")}}, items...)
}

func appearanceEditActions(props AppearancePageProps) ui.Node {
	reset := html.Props{Class: "button secondary", Type: "button", Disabled: !props.Editable, Data: map[string]string{"hcm-action": "rollback-appearance"}}
	if props.OnReset != nil {
		reset.OnClick = ui.UseEvent(func(ui.MouseEvent) { props.OnReset() })
	}
	summary := AppearanceThemeSummary(props.Locale, props.Theme)
	return html.Div(html.Props{Class: "appearance-actions appearance-actions-sticky sticky-actions", Data: map[string]string{"hcm-sticky-actions": "true", "hcm-edit-dirty": "false"}},
		html.Div(html.Props{Class: "appearance-edit-context"},
			html.Div(html.Props{Class: "appearance-edit-current"},
				html.Small(html.Props{Class: "appearance-context-long"}, ui.Text(props.Text("appearance.current_saved"))),
				html.Small(html.Props{Class: "appearance-context-short"}, ui.Text(props.Text("appearance.current_saved_short"))),
				html.Strong(html.Props{Data: map[string]string{"hcm-theme-current": "true"}}, ui.Text(summary)),
			),
			html.Div(html.Props{Class: "appearance-edit-proposed"},
				html.Small(html.Props{Class: "appearance-context-long"}, ui.Text(props.Text("appearance.proposed"))),
				html.Small(html.Props{Class: "appearance-context-short"}, ui.Text(props.Text("appearance.proposed_short"))),
				html.Strong(html.Props{Data: map[string]string{"hcm-theme-proposed": "true"}}, ui.Text(summary)),
			),
		),
		html.Div(html.Props{Class: "appearance-edit-command"},
			html.P(html.Props{ID: "appearance-status", Class: "appearance-status", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(props.Text("appearance.no_changes"))),
			html.Button(html.Props{Class: "button secondary appearance-edit-preview", Type: "button", Aria: map[string]string{"label": props.Text("appearance.open_preview")}, OnClick: ui.UseEvent(func(ui.MouseEvent) { requestAppearancePreview() })}, navIcon("expand")),
			html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: true, Data: map[string]string{"hcm-action": "save-appearance", "hcm-editable": fmt.Sprint(props.Editable)}, Raw: map[string]any{"aria-describedby": "appearance-status"}},
				html.Span(html.Props{Class: "appearance-label-long"}, ui.Text(props.Text("appearance.save"))),
				html.Span(html.Props{Class: "appearance-label-short"}, ui.Text(props.Text("appearance.save_short"))),
			),
			html.Button(reset, ui.Text(props.Text("appearance.restore"))),
		),
	)
}

func appearancePreviewLauncher(props appearancePreviewProps) ui.Node {
	open := ui.UseState(false)
	initialPage := PageHome
	if len(props.PreviewPages) > 0 {
		initialPage = props.PreviewPages[0].ID
	}
	page := ui.UseState(initialPage)
	mode := ui.UseState("current")
	useDrawerFocusTrap("appearance-preview-dialog", "appearance-preview-open", open.Get())
	useAppearancePreviewScrollReset(page.Get(), open.Get())
	return html.Div(html.Props{Class: "appearance-preview-launcher"},
		html.Button(html.Props{ID: "appearance-preview-open", Class: "button secondary appearance-preview-open", Type: "button", Aria: map[string]string{"haspopup": "dialog", "expanded": fmt.Sprint(open.Get()), "controls": "appearance-preview-dialog"}, OnClick: ui.UseEvent(func(ui.MouseEvent) { open.Set(true) })}, navIcon("expand"), ui.Text(props.Text("appearance.open_preview"))),
		appearancePreviewDialog(props, appearancePreviewDialogState{
			open: open.Get(), page: page.Get(), mode: mode.Get(),
			selectPage: func(next PageID) { page.Set(next) }, selectMode: func(next string) { mode.Set(next) }, close: func() { open.Set(false) },
		}),
	)
}

type appearancePreviewDialogState struct {
	open       bool
	page       PageID
	mode       string
	selectPage func(PageID)
	selectMode func(string)
	close      func()
}

func appearancePreviewDialog(props appearancePreviewProps, state appearancePreviewDialogState) ui.Node {
	dialog := html.Props{ID: "appearance-preview-dialog", Class: "appearance-preview-overlay", Raw: map[string]any{"role": "dialog", "aria-modal": "true", "aria-labelledby": "appearance-preview-dialog-title", "tabindex": "-1"}, OnKeyDown: ui.UseEvent(func(event ui.KeyboardEvent) {
		if drawerEscapeCloses(event.GetKey()) {
			state.close()
		}
	})}
	if !state.open {
		dialog.Raw["hidden"] = "hidden"
		dialog.Raw["aria-hidden"] = "true"
	}
	tabs := make([]ui.Node, 0, len(props.PreviewPages))
	currentPageLabel := ""
	for _, destination := range props.PreviewPages {
		target := destination.ID
		button := html.Props{Type: "button", Class: "appearance-preview-tab", Data: map[string]string{"hcm-preview-page": string(target)}}
		if state.page == target {
			currentPageLabel = destination.Label
			button.Class += " is-selected"
			button.Raw = map[string]any{"aria-current": "page"}
		}
		button.OnClick = ui.UseEvent(func(ui.MouseEvent) { state.selectPage(target) })
		tabs = append(tabs, html.Button(button, ui.Text(destination.Label)))
	}
	var content ui.Node = html.P(html.Props{Class: "muted"}, ui.Text(props.Text("appearance.preview_unavailable")))
	if state.open && props.RenderPreview != nil {
		content = props.RenderPreview(state.page)
	}
	theme := props.Theme
	if props.Draft != nil {
		theme = NormalizeCustomerTheme(*props.Draft)
	}
	brandName, brandMark := HeaderBrandIdentity(theme, props.PreviewTenant)
	return html.Div(dialog,
		html.Div(html.Props{Class: "appearance-preview-modal"},
			html.Div(html.Props{Class: "appearance-preview-modal-header"},
				html.Div(html.Props{}, html.H2(html.Props{ID: "appearance-preview-dialog-title"}, ui.Text(props.Text("appearance.preview_dialog_title"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("appearance.preview_dialog_help")))),
				html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) { state.close() })}, ui.Text(props.Text("appearance.close_preview"))),
			),
			html.Div(html.Props{Class: "appearance-preview-modal-tabs", Aria: map[string]string{"label": props.Text("appearance.preview_pages")}}, tabs...),
			html.Div(html.Props{Class: "appearance-preview-toolbar"},
				appearancePreviewChoices(props, "appearance.preview_theme", "mode", []string{"current", "light", "dark"}, state.mode, state.selectMode),
			),
			html.Div(html.Props{Class: "appearance-preview-live", Data: map[string]string{"hcm-preview-live": "true"}},
				html.Div(html.Props{Class: "appearance-preview-scene", Data: map[string]string{"hcm-preview-color-mode": state.mode}},
					html.Div(html.Props{Class: "appearance-preview-live-header"}, ui.CreateElement(BrandLogo, BrandLogoProps{Name: brandName, Mark: brandMark, LogoURL: theme.BrandLogoURL}), html.Span(html.Props{Class: "appearance-preview-page-name"}, ui.Text(currentPageLabel))),
					html.Div(html.Props{Class: "appearance-preview-live-content", Raw: map[string]any{"inert": ""}}, content),
				),
			),
			html.P(html.Props{Class: "appearance-preview-modal-note"}, ui.Text(props.Text("appearance.preview_read_only"))),
		),
	)
}

func appearancePreviewChoices(props appearancePreviewProps, labelKey, kind string, options []string, selected string, selectOption func(string)) ui.Node {
	buttons := make([]ui.Node, 0, len(options))
	for _, option := range options {
		option := option
		className := "appearance-preview-option"
		if option == selected {
			className += " is-selected"
		}
		buttons = append(buttons, html.Button(html.Props{
			Class: className, Type: "button", Data: map[string]string{"hcm-preview-" + kind: option},
			Raw:     map[string]any{"aria-pressed": fmt.Sprint(option == selected)},
			OnClick: ui.UseEvent(func(ui.MouseEvent) { selectOption(option) }),
		}, ui.Text(props.Text("appearance.preview_"+option))))
	}
	return html.Div(html.Props{Class: "appearance-preview-choice-group", Aria: map[string]string{"label": props.Text(labelKey)}},
		html.Span(html.Props{Class: "appearance-preview-choice-label"}, ui.Text(props.Text(labelKey))),
		html.Div(html.Props{Class: "appearance-preview-options"}, buttons...),
	)
}

func appearancePreviewLabel(locale LocaleContext, mode string) string {
	switch locale.normalized().Resolved {
	case "de-DE":
		switch mode {
		case "dark":
			return "Dunkle Vorschau"
		case "compact":
			return "Kompakte Vorschau"
		default:
			return "Helle Vorschau"
		}
	case "ar":
		switch mode {
		case "dark":
			return "معاينة داكنة"
		case "compact":
			return "معاينة مدمجة"
		default:
			return "معاينة فاتحة"
		}
	default:
		switch mode {
		case "dark":
			return "Dark preview"
		case "compact":
			return "Compact preview"
		default:
			return "Light preview"
		}
	}
}

func appearancePreviewWindow(props AppearancePageProps, theme CustomerTheme, mode, label, class string) ui.Node {
	previewTheme := theme
	previewTheme.ColorMode = mode
	if mode == "compact" {
		previewTheme.Density = "compact"
		previewTheme.ColorMode = "light"
	}
	return html.Div(html.Props{Class: "appearance-preview-window " + class, Data: map[string]string{"hcm-preview-mode": mode, "hcm-preview-color-mode": previewTheme.ColorMode, "hcm-preview-density": previewTheme.Density}, Aria: map[string]string{"label": label}},
		html.H3(html.Props{}, ui.Text(label)),
		html.Div(html.Props{Class: "appearance-preview-bar"},
			ui.CreateElement(BrandLogo, BrandLogoProps{Name: previewTheme.BrandName, Mark: previewTheme.BrandMark, LogoURL: previewTheme.BrandLogoURL, Class: "appearance-preview-logo"}),
		),
		html.Div(html.Props{Class: "appearance-preview-body"},
			html.Div(html.Props{Class: "appearance-preview-nav"}, navIcon("home"), navIcon("people"), navIcon("journeys")),
			html.Div(html.Props{Class: "appearance-preview-content"},
				html.H4(html.Props{}, ui.Text(props.Text("page.people.title"))),
				html.Div(html.Props{Class: "appearance-preview-card"},
					html.Strong(html.Props{}, ui.Text(props.Text("page.journeys.title"))),
					html.Small(html.Props{}, ui.Text(props.Text("page.work.title"))),
					html.Em(html.Props{Class: "appearance-preview-pill"}, ui.Text(props.Text("page.people.label"))),
				),
			),
		),
	)
}

func preventFormSubmit(save func(CustomerTheme), draft *CustomerTheme) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		save(NormalizeCustomerTheme(*draft))
	})
}

func previewAppearance(preview func(CustomerTheme), theme CustomerTheme) {
	if preview != nil {
		preview(NormalizeCustomerTheme(theme))
	}
}
