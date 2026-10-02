package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// PersonaMentionContext is the already-authorized conversation context used
// for discovery. A persona is visible in a room only when it is installed in
// that room, except that a one-to-one DM may discover any persona available to
// the viewer.
type PersonaMentionContext struct {
	OneToOne                bool
	AlwaysPrivate           bool
	AudienceFloorWillDivert bool
}

type PersonaMentionPerson struct {
	ID   string
	Name string
}

type PersonaMentionSkill struct {
	Name string
	Tier string
}

// PersonaChatActor is the trusted identity envelope used by chat surfaces.
// A missing or incomplete envelope is rendered as unavailable; display names
// and message text never infer persona status.
type PersonaChatActor struct {
	Icon          agenticon.Value
	IconRevision  int64
	PersonaID     string
	AgentID       string
	InvokerHandle string
	Trusted       bool
}

// PersonaMentionPersona is a safe server projection. Invocable and
// AudienceIncludesViewer are required fields at this boundary; the component
// never treats a missing value as permission.
type PersonaMentionPersona struct {
	ID                      string
	Name                    string
	Handle                  string
	Icon                    agenticon.Value
	IconRevision            int64
	Initials                string
	AvatarURL               string
	Purpose                 string
	Owner                   string
	Version                 string
	Invocable               bool
	InstalledInConversation bool
	AudienceIncludesViewer  bool
	Skills                  []PersonaMentionSkill
	DataClasses             []string
	CannotDo                []string
	ReplyPlacement          PersonaReplyPlacement
	Actor                   *PersonaChatActor
}

type PersonaReplyPlacement string

const (
	PersonaReplyInThread        PersonaReplyPlacement = "thread"
	PersonaReplyPrivateAlways   PersonaReplyPlacement = "private_always"
	PersonaReplyPrivateAudience PersonaReplyPlacement = "private_audience"
)

type PersonaMentionMenuProps struct {
	Locale         LocaleContext
	Target         string
	Query          string
	Active         int
	Context        PersonaMentionContext
	People         []PersonaMentionPerson
	Agents         []PersonaMentionPersona
	Profile        *PersonaMentionPersona
	Loading        bool
	Failed         bool
	CanAdminAgents bool
}

type PersonaMentionKeyAction string

const (
	PersonaMentionKeyNone     PersonaMentionKeyAction = ""
	PersonaMentionKeyNext     PersonaMentionKeyAction = "next"
	PersonaMentionKeyPrevious PersonaMentionKeyAction = "previous"
	PersonaMentionKeySelect   PersonaMentionKeyAction = "select"
	PersonaMentionKeyClose    PersonaMentionKeyAction = "close"
)

// FilterInvocablePersonas is the security boundary for the menu payload. It
// deliberately drops hidden records rather than passing them to a renderer
// and hiding them with CSS.
func FilterInvocablePersonas(personas []PersonaMentionPersona, context PersonaMentionContext) []PersonaMentionPersona {
	visible := make([]PersonaMentionPersona, 0, len(personas))
	for _, persona := range personas {
		if !personaMentionEligible(persona, context) {
			continue
		}
		visible = append(visible, persona)
	}
	return visible
}

func personaMentionEligible(persona PersonaMentionPersona, context PersonaMentionContext) bool {
	return strings.TrimSpace(persona.ID) != "" && strings.TrimSpace(persona.Name) != "" && persona.Invocable && persona.AudienceIncludesViewer && (persona.InstalledInConversation || context.OneToOne)
}

func personaMentionMatches(query, name string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(name)), query)
}

// NextPersonaMention wraps keyboard selection just like the existing people
// mention store. Keeping this pure lets the chat state store own the active
// index without giving this presentation component mutable global state.
func NextPersonaMention(active, delta, count int) int {
	if count <= 0 {
		return 0
	}
	return ((active+delta)%count + count) % count
}

// PersonaMentionActionForKey keeps the browser bridge's keyboard contract
// explicit and testable. Enter and Tab both accept the highlighted option.
func PersonaMentionActionForKey(key string) PersonaMentionKeyAction {
	switch key {
	case "ArrowDown":
		return PersonaMentionKeyNext
	case "ArrowUp":
		return PersonaMentionKeyPrevious
	case "Enter", "Tab":
		return PersonaMentionKeySelect
	case "Escape":
		return PersonaMentionKeyClose
	default:
		return PersonaMentionKeyNone
	}
}

