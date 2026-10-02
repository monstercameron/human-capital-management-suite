package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatmapCrewModel(locale string) Model {
	m := chatux001Fixture(locale, 0, 0, 0)
	m.MessageLocations = map[string][]ChatmapEmbed{"m": {chatmapLiveEmbed(time.Now().UTC())}}
	return m
}

// While anyone is sharing live to the conversation the header has a small Map
// control with how many; with nobody, there is none.
func TestTodo_CHATMAP_005_HeaderMap(t *testing.T) {
	header, _ := chatux001Header(t, chatux001Fixture("en-US", 0, 0, 0), handlers{}, 1440, "light")
	if got := chatux001Join(chatux001Actions(t, header)); strings.Contains(got, "header-map") {
		t.Fatal("the Map control is drawn with nobody sharing", got)
	}
	header, markup := chatux001Header(t, chatmapCrewModel("en-US"), handlers{}, 1440, "light")
	if got := chatux001Join(chatux001Actions(t, header)); got != "side-fav,chat-search-open,header-map,header-members,details" {
		t.Fatal("header with a live share = ", got)
	}
	button := chatux001Button(t, header, "header-map")
	if got := chatPolishAttr(button, "aria-label"); got != "Map: 1 sharing live now" || chatPolishAttr(button, "title") != got {
		t.Fatalf("Map name %q, tooltip %q", got, chatPolishAttr(button, "title"))
	}
	if !strings.Contains(markup, `chatmap-header-word`) || !strings.Contains(markup, ">Map<") {
		t.Fatal("the control does not say Map", markup)
	}
	for _, locale := range []string{"de-DE", "ar"} {
		header, _ = chatux001Header(t, chatmapCrewModel(locale), handlers{}, 1440, "light")
		want := strings.Replace(ChatmapText(locale, "header_map_label"), "%s", "1", 1)
		if got := chatPolishAttr(chatux001Button(t, header, "header-map"), "aria-label"); got == "" || got == ChatmapText("en-US", "header_map_label") || !strings.Contains(got, ChatmapText(locale, "header_map")) || strings.Contains(want, "%") {
			t.Fatal(locale, "Map name", got)
		}
	}
	// A share that has ended, run out, or is not live does not count; nor does a
	// deployment without locations.
	now := time.Now().UTC()
	for name, change := range map[string]func(*Model){
		"ended": func(m *Model) {
			e := m.MessageLocations["m"][0]
			e.Share.Ended = true
			m.MessageLocations["m"] = []ChatmapEmbed{e}
		},
		"expired": func(m *Model) {
			e := m.MessageLocations["m"][0]
			past := now.Add(-time.Minute)
			e.Share.ExpiresAt = &past
			m.MessageLocations["m"] = []ChatmapEmbed{e}
		},
		"not live": func(m *Model) {
			e := m.MessageLocations["m"][0]
			e.Share.Live = false
			m.MessageLocations["m"] = []ChatmapEmbed{e}
		},
		"no place": func(m *Model) {
			e := m.MessageLocations["m"][0]
			e.Share.Place.Position = nil
			m.MessageLocations["m"] = []ChatmapEmbed{e}
		},
		"disabled": func(m *Model) { m.ChatFeatures = &ChatFeatures{} },
	} {
		m := chatmapCrewModel("en-US")
		change(&m)
		if n := ChatmapLiveCount(m); n != 0 {
			t.Fatalf("%s share counted: %d", name, n)
		}
	}
	// Two people count as two; one share seen twice counts once.
	m := chatmapCrewModel("en-US")
	second := chatmapLiveEmbed(now)
	second.Share.ID, second.Share.PostID, second.Share.SharerID = "s2", "m2", "carol"
	m.MessageLocations["m2"] = []ChatmapEmbed{second}
	m.MessageLocations["again"] = m.MessageLocations["m"]
	if n := ChatmapLiveCount(m); n != 2 {
		t.Fatal("live count", n)
	}
	// Pressing it opens Conversation details at the crew section, the same panel.
	var calls []string
	m.Callbacks.ToggleDetails = func(open bool) { calls = append(calls, "details") }
	if !chatux001Click(m, "header-map") || chatux001Join(calls) != "details" {
		t.Fatal("the Map control did not open the details", calls)
	}
}

