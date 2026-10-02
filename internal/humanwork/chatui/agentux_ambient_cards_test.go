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
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		catalog := productui.ResolveProductLocale(locale)
		m := chatui.Model{Locale: catalog.Resolved, CurrentUser: "person", Text: func(key string) string { return catalog.Text(key) }}
		for _, kind := range []string{"TASK", "REMINDER"} {
			for _, scope := range []string{"PRIVATE", "PUBLIC"} {
				for _, state := range []string{"OFFERED", "NEEDS_TIME", "SOURCE_CHANGED", "SET", "ADDED"} {
					for _, width := range []string{"1440", "800", "390", "320"} {
						for _, theme := range []string{"light", "dark"} {
							c := chatui.AgentUXAmbientCard{ID: "card", AgentName: "Task Catcher", Kind: kind, Scope: scope, Person: "person", Reason: "self_commitment", Title: "Review section 3", State: state, SourceHref: "/workspace/app/chat?conversation=general&post=source", CanManage: true, Zone: "America/New_York", Due: time.Date(2026, 10, 2, 21, 0, 0, 0, time.UTC), Lead: "deadline_two_hours_and_morning", Fire: []time.Time{time.Date(2026, 10, 2, 19, 0, 0, 0, time.UTC)}}
							if kind == "REMINDER" {
								c.AgentName = "Reminder"
							}
							markup, err := ui.RenderToString(chatui.RenderAgentUXAmbientCard(m, c))
							if err != nil {
								t.Fatal(err)
							}
							if strings.Contains(markup, "⟦") || strings.Contains(markup, "⟧") || strings.Contains(markup, "chat.ambient.") || !strings.Contains(markup, "Review section 3") || !strings.Contains(markup, "href=\"/workspace/app/chat?") {
								t.Fatalf("%s %s %s %s %s %s: %s", locale, kind, scope, state, width, theme, markup)
							}
							if !strings.Contains(markup, "overflow-wrap:anywhere") || strings.Contains(markup, "font-family") || strings.Contains(markup, "#fff") {
								t.Fatal("card lacks safe sizing or shell tokens")
							}
							if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
								t.Fatal("RTL missing")
							}
							if state == "NEEDS_TIME" || state == "SOURCE_CHANGED" {
								if !strings.Contains(markup, `name="date"`) || !strings.Contains(markup, `name="clock"`) || strings.Contains(markup, `type="time" value=`) {
									t.Fatal("time prompt or uncontrolled input missing")
								}
							}
						}
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
	icon := agenticon.Generate(agenticon.Input{Name: "Stored reminder", Instructions: "Remind the channel about deadlines"})
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
