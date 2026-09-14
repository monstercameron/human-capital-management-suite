package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PersonPageProps is the route-independent contract for a person surface.
// Exactly one of Profile or Unavailable should be non-nil.
type PersonPageProps struct {
	I18nProps
	BackHref    string
	Navigate    func(string)
	Profile     *PersonProfileProps
	Unavailable PersonUnavailableProps
}

// PersonUnavailableProps owns the recovery path for a missing projection.
type PersonUnavailableProps struct {
	I18nProps
	DirectoryHref string
	Navigate      func(string)
}

// PersonProfileProps composes the three profile regions.
type PersonProfileProps struct {
	Hero         PersonHeroProps
	Details      EmploymentDetailsProps
	Organization EmploymentDetailsProps
	Compensation EmploymentDetailsProps
	Personal     SensitiveDetailsProps
	ActiveWork   ActiveWorkflowsProps
	Workflows    WorkflowLauncherProps
	// Active is PROMOUX-012's in-progress workflows for this worker, shown
	// beside History (the past ones) on the person route.
	Active  PersonActiveWorkflowsProps
	History WorkflowHistoryProps
}

// ActiveWorkflowsProps is the compact, person-scoped view of non-terminal
// workflow records. It is intentionally separate from the launcher: existing
// work is read-only context, while the launcher starts a new governed flow.
type ActiveWorkflowsProps struct {
	I18nProps
	Title       string
	Description string
	Rows        []WorkRowProps
}

// PersonProfileCompositionProps keeps the shared profile layout independent
// from the Person and Myself route adapters.
type PersonProfileCompositionProps struct {
	I18nProps
	Profile PersonProfileProps
}

// PersonHeroProps contains only the identity facts shown by the hero.
type PersonHeroProps struct {
	I18nProps
	Initials   string
	PhotoURL   string
	Name       string
	Role       string
	NameStatus WorkerFactStatus
	RoleStatus WorkerFactStatus
}

// EmploymentDetailsProps is an ordered set of labelled facts.
type EmploymentDetailsProps struct {
	I18nProps
	Title       string
	Description string
	Class       string
	Notice      string
	Facts       []ProfileFactProps
}

// SensitiveDetailsProps is a closed-by-default disclosure for personal data.
// The page adapter remains responsible for supplying only authorized facts.
type SensitiveDetailsProps struct {
	I18nProps
	Title       string
	Description string
	Badge       string
	Facts       []ProfileFactProps
}

// ProfileFactProps is the smallest reusable profile datum.
type ProfileFactProps struct {
	I18nProps
	Label  string
	Value  string
	Status WorkerFactStatus
}

// WorkflowLauncherProps owns the workflow search and filtered cards.
type WorkflowLauncherProps struct {
	I18nProps
	PersonName        string
	Heading           string
	Description       string
	HideCount         bool
	TotalCount        int
	UnavailableDetail string
	Filter            WorkflowFilterProps
	Workflows         []WorkflowCardProps
}

// WorkflowFilterProps preserves both the selected person and the directory
// return context across progressive-enhancement and live submissions.
type WorkflowFilterProps struct {
	I18nProps
	Query              string
	Action             string
	PersonID           string
	DirectoryQuery     string
	DirectoryPage      int
	DirectoryTeam      string
	DirectoryLocation  string
	DirectorySort      string
	DirectoryDirection string
	NavCollapsed       bool
	OnFilter           func(string)
}

// WorkflowCardProps is the public presentation contract of one launcher.
type WorkflowCardProps struct {
	I18nProps
	Name        string
	Category    string
	Description string
	ActionLabel string
	Href        string
	Navigate    func(string)
}

