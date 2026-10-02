package productui

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func personaAdminLifecycleBlock(locale LocaleContext, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) ui.Node {
	state := personaAdminLifecycleState(locale, persona)
	nextStep := map[personaAdminLifecyclePhase]string{
		personaAdminPhaseDraft:             "next_draft",
		personaAdminPhaseWaitingReview:     "next_in_review",
		personaAdminPhaseReadyEvaluation:   "next_reviewed",
		personaAdminPhaseEvaluationRunning: "next_evaluation_running",
		personaAdminPhaseEvaluationFailed:  "next_evaluation_failed",
		personaAdminPhaseReadyPublication:  "next_evaluated",
		personaAdminPhasePublished:         "next_published",
	}[state.Phase]
	children := []ui.Node{html.H4(html.Props{}, ui.Text(state.Heading))}
	if persona.Lifecycle == PersonaSuspended {
		return html.Div(html.Props{Class: "persona-admin-lifecycle-block", Raw: map[string]any{"data-lifecycle-heading": string(state.Phase), "data-persona-next-step": "resume"}}, children...)
	}
	version := personaAdminLocalizedNumber(locale, persona.Version)
	switch state.Phase {
	case personaAdminPhaseDraft:
		children = append(children, html.P(html.Props{}, ui.Text(strings.ReplaceAll(personaAdminR5SetupText(locale, "review_someone_else"), "{version}", version))))
	case personaAdminPhaseWaitingReview:
		reviewer := PersonaWorkerLabel(persona.Reviewer, persona.ReviewerName)
		if strings.TrimSpace(reviewer) == "" {
			reviewer = personaAdminR5Text(locale, "independent_reviewer")
		}
		waiting := strings.NewReplacer("{reviewer}", reviewer, "{version}", version).Replace(personaAdminR5SetupText(locale, "waiting_for_reviewer"))
		children = append(children, html.P(html.Props{}, ui.Text(waiting)))
		if !personaAdminCommandAllowed(snapshot, "REVIEW") {
			children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(strings.ReplaceAll(personaAdminR5SetupText(locale, "author_cannot_review"), "{version}", version))))
			if persona.Reviewer != "" {
				children = append(children, html.A(html.Props{Class: "button secondary compact", Href: Path(PageChat) + "?person=" + url.QueryEscape(persona.Reviewer)}, ui.Text(strings.ReplaceAll(personaAdminR5SetupText(locale, "message_reviewer"), "{reviewer}", reviewer))))
			}
		}
	case personaAdminPhaseReadyEvaluation, personaAdminPhaseEvaluationRunning, personaAdminPhaseEvaluationFailed:
		if persona.Reviewer != "" || persona.ReviewerName != "" {
			children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "approved_by")+" "), personaAdminPersonChip(persona.Reviewer, persona.ReviewerName, persona.ReviewerInitials, persona.ReviewerAvatarURL, personaAdminText(locale, "unknown_person"))))
		}
		if !personaAdminCommandAllowed(snapshot, "RUN_EVALUATION") {
			// The person named must be able to open this page and run the
			// evaluation. The technical contact often can do neither, so the
			// page names a colleague who administers agents, and says so in
			// general terms when the directory lists none.
			if runner := agentUX046EvaluationRunner(snapshot); runner.Label != "" {
				person := PersonaWorkerLabel(runner.ID, runner.Label)
				next := strings.ReplaceAll(personaAdminR5SetupText(locale, "next_evaluator"), "{person}", person)
				children = append(children, html.P(html.Props{}, ui.Text(next+" "), html.A(html.Props{Href: Path(PageChat) + "?person=" + url.QueryEscape(runner.ID)}, ui.Text(strings.ReplaceAll(personaAdminR5SetupText(locale, "message_evaluator"), "{person}", person)))))
			} else {
				children = append(children, html.P(html.Props{}, ui.Text(personaAdminR5SetupText(locale, "next_evaluator_role"))))
			}
		}
	case personaAdminPhaseReadyPublication:
		children = append(children, personaAdminEvaluationEvidence(locale, persona))
		children = append(children, html.P(html.Props{}, ui.Text(strings.ReplaceAll(personaAdminR5SetupText(locale, "publish_does_not_move"), "{version}", version)+personaAdminSpace), html.A(html.Props{Href: "/workspace/app/admin/agents?tab=rollout"}, ui.Text(personaAdminR5Text(locale, "rollout_link"))), ui.Text(personaAdminSentenceEnd)))
	case personaAdminPhasePublished:
		children = append(children, personaAdminReviewBlock(locale, persona, ""))
	}
	return html.Div(html.Props{Class: "persona-admin-lifecycle-block", Raw: map[string]any{"data-lifecycle-heading": string(state.Phase), "data-persona-next-step": nextStep}}, children...)
}

