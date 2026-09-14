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
// Callers must return a server-approved /workspace/assets/<filename> reference
// before setting LogoURL or persisting a theme.
type BrandAssetPickerProps struct {
	I18nProps
	Name       string
	Mark       string
	LogoURL    string
	Editable   bool
	Status     string
	OnChange   func(string)
	OnUpload   func(string)
	OnPreview  func(string)
	OnRemove   func()
	OnRollback func()
}

// GovernedBrandAssetAccept is the narrow browser hint shared by the picker
// and the server-side allowlist. It is not authorization; the asset service
// still validates content type, size, digest, and tenant ownership.
const GovernedBrandAssetAccept = "image/svg+xml,image/png,image/jpeg,image/webp"

// BrandAssetPicker renders upload, preview, remove, and rollback affordances
// with a text fallback so the workspace remains usable when an image is
// unavailable. Every action is optional and degrades to a plain server form.
func BrandAssetPicker(props BrandAssetPickerProps) ui.Node {
	logoURL := normalizedBrandLogoURL(props.LogoURL)
	name := normalizedBrandText(props.Name, 40, DefaultCustomerTheme().BrandName, false)
	mark := normalizedBrandText(props.Mark, 3, DefaultCustomerTheme().BrandMark, true)
	label := props.Text("appearance.company_logo")
	help := props.Text("appearance.company_logo_help")
	actions := make([]ui.Node, 0, 4)
	if props.OnUpload != nil {
		fileProps := html.Props{ID: "appearance-brand-logo-file", Type: "file", Disabled: !props.Editable, Raw: map[string]any{"accept": GovernedBrandAssetAccept, "aria-describedby": "appearance-brand-logo-help"}}
		fileProps.OnChange = ui.UseEvent(func(event ui.InputEvent) { props.OnUpload(event.GetValue()) })
		actions = append(actions,
			html.Label(html.Props{Class: "button secondary brand-asset-upload", For: fileProps.ID, Data: map[string]string{"hcm-asset-action": "upload"}}, ui.Text(props.Text("appearance.logo_upload")), html.Input(fileProps)),
		)
	}
	if props.OnPreview != nil {
		preview := html.Props{Class: "button secondary", Type: "button", Disabled: !props.Editable || logoURL == "", Data: map[string]string{"hcm-asset-action": "preview"}}
		preview.OnClick = ui.UseEvent(func(ui.MouseEvent) { props.OnPreview(logoURL) })
		actions = append(actions, html.Button(preview, ui.Text(props.Text("appearance.logo_preview"))))
	}
	if props.OnRemove != nil {
		remove := html.Props{Class: "button secondary", Type: "button", Disabled: !props.Editable || logoURL == "", Data: map[string]string{"hcm-asset-action": "remove"}}
		remove.OnClick = ui.UseEvent(func(ui.MouseEvent) { props.OnRemove() })
		actions = append(actions, html.Button(remove, ui.Text(props.Text("appearance.logo_remove"))))
	}
	if props.OnRollback != nil {
		rollback := html.Props{Class: "button secondary", Type: "button", Disabled: !props.Editable, Data: map[string]string{"hcm-asset-action": "rollback"}}
		rollback.OnClick = ui.UseEvent(func(ui.MouseEvent) { props.OnRollback() })
		actions = append(actions, html.Button(rollback, ui.Text(props.Text("appearance.logo_undo"))))
	}

	governed := props.OnUpload != nil && props.OnPreview != nil && props.OnRemove != nil && props.OnRollback != nil
	return html.Fieldset(html.Props{Class: "brand-asset-picker", Data: map[string]string{"hcm-brand-asset-picker": "true", "hcm-brand-asset-state": brandAssetState(logoURL), "hcm-brand-asset-status": strings.TrimSpace(props.Status), "hcm-brand-asset-governed": brandAssetBool(governed)}, Disabled: !props.Editable},
		html.Legend(html.Props{}, ui.Text(label)),
		html.Div(html.Props{Class: "brand-asset-preview", Data: map[string]string{"hcm-brand-asset-preview": "true"}},
			ui.CreateElement(BrandLogo, BrandLogoProps{Name: name, Mark: mark, LogoURL: logoURL, Class: "brand-asset-preview-logo"}),
			html.P(html.Props{Class: "muted"}, ui.Text(help)),
		),
		html.Div(html.Props{Class: "brand-asset-controls"},
			brandAssetReference(props, logoURL, governed),
			html.Small(html.Props{ID: "appearance-brand-logo-help", Class: "muted"}, ui.Text(props.Text("appearance.logo_approval_help"))),
			html.Small(html.Props{Class: "appearance-logo-action-help muted"}, ui.Text(props.Text("appearance.logo_link_help"))),
			html.Div(html.Props{Class: "brand-asset-actions"}, actions...),
		),
		html.P(html.Props{Class: "brand-asset-status", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(props.Status)),
	)
}

func brandAssetBool(value bool) string {
	return fmt.Sprintf("%t", value)
}

func brandAssetReference(props BrandAssetPickerProps, logoURL string, governed bool) ui.Node {
	if governed {
		return html.Small(html.Props{Class: "brand-asset-reference muted", Data: map[string]string{"hcm-brand-asset-reference": "approved"}}, ui.Text(props.Text("appearance.logo_approved_reference")))
	}
	// Keep the legacy reference input only when no complete adapter exists. A
	// partial adapter must never turn a browser filename into persisted state.
	return ui.CreateElement(LabeledControl, LabeledControlProps{
		For: "appearance-brand-logo", Label: props.Text("appearance.logo_link_label"), Control: html.Input(brandAssetPathProps(props, logoURL)),
	})
}

func brandAssetPathProps(props BrandAssetPickerProps, logoURL string) html.Props {
	input := html.Props{ID: "appearance-brand-logo", Type: "url", Name: "brand_logo_url", Value: logoURL, MaxLength: 240, AutoComplete: "off", Disabled: !props.Editable, Aria: map[string]string{"describedby": "appearance-brand-logo-help"}, Raw: map[string]any{"inputmode": "url"}}
	if props.OnChange != nil {
		input.OnInput = ui.UseEvent(func(event ui.InputEvent) { props.OnChange(event.GetValue()) })
	}
	return input
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
