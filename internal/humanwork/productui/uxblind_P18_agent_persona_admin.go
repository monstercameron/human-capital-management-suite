package productui

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
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
	Description string
	Tier        string
	DataClasses []string
}

type PersonaAdminInstallation struct {
	InstallationID string
	Version        string
	ConversationID string
	Conversation   string
	Kind           string
	Audience       string
	ReplyPlacement string
	// OfficialDocumentCount is nil when the server has not projected this
	// conversation's current official-document visibility yet.
	OfficialDocumentCount  *int
	OfficialDocumentTitles []string
	// Stopped is true for a placement the server suspended: the agent is not
	// offered in that conversation until it is started again. StoppedReason is
	// one of the PersonaPlacementStopped* codes, which the page turns into a
	// sentence; StartAgain is true once the cause is cured.
	Stopped       bool
	StoppedReason string
	StartAgain    bool
}

// The reasons a placement can be stopped, as the server names them to the page.
// The page shows a sentence for each and never the code.
const (
	PersonaPlacementStoppedNoIdentity       = "NO_RUNTIME_IDENTITY"
	PersonaPlacementStoppedIdentityInactive = "RUNTIME_IDENTITY_INACTIVE"
	PersonaPlacementStoppedNotPublished     = "NOT_PUBLISHED"
	PersonaPlacementStoppedVersionMissing   = "VERSION_UNAVAILABLE"
	PersonaPlacementStoppedOther            = "NEEDS_ATTENTION"
)

type PersonaAdminLimits struct {
	InvocationsPerHour string
	ConcurrentTasks    string
	DailySpend         string
}

type PersonaAdminPersona struct {
	Icon             agenticon.Value
	IconRevision     int64
	ID               string
	StarterID        string
	StarterVersion   uint32
	Handle           string
	Name             string
	Purpose          string
	Lifecycle        PersonaAdminLifecycle
	Owner            string
	OwnerName        string
	OwnerInitials    string
	OwnerAvatarURL   string
	Steward          string
	StewardName      string
	StewardInitials  string
	StewardAvatarURL string
	Version          string
	// LiveVersion is the currently published version when Version names a
	// newer draft or reviewed version.
	LiveVersion            string
	Skills                 []PersonaAdminSkill
	DerivedData            []string
	ChannelClasses         []string
	Audience               string
	AudienceRoles          []PersonaAdminTarget
	Organizations          []PersonaAdminTarget
	Installations          []PersonaAdminInstallation
	Limits                 PersonaAdminLimits
	ReviewRequired         bool
	ReviewApproved         bool
	Reviewer               string
	ReviewerName           string
	ReviewerInitials       string
	ReviewerAvatarURL      string
	ReviewApprovedAt       string
	EvaluationRef          string
	EvaluationPassedAt     string
	EvaluationRunnerName   string
	PublishedAt            string
	EvaluationStatus       string
	EvaluationCaseCount    int
	EvaluationPassed       int
	EvaluationFailed       int
	EvaluationFailureNames []string
	EvaluationFailuresURL  string
	Instructions           string
	Guidance               string `json:"guidance,omitempty"`
	PurposeLanguage        string
	DocumentReferences     []PersonaAdminDocumentReference `json:"document_references,omitempty"`
	VersionHistory         []PersonaAdminVersionHistory
	RecentRunsUnavailable  bool
	RecentRuns             []AgentControlRun
	// WorkspaceDocuments, WorkspaceIndexedAt and WorkspacePending describe the
	// documents Assistant may search across the workspace. Only the Assistant's
	// row carries them; WorkspaceIndexedAt is an RFC 3339 UTC time, empty when
	// nothing has been indexed.
	WorkspaceDocuments int
	WorkspaceIndexedAt string
	WorkspacePending   int
	// ReactionsOff is true when the agent's owner turned off its reaction to the
	// questions it is asked (AGENTUX-075). The zero value is the default: it reacts.
	ReactionsOff bool
}

type PersonaAdminTarget struct {
	ID             string
	Label          string
	PlacementLabel string
	Role           string
	Kind           string
	ViewerDirect   bool
	// Members are the people in this room, so a count that is meant for an
	// ordinary member can be read as one of them (AGENTUX-034). Only ID and
	// Role are filled.
	Members []PersonaAdminTarget
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
	Instructions   string
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
	Subject                 string
	Conversation            string
	ConversationKind        string
	Audience                string
	ReplyPlacement          string
	EffectiveSkills         []PersonaAdminSkill
	DerivedData             []string
	Warnings                []string
	AuthorizationReason     string
	OfficialDocumentCount   *int
	OfficialDocumentTitles  []string
	UnreadableDocumentCount int
}

// PersonaAdminRegionState keeps a failed optional projection from taking down
// the rest of Agent setup. Omitted counts are safe aggregate diagnostics; item
// identifiers and backend errors never cross the application boundary.
type PersonaAdminRegionState struct {
	Unavailable bool
	Omitted     int
}

// PersonaAdminSnapshot is the minimum server-owned projection needed by the
// catalogue. It deliberately has no posts, prompts, run steps, or artifacts.
type PersonaAdminSnapshot struct {
	Available                    bool
	DocumentServiceAvailable     bool
	LocalDevBootstrapAvailable   bool
	CommandPermissionsAvailable  bool
	AllowedCommands              []string
	CommandStatus                string
	CommandPersonaID             string
	CommandAction                string
	PreviewPersonaID             string
	PreviewSubjectID             string
	PreviewConversationID        string
	PreviewUnavailable           bool
	PreviewValidationFields      []string
	Personas                     []PersonaAdminPersona
	StarterCatalogAvailable      bool
	Starters                     []PersonaAdminStarter
	Preview                      PersonaAdminPreview
	SubjectOptions               []PersonaAdminTarget
	Conversations                []PersonaAdminTarget
	CatalogState                 PersonaAdminRegionState
	TargetsState                 PersonaAdminRegionState
	PreviewState                 PersonaAdminRegionState
	CommandsState                PersonaAdminRegionState
	StartersState                PersonaAdminRegionState
	DocumentsState               PersonaAdminRegionState
	EvaluationRuntimeUnavailable bool
	// ViewerSubject is the signed-in person the page is drawn for. The page
	// fills it from the view; it grants nothing and is not sent by the server.
	ViewerSubject string `json:"-"`
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
	Navigation        ui.Node
	State             PersonaAdminLoadState
	UnavailableReason string
	Snapshot          PersonaAdminSnapshot
	Client            PersonaAdminClient
	// ViewerSubject is the signed-in person, so the page does not name the
	// reader as the person to ask.
	ViewerSubject string
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
		feature("persona_catalog", "Agents", "Review agent lifecycle and access", true, false, false, false),
		feature("persona_publication", "Agent publication", "Publish, rollback, suspend, or retire reviewed agents", true, true, true, false),
		feature("persona_preview", "Agent access preview", "Preview effective skills for a person and conversation", true, false, false, false),
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
		return personaAdminUnavailable(view, locale, personaAdminText(locale, "permission_denied"))
	}
	if client == nil {
		return personaAdminUnavailable(view, locale, personaAdminText(locale, "persona_store_unavailable"))
	}
	snapshot, err := client.Snapshot(context.Background(), PersonaAdminSnapshotRequest{TenantID: view.Tenant, Principal: view.Principal})
	if err != nil {
		switch agentUX022SnapshotFailure(err) {
		case "denied":
			return personaAdminUnavailable(view, locale, personaAdminText(locale, "permission_denied"))
		case "store":
			return personaAdminUnavailable(view, locale, personaAdminText(locale, "persona_store_unavailable"))
		}
		return agentUX022LoadFailed(view, locale)
	}
	if !snapshot.Available {
		return personaAdminUnavailable(view, locale, personaAdminText(locale, "persona_store_unavailable"))
	}
	return PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, Navigation: AgentPageNavigation(view, AgentPageSetup), State: PersonaAdminReady, Snapshot: agentUX074WithPeoplePhotos(view, snapshot), Client: client, ViewerSubject: docsViewer(view)})
}

