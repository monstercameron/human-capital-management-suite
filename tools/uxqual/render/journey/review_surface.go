package journey

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PROMOUX-010: every consequential promotion action -- Start's own proposal
// submission today, Approve and Reject already, Withdraw and Cancel once
// PROMOUX-013 adds them -- confirms through this one surface rather than
// five bespoke disclosures. The RED this fixes was reproduced live: opening
// a review pushed the final action below the viewport, reflowed the People
// and Journeys sections around it, dropped focus to <body>, and offered no
// way back except a details toggle nobody would find, because the old
// <details class="jn-confirm"> block was a plain in-flow reveal with no
// focus, escape or busy handling at all.
//
// The <details>/<summary> disclosure itself is kept, unchanged in class
// names and copy, because it is the zero-JavaScript baseline every reader
// gets before any client mounts: a real click target that shows the review
// with no script required. What changes is what happens once a live client
// is running: a native "toggle" listener (review_surface_sync_wasm.go)
// mirrors the disclosure's open/closed state into GWC, and that state
// drives a github.com/monstercameron/GoWebComponents/v5/ui.Overlay in
// Modal mode -- the library's own tested focus-trap, restore-focus,
// escape-to-dismiss and outside-click-to-dismiss primitive, not a
// hand-rolled one -- so the compact review renders as a contained,
// viewport-anchored panel instead of an in-flow block that shoves
// everything below it down the page.
//
// reviewSurfaceProps is the typed content contract: every field is data the
// caller supplies, and nothing in this file knows which action it guards.
type reviewSurfaceProps struct {
	// ID namespaces this instance's disclosure, trigger and overlay ids so
	// more than one review surface can exist on the same page (one per
	// action) without colliding.
	ID string
	// TriggerLabel is the disclosure's own open-state summary text, e.g.
	// "Review and approve" or "Review and propose".
	TriggerLabel string
	// TriggerVariant is the trigger's jn-btn tone (primary/secondary/
	// danger); it defaults to secondary so the trigger never outranks the
	// page's actual primary action.
	TriggerVariant string
	// Heading is the review's own title once open, e.g. "Confirm approve"
	// or "Confirm proposal".
	Heading string
	// Facts is the compact employee/change/date/consequence summary the
	// reader confirms against before the final action fires.
	Facts []Fact
	// Note is an optional consequence warning, e.g. irreversibility.
	Note string
	// CancelLabel defaults to "Cancel". It is a real, keyboard-reachable
	// control distinct from the disclosure's own "Cancel review" toggle
	// text, deliberately styled no more prominently than Submit.
	CancelLabel string
	// Submit is the fully-built final-action button (its own label,
	// variant, disabled state and click handler already wired by the
	// caller); this surface only decides where it sits and what surrounds
	// it.
	Submit ui.Node
	// Busy is true while the submission this surface guards is in flight.
	// The action bar stays mounted and sized -- only the disabled/aria-busy
	// attributes and the status line change -- so geometry never collapses
	// mid-request, and a second click cannot fire a duplicate submission.
	Busy bool
	// BusyLabel defaults to "Submitting…" and is announced through a
	// role="status" region once Busy is true.
	BusyLabel string
}

// reviewSurface is the one confirmation component PROMOUX-010's REFACTOR
// asks for. Call it through ui.CreateElement so each instance gets its own
// hook slot (UseState for the mirrored open flag, the nested Overlay's own
// hooks) independent of every other action's review on the same page.
// reviewSurfaceDetailsID and reviewSurfaceTriggerID are exported derivations
// (not just inlined in reviewSurface below) so a caller elsewhere in this
// package that needs to address a specific review's trigger from outside
// reviewSurface itself -- proposalView's mount-time focus effect is the one
// that exists today -- computes the same id reviewSurface actually renders,
// rather than re-deriving the "-review"/"-trigger" suffixes by hand and
// risking the two falling out of sync.
func reviewSurfaceDetailsID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "review"
	}
	return id + "-review"
}

func reviewSurfaceTriggerID(id string) string {
	return reviewSurfaceDetailsID(id) + "-trigger"
}

func reviewSurface(props reviewSurfaceProps) ui.Node {
	detailsID := reviewSurfaceDetailsID(props.ID)
	triggerID := reviewSurfaceTriggerID(props.ID)
	headingID := detailsID + "-heading"
	surfaceID := detailsID + "-surface"

	triggerVariant := props.TriggerVariant
	switch triggerVariant {
	case "primary", "danger":
	default:
		triggerVariant = "secondary"
	}
	cancelLabel := nonEmpty(props.CancelLabel, "Cancel")
	busyLabel := ""
	if props.Busy {
		busyLabel = nonEmpty(props.BusyLabel, "Submitting…")
	}

	// open mirrors the <details> element's own native open/closed state
	// (see review_surface_sync_wasm.go): the disclosure remains the single
	// source of truth, so a reader who never runs JavaScript still gets a
	// working, if plainly-laid-out, review, and a live client's Escape/
	// outside-click/focus handling always agrees with what is actually
	// visible.
	open := ui.UseState(false)
	useReviewDetailsSync(detailsID, func(nowOpen bool) { open.Set(nowOpen) })

	dismiss := func() {
		open.Set(false)
		closeReviewDetails(detailsID)
	}

	cancelClick := ui.UseEvent(func(e ui.MouseEvent) {
		e.PreventDefault()
		dismiss()
	})

	bodyChildren := []ui.Node{
		html.P(html.Props{ID: headingID, Class: "jn-confirm-title"}, html.Text(props.Heading)),
	}
	if len(props.Facts) > 0 {
		bodyChildren = append(bodyChildren, factsListWithClass(props.Facts, "jn-confirm-facts"))
	}
	if props.Note != "" {
		bodyChildren = append(bodyChildren,
			html.P(html.Props{Class: "jn-confirm-note"}, iconWarning("jn-confirm-icon"), html.Text(props.Note)))
	}
	cancelBtn := html.Button(html.Props{
		Type: "button", Class: "jn-btn jn-confirm-cancel",
		DataAttr: html.DataAttribute{Name: "variant", Value: "secondary"},
		OnClick:  cancelClick,
	}, html.Text(cancelLabel))
	bodyChildren = append(bodyChildren,
		html.Div(html.Props{Class: "jn-confirm-actionbar"}, cancelBtn, props.Submit),
		html.P(html.Props{Class: "jn-confirm-status", Role: "status", Aria: map[string]string{"live": "polite"}}, html.Text(busyLabel)),
	)

	overlay := ui.CreateElement(ui.Overlay, ui.OverlayProps{
		Open:                open.Get(),
		Modal:               true,
		CloseOnOutsideClick: true,
		SurfaceID:           surfaceID,
		LabelledBy:          headingID,
		OnDismiss:           dismiss,
		SurfaceClass:        "jn-confirm-surface",
		BackdropClass:       "jn-confirm-backdrop",
		Child:               html.Div(html.Props{Class: "jn-confirm-body"}, bodyChildren...),
	})

	return html.Details(html.Props{Class: "jn-confirm", ID: detailsID, Key: detailsID},
		html.Summary(html.Props{Class: "jn-btn", ID: triggerID,
			DataAttr: html.DataAttribute{Name: "variant", Value: triggerVariant},
			Raw:      map[string]any{"role": "button"}},
			html.Span(html.Props{Class: "jn-confirm-open-label"}, html.Text(props.TriggerLabel)),
			html.Span(html.Props{Class: "jn-confirm-close-label"}, html.Text("Cancel review"))),
		overlay,
	)
}
