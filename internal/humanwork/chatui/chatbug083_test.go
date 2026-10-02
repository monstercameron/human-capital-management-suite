package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
	"time"
)

// TestTodo_CHATBUG_083 holds what the image viewer decides without a page:
// the size a picture is drawn at, where the arrow keys go, its words in the
// three languages, and what the thumbnail tells it about the image.
func TestTodo_CHATBUG_083(t *testing.T) {
	t.Run("a small image is enlarged to at most twice its size, a large one fits", func(t *testing.T) {
		for _, tc := range []struct {
			name                 string
			w, h, availW, availH int
			wantW, wantH         int
		}{
			{"the 160 px demo file", 160, 160, 1200, 800, 320, 320},
			{"small and wide", 300, 100, 1200, 800, 600, 200},
			{"enlarged only as far as the window allows", 500, 500, 1200, 800, 800, 800},
			{"exactly the room it has", 1200, 800, 1200, 800, 1200, 800},
			{"reduced by width", 2400, 800, 1200, 800, 1200, 400},
			{"reduced by height", 1000, 2000, 1200, 800, 400, 800},
			{"a phone", 160, 160, 300, 500, 300, 300},
			{"a sliver never rounds to nothing", 4000, 2, 1000, 800, 1000, 1},
			{"unknown size", 0, 160, 1200, 800, 0, 0},
			{"unknown room", 160, 160, 0, 800, 0, 0},
		} {
			if w, h := chatImageViewerSize(tc.w, tc.h, tc.availW, tc.availH); w != tc.wantW || h != tc.wantH {
				t.Errorf("%s: %dx%d in %dx%d is drawn %dx%d, want %dx%d", tc.name, tc.w, tc.h, tc.availW, tc.availH, w, h, tc.wantW, tc.wantH)
			}
		}
	})

	t.Run("the arrow keys step through the images and stop at the ends", func(t *testing.T) {
		for _, tc := range []struct {
			key  string
			rtl  bool
			want int
		}{{"ArrowRight", false, 1}, {"ArrowLeft", false, -1}, {"ArrowRight", true, -1}, {"ArrowLeft", true, 1}, {"ArrowUp", false, 0}, {"Enter", true, 0}} {
			if got := chatImageViewerStep(tc.key, tc.rtl); got != tc.want {
				t.Errorf("%s (rtl=%v) steps %d, want %d", tc.key, tc.rtl, got, tc.want)
			}
		}
		for _, tc := range []struct {
			index, step, count int
			to                 int
			ok                 bool
		}{{0, 1, 3, 1, true}, {1, 1, 3, 2, true}, {2, 1, 3, 0, false}, {0, -1, 3, 0, false}, {2, -1, 3, 1, true}, {0, 1, 1, 0, false}, {-1, 1, 3, 0, false}, {1, 0, 3, 0, false}} {
			if to, ok := chatImageViewerNeighbour(tc.index, tc.step, tc.count); to != tc.to || ok != tc.ok {
				t.Errorf("from %d by %d of %d: %d %v, want %d %v", tc.index, tc.step, tc.count, to, ok, tc.to, tc.ok)
			}
		}
	})

	t.Run("its words in the reader's language", func(t *testing.T) {
		for locale, want := range map[string][4]string{
			"en-US": {"Open original", "Previous image", "Next image", "2 of 5"},
			"de-DE": {"Original öffnen", "Vorheriges Bild", "Nächstes Bild", "2 von 5"},
			"ar":    {"فتح الأصل", "الصورة السابقة", "الصورة التالية", "٢ من ٥"},
			"fr-FR": {"Open original", "Previous image", "Next image", "2 of 5"},
		} {
			labels := chatbug083ViewerLabels(locale, "Image viewer", "Close image viewer", "Download", "Show image at actual size", "Fit image to screen")
			if got := [4]string{labels.OpenOriginal, labels.Previous, labels.Next, labels.position(2, 5)}; got != want {
				t.Errorf("%s: %q, want %q", locale, got, want)
			}
			if labels.Title != "Image viewer" || labels.Close != "Close image viewer" || labels.Download != "Download" || labels.ActualSize == "" || labels.Fit == "" {
				t.Errorf("%s: the page's own words were lost: %+v", locale, labels)
			}
		}
	})

	t.Run("the thumbnail says who sent the image and when", func(t *testing.T) {
		m := chatux022Model()
		today := time.Now()
		msg := Message{ID: "m1", AuthorID: "loretta", Author: " Loretta Haynes ", SentAt: today, TimeLabel: "9:41 AM",
			Attachments: []Attachment{{ID: "a1", Name: "floor-plan.png", ContentType: "image/png", Width: 160, Height: 160, Bytes: 2048}}}
		tile := stdhtml.UnescapeString(renderNode(t, attachments(m, msg)))
		for _, want := range []string{`data-media-name="floor-plan.png"`, `data-media-author="Loretta Haynes"`, `data-media-sent="` + chatux019PinTime(m, today) + `"`, `data-media-width="160"`, `data-media-height="160"`, `data-action="view-image"`} {
			if !strings.Contains(tile, want) {
				t.Errorf("the thumbnail lacks %s: %s", want, tile)
			}
		}
		// An older message says the day as well as the time; one with no time
		// recorded keeps the label the timeline shows.
		media := map[string]string{}
		old := time.Date(2026, 3, 9, 14, 5, 0, 0, time.Local)
		chatbug083Describe(m, Message{Author: "Walt Brennan", SentAt: old}, media)
		if media["media-sent"] != formatShortDate(m.Locale, old)+", "+chat5Clock(m.Locale, old) || media["media-author"] != "Walt Brennan" {
			t.Errorf("an older image is described %+v", media)
		}
		chatbug083Describe(m, Message{TimeLabel: "9:41 AM"}, media)
		if media["media-sent"] != "9:41 AM" || media["media-author"] != "" {
			t.Errorf("an image with no recorded time is described %+v", media)
		}
	})

	t.Run("styles", func(t *testing.T) {
		sheet := ScopedStylesheet()
		base, mine := strings.Index(sheet, ".chat-image-viewer img{display:block;"), strings.Index(sheet, ".chat-image-viewer-media:not(.chat-image-viewer-zoomed) img{width:var(--chat-viewer-w,auto);height:var(--chat-viewer-h,auto)}")
		if base < 0 || mine < base {
			t.Fatalf("the viewer's drawn size (at %d) is not in the served stylesheet after the viewer's first rules (at %d)", mine, base)
		}
		scope := strings.Index(sheet, "@scope (.chat-workspace){")
		if end := strings.LastIndex(sheet[:mine], "}}"); scope < 0 || end < scope {
			t.Fatal("the viewer hangs on the document body; its rules must be outside the workspace scope")
		}
		for _, rule := range []string{".chat-image-viewer-caption{", ".chat-image-viewer-tools{", ".chat-image-viewer-step{", ".chat-image-viewer-prev{inset-inline-start:6px}", ".chat-image-viewer-next{inset-inline-end:6px}", `.chat-image-viewer[dir="rtl"] .chat-image-viewer-step{scale:-1 1}`, "@media(max-width:720px){"} {
			if !strings.Contains(ChatBug083Styles, rule) {
				t.Errorf("the viewer has no rule %s", rule)
			}
		}
	})
}
