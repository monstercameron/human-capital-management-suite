package chatui

import "strings"

// CHATUX-041: the words of Conversation details, round three. Skills read as
// sentences, the pin buttons say what they pin, the joining row says where the
// gate stands, and the filter grid names its two columns.

const (
	chatux041KeyRolesSet       = "chat.ux041.roles_set"
	chatux041KeyRolesNone      = "chat.ux041.roles_none"
	chatux041KeyPinTeamOn      = "chat.ux041.pin_team_on"
	chatux041KeyPinTeamOff     = "chat.ux041.pin_team_off"
	chatux041KeyPinProjectOn   = "chat.ux041.pin_project_on"
	chatux041KeyPinProjectOff  = "chat.ux041.pin_project_off"
	chatux041KeySkillPolicy    = "chat.ux041.skill_policy"
	chatux041KeySkillReply     = "chat.ux041.skill_reply"
	chatux041KeySkillDocuments = "chat.ux041.skill_documents"
	chatux041KeyReadsChannel   = "chat.ux041.reads_channel"
	chatux041KeyReadsWorkspace = "chat.ux041.reads_workspace"
	chatux041KeyReadsNone      = "chat.ux041.reads_none"
	chatux041KeyFactOwner      = "chat.ux041.fact_owner"
	chatux041KeyFactVersion    = "chat.ux041.fact_version"
	chatux041KeyFactReads      = "chat.ux041.fact_reads"
	chatux041KeyFactAccess     = "chat.ux041.fact_access"
	chatux041KeyFactSkills     = "chat.ux041.fact_skills"
	chatux041KeyFactNever      = "chat.ux041.fact_never"
	chatux041KeyColWorkspace   = "chat.ux041.col_workspace"
	chatux041KeyColChannel     = "chat.ux041.col_channel"
	chatux041KeyWorkspaceScope = "chat.ux041.workspace_scope"
	chatux041KeyGateAnyone     = "chat.ux041.gate_anyone"
	chatux041KeyGateInvite     = "chat.ux041.gate_invite"
	chatux041KeyGateOne        = "chat.ux041.gate_one"
	chatux041KeyGateMany       = "chat.ux041.gate_many"
	chatux041KeyAck            = "chat.ux041.ack"
)

var chatux041Copy = map[string]map[string]string{
	"en-US": {
		chatux041KeyRolesSet: "{n} set", chatux041KeyRolesNone: "None set",
		chatux041KeyPinTeamOn: "Show the purpose and team above the messages", chatux041KeyPinTeamOff: "Stop showing the purpose and team above the messages",
		chatux041KeyPinProjectOn: "Show the project above the messages", chatux041KeyPinProjectOff: "Stop showing the project above the messages",
		chatux041KeySkillPolicy: "Searches policy documents you can open", chatux041KeySkillReply: "Writes the agent's reply in a private chat",
		chatux041KeySkillDocuments: "Searches documents every workspace member can read",
		chatux041KeyReadsChannel:   "Documents in this channel you can open", chatux041KeyReadsWorkspace: "Workspace documents you can open", chatux041KeyReadsNone: "No documents",
		chatux041KeyFactOwner: "Owner", chatux041KeyFactVersion: "Version", chatux041KeyFactReads: "Reads", chatux041KeyFactAccess: "Access", chatux041KeyFactSkills: "Can do", chatux041KeyFactNever: "Never",
		chatux041KeyColWorkspace: "Workspace", chatux041KeyColChannel: "This channel",
		chatux041KeyWorkspaceScope: "Changes here apply to every channel.",
		chatux041KeyGateAnyone:     "Anyone can join", chatux041KeyGateInvite: "By invitation", chatux041KeyGateOne: "1 question", chatux041KeyGateMany: "{n} questions",
		chatux041KeyAck: "I understand the channel becomes read-only and leaves the channel list.",
	},
	"de-DE": {
		chatux041KeyRolesSet: "{n} gesetzt", chatux041KeyRolesNone: "Keine gesetzt",
		chatux041KeyPinTeamOn: "Zweck und Team über den Nachrichten anzeigen", chatux041KeyPinTeamOff: "Zweck und Team nicht mehr über den Nachrichten anzeigen",
		chatux041KeyPinProjectOn: "Projekt über den Nachrichten anzeigen", chatux041KeyPinProjectOff: "Projekt nicht mehr über den Nachrichten anzeigen",
		chatux041KeySkillPolicy: "Durchsucht Richtliniendokumente, die Sie öffnen können", chatux041KeySkillReply: "Verfasst die Antwort des Agenten in einem privaten Chat",
		chatux041KeySkillDocuments: "Durchsucht Dokumente, die alle Mitglieder des Arbeitsbereichs lesen dürfen",
		chatux041KeyReadsChannel:   "Dokumente in diesem Kanal, die Sie öffnen können", chatux041KeyReadsWorkspace: "Dokumente des Arbeitsbereichs, die Sie öffnen können", chatux041KeyReadsNone: "Keine Dokumente",
		chatux041KeyFactOwner: "Verantwortlich", chatux041KeyFactVersion: "Version", chatux041KeyFactReads: "Liest", chatux041KeyFactAccess: "Zugriff", chatux041KeyFactSkills: "Kann", chatux041KeyFactNever: "Nie",
		chatux041KeyColWorkspace: "Arbeitsbereich", chatux041KeyColChannel: "Dieser Kanal",
		chatux041KeyWorkspaceScope: "Änderungen hier gelten für alle Kanäle.",
		chatux041KeyGateAnyone:     "Jeder kann beitreten", chatux041KeyGateInvite: "Nur auf Einladung", chatux041KeyGateOne: "1 Frage", chatux041KeyGateMany: "{n} Fragen",
		chatux041KeyAck: "Ich verstehe, dass der Kanal schreibgeschützt wird und aus der Kanalliste verschwindet.",
	},
	"ar": {
		chatux041KeyRolesSet: "{n} محددة", chatux041KeyRolesNone: "لا تسميات",
		chatux041KeyPinTeamOn: "عرض الغرض والفريق فوق الرسائل", chatux041KeyPinTeamOff: "إيقاف عرض الغرض والفريق فوق الرسائل",
		chatux041KeyPinProjectOn: "عرض المشروع فوق الرسائل", chatux041KeyPinProjectOff: "إيقاف عرض المشروع فوق الرسائل",
		chatux041KeySkillPolicy: "يبحث في مستندات السياسات التي يمكنك فتحها", chatux041KeySkillReply: "يكتب رد الوكيل في محادثة خاصة",
		chatux041KeySkillDocuments: "يبحث في المستندات التي يمكن لكل أعضاء مساحة العمل قراءتها",
		chatux041KeyReadsChannel:   "مستندات هذه القناة التي يمكنك فتحها", chatux041KeyReadsWorkspace: "مستندات مساحة العمل التي يمكنك فتحها", chatux041KeyReadsNone: "لا مستندات",
		chatux041KeyFactOwner: "المسؤول", chatux041KeyFactVersion: "الإصدار", chatux041KeyFactReads: "يقرأ", chatux041KeyFactAccess: "الوصول", chatux041KeyFactSkills: "يستطيع", chatux041KeyFactNever: "لا يفعل أبدًا",
		chatux041KeyColWorkspace: "مساحة العمل", chatux041KeyColChannel: "هذه القناة",
		chatux041KeyWorkspaceScope: "التغييرات هنا تسري على كل القنوات.",
		chatux041KeyGateAnyone:     "يمكن للجميع الانضمام", chatux041KeyGateInvite: "بدعوة فقط", chatux041KeyGateOne: "سؤال واحد", chatux041KeyGateMany: "{n} أسئلة",
		chatux041KeyAck: "أدرك أن القناة ستصبح للقراءة فقط وتختفي من قائمة القنوات.",
	},
}

