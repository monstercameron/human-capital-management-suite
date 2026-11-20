package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// Issue 3990: "Help me understand my enrollment options". From a benefits
// discussion, or from a private agent conversation that asks about benefits,
// the person can open a guide that only they see. Before anything is read it
// says who sees it and which sources it uses, and it copies nothing from the
// conversation. On the person's request it reads, from the benefits service,
// the person's own eligibility and enrollment-window status for the kind of
// question they chose (a new-hire window and a life-event window are different
// windows). It shows that status with the published policy version, its source
// and when it was read, apart from the general rule; it never computes a
// deadline from a message, and with missing or unclear inputs it says so and
// points to the existing support request. Reading elects, uploads, discloses
// and sends nothing; the checklist and the support question are the person's
// private notes, and any later action stays in the owning flow.

const (
	// EnrollmentGuideNewHire, EnrollmentGuideLifeEvent and EnrollmentGuideNotSure
	// are the kinds of question the person chooses between.
	EnrollmentGuideNewHire   = "new-hire"
	EnrollmentGuideLifeEvent = "life-event"
	EnrollmentGuideNotSure   = "not-sure"

	issue3990Loading       = "loading"
	issue3990PhaseReady    = "ready"
	issue3990Unavailable   = "unavailable"
	issue3990Failed        = "failed"
	issue3990StaleAfter    = 5 * time.Minute
	issue3990NoteLimit     = 2000
	issue3990SupportHref   = "/workspace/app/help/hr-service-request"
	issue3990NoteID        = "enroll-guide-note"
	issue3990OpenAction    = "enroll-open"
	issue3990CloseAction   = "enroll-close"
	issue3990KindAction    = "enroll-kind"
	issue3990ReadAction    = "enroll-read"
	issue3990ChangeAction  = "enroll-change"
	issue3990StepAction    = "enroll-step"
	issue3990SourceAction  = "enroll-source"
	issue3990NoteAction    = "enroll-note"
	issue3990RegionSel     = `.enroll-guide[data-enroll-guide="open"]`
	issue3990OfferSelector = `.enroll-guide-offer`
)

// EnrollmentGuideStep is one next step the owning service or policy lists, with
// the evidence it asks for and where that comes from.
type EnrollmentGuideStep struct {
	ID, Title, Evidence, Source string
}

// EnrollmentGuideStatus is what the benefits service says about the person's own
// situation for one kind of question. Eligibility is "eligible", "conditional",
// "ineligible" or "unknown"; Window is "open", "closed", "conditional" or
// "unknown". Dates are the service's own and are shown only with TimeZone and a
// policy version.
type EnrollmentGuideStatus struct {
	// Kind is the kind of question this answers; an answer to another kind is
	// not shown.
	Kind                   string
	Eligibility, Window    string
	WindowStart, WindowEnd time.Time
	TimeZone               string
	// PolicyName and PolicyVersion name the published policy the status applies;
	// Source names the service that answered.
	PolicyName, PolicyVersion, Source string
	// Revision identifies the plan and window revision read, so a later read can
	// tell whether they changed.
	Revision string
	ReadAt   time.Time
	// NeedsEventDate: the service has no qualifying-event date for the person.
	NeedsEventDate bool
	// MoreThanOneWindow: more than one window could apply, so none is named.
	MoreThanOneWindow bool
	// Unavailable: the service has no status to give for this person.
	Unavailable bool
	// EnrollHref and SupportHref are same-site paths into the owning flows.
	EnrollHref, SupportHref string
	Checklist               []EnrollmentGuideStep
}

// issue3990UI is the guide's state, kept per person on the page.
type issue3990UI struct {
	Open bool
	// Room is the conversation the guide was opened in; it is drawn only there.
	Room     string
	Kind     string
	Phase    string
	Status   EnrollmentGuideStatus
	Changed  bool
	Stale    bool
	NeedKind bool
	Seq      int
	Note     string
	Done     map[string]bool
}

var issue3990Words = []string{"benefit", "enrol", "dependent", "qualifying event", "life event",
	// de-DE and ar names, topics and questions.
	"sozialleistung", "mitarbeitervorteil", "anmeldefenster", "lebensereignis", "angehörig", "مزايا", "التسجيل في", "معال"}