func PersonaAdminPage(props PersonaAdminPageProps) ui.Node {
	locale := props.Locale
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	navigation := props.Navigation
	statusView := View{Page: PagePersonaAdmin, Locale: locale}
	if navigation == nil {
		navigation = AgentPageNavigation(statusView, AgentPageSetup)
	}
	if props.State == "" {
		props.State = PersonaAdminUnavailable
	}
	if props.Snapshot.ViewerSubject == "" {
		props.Snapshot.ViewerSubject = props.ViewerSubject
	}
	if props.State == PersonaAdminLoading {
		return personaAdminStatus(statusView, locale, PersonaAdminLoading, "")
	}
	if props.State == PersonaAdminUnavailable {
		detail := strings.TrimSpace(props.UnavailableReason)
		if detail == "" {
			detail = personaAdminText(locale, "service_unavailable")
		}
		return personaAdminStatus(statusView, locale, PersonaAdminUnavailable, detail)
	}

	groups := make(map[PersonaAdminLifecycle][]PersonaAdminPersona)
	for _, persona := range props.Snapshot.Personas {
		groups[persona.Lifecycle] = append(groups[persona.Lifecycle], persona)
	}
	ordered := []PersonaAdminLifecycle{PersonaDraft, PersonaInReview, PersonaPublished, PersonaSuspended, PersonaRetired}
	sections := make([]ui.Node, 0, len(ordered))
	showGroupHeadings := len(props.Snapshot.Personas) >= 5
	for _, lifecycle := range ordered {
		personas := groups[lifecycle]
		if len(personas) == 0 {
			continue
		}
		cards := make([]ui.Node, 0, len(personas))
		for _, persona := range personas {
			cards = append(cards, personaAdminCatalogEntry(locale, props.Client, persona, props.Snapshot))
		}
		if !showGroupHeadings {
			sections = append(sections, cards...)
			continue
		}
		sections = append(sections, html.Section(html.Props{Class: "persona-admin-lifecycle", Raw: map[string]any{"data-lifecycle": string(lifecycle), "aria-labelledby": "persona-admin-lifecycle-" + safeAgentDOMToken(string(lifecycle))}},
			html.H2(html.Props{ID: "persona-admin-lifecycle-" + safeAgentDOMToken(string(lifecycle))}, ui.Text(personaAdminText(locale, "lifecycle_"+strings.ToLower(string(lifecycle))))),
			html.Div(html.Props{Class: "persona-admin-cards"}, cards...),
		))
	}
	if len(sections) == 0 && !props.Snapshot.CatalogState.Unavailable {
		sections = append(sections, html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(personaAdminText(locale, "empty"))))
	}

	startersAvailable := len(personaAdminReadyStarters(props.Snapshot)) > 0
	newAgentTarget := "persona-admin-editor"
	newAgentAction := ui.Node(nil)
	newAgentNote := ui.Node(nil)
	if startersAvailable {
		newAgentAction = html.Button(html.Props{Class: "button primary", Type: "button", Aria: map[string]string{"controls": newAgentTarget, "expanded": "false"}, Raw: map[string]any{"data-persona-new-agent": newAgentTarget}}, ui.Text(personaAdminText(locale, "new_agent")))
	} else if !props.Snapshot.StartersState.Unavailable {
		newAgentNote = personaAdminNoStarterNotice(locale, props.Snapshot.SubjectOptions, props.ViewerSubject)
	}
	editor := ui.Node(nil)
	if startersAvailable {
		editor = PersonaAdminEditor(locale, props.Client, props.Snapshot)
	}
	catalogFailure := personaAdminRegionFailure(locale, "catalog", props.Snapshot.CatalogState)
	starterFailure := personaAdminRegionFailure(locale, "starters", props.Snapshot.StartersState)
	commandFailure := personaAdminRegionFailure(locale, "commands", props.Snapshot.CommandsState)
	documentFailure := personaAdminRegionFailure(locale, "documents", props.Snapshot.DocumentsState)
	return ProductPageFrame(ProductPageFrameProps{
		Class: "agent-page-frame", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": "persona-admin-title"}, Raw: map[string]any{"data-persona-admin-state": string(props.State)},
		Breadcrumbs: personaAdminBreadcrumb(locale), Title: personaAdminText(locale, "title"), TitleID: "persona-admin-title",
		Actions: []ui.Node{navigation},
		Body: []ui.Node{html.Div(html.Props{Class: "persona-admin-page"},
			html.Div(html.Props{Class: "persona-admin-hero"},
				html.P(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "description"))),
				html.Div(html.Props{Class: "persona-admin-hero-actions"}, newAgentAction),
				newAgentNote,
				starterFailure,
			),
			html.Section(html.Props{Class: "persona-admin-catalog", Aria: map[string]string{"labelledby": "persona-admin-catalog-title"}},
				html.Div(html.Props{Class: "section-head persona-admin-catalog-heading"}, html.H2(html.Props{ID: "persona-admin-catalog-title"}, ui.Text(personaAdminText(locale, "catalog_title"))), html.P(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "catalog_detail")))),
				catalogFailure,
				commandFailure,
				documentFailure,
				html.Div(html.Props{Class: "persona-admin-lifecycle-list"}, sections...),
			),
			editor,
			personaAdminPreview(locale, props.Client, props.Snapshot),
		)},
	})
}

func personaAdminRegionFailure(locale LocaleContext, region string, state PersonaAdminRegionState) ui.Node {
	if !state.Unavailable {
		return nil
	}
	return html.Div(html.Props{Class: "persona-admin-region-failure", Role: "status", Raw: map[string]any{"data-persona-admin-region": region, "data-omitted-count": state.Omitted}},
		html.P(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, region+"_unavailable"))),
		html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"data-persona-admin-retry": region}}, ui.Text(personaAdminText(locale, "retry"))),
	)
}

func personaAdminCatalogEntry(locale LocaleContext, client PersonaAdminClient, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) ui.Node {
	return html.Div(html.Props{Class: "persona-admin-entry"}, personaAdminCard(locale, client, persona, snapshot))
}

func personaAdminStatus(view View, locale LocaleContext, state PersonaAdminLoadState, detail string) ui.Node {
	view.Page = PagePersonaAdmin
	view.Locale = locale
	body := []ui.Node(nil)
	if state == PersonaAdminLoading {
		body = append(body, AgentLoadingFrame(AgentLoadingProps{Locale: locale, Shape: AgentLoadingCards, Rows: 3, Status: personaAdminText(locale, "loading"), RetryRaw: map[string]any{"data-persona-admin-retry": "page"}}))
	} else {
		body = append(body, ui.CreateElement(EmptyState, EmptyStateProps{Title: personaAdminText(locale, "unavailable_title"), Description: detail, Role: "status", Class: "persona-admin-unavailable"}))
	}
	raw := map[string]any{"data-persona-admin-state": string(state)}
	if state == PersonaAdminLoading {
		raw["role"], raw["aria-live"] = "status", "polite"
	}
	return ProductPageFrame(ProductPageFrameProps{
		Class: "agent-page-frame", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": "persona-admin-title"}, Raw: raw,
		Breadcrumbs: personaAdminBreadcrumb(locale), Title: personaAdminText(locale, "title"), TitleID: "persona-admin-title",
		Actions: []ui.Node{AgentPageNavigation(view, AgentPageSetup)}, Body: []ui.Node{html.Div(html.Props{Class: "persona-admin-page"}, body...)},
	})
}

func personaAdminUnavailable(view View, locale LocaleContext, detail string) ui.Node {
	if detail == personaAdminText(locale, "permission_denied") {
		return agentOperationsDenied(view, locale)
	}
	return personaAdminStatus(view, locale, PersonaAdminUnavailable, detail)
}

func personaAdminCard(locale LocaleContext, client PersonaAdminClient, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) ui.Node {
	persona = personaAdminPersonaWithDirectoryPeople(persona, snapshot.SubjectOptions)
	lifecycle := personaAdminLifecycleState(locale, persona)
	id := "persona-admin-" + safeAgentDOMToken(persona.ID)
	skillNodes := make([]ui.Node, 0, len(persona.Skills))
	for _, skill := range persona.Skills {
		dataClassParts := make([]string, 0, len(skill.DataClasses))
		for _, class := range skill.DataClasses {
			dataClassParts = append(dataClassParts, PersonaDataClassLabelForLocale(locale, class))
		}
		name := PersonaSkillLabelForLocale(locale, skill.ID, skill.Name, skill.Description)
		description := PersonaSkillDescription(locale, skill.ID, skill.Description)
		if description == "" && len(dataClassParts) > 0 {
			description = personaAdminText(locale, "skill_uses") + " " + strings.Join(dataClassParts, ", ") + "."
		}
		if description == "" {
			description = personaAdminText(locale, "skill_description_missing")
		}
		skillNodes = append(skillNodes, html.Li(html.Props{Class: "persona-admin-skill", Raw: map[string]any{"data-skill-tier": strings.ToUpper(skill.Tier)}},
			html.Div(html.Props{Class: "persona-admin-skill-copy"},
				html.Div(html.Props{Class: "persona-admin-skill-title"}, html.Strong(html.Props{Dir: "auto"}, ui.Text(name)), html.Span(html.Props{Class: "status persona-admin-tier"}, ui.Text(personaAdminSkillTierLabel(locale, skill)))),
				html.Small(html.Props{Class: "muted", Dir: "auto", Raw: map[string]any{"lang": "und"}}, ui.Text(description)))))
	}
	actions := personaAdminLifecycleActions(locale, client, persona, snapshot)
	skills := ui.Node(html.P(html.Props{Class: "muted persona-admin-empty"}, ui.Text(personaAdminText(locale, "no_skills"))))
	if len(skillNodes) > 0 {
		skills = html.Ul(html.Props{Class: "persona-admin-skill-list"}, skillNodes...)
	}
	purposeLanguage := strings.TrimSpace(persona.PurposeLanguage)
	if purposeLanguage == "" {
		purposeLanguage = agentUXR7ContentLanguage(persona.Purpose)
	}
	purpose, purposeLanguage := agentUXR7Purpose(locale, persona.Purpose, purposeLanguage)
	return html.Article(html.Props{Class: "surface persona-admin-card", Aria: map[string]string{"labelledby": id + "-title"}, Raw: map[string]any{"data-persona-id": persona.ID, "data-lifecycle": string(persona.Lifecycle), "data-agent-version": persona.Version}},
		html.Div(html.Props{Class: "persona-admin-card-heading"}, html.Div(html.Props{Class: "persona-admin-card-identity"}, html.Div(html.Props{Class: "persona-admin-card-title"}, agentUX074Icon(persona.Icon, persona.ID, personaAdminIDs(snapshot)),
			// The version badge follows the name on the name's own line; it used to
			// be a sibling of the whole title and floated away from it.
			html.Div(html.Props{Class: "persona-admin-card-name"}, html.H3(html.Props{ID: id + "-title", Dir: "auto", Raw: map[string]any{"lang": purposeLanguage}}, ui.Text(persona.Name)), personaAdminLifecycleBadges(locale, persona, lifecycle)),
			html.Small(html.Props{Class: "muted"}, html.Span(html.Props{Dir: "ltr"}, ui.Text(personaAdminHandle(persona.Handle))), ui.Text(" · "), html.A(html.Props{Href: Path(PageAgents) + "?agent=" + url.QueryEscape(persona.ID)}, ui.Text(agentUXR7Text(locale, "ask_link")))))), personaAdminHeaderActions(locale, client, persona, snapshot)),
		personaAdminVersionEditorPanel(locale, client, persona, snapshot),
		personaAdminMoreActionsPanel(locale, client, persona, snapshot),
		personaAdminFailureWarning(locale, persona),
		personaAdminLifecycleSentence(locale, persona, lifecycle),
		html.P(html.Props{Class: "persona-admin-purpose", Dir: "auto", Raw: map[string]any{"lang": purposeLanguage}}, html.Tag("bdi", html.Props{}, ui.Text(purpose))),
		html.Tag("dl", html.Props{Class: "persona-admin-facts"},
			personaAdminPersonDefinition(locale, "owner", persona.Owner, persona.OwnerName, persona.OwnerInitials, persona.OwnerAvatarURL), personaAdminPersonDefinition(locale, "steward", persona.Steward, persona.StewardName, persona.StewardInitials, persona.StewardAvatarURL), personaAdminAudienceDefinition(locale, persona), personaAdminReadDefinition(locale, persona), personaAdminWorkspaceSearchDefinition(locale, persona), personaAdminLimitsDefinition(locale, persona)),
		html.H4(html.Props{}, ui.Text(personaAdminText(locale, "skills"))), skills,
		personaAdminPlacements(locale, persona, snapshot),
		personaAdminReactionsControl(locale, persona, snapshot),
		personaAdminLifecycleProgress(locale, persona, lifecycle),
		html.P(html.Props{ID: id + "-command-status", Class: "persona-admin-command-status", Role: "status", Raw: map[string]any{"aria-live": "polite", "aria-atomic": "true", "data-persona-command-status": persona.ID}}, ui.Text(personaAdminCardCommandStatus(locale, snapshot.CommandStatus, snapshot.CommandAction, snapshot.CommandPersonaID, persona))),
		personaAdminLifecycleBlock(locale, persona, snapshot),
		html.Div(html.Props{Class: "persona-admin-actions"}, actions...),
		integrate1AgentIconControls(locale, persona),
		personaAdminInstructionsSection(locale, persona),
		personaAdminDocumentReferences(locale, persona.ID, persona.DocumentReferences),
		personaAdminTechnicalDetails(locale, persona),
	)
}

