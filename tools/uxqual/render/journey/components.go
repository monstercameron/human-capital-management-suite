package journey

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/positionpicker"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/uicomponents"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
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
	severityBlocking  = "blocking"
	severityWarning   = "warning"
	severityNeedsData = "needs-data"
	severityInfo      = "info"
	severitySuccess   = "success"
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
	// fieldKindPositionPicker is UXLIVE-011's governed position choice. It
	// is not an <input> with a different type: the whole control is
	// productui.PositionPicker, the accessible fieldset PROMOUX-004 built,
	// so this renderer does not draw a second version of it.
	fieldKindPositionPicker = "positionpicker"
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
	case severityBlocking, severityWarning, severityNeedsData, severitySuccess:
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
	case severityWarning, severityNeedsData:
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
	return stepStateWordLocale("en-US", state)
}

func stepStateWordLocale(locale, state string) string {
	copy := productui.ResolveProductLocale(locale)
	switch state {
	case stepDone:
		return copy.Text("journey.step_state_done")
	case stepActive:
		return copy.Text("journey.step_state_active")
	case stepFailed:
		return copy.Text("journey.step_state_failed")
	default:
		return copy.Text("journey.step_state_upcoming")
	}
}

// severityWord is the same idea for a finding: the severity is spelled out
// next to the message rather than encoded in the border color.
func severityWord(severity string) string {
	return severityWordLocale("en-US", severity)
}

