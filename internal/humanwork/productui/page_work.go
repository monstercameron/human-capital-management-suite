package productui

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type workCollectionOptions struct {
	Title      string
	ListDetail bool
	Kind       string
}

func localizedWorkTitle(locale LocaleContext, item WorkItem) string {
	if item.TitleKey != "" {
		return locale.Text(item.TitleKey)
	}
	return item.Title
}

func localizedWorkStatus(locale LocaleContext, item WorkItem) string {
	if item.StatusKey != "" {
		return locale.Text(item.StatusKey)
	}
	return item.Status
}

// workPage is a route adapter: it resolves application state into immutable,
// purpose-built props and delegates all markup to the component layer.
func workPage(view View) ui.Node {
	// The queue and its preview draw from the admitted population, so
	// denied proposal artifacts never render.
	scoped := view
	scoped.Work = MyWorkItems(admittedWork(view), scoped.Viewer)
	collection := workCollectionProps(scoped, workCollectionOptions{Title: scoped.Locale.Text("home.needs_action"), ListDetail: true})
	collection.Description = scoped.Locale.Text("work.action_queue_description")
	if scoped.WorkFilter == "" {
		collection.EmptyTitle = scoped.Locale.Text("work.empty_title")
		collection.EmptyDetail = scoped.Locale.Text("work.action_queue_empty_detail")
	} else if scoped.WorkFilter == "review" {
		collection.EmptyTitle = scoped.Locale.Text("work.review_empty_title")
		collection.EmptyDetail = scoped.Locale.Text("work.review_empty_detail")
	} else if scoped.WorkFilter == "blocked" {
		collection.EmptyTitle = scoped.Locale.Text("work.blocked_empty_title")
		collection.EmptyDetail = scoped.Locale.Text("work.blocked_empty_detail")
	}
	collection.Footer.Label = ""
	if len(collection.Rows) == 0 && (len(scoped.EffectivePermissions) == 0 || scoped.Can(PageJourneys, "view")) {
		collection.Footer.Action = ActionLinkProps{Label: scoped.Locale.Text("work.track_requests"), Href: statefulHref(scoped, PageJourneys), Navigate: scoped.Navigate}
	}
	buckets := pageWorkBuckets(scoped.Work, scoped.Viewer)
	draftsView := scoped
	draftsView.Work = buckets.Drafts
	draftsView.WorkFilter = "drafts"
	drafts := workCollectionProps(draftsView, workCollectionOptions{Title: scoped.Locale.Text("work.resumable_drafts"), Kind: "drafts"})
	drafts.Description = scoped.Locale.Text("work.drafts_description")
	drafts.Tabs = nil
	tracked := trackedRequestsFor(scoped, append(append([]WorkItem(nil), buckets.Tracked...), buckets.PassiveWaits...))
	return ui.CreateElement(WorkPage, WorkPageProps{
		I18nProps:   I18nProps{Locale: scoped.Locale},
		Collection:  collection,
		Preview:     workPreviewProps(scoped, selectedOpenWork(scoped)),
		HidePreview: len(collection.Rows) == 0,
		Drafts:      drafts, Tracked: tracked,
		// A filtered queue must not silently append unrelated tracked people
		// beneath its result set; the explicit Tracked tab owns those rows.
		ShowSecondary: scoped.WorkFilter == "" && len(drafts.Rows) > 0,
	})
}

func trackedRequestsFor(view View, items []WorkItem) TrackedRequestsProps {
	props := TrackedRequestsProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Title:     view.Locale.Text("home.tracked_title"), Description: view.Locale.Text("home.tracked_description"),
		EmptyTitle: view.Locale.Text("home.tracked_empty_title"), EmptyDetail: view.Locale.Text("home.tracked_empty_detail"),
	}
	byID := make(map[string]WorkItem, len(items))
	for _, item := range items {
		if _, exists := byID[item.ID]; !exists {
			byID[item.ID] = item
		}
	}
	for _, summary := range SummarizeTracked(items) {
		source := byID[summary.ID]
		props.Items = append(props.Items, TrackedRequestProps{ID: summary.ID, Title: localizedWorkTitle(view.Locale, source), Person: source.Person, Status: localizedWorkStatus(view.Locale, source), Due: summary.Due, Open: summary.Open, Href: source.Href, Navigate: view.Navigate})
	}
	return props
}