func issue3990Mentions(text string) bool {
	text = strings.ToLower(text)
	for _, word := range issue3990Words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// issue3990Where reports whether the guide is offered in the open conversation:
// a conversation the person has joined, in this tenant, that is about benefits
// by its name or topic, or their private agent conversation while the unsent
// message asks about benefits. Only names, topics and the draft are looked at;
// no message is read.
func issue3990Where(m Model) bool {
	c := m.selected()
	if c.ID == "" || !c.Joined {
		return false
	}
	if c.HostTenantID != "" && c.HostTenantID != m.CurrentTenantID {
		return false
	}
	if issue3990Mentions(c.Name) || issue3990Mentions(c.Topic) {
		return true
	}
	return c.Kind == DirectMessage && c.Agent && issue3990Mentions(m.Draft)
}

func issue3990SafeHref(href string) bool { return issue3986SafeHref(href) }

// issue3990Apply decides what one press does and returns the effect that
// remains for the browser: "open", "close", "read" or "".
func issue3990Apply(m Model, local localStore, action, id string) string {
	switch action {
	case issue3990OpenAction:
		local.update(func(u *localUI) {
			st := u.enroll
			if st.Room != m.SelectedID {
				st = issue3990UI{Note: st.Note}
			}
			st.Open, st.Room, st.NeedKind = true, m.SelectedID, false
			// A status read earlier is kept but must be read again before it is acted on.
			st.Stale = st.Phase == issue3990PhaseReady
			if st.Phase == issue3990Loading {
				st.Phase = ""
			}
			u.enroll = st
		})
		return "open"
	case issue3990CloseAction:
		local.update(func(u *localUI) { u.enroll.Open = false; u.enroll.Seq++ })
		return "close"
	case issue3990KindAction:
		switch id {
		case EnrollmentGuideNewHire, EnrollmentGuideLifeEvent, EnrollmentGuideNotSure:
		default:
			return ""
		}
		local.update(func(u *localUI) {
			if u.enroll.Kind != id {
				// Another kind of question is another window: nothing read for the
				// first is carried over.
				u.enroll.Phase, u.enroll.Status, u.enroll.Changed, u.enroll.Stale, u.enroll.Done = "", EnrollmentGuideStatus{}, false, false, nil
				u.enroll.Seq++
			}
			u.enroll.Kind, u.enroll.NeedKind = id, false
		})
		return ""
	case issue3990ReadAction:
		return issue3990Read(m, local)
	case issue3990ChangeAction:
		// Back to the choice of kind, with nothing read for the old one carried over
		// and the person's note kept.
		local.update(func(u *localUI) {
			u.enroll.Phase, u.enroll.Status, u.enroll.Changed, u.enroll.Stale, u.enroll.Done, u.enroll.NeedKind = "", EnrollmentGuideStatus{}, false, false, nil, false
			u.enroll.Seq++
		})
		return "read"
	case issue3990StepAction:
		local.update(func(u *localUI) {
			if id == "" {
				return
			}
			next := map[string]bool{}
			for key, value := range u.enroll.Done {
				next[key] = value
			}
			if next[id] {
				delete(next, id)
			} else {
				next[id] = true
			}
			u.enroll.Done = next
		})
		return ""
	case issue3990NoteAction:
		local.update(func(u *localUI) { u.enroll.Note = issue3990Clip(id) })
		return ""
	case issue3990SourceAction:
		local.update(func(u *localUI) { u.enroll.Note = issue3990Clip(issue3990Template(m, u.enroll)) })
		return ""
	}
	return ""
}

func issue3990Clip(text string) string {
	if runes := []rune(text); len(runes) > issue3990NoteLimit {
		return string(runes[:issue3990NoteLimit])
	}
	return text
}

// issue3990Template is the start of a question to support: it names the kind of
// question and, when a status was read, the policy it came from. It carries
// nothing about the person, and the person edits it.
func issue3990Template(m Model, st issue3990UI) string {
	kind := issue3990Text(m, "kind_"+st.Kind)
	if st.Kind == "" {
		kind = issue3990Text(m, "kind_not-sure")
	}
	text := issue3990Format(m, "note_plain", map[string]string{"kind": kind})
	if st.Phase == issue3990PhaseReady && st.Status.PolicyVersion != "" {
		text = issue3990Format(m, "note_source", map[string]string{"kind": kind, "policy": st.Status.PolicyName, "version": st.Status.PolicyVersion,
			"time": localize.FormatClock(m.Locale, st.Status.ReadAt.Local())})
	}
	if prior := strings.TrimSpace(st.Note); prior != "" {
		return prior + "\n" + text
	}
	return text
}

// issue3990Read asks the benefits service for the person's own status, for the
// kind they chose. Without a way to read, the guide says the status is not
// available here rather than showing anything.
func issue3990Read(m Model, local localStore) string {
	st := local.get().enroll
	if st.Kind == "" {
		local.update(func(u *localUI) { u.enroll.NeedKind = true })
		return ""
	}
	if m.Callbacks.ReadEnrollmentGuide == nil {
		local.update(func(u *localUI) {
			u.enroll.Phase, u.enroll.Status, u.enroll.Changed, u.enroll.Stale, u.enroll.NeedKind = issue3990Unavailable, EnrollmentGuideStatus{}, false, false, false
		})
		return "read"
	}
	if st.Phase == issue3990Loading {
		return ""
	}
	kind, seq := st.Kind, st.Seq+1
	previous := st
	local.update(func(u *localUI) {
		u.enroll.Phase, u.enroll.Seq, u.enroll.NeedKind, u.enroll.Stale = issue3990Loading, seq, false, false
	})
	m.Callbacks.ReadEnrollmentGuide(kind, func(status EnrollmentGuideStatus, err error) {
		local.update(func(u *localUI) {
			if !u.enroll.Open || u.enroll.Seq != seq || u.enroll.Kind != kind {
				return
			}
			switch {
			case err != nil:
				u.enroll.Phase, u.enroll.Status = issue3990Failed, EnrollmentGuideStatus{}
			case status.Unavailable:
				u.enroll.Phase, u.enroll.Status, u.enroll.Changed = issue3990Unavailable, EnrollmentGuideStatus{}, false
			case status.Kind != kind:
				// An answer to another question is not an answer to this one.
				u.enroll.Phase, u.enroll.Status, u.enroll.Changed = issue3990Unavailable, EnrollmentGuideStatus{}, false
			default:
				u.enroll.Changed = previous.Phase == issue3990PhaseReady && previous.Status.Kind == kind &&
					previous.Status.Revision != status.Revision
				u.enroll.Phase, u.enroll.Status = issue3990PhaseReady, status
				kept := map[string]bool{}
				for _, step := range status.Checklist {
					if u.enroll.Done[step.ID] {
						kept[step.ID] = true
					}
				}
				u.enroll.Done = kept
			}
		})
	})
	return "read"
}

// issue3990Click handles the panel's presses and reports whether it was one.
func issue3990Click(e ui.Event, m Model, local localStore) bool {
	action, id, _ := eventAction(e)
	if !strings.HasPrefix(action, "enroll-") {
		return false
	}
	if st := local.get().enroll; st.Open && st.Phase != "" && st.Phase != issue3990Loading {
		// What the person typed (including text they cleared) is kept before
		// anything redraws the field; a box not on the page changes nothing.
		if typed, ok := issue3990NoteField(); ok {
			local.update(func(u *localUI) { u.enroll.Note = issue3990Clip(typed) })
		}
	}
	if action != issue3990KindAction && action != issue3990StepAction {
		e.PreventDefault()
	}
	switch issue3990Apply(m, local, action, id) {
	case "open":
		chatux026Focus(issue3990RegionSel)
	case "close":
		chatux026Focus(issue3990OfferSelector)
	case "read":
		chatux026Focus(issue3990RegionSel)
	}
	if action == issue3990SourceAction {
		chatux026Focus("#" + issue3990NoteID)
	}
	return true
}

// issue3990Panel is the offer, the scope, and the guide, drawn above the
// message box for this person only.
func issue3990Panel(m Model, local localUI) ui.Node {
	st := local.enroll
	open := st.Open && st.Room == m.SelectedID
	if !open && !issue3990Where(m) {
		return nil
	}
	title := issue3990Text(m, "open")
	if !open {
		return html.Div(html.Props{Class: "enroll-guide enroll-guide-closed", Data: map[string]string{"enroll-guide": "closed"}},
			html.Button(html.Props{Class: "enroll-guide-offer", Type: "button", Aria: map[string]string{"expanded": "false"}, Data: map[string]string{"action": issue3990OpenAction}, Text: title}))
	}
	children := []ui.Node{html.H4(html.Props{Class: "enroll-title", Dir: "auto", Text: title})}
	switch st.Phase {
	case issue3990Loading:
		children = append(children, html.P(html.Props{Class: "enroll-status", Role: "status", Dir: "auto", Text: issue3990Text(m, "loading")}),
			html.Div(html.Props{Class: "enroll-actions"}, issue3990Button(m, issue3990CloseAction, "close", false)))
	case issue3990PhaseReady:
		children = append(children, issue3990Result(m, st)...)
	case issue3990Unavailable:
		children = append(children,
			html.P(html.Props{Class: "enroll-status", Role: "status", Dir: "auto", Text: issue3990Text(m, "unavailable")}),
			html.P(html.Props{Class: "enroll-note", Dir: "auto", Text: issue3990Text(m, "general")}))
		children = append(children, issue3990Support(m, st, "")...)
	case issue3990Failed:
		children = append(children,
			html.P(html.Props{Class: "enroll-failed", Role: "alert", Dir: "auto", Text: issue3990Text(m, "failed")}))
		children = append(children, issue3990Support(m, st, "")...)
	default:
		children = append(children, issue3990Scope(m, st)...)
	}
	return html.Div(html.Props{Class: "enroll-guide", Role: "region", TabIndex: -1, Aria: map[string]string{"label": title}, Data: map[string]string{"enroll-guide": "open", "phase": st.Phase}}, children...)
}

// issue3990Scope says who sees the guide and what it uses before anything is
// read, and asks which kind of question this is.
func issue3990Scope(m Model, st issue3990UI) []ui.Node {
	nodes := []ui.Node{
		html.P(html.Props{Class: "enroll-note", Dir: "auto", Text: issue3990Text(m, "scope_intro")}),
		html.Ul(html.Props{Class: "enroll-scope"},
			html.Li(html.Props{Dir: "auto", Text: issue3990Text(m, "scope_audience")}),
			html.Li(html.Props{Dir: "auto", Text: issue3990Text(m, "scope_source")}),
			html.Li(html.Props{Dir: "auto", Text: issue3990Text(m, "scope_not_copied")})),
		html.P(html.Props{Class: "enroll-note", Dir: "auto", Text: issue3990Text(m, "general")}),
	}
	options := make([]ui.Node, 0, 4)
	options = append(options, html.Legend(html.Props{Class: "enroll-legend", Dir: "auto", Text: issue3990Text(m, "kind_legend")}))
	for _, kind := range []string{EnrollmentGuideNewHire, EnrollmentGuideLifeEvent, EnrollmentGuideNotSure} {
		id := "enroll-kind-" + kind
		options = append(options, html.Label(html.Props{Class: "enroll-kind", For: id},
			html.Input(html.Props{ID: id, Type: "radio", Name: "enroll-guide-kind", Value: kind, Checked: st.Kind == kind, Data: map[string]string{"action": issue3990KindAction, "id": kind}}),
			html.Span(html.Props{Dir: "auto", Text: issue3990Text(m, "kind_"+kind)})))
	}
	nodes = append(nodes, html.Fieldset(html.Props{Class: "enroll-kinds"}, options...))
	if st.NeedKind {
		nodes = append(nodes, html.P(html.Props{Class: "enroll-failed", Role: "alert", Dir: "auto", Text: issue3990Text(m, "need_kind")}))
	}
	nodes = append(nodes,
		html.P(html.Props{Class: "enroll-note", Dir: "auto", Text: issue3990Text(m, "private")}),
		html.Div(html.Props{Class: "enroll-actions"}, issue3990Button(m, issue3990ReadAction, "read", true), issue3990Button(m, issue3990CloseAction, "close", false)))
	return nodes
}

// issue3990Window is the window line, or "" with the reason it is not shown. A
// window is named only for a kind the person chose, with no other window
// possible, an event date where one is needed, a time zone and a policy version.
func issue3990Window(m Model, st issue3990UI) (line, reason string) {
	s := st.Status
	switch {
	case st.Kind == EnrollmentGuideNotSure:
		return "", "reason_not_sure"
	case s.MoreThanOneWindow:
		return "", "reason_both"
	case st.Kind == EnrollmentGuideLifeEvent && s.NeedsEventDate:
		return "", "reason_event_date"
	case s.PolicyVersion == "":
		return "", "reason_no_policy"
	}
	label := ""
	switch s.Window {
	case "open", "closed", "conditional":
		label = issue3990Text(m, "window_"+s.Window)
	default:
		return "", "reason_no_window"
	}
	if s.TimeZone == "" {
		return "", "reason_no_zone"
	}
	if s.WindowStart.IsZero() && s.WindowEnd.IsZero() {
		return "", "reason_no_window"
	}
	zone := time.UTC
	if loc, err := time.LoadLocation(s.TimeZone); err == nil {
		zone = loc
	}
	date := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return formatShortDate(m.Locale, t.In(zone))
	}
	span := strings.Trim(date(s.WindowStart)+" – "+date(s.WindowEnd), " –")
	return label + " · " + span + " (" + s.TimeZone + ")", ""
}