func severityWordLocale(locale, severity string) string {
	copy := productui.ResolveProductLocale(locale)
	switch severity {
	case severityBlocking:
		return copy.Text("journey.finding_blocking")
	case severityWarning:
		return copy.Text("journey.finding_warning")
	case severityNeedsData:
		return copy.Text("journey.finding_needs_data")
	case severitySuccess:
		return copy.Text("journey.finding_passed")
	default:
		return copy.Text("journey.finding_information")
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
	locale        string
	// sharedBlocked is the one reason every action in the current section
	// is refused for, when they all give the same one. An action card that
	// sees it points at the section's single statement instead of repeating
	// it (UXLIVE-017).
	sharedBlocked string
}

func liveOf(p Page) live {
	return live{values: p.Values, onFieldChange: p.OnFieldChange, locale: p.Locale}
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
		html.Span(html.Props{Class: class, Dir: "auto"}, html.Text(value)),
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
				html.Span(html.Props{Class: "jn-markwell"}, RenderIcon(IconBrandMark, "", nil)),
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
func noticeRegion(p Page) ui.Node {
	region := html.Props{Class: "jn-live", Role: "status", Aria: map[string]string{"live": "polite"}}
	n, locale := p.Notice, p.Locale
	if n == nil {
		return html.Div(region)
	}
	invalid := invalidProposalFields(p)
	links := make([]ui.Node, 0, len(invalid))
	for _, field := range invalid {
		id := field.ID
		var focus func()
		if p.OnFocusField != nil {
			focus = func() { p.OnFocusField(id) }
		}
		links = append(links, html.Li(html.Props{}, html.A(html.Props{Href: "#" + id, OnClick: activate(focus)}, html.Text(field.Label))))
	}
	tone := toneOf(n.Tone)
	return html.Div(region,
		html.Div(html.Props{Class: "jn-noticeband"},
			html.Div(html.Props{Class: "jn-shell"},
				html.Div(html.Props{Class: "jn-notice", DataAttr: html.DataAttribute{Name: "tone", Value: tone}},
					iconForTone(tone, "jn-notice-icon"),
					html.Div(html.Props{Class: "jn-notice-body"},
						html.P(html.Props{Class: "jn-notice-title"},
							visuallyHidden(severityLabelForToneLocale(tone, locale)+": "),
							html.Text(n.Title)),
						htmlIf(n.Detail != "", func() ui.Node {
							return html.P(html.Props{Class: "jn-notice-detail"}, html.Text(n.Detail))
						}),
						htmlIf(len(links) > 0, func() ui.Node {
							return html.Nav(html.Props{Class: "jn-notice-fields", Aria: map[string]string{"label": productui.ResolveProductLocale(locale).Text("journey.invalid_fields")}},
								html.P(html.Props{Class: "jn-notice-fields-title"}, html.Text(productui.ResolveProductLocale(locale).Text("journey.invalid_fields"))),
								html.Ul(html.Props{Class: "jn-notice-fieldlist"}, links...))
						}),
						htmlIf(n.SupportReference != "", func() ui.Node {
							return noticeSupportDetails(locale, n.SupportReference)
						}),
					),
				),
			),
		),
	)
}

func noticeSupportDetails(locale, reference string) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	return html.Details(html.Props{Class: "jn-notice-support"},
		html.Summary(html.Props{}, html.Text(copy.Text("journey.support_details"))),
		html.Div(html.Props{Class: "jn-notice-support-body"},
			html.Label(html.Props{For: "jn-support-reference"}, html.Text(copy.Text("journey.support_reference"))),
			html.Input(html.Props{ID: "jn-support-reference", Class: "jn-support-reference", Type: "text", Value: reference, ReadOnly: true}),
			html.P(html.Props{}, html.Text(copy.Text("journey.support_reference_help"))),
		),
	)
}

func invalidProposalFields(p Page) []Field {
	var fields []Field
	if p.Proposal != nil {
		fields = p.Proposal.Form.Fields
	} else if p.List != nil {
		fields = p.List.Form.Fields
	}
	invalid := make([]Field, 0)
	for _, field := range fields {
		if field.Error != "" && field.ID != "" {
			invalid = append(invalid, field)
		}
	}
	return invalid
}

func severityLabelForTone(tone string) string {
	return severityLabelForToneLocale(tone, "")
}

func severityLabelForToneLocale(tone, locale string) string {
	copy := productui.ResolveProductLocale(locale)
	switch tone {
	case toneSuccess:
		return copy.Text("journey.severity_success")
	case toneWarning:
		return copy.Text("journey.severity_warning")
	case toneDanger:
		return copy.Text("journey.severity_error")
	default:
		return copy.Text("journey.severity_info")
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

// proposalHeadingID is proposalView's own heading id. It is kept, and stays
// script-focusable (see pageHeader), for exactly the reason it was added:
// naming page context for a screen reader on a route change is correct.
// It is deliberately NOT the mount-focus target below -- live measurement
// showed the heading already sits inside the viewport at the moment Start
// lands, so focusing it cannot also scroll anything into view, and RED's
// below-the-fold clause needs an element that genuinely starts off-screen.
//
// proposeReviewID is the id proposalFormSection gives its own reviewSurface
// for the Start path (see reviewSurfaceDetailsID/reviewSurfaceTriggerID in
// review_surface.go); proposeReviewTriggerID derives the trigger id through
// those same functions rather than re-deriving the "-review-trigger" suffix
// by hand, so the two call sites cannot drift apart.
const (
	proposalHeadingID = "proposal-heading"
	proposeReviewID   = "propose"
)

var proposeReviewTriggerID = reviewSurfaceTriggerID(proposeReviewID)

func proposalView(p Page, v ProposalView) ui.Node {
	copy := productui.ResolveProductLocale(p.Locale)
	name := copy.Text("journey.employee_label")
	if v.Subject != nil && strings.TrimSpace(v.Subject.Name) != "" {
		name = v.Subject.Name
	}
	return html.Div(html.Props{Class: "jn-stack jn-proposal-view"},
		pageHeader(pageHeaderProps{
			HeadingID: proposalHeadingID,
			Class:     "jn-proposal-head", Eyebrow: copy.Text("journey.promotion_eyebrow"), Title: copy.Text("journey.promote_person", map[string]string{"name": name}),
			Lead: copy.Text("journey.promotion_lead"),
			Actions: []ui.Node{
				htmlIf(v.BackHref != "", func() ui.Node {
					return html.A(html.Props{Class: "jn-context-link", Href: v.BackHref, OnClick: activate(v.BackNavigate)}, html.Text(copy.Text("journey.profile_link", map[string]string{"name": name})))
				}),
				htmlIf(v.JourneysLink.Href != "", func() ui.Node {
					return html.A(html.Props{Class: "jn-context-link", Href: v.JourneysLink.Href, OnClick: activate(v.JourneysLink.OnNavigate)}, html.Text(copy.Text("journey.all_link")))
				}),
			},
		}),
		htmlIf(v.Loading, func() ui.Node {
			return loadingPanel(copy.Text("journey.loading_employee_title"), copy.Text("journey.loading_employee_detail"))
		}),
		htmlIf(!v.Loading, func() ui.Node { return promotionSubjectCard(p.Locale, v.Subject) }),
		htmlIf(!v.Loading, func() ui.Node { return engineUnavailableCallout(p.Locale, v.EngineAvailable, v.EngineNotice) }),
		htmlIf(!v.Loading, func() ui.Node { return proposalFormSection(liveOf(p), v.Form, copy.Text("journey.form_heading")) }),
		// focusOnMount is a real child element (ui.CreateElement), not a
		// plain nested call, so its effect gets its own fiber isolated
		// from LiveComponent's -- see mount_focus.go's own doc comment for
		// why that distinction is what makes this reliable across a
		// Page.List<->Page.Proposal transition. It targets the review
		// surface's own trigger, not the heading above: that is the
		// element live measurement found genuinely below the fold, and a
		// programmatic focus scrolls its target into view as a browser
		// side effect, closing that clause and the lost-focus one
		// together. It is placed alongside proposalFormSection under the
		// same !v.Loading gate rather than unconditionally at the top: the
		// trigger this targets does not exist in the DOM until Loading has
		// cleared, and this component's effect fires on its OWN first
		// appearance in the tree, not on proposalView's -- mounting it
		// unconditionally would fire (and permanently spend, since its
		// dependency is a compile-time constant) that one mount-effect
		// during the loading placeholder, before there is anything to
		// focus.
		htmlIf(!v.Loading, func() ui.Node {
			return ui.CreateElement(focusOnMount, focusOnMountProps{TargetID: proposeReviewTriggerID})
		}),
	)
}

func promotionSubjectCard(locale string, s *PromotionSubject) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	if s == nil {
		return html.Section(html.Props{Class: "jn-panel jn-subject-card", Aria: map[string]string{"labelledby": "subject-heading"}},
			html.H2(html.Props{ID: "subject-heading"}, html.Text(copy.Text("journey.subject_unavailable_title"))),
			html.P(html.Props{Class: "jn-muted"}, html.Text(copy.Text("journey.subject_unavailable_detail"))),
		)
	}
	return html.Section(html.Props{Class: "jn-panel jn-subject-card", Aria: map[string]string{"labelledby": "subject-heading"}},
		html.Div(html.Props{Class: "jn-subject-identity"},
			uicomponents.Avatar(uicomponents.AvatarProps{Name: s.Name, PhotoURL: s.PhotoURL, Class: "jn-subject-avatar", Decorative: true}),
			html.Div(html.Props{},
				html.P(html.Props{Class: "jn-eyebrow"}, html.Text(copy.Text("journey.employee_label"))),
				html.H2(html.Props{ID: "subject-heading"}, html.Text(s.Name)),
				htmlIf(s.Title != "", func() ui.Node { return html.P(html.Props{Class: "jn-subject-title"}, html.Text(s.Title)) }),
			),
			chip(toneInfo, copy.Text("journey.employee_verified")),
		),
		factsListWithClass([]Fact{
			{Label: copy.Text("journey.worker_number"), Value: valueOrDash(s.Number)},
			{Label: copy.Text("journey.current_job"), Value: valueOrDash(joinNonEmpty(" · ", s.Title, s.Grade))},
			{Label: copy.Text("journey.organization"), Value: valueOrDash(s.OrgUnit)},
			{Label: copy.Text("journey.location"), Value: valueOrDash(s.Location)},
			{Label: copy.Text("journey.current_base"), Value: valueOrDash(s.PayLine)},
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
	copy := productui.ResolveProductLocale(p.Locale)
	return html.Div(html.Props{Class: "jn-stack"},
		pageHeader(pageHeaderProps{Eyebrow: copy.Text("journey.list_eyebrow"), Title: copy.Text("journey.list_title"), Lead: copy.Text("journey.list_lead")}),
		peopleSection(v.People),
		htmlIf(v.People != nil, func() ui.Node { return newEmployeeSection(l, v.People.Form) }),
		engineUnavailableCallout(p.Locale, v.EngineAvailable, v.EngineNotice),
		proposalSection(l, v),
		journeysSection(p.Locale, v),
	)
}

// embeddedListView gives the product route a workflow-first information
// architecture. The standalone operational surface remains workforce-first,
// while the integrated product already has a dedicated People module and
// therefore leads with the records and actions readers came here to use.
func embeddedListView(p Page, v ListView) ui.Node {
	copy := productui.ResolveProductLocale(p.Locale)
	if len(v.Journeys) == 0 {
		v.Empty = copy.Text("journey.list_empty")
	}
	return html.Div(html.Props{Class: "jn-stack"},
		pageHeader(pageHeaderProps{Eyebrow: copy.Text("journey.list_eyebrow"), Title: copy.Text("journey.list_title"), Lead: copy.Text("journey.list_lead"),
			Actions: []ui.Node{htmlIf(v.People != nil && v.People.DirectoryLink.Href != "", func() ui.Node {
				link := v.People.DirectoryLink
				return html.A(html.Props{Class: "jn-btn", Href: link.Href, OnClick: activate(link.OnNavigate)}, html.Text(copy.Text("journey.list_choose_employee")))
			})},
		}),
		journeysSection(p.Locale, v),
		engineUnavailableCallout(p.Locale, v.EngineAvailable, v.EngineNotice),
	)
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

func journeysSection(locale string, v ListView) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	body := ui.Node(nil)
	switch {
	case len(v.Groups) > 0:
		body = html.Div(html.Props{Class: "jn-journey-groups"}, journeySubjectGroupSections(locale, v.Groups)...)
	case len(v.Journeys) == 0:
		body = emptyStateLocale(locale, v.Empty)
	case hasLifecycleGroups(v.Journeys):
		sections := make([]ui.Node, 0, 5)
		for _, group := range groupedJourneys(v.Journeys) {
			rows := html.Map(group.cards, func(j JourneyCard) ui.Node {
				return html.Li(html.Props{Class: "jn-griditem"}, journeyCardLocale(locale, j))
			})
			sections = append(sections, html.Section(html.Props{Class: "jn-journey-group", Aria: map[string]string{"labelledby": "journeys-group-" + group.id}},
				html.Div(html.Props{Class: "jn-sectionhead jn-sectionhead-inline"},
					html.H3(html.Props{ID: "journeys-group-" + group.id}, html.Text(copy.Text("journey.group."+group.id))),
					chip(toneNeutral, countLabelLocale(locale, len(group.cards))),
				),
				html.Ul(html.Props{Class: "jn-grid", Role: "list"}, rows...),
			))
		}
		body = html.Div(html.Props{Class: "jn-journey-groups"}, sections...)
	default:
		body = html.Ul(html.Props{Class: "jn-grid", Role: "list"},
			html.Map(v.Journeys, func(j JourneyCard) ui.Node {
				return html.Li(html.Props{Class: "jn-griditem"}, journeyCardLocale(locale, j))
			})...)
	}
	return html.Section(html.Props{Aria: map[string]string{"labelledby": "journeys-heading"}},
		// A heading and the count of what is under it are one statement.
		// Pushed to opposite edges of a wide page they read as two.
		html.Div(html.Props{Class: "jn-sectionhead jn-sectionhead-inline"},
			html.H2(html.Props{ID: "journeys-heading"}, html.Text(copy.Text("journey.section_title"))),
			chip(toneNeutral, countLabelLocale(locale, len(v.Journeys))),
		),
		body,
	)
}

func hasLifecycleGroups(cards []JourneyCard) bool {
	for _, card := range cards {
		if card.Group != "" {
			return true
		}
	}
	return false
}

// journeySubjectGroupSections renders one accessible section per subject
// group: a heading naming the subject, the group's distinct statuses (the
// "and status" half of GREEN, visible across the group even when a reader
// does not open every card in it), and the subject's own journeys, each
// still carrying its own per-card status chip (the "within" half).
func journeySubjectGroupSections(locale string, groups []JourneySubjectGroup) []ui.Node {
	sections := make([]ui.Node, 0, len(groups))
	for index, group := range groups {
		sections = append(sections, journeySubjectGroupSection(locale, group, index))
	}
	return sections
}

func journeySubjectGroupSection(locale string, group JourneySubjectGroup, index int) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	headingID := "journey-group-" + strconv.Itoa(index) + "-heading"
	// The group's statuses summarize the cards inside it. With exactly one
	// card there is nothing to summarize: the chip repeats, word for word and
	// a few pixels away, the chip the card already carries, and a screen
	// reader hears the same status twice for one request. The summary is
	// therefore omitted rather than hidden, so the two agree about what is
	// on the page.
	statusChips := make([]ui.Node, 0, len(group.Statuses))
	if len(group.Journeys) > 1 {
		for _, status := range group.Statuses {
			statusChips = append(statusChips, chip(status.Tone, status.Label))
		}
	}
	return html.Section(html.Props{Class: "jn-journey-group", Aria: map[string]string{"labelledby": headingID}},
		html.Div(html.Props{Class: "jn-journey-group-head"},
			html.H3(html.Props{ID: headingID, Class: "jn-journey-group-subject"}, html.Text(group.Subject)),
			chip(toneNeutral, countLabelLocale(locale, len(group.Journeys))),
			htmlIf(len(statusChips) > 0, func() ui.Node {
				// The region is named after the subject it belongs to: with
				// several groups on the page, "Statuses in this group" gives a
				// screen reader no way to tell one from the next.
				return html.Div(html.Props{Class: "jn-journey-group-statuses",
					Aria: map[string]string{"label": copy.Text("journey.group_statuses", map[string]string{"name": group.Subject})}},
					statusChips...)
			}),
		),
		html.Ul(html.Props{Class: "jn-grid jn-journey-group-list", Role: "list"},
			html.Map(group.Journeys, func(j JourneyCard) ui.Node {
				return html.Li(html.Props{Class: "jn-griditem"}, journeyCardLocale(locale, j))
			})...),
	)
}

type journeyGroupBlock struct {
	id    string
	cards []JourneyCard
}

// Grouping preserves the service order within each lifecycle bucket; it
// changes only presentation, never the authorized population or status truth.
func groupedJourneys(cards []JourneyCard) []journeyGroupBlock {
	order := []JourneyGroup{JourneyGroupReview, JourneyGroupWaiting, JourneyGroupIssue, JourneyGroupClosed, ""}
	buckets := make(map[JourneyGroup][]JourneyCard, len(order))
	for _, card := range cards {
		group := card.Group
		switch group {
		case JourneyGroupReview, JourneyGroupWaiting, JourneyGroupIssue, JourneyGroupClosed:
		default:
			group = ""
		}
		buckets[group] = append(buckets[group], card)
	}
	result := make([]journeyGroupBlock, 0, len(order))
	for _, group := range order {
		if len(buckets[group]) > 0 {
			id := string(group)
			if id == "" {
				id = "other"
			}
			result = append(result, journeyGroupBlock{id: id, cards: buckets[group]})
		}
	}
	return result
}

func countLabelLocale(locale string, n int) string {
	copy := productui.ResolveProductLocale(locale)
	if n == 1 {
		return copy.Text("journey.count_one")
	}
	return copy.Text("journey.count_many", map[string]string{"count": strconv.Itoa(n)})
}

func journeyCard(j JourneyCard) ui.Node {
	return journeyCardLocale("", j)
}

// JourneyReference is a short, stable label for one request, derived from
// its intent id. A tracker that lists several requests for the same person
// with the same role change and near-identical pay gave a reader nothing to
// tell them apart and gave a screen reader several identical headings
// (UXLIVE-010). It is a handle, not an identity: the full id stays in the
// authorized diagnostics disclosure.
func JourneyReference(intentID string) string {
	trimmed := strings.TrimSpace(intentID)
	if trimmed == "" {
		return ""
	}
	compact := strings.ReplaceAll(trimmed, "-", "")
	if len(compact) > 6 {
		compact = compact[len(compact)-6:]
	}
	return strings.ToUpper(compact)
}

func journeyCardLocale(locale string, j JourneyCard) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	return html.Article(html.Props{Class: "jn-card jn-journey",
		DataAttr: html.DataAttribute{Name: "stage", Value: j.Stage}},
		html.Div(html.Props{Class: "jn-journey-top"},
			html.H3(html.Props{},
				html.A(html.Props{Href: j.Href, OnClick: activate(j.OnOpen)},
					html.Text(j.WorkerName),
					htmlIf(JourneyReference(j.IntentID) != "", func() ui.Node {
						return html.Span(html.Props{Class: "jn-journey-ref"}, html.Text(JourneyReference(j.IntentID)))
					}),
					visuallyHidden(" — "+copy.Text("journey.open_request")),
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
			metaItem(copy.Text("journey.effective"), j.EffectiveDate, false),
			metaItem(copy.Text("journey.updated"), j.Updated, false),
		),
		htmlIf(j.NextStep != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-journey-next"}, metaItem(copy.Text("journey.next_step"), j.NextStep, false))
		}),
		technicalDetailsSection(locale, j.DiagnosticsAuthorized, []technicalDetail{
			{Label: "Worker", Value: j.WorkerRef},
			{Label: "Instance", Value: j.InstanceID},
		}),
		html.P(html.Props{Class: "jn-journey-foot", Aria: map[string]string{"hidden": "true"}},
			html.Text(copy.Text("journey.open_request")), RenderIcon(IconArrowRight, "jn-journey-arrow", nil),
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
func technicalDetailsSection(locale string, authorized bool, items []technicalDetail) ui.Node {
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
				Type:  "button",
				Class: "jn-copy-btn",
				Aria: map[string]string{"label": productui.ResolveProductLocale(locale).
					Text("journey.copy_value", map[string]string{"field": item.Label})},
				OnClick: activate(func() { copyToClipboard(value) }),
			}, html.Text("Copy")),
		))
	}
	if len(rows) == 0 {
		return nil
	}
	return html.Details(html.Props{Class: "jn-journey-technical"},
		html.Summary(html.Props{}, html.Text(productui.ResolveProductLocale(locale).Text("journey.technical_details"))),
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

// journeysEmptyTitle is the Journeys lifecycle tracker's empty-state heading
// (UXAUDIT-017): it names the tracking task, never a generic "nothing here".
const journeysEmptyTitle = "No journeys to track"

func emptyStateLocale(locale, message string) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	if message == "" {
		message = copy.Text("journey.list_empty")
	}
	return html.Div(html.Props{Class: "jn-panel jn-empty"},
		RenderIcon(IconEmpty, "jn-empty-mark", nil),
		html.P(html.Props{Class: "jn-empty-title"}, html.Text(copy.Text("journey.empty_title"))),
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
	copy := productui.ResolveProductLocale(l.locale)
	fields := make([]ui.Node, 0, len(f.Fields))
	for _, field := range f.Fields {
		fields = append(fields, fieldNode(l, field, f.Disabled))
	}
	submit := f.Submit
	if submit == "" {
		submit = "Propose promotion"
	}

	busy := !f.Disabled && f.Busy
	btn := html.Props{Class: "jn-btn", Type: submitButtonType(f.OnSubmit),
		DataAttr: html.DataAttribute{Name: "variant", Value: "primary"}}
	if f.Disabled {
		btn.Disabled = true
		btn.Aria = map[string]string{"describedby": "proposal-disabled"}
	} else if busy {
		btn.Disabled = true
		btn.Aria = map[string]string{"busy": "true"}
	} else if f.OnSubmit != nil {
		btn.OnClick = clickHandler(f.OnSubmit, l.collect(f.Hidden, f.Fields))
	}
	submitBtn := html.Button(btn, html.Text(submit))
	foot := []ui.Node{}
	switch {
	case f.Disabled:
		foot = append(foot, submitBtn)
	case len(f.Confirmation) > 0 || f.ConfirmationNote != "":
		// PROMOUX-010: Start's own final action goes through the same
		// shared review surface as Approve and Reject, so it keeps the
		// same compact, contained, keyboard-stable confirmation instead of
		// submitting straight from the input fields.
		triggerLabel := submit
		confirmHeading := copy.Text("journey.action_confirm_generic", map[string]string{"action": strings.ToLower(submit)})
		finalBtn := submitBtn
		if !strings.EqualFold(strings.TrimSpace(submit), strings.TrimSpace(copy.Text("journey.form_submit"))) {
			triggerLabel = copy.Text("journey.action_review_generic", map[string]string{"action": strings.ToLower(submit)})
		} else {
			// The trigger already says "Review and submit". Inside the review
			// the heading and the final button repeated it ("Confirm review
			// and submit" over a second "Review and submit"), so the button
			// that actually sends the proposal did not say it sends it.
			confirmHeading = copy.Text("journey.form_confirm_title")
			finalBtn = html.Button(btn, html.Text(copy.Text("journey.form_submit_final")))
		}
		// The review trigger is this form's primary action: the page exists to
		// propose, and nothing else on it is primary. Left at the surface's
		// secondary default it read as optional, an outlined button at the
		// foot of a long form.
		foot = append(foot, ui.CreateElement(reviewSurface, reviewSurfaceProps{
			ID:             proposeReviewID,
			TriggerVariant: "primary",
			TriggerLabel:   triggerLabel,
			Heading:        confirmHeading,
			Facts:          f.Confirmation,
			Note:           nonEmpty(f.ConfirmationNote, copy.Text("journey.form_submit_help")),
			Submit:         finalBtn,
			Busy:           busy,
			BusyLabel:      f.BusyLabel,
			DismissLabel:   copy.Text("journey.action_cancel_review"),
			CancelLabel:    copy.Text("journey.action_cancel"),
		}))
	default:
		foot = append(foot, submitBtn,
			html.P(html.Props{Class: "jn-help"},
				html.Text(copy.Text("journey.form_submit_help"))))
	}

	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "propose-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "propose-heading"}, html.Text(heading)),
		),
		html.Form(proposalFormProps(l, f),
			htmlIf(f.Disabled, func() ui.Node {
				return html.P(html.Props{ID: "proposal-disabled", Class: "jn-blocked", Raw: map[string]any{"role": "status"}},
					RenderIcon(IconWarning, "jn-blocked-icon", nil), html.Text(f.DisabledReason))
			}),
			html.Fragment(hiddenInputs(f.Hidden)...),
			html.Div(html.Props{Class: "jn-fieldgrid"}, fields...),
			html.Div(html.Props{Class: "jn-formfoot"}, foot...),
		),
	)
}

// Live callbacks own submission; the native fallback retains a real form submit.
func submitButtonType(onSubmit func(map[string]string)) string {
	if onSubmit != nil {
		return "button"
	}
	return "submit"
}

// The enhanced form has localized, field-linked validation in its client.
// Plain POST keeps native constraint validation as a progressive fallback.
func proposalFormProps(l live, f ProposalForm) html.Props {
	props := formProps(l, f.Action, f.OnSubmit, f.Hidden, f.Fields)
	if f.OnSubmit != nil {
		// GWC applies booleans as DOM properties on live mounts. The DOM
		// property is camel-cased even though the serialized attribute is not.
		props.Raw = map[string]any{"noValidate": true}
	}
	return props
}

// formProps builds a <form>'s props for both render paths. Live forms retain
// the projected method and action as semantic HTML and add a submit handler
// that keeps the interaction in the Go/WASM client. This journey renderer is
// mounted only after that client boots; the server shell owns its separate
// no-script fallback and never presents these actions as a working POST.
func formProps(l live, action string, onSubmit func(map[string]string), hidden map[string]string, fields []Field) html.Props {
	props := html.Props{Method: "post", Action: action}
	if onSubmit != nil {
		props.OnSubmit = l.submitHandler(onSubmit, hidden, fields)
	}
	return props
}

// ----------------------------------------------------------------------
// Form fields
// ----------------------------------------------------------------------

// positionPickerField renders UXLIVE-011's governed position choice through
// productui.PositionPicker -- the same fieldset/legend/radio control
// PROMOUX-004 built, with each option's vacancy window and reservation state
// tied to its input by aria-describedby.
//
// It returns the picker in place of the label/input/help stack every other
// field uses, because the picker already carries its own legend: nesting it
// inside a <label for> would name a group after a control that does not
// exist. Help and error still render beneath it, in the same reading order
// and with the same classes, so the error summary's anchors keep working.
//
// An empty Vacancies list is rendered, deliberately, as the picker's own
// empty state rather than as a text box. The whole point of this todo is
// that there is no free-text path back: a form that cannot offer a position
// says so.
func positionPickerField(l live, f Field) ui.Node {
	copy := productui.ResolveProductLocale(l.locale)
	options := make([]productui.PositionPickerOptionProps, 0, len(f.Vacancies))
	for _, vacancy := range f.Vacancies {
		options = append(options, productui.PositionPickerOptionProps{
			Reference:        vacancy.Reference,
			Title:            vacancy.Title,
			Organization:     vacancy.Organization,
			Manager:          vacancy.Manager,
			Location:         vacancy.Location,
			VacancyWindow:    vacancyWindowText(copy, vacancy.VacancyEndISO),
			ReservationState: vacancyReservationText(copy, vacancy.ReservationState),
		})
	}
	props := productui.PositionPickerProps{
		Name:        f.Name,
		Legend:      f.Label,
		Options:     options,
		Selected:    l.value(f),
		EmptyTitle:  f.EmptyTitle,
		EmptyDetail: f.EmptyDetail,
	}
	if l.controlled() {
		id, change := f.ID, l.onFieldChange
		props.OnSelect = func(reference string) { change(id, reference) }
	}

	children := []ui.Node{productui.PositionPicker(props)}
	if f.Help != "" {
		children = append(children, html.P(html.Props{ID: f.ID + "-help", Class: "jn-help"}, html.Text(f.Help)))
	}
	if f.Error != "" {
		children = append(children, html.P(html.Props{ID: f.ID + "-error", Class: "jn-error"},
			RenderIcon(IconDanger, "jn-error-icon", nil), visuallyHidden(copy.Text("journey.severity_error")+": "), html.Text(f.Error)))
	}
	props2 := html.Props{Class: "jn-field", Data: map[string]string{"span": "full"}}
	if f.Error != "" {
		props2.Data["invalid"] = "true"
	}
	return html.Div(props2, children...)
}

// vacancyWindowText localizes the disclosed vacancy end. An empty date is
// "open now" rather than an empty line: the absence of a known end is a
// fact about the position, not missing data.
func vacancyWindowText(copy productui.LocaleContext, endISO string) string {
	if strings.TrimSpace(endISO) == "" {
		return copy.Text("position_picker.open_now")
	}
	end, err := time.Parse("2006-01-02", endISO)
	if err != nil {
		return copy.Text("position_picker.open_now")
	}
	return copy.Text("position_picker.open_until", map[string]string{"date": copy.FormatDate(end)})
}

// vacancyReservationText mirrors productui's own exhaustive mapping: any
// state other than the one the domain declares available falls back to the
// non-revealing copy rather than claiming availability it cannot back up.
func vacancyReservationText(copy productui.LocaleContext, state string) string {
	if state == positionpicker.ReservationAvailable {
		return copy.Text("position_picker.reservation_available")
	}
	return copy.Text("position_picker.reservation_unavailable")
}

// fieldNode renders one labelled control. Every control gets: a <label for>
// bound to its id, a spelled-out "(required)" for assistive technology
// beside the visual asterisk, aria-describedby listing its adornments, help
// and error in reading order, and aria-invalid when the engine rejected it.
func fieldNode(l live, f Field, formDisabled bool) ui.Node {
	if f.Kind == fieldKindHidden {
		return html.HiddenInput(f.Name, f.Value)
	}
	if f.Kind == fieldKindPositionPicker {
		return positionPickerField(l, f)
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
			props := html.Props{Value: o.Value, Selected: selected}
			if o.Value == "" {
				// Props.Value omits empty strings. The placeholder needs an
				// actual value="" so the browser does not present the first
				// published choice while the client still holds no answer.
				props.Raw = map[string]any{"value": ""}
			}
			return html.Option(props, html.Text(o.Label))
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
			visuallyHidden(" ("+productui.ResolveProductLocale(l.locale).Text("journey.required")+")"))
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
			RenderIcon(IconDanger, "jn-error-icon", nil), visuallyHidden(productui.ResolveProductLocale(l.locale).Text("journey.severity_error")+": "), html.Text(f.Error)))
	}
	return html.Div(fieldProps, children...)
}

