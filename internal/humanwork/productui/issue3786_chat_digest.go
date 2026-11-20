package productui

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3786: the personal chat digest in the shell bell. A person chooses when
// they want one digest of what was held back from them (mentions that arrived
// in quiet hours or in a muted conversation, and followed threads that moved).
// When the time they chose has passed and something was held, the bell shows
// it: each item says who, where and why it waited, and opens the message. The
// server works the digest out from messages the person can read right now and
// sends no message text. "Mark as seen" tells the server the digest was read so
// the next one holds only what is newer.

// Digest cadences, as the server names them.
const (
	ChatDigestOff      = "off"
	ChatDigestDaily    = "daily"
	ChatDigestQuietEnd = "quiet_end"
)

// Why an item waited, as the server names it.
const (
	ChatDigestWhyQuietHours = "quiet_hours"
	ChatDigestWhyMuted      = "muted"
	ChatDigestWhyFollowed   = "followed"
)

// ChatDigestTimes are the local times of day a person can choose for a daily
// digest, in minutes after midnight.
var ChatDigestTimes = []int{360, 420, 480, 540, 600}

// ChatDigestItem is one item of the digest. AuthorName is empty when the page
// does not know the author; the item then says "Someone".
type ChatDigestItem struct {
	_                  incomparable
	Why                string
	ConversationID     string
	ConversationName   string
	PostID             string
	Sequence           uint64
	ThreadRootID       string
	ThreadRootSequence uint64
	AuthorID           string
	AuthorName         string
	At                 time.Time
	Count              int
}

// ChatDigest is the person's digest choice and, when it is due, the digest.
type ChatDigest struct {
	_        incomparable
	Cadence  string
	Minute   int
	Timezone string
	Due      bool
	Through  time.Time
	Items    []ChatDigestItem
}

// ChatDigestHref opens the message an item is about, the thread for a followed
// one.
func ChatDigestHref(item ChatDigestItem) string {
	return ChatNotificationHref(ChatNotification{ConversationID: item.ConversationID, PostID: item.PostID, Sequence: item.Sequence,
		ThreadRootID: item.ThreadRootID, ThreadRootSequence: item.ThreadRootSequence})
}