func personaAdminPersonaWithDirectoryPeople(persona PersonaAdminPersona, options []PersonaAdminTarget) PersonaAdminPersona {
	persona.OwnerName = personaAdminDirectoryPersonName(options, persona.Owner, persona.OwnerName)
	persona.StewardName = personaAdminDirectoryPersonName(options, persona.Steward, persona.StewardName)
	persona.ReviewerName = personaAdminDirectoryPersonName(options, persona.Reviewer, persona.ReviewerName)
	return persona
}

func personaAdminDirectoryPersonName(options []PersonaAdminTarget, reference, current string) string {
	if name := strings.TrimSpace(current); len(strings.Fields(name)) >= 2 {
		return name
	}
	wanted := strings.TrimSpace(reference)
	if wanted == "" {
		return ""
	}
	for _, option := range options {
		if strings.TrimSpace(option.ID) == wanted {
			name := strings.TrimSpace(option.Label)
			if len(strings.Fields(name)) < 2 {
				if full := PersonaWorkerLabel(reference, ""); len(strings.Fields(full)) >= 2 {
					return full
				}
			}
			return name
		}
	}
	return ""
}

func personaAdminDocumentReferences(locale LocaleContext, personaID string, references []PersonaAdminDocumentReference) ui.Node {
	rows := make([]ui.Node, 0, len(references))
	for _, reference := range references {
		title := strings.TrimSpace(reference.Title)
		if title == "" {
			title = strings.TrimSpace(reference.Label)
		}
		mode := agentDocumentPickerText(locale, "latest")
		if reference.VersionMode != "LATEST_PUBLISHED" {
			mode = strings.ReplaceAll(agentDocumentPickerText(locale, "pinned"), "{version}", personaAdminLocalizedNumber(locale, strconv.FormatUint(reference.PinnedVersion, 10)))
		}
		label := ui.Node(html.Strong(html.Props{Dir: "auto"}, ui.Text(title)))
		if reference.Readable {
			label = html.A(html.Props{Href: agentDocumentHubHref(reference.DocumentID), Target: "_blank", Dir: "auto", Raw: map[string]any{"rel": "noopener noreferrer", "lang": "und"}}, ui.Text(title))
		}
		children := []ui.Node{label}
		if location := strings.TrimSpace(reference.Location); location != "" {
			children = append(children, html.Small(html.Props{Class: "muted persona-admin-document-location"}, ui.Text(personaAdminInlineSeparator+location)))
		}
		children = append(children, html.Small(html.Props{Class: "muted persona-admin-document-mode"}, ui.Text(personaAdminInlineSeparator+mode)))
		if !reference.Readable {
			children = append(children, html.Small(html.Props{Class: "persona-admin-document-warning"}, ui.Text(personaAdminR5SetupText(locale, "cannot_open_document"))))
		}
		rows = append(rows, html.Li(html.Props{}, children...))
	}
	content := ui.Node(html.P(html.Props{Class: "muted persona-admin-empty"}, ui.Text(personaAdminText(locale, "reference_documents_none"))))
	if len(rows) > 0 {
		content = html.Ul(html.Props{}, rows...)
	}
	return html.Div(html.Props{ID: "persona-admin-documents-" + safeAgentDOMToken(personaID), Class: "persona-admin-reference-documents"}, html.H4(html.Props{}, ui.Text(agentDocumentPickerText(locale, "field_label"))), content)
}

func personaAdminHandle(handle string) string {
	handle = strings.TrimSpace(handle)
	if handle != "" && !strings.HasPrefix(handle, "@") {
		return "@" + handle
	}
	return handle
}

func personaAdminAudience(locale LocaleContext, persona PersonaAdminPersona) string {
	roles := make(map[string]string, len(persona.AudienceRoles))
	for _, role := range persona.AudienceRoles {
		roles[role.ID] = role.Label
	}
	organizations := make(map[string]string, len(persona.Organizations))
	for _, organization := range persona.Organizations {
		organizations[organization.ID] = organization.Label
	}
	parts := make([]string, 0)
	for _, raw := range strings.Split(persona.Audience, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "org:") {
			parts = append(parts, PersonaOrganizationLabelForLocale(locale, raw, organizations[raw]))
			continue
		}
		label := PersonaRoleLabelForLocale(locale, raw, roles[raw])
		if locale.Resolved == "de-DE" && strings.EqualFold(strings.NewReplacer("-", "_", " ", "_").Replace(raw), "workflow_author") {
			label = "Workflow-Autor:innen"
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func personaAdminDefinition(locale LocaleContext, key, value string) ui.Node {
	if strings.TrimSpace(value) == "" {
		value = personaAdminText(locale, "none_set")
	}
	return html.Div(html.Props{Class: "persona-admin-fact"}, html.Tag("dt", html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, key))), html.Tag("dd", html.Props{}, ui.Text(value)))
}

func personaAdminLifecycleStepper(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	current := 0
	switch {
	case persona.Lifecycle == PersonaPublished || persona.Lifecycle == PersonaSuspended || persona.Lifecycle == PersonaRetired:
		current = 3
	case persona.EvaluationRef != "":
		current = 2
	case persona.Lifecycle == PersonaInReview:
		current = 1
	}
	keys := []string{"step_draft", "step_review", "step_evaluated", "step_published"}
	published := persona.Lifecycle == PersonaPublished
	steps := make([]ui.Node, 0, len(keys))
	for index, key := range keys {
		raw := map[string]any{"data-step-state": map[bool]string{true: "current", false: "upcoming"}[index == current]}
		if index < current || published {
			raw["data-step-state"] = "complete"
		}
		if index == current {
			raw["aria-current"] = "step"
		}
		content := ui.Node(html.Span(html.Props{Class: "persona-admin-step-label"}, ui.Text(personaAdminText(locale, key))))
		if index < current || published {
			content = html.Span(html.Props{Class: "persona-admin-step-content"}, html.Span(html.Props{Class: "persona-admin-step-check", Aria: map[string]string{"hidden": "true"}}, ui.Text("✓")), html.Span(html.Props{Class: "persona-admin-step-label"}, ui.Text(personaAdminText(locale, key))))
		} else if index == current {
			content = html.Span(html.Props{Class: "persona-admin-current-step persona-admin-step-label"}, ui.Text(personaAdminText(locale, key)))
		}
		steps = append(steps, html.Li(html.Props{Raw: raw}, content))
	}
	progress := strings.NewReplacer("{current}", locale.FormatNumber(strconv.Itoa(current+1), 0), "{total}", locale.FormatNumber(strconv.Itoa(len(keys)), 0), "{state}", personaAdminText(locale, keys[current])).Replace(personaAdminText(locale, "lifecycle_progress_text"))
	return html.Div(html.Props{Class: "persona-admin-lifecycle-progress"}, html.P(html.Props{Class: "persona-admin-progress-label"}, ui.Text(progress)), html.Ol(html.Props{Aria: map[string]string{"label": personaAdminText(locale, "lifecycle_progress")}}, steps...))
}

func personaAdminLimits(locale LocaleContext, limits PersonaAdminLimits) string {
	values := make([]string, 0, 3)
	for _, value := range []string{limits.InvocationsPerHour, limits.ConcurrentTasks, limits.DailySpend} {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return personaAdminText(locale, "no_limits")
	}
	return strings.Join(values, " · ")
}

func personaAdminFact(locale LocaleContext, label, value string) ui.Node {
	if strings.TrimSpace(value) == "" {
		value = personaAdminText(locale, "no_limits")
	}
	return html.Div(html.Props{Class: "persona-admin-fact"}, html.Small(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, label))), html.Strong(html.Props{}, ui.Text(value)))
}