func personaAdminReviewApprovalAction(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	unreadable := 0
	for _, reference := range persona.DocumentReferences {
		if !reference.Readable {
			unreadable++
		}
	}
	button := html.Button(html.Props{Class: "button primary", Type: "button", Raw: map[string]any{"data-persona-command": "REVIEW", "data-persona-review-decision": "APPROVE"}}, ui.Text(personaAdminText(locale, "approve")))
	if unreadable == 0 {
		return html.Div(html.Props{Class: "persona-admin-review-decision", Raw: map[string]any{"data-review-decision-wrapper": "APPROVE"}}, button)
	}
	id := "persona-admin-approve-unread-" + safeAgentDOMToken(persona.ID)
	message := strings.NewReplacer(
		"{unreadable}", locale.FormatNumber(strconv.Itoa(unreadable), 0),
		"{total}", locale.FormatNumber(strconv.Itoa(len(persona.DocumentReferences)), 0),
	).Replace(personaAdminR5SetupText(locale, "unreadable_review"))
	owner := PersonaWorkerLabel(persona.Owner, persona.OwnerName)
	button = html.Button(html.Props{Class: "button primary", Type: "button", Disabled: true, Raw: map[string]any{"data-persona-command": "REVIEW", "data-persona-review-decision": "APPROVE", "data-review-unreadable-submit": id}}, ui.Text(personaAdminText(locale, "approve")))
	return html.Div(html.Props{Class: "persona-admin-review-decision persona-admin-unreadable-review", Raw: map[string]any{"data-review-decision-wrapper": "APPROVE"}},
		html.P(html.Props{Class: "persona-admin-document-warning", Role: "alert"}, ui.Text(message+" "), html.A(html.Props{Href: Path(PageChat) + "?person=" + url.QueryEscape(persona.Owner)}, ui.Text(strings.ReplaceAll(personaAdminR5SetupText(locale, "ask_owner_access"), "{owner}", owner)))),
		html.Label(html.Props{For: id}, html.Input(html.Props{ID: id, Type: "checkbox", Required: true, Raw: map[string]any{"data-review-unreadable-confirm": id}}), ui.Text(personaAdminR5SetupText(locale, "approve_unread"))),
		button,
	)
}

func personaAdminEvaluationEvidence(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	passed := persona.EvaluationPassed
	total := persona.EvaluationPassed + persona.EvaluationFailed
	if total == 0 && persona.EvaluationRef != "" {
		total = persona.EvaluationCaseCount
		passed = total
	}
	parts := make([]ui.Node, 0, 10)
	if date, ok := personaAdminFormattedDate(locale, persona.EvaluationPassedAt); ok {
		parts = append(parts, ui.Text(personaAdminR5SetupText(locale, "evaluation_passed_prefix")+" "), html.Time(html.Props{Raw: map[string]any{"datetime": persona.EvaluationPassedAt}}, ui.Text(date)))
	} else {
		parts = append(parts, ui.Text(personaAdminR5SetupText(locale, "evaluation_passed_prefix")))
	}
	if total > 0 {
		parts = append(parts, ui.Text(personaAdminInlineSeparator+locale.FormatNumber(strconv.Itoa(passed), 0)+" "+personaAdminR5SetupText(locale, "of")+" "+locale.FormatNumber(strconv.Itoa(total), 0)+" "+personaAdminR5SetupText(locale, "test_questions")))
	}
	runner := strings.TrimSpace(persona.EvaluationRunnerName)
	if runner != "" {
		parts = append(parts, ui.Text(personaAdminInlineSeparator+personaAdminR5SetupText(locale, "run_by")+" "+runner))
	}
	results := html.Details(html.Props{ID: "persona-admin-evaluation-result-" + safeAgentDOMToken(persona.ID), Class: "persona-admin-evaluation-results"},
		html.Summary(html.Props{}, ui.Text(personaAdminR5SetupText(locale, "view_results"))),
		html.P(html.Props{}, ui.Text(strings.NewReplacer("{passed}", locale.FormatNumber(strconv.Itoa(passed), 0), "{failed}", locale.FormatNumber(strconv.Itoa(persona.EvaluationFailed), 0)).Replace(personaAdminText(locale, "evaluation_result")))),
		personaAdminEvaluationSummary(locale, persona),
	)
	return html.Div(html.Props{Class: "persona-admin-evaluation-evidence"}, html.P(html.Props{}, parts...), results)
}