func issue3990Result(m Model, st issue3990UI) []ui.Node {
	s := st.Status
	elig := "unknown"
	switch s.Eligibility {
	case "eligible", "conditional", "ineligible":
		elig = s.Eligibility
	}
	source := s.Source
	if source == "" {
		source = issue3990Text(m, "source_unnamed")
	}
	version := issue3990Text(m, "version_unnamed")
	if s.PolicyVersion != "" {
		version = issue3990Format(m, "policy_line", map[string]string{"policy": s.PolicyName, "version": s.PolicyVersion})
	}
	nodes := []ui.Node{
		html.P(html.Props{Class: "enroll-status", Role: "status", Dir: "auto",
			Text: issue3990Format(m, "read_at", map[string]string{"time": localize.FormatClock(m.Locale, s.ReadAt.Local()), "source": source})}),
	}
	if st.Changed {
		nodes = append(nodes, html.P(html.Props{Class: "enroll-changed", Role: "alert", Dir: "auto", Text: issue3990Text(m, "changed")}))
	}
	if st.Stale || (!s.ReadAt.IsZero() && time.Since(s.ReadAt) > issue3990StaleAfter) {
		nodes = append(nodes, html.P(html.Props{Class: "enroll-stale", Role: "status", Dir: "auto", Text: issue3990Text(m, "stale")}))
	}
	nodes = append(nodes,
		html.Section(html.Props{Class: "enroll-block", Data: map[string]string{"block": "general"}, Aria: map[string]string{"label": issue3990Text(m, "general_heading")}},
			html.H5(html.Props{Dir: "auto", Text: issue3990Text(m, "general_heading")}),
			html.P(html.Props{Dir: "auto", Text: version}),
			html.P(html.Props{Class: "enroll-note", Dir: "auto", Text: issue3990Text(m, "general")})))
	facts := []ui.Node{
		html.Div(html.Props{Class: "enroll-fact"}, html.Tag("dt", html.Props{}, ui.Text(issue3990Text(m, "fact_kind"))), html.Tag("dd", html.Props{Dir: "auto"}, ui.Text(issue3990Text(m, "kind_"+st.Kind)))),
		html.Div(html.Props{Class: "enroll-fact"}, html.Tag("dt", html.Props{}, ui.Text(issue3990Text(m, "fact_eligibility"))), html.Tag("dd", html.Props{Dir: "auto"}, ui.Text(issue3990Text(m, "eligibility_"+elig)))),
	}
	line, reason := issue3990Window(m, st)
	if line == "" {
		line = issue3990Text(m, "window_unknown")
	}
	facts = append(facts, html.Div(html.Props{Class: "enroll-fact", Data: map[string]string{"fact": "window"}}, html.Tag("dt", html.Props{}, ui.Text(issue3990Text(m, "fact_window"))), html.Tag("dd", html.Props{Dir: "auto"}, ui.Text(line))))
	nodes = append(nodes, html.Section(html.Props{Class: "enroll-block", Data: map[string]string{"block": "status"}, Aria: map[string]string{"label": issue3990Text(m, "status_heading")}},
		html.H5(html.Props{Dir: "auto", Text: issue3990Text(m, "status_heading")}),
		html.Tag("dl", html.Props{Class: "enroll-facts"}, facts...)))
	if reason != "" {
		nodes = append(nodes, html.P(html.Props{Class: "enroll-note", Role: "status", Dir: "auto", Text: issue3990Text(m, reason) + " " + issue3990Text(m, "ask_support")}))
	}
	if len(s.Checklist) > 0 {
		rows := make([]ui.Node, 0, len(s.Checklist))
		for _, step := range s.Checklist {
			id := "enroll-step-" + step.ID
			parts := []ui.Node{html.Span(html.Props{Class: "enroll-step-title", Dir: "auto", Text: step.Title})}
			if step.Evidence != "" {
				parts = append(parts, html.Span(html.Props{Class: "enroll-step-meta", Dir: "auto", Text: issue3990Format(m, "evidence", map[string]string{"evidence": step.Evidence})}))
			}
			if step.Source != "" {
				parts = append(parts, html.Span(html.Props{Class: "enroll-step-meta", Dir: "auto", Text: issue3990Format(m, "step_source", map[string]string{"source": step.Source})}))
			}
			rows = append(rows, html.Li(html.Props{Class: "enroll-step"}, html.Label(html.Props{For: id},
				html.Input(html.Props{ID: id, Type: "checkbox", Checked: st.Done[step.ID], Data: map[string]string{"action": issue3990StepAction, "id": step.ID}}),
				html.Span(html.Props{Class: "enroll-step-body"}, parts...))))
		}
		nodes = append(nodes, html.Section(html.Props{Class: "enroll-block", Data: map[string]string{"block": "checklist"}, Aria: map[string]string{"label": issue3990Text(m, "checklist_heading")}},
			html.H5(html.Props{Dir: "auto", Text: issue3990Text(m, "checklist_heading")}),
			html.P(html.Props{Class: "enroll-note", Dir: "auto", Text: issue3990Text(m, "checklist_note")}),
			html.Ul(html.Props{Class: "enroll-steps"}, rows...)))
	}
	if issue3990SafeHref(s.EnrollHref) {
		nodes = append(nodes, html.A(html.Props{Class: "enroll-link", Href: s.EnrollHref, Data: map[string]string{"enroll-link": "enroll"}, Text: issue3990Text(m, "enroll_link")}),
			html.P(html.Props{Class: "enroll-note", Dir: "auto", Text: issue3990Text(m, "enroll_note")}))
	}
	nodes = append(nodes, issue3990Support(m, st, s.SupportHref)...)
	return nodes
}

