package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PersonaProgressProps is the invoker-only, ephemeral progress projection for
// a persona invocation. The caller owns authorization and supplies the text.
type PersonaProgressProps struct {
	InvocationID string
	ViewerID     string
	InvokerID    string
	AgentName    string
	Activity     string
	Announcement string
	CurrentStep  int
	TotalSteps   int
	Visible      bool
	ResultReady  bool
}

// PersonaTaskCardProps is the server-owned task summary shown after a persona
// invocation is handed to the task view. OpenTaskHref is a stable, authorized
// link supplied by the caller; this package never constructs one.
type PersonaTaskCardProps struct {
	ID               string
	Title            string
	Goal             string
	State            string
	Revision         string
	OpenTaskHref     string
	AwaitingApproval bool
}

// PersonaProgressFailure is a typed provider failure. Retry is rendered only
// when the caller explicitly marks the operation retryable.
type PersonaProgressFailure struct {
	Code         string
	InvocationID string
	ViewerID     string
	InvokerID    string
	Heading      string
	Message      string
	RetryLabel   string
	Retryable    bool
}

// PersonaProgressProjection contains only already-authorized chat data.
type PersonaProgressProjection struct {
	InvocationID     string
	ViewerID         string
	InvokerID        string
	Progress         *PersonaProgressProps
	Task             *PersonaTaskCardProps
	Failure          *PersonaProgressFailure
	PrivateReplyHref string
}

// RenderPersonaProgress renders invoker-only ephemeral progress and one
// updating task card. A non-invoker receives no progress, task, or failure.
func RenderPersonaProgress(model Model, projection PersonaProgressProjection) ui.Node {
	if strings.TrimSpace(projection.ViewerID) == "" || strings.TrimSpace(projection.InvokerID) == "" {
		return html.Div(html.Props{Class: "persona-progress-empty", Hidden: true})
	}
	children := make([]ui.Node, 0, 3)
	if projection.ViewerID == projection.InvokerID {
		if projection.Failure == nil && projection.Progress != nil && projection.Progress.Visible && !projection.Progress.ResultReady && projection.Progress.InvokerID == projection.InvokerID {
			children = append(children, renderPersonaProgressStatus(model, *projection.Progress))
		}
		if projection.Task != nil && strings.TrimSpace(projection.Task.ID) != "" && model.RenderPersonaTask != nil {
			children = append(children, model.RenderPersonaTask(*projection.Task))
		}
		if projection.Failure != nil && projection.Failure.InvokerID == projection.InvokerID {
			children = append(children, renderPersonaProgressFailure(model, *projection.Failure))
		}
		if projection.PrivateReplyHref != "" {
			children = append(children, html.A(html.Props{Class: "persona-private-result", Href: projection.PrivateReplyHref, Text: personaProgressText(model, "chat.persona.open_private_reply", "Open private reply")}))
		}
	}
	if len(children) == 0 {
		return html.Div(html.Props{Class: "persona-progress-empty", Hidden: true})
	}
	direction := "ltr"
	if strings.HasPrefix(strings.ToLower(model.Locale), "ar") {
		direction = "rtl"
	}
	return html.Section(html.Props{Class: "persona-progress-surface", Role: "region", Dir: direction, Aria: map[string]string{"label": personaProgressText(model, "chat.persona.progress_region", "Persona progress")}}, children...)
}

