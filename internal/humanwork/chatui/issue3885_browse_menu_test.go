package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3885_BrowseItemSitsInsideTheChannelsMenu: the focus return depends on
// the Browse channels item living inside the .section-menu that has the toggle.
func TestIssue3885_BrowseItemSitsInsideTheChannelsMenu(t *testing.T) {
	page := lane3Page(t, "en-US", func(m *chatui.Model) {
		m.Callbacks.OpenBrowse, m.Callbacks.OpenCreate = func() {}, func() {}
	})
	start := strings.Index(page, `class="section-menu"`)
	if start < 0 {
		t.Fatal("no Channels menu in the rail")
	}
	menu := page[start:]
	toggle, item := strings.Index(menu, "data-chat-disclosure-toggle"), strings.Index(menu, `data-action="open-browse"`)
	if toggle < 0 || item < 0 || toggle > item {
		t.Fatalf("Channels menu toggle at %d, Browse item at %d", toggle, item)
	}
}
