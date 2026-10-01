package productui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PageClock identifies the governed employee clock page.
const PageClock PageID = "time-clock"

// ClockProjectionState describes whether the clock service supplied a usable
// projection. The zero value is unavailable so an omitted projection cannot
// accidentally render an actionable clock.
type ClockProjectionState uint8

const (
	// ClockProjectionUnavailable means the clock service did not publish a
	// projection for this request.
	ClockProjectionUnavailable ClockProjectionState = iota
	// ClockProjectionReady means the projection is authorized and current.
	ClockProjectionReady
)

// ClockPhase is the worker's position in the current work session. The zero
// value asks the renderer to infer the phase from which actions the server
// offered, so a projection that predates this field still renders correctly.
type ClockPhase uint8

const (
	// ClockPhaseUnknown lets the renderer infer the phase from the actions.
	ClockPhaseUnknown ClockPhase = iota
	// ClockPhaseOut means the worker has no open session.
	ClockPhaseOut
	// ClockPhaseIn means the worker is on the clock.
	ClockPhaseIn
	// ClockPhaseBreak means the worker is on a recorded break.
	ClockPhaseBreak
)

// ClockProjection is the server-owned presentation contract for the clock
// page. Labels and destinations are already scoped and localized by the
// caller; the renderer never computes status, time, or authority.
type ClockProjection struct {
	State             ClockProjectionState
	WorkerLabel       string
	ScheduleLabel     string
	StatusLabel       string
	LastEventLabel    string
	ReceiptTraceLabel string
	Busy              bool
	ErrorLabel        string
	ClockIn           *ActionLinkProps
	ClockOut          *ActionLinkProps
	EnrollKiosk       *ActionLinkProps
	LaunchKiosk       *ActionLinkProps

	// Reason says why an unavailable projection is unavailable, when the clock
	// service made a decision about this worker (UXBLIND-123). Empty means the
	// read failed for a cause the worker can retry.
	Reason ClockReason
	// Phase is optional; the zero value is inferred from the offered actions.
	Phase ClockPhase
	// SinceLabel is an optional localized "since 7:02 AM" style line.
	SinceLabel string
	// StartBreak and EndBreak are offered only while the server allows them.
	StartBreak *ActionLinkProps
	EndBreak   *ActionLinkProps
	// Timecard and FixPunch are optional destinations shown beside the clock.
	Timecard *ActionLinkProps
	FixPunch *ActionLinkProps
}

// clockPhase resolves the phase the page presents. An explicit phase wins;
// otherwise the offered actions decide, and no action means unknown.
func (projection ClockProjection) clockPhase() ClockPhase {
	if projection.Phase != ClockPhaseUnknown {
		return projection.Phase
	}
	switch {
	case usableClockAction(projection.EndBreak):
		return ClockPhaseBreak
	case usableClockAction(projection.ClockOut) || usableClockAction(projection.StartBreak):
		if usableClockAction(projection.ClockIn) {
			return ClockPhaseUnknown
		}
		return ClockPhaseIn
	case usableClockAction(projection.ClockIn):
		return ClockPhaseOut
	}
	return ClockPhaseUnknown
}

// usableClockAction reports whether the server offered a destination and a
// label; an action missing either is treated as not offered.
func usableClockAction(action *ActionLinkProps) bool {
	return action != nil && action.Href != "" && action.Label != "" && validClockActionHref(action.Href)
}

func clockPhaseName(phase ClockPhase) string {
	switch phase {
	case ClockPhaseOut:
		return "out"
	case ClockPhaseIn:
		return "in"
	case ClockPhaseBreak:
		return "break"
	}
	return "unknown"
}

// ClockPage renders the clock page from an explicit server projection. A
// missing or unknown state fails closed to the unavailable capability panel.
func ClockPage(view View, projection ClockProjection) ui.Node {
	if projection.State != ClockProjectionReady {
		return clockUnavailablePage(view)
	}
	return clockReadyPage(view, projection)
}

// clockPage is the registry adapter. Runtime composition can call ClockPage
// with its authorized projection when it binds the page module.
func clockPage(view View) ui.Node { return ClockPage(view, view.ClockProjection) }