func personaAdminReviewDetail(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	details := make([]string, 0, 2)
	if strings.TrimSpace(persona.Reviewer) != "" || strings.TrimSpace(persona.ReviewerName) != "" {
		details = append(details, personaAdminText(locale, "reviewer")+": "+PersonaWorkerLabel(persona.Reviewer, persona.ReviewerName))
	}
	if strings.TrimSpace(persona.EvaluationRef) != "" {
		details = append(details, personaAdminText(locale, "evaluation_complete"))
	} else if persona.ReviewRequired {
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
	primary := make([]ui.Node, 0, 2)
	if persona.Lifecycle == PersonaDraft && !persona.ReviewApproved {
		allowed := personaAdminCommandAllowed(snapshot, "REQUEST_REVIEW")
		primary = append(primary, personaAdminAction(locale, persona, "REQUEST_REVIEW", "request_review", "request_review_unavailable", allowed, true, false))
	}
	if persona.Lifecycle == PersonaInReview && !persona.ReviewApproved {
		_, canReview := client.(PersonaAdminReviewClient)
		allowed := canReview && personaAdminCommandAllowed(snapshot, "REVIEW")
		if allowed {
			primary = append(primary, personaAdminReviewApprovalAction(locale, persona))
		}
	}
	if persona.ReviewApproved && persona.EvaluationRef == "" && persona.Lifecycle != PersonaPublished && persona.Lifecycle != PersonaSuspended && persona.Lifecycle != PersonaRetired {
		if strings.EqualFold(strings.TrimSpace(persona.EvaluationStatus), "RUNNING") {
			primary = append(primary, html.Button(html.Props{Class: "button primary", Type: "button", Disabled: true}, ui.Text(personaAdminText(locale, "evaluation_running_action"))))
		} else {
			allowed := personaAdminCommandAllowed(snapshot, "RUN_EVALUATION")
			primary = append(primary, personaAdminEvaluationAction(locale, persona, allowed, snapshot.EvaluationRuntimeUnavailable))
		}
	} else if persona.ReviewApproved && persona.EvaluationRef != "" && persona.Lifecycle != PersonaPublished && persona.Lifecycle != PersonaSuspended && persona.Lifecycle != PersonaRetired {
		allowed := personaAdminCommandAllowed(snapshot, "PUBLISH")
		primary = append(primary, personaAdminAction(locale, persona, "PUBLISH", "publish", "publish_permission", allowed, true, false))
	}

	result := []ui.Node{html.Div(html.Props{Class: "persona-admin-primary-actions"}, primary...)}
	if persona.Lifecycle != PersonaPublished {
		if summary := personaAdminEvaluationSummary(locale, persona); summary != nil {
			result = append(result, summary)
		}
	}
	return result
}

func personaAdminEvaluationAction(locale LocaleContext, persona PersonaAdminPersona, allowed, runtimeUnavailable bool) ui.Node {
	if !allowed {
		if runtimeUnavailable {
			return personaAdminEvaluationFallback(locale, persona, false)
		}
		return html.Div(html.Props{Class: "persona-admin-action-row"}, html.Button(html.Props{Class: "button primary", Type: "button", Disabled: true}, ui.Text(personaAdminText(locale, "run_evaluation"))), html.Small(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "evaluation_permission"))))
	}
	caseCount := persona.EvaluationCaseCount
	if caseCount <= 0 {
		caseCount = persona.EvaluationPassed + persona.EvaluationFailed
	}
	explanation := personaAdminText(locale, "evaluation_explanation")
	if caseCount > 0 {
		explanation = strings.NewReplacer("{count}", locale.FormatNumber(strconv.Itoa(caseCount), 0), "{version}", personaAdminLocalizedNumber(locale, persona.Version)).Replace(personaAdminR5SetupText(locale, "evaluation_runs_cases"))
	}
	return html.Form(html.Props{Class: "persona-admin-next-action", Raw: map[string]any{"data-persona-admin-command-form": "RUN_EVALUATION"}},
		html.Input(html.Props{Name: "persona_id", Type: "hidden", Value: persona.ID}),
		html.P(html.Props{Class: "muted"}, ui.Text(explanation)),
		html.Button(html.Props{Class: "button primary", Type: "submit", Raw: map[string]any{"data-evaluation-version": persona.Version}}, ui.Text(personaAdminText(locale, "run_evaluation"))),
		personaAdminEvaluationFallback(locale, persona, true),
	)
}

func personaAdminEvaluationSummary(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	switch strings.ToUpper(strings.TrimSpace(persona.EvaluationStatus)) {
	case "RUNNING":
		return html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(personaAdminText(locale, "evaluation_running")))
	case "PASSED", "FAILED":
		text := strings.NewReplacer("{passed}", locale.FormatNumber(strconv.Itoa(persona.EvaluationPassed), 0), "{failed}", locale.FormatNumber(strconv.Itoa(persona.EvaluationFailed), 0)).Replace(personaAdminText(locale, "evaluation_result"))
		children := []ui.Node{ui.Text(text)}
		failuresURL := strings.TrimSpace(persona.EvaluationFailuresURL)
		failuresID := "persona-admin-evaluation-failures-" + safeAgentDOMToken(persona.ID)
		if failuresURL == "" && len(persona.EvaluationFailureNames) > 0 {
			failuresURL = "#" + failuresID
		}
		if persona.EvaluationFailed > 0 && failuresURL != "" {
			children = append(children, ui.Text(" "), html.A(html.Props{Href: failuresURL}, ui.Text(personaAdminText(locale, "view_failures"))))
		}
		status := ui.Node(html.P(html.Props{Class: "persona-admin-evaluation-result", Role: "status"}, children...))
		if len(persona.EvaluationFailureNames) == 0 {
			return status
		}
		items := make([]ui.Node, 0, len(persona.EvaluationFailureNames))
		for _, name := range persona.EvaluationFailureNames {
			items = append(items, html.Li(html.Props{}, ui.Text(humanizePersonaIdentifier(name))))
		}
		return html.Div(html.Props{Class: "persona-admin-evaluation-summary"}, status,
			html.Div(html.Props{ID: failuresID, Class: "persona-admin-evaluation-failures"}, html.Strong(html.Props{}, ui.Text(personaAdminText(locale, "failed_cases"))), html.Ul(html.Props{}, items...)))
	}
	return nil
}

func personaAdminAction(locale LocaleContext, persona PersonaAdminPersona, command, labelKey, reasonKey string, available, primary, confirm bool) ui.Node {
	class := "button secondary"
	if primary {
		class = "button primary"
	}
	label := strings.ReplaceAll(personaAdminText(locale, labelKey), "{version}", personaAdminLocalizedNumber(locale, persona.Version))
	button := html.Button(html.Props{Class: class, Type: "button", Disabled: !available, Raw: map[string]any{"data-persona-command": command}}, ui.Text(label))
	if !available {
		return html.Div(html.Props{Class: "persona-admin-action-row"}, button, html.Small(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, reasonKey))))
	}
	if !confirm {
		return html.Div(html.Props{Class: "persona-admin-action-row"}, button)
	}
	question := personaAdminText(locale, strings.ToLower(command)+"_confirm") + " " + persona.Name + "?"
	return html.Details(html.Props{Class: "persona-admin-confirm"},
		html.Summary(html.Props{}, ui.Text(personaAdminText(locale, labelKey))),
		html.P(html.Props{}, ui.Text(question)),
		html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"data-persona-command": command, "data-persona-confirm": question}}, ui.Text(personaAdminText(locale, strings.ToLower(command)+"_confirm_action"))),
	)
}

func personaAdminActionDecision(node ui.Node, decision string) ui.Node {
	// The delegated browser command handler reads this marker from the button.
	return html.Div(html.Props{Class: "persona-admin-review-decision", Raw: map[string]any{"data-review-decision-wrapper": decision}}, node)
}

func personaAdminTechnicalDetails(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	return personaAdminVersionHistory(locale, persona)
}

func personaAdminTechnicalRow(locale LocaleContext, key, value string) ui.Node {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(personaAdminText(locale, key))), html.Tag("dd", html.Props{}, ui.Text(value)))
}

