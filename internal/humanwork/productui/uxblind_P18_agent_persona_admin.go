package productui

import (
	"context"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PagePersonaAdmin is the administrator-only persona catalogue. Its runtime
// projection contains policy metadata only; task content is intentionally not
// part of this contract.
const PagePersonaAdmin PageID = "persona-admin"

type PersonaAdminLoadState string

const (
	PersonaAdminLoading     PersonaAdminLoadState = "loading"
	PersonaAdminReady       PersonaAdminLoadState = "ready"
	PersonaAdminUnavailable PersonaAdminLoadState = "unavailable"
)

type PersonaAdminLifecycle string

const (
	PersonaDraft     PersonaAdminLifecycle = "DRAFT"
	PersonaInReview  PersonaAdminLifecycle = "IN_REVIEW"
	PersonaPublished PersonaAdminLifecycle = "PUBLISHED"
	PersonaSuspended PersonaAdminLifecycle = "SUSPENDED"
	PersonaRetired   PersonaAdminLifecycle = "RETIRED"
)

// PersonaAdminSkill is derived from the pinned skill registry entry. Data
// classes are disclosure metadata, not a second authorization decision.
type PersonaAdminSkill struct {
	ID          string
	Name        string
	Tier        string
	DataClasses []string
}

type PersonaAdminInstallation struct {
	Version        string
	ConversationID string
	Conversation   string
	Kind           string
	Audience       string
	ReplyPlacement string
}

type PersonaAdminLimits struct {
	InvocationsPerHour string
	ConcurrentTasks    string
	DailySpend         string
}

type PersonaAdminPersona struct {
	ID             string
	StarterID      string
	StarterVersion uint32
	Handle         string
	Name           string
	Purpose        string
	Lifecycle      PersonaAdminLifecycle
	Owner          string
	Steward        string
	Version        string
	Skills         []PersonaAdminSkill
	DerivedData    []string
	ChannelClasses []string
	Audience       string
	Installations  []PersonaAdminInstallation
	Limits         PersonaAdminLimits
	ReviewRequired bool
	ReviewApproved bool
	Reviewer       string
	EvaluationRef  string
}

type PersonaAdminTarget struct {
	ID    string
	Label string
}

// PersonaAdminStarter is a server-approved starter backed by a published
// manifest, active skill grants, and a bounded channel ceiling.
type PersonaAdminStarter struct {
	ID             string
	Version        uint32
	Name           string
	Handle         string
	Purpose        string
	ManifestID     string
	ChannelClasses []string
	SkillGrantIDs  []string
}

// PersonaAdminStarterCatalog reports whether the trusted starter catalog is
// connected and includes only starters with current manifest and grant pins.
type PersonaAdminStarterCatalog struct {
	Available bool
	Starters  []PersonaAdminStarter
}

// PersonaAdminStarterSource resolves ready-to-use starters for the admitted
// tenant principal. Implementations must derive authority from the request
// context and verify the tenant and principal fields against that context.
type PersonaAdminStarterSource interface {
	PersonaAdminStarterCatalog(context.Context, PersonaAdminSnapshotRequest) (PersonaAdminStarterCatalog, error)
}

type PersonaAdminPreview struct {
	Subject             string
	Conversation        string
	ConversationKind    string
	Audience            string
	ReplyPlacement      string
	EffectiveSkills     []PersonaAdminSkill
	DerivedData         []string
	Warnings            []string
	AuthorizationReason string
}

// PersonaAdminSnapshot is the minimum server-owned projection needed by the
// catalogue. It deliberately has no posts, prompts, run steps, or artifacts.
type PersonaAdminSnapshot struct {
	Available                   bool
	LocalDevBootstrapAvailable  bool
	CommandPermissionsAvailable bool
	AllowedCommands             []string
	CommandStatus               string
	PreviewPersonaID            string
	PreviewSubjectID            string
	PreviewConversationID       string
	PreviewUnavailable          bool
	Personas                    []PersonaAdminPersona
	StarterCatalogAvailable     bool
	Starters                    []PersonaAdminStarter
	Preview                     PersonaAdminPreview
	SubjectOptions              []PersonaAdminTarget
	Conversations               []PersonaAdminTarget
}

type PersonaAdminSnapshotRequest struct {
	TenantID  string
	Principal string
}

type PersonaAdminPreviewRequest struct {
	PersonaID      string
	SubjectID      string
	ConversationID string
}

// PersonaAdminClient is the seam for the agent-owned store and review
// service. Implementations must re-authorize every action server-side.
type PersonaAdminClient interface {
	Snapshot(context.Context, PersonaAdminSnapshotRequest) (PersonaAdminSnapshot, error)
	Preview(context.Context, PersonaAdminPreviewRequest) (PersonaAdminPreview, error)
	RequestReview(string) error
	PublishPersona(string) error
	RollbackPersona(string) error
	SuspendPersona(string) error
	RetirePersona(string) error
}

type PersonaAdminPageProps struct {
	I18nProps
	State             PersonaAdminLoadState
	UnavailableReason string
	Snapshot          PersonaAdminSnapshot
	Client            PersonaAdminClient
}

// PersonaAdminReviewClient records an explicit independently authorized decision.
type PersonaAdminReviewClient interface {
	ReviewPersona(string, string) error
}

type personaAdminPageModuleRenderer struct{}

func (personaAdminPageModuleRenderer) Render(view View) ui.Node {
	return BuildPersonaAdminPage(view, view.PersonaAdminClient)
}

func (personaAdminPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("persona_catalog", "Persona catalog", "Review persona lifecycle and reach metadata", true, false, false, false),
		feature("persona_publication", "Persona publication", "Publish, rollback, suspend, or retire reviewed personas", true, true, true, false),
		feature("persona_preview", "Persona reach preview", "Preview effective skills for a user and conversation", true, false, false, false),
	}
}