// ----------------------------------------------------------------------
// Detail view
// ----------------------------------------------------------------------

func detailView(p Page, v DetailView) ui.Node {
	if v.Unavailable {
		return html.Div(html.Props{Class: "jn-stack jn-detail-unavailable"}, detailNavigationLocale(p.Locale, v))
	}
	l := liveOf(p)
	return html.Div(html.Props{Class: "jn-stack"},
		detailNavigationLocale(p.Locale, v),
		heroSectionLocale(p.Locale, v.Journey, v.Diagnostics),
		blockedFindingBannerLocale(p.Locale, v),
		actionsSection(l, v.Actions),
		stepperSectionLocale(p.Locale, v.Steps),
		proposalDetailSectionLocale(p.Locale, v),
		html.Div(html.Props{Class: "jn-columns"},
			html.Div(html.Props{Class: "jn-col"},
				preflightSectionLocale(p.Locale, v),
				effectiveDateWaitLocale(p.Locale, v.EffectiveDateWait),
				outcomeSectionLocale(p.Locale, v.Ledger, v.PendingOutcome),
			),
			html.Aside(html.Props{Class: "jn-col jn-rail", Aria: map[string]string{"label": productui.ResolveProductLocale(p.Locale).Text("journey.actions_history")}},
				timelineSectionLocale(p.Locale, v.Timeline),
			),
		),
		diagnosticsSectionLocale(p.Locale, v),
	)
}