func personaAdminPreview(locale LocaleContext, client PersonaAdminClient, snapshot PersonaAdminSnapshot) ui.Node {
	_ = client
	preview := snapshot.Preview
	subjectLabel := personaAdminTargetLabel(snapshot.SubjectOptions, snapshot.PreviewSubjectID, preview.Subject, true, locale)
	conversationLabel := personaAdminTargetLabel(snapshot.Conversations, snapshot.PreviewConversationID, preview.Conversation, false, locale)
	personas := make([]personaAdminComboboxOption, 0, len(snapshot.Personas))
	selected := snapshot.PreviewPersonaID
	personaLabel := ""
	for _, persona := range snapshot.Personas {
		personas = append(personas, personaAdminComboboxOption{Value: persona.ID, Label: persona.Name})
		if selected != "" && (persona.ID == selected || strings.TrimPrefix(persona.Handle, "@") == selected) {
			personaLabel = persona.Name
		}
	}
	users := make([]personaAdminComboboxOption, 0, len(snapshot.SubjectOptions)+1)
	for _, option := range snapshot.SubjectOptions {
		users = append(users, personaAdminComboboxOption{Value: option.ID, Label: personaAdminPersonOption(locale, option)})
	}
	conversations := make([]personaAdminComboboxOption, 0, len(snapshot.Conversations)+1)
	for _, option := range snapshot.Conversations {
		conversations = append(conversations, personaAdminComboboxOption{Value: option.ID, Label: personaAdminConversationOption(locale, option)})
	}
	if len(snapshot.SubjectOptions) == 0 && preview.Subject != "" {
		users = append(users, personaAdminComboboxOption{Value: preview.Subject, Label: preview.Subject})
	}
	if len(snapshot.Conversations) == 0 && preview.Conversation != "" {
		conversations = append(conversations, personaAdminComboboxOption{Value: preview.Conversation, Label: preview.Conversation})
	}
	dataParts := make([]string, 0, len(preview.DerivedData))
	for _, dataClass := range preview.DerivedData {
		dataParts = append(dataParts, PersonaDataClassLabelForLocale(locale, dataClass))
	}
	selectionComplete := selected != "" && (snapshot.PreviewSubjectID != "" || preview.Subject != "") && (snapshot.PreviewConversationID != "" || preview.Conversation != "")
	result := ui.Node(html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(personaAdminText(locale, "preview_choose_all"))))
	if snapshot.TargetsState.Unavailable {
		result = personaAdminRegionFailure(locale, "targets", snapshot.TargetsState)
	} else if snapshot.PreviewState.Unavailable || snapshot.PreviewUnavailable {
		result = personaAdminRegionFailure(locale, "preview", PersonaAdminRegionState{Unavailable: true, Omitted: snapshot.PreviewState.Omitted})
	} else if selectionComplete {
		allowed := personaAdminPreviewAllows(preview)
		sentenceKey := "preview_denied_sentence"
		if allowed {
			sentenceKey = "agent_setup.preview_allowed"
		}
		template := personaAdminText(locale, sentenceKey)
		if allowed {
			template = locale.Text(sentenceKey)
		}
		sentence := strings.NewReplacer("{person}", subjectLabel, "{persona}", personaLabel, "{conversation}", conversationLabel).Replace(template)
		if preview.OfficialDocumentCount != nil {
			content := strings.NewReplacer("{conversation}", conversationLabel, "{count}", strconv.Itoa(*preview.OfficialDocumentCount)).Replace(locale.Text("agent_setup.preview_placed_documents"))
			sentence += " " + strings.ReplaceAll(locale.Text("agent_setup.preview_reads"), "{content}", content)
		} else if len(dataParts) > 0 {
			sentence += " " + strings.ReplaceAll(locale.Text("agent_setup.preview_reads"), "{content}", strings.Join(dataParts, ", "))
		}
		actions, hasReply := personaAdminPreviewActions(locale, preview.EffectiveSkills, subjectLabel)
		if len(actions) > 0 {
			sentence += " " + strings.ReplaceAll(locale.Text("agent_setup.preview_does"), "{content}", strings.Join(actions, ", "))
		}
		if placement := strings.TrimSpace(preview.ReplyPlacement); placement != "" && !hasReply {
			audience := personaAdminHumanizedList(locale, preview.Audience)
			if locale.Resolved == "en-US" {
				audience = strings.ToLower(audience)
			}
			sentence += " " + strings.NewReplacer("{placement}", strings.ToLower(placement), "{audience}", audience).Replace(personaAdminText(locale, "preview_replies"))
		}
		if reason := strings.TrimSpace(preview.AuthorizationReason); reason != "" {
			sentence += " " + reason
		}
		if len(preview.Warnings) > 0 {
			sentence += " " + personaAdminText(locale, "preview_withheld") + " " + strings.Join(preview.Warnings, " ")
		}
		result = personaAdminAccessCheckResult(locale, personaLabel, subjectLabel, conversationLabel, preview, sentence)
	} else if len(snapshot.PreviewValidationFields) > 0 {
		result = nil
	}
	return html.Section(html.Props{Class: "surface persona-admin-preview", Aria: map[string]string{"labelledby": "persona-admin-preview-title"}, Raw: map[string]any{"data-preview-subject": preview.Subject, "data-preview-conversation": preview.Conversation}},
		html.Div(html.Props{Class: "persona-admin-preview-heading"}, html.H2(html.Props{ID: "persona-admin-preview-title"}, ui.Text(personaAdminText(locale, "preview_title"))), html.P(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "preview_detail")))),
		html.Form(html.Props{Class: "persona-admin-preview-form", Raw: map[string]any{"data-persona-preview-check": "true"}},
			html.Div(html.Props{Class: "persona-admin-preview-controls"},
				personaAdminAgentSelect(locale, selected, len(snapshot.Personas) == 0, slices.Contains(snapshot.PreviewValidationFields, "persona"), personas),
				personaAdminSearchableSelect(locale, "subject", "choose_user", snapshot.PreviewSubjectID, subjectLabel, snapshot.TargetsState.Unavailable, slices.Contains(snapshot.PreviewValidationFields, "subject"), users),
				personaAdminSearchableSelect(locale, "conversation", "choose_conversation", snapshot.PreviewConversationID, conversationLabel, snapshot.TargetsState.Unavailable, slices.Contains(snapshot.PreviewValidationFields, "conversation"), conversations),
			),
			html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: snapshot.TargetsState.Unavailable}, ui.Text(personaAdminText(locale, "preview_check"))),
		),
		result,
	)
}

func personaAdminSimpleSelect(locale LocaleContext, suffix, labelKey, selected string, disabled bool, options []personaAdminComboboxOption) ui.Node {
	id := "persona-admin-preview-" + suffix
	items := []ui.Node{html.Option(html.Props{Value: "", Disabled: true, Selected: selected == ""}, ui.Text(personaAdminText(locale, labelKey+"_placeholder")))}
	for _, option := range options {
		items = append(items, html.Option(html.Props{Value: option.Value, Selected: option.Value == selected}, ui.Text(option.Label)))
	}
	return html.Div(html.Props{Class: "persona-admin-preview-picker"},
		html.Label(html.Props{For: id}, ui.Text(personaAdminText(locale, labelKey))),
		html.Select(html.Props{ID: id, Name: "persona_preview_" + suffix, Disabled: disabled, Raw: map[string]any{"data-persona-combobox": suffix}}, items...),
	)
}

func personaAdminPreviewAllows(preview PersonaAdminPreview) bool {
	return len(preview.EffectiveSkills) > 0 || len(preview.DerivedData) > 0 || strings.TrimSpace(preview.ReplyPlacement) != "" || strings.TrimSpace(preview.AuthorizationReason) != ""
}

func personaAdminTargetLabel(options []PersonaAdminTarget, selectedID, projected string, person bool, locale LocaleContext) string {
	wanted := strings.TrimSpace(selectedID)
	if wanted == "" {
		wanted = strings.TrimSpace(projected)
	}
	for _, option := range options {
		if option.ID != wanted && option.Label != wanted {
			continue
		}
		if person {
			name := strings.TrimSpace(option.Label)
			if len(strings.Fields(name)) < 2 {
				if fallback := PersonaWorkerLabel(option.ID, ""); len(strings.Fields(fallback)) >= 2 {
					name = fallback
				}
			}
			return name
		}
		return strings.TrimSuffix(personaAdminConversationOption(locale, option), personaAdminCompactSeparator+personaConversationKind(locale, option.Kind))
	}
	if person {
		return PersonaWorkerLabel(wanted, projected)
	}
	return humanizePersonaIdentifier(projected)
}

type personaAdminComboboxOption struct {
	Value string
	Label string
}

func personaAdminSearchableSelect(locale LocaleContext, suffix, labelKey, selected, selectedLabel string, disabled, invalid bool, options []personaAdminComboboxOption) ui.Node {
	id := "persona-admin-preview-" + suffix
	listID := id + "-options"
	items := make([]ui.Node, 0, len(options))
	for _, option := range options {
		items = append(items, html.Option(html.Props{Value: option.Label, Raw: map[string]any{"data-value": option.Value}}))
	}
	raw := map[string]any{"value": selectedLabel, "list": listID, "role": "combobox", "aria-autocomplete": "list", "autocomplete": "off", "data-persona-combobox": suffix, "data-selected-value": selected, "aria-describedby": id + "-error"}
	if invalid {
		raw["aria-invalid"] = "true"
	}
	return html.Div(html.Props{Class: "persona-admin-preview-picker"},
		html.Label(html.Props{For: id}, ui.Text(personaAdminText(locale, labelKey))),
		html.Input(html.Props{ID: id, Name: "persona_preview_" + suffix, Type: "search", Disabled: disabled, Placeholder: personaAdminText(locale, labelKey+"_placeholder"), Raw: raw}),
		html.Datalist(html.Props{ID: listID}, items...),
		html.P(html.Props{ID: id + "-error", Class: "field-error", Hidden: !invalid}, ui.Text(personaAdminPreviewValidationText(locale, suffix))),
	)
}

func personaAdminPersonOption(locale LocaleContext, option PersonaAdminTarget) string {
	name := strings.TrimSpace(option.Label)
	if len(strings.Fields(name)) < 2 {
		fallback := PersonaWorkerLabel(option.ID, "")
		if len(strings.Fields(fallback)) >= 2 {
			name = fallback
		}
	}
	if role := strings.TrimSpace(option.Role); role != "" {
		name += " — " + PersonaRoleLabelForLocale(locale, role, "")
	}
	return name
}

func personaAdminConversationOption(locale LocaleContext, option PersonaAdminTarget) string {
	label := strings.TrimSpace(option.Label)
	normalized := strings.ToLower(strings.NewReplacer(" ", "_", "-", "_").Replace(strings.TrimSpace(option.Kind)))
	if (normalized == "dm" || normalized == "direct_message") && option.ViewerDirect {
		return strings.ReplaceAll(locale.Text("agent_setup.your_direct_conversation"), "{persona}", label)
	}
	if normalized == "channel" || normalized == "private_channel" || normalized == "public_channel" {
		label = "#" + strings.TrimPrefix(label, "#")
	}
	if kind := strings.TrimSpace(option.Kind); kind != "" {
		label += personaAdminCompactSeparator + personaConversationKind(locale, kind)
	}
	return label
}