// PersonPage renders a profile or a truthful unavailable state.
func PersonPage(props PersonPageProps) ui.Node {
	children := []ui.Node{
		softwareLink(props.Navigate, html.Props{Class: "back-link"}, props.BackHref, ui.Text(props.Text("person.return_directory"))),
	}
	if props.Profile == nil {
		missing := PersonUnavailableProps{I18nProps: props.I18nProps, DirectoryHref: props.BackHref, Navigate: props.Navigate}
		if props.Unavailable.DirectoryHref != "" || props.Unavailable.Navigate != nil {
			missing = props.Unavailable
			missing.I18nProps = props.I18nProps
		}
		children = append(children, ui.CreateElement(PersonUnavailable, missing))
		return html.Div(html.Props{Class: "person-page"}, children...)
	}
	children = append(children, ui.CreateElement(PersonProfileComposition, PersonProfileCompositionProps{
		I18nProps: props.I18nProps, Profile: *props.Profile,
	}))
	return html.Div(html.Props{Class: "person-page"}, children...)
}

// PersonProfileComposition renders the reusable profile body shared by the
// directory profile and authenticated self-service surface.
func PersonProfileComposition(props PersonProfileCompositionProps) ui.Node {
	profile := props.Profile
	profile.Hero.I18nProps = props.I18nProps
	profile.Details.I18nProps = props.I18nProps
	profile.Organization.I18nProps = props.I18nProps
	profile.Compensation.I18nProps = props.I18nProps
	profile.Personal.I18nProps = props.I18nProps
	profile.ActiveWork.I18nProps = props.I18nProps
	profile.Workflows.I18nProps = props.I18nProps
	profile.History.I18nProps = props.I18nProps
	profile.Active.I18nProps = props.I18nProps
	// The active-requests section already gives each journey its contextual
	// resume link. Do not repeat that same destination in the workflow picker.
	if len(profile.Active.Items) > 0 {
		activeHref := make(map[string]bool, len(profile.Active.Items))
		for _, item := range profile.Active.Items {
			activeHref[item.Href] = true
		}
		available := make([]WorkflowCardProps, 0, len(profile.Workflows.Workflows))
		for _, card := range profile.Workflows.Workflows {
			if !activeHref[card.Href] {
				available = append(available, card)
			}
		}
		profile.Workflows.Workflows = available
	}
	return html.Div(html.Props{Class: "person-profile-composition"},
		ui.CreateElement(PersonProfileHeader, profile.Hero),
		html.Div(html.Props{Class: "person-layout"},
			html.Div(html.Props{Class: "person-detail-stack"},
				ui.CreateElement(EmploymentDetails, profile.Details),
				ui.CreateElement(EmploymentDetails, profile.Organization),
				ui.CreateElement(EmploymentDetails, profile.Compensation),
				ui.CreateElement(SensitiveDetails, profile.Personal),
			),
			activeWorkNode(profile.ActiveWork),
			ui.CreateElement(WorkflowLauncher, profile.Workflows),
		),
		ui.CreateElement(PersonActiveWorkflows, profile.Active),
		ui.CreateElement(WorkflowHistory, profile.History),
	)
}

func activeWorkNode(props ActiveWorkflowsProps) ui.Node {
	if len(props.Rows) == 0 {
		return ui.Text("")
	}
	return ui.CreateElement(ActiveWorkflows, props)
}

// PersonUnavailable renders no worker data and offers one safe recovery path.
func PersonUnavailable(props PersonUnavailableProps) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title: props.Text("person.unavailable"), Description: props.Text("person.unavailable_detail"), Role: "status",
		Action: &ActionLinkProps{Label: props.Text("person.return_directory"), Href: props.DirectoryHref, Class: "button secondary", Navigate: props.Navigate},
	})
}

// PersonProfileHeader renders the employee identity without raw record metadata.
func PersonProfileHeader(props PersonHeroProps) ui.Node {
	return html.Section(html.Props{Class: "surface person-hero", Aria: map[string]string{"label": props.Text("person.summary")}},
		html.Div(html.Props{Class: "person-identity"},
			personAvatar(props.Name, props.Initials, props.PhotoURL, "profile"),
			html.Div(html.Props{},
				html.Span(html.Props{Class: "eyebrow"}, ui.Text(props.Text("person.worker_profile"))),
				html.H2(html.Props{}, ui.Text(props.Name)),
				html.P(html.Props{Class: "muted"}, ui.Text(props.Role)),
			),
		),
	)
}