func personaAdminPreviewValidationText(locale LocaleContext, field string) string {
	key := map[string]string{"persona": "choose_agent", "subject": "choose_person", "conversation": "choose_conversation"}[field]
	if key == "" {
		key = "choose_agent"
	}
	return personaAdminR5SetupText(locale, key)
}

func personaAdminAccessCheckResult(locale LocaleContext, agent, person, conversation string, preview PersonaAdminPreview, compatibilitySummary string) ui.Node {
	documents := append([]string(nil), preview.OfficialDocumentTitles...)
	if len(documents) == 0 {
		for _, dataClass := range preview.DerivedData {
			documents = append(documents, PersonaDataClassLabelForLocale(locale, dataClass))
		}
	}
	if len(documents) == 0 {
		documents = append(documents, personaAdminText(locale, "none_set"))
	}
	actions := make([]string, 0, len(preview.EffectiveSkills))
	for _, skill := range preview.EffectiveSkills {
		switch lastPersonaIdentifierSegment(skill.ID) {
		case "knowledge_search_with_citations":
			actions = append(actions, personaAdminR5SetupText(locale, "action_search_cite"))
		case "chat_reply":
			actions = append(actions, personaAdminR5SetupText(locale, "action_reply_conversation"))
		default:
			actions = append(actions, PersonaSkillLabelForLocale(locale, skill.ID, skill.Name, skill.Description))
		}
	}
	if len(actions) == 0 {
		actions = append(actions, personaAdminText(locale, "none_set"))
	}
	baseKey := "preview_result_no_hidden"
	if preview.UnreadableDocumentCount > 0 {
		baseKey = "preview_result"
	}
	text := strings.NewReplacer(
		"{conversation}", conversation,
		"{person}", person,
		"{agent}", agent,
		"{documents}", strings.Join(documents, ", "),
		"{actions}", strings.Join(actions, ", "),
		"{unreadable}", locale.FormatNumber(strconv.Itoa(preview.UnreadableDocumentCount), 0),
		"{first}", personaAdminFirstName(person),
	).Replace(personaAdminR5SetupText(locale, baseKey))
	content := []ui.Node{ui.Text(text)}
	if strings.HasPrefix(conversation, "#") {
		channel := strings.Fields(conversation)[0]
		if before, after, found := strings.Cut(text, channel); found {
			content = []ui.Node{ui.Text(before), html.Tag("bdi", html.Props{Dir: "ltr"}, ui.Text(channel)), ui.Text(after)}
		}
	}
	return html.Div(html.Props{ID: "persona-admin-preview-result", Class: "persona-admin-preview-result", Role: "status", Raw: map[string]any{"tabindex": "-1", "aria-live": "polite", "data-persona-preview-result": "true", "data-access-summary": compatibilitySummary}}, html.P(html.Props{}, content...))
}

func personaAdminFirstName(name string) string {
	if fields := strings.Fields(strings.TrimSpace(name)); len(fields) > 0 {
		return fields[0]
	}
	return name
}

// personaAdminNoStarterNotice replaces the New agent action when no template
// can be used. It says plainly that the build carries none and what the reader
// can still do. A colleague is named only when the directory lists someone
// else who looks after the installation: the person reading Agent setup
// administers the workspace, so they are never told to go and ask the person
// who does.
func personaAdminNoStarterNotice(locale LocaleContext, options []PersonaAdminTarget, viewer string) ui.Node {
	manager := personaAdminRoleContact(options, "platform_administrator")
	if manager.Label == "" || agentUX074ViewerIs(viewer, manager) {
		manager = personaAdminRoleContact(options, "agent_administrator")
	}
	children := []ui.Node{ui.Text(personaAdminR5SetupText(locale, "no_template"))}
	if manager.Label == "" || agentUX074ViewerIs(viewer, manager) {
		children = append(children, ui.Text(personaAdminSpace+personaAdminR5SetupText(locale, "no_template_next")))
	} else {
		children = append(children,
			ui.Text(personaAdminSpace+strings.ReplaceAll(personaAdminR5SetupText(locale, "named_template_manager"), "{person}", manager.Label)+personaAdminSpace),
			html.A(html.Props{Class: "button secondary compact", Href: Path(PageChat) + "?person=" + url.QueryEscape(manager.ID)}, ui.Text(strings.ReplaceAll(personaAdminR5SetupText(locale, "message_person"), "{person}", manager.Label))),
		)
	}
	return html.Div(html.Props{ID: "persona-admin-new-agent-note", Class: "persona-admin-new-agent-note", Role: "status"}, children...)
}