// BuildPersonaAdminPage composes the authenticated server projection. A
// denied page never calls the client, which prevents unauthorized discovery
// through a client-side unavailable/loading distinction.
func BuildPersonaAdminPage(view View, client PersonaAdminClient) ui.Node {
	locale := view.Locale
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	if !view.Can(PagePersonaAdmin, "view") {
		return personaAdminUnavailable(locale, personaAdminText(locale, "permission_denied"))
	}
	if client == nil {
		return personaAdminUnavailable(locale, personaAdminText(locale, "service_unavailable"))
	}
	snapshot, err := client.Snapshot(context.Background(), PersonaAdminSnapshotRequest{TenantID: view.Tenant, Principal: view.Principal})
	if err != nil || !snapshot.Available {
		return personaAdminUnavailable(locale, personaAdminText(locale, "service_unavailable"))
	}
	return PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: snapshot, Client: client})
}

func PersonaAdminPage(props PersonaAdminPageProps) ui.Node {
	locale := props.Locale
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	if props.State == "" {
		props.State = PersonaAdminUnavailable
	}
	if props.State == PersonaAdminLoading {
		return personaAdminStatus(locale, PersonaAdminLoading, "")
	}
	if props.State == PersonaAdminUnavailable {
		detail := strings.TrimSpace(props.UnavailableReason)
		if detail == "" {
			detail = personaAdminText(locale, "service_unavailable")
		}
		return personaAdminStatus(locale, PersonaAdminUnavailable, detail)
	}

	groups := make(map[PersonaAdminLifecycle][]PersonaAdminPersona)
	for _, persona := range props.Snapshot.Personas {
		groups[persona.Lifecycle] = append(groups[persona.Lifecycle], persona)
	}
	ordered := []PersonaAdminLifecycle{PersonaDraft, PersonaInReview, PersonaPublished, PersonaSuspended, PersonaRetired}
	sections := make([]ui.Node, 0, len(ordered))
	for _, lifecycle := range ordered {
		personas := groups[lifecycle]
		if len(personas) == 0 {
			continue
		}
		cards := make([]ui.Node, 0, len(personas))
		for _, persona := range personas {
			cards = append(cards, personaAdminCard(locale, props.Client, persona, props.Snapshot))
		}
		sections = append(sections, html.Section(html.Props{Class: "persona-admin-lifecycle", Raw: map[string]any{"data-lifecycle": string(lifecycle), "aria-labelledby": "persona-admin-lifecycle-" + safeAgentDOMToken(string(lifecycle))}},
			html.H2(html.Props{ID: "persona-admin-lifecycle-" + safeAgentDOMToken(string(lifecycle))}, ui.Text(personaAdminText(locale, "lifecycle_"+strings.ToLower(string(lifecycle))))),
			html.Div(html.Props{Class: "persona-admin-cards"}, cards...),
		))
	}
	if len(sections) == 0 {
		sections = append(sections, html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(personaAdminText(locale, "empty"))))
	}

	return html.Section(html.Props{Class: "persona-admin-page", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": "persona-admin-title"}, Raw: map[string]any{"data-persona-admin-state": string(props.State)}},
		html.Div(html.Props{Class: "persona-admin-hero"},
			html.P(html.Props{Class: "eyebrow"}, ui.Text(personaAdminText(locale, "eyebrow"))),
			html.H1(html.Props{ID: "persona-admin-title"}, ui.Text(personaAdminText(locale, "title"))),
			html.P(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "description"))),
		),
		html.Section(html.Props{Class: "persona-admin-catalog", Aria: map[string]string{"labelledby": "persona-admin-catalog-title"}},
			html.Div(html.Props{Class: "section-head"}, html.H2(html.Props{ID: "persona-admin-catalog-title"}, ui.Text(personaAdminText(locale, "catalog_title"))), html.P(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "catalog_detail")))),
			html.Div(html.Props{Class: "persona-admin-lifecycle-list"}, sections...),
		),
		PersonaAdminEditor(locale, props.Client, props.Snapshot),
		personaAdminPreview(locale, props.Client, props.Snapshot),
	)
}

