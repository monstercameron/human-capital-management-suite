package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Help me stop or revise this request (#3980). The person who started a
// workforce-change request gets a deterministic, private reading of where the
// request stands: scheduled, still waiting for approval, already applied,
// needing repair, ended, or unknown, with when that was last updated and a
// link to the request. It reads the journey's own stage and nothing else,
// changes nothing and submits nothing. A cancellation request is never shown
// as a cancellation, and an applied change is never described as undone;
// every governed action (a cancellation request, a correction, a new proposal)
// is started on the request page, which checks access and the current state
// again first.

// stopReviseKind groups a journey stage by what it means for stopping or
// revising the change.
func stopReviseKind(stage string) string {
	switch stage {
	case "JOURNEY_STAGE_WAITING_EFFECTIVE_DATE":
		return "scheduled"
	case "JOURNEY_STAGE_PROPOSED", "JOURNEY_STAGE_BLOCKED", "JOURNEY_STAGE_AWAITING_APPROVAL", "JOURNEY_STAGE_FINANCE_APPROVAL",
		"JOURNEY_STAGE_MANAGER_APPROVAL", "JOURNEY_STAGE_REVALIDATION", "JOURNEY_STAGE_REAPPROVAL":
		return "pending"
	case "JOURNEY_STAGE_EXECUTED", "JOURNEY_STAGE_OBSERVING_EFFECTS", "JOURNEY_STAGE_AWAITING_ACKNOWLEDGEMENT",
		"JOURNEY_STAGE_RECORDED", "JOURNEY_STAGE_COMPLETED":
		return "applied"
	case "JOURNEY_STAGE_REPAIR_REQUIRED":
		return "repair"
	case "JOURNEY_STAGE_REJECTED":
		return "ended"
	case "JOURNEY_STAGE_FAILED":
		// The service uses it for any other terminal, so it does not say
		// whether any part reached the record: never claim "not applied".
		return "failed"
	}
	return "unknown"
}

// journeyStopReviseOffered: only the person who started the request is
// offered the reading, and only on a card that loaded.
func journeyStopReviseOffered(p JourneyPreview) bool {
	return p.RequestedByViewer && p.State == "ready" && p.Readable
}

type stopReviseWords struct{ label, state, path string }

var stopReviseKinds = map[string]stopReviseWords{
	"scheduled": {
		"Scheduled, not applied yet",
		"Nothing has changed on the employee record yet. The change is approved and waits for its effective date, and it is checked again before it takes effect.",
		"A cancellation request is not a cancellation: once you ask, it can be pending, accepted, too late, or in need of repair, and only the request itself says which. Open the request to see whether you can ask.",
	},
	"pending": {
		"Not applied yet, still in review",
		"Nothing has changed on the employee record. The request is still being reviewed, approved or checked, and has not been applied.",
		"A cancellation request is not a cancellation: once you ask, it can be pending, accepted, too late, or in need of repair, and only the request itself says which. Open the request to see whether you can withdraw or ask.",
	},
	"applied": {
		"Already applied",
		"This change has already taken effect on the employee record.",
		"Cancelling cannot undo it. If it is wrong, open the request to see the permitted correction or repair route.",
	},
	"repair": {
		"Needs repair",
		"The change did not reach a consistent end and needs a governed repair before it can finish.",
		"Cancelling does not fix it. Open the request to see who can start the governed repair.",
	},
	"ended": {
		"Ended without being applied",
		"This request ended and, as recorded, was not applied to the employee record.",
		"There is nothing left to stop. To try again, prepare a new proposal from the request.",
	},
	"failed": {
		"Ended, outcome not clear",
		"This request ended without finishing. From here it is not clear whether any part of it reached the employee record.",
		"Cancelling does not settle that. Open the request to see how it ended and which correction route is permitted.",
	},
	"unknown": {
		"State unknown",
		"The current state of this request could not be confirmed, so nothing here says whether it has taken effect.",
		"Open the request to see its current state before you decide anything.",
	},
}

var stopReviseKindsDE = map[string]stopReviseWords{
	"scheduled": {
		"Geplant, noch nicht umgesetzt",
		"Am Mitarbeiterdatensatz hat sich noch nichts geändert. Die Änderung ist genehmigt, wartet auf ihr Wirksamkeitsdatum und wird vor dem Inkrafttreten erneut geprüft.",
		"Eine Stornierungsanfrage ist keine Stornierung: Nach der Anfrage kann sie ausstehend, angenommen, zu spät oder reparaturbedürftig sein, und nur der Antrag selbst sagt, was zutrifft. Öffnen Sie den Antrag, um zu sehen, ob Sie anfragen können.",
	},
	"pending": {
		"Noch nicht umgesetzt, in Prüfung",
		"Am Mitarbeiterdatensatz hat sich nichts geändert. Der Antrag wird noch geprüft, genehmigt oder kontrolliert und ist nicht umgesetzt.",
		"Eine Stornierungsanfrage ist keine Stornierung: Nach der Anfrage kann sie ausstehend, angenommen, zu spät oder reparaturbedürftig sein, und nur der Antrag selbst sagt, was zutrifft. Öffnen Sie den Antrag, um zu sehen, ob Sie zurückziehen oder anfragen können.",
	},
	"applied": {
		"Bereits umgesetzt",
		"Diese Änderung ist bereits im Mitarbeiterdatensatz wirksam geworden.",
		"Eine Stornierung kann sie nicht rückgängig machen. Ist sie falsch, öffnen Sie den Antrag, um den zulässigen Korrektur- oder Reparaturweg zu sehen.",
	},
	"repair": {
		"Reparatur nötig",
		"Die Änderung hat keinen konsistenten Abschluss erreicht und braucht eine geregelte Reparatur, bevor sie enden kann.",
		"Eine Stornierung behebt das nicht. Öffnen Sie den Antrag, um zu sehen, wer die geregelte Reparatur starten kann.",
	},
	"ended": {
		"Beendet, ohne umgesetzt zu werden",
		"Dieser Antrag wurde beendet und laut Protokoll nicht im Mitarbeiterdatensatz umgesetzt.",
		"Es gibt nichts mehr zu stoppen. Um es erneut zu versuchen, erstellen Sie aus dem Antrag einen neuen Vorschlag.",
	},
	"failed": {
		"Beendet, Ergebnis unklar",
		"Dieser Antrag wurde beendet, ohne abgeschlossen zu sein. Von hier aus ist nicht klar, ob ein Teil davon den Mitarbeiterdatensatz erreicht hat.",
		"Eine Stornierung klärt das nicht. Öffnen Sie den Antrag, um zu sehen, wie er endete und welcher Korrekturweg zulässig ist.",
	},
	"unknown": {
		"Status unbekannt",
		"Der aktuelle Status dieses Antrags konnte nicht bestätigt werden; hier steht daher nicht, ob er wirksam geworden ist.",
		"Öffnen Sie den Antrag, um den aktuellen Status zu sehen, bevor Sie etwas entscheiden.",
	},
}

var stopReviseKindsAR = map[string]stopReviseWords{
	"scheduled": {
		"مجدول ولم يُطبَّق بعد",
		"لم يتغير شيء في سجل الموظف بعد. التغيير معتمد وينتظر تاريخ سريانه، ويُفحص مجددًا قبل أن يسري.",
		"طلب الإلغاء ليس إلغاءً: بعد تقديمه قد يكون معلقًا أو مقبولًا أو متأخرًا أو بحاجة إلى إصلاح، والطلب نفسه وحده يبيّن ذلك. افتح الطلب لترى هل يمكنك تقديمه.",
	},
	"pending": {
		"لم يُطبَّق بعد وما زال قيد المراجعة",
		"لم يتغير شيء في سجل الموظف. لا يزال الطلب قيد المراجعة أو الاعتماد أو الفحص ولم يُطبَّق.",
		"طلب الإلغاء ليس إلغاءً: بعد تقديمه قد يكون معلقًا أو مقبولًا أو متأخرًا أو بحاجة إلى إصلاح، والطلب نفسه وحده يبيّن ذلك. افتح الطلب لترى هل يمكنك سحبه أو تقديم طلب.",
	},
	"applied": {
		"طُبّق بالفعل",
		"سرى هذا التغيير بالفعل في سجل الموظف.",
		"لا يمكن للإلغاء التراجع عنه. إن كان خاطئًا فافتح الطلب لترى مسار التصحيح أو الإصلاح المسموح به.",
	},
	"repair": {
		"يحتاج إلى إصلاح",
		"لم يصل التغيير إلى نهاية متسقة ويحتاج إلى إصلاح خاضع للضوابط قبل أن يكتمل.",
		"الإلغاء لا يصلحه. افتح الطلب لترى من يمكنه بدء الإصلاح الخاضع للضوابط.",
	},
	"ended": {
		"انتهى دون أن يُطبَّق",
		"انتهى هذا الطلب ولم يُطبَّق على سجل الموظف بحسب السجل.",
		"لا يوجد ما يمكن إيقافه. للمحاولة مجددًا، أعدّ مقترحًا جديدًا من الطلب.",
	},
	"failed": {
		"انتهى والنتيجة غير واضحة",
		"انتهى هذا الطلب دون أن يكتمل. ولا يتضح من هنا هل وصل جزء منه إلى سجل الموظف.",
		"الإلغاء لا يحسم ذلك. افتح الطلب لترى كيف انتهى وما مسار التصحيح المسموح به.",
	},
	"unknown": {
		"الحالة غير معروفة",
		"تعذّر تأكيد الحالة الحالية لهذا الطلب، لذا لا يذكر هذا ما إذا كان قد سرى.",
		"افتح الطلب لترى حالته الحالية قبل أن تقرر أي شيء.",
	},
}

var stopReviseWordsCommon = map[string][3]string{
	"summary":   {"Help me stop or revise this request", "Hilfe: Antrag stoppen oder überarbeiten", "ساعدني في إيقاف هذا الطلب أو تعديله"},
	"private":   {"Only you see this. Nothing is cancelled, changed or sent from here.", "Nur Sie sehen das. Von hier aus wird nichts storniert, geändert oder gesendet.", "أنت وحدك ترى هذا. لا يتم إلغاء أو تغيير أو إرسال أي شيء من هنا."},
	"confirmed": {"Last updated {when}", "Zuletzt aktualisiert: {when}", "آخر تحديث: {when}"},
	"noconfirm": {"When this was last updated was not reported.", "Wann dies zuletzt aktualisiert wurde, wurde nicht gemeldet.", "لم يُذكر وقت آخر تحديث لهذا."},
	"effective": {"Effective date on the request: {date}", "Wirksamkeitsdatum im Antrag: {date}", "تاريخ السريان في الطلب: {date}"},
	"revise":    {"To use a different date or manager, prepare a new proposal from the request. The original stays on record, and the new proposal is checked and approved again; earlier approvals do not carry over.", "Für ein anderes Datum oder eine andere Führungskraft erstellen Sie aus dem Antrag einen neuen Vorschlag. Das Original bleibt erhalten, und der neue Vorschlag wird erneut geprüft und genehmigt; frühere Genehmigungen gelten nicht weiter.", "لاستخدام تاريخ أو مدير مختلف، أعدّ مقترحًا جديدًا من الطلب. يبقى الأصل في السجل، ويُفحص المقترح الجديد ويُعتمد من جديد؛ ولا تنتقل الموافقات السابقة."},
	"recheck":   {"When you go ahead, the request checks your access and its current state again. If something changed, you will be asked to review it again first.", "Wenn Sie fortfahren, prüft der Antrag Ihren Zugriff und seinen aktuellen Status erneut. Hat sich etwas geändert, werden Sie zuerst um eine neue Prüfung gebeten.", "عند المتابعة يفحص الطلب صلاحيتك وحالته الحالية مجددًا. وإن تغيّر شيء فسيُطلب منك مراجعته من جديد أولًا."},
	"open":      {"Open the request for {name}", "Antrag für {name} öffnen", "افتح الطلب الخاص بـ {name}"},
}

func stopReviseKindWords(m Model, kind string) stopReviseWords {
	table := stopReviseKinds
	switch chatbug039LocaleIndex(m.Locale) {
	case 1:
		table = stopReviseKindsDE
	case 2:
		table = stopReviseKindsAR
	}
	if w, ok := table[kind]; ok && w.label != "" {
		return w
	}
	return stopReviseKinds[kind]
}

func stopReviseText(m Model, key string, vars map[string]string) string {
	words := stopReviseWordsCommon[key]
	text := chatbug039Text(key, words[chatbug039LocaleIndex(m.Locale)], words[0])
	for k, v := range vars {
		text = strings.ReplaceAll(text, "{"+k+"}", v)
	}
	return text
}

func stopReviseConfirmed(m Model, p JourneyPreview) string {
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(p.Confirmed))
	if err != nil || at.IsZero() || at.Year() < 2000 {
		return stopReviseText(m, "noconfirm", nil)
	}
	at = at.UTC()
	return stopReviseText(m, "confirmed", map[string]string{"when": formatShortDate(m.Locale, at) + ", " + localize.FormatClock(m.Locale, at) + " UTC"})
}