func personaAdminR5SetupText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"rejection_reason":           {"What should the owner change before requesting review again?", "Was soll die verantwortliche Person ändern, bevor sie erneut eine Prüfung anfordert?", "ما الذي يجب أن يغيّره المالك قبل طلب المراجعة مجددًا؟"},
		"no_template":                {"New agents start from a reviewed template, and this build ships with none.", "Neue Agenten beginnen mit einer geprüften Vorlage; dieser Build enthält keine.", "يبدأ الوكلاء الجدد من قالب تمت مراجعته، وهذا الإصدار لا يتضمن أي قالب."},
		"no_template_next":           {"Templates arrive with a product update, not from this page. To change an agent you already have, choose Edit on its card.", "Vorlagen kommen mit einem Produktupdate, nicht über diese Seite. Um einen vorhandenen Agenten zu ändern, wählen Sie auf seiner Karte „Bearbeiten“.", "تصل القوالب مع تحديث المنتج وليس من هذه الصفحة. لتغيير وكيل موجود لديك، اختر «تحرير» في بطاقته."},
		"named_template_manager":     {"{person} can add one with a product update.", "{person} kann mit einem Produktupdate eine hinzufügen.", "يمكن لـ {person} إضافة قالب مع تحديث المنتج."},
		"message_person":             {"Message {person}", "Nachricht an {person}", "مراسلة {person}"},
		"retire_agent":               {"Retire agent…", "Agent stilllegen…", "إيقاف الوكيل…"},
		"retire_role_reason":         {"Only an agent administrator can retire an agent. Ask your agent administrator.", "Nur die Agentenadministration kann einen Agenten stilllegen. Fragen Sie sie.", "يمكن لمسؤول الوكلاء وحده إيقاف وكيل نهائياً. تواصل معه."},
		"retire_role_reason_named":   {"Only an agent administrator can retire an agent. Ask {person}.", "Nur die Agentenadministration kann einen Agenten stilllegen. Fragen Sie {person}.", "يمكن لمسؤول الوكلاء وحده إيقاف وكيل نهائياً. تواصل مع {person}."},
		"audience_roles":             {"People in these {count} roles who are members of a conversation where this agent is added", "Personen in diesen {count} Rollen, die Mitglied einer Unterhaltung mit diesem Agenten sind", "الأشخاص في هذه الأدوار وعددها {count} ممن هم أعضاء في محادثة أُضيف إليها هذا الوكيل"},
		"what_can_read":              {"Documents marked official in each conversation it is added to, plus the documents listed under Documents this agent reads ({count}).", "Als offiziell markierte Dokumente in jeder Unterhaltung, zu der der Agent hinzugefügt wurde, sowie die unter „Dokumente, die dieser Agent liest“ aufgeführten Dokumente ({count}).", "المستندات المعلَّمة كرسمية في كل محادثة أُضيف إليها، بالإضافة إلى المستندات المدرجة ضمن «المستندات التي يقرأها هذا الوكيل» ({count})."},
		"cancel":                     {"Cancel", "Abbrechen", "إلغاء"},
		"add":                        {"Add", "Hinzufügen", "إضافة"},
		"versions":                   {"Versions", "Versionen", "الإصدارات"},
		"technical_version_row":      {"Version {version} · {state}", "Version {version} · {state}", "الإصدار {version} · {state}"},
		"technical_live_version_row": {"Version {version} · Live", "Version {version} · Aktiv", "الإصدار {version} · مباشر"},
		"review_heading":             {"Review", "Prüfung", "المراجعة"},
		"review_someone_else":        {"Someone other than you must review version {version}.", "Eine andere Person muss Version {version} prüfen.", "يجب أن يراجع شخص آخر غيرك الإصدار {version}."},
		"waiting_for_reviewer":       {"Waiting for {reviewer} to review version {version}.", "Warten auf die Prüfung von Version {version} durch {reviewer}.", "بانتظار أن يراجع {reviewer} الإصدار {version}."},
		"author_cannot_review":       {"You wrote version {version}, so someone else must review it.", "Sie haben Version {version} verfasst; deshalb muss eine andere Person sie prüfen.", "أنت كتبت الإصدار {version}، لذلك يجب أن يراجعه شخص آخر."},
		"message_reviewer":           {"Message {reviewer}", "Nachricht an {reviewer}", "مراسلة {reviewer}"},
		"evaluation_heading":         {"Evaluation", "Evaluierung", "التقييم"},
		"evaluation_runs_cases":      {"Runs {count} test questions against version {version}. Takes a few minutes.", "Führt {count} Testfragen mit Version {version} aus. Das dauert einige Minuten.", "يشغّل {count} أسئلة اختبار على الإصدار {version}. يستغرق بضع دقائق."},
		"evaluation_passed_prefix":   {"Evaluation passed", "Evaluierung bestanden", "نجح التقييم"},
		"of":                         {"of", "von", "من"},
		"test_questions":             {"test questions", "Testfragen", "أسئلة اختبار"},
		"run_by":                     {"run by", "ausgeführt von", "أجراه"},
		"view_results":               {"View results", "Ergebnisse ansehen", "عرض النتائج"},
		"next_evaluator":             {"Next: {person} runs the evaluation.", "Als Nächstes führt {person} die Evaluierung aus.", "التالي: يجري {person} التقييم."},
		"no_installations_live":      {"Not added to a conversation yet. Choose {action} so people can use it.", "Noch zu keiner Unterhaltung hinzugefügt. Wählen Sie „{action}“, damit Personen ihn verwenden können.", "لم تتم إضافته إلى محادثة بعد. اختر «{action}» ليتمكن الأشخاص من استخدامه."},
		"next_evaluator_role":        {"Next: a person who administers agents runs the evaluation.", "Als Nächstes führt eine Person aus der Agentenadministration die Evaluierung aus.", "التالي: يجري التقييم شخص يدير الوكلاء."},
		"message_evaluator":          {"Message {person}", "Nachricht an {person}", "مراسلة {person}"},
		"ready_publish_heading":      {"Ready to publish", "Bereit zur Veröffentlichung", "جاهز للنشر"},
		"publish_does_not_move":      {"Publishing does not change any conversation. Afterwards, move conversations to version {version} in", "Die Veröffentlichung ändert keine Unterhaltung. Verschieben Sie Unterhaltungen anschließend unter", "لا يغيّر النشر أي محادثة. بعد ذلك، انقل المحادثات إلى الإصدار {version} في"},
		"published_heading":          {"Published", "Veröffentlicht", "منشور"},
		"unreadable_review":          {"You cannot open {unreadable} of the {total} documents this agent reads.", "Sie können {unreadable} der {total} Dokumente, die dieser Agent liest, nicht öffnen.", "لا يمكنك فتح {unreadable} من أصل {total} من المستندات التي يقرأها هذا الوكيل."},
		"ask_owner_access":           {"Ask {owner} for access", "{owner} um Zugriff bitten", "اطلب الوصول من {owner}"},
		"approve_unread":             {"Approve without reading the documents", "Ohne Lesen der Dokumente genehmigen", "الموافقة دون قراءة المستندات"},
		"choose_person":              {"Choose a person.", "Wählen Sie eine Person aus.", "اختر شخصاً."},
		"choose_agent":               {"Choose an agent.", "Wählen Sie einen Agenten aus.", "اختر وكيلاً."},
		"choose_conversation":        {"Choose a conversation.", "Wählen Sie eine Unterhaltung aus.", "اختر محادثة."},
		"preview_result":             {"In {conversation}, for {person}, {agent} can read: {documents}. It can: {actions}. It cannot read: {unreadable} documents {first} cannot open.", "In {conversation} kann {agent} für {person} Folgendes lesen: {documents}. Der Agent kann: {actions}. Nicht lesbar sind: {unreadable} Dokumente, die {first} nicht öffnen kann.", "في {conversation}، بالنسبة إلى {person}، يمكن لـ {agent} قراءة: {documents}. ويمكنه: {actions}. ولا يمكنه قراءة: {unreadable} من المستندات التي لا يستطيع {first} فتحها."},
		"preview_result_no_hidden":   {"In {conversation}, for {person}, {agent} can read: {documents}. It can: {actions}.", "In {conversation} kann {agent} für {person} Folgendes lesen: {documents}. Der Agent kann: {actions}.", "في {conversation}، بالنسبة إلى {person}، يمكن لـ {agent} قراءة: {documents}. ويمكنه: {actions}."},
		"action_search_cite":         {"search and cite", "suchen und zitieren", "البحث والاستشهاد"},
		"action_reply_conversation":  {"reply in the conversation", "in der Unterhaltung antworten", "الرد في المحادثة"},
		"no_unreadable_documents":    {"No additional documents are hidden from {person}.", "Keine weiteren Dokumente sind für {person} ausgeblendet.", "لا توجد مستندات إضافية مخفية عن {person}."},
		"cannot_open_document":       {"You cannot open this document.", "Sie können dieses Dokument nicht öffnen.", "لا يمكنك فتح هذا المستند."},
	}
	values, ok := copy[key]
	if !ok {
		return key
	}
	index := 0
	switch locale.Resolved {
	case "de-DE":
		index = 1
	case "ar":
		index = 2
	}
	return values[index]
}
