package journey

import (
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/uicomponents"
)

// This file holds every section of the Promotion journey page. Three rules
// run through all of it:
//
//   - The page is live-first. When the contract carries a callback
//     (Page.OnFieldChange, ProposalForm.OnSubmit, Action.OnSubmit,
//     JourneyCard.OnOpen, NavLink.OnNavigate) the renderer wires it as a GWC
//     event handler and the browser's own navigation or POST is prevented.
//     When it does not -- the SSR and test path -- the same tree renders as
//     plain links and plain forms, so every assertion in this package is
//     made against markup rather than a simulated DOM.
//   - Nothing means anything by color alone. Every tone is paired with a
//     word (a chip's own label, a visually-hidden state name on a step, a
//     severity word on a finding), so the page reads the same in
//     monochrome, in forced-colors mode, and to a screen reader.
//   - Nothing is positioned by an inline style attribute. The document's
//     content-security-policy pins the sha256 of one stylesheet, and a
//     style="" attribute is governed by style-src-attr, which that policy
//     does not allow. Everything data-driven -- the pay band gauge, the
//     budget meter -- is therefore drawn as inline SVG, whose x/width are
//     presentation attributes rather than CSS.

// Tone vocabulary. Anything outside this set is normalised to toneNeutral by
// toneOf, so a tone the projecting lane adds later degrades to a plain chip
// instead of an unstyled one.
const (
	toneNeutral = "neutral"
	toneInfo    = "info"
	toneSuccess = "success"
	toneWarning = "warning"
	toneDanger  = "danger"
)

// Finding severity vocabulary.
const (
	severityBlocking = "blocking"
	severityWarning  = "warning"
	severityInfo     = "info"
	severitySuccess  = "success"
)

// Step state vocabulary.
const (
	stepDone     = "done"
	stepActive   = "active"
	stepUpcoming = "upcoming"
	stepFailed   = "failed"
)

// Field kind vocabulary.
const (
	fieldKindText     = "text"
	fieldKindNumber   = "number"
	fieldKindDate     = "date"
	fieldKindSelect   = "select"
	fieldKindTextarea = "textarea"
	fieldKindHidden   = "hidden"
)

// skipTarget is the id of the <main> landmark the skip link jumps to.
const skipTarget = "main-content"

func toneOf(tone string) string {
	switch tone {
	case toneInfo, toneSuccess, toneWarning, toneDanger:
		return tone
	default:
		return toneNeutral
	}
}

func severityOf(severity string) string {
	switch severity {
	case severityBlocking, severityWarning, severitySuccess:
		return severity
	default:
		return severityInfo
	}
}

func stepStateOf(state string) string {
	switch state {
	case stepDone, stepActive, stepFailed:
		return state
	default:
		return stepUpcoming
	}
}

// severityTone maps the findings vocabulary onto the tone vocabulary, so a
// blocking finding and a danger chip look like the same thing because they
// are the same thing.
func severityTone(severity string) string {
	switch severityOf(severity) {
	case severityBlocking:
		return toneDanger
	case severityWarning:
		return toneWarning
	case severitySuccess:
		return toneSuccess
	default:
		return toneInfo
	}
}

// stepStateWord is the screen-reader-only name of a step's state, so the
// stepper's meaning survives without its colors and markers.
func stepStateWord(state string) string {
	switch state {
	case stepDone:
		return "Completed"
	case stepActive:
		return "Current stage"
	case stepFailed:
		return "Did not complete"
	default:
		return "Not started"
	}
}

// severityWord is the same idea for a finding: the severity is spelled out
// next to the message rather than encoded in the border color.
func severityWord(severity string) string {
	switch severity {
	case severityBlocking:
		return "Blocking"
	case severityWarning:
		return "Warning"
	case severitySuccess:
		return "Passed"
	default:
		return "Information"
	}
}

func visuallyHidden(text string) ui.Node {
	return html.Span(html.Props{Class: "jn-visually-hidden"}, html.Text(text))
}

// GWC v5.0.1 ships typed builders for most of HTML but not for the
// description list, which is the right element for a label/value grid (a
// <div> grid of <span>s carries no relationship a screen reader can walk).
// html.Tag is the library's own escape hatch for exactly this and goes
// through the same props normalisation as every typed builder.
func dl(props html.Props, children ...ui.Node) ui.Node { return html.Tag("dl", props, children...) }
func dt(props html.Props, children ...ui.Node) ui.Node { return html.Tag("dt", props, children...) }
func dd(props html.Props, children ...ui.Node) ui.Node { return html.Tag("dd", props, children...) }

// ----------------------------------------------------------------------
// Live wiring
// ----------------------------------------------------------------------

// live carries the page-level live callbacks down to the controls that need
// them. Its zero value is the SSR path: every control renders uncontrolled
// and every form posts.
type live struct {
	values        map[string]string
	onFieldChange func(fieldID, value string)
}

func liveOf(p Page) live {
	return live{values: p.Values, onFieldChange: p.OnFieldChange}
}

// controlled reports whether the client owns the field values. When it
// does, a control's value comes from Page.Values and every keystroke goes
// back through OnFieldChange; when it does not, the browser owns the value
// and the renderer only seeds it.
func (l live) controlled() bool { return l.onFieldChange != nil }

// value is the value to render for f: the client's, when it has one, and
// the field's own otherwise. The fallback matters on the first live render,
// which must be byte-identical to the server-rendered shell.
func (l live) value(f Field) string {
	if v, ok := l.values[f.ID]; ok {
		return v
	}
	return f.Value
}

// input returns the handler for a field's input event, or the zero handler
// on the SSR path (GWC omits an unset handler entirely, so the same call
// produces plain markup).
func (l live) input(f Field) ui.Handler {
	if !l.controlled() {
		return ui.Handler{}
	}
	id := f.ID
	change := l.onFieldChange
	return ui.UseEvent(func(e ui.InputEvent) { change(id, e.GetValue()) })
}

// collect assembles what a submitted form carries: every hidden entry, then
// every visible field's current value keyed by its Name. Hidden entries are
// written first so a field can never shadow the CSRF token by being named
// after it -- if a projection does that, the token wins and the server
// rejects the request rather than accepting a forged one.
func (l live) collect(hidden map[string]string, fields []Field) map[string]string {
	values := make(map[string]string, len(hidden)+len(fields))
	for _, f := range fields {
		if f.Name == "" {
			continue
		}
		if f.Kind == fieldKindHidden {
			values[f.Name] = f.Value
			continue
		}
		values[f.Name] = l.value(f)
	}
	for k, v := range hidden {
		values[k] = v
	}
	return values
}

// submitHandler wires a form's submit event to a live callback, preventing
// the browser's own POST. It returns the zero handler when the callback is
// nil, which is what leaves the SSR form a real, working POST.
func (l live) submitHandler(onSubmit func(map[string]string), hidden map[string]string, fields []Field) ui.Handler {
	if onSubmit == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(e ui.FormEvent) {
		e.PreventDefault()
		onSubmit(l.collect(hidden, fields))
	})
}

// activate wires a link to a live callback. Href is still emitted: a link
// the client handles must remain a real link, or middle-click, "open in new
// tab" and copy-link all stop working (WCAG 2.5.3 and plain good manners).
func activate(fn func()) ui.Handler {
	if fn == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(e ui.MouseEvent) {
		e.PreventDefault()
		fn()
	})
}

// clickHandler wires a plain button.
func clickHandler(fn func(map[string]string), values map[string]string) ui.Handler {
	if fn == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(e ui.MouseEvent) {
		e.PreventDefault()
		fn(values)
	})
}

// ----------------------------------------------------------------------
// Small shared pieces
// ----------------------------------------------------------------------

// chip is the page's one status pill: a tinted background, dark text of the
// same hue, and the tone's own icon. The label is always present -- a chip
// is never just a color.
func chip(tone, label string) ui.Node {
	t := toneOf(tone)
	return html.Span(html.Props{Class: "jn-chip", DataAttr: html.DataAttribute{Name: "tone", Value: t}},
		chipIcon(t),
		html.Text(label),
	)
}

// chipIcon is the 12px tone glyph inside a chip. Neutral gets a dot because
// there is no such thing as a neutral icon that is not noise.
func chipIcon(tone string) ui.Node {
	if tone == toneNeutral {
		return html.Span(html.Props{Class: "jn-chip-dot", Aria: map[string]string{"hidden": "true"}})
	}
	return iconForTone(tone, "jn-chip-icon")
}

