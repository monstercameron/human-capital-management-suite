package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3811: a new hire's welcome card. The post's body only says whom it is
// for; everything on the card (the start day, each requirement, who owns it,
// where it stands) is what the onboarding owner said when the page last asked,
// held in OnboardingCardsState. Chat never stores or decides any of it: an item
// the owner has not confirmed reads "not verified yet", and nothing here can
// mark work done. The card links to the owning page, which checks permission
// itself.

// issue3811TasksPath is the page the onboarding tasks are done on. The href is
// built from this constant, never from the post.
const issue3811TasksPath = "/workspace/app/onboarding/tasks"

// OnboardingItemView is one requirement as the owner reported it.
type OnboardingItemView struct {
	Kind        string
	Owner       string
	Status      string
	ConfirmedOn string
	Required    bool
}

// OnboardingCardView is what the server said this reader may see of one card.
type OnboardingCardView struct {
	// Loaded is false until the server has answered for this card.
	Loaded bool
	// State is "active", "closed", "cancelled", "ended" or "unavailable"; empty
	// when the server would not describe the card to this reader.
	State     string
	StartDate string
	Items     []OnboardingItemView
}

// OnboardingCardsState is the page's transient view of the welcome cards on
// screen. Never message content.
type OnboardingCardsState struct {
	Views map[string]OnboardingCardView
}

// IsOnboardingCardBody reports whether a post body is a welcome card.
func IsOnboardingCardBody(body string) bool {
	_, ok := chat.ParseOnboardingCard(body)
	return ok
}

const (
	keyIssue3811Title      = "chat.issue3811.title"
	keyIssue3811Intro      = "chat.issue3811.intro"
	keyIssue3811Start      = "chat.issue3811.start"
	keyIssue3811Loading    = "chat.issue3811.loading"
	keyIssue3811Ended      = "chat.issue3811.ended"
	keyIssue3811Cancelled  = "chat.issue3811.cancelled"
	keyIssue3811Closed     = "chat.issue3811.closed"
	keyIssue3811Unreadable = "chat.issue3811.unreadable"
	keyIssue3811Hidden     = "chat.issue3811.hidden"
	keyIssue3811Empty      = "chat.issue3811.empty"
	keyIssue3811List       = "chat.issue3811.list"
	keyIssue3811Owner      = "chat.issue3811.owner"
	keyIssue3811NoOwner    = "chat.issue3811.no_owner"
	keyIssue3811Confirmed  = "chat.issue3811.confirmed_on"
	keyIssue3811Source     = "chat.issue3811.source"
	keyIssue3811Open       = "chat.issue3811.open"
	keyIssue3811Required   = "chat.issue3811.required"
	keyIssue3811StatusKey  = "chat.issue3811.status."
	keyIssue3811NextKey    = "chat.issue3811.next."
	keyIssue3811KindKey    = "chat.issue3811.kind."
)

