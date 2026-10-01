package productui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// MissingPunchProjectionState describes whether the server supplied an
// authorized missing-punch projection.
type MissingPunchProjectionState uint8

const (
	MissingPunchUnavailable MissingPunchProjectionState = iota
	MissingPunchReady
)

// PageMissingPunch identifies the governed correction and review surface.
const PageMissingPunch PageID = "time-missing-punch"

type missingPunchDraftState struct {
	proposed, reason, err string
	receipt               *MissingPunchReceipt
	submitting            bool
}

// MissingPunchSessionView is the immutable server projection of the session
// and original observation. The UI never edits these values.
type MissingPunchSessionView struct {
	SessionID, WorkerRef, OriginalObservationID   string
	WorkerLabel, OriginalEventLabel, SessionLabel string
	WorkerTimezone                                string
	OriginalAt                                    time.Time
	ExpectedRevision                              uint64
	PeriodClosed                                  bool
	IdempotencyKey                                string
}

// MissingPunchReviewView is an authorized supervisor review item.
type MissingPunchReviewView struct {
	RequestID, WorkerRef, WorkerLabel, SessionLabel, OriginalEventLabel             string
	OriginalAt, ProposedOutAt                                                       time.Time
	Reason, RequestedBy, WorkflowTraceHref, ReopenRef, DecisionNote, IdempotencyKey string
	Revision                                                                        uint64
	PeriodClosed                                                                    bool
}

// MissingPunchSubmission is the typed request sent to the application port.
type MissingPunchSubmission struct {
	SessionID, WorkerRef, OriginalObservationID, Reason, IdempotencyKey string
	ProposedOutAt                                                       time.Time
	ExpectedRevision                                                    uint64
}

// MissingPunchDecision is the typed independent supervisor decision.
type MissingPunchDecision struct {
	RequestID, WorkerRef, DecisionNote, ReopenRef, IdempotencyKey string
	ExpectedRevision                                              uint64
	Approve                                                       bool
}

// MissingPunchReceipt is durable workflow evidence returned by the native
// transport/application seam. A receipt without a trace is not rendered.
type MissingPunchReceipt struct {
	RequestID, Status, WorkflowTraceHref, WorkflowID, PlanDigest, NodeID string
	WorkflowTraceID, WorkflowInstanceRef                                 string
	Revision                                                             uint64
	Attempt                                                              int
	InstanceVersion                                                      int64
}

// MissingPunchSubmitter submits a request through the authenticated native
// transport. Implementations must perform authorization and idempotency.
type MissingPunchSubmitter interface {
	SubmitMissingPunch(MissingPunchSubmission) (MissingPunchReceipt, error)
}

// MissingPunchDecider completes a supervisor review through native transport.
type MissingPunchDecider interface {
	DecideMissingPunch(MissingPunchDecision) (MissingPunchReceipt, error)
}

// MissingPunchAdminProjection is the complete server-owned page projection.
type MissingPunchAdminProjection struct {
	State         MissingPunchProjectionState
	Session       MissingPunchSessionView
	Pending       []MissingPunchReviewView
	Submitter     MissingPunchSubmitter
	Decider       MissingPunchDecider
	Error, Notice string
}

// MissingPunchAdminPage renders the worker request and supervisor review
// surfaces. Missing capability or ports fail closed with no static actions.
func MissingPunchAdminPage(view View, projection MissingPunchAdminProjection) ui.Node {
	copy := missingPunchText(view.Locale)
	if projection.State != MissingPunchReady || projection.Submitter == nil && projection.Decider == nil {
		return html.Section(html.Props{Class: "time-missing-punch punch-page", Raw: map[string]any{"aria-labelledby": "missing-punch-title"}},
			missingPunchHead(copy, copy.intro),
			html.Div(html.Props{Class: "surface clock-unavailable", Role: "status"},
				html.Span(html.Props{Class: "clock-unavailable-mark", Raw: map[string]any{"aria-hidden": "true"}}, productIcon("clock", "clock-unavailable-icon")),
				html.Div(html.Props{},
					html.H2(html.Props{}, ui.Text(copy.unavailable)),
					html.P(html.Props{Class: "muted"}, ui.Text(copy.unavailableHelp)),
					timeRetryButton(view, PageMissingPunch, timeText(view.Locale, "retry")),
				),
			),
		)
	}
	return missingPunchReadyPage(view, projection, copy)
}