// ClockPageModule returns the published module definition for the governed
// clock page. Its unavailable state remains fail-closed until the runtime
// projection is supplied by the clock service.
func ClockPageModule() PageModule {
	return pageModule(PageDefinition{
		ID: PageClock, Route: "/workspace/app/time/clock", Label: "Time clock", Icon: "clock",
		Title: "Time clock", Subtitle: "Clock in, take breaks and clock out.",
		LabelKey: "page.time_clock.label", TitleKey: "page.time_clock.title", SubtitleKey: "page.time_clock.subtitle",
		SearchTerms: []string{"time", "clock", "punch", "kiosk", "break", "timecard"}, PrimaryNav: true, Admitted: true, NavigationPublished: true, RenderOrder: 175, OwnsHeading: true,
	}, clockPageModuleRenderer{}, PageAccessPolicy{Audience: PageAudienceWorker}, routeProfileFor("/workspace/app/time/clock"), dataProfileFor("/workspace/app/time/clock"))
}

// clockPageModuleRenderer is kept stateless so the registry can use the same
// renderer contract as every other product page.
type clockPageModuleRenderer struct{}

func (clockPageModuleRenderer) Render(view View) ui.Node { return clockPage(view) }

func (clockPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("clock_status", "Clock status", "Authorized current clock status", true, false, false, false),
		feature("clock_actions", "Clock actions", "Use authorized clock actions", true, true, true, false),
		feature("kiosk_access", "Kiosk access", "Enroll or launch an authorized kiosk", true, true, true, false),
	}
}

// clockPageHead is the page title block. The clock page owns its heading so
// the h1 stays the one landmark the router focuses after navigation.
func clockPageHead(view View, subtitleKey string) ui.Node {
	return html.Div(html.Props{Class: "page-head clock-page-head", Data: map[string]string{"hcm-page": string(PageClock)}},
		html.Div(html.Props{},
			html.H1(html.Props{ID: "clock-page-title"}, ui.Text(clockText(view.Locale, "title"))),
			html.P(html.Props{Class: "subtitle"}, ui.Text(clockText(view.Locale, subtitleKey))),
		),
	)
}

func clockUnavailablePage(view View) ui.Node {
	// UXBLIND-123: state the real reason. A worker the clock ruled out is told
	// why, and only an administrator is shown how to change it; nobody is sent
	// to a supervisor for access that supervisor cannot give.
	reason := clockUnavailableReason(view)
	title, help := clockText(view.Locale, "unavailable"), clockText(view.Locale, "unavailable_help")
	if reason != ClockReasonNone {
		title, help = clockText(view.Locale, "reason."+string(reason)+".title"), clockText(view.Locale, "reason."+string(reason)+".help")
	}
	body := []ui.Node{
		html.H2(html.Props{}, ui.Text(title)),
		html.P(html.Props{Class: "muted"}, ui.Text(help)),
	}
	if reason == ClockReasonNone {
		body = append(body, timeRetryButton(view, PageClock, timeText(view.Locale, "retry")))
	}
	if reason != ClockReasonNone && ResolveClockSurface(view.ClockAvailability).AdminHelp {
		body = append(body, html.P(html.Props{Class: "clock-unavailable-admin"},
			html.Strong(html.Props{}, ui.Text(clockText(view.Locale, "admin_badge")+": ")),
			ui.Text(clockText(view.Locale, "reason."+string(reason)+".admin")),
		))
	}
	return html.Section(html.Props{
		Class: "clock-page clock-page-unavailable",
		Raw:   map[string]any{"aria-labelledby": "clock-page-title", "data-clock-reason": string(reason)},
	},
		clockPageHead(view, "description"),
		html.Div(html.Props{Class: "surface clock-unavailable", Role: "status"},
			html.Span(html.Props{Class: "clock-unavailable-mark", Raw: map[string]any{"aria-hidden": "true"}}, productIcon("clock", "clock-unavailable-icon")),
			html.Div(html.Props{}, body...),
		),
	)
}

