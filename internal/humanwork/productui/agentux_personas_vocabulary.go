package productui

import (
	"regexp"
	"strings"
	"unicode"
)

var personaReferencePrefix = regexp.MustCompile(`^[a-z]{2,8}-\d+-`)

// PersonaAdminVocabulary converts a persona code into readable fallback copy.
// More specific helpers below should be preferred when the value's kind is known.
func PersonaAdminVocabulary(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if label := PersonaLifecycleLabel(ResolveProductLocale(DefaultProductLocale), v); label != v {
		return label
	}
	if label := PersonaTierLabel(ResolveProductLocale(DefaultProductLocale), v); label != v {
		return label
	}
	return humanizePersonaIdentifier(v)
}

// PersonaLifecycleLabel gives lifecycle codes their business-facing name.
func PersonaLifecycleLabel(locale LocaleContext, value string) string {
	return personaVocabularyLabel(locale, strings.ToUpper(strings.TrimSpace(value)), map[string][3]string{
		"DRAFT": {"Draft", "Entwurf", "مسودة"}, "IN_REVIEW": {"In review", "In Prüfung", "قيد المراجعة"},
		"PUBLISHED": {"Published", "Veröffentlicht", "منشور"}, "SUSPENDED": {"Suspended", "Ausgesetzt", "موقوف"},
		"RETIRED": {"Retired", "Stillgelegt", "متقاعدة"},
	})
}

// PersonaTierLabel explains the effect of a skill tier instead of exposing T-codes.
func PersonaTierLabel(locale LocaleContext, value string) string {
	return personaVocabularyLabel(locale, strings.ToUpper(strings.TrimSpace(value)), map[string][3]string{
		"T0": {"Read-only", "Schreibgeschützt", "للقراءة فقط"}, "T1": {"Private drafts", "Private Entwürfe", "مسودات خاصة"},
		"T2": {"Posts messages", "Nachrichten senden", "نشر الرسائل"}, "T3": {"Submits requests", "Anfragen einreichen", "إرسال الطلبات"},
		"T4": {"Writes to connected systems", "In verbundene Systeme schreiben", "الكتابة إلى الأنظمة المتصلة"},
	})
}

// PersonaDataClassLabel turns registry data classes into sentence-case English.
func PersonaDataClassLabel(value string) string {
	return PersonaDataClassLabelForLocale(ResolveProductLocale(DefaultProductLocale), value)
}

// PersonaDataClassLabelForLocale localizes the registry values known to the UI.
func PersonaDataClassLabelForLocale(locale LocaleContext, value string) string {
	return personaVocabularyLabel(locale, strings.ToUpper(strings.TrimSpace(value)), map[string][3]string{
		"POLICY":          {"Policy documents", "Richtliniendokumente", "مستندات السياسات"},
		"POLICY_DOCUMENT": {"Policy documents", "Richtliniendokumente", "مستندات السياسات"},
		"COMPENSATION":    {"Compensation information", "Vergütungsinformationen", "معلومات التعويضات"},
	})
}

// PersonaRoleLabel prefers the role name projected by the server and otherwise
// turns the role identifier into a readable fallback.
func PersonaRoleLabel(identifier, displayName string) string {
	return PersonaRoleLabelForLocale(ResolveProductLocale(DefaultProductLocale), identifier, displayName)
}

// PersonaRoleLabelForLocale localizes well-known directory roles. A projected
// display name remains authoritative for tenant-defined roles.
func PersonaRoleLabelForLocale(locale LocaleContext, identifier, displayName string) string {
	key := strings.ToLower(strings.TrimSpace(identifier))
	labels := map[string][3]string{
		"comp_admin":         {"Compensation administrator", "Vergütungsadministration", "مسؤول التعويضات"},
		"employees":          {"Employees", "Beschäftigte", "الموظفون"},
		"hcm_admin":          {"HCM administrator", "HCM-Administration", "مسؤول إدارة رأس المال البشري"},
		"intent_author":      {"Workflow author", "Workflow-Autor:innen", "مؤلف سير العمل"},
		"promotion_operator": {"Promotion operator", "Beförderungsverwaltung", "مسؤول الترقيات"},
		"executive":          {"Executive", "Führungskraft", "تنفيذي"},
	}
	if _, ok := labels[key]; ok {
		return personaVocabularyLabel(locale, key, labels)
	}
	if label := strings.TrimSpace(displayName); label != "" {
		return label
	}
	return humanizePersonaIdentifier(identifier)
}

// PersonaOrganizationLabel prefers the unit name projected by the server.
func PersonaOrganizationLabel(identifier, displayName string) string {
	return PersonaOrganizationLabelForLocale(ResolveProductLocale(DefaultProductLocale), identifier, displayName)
}

