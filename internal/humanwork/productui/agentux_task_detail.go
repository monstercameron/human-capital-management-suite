package productui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The task detail sections below (checkpoints, artifacts, approvals, submitted
// requests, plan changes) are filled by the server from the task's own record.
// The server sends short codes rather than sentences, so the page can say them
// in the reader's language. A value the page does not know is shown as it
// came: older projections and test fixtures send finished text.

// Checkpoint kinds the server sends in AgentCheckpoint.Kind.
const (
	AgentCheckpointPlanConfirmed   = "plan_confirmed"
	AgentCheckpointStepFinished    = "step_finished"
	AgentCheckpointWaitingApproval = "waiting_approval"
	AgentCheckpointWaitingReply    = "waiting_reply"
	AgentCheckpointWaitingTimer    = "waiting_timer"
	AgentCheckpointWaitingUpdate   = "waiting_update"
	AgentCheckpointResumed         = "resumed"
	AgentCheckpointPaused          = "paused"
)

// Artifact kinds the server sends in AgentArtifact.Kind.
const (
	AgentArtifactDraft    = "draft"
	AgentArtifactDocument = "document"
	AgentArtifactReport   = "report"
	AgentArtifactResult   = "result"
)

// Statuses the server sends in AgentIntentStatus.Status.
const (
	AgentIntentDraft            = "draft"
	AgentIntentAwaitingApproval = "awaiting_approval"
	AgentIntentExecuting        = "executing"
	AgentIntentObserved         = "observed"
	AgentIntentNeedsRepair      = "needs_repair"
	AgentIntentFailed           = "failed"
)

// Approval source codes. A document source is "document:" followed by the
// label the task's owner already sees for that document.
const (
	AgentApprovalSourceRequest     = "request"
	AgentApprovalSourceRecords     = "records"
	AgentApprovalSourceEarlierStep = "earlier_step"
	AgentApprovalSourceDocument    = "document:"
)

// Plan change directions in AgentPlanChange.Change.
const (
	AgentPlanChangeAdded   = "added"
	AgentPlanChangeRemoved = "removed"
)

// AgentPlanChange is one step a new plan revision adds or drops, compared with
// the plan the user last confirmed.
type AgentPlanChange struct {
	Change string
	Step   AgentTaskStep
}

func agentTaskDetailText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"checkpoint." + AgentCheckpointPlanConfirmed:   {"You confirmed the plan", "Sie haben den Plan bestätigt", "أكّدتَ الخطة"},
		"checkpoint." + AgentCheckpointStepFinished:    {"Finished: {step}", "Abgeschlossen: {step}", "اكتمل: {step}"},
		"checkpoint." + AgentCheckpointWaitingApproval: {"Stopped to wait for your approval", "Angehalten, bis Sie zustimmen", "توقفت بانتظار موافقتك"},
		"checkpoint." + AgentCheckpointWaitingReply:    {"Stopped to wait for your reply", "Angehalten, bis Sie antworten", "توقفت بانتظار ردّك"},
		"checkpoint." + AgentCheckpointWaitingTimer:    {"Stopped to wait until a set time", "Angehalten bis zu einem festgelegten Zeitpunkt", "توقفت حتى وقت محدد"},
		"checkpoint." + AgentCheckpointWaitingUpdate:   {"Stopped to wait for an update", "Angehalten, bis eine Aktualisierung eintrifft", "توقفت بانتظار تحديث"},
		"checkpoint." + AgentCheckpointResumed:         {"Picked up again", "Wieder aufgenommen", "استؤنفت"},
		"checkpoint." + AgentCheckpointPaused:          {"Paused", "Pausiert", "أُوقفت مؤقتًا"},

		"artifact." + AgentArtifactDraft:    {"Private draft", "Privater Entwurf", "مسودة خاصة"},
		"artifact." + AgentArtifactDocument: {"Document", "Dokument", "مستند"},
		"artifact." + AgentArtifactReport:   {"Report", "Bericht", "تقرير"},
		"artifact." + AgentArtifactResult:   {"Result", "Ergebnis", "نتيجة"},
		"artifact.no_link":                  {"Kept with this task", "Bei dieser Aufgabe gespeichert", "محفوظ مع هذه المهمة"},

		"intent." + AgentIntentDraft:            {"Draft, not sent", "Entwurf, nicht gesendet", "مسودة لم تُرسل"},
		"intent." + AgentIntentAwaitingApproval: {"Waiting for your approval", "Wartet auf Ihre Zustimmung", "بانتظار موافقتك"},
		"intent." + AgentIntentExecuting:        {"Being carried out", "Wird ausgeführt", "قيد التنفيذ"},
		"intent." + AgentIntentObserved:         {"Done and checked", "Erledigt und geprüft", "تمّ وجرى التحقق"},
		"intent." + AgentIntentNeedsRepair:      {"Needs review before it continues", "Muss geprüft werden, bevor es weitergeht", "يحتاج إلى مراجعة قبل المتابعة"},
		"intent." + AgentIntentFailed:           {"Could not be completed", "Konnte nicht abgeschlossen werden", "تعذّر إكماله"},

		"source." + AgentApprovalSourceRequest:     {"Your request", "Ihre Anfrage", "طلبك"},
		"source." + AgentApprovalSourceRecords:     {"Records the agent read for you", "Daten, die der Agent für Sie gelesen hat", "سجلات قرأها الوكيل لك"},
		"source." + AgentApprovalSourceEarlierStep: {"An earlier step of this task", "Ein früherer Schritt dieser Aufgabe", "خطوة سابقة من هذه المهمة"},
		"source.document":                          {"Document: {name}", "Dokument: {name}", "مستند: {name}"},
		"source.document_unnamed":                  {"A document", "Ein Dokument", "مستند"},
		"source.none":                              {"No sources were recorded for this step.", "Für diesen Schritt wurden keine Quellen erfasst.", "لم تُسجَّل مصادر لهذه الخطوة."},

		"taint.CANONICAL_FACT":               {"From company records", "Aus Unternehmensdaten", "من سجلات الشركة"},
		"taint.HUMAN_ASSERTION":              {"Written by a person", "Von einer Person geschrieben", "كتبه شخص"},
		"taint.USER_AUTHORED":                {"Written by you", "Von Ihnen geschrieben", "كتبتَه أنت"},
		"taint.AGENT_DERIVED":                {"Written by the agent", "Vom Agenten geschrieben", "كتبه الوكيل"},
		"taint.TOOL_DERIVED":                 {"Read by a tool", "Von einem Werkzeug gelesen", "قرأته أداة"},
		"taint.EXTERNAL_UNTRUSTED":           {"From outside the company, not verified", "Von außerhalb des Unternehmens, nicht geprüft", "من خارج الشركة، غير موثَّق"},
		"taint.UNTRUSTED_PEER":               {"From other people's messages, not verified", "Aus Nachrichten anderer Personen, nicht geprüft", "من رسائل أشخاص آخرين، غير موثَّق"},
		"taint.UNTRUSTED_REFERENCE_DOCUMENT": {"From an attached document, not verified", "Aus einem angehängten Dokument, nicht geprüft", "من مستند مرفق، غير موثَّق"},

		"plan.changes":                   {"What changed in this plan", "Was sich an diesem Plan geändert hat", "ما الذي تغيّر في هذه الخطة"},
		"plan." + AgentPlanChangeAdded:   {"Added: {step}", "Hinzugefügt: {step}", "أُضيفت: {step}"},
		"plan." + AgentPlanChangeRemoved: {"Removed: {step}", "Entfernt: {step}", "أُزيلت: {step}"},
		"plan.confirm_note":              {"Review the changes, then start the plan to continue.", "Prüfen Sie die Änderungen und starten Sie dann den Plan, um fortzufahren.", "راجع التغييرات ثم ابدأ الخطة للمتابعة."},
		"approval.digest":                {"Approval reference", "Zustimmungsreferenz", "مرجع الموافقة"},
		"approval.digest_note":           {"Your approval covers exactly this step. If the step changes, you are asked again.", "Ihre Zustimmung gilt genau für diesen Schritt. Ändert er sich, werden Sie erneut gefragt.", "موافقتك تخص هذه الخطوة تحديدًا. إذا تغيّرت الخطوة فسيُطلب منك الموافقة مجددًا."},
		"goal.heading":                   {"Your request", "Ihre Anfrage", "طلبك"},
		"approval.content":               {"Where the content comes from", "Woher der Inhalt stammt", "مصدر المحتوى"},
	}
	values, ok := copy[key]
	if !ok {
		return ""
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

// agentTaskStepLabel names a step the way the plan list does.
func agentTaskStepLabel(locale LocaleContext, name string) string {
	label, _ := agentStepPresentation(locale, AgentTaskStep{Name: name})
	return label
}

func agentTaskCheckpointItem(locale LocaleContext, checkpoint AgentCheckpoint, now time.Time) ui.Node {
	label := strings.TrimSpace(checkpoint.Label)
	if text := agentTaskDetailText(locale, "checkpoint."+strings.TrimSpace(checkpoint.Kind)); text != "" {
		label = strings.ReplaceAll(text, "{step}", agentTaskStepLabel(locale, checkpoint.Step))
	}
	when := strings.TrimSpace(checkpoint.At)
	if at, err := time.Parse(time.RFC3339Nano, when); err == nil {
		when = agentTaskDateTimeLabel(locale, at, now)
	}
	children := []ui.Node{html.Strong(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(label))}
	if when != "" {
		children = append(children, html.Span(html.Props{Class: "muted"}, ui.Text(when)))
	}
	return html.Li(html.Props{Raw: map[string]any{"data-checkpoint": strings.TrimSpace(checkpoint.Kind)}}, children...)
}

func agentTaskArtifactItem(view View, locale LocaleContext, artifact AgentArtifact) ui.Node {
	kind := strings.TrimSpace(artifact.Kind)
	kindLabel := agentTaskDetailText(locale, "artifact."+kind)
	if kindLabel == "" {
		kindLabel = kind
	}
	name := strings.TrimSpace(artifact.Name)
	if name == "" {
		name = kindLabel
	}
	raw := map[string]any{"data-artifact": kind}
	if href := strings.TrimSpace(artifact.Href); href != "" {
		return html.Li(html.Props{Raw: raw}, softwareLink(view.Navigate, html.Props{}, href, ui.Text(name)), html.Span(html.Props{Class: "muted"}, ui.Text(kindLabel)))
	}
	// Nothing to open: say what it is and that it stays with the task, rather
	// than offering a link that goes nowhere.
	return html.Li(html.Props{Raw: raw}, html.Strong(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(name)), html.Span(html.Props{Class: "muted"}, ui.Text(agentTaskDetailText(locale, "artifact.no_link"))))
}

func agentTaskApprovalSource(locale LocaleContext, source string) string {
	source = strings.TrimSpace(source)
	if name, ok := strings.CutPrefix(source, AgentApprovalSourceDocument); ok {
		if name = strings.TrimSpace(name); name != "" {
			return strings.ReplaceAll(agentTaskDetailText(locale, "source.document"), "{name}", name)
		}
		return agentTaskDetailText(locale, "source.document_unnamed")
	}
	if text := agentTaskDetailText(locale, "source."+source); text != "" {
		return text
	}
	return source
}

// agentTaskTaintLabels turns the comma-separated labels of an approval into
// plain statements of where the content came from.
func agentTaskTaintLabels(locale LocaleContext, taint string) []string {
	var labels []string
	seen := map[string]bool{}
	for _, code := range strings.Split(taint, ",") {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		label := agentTaskDetailText(locale, "taint."+strings.ToUpper(code))
		if label == "" {
			label = code
		}
		if !seen[label] {
			seen[label] = true
			labels = append(labels, label)
		}
	}
	return labels
}

func agentTaskApprovalCard(locale LocaleContext, approval AgentApproval) ui.Node {
	summary := agentTaskStepLabel(locale, approval.Summary)
	children := []ui.Node{html.H3(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(summary))}
	if digest := strings.TrimSpace(approval.Digest); digest != "" {
		children = append(children,
			html.P(html.Props{Class: "agents-approval-digest"}, html.Strong(html.Props{}, ui.Text(agentTaskDetailText(locale, "approval.digest")+": ")), html.Code(html.Props{Class: "agents-digest", Dir: "ltr"}, ui.Text(digest))),
			html.P(html.Props{Class: "muted"}, ui.Text(agentTaskDetailText(locale, "approval.digest_note"))),
		)
	}
	if labels := agentTaskTaintLabels(locale, approval.Taint); len(labels) > 0 {
		items := make([]ui.Node, 0, len(labels))
		for _, label := range labels {
			items = append(items, html.Li(html.Props{}, html.Span(html.Props{Class: "status agents-taint"}, ui.Text(label))))
		}
		children = append(children, html.H4(html.Props{}, ui.Text(agentTaskDetailText(locale, "approval.content"))), html.Ul(html.Props{Class: "agents-taint-list"}, items...))
	}
	children = append(children, html.H4(html.Props{}, ui.Text(locale.Text("agents.sources"))))
	if len(approval.Sources) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(agentTaskDetailText(locale, "source.none"))))
	} else {
		sources := make([]ui.Node, 0, len(approval.Sources))
		for _, source := range approval.Sources {
			sources = append(sources, html.Li(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(agentTaskApprovalSource(locale, source))))
		}
		children = append(children, html.Ul(html.Props{}, sources...))
	}
	return html.Article(html.Props{Class: "agents-approval-card", Raw: map[string]any{"data-approval-id": approval.ID}}, children...)
}

