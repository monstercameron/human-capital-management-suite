package chatui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestAgentUXAmbient_Cards_Browser(t *testing.T) {
	runAgentUXAmbientCardsBrowser(t, "TASK", "REMINDER")
}

func runAgentUXAmbientCardsBrowser(t *testing.T, kinds ...string) {
	t.Helper()
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		catalog := productui.ResolveProductLocale(locale)
		m := chatui.Model{Locale: catalog.Resolved, CurrentUser: "person", Text: func(key string) string { return catalog.Text(key) }}
		for _, kind := range kinds {
			for _, scope := range []string{"PRIVATE", "PUBLIC"} {
				states := []string{"OFFERED", "SOURCE_CHANGED", "ADDED"}
				if kind == "REMINDER" {
					states = []string{"OFFERED", "NEEDS_TIME", "SOURCE_CHANGED", "SET"}
				}
				for _, state := range states {
					c := chatui.AgentUXAmbientCard{ID: "card", AgentName: "Task Catcher", Kind: kind, Scope: scope, Person: "person", Reason: "self_commitment", Title: "Review section 3", State: state, SourceHref: "/workspace/app/chat?conversation=general&post=source", CanManage: true, Zone: "America/New_York", Due: time.Date(2026, 10, 2, 21, 0, 0, 0, time.UTC), Lead: "deadline_two_hours_and_morning", Fire: []time.Time{time.Date(2026, 10, 2, 19, 0, 0, 0, time.UTC)}}
					if kind == "REMINDER" {
						c.AgentName = "Reminder"
					}
					markup, err := ui.RenderToString(chatui.RenderAgentUXAmbientCard(m, c))
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(markup, "⟦") || strings.Contains(markup, "⟧") || strings.Contains(markup, "chat.ambient.") || !strings.Contains(markup, "Review section 3") || !strings.Contains(markup, "href=\"/workspace/app/chat?") {
						t.Fatalf("%s %s %s %s: %s", locale, kind, scope, state, markup)
					}
					if !strings.Contains(markup, "overflow-wrap:anywhere") || strings.Contains(markup, "font-family") || strings.Contains(markup, "#fff") {
						t.Fatalf("%s %s card lacks safe sizing or shell tokens", locale, kind)
					}
					if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
						t.Fatalf("%s %s missing RTL", kind, state)
					}
					if state == "OFFERED" {
						want := ambientCardOfferCopy(locale, kind, scope)
						want = strings.ReplaceAll(want, "'", "&#39;")
						if !strings.Contains(markup, want) {
							t.Fatalf("%s %s %s offer missing %q: %s", locale, kind, scope, want, markup)
						}
						if kind == "TASK" && (!strings.Contains(markup, `data-ambient-action="ADD"`) || scope == "PRIVATE" && !strings.Contains(markup, `data-ambient-action="NOT_TASK"`)) {
							t.Fatalf("%s %s task offer has no expected actions: %s", locale, scope, markup)
						}
						if kind == "REMINDER" && (!strings.Contains(markup, `data-ambient-action="SET"`) || !strings.Contains(markup, ambientReminderScheduleCopy(locale)) || !strings.Contains(markup, ambientReminderAtPrefix(locale))) {
							t.Fatalf("%s %s reminder offer omits its actions or stated schedule: %s", locale, scope, markup)
						}
					}
					if state == "NEEDS_TIME" || state == "SOURCE_CHANGED" {
						if !strings.Contains(markup, `name="date"`) || !strings.Contains(markup, `name="clock"`) || strings.Contains(markup, `type="time" value=`) {
							t.Fatalf("%s %s %s time prompt or uncontrolled input missing: %s", locale, kind, state, markup)
						}
					}
					if state == "SOURCE_CHANGED" && !strings.Contains(markup, `role="status"`) {
						t.Fatalf("%s %s source-change state has no status: %s", locale, kind, markup)
					}
					if state == "SET" && (!strings.Contains(markup, `data-ambient-action="EDIT_TIME"`) || !strings.Contains(markup, `data-ambient-action="SNOOZE"`) || !strings.Contains(markup, `data-ambient-action="CANCEL"`)) {
						t.Fatalf("%s reminder lacks set-state actions: %s", locale, markup)
					}
				}
			}
		}
		for _, state := range []string{"loading", "error", "ready"} {
			markup, err := ui.RenderToString(chatui.RenderAgentUXAmbientList(m, nil, state))
			if err != nil || markup == "" || strings.Contains(markup, "⟦") {
				t.Fatalf("%s %s state %q %v", locale, state, markup, err)
			}
		}
		for _, state := range []string{"loading", "error"} {
			card := chatui.AgentUXAmbientCard{ID: "kept", Kind: "TASK", Scope: "PUBLIC", Title: "Keep the existing offer", State: "OFFERED", CanManage: true}
			markup, err := ui.RenderToString(chatui.RenderAgentUXAmbientList(m, []chatui.AgentUXAmbientCard{card}, state))
			if err != nil || !strings.Contains(markup, card.Title) || strings.Contains(markup, "⟦") {
				t.Fatalf("refresh hid prior offer %s %s %v", locale, markup, err)
			}
			if state == "error" && !strings.Contains(markup, `data-ambient-action="REFRESH"`) {
				t.Fatal("read error has no retry")
			}
		}
		for _, state := range []string{"CANCELLED", "SOURCE_UNAVAILABLE"} {
			card := chatui.AgentUXAmbientCard{ID: "removed", Kind: "REMINDER", Scope: "PRIVATE", Person: "person", Title: "Deleted secret body", State: state, CanManage: true, Reason: "source_deleted", Due: time.Now(), Fire: []time.Time{time.Now()}}
			markup, err := ui.RenderToString(chatui.RenderAgentUXAmbientCard(m, card))
			if err != nil || strings.Contains(markup, card.Title) || strings.Contains(markup, "⟦") || !strings.Contains(markup, `data-ambient-action="DISMISS"`) || strings.Contains(markup, `data-ambient-action="SET"`) {
				t.Fatalf("unsafe cancellation notice %s %s %v", locale, markup, err)
			}
		}
	}
}

