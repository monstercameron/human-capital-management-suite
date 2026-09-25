package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// BrandLogoProps is the narrow presentation contract for the customer-owned
// identity slot. The server supplies an already-governed same-origin asset;
// the component never decides tenant identity or fetch policy.
type BrandLogoProps struct {
	Name           string
	AccessibleName string
	Mark           string
	LogoURL        string
	Class          string
}

// BrandAssetPickerProps is the presentation contract for an organization
// logo. Upload is deliberately an adapter callback: this package does not
// receive file bytes, mint asset URLs, or bypass the governed asset service.
// Callers must return a server-approved same-origin content digest reference
// before setting LogoURL or persisting a theme.
type BrandAssetPickerProps struct {
	I18nProps
	Name             string
	Mark             string
	LogoURL          string
	PublishedLogoURL string
	Editable         bool
	Status           string
	OnChange         func(string)
	OnUpload         func(string, []byte, func(string, error))
	OnPreview        func(string)
	OnLoad           func(int, func([]BrandAssetOption, int, error))
	OnRemove         func(int, func(error))
	OnRollback       func(int, int, func(string, error))
	Approved         []BrandAssetOption
}

// BrandAssetOption is a server-registered, tenant-approved logo choice.
type BrandAssetOption struct {
	Label        string
	URL          string
	Revision     int
	HeadRevision int
	CanRollback  bool
	CanRemove    bool
	Digest       string
	Width        int
	Height       int
}

// GovernedBrandAssetAccept is the narrow browser hint shared by the picker
// and the server-side allowlist. It is not authorization; the asset service
// still validates content type, size, digest, and tenant ownership.
const GovernedBrandAssetAccept = "image/png,image/jpeg,image/webp"