func workCollectionProps(view View, options workCollectionOptions) WorkCollectionProps {
	// Every surface that shows this strip names the same filters, and each
	// states how much work it holds, so a viewer can see where their work is
	// without opening each one (UXLIVE-018).
	tabs := []WorkTabProps{
		workTabProps(view, "", view.Locale.Text("work.all")),
		workTabProps(view, "review", view.Locale.Text("work.awaiting")),
		workTabProps(view, "blocked", view.Locale.Text("work.blocked")),
		workTabProps(view, "mine", view.Locale.Text("work.mine")),
		// PROMOUX-012: promotions the viewer proposed, tracked apart from the
		// work they must do.
		workTabProps(view, "tracked", view.Locale.Text("work.tracked")),
	}
	tabs = append(tabs, WorkTabProps{Label: view.Locale.Text("work.past"), Href: statefulHref(view, PageHistory), Navigate: view.Navigate})
	items := view.Work
	if view.WorkFilter == "" {
		// PROMOUX-012: the default view is the viewer's actionable work only;
		// a visible journey that asks nothing of them is not "work".
		items = ActionableWorkItems(items)
	}
	// UXAUDIT-017: an action queue orders by urgency, not admission order.
	items = SortWorkByUrgency(items)
	selectedID := ""
	if options.ListDetail {
		selectedID = selectedOpenWork(view).ID
	}
	rows := make([]WorkRowProps, 0, len(items))
	undisclosed := false
	for _, item := range items {
		undisclosed = undisclosed || !item.WorkSummary
		rows = append(rows, WorkRowProps{
			ID: item.ID, Initials: item.Initials, PhotoURL: item.PhotoURL, Title: localizedWorkTitle(view.Locale, item), Person: item.Person,
			Summary: item.Summary, Due: item.Due, JourneyStage: localizedWorkStatus(view.Locale, item),
			NextStep: workNextStepText(view.Locale, item.NextStep), WaitingOn: workWaitingOnText(view.Locale, item.WaitingOn),
			Assignment: workAssignmentText(view.Locale, item), WorkDue: workDueText(view.Locale, item),
			NextAction:       workNextActionText(view.Locale, item),
			Tracking:         workTrackingText(view.Locale, item),
			StatusProjection: item.StatusProjection,
			Disposition:      approvalDispositionCardProps(view.Locale, item.Disposition),
			Href:             workFilterHref(view, view.WorkFilter, "selected", item.ID),
			Selected:         options.ListDetail && item.ID == selectedID, Navigate: view.Navigate,
		})
	}
	footer := WorkCollectionFooterProps{Label: view.Locale.Text("work.authorized")}
	if undisclosed {
		// Some rows carry no server work item summary (no open work item, or
		// one this viewer may not see); say so once instead of a "Not
		// reported" cell on every such row.
		footer.Note = view.Locale.Text("work.assignee_note")
	}
	if !options.ListDetail {
		footer.Action = ActionLinkProps{
			Label: view.Locale.Text("work.view"), Href: statefulHref(view, PageWork), Navigate: view.Navigate,
		}
	}
	emptyTitle, emptyDetail := view.Locale.Text("work.empty_title"), view.Locale.Text("work.empty_detail")
	if view.WorkFilter == "tracked" {
		emptyTitle, emptyDetail = view.Locale.Text("work.tracked_empty_title"), view.Locale.Text("work.tracked_empty_detail")
	}
	kind := options.Kind
	if kind == "" {
		kind = "action-queue"
	}
	return WorkCollectionProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Title:     options.Title, Kind: kind, CountLabel: view.Locale.Plural("work.item_count", int64(len(items))), Tabs: tabs, Rows: rows,
		Footer: footer, EmptyTitle: emptyTitle, EmptyDetail: emptyDetail,
	}
}

func selectedOpenWork(view View) WorkItem {
	if view.WorkFilter != "" {
		return selectedWork(view)
	}
	items := ActionableWorkItems(view.Work)
	if view.SelectedWork != "" {
		for _, item := range items {
			if item.ID == view.SelectedWork {
				return item
			}
		}
		return WorkItem{}
	}
	// No explicit selection: preview the same item the urgency-ordered
	// queue shows first, so the highlighted row and the preview panel never
	// disagree about which item "first" means.
	ordered := SortWorkByUrgency(items)
	if len(ordered) > 0 {
		return ordered[0]
	}
	return WorkItem{}
}