func personaAdminStatus(locale LocaleContext, state PersonaAdminLoadState, detail string) ui.Node {
	if state == PersonaAdminLoading {
		return html.Section(html.Props{Class: "persona-admin-page", Raw: map[string]any{"data-persona-admin-state": string(state), "role": "status", "aria-live": "polite"}},
			html.H1(html.Props{ID: "persona-admin-title"}, ui.Text(personaAdminText(locale, "title"))), html.P(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "loading"))))
	}
	return html.Section(html.Props{Class: "persona-admin-page", Raw: map[string]any{"data-persona-admin-state": string(state)}},
		html.H1(html.Props{ID: "persona-admin-title"}, ui.Text(personaAdminText(locale, "title"))),
		ui.CreateElement(EmptyState, EmptyStateProps{Title: personaAdminText(locale, "unavailable_title"), Description: detail, Role: "status", Class: "persona-admin-unavailable"}),
	)
}

func personaAdminUnavailable(locale LocaleContext, detail string) ui.Node {
	return personaAdminStatus(locale, PersonaAdminUnavailable, detail)
}

func personaAdminCard(locale LocaleContext, client PersonaAdminClient, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) ui.Node {
	id := "persona-admin-" + safeAgentDOMToken(persona.ID)
	skillNodes := make([]ui.Node, 0, len(persona.Skills))
	for _, skill := range persona.Skills {
		dataClass := strings.Join(skill.DataClasses, ", ")
		if dataClass == "" {
			dataClass = personaAdminText(locale, "not_reported")
		}
		skillNodes = append(skillNodes, html.Li(html.Props{Class: "persona-admin-skill", Raw: map[string]any{"data-skill-tier": strings.ToUpper(skill.Tier)}},
			html.Strong(html.Props{}, ui.Text(skill.Name)), html.Span(html.Props{Class: "status"}, ui.Text(strings.ToUpper(skill.Tier))), html.Small(html.Props{Class: "muted"}, ui.Text(dataClass))))
	}
	if len(skillNodes) == 0 {
		skillNodes = append(skillNodes, html.Li(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "no_skills"))))
	}
	dataClasses := strings.Join(persona.DerivedData, ", ")
	if dataClasses == "" {
		dataClasses = personaAdminText(locale, "not_reported")
	}
	installations := make([]ui.Node, 0, len(persona.Installations))
	for _, installation := range persona.Installations {
		placement := strings.TrimSpace(installation.ReplyPlacement)
		if placement == "" {
			placement = personaAdminText(locale, "not_reported")
		}
		detail := strings.TrimSpace(installation.Kind) + " · " + placement
		if installation.Version != "" {
			detail = personaAdminText(locale, "version") + " " + installation.Version + " · " + detail
		}
		installations = append(installations, html.Li(html.Props{Class: "persona-admin-installation", Raw: map[string]any{"data-conversation-id": installation.ConversationID, "data-persona-version": installation.Version}},
			html.Strong(html.Props{}, ui.Text(installation.Conversation)), html.Small(html.Props{Class: "muted"}, ui.Text(detail)), html.Span(html.Props{Class: "muted"}, ui.Text(installation.Audience))))
	}
	if len(installations) == 0 {
		installations = append(installations, html.Li(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "no_installations"))))
	}
	actions := personaAdminLifecycleActions(locale, client, persona, snapshot)
	editorClient := client
	if !personaAdminCommandAllowed(snapshot, "CREATE_VERSION") {
		editorClient = nil
	}
	review := personaAdminText(locale, "review_not_required")
	if persona.ReviewRequired && !persona.ReviewApproved {
		review = personaAdminText(locale, "review_required")
	} else if persona.ReviewApproved {
		review = personaAdminText(locale, "review_approved")
	}
	return html.Article(html.Props{Class: "surface persona-admin-card", Aria: map[string]string{"labelledby": id + "-title"}, Raw: map[string]any{"data-persona-id": persona.ID, "data-lifecycle": string(persona.Lifecycle)}},
		html.Div(html.Props{Class: "persona-admin-card-heading"}, html.Div(html.Props{}, html.H3(html.Props{ID: id + "-title"}, ui.Text(persona.Name)), html.Small(html.Props{Class: "muted"}, ui.Text(persona.Handle+" · "+personaAdminText(locale, "version")+" "+persona.Version))), html.Span(html.Props{Class: "status"}, ui.Text(string(persona.Lifecycle)))),
		html.P(html.Props{Class: "persona-admin-purpose"}, ui.Text(persona.Purpose)),
		html.Div(html.Props{Class: "persona-admin-facts"},
			personaAdminFact(locale, "owner", persona.Owner), personaAdminFact(locale, "steward", persona.Steward), personaAdminFact(locale, "audience", persona.Audience), personaAdminFact(locale, "data_reach", dataClasses), personaAdminFact(locale, "limits", personaAdminLimits(locale, persona.Limits)),
		),
		html.H4(html.Props{}, ui.Text(personaAdminText(locale, "skills"))), html.Ul(html.Props{Class: "persona-admin-skill-list"}, skillNodes...),
		html.H4(html.Props{}, ui.Text(personaAdminText(locale, "placements"))), html.Ul(html.Props{Class: "persona-admin-installation-list"}, installations...),
		html.Div(html.Props{Class: "persona-admin-review", Raw: map[string]any{"role": "status", "data-review-approved": persona.ReviewApproved}}, html.Strong(html.Props{}, ui.Text(personaAdminText(locale, "review_step"))), html.Span(html.Props{Class: "muted"}, ui.Text(review)), personaAdminReviewDetail(locale, persona)),
		html.Div(html.Props{Class: "persona-admin-actions"}, actions...),
		personaAdminVersionEditor(locale, editorClient, persona),
	)
}

