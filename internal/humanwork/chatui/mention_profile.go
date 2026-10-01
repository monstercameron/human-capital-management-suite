package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const PersonaProfileStyles = `.mention-agent-row{display:flex;flex-direction:column;min-width:0}.mention-option.persona{flex-wrap:wrap;align-items:center;width:100%}.mention-option.persona .mention-purpose{flex-basis:100%;font-size:.8125rem;white-space:normal;overflow-wrap:anywhere;text-align:start}.mention-attribution{flex-basis:100%;font-size:.75rem;color:var(--muted)}.mention-profile-trigger{padding:6px 8px;font-size:.8125rem;cursor:pointer;color:var(--accent)}.mention-profile-card{padding:8px 10px;border-top:1px solid var(--line);font-size:.8125rem;overflow-wrap:anywhere;max-width:100%}.mention-profile-card p{margin:4px 0}.mention-profile-section h4{margin:6px 0 2px}.mention-profile-list{margin:0;padding-inline-start:18px}.persona-profile-post{margin-block:4px}.persona-task-card .agents-task-status{margin-inline-start:8px}.persona-task-card .agents-task-link{display:inline-block;padding-block:6px}@media(max-width:390px){.mention-menu{max-height:45vh;width:100%}.agent-badge{white-space:normal;flex-wrap:wrap}}`

// PersonaMentionSkill is the safe, plain-language skill projection displayed
// before a member invokes a persona.
type PersonaMentionSkill struct {
	Name, Tier string
}

// PersonaReplyPlacement describes the server's effective reply destination.
type PersonaReplyPlacement string

const (
	PersonaReplyInThread        PersonaReplyPlacement = "thread"
	PersonaReplyPrivateAlways   PersonaReplyPlacement = "private_always"
	PersonaReplyPrivateAudience PersonaReplyPlacement = "private_audience"
)

func personaProfileCard(locale string, persona ResolvedPersonaMention) ui.Node {
	direction := "ltr"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "ar") {
		direction = "rtl"
	}
	rows := []ui.Node{
		html.P(html.Props{Class: "mention-profile-purpose"}, html.Strong(html.Props{Text: personaMentionText(locale, "purpose") + ": "}), ui.Text(personaProfileFact(persona.Purpose, locale))),
		html.P(html.Props{Class: "mention-profile-owner"}, html.Strong(html.Props{Text: personaMentionText(locale, "owner") + ": "}), ui.Text(personaProfileFact(persona.Owner, locale))),
		html.P(html.Props{Class: "mention-profile-version"}, html.Strong(html.Props{Text: personaMentionText(locale, "version") + ": "}), ui.Text(personaProfileFact(persona.Version, locale))),
		html.P(html.Props{Class: "mention-profile-access", Text: personaMentionText(locale, "acts_with_access")}),
		personaProfileList(locale, "skills", personaSkillRows(locale, persona.Skills)),
		personaProfileList(locale, "data_reach", persona.DataClasses),
		personaProfileList(locale, "cannot_do", persona.CannotDo),
		html.P(html.Props{Class: "mention-profile-replies"}, html.Strong(html.Props{Text: personaMentionText(locale, "replies") + ": "}), ui.Text(personaReplyLabel(locale, persona.ReplyPlacement))),
	}
	return html.Article(html.Props{Class: "mention-profile-card", Role: "region", Dir: direction, Aria: map[string]string{"label": personaMentionText(locale, "profile")}}, rows...)
}

func personaProfileList(locale, key string, values []string) ui.Node {
	rows := make([]ui.Node, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			rows = append(rows, html.Li(html.Props{Text: personaActivityLabel(locale, strings.TrimSpace(value))}))
		}
	}
	if len(rows) == 0 {
		rows = append(rows, html.Li(html.Props{Text: personaMentionText(locale, "unavailable")}))
	}
	return html.Section(html.Props{Class: "mention-profile-section", Aria: map[string]string{"label": personaMentionText(locale, key)}},
		html.H4(html.Props{Text: personaMentionText(locale, key)}), html.Ul(html.Props{Class: "mention-profile-list"}, rows...))
}

func personaSkillRows(locale string, skills []PersonaMentionSkill) []string {
	rows := make([]string, 0, len(skills))
	for _, skill := range skills {
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		tier := strings.TrimSpace(skill.Tier)
		if tier != "" {
			name += " — " + personaTierLabel(locale, tier)
		}
		rows = append(rows, name)
	}
	return rows
}