var issue3811Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3811Title:                          "Welcome to your first days",
		keyIssue3811Intro:                          "Here is what is needed before you start and who to ask about each item.",
		keyIssue3811Start:                          "Your plan starts {date}.",
		keyIssue3811Loading:                        "Checking your checklist...",
		keyIssue3811Ended:                          "This welcome was for an earlier onboarding and no longer applies.",
		keyIssue3811Cancelled:                      "This onboarding was cancelled. Ask People if you have questions.",
		keyIssue3811Closed:                         "Your onboarding is complete.",
		keyIssue3811Unreadable:                     "Your checklist cannot be checked right now. Try again later; nothing here means you are ready or not ready.",
		keyIssue3811Hidden:                         "This checklist is private to the person it was written for.",
		keyIssue3811Empty:                          "There is nothing on your checklist yet.",
		keyIssue3811List:                           "Your onboarding checklist",
		keyIssue3811Owner:                          "Ask: {owner}",
		keyIssue3811NoOwner:                        "No contact is named yet.",
		keyIssue3811Confirmed:                      "Confirmed on {date}",
		keyIssue3811Source:                         "This list comes from People records, not from this chat. Chat cannot confirm, approve or complete anything.",
		keyIssue3811Open:                           "Open my onboarding tasks",
		keyIssue3811Required:                       "Required",
		keyIssue3811StatusKey + "confirmed":        "Confirmed",
		keyIssue3811StatusKey + "pending":          "Still to do",
		keyIssue3811StatusKey + "attention":        "Needs attention",
		keyIssue3811StatusKey + "unverified":       "Not verified yet",
		keyIssue3811StatusKey + "optional":         "Optional",
		keyIssue3811NextKey + "pending":            "Next: finish this with the contact below.",
		keyIssue3811NextKey + "attention":          "Next: ask the contact below what is needed.",
		keyIssue3811NextKey + "unverified":         "Next: ask the contact below to check it.",
		keyIssue3811KindKey + "work_authorization": "Right to work",
		keyIssue3811KindKey + "payroll":            "Pay",
		keyIssue3811KindKey + "access":             "Access",
		keyIssue3811KindKey + "equipment":          "Equipment",
		keyIssue3811KindKey + "training":           "Training",
		keyIssue3811KindKey + "legal":              "Legal",
		keyIssue3811KindKey + "medical":            "Health",
		keyIssue3811KindKey + "identity":           "Identity",
		keyIssue3811KindKey + "task":               "Task",
		keyIssue3811KindKey + "optional":           "Optional item",
		keyIssue3811KindKey + "other":              "Item",
	},
	"de-DE": {
		keyIssue3811Title:                          "Willkommen zu Ihren ersten Tagen",
		keyIssue3811Intro:                          "Hier steht, was vor Ihrem Start nötig ist und wen Sie zu jedem Punkt fragen können.",
		keyIssue3811Start:                          "Ihr Plan beginnt am {date}.",
		keyIssue3811Loading:                        "Ihre Checkliste wird geprüft...",
		keyIssue3811Ended:                          "Dieses Willkommen gehörte zu einem früheren Onboarding und gilt nicht mehr.",
		keyIssue3811Cancelled:                      "Dieses Onboarding wurde abgesagt. Fragen Sie bei Fragen die Personalabteilung.",
		keyIssue3811Closed:                         "Ihr Onboarding ist abgeschlossen.",
		keyIssue3811Unreadable:                     "Ihre Checkliste kann gerade nicht geprüft werden. Versuchen Sie es später erneut; hier steht weder, dass Sie bereit sind, noch dass Sie es nicht sind.",
		keyIssue3811Hidden:                         "Diese Checkliste ist privat und nur für die Person bestimmt, für die sie geschrieben wurde.",
		keyIssue3811Empty:                          "Auf Ihrer Checkliste steht noch nichts.",
		keyIssue3811List:                           "Ihre Onboarding-Checkliste",
		keyIssue3811Owner:                          "Fragen Sie: {owner}",
		keyIssue3811NoOwner:                        "Es ist noch keine Kontaktperson benannt.",
		keyIssue3811Confirmed:                      "Bestätigt am {date}",
		keyIssue3811Source:                         "Diese Liste stammt aus den Personalunterlagen, nicht aus diesem Chat. Der Chat kann nichts bestätigen, genehmigen oder abschließen.",
		keyIssue3811Open:                           "Meine Onboarding-Aufgaben öffnen",
		keyIssue3811Required:                       "Erforderlich",
		keyIssue3811StatusKey + "confirmed":        "Bestätigt",
		keyIssue3811StatusKey + "pending":          "Noch offen",
		keyIssue3811StatusKey + "attention":        "Braucht Aufmerksamkeit",
		keyIssue3811StatusKey + "unverified":       "Noch nicht geprüft",
		keyIssue3811StatusKey + "optional":         "Freiwillig",
		keyIssue3811NextKey + "pending":            "Nächster Schritt: Schließen Sie dies mit der Kontaktperson unten ab.",
		keyIssue3811NextKey + "attention":          "Nächster Schritt: Fragen Sie die Kontaktperson unten, was nötig ist.",
		keyIssue3811NextKey + "unverified":         "Nächster Schritt: Bitten Sie die Kontaktperson unten um eine Prüfung.",
		keyIssue3811KindKey + "work_authorization": "Arbeitserlaubnis",
		keyIssue3811KindKey + "payroll":            "Gehalt",
		keyIssue3811KindKey + "access":             "Zugang",
		keyIssue3811KindKey + "equipment":          "Ausstattung",
		keyIssue3811KindKey + "training":           "Schulung",
		keyIssue3811KindKey + "legal":              "Rechtliches",
		keyIssue3811KindKey + "medical":            "Gesundheit",
		keyIssue3811KindKey + "identity":           "Identität",
		keyIssue3811KindKey + "task":               "Aufgabe",
		keyIssue3811KindKey + "optional":           "Optionaler Punkt",
		keyIssue3811KindKey + "other":              "Punkt",
	},
	"ar": {
		keyIssue3811Title:                          "مرحبًا بك في أيامك الأولى",
		keyIssue3811Intro:                          "هذا ما يلزم قبل أن تبدأ، ومن تسأله عن كل بند.",
		keyIssue3811Start:                          "تبدأ خطتك في {date}.",
		keyIssue3811Loading:                        "جارٍ التحقق من قائمتك...",
		keyIssue3811Ended:                          "كان هذا الترحيب لتهيئة سابقة ولم يعد ساريًا.",
		keyIssue3811Cancelled:                      "أُلغيت هذه التهيئة. اسأل الموارد البشرية إن كانت لديك أسئلة.",
		keyIssue3811Closed:                         "اكتملت تهيئتك.",
		keyIssue3811Unreadable:                     "تعذّر التحقق من قائمتك الآن. حاول لاحقًا؛ لا يعني أي شيء هنا أنك جاهز أو غير جاهز.",
		keyIssue3811Hidden:                         "هذه القائمة خاصة بالشخص الذي كُتبت له.",
		keyIssue3811Empty:                          "لا يوجد شيء في قائمتك بعد.",
		keyIssue3811List:                           "قائمة تهيئتك",
		keyIssue3811Owner:                          "اسأل: {owner}",
		keyIssue3811NoOwner:                        "لم يُحدَّد شخص للتواصل بعد.",
		keyIssue3811Confirmed:                      "تم التأكيد في {date}",
		keyIssue3811Source:                         "تأتي هذه القائمة من سجلات الموارد البشرية وليس من هذه المحادثة. لا تستطيع المحادثة تأكيد أي شيء أو اعتماده أو إكماله.",
		keyIssue3811Open:                           "فتح مهام تهيئتي",
		keyIssue3811Required:                       "مطلوب",
		keyIssue3811StatusKey + "confirmed":        "تم التأكيد",
		keyIssue3811StatusKey + "pending":          "لم يُنجز بعد",
		keyIssue3811StatusKey + "attention":        "يحتاج إلى اهتمام",
		keyIssue3811StatusKey + "unverified":       "لم يُتحقق منه بعد",
		keyIssue3811StatusKey + "optional":         "اختياري",
		keyIssue3811NextKey + "pending":            "الخطوة التالية: أنجز هذا مع جهة التواصل أدناه.",
		keyIssue3811NextKey + "attention":          "الخطوة التالية: اسأل جهة التواصل أدناه عمّا هو مطلوب.",
		keyIssue3811NextKey + "unverified":         "الخطوة التالية: اطلب من جهة التواصل أدناه التحقق منه.",
		keyIssue3811KindKey + "work_authorization": "حق العمل",
		keyIssue3811KindKey + "payroll":            "الراتب",
		keyIssue3811KindKey + "access":             "الوصول",
		keyIssue3811KindKey + "equipment":          "المعدات",
		keyIssue3811KindKey + "training":           "التدريب",
		keyIssue3811KindKey + "legal":              "قانوني",
		keyIssue3811KindKey + "medical":            "الصحة",
		keyIssue3811KindKey + "identity":           "الهوية",
		keyIssue3811KindKey + "task":               "مهمة",
		keyIssue3811KindKey + "optional":           "بند اختياري",
		keyIssue3811KindKey + "other":              "بند",
	},
}