func personaAdminLimits(locale LocaleContext, limits PersonaAdminLimits) string {
	values := make([]string, 0, 3)
	for _, value := range []string{limits.InvocationsPerHour, limits.ConcurrentTasks, limits.DailySpend} {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return personaAdminText(locale, "not_reported")
	}
	return strings.Join(values, " · ")
}

func personaAdminFact(locale LocaleContext, label, value string) ui.Node {
	if strings.TrimSpace(value) == "" {
		value = personaAdminText(locale, "not_reported")
	}
	return html.Div(html.Props{Class: "persona-admin-fact"}, html.Small(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, label))), html.Strong(html.Props{}, ui.Text(value)))
}

func personaAdminReviewDetail(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	details := make([]string, 0, 2)
	if strings.TrimSpace(persona.Reviewer) != "" {
		details = append(details, personaAdminText(locale, "reviewer")+": "+persona.Reviewer)
	}
	if strings.TrimSpace(persona.EvaluationRef) != "" {
		details = append(details, personaAdminText(locale, "evaluation")+": "+persona.EvaluationRef)
	} else if persona.ReviewApproved {
		details = append(details, personaAdminText(locale, "evaluation_required"))
	}
	if len(details) == 0 {
		return html.Span(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "review_detail_unavailable")))
	}
	return html.Span(html.Props{Class: "muted"}, ui.Text(strings.Join(details, " · ")))
}

func personaAdminCommandAllowed(snapshot PersonaAdminSnapshot, action string) bool {
	if !snapshot.CommandPermissionsAvailable {
		return true
	}
	for _, allowed := range snapshot.AllowedCommands {
		if allowed == action {
			return true
		}
	}
	return false
}