// blockedFindingBannerLocale puts the first actual blocking check within
// sight of the status. It makes no claim that this page can edit or resubmit
// the proposal; the checks section below retains complete evidence.
func blockedFindingBannerLocale(locale string, detail DetailView) ui.Node {
	if detail.Journey.Stage != "BLOCKED" {
		return nil
	}
	for _, finding := range detail.Findings {
		if severityOf(finding.Severity) != "blocking" || finding.Message == "" {
			continue
		}
		copy := productui.ResolveProductLocale(locale)
		message := finding.Message
		// Keep the exact engine evidence in Checks and timing. The prominent
		// status summary should use business language for known typed findings.
		switch finding.Code {
		case "promotion.pay_below_band_minimum":
			message = copy.Text("journey.blocked_pay_below_band")
		case "promotion.pay_above_band_maximum":
			message = copy.Text("journey.blocked_pay_above_band")
		}
		return html.Div(html.Props{Class: "jn-notice", Data: map[string]string{"tone": "warning"}},
			RenderIcon(IconWarning, "jn-notice-icon", nil),
			html.Div(html.Props{Class: "jn-notice-body"},
				html.P(html.Props{Class: "jn-notice-title"}, html.Text(copy.Text("journey.stage_blocked"))),
				html.P(html.Props{Class: "jn-notice-detail"}, html.Text(message)),
			),
		)
	}
	return nil
}