func PersonaMentionInputAria(target string, active int, open bool) map[string]string {
	aria := map[string]string{"autocomplete": "list", "haspopup": "listbox", "expanded": personaBoolString(open), "keyshortcuts": "ArrowDown ArrowUp Enter Tab Escape"}
	if open {
		aria["controls"] = target + "-persona-mentions"
		aria["activedescendant"] = target + "-persona-mention-" + personaNumber(active+1)
	}
	return aria
}

// PersonaMentionMenu renders a grouped People/Agents menu. The caller can
// mount this beside the existing chat composer and continue to use the
// mention store's keyboard selection and data-action dispatch.
func PersonaMentionMenu(props PersonaMentionMenuProps) ui.Node {
	locale := props.Locale
	target := safeAgentDOMToken(props.Target)
	if target == "unknown" {
		target = "chat"
	}
	people := make([]PersonaMentionPerson, 0, len(props.People))
	for _, person := range props.People {
		if strings.TrimSpace(person.ID) != "" && strings.TrimSpace(person.Name) != "" && personaMentionMatches(props.Query, person.Name) {
			people = append(people, person)
		}
	}
	agents := FilterInvocablePersonas(props.Agents, props.Context)
	eligibleAgentCount := len(agents)
	filteredAgents := agents[:0]
	for _, agent := range agents {
		if personaMentionMatches(props.Query, agent.Name) || personaMentionMatches(props.Query, agent.Handle) || personaMentionMatches(props.Query, agent.ID) {
			filteredAgents = append(filteredAgents, agent)
		}
	}
	agents = filteredAgents

	optionCount := len(people) + len(agents)
	if props.Loading || props.Failed {
		optionCount = len(people)
	}
	active := props.Active
	if active < 0 || active >= optionCount {
		active = 0
	}
	children := []ui.Node{html.P(html.Props{ID: target + "-persona-mention-heading", Class: "sr-only", Text: personaMentionText(locale, "menu")})}
	optionIndex := 0
	children = append(children, html.H3(html.Props{Class: "persona-mention-group", Text: personaMentionText(locale, "agents")}))
	if props.Loading {
		children = append(children, html.P(html.Props{Class: "persona-mention-state loading", Role: "status", Aria: map[string]string{"live": "polite"}, Text: personaMentionText(locale, "loading")}))
	} else if props.Failed {
		children = append(children, html.Div(html.Props{Class: "persona-mention-state error", Role: "status", Aria: map[string]string{"live": "assertive"}},
			html.P(html.Props{Text: personaMentionText(locale, "load_failed")}),
			html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "persona-mention-retry"}}, ui.Text(personaMentionText(locale, "retry")))))
	} else if len(agents) > 0 {
		for _, agent := range agents {
			children = append(children, personaMentionAgentOption(locale, target, agent, optionIndex, active == optionIndex))
			optionIndex++
		}
	} else {
		if strings.TrimSpace(props.Query) != "" && eligibleAgentCount > 0 {
			children = append(children, html.P(html.Props{Class: "persona-mention-state empty", Role: "status", Aria: map[string]string{"live": "polite"}, Text: personaMentionText(locale, "no_agent_matches")}))
		} else {
			actions := []ui.Node{html.A(html.Props{Class: "persona-mention-action", Href: "/workspace/app/chat/agents", Text: personaMentionText(locale, "agents_page")})}
			if props.CanAdminAgents {
				actions = append(actions, html.A(html.Props{Class: "persona-mention-action", Href: "/workspace/app/admin/personas", Text: personaMentionText(locale, "add_agent")}))
			}
			children = append(children, html.Div(html.Props{Class: "persona-mention-empty"},
				html.P(html.Props{Role: "status", Aria: map[string]string{"live": "polite"}, Text: personaMentionText(locale, "no_agents")}),
				html.Div(html.Props{Class: "persona-mention-actions"}, actions...)))
		}
	}
	if len(people) > 0 {
		children = append(children, html.H3(html.Props{Class: "persona-mention-group", Text: personaMentionText(locale, "people")}))
		for _, person := range people {
			children = append(children, personaMentionPersonOption(target, person, optionIndex, active == optionIndex))
			optionIndex++
		}
	}

	root := html.Div(html.Props{
		ID: target + "-persona-mentions", Class: "persona-mention-menu", Role: "listbox", Dir: string(locale.Direction),
		Aria: map[string]string{"labelledby": target + "-persona-mention-heading", "live": "polite"},
		Data: map[string]string{"query": props.Query, "active": personaNumber(active), "option-count": personaNumber(optionCount)},
	}, children...)
	if props.Profile == nil {
		return root
	}
	return html.Div(html.Props{Class: "persona-mention-surface", Dir: string(locale.Direction)}, root, PersonaProfileCard(PersonaProfileCardProps{Locale: locale, Context: props.Context, Persona: *props.Profile}))
}

