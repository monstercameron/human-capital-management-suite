package chatui

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// PersonaProgressProps is the invoker-only, ephemeral progress projection for
// a persona invocation. The caller owns authorization and supplies the text.
type PersonaProgressProps struct {
	InvocationID   string
	ViewerID       string
	InvokerID      string
	AgentName      string
	Activity       string
	Announcement   string
	CurrentStep    int
	TotalSteps     int
	ElapsedSeconds int
	Visible        bool
	ResultReady    bool
	Deadline       time.Time
	// Provisional marks a waiting state the client drew before the server
	// reported on the question. It has no invocation to try again, so past its
	// deadline it says the answer was interrupted without offering Try again.
	Provisional bool
	// QuestionPostID is the question this state answers, so a card that ends
	// without a run behind it can offer to ask that question again (CHATUX-003).
	QuestionPostID string
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
	// AskedAt is when the question this failure answers was sent. The retry is
	// offered only while that question is recent (agentRetryWindow); zero means
	// unknown and keeps the offer.
	AskedAt time.Time
	// AskAgainPostID is the question to ask again on a click, for a failure with no
	// run to retry. Asking is only ever the person's deliberate click.
	AskAgainPostID string
}

// agentRetryWindow is how long after the question a failed answer still offers
// Try again. The invocation record carries no expiry the client can read, so
// the offer lapses with the question, after fifteen minutes.
const agentRetryWindow = 15 * time.Minute

func agentRetryExpired(askedAt, now time.Time) bool {
	return !askedAt.IsZero() && now.Sub(askedAt) > agentRetryWindow
}

// PersonaProgressProjection contains only already-authorized chat data.
type PersonaProgressProjection struct {
	InvocationID     string
	ViewerID         string
	InvokerID        string
	AgentName        string
	Progress         *PersonaProgressProps
	Task             *PersonaTaskCardProps
	Failure          *PersonaProgressFailure
	PrivateReplyHref string
	DurablePostID    string
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
			children = append(children, renderPersonaProgressFailure(model, *projection.Failure, projection.AgentName))
		}
	}
	if len(children) == 0 {
		return html.Div(html.Props{Class: "persona-progress-empty", Hidden: true})
	}
	direction := "ltr"
	if strings.HasPrefix(strings.ToLower(model.Locale), "ar") {
		direction = "rtl"
	}
	return html.Section(html.Props{Class: "persona-progress-surface", Role: "region", Dir: direction, Aria: map[string]string{"label": personaProgressText(model, "chat.agent.response_region", "Agent response")}}, children...)
}