// journeyStopRevisePanel is the private, deterministic disclosure, or nil when
// the viewer is not offered it.
func journeyStopRevisePanel(m Model, p JourneyPreview) ui.Node {
	if !journeyStopReviseOffered(p) {
		return nil
	}
	kind := stopReviseKind(p.Stage)
	words := stopReviseKindWords(m, kind)
	body := []ui.Node{
		html.P(html.Props{Class: "chat-journey-resolve-note", Text: stopReviseText(m, "private", nil)}),
		html.P(html.Props{Class: "chat-journey-stop-state", Dir: "auto", Data: map[string]string{"state": kind}, Text: words.label}),
		html.P(html.Props{Class: "chat-journey-resolve-note", Dir: "auto", Text: words.state}),
	}
	if effective, err := time.Parse("2006-01-02", p.Effective); err == nil && kind != "unknown" {
		body = append(body, html.P(html.Props{Class: "chat-journey-resolve-note", Dir: "auto", Text: stopReviseText(m, "effective", map[string]string{"date": formatShortDate(m.Locale, effective)})}))
	}
	body = append(body,
		html.P(html.Props{Class: "chat-journey-resolve-note", Dir: "auto", Role: "status", Text: stopReviseConfirmed(m, p)}),
		html.P(html.Props{Class: "chat-journey-resolve-note", Dir: "auto", Text: words.path}))
	if kind == "scheduled" || kind == "pending" {
		body = append(body, html.P(html.Props{Class: "chat-journey-resolve-note", Dir: "auto", Text: stopReviseText(m, "revise", nil)}))
	}
	body = append(body,
		html.P(html.Props{Class: "chat-journey-resolve-note", Dir: "auto", Text: stopReviseText(m, "recheck", nil)}),
		html.A(html.Props{Class: "button secondary small chat-journey-resolve-open", Href: JourneyReferenceURL(p.IntentID), Data: map[string]string{"action": "open-journey-reference", "id": p.IntentID}, Dir: "auto", Text: stopReviseText(m, "open", map[string]string{"name": p.Worker})}))
	return html.Details(html.Props{Class: "chat-journey-resolve chat-journey-stop-revise", Data: map[string]string{"journey-stop-revise": p.IntentID}},
		html.Summary(html.Props{Class: "chat-journey-resolve-summary", Text: stopReviseText(m, "summary", nil)}),
		html.Div(html.Props{Class: "chat-journey-resolve-body"}, body...))
}

const journeyStopReviseStyles = `.chat-journey-stop-state{margin:0;font-weight:600}`