func clockReadyPage(view View, projection ClockProjection) ui.Node {
	pageAttrs := map[string]any{"aria-labelledby": "clock-page-title"}
	if projection.Busy {
		pageAttrs["aria-busy"] = "true"
	}
	phase := projection.clockPhase()
	children := []ui.Node{
		clockPageHead(view, "description"),
		html.Div(html.Props{Class: "clock-layout"},
			clockNowPanel(view, projection, phase),
			clockSidePanel(view, projection),
		),
	}
	if kiosk := clockKioskPanel(view, projection); kiosk != nil {
		children = append(children, kiosk)
	}
	return html.Section(html.Props{Class: "clock-page", Raw: pageAttrs}, children...)
}

// clockNowPanel is the one place a worker looks to learn whether they are on
// the clock, and the one place the next action lives.
func clockNowPanel(view View, projection ClockProjection, phase ClockPhase) ui.Node {
	locale := view.Locale
	stateLine := strings.TrimSpace(projection.StatusLabel)
	sinceLine := strings.TrimSpace(projection.SinceLabel)
	if sinceLine == "" {
		sinceLine = strings.TrimSpace(projection.LastEventLabel)
	}
	head := []ui.Node{
		html.H2(html.Props{ID: "clock-status-title", Class: "sr-only"}, ui.Text(clockText(locale, "status_title"))),
	}
	if worker := strings.TrimSpace(projection.WorkerLabel); worker != "" {
		head = append(head, html.P(html.Props{Class: "clock-worker"}, ui.Text(worker)))
	}
	if stateLine != "" {
		head = append(head, html.Div(html.Props{Class: "clock-state"},
			html.Span(html.Props{Class: "clock-state-dot", Raw: map[string]any{"aria-hidden": "true"}}),
			html.P(html.Props{Class: "clock-state-label"}, ui.Text(stateLine)),
		))
	}
	if sinceLine != "" {
		head = append(head, html.P(html.Props{Class: "clock-state-since"}, ui.Text(sinceLine)))
	}
	children := head
	if projection.ErrorLabel != "" {
		children = append(children, html.P(html.Props{Class: "clock-notice clock-notice-error", Role: "alert"}, ui.Text(projection.ErrorLabel)))
	}
	if projection.Busy {
		// The button keeps the place the real action occupied, so the layout
		// does not jump, and says what is happening.
		children = append(children,
			html.P(html.Props{Class: "sr-only", Role: "status"}, ui.Text(clockText(locale, "action_busy"))),
			html.Div(html.Props{Class: "clock-actions"},
				html.Button(html.Props{Class: "button primary clock-primary", Type: "button", Disabled: true, Aria: map[string]string{"busy": "true"}}, ui.Text(clockText(locale, "action_busy")))),
		)
	} else if code := strings.TrimSpace(projection.ReceiptTraceLabel); code != "" && projection.ErrorLabel == "" {
		children = append(children, html.P(html.Props{Class: "clock-notice clock-notice-success", Role: "status"},
			html.Strong(html.Props{}, ui.Text(clockText(locale, "action_success"))),
			html.Span(html.Props{Class: "clock-receipt"}, ui.Text(clockText(locale, "receipt_trace")+": "+code)),
		))
	}
	if !projection.Busy {
		if actions := clockActionRow(view, projection, phase); actions != nil {
			children = append(children, actions)
		}
		if hint := clockPhaseHint(locale, phase); hint != "" {
			children = append(children, html.P(html.Props{Class: "clock-hint"}, ui.Text(hint)))
		}
	}
	return html.Section(html.Props{
		Class: "surface clock-now",
		Data:  map[string]string{"phase": clockPhaseName(phase)},
		Raw:   map[string]any{"aria-labelledby": "clock-status-title"},
	}, children...)
}

// clockPhaseHint only speaks before the day starts. Once the worker is on the
// clock the state, the buttons and their own labels say what to do; a sentence
// pointing at "the button above" would be ambiguous with two of them.
func clockPhaseHint(locale LocaleContext, phase ClockPhase) string {
	if phase == ClockPhaseOut {
		return clockText(locale, "hint_out")
	}
	return ""
}