func renderPersonaProgressStatus(model Model, progress PersonaProgressProps) ui.Node {
	agent := personaAgentName(model, progress.AgentName)
	if !progress.Deadline.IsZero() && !time.Now().Before(progress.Deadline) {
		return renderPersonaProgressFailure(model, PersonaProgressFailure{InvocationID: progress.InvocationID, ViewerID: progress.ViewerID, InvokerID: progress.InvokerID, Code: "ANSWER_INTERRUPTED", Retryable: !progress.Provisional, AskAgainPostID: progress.QuestionPostID, AskedAt: agentQuestionSentAt(model, progress.QuestionPostID)}, agent)
	}
	elapsedSeconds := max(0, progress.ElapsedSeconds)
	working := personaProgressText(model, "chat.agent.finding_answer", "Finding an answer in your policy documents…")
	if elapsedSeconds >= 20 {
		working = personaProgressText(model, "chat.agent.still_working", "Still working…")
	}
	announcement := strings.TrimSpace(progress.Announcement)
	if announcement == "" {
		announcement = working
	}
	children := []ui.Node{
		renderAgentReplyIdentity(model, agent),
		html.Div(html.Props{Class: "agent-reply-state-line", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true", "label": announcement}},
			html.Span(html.Props{Class: "agent-reply-state-copy", Text: working}),
			html.Span(html.Props{Class: "agent-working-dots", Aria: map[string]string{"hidden": "true"}}, html.Span(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{})),
			progressCounter(elapsedSeconds)),
	}
	if elapsedSeconds >= 5 {
		children[1] = html.Div(html.Props{Class: "agent-reply-state-line", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true", "label": announcement}},
			html.Span(html.Props{Class: "agent-reply-state-copy", Text: working}),
			html.Span(html.Props{Class: "agent-working-dots", Aria: map[string]string{"hidden": "true"}}, html.Span(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{})),
			progressCounter(elapsedSeconds),
			html.Span(html.Props{Class: "agent-reply-state-divider", Aria: map[string]string{"hidden": "true"}, Text: "·"}),
			html.Button(html.Props{Class: "agent-progress-cancel", Type: "button", Disabled: model.Callbacks.CancelPersonaInvocation == nil, Data: map[string]string{"action": "agent-invocation-cancel", "id": progress.InvocationID}, Text: agentAnswerStopLabel(model.Locale)}))
	}
	return html.Article(html.Props{Class: "persona-progress-status agent-reply-row", Data: map[string]string{"agent-progress": "true", "agent-invocation-id": progress.InvocationID, "agent-reply-state": "working", "reduced-motion": "respect"}}, children...)
}

func progressCounter(elapsedSeconds int) ui.Node {
	if elapsedSeconds < 5 {
		return nil
	}
	return html.Time(html.Props{Class: "agent-reply-counter", Text: fmt.Sprintf("%d:%02d", elapsedSeconds/60, elapsedSeconds%60)})
}

func renderPersonaProgressFailure(model Model, failure PersonaProgressFailure, agentName string) ui.Node {
	agentName = personaAgentName(model, agentName)
	description := chat.AgentAnswerFailureFor(model.Locale, agentName, failure.Code)
	// CHATBUG-035: once the retry has lapsed the sentence stops telling the
	// reader to try again, since there is nothing left to press.
	lapsed := description.Retryable && agentRetryExpired(failure.AskedAt, time.Now())
	message := description.Sentence
	if !lapsed {
		message += " " + description.NextStep
	}
	// CHATBUG-035: one icon, the warning; the privacy line is words only, and the
	// sentence is its own live region rather than a hidden copy read after it.
	children := []ui.Node{renderAgentReplyIdentity(model, agentName), html.P(html.Props{Class: "agent-failure-heading", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}}, icon("warning"), html.Span(html.Props{Text: message})), html.Div(html.Props{Class: "agent-reply-private"}, html.Span(html.Props{Text: personaProgressText(model, "chat.agent.only_visible", "Only visible to you")}))}
	if failure.Retryable && description.Retryable && !lapsed {
		label := strings.TrimSpace(failure.RetryLabel)
		if label == "" {
			label = personaProgressText(model, "chat.agent.try_again", "Try again")
		}
		children = append(children, html.Button(html.Props{Class: "button secondary persona-progress-retry", Type: "button", Aria: map[string]string{"label": label}, Data: map[string]string{"agent-action": "retry", "agent-invocation-id": failure.InvocationID}}, ui.Text(label)))
	}
	if !(failure.Retryable && description.Retryable && !lapsed) && failure.AskAgainPostID != "" && description.Retryable && !lapsed {
		children = append(children, chatux003AskAgainButton(model, failure.AskAgainPostID))
	}
	return html.Article(html.Props{Class: "persona-progress-failure agent-reply-row", Data: map[string]string{"agent-failure": "typed", "agent-reply-state": "failed"}}, children...)
}

func personaAgentName(model Model, name string) string {
	if value := strings.TrimSpace(name); value != "" {
		return value
	}
	return personaProgressText(model, "chat.agent.name", "Agent")
}

func personaProgressText(model Model, key, fallback string) string {
	return chatbug039Text(key, agentReplyFallback(model.Locale, key, fallback), fallback)
}

// PersonaProgressStyles keeps live updates calm for people who request less
// motion. The host may append it alongside the chat stylesheet.
const PersonaProgressStyles = `.persona-progress-surface{min-width:0}.agent-reply-row,.persona-task-card{margin-block:8px;padding:10px 12px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-brand-soft);overflow-wrap:anywhere;min-width:0}.agent-reply-identity{display:flex;align-items:center;gap:8px;min-width:0}.agent-reply-name{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.agent-reply-state-text,.agent-reply-answer{margin:6px 0;white-space:pre-wrap}.agent-reply-meta{display:flex;flex-wrap:wrap;align-items:center;gap:6px;color:var(--hcm-color-text-muted);font-size:.8125rem}.agent-reply-open{color:var(--hcm-color-brand-primary)}.persona-task-state,.persona-task-approval{display:inline-block;margin-inline-start:8px;font-size:.8125rem;color:var(--hcm-color-text-muted)}.persona-task-goal{margin:6px 0}.persona-task-open{display:inline-block;margin-top:6px;color:var(--hcm-color-brand-primary)}.persona-progress-retry{margin-top:8px}@media(max-width:390px){.agent-reply-row{padding:8px 10px}.agent-reply-meta{align-items:flex-start;flex-direction:column}.agent-reply-open{max-width:100%}}@media(prefers-reduced-motion:reduce){.persona-progress-surface *{animation:none!important;transition:none!important;scroll-behavior:auto!important}}`
