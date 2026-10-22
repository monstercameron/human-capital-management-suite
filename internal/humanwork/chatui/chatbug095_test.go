package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATBUG_095 pins the four render-side clauses of the no-image bug:
// a measured tile carries its stored dimensions (no layout jump), a tile that
// cannot load ends in the file name, size and Download, the GIF badge never
// covers text, and a thread root with an image attachment renders the tile
// rather than "Preview unavailable".
func TestTodo_CHATBUG_095(t *testing.T) {
	image := Attachment{ID: "img-1", Name: "photo.png", ContentType: "image/png", Bytes: 8192, Width: 640, Height: 480}
	animated := Attachment{ID: "gif-1", Name: "party.gif", ContentType: "image/gif", Bytes: 12288, Width: 480, Height: 270}
	model := Model{State: StateReady, Locale: "en-US", CurrentUser: "alice", SelectedID: "general",
		Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel}},
		Messages:      []Message{{ID: "post", AuthorID: "alice", Author: "Alice", Body: "Look", Attachments: []Attachment{image, animated}}},
		Callbacks:     Callbacks{DownloadAttachment: func(string, string) {}},
	}
	markup := render(t, model)
	for _, want := range []string{
		`attachment-image measured`,
		`data-frame-w="640"`, `data-frame-h="480"`,
		`width="640"`, `height="480"`,
		`data-media-id="img-1"`, `data-media-thumb="thumbnail"`, `data-media-display="display"`,
		`attachment-image measured gif`,
		`data-frame-w="480"`, `data-frame-h="270"`,
		`attachment-badge`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("measured tile missing %q", want)
		}
	}
	// The pre-rendered failure fallback names the file with its size and the
	// one action; the bounded load ends here, not on a spinner.
	for _, want := range []string{`attachment-fallback`, "photo.png", "8 KB", "party.gif", "12 KB", `data-action="download-attachment"`} {
		if strings.Count(markup, want) < 1 {
			t.Errorf("failure fallback missing %q", want)
		}
	}
	// The badge labels a visible picture only: while the <img> has no source
	// (still loading) or the tile carries no image at all, the badge is
	// hidden so it can never cover the loading or fallback text. A failed
	// tile keeps its badge at the top, where the fallback reserves room.
	for _, want := range []string{
		`.attachment-image:not(.failed):has(>.attachment-image-open img:not([src])) .attachment-badge{display:none}`,
		`.attachment-image:not(:has(>.attachment-image-open img)) .attachment-badge{display:none}`,
		`.attachment-image.failed .attachment-badge{inset-block-end:auto;inset-block-start:6px}`,
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("badge scoping missing %q", want)
		}
	}
}

// TestTodo_CHATBUG_095_ThreadRoot renders the same image attachment under an
// open thread: the root shows the tile, not "Preview unavailable".
func TestTodo_CHATBUG_095_ThreadRoot(t *testing.T) {
	model := Model{State: StateReady, Locale: "en-US", CurrentUser: "alice", SelectedID: "general", ShowThread: true, ThreadParentID: "post",
		Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel}},
		Messages:      []Message{{ID: "post", AuthorID: "alice", Author: "Alice", Body: "Look", Attachments: []Attachment{{ID: "img-1", Name: "photo.png", ContentType: "image/png", Bytes: 8192, Width: 640, Height: 480}}}},
		Callbacks:     Callbacks{CloseThread: func() {}, DownloadAttachment: func(string, string) {}},
	}
	markup := render(t, model)
	root := markup[strings.Index(markup, "thread-root"):]
	if !strings.Contains(root, "attachment-image-open") {
		t.Fatalf("thread root lost its image tile: %s", root)
	}
	// The failure fallback is pre-rendered but hidden; the tile itself must
	// not carry the failed state that would show it.
	if strings.Contains(root, "attachment-image measured failed") || strings.Contains(root, "attachment-image failed") {
		t.Fatal("thread root marks a loadable image failed")
	}
}