func personaAdminPreviewActions(locale LocaleContext, skills []PersonaAdminSkill, subject string) ([]string, bool) {
	actions := make([]string, 0, len(skills))
	hasReply := false
	firstName := strings.TrimSpace(subject)
	if fields := strings.Fields(firstName); len(fields) > 0 {
		firstName = fields[0]
	}
	for _, skill := range skills {
		switch lastPersonaIdentifierSegment(skill.ID) {
		case "knowledge_search_with_citations":
			actions = append(actions, locale.Text("agent_setup.preview_search_documents"))
		case "chat_reply":
			actions = append(actions, strings.ReplaceAll(locale.Text("agent_setup.preview_reply_to"), "{person}", firstName))
			hasReply = true
		default:
			actions = append(actions, PersonaSkillLabelForLocale(locale, skill.ID, skill.Name, skill.Description))
		}
	}
	return actions, hasReply
}

func personaConversationKind(locale LocaleContext, kind string) string {
	normalized := strings.ToLower(strings.NewReplacer(" ", "_", "-", "_").Replace(strings.TrimSpace(kind)))
	switch normalized {
	case "channel", "public_channel", "private_channel", "dm", "direct_message", "group":
		return personaAdminText(locale, "conversation_"+normalized)
	}
	return humanizePersonaIdentifier(kind)
}

func personaAdminSkillNames(locale LocaleContext, skills []PersonaAdminSkill) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, PersonaSkillLabelForLocale(locale, skill.ID, skill.Name, skill.Description))
	}
	return names
}

func personaAdminHumanizedList(locale LocaleContext, value string) string {
	parts := make([]string, 0)
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, PersonaRoleLabelForLocale(locale, part, ""))
		}
	}
	return strings.Join(parts, ", ")
}

func personaAdminCardCommandStatus(locale LocaleContext, code, action, commandPersonaID string, persona PersonaAdminPersona) string {
	if strings.TrimSpace(code) == "" || (commandPersonaID != "" && commandPersonaID != persona.ID) {
		return ""
	}
	if code == "success" {
		if strings.TrimSpace(action) == "" {
			return PersonaAdminCommandStatusText(locale, code)
		}
		return personaAdminCommandOutcomeText(locale, action, persona)
	}
	return PersonaAdminCommandStatusText(locale, code)
}