// hiddenInputs emits a form's hidden inputs in sorted key order. Sorting is
// not cosmetic: Go map iteration is randomised, so an unsorted range would
// make every render of the same page a different document, and the
// reconciler would churn every hidden input on every re-render.
func hiddenInputs(values map[string]string) []ui.Node {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	nodes := make([]ui.Node, 0, len(keys))
	for _, k := range keys {
		nodes = append(nodes, html.HiddenInput(k, values[k]))
	}
	return nodes
}

// metaItem is one "Label value" pair in a card's dense meta row.
func metaItem(label, value string, mono bool) ui.Node {
	if value == "" {
		return nil
	}
	class := "jn-meta-value"
	if mono {
		class += " jn-mono"
	}
	return html.Span(html.Props{Class: "jn-meta-item"},
		html.Span(html.Props{Class: "jn-meta-key"}, html.Text(label+" ")),
		html.Span(html.Props{Class: class}, html.Text(value)),
	)
}

// htmlIf is html.If with a lazily built node: html.If evaluates its
// argument before it can decide, which is wrong whenever the node's
// construction dereferences the thing the condition is guarding.
func htmlIf(cond bool, build func() ui.Node) ui.Node {
	if !cond {
		return nil
	}
	return build()
}

// clampPct pins a percentage into [0, 100] and formats it for an SVG
// coordinate. A projection that computes 118% of a pay band is telling the
// truth about the proposal; drawing the marker off the end of the track
// would just lose it, so the gauge clamps and the note carries the number.
func clampPct(pct float64) float64 {
	switch {
	case pct < 0:
		return 0
	case pct > 100:
		return 100
	default:
		return pct
	}
}

// coord formats a float for an SVG attribute with a stable, locale-free
// representation, so two renders of the same page produce the same bytes.
func coord(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// ----------------------------------------------------------------------
// Chrome: skip link, masthead, notice, footer
// ----------------------------------------------------------------------

func skipLink() ui.Node {
	return html.A(html.Props{Class: "jn-skip", Href: "#" + skipTarget}, html.Text("Skip to main content"))
}

func masthead(p Page) ui.Node {
	return html.Header(html.Props{Class: "jn-masthead"},
		html.Div(html.Props{Class: "jn-shell jn-masthead-inner"},
			html.Div(html.Props{Class: "jn-brandbar"},
				html.Span(html.Props{Class: "jn-markwell"}, BrandMark()),
				html.Div(html.Props{Class: "jn-brandtext"},
					html.Span(html.Props{Class: "jn-brand-name"}, html.Text(p.Brand)),
					htmlIf(p.TenantLabel != "", func() ui.Node {
						return html.Span(html.Props{Class: "jn-tenant"}, html.Text(p.TenantLabel))
					}),
				),
			),
			mastheadNav(p.Nav),
			principalChip(p.Principal),
		),
	)
}

func mastheadNav(links []NavLink) ui.Node {
	if len(links) == 0 {
		return nil
	}
	return html.Nav(html.Props{Class: "jn-nav", Aria: map[string]string{"label": "Primary"}},
		html.Ul(html.Props{Role: "list"}, html.Map(links, func(l NavLink) ui.Node {
			props := html.Props{Href: l.Href, OnClick: activate(l.OnNavigate)}
			if l.Current {
				props.Aria = map[string]string{"current": "page"}
			}
			return html.Li(html.Props{}, html.A(props, html.Text(l.Label)))
		})...),
	)
}

// principalChip names who is signed in, which roles admitted them and the
// purpose the session was opened under. The page shows this on every screen
// because every answer below it was filtered by exactly these three things.
//
// UXAUDIT-007's GREEN clause requires this explanation to sit next to an
// exit action, in the same masthead: a reviewer told "Purpose:
// compensation_review" needs an obvious way to leave that context from the
// same place they learned they were in it. This session has no narrower way
// to drop just the purpose while staying signed in, so the exit control is
// the same LogoutHref destination the ordinary "Sign out" control already
// uses -- but when a Purpose is present, its label names that purpose
// explicitly ("Exit <purpose> and sign out") instead of the bare, generic
// "Sign out" a purposeless session still gets. The pairing is deliberate:
// the two spans render adjacently, and [TestPrincipalChipPairsPurposeWithItsExit]
// asserts the pairing by value, not merely that each half exists somewhere
// on the page.
func principalChip(pr Principal) ui.Node {
	if pr.Subject == "" && len(pr.Roles) == 0 && pr.Purpose == "" && pr.LogoutHref == "" {
		return nil
	}
	children := []ui.Node{
		visuallyHidden("Signed in as "),
		html.Span(html.Props{Class: "jn-principal-subject"}, html.Text(pr.Subject)),
	}
	for _, role := range pr.Roles {
		children = append(children, html.Span(html.Props{Class: "jn-rolechip"}, html.Text(role)))
	}
	if pr.Purpose != "" {
		children = append(children, html.Span(html.Props{Class: "jn-principal-purpose"},
			visuallyHidden("Purpose: "), html.Text(pr.Purpose)))
	}
	if pr.LogoutHref != "" {
		exitLabel := "Sign out"
		exitProps := html.Props{Class: "jn-logout", Href: pr.LogoutHref}
		if pr.Purpose != "" {
			exitLabel = "Exit " + pr.Purpose + " and sign out"
			exitProps.Class = "jn-logout jn-exit-purpose"
			exitProps.Aria = map[string]string{"label": exitLabel}
		}
		children = append(children, html.A(exitProps, html.Text(exitLabel)))
	}
	return html.Div(html.Props{Class: "jn-principal"}, children...)
}

// noticeRegion is always in the document, whether or not there is a notice
// to show. A live region has to exist before the content it announces: a
// role="status" element inserted at the same moment as its text is not
// reliably announced, and on the live client the notice appears and
// disappears as RPCs answer.
func noticeRegion(n *Notice) ui.Node {
	region := html.Props{Class: "jn-live", Role: "status", Aria: map[string]string{"live": "polite"}}
	if n == nil {
		return html.Div(region)
	}
	tone := toneOf(n.Tone)
	return html.Div(region,
		html.Div(html.Props{Class: "jn-noticeband"},
			html.Div(html.Props{Class: "jn-shell"},
				html.Div(html.Props{Class: "jn-notice", DataAttr: html.DataAttribute{Name: "tone", Value: tone}},
					iconForTone(tone, "jn-notice-icon"),
					html.Div(html.Props{Class: "jn-notice-body"},
						html.P(html.Props{Class: "jn-notice-title"},
							visuallyHidden(severityLabelForTone(tone)+": "),
							html.Text(n.Title)),
						htmlIf(n.Detail != "", func() ui.Node {
							return html.P(html.Props{Class: "jn-notice-detail"}, html.Text(n.Detail))
						}),
					),
				),
			),
		),
	)
}

func severityLabelForTone(tone string) string {
	switch tone {
	case toneSuccess:
		return "Success"
	case toneWarning:
		return "Warning"
	case toneDanger:
		return "Error"
	default:
		return "Notice"
	}
}

func footer(f Footer) ui.Node {
	items := make([]ui.Node, 0, 4)
	if f.PolicyVersion != "" {
		items = append(items, metaItem("Policy", f.PolicyVersion, true))
	}
	if f.CellID != "" {
		items = append(items, metaItem("Cell", f.CellID, true))
	}
	if f.BuildRef != "" {
		items = append(items, metaItem("Build", f.BuildRef, true))
	}
	lines := make([]ui.Node, 0, len(f.Lines)+1)
	if len(items) > 0 {
		lines = append(lines, html.P(html.Props{Class: "jn-provenance"}, items...))
	}
	for _, line := range f.Lines {
		lines = append(lines, html.P(html.Props{}, html.Text(line)))
	}
	return html.Footer(html.Props{Class: "jn-footer"},
		html.Div(html.Props{Class: "jn-shell jn-footer-inner"},
			html.H2(html.Props{Class: "jn-visually-hidden"}, html.Text("Provenance")),
			html.Fragment(lines...),
		),
	)
}

// ----------------------------------------------------------------------
// Focused proposal view
// ----------------------------------------------------------------------

func proposalView(p Page, v ProposalView) ui.Node {
	name := "this employee"
	if v.Subject != nil && strings.TrimSpace(v.Subject.Name) != "" {
		name = v.Subject.Name
	}
	return html.Div(html.Props{Class: "jn-stack jn-proposal-view"},
		pageHeader(pageHeaderProps{
			Class: "jn-proposal-head", Eyebrow: "Career & compensation", Title: "Promote " + name,
			Lead: "Build a governed change for this employee. Their current assignment is locked from the authorized worker record; review it before entering the proposed role and pay.",
			Actions: []ui.Node{
				htmlIf(v.BackHref != "", func() ui.Node {
					return html.A(html.Props{Class: "jn-context-link", Href: v.BackHref, OnClick: activate(v.BackNavigate)}, html.Text("View "+name+"'s profile"))
				}),
				htmlIf(v.JourneysLink.Href != "", func() ui.Node {
					return html.A(html.Props{Class: "jn-context-link", Href: v.JourneysLink.Href, OnClick: activate(v.JourneysLink.OnNavigate)}, html.Text("View all promotion journeys"))
				}),
			},
		}),
		htmlIf(v.Loading, func() ui.Node {
			return loadingPanel("Loading employee context", "Reading the selected employee and the promotion options available under this access purpose.")
		}),
		htmlIf(!v.Loading, func() ui.Node { return promotionSubjectCard(v.Subject) }),
		htmlIf(!v.Loading, func() ui.Node { return engineUnavailableCallout(v.EngineAvailable, v.EngineNotice) }),
		htmlIf(!v.Loading, func() ui.Node { return proposalFormSection(liveOf(p), v.Form, "Promotion details") }),
	)
}

func promotionSubjectCard(s *PromotionSubject) ui.Node {
	if s == nil {
		return html.Section(html.Props{Class: "jn-panel jn-subject-card", Aria: map[string]string{"labelledby": "subject-heading"}},
			html.H2(html.Props{ID: "subject-heading"}, html.Text("Employee context unavailable")),
			html.P(html.Props{Class: "jn-muted"}, html.Text("This employee is not readable under the current access purpose. Return to their profile or choose another employee there.")),
		)
	}
	return html.Section(html.Props{Class: "jn-panel jn-subject-card", Aria: map[string]string{"labelledby": "subject-heading"}},
		html.Div(html.Props{Class: "jn-subject-identity"},
			uicomponents.Avatar(uicomponents.AvatarProps{Name: s.Name, PhotoURL: s.PhotoURL, Class: "jn-subject-avatar", Decorative: true}),
			html.Div(html.Props{},
				html.P(html.Props{Class: "jn-eyebrow"}, html.Text("Promotion subject")),
				html.H2(html.Props{ID: "subject-heading"}, html.Text(s.Name)),
				htmlIf(s.Title != "", func() ui.Node { return html.P(html.Props{Class: "jn-subject-title"}, html.Text(s.Title)) }),
			),
			chip(toneInfo, "Profile context locked"),
		),
		factsListWithClass([]Fact{
			{Label: "Worker", Value: valueOrDash(s.Number)},
			{Label: "Current job", Value: valueOrDash(joinNonEmpty(" · ", s.JobCode, s.Grade)), Mono: true},
			{Label: "Organization", Value: valueOrDash(s.OrgUnit)},
			{Label: "Location", Value: valueOrDash(s.Location)},
			{Label: "Current base", Value: valueOrDash(s.PayLine)},
		}, "jn-facts jn-subject-facts"),
	)
}

func joinNonEmpty(separator string, values ...string) string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			kept = append(kept, value)
		}
	}
	return strings.Join(kept, separator)
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