// chatux041SkillSentence turns a skill's description from its definition into
// the plain sentence a person reads. The projection the client receives carries
// the description and the tier but not the skill's id, so the shipped skills are
// recognised by what their descriptions say; a skill this table does not know is
// shown as its first sentence, as before.
func chatux041SkillSentence(locale, description string) string {
	text := strings.ToLower(description)
	key := ""
	switch {
	case strings.Contains(text, "policy documents"):
		key = chatux041KeySkillPolicy
	case strings.Contains(text, "persona reply"):
		key = chatux041KeySkillReply
	case strings.Contains(text, "search workspace documents"):
		key = chatux041KeySkillDocuments
	}
	if key == "" {
		return ""
	}
	return laneText(Model{Locale: locale}, chatux041Copy, key)
}

// chatux041TierSuffix is the tier in words, in brackets after the sentence.
// English tiers are lower case inside the brackets ("read only").
func chatux041TierSuffix(locale, label string) string {
	if label == "" {
		return ""
	}
	if chatEmojiLocale(locale) == "en-US" {
		label = strings.ToLower(label)
	}
	return " (" + label + ")"
}

// chatux041ReadsPhrase says what an agent reads here, in one phrase, from the
// same facts as the collapsed line (chatAgentReadScope) so the two never
// disagree. It is "" when the server named no documents.
func chatux041ReadsPhrase(m Model, persona ResolvedPersonaMention) string {
	switch persona.DocumentScope {
	case "CHANNEL_DOCUMENTS":
		return laneText(m, chatux041Copy, chatux041KeyReadsChannel)
	case "WORKSPACE_DOCUMENTS":
		return laneText(m, chatux041Copy, chatux041KeyReadsWorkspace)
	case "NO_DOCUMENTS":
		return laneText(m, chatux041Copy, chatux041KeyReadsNone)
	}
	for _, class := range persona.DataClasses {
		if class == "POLICY_DOCUMENT" {
			return chatux017Text(m.Locale, "reads_policy")
		}
	}
	return laneText(m, chatux041Copy, chatux041KeyReadsChannel)
}

// chatux041PinLabel names what a pin button in Conversation details pins:
// "team" is the About card, "project" the project card, both shown above the
// messages while pinned.
func chatux041PinLabel(m Model, which string, pinned bool) string {
	key := map[string]string{"team": chatux041KeyPinTeamOn, "project": chatux041KeyPinProjectOn}[which]
	if pinned {
		key = map[string]string{"team": chatux041KeyPinTeamOff, "project": chatux041KeyPinProjectOff}[which]
	}
	return laneText(m, chatux041Copy, key)
}