// MissingPunchAdminPageModule supplies the route renderer for composition.
func MissingPunchAdminPageModule() PageModule {
	return pageModule(PageDefinition{ID: PageMissingPunch, Route: "/workspace/app/time/missing-punch", Label: "Fix a missing punch", Icon: "people", Title: "Fix a missing punch", Subtitle: "Tell your supervisor what to correct on your timecard.", LabelKey: "page.time_missing_punch.label", TitleKey: "page.time_missing_punch.title", SubtitleKey: "page.time_missing_punch.subtitle", SearchTerms: []string{"time", "punch", "correction"}, Admitted: false, NavigationPublished: false, RenderOrder: 176, OwnsHeading: true}, missingPunchPageModuleRenderer{}, PageAccessPolicy{Audience: PageAudienceWorker}, routeProfileFor("/workspace/app/time/missing-punch"), dataProfileFor("/workspace/app/time/missing-punch"))
}

type missingPunchPageModuleRenderer struct{}

func (missingPunchPageModuleRenderer) Render(view View) ui.Node {
	return MissingPunchAdminPage(view, view.MissingPunchProjection)
}

func (missingPunchPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{feature("missing_punch_request", "Missing punch request", "Submit a governed correction", true, true, false, false), feature("missing_punch_review", "Missing punch review", "Approve or reject a correction", true, true, true, false)}
}

// missingPunchHead is the page title block; the page owns its one h1.
func missingPunchHead(copy missingPunchCopy, subtitle string) ui.Node {
	return missingPunchHeadTitled(copy.title, subtitle)
}

func missingPunchHeadTitled(title, subtitle string) ui.Node {
	return html.Div(html.Props{Class: "page-head punch-head", Data: map[string]string{"hcm-page": string(PageMissingPunch)}},
		html.Div(html.Props{},
			html.H1(html.Props{ID: "missing-punch-title"}, ui.Text(title)),
			html.P(html.Props{Class: "subtitle"}, ui.Text(subtitle)),
		),
	)
}

func missingPunchReadyPage(view View, projection MissingPunchAdminProjection, copy missingPunchCopy) ui.Node {
	head := missingPunchHead(copy, copy.intro)
	if projection.Submitter == nil {
		// A supervisor who only reviews is not fixing their own punch.
		head = missingPunchHeadTitled(copy.reviewTitle, copy.reviewIntro)
	}
	children := []ui.Node{head}
	if strings.TrimSpace(projection.Error) != "" {
		children = append(children, html.P(html.Props{Class: "clock-notice clock-notice-error", Role: "alert"}, ui.Text(projection.Error)))
	}
	if strings.TrimSpace(projection.Notice) != "" {
		children = append(children, html.P(html.Props{Class: "clock-notice clock-notice-success", Role: "status", Raw: map[string]any{"aria-live": "polite"}}, html.Strong(html.Props{}, ui.Text(copy.receipt)), html.Span(html.Props{}, ui.Text(projection.Notice))))
	}
	if projection.Submitter != nil {
		children = append(children, ui.CreateElement(missingPunchRequestForm, missingPunchRequestProps{View: view, Projection: projection, Copy: copy}))
	}
	if projection.Decider != nil {
		children = append(children, missingPunchReviews(view, projection, copy))
	}
	return html.Section(html.Props{Class: "time-missing-punch punch-page", Raw: map[string]any{"aria-labelledby": "missing-punch-title"}}, children...)
}

type missingPunchRequestProps struct {
	View       View
	Projection MissingPunchAdminProjection
	Copy       missingPunchCopy
}

