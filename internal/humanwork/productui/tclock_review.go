package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TimecardApprover records a supervisor's approval of one or more timecards
// through the authenticated transport. Implementations authorize every id.
type TimecardApprover interface {
	ApproveTimecards(ids []string) error
}

// TimeReviewRow is one worker's timecard for the period under review.
type TimeReviewRow struct {
	ID            string
	Worker        string
	Role          string
	HoursLabel    string
	OvertimeLabel string
	// Problems name what blocks approval. A row with problems is never
	// selectable; the supervisor opens it and fixes the cause first.
	Problems []string
	Open     *ActionLinkProps
}

// TimeReviewProjection is the supervisor's queue for one period, already
// split by the service into rows that need attention and rows that are ready.
type TimeReviewProjection struct {
	State       TimeSurfaceState
	PeriodLabel string
	Needs       []TimeReviewRow
	Ready       []TimeReviewRow
	Approver    TimecardApprover
	Notice      string
	Error       string
}

// TimeReviewPage puts the timecards that need a decision first and offers a
// bulk approval only for the ones that are ready.
func TimeReviewPage(view View, projection TimeReviewProjection) ui.Node {
	return timeReviewPage(view, projection, true)
}

// timeReviewPage renders the queue. The registered route sits under the
// shell's own page heading, so it passes withHead=false and gets the period
// as a plain line instead of a second h1.
func timeReviewPage(view View, projection TimeReviewProjection, withHead bool) ui.Node {
	locale := view.Locale
	title := timeText(locale, "review.title")
	if projection.State != TimeSurfaceReady {
		return timeUnavailable(view, PageTimeApproval, "review-title", title, timeText(locale, "review.intro"), timeText(locale, "review.unavailable"), timeText(locale, "review.unavailable_help"))
	}
	subtitle := strings.ReplaceAll(timeCountText(locale, "review.period", len(projection.Needs)+len(projection.Ready)), "{period}", projection.PeriodLabel)
	var head ui.Node = html.P(html.Props{Class: "time-period-line muted"}, ui.Text(subtitle))
	if withHead {
		head = timeHead(PageTimeApproval, "review-title", title, subtitle)
	}
	children := timeNodes(
		head,
		timeNotice("error", projection.Error),
		timeNotice("success", projection.Notice),
	)
	if len(projection.Needs)+len(projection.Ready) == 0 {
		children = append(children, html.Div(html.Props{Class: "surface time-empty"},
			html.H2(html.Props{}, ui.Text(timeText(locale, "review.empty"))),
			html.P(html.Props{Class: "muted"}, ui.Text(timeText(locale, "review.empty_help"))),
		))
		return html.Section(html.Props{Class: "time-page review-page", Raw: map[string]any{"aria-labelledby": "review-title"}}, children...)
	}
	if len(projection.Needs) > 0 {
		children = append(children, timeReviewNeeds(view, projection))
	}
	if len(projection.Ready) > 0 || projection.Approver != nil {
		children = append(children, ui.CreateElement(timeReviewReady, timeReviewReadyProps{View: view, Projection: projection}))
	}
	return html.Section(html.Props{Class: "time-page review-page", Raw: map[string]any{"aria-labelledby": "review-title"}}, children...)
}

func timeReviewNeeds(view View, projection TimeReviewProjection) ui.Node {
	locale := view.Locale
	rows := make([]ui.Node, 0, len(projection.Needs))
	for _, row := range projection.Needs {
		problems := make([]ui.Node, 0, len(row.Problems))
		for _, problem := range row.Problems {
			problems = append(problems, timePill(TimeToneBad, problem))
		}
		rows = append(rows, html.WithKey(html.Li(html.Props{Class: "review-row review-row-needs"},
			html.Div(html.Props{Class: "review-who"},
				html.Strong(html.Props{}, ui.Text(row.Worker)),
				html.Span(html.Props{Class: "muted"}, ui.Text(row.Role)),
			),
			html.Div(html.Props{Class: "review-problems"}, problems...),
			html.Div(html.Props{Class: "review-hours"}, html.Tag("bdi", html.Props{}, ui.Text(row.HoursLabel))),
			html.Div(html.Props{Class: "review-action"}, timeNodes(timeLinkFor(view, row.Open, "button secondary", row.Worker))...),
		), "needs-"+safeID(row.ID)))
	}
	return html.Section(html.Props{Class: "surface review-group review-group-needs", Raw: map[string]any{"aria-labelledby": "review-needs-title"}},
		html.Div(html.Props{Class: "review-group-head"},
			html.H2(html.Props{ID: "review-needs-title"}, ui.Text(timeCountText(locale, timeCountKey("review.needs", len(projection.Needs)), len(projection.Needs)))),
			html.P(html.Props{Class: "muted"}, ui.Text(timeText(locale, "review.needs_help"))),
		),
		html.Ul(html.Props{Class: "review-rows"}, rows...),
	)
}