// ----------------------------------------------------------------------
// List view
// ----------------------------------------------------------------------

// listView is workforce-first, in this order: who this cell knows about, the
// form that adds to them, the proposal the selected person is the subject
// of, and only then the journeys already open. It reads the way the task
// runs -- create, see, pick, propose -- rather than the way the data model
// is shaped.
func listView(p Page, v ListView) ui.Node {
	l := liveOf(p)
	return html.Div(html.Props{Class: "jn-stack"},
		pageHeader(pageHeaderProps{Eyebrow: "Governed promotions", Title: "Promotion journeys", Lead: leadSentence(p)}),
		peopleSection(v.People),
		htmlIf(v.People != nil, func() ui.Node { return newEmployeeSection(l, v.People.Form) }),
		engineUnavailableCallout(v.EngineAvailable, v.EngineNotice),
		proposalSection(l, v),
		journeysSection(v),
	)
}

// embeddedListView gives the product route a workflow-first information
// architecture. The standalone operational surface remains workforce-first,
// while the integrated product already has a dedicated People module and
// therefore leads with the records and actions readers came here to use.
func embeddedListView(p Page, v ListView) ui.Node {
	if len(v.Journeys) == 0 {
		v.Empty = "No promotion requests are visible yet. Choose an employee above to start a request."
	}
	return html.Div(html.Props{Class: "jn-stack"},
		pageHeader(pageHeaderProps{Eyebrow: "Workflows", Title: "Promotion journeys", Lead: "Follow promotion requests, review their progress, and open past decisions.",
			Actions: []ui.Node{htmlIf(v.People != nil && v.People.DirectoryLink.Href != "", func() ui.Node {
				link := v.People.DirectoryLink
				return html.A(html.Props{Class: "jn-btn", Href: link.Href, OnClick: activate(link.OnNavigate)}, html.Text("Choose an employee to promote"))
			})},
		}),
		journeysSection(v),
		engineUnavailableCallout(v.EngineAvailable, v.EngineNotice),
	)
}

func leadSentence(p Page) string {
	who := readableTenantLabel(p.TenantLabel)
	if who == "" {
		who = "this tenant"
	}
	return "Every promotion proposed in " + who + ", followed from the manager's request through the " +
		"execution authority gate to the approver's decision and the ledger fact the workflow records."
}

func readableTenantLabel(value string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		return r == '-' || r == '_'
	})
	for index, part := range parts {
		runes := []rune(strings.ToLower(part))
		if len(runes) > 0 {
			runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		}
		parts[index] = string(runes)
	}
	return strings.Join(parts, " ")
}

func journeysSection(v ListView) ui.Node {
	body := ui.Node(nil)
	switch {
	case len(v.Groups) > 0:
		// UXAUDIT-017: "a lifecycle tracker grouped by subject and status".
		// A projector that populates Groups renders subject sections
		// instead of the flat grid below; a projector (or fixture) that
		// never sets it -- everything before this todo -- keeps the exact
		// flat rendering the other branches below already produce.
		body = html.Div(html.Props{Class: "jn-journey-groups"}, journeySubjectGroupSections(v.Groups)...)
	case len(v.Journeys) == 0:
		body = emptyState(v.Empty)
	default:
		body = html.Ul(html.Props{Class: "jn-grid", Role: "list"},
			html.Map(v.Journeys, func(j JourneyCard) ui.Node {
				return html.Li(html.Props{Class: "jn-griditem"}, journeyCard(j))
			})...)
	}
	return html.Section(html.Props{Aria: map[string]string{"labelledby": "journeys-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "journeys-heading"}, html.Text("Journeys")),
			chip(toneNeutral, countLabel(len(v.Journeys))),
		),
		body,
	)
}

// journeySubjectGroupSections renders one accessible section per subject
// group: a heading naming the subject, the group's distinct statuses (the
// "and status" half of GREEN, visible across the group even when a reader
// does not open every card in it), and the subject's own journeys, each
// still carrying its own per-card status chip (the "within" half).
func journeySubjectGroupSections(groups []JourneySubjectGroup) []ui.Node {
	sections := make([]ui.Node, 0, len(groups))
	for index, group := range groups {
		sections = append(sections, journeySubjectGroupSection(group, index))
	}
	return sections
}

func journeySubjectGroupSection(group JourneySubjectGroup, index int) ui.Node {
	headingID := "journey-group-" + strconv.Itoa(index) + "-heading"
	statusChips := make([]ui.Node, 0, len(group.Statuses))
	for _, status := range group.Statuses {
		statusChips = append(statusChips, chip(status.Tone, status.Label))
	}
	return html.Section(html.Props{Class: "jn-journey-group", Aria: map[string]string{"labelledby": headingID}},
		html.Div(html.Props{Class: "jn-journey-group-head"},
			html.H3(html.Props{ID: headingID, Class: "jn-journey-group-subject"}, html.Text(group.Subject)),
			chip(toneNeutral, countLabel(len(group.Journeys))),
			htmlIf(len(statusChips) > 0, func() ui.Node {
				return html.Div(html.Props{Class: "jn-journey-group-statuses", Aria: map[string]string{"label": "Statuses in this group"}}, statusChips...)
			}),
		),
		html.Ul(html.Props{Class: "jn-grid jn-journey-group-list", Role: "list"},
			html.Map(group.Journeys, func(j JourneyCard) ui.Node {
				return html.Li(html.Props{Class: "jn-griditem"}, journeyCard(j))
			})...),
	)
}

func countLabel(n int) string {
	if n == 1 {
		return "1 journey"
	}
	return strconv.Itoa(n) + " journeys"
}

func journeyCard(j JourneyCard) ui.Node {
	return html.Article(html.Props{Class: "jn-card jn-journey",
		DataAttr: html.DataAttribute{Name: "stage", Value: j.Stage}},
		html.Div(html.Props{Class: "jn-journey-top"},
			html.H3(html.Props{},
				html.A(html.Props{Href: j.Href, OnClick: activate(j.OnOpen)},
					html.Text(j.WorkerName),
					visuallyHidden(" — open this journey"),
				),
			),
			chip(j.StageTone, j.StageLabel),
		),
		htmlIf(j.Headline != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-journey-headline"}, html.Text(j.Headline))
		}),
		htmlIf(j.PayLine != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-journey-pay"}, html.Text(j.PayLine))
		}),
		html.P(html.Props{Class: "jn-meta"},
			metaItem("Effective", j.EffectiveDate, false),
			metaItem("Updated", j.Updated, false),
		),
		technicalDetailsSection(j.DiagnosticsAuthorized, []technicalDetail{
			{Label: "Worker", Value: j.WorkerRef},
			{Label: "Instance", Value: j.InstanceID},
		}),
		html.P(html.Props{Class: "jn-journey-foot", Aria: map[string]string{"hidden": "true"}},
			html.Text("Open journey"), iconArrowRight("jn-journey-arrow"),
		),
	)
}