func missingPunchRequestForm(props missingPunchRequestProps) ui.Node {
	projection, copy, view := props.Projection, props.Copy, props.View
	session := projection.Session
	// The clock-out field starts empty: prefilling it with the clock-in moment
	// looks like a valid answer and could be sent unchanged.
	state := ui.UseState(missingPunchDraftState{})
	draft := state.Get()
	edit := func(update func(*missingPunchDraftState)) {
		next := state.Get()
		update(&next)
		state.Set(next)
	}
	if draft.receipt != nil && draft.receipt.RequestID != "" {
		return html.Section(html.Props{Class: "surface punch-request punch-sent", Role: "status", Raw: map[string]any{"aria-live": "polite", "aria-labelledby": "missing-punch-request-title"}},
			html.H2(html.Props{ID: "missing-punch-request-title"}, ui.Text(copy.submitted)),
			html.P(html.Props{Class: "muted"}, ui.Text(copy.afterSubmit)),
			html.P(html.Props{Class: "punch-reference"}, ui.Text(copy.reference+": "+draft.receipt.RequestID)),
			missingPunchTraceLink(draft.receipt, copy),
		)
	}
	proposedID, reasonID, errID := "missing-punch-proposed", "missing-punch-reason", "missing-punch-form-error"
	proposedAria := map[string]string{"describedby": "missing-punch-proposed-help"}
	reasonAria := map[string]string{"describedby": "missing-punch-reason-help"}
	if draft.err != "" {
		proposedAria["describedby"] += " " + errID
		reasonAria["describedby"] += " " + errID
	}
	proposed := html.Props{ID: proposedID, Name: "proposed_out_at", Type: "datetime-local", Value: draft.proposed, Required: true, Aria: proposedAria, Raw: map[string]any{"min": localDateTime(session.OriginalAt)}, OnInput: ui.UseEvent(func(e ui.InputEvent) { edit(func(d *missingPunchDraftState) { d.proposed = e.GetValue() }) })}
	reason := html.Props{ID: reasonID, Name: "reason", Value: draft.reason, Required: true, Aria: reasonAria, Raw: map[string]any{"rows": "3"}, OnInput: ui.UseEvent(func(e ui.InputEvent) { edit(func(d *missingPunchDraftState) { d.reason = e.GetValue() }) })}
	submitLabel := copy.submit
	if draft.submitting {
		submitLabel = copy.sending
	}
	form := html.Form(html.Props{ID: "missing-punch-form", Class: "punch-form", OnSubmit: ui.UseEvent(func(e ui.FormEvent) {
		e.PreventDefault()
		if draft.submitting {
			return
		}
		zone, err := time.LoadLocation(session.WorkerTimezone)
		if err != nil {
			next := state.Get()
			next.err = copy.error
			state.Set(next)
			return
		}
		proposedAt, err := time.ParseInLocation("2006-01-02T15:04", draft.proposed, zone)
		if err != nil || strings.TrimSpace(draft.reason) == "" {
			next := state.Get()
			next.err = copy.required
			state.Set(next)
			return
		}
		next := state.Get()
		next.submitting = true
		next.err = ""
		state.Set(next)
		receipt, err := projection.Submitter.SubmitMissingPunch(MissingPunchSubmission{SessionID: session.SessionID, WorkerRef: session.WorkerRef, OriginalObservationID: session.OriginalObservationID, ProposedOutAt: proposedAt, Reason: strings.TrimSpace(draft.reason), ExpectedRevision: session.ExpectedRevision, IdempotencyKey: session.IdempotencyKey})
		next = state.Get()
		next.submitting = false
		if err == nil {
			next.receipt = &receipt
		} else {
			next.err = copy.error
		}
		state.Set(next)
	})},
		html.Input(html.Props{Type: "hidden", Name: "session_id", Value: session.SessionID}), html.Input(html.Props{Type: "hidden", Name: "worker_ref", Value: session.WorkerRef}), html.Input(html.Props{Type: "hidden", Name: "original_observation_id", Value: session.OriginalObservationID}), html.Input(html.Props{Type: "hidden", Name: "expected_revision", Value: uintString(session.ExpectedRevision)}),
		html.Div(html.Props{Class: "punch-field"}, html.Label(html.Props{For: proposedID}, ui.Text(copy.proposed)), html.Input(proposed), html.P(html.Props{ID: "missing-punch-proposed-help", Class: "punch-help"}, ui.Text(copy.proposedHelp))),
		html.Div(html.Props{Class: "punch-field"}, html.Label(html.Props{For: reasonID}, ui.Text(copy.reason)), html.Textarea(reason), html.P(html.Props{ID: "missing-punch-reason-help", Class: "punch-help"}, ui.Text(copy.reasonHelp))),
		missingPunchFormError(errID, draft.err),
		html.Button(html.Props{Class: "button primary punch-submit", Type: "submit", Disabled: draft.submitting}, ui.Text(submitLabel)),
	)
	return html.Section(html.Props{Class: "surface punch-request", Raw: map[string]any{"aria-labelledby": "missing-punch-request-title"}},
		html.H2(html.Props{ID: "missing-punch-request-title"}, ui.Text(copy.request)),
		missingPunchFacts(view.Locale, copy, copy.record, session.WorkerLabel, session.SessionLabel, session.OriginalEventLabel, session.OriginalAt),
		form,
	)
}