// effectiveDateWaitLocale renders only the typed explanation supplied by the
// serving projection. In particular, it does not calculate a countdown or
// expose a manual wake control: production time authority remains in the
// workflow timer.
func effectiveDateWaitLocale(locale string, explanation *wait.EffectiveDateWait) ui.Node {
	if explanation == nil {
		return nil
	}
	rows := []Fact{
		{Label: "Effective instant", Value: explanation.EffectiveInstant.String(), Tone: toneWarning},
		{Label: "Timezone", Value: explanation.Timezone},
		{Label: "Owner", Value: explanation.Owner},
		{Label: "Scheduled action", Value: explanation.ScheduledAction},
		{Label: "Remaining checks", Value: explanation.RemainingChecks},
		{Label: "Notifications", Value: explanation.NotificationBehavior},
		{Label: "Authorized intervention", Value: explanation.AuthorizedIntervention},
	}
	if explanation.ReviewRequired {
		rows = append(rows, Fact{Label: "Review", Value: nonEmpty(explanation.ReviewReason, "Required"), Tone: toneWarning})
	}
	return html.Section(html.Props{Class: "jn-panel jn-wait-explanation", Aria: map[string]string{"labelledby": "wait-explanation-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "wait-explanation-heading"}, html.Text(productui.ResolveProductLocale(locale).Text("journey.outcome_pending_heading"))),
		),
		factsList(rows),
	)
}

func detailNavigationLocale(locale string, v DetailView) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	links := make([]ui.Node, 0, 2)
	if v.BackLink.Href != "" {
		links = append(links, html.A(html.Props{Href: v.BackLink.Href, OnClick: activate(v.BackLink.OnNavigate)},
			html.Text(nonEmpty(v.BackLink.Label, copy.Text("journey.back_employee")))))
	}
	if v.JourneysLink.Href != "" {
		links = append(links, html.A(html.Props{Href: v.JourneysLink.Href, OnClick: activate(v.JourneysLink.OnNavigate)},
			html.Text(nonEmpty(v.JourneysLink.Label, copy.Text("journey.view_all")))))
	}
	if len(links) == 0 {
		return nil
	}
	return html.Nav(html.Props{Class: "jn-context-nav", Aria: map[string]string{"label": copy.Text("journey.context_navigation")}}, links...)
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func heroSection(j JourneyCard, diagnostics bool) ui.Node {
	return heroSectionLocale("en-US", j, diagnostics)
}

func heroSectionLocale(locale string, j JourneyCard, diagnostics bool) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	return html.Section(html.Props{Class: "jn-panel jn-hero", Aria: map[string]string{"labelledby": "journey-heading"}},
		html.Div(html.Props{Class: "jn-hero-top"},
			html.Div(html.Props{},
				html.P(html.Props{Class: "jn-eyebrow"}, html.Text(copy.Text("journey.detail_title"))),
				html.H1(html.Props{ID: "journey-heading", Class: "jn-display"}, html.Text(j.WorkerName)),
				htmlIf(j.Headline != "", func() ui.Node {
					return html.P(html.Props{Class: "jn-lead"}, html.Text(j.Headline))
				}),
			),
			chip(j.StageTone, j.StageLabel),
		),
		htmlIf(j.PayLine != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-hero-pay"},
				html.Span(html.Props{Dir: "ltr"}, html.Text(j.PayLine)))
		}),
		html.P(html.Props{Class: "jn-meta jn-hero-ids"},
			metaItem(copy.Text("journey.effective"), j.EffectiveDate, false),
			metaItem(copy.Text("journey.updated"), j.Updated, false),
		),
		technicalDetailsSection(locale, diagnostics && j.DiagnosticsAuthorized, []technicalDetail{
			{Label: "Worker", Value: j.WorkerRef},
			{Label: "Intent", Value: j.IntentID},
			{Label: "Instance", Value: j.InstanceID},
		}),
	)
}

func stepperSectionLocale(locale string, steps []Step) ui.Node {
	if len(steps) == 0 {
		return nil
	}
	copy := productui.ResolveProductLocale(locale)
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "stages-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "stages-heading"}, html.Text(copy.Text("journey.stages_title"))),
		),
		html.Ol(html.Props{Class: "jn-stepper"}, html.MapIndexed(steps, func(index int, step Step) ui.Node {
			return stepNodeLocale(locale, index, step)
		})...),
	)
}

func stepNode(index int, s Step) ui.Node {
	return stepNodeLocale("en-US", index, s)
}