// technicalDetail is one raw identifier PROMOUX-008's authorized Technical
// details disclosure may show.
type technicalDetail struct {
	Label string
	Value string
}

// technicalDetailsSection renders PROMOUX-008's authorized diagnostics
// disclosure. It exists in the markup at all only when authorized is true --
// never because a value happens to be non-empty -- so two viewers this
// package treats as equally unauthorized get byte-identical markup here
// (both nil) regardless of whether the underlying journey has an instance,
// a worker reference, or nothing at all; presence, count and layout carry
// no signal about the journey's real state to a viewer who is not
// authorized to know it. Each present value is redacted on screen
// (maskIdentifier) and carries its own copy control so an authorized viewer
// can still act on the full value without it being legible on screen or in
// a shared screen.
func technicalDetailsSection(authorized bool, items []technicalDetail) ui.Node {
	if !authorized {
		return nil
	}
	rows := make([]ui.Node, 0, len(items))
	for _, item := range items {
		if item.Value == "" {
			continue
		}
		value := item.Value
		rows = append(rows, html.Div(html.Props{Class: "jn-tech-row"},
			html.Span(html.Props{Class: "jn-meta-key"}, html.Text(item.Label+" ")),
			html.Span(html.Props{Class: "jn-meta-value jn-mono"}, html.Text(maskIdentifier(value))),
			html.Button(html.Props{
				Type:    "button",
				Class:   "jn-copy-btn",
				Aria:    map[string]string{"label": "Copy " + item.Label + " value"},
				OnClick: activate(func() { copyToClipboard(value) }),
			}, html.Text("Copy")),
		))
	}
	if len(rows) == 0 {
		return nil
	}
	return html.Details(html.Props{Class: "jn-journey-technical"},
		html.Summary(html.Props{}, html.Text("Technical details")),
		html.Div(html.Props{Class: "jn-meta jn-tech-body"}, rows...),
	)
}

// maskIdentifier redacts a raw identifier for on-screen display, keeping
// only its last four characters legible. The full value still reaches the
// copy control: masking narrows what a passerby or a shared screen shows,
// not what the authorized viewer can actually use.
func maskIdentifier(value string) string {
	const visible = 4
	if len(value) <= visible {
		return strings.Repeat("•", len(value))
	}
	return "••••" + value[len(value)-visible:]
}

func emptyState(message string) ui.Node {
	if message == "" {
		message = "No promotion journeys yet."
	}
	return html.Div(html.Props{Class: "jn-panel jn-empty"},
		iconEmpty("jn-empty-mark"),
		html.P(html.Props{Class: "jn-empty-title"}, html.Text("Nothing here yet")),
		html.P(html.Props{}, html.Text(message)),
	)
}

func proposalSection(l live, v ListView) ui.Node {
	heading := "Propose a promotion"
	if name := selectedWorkerName(v.People); name != "" {
		heading = "Propose a promotion for " + name
	}
	return proposalFormSection(l, v.Form, heading)
}

func proposalFormSection(l live, f ProposalForm, heading string) ui.Node {
	fields := make([]ui.Node, 0, len(f.Fields))
	for _, field := range f.Fields {
		fields = append(fields, fieldNode(l, field, f.Disabled))
	}
	submit := f.Submit
	if submit == "" {
		submit = "Propose promotion"
	}

	btn := html.Props{Class: "jn-btn", Type: submitButtonType(f.OnSubmit),
		DataAttr: html.DataAttribute{Name: "variant", Value: "primary"}}
	if f.OnSubmit != nil {
		btn.OnClick = clickHandler(f.OnSubmit, l.collect(f.Hidden, f.Fields))
	}
	foot := []ui.Node{}
	if f.Disabled {
		btn.Disabled = true
		btn.Aria = map[string]string{"describedby": "proposal-disabled"}
		foot = append(foot, html.Button(btn, html.Text(submit)))
	} else {
		foot = append(foot, html.Button(btn, html.Text(submit)),
			html.P(html.Props{Class: "jn-help"},
				html.Text("The proposal is simulated on submit; nothing is executed until the gate admits it.")))
	}

	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "propose-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "propose-heading"}, html.Text(heading)),
		),
		html.Form(formProps(l, f.Action, f.OnSubmit, f.Hidden, f.Fields),
			htmlIf(f.Disabled, func() ui.Node {
				return html.P(html.Props{ID: "proposal-disabled", Class: "jn-blocked", Raw: map[string]any{"role": "status"}},
					iconWarning("jn-blocked-icon"), html.Text(f.DisabledReason))
			}),
			html.Fragment(hiddenInputs(f.Hidden)...),
			html.Div(html.Props{Class: "jn-fieldgrid"}, fields...),
			html.Div(html.Props{Class: "jn-formfoot"}, foot...),
		),
	)
}

// formProps builds a <form>'s props for both paths at once. Live forms keep
// method and action when the projection supplied them (so a client that
// fails to boot still degrades to a working POST) and add the submit
// handler that prevents it; SSR forms get only the plain POST.
func formProps(l live, action string, onSubmit func(map[string]string), hidden map[string]string, fields []Field) html.Props {
	props := html.Props{Method: "post", Action: action}
	if onSubmit != nil {
		props.OnSubmit = l.submitHandler(onSubmit, hidden, fields)
	}
	return props
}

// submitButtonType is "button" once a live handler owns the submission.
// A type="submit" button in a live client would ask the browser to POST and
// rely on preventDefault to catch it; making it a plain button says what is
// actually true. The form still carries its own submit handler, so pressing
// Enter inside a text field does the same thing as clicking.
func submitButtonType(onSubmit func(map[string]string)) string {
	if onSubmit != nil {
		return "button"
	}
	return "submit"
}

// ----------------------------------------------------------------------
// Form fields
// ----------------------------------------------------------------------