// missingPunchFormError keeps its live region mounted so the alert is
// announced when text arrives, and renders nothing visible while empty.
func missingPunchFormError(id, text string) ui.Node {
	class := "punch-error"
	if text == "" {
		class += " punch-error-empty"
	}
	return html.P(html.Props{ID: id, Class: class, Role: "alert"}, ui.Text(text))
}

func missingPunchTraceLink(receipt *MissingPunchReceipt, copy missingPunchCopy) ui.Node {
	if receipt == nil || strings.TrimSpace(receipt.WorkflowTraceHref) == "" || receipt.WorkflowTraceID == "" || receipt.WorkflowID == "" || receipt.PlanDigest == "" || receipt.NodeID == "" || receipt.WorkflowInstanceRef == "" || receipt.Attempt < 1 || receipt.InstanceVersion < 1 {
		return html.Fragment()
	}
	return html.A(html.Props{Class: "punch-trace", Href: receipt.WorkflowTraceHref}, ui.Text(copy.trace))
}

func missingPunchReviews(view View, projection MissingPunchAdminProjection, copy missingPunchCopy) ui.Node {
	items := make([]ui.Node, 0, len(projection.Pending))
	for _, review := range projection.Pending {
		items = append(items, html.WithKey(ui.CreateElement(missingPunchReview, missingPunchReviewProps{View: view, Review: review, Decider: projection.Decider, Copy: copy}), "review-"+safeID(review.RequestID)))
	}
	if len(items) == 0 {
		items = append(items, html.P(html.Props{Class: "muted punch-empty"}, ui.Text(copy.emptyPending)))
	}
	return html.Section(html.Props{Class: "surface punch-reviews", Raw: map[string]any{"aria-labelledby": "missing-punch-pending-title"}},
		html.H2(html.Props{ID: "missing-punch-pending-title"}, ui.Text(copy.pending)),
		html.Div(html.Props{Class: "punch-review-list"}, items...),
	)
}

type missingPunchReviewProps struct {
	View    View
	Review  MissingPunchReviewView
	Decider MissingPunchDecider
	Copy    missingPunchCopy
}

type missingPunchReviewDraft struct {
	note, reopen, err string
	busy              bool
	decided           bool
	approved          bool
	confirmingReject  bool
}