func personaAdminLifecycleActions(locale LocaleContext, client PersonaAdminClient, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) []ui.Node {
	if client == nil {
		return []ui.Node{html.Small(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "actions_unavailable")))}
	}
	actions := make([]ui.Node, 0, 5)
	if persona.Lifecycle == PersonaDraft && !persona.ReviewApproved {
		actions = append(actions, html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: !personaAdminCommandAllowed(snapshot, "REQUEST_REVIEW"), Raw: map[string]any{"data-persona-command": "REQUEST_REVIEW"}, OnClick: ui.UseEvent(func(ui.MouseEvent) { _ = client.RequestReview(persona.ID) })}, ui.Text(personaAdminText(locale, "request_review"))))
	}
	if reviewer, ok := client.(PersonaAdminReviewClient); ok && persona.Lifecycle == PersonaInReview && !persona.ReviewApproved {
		for _, decision := range []string{"APPROVE", "REJECT"} {
			decision := decision
			actions = append(actions, html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: !personaAdminCommandAllowed(snapshot, "REVIEW"), Raw: map[string]any{"data-persona-command": "REVIEW", "data-persona-review-decision": decision}, OnClick: ui.UseEvent(func(ui.MouseEvent) { _ = reviewer.ReviewPersona(persona.ID, decision) })}, ui.Text(personaAdminText(locale, strings.ToLower(decision)))))
		}
	}
	actions = append(actions,
		html.Button(html.Props{Class: "button primary", Type: "button", Disabled: !personaAdminCommandAllowed(snapshot, "PUBLISH") || !persona.ReviewApproved || persona.EvaluationRef == "" || persona.Lifecycle == PersonaRetired, Raw: map[string]any{"data-persona-command": "PUBLISH"}, OnClick: ui.UseEvent(func(ui.MouseEvent) { _ = client.PublishPersona(persona.ID) })}, ui.Text(personaAdminText(locale, "publish"))),
		html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: !personaAdminCommandAllowed(snapshot, "ROLLBACK") || (persona.Lifecycle != PersonaPublished && persona.Lifecycle != PersonaSuspended), Raw: map[string]any{"data-persona-command": "ROLLBACK"}, OnClick: ui.UseEvent(func(ui.MouseEvent) { _ = client.RollbackPersona(persona.ID) })}, ui.Text(personaAdminText(locale, "rollback"))),
		html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: !personaAdminCommandAllowed(snapshot, "SUSPEND") || persona.Lifecycle != PersonaPublished, Raw: map[string]any{"data-persona-command": "SUSPEND"}, OnClick: ui.UseEvent(func(ui.MouseEvent) { _ = client.SuspendPersona(persona.ID) })}, ui.Text(personaAdminText(locale, "suspend"))),
		html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: !personaAdminCommandAllowed(snapshot, "RETIRE") || persona.Lifecycle == PersonaRetired, Raw: map[string]any{"data-persona-command": "RETIRE"}, OnClick: ui.UseEvent(func(ui.MouseEvent) { _ = client.RetirePersona(persona.ID) })}, ui.Text(personaAdminText(locale, "retire"))),
	)
	return actions
}