// PersonaOrganizationLabelForLocale localizes well-known organization
// audiences and otherwise preserves the directory's projected display name.
func PersonaOrganizationLabelForLocale(locale LocaleContext, identifier, displayName string) string {
	if strings.EqualFold(lastPersonaIdentifierSegment(identifier), "executive") {
		return personaVocabularyLabel(locale, "executive", map[string][3]string{"executive": {"Executive leadership", "Geschäftsleitung", "القيادة التنفيذية"}})
	}
	return personaNamedValue(strings.TrimPrefix(identifier, "org:"), displayName)
}

// PersonaWorkerLabel prefers the directory name projected by the server and
// otherwise removes the tenant reference prefix before humanizing the worker.
func PersonaWorkerLabel(reference, displayName string) string {
	if label := strings.TrimSpace(displayName); label != "" {
		return label
	}
	return humanizePersonaName(personaReferencePrefix.ReplaceAllString(strings.TrimSpace(reference), ""))
}

// PersonaSkillLabel prefers registry copy and otherwise humanizes the final
// path segment of a skill identifier.
func PersonaSkillLabel(identifier, displayName, description string) string {
	return PersonaSkillLabelForLocale(ResolveProductLocale(DefaultProductLocale), identifier, displayName, description)
}

// PersonaSkillLabelForLocale localizes known skill names while keeping a
// readable server-projected fallback for tenant-defined skills.
func PersonaSkillLabelForLocale(locale LocaleContext, identifier, displayName, description string) string {
	if label := knownPersonaSkillCopy(locale, identifier, false); label != "" {
		return label
	}
	if label := strings.TrimSpace(displayName); label != "" && !strings.EqualFold(label, strings.TrimSpace(identifier)) {
		return label
	}
	if label := strings.TrimSpace(identifier); label != "" {
		return humanizePersonaIdentifier(lastPersonaIdentifierSegment(label))
	}
	if label := strings.TrimSpace(description); label != "" {
		return label
	}
	return ""
}

// PersonaSkillDescription returns plain business copy for known skills and
// otherwise preserves the server description.
func PersonaSkillDescription(locale LocaleContext, identifier, fallback string) string {
	if description := knownPersonaSkillCopy(locale, identifier, true); description != "" {
		return description
	}
	return strings.TrimSpace(fallback)
}

func knownPersonaSkillCopy(locale LocaleContext, identifier string, description bool) string {
	key := strings.ToLower(lastPersonaIdentifierSegment(identifier))
	labels := map[string][3]string{
		"knowledge_search_with_citations": {"Knowledge search with citations", "Wissenssuche mit Quellenangaben", "البحث المعرفي مع الاستشهادات"},
		"chat_reply":                      {"Chat reply", "Chat-Antwort", "رد المحادثة"},
	}
	descriptions := map[string][3]string{
		"knowledge_search_with_citations": {"Finds answers in policy documents the person asking may read, and cites them.", "Findet Antworten in Richtliniendokumenten, die die fragende Person lesen darf, und nennt die Quellen.", "يعثر على إجابات في مستندات السياسات التي يمكن للشخص السائل قراءتها، ويستشهد بها."},
		"chat_reply":                      {"Replies to the person who asked. Answers drawn from documents are sent to them privately.", "Antwortet der fragenden Person. Antworten aus Dokumenten werden ihr privat gesendet.", "يرد على الشخص الذي سأل. تُرسل إليه الإجابات المستمدة من المستندات بشكل خاص."},
	}
	if description {
		if _, ok := descriptions[key]; !ok {
			return ""
		}
		return personaVocabularyLabel(locale, key, descriptions)
	}
	if _, ok := labels[key]; !ok {
		return ""
	}
	return personaVocabularyLabel(locale, key, labels)
}

func personaNamedValue(identifier, displayName string) string {
	if label := strings.TrimSpace(displayName); label != "" {
		return label
	}
	return humanizePersonaIdentifier(identifier)
}

func personaVocabularyLabel(locale LocaleContext, value string, labels map[string][3]string) string {
	localized, ok := labels[value]
	if !ok {
		return humanizePersonaIdentifier(value)
	}
	index := 0
	switch locale.Resolved {
	case "de-DE":
		index = 1
	case "ar":
		index = 2
	}
	return localized[index]
}

func lastPersonaIdentifierSegment(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.LastIndexAny(value, ".:/"); index >= 0 {
		value = value[index+1:]
	}
	return value
}

func humanizePersonaIdentifier(value string) string {
	v := lastPersonaIdentifierSegment(value)
	v = strings.NewReplacer("_", " ", "-", " ").Replace(v)
	v = strings.Join(strings.Fields(strings.ToLower(v)), " ")
	runes := []rune(v)
	if len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}

func humanizePersonaName(value string) string {
	parts := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(value))
	for i, part := range parts {
		runes := []rune(strings.ToLower(part))
		if len(runes) > 0 {
			runes[0] = unicode.ToUpper(runes[0])
			parts[i] = string(runes)
		}
	}
	return strings.Join(parts, " ")
}