type timeReviewReadyProps struct {
	View       View
	Projection TimeReviewProjection
}

type timeReviewDraft struct {
	selected   map[string]bool
	confirming bool
	busy       bool
	done       map[string]bool
	notice     string
	err        string
}

// timeReviewReady owns the selection, the confirmation and the outcome so the
// supervisor always sees what will be approved before it happens.
func timeReviewReady(props timeReviewReadyProps) ui.Node {
	view, projection := props.View, props.Projection
	locale := view.Locale
	state := ui.UseState(timeReviewDraft{selected: map[string]bool{}, done: map[string]bool{}})
	draft := state.Get()
	edit := func(update func(*timeReviewDraft)) {
		next := state.Get()
		next.selected = timeReviewCopySet(next.selected)
		next.done = timeReviewCopySet(next.done)
		update(&next)
		state.Set(next)
	}
	visible := make([]TimeReviewRow, 0, len(projection.Ready))
	for _, row := range projection.Ready {
		if !draft.done[row.ID] {
			visible = append(visible, row)
		}
	}
	selectedIDs := make([]string, 0, len(visible))
	for _, row := range visible {
		if draft.selected[row.ID] {
			selectedIDs = append(selectedIDs, row.ID)
		}
	}
	allSelected := len(visible) > 0 && len(selectedIDs) == len(visible)

	toggleAll := html.Props{ID: "review-select-all", Type: "checkbox", Checked: allSelected, Disabled: len(visible) == 0 || draft.busy || projection.Approver == nil,
		OnChange: ui.UseEvent(func(ui.InputEvent) {
			edit(func(d *timeReviewDraft) {
				for _, row := range visible {
					if allSelected {
						delete(d.selected, row.ID)
					} else {
						d.selected[row.ID] = true
					}
				}
				d.confirming = false
			})
		})}
	rows := make([]ui.Node, 0, len(visible))
	for _, row := range visible {
		rowID := row.ID
		box := html.Props{ID: "review-row-" + safeID(rowID), Type: "checkbox", Checked: draft.selected[rowID], Disabled: draft.busy || projection.Approver == nil,
			OnChange: ui.UseEvent(func(ui.InputEvent) {
				edit(func(d *timeReviewDraft) {
					if d.selected[rowID] {
						delete(d.selected, rowID)
					} else {
						d.selected[rowID] = true
					}
					d.confirming = false
				})
			})}
		hours := row.HoursLabel
		if strings.TrimSpace(row.OvertimeLabel) != "" {
			hours += " · " + row.OvertimeLabel
		}
		rows = append(rows, html.WithKey(html.Li(html.Props{Class: "review-row review-row-ready"},
			html.Label(html.Props{Class: "review-pick", For: box.ID},
				html.Input(box),
				html.Span(html.Props{Class: "review-who"},
					html.Strong(html.Props{}, ui.Text(row.Worker)),
					html.Span(html.Props{Class: "muted"}, ui.Text(row.Role)),
				),
			),
			html.Div(html.Props{Class: "review-hours"}, html.Tag("bdi", html.Props{}, ui.Text(hours))),
			html.Div(html.Props{Class: "review-action"}, timeNodes(timeLinkFor(view, row.Open, "button secondary", row.Worker))...),
		), "ready-"+safeID(rowID)))
	}

	children := []ui.Node{
		html.Div(html.Props{Class: "review-group-head"},
			html.H2(html.Props{ID: "review-ready-title"}, ui.Text(timeCountText(locale, timeCountKey("review.ready", len(visible)), len(visible)))),
			html.P(html.Props{Class: "muted"}, ui.Text(timeText(locale, "review.ready_help"))),
		),
	}
	if draft.notice != "" {
		children = append(children, timeNotice("success", draft.notice))
	}
	if draft.err != "" {
		children = append(children, timeNotice("error", draft.err))
	}
	if len(visible) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(timeText(locale, "review.none_ready"))))
	} else {
		children = append(children,
			html.Label(html.Props{Class: "review-all", For: "review-select-all"}, html.Input(toggleAll), ui.Text(timeCountText(locale, timeCountKey("review.select_all", len(visible)), len(visible)))),
			html.Ul(html.Props{Class: "review-rows"}, rows...),
		)
	}
	if projection.Approver != nil && len(visible) > 0 {
		children = append(children, timeReviewBar(view, projection, draft, selectedIDs, edit))
	}
	return html.Section(html.Props{Class: "surface review-group review-group-ready", Raw: map[string]any{"aria-labelledby": "review-ready-title"}}, children...)
}

