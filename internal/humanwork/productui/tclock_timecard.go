package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PageTimecard identifies the worker's own timecard.
const PageTimecard PageID = "time-timecard"

// TimecardPunch is one interval or event on a day, already formatted.
type TimecardPunch struct {
	Label  string
	Detail string
	Break  bool
}

// TimecardException is a problem on a day with the way to put it right.
type TimecardException struct {
	Label string
	Fix   *ActionLinkProps
}

// TimecardDay is one calendar day of the period.
type TimecardDay struct {
	ID         string
	DateLabel  string
	TotalLabel string
	Today      bool
	Punches    []TimecardPunch
	Exceptions []TimecardException
}

// TimecardProjection is the server-owned view of one worker's period. Totals,
// labels and exceptions are computed by the time service; the page only lays
// them out.
type TimecardProjection struct {
	State       TimeSurfaceState
	PeriodLabel string
	StatusLabel string
	StatusTone  string
	Previous    *ActionLinkProps
	Next        *ActionLinkProps
	Totals      []TimeFact
	Days        []TimecardDay
	// Submit is offered only when the period can be submitted. SubmitNote
	// explains why it is absent (for example open exceptions).
	Submit     *ActionLinkProps
	SubmitNote string
	Notice     string
	Error      string
}

// TimecardPage renders the worker's own period: where they stand, what is
// wrong, and the one next step.
func TimecardPage(view View, projection TimecardProjection) ui.Node {
	locale := view.Locale
	title := timeText(locale, "timecard.title")
	if projection.State != TimeSurfaceReady {
		return timeUnavailable(view, PageTimecard, "timecard-title", title, timeText(locale, "timecard.intro"), timeText(locale, "timecard.unavailable"), timeText(locale, "timecard.unavailable_help"))
	}
	problems := 0
	firstProblem := -1
	for index, day := range projection.Days {
		if len(day.Exceptions) > 0 && firstProblem < 0 {
			firstProblem = index
		}
		problems += len(day.Exceptions)
	}
	children := timeNodes(
		timeHead(PageTimecard, "timecard-title", title, timeText(locale, "timecard.intro")),
		timeNotice("error", projection.Error),
		timeNotice("success", projection.Notice),
		timecardBar(view, projection),
	)
	if problems > 0 {
		notice := []ui.Node{ui.Text(timeCountText(locale, timeCountKey("timecard.problems", problems), problems))}
		if firstProblem >= 0 {
			day := projection.Days[firstProblem]
			notice = append(notice, html.A(html.Props{Class: "timecard-goto", Href: "#timecard-day-" + safeID(day.ID)}, ui.Text(strings.ReplaceAll(timeText(locale, "timecard.go_to"), "{date}", day.DateLabel))))
		}
		children = append(children, html.P(html.Props{Class: "clock-notice clock-notice-error", Role: "alert"}, notice...))
	}
	if facts := timeFacts("timecard-totals", projection.Totals); facts != nil {
		children = append(children, html.Section(html.Props{Class: "surface timecard-summary", Raw: map[string]any{"aria-label": timeText(locale, "timecard.totals")}}, facts))
	}
	days := make([]ui.Node, 0, len(projection.Days))
	for _, day := range projection.Days {
		days = append(days, html.WithKey(timecardDay(view, day), "day-"+safeID(day.ID)))
	}
	if len(days) == 0 {
		children = append(children, html.P(html.Props{Class: "surface muted timecard-empty"}, ui.Text(timeText(locale, "timecard.empty"))))
	} else {
		children = append(children, html.Ol(html.Props{Class: "timecard-days"}, days...))
	}
	if submit := timecardSubmit(view, projection); submit != nil {
		children = append(children, submit)
	}
	return html.Section(html.Props{Class: "time-page timecard-page", Raw: map[string]any{"aria-labelledby": "timecard-title"}}, children...)
}

func timecardBar(view View, projection TimecardProjection) ui.Node {
	locale := view.Locale
	period := html.Div(html.Props{Class: "timecard-period"},
		html.H2(html.Props{Class: "timecard-period-label"}, ui.Text(projection.PeriodLabel)),
		timePill(projection.StatusTone, projection.StatusLabel),
	)
	steps := timeNodes(
		timeLink(view, projection.Previous, "button secondary timecard-step"),
		timeLink(view, projection.Next, "button secondary timecard-step"),
	)
	return html.Nav(html.Props{Class: "timecard-bar", Raw: map[string]any{"aria-label": timeText(locale, "timecard.period_nav")}}, append([]ui.Node{period}, steps...)...)
}

func timecardDay(view View, day TimecardDay) ui.Node {
	locale := view.Locale
	state := "ok"
	if len(day.Exceptions) > 0 {
		state = "problem"
	} else if len(day.Punches) == 0 {
		state = "empty"
	}
	title := []ui.Node{html.H3(html.Props{Class: "timecard-date"}, ui.Text(day.DateLabel))}
	if day.Today {
		title = append(title, timePill(TimeToneInfo, timeText(locale, "timecard.today")))
	}
	head := html.Div(html.Props{Class: "timecard-day-head"},
		html.Div(html.Props{Class: "timecard-day-title"}, title...),
		html.P(html.Props{Class: "timecard-total"}, html.Tag("bdi", html.Props{}, ui.Text(day.TotalLabel))),
	)
	body := []ui.Node{head}
	if len(day.Punches) == 0 && len(day.Exceptions) == 0 {
		body = append(body, html.P(html.Props{Class: "muted timecard-none"}, ui.Text(timeText(locale, "timecard.no_punches"))))
	}
	if len(day.Punches) > 0 {
		punches := make([]ui.Node, 0, len(day.Punches))
		for _, punch := range day.Punches {
			class := "timecard-punch"
			if punch.Break {
				class += " timecard-punch-break"
			}
			punches = append(punches, html.Li(html.Props{Class: class},
				html.Span(html.Props{Class: "timecard-punch-time"}, html.Tag("bdi", html.Props{}, ui.Text(punch.Label))),
				html.Span(html.Props{Class: "timecard-punch-detail"}, ui.Text(punch.Detail)),
			))
		}
		body = append(body, html.Ul(html.Props{Class: "timecard-punches"}, punches...))
	}
	for _, problem := range day.Exceptions {
		body = append(body, html.Div(html.Props{Class: "timecard-problem"}, timeNodes(
			timePill(TimeToneBad, problem.Label),
			timeLink(view, problem.Fix, "button secondary timecard-fix"),
		)...))
	}
	class := "timecard-day"
	if day.Today {
		class += " timecard-day-today"
	}
	return html.Li(html.Props{ID: "timecard-day-" + safeID(day.ID), Class: class, Data: map[string]string{"state": state}}, body...)
}

func timecardSubmit(view View, projection TimecardProjection) ui.Node {
	if submit := timeLink(view, projection.Submit, "button primary timecard-submit"); submit != nil {
		return html.Div(html.Props{Class: "timecard-submit-bar"}, submit)
	}
	if strings.TrimSpace(projection.SubmitNote) != "" {
		// The action stays visible but off, with the reason beside it, so the
		// worker sees what the next step is and what is in the way.
		return html.Div(html.Props{Class: "timecard-submit-bar timecard-submit-blocked"},
			html.Button(html.Props{Class: "button primary timecard-submit", Type: "button", Disabled: true, Aria: map[string]string{"describedby": "timecard-submit-note"}}, ui.Text(timeText(view.Locale, "timecard.submit"))),
			html.P(html.Props{ID: "timecard-submit-note", Class: "muted timecard-submit-note"}, ui.Text(projection.SubmitNote)),
		)
	}
	return nil
}
