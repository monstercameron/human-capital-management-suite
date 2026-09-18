package journey

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// This file and typed_journey_b/c.go build Stylesheet from typed GWC
// declarations. Rule order matches the original const; the canonical
// serializer sorts declarations alphabetically and appends trailing
// semicolons (a byte change only, proven canonical with /tmp/cssdiff.py).
// @keyframes names are content-hashed (see typed_keyframes.go).

// declareJourneyTokens emits the :root custom properties and the base,
// masthead, notice, type-scale, proposal, chip, journey-card, and
// people-table rules, in original order.
func declareJourneyTokens() {
	declareGlobal(`:root`,
		gwccss.CustomColor("jn-canvas", gwccss.Hex("f6f7fb")),
		gwccss.CustomColor("jn-surface", gwccss.Hex("ffffff")),
		gwccss.CustomColor("jn-surface-sunk", gwccss.Hex("eef0f7")),
		gwccss.CustomColor("jn-surface-muted", gwccss.Hex("eceef5")),
		gwccss.CustomColor("jn-hairline", gwccss.Hex("dfe3ee")),
		gwccss.CustomColor("jn-control-border", gwccss.Hex("7d859c")),
		gwccss.CustomColor("jn-ink", gwccss.Hex("16192a")),
		gwccss.CustomColor("jn-ink-muted", gwccss.Hex("545a70")),
		gwccss.CustomColor("jn-masthead", gwccss.Hex("10142b")),
		gwccss.CustomColor("jn-masthead-ink", gwccss.Hex("ffffff")),
		gwccss.CustomColor("jn-masthead-muted", gwccss.Hex("b9c0d8")),
		gwccss.CustomColor("jn-masthead-chip", gwccss.Hex("232a4a")),
		gwccss.CustomColor("jn-masthead-chip-ink", gwccss.Hex("ccd3e8")),
		gwccss.CustomColor("jn-accent", gwccss.Hex("2b3a8f")),
		gwccss.CustomColor("jn-accent-strong", gwccss.Hex("1f2c73")),
		gwccss.CustomColor("jn-accent-ink", gwccss.Hex("ffffff")),
		gwccss.CustomColor("jn-accent-soft", gwccss.Hex("e4ecfb")),
		gwccss.CustomColor("jn-info", gwccss.Hex("14448f")),
		gwccss.CustomColor("jn-info-soft", gwccss.Hex("e4ecfb")),
		gwccss.CustomColor("jn-success", gwccss.Hex("0f6136")),
		gwccss.CustomColor("jn-success-soft", gwccss.Hex("dff3e6")),
		gwccss.CustomColor("jn-warning", gwccss.Hex("7a4a00")),
		gwccss.CustomColor("jn-warning-soft", gwccss.Hex("fdeed3")),
		gwccss.CustomColor("jn-danger", gwccss.Hex("9b1130")),
		gwccss.CustomColor("jn-danger-ink", gwccss.Hex("ffffff")),
		gwccss.CustomColor("jn-danger-soft", gwccss.Hex("fde6ea")),
		gwccss.CustomColor("jn-neutral", gwccss.Hex("3f465e")),
		gwccss.CustomColor("jn-neutral-soft", gwccss.Hex("eceef5")),
		gwccss.Custom("jn-font", "system-ui,-apple-system,\"Segoe UI\",Roboto,sans-serif"),
		gwccss.Custom("jn-mono", "ui-monospace,\"Cascadia Mono\",Consolas,monospace"),
		gwccss.CustomLength("jn-s1", gwccss.Rem(0.5)),
		gwccss.CustomLength("jn-s2", gwccss.Rem(1)),
		gwccss.CustomLength("jn-s3", gwccss.Rem(1.5)),
		gwccss.CustomLength("jn-s4", gwccss.Rem(2)),
		gwccss.CustomLength("jn-s5", gwccss.Rem(3)),
		gwccss.CustomLength("jn-r1", gwccss.Rem(0.375)),
		gwccss.CustomLength("jn-r2", gwccss.Rem(0.625)),
		gwccss.CustomLength("jn-r3", gwccss.Rem(0.75)),
		gwccss.CustomLength("jn-r4", gwccss.Rem(1)),
		gwccss.Custom("jn-shadow-lift", "0 2px 6px rgba(16,20,43,.10),0 14px 32px rgba(16,20,43,.09)"),
		gwccss.Custom("jn-ring", "0 0 0 3px rgba(43,58,143,.28)"),
		gwccss.Custom("jn-ease", "cubic-bezier(.22,.61,.36,1)"),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded),:where(.jn-page,.jn-embedded) *,:where(.jn-page,.jn-embedded) *::before,:where(.jn-page,.jn-embedded) *::after`,
		gwccss.Raw("box-sizing", "border-box"),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded)`,
		gwccss.Raw("-webkit-text-size-adjust", "100%"),
	)
	// Standalone documents keep their edge-to-edge canvas without resetting
	// the host product document when the Journey sheet is injected later.
	declareGlobal(`body:has(>.jn-page)`, gwccss.Margin(gwccss.Zero))
	declareGlobal(`:where(.jn-page,.jn-embedded)`,
		gwccss.Bg(gwccss.Var("jn-canvas")),
		gwccss.TextColor(gwccss.Var("jn-ink")),
		gwccss.Raw("font-family", "var(--jn-font)"),
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.LineHeight(gwccss.Num(1.55)),
		gwccss.Raw("overflow-wrap", "break-word"),
		gwccss.Raw("text-rendering", "optimizeLegibility"),
		gwccss.Raw("font-feature-settings", "\"cv05\" 1,\"ss01\" 1"),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded) :is(h1,h2,h3,h4,p,ul,ol,dl,dd,figure,table)`,
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded) :is(ul,ol)`,
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded) svg`,
		gwccss.Display.Block,
	)
	// This sheet is also injected when Journeys is embedded in the product
	// workspace. Link and focus colors must not leak into the host shell.
	declareGlobal(`:where(.jn-page,.jn-embedded) a`,
		gwccss.TextColor(gwccss.Var("jn-accent")),
		gwccss.TextDecorationThickness(gwccss.Px(1)),
		gwccss.TextUnderlineOffset(gwccss.Ems(.15)),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("color")), gwccss.S(.15), gwccss.Easing("var(--jn-ease)")),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded) a:hover`,
		gwccss.TextColor(gwccss.Var("jn-accent-strong")),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded) :focus-visible`,
		gwccss.Raw("outline", "3px solid var(--jn-accent)"),
		gwccss.OutlineOffset(gwccss.Px(2)),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
	)
	declareGlobal(`.jn-masthead :focus-visible`,
		gwccss.Raw("outline-color", "var(--jn-masthead-ink)"),
	)
	declareGlobal(`.jn-visually-hidden`,
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
	declareGlobal(`.jn-skip`,
		gwccss.Position.Absolute,
		gwccss.Left(gwccss.VarLength("jn-s1")),
		gwccss.Top(gwccss.VarLength("jn-s1")),
		gwccss.ZIndex(20),
		gwccss.Transform(gwccss.TranslateY(gwccss.Percent(-200))),
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.TextColor(gwccss.Var("jn-accent")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-control-border")),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.PaddingY(gwccss.Rem(.5)), gwccss.PaddingX(gwccss.Rem(.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("text-decoration", "none"),
		gwccss.Raw("box-shadow", "var(--jn-shadow-lift)"),
	)
	declareGlobal(`.jn-skip:focus`,
		gwccss.Transform(gwccss.TranslateY(gwccss.Zero)),
	)
	declareGlobal(`.jn-page`,
		gwccss.MinHeight(gwccss.Vh(100)),
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-shell`,
		gwccss.W(gwccss.Percent(100)),
		gwccss.MaxWidth(gwccss.Rem(80)),
		gwccss.MarginY(gwccss.Zero), gwccss.MarginX(gwccss.RawLength("auto")),
		gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.VarLength("jn-s2")),
	)
	declareGlobal(`.jn-masthead`,
		gwccss.Bg(gwccss.Var("jn-masthead")),
		gwccss.TextColor(gwccss.Var("jn-masthead-ink")),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("jn-masthead-chip")),
	)
	declareGlobal(`.jn-masthead a`,
		gwccss.TextColor(gwccss.Var("jn-masthead-ink")),
	)
	declareGlobal(`.jn-masthead-inner`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.VarLength("jn-s2")),
		gwccss.Raw("padding-top", ".875rem"),
		gwccss.Raw("padding-bottom", ".875rem"),
	)
	declareGlobal(`.jn-brandbar`,
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-markwell`,
		gwccss.Raw("flex", "none"),
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Justify.Center,
		gwccss.W(gwccss.Rem(2.5)),
		gwccss.H(gwccss.Rem(2.5)),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.Raw("background", "linear-gradient(140deg,var(--jn-masthead-chip),var(--jn-accent-strong))"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-masthead-chip")),
		gwccss.TextColor(gwccss.Var("jn-masthead-ink")),
		gwccss.Raw("box-shadow", "inset 0 1px 0 rgba(255,255,255,.10)"),
	)
	declareGlobal(`.jn-brandtext`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.MinWidth(gwccss.Zero),
		gwccss.LineHeight(gwccss.Num(1.25)),
	)
	declareGlobal(`.jn-brand-name`,
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Tracking(gwccss.Ems(-.014)),
	)
	declareGlobal(`.jn-tenant`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-masthead-muted")),
		gwccss.Tracking(gwccss.Ems(.005)),
	)
	declareGlobal(`.jn-nav ul`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Rem(.25)),
	)
	declareGlobal(`.jn-nav a`,
		gwccss.Display.InlineBlock,
		gwccss.PaddingY(gwccss.Rem(.375)), gwccss.PaddingX(gwccss.Rem(.75)),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "500"),
		gwccss.Raw("text-decoration", "none"),
		gwccss.TextColor(gwccss.Var("jn-masthead-muted")),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("background-color"), gwccss.Prop("color")), gwccss.S(.15), gwccss.Easing("var(--jn-ease)")),
	)
	declareGlobal(`.jn-nav a:hover`,
		gwccss.TextColor(gwccss.Var("jn-masthead-ink")),
		gwccss.Bg(gwccss.Var("jn-masthead-chip")),
	)
	declareGlobal(`.jn-nav a[aria-current="page"]`,
		gwccss.TextColor(gwccss.Var("jn-masthead-ink")),
		gwccss.Bg(gwccss.Var("jn-masthead-chip")),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Zero, gwccss.Px(-2), gwccss.Zero, gwccss.Zero, gwccss.Var("jn-accent-soft"))),
	)
	declareGlobal(`.jn-principal`,
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.5)),
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("jn-masthead-muted")),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-principal-subject`,
		gwccss.TextColor(gwccss.Var("jn-masthead-ink")),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-rolechip`,
		gwccss.Display.InlineBlock,
		gwccss.Bg(gwccss.Var("jn-masthead-chip")),
		gwccss.TextColor(gwccss.Var("jn-masthead-chip-ink")),
		gwccss.Rounded(gwccss.Px(999)),
		gwccss.PaddingY(gwccss.Rem(.0625)), gwccss.PaddingX(gwccss.Rem(.5)),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-family", "var(--jn-mono)"),
		gwccss.Tracking(gwccss.Ems(-.01)),
	)
	declareGlobal(`.jn-logout`,
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("jn-masthead-muted")),
	)
	declareGlobal(`.jn-masthead-inner`,
		mediaRule(gwccss.RawMedia("(min-width:64rem)"), gwccss.FlexDir.Row, gwccss.Items.Center, gwccss.Raw("justify-content", "space-between")),
	)
	declareGlobal(`.jn-principal`,
		mediaRule(gwccss.RawMedia("(min-width:64rem)"), gwccss.Raw("justify-content", "flex-end"), gwccss.MaxWidth(gwccss.Rem(34))),
	)
	declareGlobal(`.jn-noticeband`,
		gwccss.Raw("padding-top", "var(--jn-s2)"),
	)
	declareGlobal(`.jn-notice`,
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.PaddingY(gwccss.Rem(.875)), gwccss.PaddingX(gwccss.Rem(1)),
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.Raw("box-shadow", "none"),
		gwccss.Keyframes("jn-slidein", jnSlideinFrames...),
		gwccss.Animation(gwccss.RawDuration(".32s"), gwccss.Easing("var(--jn-ease)")),
		gwccss.Raw("animation-fill-mode", "both"),
	)
	declareGlobal(`.jn-notice-icon`,
		gwccss.Raw("flex", "none"),
		gwccss.Raw("margin-top", ".125rem"),
	)
	declareGlobal(`.jn-notice-body`,
		gwccss.Raw("min-width", "0"),
	)
	declareGlobal(`.jn-notice-title`,
		gwccss.Raw("font-weight", "600"),
		gwccss.Tracking(gwccss.Ems(-.01)),
	)
	declareGlobal(`.jn-notice-detail`,
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Raw("margin-top", ".125rem"),
	)
	declareGlobal(`.jn-notice-fields`,
		gwccss.Raw("margin-top", ".625rem"),
	)
	declareGlobal(`.jn-notice-support`,
		gwccss.Raw("margin-top", ".75rem"),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(`.jn-notice-support summary`,
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("text-decoration", "underline"),
		gwccss.Raw("text-underline-offset", ".18em"),
	)
	declareGlobal(`.jn-notice-support-body`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.375)),
		gwccss.Raw("margin-top", ".5rem"),
	)
	declareGlobal(`.jn-notice-support-body label`,
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-support-reference`,
		gwccss.Raw("box-sizing", "border-box"),
		gwccss.Raw("max-width", "100%"),
		gwccss.W(gwccss.Rem(22)),
		gwccss.PaddingY(gwccss.Rem(.375)), gwccss.PaddingX(gwccss.Rem(.5)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.TextColor(gwccss.Var("jn-ink")),
	)
	declareGlobal(`.jn-notice-support-body p`,
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-notice-fields-title`,
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-notice-fieldlist`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Rem(.5)),
		gwccss.Raw("list-style", "none"),
		gwccss.Raw("margin", ".375rem 0 0"),
		gwccss.Raw("padding", "0"),
	)
	declareGlobal(`.jn-notice-fieldlist a`,
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("jn-ink")),
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.PaddingY(gwccss.Rem(.25)), gwccss.PaddingX(gwccss.Rem(.5)),
		gwccss.Raw("text-decoration", "underline"),
		gwccss.Raw("text-underline-offset", ".18em"),
	)
	declareGlobal(`.jn-notice-fieldlist a:hover`,
		gwccss.Bg(gwccss.Var("jn-surface-muted")),
	)
	declareGlobal(`.jn-notice[data-tone="info"]`,
		gwccss.Bg(gwccss.Var("jn-info-soft")),
		gwccss.BorderColor(gwccss.Var("jn-info")),
	)
	declareGlobal(`.jn-notice[data-tone="info"] .jn-notice-title,.jn-notice[data-tone="info"] .jn-notice-icon`,
		gwccss.TextColor(gwccss.Var("jn-info")),
	)
	declareGlobal(`.jn-notice[data-tone="success"]`,
		gwccss.Bg(gwccss.Var("jn-success-soft")),
		gwccss.BorderColor(gwccss.Var("jn-success")),
	)
	declareGlobal(`.jn-notice[data-tone="success"] .jn-notice-title,.jn-notice[data-tone="success"] .jn-notice-icon`,
		gwccss.TextColor(gwccss.Var("jn-success")),
	)
	declareGlobal(`.jn-notice[data-tone="warning"]`,
		gwccss.Bg(gwccss.Var("jn-warning-soft")),
		gwccss.BorderColor(gwccss.Var("jn-warning")),
	)
	declareGlobal(`.jn-notice[data-tone="warning"] .jn-notice-title,.jn-notice[data-tone="warning"] .jn-notice-icon`,
		gwccss.TextColor(gwccss.Var("jn-warning")),
	)
	declareGlobal(`.jn-notice[data-tone="danger"]`,
		gwccss.Bg(gwccss.Var("jn-danger-soft")),
		gwccss.BorderColor(gwccss.Var("jn-danger")),
	)
	declareGlobal(`.jn-notice[data-tone="danger"] .jn-notice-title,.jn-notice[data-tone="danger"] .jn-notice-icon`,
		gwccss.TextColor(gwccss.Var("jn-danger")),
	)
	declareGlobal(`.jn-main`,
		gwccss.Raw("flex", "1 1 auto"),
		gwccss.Raw("padding-top", "var(--jn-s3)"),
		gwccss.Raw("padding-bottom", "var(--jn-s5)"),
	)
	declareGlobal(`.jn-stack`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.VarLength("jn-s3")),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-pagehead`,
		gwccss.MaxWidth(gwccss.Rem(46)),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded) h1`,
		gwccss.FontSize(gwccss.Rem(1.75)),
		gwccss.LineHeight(gwccss.Num(1.18)),
		gwccss.Tracking(gwccss.Ems(-.024)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(`.jn-display`,
		gwccss.FontSize(gwccss.Rem(2)),
		gwccss.LineHeight(gwccss.Num(1.1)),
		gwccss.Tracking(gwccss.Ems(-.03)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(`.jn-lead`,
		gwccss.Raw("margin-top", ".5rem"),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.FontSize(gwccss.Rem(1)),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded) h2`,
		gwccss.FontSize(gwccss.Rem(1.25)),
		gwccss.LineHeight(gwccss.Num(1.3)),
		gwccss.Tracking(gwccss.Ems(-.018)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.5)),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded) h3`,
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.LineHeight(gwccss.Num(1.35)),
		gwccss.Tracking(gwccss.Ems(-.012)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-headicon`,
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-eyebrow`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Tracking(gwccss.Ems(.07)),
		gwccss.Raw("text-transform", "uppercase"),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-sectionhead`,
		gwccss.Display.Flex,
		gwccss.Items.Baseline,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.VarLength("jn-s1")),
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Raw("margin-bottom", "var(--jn-s2)"),
	)
	declareGlobal(`.jn-count`,
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.jn-card`,
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r3")),
		gwccss.Raw("box-shadow", "none"),
		gwccss.Padding(gwccss.VarLength("jn-s2")),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-panel`,
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r4")),
		gwccss.Raw("box-shadow", "none"),
		gwccss.Padding(gwccss.VarLength("jn-s3")),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-mono`,
		gwccss.Raw("font-family", "var(--jn-mono)"),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Tracking(gwccss.Ems(-.01)),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(`.jn-num`,
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.jn-muted`,
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-proposal-view`,
		gwccss.MaxWidth(gwccss.Rem(64)),
	)
	declareGlobal(`.jn-proposal-head`,
		gwccss.MaxWidth(gwccss.Rem(52)),
	)
	declareGlobal(`.jn-context-actions`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MaxWidth(gwccss.Percent(100)),
		gwccss.RowGap(gwccss.Rem(.5)), gwccss.ColumnGap(gwccss.Rem(1.25)),
		gwccss.Raw("margin-top", "var(--jn-s2)"),
	)
	declareGlobal(`.jn-context-actions .jn-btn`,
		gwccss.MaxWidth(gwccss.Percent(100)),
		gwccss.Raw("white-space", "normal"),
		gwccss.Raw("overflow-wrap", "break-word"),
		gwccss.Raw("text-align", "center"),
	)
	declareGlobal(`.jn-context-link`,
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-subject-card`,
		gwccss.Position.Relative,
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(`.jn-subject-card::before`,
		gwccss.Raw("content", "\"\""),
		gwccss.Position.Absolute,
		gwccss.Raw("inset", "0 auto 0 0"),
		gwccss.W(gwccss.Px(4)),
		gwccss.Bg(gwccss.Var("jn-accent")),
	)
	declareGlobal(`.jn-subject-identity`,
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.VarLength("jn-s2")),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(`.jn-subject-identity>div:nth-child(2)`,
		gwccss.MinWidth(gwccss.Rem(12)),
		gwccss.Raw("flex", "1"),
	)
	declareGlobal(`.jn-subject-avatar`,
		gwccss.W(gwccss.Rem(3)),
		gwccss.H(gwccss.Rem(3)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Justify.Center,
		gwccss.Raw("flex", "none"),
		gwccss.Bg(gwccss.Var("jn-accent-soft")),
		gwccss.TextColor(gwccss.Var("jn-accent-strong")),
		gwccss.Raw("font-weight", "700"),
		gwccss.Tracking(gwccss.Ems(.02)),
	)
	declareGlobal(`.jn-subject-title`,
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("margin-top", ".125rem"),
	)
	declareGlobal(`.jn-subject-facts`,
		gwccss.Raw("margin-top", "var(--jn-s2)"),
		gwccss.Raw("padding-top", "var(--jn-s2)"),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-chip`,
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.3125)),
		gwccss.Rounded(gwccss.Px(999)),
		gwccss.PaddingY(gwccss.Rem(.1875)), gwccss.PaddingX(gwccss.Rem(.5625)),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Tracking(gwccss.Ems(-.005)),
		gwccss.Bg(gwccss.Var("jn-neutral-soft")),
		gwccss.TextColor(gwccss.Var("jn-neutral")),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("box-shadow", "inset 0 0 0 1px rgba(22,25,42,.05)"),
	)
	declareGlobal(`.jn-chip[data-tone="info"]`,
		gwccss.Bg(gwccss.Var("jn-info-soft")),
		gwccss.TextColor(gwccss.Var("jn-info")),
	)
	declareGlobal(`.jn-chip[data-tone="success"]`,
		gwccss.Bg(gwccss.Var("jn-success-soft")),
		gwccss.TextColor(gwccss.Var("jn-success")),
	)
	declareGlobal(`.jn-chip[data-tone="warning"]`,
		gwccss.Bg(gwccss.Var("jn-warning-soft")),
		gwccss.TextColor(gwccss.Var("jn-warning")),
	)
	declareGlobal(`.jn-chip[data-tone="danger"]`,
		gwccss.Bg(gwccss.Var("jn-danger-soft")),
		gwccss.TextColor(gwccss.Var("jn-danger")),
	)
	declareGlobal(`.jn-chip-dot`,
		gwccss.W(gwccss.Rem(.4375)),
		gwccss.H(gwccss.Rem(.4375)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Raw("background", "currentColor"),
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(`.jn-chip-icon`,
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(`.jn-grid`,
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("jn-s2")),
		gwccss.Raw("grid-template-columns", "repeat(auto-fill,minmax(min(19rem,100%),1fr))"),
	)
	declareGlobal(`.jn-journey-groups`,
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("jn-s4")),
	)
	declareGlobal(`.jn-journey-group>.jn-sectionhead`,
		gwccss.Raw("margin-bottom", "var(--jn-s2)"),
	)
	declareGlobal(`.jn-journey`,
		gwccss.Position.Relative,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.625)),
		gwccss.Raw("transition", "border-color var(--hcm-motion-fast,.15s) var(--jn-ease),background-color var(--hcm-motion-fast,.15s) var(--jn-ease)"),
	)
	declareGlobal(`.jn-journey::before`,
		gwccss.Raw("content", "\"\""),
		gwccss.Position.Absolute,
		gwccss.Left(gwccss.Zero),
		gwccss.Right(gwccss.Zero),
		gwccss.Top(gwccss.Zero),
		gwccss.H(gwccss.Px(3)),
		gwccss.Rounded(gwccss.RawLength("var(--jn-r3) var(--jn-r3) 0 0")),
		gwccss.Bg(gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-journey[data-stage="AWAITING_APPROVAL"]::before`,
		gwccss.Bg(gwccss.Var("jn-warning")),
	)
	declareGlobal(`.jn-journey[data-stage="COMPLETED"]::before`,
		gwccss.Bg(gwccss.Var("jn-success")),
	)
	declareGlobal(`.jn-journey[data-stage="BLOCKED"]::before,.jn-journey[data-stage="REJECTED"]::before,
.jn-journey[data-stage="FAILED"]::before`,
		gwccss.Bg(gwccss.Var("jn-danger")),
	)
	declareGlobal(`.jn-journey[data-stage="PROPOSED"]::before`,
		gwccss.Bg(gwccss.Var("jn-info")),
	)
	declareGlobal(`.jn-journey:hover`,
		gwccss.BorderColor(gwccss.Var("jn-control-border")),
	)
	declareGlobal(`.jn-journey:focus-within`,
		gwccss.Raw("box-shadow", "var(--jn-ring)"),
	)
	declareGlobal(`.jn-journey-top`,
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Rem(.75)),
	)
	declareGlobal(`.jn-journey h3`,
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(`.jn-journey h3 a`,
		gwccss.Raw("text-decoration", "none"),
		gwccss.TextColor(gwccss.Var("jn-ink")),
	)
	declareGlobal(`.jn-journey h3 a::after`,
		gwccss.Raw("content", "\"\""),
		gwccss.Position.Absolute,
		gwccss.Raw("inset", "0"),
		gwccss.Rounded(gwccss.RawLength("inherit")),
	)
	declareGlobal(`.jn-journey h3 a:hover`,
		gwccss.TextColor(gwccss.Var("jn-accent")),
	)
	declareGlobal(`.jn-journey-headline`,
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-journey-pay`,
		// One step above the card title: the pay change is the loudest fact
		// on a promotion card, and at the title's own size it stops being
		// one.
		gwccss.FontSize(gwccss.Rem(1.125)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
		gwccss.Tracking(gwccss.Ems(-.016)),
	)
	declareGlobal(`.jn-meta`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.RowGap(gwccss.Rem(.25)), gwccss.ColumnGap(gwccss.Rem(1)),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-meta-key`,
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-journey-foot`,
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.375)),
		gwccss.Raw("margin-top", "auto"),
		gwccss.Raw("padding-top", ".625rem"),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("jn-accent")),
	)
	declareGlobal(`.jn-journey-arrow`,
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("transform")), gwccss.S(.2), gwccss.Easing("var(--jn-ease)")),
	)
	declareGlobal(`.jn-journey:hover .jn-journey-arrow`,
		gwccss.Transform(gwccss.TranslateX(gwccss.Px(3))),
	)
	declareGlobal(`.jn-people-note`,
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Rem(.375)),
		gwccss.Raw("margin-top", "calc(var(--jn-s2) * -0.5)"),
		gwccss.Raw("margin-bottom", "var(--jn-s2)"),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-people-note-icon`,
		gwccss.Raw("flex", "none"),
		gwccss.TextColor(gwccss.Var("jn-info")),
		gwccss.Raw("margin-top", ".0625rem"),
	)
	declareGlobal(`.jn-peoplewrap`,
		gwccss.MaxHeight(gwccss.Rem(34)),
		gwccss.Raw("overflow-y", "auto"),
		gwccss.Raw("overscroll-behavior", "contain"),
	)
	declareGlobal(`table.jn-people`,
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(`.jn-people thead th`,
		gwccss.ZIndex(2),
	)
	declareGlobal(`.jn-people tbody th,.jn-people tbody td`,
		gwccss.H(gwccss.Rem(2.75)),
		gwccss.Raw("vertical-align", "middle"),
		gwccss.Raw("padding-top", ".375rem"),
		gwccss.Raw("padding-bottom", ".375rem"),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(`.jn-people tbody th`,
		gwccss.Raw("font-weight", "inherit"),
	)
	declareGlobal(`.jn-people-idcell`,
		gwccss.Position.Relative,
	)
	declareGlobal(`.jn-people-idbox`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Rem(.0625)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("text-align", "left"),
		gwccss.LineHeight(gwccss.Num(1.3)),
	)
	declareGlobal(`.jn-people-pick`,
		gwccss.Raw("font", "inherit"),
		gwccss.Raw("color", "inherit"),
		gwccss.Raw("background", "none"),
		gwccss.Raw("border", "0"),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("cursor", "pointer"),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.W(gwccss.Percent(100)),
	)
	declareGlobal(`.jn-people-pick:hover .jn-people-name`,
		gwccss.TextColor(gwccss.Var("jn-accent")),
	)
	declareGlobal(`.jn-people-name`,
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Tracking(gwccss.Ems(-.014)),
		gwccss.TextColor(gwccss.Var("jn-ink")),
	)
	declareGlobal(`.jn-people-title`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-people-number`,
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(`.jn-people-selected`,
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.25)),
		gwccss.Raw("margin-top", ".125rem"),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Tracking(gwccss.Ems(.04)),
		gwccss.Raw("text-transform", "uppercase"),
		gwccss.TextColor(gwccss.Var("jn-accent")),
	)
	declareGlobal(`.jn-people-selected-icon`,
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(`.jn-people-job`,
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-people-pay`,
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-people-count`,
		gwccss.Raw("font-weight", "600"),
	)
}