func stepNodeLocale(locale string, index int, s Step) ui.Node {
	state := stepStateOf(s.State)
	props := html.Props{Class: "jn-step", DataAttr: html.DataAttribute{Name: "state", Value: state}}
	if state == stepActive {
		props.Aria = map[string]string{"current": "step"}
	}
	// The step a run stopped at gets its own glyph and says so in visible
	// text. Completed steps have carried a check since this stepper shipped;
	// a stopped step used to differ from an unstarted one by hue alone, with
	// its status word audible to assistive technology and invisible to
	// everyone else (UXLIVE-019).
	var mark ui.Node
	switch state {
	case stepDone:
		mark = html.Span(html.Props{Class: "jn-stepmark", Aria: map[string]string{"hidden": "true"}}, RenderIcon(IconCheck, "jn-stepcheck", nil))
	case stepFailed:
		mark = html.Span(html.Props{Class: "jn-stepmark jn-stepdanger", Aria: map[string]string{"hidden": "true"}}, RenderIcon(IconDanger, "jn-stepcheck", nil))
	default:
		mark = html.Span(html.Props{Class: "jn-stepmark", Aria: map[string]string{"hidden": "true"}},
			html.Text(strconv.Itoa(index+1)))
	}
	stateWord := stepStateWordLocale(locale, state)
	var status ui.Node = visuallyHidden(" — " + stateWord)
	if state == stepFailed {
		status = html.Span(html.Props{Class: "jn-stepstate"}, html.Text(stateWord))
	}
	return html.Li(props,
		mark,
		html.Div(html.Props{Class: "jn-stepbody"},
			html.P(html.Props{Class: "jn-steplabel"},
				html.Text(s.Label),
				status,
			),
			htmlIf(s.Detail != "", func() ui.Node {
				return html.P(html.Props{Class: "jn-stepdetail"}, html.Text(s.Detail))
			}),
			htmlIf(s.At != "", func() ui.Node {
				return html.P(html.Props{Class: "jn-stepat", Dir: "auto"}, html.Text(s.At))
			}),
		),
	)
}

func proposalDetailSectionLocale(locale string, v DetailView) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	business := businessFacts(v.Proposal)
	if len(business) == 0 && len(v.Comparison) == 0 {
		return nil
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "proposal-heading"}, html.Text(copy.Text("journey.proposal_heading"))),
		),
	}
	if len(v.Comparison) > 0 {
		children = append(children,
			html.Div(html.Props{Class: "jn-subsection"},
				html.H3(html.Props{Class: "jn-subhead", ID: "comparison-heading"}, html.Text(copy.Text("journey.comparison_heading"))),
				comparisonTableLocale(locale, v.Comparison),
			))
	}
	if len(business) > 0 {
		children = append(children,
			html.Div(html.Props{Class: "jn-subsection"},
				html.H3(html.Props{Class: "jn-subhead"}, html.Text(copy.Text("journey.request_heading"))),
				factsList(business),
			))
	}
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "proposal-heading"}}, children...)
}

func businessFacts(facts []Fact) []Fact {
	out := make([]Fact, 0, len(facts))
	for _, fact := range facts {
		if !fact.Mono {
			out = append(out, fact)
		}
	}
	return out
}

func technicalFacts(facts []Fact) []Fact {
	out := make([]Fact, 0, len(facts))
	for _, fact := range facts {
		if fact.Mono {
			out = append(out, fact)
		}
	}
	return out
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
			dd(html.Props{Class: valueClass, Dir: "auto"}, html.Text(f.Value)),
		)
	})...)
}

func comparisonTableLocale(locale string, rows []ComparisonRow) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	return html.Div(html.Props{Class: "jn-tablewrap"},
		html.Table(html.Props{Class: "jn-table jn-compare"},
			html.Caption(html.Props{Class: "jn-visually-hidden"},
				html.Text(copy.Text("journey.comparison_caption"))),
			html.Thead(html.Props{},
				html.Tr(html.Props{},
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text(copy.Text("journey.table_attribute"))),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text(copy.Text("journey.table_current"))),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text(copy.Text("journey.table_proposed"))),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, html.Text(copy.Text("journey.table_change"))),
				),
			),
			html.Tbody(html.Props{}, html.Map(rows, func(r ComparisonRow) ui.Node { return comparisonRowLocale(locale, r) })...),
		),
	)
}

func comparisonRow(r ComparisonRow) ui.Node {
	return comparisonRowLocale("en-US", r)
}

func comparisonRowLocale(locale string, r ComparisonRow) ui.Node {
	copy := productui.ResolveProductLocale(locale)
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
		change = append(change, chip(toneInfo, copy.Text("journey.table_changed")))
		if r.Delta != "" {
			change = append(change, html.Text(" "),
				html.Span(html.Props{Class: "jn-delta", Dir: "ltr"}, html.Text(r.Delta)))
		}
	default:
		change = append(change, html.Span(html.Props{Class: "jn-muted"}, html.Text(copy.Text("journey.table_unchanged"))))
	}
	return html.Tr(props,
		html.Th(html.Props{Raw: map[string]any{"scope": "row"}}, html.Text(r.Label)),
		html.Td(html.Props{Class: "jn-num", Dir: "auto"}, html.Text(r.Current)),
		html.Td(html.Props{Class: "jn-num jn-proposed", Dir: "auto"}, html.Text(r.Proposed)),
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
	return preflightSectionLocale("en-US", v)
}

func preflightSectionLocale(locale string, v DetailView) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	if len(v.Findings) == 0 && v.PayBand == nil && v.Budget == nil && v.EffectiveWindow == nil {
		return nil
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "findings-heading"}, html.Text(copy.Text("journey.checks_heading"))),
			htmlIf(len(v.Findings) > 0, func() ui.Node {
				return html.P(html.Props{Class: "jn-count"}, html.Text(findingCountLabelLocale(locale, len(v.Findings))))
			}),
		),
	}
	if len(v.Findings) > 0 {
		children = append(children,
			html.Ul(html.Props{Class: "jn-board", Role: "list"}, html.MapIndexed(v.Findings, func(index int, finding Finding) ui.Node {
				return findingRowLocale(locale, index, finding)
			})...))
	}
	if v.PayBand != nil || v.Budget != nil {
		children = append(children, html.Div(html.Props{Class: "jn-gauges"},
			payBandGauge(v.PayBand),
			budgetMeter(v.Budget),
		))
	}
	if v.EffectiveWindow != nil {
		children = append(children, effectiveWindowLocale(locale, v.EffectiveWindow))
	}
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "findings-heading"}}, children...)
}

func findingCountLabelLocale(locale string, n int) string {
	copy := productui.ResolveProductLocale(locale)
	if n == 1 {
		return copy.Text("journey.check_one")
	}
	return copy.Text("journey.check_many", map[string]string{"count": copy.FormatNumber(strconv.Itoa(n), 0)})
}

// findingRow is one line of the preflight board: a status pill that
// animates in, the check's message, and its code. The animation is
// staggered by position through a data-row attribute the stylesheet keys
// off, because a CSS animation-delay cannot be set per element without an
// inline style the content-security-policy forbids.
func findingRow(index int, f Finding) ui.Node {
	return findingRowLocale("en-US", index, f)
}

func findingRowLocale(locale string, index int, f Finding) ui.Node {
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
			html.Span(html.Props{Class: "jn-checkpill-label"}, html.Text(severityWordLocale(locale, sev))),
		),
		html.Div(html.Props{Class: "jn-check-body"},
			html.P(html.Props{Class: "jn-check-msg"}, html.Text(f.Message)),
		),
	)
}