// fieldNode renders one labelled control. Every control gets: a <label for>
// bound to its id, a spelled-out "(required)" for assistive technology
// beside the visual asterisk, aria-describedby listing its adornments, help
// and error in reading order, and aria-invalid when the engine rejected it.
func fieldNode(l live, f Field, formDisabled bool) ui.Node {
	if f.Kind == fieldKindHidden {
		return html.HiddenInput(f.Name, f.Value)
	}

	described := make([]string, 0, 4)
	if f.Prefix != "" {
		described = append(described, f.ID+"-prefix")
	}
	if f.Suffix != "" {
		described = append(described, f.ID+"-suffix")
	}
	if f.Help != "" {
		described = append(described, f.ID+"-help")
	}
	if f.Error != "" {
		described = append(described, f.ID+"-error")
	}

	aria := map[string]string{}
	if f.Required {
		aria["required"] = "true"
	}
	if f.Error != "" {
		aria["invalid"] = "true"
	}
	if len(described) > 0 {
		aria["describedby"] = strings.Join(described, " ")
	}
	if len(aria) == 0 {
		aria = nil
	}

	value := l.value(f)
	base := html.Props{
		ID:       f.ID,
		Name:     f.Name,
		Class:    "jn-input",
		Required: f.Required,
		Disabled: formDisabled,
		Aria:     aria,
		OnInput:  l.input(f),
	}

	var control ui.Node
	span := ""
	switch f.Kind {
	case fieldKindSelect:
		// A <select> emits change, not input, for keyboard and mouse
		// selection alike, so the controlled wiring goes on OnChange; the
		// selected option still comes from the value, never from the DOM.
		base.OnInput = ui.Handler{}
		base.OnChange = l.input(f)
		control = html.Select(base, html.Map(f.Options, func(o Option) ui.Node {
			selected := o.Selected
			if l.controlled() || value != "" {
				selected = o.Value == value
			}
			return html.Option(html.Props{Value: o.Value, Selected: selected}, html.Text(o.Label))
		})...)
	case fieldKindTextarea:
		base.Rows = 4
		base.Placeholder = f.Placeholder
		control = html.Textarea(base, html.Text(value))
		span = "full"
	case fieldKindNumber:
		base.Type = "number"
		base.Value = value
		base.Placeholder = f.Placeholder
		base.Step = f.Step
		base.Min = f.Min
		base.Raw = map[string]any{"inputmode": "decimal"}
		control = html.Input(base)
	case fieldKindDate:
		base.Type = "date"
		base.Value = value
		base.Min = f.Min
		control = html.Input(base)
	default:
		base.Type = "text"
		base.Value = value
		base.Placeholder = f.Placeholder
		control = html.Input(base)
	}

	wrapChildren := make([]ui.Node, 0, 3)
	if f.Prefix != "" {
		wrapChildren = append(wrapChildren, html.Span(html.Props{ID: f.ID + "-prefix", Class: "jn-adorn",
			DataAttr: html.DataAttribute{Name: "side", Value: "prefix"}}, html.Text(f.Prefix)))
	}
	wrapChildren = append(wrapChildren, control)
	if f.Suffix != "" {
		wrapChildren = append(wrapChildren, html.Span(html.Props{ID: f.ID + "-suffix", Class: "jn-adorn",
			DataAttr: html.DataAttribute{Name: "side", Value: "suffix"}}, html.Text(f.Suffix)))
	}

	labelChildren := []ui.Node{html.Text(f.Label)}
	if f.Required {
		labelChildren = append(labelChildren,
			html.Span(html.Props{Class: "jn-req", Aria: map[string]string{"hidden": "true"}}, html.Text("*")),
			visuallyHidden(" (required)"))
	}

	fieldProps := html.Props{Class: "jn-field"}
	data := map[string]string{}
	if span != "" {
		data["span"] = span
	}
	if f.Error != "" {
		data["invalid"] = "true"
	}
	if len(data) > 0 {
		fieldProps.Data = data
	}

	children := []ui.Node{
		html.Label(html.Props{Class: "jn-label", For: f.ID}, labelChildren...),
		html.Div(html.Props{Class: "jn-inputwrap"}, wrapChildren...),
	}
	if f.Help != "" {
		children = append(children, html.P(html.Props{ID: f.ID + "-help", Class: "jn-help"}, html.Text(f.Help)))
	}
	if f.Error != "" {
		children = append(children, html.P(html.Props{ID: f.ID + "-error", Class: "jn-error"},
			iconDanger("jn-error-icon"), visuallyHidden("Error: "), html.Text(f.Error)))
	}
	return html.Div(fieldProps, children...)
}

// ----------------------------------------------------------------------
// Detail view
// ----------------------------------------------------------------------

func detailView(p Page, v DetailView) ui.Node {
	l := liveOf(p)
	return html.Div(html.Props{Class: "jn-stack"},
		detailNavigation(v),
		heroSection(v.Journey),
		actionsSection(l, v.Actions),
		stepperSection(v.Steps),
		proposalDetailSection(v),
		html.Div(html.Props{Class: "jn-columns"},
			html.Div(html.Props{Class: "jn-col"},
				preflightSection(v),
				// PROMOUX-008: the workflow/outcome/evidence panels are the
				// diagnostic evidence projection over this same detail --
				// raw node executions, the terminal ledger write and its
				// digest, evidence references -- never something an
				// ordinary approver reads a decision from (that comes from
				// actionsSection and v.Actions above, computed
				// independently). They render only for a diagnostics-
				// authorized viewer, gated on that verdict rather than on
				// whether the sections happen to have content, so an
				// unauthorized viewer's page carries neither the panels nor
				// a hint that they exist.
				htmlIf(v.Journey.DiagnosticsAuthorized, func() ui.Node { return workflowSection(v) }),
				htmlIf(v.Journey.DiagnosticsAuthorized, func() ui.Node { return outcomeSection(v.Ledger) }),
				htmlIf(v.Journey.DiagnosticsAuthorized, func() ui.Node { return evidenceSection(v.Evidence) }),
			),
			html.Aside(html.Props{Class: "jn-col jn-rail", Aria: map[string]string{"label": "Actions and history"}},
				timelineSection(v.Timeline),
			),
		),
	)
}

func detailNavigation(v DetailView) ui.Node {
	links := make([]ui.Node, 0, 2)
	if v.BackLink.Href != "" {
		links = append(links, html.A(html.Props{Href: v.BackLink.Href, OnClick: activate(v.BackLink.OnNavigate)},
			html.Text(nonEmpty(v.BackLink.Label, "Back to employee"))))
	}
	if v.JourneysLink.Href != "" {
		links = append(links, html.A(html.Props{Href: v.JourneysLink.Href, OnClick: activate(v.JourneysLink.OnNavigate)},
			html.Text(nonEmpty(v.JourneysLink.Label, "View all promotion journeys"))))
	}
	if len(links) == 0 {
		return nil
	}
	return html.Nav(html.Props{Class: "jn-context-nav", Aria: map[string]string{"label": "Journey context"}}, links...)
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func heroSection(j JourneyCard) ui.Node {
	return html.Section(html.Props{Class: "jn-panel jn-hero", Aria: map[string]string{"labelledby": "journey-heading"}},
		html.Div(html.Props{Class: "jn-hero-top"},
			html.Div(html.Props{},
				html.P(html.Props{Class: "jn-eyebrow"}, html.Text("Promotion journey")),
				html.H1(html.Props{ID: "journey-heading", Class: "jn-display"}, html.Text(j.WorkerName)),
				htmlIf(j.Headline != "", func() ui.Node {
					return html.P(html.Props{Class: "jn-lead"}, html.Text(j.Headline))
				}),
			),
			chip(j.StageTone, j.StageLabel),
		),
		htmlIf(j.PayLine != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-hero-pay"}, html.Text(j.PayLine))
		}),
		html.P(html.Props{Class: "jn-meta jn-hero-ids"},
			metaItem("Effective", j.EffectiveDate, false),
			metaItem("Updated", j.Updated, false),
		),
		technicalDetailsSection(j.DiagnosticsAuthorized, []technicalDetail{
			{Label: "Worker", Value: j.WorkerRef},
			{Label: "Intent", Value: j.IntentID},
			{Label: "Instance", Value: j.InstanceID},
		}),
	)
}

func stepperSection(steps []Step) ui.Node {
	if len(steps) == 0 {
		return nil
	}
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "stages-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "stages-heading"}, html.Text("Stages")),
		),
		html.Ol(html.Props{Class: "jn-stepper"}, html.MapIndexed(steps, stepNode)...),
	)
}