func missingPunchReview(props missingPunchReviewProps) ui.Node {
	review, decider, copy, view := props.Review, props.Decider, props.Copy, props.View
	id := "missing-punch-review-" + safeID(review.RequestID)
	draftState := ui.UseState(missingPunchReviewDraft{note: review.DecisionNote, reopen: review.ReopenRef})
	draft := draftState.Get()
	edit := func(update func(*missingPunchReviewDraft)) {
		next := draftState.Get()
		update(&next)
		draftState.Set(next)
	}
	noteID, reopenID, errID := id+"-note", id+"-reopen", id+"-error"
	noteAria := map[string]string{"describedby": id + "-note-help"}
	if draft.err != "" {
		noteAria["describedby"] += " " + errID
	}
	note := html.Props{ID: noteID, Name: "decision_note", Value: draft.note, Required: true, Aria: noteAria, Raw: map[string]any{"rows": "2"}, OnInput: ui.UseEvent(func(e ui.InputEvent) { edit(func(d *missingPunchReviewDraft) { d.note = e.GetValue() }) })}
	reopen := html.Props{ID: reopenID, Name: "reopen_ref", Value: draft.reopen, Required: review.PeriodClosed, OnInput: ui.UseEvent(func(e ui.InputEvent) { edit(func(d *missingPunchReviewDraft) { d.reopen = e.GetValue() }) })}
	decision := func(approve bool) ui.Handler {
		return ui.UseEvent(func(e ui.MouseEvent) {
			if draft.busy || draft.decided {
				return
			}
			if strings.TrimSpace(draft.note) == "" {
				edit(func(d *missingPunchReviewDraft) { d.err = copy.noteRequired })
				return
			}
			if review.PeriodClosed && approve && strings.TrimSpace(draft.reopen) == "" {
				edit(func(d *missingPunchReviewDraft) { d.err = copy.reopenRequired })
				return
			}
			edit(func(d *missingPunchReviewDraft) { d.busy, d.err, d.confirmingReject = true, "", false })
			_, err := decider.DecideMissingPunch(MissingPunchDecision{RequestID: review.RequestID, WorkerRef: review.WorkerRef, DecisionNote: strings.TrimSpace(draft.note), ReopenRef: strings.TrimSpace(draft.reopen), IdempotencyKey: review.IdempotencyKey, ExpectedRevision: review.Revision, Approve: approve})
			edit(func(d *missingPunchReviewDraft) {
				d.busy = false
				if err != nil {
					d.err = copy.error
					return
				}
				d.decided, d.approved = true, approve
			})
		})
	}
	askReject := ui.UseEvent(func(ui.MouseEvent) {
		if draft.busy || draft.decided {
			return
		}
		if strings.TrimSpace(draft.note) == "" {
			edit(func(d *missingPunchReviewDraft) { d.err = copy.noteRequired })
			return
		}
		edit(func(d *missingPunchReviewDraft) { d.confirmingReject, d.err = true, "" })
	})
	proposedAt := missingPunchDisplayTime(view.Locale, review.ProposedOutAt)
	children := []ui.Node{
		html.Header(html.Props{Class: "punch-review-head"},
			html.H3(html.Props{ID: id + "-title"}, ui.Text(review.WorkerLabel)),
			html.P(html.Props{Class: "muted"}, ui.Text(review.SessionLabel)),
		),
	}
	if proposedAt != "" {
		children = append(children, html.P(html.Props{Class: "punch-ask"}, fillBidi(copy.asks, map[string]string{"worker": review.WorkerLabel, "time": proposedAt})...))
	}
	children = append(children, missingPunchReviewFacts(view.Locale, copy, review))
	if draft.decided {
		message, class := copy.rejected, "clock-notice clock-notice-busy"
		if draft.approved {
			message, class = copy.approved, "clock-notice clock-notice-success"
		}
		children = append(children, html.P(html.Props{Class: class, Role: "status"}, ui.Text(message)))
		return html.Article(html.Props{Class: "punch-review punch-review-decided", Raw: map[string]any{"aria-labelledby": id + "-title"}}, children...)
	}
	children = append(children, html.Div(html.Props{Class: "punch-field"}, html.Label(html.Props{For: noteID}, ui.Text(copy.decisionNote)), html.Textarea(note), html.P(html.Props{ID: id + "-note-help", Class: "punch-help"}, ui.Text(copy.decisionHelp))))
	if review.PeriodClosed {
		children = append(children,
			html.P(html.Props{Class: "clock-notice clock-notice-error", Role: "alert"}, ui.Text(copy.periodClosed)),
			html.Div(html.Props{Class: "punch-field"}, html.Label(html.Props{For: reopenID}, ui.Text(copy.reopen)), html.Input(reopen)),
		)
	}
	children = append(children, missingPunchFormError(errID, draft.err))
	approveLabel := copy.approve
	if draft.busy {
		approveLabel = copy.sending
	}
	if draft.confirmingReject {
		yes := html.Button(html.Props{Class: "button destructive", Type: "button", Disabled: draft.busy, OnClick: decision(false)}, ui.Text(copy.rejectYes))
		no := html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) {
			edit(func(d *missingPunchReviewDraft) { d.confirmingReject = false })
		})}, ui.Text(copy.rejectNo))
		children = append(children, timeConfirmPanel(id+"-reject", copy.rejectTitle, copy.rejectBody, true, yes, no))
	} else {
		children = append(children, html.Div(html.Props{Class: "punch-decision"},
			html.Button(html.Props{Class: "button primary", Type: "button", Disabled: draft.busy, OnClick: decision(true)}, ui.Text(approveLabel)),
			html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: draft.busy, OnClick: askReject}, ui.Text(copy.reject)),
		))
	}
	if review.WorkflowTraceHref != "" {
		children = append(children, html.A(html.Props{Class: "punch-trace", Href: review.WorkflowTraceHref}, ui.Text(copy.traceReview)))
	}
	return html.Article(html.Props{Class: "punch-review", Raw: map[string]any{"aria-labelledby": id + "-title"}}, children...)
}