// BrandAssetPicker renders upload, preview, remove, and rollback affordances
// with a text fallback so the workspace remains usable when an image is
// unavailable. Every action is optional and degrades to a plain server form.
func BrandAssetPicker(props BrandAssetPickerProps) ui.Node {
	assets := ui.UseState(append([]BrandAssetOption(nil), props.Approved...))
	selected := ui.UseState(normalizedBrandLogoURL(props.LogoURL))
	status := ui.UseState(strings.TrimSpace(props.Status))
	busy := ui.UseState(false)
	nextBefore := ui.UseState(0)
	ui.UseMount(func() func() {
		if props.OnLoad != nil {
			props.OnLoad(0, func(options []BrandAssetOption, next int, err error) {
				if err != nil {
					status.Set(props.Text("appearance.asset_status_history_failed"))
					return
				}
				assets.Set(options)
				nextBefore.Set(next)
			})
		}
		return nil
	})
	logoURL := selected.Get()
	var selectedOption BrandAssetOption
	for _, candidate := range assets.Get() {
		if candidate.URL != "" && normalizedBrandLogoURL(candidate.URL) == logoURL {
			selectedOption = candidate
			if candidate.CanRemove {
				break
			}
		}
	}
	name := normalizedBrandText(props.Name, 40, DefaultCustomerTheme().BrandName, false)
	mark := normalizedBrandText(props.Mark, 3, DefaultCustomerTheme().BrandMark, true)
	label := props.Text("appearance.company_logo")
	help := props.Text("appearance.company_logo_help")
	actions := make([]ui.Node, 0, 4)
	for _, option := range assets.Get() {
		option := option
		approvedURL := normalizedBrandLogoURL(option.URL)
		if approvedURL == "" || props.OnChange == nil {
			continue
		}
		choose := html.Props{Class: "button secondary", Type: "button", Disabled: !props.Editable, Data: map[string]string{"hcm-asset-action": "choose", "hcm-asset-ref": approvedURL}}
		choose.OnClick = ui.UseEvent(func(ui.MouseEvent) { selected.Set(approvedURL); props.OnChange(approvedURL) })
		actions = append(actions, html.Button(choose, ui.Text(option.Label)))
	}
	if props.OnUpload != nil {
		upload := props.OnUpload
		choose := html.Props{Class: "button secondary brand-asset-upload", Type: "button", Disabled: !props.Editable || busy.Get(), Data: map[string]string{"hcm-asset-action": "upload"}, Aria: map[string]string{"describedby": "appearance-brand-logo-help"}}
		choose.OnClick = ui.UseEvent(func(ui.MouseEvent) {
			status.Set(props.Text("appearance.logo_upload"))
			ui.PickFile(GovernedBrandAssetAccept, func(file ui.PickedFile) {
				busy.Set(true)
				status.Set(props.Text("appearance.asset_status_uploading"))
				upload(file.Name, file.Data, func(url string, err error) {
					busy.Set(false)
					if err != nil {
						status.Set(props.Text("appearance.asset_status_upload_failed"))
						return
					}
					status.Set(props.Text("appearance.asset_status_uploaded"))
					selected.Set(normalizedBrandLogoURL(url))
					if props.OnChange != nil {
						props.OnChange(url)
					}
					if props.OnLoad != nil {
						props.OnLoad(0, func(options []BrandAssetOption, next int, loadErr error) {
							if loadErr == nil {
								assets.Set(options)
								nextBefore.Set(next)
							}
						})
					}
				})
			})
		})
		actions = append(actions, html.Button(choose, ui.Text(props.Text("appearance.logo_upload"))))
	}
	if props.OnPreview != nil {
		preview := html.Props{Class: "button secondary", Type: "button", Disabled: !props.Editable || logoURL == "", Data: map[string]string{"hcm-asset-action": "preview"}}
		preview.OnClick = ui.UseEvent(func(ui.MouseEvent) { props.OnPreview(logoURL) })
		actions = append(actions, html.Button(preview, ui.Text(props.Text("appearance.logo_preview"))))
	}
	if props.OnRemove != nil {
		remove := html.Props{Class: "button secondary", Type: "button", Disabled: !props.Editable || busy.Get() || logoURL == "" || !selectedOption.CanRemove, Data: map[string]string{"hcm-asset-action": "remove"}}
		remove.OnClick = ui.UseEvent(func(ui.MouseEvent) {
			busy.Set(true)
			status.Set(props.Text("appearance.asset_status_removing"))
			props.OnRemove(selectedOption.HeadRevision, func(err error) {
				busy.Set(false)
				if err != nil {
					status.Set(props.Text("appearance.asset_status_remove_failed"))
					return
				}
				status.Set(props.Text("appearance.asset_status_removed"))
				selected.Set("")
				if props.OnChange != nil {
					props.OnChange("")
				}
				if props.OnLoad != nil {
					props.OnLoad(0, func(options []BrandAssetOption, next int, loadErr error) {
						if loadErr == nil {
							assets.Set(options)
							nextBefore.Set(next)
						}
					})
				}
			})
		})
		actions = append(actions, html.Button(remove, ui.Text(props.Text("appearance.logo_remove"))))
	}
	if props.OnRollback != nil {
		for _, option := range assets.Get() {
			if option.Revision < 1 || !option.CanRollback {
				continue
			}
			option := option
			rollback := html.Props{Class: "button secondary", Type: "button", Disabled: !props.Editable || busy.Get(), Data: map[string]string{"hcm-asset-action": "rollback", "hcm-asset-revision": fmt.Sprint(option.Revision)}}
			rollback.OnClick = ui.UseEvent(func(ui.MouseEvent) {
				busy.Set(true)
				status.Set(props.Text("appearance.asset_status_restoring"))
				props.OnRollback(option.Revision, option.HeadRevision, func(url string, err error) {
					busy.Set(false)
					if err != nil {
						status.Set(props.Text("appearance.asset_status_restore_failed"))
						return
					}
					status.Set(props.Text("appearance.asset_status_restored"))
					selected.Set(normalizedBrandLogoURL(url))
					if props.OnChange != nil {
						props.OnChange(url)
					}
					if props.OnLoad != nil {
						props.OnLoad(0, func(options []BrandAssetOption, next int, loadErr error) {
							if loadErr == nil {
								assets.Set(options)
								nextBefore.Set(next)
							}
						})
					}
				})
			})
			actions = append(actions, html.Button(rollback, ui.Text(fmt.Sprintf("%s · %d", props.Text("appearance.logo_undo"), option.Revision))))
		}
	}
	if props.OnLoad != nil && nextBefore.Get() > 0 {
		loadMore := html.Props{Class: "button secondary", Type: "button", Disabled: !props.Editable || busy.Get(), Data: map[string]string{"hcm-asset-action": "load-more"}}
		loadMore.OnClick = ui.UseEvent(func(ui.MouseEvent) {
			before := nextBefore.Get()
			busy.Set(true)
			props.OnLoad(before, func(options []BrandAssetOption, next int, err error) {
				busy.Set(false)
				if err != nil {
					status.Set(props.Text("appearance.asset_status_history_failed"))
					return
				}
				assets.Set(append(assets.Get(), options...))
				nextBefore.Set(next)
			})
		})
		actions = append(actions, html.Button(loadMore, ui.Text(props.Text("appearance.asset_load_more"))))
	}

	governed := props.OnUpload != nil && props.OnLoad != nil && props.OnPreview != nil && props.OnRemove != nil && props.OnRollback != nil
	return html.Fieldset(html.Props{Class: "brand-asset-picker", Data: map[string]string{"hcm-brand-asset-picker": "true", "hcm-brand-asset-state": brandAssetState(logoURL), "hcm-brand-asset-status": strings.TrimSpace(status.Get()), "hcm-brand-asset-governed": brandAssetBool(governed)}, Disabled: !props.Editable},
		html.Legend(html.Props{}, ui.Text(label)),
		html.Div(html.Props{Class: "brand-asset-preview", Data: map[string]string{"hcm-brand-asset-preview": "true"}},
			ui.CreateElement(BrandLogo, BrandLogoProps{Name: name, Mark: mark, LogoURL: logoURL, Class: "brand-asset-preview-logo"}),
			html.P(html.Props{Class: "muted"}, ui.Text(help)),
		),
		brandAssetVariantPreviews(name, mark, logoURL, props.I18nProps),
		brandAssetDiffSummary(props.PublishedLogoURL, logoURL, assets.Get(), props.I18nProps),
		html.Div(html.Props{Class: "brand-asset-controls"},
			brandAssetReference(props, logoURL, governed),
			html.Small(html.Props{ID: "appearance-brand-logo-help", Class: "muted"}, ui.Text(props.Text("appearance.logo_approval_help"))),
			html.Small(html.Props{Class: "appearance-logo-action-help muted"}, ui.Text(props.Text("appearance.logo_link_help"))),
			html.Div(html.Props{Class: "brand-asset-actions"}, actions...),
		),
		html.P(html.Props{Class: "brand-asset-status", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(status.Get())),
	)
}

func brandAssetDiffSummary(beforeURL, afterURL string, assets []BrandAssetOption, i18n I18nProps) ui.Node {
	if beforeURL == afterURL {
		return html.P(html.Props{Class: "muted", Data: map[string]string{"hcm-brand-asset-diff": "unchanged"}}, ui.Text(i18n.Text("appearance.asset_diff_unchanged")))
	}
	lookup := func(url string) (BrandAssetOption, bool) {
		for _, item := range assets {
			if item.URL == url && url != "" {
				return item, true
			}
		}
		return BrandAssetOption{}, false
	}
	before, beforeOK := lookup(beforeURL)
	after, afterOK := lookup(afterURL)
	value := func(url string, ok bool, item BrandAssetOption, field string) string {
		if !ok {
			if field == "url" && url == "" {
				return i18n.Text("appearance.asset_none")
			}
			return i18n.Text("appearance.asset_unavailable")
		}
		switch field {
		case "revision":
			return fmt.Sprint(item.Revision)
		case "file":
			return item.Label
		case "digest":
			return item.Digest
		case "dimensions":
			return fmt.Sprintf("%d × %d", item.Width, item.Height)
		case "url":
			return item.URL
		}
		return ""
	}
	fields := []struct{ key, label string }{{"revision", "appearance.asset_diff_revision"}, {"file", "appearance.asset_diff_file"}, {"digest", "appearance.asset_diff_digest"}, {"dimensions", "appearance.asset_diff_dimensions"}, {"url", "appearance.asset_diff_url"}}
	rows := make([]ui.Node, 0, len(fields))
	for _, field := range fields {
		from, to := value(beforeURL, beforeOK, before, field.key), value(afterURL, afterOK, after, field.key)
		if from == to {
			continue
		}
		rows = append(rows, html.Div(html.Props{Class: "brand-asset-diff-row", Data: map[string]string{"hcm-brand-asset-diff-field": field.key}}, html.Strong(html.Props{}, ui.Text(i18n.Text(field.label))), html.Span(html.Props{}, ui.Text(from+" → "+to))))
	}
	return html.Section(html.Props{Class: "brand-asset-diff", Data: map[string]string{"hcm-brand-asset-diff": "changed"}}, html.H3(html.Props{}, ui.Text(i18n.Text("appearance.asset_diff_title"))), html.Div(html.Props{Class: "brand-asset-diff-fields"}, rows...))
}

func brandAssetVariantPreviews(name, mark, logoURL string, i18n I18nProps) ui.Node {
	proxyURL := ""
	if strings.HasPrefix(logoURL, "/workspace/brand-assets/") {
		proxyURL = logoURL + "?variant=proxy"
	}
	proxy := html.Img(html.Props{Src: proxyURL, Alt: "", Width: "32", Height: "32", Loading: "lazy", Class: "brand-asset-favicon-preview", Aria: map[string]string{"hidden": "true"}})
	compactImage := html.Img(html.Props{Src: proxyURL, Alt: "", Width: "30", Height: "30", Loading: "lazy", Class: "brand-asset-compact-preview", Aria: map[string]string{"hidden": "true"}})
	return html.Div(html.Props{Class: "brand-asset-variant-previews", Data: map[string]string{"hcm-asset-variants": "shell favicon compact contrast"}},
		html.Div(html.Props{Class: "brand-asset-variant-shell", Data: map[string]string{"hcm-asset-variant": "shell"}},
			html.Small(html.Props{}, ui.Text(i18n.Text("appearance.logo_preview"))),
			ui.CreateElement(BrandLogo, BrandLogoProps{Name: name, Mark: mark, LogoURL: logoURL}),
		),
		html.Div(html.Props{Class: "brand-asset-variant-favicon", Data: map[string]string{"hcm-asset-variant": "favicon"}},
			html.Small(html.Props{}, ui.Text(i18n.Text("appearance.asset_variant_favicon"))), proxy,
		),
		html.Div(html.Props{Class: "brand-asset-variant-compact", Data: map[string]string{"hcm-asset-variant": "compact"}},
			html.Small(html.Props{}, ui.Text(i18n.Text("appearance.asset_variant_compact"))),
			compactImage, html.Span(html.Props{Class: "wordmark-mark"}, ui.Text(mark)), html.Span(html.Props{}, ui.Text(name)),
		),
		html.Div(html.Props{Class: "brand-asset-variant-contrast-light", Data: map[string]string{"hcm-asset-variant": "contrast-light"}},
			html.Small(html.Props{}, ui.Text(i18n.Text("appearance.asset_variant_light"))), ui.CreateElement(BrandLogo, BrandLogoProps{Name: name, Mark: mark, LogoURL: logoURL}),
		),
		html.Div(html.Props{Class: "brand-asset-variant-contrast-dark", Data: map[string]string{"hcm-asset-variant": "contrast-dark"}},
			html.Small(html.Props{}, ui.Text(i18n.Text("appearance.asset_variant_dark"))), ui.CreateElement(BrandLogo, BrandLogoProps{Name: name, Mark: mark, LogoURL: logoURL}),
		),
	)
}

func brandAssetBool(value bool) string {
	return fmt.Sprintf("%t", value)
}

func brandAssetReference(props BrandAssetPickerProps, _ string, _ bool) ui.Node {
	return html.Small(html.Props{Class: "brand-asset-reference muted", Data: map[string]string{"hcm-brand-asset-reference": "approved"}}, ui.Text(props.Text("appearance.logo_link_help")))
}

func brandAssetState(ref string) string {
	if normalizedBrandLogoURL(ref) == "" {
		return "empty"
	}
	return "configured"
}

// BrandLogo renders a stable header-sized logo slot. The text signature is
// always present as a resilient compact-navigation and load-failure fallback,
// while one visually-hidden name gives the surrounding home link a reliable
// accessible name whether the logo is an image or text.
func BrandLogo(props BrandLogoProps) ui.Node {
	name := normalizedBrandText(props.Name, 40, DefaultCustomerTheme().BrandName, false)
	accessibleName := normalizedBrandText(props.AccessibleName, 120, name, false)
	mark := normalizedBrandText(props.Mark, 3, DefaultCustomerTheme().BrandMark, true)
	logoURL := normalizedBrandLogoURL(props.LogoURL)
	state := "fallback"
	imageProps := html.Props{
		Class: "brand-logo-image", Width: "180", Height: "40", Loading: "eager",
		Aria: map[string]string{"hidden": "true"},
		Data: map[string]string{"hcm-brand-logo": ""},
		Raw:  map[string]any{"alt": "", "decoding": "async"},
	}
	if logoURL != "" {
		state = "configured"
		imageProps.Src = logoURL
	}
	className := strings.TrimSpace("brand-logo-slot " + props.Class)
	return html.Span(html.Props{Class: className, Data: map[string]string{"hcm-brand-logo-slot": "", "hcm-brand-logo-state": state}},
		html.Img(imageProps),
		html.Span(html.Props{Class: "brand-logo-fallback", Aria: map[string]string{"hidden": "true"}},
			html.Span(html.Props{Class: "wordmark-mark", Data: map[string]string{"hcm-brand-mark": ""}}, ui.Text(mark)),
			html.Span(html.Props{Class: "wordmark-label", Data: map[string]string{"hcm-brand-name": ""}}, ui.Text(name)),
		),
		html.Span(html.Props{Class: "sr-only", Data: map[string]string{"hcm-brand-name": ""}}, ui.Text(accessibleName)),
	)
}