func workTabProps(view View, filter, label string) WorkTabProps {
	// The count is this filter's own projection of the viewer's work, taken
	// from the same function that builds the list it links to, so the number
	// and the page behind it can never disagree.
	count := len(FilterWorkCollection(view.Work, ParseWorkCollectionFilter(filter)))
	return WorkTabProps{
		Label: label, Href: workFilterHref(view, filter),
		Count:  view.Locale.FormatNumber(strconv.Itoa(count), 0),
		Active: view.WorkFilter == filter, Navigate: view.Navigate,
	}
}

// workFilterHref is a My Work address that always states its filter. My Work
// adopts the viewer's saved filter when the address carries none (UXAUDIT-017,
// "retain filters on return"), so the unfiltered "Open work" view must be
// addressed as an explicit empty filter; otherwise choosing it would silently
// re-apply the saved filter and the viewer could never clear it.
func workFilterHref(view View, filter string, keyValues ...string) string {
	href := statefulHref(view, PageWork, append([]string{"filter", filter}, keyValues...)...)
	if filter == "" {
		return withExplicitEmptyQuery(href, "filter")
	}
	return href
}

// workNextStepText localizes a shared NextStep code; an empty or unknown code
// renders nothing rather than a placeholder.
func workNextStepText(locale LocaleContext, code string) string {
	if !knownWorkNextSteps[code] {
		return ""
	}
	return locale.Text("work.row_next_step", map[string]string{"step": locale.Text("work.next_step." + code)})
}

// workWaitingOnText localizes a shared StageActor code the same way.
func workWaitingOnText(locale LocaleContext, code string) string {
	if !knownWorkActors[code] {
		return ""
	}
	return locale.Text("work.row_waiting_on", map[string]string{"actor": locale.Text("work.waiting_on." + code)})
}

// workAssignmentText states who holds the current work item, from the server
// summary only: the viewer themself, a claim the viewer may take, or the
// disclosed assignee. No summary, or no disclosed assignee, renders nothing.
func workAssignmentText(locale LocaleContext, item WorkItem) string {
	if !item.WorkSummary {
		return ""
	}
	switch item.ViewerMembership {
	case "ASSIGNEE", "CLAIMANT":
		return locale.Text("work.row_assigned_to_you")
	case "CANDIDATE":
		return locale.Text("work.row_claimable_by_you")
	}
	if item.AssigneeName != "" {
		return locale.Text("work.row_assigned_to", map[string]string{"assignee": item.AssigneeName})
	}
	return ""
}

// workDueText is the work item's real deadline, when the server disclosed one.
func workDueText(locale LocaleContext, item WorkItem) string {
	if !item.WorkSummary || item.WorkDue == "" {
		return ""
	}
	return locale.Text("work.row_due", map[string]string{"date": item.WorkDue})
}

// workNextActionText is the viewer's next executable action (WorkNextAction).
func workNextActionText(locale LocaleContext, item WorkItem) string {
	code := WorkNextAction(item)
	if code == "" {
		return ""
	}
	return locale.Text("work.row_next_action", map[string]string{"action": locale.Text("work.action." + code)})
}

// workTrackingText states, on a row the viewer tracks but need not act on,
// that nothing is theirs to do (PROMOUX-012). A passive wait adds nothing
// else: the row's next step and waiting-on lines already name the next
// transition and its owner.
func workTrackingText(locale LocaleContext, item WorkItem) string {
	if item.Terminal || item.ViewerResponsibility != workResponsibilityTracking {
		return ""
	}
	return locale.Text("work.row_no_action_needed")
}

// knownWorkNextSteps and knownWorkActors mirror the closed vocabularies of
// tools/uxqual/journeyclient.NextStep and StageActor; internal/ never imports
// tools/, so the codes cross as strings. TestTodo_UXAUDIT_017_Regression in tools/uxqual/productclient
// pins the two lists together.
var (
	knownWorkNextSteps = map[string]bool{
		"start_approval": true, "correct_proposal": true, "approval_decision": true, "manager_decision": true,
		"finance_decision": true, "reapproval_decision": true, "repair": true, "await_effective_date": true, "system_processing": true,
		"await_acknowledgement": true,
	}
	knownWorkActors = map[string]bool{"proposer": true, "approver": true, "manager": true, "finance": true, "system": true}
)

// KnownWorkNextStep and KnownWorkActor report whether code has product copy.
func KnownWorkNextStep(code string) bool { return knownWorkNextSteps[code] }
func KnownWorkActor(code string) bool    { return knownWorkActors[code] }