func personaAdminText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"eyebrow": {"Admin / Agents", "Administration / Agenten", "المسؤول / الوكلاء"}, "title": {"Agent setup", "Agenten einrichten", "إعداد الوكلاء"},
		"description":     {"Decide who each agent is, what it may read and do, and which conversations (channels and direct messages) people can use it in.", "Legen Sie fest, wer jeder Agent ist, was er lesen und tun darf und in welchen Unterhaltungen (Kanälen und Direktnachrichten) Menschen ihn verwenden können.", "حدّد هوية كل وكيل وما يمكنه قراءته وفعله وفي أي محادثات (قنوات ورسائل مباشرة) يمكن للأشخاص استخدامه."},
		"agent_explainer": {"An agent is an assistant people can ask on the Agents page or @mention in Chat. It only ever acts with the access of the person asking.", "Ein Agent ist ein Assistent, den Menschen auf der Seite „Agenten“ fragen oder im Chat mit @ erwähnen können. Er handelt immer nur mit dem Zugriff der fragenden Person.", "الوكيل مساعد يمكن للأشخاص سؤاله في صفحة الوكلاء أو الإشارة إليه بعلامة @ في المحادثة. ولا يتصرف إلا بصلاحيات الشخص السائل."},
		"catalog_title":   {"Agents", "Agenten", "الوكلاء"}, "catalog_detail": {"Each card shows what the agent can do and how close it is to being available.", "Jede Karte zeigt, was der Agent tun kann und wie nah er an der Verfügbarkeit ist.", "توضح كل بطاقة ما يمكن للوكيل فعله ومدى قربه من أن يصبح متاحاً."},
		"page_links": {"Agent pages", "Agentenseiten", "صفحات الوكلاء"}, "setup_link": {"Setup", "Einrichtung", "الإعداد"}, "operations_link": {"Operations", "Betrieb", "العمليات"}, "ask_agents_link": {"Ask", "Fragen", "اسأل"}, "ask_this_agent": {"Ask this agent", "Diesen Agenten fragen", "اسأل هذا الوكيل"},
		"breadcrumb": {"Breadcrumb", "Brotkrümelnavigation", "مسار التنقل"}, "breadcrumb_admin": {"Admin", "Administration", "المسؤول"}, "breadcrumb_agents": {"Agents", "Agenten", "الوكلاء"},
		"new_agent": {"New agent", "Neuer Agent", "وكيل جديد"}, "new_agent_no_template": {"New agents start from a reviewed template. No template is installed in this workspace yet. Ask your platform administrator to add one.", "Neue Agenten beginnen mit einer geprüften Vorlage. In diesem Arbeitsbereich ist noch keine Vorlage installiert. Bitten Sie Ihre Plattformadministration, eine hinzuzufügen.", "تبدأ الوكلاء الجدد من قالب تمت مراجعته. لا يوجد قالب مثبت في مساحة العمل هذه بعد. اطلب من مسؤول المنصة إضافة قالب."},
		"loading": {"Loading agent setup…", "Agenteneinrichtung wird geladen…", "جارٍ تحميل إعداد الوكلاء…"}, "unavailable_title": {"Agent setup is unavailable", "Agenteneinrichtung ist nicht verfügbar", "إعداد الوكلاء غير متاح"},
		"permission_denied": {"You do not have agent administration permission.", "Sie benötigen die Berechtigung zur Agentenverwaltung.", "ليست لديك صلاحية إدارة الوكلاء."}, "service_unavailable": {"The agent service is not connected to this workspace.", "Der Agentendienst ist nicht mit diesem Arbeitsbereich verbunden.", "خدمة الوكلاء غير متصلة بمساحة العمل هذه."}, "persona_store_unavailable": {"The agent store is not connected to this workspace.", "Der Agentenspeicher ist nicht mit diesem Arbeitsbereich verbunden.", "مخزن الوكلاء غير متصل بمساحة العمل هذه."}, "empty": {"No agents are configured. Choose New agent to get started.", "Es sind keine Agenten eingerichtet. Wählen Sie „Neuer Agent“, um zu beginnen.", "لم تتم تهيئة أي وكلاء. اختر «وكيل جديد» للبدء."},
		"retry":                 {"Try again", "Erneut versuchen", "حاول مرة أخرى"},
		"catalog_unavailable":   {"Agent details could not be loaded. Try again without leaving this page.", "Agentendetails konnten nicht geladen werden. Versuchen Sie es auf dieser Seite erneut.", "تعذر تحميل تفاصيل الوكلاء. حاول مرة أخرى دون مغادرة الصفحة."},
		"targets_unavailable":   {"People and conversations could not be loaded. Try again before checking access.", "Personen und Unterhaltungen konnten nicht geladen werden. Versuchen Sie es vor der Zugriffsprüfung erneut.", "تعذر تحميل الأشخاص والمحادثات. حاول مرة أخرى قبل التحقق من الوصول."},
		"preview_unavailable":   {"Effective access could not be checked for this selection. Try again.", "Der wirksame Zugriff konnte für diese Auswahl nicht geprüft werden. Versuchen Sie es erneut.", "تعذر التحقق من الوصول الفعلي لهذا الاختيار. حاول مرة أخرى."},
		"commands_unavailable":  {"Agent actions could not be loaded. Try again before changing an agent.", "Agentenaktionen konnten nicht geladen werden. Versuchen Sie es vor einer Änderung erneut.", "تعذر تحميل إجراءات الوكلاء. حاول مرة أخرى قبل تغيير أي وكيل."},
		"starters_unavailable":  {"New-agent templates could not be loaded. Try again to create an agent.", "Vorlagen für neue Agenten konnten nicht geladen werden. Versuchen Sie es erneut.", "تعذر تحميل قوالب الوكلاء الجدد. حاول مرة أخرى لإنشاء وكيل."},
		"documents_unavailable": {"Reference documents could not be loaded. Try again before editing them.", "Referenzdokumente konnten nicht geladen werden. Versuchen Sie es vor der Bearbeitung erneut.", "تعذر تحميل المستندات المرجعية. حاول مرة أخرى قبل تحريرها."},
		"owner":                 {"Business owner", "Fachverantwortliche Person", "المالك المسؤول"}, "steward": {"Technical contact", "Technischer Kontakt", "جهة الاتصال التقنية"}, "skills": {"What it can do", "Was er tun kann", "ما يمكنه فعله"}, "limits": {"Usage limits", "Nutzungsgrenzen", "حدود الاستخدام"}, "version": {"Version", "Version", "الإصدار"},
		"review_required": {"An independent reviewer must approve this version.", "Eine unabhängige prüfende Person muss diese Version genehmigen.", "يجب أن يوافق مراجع مستقل على هذا الإصدار."}, "review_approved": {"Approved by an independent reviewer.", "Von einer unabhängigen prüfenden Person genehmigt.", "وافق مراجع مستقل على الإصدار."}, "review_not_required": {"No publication review is recorded yet.", "Noch keine Veröffentlichungsprüfung erfasst.", "لم تُسجل مراجعة للنشر بعد."}, "reviewer": {"Reviewer", "Prüfende Person", "المراجع"}, "evaluation_required": {"Evaluation is required before publication.", "Vor der Veröffentlichung ist eine Evaluierung erforderlich.", "يلزم التقييم قبل النشر."}, "review_detail_unavailable": {"Review evidence is not available.", "Prüfnachweise sind nicht verfügbar.", "أدلة المراجعة غير متاحة."},
		"request_review": {"Request review", "Prüfung anfordern", "طلب المراجعة"}, "approve": {"Approve version", "Version genehmigen", "الموافقة على الإصدار"}, "reject": {"Reject version", "Version ablehnen", "رفض الإصدار"}, "publish": {"Publish version {version}", "Version {version} veröffentlichen", "نشر الإصدار {version}"}, "rollback": {"Rollback", "Rollback", "تراجع"}, "suspend": {"Suspend", "Aussetzen", "إيقاف"}, "retire": {"Retire", "Stilllegen", "إحالة للتقاعد"},
		"actions_unavailable": {"Actions are unavailable while the agent service is disconnected.", "Aktionen sind nicht verfügbar, solange der Agentendienst getrennt ist.", "الإجراءات غير متاحة أثناء انقطاع خدمة الوكلاء."},
		"preview_title":       {"Test what an agent can do for someone", "Testen, was ein Agent für eine Person tun kann", "اختبار ما يمكن للوكيل فعله لشخص"}, "preview_detail": {"Pick a person and a conversation to see what the selected agent could read and do for them there.", "Wählen Sie eine Person und eine Unterhaltung, um zu sehen, was der ausgewählte Agent dort für sie lesen und tun könnte.", "اختر شخصاً ومحادثة لمعرفة ما يمكن للوكيل المحدد قراءته وفعله له هناك."}, "choose_persona": {"Agent", "Agent", "الوكيل"}, "choose_user": {"Person", "Person", "الشخص"}, "choose_conversation": {"Conversation", "Unterhaltung", "المحادثة"}, "preview_check": {"Check", "Prüfen", "تحقق"},
		"no_limits": {"No limits set", "Keine Grenzen festgelegt", "لم يتم تحديد حدود"}, "set_limit": {"Set a limit", "Limit festlegen", "تعيين حد"}, "none_set": {"None set", "Keine festgelegt", "لم يتم التحديد"}, "unknown_person": {"Unknown person", "Unbekannte Person", "شخص غير معروف"},
		"nothing_available": {"Nothing available", "Nichts verfügbar", "لا شيء متاح"}, "who_can_use": {"Who can use it", "Wer ihn verwenden kann", "من يمكنه استخدامه"}, "everyone_roles": {"People in these {count} roles who are members of a conversation where this agent is added", "Personen in diesen {count} Rollen, die Mitglieder einer Unterhaltung sind, der dieser Agent hinzugefügt wurde", "الأشخاص في هذه الأدوار الـ {count} الذين هم أعضاء في محادثة أضيف إليها هذا الوكيل"},
		"what_can_read": {"What it can read", "Was er lesen kann", "ما يمكنه قراءته"}, "what_can_read_prefix": {"Official documents in each conversation it is added to, plus the documents listed under '", "Offizielle Dokumente in jeder Unterhaltung, der er hinzugefügt wurde, sowie die Dokumente unter ‚", "المستندات الرسمية في كل محادثة تمت إضافته إليها، بالإضافة إلى المستندات المدرجة ضمن «"}, "what_can_read_suffix": {"'.", "‘.", "»."}, "documents_picker_label": {"Documents this agent reads", "Dokumente, die dieser Agent liest", "المستندات التي يقرأها هذا الوكيل"}, "where_installed": {"Where it is added", "Wo er hinzugefügt wurde", "أين تمت إضافته"},
		"no_installations": {"Not added to a conversation yet. Publish it first, then add it to a conversation here.", "Noch zu keiner Unterhaltung hinzugefügt. Veröffentlichen Sie ihn zuerst und fügen Sie ihn dann hier einer Unterhaltung hinzu.", "لم تتم إضافته إلى محادثة بعد. انشره أولاً، ثم أضفه إلى محادثة من هنا."},
		"add_conversation": {"Add to a conversation", "Zu einer Unterhaltung hinzufügen", "إضافة إلى محادثة"}, "add": {"Add", "Hinzufügen", "إضافة"}, "remove_placement": {"Remove", "Entfernen", "إزالة"}, "running_version": {"running version {version}", "führt Version {version} aus", "يشغّل الإصدار {version}"}, "direct_conversation": {"Direct conversation with {person}", "Direkte Unterhaltung mit {person}", "محادثة مباشرة مع {person}"},
		"documents_in_conversation": {"{count} documents in this conversation", "{count} Dokumente in dieser Unterhaltung", "{count} مستندات في هذه المحادثة"}, "documents_count_unavailable": {"Open this conversation's Documents", "Dokumente dieser Unterhaltung öffnen", "فتح مستندات هذه المحادثة"}, "no_placed_documents": {"No official documents are in this conversation. The agent will not find anything to cite.", "In dieser Unterhaltung gibt es keine offiziellen Dokumente. Der Agent findet nichts zum Zitieren.", "لا توجد مستندات رسمية في هذه المحادثة. لن يجد الوكيل شيئاً للاستشهاد به."}, "place_document": {"Place a document", "Dokument platzieren", "وضع مستند"},
		"documents_in_conversation_one": {"{count} document in this conversation", "{count} Dokument in dieser Unterhaltung", "مستند واحد في هذه المحادثة"}, "documents_in_conversation_two": {"{count} documents in this conversation", "{count} Dokumente in dieser Unterhaltung", "مستندان في هذه المحادثة"}, "documents_in_conversation_many": {"{count} documents in this conversation", "{count} Dokumente in dieser Unterhaltung", "{count} مستندات في هذه المحادثة"},
		"skill_uses": {"Uses:", "Verwendet:", "تستخدم:"}, "skill_description_missing": {"Performs this approved action within the access shown here.", "Führt diese genehmigte Aktion innerhalb des hier gezeigten Zugriffs aus.", "ينفذ هذا الإجراء المعتمد ضمن الوصول الموضح هنا."},
		"review_step": {"Independent review", "Unabhängige Prüfung", "مراجعة مستقلة"}, "evaluation_complete": {"Evaluation passed.", "Evaluierung bestanden.", "تم اجتياز التقييم."},
		"lifecycle_progress": {"Publication progress", "Veröffentlichungsfortschritt", "تقدم النشر"}, "step_draft": {"Draft", "Entwurf", "مسودة"},
		"step_review": {"Reviewed", "Geprüft", "تمت مراجعته"}, "step_evaluated": {"Evaluated", "Evaluiert", "تم تقييمه"}, "step_published": {"Published", "Veröffentlicht", "منشور"}, "lifecycle_progress_text": {"Step {current} of {total}: {state}", "Schritt {current} von {total}: {state}", "الخطوة {current} من {total}: {state}"},
		"request_review_unavailable": {"Requesting review requires an Agent reviewer. Ask an Agent administrator to assign one.", "Für die Prüfungsanfrage ist eine Agentenprüfung erforderlich. Bitten Sie die Agentenadministration um Zuweisung.", "يتطلب طلب المراجعة مراجع وكلاء. اطلب من مسؤول الوكلاء تعيينه."},
		"review_unavailable":         {"Independent review requires the Agent reviewer role. An Agent administrator can grant it.", "Die unabhängige Prüfung erfordert die Rolle „Agentenprüfung“. Die Agentenadministration kann sie vergeben.", "تتطلب المراجعة المستقلة دور مراجع الوكلاء. يمكن لمسؤول الوكلاء منحه."},
		"publish_needs_review":       {"Publish needs an approved independent review.", "Die Veröffentlichung benötigt eine genehmigte unabhängige Prüfung.", "يتطلب النشر مراجعة مستقلة معتمدة."},
		"publish_needs_evaluation":   {"Publish needs a passing evaluation.", "Die Veröffentlichung benötigt eine bestandene Bewertung.", "يتطلب النشر تقييماً ناجحاً."},
		"publish_permission":         {"Publishing requires the Agent publisher role. An Agent administrator can grant it.", "Die Veröffentlichung erfordert die Rolle „Agentenveröffentlichung“. Die Agentenadministration kann sie vergeben.", "يتطلب النشر دور ناشر الوكلاء. يمكن لمسؤول الوكلاء منحه."},
		"already_published":          {"This agent is already published.", "Dieser Agent ist bereits veröffentlicht.", "هذا الوكيل منشور بالفعل."}, "retired_locked": {"A retired agent cannot be published again.", "Ein stillgelegter Agent kann nicht erneut veröffentlicht werden.", "لا يمكن نشر وكيل متقاعد مرة أخرى."},
		"rollback_requires_publication": {"Rollback becomes available after a version has been published.", "Rollback wird nach der Veröffentlichung einer Version verfügbar.", "يصبح التراجع متاحاً بعد نشر إصدار."},
		"rollback_permission":           {"Rollback requires the Agent lifecycle administrator role. An Agent administrator can grant it.", "Rollback erfordert die Rolle „Agenten-Lebenszyklusverwaltung“. Die Agentenadministration kann sie vergeben.", "يتطلب التراجع دور مسؤول دورة حياة الوكلاء. يمكن لمسؤول الوكلاء منحه."},
		"suspend_requires_publication":  {"Suspend becomes available after publication.", "Aussetzen wird nach der Veröffentlichung verfügbar.", "يصبح الإيقاف متاحاً بعد النشر."},
		"suspend_permission":            {"Suspending requires the Agent lifecycle administrator role. An Agent administrator can grant it.", "Das Aussetzen erfordert die Rolle „Agenten-Lebenszyklusverwaltung“. Die Agentenadministration kann sie vergeben.", "يتطلب الإيقاف دور مسؤول دورة حياة الوكلاء. يمكن لمسؤول الوكلاء منحه."},
		"retire_permission":             {"Retiring requires the Agent lifecycle administrator role. An Agent administrator can grant it.", "Das Stilllegen erfordert die Rolle „Agenten-Lebenszyklusverwaltung“. Die Agentenadministration kann sie vergeben.", "تتطلب الإحالة للتقاعد دور مسؤول دورة حياة الوكلاء. يمكن لمسؤول الوكلاء منحه."},
		"already_retired":               {"This agent is already retired.", "Dieser Agent ist bereits stillgelegt.", "هذا الوكيل متقاعد بالفعل."},
		"secondary_actions":             {"More actions", "Weitere Aktionen", "إجراءات أخرى"}, "more": {"More", "Mehr", "المزيد"},
		"rollback_confirm": {"Roll back", "Rollback", "التراجع عن"}, "rollback_confirm_action": {"Confirm rollback", "Rollback bestätigen", "تأكيد التراجع"},
		"suspend_confirm": {"Suspend", "Aussetzen", "إيقاف"}, "suspend_confirm_action": {"Confirm suspension", "Aussetzen bestätigen", "تأكيد الإيقاف"},
		"retire_confirm": {"Retire", "Stilllegen", "تقاعد"}, "retire_confirm_action": {"Confirm retirement", "Stilllegung bestätigen", "تأكيد التقاعد"},
		"technical_details": {"Technical details", "Technische Details", "التفاصيل التقنية"}, "technical_persona_id": {"Persona identifier", "Persona-Kennung", "معرّف الشخصية"},
		"technical_state": {"Lifecycle code", "Lebenszykluscode", "رمز دورة الحياة"}, "technical_owner": {"Owner reference", "Eigentümerreferenz", "مرجع المالك"},
		"technical_steward": {"Steward reference", "Betreuungsreferenz", "مرجع المشرف"}, "technical_audience": {"Audience identifiers", "Zielgruppenkennungen", "معرّفات الجمهور"},
		"technical_skill": {"Skill pin", "Fähigkeits-Pin", "تثبيت المهارة"},
		"run_evaluation":  {"Run evaluation", "Evaluierung starten", "تشغيل التقييم"}, "evaluation_explanation": {"Checks this version against the approved scenarios. It usually takes a few minutes.", "Prüft diese Version anhand der genehmigten Szenarien. Dies dauert normalerweise einige Minuten.", "يتحقق من هذا الإصدار وفق السيناريوهات المعتمدة. يستغرق عادةً بضع دقائق."},
		"evaluation_done_by": {"Evaluation is done by", "Die Evaluierung wird durchgeführt von", "يجري التقييم بواسطة"}, "message_them": {"Message them", "Nachricht senden", "أرسل إليه رسالة"}, "approved_by": {"Approved by", "Genehmigt von", "وافق عليه"}, "reviewed_and_approved_by": {"Reviewed and approved by", "Geprüft und genehmigt von", "تمت مراجعته والموافقة عليه بواسطة"}, "independent_reviewer": {"an independent reviewer", "einer unabhängigen prüfenden Person", "مراجع مستقل"}, "on_date": {"on", "am", "في"}, "evaluation_passed": {"Evaluation passed", "Evaluierung bestanden", "نجح التقييم"}, "published_on": {"Published on", "Veröffentlicht am", "نُشر في"}, "posts_reply": {"Posts a reply", "Sendet eine Antwort", "ينشر رداً"},
		"live_and_draft":          {"Version {live} is live. Version {draft} is a draft.", "Version {live} ist aktiv. Version {draft} ist ein Entwurf.", "الإصدار {live} مباشر. الإصدار {draft} مسودة."},
		"next_draft":              {"Request review next; an independent reviewer must approve version {version}.", "Fordern Sie als Nächstes eine Prüfung an; eine unabhängige Person muss Version {version} genehmigen.", "اطلب المراجعة تالياً؛ يجب أن يوافق مراجع مستقل على الإصدار {version}."},
		"next_in_review":          {"{reviewer} must review version {version}; the author cannot review their own version.", "{reviewer} muss Version {version} prüfen; Verfassende dürfen ihre eigene Version nicht prüfen.", "يجب أن يراجع {reviewer} الإصدار {version}؛ لا يمكن للمؤلف مراجعة إصداره."},
		"next_reviewed":           {"Run evaluation next; it checks version {version} against the approved cases.", "Führen Sie als Nächstes die Evaluation aus; sie prüft Version {version} anhand der genehmigten Fälle.", "شغّل التقييم تالياً؛ فهو يتحقق من الإصدار {version} مقابل الحالات المعتمدة."},
		"next_evaluation_running": {"Evaluation is running; the result will appear here when it finishes.", "Die Evaluation läuft; das Ergebnis erscheint nach Abschluss hier.", "التقييم قيد التشغيل؛ ستظهر النتيجة هنا عند انتهائه."},
		"next_evaluation_failed":  {"Fix the failing cases, then run evaluation again.", "Beheben Sie die fehlgeschlagenen Fälle und führen Sie die Evaluation erneut aus.", "أصلح الحالات الفاشلة، ثم شغّل التقييم مرة أخرى."},
		"next_evaluated":          {"Publish version {version} next; conversations running version {live} keep it until they are rolled forward.", "Veröffentlichen Sie als Nächstes Version {version}; Unterhaltungen mit Version {live} behalten sie bis zur Umstellung.", "انشر الإصدار {version} تالياً؛ تحتفظ المحادثات التي تشغّل الإصدار {live} به حتى يتم نقلها."},
		"next_published":          {"Version {version} is live; people can use it in the conversations listed above.", "Version {version} ist aktiv; Personen können sie in den oben aufgeführten Unterhaltungen verwenden.", "الإصدار {version} مباشر؛ يمكن للأشخاص استخدامه في المحادثات المذكورة أعلاه."},
		"evaluation_permission":   {"Running an evaluation requires the Agent evaluator role. An Agent administrator can grant it.", "Das Starten einer Evaluierung erfordert die Rolle „Agentenevaluierung“. Die Agentenadministration kann sie vergeben.", "يتطلب تشغيل التقييم دور مقيّم الوكلاء. يمكن لمسؤول الوكلاء منحه."}, "evaluation_running": {"Evaluation is running. Results will appear here.", "Die Evaluierung läuft. Die Ergebnisse werden hier angezeigt.", "التقييم قيد التشغيل. ستظهر النتائج هنا."}, "evaluation_running_action": {"Evaluation running", "Evaluation läuft", "التقييم قيد التشغيل"},
		"evaluation_result": {"Evaluation result: {passed} passed, {failed} failed.", "Evaluierungsergebnis: {passed} bestanden, {failed} fehlgeschlagen.", "نتيجة التقييم: نجح {passed} وفشل {failed}."}, "view_failures": {"View failures", "Fehler anzeigen", "عرض حالات الفشل"}, "failed_cases": {"Failed cases", "Fehlgeschlagene Fälle", "الحالات الفاشلة"},
		"choose_persona_placeholder": {"Choose an agent", "Agenten auswählen", "اختر وكيلاً"}, "choose_user_placeholder": {"Choose a person", "Person auswählen", "اختر شخصاً"},
		"choose_conversation_placeholder": {"Choose a conversation", "Unterhaltung auswählen", "اختر محادثة"}, "search_persona": {"Search agents", "Agenten suchen", "البحث عن الوكلاء"},
		"search_subject": {"Search users", "Benutzer suchen", "البحث عن المستخدمين"}, "search_conversation": {"Search conversations", "Unterhaltungen suchen", "البحث عن المحادثات"},
		"preview_choose_all":      {"Choose an agent, a person and a conversation, then press Check.", "Wählen Sie einen Agenten, eine Person und eine Unterhaltung und drücken Sie dann „Prüfen“.", "اختر وكيلاً وشخصاً ومحادثة، ثم اضغط «تحقق»."},
		"preview_sentence":        {"{person} can ask {persona} in {conversation}.", "{person} kann {persona} in {conversation} fragen.", "يمكن لـ {person} سؤال {persona} في {conversation}."},
		"preview_denied_sentence": {"{person} cannot use {persona} in {conversation}.", "{person} kann {persona} in {conversation} nicht verwenden.", "لا يمكن لـ {person} استخدام {persona} في {conversation}."},
		"preview_reads":           {"It can read {content}.", "Sie kann {content} lesen.", "يمكنها قراءة {content}."},
		"preview_does":            {"It could do: {content}.", "Er könnte Folgendes tun: {content}.", "يمكنه فعل ما يلي: {content}."},
		"preview_replies":         {"It replies in the {placement}, visible to {audience}.", "Sie antwortet in {placement}, sichtbar für {audience}.", "ترد في {placement}، وتكون مرئية لـ {audience}."},
		"preview_can_use":         {"can use", "kann verwenden", "يمكنه استخدام"}, "preview_in": {"in", "in", "في"}, "preview_gets_nothing": {"does not receive agent skills in", "erhält keine Agentenfähigkeiten in", "لا يحصل على مهارات الوكيل في"},
		"preview_withheld":     {"Some access is withheld because:", "Ein Teil des Zugriffs wird zurückgehalten, weil:", "تم حجب بعض الوصول لأن:"},
		"conversation_channel": {"Channel", "Kanal", "قناة"}, "conversation_public_channel": {"Public channel", "Öffentlicher Kanal", "قناة عامة"}, "conversation_private_channel": {"Private channel", "Privater Kanal", "قناة خاصة"},
		"conversation_dm": {"Direct message", "Direktnachricht", "رسالة مباشرة"}, "conversation_direct_message": {"Direct message", "Direktnachricht", "رسالة مباشرة"},
		"conversation_group":       {"Group", "Gruppe", "مجموعة"},
		"reference_documents_none": {"None", "Keine", "لا يوجد"},
	}
	if values, ok := copy[key]; ok {
		index := 0
		switch locale.Resolved {
		case "de-DE":
			index = 1
		case "ar":
			index = 2
		}
		return values[index]
	}
	return locale.Text("persona_admin." + key)
}