// issue3990Support is the way to the existing support request, the person's
// private question for it, and the buttons that read again or close.
func issue3990Support(m Model, st issue3990UI, supportHref string) []ui.Node {
	if !issue3990SafeHref(supportHref) {
		supportHref = issue3990SupportHref
	}
	none := map[string]any{"form": issue3788FormNone}
	nodes := []ui.Node{
		html.A(html.Props{Class: "enroll-link", Href: supportHref, Data: map[string]string{"enroll-link": "support"}, Text: issue3990Text(m, "support_link")}),
		html.Div(html.Props{Class: "enroll-field"},
			html.Label(html.Props{For: issue3990NoteID, Text: issue3990Text(m, "note_label")}),
			html.Textarea(html.Props{ID: issue3990NoteID, Class: "chat-input", Rows: 3, Dir: "auto", Raw: none, Data: map[string]string{"chat-value": st.Note},
				Aria: map[string]string{"describedby": issue3990NoteID + "-help"}}),
			html.P(html.Props{ID: issue3990NoteID + "-help", Class: "enroll-note", Dir: "auto", Text: issue3990Text(m, "note_help")})),
		html.P(html.Props{Class: "enroll-note", Dir: "auto", Text: issue3990Text(m, "private")}),
		html.Div(html.Props{Class: "enroll-actions"},
			issue3990Button(m, issue3990SourceAction, "note_start", false),
			issue3990Button(m, issue3990ReadAction, "again", false),
			issue3990Button(m, issue3990ChangeAction, "change_kind", false),
			issue3990Button(m, issue3990CloseAction, "close", false)),
	}
	return nodes
}

