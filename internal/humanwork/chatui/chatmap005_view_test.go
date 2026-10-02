package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatmapLiveEmbed(now time.Time) ChatmapEmbed {
	expiry := now.Add(15 * time.Minute)
	position := now.Add(-5 * time.Minute)
	return ChatmapEmbed{Locale: "en-US", Now: now, ViewerID: "bob", ViewerTenantID: "t", Sharer: "Alice", Share: chat.LocationShare{ID: "s", TenantID: "t", ConversationID: "c", PostID: "m", SharerID: "alice", SharerTenantID: "t", Live: true, LiveInterval: 30 * time.Second, PositionAt: &position, ExpiresAt: &expiry, Place: chat.LocationPlace{Position: &chat.LocationPosition{Latitude: 40, Longitude: 10, Accuracy: 20}, CapturedAt: position, Label: "Truck 4"}}}
}

func TestTodo_CHATMAP_005_Browser(t *testing.T) {
	now := time.Now().UTC()
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(ChatmapShareSheet(locale, "composer", "t", "c", false))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"live15", "live60", "live480", "live_note", "retention", "mine", "crew_map"} {
			if !strings.Contains(markup, ChatmapText(locale, key)) {
				t.Fatal(locale, key)
			}
		}
		banner, err := ui.RenderToString(ChatmapLiveBanner(locale, 14))
		if err != nil || !strings.Contains(banner, ChatmapText(locale, "stop_live")) || !strings.Contains(banner, "14") || !strings.Contains(banner, `role="status"`) {
			t.Fatal(locale, "banner", banner, err)
		}
	}
	// Live card: labelled live, paused once the sharer's page has gone quiet.
	v := chatmapLiveEmbed(now)
	v.Share.Paused = true
	markup, err := ui.RenderToString(ChatmapLocationEmbed(v))
	if err != nil || !strings.Contains(markup, "Live") || !strings.Contains(markup, "Paused: the sharer") || !strings.Contains(markup, `data-live="true"`) {
		t.Fatal("live card", markup, err)
	}
	if strings.Contains(markup, "Stop sharing") {
		t.Fatal("a reader was offered Stop")
	}
	v.ViewerID = "alice"
	if markup, _ = ui.RenderToString(ChatmapLocationEmbed(v)); !strings.Contains(markup, "Stop sharing") || !strings.Contains(markup, "Sharing for 15 more minutes") {
		t.Fatal("the sharer cannot stop or see the time left", markup)
	}
	// Ended: when it ended, and no place.
	ended := now.Add(-time.Minute)
	v.Share.Ended, v.Share.EndedAt, v.Share.Place = true, &ended, chat.LocationPlace{}
	markup, _ = ui.RenderToString(ChatmapLocationEmbed(v))
	if !strings.Contains(markup, "Location no longer shared") || !strings.Contains(markup, "Sharing ended at "+ended.Format("15:04")) || strings.Contains(markup, "Truck 4") || strings.Contains(markup, "40.0") {
		t.Fatal("ended card", markup)
	}
}

func TestTodo_CHATMAP_005_CrewView(t *testing.T) {
	now := time.Now().UTC()
	live := chatmapLiveEmbed(now).Share
	site := chat.LocationSite{ID: "s1", Label: "Depot", Address: "Main Street"}
	markup, err := ui.RenderToString(ChatmapCrewView("en-US", []ChatmapCrewEntry{{Number: 1, Name: "Alice", Share: live}, {Number: 2, Site: &site}}, "blob:https://product.example/x", "https://product.example", now))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1. Alice", "Within 20 m", "Shared 5 minutes ago", "Open message", `data-post="m"`, "2. Depot", "Job sites", "Crew map with 1 people sharing live and 1 job sites", "Schematic"} {
		if !strings.Contains(markup, want) {
			t.Fatal("crew view missing", want, markup)
		}
	}
	// An outside picture address is dropped and the list still carries the meaning.
	markup, _ = ui.RenderToString(ChatmapCrewView("en-US", []ChatmapCrewEntry{{Number: 1, Name: "Alice", Share: live}}, "https://outside.example/pixel", "https://product.example", now))
	if strings.Contains(markup, "outside.example") || !strings.Contains(markup, "1. Alice") {
		t.Fatal("outside picture accepted", markup)
	}
	empty, _ := ui.RenderToString(ChatmapCrewView("de-DE", nil, "", "", now))
	if !strings.Contains(empty, ChatmapText("de-DE", "crew_empty")) {
		t.Fatal("empty crew map", empty)
	}
}

func TestTodo_CHATMAP_004_TypedAddress(t *testing.T) {
	now := time.Now().UTC()
	v := ChatmapEmbed{Locale: "en-US", Now: now, ViewerID: "bob", ViewerTenantID: "t", Sharer: "Alice", Share: chat.LocationShare{ID: "s", TenantID: "t", ConversationID: "c", PostID: "m", SharerID: "alice", SharerTenantID: "t", SharedAt: now.Add(-2 * time.Minute), Place: chat.LocationPlace{Source: chat.LocationAddress, Address: "12 Quay Street, back gate", Precision: "exact"}}}
	markup, err := ui.RenderToString(ChatmapLocationEmbed(v))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"12 Quay Street, back gate", "Shared 2 minutes ago", "Shared by Alice", "Map detail unavailable", "Copy address", `href="geo:0,0?q=12+Quay+Street%2C+back+gate"`} {
		if !strings.Contains(markup, want) {
			t.Fatal("typed address card missing", want, markup)
		}
	}
	if strings.Contains(markup, "Within") || strings.Contains(markup, "NaN") {
		t.Fatal("an accuracy was invented for an address", markup)
	}
}