func personaAdminPreview(locale LocaleContext, client PersonaAdminClient, snapshot PersonaAdminSnapshot) ui.Node {
	preview := snapshot.Preview
	personas := make([]ui.Node, 0, len(snapshot.Personas))
	selected := snapshot.PreviewPersonaID
	if selected == "" && len(snapshot.Personas) > 0 {
		selected = snapshot.Personas[0].ID
	}
	for _, persona := range snapshot.Personas {
		personas = append(personas, html.Option(html.Props{Value: persona.ID, Selected: persona.ID == selected}, ui.Text(persona.Name)))
	}
	users := make([]ui.Node, 0, len(snapshot.SubjectOptions))
	for _, option := range snapshot.SubjectOptions {
		selected := option.ID == snapshot.PreviewSubjectID
		if snapshot.PreviewSubjectID == "" {
			selected = option.ID == preview.Subject || option.Label == preview.Subject
		}
		users = append(users, html.Option(html.Props{Value: option.ID, Selected: selected}, ui.Text(option.Label)))
	}
	conversations := make([]ui.Node, 0, len(snapshot.Conversations))
	for _, option := range snapshot.Conversations {
		selected := option.ID == snapshot.PreviewConversationID
		if snapshot.PreviewConversationID == "" {
			selected = option.ID == preview.Conversation || option.Label == preview.Conversation
		}
		conversations = append(conversations, html.Option(html.Props{Value: option.ID, Selected: selected}, ui.Text(option.Label)))
	}
	if len(users) == 0 {
		users = append(users, html.Option(html.Props{Value: preview.Subject, Selected: true}, ui.Text(preview.Subject)))
	}
	if len(conversations) == 0 {
		conversations = append(conversations, html.Option(html.Props{Value: preview.Conversation, Selected: true}, ui.Text(preview.Conversation)))
	}
	skills := make([]ui.Node, 0, len(preview.EffectiveSkills))
	for _, skill := range preview.EffectiveSkills {
		skills = append(skills, html.Li(html.Props{Class: "persona-admin-preview-skill", Raw: map[string]any{"data-skill-tier": strings.ToUpper(skill.Tier)}}, html.Strong(html.Props{}, ui.Text(skill.Name)), html.Span(html.Props{Class: "status"}, ui.Text(strings.ToUpper(skill.Tier)))))
	}
	if len(skills) == 0 {
		skills = append(skills, html.Li(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "no_effective_skills"))))
	}
	dataReach := strings.Join(preview.DerivedData, ", ")
	if dataReach == "" {
		dataReach = personaAdminText(locale, "not_reported")
	}
	warnings := make([]ui.Node, 0, len(preview.Warnings))
	if snapshot.PreviewUnavailable {
		warnings = append(warnings, html.Li(html.Props{Class: "persona-admin-preview-warning", Role: "status"}, ui.Text(personaAdminText(locale, "preview_unavailable"))))
	}
	for _, warning := range preview.Warnings {
		warnings = append(warnings, html.Li(html.Props{Class: "persona-admin-preview-warning"}, ui.Text(warning)))
	}
	if len(warnings) == 0 {
		warnings = append(warnings, html.Li(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "no_preview_warnings"))))
	}
	refresh := html.Small(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "actions_unavailable")))
	if client != nil {
		refresh = html.Small(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "preview_server_authorized")))
	}
	return html.Section(html.Props{Class: "surface persona-admin-preview", Aria: map[string]string{"labelledby": "persona-admin-preview-title"}, Raw: map[string]any{"data-preview-subject": preview.Subject, "data-preview-conversation": preview.Conversation}},
		html.Div(html.Props{Class: "section-head"}, html.H2(html.Props{ID: "persona-admin-preview-title"}, ui.Text(personaAdminText(locale, "preview_title"))), html.P(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "preview_detail")))),
		html.Div(html.Props{Class: "persona-admin-preview-controls"},
			html.Label(html.Props{For: "persona-admin-preview-persona"}, ui.Text(personaAdminText(locale, "choose_persona"))), html.Select(html.Props{ID: "persona-admin-preview-persona", Name: "persona_preview_persona", Disabled: len(personas) == 0}, personas...),
			html.Label(html.Props{For: "persona-admin-preview-subject"}, ui.Text(personaAdminText(locale, "choose_user"))), html.Select(html.Props{ID: "persona-admin-preview-subject", Name: "persona_preview_subject"}, users...),
			html.Label(html.Props{For: "persona-admin-preview-conversation"}, ui.Text(personaAdminText(locale, "choose_conversation"))), html.Select(html.Props{ID: "persona-admin-preview-conversation", Name: "persona_preview_conversation"}, conversations...),
		),
		html.Div(html.Props{Class: "persona-admin-preview-summary"}, personaAdminFact(locale, "audience", preview.Audience), personaAdminFact(locale, "placement", preview.ReplyPlacement), personaAdminFact(locale, "data_reach", dataReach)),
		html.H3(html.Props{}, ui.Text(personaAdminText(locale, "effective_skills"))), html.Ul(html.Props{Class: "persona-admin-preview-skills"}, skills...),
		html.H3(html.Props{}, ui.Text(personaAdminText(locale, "preview_warnings"))), html.Ul(html.Props{Class: "persona-admin-preview-warnings"}, warnings...), refresh,
	)
}

func personaAdminText(locale LocaleContext, key string) string {
	return locale.Text("persona_admin." + key)
}