func agentTaskIntentItem(locale LocaleContext, intent AgentIntentStatus) ui.Node {
	status := strings.TrimSpace(intent.Status)
	label := agentTaskDetailText(locale, "intent."+status)
	if label == "" {
		label = status
	}
	return html.Li(html.Props{Raw: map[string]any{"data-intent-status": status}}, html.Strong(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(agentTaskStepLabel(locale, intent.Name))), html.Span(html.Props{Class: "status"}, ui.Text(label)))
}

// agentTaskPlanChanges lists what a new plan revision adds and drops, so the
// user confirms the difference and not only the result.
func agentTaskPlanChanges(locale LocaleContext, changes []AgentPlanChange) ui.Node {
	items := make([]ui.Node, 0, len(changes))
	for _, change := range changes {
		text := agentTaskDetailText(locale, "plan."+strings.TrimSpace(change.Change))
		if text == "" {
			continue
		}
		name, _ := agentStepPresentation(locale, change.Step)
		items = append(items, html.Li(html.Props{Raw: map[string]any{"data-plan-change": strings.TrimSpace(change.Change), "dir": "auto"}}, ui.Text(strings.ReplaceAll(text, "{step}", name))))
	}
	if len(items) == 0 {
		return nil
	}
	return html.Div(html.Props{Class: "agents-plan-changes"},
		html.H4(html.Props{}, ui.Text(agentTaskDetailText(locale, "plan.changes"))),
		html.Ul(html.Props{}, items...),
		html.P(html.Props{Class: "muted"}, ui.Text(agentTaskDetailText(locale, "plan.confirm_note"))),
	)
}

// agentTaskFullGoal shows the whole request when the heading had to shorten
// it. The heading is the first line of the request, cut to fit a list row.
func agentTaskFullGoal(locale LocaleContext, task AgentTask) ui.Node {
	goal := strings.TrimSpace(task.Goal)
	if goal == "" || goal == strings.TrimSpace(task.Title) || strings.TrimSpace(task.Title) == "" {
		return nil
	}
	return html.Section(html.Props{Class: "agents-task-request-detail"},
		html.H3(html.Props{}, ui.Text(agentTaskDetailText(locale, "goal.heading"))),
		html.P(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(goal)),
	)
}
