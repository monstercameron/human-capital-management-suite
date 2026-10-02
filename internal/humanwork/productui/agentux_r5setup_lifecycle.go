package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type personaAdminLifecyclePhase string

const (
	personaAdminPhaseDraft             personaAdminLifecyclePhase = "draft"
	personaAdminPhaseWaitingReview     personaAdminLifecyclePhase = "waiting-review"
	personaAdminPhaseReadyEvaluation   personaAdminLifecyclePhase = "ready-evaluation"
	personaAdminPhaseEvaluationRunning personaAdminLifecyclePhase = "evaluation-running"
	personaAdminPhaseEvaluationFailed  personaAdminLifecyclePhase = "evaluation-failed"
	personaAdminPhaseReadyPublication  personaAdminLifecyclePhase = "ready-publication"
	personaAdminPhasePublished         personaAdminLifecyclePhase = "published"
)

// personaAdminLifecyclePresentation is the single mapping used by the card
// badge, lifecycle sentence, step label, and lifecycle-block heading.
type personaAdminLifecyclePresentation struct {
	Phase          personaAdminLifecyclePhase
	Step           int
	CompletedSteps int
	StepLabel      string
	NextStepLabel  string
	Heading        string
	Badge          string
	SentenceKey    string
	LiveVersion    string
	Placement      string
	PlacementVer   string
}

func personaAdminLifecycleState(locale LocaleContext, persona PersonaAdminPersona) personaAdminLifecyclePresentation {
	phase := personaAdminPhaseDraft
	switch {
	case persona.Lifecycle == PersonaPublished || persona.Lifecycle == PersonaSuspended:
		phase = personaAdminPhasePublished
	case persona.EvaluationRef != "" || (strings.EqualFold(persona.EvaluationStatus, "PASSED") && persona.EvaluationFailed == 0):
		phase = personaAdminPhaseReadyPublication
	case strings.EqualFold(persona.EvaluationStatus, "RUNNING"):
		phase = personaAdminPhaseEvaluationRunning
	case strings.EqualFold(persona.EvaluationStatus, "FAILED") || persona.EvaluationFailed > 0:
		phase = personaAdminPhaseEvaluationFailed
	case persona.ReviewApproved:
		phase = personaAdminPhaseReadyEvaluation
	case persona.Lifecycle == PersonaInReview:
		phase = personaAdminPhaseWaitingReview
	}

	// The step label names the step in progress, not the last one finished: once
	// a reviewer has approved, the version is at step three, ready to evaluate,
	// and once the evaluation has passed it is at step four, ready to publish.
	// The badge takes the same label, so the badge, the sentence and the stepper
	// always say the same thing.
	keys := map[personaAdminLifecyclePhase]struct {
		step, complete                 int
		label, next, heading, sentence string
	}{
		personaAdminPhaseDraft:             {1, 0, "phase_draft", "phase_waiting_review", "heading_review", "sentence_draft"},
		personaAdminPhaseWaitingReview:     {2, 1, "phase_waiting_review", "phase_ready_evaluation", "heading_review", "sentence_waiting_review"},
		personaAdminPhaseReadyEvaluation:   {3, 2, "phase_ready_evaluation", "phase_ready_publication", "heading_evaluation", "sentence_ready_evaluation"},
		personaAdminPhaseEvaluationRunning: {3, 2, "phase_evaluation_running", "phase_ready_publication", "heading_evaluation", "sentence_evaluation_running"},
		personaAdminPhaseEvaluationFailed:  {3, 2, "phase_evaluation_failed", "phase_ready_evaluation", "heading_evaluation", "sentence_evaluation_failed"},
		personaAdminPhaseReadyPublication:  {4, 3, "phase_ready_publication", "phase_published", "heading_ready_publication", "sentence_ready_publication"},
		personaAdminPhasePublished:         {4, 4, "phase_published", "", "heading_published", "sentence_published_no_placements"},
	}[phase]

	live := personaAdminLiveVersionValue(persona)
	placement, placementVersion := personaAdminOutdatedPlacement(locale, persona)
	sentence := keys.sentence
	if phase == personaAdminPhasePublished {
		switch {
		case placementVersion != "":
			sentence = "sentence_published_outdated"
		case len(persona.Installations) > 0:
			sentence = "sentence_published_everywhere"
		}
		if persona.Lifecycle == PersonaSuspended {
			keys.label = "phase_suspended"
			keys.heading = "phase_suspended"
			sentence = "sentence_suspended"
		}
	} else if live == "" {
		sentence += "_no_live"
	}
	return personaAdminLifecyclePresentation{
		Phase: phase, Step: keys.step, CompletedSteps: keys.complete,
		StepLabel: personaAdminR5Text(locale, keys.label), NextStepLabel: personaAdminR5Text(locale, keys.next),
		Heading: personaAdminR5Text(locale, keys.heading), Badge: personaAdminR5Text(locale, keys.label),
		SentenceKey: sentence, LiveVersion: live, Placement: placement, PlacementVer: placementVersion,
	}
}