// EmploymentDetails renders a stable section around an extensible fact list.
func EmploymentDetails(props EmploymentDetailsProps) ui.Node {
	title := props.Title
	if title == "" {
		title = props.Text("person.employment_details")
	}
	description := props.Description
	if description == "" {
		description = props.Text("person.employment_detail")
	}
	missingCount := 0
	for _, fact := range props.Facts {
		if fact.Status == WorkerFactMissing {
			missingCount++
		}
	}
	facts := make([]ui.Node, 0, len(props.Facts))
	missingFacts := make([]ui.Node, 0, missingCount)
	for _, fact := range props.Facts {
		fact.I18nProps = props.I18nProps
		if missingCount >= 2 && fact.Status == WorkerFactMissing {
			missingFacts = append(missingFacts, ui.CreateElement(ProfileFact, fact))
			continue
		}
		facts = append(facts, ui.CreateElement(ProfileFact, fact))
	}
	// One absent field is easier to scan in place. When several are absent,
	// keep the authorized field names available in a native disclosure while
	// letting the known facts lead the profile.
	class := "surface person-details"
	if props.Class != "" {
		class += " " + props.Class
	}
	children := []ui.Node{
		ui.CreateElement(SectionHeading, SectionHeadingProps{Title: title, Description: description, ShowDescription: true}),
	}
	if len(facts) > 0 {
		children = append(children, html.Tag("dl", html.Props{Class: "person-fact-grid"}, facts...))
	}
	if len(missingFacts) > 0 {
		children = append(children, html.Details(html.Props{Class: "profile-missing-details"},
			html.Summary(html.Props{Class: "profile-missing-summary"},
				ui.Text(props.Locale.Plural("person.unreported_fields", int64(missingCount))),
				productIcon("expand", "profile-missing-chevron"),
			),
			html.Tag("dl", html.Props{Class: "person-fact-grid"}, missingFacts...),
		))
	}
	if props.Notice != "" {
		children = append(children, html.P(html.Props{Class: "profile-data-boundary", Raw: map[string]any{"role": "note"}}, ui.Text(props.Notice)))
	}
	return html.Section(html.Props{Class: class}, children...)
}

// SensitiveDetails keeps personal identifiers outside the default reading
// path. A user must deliberately open the native disclosure to view them.
func SensitiveDetails(props SensitiveDetailsProps) ui.Node {
	facts := make([]ui.Node, 0, len(props.Facts))
	for _, fact := range props.Facts {
		fact.I18nProps = props.I18nProps
		facts = append(facts, ui.CreateElement(ProfileFact, fact))
	}
	badge := props.Badge
	if badge == "" {
		badge = props.Text("person.sensitive")
	}
	return html.Details(html.Props{Class: "surface sensitive-details"},
		html.Summary(html.Props{Class: "sensitive-summary"},
			html.Div(html.Props{Class: "sensitive-heading"},
				html.Span(html.Props{Class: "privacy-icon", Aria: map[string]string{"hidden": "true"}}, productIcon("privacy", "privacy-icon-glyph")),
				html.Div(html.Props{},
					html.H2(html.Props{}, ui.Text(props.Title)),
					html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
				),
			),
			html.Span(html.Props{Class: "status privacy-badge"}, ui.Text(badge)),
			productIcon("expand", "sensitive-summary-chevron"),
		),
		html.Div(html.Props{Class: "privacy-notice", Raw: map[string]any{"role": "note"}},
			html.Strong(html.Props{}, ui.Text(props.Text("person.private_data"))),
			html.Span(html.Props{}, ui.Text(props.Text("person.private_detail"))),
		),
		html.Tag("dl", html.Props{Class: "person-fact-grid sensitive-fact-grid"}, facts...),
	)
}