// Issue3811Text is the copy of the welcome card in a locale.
func Issue3811Text(locale, key string) string {
	return chatbug039Text(key, issue3811Copy[chatEmojiLocale(locale)][key], issue3811Copy["en-US"][key])
}

func issue3811Kind(m Model, kind string) string {
	key := keyIssue3811KindKey + strings.ToLower(kind)
	if _, known := issue3811Copy["en-US"][key]; !known {
		key = keyIssue3811KindKey + "other"
	}
	return laneText(m, issue3811Copy, key)
}

func issue3811Status(m Model, status string) string {
	key := keyIssue3811StatusKey + status
	if _, known := issue3811Copy["en-US"][key]; !known {
		key = keyIssue3811StatusKey + "unverified"
	}
	return laneText(m, issue3811Copy, key)
}

func issue3811Day(m Model, day string) string {
	if at, err := time.Parse("2006-01-02", day); err == nil {
		return formatDay(m.Locale, at, true)
	}
	return day
}

func issue3811Item(m Model, v OnboardingCardView, item OnboardingItemView) ui.Node {
	status := item.Status
	if _, known := issue3811Copy["en-US"][keyIssue3811StatusKey+status]; !known {
		status = "unverified"
	}
	head := []ui.Node{
		html.Strong(html.Props{Class: "chat-onboarding-kind", Dir: "auto", Text: issue3811Kind(m, item.Kind)}),
		html.Span(html.Props{Class: "chat-onboarding-status chat-onboarding-status-" + status, Text: issue3811Status(m, status)}),
	}
	if item.Required {
		head = append(head, html.Span(html.Props{Class: "chat-onboarding-required", Text: laneText(m, issue3811Copy, keyIssue3811Required)}))
	}
	lines := []ui.Node{html.Div(html.Props{Class: "chat-onboarding-item-head"}, head...)}
	if status == "confirmed" && item.ConfirmedOn != "" {
		lines = append(lines, html.P(html.Props{Class: "chat-onboarding-line", Text: laneTextf(m, issue3811Copy, keyIssue3811Confirmed, map[string]string{"date": issue3811Day(m, item.ConfirmedOn)})}))
	}
	if next := keyIssue3811NextKey + status; issue3811Copy["en-US"][next] != "" {
		lines = append(lines, html.P(html.Props{Class: "chat-onboarding-line", Text: laneText(m, issue3811Copy, next)}))
	}
	if strings.TrimSpace(item.Owner) != "" {
		lines = append(lines, html.P(html.Props{Class: "chat-onboarding-line chat-onboarding-owner", Dir: "auto", Text: laneTextf(m, issue3811Copy, keyIssue3811Owner, map[string]string{"owner": item.Owner})}))
	} else if status != "confirmed" && status != "optional" {
		lines = append(lines, html.P(html.Props{Class: "chat-onboarding-line chat-onboarding-owner", Text: laneText(m, issue3811Copy, keyIssue3811NoOwner)}))
	}
	return html.Li(html.Props{Class: "chat-onboarding-item", Data: map[string]string{"status": status}}, lines...)
}

