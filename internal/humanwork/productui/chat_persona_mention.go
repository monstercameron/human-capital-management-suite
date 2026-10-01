package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
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
	Locale  LocaleContext
	Target  string
	Query   string
	Active  int
	Context PersonaMentionContext
	People  []PersonaMentionPerson
	Agents  []PersonaMentionPersona
	Profile *PersonaMentionPersona
}

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

func PersonaMentionInputAria(target string, active int, open bool) map[string]string {
	aria := map[string]string{"autocomplete": "list", "haspopup": "listbox", "keyshortcuts": "ArrowDown ArrowUp Enter Escape"}
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
	filteredAgents := agents[:0]
	for _, agent := range agents {
		if personaMentionMatches(props.Query, agent.Name) {
			filteredAgents = append(filteredAgents, agent)
		}
	}
	agents = filteredAgents

	optionCount := len(people) + len(agents)
	active := props.Active
	if active < 0 || active >= optionCount {
		active = 0
	}
	children := []ui.Node{html.P(html.Props{ID: target + "-persona-mention-heading", Class: "sr-only", Text: personaMentionText(locale, "menu")})}
	optionIndex := 0
	if len(people) > 0 {
		children = append(children, html.H3(html.Props{Class: "persona-mention-group", Text: personaMentionText(locale, "people")}))
		for _, person := range people {
			children = append(children, personaMentionPersonOption(target, person, optionIndex, active == optionIndex))
			optionIndex++
		}
	}
	if len(agents) > 0 {
		children = append(children, html.H3(html.Props{Class: "persona-mention-group", Text: personaMentionText(locale, "agents")}))
		for _, agent := range agents {
			children = append(children, personaMentionAgentOption(locale, target, agent, optionIndex, active == optionIndex))
			optionIndex++
		}
	}
	if optionCount == 0 {
		children = append(children, html.P(html.Props{Class: "persona-mention-empty", Role: "status", Aria: map[string]string{"live": "polite"}, Text: personaMentionText(locale, "none")}))
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
	return html.Div(html.Props{Class: "persona-mention-agent-row"},
		html.Button(html.Props{
			ID: optionID, Class: "persona-mention-option persona-mention-agent", Type: "button", Role: "option", TabIndex: -1,
			Data: map[string]string{"action": "mention-pick", "kind": "agent", "id": agent.ID, "profile-id": agent.ID},
			Aria: map[string]string{"selected": personaBoolString(selected)},
		}, ui.Text("@"+agent.Name), html.Small(html.Props{Class: "persona-mention-purpose", Text: agent.Purpose}), PersonaAgentBadge(agent.Actor)),
		html.Button(html.Props{
			Class: "persona-mention-profile", Type: "button", Data: map[string]string{"action": "persona-profile", "id": agent.ID},
			Aria: map[string]string{"label": personaMentionText(locale, "view_profile") + ": " + agent.Name, "controls": target + "-persona-profile-" + safeAgentDOMToken(agent.ID)},
		}, ui.Text(personaMentionText(locale, "profile"))),
	)
}

// PersonaAgentBadge renders the permanent agent marker and explicit invoker
// attribution for a mention chip. It fails closed when actor fields are absent.
func PersonaAgentBadge(actor *PersonaChatActor) ui.Node {
	if actor == nil || !actor.Trusted || strings.TrimSpace(actor.PersonaID) == "" || strings.TrimSpace(actor.AgentID) == "" {
		return html.Span(html.Props{Class: "agent-badge unavailable", Aria: map[string]string{"label": "Agent identity unavailable"}, Text: "Agent identity unavailable"})
	}
	attribution := "Acting for unavailable"
	if strings.TrimSpace(actor.InvokerHandle) != "" {
		attribution = "acting for @" + strings.TrimSpace(actor.InvokerHandle)
	}
	return html.Span(html.Props{Class: "agent-badge", Aria: map[string]string{"label": "Agent; " + attribution}}, html.Strong(html.Props{Text: "Agent"}), html.Span(html.Props{Class: "agent-attribution", Text: attribution}))
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
		"menu": "Mention someone", "people": "People", "agents": "Agents", "none": "No people or agents match this search.", "view_profile": "View persona profile", "profile": "Profile", "close_profile": "Close persona profile", "purpose": "Purpose", "owner": "Owner", "version": "Version", "skills": "Skills", "data_reach": "Data this persona can reach", "acts_with_access": "Acts with your current access.", "cannot_do": "What it cannot do", "replies": "Replies go", "in_thread": "in this thread", "private_always": "privately because this conversation is always private", "private_audience": "privately because the audience floor will divert this answer", "unavailable": "Not provided", "none_listed": "No skills listed.",
		"tier_t0": "Read only", "tier_t1": "Private draft", "tier_t2": "Communicate", "tier_t3": "Governed submission", "tier_t4": "External write",
	},
	"de": {
		"menu": "Person erwähnen", "people": "Personen", "agents": "Agenten", "none": "Keine passende Person oder kein passender Agent gefunden.", "view_profile": "Persona-Profil anzeigen", "profile": "Profil", "close_profile": "Persona-Profil schließen", "purpose": "Zweck", "owner": "Besitzer", "version": "Version", "skills": "Fähigkeiten", "data_reach": "Daten, auf die diese Persona zugreifen kann", "acts_with_access": "Handelt mit Ihrem aktuellen Zugriff.", "cannot_do": "Was sie nicht kann", "replies": "Antworten gehen", "in_thread": "in diesen Thread", "private_always": "privat, weil diese Unterhaltung immer privat ist", "private_audience": "privat, weil die Zielgruppe diese Antwort nicht vollständig sehen darf", "unavailable": "Nicht angegeben", "none_listed": "Keine Fähigkeiten aufgeführt.",
		"tier_t0": "Nur lesen", "tier_t1": "Privater Entwurf", "tier_t2": "Kommunizieren", "tier_t3": "Gesteuerte Einreichung", "tier_t4": "Externer Schreibzugriff",
	},
	"ar": {
		"menu": "الإشارة إلى شخص", "people": "الأشخاص", "agents": "الوكلاء", "none": "لا يوجد شخص أو وكيل يطابق هذا البحث.", "view_profile": "عرض ملف الشخصية", "profile": "الملف الشخصي", "close_profile": "إغلاق ملف الشخصية", "purpose": "الغرض", "owner": "المالك", "version": "الإصدار", "skills": "المهارات", "data_reach": "فئات البيانات التي يمكن لهذه الشخصية الوصول إليها", "acts_with_access": "يعمل الوكيل بصلاحياتك الحالية.", "cannot_do": "ما لا يستطيع فعله", "replies": "تذهب الردود", "in_thread": "إلى سلسلة المحادثة الحالية", "private_always": "بشكل خاص لأن هذه المحادثة خاصة دائماً", "private_audience": "بشكل خاص لأن نطاق الجمهور سيحوّل هذه الإجابة", "unavailable": "غير متوفر", "none_listed": "لا توجد مهارات مدرجة.",
		"tier_t0": "قراءة فقط", "tier_t1": "مسودة خاصة", "tier_t2": "تواصل", "tier_t3": "إرسال خاضع للحوكمة", "tier_t4": "كتابة خارجية",
	},
}