// ProfileFact renders one label/value pair.
func ProfileFact(props ProfileFactProps) ui.Node {
	children := []ui.Node{ui.Text(props.Value)}
	// The localized stand-in already tells readers that this source value is
	// absent. A second "Not supplied" badge adds noise to sparse profiles.
	// Unknown and withheld verdicts remain explicit because they mean
	// something different from an unreported source value.
	redundantMissing := props.Status == WorkerFactMissing && props.Value == props.Text("common.not_reported")
	if props.Status != "" && props.Status != WorkerFactPresent && !redundantMissing {
		statusKey := "person.fact_status." + strings.ToLower(string(props.Status))
		children = append(children, html.Span(html.Props{Class: "profile-fact-status status fact-" + strings.ToLower(string(props.Status))}, ui.Text(props.Text(statusKey))))
	}
	return html.Div(html.Props{Class: "profile-fact", Data: map[string]string{"fact-status": string(props.Status)}},
		html.Tag("dt", html.Props{}, ui.Text(props.Label)),
		html.Tag("dd", html.Props{}, children...),
	)
}

// ActiveWorkflows renders already-open work for this employee before the
// separate new-work launcher. The empty state is truthful: no synthetic work
// item is created merely to fill the profile.
func ActiveWorkflows(props ActiveWorkflowsProps) ui.Node {
	rows := make([]ui.Node, 0, len(props.Rows))
	for _, row := range props.Rows {
		row.I18nProps = props.I18nProps
		rows = append(rows, ui.CreateElement(WorkRow, row))
	}
	if len(rows) == 0 {
		rows = append(rows, html.Li(html.Props{Class: "collection-empty"},
			html.Strong(html.Props{}, ui.Text(props.Text("work.empty_title"))),
			html.Small(html.Props{}, ui.Text(props.Text("work.empty_detail"))),
		))
	}
	title := props.Title
	if title == "" {
		title = props.Text("work.all")
	}
	return html.Section(html.Props{Class: "surface active-workflows", Aria: map[string]string{"label": title}},
		ui.CreateElement(SectionHeading, SectionHeadingProps{
			Title: title, Description: props.Description, ShowDescription: true,
			Trailing: html.Span(html.Props{Class: "count"}, ui.Text(props.Locale.Plural("work.item_count", int64(len(props.Rows))))),
		}),
		html.Ul(html.Props{Class: "work-rows", Raw: map[string]any{"role": "list"}}, rows...),
	)
}

// WorkflowLauncher composes search, empty state, and workflow cards.
func WorkflowLauncher(props WorkflowLauncherProps) ui.Node {
	props.Filter.I18nProps = props.I18nProps
	results := make([]ui.Node, 0, len(props.Workflows))
	for _, workflow := range props.Workflows {
		workflow.I18nProps = props.I18nProps
		results = append(results, ui.CreateElement(WorkflowCard, workflow))
	}
	if len(results) == 0 {
		title, detail := props.Text("workflow.none"), props.Text("workflow.none_detail")
		if props.TotalCount == 0 {
			title, detail = props.Text("workflow.unavailable"), props.Text("workflow.unavailable_detail")
			if props.UnavailableDetail != "" {
				detail = props.UnavailableDetail
			}
		}
		results = append(results, html.Div(html.Props{Class: "workflow-empty", Raw: map[string]any{"role": "status"}},
			html.Strong(html.Props{}, ui.Text(title)),
			html.P(html.Props{Class: "muted"}, ui.Text(detail)),
		))
	}
	headingText := props.Heading
	if headingText == "" {
		headingText = props.Text("workflow.start")
	}
	description := props.Description
	if description == "" {
		description = props.Text("workflow.choose", map[string]string{"name": props.PersonName})
	}
	var trailing ui.Node
	if props.TotalCount > 0 && !props.HideCount {
		trailing = html.Span(html.Props{Class: "count"}, ui.Text(props.Locale.Plural("workflow.available_count", int64(props.TotalCount))))
	}
	children := []ui.Node{ui.CreateElement(SectionHeading, SectionHeadingProps{
		Title: headingText, Description: description, ShowDescription: true, Class: "workflow-heading", Trailing: trailing,
	})}
	if props.TotalCount > 0 {
		children = append(children, ui.CreateElement(WorkflowFilter, props.Filter))
	}
	children = append(children, html.Div(html.Props{Class: "workflow-results"}, results...))
	return html.Section(html.Props{Class: "surface workflow-launcher", Aria: map[string]string{"label": props.Text("workflow.available_aria")}}, children...)
}