func ambientCardOfferCopy(locale, kind, scope string) string {
	index := 0
	if locale == "de-DE" {
		index = 1
	} else if locale == "ar" {
		index = 2
	}
	copy := map[string][3]string{
		"TASK:PRIVATE":     {"Add to my tasks", "Zu meinen Aufgaben hinzufügen", "أضف إلى مهامي"},
		"TASK:PUBLIC":      {"Add to this channel's to-do list", "Zur Aufgabenliste dieses Kanals hinzufügen", "أضف إلى قائمة مهام هذه القناة"},
		"REMINDER:PRIVATE": {"Remind me", "Mich erinnern", "ذكّرني"},
		"REMINDER:PUBLIC":  {"Remind this channel", "Diesen Kanal erinnern", "ذكّر هذه القناة"},
	}
	return copy[kind+":"+scope][index]
}

func ambientReminderScheduleCopy(locale string) string {
	copy := map[string]string{
		"en-US": "Default: that morning and two hours before the deadline; you can choose another time",
		"de-DE": "Standard: am Morgen und zwei Stunden vor der Frist; Sie können eine andere Zeit wählen",
		"ar":    "الافتراضي: صباح ذلك اليوم وقبل الموعد بساعتين؛ يمكنك اختيار وقت آخر",
	}
	return copy[locale]
}

func ambientReminderAtPrefix(locale string) string {
	copy := map[string]string{"en-US": "Remind at:", "de-DE": "Erinnern um:", "ar": "التذكير عند:"}
	return copy[locale]
}

func TestAgentUXAmbient_Cards_Security(t *testing.T) {
	m := chatui.Model{Locale: "en-US", CurrentUser: "peer"}
	c := chatui.AgentUXAmbientCard{ID: "private", Scope: "PRIVATE", Person: "author", Title: "Private task", Kind: "TASK", State: "OFFERED"}
	markup, err := ui.RenderToString(chatui.RenderAgentUXAmbientCard(m, c))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Private task") {
		t.Fatal("private card rendered to peer")
	}
	c.Scope = "PUBLIC"
	c.SourceHref = "javascript:alert(1)"
	c.Title = "<script>send secrets</script>"
	markup, err = ui.RenderToString(chatui.RenderAgentUXAmbientCard(m, c))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "javascript:") || strings.Contains(markup, "<script>") {
		t.Fatal("source or body escaped rendering boundary")
	}
	icon := chatui.AgentIconFixture(agenticon.Input{Name: "Stored reminder", Instructions: "Remind the channel about deadlines"})
	m.Members = []chatui.Member{{ID: "reminder", Name: "Renamed agent", Agent: true, Icon: icon}}
	c.AgentID = "reminder"
	c.AgentName = "Reminder"
	c.Title = "Original deadline"
	markup, err = ui.RenderToString(chatui.RenderAgentUXAmbientCard(m, c))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := ui.RenderToString(agenticon.Node(icon))
	if err != nil || !strings.Contains(markup, expected) {
		t.Fatal("card did not use its agent's stored icon by identity")
	}
}
