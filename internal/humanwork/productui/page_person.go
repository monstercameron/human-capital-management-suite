package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// resolvedPersonPageLabel is the single identity source for the object page's
// title and breadcrumb. The preferred display name has its own discovery
// verdict; a worker number is appended only when the server explicitly
// admits that field. Legal name remains in the closed personal-data section.
func resolvedPersonPageLabel(view View) (string, bool) {
	// A route transition may still carry the previous worker population while
	// its destination read is in flight. Never identify the new subject from
	// that stale baseline, even when its ID happens to be present there.
	if view.Loading || view.ContentLoading {
		return "", false
	}
	person, ok := exactPerson(view)
	if !ok || !DiscoveryAdmitted(person.ID, workerIdentityVerdicts(view)) {
		return "", false
	}
	identity := ResolveWorkerIdentity(view.Locale, person, workerIdentityVerdicts(view))
	if identity.NameStatus != WorkerFactPresent || strings.TrimSpace(identity.Name) == "" {
		return "", false
	}
	return identity.Label, true
}

// personPage is the route adapter. It resolves authorized projection data and
// passes presentation-only props into the reusable component tree.
func personPage(view View) ui.Node {
	returnHref := peopleReturnHref(view)
	props := PersonPageProps{I18nProps: I18nProps{Locale: view.Locale}, BackHref: returnHref, Navigate: view.Navigate}
	person, ok := exactPerson(view)
	if !ok || !DiscoveryAdmitted(person.ID, workerIdentityVerdicts(view)) {
		props.Unavailable = PersonUnavailableProps{DirectoryHref: returnHref, Navigate: view.Navigate}
		return ui.CreateElement(PersonPage, props)
	}

	profile := personProfileProps(view, person, PagePerson)
	props.Profile = &profile
	return ui.CreateElement(PersonPage, props)
}

// personProfileProps is the shared adapter from one authorized worker row to
// the narrow profile component contract. Person and self-service routes use
// the same facts and composition; only their navigation state differs.
func personProfileProps(view View, person Person, target PageID) PersonProfileProps {
	text := view.Locale.Text
	identity := ResolveWorkerIdentity(view.Locale, person, workerIdentityVerdicts(view))
	overview := ResolveWorkerOverview(view.Locale, person, view.RecordVerdicts)
	employment := ResolveWorkerEmployment(view.Locale, person, view.RecordVerdicts)
	pay := ResolveWorkerPay(view.Locale, person, view.RecordVerdicts)
	value := func(raw string) string { return valueOrUnavailableFor(view.Locale, raw) }
	// fact renders one fact through the record's field verdict once the
	// server sends verdicts, honoring its disposition: HIDE omits the row
	// entirely. A silent server keeps the current values. Allowed values
	// project identically to value(), so governing changes nothing until
	// a verdict actually withholds.
	fields := view.RecordVerdicts[person.ID].Fields
	silent := len(view.RecordVerdicts) == 0
	fact := func(facts []ProfileFactProps, label, name, raw string) []ProfileFactProps {
		// The worker and record identifiers are machine keys, set as code
		// whenever their value is shown (a withheld value is a sentence).
		code := raw != "" && (name == "worker_id" || name == "record_id")
		if silent {
			status := WorkerFactPresent
			if raw == "" {
				status = WorkerFactMissing
			}
			return append(facts, ProfileFactProps{Label: label, Value: value(raw), Status: status, Code: code})
		}
		field, known := fields[name]
		if record, hasRecord := view.RecordVerdicts[person.ID]; hasRecord && !record.Disclosable {
			// A governed record that is not disclosable is a policy decision,
			// not an absent field verdict. Keep that distinction in the
			// presentation contract and never inspect or render its raw value.
			return append(facts, ProfileFactProps{Label: label, Value: view.Locale.Text("provenance.value.withheld"), Status: WorkerFactWithheld})
		}
		if !known {
			projected, _ := ProjectField(view.Locale, raw, AuthorizedField{})
			return append(facts, ProfileFactProps{Label: label, Value: projected.Text, Status: WorkerFactUnknown})
		}
		projected, admitted := ProjectField(view.Locale, raw, field)
		if !admitted {
			return facts
		}
		status := WorkerFactWithheld
		if field.Disposition == FieldShow || field.Disposition == "" && field.Effect == PresentationAllow {
			status = WorkerFactPresent
			if raw == "" {
				status = WorkerFactMissing
			}
		}
		return append(facts, ProfileFactProps{Label: label, Value: projected.Text, Status: status, Code: code && status == WorkerFactPresent})
	}
	details := profileFactsFromWorkerSection(overview)
	organization := profileFactsFromWorkerSection(employment)
	compensation := profileFactsFromWorkerSection(pay)
	personal := fact(nil, text("person.legal_name"), "legal_name", person.LegalName)
	personal = fact(personal, text("person.preferred_name"), "preferred_name", person.PreferredName)
	personal = fact(personal, text("person.worker_id"), "worker_id", person.WorkerID)
	personal = fact(personal, text("person.worker_ref"), "record_id", person.ID)

	return PersonProfileProps{
		Hero: PersonHeroProps{
			Initials: identity.Initials, PhotoURL: identity.PhotoURL, Name: identity.Label, Role: identity.Role,
			NameStatus: identity.NameStatus, RoleStatus: identity.RoleStatus,
		},
		Details: EmploymentDetailsProps{
			Title: text("person.employment_overview"), Description: text("person.employment_overview_detail"),
			Facts: details},
		Organization: EmploymentDetailsProps{
			Title: text("person.organization"), Description: text("person.organization_detail"), Class: "organization-details",
			Facts: organization,
		},
		Compensation: EmploymentDetailsProps{
			Title: text("person.compensation"), Description: text("person.compensation_detail"), Class: "compensation-details",
			Facts: compensation},
		Personal: SensitiveDetailsProps{
			Title: text("person.personal_information"), Description: text("person.personal_hidden"), Badge: text("person.restricted"),
			Facts: personal,
		},
		Workflows: personWorkflowLauncherProps(view, person, target),
		Active:    personActiveWorkflowsProps(view, person, target),
		History: workflowHistoryPropsForTarget(view, person.ID, target, text("work.past"),
			text("person.history_detail", map[string]string{"name": identity.Name}), true),
	}
}