func diagnosticsSectionLocale(locale string, v DetailView) ui.Node {
	if !v.Diagnostics || !v.Journey.DiagnosticsAuthorized {
		return nil
	}
	technical := technicalFacts(v.Proposal)
	if len(technical) == 0 && len(v.Engine) == 0 && len(v.Nodes) == 0 && len(v.WorkItems) == 0 &&
		len(v.Evidence) == 0 && v.Ledger == nil && len(v.Findings) == 0 && len(diagnosticTimelineRefs(v.Timeline)) == 0 {
		return nil
	}
	children := []ui.Node{
		html.P(html.Props{Class: "jn-help"}, html.Text("Identifiers and execution evidence for authorized support staff.")),
	}
	if len(technical) > 0 {
		children = append(children, html.Section(html.Props{Class: "jn-panel jn-diagnostic-panel"},
			html.H3(html.Props{}, html.Text("Request identifiers")), factsList(technical)))
	}
	if len(v.Findings) > 0 {
		children = append(children, diagnosticFindings(v.Findings))
	}
	children = append(children, workflowSection(v), ledgerDiagnostics(v.Ledger), evidenceSection(v.Evidence), diagnosticTimelineSection(v.Timeline))
	return html.Details(html.Props{Class: "jn-diagnostics"},
		html.Summary(html.Props{Class: "jn-diagnostics-summary"}, html.Text(productui.ResolveProductLocale(locale).Text("journey.diagnostics_heading"))),
		html.Div(html.Props{Class: "jn-diagnostics-body"}, children...),
	)
}

// diagnosticTimelineRefs are trace references, not business history. Keep
// them out of the ordinary timeline so an unauthorized viewer cannot recover
// internal record identity from the otherwise useful human-readable history.
func diagnosticTimelineRefs(events []TimelineEvent) []string {
	refs := make([]string, 0, len(events))
	for _, event := range events {
		if event.Ref != "" {
			refs = append(refs, event.Ref)
		}
	}
	return refs
}

func diagnosticTimelineSection(events []TimelineEvent) ui.Node {
	refs := diagnosticTimelineRefs(events)
	if len(refs) == 0 {
		return nil
	}
	facts := make([]Fact, 0, len(refs))
	for i, ref := range refs {
		facts = append(facts, Fact{Label: "History trace " + strconv.Itoa(i+1), Value: ref, Mono: true})
	}
	return html.Section(html.Props{Class: "jn-panel jn-diagnostic-panel"},
		html.H3(html.Props{}, html.Text("History trace references")), factsList(facts))
}

func diagnosticFindings(findings []Finding) ui.Node {
	facts := make([]Fact, 0, len(findings))
	for _, finding := range findings {
		if finding.Code != "" {
			facts = append(facts, Fact{Label: severityWord(severityOf(finding.Severity)), Value: finding.Code, Mono: true})
		}
	}
	if len(facts) == 0 {
		return nil
	}
	return html.Section(html.Props{Class: "jn-panel jn-diagnostic-panel"},
		html.H3(html.Props{}, html.Text("Check identifiers")), factsList(facts))
}

func ledgerDiagnostics(l *LedgerCard) ui.Node {
	if l == nil {
		return nil
	}
	return html.Section(html.Props{Class: "jn-panel jn-diagnostic-panel"},
		html.H3(html.Props{}, html.Text("Ledger record")),
		factsList([]Fact{
			{Label: "Stream", Value: l.StreamKey, Mono: true},
			{Label: "Sequence", Value: l.Sequence, Mono: true},
			{Label: "Schema", Value: l.SchemaRef, Mono: true},
			{Label: "Digest", Value: l.Digest, Mono: true},
			{Label: "Idempotency key", Value: l.IdempotencyKey, Mono: true},
		}))
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
	return effectiveWindowLocale("en-US", w)
}

func effectiveWindowLocale(locale string, w *EffectiveWindow) ui.Node {
	if w == nil {
		return nil
	}
	copy := productui.ResolveProductLocale(locale)
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
		html.H3(html.Props{Class: "jn-subhead"}, html.Text(copy.Text("journey.effective_window_heading"))),
		html.Ol(html.Props{Class: "jn-strip"},
			stop(copy.Text("journey.cycle_opens"), w.Start, "start"),
			stop(copy.Text("journey.takes_effect"), w.EffectiveDate, "effective"),
			stop(copy.Text("journey.current_as_of"), w.KnownAt, "known"),
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
	if len(v.Engine) == 0 && len(v.Nodes) == 0 && len(v.WorkItems) == 0 && len(v.WaitExplanation) == 0 {
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
	// PROMOUX-014: RED was that "Waiting for effective date" named no
	// instant, timezone, owner, scheduled action or explanation. This
	// subsection is that explanation, sourced entirely from the engine
	// (tools/uxqual/journeyclient's waitExplanationFacts): it renders only
	// when every one of those facts arrived, never a partial guess.
	if len(v.WaitExplanation) > 0 {
		children = append(children, html.Div(html.Props{Class: "jn-subsection", DataAttr: html.DataAttribute{Name: "wait-explanation", Value: "present"}},
			html.H3(html.Props{Class: "jn-subhead"}, html.Text("Waiting for effective date")),
			factsList(v.WaitExplanation)))
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
				RenderIcon(IconClock, "jn-workitem-icon", nil), html.Text(w.Kind)),
			chip(w.Tone, w.Status),
		),
		html.P(html.Props{Class: "jn-workitem-lines"}, lines...),
	)
}

func outcomeSection(l *LedgerCard, pending string) ui.Node {
	return outcomeSectionLocale("en-US", l, pending)
}

func outcomeSectionLocale(locale string, l *LedgerCard, pending string) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	heading := copy.Text("journey.outcome_heading")
	var body ui.Node
	if l == nil {
		heading = copy.Text("journey.outcome_pending_heading")
		if strings.TrimSpace(pending) == "" {
			pending = copy.Text("journey.outcome_default")
		}
		body = html.P(html.Props{Class: "jn-quiet"},
			RenderIcon(IconClock, "jn-quiet-icon", nil),
			html.Text(pending))
	} else {
		body = factsList(outcomeFacts(locale, l))
	}
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "outcome-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "outcome-heading"},
				RenderIcon(IconLedger, "jn-headicon", nil), html.Text(heading)),
			htmlIf(l != nil, func() ui.Node {
				label, tone := outcomeChip(locale, l)
				return chip(tone, label)
			}),
		),
		body,
	)
}

// outcomeTone is the tone a terminal record is presented in. A record that
// recorded no promotion is never success, whatever else is unknown about it.
func outcomeTone(l *LedgerCard) string {
	if strings.TrimSpace(l.StatusTone) != "" {
		return toneOf(l.StatusTone)
	}
	if l.Recorded {
		return toneSuccess
	}
	return toneWarning
}

// outcomeChip is the panel head's status chip: the terminal outcome's own
// label and tone, not a standing claim that something was recorded.
func outcomeChip(locale string, l *LedgerCard) (label, tone string) {
	copy := productui.ResolveProductLocale(locale)
	label = strings.TrimSpace(l.StatusLabel)
	if label == "" {
		label = copy.Text("journey.stage_recorded")
		if !l.Recorded {
			label = copy.Text("journey.outcome_not_recorded")
		}
	}
	return label, outcomeTone(l)
}

// outcomeFacts states what the terminal record says. A run that recorded a
// refusal says so and carries no effective date, because nothing took
// effect on one (UXLIVE-001).
func outcomeFacts(locale string, l *LedgerCard) []Fact {
	copy := productui.ResolveProductLocale(locale)
	result := copy.Text("journey.outcome_recorded")
	if !l.Recorded {
		result = copy.Text("journey.outcome_not_recorded")
	}
	facts := []Fact{{Label: copy.Text("journey.outcome_result"), Value: result, Tone: outcomeTone(l)}}
	if l.Recorded {
		facts = append(facts, Fact{Label: copy.Text("journey.compare_effective"), Value: l.EffectiveAt, Tone: toneSuccess})
	}
	return append(facts, Fact{Label: copy.Text("journey.outcome_recorded_at"), Value: l.RecordedAt})
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
	copy := productui.ResolveProductLocale(l.locale)
	shared := sharedDisabledReason(actions)
	l.sharedBlocked = shared
	children := []ui.Node{
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "actions-heading"}, html.Text(copy.Text("journey.actions_heading"))),
		),
	}
	if shared != "" {
		children = append(children, html.P(html.Props{ID: sharedBlockedID, Class: "jn-blocked"},
			RenderIcon(IconDanger, "jn-blocked-icon", nil), html.Text(shared)))
	}
	return html.Section(html.Props{Aria: map[string]string{"labelledby": "actions-heading"}},
		append(children, html.Div(html.Props{Class: "jn-actions"}, html.Map(actions, func(a Action) ui.Node {
			return actionCard(l, a)
		})...))...,
	)
}