func personaProfileFact(value, locale string) string {
	if strings.TrimSpace(value) == "" {
		return personaMentionText(locale, "unavailable")
	}
	return strings.TrimSpace(value)
}

func personaReplyLabel(locale string, placement PersonaReplyPlacement) string {
	switch placement {
	case PersonaReplyInThread:
		return personaMentionText(locale, "in_thread")
	case PersonaReplyPrivateAlways:
		return personaMentionText(locale, "private_always")
	case PersonaReplyPrivateAudience:
		return personaMentionText(locale, "private_audience")
	default:
		return personaMentionText(locale, "unavailable")
	}
}

func personaTierLabel(locale, tier string) string {
	key := "tier_" + strings.ToLower(strings.TrimSpace(tier))
	if label := personaMentionText(locale, key); label != key {
		return label
	}
	return personaMentionText(locale, "tier_unknown")
}

func personaMentionText(locale, key string) string {
	language := strings.ToLower(strings.TrimSpace(locale))
	if index := strings.IndexByte(language, '-'); index > 0 {
		language = language[:index]
	}
	personaMentionCopy := map[string]map[string]string{
		"en": {
			"menu": "Mention someone", "none": "No people or agents match this search.", "agents": "Agents", "agent_badge": "Agent", "profile": "Persona profile", "purpose": "Purpose", "owner": "Owner", "version": "Version", "skills": "Skills", "data_reach": "Data this persona can reach", "acts_with_access": "Acts with your current access.", "cannot_do": "What it cannot do", "replies": "Replies go", "in_thread": "in this thread", "private_always": "privately because this conversation is always private", "private_audience": "in this thread when the conversation audience can access the answer; otherwise privately", "unavailable": "Not provided", "tier_t0": "Read only", "tier_t1": "Private draft", "tier_t2": "Communicate", "tier_t3": "Governed submission", "tier_t4": "External write", "tier_unknown": "Tier not provided",
		},
		"de": {
			"menu": "Person erwähnen", "none": "Keine passende Person oder kein passender Agent gefunden.", "agents": "Agenten", "agent_badge": "Agent", "profile": "Persona-Profil", "purpose": "Zweck", "owner": "Verantwortlich", "version": "Version", "skills": "Fähigkeiten", "data_reach": "Datenzugriff dieser Persona", "acts_with_access": "Handelt mit Ihrem aktuellen Zugriff.", "cannot_do": "Was sie nicht kann", "replies": "Antworten gehen", "in_thread": "in diesen Thread", "private_always": "privat, weil diese Unterhaltung immer privat ist", "private_audience": "in diesem Thread, wenn die Zielgruppe die Antwort sehen darf; andernfalls privat", "unavailable": "Nicht angegeben", "tier_t0": "Nur lesen", "tier_t1": "Privater Entwurf", "tier_t2": "Kommunizieren", "tier_t3": "Gesteuerte Einreichung", "tier_t4": "Externer Schreibzugriff", "tier_unknown": "Stufe nicht angegeben",
		},
		"ar": {
			"menu": "الإشارة إلى شخص", "none": "لا يوجد شخص أو وكيل يطابق هذا البحث.", "agents": "الوكلاء", "agent_badge": "وكيل", "profile": "ملف الشخصية", "purpose": "الغرض", "owner": "المالك", "version": "الإصدار", "skills": "المهارات", "data_reach": "فئات البيانات التي يمكن لهذه الشخصية الوصول إليها", "acts_with_access": "يعمل الوكيل بصلاحياتك الحالية.", "cannot_do": "ما لا يستطيع فعله", "replies": "تذهب الردود", "in_thread": "إلى سلسلة المحادثة الحالية", "private_always": "بشكل خاص لأن هذه المحادثة خاصة دائماً", "private_audience": "في سلسلة المحادثة عندما يستطيع الجمهور الوصول إلى الإجابة؛ وإلا بشكل خاص", "unavailable": "غير متوفر", "tier_t0": "قراءة فقط", "tier_t1": "مسودة خاصة", "tier_t2": "تواصل", "tier_t3": "إرسال خاضع للحوكمة", "tier_t4": "كتابة خارجية", "tier_unknown": "المستوى غير متوفر",
		},
	}
	if copy, ok := personaMentionCopy[language]; ok {
		if value := strings.TrimSpace(copy[key]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(personaMentionCopy["en"][key]); value != "" {
		return value
	}
	return key
}