// WorkflowFilter is an SSR-safe GET filter with an optional live callback.
func WorkflowFilter(props WorkflowFilterProps) ui.Node {
	query := props.Query
	input := SearchInputProps{ID: "workflow-search", Name: "workflow_q", Value: props.Query,
		Placeholder: props.Text("workflow.filter_placeholder"), AriaLabel: props.Text("workflow.filter_aria")}
	formProps := html.Props{Class: "workflow-search", Action: props.Action, Method: "get", Raw: map[string]any{"role": "search"}}
	if props.OnFilter != nil {
		input.OnInput = func(value string) { query = value }
		onFilter := props.OnFilter
		formProps.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			onFilter(query)
		})
	}
	children := []ui.Node{
		html.Label(html.Props{For: "workflow-search"}, ui.Text(props.Text("workflow.find"))),
		html.Div(html.Props{Class: "workflow-search-control"},
			ui.CreateElement(SearchInput, input),
			html.Button(html.Props{Class: "button secondary", Type: "submit"}, ui.Text(props.Text("workflow.filter"))),
		),
	}
	if props.PersonID != "" {
		children = append(children, html.Tag("input", html.Props{Name: "person", Value: props.PersonID, Raw: map[string]any{"type": "hidden"}}))
	}
	if locale := props.Locale.normalized(); locale.Resolved != DefaultProductLocale {
		children = append(children, html.Tag("input", html.Props{Name: "locale", Value: locale.Resolved, Raw: map[string]any{"type": "hidden"}}))
	}
	if props.DirectoryQuery != "" {
		children = append(children, html.Tag("input", html.Props{Name: "q", Value: props.DirectoryQuery, Raw: map[string]any{"type": "hidden"}}))
	}
	if props.DirectoryPage > 1 {
		children = append(children, html.Tag("input", html.Props{Name: "page", Value: peoplePageValue(props.DirectoryPage), Raw: map[string]any{"type": "hidden"}}))
	}
	for _, field := range []struct{ name, value string }{
		{"team", props.DirectoryTeam}, {"location", props.DirectoryLocation}, {"sort", props.DirectorySort}, {"dir", props.DirectoryDirection},
	} {
		if field.value != "" {
			children = append(children, html.Tag("input", html.Props{Name: field.name, Value: field.value, Raw: map[string]any{"type": "hidden"}}))
		}
	}
	if props.NavCollapsed {
		children = append(children, html.Tag("input", html.Props{Name: "nav", Value: "collapsed", Raw: map[string]any{"type": "hidden"}}))
	}
	return html.Form(formProps, children...)
}

// WorkflowCard renders one governed workflow launcher.
func WorkflowCard(props WorkflowCardProps) ui.Node {
	actionLabel := props.ActionLabel
	if actionLabel == "" {
		actionLabel = props.Text("workflow.start_named", map[string]string{"name": props.Name})
	}
	return html.Article(html.Props{Class: "workflow-card"},
		html.Div(html.Props{Class: "workflow-icon", Aria: map[string]string{"hidden": "true"}}, productIcon("launch", "workflow-icon-glyph")),
		html.Div(html.Props{Class: "workflow-copy"},
			html.Span(html.Props{Class: "eyebrow"}, ui.Text(props.Category)),
			html.H3(html.Props{}, ui.Text(props.Name)),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
		),
		softwareLink(props.Navigate, html.Props{Class: "button primary"}, props.Href, ui.Text(actionLabel)),
	)
}