func stepNode(index int, s Step) ui.Node {
	state := stepStateOf(s.State)
	props := html.Props{Class: "jn-step", DataAttr: html.DataAttribute{Name: "state", Value: state}}
	if state == stepActive {
		props.Aria = map[string]string{"current": "step"}
	}
	var mark ui.Node
	if state == stepDone {
		mark = html.Span(html.Props{Class: "jn-stepmark", Aria: map[string]string{"hidden": "true"}}, iconCheck("jn-stepcheck"))
	} else {
		mark = html.Span(html.Props{Class: "jn-stepmark", Aria: map[string]string{"hidden": "true"}},
			html.Text(strconv.Itoa(index+1)))
	}
	return html.Li(props,
		mark,
		html.Div(html.Props{Class: "jn-stepbody"},
			html.P(html.Props{Class: "jn-steplabel"},
				html.Text(s.Label),
				visuallyHidden(" — "+stepStateWord(state)),
			),
			htmlIf(s.Detail != "", func() ui.Node {
				return html.P(html.Props{Class: "jn-stepdetail"}, html.Text(s.Detail))
			}),
			htmlIf(s.At != "", func() ui.Node {
				return html.P(html.Props{Class: "jn-stepat"}, html.Text(s.At))
			}),
		),
	)
}

func proposalDetailSection(v DetailView) ui.Node {
	if len(v.Proposal) == 0 && len(v.Comparison) == 0 {
		return nil
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "proposal-heading"}, html.Text("Proposal")),
		),
	}
	if len(v.Comparison) > 0 {
		children = append(children,
			html.Div(html.Props{Class: "jn-subsection"},
				html.H3(html.Props{Class: "jn-subhead", ID: "comparison-heading"}, html.Text("Current and proposed")),
				comparisonTable(v.Comparison),
			))
	}
	if len(v.Proposal) > 0 {
		children = append(children,
			html.Div(html.Props{Class: "jn-subsection"},
				html.H3(html.Props{Class: "jn-subhead"}, html.Text("Request")),
				factsList(v.Proposal),
			))
	}
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "proposal-heading"}}, children...)
}

func factsList(facts []Fact) ui.Node {
	return factsListWithClass(facts, "jn-facts")
}

func factsListWithClass(facts []Fact, class string) ui.Node {
	return dl(html.Props{Class: class}, html.Map(facts, func(f Fact) ui.Node {
		props := html.Props{Class: "jn-fact"}
		if tone := toneOf(f.Tone); f.Tone != "" && tone != toneNeutral {
			props.DataAttr = html.DataAttribute{Name: "tone", Value: tone}
		}
		valueClass := ""
		if f.Mono {
			valueClass = "jn-mono"
		}
		return html.Div(props,
			dt(html.Props{}, html.Text(f.Label)),
			dd(html.Props{Class: valueClass}, html.Text(f.Value)),
		)
	})...)
}

func comparisonTable(rows []ComparisonRow) ui.Node {
	return html.Div(html.Props{Class: "jn-tablewrap"},
		html.Table(html.Props{Class: "jn-table"},
			html.Caption(html.Props{Class: "jn-visually-hidden"},
				html.Text("Current placement and pay compared with the proposal")),
			html.Thead(html.Props{},
				html.Tr(html.Props{},
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text("Attribute")),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text("Current")),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text("Proposed")),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text("Change")),
				),
			),
			html.Tbody(html.Props{}, html.Map(rows, comparisonRow)...),
		),
	)
}

func comparisonRow(r ComparisonRow) ui.Node {
	props := html.Props{}
	if r.Changed {
		props.DataAttr = html.DataAttribute{Name: "changed", Value: "true"}
	}
	// The change column always says in words what the row's tint says in
	// color, so a changed row is never identified by its background alone
	// (WCAG 1.4.1) -- and a row marked Changed whose Delta is empty (a job
	// code, a position id: changed but with no arithmetic to report) says
	// "Changed" rather than the flatly wrong "No change".
	change := []ui.Node{}
	switch {
	case r.Changed:
		change = append(change, chip(toneInfo, "Changed"))
		if r.Delta != "" {
			change = append(change, html.Text(" "),
				html.Span(html.Props{Class: "jn-delta"}, html.Text(r.Delta)))
		}
	default:
		change = append(change, html.Span(html.Props{Class: "jn-muted"}, html.Text("No change")))
	}
	return html.Tr(props,
		html.Th(html.Props{Raw: map[string]any{"scope": "row"}}, html.Text(r.Label)),
		html.Td(html.Props{Class: "jn-num"}, html.Text(r.Current)),
		html.Td(html.Props{Class: "jn-num jn-proposed"}, html.Text(r.Proposed)),
		html.Td(html.Props{Class: "jn-num jn-change"}, change...),
	)
}

// ----------------------------------------------------------------------
// Preflight: findings board, pay band gauge, budget meter, date window
// ----------------------------------------------------------------------

// preflightSection is where the simulation stops being a list of strings
// and starts being an instrument panel: what the checks said, where the
// proposal lands in the band, what it costs the envelope, and when it takes
// effect. Each piece is absent rather than empty when the simulation did
// not produce it.
func preflightSection(v DetailView) ui.Node {
	if len(v.Findings) == 0 && v.PayBand == nil && v.Budget == nil && v.EffectiveWindow == nil {
		return nil
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "findings-heading"}, html.Text("Preflight and simulation")),
			htmlIf(len(v.Findings) > 0, func() ui.Node {
				return html.P(html.Props{Class: "jn-count"}, html.Text(findingCountLabel(len(v.Findings))))
			}),
		),
	}
	if len(v.Findings) > 0 {
		children = append(children,
			html.Ul(html.Props{Class: "jn-board", Role: "list"}, html.MapIndexed(v.Findings, findingRow)...))
	}
	if v.PayBand != nil || v.Budget != nil {
		children = append(children, html.Div(html.Props{Class: "jn-gauges"},
			payBandGauge(v.PayBand),
			budgetMeter(v.Budget),
		))
	}
	if v.EffectiveWindow != nil {
		children = append(children, effectiveWindow(v.EffectiveWindow))
	}
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "findings-heading"}}, children...)
}

func findingCountLabel(n int) string {
	if n == 1 {
		return "1 check"
	}
	return strconv.Itoa(n) + " checks"
}

// findingRow is one line of the preflight board: a status pill that
// animates in, the check's message, and its code. The animation is
// staggered by position through a data-row attribute the stylesheet keys
// off, because a CSS animation-delay cannot be set per element without an
// inline style the content-security-policy forbids.
func findingRow(index int, f Finding) ui.Node {
	sev := severityOf(f.Severity)
	tone := severityTone(sev)
	row := index
	if row > 7 {
		row = 7
	}
	return html.Li(html.Props{Class: "jn-check",
		Data: map[string]string{"severity": sev, "row": strconv.Itoa(row)}},
		html.Span(html.Props{Class: "jn-checkpill",
			DataAttr: html.DataAttribute{Name: "tone", Value: tone}},
			iconForSeverity(sev, "jn-checkpill-icon"),
			html.Span(html.Props{Class: "jn-checkpill-label"}, html.Text(severityWord(sev))),
		),
		html.Div(html.Props{Class: "jn-check-body"},
			html.P(html.Props{Class: "jn-check-msg"}, html.Text(f.Message)),
			htmlIf(f.Code != "", func() ui.Node {
				return html.P(html.Props{Class: "jn-check-code jn-mono"}, html.Text(f.Code))
			}),
		),
	)
}