// clockActionRow places exactly one primary action, chosen by phase, with the
// remaining authorized actions beneath it as secondary buttons.
func clockActionRow(view View, projection ClockProjection, phase ClockPhase) ui.Node {
	var primary, secondary ui.Node
	switch phase {
	case ClockPhaseBreak:
		primary = clockAction(view, projection.EndBreak, "clock_actions", "update", "button primary clock-primary")
		secondary = clockAction(view, projection.ClockOut, "clock_actions", "update", "button secondary clock-secondary")
	case ClockPhaseIn:
		primary = clockAction(view, projection.ClockOut, "clock_actions", "update", "button primary clock-primary")
		secondary = clockAction(view, projection.StartBreak, "clock_actions", "update", "button secondary clock-secondary")
		if primary != nil && projection.ClockOut != nil {
			primary = ui.CreateElement(clockConfirmedAction, clockConfirmedActionProps{View: view, Action: *projection.ClockOut})
		}
	default:
		primary = clockAction(view, projection.ClockIn, "clock_actions", "create", "button primary clock-primary")
		secondary = clockAction(view, projection.StartBreak, "clock_actions", "update", "button secondary clock-secondary")
		if secondary == nil {
			secondary = clockAction(view, projection.ClockOut, "clock_actions", "update", "button secondary clock-secondary")
		}
	}
	nodes := make([]ui.Node, 0, 2)
	if primary != nil {
		nodes = append(nodes, primary)
	}
	if secondary != nil {
		nodes = append(nodes, secondary)
	}
	if len(nodes) == 0 {
		return nil
	}
	return html.Div(html.Props{Class: "clock-actions"}, nodes...)
}

type clockConfirmedActionProps struct {
	View   View
	Action ActionLinkProps
}

// clockConfirmedAction guards clock-out: the first tap asks, only the named
// confirmation records the punch. Keeping working closes the question and
// records nothing.
func clockConfirmedAction(props clockConfirmedActionProps) ui.Node {
	asking := ui.UseState(false)
	view := props.View
	if !clockFeatureAllowed(view, "clock_actions", "update") || props.Action.Href == "" || props.Action.Label == "" || !validClockActionHref(props.Action.Href) {
		return html.Fragment()
	}
	if !asking.Get() {
		return html.Button(html.Props{
			Class: "button primary clock-primary", Type: "button",
			OnClick: ui.UseEvent(func(ui.MouseEvent) { asking.Set(true) }),
		}, ui.Text(props.Action.Label))
	}
	confirm := props.Action
	confirm.Class = "button destructive clock-confirm-yes"
	confirm.Label = clockText(view.Locale, "confirm_yes")
	if confirm.Navigate == nil {
		confirm.Navigate = view.Navigate
	}
	inner := confirm.Navigate
	confirm.Navigate = func(href string) {
		asking.Set(false)
		if inner != nil {
			inner(href)
		}
	}
	return clockConfirmPanel(view.Locale, ActionLink(confirm), html.Button(html.Props{
		Class: "button secondary clock-confirm-no", Type: "button",
		OnClick: ui.UseEvent(func(ui.MouseEvent) { asking.Set(false) }),
	}, ui.Text(clockText(view.Locale, "confirm_no"))))
}

// clockConfirmPanel is the clock-out question: what will happen, the named
// confirming action, and the way back.
func clockConfirmPanel(locale LocaleContext, yes, no ui.Node) ui.Node {
	return html.Div(html.Props{Class: "clock-confirm", Role: "group", Raw: map[string]any{"aria-labelledby": "clock-confirm-title"}},
		html.P(html.Props{ID: "clock-confirm-title", Class: "clock-confirm-title"}, ui.Text(clockText(locale, "confirm_title"))),
		html.P(html.Props{Class: "clock-confirm-body"}, ui.Text(clockText(locale, "confirm_body"))),
		html.Div(html.Props{Class: "clock-confirm-actions"}, yes, no),
	)
}

