package tokens

import (
	"fmt"
	"strings"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// This file builds the WorkspaceCSS stylesheets from typed GWC declarations.
// Rule order matches the original consts; the canonical serializer sorts
// declarations alphabetically and appends trailing semicolons (a byte change
// only, proven canonical per sheet with /tmp/cssdiff.py).
//
// Each @media query is emitted as ONE block via atRule(header, inner) where
// inner is a separately harvested typed sheet: per-selector mediaRule would
// emit one @media block per selector, which would break the single-block
// shape the mode-contract tests read.

// cssVariablesTyped renders the palette as a `:root` custom-property block.
// Iteration order matches CSSVariables.
func cssVariablesTyped() string {
	return buildTypedSheet(declareCSSVariables)
}

func declareCSSVariables() {
	p := Palette
	all := []Color{p.Text, p.TextMuted, p.Background, p.Surface, p.Border, p.Accent, p.AccentText, p.Success, p.Warning, p.WarningText, p.Danger, p.DangerText, p.Info}
	parts := make([]any, 0, len(all))
	for _, c := range all {
		parts = append(parts, gwccss.Custom("color-"+c.Name, c.Hex))
	}
	// Typography is intentionally expressed as semantic custom properties.
	// The stacks contain only broadly available system faces, so an unavailable
	// customer font cannot leave text with a missing family or unstable fallback.
	parts = append(parts,
		gwccss.Custom("font-family-sans", `ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif`),
		gwccss.Custom("font-family-mono", `ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace`),
		gwccss.Custom("font-size-display", `clamp(2rem,1.5rem + 2vw,3.5rem)`),
		gwccss.Custom("font-size-page-title", `clamp(1.75rem,1.35rem + 1.5vw,2.5rem)`),
		gwccss.Custom("font-size-section", `clamp(1.25rem,1.1rem + .6vw,1.75rem)`),
		gwccss.Custom("font-size-body", `1rem`),
		gwccss.Custom("font-size-label", `clamp(.875rem,.84rem + .15vw,1rem)`),
		gwccss.Custom("font-size-helper", `clamp(.8125rem,.78rem + .12vw,.9375rem)`),
		gwccss.Custom("font-size-table", `clamp(.8125rem,.78rem + .12vw,.9375rem)`),
		gwccss.Custom("font-size-code", `clamp(.8125rem,.78rem + .12vw,.9375rem)`),
		gwccss.Custom("line-height-display", `1.1`),
		gwccss.Custom("line-height-heading", `1.25`),
		gwccss.Custom("line-height-body", `1.5`),
		gwccss.Custom("line-height-tight", `1.35`),
		gwccss.Custom("measure-readable", `70ch`),
		gwccss.Custom("measure-prose", `65ch`),
		// Shape, boundary, surface and depth are semantic contracts. Components
		// consume these names so customer presets change geometry without
		// introducing page-specific radii or shadows.
		gwccss.Custom("radius-control", `8px`),
		gwccss.Custom("radius-surface", `12px`),
		gwccss.Custom("radius-overlay", `12px`),
		gwccss.Custom("radius-status", `6px`),
		gwccss.Custom("radius-brand", `16px`),
		gwccss.Custom("border-width-boundary", `1px`),
		gwccss.Custom("border-width-focus", `2px`),
		gwccss.Custom("border-width-status", `2px`),
		gwccss.Custom("border-style-default", `solid`),
		gwccss.Custom("border-color-boundary", `var(--color-border)`),
		gwccss.Custom("border-color-control", `var(--color-border)`),
		gwccss.Custom("border-color-focus", `var(--color-accent)`),
		gwccss.Custom("border-color-selection", `var(--color-accent)`),
		gwccss.Custom("border-color-success", `var(--color-success)`),
		gwccss.Custom("border-color-warning", `var(--color-warning)`),
		gwccss.Custom("border-color-danger", `var(--color-danger)`),
		gwccss.Custom("surface-canvas", `var(--color-background)`),
		gwccss.Custom("surface-raised", `var(--color-surface)`),
		gwccss.Custom("surface-overlay", `var(--color-background)`),
		gwccss.Custom("surface-selected", `var(--color-surface)`),
		gwccss.Custom("elevation-flat", `none`),
		gwccss.Custom("elevation-overlay", `0 8px 24px rgba(26,29,41,.12)`),
		gwccss.Custom("elevation-dialog", `0 16px 40px rgba(26,29,41,.16)`),
	)
	declareGlobal(`:root`, parts...)
}

// workspaceBasePreTyped holds the WorkspaceCSS body rules authored before
// the 640px breakpoint (universal through main), in original order.
func workspaceBasePreTyped() string {
	return buildTypedSheet(declareWorkspaceBasePre)
}

// shapeContractsTyped contains the closed customer shape presets and shared
// surface contracts. The attributes are written by the production theme
// adapter; components only consume the semantic variables below.
func shapeContractsTyped() string {
	return buildTypedSheet(func() {
		declareGlobal(`:root[data-hcm-shape="precise"]`, gwccss.Custom("radius-control", `4px`), gwccss.Custom("radius-surface", `6px`), gwccss.Custom("radius-overlay", `6px`), gwccss.Custom("radius-status", `4px`), gwccss.Custom("radius-brand", `8px`))
		declareGlobal(`:root[data-hcm-shape="rounded"]`, gwccss.Custom("radius-control", `12px`), gwccss.Custom("radius-surface", `18px`), gwccss.Custom("radius-overlay", `18px`), gwccss.Custom("radius-status", `8px`), gwccss.Custom("radius-brand", `20px`))
		declareGlobal(`.surface`, gwccss.Bg(gwccss.Var("surface-raised")), gwccss.Raw("border", "var(--border-width-boundary) var(--border-style-default) var(--border-color-boundary)"), gwccss.Raw("border-radius", "var(--radius-surface)"), gwccss.Raw("box-shadow", "var(--elevation-flat)"))
		declareGlobal(`.popover,.menu,[role="menu"],[role="listbox"]`, gwccss.Bg(gwccss.Var("surface-overlay")), gwccss.Raw("border", "var(--border-width-boundary) var(--border-style-default) var(--border-color-boundary)"), gwccss.Raw("border-radius", "var(--radius-overlay)"), gwccss.Raw("box-shadow", "var(--elevation-overlay)"))
		declareGlobal(`.dialog,[role="dialog"]`, gwccss.Bg(gwccss.Var("surface-overlay")), gwccss.Raw("border", "var(--border-width-boundary) var(--border-style-default) var(--border-color-boundary)"), gwccss.Raw("border-radius", "var(--radius-overlay)"), gwccss.Raw("box-shadow", "var(--elevation-dialog)"))
		declareGlobal(`:where(a[href],input,select,textarea,button,summary):focus-visible`, gwccss.Raw("outline", "var(--border-width-focus) solid var(--border-color-focus)"), gwccss.OutlineOffset(gwccss.Px(2)), gwccss.Raw("box-shadow", "0 0 0 var(--border-width-focus) var(--surface-overlay)"))
		declareGlobal(`[aria-selected="true"],[data-selected="true"]`, gwccss.Bg(gwccss.Var("surface-selected")), gwccss.Raw("border-inline-start", "var(--border-width-status) var(--border-style-default) var(--border-color-selection)"))
		declareGlobal(`.badge,.tag,.status`, gwccss.Raw("border-radius", "var(--radius-status)"), gwccss.Raw("border", "var(--border-width-boundary) var(--border-style-default) var(--border-color-boundary)"), gwccss.Raw("box-shadow", "var(--elevation-flat)"))
	})
}

func declareWorkspaceBasePre() {
	declareGlobal(`*`,
		gwccss.Raw("box-sizing", "border-box"),
	)
	declareGlobal(`body`,
		gwccss.Margin(gwccss.Zero),
		gwccss.Bg(gwccss.Var("surface-canvas")),
		gwccss.TextColor(gwccss.Var("color-text")),
		gwccss.Raw("font-family", "var(--font-family-sans)"),
		gwccss.Raw("font-size", "var(--font-size-body)"),
		gwccss.Raw("line-height", "var(--line-height-body)"),
		gwccss.MaxWidth(gwccss.Percent(100)),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(`:where(h1,h2,h3,h4,h5,h6)`,
		gwccss.Raw("font-family", "var(--font-family-sans)"),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("line-height", "var(--line-height-heading)"),
		gwccss.Raw("text-wrap", "balance"),
	)
	declareGlobal(`h1`,
		gwccss.Raw("font-size", "var(--font-size-page-title)"),
		gwccss.Raw("line-height", "var(--line-height-display)"),
	)
	declareGlobal(`h2`, gwccss.Raw("font-size", "var(--font-size-section)"))
	declareGlobal(`h3,h4,h5,h6`, gwccss.Raw("font-size", "var(--font-size-label)"))
	declareGlobal(`:where(p,li,.provenance)`,
		gwccss.Raw("max-inline-size", "var(--measure-prose)"),
	)
	declareGlobal(`.workspace`,
		gwccss.MaxWidth(gwccss.Rem(60)),
		gwccss.MarginY(gwccss.Zero), gwccss.MarginX(gwccss.RawLength("auto")),
		gwccss.Padding(gwccss.Rem(1)),
	)
	declareGlobal(`header.workspace-header`,
		gwccss.Padding(gwccss.Rem(1)),
		gwccss.Raw("border-bottom", "var(--border-width-boundary) var(--border-style-default) var(--border-color-boundary)"),
	)
	declareGlobal(`main`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(1.5)),
		gwccss.Padding(gwccss.Rem(1)),
	)
}

// workspaceBasePostTyped holds the WorkspaceCSS body rules authored after
// the 640px breakpoint (section onward), in original order.
func workspaceBasePostTyped() string {
	return buildTypedSheet(declareWorkspaceBasePost)
}

func declareWorkspaceBasePost() {
	declareGlobal(`section`,
		gwccss.Bg(gwccss.Var("surface-raised")),
		gwccss.Raw("border", "var(--border-width-boundary) var(--border-style-default) var(--border-color-boundary)"),
		gwccss.Raw("border-radius", "var(--radius-surface)"),
		gwccss.Raw("box-shadow", "var(--elevation-flat)"),
		gwccss.Padding(gwccss.Rem(1)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.field`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.25)),
		gwccss.Raw("margin-bottom", "1rem"),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.field label`,
		gwccss.Raw("font-size", "var(--font-size-label)"),
		gwccss.Raw("line-height", "var(--line-height-tight)"),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.field input,.field textarea`,
		gwccss.Raw("font", "inherit"),
		gwccss.Padding(gwccss.Rem(.5)),
		gwccss.Raw("border", "var(--border-width-boundary) var(--border-style-default) var(--border-color-control)"),
		gwccss.Raw("border-radius", "var(--radius-control)"),
		gwccss.W(gwccss.Percent(100)),
		gwccss.MaxWidth(gwccss.Percent(100)),
	)
	declareGlobal(`.field .field-static`,
		gwccss.Margin(gwccss.Zero),
		gwccss.PaddingY(gwccss.Rem(.5)), gwccss.PaddingX(gwccss.Zero),
	)
	declareGlobal(`.field .error`,
		gwccss.TextColor(gwccss.Var("color-danger")),
		gwccss.Raw("font-size", "var(--font-size-helper)"),
		gwccss.Raw("line-height", "var(--line-height-tight)"),
	)
	declareGlobal(`[data-type-role="display"],.type-display`,
		gwccss.Raw("font-size", "var(--font-size-display)"),
		gwccss.Raw("line-height", "var(--line-height-display)"),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(`[data-type-role="page-title"],.type-page-title`,
		gwccss.Raw("font-size", "var(--font-size-page-title)"),
		gwccss.Raw("line-height", "var(--line-height-display)"),
	)
	declareGlobal(`[data-type-role="section"],.type-section`,
		gwccss.Raw("font-size", "var(--font-size-section)"),
		gwccss.Raw("line-height", "var(--line-height-heading)"),
	)
	declareGlobal(`[data-type-role="label"],.type-label`,
		gwccss.Raw("font-size", "var(--font-size-label)"),
		gwccss.Raw("line-height", "var(--line-height-tight)"),
	)
	declareGlobal(`[data-type-role="helper"],.type-helper`,
		gwccss.Raw("font-size", "var(--font-size-helper)"),
		gwccss.Raw("line-height", "var(--line-height-tight)"),
	)
	declareGlobal(`[data-type-role="table"],.type-table,.table-scroll`,
		gwccss.Raw("font-size", "var(--font-size-table)"),
		gwccss.Raw("line-height", "var(--line-height-body)"),
	)
	declareGlobal(`[data-type-role="code"],.type-code,code,kbd,pre`,
		gwccss.Raw("font-family", "var(--font-family-mono)"),
		gwccss.Raw("font-size", "var(--font-size-code)"),
		gwccss.Raw("line-height", "var(--line-height-tight)"),
	)
	declareGlobal(`ul.findings,ol.timeline`,
		gwccss.Raw("list-style", "none"),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.5)),
	)
	declareGlobal(`.finding,.check`,
		gwccss.Raw("border-inline-start", ".25rem var(--border-style-default) var(--border-color-boundary)"),
		gwccss.PaddingY(gwccss.Rem(.25)), gwccss.PaddingX(gwccss.Rem(.75)),
	)
	declareGlobal(`.finding[data-severity="blocking"],.check[data-severity="blocking"]`,
		gwccss.BorderColor(gwccss.Var("border-color-danger")),
	)
	declareGlobal(`.finding[data-severity="warning"],.check[data-severity="warning"]`,
		gwccss.BorderColor(gwccss.Var("border-color-warning")),
	)
	declareGlobal(`.finding[data-severity="success"],.check[data-severity="success"]`,
		gwccss.BorderColor(gwccss.Var("border-color-success")),
	)
	declareGlobal(`.finding[data-severity="info"],.check[data-severity="info"]`,
		gwccss.BorderColor(gwccss.Var("border-color-boundary")),
	)
	declareGlobal(`.status-banner`,
		gwccss.PaddingY(gwccss.Rem(.75)), gwccss.PaddingX(gwccss.Rem(1)),
		gwccss.Raw("border-radius", "var(--radius-status)"),
		gwccss.Raw("border", "var(--border-width-status) var(--border-style-default) var(--border-color-boundary)"),
		gwccss.Raw("box-shadow", "var(--elevation-flat)"),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.status-banner[data-status="ready"]`,
		gwccss.Bg(gwccss.Var("color-success")),
		gwccss.TextColor(gwccss.Var("color-accent-text")),
		gwccss.BorderColor(gwccss.Var("border-color-success")),
	)
	declareGlobal(`.status-banner[data-status="needs_review"],.status-banner[data-status="pending"]`,
		gwccss.Bg(gwccss.Var("color-warning")),
		gwccss.TextColor(gwccss.Var("color-warning-text")),
		gwccss.BorderColor(gwccss.Var("border-color-warning")),
	)
	declareGlobal(`.status-banner[data-status="failed"]`,
		gwccss.Bg(gwccss.Var("color-danger")),
		gwccss.TextColor(gwccss.Var("color-danger-text")),
		gwccss.BorderColor(gwccss.Var("border-color-danger")),
	)
	declareGlobal(`.actions`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Rem(.75)),
	)
	declareGlobal(`button`,
		gwccss.Raw("font", "inherit"),
		gwccss.PaddingY(gwccss.Rem(.6)), gwccss.PaddingX(gwccss.Rem(1.2)),
		gwccss.Raw("border-radius", "var(--radius-control)"),
		gwccss.Border(gwccss.Px(1), gwccss.Transparent),
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(`button[data-variant="primary"]`,
		gwccss.Bg(gwccss.Var("color-accent")),
		gwccss.TextColor(gwccss.Var("color-accent-text")),
	)
	declareGlobal(`button[data-variant="secondary"]`,
		gwccss.Bg(gwccss.Var("surface-raised")),
		gwccss.TextColor(gwccss.Var("color-text")),
		gwccss.BorderColor(gwccss.Var("border-color-control")),
	)
	declareGlobal(`button[data-variant="danger"]`,
		gwccss.Bg(gwccss.Var("color-danger")),
		gwccss.TextColor(gwccss.Var("color-danger-text")),
	)
	declareGlobal(`.visually-hidden`,
		gwccss.Position.Absolute,
		gwccss.W(gwccss.Px(1)),
		gwccss.H(gwccss.Px(1)),
		gwccss.Padding(gwccss.Zero),
		gwccss.Margin(gwccss.Px(-1)),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Raw("clip", "rect(0,0,0,0)"),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("border", "0"),
	)
	declareGlobal(`img,svg,video,canvas`,
		gwccss.Raw("max-inline-size", "100%"),
		gwccss.H(gwccss.RawLength("auto")),
	)
	declareGlobal(`:where(input,select,textarea,button)`,
		gwccss.Raw("max-inline-size", "100%"),
	)
	declareGlobal(`pre`,
		gwccss.Raw("max-inline-size", "100%"),
		gwccss.Raw("overflow", "auto"),
	)
	declareGlobal(`.widget-slot`,
		gwccss.Raw("min-inline-size", "0"),
		gwccss.Raw("max-inline-size", "100%"),
	)
	declareGlobal(`.table-container,.table-scroll`,
		gwccss.Raw("min-inline-size", "0"),
		gwccss.Raw("max-inline-size", "100%"),
	)
	declareGlobal(`.table-scroll`,
		gwccss.Raw("overflow-x", "auto"),
		gwccss.Raw("overscroll-behavior-inline", "contain"),
		gwccss.Raw("scrollbar-width", "thin"),
		gwccss.Raw("touch-action", "pan-x pan-y"),
	)
	declareGlobal(`.table-scroll table`,
		gwccss.Raw("inline-size", "max-content"),
		gwccss.Raw("min-inline-size", "100%"),
		gwccss.Raw("table-layout", "auto"),
		gwccss.Raw("border-collapse", "collapse"),
	)
	declareGlobal(`.table-scroll :where(th,td)`,
		gwccss.Raw("min-inline-size", "8rem"),
		gwccss.Raw("overflow-wrap", "normal"),
		gwccss.Raw("word-break", "normal"),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(`.table-scroll-cue`,
		gwccss.TextColor(gwccss.Var("color-text-muted")),
		gwccss.Raw("font-size", "var(--font-size-helper)"),
		gwccss.Raw("line-height", "var(--line-height-tight)"),
		gwccss.Raw("margin-block", ".25rem"),
	)
}

// workspaceMedia640Typed is the one grid breakpoint, in original position
// (between the base main rule and the base section rule).
func workspaceMedia640Typed() string {
	return atRule("@media (min-width:640px)", buildTypedSheet(declareWorkspaceMedia640))
}

func declareWorkspaceMedia640() {
	declareGlobal(`main`,
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1)),
		gwccss.Gap(gwccss.Rem(1.5)),
	)
	declareGlobal(`section.timeline,section.actions`,
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
	)
}

// responsiveLayoutTyped is the renderer-owned mapping from the closed
// floorplan projection to CSS. The narrow rules are unconditional; wider
// rules only add capability through fixed min-width queries. No page,
// request, locale, or user-agent value is interpolated into this stylesheet.
// Tier iteration order matches responsiveLayoutCSS.
func responsiveLayoutTyped() string {
	var b strings.Builder
	b.WriteString(buildTypedSheet(declareLayoutBase))
	b.WriteString(buildTypedSheet(declareLayoutNarrow))
	b.WriteString(layoutTierTyped("compact", "40rem"))
	b.WriteString(layoutTierTyped("standard", "60rem"))
	b.WriteString(layoutTierTyped("wide", "80rem"))
	return b.String()
}

func declareLayoutBase() {
	declareGlobal(`.layout-region`,
		gwccss.Raw("min-inline-size", "0"),
		gwccss.Gap(gwccss.Rem(.5)),
	)
	declareGlobal(`.layout-region>:where(h1,h2,h3,h4,h5,h6)`,
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
	)
}

func declareLayoutNarrow() {
	declareLayoutTierRules("narrow")
}

func layoutTierTyped(breakpoint, minWidth string) string {
	return atRule("@media (min-width:"+minWidth+")", buildTypedSheet(func() {
		declareLayoutTierRules(breakpoint)
	}))
}

// declareLayoutTierRules emits one breakpoint tier's rules, in the same
// column order as writeResponsiveTier.
func declareLayoutTierRules(breakpoint string) {
	prefix := `.layout-region[data-layout-` + breakpoint
	declareGlobal(prefix+`-mode="flow"]`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
	)
	declareGlobal(prefix+`-mode="grid"]`,
		gwccss.Display.Grid,
	)
	for columns := 1; columns <= 12; columns++ {
		declareGlobal(fmt.Sprintf(prefix+`-mode="grid"][data-layout-%s-columns="%d"]`, breakpoint, columns),
			gwccss.Raw("grid-template-columns", fmt.Sprintf("repeat(%d,minmax(0,1fr))", columns)),
		)
	}
	declareGlobal(prefix+`-mode="grid"][data-layout-`+breakpoint+`-stacked="true"]`,
		gwccss.Raw("grid-template-columns", "1fr"),
	)
}

// modeContractsTyped contains renderer-owned presentation contracts for user
// agents that replace the normal colour scheme, and for printed evidence.
// One @media block per contract, in original order. The @page rule stays
// first inside the print block, exactly as authored.
func modeContractsTyped() string {
	return contrastMoreTyped() + forcedColorsTyped() + printContractTyped()
}

func contrastMoreTyped() string {
	return atRule("@media "+string(gwccss.ContrastMore), buildTypedSheet(declareContrastMore))
}

func declareContrastMore() {
	declareGlobal(`body,section,.workspace-header,.field :where(input,select,textarea),.status-banner,.table-scroll :where(th,td)`,
		gwccss.Raw("color", "CanvasText!important"), gwccss.Raw("background", "Canvas!important"),
	)
	declareGlobal(`section,.workspace-header,.field :where(input,select,textarea),.status-banner,.table-scroll :where(th,td)`,
		gwccss.Raw("border-color", "CanvasText!important"),
	)
	declareGlobal(`.field .error,.table-scroll-cue`,
		gwccss.Raw("color", "CanvasText!important"),
	)
	declareGlobal(`a`,
		gwccss.Raw("color", "LinkText!important"),
	)
	declareGlobal(`button`,
		gwccss.Raw("color", "ButtonText!important"), gwccss.Raw("background", "ButtonFace!important"), gwccss.Raw("border-color", "ButtonText!important"),
	)
	declareGlobal(`:where(a[href],input,select,textarea,button,summary):focus-visible`,
		gwccss.Raw("outline", "3px solid Highlight!important"), gwccss.OutlineOffset(gwccss.Px(2)), gwccss.Raw("box-shadow", "0 0 0 1px Canvas!important"),
	)
	declareGlobal(`.finding,.check`,
		gwccss.Raw("border-inline-start", ".35rem solid CanvasText!important"),
	)
	declareGlobal(`.status-banner`,
		gwccss.BorderWidth(gwccss.RawLength("2px!important")),
	)
}

func forcedColorsTyped() string {
	return atRule("@media "+string(gwccss.ForcedColors), buildTypedSheet(declareForcedColors))
}

func declareForcedColors() {
	declareGlobal(`body`,
		gwccss.Raw("background", "Canvas!important"), gwccss.Raw("color", "CanvasText!important"),
	)
	declareGlobal(`section,.workspace-header,.field :where(input,select,textarea),.table-scroll :where(th,td),.provenance`,
		gwccss.Raw("background", "Canvas!important"), gwccss.Raw("color", "CanvasText!important"), gwccss.Raw("border", "1px solid CanvasText!important"),
	)
	declareGlobal(`a`,
		gwccss.Raw("color", "LinkText!important"),
	)
	declareGlobal(`button`,
		gwccss.Raw("background", "ButtonFace!important"), gwccss.Raw("color", "ButtonText!important"), gwccss.Raw("border", "1px solid ButtonText!important"),
	)
	declareGlobal(`button[data-variant="primary"],button[data-variant="danger"]`,
		gwccss.Raw("background", "ButtonFace!important"), gwccss.Raw("color", "ButtonText!important"),
	)
	declareGlobal(`.status-banner`,
		gwccss.Raw("background", "Canvas!important"), gwccss.Raw("color", "CanvasText!important"), gwccss.Raw("border", "2px solid CanvasText!important"),
	)
	declareGlobal(`.field .error,.table-scroll-cue`,
		gwccss.Raw("color", "CanvasText!important"),
	)
	declareGlobal(`.finding,.check`,
		gwccss.Raw("border-inline-start", ".35rem solid CanvasText!important"),
	)
	declareGlobal(`:where(a[href],input,select,textarea,button,summary):focus-visible`,
		gwccss.Raw("outline", "3px solid Highlight!important"), gwccss.OutlineOffset(gwccss.Px(2)), gwccss.Raw("box-shadow", "0 0 0 1px Canvas!important"), gwccss.Raw("forced-color-adjust", "auto"),
	)
}

func printContractTyped() string {
	return atRule("@media "+string(gwccss.Print), buildTypedSheet(declarePrintContract))
}

func declarePrintContract() {
	declareGlobal(`@page`,
		gwccss.Raw("margin", "1.5cm"),
	)
	declareGlobal(`html,body`,
		gwccss.Raw("background", "#fff!important"), gwccss.Raw("color", "#000!important"),
	)
	declareGlobal(`body`,
		gwccss.FontSize(gwccss.RawLength("10.5pt")),
		gwccss.LineHeight(gwccss.Num(1.35)),
		gwccss.MaxWidth(gwccss.RawLength("none")),
		gwccss.Raw("overflow", "visible"),
	)
	declareGlobal(`.workspace`,
		gwccss.MaxWidth(gwccss.RawLength("none")),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(`header.workspace-header,main,section,footer,.finding,.check,.status-banner,.provenance,.simulation-generated,.table-scroll :where(th,td)`,
		gwccss.Raw("color", "#000!important"), gwccss.Raw("background", "#fff!important"), gwccss.Raw("border-color", "#000!important"),
	)
	declareGlobal(`section`,
		gwccss.Raw("break-inside", "avoid"), gwccss.Raw("page-break-inside", "avoid"),
	)
	declareGlobal(`nav,.skip-link,.actions,.interactive-only,[data-print="interactive-only"]`,
		gwccss.Raw("display", "none!important"),
	)
	declareGlobal(`.print-evidence`,
		gwccss.Raw("position", "static!important"),
		gwccss.W(gwccss.RawLength("auto!important")),
		gwccss.H(gwccss.RawLength("auto!important")),
		gwccss.Raw("margin", ".5rem 0!important"),
		gwccss.Raw("overflow", "visible!important"),
		gwccss.Raw("clip", "auto!important"),
		gwccss.Raw("white-space", "normal!important"),
	)
	declareGlobal(`:where(input,select,textarea)`,
		gwccss.Raw("color", "#000!important"), gwccss.Raw("background", "#fff!important"), gwccss.Raw("border-color", "#000!important"),
	)
	declareGlobal(`.table-scroll`,
		gwccss.Raw("overflow", "visible!important"),
	)
	declareGlobal(`.table-scroll table`,
		gwccss.Raw("inline-size", "100%!important"), gwccss.Raw("min-inline-size", "0!important"),
	)
	declareGlobal(`.table-scroll :where(th,td)`,
		gwccss.Raw("min-inline-size", "0!important"), gwccss.Raw("white-space", "normal!important"), gwccss.Raw("overflow-wrap", "anywhere!important"),
	)
}