// payBandGauge draws where the current and proposed base sit in the target
// grade's band. It is an SVG rather than styled divs for one specific
// reason: the marker positions are data, and the only way to express data
// as geometry without an inline style attribute -- which this document's
// content-security-policy forbids -- is an SVG presentation attribute.
//
// The drawing is labelled, not decorative: it carries role="img" and an
// aria-label naming both positions, and the same reading is repeated as
// text underneath, so nothing here is available only to people who can see
// it.
func payBandGauge(b *PayBand) ui.Node {
	if b == nil {
		return nil
	}
	const (
		width   = 300.0
		trackY  = 26.0
		trackH  = 10.0
		padding = 6.0
	)
	span := width - 2*padding
	at := func(pct float64) float64 { return padding + span*clampPct(pct)/100 }
	currentX, proposedX := at(b.CurrentPct), at(b.ProposedPct)
	fillFrom, fillTo := currentX, proposedX
	if fillFrom > fillTo {
		fillFrom, fillTo = fillTo, fillFrom
	}

	label := "Pay band for the target grade: current base " + b.Current +
		", proposed base " + b.Proposed + ", band from " + b.Min + " to " + b.Max + "."

	return html.Div(html.Props{Class: "jn-gauge"},
		html.H3(html.Props{Class: "jn-subhead"}, html.Text("Pay band position")),
		html.Svg(html.Props{Class: "jn-band", Raw: map[string]any{
			"viewBox": "0 0 300 62", "role": "img", "aria-label": label,
			"focusable": "false",
		}},
			html.Rect(html.Props{Class: "jn-band-track", Raw: map[string]any{
				"x": coord(padding), "y": coord(trackY), "width": coord(span), "height": coord(trackH), "rx": "5",
			}}),
			html.Rect(html.Props{Class: "jn-band-fill", Raw: map[string]any{
				"x": coord(fillFrom), "y": coord(trackY), "width": coord(fillTo - fillFrom), "height": coord(trackH), "rx": "5",
			}}),
			html.Line(html.Props{Class: "jn-band-tick", Raw: map[string]any{
				"x1": coord(at(50)), "x2": coord(at(50)), "y1": coord(trackY - 5), "y2": coord(trackY + trackH + 5),
			}}),
			html.Line(html.Props{Class: "jn-band-marker", DataAttr: html.DataAttribute{Name: "which", Value: "current"},
				Raw: map[string]any{
					"x1": coord(currentX), "x2": coord(currentX), "y1": coord(trackY - 8), "y2": coord(trackY + trackH + 8),
				}}),
			html.Line(html.Props{Class: "jn-band-marker", DataAttr: html.DataAttribute{Name: "which", Value: "proposed"},
				Raw: map[string]any{
					"x1": coord(proposedX), "x2": coord(proposedX), "y1": coord(trackY - 8), "y2": coord(trackY + trackH + 8),
				}}),
		),
		html.P(html.Props{Class: "jn-gauge-scale"},
			html.Span(html.Props{}, html.Text("Min "+b.Min)),
			html.Span(html.Props{}, html.Text("Mid "+b.Mid)),
			html.Span(html.Props{}, html.Text("Max "+b.Max)),
		),
		dl(html.Props{Class: "jn-gauge-legend"},
			dt(html.Props{DataAttr: html.DataAttribute{Name: "which", Value: "current"}}, html.Text("Current")),
			dd(html.Props{Class: "jn-num"}, html.Text(b.Current)),
			dt(html.Props{DataAttr: html.DataAttribute{Name: "which", Value: "proposed"}}, html.Text("Proposed")),
			dd(html.Props{Class: "jn-num"}, html.Text(b.Proposed)),
		),
		htmlIf(b.Note != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-gauge-note"}, html.Text(b.Note))
		}),
	)
}

// budgetMeter draws what the request takes out of the approved envelope.
// Same reasoning as payBandGauge: the width is data, so it is SVG geometry
// rather than a styled div, and the number is repeated in text.
func budgetMeter(b *Budget) ui.Node {
	if b == nil {
		return nil
	}
	const (
		width   = 300.0
		barY    = 6.0
		barH    = 14.0
		padding = 0.0
	)
	used := clampPct(b.UsedPct)
	tone := toneSuccess
	switch {
	case used >= 95:
		tone = toneDanger
	case used >= 80:
		tone = toneWarning
	}
	label := "Approved envelope: " + strconv.FormatFloat(used, 'f', 1, 64) +
		"% committed once this request is included."

	return html.Div(html.Props{Class: "jn-gauge"},
		html.H3(html.Props{Class: "jn-subhead"}, html.Text("Budget impact")),
		html.Svg(html.Props{Class: "jn-meter", DataAttr: html.DataAttribute{Name: "tone", Value: tone},
			Raw: map[string]any{
				"viewBox": "0 0 300 26", "role": "img", "aria-label": label,
				"focusable": "false",
			}},
			html.Rect(html.Props{Class: "jn-meter-track", Raw: map[string]any{
				"x": coord(padding), "y": coord(barY), "width": coord(width), "height": coord(barH), "rx": "7",
			}}),
			html.Rect(html.Props{Class: "jn-meter-fill", Raw: map[string]any{
				"x": coord(padding), "y": coord(barY), "width": coord(width * used / 100), "height": coord(barH), "rx": "7",
			}}),
		),
		dl(html.Props{Class: "jn-gauge-legend"},
			dt(html.Props{}, html.Text("Available")),
			dd(html.Props{Class: "jn-num"}, html.Text(b.Available)),
			dt(html.Props{}, html.Text("Committed")),
			dd(html.Props{Class: "jn-num"}, html.Text(b.Committed)),
			dt(html.Props{}, html.Text("This request")),
			dd(html.Props{Class: "jn-num"}, html.Text(b.Requested)),
		),
		htmlIf(b.Note != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-gauge-note"}, html.Text(b.Note))
		}),
	)
}

// effectiveWindow places the effective date on the cycle, and names the
// as-known-at time every figure above was read at. The second half is not
// decoration: this page is a temporal read, and a reader who does not know
// which instant the answers are from cannot judge them.
func effectiveWindow(w *EffectiveWindow) ui.Node {
	if w == nil {
		return nil
	}
	stop := func(label, value, which string) ui.Node {
		if value == "" {
			return nil
		}
		return html.Li(html.Props{Class: "jn-stop", DataAttr: html.DataAttribute{Name: "which", Value: which}},
			html.Span(html.Props{Class: "jn-stop-dot", Aria: map[string]string{"hidden": "true"}}),
			html.Span(html.Props{Class: "jn-stop-label"}, html.Text(label)),
			html.Span(html.Props{Class: "jn-stop-value"}, html.Text(value)),
		)
	}
	return html.Div(html.Props{Class: "jn-window"},
		html.H3(html.Props{Class: "jn-subhead"}, html.Text("Effective window")),
		html.Ol(html.Props{Class: "jn-strip"},
			stop("Cycle opens", w.Start, "start"),
			stop("Takes effect", w.EffectiveDate, "effective"),
			stop("As known at", w.KnownAt, "known"),
		),
		htmlIf(w.Note != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-gauge-note"}, html.Text(w.Note))
		}),
	)
}

// ----------------------------------------------------------------------
// Workflow
// ----------------------------------------------------------------------

func workflowSection(v DetailView) ui.Node {
	if len(v.Engine) == 0 && len(v.Nodes) == 0 && len(v.WorkItems) == 0 {
		return nil
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "workflow-heading"}, html.Text("Workflow")),
		),
	}
	if len(v.Engine) > 0 {
		children = append(children, html.Div(html.Props{Class: "jn-subsection"},
			html.H3(html.Props{Class: "jn-subhead"}, html.Text("Instance")),
			factsList(v.Engine)))
	}
	if len(v.Nodes) > 0 {
		children = append(children, html.Div(html.Props{Class: "jn-subsection"},
			html.H3(html.Props{Class: "jn-subhead"}, html.Text("Node executions")),
			nodesTable(v.Nodes)))
	}
	if len(v.WorkItems) > 0 {
		children = append(children, html.Div(html.Props{Class: "jn-subsection"},
			html.H3(html.Props{Class: "jn-subhead"}, html.Text("Work items")),
			html.Ul(html.Props{Class: "jn-workitems", Role: "list"}, html.Map(v.WorkItems, workItemCard)...)))
	}
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "workflow-heading"}}, children...)
}

func nodesTable(nodes []NodeRow) ui.Node {
	return html.Div(html.Props{Class: "jn-tablewrap"},
		html.Table(html.Props{Class: "jn-table jn-zebra"},
			html.Caption(html.Props{Class: "jn-visually-hidden"},
				html.Text("Every durable node execution recorded for this workflow instance")),
			html.Thead(html.Props{},
				html.Tr(html.Props{},
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text("Node")),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text("Step type")),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text("Status")),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Class: "jn-num"}, html.Text("Attempt")),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text("Started")),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text("Completed")),
				),
			),
			html.Tbody(html.Props{}, html.Map(nodes, func(n NodeRow) ui.Node {
				return html.Tr(html.Props{},
					html.Th(html.Props{Raw: map[string]any{"scope": "row"}, Class: "jn-mono"}, html.Text(n.NodeID)),
					html.Td(html.Props{}, html.Text(n.StepType)),
					html.Td(html.Props{}, chip(n.Tone, n.Status)),
					html.Td(html.Props{Class: "jn-num"}, html.Text(n.Attempt)),
					html.Td(html.Props{Class: "jn-num"}, html.Text(n.Started)),
					html.Td(html.Props{Class: "jn-num"}, html.Text(n.Completed)),
				)
			})...),
		),
	)
}