func missingPunchReviewFacts(locale LocaleContext, copy missingPunchCopy, review MissingPunchReviewView) ui.Node {
	rows := []ui.Node{
		missingPunchMomentRow(copy.original, review.OriginalEventLabel, missingPunchDisplayTime(locale, review.OriginalAt)),
	}
	if strings.TrimSpace(review.Reason) != "" {
		rows = append(rows, missingPunchFactRow(copy.reasonGiven, review.Reason))
	}
	if strings.TrimSpace(review.RequestedBy) != "" && review.RequestedBy != review.WorkerRef && review.RequestedBy != review.WorkerLabel {
		rows = append(rows, missingPunchFactRow(copy.requestedBy, review.RequestedBy))
	}
	return html.Tag("dl", html.Props{Class: "punch-record"}, rows...)
}

func missingPunchFacts(locale LocaleContext, copy missingPunchCopy, heading, worker, session, event string, at time.Time) ui.Node {
	rows := []ui.Node{}
	if strings.TrimSpace(worker) != "" {
		rows = append(rows, missingPunchFactRow(copy.worker, worker))
	}
	if strings.TrimSpace(session) != "" {
		rows = append(rows, missingPunchFactRow(copy.session, session))
	}
	rows = append(rows, missingPunchMomentRow(copy.original, event, missingPunchDisplayTime(locale, at)))
	return html.Div(html.Props{Class: "punch-record-block"},
		html.H3(html.Props{Class: "punch-record-title"}, ui.Text(heading)),
		html.Tag("dl", html.Props{Class: "punch-record"}, rows...),
	)
}

func missingPunchFactRow(label, value string) ui.Node {
	return html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(label)), html.Tag("dd", html.Props{}, ui.Text(value)))
}

// missingPunchMomentRow is a fact whose value ends in a date and time. The
// moment sits in its own left-to-right run so an Arabic sentence cannot
// scramble it.
func missingPunchMomentRow(label, event string, when string) ui.Node {
	value := []ui.Node{}
	if event != "" {
		value = append(value, ui.Text(event+", "))
	}
	if when != "" {
		value = append(value, html.Tag("bdi", html.Props{Dir: "ltr"}, ui.Text(when)))
	}
	return html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(label)), html.Tag("dd", html.Props{}, value...))
}

// missingPunchDisplayTime renders a moment for reading. The datetime-local
// input keeps the ISO form; everything a person reads uses their locale.
func missingPunchDisplayTime(locale LocaleContext, value time.Time) string {
	if value.IsZero() {
		return ""
	}
	value = value.Local()
	switch locale.normalized().Resolved {
	case "de-DE":
		return value.Format("02.01.2006, 15:04") + " Uhr"
	case "ar", "ar-SA":
		return value.Format("2006/01/02 15:04")
	}
	return value.Format("Mon, Jan 2, 3:04 PM")
}

func submitMissingPunch(port MissingPunchSubmitter, session MissingPunchSessionView, draft struct{ proposed, reason string }, copy missingPunchCopy) {
	proposed, err := time.Parse("2006-01-02T15:04", draft.proposed)
	if err != nil || strings.TrimSpace(draft.reason) == "" {
		return
	}
	_, _ = port.SubmitMissingPunch(MissingPunchSubmission{SessionID: session.SessionID, WorkerRef: session.WorkerRef, OriginalObservationID: session.OriginalObservationID, ProposedOutAt: proposed, Reason: strings.TrimSpace(draft.reason), ExpectedRevision: session.ExpectedRevision, IdempotencyKey: session.SessionID + ":" + uintString(session.ExpectedRevision)})
}

func localDateTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("2006-01-02T15:04")
}
func uintString(value uint64) string { return strconv.FormatUint(value, 10) }
func safeID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "item"
	}
	return b.String()
}