func personaAdminOutdatedPlacement(locale LocaleContext, persona PersonaAdminPersona) (string, string) {
	for _, installation := range persona.Installations {
		version := strings.TrimSpace(installation.Version)
		if version != "" && version != strings.TrimSpace(persona.Version) {
			return personaAdminPlacementName(locale, installation), version
		}
	}
	return "", ""
}

func personaAdminLifecycleSentence(locale LocaleContext, persona PersonaAdminPersona, state personaAdminLifecyclePresentation) ui.Node {
	values := strings.NewReplacer(
		"{version}", personaAdminLocalizedNumber(locale, persona.Version),
		"{live}", personaAdminLocalizedNumber(locale, state.LiveVersion),
		"{placement}", state.Placement,
		"{placement_version}", personaAdminLocalizedNumber(locale, state.PlacementVer),
	)
	if state.SentenceKey == "sentence_published_outdated" {
		prefix := values.Replace(personaAdminR5Text(locale, "sentence_published_outdated_prefix"))
		return html.P(html.Props{Class: "persona-admin-version-status", Role: "status", Raw: map[string]any{"data-lifecycle-sentence": state.SentenceKey}},
			ui.Text(prefix+" "),
			html.A(html.Props{Href: "/workspace/app/admin/agents?tab=rollout"}, ui.Text(personaAdminR5Text(locale, "rollout_link"))),
			ui.Text(personaAdminR5Text(locale, "sentence_end")),
		)
	}
	raw := map[string]any{"data-lifecycle-sentence": state.SentenceKey}
	if state.Phase == personaAdminPhaseDraft && state.LiveVersion != "" {
		raw["data-live-draft-summary"] = strings.NewReplacer("{live}", personaAdminLocalizedNumber(locale, state.LiveVersion), "{draft}", personaAdminLocalizedNumber(locale, persona.Version)).Replace(personaAdminText(locale, "live_and_draft"))
	}
	return html.P(html.Props{Class: "persona-admin-version-status", Role: "status", Raw: raw}, ui.Text(values.Replace(personaAdminR5Text(locale, state.SentenceKey))))
}

func personaAdminLifecycleBadges(locale LocaleContext, persona PersonaAdminPersona, state personaAdminLifecyclePresentation) ui.Node {
	badges := make([]ui.Node, 0, 2)
	if state.LiveVersion != "" && state.LiveVersion != strings.TrimSpace(persona.Version) {
		label := strings.ReplaceAll(personaAdminR5Text(locale, "live_badge"), "{version}", personaAdminLocalizedNumber(locale, state.LiveVersion))
		badges = append(badges, html.Span(html.Props{Class: "status persona-admin-status", Raw: map[string]any{"data-status-tone": "published", "data-lifecycle-badge": "live"}}, ui.Text(label)))
	}
	label := strings.NewReplacer("{version}", personaAdminLocalizedNumber(locale, persona.Version), "{phase}", state.Badge).Replace(personaAdminR5Text(locale, "version_badge"))
	tone := "neutral"
	if state.Phase == personaAdminPhasePublished && persona.Lifecycle != PersonaSuspended && len(badges) == 0 {
		tone = "published"
	}
	badges = append(badges, html.Span(html.Props{Class: "status persona-admin-status", Raw: map[string]any{"data-status-tone": tone, "data-lifecycle-badge": string(state.Phase)}}, ui.Text(label)))
	return html.Span(html.Props{Class: "persona-admin-statuses"}, badges...)
}