func profileFactsFromWorkerSection(section WorkerSection) []ProfileFactProps {
	facts := make([]ProfileFactProps, 0, len(section.Facts))
	for _, fact := range section.Facts {
		// Provenance metadata belongs in authorized diagnostics, not the
		// ordinary employee overview. The underlying section retains it.
		if fact.Name == "record_source" || fact.Name == "record_created" {
			continue
		}
		facts = append(facts, ProfileFactProps{Label: fact.Label, Value: fact.Value, Status: fact.Status})
	}
	return facts
}

func personWorkflowLauncherProps(view View, person Person, target PageID) WorkflowLauncherProps {
	identity := ResolveWorkerIdentity(view.Locale, person, workerIdentityVerdicts(view))
	authorized := len(view.EffectivePermissions) == 0 || view.Can(PageJourneys, "create")
	actions := []WorkerAction(nil)
	if authorized {
		actions = DiscoverWorkerActions(view.PersonWorkflows, person, view.WorkflowQuery)
	}
	workflows := make([]WorkflowCardProps, 0, len(actions))
	activeItem, hasActiveJourney := activePromotionWorkItem(view, person.ID)
	hasActivePromotion := false
	for _, action := range actions {
		// Never advertise a duplicate Start when an open journey already
		// exists, even if an availability projection lags the journey state.
		if action.ID == "promotion" && (person.PromotionAvailability == PromotionActiveConflict || hasActiveJourney) {
			if hasActiveJourney {
				hasActivePromotion = true
				workflows = append(workflows, WorkflowCardProps{
					Name:            view.Locale.Text("people.open_active_promotion"),
					ActionLabel:     view.Locale.Text("people.open_active_promotion"),
					AccessibleLabel: view.Locale.Text("people.open_active_promotion_aria", map[string]string{"name": identity.Label}),
					Category:        action.Category,
					Description:     PromotionAvailabilityReason(view.Locale, PromotionActiveConflict),
					Href:            JourneyDetailHref(view, activeItem.ID), Navigate: view.Navigate,
				})
			}
			continue
		}
		if action.ID == "promotion" && !personPromotionEligible(person) {
			continue
		}
		workflows = append(workflows, WorkflowCardProps{
			Name: action.Name, Category: action.Category, Description: action.Description, Href: action.Href, Navigate: view.Navigate,
			AccessibleLabel: view.Locale.Text("people.workflow_aria", map[string]string{"workflow": action.Name, "name": identity.Label}),
		})
	}
	filter := workflowFilterProps(view, person, target)
	unavailableDetail := ""
	switch {
	case !authorized:
		unavailableDetail = PromotionAvailabilityReason(view.Locale, PromotionWithheld)
	case hasActiveJourney:
		// The active-promotion card above is the recovery path. When the
		// launcher has no card to show anyway, the empty state says why
		// rather than falling back to the generic unavailable sentence
		// (UXLIVE-023); this names nothing the page does not already show
		// in its own active-workflows list.
		if len(workflows) == 0 {
			unavailableDetail = PromotionAvailabilityReason(view.Locale, PromotionActiveConflict)
		}
	case !personPromotionEligible(person):
		unavailableDetail = PromotionAvailabilityReason(view.Locale, person.PromotionAvailability)
	}
	launcher := WorkflowLauncherProps{
		UnavailableDetail: unavailableDetail,
		PersonName:        identity.Label, TotalCount: len(workflows), Filter: filter, Workflows: workflows,
	}
	if hasActivePromotion {
		launcher.Heading = view.Locale.Text("workflow.continue_heading")
		launcher.Description = view.Locale.Text("workflow.continue_detail", map[string]string{"name": identity.Name})
		launcher.HideCount = true // "available" would count a request already in progress.
	}
	return launcher
}

func workflowFilterProps(view View, person Person, target PageID) WorkflowFilterProps {
	personID := person.ID
	if target == PageMyself {
		// The self-service route always derives its worker from Viewer.PersonID;
		// it does not accept an address-bar worker selector.
		personID = ""
	}
	filter := WorkflowFilterProps{
		Query: view.WorkflowQuery, Action: pageHref(target), PersonID: personID,
		DirectoryQuery: view.Query, DirectoryPage: view.PeoplePage, DirectoryTeam: view.PeopleTeam, DirectoryLocation: view.PeopleLocation,
		DirectorySort: view.PeopleSort, DirectoryDirection: view.PeopleDirection, NavCollapsed: view.NavCollapsed,
	}
	if view.Navigate != nil {
		filter.OnFilter = func(query string) {
			if target == PageMyself {
				view.Navigate(statefulHref(view, PageMyself, "workflow_q", query))
				return
			}
			view.Navigate(statefulHref(view, target, "person", person.ID, "q", view.Query,
				"team", view.PeopleTeam, "location", view.PeopleLocation, "sort", view.PeopleSort, "dir", view.PeopleDirection,
				"page", peoplePageValue(view.PeoplePage), "workflow_q", query))
		}
	}
	return filter
}

func peopleReturnHref(view View) string {
	return peopleDirectoryHref(view, view.PeoplePage, view.Query, view.PeopleTeam, view.PeopleLocation, view.PeopleEligibleOnly, view.PeopleSort, view.PeopleDirection)
}
