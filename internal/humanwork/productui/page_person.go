package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// personPage is the route adapter. It resolves authorized projection data and
// passes presentation-only props into the reusable component tree.
func personPage(view View) ui.Node {
	returnHref := peopleReturnHref(view)
	props := PersonPageProps{I18nProps: I18nProps{Locale: view.Locale}, BackHref: returnHref, Navigate: view.Navigate}
	person, ok := exactPerson(view)
	if !ok || !DiscoveryAdmitted(person.ID, view.RecordVerdicts) {
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
	value := func(raw string) string { return valueOrUnavailableFor(view.Locale, raw) }
	// fact renders one fact through the record's field verdict once the
	// server sends verdicts, honoring its disposition: HIDE omits the row
	// entirely. A silent server keeps the current values. Allowed values
	// project identically to value(), so governing changes nothing until
	// a verdict actually withholds.
	fields := view.RecordVerdicts[person.ID].Fields
	silent := len(view.RecordVerdicts) == 0
	fact := func(facts []ProfileFactProps, label, name, raw string) []ProfileFactProps {
		if silent {
			return append(facts, ProfileFactProps{Label: label, Value: value(raw)})
		}
		projected, admitted := ProjectField(view.Locale, raw, fields[name])
		if !admitted {
			return facts
		}
		return append(facts, ProfileFactProps{Label: label, Value: projected.Text})
	}
	// heroFact projects one hero string; a hidden hero field reads as
	// withheld so the admitted profile keeps its anchor.
	heroFact := func(name, raw string) string {
		if silent {
			return value(raw)
		}
		projected, admitted := ProjectField(view.Locale, raw, fields[name])
		if !admitted {
			return view.Locale.Text("provenance.value.withheld")
		}
		return projected.Text
	}
	details := fact(nil, text("person.worker_number"), "worker_number", person.WorkerNumber)
	details = fact(details, text("person.job_code"), "job_code", person.JobCode)
	details = fact(details, text("person.job_level"), "job_level", person.Grade)
	details = fact(details, text("person.hire_date"), "hire_date", person.HireDate)
	details = fact(details, text("person.employment_type"), "employment_type", "")
	details = fact(details, text("person.time_type"), "time_type", "")
	details = fact(details, text("person.record_source"), "record_source", person.Source)
	details = fact(details, text("person.record_created"), "record_created", person.CreatedAt)
	organization := fact(nil, text("person.organization_unit"), "organization_unit", person.Team)
	organization = fact(organization, text("person.manager"), "manager", person.Manager)
	organization = fact(organization, text("person.position_id"), "position_id", person.PositionID)
	organization = fact(organization, text("person.work_location"), "work_location", person.Location)
	organization = fact(organization, text("person.company"), "company", "")
	organization = fact(organization, text("person.business_unit"), "business_unit", "")
	organization = fact(organization, text("person.cost_center"), "cost_center", "")
	organization = fact(organization, text("person.work_arrangement"), "work_arrangement", "")
	compensation := fact(nil, text("person.base_pay"), "base_pay", money(view.Locale, person.BasePay))
	compensation = fact(compensation, text("person.bonus_target"), "bonus_target", percentage(view.Locale, person.BonusTarget))
	compensation = fact(compensation, text("person.pay_zone"), "pay_zone", person.PayZone)
	compensation = fact(compensation, text("person.pay_frequency"), "pay_frequency", "")
	personal := fact(nil, text("person.legal_name"), "legal_name", person.LegalName)
	personal = fact(personal, text("person.preferred_name"), "preferred_name", person.PreferredName)
	personal = fact(personal, text("person.worker_id"), "worker_id", person.WorkerID)
	personal = fact(personal, text("person.worker_ref"), "record_id", person.ID)
	return PersonProfileProps{
		Hero: PersonHeroProps{
			Initials: person.Initials, PhotoURL: person.PhotoURL, Name: heroFact("name", person.Name), Role: heroFact("role", person.Role),
			Status: text("person.visible_scope"), Source: heroFact("source", person.Source),
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
			text("person.history_detail", map[string]string{"name": person.Name}), true),
	}
}

func personWorkflowLauncherProps(view View, person Person, target PageID) WorkflowLauncherProps {
	filtered := filteredPersonWorkflows(view)
	if len(view.EffectivePermissions) > 0 && !view.Can(PageJourneys, "create") {
		filtered = nil
	}
	workflows := make([]WorkflowCardProps, 0, len(filtered))
	// PROMOUX-012: an open journey the view already holds for this worker
	// suppresses Start even when the availability verdict disagrees, so the
	// profile can never list an active promotion beside a duplicate start.
	activeItem, hasActiveJourney := activePromotionWorkItem(view, person.ID)
	for _, workflow := range filtered {
		if workflow.ID == "promotion" && (person.PromotionAvailability == PromotionActiveConflict || hasActiveJourney) {
			// PROMOUX-002 GREEN #3: Start becomes a link to the journey
			// already in flight, exactly as the People row does, instead of
			// disappearing with only a reason left behind.
			if hasActiveJourney {
				workflows = append(workflows, WorkflowCardProps{
					Name: view.Locale.Text("people.open_active_promotion"), Category: workflow.Category,
					Description: PromotionAvailabilityReason(view.Locale, PromotionActiveConflict),
					Href:        JourneyDetailHref(view, activeItem.ID), Navigate: view.Navigate,
				})
			}
			continue
		}
		if workflow.ID == "promotion" && !personPromotionEligible(person) {
			continue
		}
		href := workflow.Href
		if workflow.LaunchHref != nil {
			href = workflow.LaunchHref(person.ID)
		}
		workflows = append(workflows, WorkflowCardProps{
			Name: workflow.Name, Category: workflow.Category, Description: workflow.Description, Href: href, Navigate: view.Navigate,
		})
	}
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
	// GREEN #2: every displayed availability state carries a server-provided
	// reason -- including a viewer this page has already denied every
	// workflow to above, who previously saw a blank UnavailableDetail. The
	// local create-authority check is asked again here (matching the
	// `filtered = nil` gate above) rather than trusted from
	// person.PromotionAvailability alone, because a caller may construct a
	// Person directly (as component tests here do) without routing it
	// through the productclient projection that would otherwise have baked
	// the same authorization into the code.
	authorized := len(view.EffectivePermissions) == 0 || view.Can(PageJourneys, "create")
	unavailableDetail := ""
	switch {
	case !authorized:
		unavailableDetail = PromotionAvailabilityReason(view.Locale, PromotionWithheld)
	case hasActiveJourney:
		// The "Open active promotion" card above already carries continuity;
		// this is not also an empty-menu fallback.
	case !personPromotionEligible(person):
		unavailableDetail = PromotionAvailabilityReason(view.Locale, person.PromotionAvailability)
	}
	return WorkflowLauncherProps{
		UnavailableDetail: unavailableDetail,
		PersonName:        person.Name, TotalCount: len(workflows), Filter: filter, Workflows: workflows,
	}
}

func peopleReturnHref(view View) string {
	return peopleDirectoryHref(view, view.PeoplePage, view.Query, view.PeopleTeam, view.PeopleLocation, view.PeopleEligibleOnly, view.PeopleSort, view.PeopleDirection)
}