func personaAdminLifecycleProgress(locale LocaleContext, persona PersonaAdminPersona, state personaAdminLifecyclePresentation) ui.Node {
	labels := []string{"step_draft", "step_review", "step_evaluated", "step_published"}
	steps := make([]ui.Node, 0, len(labels))
	for index, key := range labels {
		step := index + 1
		status := "upcoming"
		if step <= state.CompletedSteps {
			status = "complete"
		} else if step == state.Step {
			status = "current"
		}
		label := personaAdminText(locale, key)
		if status == "current" {
			label = state.StepLabel
		}
		content := []ui.Node{html.Span(html.Props{Class: "persona-admin-step-label"}, ui.Text(label))}
		if status == "complete" {
			content = append([]ui.Node{html.Span(html.Props{Class: "persona-admin-step-check", Aria: map[string]string{"hidden": "true"}}, ui.Text("✓"))}, content...)
		}
		raw := map[string]any{"data-step-state": status, "data-step-number": step}
		if status == "current" {
			raw["aria-current"] = "step"
		}
		steps = append(steps, html.Li(html.Props{Raw: raw}, html.Span(html.Props{Class: "persona-admin-step-content"}, content...)))
	}
	progress := strings.NewReplacer(
		"{current}", locale.FormatNumber(strconv.Itoa(state.Step), 0),
		"{total}", locale.FormatNumber(strconv.Itoa(len(labels)), 0),
		"{state}", state.StepLabel,
	).Replace(personaAdminText(locale, "lifecycle_progress_text"))
	mobile := state.StepLabel
	if state.NextStepLabel != "" && state.CompletedSteps < len(labels) {
		mobile = strings.NewReplacer("{current}", state.StepLabel, "{next}", state.NextStepLabel).Replace(personaAdminR5Text(locale, "mobile_progress"))
	}
	return html.Div(html.Props{Class: "persona-admin-lifecycle-progress", Raw: map[string]any{"data-lifecycle-phase": string(state.Phase)}},
		html.P(html.Props{Class: "persona-admin-progress-label"}, ui.Text(progress)),
		html.Ol(html.Props{Aria: map[string]string{"label": personaAdminText(locale, "lifecycle_progress")}}, steps...),
		html.P(html.Props{Class: "persona-admin-mobile-progress", Raw: map[string]any{"data-mobile-progress": "true"}}, ui.Text(mobile)),
	)
}