func issue3990Button(m Model, action, key string, primary bool) ui.Node {
	class := "button small"
	if !primary {
		class = "button secondary small"
	}
	return html.Button(html.Props{Class: class, Type: "button", Data: map[string]string{"action": action}, Text: issue3990Text(m, key)})
}

func issue3990Format(m Model, key string, values map[string]string) string {
	text := issue3990Text(m, key)
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

func issue3990Text(m Model, key string) string {
	table := map[string][3]string{
		"open":                    {"Help me understand my enrollment options", "Helfen Sie mir, meine Anmeldeoptionen zu verstehen", "ساعدني في فهم خيارات التسجيل الخاصة بي"},
		"scope_intro":             {"Before anything is read, this is what the guide will use:", "Bevor etwas gelesen wird, verwendet die Hilfe Folgendes:", "قبل قراءة أي شيء، هذا ما سيستخدمه الدليل:"},
		"scope_audience":          {"Only you can see this guide. It is not posted to the conversation, and nothing from it is shown to others.", "Nur Sie sehen diese Hilfe. Sie wird nicht in der Unterhaltung gepostet, und nichts daraus wird anderen angezeigt.", "أنت وحدك ترى هذا الدليل. لا يُنشر في المحادثة ولا يظهر أي شيء منه للآخرين."},
		"scope_source":            {"Sources: your own eligibility and enrollment-window status from the benefits service, and the published policy it applies. Only what you may see is read, and only after you ask.", "Quellen: Ihr eigener Anspruchs- und Anmeldefensterstatus aus dem Leistungsdienst und die veröffentlichte Richtlinie, die dort gilt. Gelesen wird nur, was Sie sehen dürfen, und erst nach Ihrer Anfrage.", "المصادر: حالة أهليتك ونافذة التسجيل الخاصة بك من خدمة المزايا، والسياسة المنشورة التي تنطبق. تُقرأ فقط الأمور التي يحق لك رؤيتها، وبعد طلبك فقط."},
		"scope_not_copied":        {"Messages, history and files of this conversation are not copied into the guide.", "Nachrichten, Verlauf und Dateien dieser Unterhaltung werden nicht in die Hilfe kopiert.", "لا تُنسخ رسائل هذه المحادثة ولا سجلها ولا ملفاتها إلى الدليل."},
		"general":                 {"Messages in a channel are general reminders. They are not a decision about your eligibility and not your deadline.", "Nachrichten in einem Kanal sind allgemeine Hinweise. Sie sind keine Entscheidung über Ihren Anspruch und nicht Ihre Frist.", "الرسائل في القناة تذكيرات عامة. وهي ليست قرارًا بشأن أهليتك وليست موعدك النهائي."},
		"kind_legend":             {"What kind of question is this?", "Um welche Art von Frage geht es?", "ما نوع هذا السؤال؟"},
		"kind_new-hire":           {"New-hire enrollment", "Anmeldung als neue Mitarbeitende", "التسجيل للموظفين الجدد"},
		"kind_life-event":         {"Life event, such as adding a dependent", "Lebensereignis, etwa das Hinzufügen einer abhängigen Person", "حدث حياتي، مثل إضافة معال"},
		"kind_not-sure":           {"I am not sure", "Ich bin nicht sicher", "لست متأكدًا"},
		"need_kind":               {"Choose which kind of question this is before you check.", "Wählen Sie die Art der Frage, bevor Sie prüfen.", "اختر نوع السؤال قبل أن تتحقق."},
		"private":                 {"Only you can see this. Nothing is elected, uploaded or sent, and no details are shared with anyone. Any later step happens in its own page, where it is reviewed first.", "Nur Sie sehen dies. Es wird nichts gewählt, hochgeladen oder gesendet, und keine Angaben werden mit anderen geteilt. Jeder weitere Schritt erfolgt auf seiner eigenen Seite und wird dort zuerst geprüft.", "أنت وحدك ترى هذا. لا يُختار شيء ولا يُرفع ولا يُرسل، ولا تُشارك أي تفاصيل مع أحد. أي خطوة لاحقة تتم في صفحتها الخاصة وتُراجع هناك أولًا."},
		"read":                    {"Check my status", "Meinen Status prüfen", "التحقق من حالتي"},
		"again":                   {"Check again", "Erneut prüfen", "التحقق مرة أخرى"},
		"change_kind":             {"Change the kind of question", "Art der Frage ändern", "تغيير نوع السؤال"},
		"close":                   {"Close", "Schließen", "إغلاق"},
		"loading":                 {"Checking your status…", "Ihr Status wird geprüft …", "جارٍ التحقق من حالتك…"},
		"unavailable":             {"Your enrollment status could not be checked here, so no window, deadline or eligibility is shown. If you are unsure, ask support.", "Ihr Anmeldestatus konnte hier nicht geprüft werden, daher werden kein Fenster, keine Frist und kein Anspruch angezeigt. Wenn Sie unsicher sind, fragen Sie den Support.", "تعذّر التحقق من حالة تسجيلك هنا، لذا لا تُعرض أي نافذة أو موعد نهائي أو أهلية. إذا لم تكن متأكدًا، فاسأل الدعم."},
		"failed":                  {"Your status could not be read, so this is not an answer about your eligibility or deadline. Nothing was changed. Try again, or ask support.", "Ihr Status konnte nicht gelesen werden. Dies ist also keine Antwort zu Anspruch oder Frist. Es wurde nichts geändert. Versuchen Sie es erneut oder fragen Sie den Support.", "تعذّرت قراءة حالتك، لذا فهذه ليست إجابة عن أهليتك أو موعدك النهائي. لم يتغير شيء. حاول مرة أخرى أو اسأل الدعم."},
		"read_at":                 {"Read at {time} from {source}", "Gelesen um {time} aus {source}", "قُرئ في {time} من {source}"},
		"source_unnamed":          {"the benefits service", "dem Leistungsdienst", "خدمة المزايا"},
		"changed":                 {"The window or the plan changed while this guide was open. What is shown now is the latest; check it again before you act.", "Das Fenster oder der Plan hat sich geändert, während diese Hilfe geöffnet war. Angezeigt wird der neueste Stand; prüfen Sie ihn erneut, bevor Sie handeln.", "تغيّرت النافذة أو الخطة أثناء فتح هذا الدليل. ما يُعرض الآن هو الأحدث؛ تحقق منه مرة أخرى قبل أن تتصرف."},
		"stale":                   {"This may be out of date. Check again before you act on it.", "Dies ist möglicherweise veraltet. Prüfen Sie erneut, bevor Sie handeln.", "قد يكون هذا قديمًا. تحقق مرة أخرى قبل أن تتصرف بناءً عليه."},
		"general_heading":         {"The general rule", "Die allgemeine Regel", "القاعدة العامة"},
		"status_heading":          {"Your status", "Ihr Status", "حالتك"},
		"policy_line":             {"Published policy: {policy}, version {version}.", "Veröffentlichte Richtlinie: {policy}, Version {version}.", "السياسة المنشورة: {policy}، الإصدار {version}."},
		"version_unnamed":         {"The policy version was not stated, so no deadline is shown.", "Die Richtlinienversion wurde nicht genannt, daher wird keine Frist angezeigt.", "لم يُذكر إصدار السياسة، لذا لا يُعرض أي موعد نهائي."},
		"fact_kind":               {"Question", "Frage", "السؤال"},
		"fact_eligibility":        {"Eligibility", "Anspruch", "الأهلية"},
		"fact_window":             {"Enrollment window", "Anmeldefenster", "نافذة التسجيل"},
		"eligibility_eligible":    {"Eligible", "Anspruchsberechtigt", "مؤهل"},
		"eligibility_conditional": {"Conditional", "Unter Bedingungen", "مشروط"},
		"eligibility_ineligible":  {"Not eligible", "Nicht anspruchsberechtigt", "غير مؤهل"},
		"eligibility_unknown":     {"Not known", "Nicht bekannt", "غير معروف"},
		"window_open":             {"Open", "Offen", "مفتوحة"},
		"window_closed":           {"Closed", "Geschlossen", "مغلقة"},
		"window_conditional":      {"Depends on conditions", "Abhängig von Bedingungen", "تعتمد على شروط"},
		"window_unknown":          {"Not known", "Nicht bekannt", "غير معروفة"},
		"reason_not_sure":         {"Because you are not sure which kind of question this is, more than one window may apply, so none is named.", "Da Sie nicht sicher sind, um welche Art von Frage es geht, könnten mehrere Fenster gelten, daher wird keines genannt.", "بما أنك لست متأكدًا من نوع السؤال، فقد تنطبق أكثر من نافذة، لذا لا تُذكر أي نافذة."},
		"reason_both":             {"More than one window could apply to you, so none is named.", "Für Sie könnten mehrere Fenster gelten, daher wird keines genannt.", "قد تنطبق عليك أكثر من نافذة، لذا لا تُذكر أي نافذة."},
		"reason_event_date":       {"The benefits service has no event date for you, so no deadline can be given. A date in a message is not used.", "Der Leistungsdienst hat kein Ereignisdatum für Sie, daher kann keine Frist genannt werden. Ein Datum aus einer Nachricht wird nicht verwendet.", "ليس لدى خدمة المزايا تاريخ حدث خاص بك، لذا لا يمكن إعطاء موعد نهائي. لا يُستخدم تاريخ ورد في رسالة."},
		"reason_no_policy":        {"No policy version was given with this status, so no window is shown.", "Zu diesem Status wurde keine Richtlinienversion genannt, daher wird kein Fenster angezeigt.", "لم تُذكر نسخة السياسة مع هذه الحالة، لذا لا تُعرض أي نافذة."},
		"reason_no_window":        {"The benefits service gave no dates for this window, so none are shown.", "Der Leistungsdienst hat für dieses Fenster keine Daten genannt, daher werden keine angezeigt.", "لم تُقدّم خدمة المزايا تواريخ لهذه النافذة، لذا لا تُعرض أي تواريخ."},
		"reason_no_zone":          {"The window came without a time zone, so its dates are not shown.", "Das Fenster wurde ohne Zeitzone geliefert, daher werden seine Daten nicht angezeigt.", "جاءت النافذة دون منطقة زمنية، لذا لا تُعرض تواريخها."},
		"ask_support":             {"Ask support to confirm your situation.", "Bitten Sie den Support, Ihre Situation zu bestätigen.", "اطلب من الدعم تأكيد وضعك."},
		"checklist_heading":       {"Next steps and evidence", "Nächste Schritte und Nachweise", "الخطوات التالية والأدلة"},
		"checklist_note":          {"Your private checklist. Ticking a step only marks it here: nothing is submitted or uploaded.", "Ihre private Checkliste. Das Abhaken eines Schritts markiert ihn nur hier: Es wird nichts eingereicht oder hochgeladen.", "قائمتك الخاصة. وضع علامة على خطوة يميّزها هنا فقط: لا يُقدَّم شيء ولا يُرفع."},
		"evidence":                {"Evidence: {evidence}", "Nachweis: {evidence}", "الدليل: {evidence}"},
		"step_source":             {"Source: {source}", "Quelle: {source}", "المصدر: {source}"},
		"enroll_link":             {"Go to enrollment", "Zur Anmeldung", "الانتقال إلى التسجيل"},
		"enroll_note":             {"Enrollment opens in its own page, which checks your access and the current window again before anything is submitted.", "Die Anmeldung öffnet sich auf einer eigenen Seite, die Ihren Zugriff und das aktuelle Fenster erneut prüft, bevor etwas eingereicht wird.", "يفتح التسجيل في صفحته الخاصة، التي تتحقق من وصولك والنافذة الحالية مرة أخرى قبل تقديم أي شيء."},
		"support_link":            {"Ask support (HR service request)", "Support fragen (HR-Serviceanfrage)", "اسأل الدعم (طلب خدمة الموارد البشرية)"},
		"note_label":              {"My question for support (kept only on this page)", "Meine Frage an den Support (nur auf dieser Seite gespeichert)", "سؤالي للدعم (يُحفظ في هذه الصفحة فقط)"},
		"note_help":               {"Write only what you choose to share. Nothing is sent from here; copy it into the support request yourself.", "Schreiben Sie nur, was Sie teilen möchten. Von hier wird nichts gesendet; kopieren Sie den Text selbst in die Supportanfrage.", "اكتب فقط ما تختار مشاركته. لا يُرسل شيء من هنا؛ انسخه بنفسك إلى طلب الدعم."},
		"note_start":              {"Start my question", "Meine Frage beginnen", "ابدأ سؤالي"},
		"note_plain":              {"My question is about: {kind}.\nMy question: ", "Meine Frage betrifft: {kind}.\nMeine Frage: ", "سؤالي يتعلق بـ: {kind}.\nسؤالي: "},
		"note_source":             {"My question is about: {kind}. Source: {policy}, version {version}, read at {time}.\nMy question: ", "Meine Frage betrifft: {kind}. Quelle: {policy}, Version {version}, gelesen um {time}.\nMeine Frage: ", "سؤالي يتعلق بـ: {kind}. المصدر: {policy}، الإصدار {version}، قُرئ في {time}.\nسؤالي: "},
	}
	return chatbug039Text("issue3990."+key, table[key][chatbug039LocaleIndex(m.Locale)], table[key][0])
}

// chatIssue3990Styles lays the guide out above the message box: logical
// properties so it holds in right-to-left, wrapping text, and touch-sized
// controls on a phone.
const chatIssue3990Styles = `.enroll-guide{display:grid;gap:8px;margin-block:8px;min-width:0}` +
	`.enroll-guide:not(.enroll-guide-closed){padding:12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface)}` +
	`.enroll-guide-offer{justify-self:start;min-height:36px;padding:6px 12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem;cursor:pointer;text-align:start}` +
	`.enroll-guide-offer:hover,.enroll-guide-offer:focus-visible{border-color:var(--accent);color:var(--accent)}` +
	`.enroll-title,.enroll-guide h5{margin:0;font-size:.9375rem}.enroll-guide p{margin:0;font-size:.8125rem;line-height:1.4;overflow-wrap:anywhere}` +
	`.enroll-note,.enroll-step-meta{color:var(--muted)}.enroll-step-meta{display:block;font-size:.75rem;overflow-wrap:anywhere}` +
	`.enroll-failed{color:var(--danger,var(--ink))}.enroll-changed,.enroll-stale{font-weight:600}` +
	`.enroll-scope,.enroll-steps{display:grid;gap:6px;margin:0;padding:0;padding-inline-start:0;list-style:none;font-size:.8125rem}` +
	`.enroll-kinds{display:grid;gap:4px;margin:0;padding:0;border:0;min-width:0}.enroll-legend{padding:0;font-size:.8125rem;font-weight:600}` +
	`.enroll-kind,.enroll-step label{display:flex;align-items:flex-start;gap:8px;min-height:32px;cursor:pointer;font-size:.8125rem}` +
	`.enroll-block{display:grid;gap:4px;padding-block:6px;border-block-start:1px solid var(--line);min-width:0}` +
	`.enroll-facts{display:grid;gap:4px;margin:0}.enroll-fact{display:flex;flex-wrap:wrap;gap:4px 12px}.enroll-fact dt{font-weight:600}.enroll-fact dd{margin:0;margin-inline-start:0;overflow-wrap:anywhere}` +
	`.enroll-link{justify-self:start;display:inline-flex;align-items:center;min-height:32px;font-size:.8125rem}` +
	`.enroll-field{display:grid;gap:4px;min-width:0}.enroll-field textarea{width:100%;box-sizing:border-box}` +
	`.enroll-actions{display:flex;flex-wrap:wrap;gap:8px}` +
	`@media (pointer:coarse),(max-width:480px){.enroll-guide-offer,.enroll-link,.enroll-kind,.enroll-step label,.enroll-actions .button{min-height:44px}.enroll-actions{flex-direction:column}.enroll-actions .button{width:100%}}`