func timeReviewBar(view View, projection TimeReviewProjection, draft timeReviewDraft, ids []string, edit func(func(*timeReviewDraft))) ui.Node {
	locale := view.Locale
	approveLabel := timeCountText(locale, timeCountKey("review.approve", len(ids)), len(ids))
	if len(ids) == 0 {
		return html.Div(html.Props{Class: "review-bar"},
			html.P(html.Props{Class: "muted review-bar-hint"}, ui.Text(timeText(locale, "review.pick_hint"))),
			html.Button(html.Props{Class: "button primary review-approve", Type: "button", Disabled: true}, ui.Text(timeText(locale, "review.approve_none"))),
		)
	}
	if draft.confirming {
		yesLabel := timeCountText(locale, timeCountKey("review.confirm_yes", len(ids)), len(ids))
		if draft.busy {
			yesLabel = timeText(locale, "review.approving")
		}
		yes := html.Button(html.Props{Class: "button primary review-confirm-yes", Type: "button", Disabled: draft.busy, OnClick: ui.UseEvent(func(ui.MouseEvent) {
			if draft.busy {
				return
			}
			edit(func(d *timeReviewDraft) { d.busy, d.err = true, "" })
			err := projection.Approver.ApproveTimecards(ids)
			edit(func(d *timeReviewDraft) {
				d.busy = false
				if err != nil {
					d.err = timeText(locale, "review.error")
					return
				}
				for _, id := range ids {
					d.done[id] = true
					delete(d.selected, id)
				}
				d.confirming = false
				d.notice = timeCountText(locale, timeCountKey("review.approved", len(ids)), len(ids))
			})
		})}, ui.Text(yesLabel))
		no := html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: draft.busy, OnClick: ui.UseEvent(func(ui.MouseEvent) {
			edit(func(d *timeReviewDraft) { d.confirming = false })
		})}, ui.Text(timeText(locale, "review.confirm_no")))
		return html.Div(html.Props{Class: "review-bar review-bar-confirm"},
			timeConfirmPanel("review-confirm-title", timeCountText(locale, timeCountKey("review.confirm_title", len(ids)), len(ids)), strings.ReplaceAll(timeText(locale, "review.confirm_body"), "{period}", projection.PeriodLabel), false, yes, no))
	}
	return html.Div(html.Props{Class: "review-bar"},
		html.P(html.Props{Class: "review-bar-count", Role: "status"}, ui.Text(timeCountText(locale, timeCountKey("review.selected", len(ids)), len(ids)))),
		html.Button(html.Props{Class: "button primary review-approve", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) {
			edit(func(d *timeReviewDraft) { d.confirming, d.notice = true, "" })
		})}, ui.Text(approveLabel)),
	)
}

func timeReviewCopySet(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