func chatDigestTimeLabel(minute int) string {
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

// ChatDigestPanelProps is the digest part of the bell.
type ChatDigestPanelProps struct {
	I18nProps
	Digest      *ChatDigest
	Unavailable bool
	Save        func(cadence string, minute int, done func(error))
	Seen        func(through time.Time, done func(error))
	Navigate    func(string)
}

// ChatDigestPanel shows the digest when it is due, and the choice of when to get one.
func ChatDigestPanel(props ChatDigestPanelProps) ui.Node {
	digest := props.Digest
	chosen := ui.UseState(ChatDigestOff)
	minute := ui.UseState(480)
	// seen names the digest (by the moment it was worked out) the person marked as
	// seen, so a later digest in the same session is not hidden by an earlier answer.
	seen := ui.UseState("")
	status := ui.UseState("")
	if digest == nil && !props.Unavailable {
		return html.Div(html.Props{})
	}
	if props.Unavailable {
		return html.Section(html.Props{Class: "chat-digest", Aria: map[string]string{"label": props.Text("chat_digest.heading")}},
			html.H3(html.Props{}, ui.Text(props.Text("chat_digest.heading"))),
			html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(props.Text("chat_digest.unavailable"))))
	}
	// The draft starts at what the server holds and follows the person's own
	// choices after that.
	if status.Get() == "" && chosen.Get() == ChatDigestOff && digest.Cadence != ChatDigestOff && digest.Cadence != "" {
		chosen.Set(digest.Cadence)
		minute.Set(digest.Minute)
	}
	save := func(cadence string, at int) {
		chosen.Set(cadence)
		minute.Set(at)
		status.Set("saving")
		if props.Save == nil {
			status.Set("failed")
			return
		}
		props.Save(cadence, at, func(err error) {
			if err != nil {
				status.Set("failed")
				return
			}
			status.Set("saved")
		})
	}
	nodes := []ui.Node{html.H3(html.Props{}, ui.Text(props.Text("chat_digest.heading")))}
	through := digest.Through.UTC().Format(time.RFC3339Nano)
	if digest.Due && len(digest.Items) > 0 && seen.Get() != through {
		rows := make([]ui.Node, 0, len(digest.Items))
		for _, item := range digest.Items {
			actor := strings.TrimSpace(item.AuthorName)
			if actor == "" {
				actor = props.Text("chat_notifications.someone")
			}
			place := strings.TrimSpace(item.ConversationName)
			if place == "" {
				place = props.Text("chat_notifications.unnamed_place")
			} else {
				place = "#" + place
			}
			key := "chat_digest.item_" + item.Why
			title := props.Text(key, map[string]string{"actor": actor, "place": place})
			if item.Why == ChatDigestWhyFollowed {
				// "3 new replies", not a bare number.
				key = "chat_digest.item_followed_many"
				if item.Count <= 1 {
					key = "chat_digest.item_followed_one"
				}
				title = props.Text(key, map[string]string{"place": place, "count": fmt.Sprint(max(item.Count, 1))})
			}
			when := props.Text("notifications.time_unavailable")
			whenProps := html.Props{Class: "muted workflow-notification-time"}
			if !item.At.IsZero() {
				when = props.Locale.FormatTimestamp(item.At)
				whenProps.Raw = map[string]any{"datetime": item.At.UTC().Format(time.RFC3339)}
			}
			linkProps := html.Props{Class: "workflow-notification chat-notification", Aria: map[string]string{"label": title + ", " + when}}
			row := html.Li(html.Props{Class: "workflow-notification-row chat-notification-row is-unread"},
				softwareLink(props.Navigate, linkProps, ChatDigestHref(item), html.Strong(html.Props{}, ui.Text(title)), html.Tag("time", whenProps, ui.Text(when))))
			row = withKey(row, item.PostID+"/"+item.Why)
			rows = append(rows, row)
		}
		nodes = append(nodes,
			html.P(html.Props{}, ui.Text(props.Text("chat_digest.intro"))),
			html.Ul(html.Props{Class: "workflow-notifications chat-notifications chat-digest-items", Aria: map[string]string{"label": props.Text("chat_digest.heading")}}, rows...))
		if props.Seen != nil {
			markSeen := ui.UseEvent(func(ui.MouseEvent) {
				seen.Set(through)
				props.Seen(digest.Through, func(err error) {
					if err != nil {
						seen.Set("")
						status.Set("failed")
					}
				})
			})
			nodes = append(nodes, html.Div(html.Props{Class: "chat-notification-actions"},
				html.Button(html.Props{Class: "button secondary compact", Type: "button", OnClick: markSeen}, ui.Text(props.Text("chat_digest.mark_seen")))))
		}
	} else if chosen.Get() == ChatDigestOff {
		nodes = append(nodes, html.P(html.Props{Class: "muted"}, ui.Text(props.Text("chat_digest.off_hint"))))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "muted"}, ui.Text(props.Text("chat_digest.nothing_waiting"))))
	}
	choices := []struct{ id, key string }{{ChatDigestOff, "chat_digest.cadence_off"}, {ChatDigestDaily, "chat_digest.cadence_daily"}, {ChatDigestQuietEnd, "chat_digest.cadence_quiet_end"}}
	radios := make([]ui.Node, 0, len(choices))
	for _, choice := range choices {
		choice := choice
		id := "chat-digest-cadence-" + choice.id
		radios = append(radios, html.Label(html.Props{Class: "accessibility-choice", For: id},
			html.Input(html.Props{ID: id, Type: "radio", Name: "chat-digest-cadence", Value: choice.id, Checked: chosen.Get() == choice.id,
				OnChange: ui.UseEvent(func(ui.InputEvent) { save(choice.id, minute.Get()) })}),
			html.Span(html.Props{}, ui.Text(props.Text(choice.key)))))
	}
	nodes = append(nodes, html.Fieldset(html.Props{Class: "chat-digest-choice"},
		html.Legend(html.Props{}, ui.Text(props.Text("chat_digest.when"))), html.Div(html.Props{Class: "chat-digest-radios"}, radios...)))
	if chosen.Get() == ChatDigestDaily {
		times := make([]ui.Node, 0, len(ChatDigestTimes))
		for _, at := range ChatDigestTimes {
			at := at
			id := fmt.Sprintf("chat-digest-time-%d", at)
			times = append(times, html.Label(html.Props{Class: "accessibility-choice", For: id},
				html.Input(html.Props{ID: id, Type: "radio", Name: "chat-digest-time", Value: fmt.Sprint(at), Checked: minute.Get() == at,
					OnChange: ui.UseEvent(func(ui.InputEvent) { save(ChatDigestDaily, at) })}),
				html.Span(html.Props{}, ui.Text(chatDigestTimeLabel(at)))))
		}
		nodes = append(nodes, html.Fieldset(html.Props{Class: "chat-digest-choice"},
			html.Legend(html.Props{}, ui.Text(props.Text("chat_digest.time"))), html.Div(html.Props{Class: "chat-digest-radios"}, times...)))
	}
	if chosen.Get() == ChatDigestQuietEnd {
		nodes = append(nodes, html.P(html.Props{Class: "muted"}, ui.Text(props.Text("chat_digest.quiet_end_hint"))))
	}
	statusText := ""
	switch status.Get() {
	case "saving":
		statusText = props.Text("chat_digest.saving")
	case "saved":
		statusText = props.Text("chat_digest.saved")
	case "failed":
		statusText = props.Text("chat_digest.failed")
	}
	statusProps := html.Props{Class: "muted chat-digest-status", Raw: map[string]any{"aria-live": "polite", "aria-atomic": "true"}}
	if status.Get() == "failed" {
		statusProps = html.Props{Class: "notification-read-error chat-digest-status", Role: "alert"}
	}
	nodes = append(nodes, html.P(statusProps, ui.Text(statusText)))
	return html.Section(html.Props{Class: "chat-digest", Aria: map[string]string{"label": props.Text("chat_digest.heading")}}, nodes...)
}