// issue3811Body is what the card says below its heading.
func issue3811Body(m Model, view OnboardingCardView) []ui.Node {
	say := func(key string, class string) []ui.Node {
		return []ui.Node{html.P(html.Props{Class: class, Role: "status", Text: laneText(m, issue3811Copy, key)})}
	}
	switch {
	case !view.Loaded:
		return say(keyIssue3811Loading, "chat-onboarding-note")
	case view.State == "ended":
		return say(keyIssue3811Ended, "chat-onboarding-note")
	case view.State == "cancelled":
		return say(keyIssue3811Cancelled, "chat-onboarding-note")
	case view.State == "closed":
		return say(keyIssue3811Closed, "chat-onboarding-note")
	case view.State == "unavailable":
		return say(keyIssue3811Unreadable, "chat-onboarding-note")
	case view.State != "active":
		return say(keyIssue3811Hidden, "chat-onboarding-note")
	}
	out := []ui.Node{html.P(html.Props{Class: "chat-onboarding-line", Text: laneText(m, issue3811Copy, keyIssue3811Intro)})}
	if view.StartDate != "" {
		out = append(out, html.P(html.Props{Class: "chat-onboarding-line", Text: laneTextf(m, issue3811Copy, keyIssue3811Start, map[string]string{"date": issue3811Day(m, view.StartDate)})}))
	}
	if len(view.Items) == 0 {
		return append(out, html.P(html.Props{Class: "chat-onboarding-note", Text: laneText(m, issue3811Copy, keyIssue3811Empty)}))
	}
	items := make([]ui.Node, 0, len(view.Items))
	for _, item := range view.Items {
		items = append(items, issue3811Item(m, view, item))
	}
	out = append(out, html.Ul(html.Props{Class: "chat-onboarding-list", Aria: map[string]string{"label": laneText(m, issue3811Copy, keyIssue3811List)}}, items...))
	out = append(out, html.A(html.Props{Class: "button primary small chat-onboarding-action", Href: issue3811TasksPath, Data: map[string]string{"action": "open-onboarding-tasks"}}, ui.Text(laneText(m, issue3811Copy, keyIssue3811Open))))
	return out
}