func workItemCard(w WorkItemCard) ui.Node {
	lines := make([]ui.Node, 0, 6)
	line := func(label, value string, mono bool) {
		if value == "" {
			return
		}
		class := "jn-workitem-line"
		if mono {
			class += " jn-mono"
		}
		lines = append(lines, html.Span(html.Props{Class: class},
			html.Span(html.Props{Class: "jn-meta-key"}, html.Text(label+" ")),
			html.Text(value)))
	}
	line("Owner", w.Owner, false)
	line("Node", w.NodeID, true)
	line("Deadline", w.Deadline, false)
	line("Claimed", w.Claimed, false)
	line("Completed", w.Completed, false)
	line("Id", w.ID, true)
	return html.Li(html.Props{Class: "jn-workitem"},
		html.Div(html.Props{Class: "jn-workitem-top"},
			html.P(html.Props{Class: "jn-workitem-kind"},
				iconClock("jn-workitem-icon"), html.Text(w.Kind)),
			chip(w.Tone, w.Status),
		),
		html.P(html.Props{Class: "jn-workitem-lines"}, lines...),
	)
}

func outcomeSection(l *LedgerCard) ui.Node {
	var body ui.Node
	if l == nil {
		body = html.P(html.Props{Class: "jn-quiet"},
			iconClock("jn-quiet-icon"),
			html.Text("This promotion has not been recorded yet. Required approvals, the effective date and final checks must be complete first."))
	} else {
		body = factsList([]Fact{
			{Label: "Stream", Value: l.StreamKey, Mono: true},
			{Label: "Sequence", Value: l.Sequence, Mono: true},
			{Label: "Schema", Value: l.SchemaRef, Mono: true},
			{Label: "Digest", Value: l.Digest, Mono: true},
			{Label: "Idempotency key", Value: l.IdempotencyKey, Mono: true},
			{Label: "Recorded at", Value: l.RecordedAt},
			{Label: "Effective at", Value: l.EffectiveAt, Tone: toneSuccess},
		})
	}
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "outcome-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "outcome-heading"},
				iconLedger("jn-headicon"), html.Text("Recorded outcome")),
			htmlIf(l != nil, func() ui.Node { return chip(toneSuccess, "Recorded") }),
		),
		body,
	)
}

func evidenceSection(evidence []string) ui.Node {
	if len(evidence) == 0 {
		return nil
	}
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "evidence-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "evidence-heading"}, html.Text("Evidence")),
			html.P(html.Props{Class: "jn-count"}, html.Text(evidenceCountLabel(len(evidence)))),
		),
		html.Ul(html.Props{Class: "jn-evidence", Role: "list"}, html.Map(evidence, func(e string) ui.Node {
			return html.Li(html.Props{}, html.Text(e))
		})...),
	)
}

func evidenceCountLabel(n int) string {
	if n == 1 {
		return "1 record"
	}
	return strconv.Itoa(n) + " records"
}

// ----------------------------------------------------------------------
// Right rail: actions and timeline
// ----------------------------------------------------------------------

func actionsSection(l live, actions []Action) ui.Node {
	if len(actions) == 0 {
		return nil
	}
	return html.Section(html.Props{Aria: map[string]string{"labelledby": "actions-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "actions-heading"}, html.Text("Actions")),
		),
		html.Div(html.Props{Class: "jn-actions"}, html.Map(actions, func(a Action) ui.Node {
			return actionCard(l, a)
		})...),
	)
}

// actionCard renders one governed operation as its own self-contained
// form. Nothing is shared between actions: each carries its own hidden
// inputs and its own callback, so a page that shows three actions offers
// three independent submissions and no ambient state decides which one
// fires.
func actionCard(l live, a Action) ui.Node {
	variant := a.Variant
	switch variant {
	case "primary", "secondary", "danger":
	default:
		variant = "secondary"
	}
	reasonID := "action-" + a.ID + "-blocked"

	children := []ui.Node{
		html.H3(html.Props{}, html.Text(a.Label)),
	}
	if a.Description != "" {
		children = append(children, html.P(html.Props{Class: "jn-action-desc"}, html.Text(a.Description)))
	}
	if a.ActsAs != "" {
		label := a.ActsAsLabel
		if strings.TrimSpace(label) == "" {
			label = "Acts as " + a.ActsAs
		}
		children = append(children, html.P(html.Props{Class: "jn-actsas"},
			iconPerson("jn-actsas-icon"),
			html.Text(label)))
	}
	children = append(children, html.Fragment(hiddenInputs(a.Hidden)...))
	for _, f := range a.Fields {
		children = append(children, fieldNode(l, f, a.Disabled))
	}

	btn := html.Props{Class: "jn-btn", Type: submitButtonType(a.OnSubmit),
		DataAttr: html.DataAttribute{Name: "variant", Value: variant}}
	if a.Disabled {
		btn.Disabled = true
		btn.Aria = map[string]string{"describedby": reasonID}
	} else if a.OnSubmit != nil {
		btn.OnClick = clickHandler(a.OnSubmit, l.collect(a.Hidden, a.Fields))
	}
	submit := html.Button(btn, html.Text(a.Label))
	if !a.Disabled && (len(a.Confirmation) > 0 || a.ConfirmationNote != "") {
		confirmChildren := []ui.Node{
			html.P(html.Props{Class: "jn-confirm-title"}, html.Text("Confirm "+strings.ToLower(a.Label))),
		}
		if len(a.Confirmation) > 0 {
			confirmChildren = append(confirmChildren, factsListWithClass(a.Confirmation, "jn-confirm-facts"))
		}
		if a.ConfirmationNote != "" {
			confirmChildren = append(confirmChildren,
				html.P(html.Props{Class: "jn-confirm-note"}, iconWarning("jn-confirm-icon"), html.Text(a.ConfirmationNote)))
		}
		confirmChildren = append(confirmChildren, submit)
		children = append(children, html.Details(html.Props{Class: "jn-confirm"},
			html.Summary(html.Props{Class: "jn-btn", DataAttr: html.DataAttribute{Name: "variant", Value: "secondary"}, Raw: map[string]any{"role": "button"}},
				html.Span(html.Props{Class: "jn-confirm-open-label"}, html.Text("Review and "+strings.ToLower(a.Label))),
				html.Span(html.Props{Class: "jn-confirm-close-label"}, html.Text("Cancel review"))),
			html.Div(html.Props{Class: "jn-confirm-body"}, confirmChildren...),
		))
	} else {
		children = append(children, submit)
	}
	if a.Disabled && a.DisabledReason != "" {
		children = append(children, html.P(html.Props{ID: reasonID, Class: "jn-blocked"},
			iconWarning("jn-blocked-icon"), html.Text(a.DisabledReason)))
	}

	props := formProps(l, a.Action, a.OnSubmit, a.Hidden, a.Fields)
	props.Class = "jn-card jn-action"
	props.DataAttr = html.DataAttribute{Name: "variant", Value: variant}
	return html.Form(props, children...)
}

func timelineSection(events []TimelineEvent) ui.Node {
	if len(events) == 0 {
		return nil
	}
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "timeline-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "timeline-heading"}, html.Text("History")),
		),
		html.Ol(html.Props{Class: "jn-timeline"}, html.Map(events, timelineNode)...),
	)
}

func timelineNode(e TimelineEvent) ui.Node {
	tone := toneOf(e.Tone)
	return html.Li(html.Props{Class: "jn-tl", DataAttr: html.DataAttribute{Name: "tone", Value: tone}},
		html.Span(html.Props{Class: "jn-tldot", Aria: map[string]string{"hidden": "true"}}),
		html.Div(html.Props{Class: "jn-tlbody"},
			html.P(html.Props{Class: "jn-tlat"}, html.Text(e.At)),
			html.P(html.Props{Class: "jn-tltitle"}, html.Text(e.Title)),
			htmlIf(e.Actor != "", func() ui.Node {
				return html.P(html.Props{Class: "jn-tldetail"}, html.Text("by "+e.Actor))
			}),
			htmlIf(e.Detail != "", func() ui.Node {
				return html.P(html.Props{Class: "jn-tldetail"}, html.Text(e.Detail))
			}),
			htmlIf(e.Ref != "", func() ui.Node {
				return html.P(html.Props{Class: "jn-tldetail jn-mono"}, html.Text(e.Ref))
			}),
		),
	)
}