func withKey(node ui.Node, key string) ui.Node {
	if node.Props == nil {
		node.Props = map[string]any{}
	}
	node.Props["key"] = key
	return node
}

func chatDigestTranslations(locale string) map[string]string {
	switch locale {
	case "de-DE":
		return map[string]string{
			"chat_digest.heading":            "Ihre Zusammenfassung",
			"chat_digest.unavailable":        "Die Chat-Zusammenfassung ist vorübergehend nicht verfügbar.",
			"chat_digest.intro":              "Das ist liegen geblieben, während Sie nicht erreichbar waren.",
			"chat_digest.item_quiet_hours":   "{actor} hat Sie in {place} erwähnt, während Ihre Ruhezeit galt",
			"chat_digest.item_muted":         "{actor} hat Sie in {place} erwähnt (stummgeschaltet)",
			"chat_digest.item_followed_one":  "Eine neue Antwort in einem Thread, dem Sie folgen ({place})",
			"chat_digest.item_followed_many": "{count} neue Antworten in einem Thread, dem Sie folgen ({place})",
			"chat_digest.mark_seen":          "Als gesehen markieren",
			"chat_digest.off_hint":           "Wählen Sie unten, ob und wann Sie eine Zusammenfassung erhalten möchten.",
			"chat_digest.nothing_waiting":    "Nichts ist liegen geblieben.",
			"chat_digest.when":               "Zusammenfassung erhalten",
			"chat_digest.cadence_off":        "Nie",
			"chat_digest.cadence_daily":      "Täglich zu einer festen Uhrzeit",
			"chat_digest.cadence_quiet_end":  "Wenn meine Ruhezeit endet",
			"chat_digest.time":               "Uhrzeit",
			"chat_digest.quiet_end_hint":     "Dazu müssen Ruhezeiten in Ihren Chat-Einstellungen eingeschaltet sein.",
			"chat_digest.saving":             "Wird gespeichert …",
			"chat_digest.saved":              "Gespeichert.",
			"chat_digest.failed":             "Das konnte nicht gespeichert werden. Versuchen Sie es erneut.",
		}
	case "ar":
		return map[string]string{
			"chat_digest.heading":            "ملخصك",
			"chat_digest.unavailable":        "ملخص الدردشة غير متاح مؤقتًا.",
			"chat_digest.intro":              "هذا ما تأخر وصوله إليك أثناء غيابك.",
			"chat_digest.item_quiet_hours":   "أشار إليك {actor} في {place} خلال ساعات الهدوء",
			"chat_digest.item_muted":         "أشار إليك {actor} في {place} (مكتومة)",
			"chat_digest.item_followed_one":  "رد جديد في سلسلة رسائل تتابعها ({place})",
			"chat_digest.item_followed_many": "{count} ردود جديدة في سلسلة رسائل تتابعها ({place})",
			"chat_digest.mark_seen":          "وضع علامة تمت المشاهدة",
			"chat_digest.off_hint":           "اختر أدناه ما إذا كنت تريد ملخصًا ومتى.",
			"chat_digest.nothing_waiting":    "لا شيء في الانتظار.",
			"chat_digest.when":               "تلقي ملخص",
			"chat_digest.cadence_off":        "أبدًا",
			"chat_digest.cadence_daily":      "يوميًا في وقت محدد",
			"chat_digest.cadence_quiet_end":  "عند انتهاء ساعات الهدوء",
			"chat_digest.time":               "الوقت",
			"chat_digest.quiet_end_hint":     "يتطلب ذلك تشغيل ساعات الهدوء في إعدادات الدردشة.",
			"chat_digest.saving":             "جارٍ الحفظ…",
			"chat_digest.saved":              "تم الحفظ.",
			"chat_digest.failed":             "تعذّر الحفظ. حاول مرة أخرى.",
		}
	default:
		return map[string]string{
			"chat_digest.heading":            "Your digest",
			"chat_digest.unavailable":        "The chat digest is temporarily unavailable.",
			"chat_digest.intro":              "This waited for you while you were away.",
			"chat_digest.item_quiet_hours":   "{actor} mentioned you in {place} during your quiet hours",
			"chat_digest.item_muted":         "{actor} mentioned you in {place} (muted)",
			"chat_digest.item_followed_one":  "1 new reply in a thread you follow ({place})",
			"chat_digest.item_followed_many": "{count} new replies in a thread you follow ({place})",
			"chat_digest.mark_seen":          "Mark as seen",
			"chat_digest.off_hint":           "Choose below if and when you want a digest.",
			"chat_digest.nothing_waiting":    "Nothing is waiting for you.",
			"chat_digest.when":               "Get a digest",
			"chat_digest.cadence_off":        "Never",
			"chat_digest.cadence_daily":      "Every day at a set time",
			"chat_digest.cadence_quiet_end":  "When my quiet hours end",
			"chat_digest.time":               "Time of day",
			"chat_digest.quiet_end_hint":     "This needs quiet hours switched on in your chat settings.",
			"chat_digest.saving":             "Saving…",
			"chat_digest.saved":              "Saved.",
			"chat_digest.failed":             "That could not be saved. Try again.",
		}
	}
}

func chatDigestStylesheet() string {
	return `
.chat-digest{display:grid;gap:var(--hcm-space-2);margin-block:var(--hcm-space-3)}
.chat-digest-choice{border:0;margin:0;padding:0;min-inline-size:0}
.chat-digest-radios{display:flex;flex-wrap:wrap;gap:var(--hcm-space-2)}
.chat-digest-radios .accessibility-choice{min-height:44px;box-sizing:border-box;align-items:center}
.chat-digest-status:empty{margin:0;block-size:0;overflow:hidden}
`
}