func personaMentionPersonOption(target string, person PersonaMentionPerson, index int, selected bool) ui.Node {
	return html.Button(html.Props{
		ID: target + "-persona-mention-" + personaNumber(index+1), Class: "persona-mention-option", Type: "button", Role: "option", TabIndex: -1,
		Data: map[string]string{"action": "mention-pick", "kind": "person", "id": person.ID},
		Aria: map[string]string{"selected": personaBoolString(selected)},
	}, ui.Text("@"+person.Name))
}

func personaMentionAgentOption(locale LocaleContext, target string, agent PersonaMentionPersona, index int, selected bool) ui.Node {
	optionID := target + "-persona-mention-" + personaNumber(index+1)
	handle := personaMentionHandle(agent)
	initials := strings.TrimSpace(agent.Initials)
	if initials == "" {
		initials = personaMentionInitials(agent.Name)
	}
	return html.Div(html.Props{Class: "persona-mention-agent-row"},
		html.Button(html.Props{
			ID: optionID, Class: "persona-mention-option persona-mention-agent", Type: "button", Role: "option", TabIndex: -1,
			Data: map[string]string{"action": "mention-pick", "kind": "agent", "id": agent.ID, "profile-id": agent.ID},
			Aria: map[string]string{"selected": personaBoolString(selected), "label": agent.Name + ", @" + handle + ", " + personaMentionText(locale, "agent")},
		}, agenticon.NodeFor(agent.Icon, agent.ID),
			html.Span(html.Props{Class: "persona-mention-identity"},
				html.Strong(html.Props{Class: "persona-mention-name", Text: agent.Name}),
				html.Small(html.Props{Class: "persona-mention-handle", Dir: "ltr", Text: "@" + handle})),
			html.Small(html.Props{Class: "persona-mention-purpose", Text: agent.Purpose}), personaMentionTypeBadge(locale)),
		html.Button(html.Props{
			Class: "persona-mention-profile", Type: "button", Data: map[string]string{"action": "persona-profile", "id": agent.ID},
			Aria: map[string]string{"label": personaMentionText(locale, "view_profile") + ": " + agent.Name, "controls": target + "-persona-profile-" + safeAgentDOMToken(agent.ID)},
		}, ui.Text(personaMentionText(locale, "profile"))),
	)
}

func personaMentionHandle(agent PersonaMentionPersona) string {
	handle := strings.TrimSpace(strings.TrimPrefix(agent.Handle, "@"))
	if handle != "" {
		return handle
	}
	return strings.Join(strings.Fields(strings.ToLower(agent.Name)), "-")
}

func personaMentionInitials(name string) string {
	words := strings.Fields(name)
	if len(words) == 0 {
		return "?"
	}
	initials := []rune(strings.ToUpper(words[0]))[:1]
	if len(words) > 1 {
		initials = append(initials, []rune(strings.ToUpper(words[len(words)-1]))[0])
	}
	return string(initials)
}

func personaMentionTypeBadge(locale LocaleContext) ui.Node {
	agent := personaMentionText(locale, "agent")
	return html.Span(html.Props{Class: "agent-badge", Aria: map[string]string{"label": agent}, Text: agent})
}