// clockSidePanel holds the facts that support the decision (today's shift,
// the last thing on record) and the routes for putting a punch right.
func clockSidePanel(view View, projection ClockProjection) ui.Node {
	locale := view.Locale
	children := []ui.Node{}
	facts := []ui.Node{}
	if value := strings.TrimSpace(projection.ScheduleLabel); value != "" {
		facts = append(facts, clockFact(locale, "schedule", value))
	}
	if value := strings.TrimSpace(projection.LastEventLabel); value != "" && strings.TrimSpace(projection.SinceLabel) != "" {
		facts = append(facts, clockFact(locale, "last_event", value))
	}
	if len(facts) > 0 {
		children = append(children, html.Tag("dl", html.Props{Class: "clock-facts"}, facts...))
	}
	var links []ui.Node
	for _, link := range []*ActionLinkProps{projection.FixPunch, projection.Timecard} {
		if node := clockAction(view, link, FeatureContent, "view", "clock-link"); node != nil {
			links = append(links, node)
		}
	}
	if len(links) > 0 {
		children = append(children, html.Div(html.Props{Class: "clock-links"},
			html.H2(html.Props{Class: "clock-side-title"}, ui.Text(clockText(locale, "links_title"))),
			html.Div(html.Props{Class: "clock-link-list"}, links...),
		))
	}
	if len(children) == 0 {
		return html.Div(html.Props{Class: "clock-side clock-side-empty", Hidden: true})
	}
	return html.Aside(html.Props{Class: "surface clock-side"}, children...)
}

// clockKioskPanel explains what the shared tablet is before offering to set
// one up or open it.
func clockKioskPanel(view View, projection ClockProjection) ui.Node {
	var actions []ui.Node
	if node := clockAction(view, projection.EnrollKiosk, "kiosk_access", "create", "button secondary"); node != nil {
		actions = append(actions, node)
	}
	if node := clockAction(view, projection.LaunchKiosk, "kiosk_access", "update", "button secondary"); node != nil {
		actions = append(actions, node)
	}
	if len(actions) == 0 || projection.Busy {
		return nil
	}
	return html.Section(html.Props{Class: "surface clock-kiosk", Raw: map[string]any{"aria-labelledby": "clock-kiosk-title"}},
		html.Div(html.Props{Class: "clock-kiosk-copy"},
			html.H2(html.Props{ID: "clock-kiosk-title"}, ui.Text(clockText(view.Locale, "kiosk_title"))),
			html.P(html.Props{Class: "muted"}, ui.Text(clockText(view.Locale, "kiosk_body"))),
		),
		html.Div(html.Props{Class: "clock-kiosk-actions"}, actions...),
	)
}

func clockFact(locale LocaleContext, key, value string) ui.Node {
	return html.Div(html.Props{Class: "clock-page-fact"},
		html.Tag("dt", html.Props{Class: "clock-page-fact-label"}, ui.Text(clockText(locale, key))),
		html.Tag("dd", html.Props{Class: "clock-page-fact-value"}, ui.Text(value)),
	)
}

func clockAction(view View, action *ActionLinkProps, feature FeatureID, operation, class string) ui.Node {
	if action == nil || action.Href == "" || action.Label == "" || !validClockActionHref(action.Href) || !clockFeatureAllowed(view, feature, operation) {
		return nil
	}
	copy := *action
	if class != "" {
		copy.Class = class
	}
	if copy.Navigate == nil {
		copy.Navigate = view.Navigate
	}
	return ActionLink(copy)
}

func clockFeatureAllowed(view View, feature FeatureID, operation string) bool {
	return view.CanFeature(PageClock, feature, operation)
}

func validClockActionHref(href string) bool {
	parsed, err := url.Parse(href)
	if err != nil || parsed.IsAbs() || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" || parsed.Path == "" {
		return false
	}
	for _, prefix := range []string{"/workspace/app/time/clock", "/workspace/app/time/kiosk", "/workspace/app/time/missing-punch", "/workspace/app/time/timecard"} {
		if parsed.Path == prefix || strings.HasPrefix(parsed.Path, prefix+"/") {
			return parsed.RawQuery == "" && parsed.Fragment == ""
		}
	}
	return false
}

func clockText(locale LocaleContext, key string) string {
	lookupKey := "clock_page." + key
	if key == "unavailable" {
		lookupKey = "clock_page.service_state"
	}
	if key == "title" {
		lookupKey = "page.time_clock.title"
	}
	if localized := locale.Text(lookupKey); localized != lookupKey && !strings.HasPrefix(localized, "⟦") {
		return localized
	}
	values := clockCopyFallback(locale.normalized().Resolved)
	if value, ok := values[lookupKey]; ok {
		return value
	}
	return clockCopyEN[lookupKey]
}
