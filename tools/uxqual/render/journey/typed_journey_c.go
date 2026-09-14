package journey

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// declareJourneyMotion emits the checkpill tones, gauges, window strip, work
// items, actions rail, timeline, footer, responsive composition, motion, and
// print rules, in original order.
func declareJourneyMotion() {
	declareGlobal(`.jn-wait-explanation`,
		gwccss.BorderLeft(gwccss.Px(3), gwccss.Var("jn-warning")),
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
		gwccss.FontSize(gwccss.Rem(.875)),
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
		gwccss.FontSize(gwccss.Rem(.6875)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
		gwccss.Raw("margin-top", ".125rem"),
	)
	declareGlobal(`.jn-gauge-legend`,
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.Fr(1)),
		gwccss.RowGap(gwccss.Rem(.125)), gwccss.ColumnGap(gwccss.Rem(.625)),
		gwccss.Raw("margin-top", ".625rem"),
		gwccss.FontSize(gwccss.Rem(.8125)),
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
		gwccss.Rounded(gwccss.Px(2)),
		gwccss.Bg(gwccss.Var("jn-neutral")),
	)
	declareGlobal(`.jn-gauge-legend dt[data-which="proposed"]::before`,
		gwccss.Bg(gwccss.Var("jn-accent-strong")),
	)
	declareGlobal(`.jn-gauge-legend dd`,
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("text-align", "right"),
		gwccss.Raw("font-weight", "620"),
	)
	declareGlobal(`.jn-gauge-note`,
		gwccss.Raw("margin-top", ".625rem"),
		gwccss.FontSize(gwccss.Rem(.75)),
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
		gwccss.Raw("padding-left", "1.125rem"),
	)
	declareGlobal(`.jn-stop`,
		mediaRule(gwccss.RawMedia("(min-width:44rem)"), gwccss.Raw("padding-left", "0"), gwccss.Raw("padding-top", "1.125rem")),
	)
	declareGlobal(`.jn-stop-dot`,
		gwccss.Position.Absolute,
		gwccss.Left(gwccss.Zero),
		gwccss.Top(gwccss.Rem(.375)),
		gwccss.W(gwccss.Rem(.625)),
		gwccss.H(gwccss.Rem(.625)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Border(gwccss.Px(2), gwccss.Var("jn-control-border")),
		gwccss.Bg(gwccss.Var("jn-surface")),
	)
	declareGlobal(`.jn-stop-dot`,
		mediaRule(gwccss.RawMedia("(min-width:44rem)"), gwccss.Left(gwccss.Zero), gwccss.Top(gwccss.Zero)),
	)
	declareGlobal(`.jn-stop::before`,
		mediaRule(gwccss.RawMedia("(min-width:44rem)"), gwccss.Raw("content", "\"\""), gwccss.Position.Absolute, gwccss.Left(gwccss.Rem(.625)), gwccss.Right(gwccss.Zero), gwccss.Top(gwccss.Rem(.25)), gwccss.H(gwccss.Px(2)), gwccss.Bg(gwccss.Var("jn-hairline"))),
	)
	declareGlobal(`.jn-stop:last-child::before`,
		mediaRule(gwccss.RawMedia("(min-width:44rem)"), gwccss.Display.None),
	)
	declareGlobal(`.jn-stop[data-which="effective"] .jn-stop-dot`,
		gwccss.BorderColor(gwccss.Var("jn-accent")),
		gwccss.Bg(gwccss.Var("jn-accent")),
	)
	declareGlobal(`.jn-stop-label`,
		gwccss.FontSize(gwccss.Rem(.6875)),
		gwccss.Raw("font-weight", "660"),
		gwccss.Tracking(gwccss.Ems(.05)),
		gwccss.Raw("text-transform", "uppercase"),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-stop-value`,
		gwccss.FontSize(gwccss.Rem(.875)),
		gwccss.Raw("font-weight", "620"),
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
		gwccss.FontSize(gwccss.Rem(.875)),
		gwccss.Raw("font-weight", "640"),
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
		gwccss.FontSize(gwccss.Rem(.8125)),
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
		gwccss.FontSize(gwccss.Rem(.75)),
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
		gwccss.FontSize(gwccss.Rem(.8125)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-actsas`,
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.375)),
		gwccss.FontSize(gwccss.Rem(.75)),
		gwccss.TextColor(gwccss.Var("jn-info")),
		gwccss.Bg(gwccss.Var("jn-info-soft")),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.PaddingY(gwccss.Rem(.3125)), gwccss.PaddingX(gwccss.Rem(.5)),
	)
	declareGlobal(`.jn-blocked`,
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Rem(.375)),
		gwccss.FontSize(gwccss.Rem(.75)),
		gwccss.TextColor(gwccss.Var("jn-warning")),
		gwccss.Bg(gwccss.Var("jn-warning-soft")),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.PaddingY(gwccss.Rem(.3125)), gwccss.PaddingX(gwccss.Rem(.5)),
	)
	declareGlobal(`.jn-confirm-trigger`,
		gwccss.W(gwccss.Percent(100)),
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
		gwccss.Raw("font-weight", "680"),
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
		gwccss.FontSize(gwccss.Rem(1.375)),
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
	declareGlobal(`.jn-confirm-facts .jn-fact dd`,
		gwccss.FontSize(gwccss.Rem(.8125)),
	)
	declareGlobal(`.jn-confirm-note`,
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Rem(.375)),
		gwccss.Padding(gwccss.Rem(.5)),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.Bg(gwccss.Var("jn-warning-soft")),
		gwccss.TextColor(gwccss.Var("jn-warning")),
		gwccss.FontSize(gwccss.Rem(.75)),
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
	declareGlobal(`.jn-confirm-surface`,
		gwccss.Position.Fixed,
		gwccss.Top(gwccss.Percent(50)),
		gwccss.Left(gwccss.Percent(50)),
		gwccss.Raw("transform", "translate(-50%,-50%)"),
		gwccss.Raw("width", "min(34rem,calc(100vw - 2rem))"),
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
	declareGlobal(`.jn-confirm-backdrop`,
		gwccss.Position.Fixed,
		gwccss.Raw("inset", "0"),
		gwccss.Raw("background", "color-mix(in srgb,var(--jn-ink) 45%,transparent)"),
	)
	declareGlobal(`.jn-confirm-actionbar`,
		gwccss.Position.Sticky,
		gwccss.Bottom(gwccss.Zero),
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Rem(.625)),
		gwccss.Raw("justify-content", "flex-end"),
		gwccss.PaddingY(gwccss.Rem(.625)),
		gwccss.Raw("margin-top", ".125rem"),
		gwccss.Bg(gwccss.Var("jn-surface")),
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
		gwccss.FontSize(gwccss.Rem(.75)),
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
		gwccss.FontSize(gwccss.Rem(.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.jn-tltitle`,
		gwccss.FontSize(gwccss.Rem(.875)),
		gwccss.Raw("font-weight", "620"),
	)
	declareGlobal(`.jn-tldetail`,
		gwccss.FontSize(gwccss.Rem(.8125)),
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
		gwccss.FontSize(gwccss.Rem(.75)),
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
		gwccss.Rounded(gwccss.Px(999)),
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