// issue3811OnboardingCard draws the card as its own row: a labelled region, not
// a message (no avatar, no reply or react actions). The source line says where
// the list comes from, so nobody reads it as a Chat decision.
func issue3811OnboardingCard(m Model, msg Message) (ui.Node, bool) {
	if !IsOnboardingCardBody(msg.Body) {
		return nil, false
	}
	title := laneText(m, issue3811Copy, keyIssue3811Title)
	children := []ui.Node{html.Strong(html.Props{Class: "chat-onboarding-title", Dir: "auto", Text: title})}
	children = append(children, issue3811Body(m, m.Onboarding.Views[msg.ID])...)
	children = append(children, html.P(html.Props{Class: "chat-onboarding-source", Text: laneText(m, issue3811Copy, keyIssue3811Source)}))
	if msg.TimeLabel != "" {
		children = append(children, html.Time(html.Props{Class: "message-time", Text: msg.TimeLabel}))
	}
	region := html.Section(html.Props{Class: "chat-onboarding-card", Aria: map[string]string{"label": title}, Data: map[string]string{"onboarding-card": msg.ID}}, children...)
	return html.Div(html.Props{Class: "message-row chat-onboarding-card-row", Data: map[string]string{"message-id": msg.ID}}, region), true
}

// Issue3811Styles lays the card out for a phone first: the action is a 44px
// target, the card wraps rather than scrolls at 320px, status is words and not
// only colour, and logical properties keep it right in a right-to-left language.
const Issue3811Styles = `
.chat-onboarding-card-row{display:flex;padding:6px 16px}
.chat-onboarding-card{display:flex;flex-direction:column;align-items:flex-start;gap:8px;box-sizing:border-box;inline-size:min(100%,36rem);max-inline-size:100%;min-inline-size:0;padding:12px 14px;border:1px solid var(--hcm-color-border);border-inline-start:3px solid var(--hcm-color-brand-primary);border-radius:var(--hcm-radius-control);background:var(--soft);overflow-wrap:anywhere}
.chat-onboarding-title{font-size:1rem}
.chat-onboarding-line,.chat-onboarding-note,.chat-onboarding-source{margin:0;font-size:.875rem}
.chat-onboarding-source{font-size:.75rem;color:var(--hcm-color-text-muted)}
.chat-onboarding-list{display:flex;flex-direction:column;gap:8px;inline-size:100%;margin:0;padding:0;list-style:none}
.chat-onboarding-item{display:flex;flex-direction:column;gap:2px;padding:8px 10px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface,transparent)}
.chat-onboarding-item-head{display:flex;flex-wrap:wrap;align-items:center;gap:6px}
.chat-onboarding-status,.chat-onboarding-required{padding:1px 8px;border:1px solid var(--hcm-color-border);border-radius:999px;font-size:.75rem}
.chat-onboarding-status-confirmed{border-color:var(--hcm-color-brand-primary)}
.chat-onboarding-status-attention{font-weight:600}
.chat-onboarding-action{display:inline-flex;align-items:center;justify-content:center;min-block-size:44px;min-inline-size:44px;text-decoration:none}
.chat-onboarding-card .message-time{font-size:.6875rem;color:var(--hcm-color-text-muted)}
`