func renderPersonaProgressStatus(model Model, progress PersonaProgressProps) ui.Node {
	agent := strings.TrimSpace(progress.AgentName)
	if agent == "" {
		agent = personaProgressText(model, "chat.persona.agent", "Persona")
	}
	activity := strings.TrimSpace(progress.Activity)
	activity = personaActivityLabel(model.Locale, activity)
	if activity == "" {
		activity = personaProgressText(model, "chat.persona.working", "Working")
	}
	announcement := strings.TrimSpace(progress.Announcement)
	if announcement == "" {
		announcement = agent + ": " + activity
		if progress.CurrentStep > 0 && progress.TotalSteps >= progress.CurrentStep {
			announcement += " (" + strconv.Itoa(progress.CurrentStep) + "/" + strconv.Itoa(progress.TotalSteps) + ")"
		}
	}
	return html.Div(html.Props{Class: "persona-progress-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}, Data: map[string]string{"agent-progress": "true", "agent-invocation-id": progress.InvocationID, "reduced-motion": "respect"}}, ui.Text(announcement))
}

func renderPersonaProgressFailure(model Model, failure PersonaProgressFailure) ui.Node {
	heading := strings.TrimSpace(failure.Heading)
	if heading == "" {
		heading = personaProgressText(model, "chat.persona.provider_failed", "Provider unavailable")
		if failure.Code != "" && failure.Code != "MODEL_UNAVAILABLE" && failure.Code != "PROVIDER_UNAVAILABLE" && failure.Code != "MODEL_TIMEOUT" {
			heading = personaActivityLabel(model.Locale, "Request unavailable")
		}
	}
	children := []ui.Node{html.Strong(html.Props{}, ui.Text(heading))}
	if strings.TrimSpace(failure.Message) != "" {
		children = append(children, html.P(html.Props{}, ui.Text(personaActivityLabel(model.Locale, failure.Message))))
	}
	if failure.Retryable {
		label := strings.TrimSpace(failure.RetryLabel)
		if label == "" {
			label = personaProgressText(model, "chat.persona.retry", "Retry")
		}
		children = append(children, html.Button(html.Props{Class: "button secondary persona-progress-retry", Type: "button", Aria: map[string]string{"label": label}, Data: map[string]string{"agent-action": "retry", "agent-invocation-id": failure.InvocationID}}, ui.Text(label)))
	}
	return html.Div(html.Props{Class: "persona-progress-failure", Role: "alert", Aria: map[string]string{"live": "assertive", "atomic": "true"}, Data: map[string]string{"agent-failure": "typed", "agent-failure-code": failure.Code}}, children...)
}

func personaProgressText(model Model, key, fallback string) string {
	if model.Text != nil {
		if value := strings.TrimSpace(model.Text(key)); value != "" && value != key {
			return value
		}
	}
	if key == "chat.persona.open_private_reply" {
		return personaActivityLabel(model.Locale, "Open private reply")
	}
	locale := strings.ToLower(strings.TrimSpace(model.Locale))
	switch {
	case strings.HasPrefix(locale, "de"):
		return map[string]string{"chat.persona.progress_region": "Persona-Fortschritt", "chat.persona.agent": "Persona", "chat.persona.working": "Wird bearbeitet", "chat.persona.awaiting_approval": "Wartet auf Genehmigung", "chat.persona.open_task": "Aufgabe öffnen", "chat.persona.task": "Aufgabe", "chat.persona.provider_failed": "Anbieter nicht verfügbar", "chat.persona.retry": "Erneut versuchen"}[key]
	case strings.HasPrefix(locale, "ar"):
		return map[string]string{"chat.persona.progress_region": "تقدم الشخصية", "chat.persona.agent": "الشخصية", "chat.persona.working": "جارٍ العمل", "chat.persona.awaiting_approval": "بانتظار الموافقة", "chat.persona.open_task": "فتح المهمة", "chat.persona.task": "مهمة", "chat.persona.provider_failed": "المزوّد غير متاح", "chat.persona.retry": "إعادة المحاولة"}[key]
	default:
		return fallback
	}
}

// PersonaProgressStyles keeps live updates calm for people who request less
// motion. The host may append it alongside the chat stylesheet.
const PersonaProgressStyles = `.persona-progress-status,.persona-task-card,.persona-progress-failure{margin:8px 16px;padding:10px 12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);overflow-wrap:anywhere}.persona-progress-status{background:var(--soft)}.persona-task-state,.persona-task-approval{display:inline-block;margin-inline-start:8px;font-size:.8125rem;color:var(--muted)}.persona-task-goal{margin:6px 0}.persona-task-open{display:inline-block;margin-top:6px;color:var(--accent)}.persona-progress-retry{margin-top:8px}@media(prefers-reduced-motion:reduce){.persona-progress-surface *{animation:none!important;transition:none!important;scroll-behavior:auto!important}}`