func personaAdminR5Text(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"phase_draft":                         {"Draft", "Entwurf", "مسودة"},
		"phase_waiting_review":                {"Waiting for review", "Wartet auf Prüfung", "بانتظار المراجعة"},
		"phase_ready_evaluation":              {"Ready to evaluate", "Bereit zur Evaluierung", "جاهز للتقييم"},
		"phase_evaluation_running":            {"Evaluation running", "Evaluierung läuft", "التقييم قيد التشغيل"},
		"phase_evaluation_failed":             {"Evaluation needs changes", "Evaluierung erfordert Änderungen", "يحتاج التقييم إلى تعديلات"},
		"phase_ready_publication":             {"Ready to publish", "Bereit zur Veröffentlichung", "جاهز للنشر"},
		"phase_published":                     {"Published", "Veröffentlicht", "منشور"},
		"phase_suspended":                     {"Paused", "Pausiert", "متوقف مؤقتًا"},
		"heading_review":                      {"Review", "Prüfung", "المراجعة"},
		"heading_evaluation":                  {"Evaluation", "Evaluierung", "التقييم"},
		"heading_ready_publication":           {"Ready to publish", "Bereit zur Veröffentlichung", "جاهز للنشر"},
		"heading_published":                   {"Published", "Veröffentlicht", "منشور"},
		"live_badge":                          {"Live · version {version}", "Aktiv · Version {version}", "مباشر · الإصدار {version}"},
		"version_badge":                       {"Version {version} · {phase}", "Version {version} · {phase}", "الإصدار {version} · {phase}"},
		"sentence_draft":                      {"People are using version {live}. Version {version} is a draft.", "Personen verwenden Version {live}. Version {version} ist ein Entwurf.", "يستخدم الأشخاص الإصدار {live}. الإصدار {version} مسودة."},
		"sentence_waiting_review":             {"People are using version {live}. Version {version} is waiting for review.", "Personen verwenden Version {live}. Version {version} wartet auf Prüfung.", "يستخدم الأشخاص الإصدار {live}. الإصدار {version} بانتظار المراجعة."},
		"sentence_ready_evaluation":           {"People are using version {live}. Version {version} is waiting for evaluation.", "Personen verwenden Version {live}. Version {version} wartet auf die Evaluierung.", "يستخدم الأشخاص الإصدار {live}. الإصدار {version} بانتظار التقييم."},
		"sentence_evaluation_running":         {"People are using version {live}. Version {version} is being evaluated.", "Personen verwenden Version {live}. Version {version} wird evaluiert.", "يستخدم الأشخاص الإصدار {live}. جارٍ تقييم الإصدار {version}."},
		"sentence_evaluation_failed":          {"People are using version {live}. Version {version} needs changes before it can be published.", "Personen verwenden Version {live}. Version {version} muss vor der Veröffentlichung geändert werden.", "يستخدم الأشخاص الإصدار {live}. يحتاج الإصدار {version} إلى تعديلات قبل نشره."},
		"sentence_ready_publication":          {"People are using version {live}. Version {version} is ready to publish.", "Personen verwenden Version {live}. Version {version} kann veröffentlicht werden.", "يستخدم الأشخاص الإصدار {live}. الإصدار {version} جاهز للنشر."},
		"sentence_draft_no_live":              {"Version {version} is a draft.", "Version {version} ist ein Entwurf.", "الإصدار {version} مسودة."},
		"sentence_waiting_review_no_live":     {"Version {version} is waiting for review.", "Version {version} wartet auf Prüfung.", "الإصدار {version} بانتظار المراجعة."},
		"sentence_ready_evaluation_no_live":   {"Version {version} is waiting for evaluation.", "Version {version} wartet auf die Evaluierung.", "الإصدار {version} بانتظار التقييم."},
		"sentence_evaluation_running_no_live": {"Version {version} is being evaluated.", "Version {version} wird evaluiert.", "جارٍ تقييم الإصدار {version}."},
		"sentence_evaluation_failed_no_live":  {"Version {version} needs changes before it can be published.", "Version {version} muss vor der Veröffentlichung geändert werden.", "يحتاج الإصدار {version} إلى تعديلات قبل نشره."},
		"sentence_ready_publication_no_live":  {"Version {version} is ready to publish.", "Version {version} kann veröffentlicht werden.", "الإصدار {version} جاهز للنشر."},
		"sentence_published_outdated_prefix":  {"Version {version} is published. {placement} still runs version {placement_version}. Move conversations to version {version} in", "Version {version} ist veröffentlicht. In {placement} läuft noch Version {placement_version}. Verschieben Sie Unterhaltungen auf Version {version} unter", "تم نشر الإصدار {version}. لا تزال {placement} تشغّل الإصدار {placement_version}. انقل المحادثات إلى الإصدار {version} في"},
		"sentence_published_everywhere":       {"Every conversation runs version {version}.", "In jeder Unterhaltung läuft Version {version}.", "تشغّل كل محادثة الإصدار {version}."},
		"sentence_published_no_placements":    {"Version {version} is published. Add it to a conversation before people can use it.", "Version {version} ist veröffentlicht. Fügen Sie sie einer Unterhaltung hinzu, bevor Personen sie verwenden können.", "تم نشر الإصدار {version}. أضفه إلى محادثة قبل أن يتمكن الأشخاص من استخدامه."},
		"sentence_suspended":                  {"Version {version} is paused. Resume the agent to let it answer again.", "Version {version} ist pausiert. Setzen Sie den Agenten fort, damit er wieder antwortet.", "الإصدار {version} متوقف مؤقتًا. استأنف الوكيل حتى يجيب مجددًا."},
		"rollout_link":                        {"Operations › Rollout", "Betrieb › Versionswechsel", "العمليات › الطرح"},
		"sentence_end":                        {".", ".", "."},
		"mobile_progress":                     {"{current} · next: {next}", "{current} · als Nächstes: {next}", "{current} · التالي: {next}"},
		"independent_reviewer":                {"an independent reviewer", "eine unabhängige prüfende Person", "مراجع مستقل"},
		"command_completed":                   {"The change was saved.", "Die Änderung wurde gespeichert.", "تم حفظ التغيير."},
		"command_review_requested":            {"Review of version {version} requested from {reviewer}.", "Prüfung von Version {version} bei {reviewer} angefordert.", "طُلبت مراجعة الإصدار {version} من {reviewer}."},
		"command_version_approved":            {"Version {version} approved.", "Version {version} genehmigt.", "تمت الموافقة على الإصدار {version}."},
		"command_evaluation_passed":           {"Evaluation passed: {passed} of {total} cases.", "Evaluierung bestanden: {passed} von {total} Testfragen.", "نجح التقييم: {passed} من {total} حالات."},
		"command_version_published":           {"Version {version} published.", "Version {version} veröffentlicht.", "تم نشر الإصدار {version}."},
		"command_version_saved":               {"Version {version} saved as a draft.", "Version {version} als Entwurf gespeichert.", "تم حفظ الإصدار {version} كمسودة."},
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

func personaAdminCommandOutcomeText(locale LocaleContext, action string, persona PersonaAdminPersona) string {
	key := "command_completed"
	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "REQUEST_REVIEW":
		key = "command_review_requested"
	case "REVIEW":
		key = "command_version_approved"
	case "REJECT":
		return agentUXR7Text(locale, "review_rejected", "{version}", personaAdminLocalizedNumber(locale, persona.Version))
	case "RUN_EVALUATION":
		key = "command_evaluation_passed"
	case "PUBLISH":
		key = "command_version_published"
	case "CREATE_VERSION":
		key = "command_version_saved"
	case "SUSPEND":
		return agentUXR7Text(locale, "pause_saved", "{agent}", persona.Name)
	case "RESUME":
		return agentUXR7Text(locale, "resume_saved", "{agent}", persona.Name)
	}
	reviewer := PersonaWorkerLabel(persona.Reviewer, persona.ReviewerName)
	if strings.TrimSpace(reviewer) == "" {
		reviewer = personaAdminR5Text(locale, "independent_reviewer")
	}
	total := persona.EvaluationPassed + persona.EvaluationFailed
	if total == 0 {
		total = persona.EvaluationPassed
	}
	return strings.NewReplacer(
		"{version}", personaAdminLocalizedNumber(locale, persona.Version),
		"{reviewer}", reviewer,
		"{passed}", locale.FormatNumber(strconv.Itoa(persona.EvaluationPassed), 0),
		"{total}", locale.FormatNumber(strconv.Itoa(total), 0),
	).Replace(personaAdminR5Text(locale, key))
}