// PersonaAgentBadge renders the permanent agent marker and explicit invoker
// attribution for a mention chip. It fails closed when actor fields are absent.
func PersonaAgentBadge(actor *PersonaChatActor) ui.Node {
	return personaAgentBadge(ResolveProductLocale("en-US"), actor)
}

func personaAgentBadge(locale LocaleContext, actor *PersonaChatActor) ui.Node {
	if actor == nil || !actor.Trusted || strings.TrimSpace(actor.PersonaID) == "" || strings.TrimSpace(actor.AgentID) == "" {
		unavailable := personaMentionText(locale, "identity_unavailable")
		return html.Span(html.Props{Class: "agent-badge unavailable", Aria: map[string]string{"label": unavailable}, Text: unavailable})
	}
	attribution := personaMentionText(locale, "acting_unavailable")
	if strings.TrimSpace(actor.InvokerHandle) != "" {
		attribution = personaMentionText(locale, "acting_for") + " @" + strings.TrimSpace(actor.InvokerHandle)
	}
	agent := personaMentionText(locale, "agent")
	return html.Span(html.Props{Class: "agent-badge", Aria: map[string]string{"label": agent + "; " + attribution}}, html.Strong(html.Props{Text: agent}), html.Span(html.Props{Class: "agent-attribution", Text: attribution}))
}

func personaBoolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func personaNumber(value int) string { return strconv.Itoa(value) }

type PersonaProfileCardProps struct {
	Locale  LocaleContext
	Context PersonaMentionContext
	Persona PersonaMentionPersona
}

// PersonaProfileCard is shared by menu, post and member-list placements. The
// card accepts only the same authorized persona projection as the menu and
// emits no detail for a persona the viewer cannot invoke in this context.
func PersonaProfileCard(props PersonaProfileCardProps) ui.Node {
	if !personaMentionEligible(props.Persona, props.Context) {
		return html.Div(html.Props{Class: "persona-profile-slot", Hidden: true})
	}
	locale := props.Locale
	persona := props.Persona
	profileID := "persona-profile-" + safeAgentDOMToken(persona.ID)
	children := []ui.Node{
		html.Div(html.Props{Class: "persona-profile-heading"},
			html.H2(html.Props{ID: profileID + "-title", Text: persona.Name}),
			html.Button(html.Props{Class: "persona-profile-close", Type: "button", Data: map[string]string{"action": "persona-profile-close", "id": persona.ID}, Aria: map[string]string{"label": personaMentionText(locale, "close_profile")}}, ui.Text("×")),
		),
		html.P(html.Props{Class: "persona-profile-purpose"},
			html.Strong(html.Props{Text: personaMentionText(locale, "purpose") + ": "}), ui.Text(persona.Purpose)),
		personaProfileFact(locale, "owner", persona.Owner),
		personaProfileFact(locale, "version", persona.Version),
		html.Section(html.Props{Class: "persona-profile-section", Aria: map[string]string{"label": personaMentionText(locale, "skills")}},
			html.H3(html.Props{Text: personaMentionText(locale, "skills")}), personaSkills(locale, persona.Skills)),
		html.Section(html.Props{Class: "persona-profile-section", Aria: map[string]string{"label": personaMentionText(locale, "data_reach")}},
			html.H3(html.Props{Text: personaMentionText(locale, "data_reach")}), personaValues(persona.DataClasses, "persona-profile-data-class")),
		html.P(html.Props{Class: "persona-profile-access", Text: personaMentionText(locale, "acts_with_access")}),
		html.Section(html.Props{Class: "persona-profile-section", Aria: map[string]string{"label": personaMentionText(locale, "cannot_do")}},
			html.H3(html.Props{Text: personaMentionText(locale, "cannot_do")}), personaValues(persona.CannotDo, "persona-profile-cannot-do")),
		html.P(html.Props{Class: "persona-profile-replies"}, html.Strong(html.Props{Text: personaMentionText(locale, "replies") + ": "}), ui.Text(personaReplyPlacementLabel(locale, persona.ReplyPlacement, props.Context))),
	}
	return html.Article(html.Props{ID: profileID, Class: "persona-profile-card", Role: "dialog", Dir: string(locale.Direction), Aria: map[string]string{"modal": "false", "labelledby": profileID + "-title"}}, children...)
}