// sharedBlockedID names the one section-level reason element every action
// points at when they all share a reason.
const sharedBlockedID = "actions-blocked"

// sharedDisabledReason is the reason every action in the section is refused
// for, when there is more than one action and they all give the same one.
// Repeating it under each card said the same sentence three times and
// buried whatever the reader could still do (UXLIVE-017).
func sharedDisabledReason(actions []Action) string {
	if len(actions) < 2 {
		return ""
	}
	reason := ""
	for _, a := range actions {
		if !a.Disabled || strings.TrimSpace(a.DisabledReason) == "" {
			return ""
		}
		if reason == "" {
			reason = a.DisabledReason
			continue
		}
		if a.DisabledReason != reason {
			return ""
		}
	}
	return reason
}

// actionCard renders one governed operation as its own self-contained
// form. Nothing is shared between actions: each carries its own hidden
// inputs and its own callback, so a page that shows three actions offers
// three independent submissions and no ambient state decides which one
// fires.
func actionCard(l live, a Action) ui.Node {
	sharedReason := l.sharedBlocked
	copy := productui.ResolveProductLocale(l.locale)
	variant := a.Variant
	switch variant {
	case "primary", "secondary", "danger":
	default:
		variant = "secondary"
	}
	// When every action is refused for the same reason the section states it
	// once and each control points at that one statement (UXLIVE-017).
	reasonID := "action-" + a.ID + "-blocked"
	if sharedReason != "" {
		reasonID = sharedBlockedID
	}

	children := []ui.Node{
		html.H3(html.Props{}, html.Text(a.Label)),
	}
	if a.Description != "" {
		children = append(children, html.P(html.Props{Class: "jn-action-desc"}, html.Text(a.Description)))
	}
	if a.ActsAs != "" {
		label := a.ActsAsLabel
		if strings.TrimSpace(label) == "" {
			label = copy.Text("journey.action_acts_as", map[string]string{"actor": a.ActsAs})
		}
		children = append(children, html.P(html.Props{Class: "jn-actsas"},
			RenderIcon(IconPerson, "jn-actsas-icon", nil),
			html.Text(label)))
	}
	children = append(children, html.Fragment(hiddenInputs(a.Hidden)...))

	busy := !a.Disabled && a.Busy
	btn := html.Props{Class: "jn-btn", Type: submitButtonType(a.OnSubmit),
		DataAttr: html.DataAttribute{Name: "variant", Value: variant}}
	if a.Disabled {
		btn.Disabled = true
		btn.Aria = map[string]string{"describedby": reasonID}
	} else if busy {
		btn.Disabled = true
		btn.Aria = map[string]string{"busy": "true"}
	} else if a.OnSubmit != nil {
		btn.OnClick = clickHandler(a.OnSubmit, l.collect(a.Hidden, a.Fields))
	}
	// A control keeps one name across its states. Offered, an action with a
	// confirmation is reached through its review trigger ("Review and
	// withdraw"); refused, the plain button used to fall back to the bare
	// label ("Withdraw"), renaming the same control (UXLIVE-017).
	submitLabel := a.Label
	if a.Disabled && (len(a.Confirmation) > 0 || a.ConfirmationNote != "") {
		_, reviewLabel := actionConfirmationCopy(l.locale, a)
		if strings.TrimSpace(reviewLabel) != "" {
			submitLabel = reviewLabel
		}
	}
	submit := html.Button(btn, html.Text(submitLabel))
	if !a.Disabled && (len(a.Confirmation) > 0 || a.ConfirmationNote != "") {
		confirmTitle, reviewLabel := actionConfirmationCopy(l.locale, a)
		children = append(children, ui.CreateElement(reviewSurface, reviewSurfaceProps{
			ID:             "action-" + a.ID,
			TriggerLabel:   reviewLabel,
			TriggerVariant: variant,
			Heading:        confirmTitle,
			Facts:          a.Confirmation,
			Fields: html.Map(a.Fields, func(field Field) ui.Node {
				return fieldNode(l, field, false)
			}),
			Note:         a.ConfirmationNote,
			Submit:       submit,
			Busy:         busy,
			BusyLabel:    a.BusyLabel,
			DismissLabel: copy.Text("journey.action_cancel_review"),
			CancelLabel:  copy.Text("journey.action_cancel"),
		}))
	} else {
		for _, f := range a.Fields {
			children = append(children, fieldNode(l, f, a.Disabled))
		}
		children = append(children, submit)
	}
	if a.Disabled && a.DisabledReason != "" && sharedReason == "" {
		children = append(children, html.P(html.Props{ID: reasonID, Class: "jn-blocked"},
			RenderIcon(IconWarning, "jn-blocked-icon", nil), html.Text(a.DisabledReason)))
	}

	props := formProps(l, a.Action, a.OnSubmit, a.Hidden, a.Fields)
	props.Class = "jn-card jn-action"
	props.DataAttr = html.DataAttribute{Name: "variant", Value: variant}
	return html.Form(props, children...)
}

func actionConfirmationCopy(locale string, a Action) (title, review string) {
	copy := productui.ResolveProductLocale(locale)
	vars := map[string]string{"action": strings.ToLower(a.Label)}
	title, review = a.ConfirmTitle, a.ReviewLabel
	if strings.TrimSpace(title) == "" {
		switch a.ID {
		case "approve", "reject", "execute":
			title = copy.Text("journey.action_confirm_" + a.ID)
		default:
			title = copy.Text("journey.action_confirm_generic", vars)
		}
	}
	if strings.TrimSpace(review) == "" {
		switch a.ID {
		case "approve", "reject", "execute":
			review = copy.Text("journey.action_review_" + a.ID)
		default:
			review = copy.Text("journey.action_review_generic", vars)
		}
	}
	return title, review
}

func timelineSectionLocale(locale string, events []TimelineEvent) ui.Node {
	if len(events) == 0 {
		return nil
	}
	copy := productui.ResolveProductLocale(locale)
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "timeline-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "timeline-heading"}, html.Text(copy.Text("journey.history_heading"))),
		),
		html.Ol(html.Props{Class: "jn-timeline"}, html.Map(events, func(e TimelineEvent) ui.Node { return timelineNodeLocale(locale, e) })...),
	)
}

func timelineNode(e TimelineEvent) ui.Node {
	return timelineNodeLocale("en-US", e)
}

func timelineNodeLocale(locale string, e TimelineEvent) ui.Node {
	tone := toneOf(e.Tone)
	actor := businessTimelineActor(e.Actor)
	return html.Li(html.Props{Class: "jn-tl", DataAttr: html.DataAttribute{Name: "tone", Value: tone}},
		html.Span(html.Props{Class: "jn-tldot", Aria: map[string]string{"hidden": "true"}}),
		html.Div(html.Props{Class: "jn-tlbody"},
			html.P(html.Props{Class: "jn-tlat"}, html.Text(e.At)),
			html.P(html.Props{Class: "jn-tltitle"}, html.Text(e.Title)),
			htmlIf(actor != "", func() ui.Node {
				return html.P(html.Props{Class: "jn-tldetail"}, html.Text(productui.ResolveProductLocale(locale).Text("journey.timeline_by", map[string]string{"actor": actor})))
			}),
			htmlIf(e.Detail != "", func() ui.Node {
				return html.P(html.Props{Class: "jn-tldetail", Dir: "auto"}, html.Text(e.Detail))
			}),
		),
	)
}

// businessTimelineActor admits presentation labels while suppressing values
// that are clearly protocol principals or service names. The authorized
// diagnostics projection still carries its own typed evidence; business
// history must never become an identity side channel.
func businessTimelineActor(actor string) string {
	normalized := strings.ToLower(strings.TrimSpace(actor))
	if strings.HasPrefix(normalized, "principal:") || strings.HasPrefix(normalized, "principal/") ||
		normalized == "journeyservice" || normalized == "journey service" {
		return ""
	}
	return actor
}
