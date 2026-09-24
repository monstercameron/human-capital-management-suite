package journeycss

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// declareJourneyMotion emits the checkpill tones, gauges, window strip, work
// items, actions rail, timeline, footer, responsive composition, motion, and
// print rules, in original order.
func declareJourneyMotion() {
	declareGlobal(`.jn-wait-explanation`,
		gwccss.Raw("border-inline-start", "3px solid var(--jn-warning)"),
	)
	declareGlobal(`.jn-wait-explanation .jn-facts`,
		gwccss.Raw("margin-top", "var(--jn-s2)"),
	)
	declareGlobal(`.jn-checkpill[data-tone="warning"]`,
		gwccss.Keyframes("jn-amber", jnAmberFrames...),
		gwccss.Animation(gwccss.RawDuration("2.8s"), gwccss.EaseInOut),
		gwccss.Raw("animation-delay", ".4s"),
		gwccss.Raw("animation-iteration-count", "infinite"),
	)
	declareGlobal(`.jn-checkpill[data-tone="danger"]`,
		gwccss.Keyframes("jn-alert", jnAlertFrames...),
		gwccss.Animation(gwccss.RawDuration("2.2s"), gwccss.EaseInOut),
		gwccss.Raw("animation-delay", ".3s"),
		gwccss.Raw("animation-iteration-count", "infinite"),
	)
	declareGlobal(`.jn-checkpill[data-tone="info"]`,
		gwccss.Keyframes("jn-breathe", jnBreatheFrames...),
		gwccss.Animation(gwccss.RawDuration("3.2s"), gwccss.EaseInOut),
		gwccss.Raw("animation-delay", ".5s"),
		gwccss.Raw("animation-iteration-count", "infinite"),
	)
	declareGlobal(`.jn-checkpill-icon`,
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(`.jn-check-body`,
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-check-msg`,
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(`.jn-check-code`,
		gwccss.Raw("margin-top", ".125rem"),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-gauges`,
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("jn-s2")),
		gwccss.GridCols(gwccss.Fr(1)),
		gwccss.Raw("margin-top", "var(--jn-s3)"),
	)
	declareGlobal(`.jn-gauges`,
		mediaRule(gwccss.RawMedia("(min-width:52rem)"), gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1))),
	)
	declareGlobal(`.jn-gauge`,
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.PaddingY(gwccss.Rem(.875)), gwccss.PaddingX(gwccss.Rem(1)),
		gwccss.Bg(gwccss.Var("jn-surface-sunk")),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-gauge .jn-subhead`,
		gwccss.Raw("margin-bottom", ".625rem"),
	)
	declareGlobal(`.jn-band,.jn-meter`,
		gwccss.W(gwccss.Percent(100)),
		gwccss.H(gwccss.RawLength("auto")),
		gwccss.Raw("overflow", "visible"),
	)
	declareGlobal(`.jn-band-track`,
		gwccss.Raw("fill", "var(--jn-surface-muted)"),
		gwccss.Raw("stroke", "var(--jn-control-border)"),
		gwccss.Raw("stroke-width", "1"),
	)
	declareGlobal(`.jn-band-fill`,
		gwccss.Raw("fill", "var(--jn-accent)"),
		gwccss.OpacityNum(gwccss.Num(.85)),
		gwccss.Raw("transform-box", "fill-box"),
		gwccss.Raw("transform-origin", "left center"),
		gwccss.Keyframes("jn-grow-x-svg", jnGrowXSVGFrames...),
		gwccss.Animation(gwccss.RawDuration(".6s"), gwccss.Easing("var(--jn-ease)")),
		gwccss.Raw("animation-delay", ".15s"),
		gwccss.Raw("animation-fill-mode", "both"),
	)
	declareGlobal(`.jn-band-tick`,
		gwccss.Raw("stroke", "var(--jn-control-border)"),
		gwccss.Raw("stroke-width", "1"),
		gwccss.Raw("stroke-dasharray", "2 2"),
	)
	declareGlobal(`.jn-band-marker`,
		gwccss.Raw("stroke-width", "3"),
		gwccss.Raw("stroke-linecap", "round"),
		gwccss.Keyframes("jn-drop", jnDropFrames...),
		gwccss.Animation(gwccss.RawDuration(".45s"), gwccss.Easing("var(--jn-ease)")),
		gwccss.Raw("animation-delay", ".5s"),
		gwccss.Raw("animation-fill-mode", "both"),
	)
	declareGlobal(`.jn-band-marker[data-which="current"]`,
		gwccss.Raw("stroke", "var(--jn-neutral)"),
	)
	declareGlobal(`.jn-band-marker[data-which="proposed"]`,
		gwccss.Raw("stroke", "var(--jn-accent-strong)"),
		gwccss.Raw("animation-delay", ".65s"),
	)
	declareGlobal(`.jn-meter-track`,
		gwccss.Raw("fill", "var(--jn-surface-muted)"),
		gwccss.Raw("stroke", "var(--jn-control-border)"),
		gwccss.Raw("stroke-width", "1"),
	)
	declareGlobal(`.jn-meter-fill`,
		gwccss.Raw("transform-box", "fill-box"),
		gwccss.Raw("transform-origin", "left center"),
		gwccss.Keyframes("jn-grow-x-svg", jnGrowXSVGFrames...),
		gwccss.Animation(gwccss.RawDuration(".7s"), gwccss.Easing("var(--jn-ease)")),
		gwccss.Raw("animation-delay", ".2s"),
		gwccss.Raw("animation-fill-mode", "both"),
	)
	declareGlobal(`.jn-meter[data-tone="success"] .jn-meter-fill`,
		gwccss.Raw("fill", "var(--jn-success)"),
	)
	declareGlobal(`.jn-meter[data-tone="warning"] .jn-meter-fill`,
		gwccss.Raw("fill", "var(--jn-warning)"),
	)
	declareGlobal(`.jn-meter[data-tone="danger"] .jn-meter-fill`,
		gwccss.Raw("fill", "var(--jn-danger)"),
		// Dual animation: two Keyframes() rules would emit two competing
		// animation-name declarations, so the pair stays one Raw shorthand.
		// The hashed names resolve to the jn-grow-x-svg / jn-alert blocks
		// emitted by their single-animation owners; TestJourneyKeyframesResolve
		// fails loudly if a frames edit ever desyncs them.
		gwccss.Raw("animation", "jn-grow-x-svg-12bsajj0jnz3p .7s var(--jn-ease) .2s both,jn-alert-20wtq4njut0zx 2.2s ease-in-out 1s infinite"),
	)
	declareGlobal(`.jn-gauge-scale`,
		gwccss.Display.Flex,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Rem(.5)),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
		gwccss.Raw("margin-top", ".125rem"),
	)
	declareGlobal(`.jn-gauge-legend`,
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.Fr(1)),
		gwccss.RowGap(gwccss.Rem(.125)), gwccss.ColumnGap(gwccss.Rem(.625)),
		gwccss.Raw("margin-top", ".625rem"),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Items.Baseline,
	)
	declareGlobal(`.jn-gauge-legend dt`,
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.375)),
	)
	declareGlobal(`.jn-gauge-legend dt[data-which]::before`,
		gwccss.Raw("content", "\"\""),
		gwccss.W(gwccss.Rem(.5)),
		gwccss.H(gwccss.Rem(.5)),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-xs,4px)")),
		gwccss.Bg(gwccss.Var("jn-neutral")),
	)
	declareGlobal(`.jn-gauge-legend dt[data-which="proposed"]::before`,
		gwccss.Bg(gwccss.Var("jn-accent-strong")),
	)
	declareGlobal(`.jn-gauge-legend dd`,
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("text-align", "end"),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-gauge-note`,
		gwccss.Raw("margin-top", ".625rem"),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-window`,
		gwccss.Raw("margin-top", "var(--jn-s3)"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.PaddingY(gwccss.Rem(.875)), gwccss.PaddingX(gwccss.Rem(1)),
		gwccss.Bg(gwccss.Var("jn-surface-sunk")),
	)
	declareGlobal(`.jn-strip`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.5)),
	)
	declareGlobal(`.jn-strip`,
		mediaRule(gwccss.RawMedia("(min-width:44rem)"), gwccss.FlexDir.Row, gwccss.Gap(gwccss.Zero)),
	)
	declareGlobal(`.jn-stop`,
		gwccss.Position.Relative,
		gwccss.Raw("flex", "1 1 0"),
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.0625)),
		gwccss.Raw("padding-inline-start", "1.125rem"),
	)
	declareGlobal(`.jn-stop`,
		mediaRule(gwccss.RawMedia("(min-width:44rem)"), gwccss.Raw("padding-inline-start", "0"), gwccss.Raw("padding-top", "1.125rem")),
	)
	declareGlobal(`.jn-stop-dot`,
		gwccss.Position.Absolute,
		gwccss.Raw("inset-inline-start", "0"),
		gwccss.Top(gwccss.Rem(.375)),
		gwccss.W(gwccss.Rem(.625)),
		gwccss.H(gwccss.Rem(.625)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Border(gwccss.Px(2), gwccss.Var("jn-control-border")),
		gwccss.Bg(gwccss.Var("jn-surface")),
	)
	declareGlobal(`.jn-stop-dot`,
		mediaRule(gwccss.RawMedia("(min-width:44rem)"), gwccss.Raw("inset-inline-start", "0"), gwccss.Top(gwccss.Zero)),
	)
	declareGlobal(`.jn-stop::before`,
		mediaRule(gwccss.RawMedia("(min-width:44rem)"), gwccss.Raw("content", "\"\""), gwccss.Position.Absolute, gwccss.Raw("inset-inline-start", ".625rem"), gwccss.Raw("inset-inline-end", "0"), gwccss.Top(gwccss.Rem(.25)), gwccss.H(gwccss.Px(2)), gwccss.Bg(gwccss.Var("jn-hairline"))),
	)
	declareGlobal(`.jn-stop:last-child::before`,
		mediaRule(gwccss.RawMedia("(min-width:44rem)"), gwccss.Display.None),
	)
	declareGlobal(`.jn-stop[data-which="effective"] .jn-stop-dot`,
		gwccss.BorderColor(gwccss.Var("jn-accent")),
		gwccss.Bg(gwccss.Var("jn-accent")),
	)
	declareGlobal(`.jn-stop-label`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("letter-spacing", "var(--hcm-tracking-caps,.05em)"),
		gwccss.Raw("text-transform", "uppercase"),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-stop-value`,
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.jn-stop[data-which="effective"] .jn-stop-value`,
		gwccss.TextColor(gwccss.Var("jn-accent")),
	)
	declareGlobal(`.jn-workitems`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.75)),
	)
	declareGlobal(`.jn-workitem`,
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.PaddingY(gwccss.Rem(.75)), gwccss.PaddingX(gwccss.Rem(.875)),
		gwccss.Bg(gwccss.Var("jn-surface-sunk")),
	)
	declareGlobal(`.jn-workitem-top`,
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("margin-bottom", ".375rem"),
	)
	declareGlobal(`.jn-workitem-kind`,
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.375)),
	)
	declareGlobal(`.jn-workitem-icon`,
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-workitem-lines`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.125)),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-evidence`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Rem(.375)),
	)
	declareGlobal(`.jn-evidence li`,
		gwccss.Bg(gwccss.Var("jn-surface-sunk")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.PaddingY(gwccss.Rem(.1875)), gwccss.PaddingX(gwccss.Rem(.5)),
		gwccss.Raw("font-family", "var(--jn-mono)"),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(`.jn-quiet`,
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Rem(.625)),
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Raw("border", "1px dashed var(--jn-control-border)"),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.PaddingY(gwccss.Rem(.875)), gwccss.PaddingX(gwccss.Rem(1)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Bg(gwccss.Var("jn-surface-sunk")),
	)
	declareGlobal(`.jn-quiet-icon`,
		gwccss.Raw("flex", "none"),
		gwccss.Raw("margin-top", ".125rem"),
	)
	declareGlobal(`.jn-actions`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.VarLength("jn-s2")),
	)
	declareGlobal(`.jn-action`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.625)),
		gwccss.BorderTop(gwccss.Px(3), gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-action[data-variant="primary"]`,
		gwccss.Raw("border-top-color", "var(--jn-accent)"),
	)
	declareGlobal(`.jn-action[data-variant="danger"]`,
		gwccss.Raw("border-top-color", "var(--jn-danger)"),
	)
	declareGlobal(`.jn-action h3`,
		gwccss.Tracking(gwccss.Ems(-.014)),
	)
	declareGlobal(`.jn-action-desc`,
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-actsas`,
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.375)),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-info")),
		gwccss.Bg(gwccss.Var("jn-info-soft")),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.PaddingY(gwccss.Rem(.3125)), gwccss.PaddingX(gwccss.Rem(.5)),
	)
	declareGlobal(`.jn-blocked`,
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Rem(.375)),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-warning")),
		gwccss.Bg(gwccss.Var("jn-warning-soft")),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.PaddingY(gwccss.Rem(.3125)), gwccss.PaddingX(gwccss.Rem(.5)),
	)
	declareGlobal(`.jn-confirm-trigger`,
		gwccss.W(gwccss.Percent(100)),
	)
	declareGlobal(`.jn-confirm-close-label`,
		gwccss.Display.None,
	)
	// Open, the trigger reads "Cancel review". A cancel is never the primary
	// action, whatever tone the trigger has closed, so it takes the secondary
	// look; the dialog's own submit is the primary.
	declareGlobal(`.jn-confirm[open] > summary.jn-btn`,
		gwccss.Raw("background", "var(--jn-surface)"),
		gwccss.TextColor(gwccss.Var("jn-accent")),
		gwccss.BorderColor(gwccss.Var("jn-control-border")),
		gwccss.Raw("box-shadow", "none"),
	)
	declareGlobal(`.jn-confirm[open] > summary .jn-confirm-open-label`,
		gwccss.Display.None,
	)
	declareGlobal(`.jn-confirm[open] > summary .jn-confirm-close-label`,
		gwccss.Display.Inline,
	)
	declareGlobal(`.jn-confirm-dialog`,
		gwccss.Position.Fixed,
		gwccss.Raw("inset", "0"),
		gwccss.Raw("margin", "auto"),
		gwccss.Raw("width", "min(38rem, calc(100vw - 1.25rem))"),
		gwccss.Raw("max-height", "calc(100dvh - 1.5rem)"),
		gwccss.Raw("padding", "0"),
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.TextColor(gwccss.Var("jn-ink")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-control-border")),
		gwccss.Rounded(gwccss.VarLength("jn-r3")),
		gwccss.Raw("box-shadow", "0 1.5rem 4rem rgba(0,0,0,.35)"),
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(`.jn-confirm-dialog[open]`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
	)
	declareGlobal(`.jn-confirm-dialog::backdrop`,
		gwccss.Raw("background", "rgba(8,15,22,.72)"),
	)
	declareGlobal(`.jn-confirm-head`,
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Padding(gwccss.Rem(1)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-confirm-title`,
		gwccss.FontSize(gwccss.Rem(1.125)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(`.jn-confirm-cancel`,
		gwccss.Raw("min-height", "2.5rem"),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("flex", "0 0 2.5rem"),
		gwccss.Raw("width", "2.5rem"),
		gwccss.Raw("padding", "0"),
		mediaRule(gwccss.RawMedia("(min-width:22.5625rem)"),
			gwccss.Raw("flex", "0 1 auto"),
			gwccss.Raw("width", "auto"),
			gwccss.PaddingY(gwccss.Rem(.625)), gwccss.PaddingX(gwccss.Rem(1)),
		),
	)
	declareGlobal(`.jn-confirm-cancel-label`,
		gwccss.Display.None,
		mediaRule(gwccss.RawMedia("(min-width:22.5625rem)"), gwccss.Display.Inline),
	)
	declareGlobal(`.jn-confirm-cancel-glyph`,
		gwccss.Display.Inline,
		gwccss.FontSize(gwccss.Rem(1.25)),
		mediaRule(gwccss.RawMedia("(min-width:22.5625rem)"), gwccss.Display.None),
	)
	declareGlobal(`.jn-confirm-scroll`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.875)),
		gwccss.Raw("min-height", "0"),
		gwccss.Raw("overflow-y", "auto"),
		gwccss.Raw("overscroll-behavior", "contain"),
		gwccss.Padding(gwccss.Rem(1)),
	)
	declareGlobal(`.jn-confirm-facts`,
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Fr(1)),
		gwccss.Gap(gwccss.Rem(.5)),
		mediaRule(gwccss.RawMedia("(min-width:30rem)"), gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))))),
	)
	// What the reader is confirming is the most important text in the
	// dialog; at 13px it was its smallest.
	declareGlobal(`.jn-confirm-facts .jn-fact dd`,
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(`.jn-confirm-note`,
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Rem(.375)),
		gwccss.Padding(gwccss.Rem(.5)),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.Bg(gwccss.Var("jn-warning-soft")),
		gwccss.TextColor(gwccss.Var("jn-warning")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(`.jn-confirm-icon`,
		gwccss.Raw("flex", "none"),
		gwccss.Raw("margin-top", ".0625rem"),
	)
	// PROMOUX-010: .jn-confirm-surface and .jn-confirm-backdrop are the
	// ui.Overlay-rendered layer a live client mounts once the <details>
	// above opens (review_surface.go). They exist only in the browser --
	// ui.Overlay's server build returns a plain Fragment, so the SSR/test
	// path keeps rendering .jn-confirm-body exactly as before, in flow --
	// which is why these rules can use position:fixed freely: they never
	// touch the no-JS document, only the live one, where they are what
	// keeps the review contained (RED: "moves unrelated sections") and its
	// final action reachable (RED: "pushes the final action below the
	// viewport") regardless of how tall the People and Journeys sections
	// around it are.
	// The narrow base case is a full-width bottom sheet; from about 481px (30.0625rem) up the
	// panel is centred by inset and auto margins. Never by transform: the
	// jn-slidein keyframes end at transform:none with fill-mode both, which
	// silently replaced translate(-50%,-50%) and left the panel's top-left
	// corner at the viewport centre, its fields and actions below the fold.
	declareGlobal(`.jn-confirm-surface`,
		gwccss.Position.Fixed,
		gwccss.Raw("inset", "auto 0 0 0"),
		gwccss.Raw("margin", "0"),
		gwccss.Raw("height", "fit-content"),
		gwccss.Raw("width", "100%"),
		gwccss.Raw("max-height", "calc(100vh - 2rem)"),
		gwccss.Raw("overflow-y", "auto"),
		gwccss.Raw("overscroll-behavior", "contain"),
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.Padding(gwccss.Rem(1)),
		gwccss.Raw("box-shadow", "var(--jn-shadow-lift)"),
		gwccss.Keyframes("jn-slidein", jnSlideinFrames...),
		gwccss.Animation(gwccss.RawDuration(".18s"), gwccss.Easing("var(--jn-ease)")),
		gwccss.Raw("animation-fill-mode", "both"),
	)
	// Dynamic viewport units where supported (a browser without them drops
	// the declaration and keeps the vh bound above), so mobile browser
	// chrome cannot push the action bar out of reach.
	declareGlobal(`:root .jn-confirm-surface`,
		gwccss.Raw("max-height", "calc(100dvh - 2rem)"),
	)
	// Sticky insets are measured from the scroll container's content box, so
	// with the panel's 1rem padding a bar pinned at bottom:0 left a 1rem strip
	// where scrolled fields showed through beneath the actions. Pin the bar
	// to the panel's inner edge and carry the padding inside it instead.
	declareGlobal(`:root .jn-confirm-surface .jn-confirm-actionbar:where(*)`,
		gwccss.Raw("bottom", "-1rem"),
		gwccss.Raw("margin-bottom", "-1rem"),
		gwccss.Raw("padding-bottom", "1.625rem"),
	)
	declareGlobal(`:root .jn-confirm-surface`,
		mediaRule(gwccss.RawMedia("(min-width:30.0625rem)"),
			gwccss.Raw("inset", "0"),
			gwccss.Raw("margin", "auto"),
			gwccss.Raw("width", "min(34rem,calc(100vw - 2rem))"),
		),
	)
	// Inside the live overlay the body is the surface's child, not the
	// in-flow reveal the rule above styles, so it fell back to display:block
	// and every field, fact list and label ran together with no spacing.
	declareGlobal(`.jn-confirm-surface>.jn-confirm-body`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.875)),
	)
	declareGlobal(`.jn-confirm-backdrop`,
		gwccss.Position.Fixed,
		gwccss.Raw("inset", "0"),
		gwccss.Raw("background", "color-mix(in srgb,var(--jn-ink) 45%,transparent)"),
	)
	declareGlobal(`.jn-confirm-actionbar`,
		gwccss.Position.Sticky,
		gwccss.Bottom(gwccss.Zero),
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Rem(.625)),
		gwccss.Raw("justify-content", "flex-end"),
		gwccss.PaddingY(gwccss.Rem(.625)),
		gwccss.Raw("margin-top", ".125rem"),
		gwccss.Bg(gwccss.Var("jn-surface")),
	)
	// As with shared popovers, flatten the open disclosure's anonymous box
	// so the fixed overlay remains visible to accessibility, not only paint.
	declareGlobal(`.jn-confirm[open]::details-content`,
		gwccss.Raw("display", "contents"),
		gwccss.Raw("content-visibility", "visible"),
	)
	declareGlobal(`.jn-confirm-actionbar>.jn-btn`,
		gwccss.Raw("flex-shrink", "0"),
		gwccss.Raw("max-width", "100%"),
		gwccss.Raw("white-space", "normal"),
	)
	// Header close glyphs may be square; a labelled action-bar Cancel is not.
	declareGlobal(`.jn-confirm-actionbar>.jn-confirm-cancel`,
		gwccss.Raw("flex", "0 0 auto"),
		gwccss.Raw("width", "auto"),
		gwccss.PaddingY(gwccss.Rem(.625)), gwccss.PaddingX(gwccss.Rem(1)),
	)
	// The cancel control is deliberately ordered and styled to never
	// outrank the final action beside it (RED: "makes Cancel the most
	// visually prominent control"): same size, secondary tone, and second
	// in reading and DOM order, with the submit button -- whatever variant
	// the caller gave it -- placed after it.
	declareGlobal(`.jn-confirm-cancel`,
		gwccss.Raw("order", "0"),
	)
	declareGlobal(`.jn-confirm-status`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Raw("min-height", "1em"),
	)
	declareGlobal(`.jn-timeline`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
	)
	declareGlobal(`.jn-tl`,
		gwccss.Position.Relative,
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("padding-bottom", "var(--jn-s2)"),
	)
	declareGlobal(`.jn-tl:last-child`,
		gwccss.Raw("padding-bottom", "0"),
	)
	declareGlobal(`.jn-tl::before`,
		gwccss.Raw("content", "\"\""),
		gwccss.Position.Absolute,
		gwccss.Raw("inset-inline-start", ".4375rem"),
		gwccss.Top(gwccss.Rem(1.125)),
		gwccss.Bottom(gwccss.Zero),
		gwccss.W(gwccss.Px(2)),
		gwccss.Bg(gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-tl:last-child::before`,
		gwccss.Display.None,
	)
	declareGlobal(`.jn-tldot`,
		gwccss.Raw("flex", "none"),
		gwccss.W(gwccss.Rem(1)),
		gwccss.H(gwccss.Rem(1)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Raw("margin-top", ".1875rem"),
		gwccss.Border(gwccss.Px(3), gwccss.Var("jn-neutral")),
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.Position.Relative,
		gwccss.ZIndex(1),
	)
	declareGlobal(`.jn-tl[data-tone="info"] .jn-tldot`,
		gwccss.BorderColor(gwccss.Var("jn-info")),
	)
	declareGlobal(`.jn-tl[data-tone="success"] .jn-tldot`,
		gwccss.BorderColor(gwccss.Var("jn-success")),
	)
	declareGlobal(`.jn-tl[data-tone="warning"] .jn-tldot`,
		gwccss.BorderColor(gwccss.Var("jn-warning")),
	)
	declareGlobal(`.jn-tl[data-tone="danger"] .jn-tldot`,
		gwccss.BorderColor(gwccss.Var("jn-danger")),
	)
	declareGlobal(`.jn-tl:first-child .jn-tldot`,
		gwccss.Keyframes("jn-halo", jnHaloFrames...),
		gwccss.Animation(gwccss.RawDuration("2.6s"), gwccss.Easing("var(--jn-ease)")),
		gwccss.Raw("animation-iteration-count", "infinite"),
	)
	declareGlobal(`.jn-tlbody`,
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.125)),
	)
	declareGlobal(`.jn-tlat`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.jn-tltitle`,
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-tldetail`,
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-footer`,
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Bg(gwccss.Var("jn-surface")),
	)
	declareGlobal(`.jn-footer-inner`,
		gwccss.Raw("padding-top", "var(--jn-s3)"),
		gwccss.Raw("padding-bottom", "var(--jn-s3)"),
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.25)),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-provenance`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.RowGap(gwccss.Rem(.25)), gwccss.ColumnGap(gwccss.Rem(1)),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded) :is(img,svg,video,canvas)`,
		gwccss.MaxWidth(gwccss.Percent(100)),
	)
	declareGlobal(`:where(.jn-shell,.jn-page,.jn-pagehead,.jn-card,.jn-cardhead,.jn-grid,.jn-griditem,
.jn-columns,.jn-maincol,.jn-rail,.jn-field,.jn-fieldgrid,.jn-fact,.jn-tlbody,
.jn-network-stage,.jn-proxy-copy)`,
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded) :is(input,select,textarea,button)`,
		gwccss.MaxWidth(gwccss.Percent(100)),
	)
	declareGlobal(`:where(.jn-pagehead,.jn-cardhead,.jn-toolbar,.jn-actions,.jn-provenance)`,
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(`:where(.jn-title,.jn-cardtitle,.jn-tltitle,.jn-tldetail,.jn-fact dd)`,
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(`.jn-tablewrap`,
		gwccss.MaxWidth(gwccss.Percent(100)),
		gwccss.Raw("overscroll-behavior-inline", "contain"),
		gwccss.Raw("scrollbar-width", "thin"),
	)
	// One request's short reference sits beside the person's name so several
	// requests for the same person with the same change can be told apart
	// (UXLIVE-010). It is a handle, not the identity, so it reads quieter
	// than the name it follows.
	declareGlobal(`.jn-journey-ref`,
		// The reference is a Latin token sitting beside a name that may be
		// written in either direction. Without isolation the two merge into
		// one bidi run, the span's inline-start ends up on the far side of
		// the pair, and its margin lands outside them instead of between
		// them -- which is how "Adrian F54F9D" came out as "AdrianF54F9D" in
		// Arabic. Isolating the token makes it an atomic run placed by the
		// paragraph's own direction, so the margin is between the two in
		// both.
		gwccss.Raw("unicode-bidi", "isolate"),
		gwccss.Raw("margin-inline-start", ".5rem"),
		gwccss.Raw("font-size", "0.75rem"),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("letter-spacing", ".04em"),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
		gwccss.Raw("opacity", ".7"),
	)
	// The step a run stopped at names its state in visible text beside the
	// step label, so the stopped step is not distinguished from an unstarted
	// one by hue alone (UXLIVE-019).
	//
	// It inherits the step label's own colour rather than naming one: the
	// label is already themed for a stopped step in both colour modes, and a
	// second opinion about danger is how this badge first shipped at 1.11:1
	// against the card.
	declareGlobal(`.jn-stepstate`,
		gwccss.Display.InlineBlock,
		gwccss.Raw("margin-inline-start", "var(--jn-s2)"),
		gwccss.Raw("font-size", "0.75rem"),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("letter-spacing", ".02em"),
		gwccss.Raw("color", "inherit"),
	)
	// Every paragraph on this page was being set at body size, whatever this
	// stylesheet asked for, so a card's role change, its pay, its dates and
	// its next step all came out at 16px and the card had no hierarchy at
	// all. That is what made it look like a wall of text.
	//
	// The cause is the product type scale, which reaches into this page
	// because the page is embedded in it:
	//
	//	:where(.app-shell,.jn-embedded) :is(.prose,.prose p,...,p,li,dd,dt,...)
	//
	// :where() contributes nothing, but :is() takes the specificity of its
	// most specific argument, and `.prose p` makes that (0,1,1) -- more than
	// the (0,1,0) of a plain class here. This is the same trap that flattened
	// the shell's own list rows (UXLIVE-020's `:is()` scoping fix); it is
	// worth stating twice, because a selector list that looks like a type
	// selector is not scored like one.
	//
	// The product rule has since been split so its bare-element half scores
	// nothing and every component is heard again -- that is the general fix,
	// and it is where this belongs. These stay as an explicit local pin:
	// this card's scale is the thing the page is read by, and scoping it to
	// (0,2,0) means a future high-specificity rule cannot flatten it again
	// without someone deciding to.
	declareGlobal(`:is(.jn-page,.jn-embedded) .jn-journey-headline`,
		gwccss.Raw("font-size", "0.875rem"),
	)
	declareGlobal(`:is(.jn-page,.jn-embedded) .jn-journey-pay`,
		gwccss.Raw("font-size", "1.125rem"),
	)
	declareGlobal(`:is(.jn-page,.jn-embedded) .jn-meta`,
		gwccss.Raw("font-size", "0.8125rem"),
	)
	declareGlobal(`:is(.jn-page,.jn-embedded) .jn-journey-next`,
		gwccss.Raw("font-size", "0.8125rem"),
	)
	declareGlobal(`:is(.jn-page,.jn-embedded) .jn-journey-foot`,
		gwccss.Raw("font-size", "0.8125rem"),
	)
	// The same rule caps every paragraph at the prose measure, which is a
	// reading width for running text and not for a card that is already as
	// wide as it is allowed to be.
	declareGlobal(`:is(.jn-page,.jn-embedded) .jn-journey :is(p,li,dd,dt)`,
		gwccss.Raw("max-inline-size", "none"),
	)

	// The journeys list's subject groups shipped with no rules at all: the
	// head's three parts stacked as block boxes, so every card was preceded
	// by the subject on one line, "1 request" on the next and its status on
	// a third, and each group opened its own multi-column grid for the one
	// card it usually holds -- leaving three quarters of every row empty.
	// What follows is the layout that markup always assumed.
	//
	// Groups flow into the same track width a card wants, so several
	// one-request subjects sit side by side instead of one per row.
	declareGlobal(`.jn-journey-groups`,
		gwccss.Raw("grid-template-columns", "repeat(auto-fill,minmax(min(21rem,100%),1fr))"),
		gwccss.Raw("gap", "var(--jn-s3)"),
		gwccss.Raw("align-items", "start"),
	)
	// A subject with more than one request takes the whole row, so its own
	// cards lay out beside each other rather than stacking inside one narrow
	// track. An engine without :has() keeps the single-track behaviour,
	// which is what this page did before.
	declareGlobal(`.jn-journey-group:has(.jn-griditem+.jn-griditem)`,
		gwccss.Raw("grid-column", "1/-1"),
	)
	declareGlobal(`.jn-journey-group-head`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Raw("align-items", "baseline"),
		gwccss.Raw("gap", "0 var(--jn-s1)"),
		gwccss.Raw("padding-block-end", ".4375rem"),
		gwccss.Raw("margin-block-end", ".75rem"),
		gwccss.Raw("border-block-end", "1px solid var(--jn-hairline)"),
	)
	// The group head names a subject; the cards under it carry the requests.
	// It is set quieter than a card title on purpose, so scanning the page
	// reads the requests rather than the headings.
	declareGlobal(`.jn-journey-group-subject`,
		gwccss.Raw("margin", "0"),
		gwccss.Raw("font-size", "0.875rem"),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("letter-spacing", "-.01em"),
		gwccss.Raw("min-inline-size", "0"),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	// The statuses read as part of the head's one sentence -- "Omar, five
	// requests, blocked and failed" -- so they follow the count rather than
	// being pushed to the end of the row. A subject with several requests
	// takes the full page width, and the end of that row is far enough away
	// that the connection is lost.
	declareGlobal(`.jn-journey-group-statuses`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Raw("gap", ".375rem"),
		gwccss.Raw("margin-inline-start", ".25rem"),
	)

	// A heading and the count of what is under it are one statement, so they
	// are set beside each other. The default head pushes its two ends apart,
	// which reads correctly when the second end is an action and badly when
	// it is a count on a page 1300 pixels wide.
	declareGlobal(`.jn-sectionhead-inline`,
		gwccss.Raw("justify-content", "flex-start"),
		gwccss.Raw("gap", ".625rem"),
	)

	// The card itself. Its parts were all set at the same weight and the
	// same distance apart, so a reader had to read every line to find the
	// one that mattered. The order of importance is: whose request it is,
	// what it changes, what it pays, and what happens next.
	declareGlobal(`.jn-journey`,
		gwccss.Raw("padding", "1.125rem"),
		gwccss.Raw("gap", ".5rem"),
	)
	declareGlobal(`.jn-journey h3`,
		gwccss.Raw("font-size", "1rem"),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("letter-spacing", "-.012em"),
		gwccss.Raw("line-height", "1.25"),
	)
	// The role change follows its own title closely, as a subtitle does.
	declareGlobal(`.jn-journey-headline`,
		gwccss.Raw("margin-block-start", "-.25rem"),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	// Effective and Updated are two facts, not one run of words. They are
	// separated by space alone: a card is narrow enough that they wrap
	// often, and a border divider draws a rule at the start of every wrapped
	// line, where there is nothing to divide. Space works on both lines and
	// adds nothing for a screen reader to announce.
	declareGlobal(`.jn-journey>.jn-meta`,
		gwccss.Raw("column-gap", "1rem"),
		gwccss.Raw("row-gap", ".1875rem"),
	)
	// The next step is the actionable line on the card, and it had no rule
	// of its own at all. The edge marks it; the value carries the weight.
	// Colour is never the only signal here -- the line says what the step is.
	declareGlobal(`.jn-journey-next`,
		gwccss.Raw("border-inline-start", "2px solid var(--jn-hairline)"),
		gwccss.Raw("padding-inline-start", ".5625rem"),
		gwccss.Raw("font-size", "0.8125rem"),
	)
	declareGlobal(`.jn-journey-next .jn-meta-value`,
		gwccss.Raw("color", "var(--jn-ink)"),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-journey[data-stage="AWAITING_APPROVAL"] .jn-journey-next`,
		gwccss.Raw("border-inline-start-color", "var(--jn-warning)"),
	)
	declareGlobal(`.jn-journey[data-stage="BLOCKED"] .jn-journey-next,
.jn-journey[data-stage="FAILED"] .jn-journey-next,
.jn-journey[data-stage="REPAIR_REQUIRED"] .jn-journey-next`,
		gwccss.Raw("border-inline-start-color", "var(--jn-danger)"),
	)

	// The authorized diagnostics disclosure had no rules either: a bare
	// summary at body size, competing with the request it belongs to. It is
	// secondary by nature, so it is set that way.
	//
	// Every control inside the card is given a stacking position, because
	// the card's title is a stretched link whose overlay covers the whole
	// card: without this the disclosure and its copy controls sit underneath
	// it, and a click opens the request instead of doing what it says.
	declareGlobal(`.jn-journey-technical,.jn-journey-technical summary,.jn-journey .jn-copy-btn`,
		gwccss.Position.Relative,
		gwccss.Raw("z-index", "1"),
	)
	declareGlobal(`.jn-journey-technical>summary`,
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("color", "var(--jn-ink-muted)"),
		gwccss.Raw("font-size", "0.75rem"),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("inline-size", "fit-content"),
		gwccss.Raw("border-radius", "var(--jn-r1)"),
		gwccss.Raw("padding", ".125rem .3125rem"),
		gwccss.Raw("margin-inline-start", "-.3125rem"),
	)
	declareGlobal(`.jn-journey-technical>summary:hover`,
		gwccss.Raw("color", "var(--jn-ink)"),
	)
	declareGlobal(`.jn-journey-technical[open]>summary`,
		gwccss.Raw("color", "var(--jn-ink)"),
		gwccss.Raw("margin-block-end", ".375rem"),
	)
	declareGlobal(`.jn-tech-body`,
		gwccss.Display.Grid,
		gwccss.Raw("gap", ".25rem"),
	)
	declareGlobal(`.jn-tech-row`,
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "center"),
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Raw("gap", ".375rem"),
		gwccss.Raw("font-size", "0.75rem"),
	)
	// The copy control is a real button and stays one: it keeps a hit area a
	// pointer can find and a focus ring a keyboard can see.
	declareGlobal(`.jn-copy-btn`,
		gwccss.Raw("appearance", "none"),
		gwccss.Raw("background", "transparent"),
		gwccss.Raw("border", "1px solid var(--jn-hairline)"),
		gwccss.Raw("border-radius", "var(--jn-r1)"),
		gwccss.Raw("color", "var(--jn-ink-muted)"),
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("font", "inherit"),
		gwccss.Raw("font-size", "0.75rem"),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("padding", ".0625rem .375rem"),
		gwccss.Raw("min-block-size", "1.5rem"),
	)
	declareGlobal(`.jn-copy-btn:hover`,
		gwccss.Raw("color", "var(--jn-ink)"),
		gwccss.Raw("border-color", "var(--jn-control-border)"),
	)
	declareGlobal(`.jn-copy-btn:focus-visible`,
		gwccss.Raw("box-shadow", "var(--jn-ring)"),
		gwccss.Raw("outline", "none"),
	)

	// The governed position picker (UXLIVE-011) is productui's control
	// rendered inside this page, and this page had no rules for it at all:
	// its title and three labels are a <strong> and three <small>s in normal
	// flow, so they rendered as one unbroken run -- "Sales
	// DirectorSalesBoston, MAOpen now". The control is not the defect; a
	// page that borrows a component and does not lay it out is.
	//
	// The option is a row: the radio, then the position's identity, then its
	// availability at the end. The labels are separated by real space rather
	// than by punctuation the reader would also hear read aloud.
	declareGlobal(`.position-picker`,
		gwccss.Display.Block,
		gwccss.Raw("border", "1px solid var(--jn-hairline)"),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.Raw("padding", "var(--jn-s3)"),
		gwccss.Raw("margin", "0"),
		gwccss.Raw("min-inline-size", "0"),
	)
	declareGlobal(`.position-picker>legend`,
		gwccss.Raw("padding-inline", "var(--jn-s2)"),
		gwccss.Raw("font-size", "0.8125rem"),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("color", "var(--jn-ink-muted)"),
	)
	declareGlobal(`.position-picker-options`,
		gwccss.Display.Grid,
		gwccss.Raw("gap", "var(--jn-s2)"),
		gwccss.Raw("margin", "0"),
		gwccss.Raw("padding", "0"),
		gwccss.Raw("list-style", "none"),
		gwccss.Raw("max-block-size", "18rem"),
		gwccss.Raw("overflow-y", "auto"),
	)
	// A list item takes the prose measure by default; an option row spans
	// the list, or two thirds of the picker sat empty beside every option.
	declareGlobal(`.position-picker-option`,
		gwccss.Raw("max-inline-size", "none"),
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Raw("align-items", "baseline"),
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Raw("gap", "var(--jn-s2)"),
		gwccss.Raw("padding", "var(--jn-s2)"),
		gwccss.Raw("border", "1px solid var(--jn-hairline)"),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
	)
	// The chosen position is marked on its whole row, as selected choice
	// cards are elsewhere in the product (appearance, organization
	// visibility), not by the 13px radio alone.
	declareGlobal(`.position-picker-option:has(input:checked)`,
		gwccss.Raw("border-color", "var(--jn-accent)"),
		gwccss.Raw("background", "var(--jn-accent-soft)"),
	)
	declareGlobal(`.position-picker-option:hover`,
		gwccss.Raw("border-color", "var(--jn-control-border)"),
	)
	declareGlobal(`.position-picker-option>label`,
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "baseline"),
		gwccss.Raw("gap", "var(--jn-s2)"),
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("min-inline-size", "0"),
	)
	// The identity stacks: the title reads as the choice, the organization,
	// manager and location as what distinguishes two positions with the same
	// title -- which this catalog has, so they are not decoration.
	declareGlobal(`.position-picker-option-main`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Raw("align-items", "baseline"),
		gwccss.Raw("gap", "0 var(--jn-s2)"),
		gwccss.Raw("min-inline-size", "0"),
	)
	// The position's title leads its row at list size (14px), a step under
	// the section heading above the picker rather than level with it.
	declareGlobal(`.position-picker-option-main>strong`,
		gwccss.Raw("font-size", "0.875rem"),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.position-picker-option-main>small`,
		gwccss.Raw("color", "var(--jn-ink-muted)"),
		gwccss.Raw("font-size", "0.8125rem"),
	)
	// An empty <small> is what a position with no recorded manager produces.
	// It would otherwise still occupy a gap, so two options would disagree
	// about their own spacing for a reason the reader cannot see.
	declareGlobal(`.position-picker-option-main>small:empty`,
		gwccss.Display.None,
	)
	declareGlobal(`.position-picker-option-meta`,
		gwccss.Raw("color", "var(--jn-ink-muted)"),
		gwccss.Raw("font-size", "0.8125rem"),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(`.position-picker-empty`,
		gwccss.Raw("border-style", "dashed"),
	)
	declareGlobal(`.position-picker-empty-title`,
		gwccss.Raw("margin", "var(--jn-s2) 0 var(--jn-s1)"),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.position-picker-empty-detail`,
		gwccss.Display.Block,
		gwccss.Raw("color", "var(--jn-ink-muted)"),
	)
	// The current-versus-proposed table is the artifact reviewers read, and
	// its Change column is the point. One long value in one cell used to
	// widen the auto-layout table past its wrapper and carry that column off
	// the screen (UXLIVE-003). Fixed layout keeps every column on screen and
	// lets an unexpectedly long value wrap inside its own cell instead.
	declareGlobal(`.jn-compare`,
		gwccss.Raw("table-layout", "fixed"),
		gwccss.W(gwccss.Percent(100)),
	)
	// white-space is reset with the wrap: the change cell is styled nowrap so
	// an amount never splits mid-number, which also stopped the cell wrapping
	// at all and kept 41px of the table off its wrapper. The selector is :is
	// rather than :where because .jn-num sets that nowrap and the zero
	// specificity of :where lost to it; the chip and the amount inside keep
	// their own nowrap, so only the space between them wraps.
	declareGlobal(`.jn-compare :is(td,th)`,
		gwccss.Raw("overflow-wrap", "anywhere"),
		gwccss.Raw("white-space", "normal"),
	)
	// The change cell is the one `.jn-table td.jn-change` holds at nowrap,
	// which outranks the rule above; this names it at the same shape inside
	// the comparison table so the chip and the amount may sit on two lines
	// while neither is broken internally.
	declareGlobal(`.jn-compare.jn-table :is(td,th).jn-change`,
		gwccss.Raw("white-space", "normal"),
	)
	declareGlobal(`.jn-network-stage`,
		gwccss.Position.Relative,
		gwccss.MinHeight(gwccss.Rem(24)),
	)
	declareGlobal(`.jn-network-stale`,
		gwccss.OpacityNum(gwccss.Num(.22)),
		gwccss.Raw("pointer-events", "none"),
		gwccss.Raw("user-select", "none"),
	)
	declareGlobal(`.jn-network-proxy`,
		gwccss.Position.Absolute,
		gwccss.Raw("inset", "0"),
		gwccss.ZIndex(2),
		gwccss.Padding(gwccss.VarLength("jn-s3")),
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(`.jn-proxy-toolbar,.jn-proxy-row`,
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.VarLength("jn-s2")),
	)
	declareGlobal(`.jn-proxy-toolbar`,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.MinHeight(gwccss.Rem(3)),
		gwccss.Raw("padding-bottom", "var(--jn-s2)"),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-proxy-rows`,
		gwccss.Display.Grid,
	)
	declareGlobal(`.jn-proxy-row`,
		gwccss.MinHeight(gwccss.Rem(4.75)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-proxy-block`,
		gwccss.Position.Relative,
		gwccss.Display.Block,
		gwccss.Raw("overflow", "hidden"),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.Bg(gwccss.Var("jn-surface-muted")),
	)
	declareGlobal(`.jn-proxy-heading`,
		gwccss.W(gwccss.MinLen(gwccss.Rem(18), gwccss.Percent(54))),
		gwccss.H(gwccss.Rem(1)),
	)
	declareGlobal(`.jn-proxy-control`,
		gwccss.W(gwccss.Rem(9)),
		gwccss.H(gwccss.Rem(2.5)),
	)
	declareGlobal(`.jn-proxy-avatar`,
		gwccss.Raw("flex", "none"),
		gwccss.W(gwccss.Rem(2.5)),
		gwccss.H(gwccss.Rem(2.5)),
		gwccss.Rounded(gwccss.Percent(50)),
	)
	declareGlobal(`.jn-proxy-copy`,
		gwccss.Display.Grid,
		gwccss.Raw("flex", "1"),
		gwccss.Gap(gwccss.Rem(.625)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-proxy-line`,
		gwccss.W(gwccss.Percent(72)),
		gwccss.H(gwccss.Rem(.75)),
	)
	declareGlobal(`.jn-proxy-line-short`,
		gwccss.W(gwccss.Percent(42)),
	)
	declareGlobal(`.jn-proxy-chip`,
		gwccss.Display.None,
		gwccss.Raw("flex", "none"),
		gwccss.W(gwccss.Rem(5.5)),
		gwccss.H(gwccss.Rem(1.625)),
		gwccss.Rounded(gwccss.VarLength("jn-rpill")),
	)
	declareGlobal(`.jn-proxy-control`,
		gwccss.W(gwccss.Rem(6)),
	)
	declareGlobal(`:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .jn-proxy-block::after,
:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .jn-loading::after`,
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Raw("content", "\"\""), gwccss.Position.Absolute, gwccss.Raw("inset", "0"), gwccss.Raw("background", "linear-gradient(100deg,transparent 18%,rgba(255,255,255,.58) 48%,transparent 78%)"), gwccss.Keyframes("jn-sweep", jnSweepFrames...), gwccss.Animation(gwccss.RawDuration("1.25s"), gwccss.Linear), gwccss.Raw("animation-iteration-count", "infinite"), gwccss.Raw("pointer-events", "none")),
	)
	declareGlobal(`.jn-proxy-chip`,
		mediaRule(gwccss.RawMedia("(min-width:40rem)"), gwccss.Display.Block),
	)
	declareGlobal(`.jn-proxy-control`,
		mediaRule(gwccss.RawMedia("(min-width:40rem)"), gwccss.W(gwccss.Rem(9))),
	)
	declareGlobal(`.jn-page`,
		mediaRule(gwccss.RawMedia("print"), gwccss.Bg(gwccss.Var("jn-surface"))),
	)
	declareGlobal(`.jn-masthead,.jn-skip,.jn-actions,.jn-btn`,
		mediaRule(gwccss.RawMedia("print"), gwccss.Display.None),
	)
	declareGlobal(`.jn-card,.jn-panel`,
		mediaRule(gwccss.RawMedia("print"), gwccss.Raw("box-shadow", "none"), gwccss.Raw("break-inside", "avoid")),
	)
	declareGlobal(`.jn-columns`,
		mediaRule(gwccss.RawMedia("print"), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(`.jn-rail`,
		mediaRule(gwccss.RawMedia("print"), gwccss.Position.Static, gwccss.MaxHeight(gwccss.RawLength("none")), gwccss.Raw("overflow", "visible")),
	)
	declareGlobal(`:where(.jn-page,.jn-embedded),:where(.jn-page,.jn-embedded) *,:where(.jn-page,.jn-embedded) *::before,:where(.jn-page,.jn-embedded) *::after`,
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("animation-duration", ".001ms !important"), gwccss.Raw("animation-iteration-count", "1 !important"), gwccss.TransitionDuration(gwccss.RawDuration(".001ms !important")), gwccss.Raw("scroll-behavior", "auto !important")),
	)
	declareGlobal(`.jn-journey:hover`,
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("transform", "none")),
	)
	declareGlobal(`.jn-checkpill::after`,
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Display.None),
	)
}