func personaProfileFact(locale LocaleContext, key, value string) ui.Node {
	if strings.TrimSpace(value) == "" {
		return html.P(html.Props{Class: "persona-profile-fact muted", Text: personaMentionText(locale, key) + ": " + personaMentionText(locale, "unavailable")})
	}
	return html.P(html.Props{Class: "persona-profile-fact"}, html.Strong(html.Props{Text: personaMentionText(locale, key) + ": "}), ui.Text(value))
}

func personaSkills(locale LocaleContext, skills []PersonaMentionSkill) ui.Node {
	if len(skills) == 0 {
		return html.P(html.Props{Class: "muted", Text: personaMentionText(locale, "none_listed")})
	}
	rows := make([]ui.Node, 0, len(skills))
	for _, skill := range skills {
		if strings.TrimSpace(skill.Name) == "" {
			continue
		}
		rows = append(rows, html.Li(html.Props{Class: "persona-profile-skill", Data: map[string]string{"tier": strings.ToUpper(strings.TrimSpace(skill.Tier))}},
			html.Strong(html.Props{Text: skill.Name}), html.Span(html.Props{Class: "persona-profile-tier", Text: personaSkillTierLabel(locale, skill.Tier)})))
	}
	return html.Ul(html.Props{Class: "persona-profile-skill-list"}, rows...)
}

func personaValues(values []string, class string) ui.Node {
	rows := make([]ui.Node, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			rows = append(rows, html.Li(html.Props{Class: class, Text: value}))
		}
	}
	if len(rows) == 0 {
		return html.Ul(html.Props{Class: class + "-list"})
	}
	return html.Ul(html.Props{Class: class + "-list"}, rows...)
}

func personaReplyPlacementLabel(locale LocaleContext, placement PersonaReplyPlacement, context PersonaMentionContext) string {
	if context.AlwaysPrivate {
		placement = PersonaReplyPrivateAlways
	} else if context.AudienceFloorWillDivert {
		placement = PersonaReplyPrivateAudience
	}
	switch placement {
	case PersonaReplyPrivateAlways:
		return personaMentionText(locale, "private_always")
	case PersonaReplyPrivateAudience:
		return personaMentionText(locale, "private_audience")
	default:
		return personaMentionText(locale, "in_thread")
	}
}

func personaSkillTierLabel(locale LocaleContext, tier string) string {
	key := "tier_" + strings.ToLower(strings.TrimSpace(tier))
	if value := personaMentionText(locale, key); value != key {
		return value
	}
	return strings.TrimSpace(tier)
}

func personaMentionText(locale LocaleContext, key string) string {
	language := strings.ToLower(strings.TrimSpace(locale.Resolved))
	if index := strings.IndexByte(language, '-'); index > 0 {
		language = language[:index]
	}
	if copy, ok := personaMentionCopy[language]; ok {
		if value := strings.TrimSpace(copy[key]); value != "" {
			return value
		}
	}
	return personaMentionCopy["en"][key]
}

