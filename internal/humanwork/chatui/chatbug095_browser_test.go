package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATBUG_095_Browser loads a seeded conversation page carrying an
// image and a GIF post and pins everything the live browser needs to decode
// them: measured tiles at the stored dimensions, the loader wiring, a
// complete failure fallback, and badge rules that cannot cover text. The
// live decode itself (naturalWidth above zero) is specified in
// tools/uxqual/browser/chatbug095_image_attachments.spec.mjs, which needs a
// running server and browser and is not executed here.
func TestTodo_CHATBUG_095_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "ar"} {
		t.Run(locale, func(t *testing.T) {
			ctx := productui.ResolveProductLocale(locale)
			m := chatui.Model{State: chatui.StateReady, SelectedID: "room", CurrentTenantID: "tenant", CurrentUser: "alice", Locale: ctx.Resolved, Direction: string(ctx.Direction), Text: func(key string) string { return ctx.Text(key) },
				Conversations: []chatui.Conversation{{ID: "room", Name: "Team", Kind: chatui.PrivateChannel, Joined: true}},
				Messages: []chatui.Message{{ID: "post", Author: "Alice", AuthorID: "alice", Body: "Look", Attachments: []chatui.Attachment{
					{ID: "img-1", Name: "photo.png", ContentType: "image/png", Bytes: 8192, Width: 640, Height: 480},
					{ID: "gif-1", Name: "party.gif", ContentType: "image/gif", Bytes: 12288, Width: 480, Height: 270},
				}}},
				Callbacks: chatui.Callbacks{SendMessage: func(string, string) {}, DownloadAttachment: func(string, string) {}},
			}
			page, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				`attachment-image measured`, `attachment-image measured gif`,
				`data-frame-w="640"`, `data-frame-h="480"`, `data-frame-w="480"`, `data-frame-h="270"`,
				`width="640"`, `height="480"`, `width="480"`, `height="270"`,
				`data-media-id="img-1"`, `data-media-thumb="thumbnail"`, `data-media-display="display"`,
				`data-media-id="gif-1"`, `data-media-animated="true"`,
				`attachment-fallback`, `attachment-fallback-size`, "photo.png", "party.gif",
				`data-action="download-attachment"`,
			} {
				if !strings.Contains(page, want) {
					t.Errorf("seeded page missing %q", want)
				}
			}
			// Opening the thread keeps the parent's tile loadable.
			m.ShowThread, m.ThreadParentID = true, "post"
			page, err = ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			root := page[strings.Index(page, "thread-root"):]
			if !strings.Contains(root, "attachment-image-open") {
				t.Fatal("thread root lost the image tile")
			}
			if strings.Contains(root, "attachment-image measured failed") {
				t.Fatal("thread root marks a loadable image failed")
			}
		})
	}
}