func workPreviewProps(view View, item WorkItem) WorkPreviewProps {
	if item.ID == "" {
		return WorkPreviewProps{
			Empty: true, EmptyTitle: view.Locale.Text("work.nothing_selected"), EmptyDetail: view.Locale.Text("work.nothing_detail"),
			Action: ActionLinkProps{Label: view.Locale.Text("work.show_all"), Href: workFilterHref(view, ""), Class: "button secondary", Navigate: view.Navigate},
		}
	}
	facts := []FactProps{}
	if step := workNextStepText(view.Locale, item.NextStep); step != "" {
		facts = append(facts, FactProps{Label: view.Locale.Text("work.next_step_label"), Value: view.Locale.Text("work.next_step." + item.NextStep)})
	}
	if workWaitingOnText(view.Locale, item.WaitingOn) != "" {
		facts = append(facts, FactProps{Label: view.Locale.Text("work.waiting_on_label"), Value: view.Locale.Text("work.waiting_on." + item.WaitingOn)})
	}
	for _, text := range []string{workAssignmentText(view.Locale, item), workDueText(view.Locale, item), workNextActionText(view.Locale, item)} {
		if text != "" {
			facts = append(facts, FactProps{Label: view.Locale.Text("work.current_work_item_label"), Value: text})
		}
	}
	return WorkPreviewProps{
		ID: item.ID, Initials: item.Initials, PhotoURL: item.PhotoURL, Title: localizedWorkTitle(view.Locale, item), Person: item.Person,
		Summary: item.Summary, JourneyStage: localizedWorkStatus(view.Locale, item), StatusProjection: item.StatusProjection, Provenance: item.Provenance,
		Disposition: approvalDispositionCardProps(view.Locale, item.Disposition), FactsTitle: view.Locale.Text("work.server_proposal"),
		Facts: append(facts,
			FactProps{Label: view.Locale.Text("work.effective_date"), Value: valueOrUnavailableFor(view.Locale, item.EffectiveDate)},
			FactProps{Label: view.Locale.Text("work.current_base"), Value: money(view.Locale, item.CurrentBase)},
			FactProps{Label: view.Locale.Text("work.proposed_base"), Value: money(view.Locale, item.ProposedBase)},
		),
		// PROMOUX-008: the journey id is a work-item UUID -- RED names it by
		// name -- so it no longer sits in the plain Facts list every viewer
		// of this page reads. It is authorized diagnostics-only.
		Diagnostics: workTechnicalDetails(view, item),
		Action:      ActionLinkProps{Label: view.Locale.Text("work.open_journey"), Href: item.Href, Class: "button primary full", Navigate: view.Navigate},
	}
}

// workTechnicalDetails is PROMOUX-008's authorized-only disclosure for the
// selected work item's journey id. Available is view.Can's server-derived
// verdict, never item.ID's presence: item.ID is always set for a real
// selection (workPreviewProps already returned the Empty variant above when
// it is not), so gating on Available alone is what keeps two viewers at the
// same authority level -- one selecting a journey, one not having reached
// this function at all because their queue is empty -- indistinguishable in
// disclosure shape; the only thing that varies is view.Can's answer.
func workTechnicalDetails(view View, item WorkItem) TechnicalDetailsProps {
	if !view.Can(PageJourneyDiagnostics, "view") {
		return TechnicalDetailsProps{}
	}
	return TechnicalDetailsProps{
		Available: true,
		Items:     []TechnicalDetailItem{{Label: view.Locale.Text("work.journey_id"), Value: item.ID}},
	}
}

func valueOrUnavailable(value string) string {
	if value == "" {
		return "Not reported"
	}
	return value
}

func valueOrUnavailableFor(locale LocaleContext, value string) string {
	if value == "" {
		return locale.Text("common.not_reported")
	}
	return value
}

func money(locale LocaleContext, value values.Money) string {
	if value.Validate() != nil {
		return locale.Text("common.not_disclosed")
	}
	return locale.FormatMoney(value.Amount().String(), value.Currency(), int(value.Amount().Scale()))
}

// percentage formats an exact decimal ratio without converting compensation
// data through binary floating point.
func percentage(locale LocaleContext, value string) string {
	ratio, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok {
		return locale.Text("common.not_disclosed")
	}
	ratio.Mul(ratio, big.NewRat(100, 1))
	decimal := strings.TrimRight(strings.TrimRight(ratio.FloatString(2), "0"), ".")
	fraction := 0
	if point := strings.IndexByte(decimal, '.'); point >= 0 {
		fraction = len(decimal) - point - 1
	}
	return locale.FormatNumber(decimal, fraction) + "%"
}
