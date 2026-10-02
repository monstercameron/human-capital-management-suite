package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const PersonaProfileStyles = `.mention-agent-row{display:flex;flex-direction:column;min-width:0}.mention-agent-option{display:flex;align-items:stretch;min-width:0}.mention-agent-option>.mention-option{flex:1}.mention-agent-info{display:inline-flex;align-items:center;justify-content:center;flex:0 0 44px;min-width:44px;min-height:44px;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--muted);cursor:pointer}.mention-agent-info:hover,.mention-agent-info:focus-visible{background:var(--soft);color:var(--accent)}.mention-agent-info .chat-icon{width:18px;height:18px}.mention-agent-preview{margin:2px 6px 6px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--soft)}.mention-option.persona{align-items:center;width:100%;min-width:0}.mention-agent-identity{display:flex;flex:1;min-width:0;flex-direction:column;align-items:flex-start}.mention-option.persona .mention-name,.mention-option.persona .mention-handle{display:block;max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.mention-handle{color:var(--muted);font-size:.75rem}.mention-option.persona .mention-purpose{flex-basis:100%;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;line-clamp:2;overflow:hidden;overflow-wrap:anywhere;text-align:start;font-size:.8125rem}.mention-agent-state{margin:4px 8px;color:var(--muted);font-size:.8125rem;overflow-wrap:anywhere}.mention-agent-state.error{display:flex;align-items:center;justify-content:space-between;gap:8px}.mention-agent-state.error p{margin:0}.mention-attribution{flex-basis:100%;font-size:.75rem;color:var(--muted)}.mention-profile-card{padding:8px 10px;border-top:1px solid var(--line);font-size:.8125rem;overflow-wrap:anywhere;max-width:100%}.mention-profile-card p{margin:4px 0}.mention-profile-section h4{margin:6px 0 2px}.mention-profile-list{margin:0;padding-inline-start:18px}.persona-task-card .agents-task-status{margin-inline-start:8px}.persona-task-card .agents-task-link{display:inline-block;padding-block:6px}@media(max-width:390px){.mention-menu{max-height:45vh;width:100%;max-width:calc(100vw - 16px)}.mention-option.persona{flex-wrap:wrap}.mention-option.persona .agent-badge{white-space:nowrap}.mention-agent-state.error{align-items:flex-start;flex-direction:column}}`

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
		personaProfileList(locale, "data_reach", chatPolishDataClasses(locale, persona.DataClasses)),
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
			"menu": "Mention someone", "none": "No people or agents match this search.", "agents": "Agents", "agent_badge": "Agent", "profile": "Agent details", "purpose": "Purpose", "owner": "Owner", "version": "Version", "skills": "Skills", "data_reach": "Data this agent can reach", "acts_with_access": "Acts with your current access.", "cannot_do": "What it cannot do", "replies": "Replies go", "in_thread": "in this thread", "private_always": "privately because this conversation is always private", "private_audience": "in this thread when the conversation audience can access the answer; otherwise privately", "unavailable": "Not provided", "tier_t0": "Read only", "tier_t1": "Private draft", "tier_t2": "Communicate", "tier_t3": "Governed submission", "tier_t4": "External write", "tier_unknown": "Tier not provided",
		},
		"de": {
			"menu": "Person erwähnen", "none": "Keine passende Person oder kein passender Agent gefunden.", "agents": "Agenten", "agent_badge": "Agent", "profile": "Agentendetails", "purpose": "Zweck", "owner": "Verantwortlich", "version": "Version", "skills": "Fähigkeiten", "data_reach": "Datenzugriff dieses Agenten", "acts_with_access": "Handelt mit Ihrem aktuellen Zugriff.", "cannot_do": "Was er nicht kann", "replies": "Antworten gehen", "in_thread": "in diesen Thread", "private_always": "privat, weil diese Unterhaltung immer privat ist", "private_audience": "in diesem Thread, wenn die Zielgruppe die Antwort sehen darf; andernfalls privat", "unavailable": "Nicht angegeben", "tier_t0": "Nur lesen", "tier_t1": "Privater Entwurf", "tier_t2": "Kommunizieren", "tier_t3": "Gesteuerte Einreichung", "tier_t4": "Externer Schreibzugriff", "tier_unknown": "Stufe nicht angegeben",
		},
		"ar": {
			"menu": "الإشارة إلى شخص", "none": "لا يوجد شخص أو وكيل يطابق هذا البحث.", "agents": "الوكلاء", "agent_badge": "وكيل", "profile": "تفاصيل الوكيل", "purpose": "الغرض", "owner": "المالك", "version": "الإصدار", "skills": "المهارات", "data_reach": "فئات البيانات التي يمكن لهذا الوكيل الوصول إليها", "acts_with_access": "يعمل الوكيل بصلاحياتك الحالية.", "cannot_do": "ما لا يستطيع فعله", "replies": "تذهب الردود", "in_thread": "إلى سلسلة المحادثة الحالية", "private_always": "بشكل خاص لأن هذه المحادثة خاصة دائماً", "private_audience": "في سلسلة المحادثة عندما يستطيع الجمهور الوصول إلى الإجابة؛ وإلا بشكل خاص", "unavailable": "غير متوفر", "tier_t0": "قراءة فقط", "tier_t1": "مسودة خاصة", "tier_t2": "تواصل", "tier_t3": "إرسال خاضع للحوكمة", "tier_t4": "كتابة خارجية", "tier_unknown": "المستوى غير متوفر",
		},
	}
	return chatbug039Text(key, personaMentionCopy[language][key], personaMentionCopy["en"][key])
}

func chatPolishDataClasses(locale string, classes []string) []string {
	labels := []string{}
	for _, class := range classes {
		if class == "POLICY_DOCUMENT" {
			labels = append(labels, chatPolishPolicyScope(locale))
		} else if strings.Contains(class, " ") && !strings.Contains(class, "_") && !strings.Contains(class, "hcmnext.") {
			labels = append(labels, class)
		}
	}
	return labels
}