var personaMentionCopy = map[string]map[string]string{
	"en": {
		"menu": "Mention someone", "people": "People in this conversation", "agents": "Agents", "none": "No people or agents match this search.", "no_agents": "No agents are in this conversation yet. You can still ask on the Agents page.", "no_agent_matches": "No agents match this search.", "agents_page": "Agents page", "add_agent": "Add an agent to this conversation", "loading": "Loading agents…", "load_failed": "The agent list could not be loaded.", "retry": "Retry loading agents", "agent": "Agent", "identity_unavailable": "Agent identity unavailable", "acting_unavailable": "Acting for unavailable", "acting_for": "acting for", "view_profile": "View agent details", "profile": "Profile", "close_profile": "Close agent details", "purpose": "Purpose", "owner": "Owner", "version": "Version", "skills": "Skills", "data_reach": "Data this agent can reach", "acts_with_access": "Acts with your current access.", "cannot_do": "What it cannot do", "replies": "Replies go", "in_thread": "in this thread", "private_always": "privately because this conversation is always private", "private_audience": "privately because the audience floor will divert this answer", "unavailable": "Not provided", "none_listed": "No skills listed.",
		"tier_t0": "Read only", "tier_t1": "Private draft", "tier_t2": "Communicate", "tier_t3": "Governed submission", "tier_t4": "External write",
	},
	"de": {
		"menu": "Person erwähnen", "people": "Personen in dieser Unterhaltung", "agents": "Agenten", "none": "Keine passende Person oder kein passender Agent gefunden.", "no_agents": "In dieser Unterhaltung gibt es noch keine Agenten. Sie können trotzdem auf der Seite „Agenten“ fragen.", "no_agent_matches": "Keine Agenten passen zu dieser Suche.", "agents_page": "Seite „Agenten“", "add_agent": "Agent zu dieser Unterhaltung hinzufügen", "loading": "Agenten werden geladen…", "load_failed": "Die Agentenliste konnte nicht geladen werden.", "retry": "Agenten erneut laden", "agent": "Agent", "identity_unavailable": "Agentenidentität nicht verfügbar", "acting_unavailable": "Handelt für eine nicht verfügbare Person", "acting_for": "handelt für", "view_profile": "Agentendetails anzeigen", "profile": "Profil", "close_profile": "Agentendetails schließen", "purpose": "Zweck", "owner": "Besitzer", "version": "Version", "skills": "Fähigkeiten", "data_reach": "Daten, auf die dieser Agent zugreifen kann", "acts_with_access": "Handelt mit Ihrem aktuellen Zugriff.", "cannot_do": "Was er nicht kann", "replies": "Antworten gehen", "in_thread": "in diesen Thread", "private_always": "privat, weil diese Unterhaltung immer privat ist", "private_audience": "privat, weil die Zielgruppe diese Antwort nicht vollständig sehen darf", "unavailable": "Nicht angegeben", "none_listed": "Keine Fähigkeiten aufgeführt.",
		"tier_t0": "Nur lesen", "tier_t1": "Privater Entwurf", "tier_t2": "Kommunizieren", "tier_t3": "Gesteuerte Einreichung", "tier_t4": "Externer Schreibzugriff",
	},
	"ar": {
		"menu": "الإشارة إلى شخص", "people": "الأشخاص في هذه المحادثة", "agents": "الوكلاء", "none": "لا يوجد شخص أو وكيل يطابق هذا البحث.", "no_agents": "لا يوجد وكلاء في هذه المحادثة حتى الآن. لا يزال بإمكانك طرح سؤالك في صفحة الوكلاء.", "no_agent_matches": "لا يوجد وكلاء يطابقون هذا البحث.", "agents_page": "صفحة الوكلاء", "add_agent": "إضافة وكيل إلى هذه المحادثة", "loading": "جارٍ تحميل الوكلاء…", "load_failed": "تعذر تحميل قائمة الوكلاء.", "retry": "إعادة محاولة تحميل الوكلاء", "agent": "وكيل", "identity_unavailable": "هوية الوكيل غير متاحة", "acting_unavailable": "ينوب عن شخص غير متاح", "acting_for": "ينوب عن", "view_profile": "عرض تفاصيل الوكيل", "profile": "التفاصيل", "close_profile": "إغلاق تفاصيل الوكيل", "purpose": "الغرض", "owner": "المالك", "version": "الإصدار", "skills": "المهارات", "data_reach": "فئات البيانات التي يمكن لهذا الوكيل الوصول إليها", "acts_with_access": "يعمل الوكيل بصلاحياتك الحالية.", "cannot_do": "ما لا يستطيع فعله", "replies": "تذهب الردود", "in_thread": "إلى سلسلة المحادثة الحالية", "private_always": "بشكل خاص لأن هذه المحادثة خاصة دائماً", "private_audience": "بشكل خاص لأن نطاق الجمهور سيحوّل هذه الإجابة", "unavailable": "غير متوفر", "none_listed": "لا توجد مهارات مدرجة.",
		"tier_t0": "قراءة فقط", "tier_t1": "مسودة خاصة", "tier_t2": "تواصل", "tier_t3": "إرسال خاضع للحوكمة", "tier_t4": "كتابة خارجية",
	},
}
