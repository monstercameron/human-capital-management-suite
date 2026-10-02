package chatui

import (
	"strings"
	"testing"
)

// loadedMemo is an attachment whose preview fetched and decoded.
func loadedMemo(t *testing.T) *chatImageMemo {
	t.Helper()
	m := &chatImageMemo{}
	m.fetched("blob:gif")
	m.apply(chatImageEventDecoded)
	if m.phase != chatImageLoaded {
		t.Fatalf("decoded preview is %v, want loaded", m.phase)
	}
	return m
}

// TestTodo_CHATBUG_009 walks the transitions a hover or a menu puts an
// attachment through: the timeline re-renders, the element is replaced, the
// request of the old element is aborted. The preview must stay loaded, an
// abort must never read as a failure, and "failed" must not flip back.
func TestTodo_CHATBUG_009(t *testing.T) {
	t.Run("a remounted element shows the loaded preview again", func(t *testing.T) {
		m := loadedMemo(t)
		for i := 0; i < 5; i++ { // five hover and menu re-renders
			if got := m.remounted(); got != chatImageEffectShowLoaded {
				t.Fatalf("remount %d: effect %v, want show-loaded", i, got)
			}
		}
		if m.objectURL != "blob:gif" || m.phase != chatImageLoaded {
			t.Fatalf("remounting changed the memo: %+v", m)
		}
	})

	t.Run("an aborted request is not a failure", func(t *testing.T) {
		pending := &chatImageMemo{}
		pending.apply(chatImageEventCancelled)
		if pending.phase != chatImagePending || pending.remounted() != chatImageEffectFetch {
			t.Fatalf("aborted pending request left the memo %+v, want pending and fetch on remount", pending)
		}
		loaded := loadedMemo(t)
		loaded.apply(chatImageEventCancelled)
		if loaded.phase != chatImageLoaded || loaded.objectURL == "" {
			t.Fatalf("abort of a superseded request unloaded the preview: %+v", loaded)
		}
	})

	t.Run("a real failure shows unavailable once and stays", func(t *testing.T) {
		m := &chatImageMemo{}
		m.apply(chatImageEventBroke)
		for i := 0; i < 5; i++ {
			if got := m.remounted(); got != chatImageEffectShowFailed {
				t.Fatalf("remount %d after failure: effect %v, want show-failed (no refetch, no flip)", i, got)
			}
		}
		m.apply(chatImageEventCancelled)
		if m.phase != chatImageFailed {
			t.Fatalf("abort after failure changed the phase to %v", m.phase)
		}
	})

	t.Run("the single retry loads or fails for good", func(t *testing.T) {
		m := &chatImageMemo{}
		m.apply(chatImageEventBroke)
		m.apply(chatImageEventRetry)
		if m.phase != chatImagePending || !m.retried {
			t.Fatalf("retry left %+v, want pending and retried", m)
		}
		m.apply(chatImageEventBroke)
		m.apply(chatImageEventRetry)
		if m.phase != chatImageFailed {
			t.Fatalf("a second retry started: phase %v", m.phase)
		}
		ok := &chatImageMemo{}
		ok.apply(chatImageEventBroke)
		ok.apply(chatImageEventRetry)
		ok.fetched("blob:again")
		ok.apply(chatImageEventDecoded)
		if ok.phase != chatImageLoaded || ok.remounted() != chatImageEffectShowLoaded {
			t.Fatalf("a retried preview that loaded is %+v", ok)
		}
	})

	t.Run("a decode error drops the bytes", func(t *testing.T) {
		m := loadedMemo(t)
		if got := m.discardURL(); got != "blob:gif" || m.objectURL != "" {
			t.Fatalf("discardURL returned %q and left %q", got, m.objectURL)
		}
		m.apply(chatImageEventBroke)
		if m.remounted() != chatImageEffectShowFailed {
			t.Fatal("a preview that failed to decode did not stay failed")
		}
	})

	t.Run("decode without bytes does not load", func(t *testing.T) {
		m := &chatImageMemo{}
		m.apply(chatImageEventDecoded)
		if m.phase != chatImagePending {
			t.Fatalf("decoded event without bytes made the memo %v", m.phase)
		}
	})

	t.Run("a second fetch does not displace the first URL", func(t *testing.T) {
		m := &chatImageMemo{}
		m.fetched("blob:first")
		m.fetched("blob:second")
		if m.objectURL != "blob:first" {
			t.Fatalf("memo owns %q, want the first fetch", m.objectURL)
		}
	})
}

// TestTodo_CHATBUG_009_Browser pins the rendered tile: the same tree on every
// re-render, a placeholder of fixed size while loading, and no timer that
// turns a slow load into "Preview unavailable".
func TestTodo_CHATBUG_009_Browser(t *testing.T) {
	model := Model{Text: func(key string) string { return EnglishCopy()[key] }}
	message := Message{ID: "post", Attachments: []Attachment{{ID: "gif", Name: "confetti.gif", ContentType: "image/gif", Width: 120, Height: 90}}}
	first := renderNode(t, attachments(model, message))
	for i := 0; i < 3; i++ {
		if again := renderNode(t, attachments(model, message)); again != first {
			t.Fatalf("re-render %d changed the attachment markup:\n%s\n%s", i, first, again)
		}
	}
	for _, want := range []string{`class="attachment-image measured gif"`, `data-frame-width="120"`, `<img`} {
		if !strings.Contains(first, want) {
			t.Errorf("tile markup missing %q: %s", want, first)
		}
	}
	for _, banned := range []string{` src=`, `failed`} {
		if strings.Contains(first, banned) {
			t.Errorf("a tile still loading must carry neither a src nor a failed state (%q): %s", banned, first)
		}
	}

	if strings.Contains(Stylesheet, "chat-attachment-stall") {
		t.Error("a timer still turns a src-less preview into Preview unavailable")
	}
	placeholder := `.attachment-image:not(.failed):has(>.attachment-image-open img:not([src])){background:`
	if !strings.Contains(Stylesheet, placeholder) {
		t.Error("a loading tile has no neutral placeholder")
	}
	if !strings.Contains(Stylesheet, `.attachment-fallback{display:none}.attachment-image.failed .attachment-fallback{display:flex}`) {
		t.Error("Preview unavailable is not gated on the failed state alone")
	}
}
