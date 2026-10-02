package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_CHATMAP_004_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(ChatmapShareSheet(locale, "composer", "t", "c", false))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"location", "where", "address", "site", "explain", "device", "precision", "duration", "send", "preview"} {
			if !strings.Contains(markup, ChatmapText(locale, key)) {
				t.Fatal(locale, key, markup)
			}
		}
		if strings.Contains(markup, `value=""`) || !strings.Contains(markup, `type="button"`) {
			t.Fatal("typed input controlled by Value")
		}
	}
	if !ChatmapSlashCommand("/location") || ChatmapSlashCommand("/locationx") {
		t.Fatal("slash matching")
	}
}
func TestTodo_CHATMAP_004_Accessibility(t *testing.T) {
	if !strings.Contains(ChatmapStyles, "44px") || !strings.Contains(ChatmapStyles, ":focus-visible") || !strings.Contains(ChatmapStyles, "prefers-reduced-motion") {
		t.Fatal("touch, focus or motion missing")
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(ChatmapShareSheet(locale, "composer", "t", "c", true))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`role="status"`, `aria-live="polite"`, `aria-describedby="composer-location-status"`, "<label", "disabled"} {
			if !strings.Contains(markup, want) {
				t.Fatal(want)
			}
		}
	}
}
func TestTodo_CHATMAP_003_Accessibility(t *testing.T) {
	now := time.Now()
	expiry := now.Add(time.Hour)
	v := ChatmapEmbed{Locale: "en-US", Now: now, ViewerID: "alice", ViewerTenantID: "t", Sharer: "Alice", Attribution: "Schematic · no map data", Share: chat.LocationShare{ID: "s", TenantID: "t", ConversationID: "c", PostID: "m", SharerID: "alice", SharerTenantID: "t", ExpiresAt: &expiry, Place: chat.LocationPlace{Position: &chat.LocationPosition{Latitude: 40, Longitude: 10, Accuracy: 20}, CapturedAt: now.Add(-4 * time.Minute), Label: "Depot", Address: "Main Street"}}}
	markup, err := ui.RenderToString(ChatmapLocationEmbed(v))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Depot", "Main Street", "Within 20 m", "Shared 4 minutes ago", "Alice", "leaves the product", "Stop sharing", `width="400"`, `height="200"`, `loading="lazy"`, "alt="} {
		if !strings.Contains(markup, want) {
			t.Fatal(want, markup)
		}
	}
	if !strings.Contains(markup, `href="geo:40.000000,10.000000?q=Depot"`) || !strings.Contains(markup, `data-chatmap-address="true"`) {
		t.Fatal("device handoff or synchronous copy address missing", markup)
	}
	v.ViewerID = "bob"
	markup, _ = ui.RenderToString(ChatmapLocationEmbed(v))
	if strings.Contains(markup, "Stop sharing") {
		t.Fatal("stop offered to reader")
	}
	v.Share.Ended = true
	markup, _ = ui.RenderToString(ChatmapLocationEmbed(v))
	if !strings.Contains(markup, "Location no longer shared") || strings.Contains(markup, "Main Street") || strings.Contains(markup, "40.00000") {
		t.Fatal("expired data visible", markup)
	}
}
func TestTodo_CHATMAP_004(t *testing.T) {
	markup := render(t, Model{State: StateReady, Locale: "en-US", CurrentTenantID: "t", CurrentUser: "alice", SelectedID: "c", Conversations: []Conversation{{ID: "c", Name: "Crew", Joined: true}}, Callbacks: Callbacks{SendMessage: func(string, string) {}}})
	if !strings.Contains(markup, "chatmap-sheet") {
		t.Fatal("composer action unreachable")
	}
}
func TestTodo_CHATMAP_003_Security(t *testing.T) {
	for _, raw := range []string{"https://outside.example/pixel", "//outside.example/pixel", "/picture?latitude=42", "/picture#longitude=10", "blob:https://outside.example/pixel"} {
		if ChatmapOwnPictureURL(raw, "https://product.example") {
			t.Fatal("external picture or coordinate address accepted", raw)
		}
	}
	if !ChatmapOwnPictureURL("/chat/locations/image/opaque", "https://product.example") || !ChatmapOwnPictureURL("blob:https://product.example/opaque", "https://product.example") {
		t.Fatal("own-origin picture refused")
	}
	markup, err := ui.RenderToString(ChatmapShareSheet("ar", "composer", "tenant", "room", false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `dir="rtl"`) || strings.Contains(markup, `name="lat"`) || strings.Contains(markup, `name="lon"`) || strings.Contains(markup, `name="readable"`) {
		t.Fatal("position could be serialized into a native GET form", markup)
	}
}
