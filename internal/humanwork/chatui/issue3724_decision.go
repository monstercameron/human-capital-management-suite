package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Decide from the conversation (#3724). The routed approver of a workflow
// request reviews the exact proposal the card holds and approves or rejects
// it without leaving the conversation. The card is an entry point to that
// review, not evidence by itself: the decision is sent with the proposal
// revision the approver read, and the owning journey service refuses it when
// the proposal has changed since. Everyone else sees why they cannot decide
// and who can. Nothing here widens access: the Approve and Reject controls
// exist only for the viewer the resolver named as the routed approver (#3994),
// and the service checks the route again before anything is recorded.

// JourneyDecisionState is the transient client state of one card's review: is
// the review open, the reason the approver has typed (kept when a decision
// fails), a decision in flight ("approve" or "reject"), the last refusal
// ("reason", "stale", "denied" or "unavailable") and the receipt of a
// decision made here ("approved" or "rejected").
type JourneyDecisionState struct {
	Open    bool
	Reason  string
	Busy    string
	Error   string
	Receipt string
}

const (
	journeyDecisionToggleAction  = "journey-decision-toggle"
	journeyDecisionApproveAction = "journey-decide-approve"
	journeyDecisionRejectAction  = "journey-decide-reject"
	journeyDecisionDraftAction   = "journey-decision-draft"
)

// journeyDecisionMayDecide is true only for the routed approver, on a card
// that loaded, in a stage that asks for an approval.
func journeyDecisionMayDecide(m Model, p JourneyPreview) bool {
	return p.State == "ready" && p.Readable && p.StageTone != "done" && journeyStageAwaitsApprover(p.Stage) && p.WaitsOnViewer
}

// journeyDecisionAct runs one of the review's actions for the card id and
// reports whether it handled the action. It reaches a callback only for the
// routed approver, and never sends a second decision while one is in flight.
func journeyDecisionAct(m Model, action, id string) bool {
	switch action {
	case journeyDecisionToggleAction, journeyDecisionApproveAction, journeyDecisionRejectAction:
	default:
		return false
	}
	p, ok := m.JourneyPreviews[id]
	if !ok || id == "" {
		return true
	}
	state := m.JourneyDecisions[id]
	if !journeyDecisionMayDecide(m, p) {
		return true
	}
	switch action {
	case journeyDecisionToggleAction:
		if m.Callbacks.ToggleJourneyDecision != nil {
			m.Callbacks.ToggleJourneyDecision(id, !state.Open)
		}
	default:
		// A decision needs the revision the approver reviewed and a card that
		// is not already sending one.
		if strings.TrimSpace(p.Revision) == "" || state.Busy != "" || m.Callbacks.DecideJourney == nil {
			return true
		}
		m.Callbacks.DecideJourney(id, action == journeyDecisionApproveAction)
	}
	return true
}

// journeyDecisionDraftInput records the reason typed in a review's field.
func journeyDecisionDraftInput(m Model, e ui.Event, value string) bool {
	action, id, _ := eventAction(e)
	if action != journeyDecisionDraftAction || id == "" {
		return false
	}
	if m.Callbacks.SetJourneyDecisionDraft != nil {
		m.Callbacks.SetJourneyDecisionDraft(id, value)
	}
	return true
}

var journeyDecisionWords = map[string][3]string{
	"open":             {"Review and decide", "Prüfen und entscheiden", "راجِع وقرّر"},
	"close":            {"Close review", "Prüfung schließen", "أغلق المراجعة"},
	"title":            {"Review before you decide", "Vor der Entscheidung prüfen", "راجع قبل أن تقرر"},
	"person":           {"Person", "Person", "الشخص"},
	"change":           {"Change", "Änderung", "التغيير"},
	"when":             {"Effective date", "Wirksam ab", "تاريخ السريان"},
	"routed":           {"Routed approver", "Zuständig für die Genehmigung", "المعتمد المحدد"},
	"rev":              {"Revision {rev}", "Revision {rev}", "المراجعة {rev}"},
	"scope":            {"Your decision covers this approval step only. Later steps are decided by their own approvers.", "Ihre Entscheidung gilt nur für diesen Genehmigungsschritt. Spätere Schritte entscheiden die jeweils Zuständigen.", "يقتصر قرارك على خطوة الاعتماد هذه. تُحسم الخطوات اللاحقة بواسطة معتمديها."},
	"guard":            {"If the proposal changes while you review, your decision is refused and you see the current proposal.", "Ändert sich der Vorschlag während Ihrer Prüfung, wird Ihre Entscheidung abgelehnt und Sie sehen den aktuellen Vorschlag.", "إذا تغيّر المقترح أثناء مراجعتك، يُرفض قرارك وترى المقترح الحالي."},
	"limits":           {"Pay figures and check details are not shown in chat. Open the request to see everything you are cleared to see.", "Gehaltsangaben und Prüfdetails werden im Chat nicht angezeigt. Öffnen Sie den Antrag, um alles zu sehen, wofür Sie berechtigt sind.", "لا تُعرض أرقام الراتب وتفاصيل الفحوصات في الدردشة. افتح الطلب لترى كل ما يحق لك رؤيته."},
	"reason":           {"Reason (required to reject)", "Begründung (zum Ablehnen erforderlich)", "السبب (مطلوب عند الرفض)"},
	"approve":          {"Approve", "Genehmigen", "اعتماد"},
	"reject":           {"Reject", "Ablehnen", "رفض"},
	"busy":             {"Recording your decision…", "Ihre Entscheidung wird erfasst …", "جارٍ تسجيل قرارك…"},
	"norev":            {"The version of this request you are reviewing could not be confirmed, so it cannot be decided here. Open the request to decide.", "Die Version dieses Antrags, die Sie prüfen, konnte nicht bestätigt werden; er kann hier nicht entschieden werden. Öffnen Sie den Antrag, um zu entscheiden.", "تعذّر تأكيد نسخة هذا الطلب التي تراجعها، لذا لا يمكن البتّ فيه هنا. افتح الطلب لتقرر."},
	"open_request":     {"Open the request for {name}", "Antrag für {name} öffnen", "افتح الطلب الخاص بـ {name}"},
	"err_reason":       {"Give a reason to reject this step. Your text is kept.", "Geben Sie eine Begründung für die Ablehnung an. Ihr Text bleibt erhalten.", "اكتب سببًا لرفض هذه الخطوة. نصّك محفوظ."},
	"err_stale":        {"This request changed after you opened it. Nothing was decided. The card now shows the current proposal; review it and decide again. Your reason is kept.", "Dieser Antrag hat sich geändert, nachdem Sie ihn geöffnet haben. Es wurde nichts entschieden. Die Karte zeigt jetzt den aktuellen Vorschlag; prüfen Sie ihn und entscheiden Sie erneut. Ihre Begründung bleibt erhalten.", "تغيّر هذا الطلب بعد أن فتحته. لم يُتخذ أي قرار. تعرض البطاقة الآن المقترح الحالي؛ راجعه وقرّر مجددًا. سببك محفوظ."},
	"err_denied":       {"You are not allowed to decide this step. Nothing was changed.", "Sie dürfen diesen Schritt nicht entscheiden. Es wurde nichts geändert.", "لا يحق لك البتّ في هذه الخطوة. لم يتغير شيء."},
	"err_unavailable":  {"Your decision could not be recorded. Nothing was changed. Try again; your reason is kept.", "Ihre Entscheidung konnte nicht erfasst werden. Es wurde nichts geändert. Versuchen Sie es erneut; Ihre Begründung bleibt erhalten.", "تعذّر تسجيل قرارك. لم يتغير شيء. حاول مرة أخرى؛ سببك محفوظ."},
	"receipt_approved": {"You approved this step. The request moves on as its workflow decides; the card shows where it stands now.", "Sie haben diesen Schritt genehmigt. Der Antrag läuft weiter, wie es sein Workflow vorsieht; die Karte zeigt den aktuellen Stand.", "لقد اعتمدت هذه الخطوة. يتابع الطلب مساره كما يحدده سير عمله؛ تعرض البطاقة وضعه الآن."},
	"receipt_rejected": {"You rejected this step. The request moves on as its workflow decides; the card shows where it stands now.", "Sie haben diesen Schritt abgelehnt. Der Antrag läuft weiter, wie es sein Workflow vorsieht; die Karte zeigt den aktuellen Stand.", "لقد رفضت هذه الخطوة. يتابع الطلب مساره كما يحدده سير عمله؛ تعرض البطاقة وضعه الآن."},
	"waiting_named":    {"Waiting on {name}. Only the routed approver can decide this step.", "Wartet auf {name}. Nur die zuständige Person kann diesen Schritt entscheiden.", "بانتظار {name}. المعتمد المحدد وحده يمكنه البتّ في هذه الخطوة."},
	"waiting_unnamed":  {"Waiting on an approver. Only the routed approver can decide this step.", "Wartet auf eine genehmigende Person. Nur die zuständige Person kann diesen Schritt entscheiden.", "بانتظار معتمد. المعتمد المحدد وحده يمكنه البتّ في هذه الخطوة."},
}

func journeyDecisionText(m Model, key string, vars map[string]string) string {
	words := journeyDecisionWords[key]
	text := chatbug039Text(key, words[chatbug039LocaleIndex(m.Locale)], words[0])
	for k, v := range vars {
		text = strings.ReplaceAll(text, "{"+k+"}", v)
	}
	return text
}

// journeyDecisionPanel is the review panel beside a journey card, or nil when
// there is nothing to say for this viewer.
func journeyDecisionPanel(m Model, p JourneyPreview) ui.Node {
	if p.State != "ready" || !p.Readable {
		return nil
	}
	state := m.JourneyDecisions[p.IntentID]
	switch {
	case journeyDecisionMayDecide(m, p) && m.Callbacks.ToggleJourneyDecision != nil && m.Callbacks.DecideJourney != nil:
		return journeyDecisionReview(m, p, state)
	case state.Receipt != "":
		return html.Div(html.Props{Class: "chat-journey-decision"}, journeyDecisionReceipt(m, state))
	}
	return nil
}

// journeyDecisionNote is the line inside the card that says why a viewer who
// is not the routed approver cannot decide, and who can. It is plain text
// within the card's own link, so the card stays one link (#3777).
func journeyDecisionNote(m Model, p JourneyPreview) ui.Node {
	if p.State != "ready" || !p.Readable || p.WaitsOnViewer || p.StageTone == "done" || !journeyStageAwaitsApprover(p.Stage) {
		return nil
	}
	key, vars := "waiting_unnamed", map[string]string(nil)
	if name := strings.TrimSpace(p.Approver); name != "" {
		key, vars = "waiting_named", map[string]string{"name": name}
	}
	return html.Span(html.Props{Class: "chat-journey-waiting-note", Dir: "auto", Text: journeyDecisionText(m, key, vars)})
}

func journeyDecisionReceipt(m Model, state JourneyDecisionState) ui.Node {
	key := "receipt_approved"
	if state.Receipt == "rejected" {
		key = "receipt_rejected"
	}
	return html.P(html.Props{Class: "chat-journey-decision-note chat-journey-decision-receipt", Role: "status", Text: journeyDecisionText(m, key, nil)})
}

func journeyDecisionFact(m Model, label, value string) []ui.Node {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []ui.Node{html.Span(html.Props{Class: "chat-journey-decision-key", Text: journeyDecisionText(m, label, nil)}), html.Span(html.Props{Class: "chat-journey-decision-value", Dir: "auto", Text: value})}
}

func journeyDecisionReview(m Model, p JourneyPreview, state JourneyDecisionState) ui.Node {
	id := p.IntentID
	panelID := "journey-decision-" + id
	toggleKey := "open"
	if state.Open {
		toggleKey = "close"
	}
	toggle := html.Button(html.Props{Class: "button secondary small chat-journey-decision-toggle", Type: "button",
		Data: map[string]string{"action": journeyDecisionToggleAction, "id": id},
		Aria: map[string]string{"expanded": boolString(state.Open), "controls": panelID}, Text: journeyDecisionText(m, toggleKey, nil)})
	if state.Receipt != "" && !state.Open {
		return html.Div(html.Props{Class: "chat-journey-decision"}, toggle, journeyDecisionReceipt(m, state))
	}
	if !state.Open {
		return html.Div(html.Props{Class: "chat-journey-decision"}, toggle)
	}

	move := strings.TrimSpace(p.From)
	if to := strings.TrimSpace(p.To); to != "" {
		if move != "" {
			move += " → "
		}
		move += to
	}
	facts := journeyDecisionFact(m, "person", p.Worker)
	facts = append(facts, journeyDecisionFact(m, "change", move)...)
	facts = append(facts, journeyDecisionFact(m, "when", journeyDecisionDate(m, p))...)
	facts = append(facts, journeyDecisionFact(m, "routed", p.Approver)...)
	rev := strings.TrimSpace(p.Revision)

	body := []ui.Node{
		html.Strong(html.Props{ID: panelID + "-title", Class: "chat-journey-decision-title", Text: journeyDecisionText(m, "title", nil)}),
		html.Div(html.Props{Class: "chat-journey-decision-facts"}, facts...),
	}
	if rev != "" {
		body = append(body, html.P(html.Props{Class: "chat-journey-decision-note", Dir: "auto", Text: journeyDecisionText(m, "rev", map[string]string{"rev": rev})}))
	}
	body = append(body,
		html.P(html.Props{Class: "chat-journey-decision-note", Text: journeyDecisionText(m, "scope", nil)}),
		html.P(html.Props{Class: "chat-journey-decision-note", Text: journeyDecisionText(m, "guard", nil)}),
		html.P(html.Props{Class: "chat-journey-decision-note", Text: journeyDecisionText(m, "limits", nil)}),
	)

	if state.Receipt != "" {
		body = append(body, journeyDecisionReceipt(m, state))
	} else {
		busy := state.Busy != ""
		locked := busy || rev == ""
		fieldID := "journey-decision-reason-" + id
		describedBy := panelID + "-hint"
		fieldAria := map[string]string{"describedby": describedBy}
		if state.Error == "reason" {
			fieldAria["invalid"] = "true"
			fieldAria["describedby"] = describedBy + " " + panelID + "-error"
		}
		body = append(body,
			html.Label(html.Props{For: fieldID, Class: "chat-journey-decision-label", Text: journeyDecisionText(m, "reason", nil)}),
			html.Textarea(html.Props{ID: fieldID, Class: "chat-input chat-journey-decision-reason", Rows: 3, MaxLength: 1000, Dir: "auto",
				Data: map[string]string{"chat-value": state.Reason, "action": journeyDecisionDraftAction, "id": id},
				Aria: fieldAria}),
			html.Span(html.Props{ID: describedBy, Class: "sr-only", Text: journeyDecisionText(m, "limits", nil)}),
		)
		if rev == "" {
			body = append(body, html.P(html.Props{Class: "chat-journey-decision-note", Role: "status", Text: journeyDecisionText(m, "norev", nil)}))
		}
		if state.Error != "" {
			body = append(body, html.P(html.Props{ID: panelID + "-error", Class: "chat-journey-decision-error", Role: "alert", Dir: "auto", Text: journeyDecisionText(m, "err_"+state.Error, nil)}))
		}
		button := func(action, key, class string) ui.Node {
			props := html.Props{Class: class, Type: "button", Data: map[string]string{"action": action, "id": id}, Text: journeyDecisionText(m, key, nil)}
			if locked {
				props.Aria = map[string]string{"disabled": "true"}
			}
			return html.Button(props)
		}
		actions := []ui.Node{
			button(journeyDecisionApproveAction, "approve", "button primary small chat-journey-decision-approve"),
			button(journeyDecisionRejectAction, "reject", "button secondary small chat-journey-decision-reject"),
		}
		if busy {
			actions = append(actions, html.Span(html.Props{Class: "chat-journey-decision-busy", Role: "status", Text: journeyDecisionText(m, "busy", nil)}))
		}
		body = append(body, html.Div(html.Props{Class: "chat-journey-decision-actions"}, actions...))
	}
	body = append(body, html.A(html.Props{Class: "button secondary small chat-journey-resolve-open", Href: JourneyReferenceURL(id),
		Data: map[string]string{"action": "open-journey-reference", "id": id}, Dir: "auto",
		Text: journeyDecisionText(m, "open_request", map[string]string{"name": p.Worker})}))

	panelProps := html.Props{ID: panelID, Class: "chat-journey-decision-panel", Role: "group", Aria: map[string]string{"labelledby": panelID + "-title"}}
	if state.Busy != "" {
		panelProps.Aria["busy"] = "true"
	}
	return html.Div(html.Props{Class: "chat-journey-decision"}, toggle, html.Div(panelProps, body...))
}

func journeyDecisionDate(m Model, p JourneyPreview) string {
	effective, err := time.Parse("2006-01-02", p.Effective)
	if err != nil {
		return ""
	}
	return formatShortDate(m.Locale, effective)
}

// journeyDraftEmbeds is journeyPreviewEmbeds for the composer's unsent draft:
// the same cards, but never a way to decide from text that has not been sent.
func journeyDraftEmbeds(m Model, body string) []ui.Node {
	m.Callbacks.DecideJourney, m.Callbacks.ToggleJourneyDecision, m.Callbacks.SetJourneyDecisionDraft = nil, nil, nil
	m.JourneyDecisions = nil
	return journeyPreviewEmbeds(m, body)
}

const journeyDecisionStyles = `.chat-journey-decision{display:flex;flex-direction:column;gap:6px;min-width:0}` +
	`.chat-journey-decision-toggle{align-self:flex-start;min-height:44px}` +
	`.chat-journey-decision-panel{display:flex;flex-direction:column;gap:8px;min-width:0;border:1px solid var(--line);border-radius:10px;padding-block:12px;padding-inline:12px;font-size:.8125rem;overflow-wrap:anywhere}` +
	`.chat-journey-waiting-note{font-size:.8125rem;color:var(--muted);overflow-wrap:anywhere}` +
	`.chat-journey-decision-title{font-size:.875rem}` +
	`.chat-journey-decision-facts{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:2px 12px;margin:0}` +
	`.chat-journey-decision-key{color:var(--muted)}` +
	`.chat-journey-decision-value{font-variant-numeric:tabular-nums}` +
	`.chat-journey-decision-note{margin:0;color:var(--muted)}` +
	`.chat-journey-decision-label{font-weight:600}` +
	`.chat-journey-decision-reason{width:100%;min-height:44px;resize:vertical}` +
	`.chat-journey-decision-error{margin:0;color:var(--danger,#b42318);font-weight:600}` +
	`.chat-journey-decision-actions{display:flex;flex-wrap:wrap;align-items:center;gap:8px}` +
	`.chat-journey-decision-actions .button{min-height:44px;min-width:44px}` +
	`.chat-journey-decision-actions [aria-disabled="true"]{opacity:.6;cursor:not-allowed}` +
	`@media (max-width:480px){.chat-journey-decision-facts{grid-template-columns:minmax(0,1fr)}.chat-journey-decision-actions .button{flex:1 1 auto}}`