// The crew view is a section of Conversation details, present only while anyone
// is sharing, with every state drawn.
func TestTodo_CHATMAP_005_CrewSection(t *testing.T) {
	quiet := chatux001Fixture("en-US", 0, 0, 0)
	if section := chatmapCrewSection(quiet); section != nil {
		t.Fatal("a crew section with nobody sharing")
	}
	m := chatmapCrewModel("en-US")
	m.ShowDetails = true
	panel := renderNode(t, chatux005Details(m, handlers{}))
	if !strings.Contains(panel, `id="chat-details-crewmap"`) || !strings.Contains(panel, "Crew map") || !strings.Contains(panel, "Loading locations") {
		t.Fatal("details has no crew section while someone shares", panel)
	}
	if strings.Contains(renderNode(t, chatux005Details(quiet, handlers{})), "chat-details-crewmap") {
		t.Fatal("details has a crew section with nobody sharing")
	}
	if strings.Contains(panel, `role="tablist"`) {
		t.Fatal("the details panel gained a tab bar")
	}
	props := chatmapCrewProps{Locale: "en-US", Tenant: "t", Conversation: "c", Viewer: "bob", ViewerTenant: "t", Names: map[string]string{"m": "Alice"}}
	live := chatmapLiveEmbed(time.Now().UTC()).Share
	loaded := chatmapCrewState{Loaded: true, View: chat.LiveMapView{Shares: []chat.LocationShare{live}, Sites: []chat.LocationSite{{ID: "site", Label: "Depot", Address: "1 Main Street", Position: chat.LocationPosition{Latitude: 40, Longitude: 10, Accuracy: 5}}}}}
	markup := renderNode(t, chatmapCrewPanelView(props, loaded, ui.Handler{}))
	for _, want := range []string{"1. Alice", "Within 20 m", "Open message", "Depot", "Crew map with 1 people sharing live and 1 job sites", "Schematic"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("crew view lacks %q: %s", want, markup)
		}
	}
	// The viewer is "You"; nobody sharing says so.
	props.Viewer, props.ViewerTenant = "alice", "t"
	if markup = renderNode(t, chatmapCrewPanelView(props, loaded, ui.Handler{})); !strings.Contains(markup, "1. You") {
		t.Fatal("the sharer is not named You", markup)
	}
	if markup = renderNode(t, chatmapCrewPanelView(props, chatmapCrewState{Loaded: true}, ui.Handler{})); !strings.Contains(markup, "Nobody is sharing live to this conversation right now.") {
		t.Fatal("empty crew view", markup)
	}
	// Failed with nothing read: the sentence and Try again. Failed after a good
	// read: the last map stays above the same sentence and button.
	for name, state := range map[string]chatmapCrewState{"first read": {Failed: true}, "later read": {Loaded: true, Failed: true, View: loaded.View}} {
		markup = renderNode(t, chatmapCrewPanelView(props, state, ui.Handler{}))
		if !strings.Contains(markup, "The crew map could not load. Try again.") || !strings.Contains(markup, ">Try again<") || !strings.Contains(markup, `role="alert"`) {
			t.Fatal(name, "failure not drawn", markup)
		}
		if name == "later read" && !strings.Contains(markup, "Within 20 m") {
			t.Fatal("the last good map was dropped on a failed read")
		}
	}
	for _, locale := range []string{"de-DE", "ar"} {
		props.Locale = locale
		markup = renderNode(t, chatmapCrewPanelView(props, chatmapCrewState{Failed: true}, ui.Handler{}))
		if !strings.Contains(markup, ChatmapText(locale, "crew_failed")) || ChatmapText(locale, "crew_failed") == ChatmapText("en-US", "crew_failed") {
			t.Fatal(locale, "failure copy", markup)
		}
	}
}
