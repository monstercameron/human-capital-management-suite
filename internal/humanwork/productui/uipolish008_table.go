package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// uipolish008TableStylesheet applies the persisted root density to the shared
// matrix. The table owns its responsive card treatment; this layer only
// adjusts spacing and keeps sortable controls touch-sized in every mode.
func uipolish008TableStylesheet() string {
	return buildTypedSheet(func() {
		declareGlobal(".data-table-cell",
			gwccss.PaddingY(gwccss.RawLength("calc(3px + var(--hcm-space-1) * var(--hcm-density))")),
		)
		declareGlobal(".data-table .data-table-row",
			mediaRule(gwccss.MaxW(1050),
				gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-1) * var(--hcm-density))")),
				gwccss.Padding(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")),
			),
		)
		declareGlobal(".data-table-sort",
			mediaRule(gwccss.MaxW(1050), gwccss.MinHeight(gwccss.Px(44))),
		)
		// At phone widths the card label and value must share a usable line;
		// the previous 18px gap made long localized labels force tall, narrow
		// cards. This only tightens spacing and permits wrapping, never hides a
		// column or changes its DOM order.
		declareGlobal(".data-table .data-table-cell",
			mediaRule(gwccss.MaxW(420),
				gwccss.Gap(gwccss.Px(8)),
				gwccss.Padding(gwccss.Px(2)),
				gwccss.Raw("overflow-wrap", "anywhere"),
			),
		)
		// Sort controls wrap at the smallest viewport instead of requiring a
		// hidden horizontal gesture before the user can discover the next field.
		// The table body remains a one-column semantic list at this breakpoint.
		declareGlobal(".data-table-head",
			mediaRule(gwccss.MaxW(420), gwccss.Raw("flex-wrap", "wrap"), gwccss.Raw("overflow-x", "visible")),
		)
		declareGlobal(".workflow-history.history-density-compact .history-row",
			gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(16)), gwccss.Gap(gwccss.Px(12)),
		)
		declareGlobal(".workflow-history.history-density-compact .history-row",
			mediaRule(gwccss.MaxW(420), gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(12)), gwccss.Gap(gwccss.Px(8))),
		)
	})
}
