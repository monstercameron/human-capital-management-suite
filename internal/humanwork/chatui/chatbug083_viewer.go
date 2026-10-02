package chatui

import "strings"

// CHATBUG-083. Opening an image gave less than the message did: a dark page
// with the picture at its own size and a Close button. The viewer now names
// the file, who sent it and when; offers Download and Open original; steps
// through the images of the conversation with the arrow keys or the two
// buttons at its sides; and draws a small image larger, never past twice its
// own size, while a large one still fits the window. image_viewer_js.go builds
// it; what does not need a page is here.

// ChatBug083Styles is joined after the viewer's first rules in
// ScopedStylesheet: the viewer hangs on the document body, outside the
// workspace, so it reads the product's own colour tokens.
const ChatBug083Styles = `
.chat-image-viewer{padding:64px 56px 16px}
.chat-image-viewer-media:not(.chat-image-viewer-zoomed) img{width:var(--chat-viewer-w,auto);height:var(--chat-viewer-h,auto)}
.chat-image-viewer-close{border-color:var(--hcm-color-border);background:var(--hcm-color-surface);color:var(--hcm-color-text)}
.chat-image-viewer-caption{position:absolute;z-index:2;top:var(--hcm-space-2);inset-inline-start:var(--hcm-space-3);max-inline-size:calc(100% - 420px);min-inline-size:0;display:grid;gap:2px;color:#fff;font:inherit;font-size:.875rem;line-height:1.3;text-align:start}
.chat-image-viewer-caption>*{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.chat-image-viewer-name{font-weight:650}
.chat-image-viewer-from{font-size:.75rem;color:rgba(255,255,255,.78)}
.chat-image-viewer-tools{position:absolute;z-index:2;top:var(--hcm-space-2);inset-inline-end:calc(var(--hcm-space-3) + 52px);display:flex;align-items:center;gap:8px}
.chat-image-viewer-tools>button{position:static;min-block-size:44px;padding:var(--hcm-space-2) var(--hcm-space-3);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text);font:inherit;font-size:.875rem;box-shadow:var(--hcm-shadow-raised);cursor:pointer}
.chat-image-viewer-tools>button:disabled{cursor:default;opacity:.65}
.chat-image-viewer-tools>button[hidden]{display:none}
.chat-image-viewer-step{position:absolute;z-index:2;top:50%;transform:translateY(-50%);inline-size:44px;block-size:44px;border:1px solid var(--hcm-color-border);border-radius:50%;background:var(--hcm-color-surface);color:var(--hcm-color-text);font:28px/1 sans-serif;box-shadow:var(--hcm-shadow-raised);cursor:pointer}
.chat-image-viewer-step:disabled{opacity:.35;cursor:default}
.chat-image-viewer-step[hidden]{display:none}
.chat-image-viewer-prev{inset-inline-start:6px}
.chat-image-viewer-next{inset-inline-end:6px}
.chat-image-viewer[dir="rtl"] .chat-image-viewer-step{scale:-1 1}
.chat-image-viewer-tools>button:focus-visible,.chat-image-viewer-step:focus-visible{outline:3px solid var(--hcm-color-brand-primary);outline-offset:2px}
@media(max-width:720px){
.chat-image-viewer{padding:64px 8px 72px}
.chat-image-viewer-caption{max-inline-size:calc(100% - 76px)}
.chat-image-viewer-tools{top:auto;bottom:var(--hcm-space-3);inset-inline:var(--hcm-space-3);justify-content:center;flex-wrap:wrap}
.chat-image-viewer-step{top:auto;bottom:var(--hcm-space-3);transform:none}
}
`

// chatbug083Labels is every word the viewer shows or speaks.
type chatbug083Labels struct {
	Title, Close, Download, ActualSize, Fit string
	OpenOriginal, Previous, Next, Position  string
	// ArabicDigits says counts are written as the dates beside them are.
	ArabicDigits bool
}

var chatbug083Copy = map[string][4]string{
	"en-US": {"Open original", "Previous image", "Next image", "{n} of {total}"},
	"de-DE": {"Original öffnen", "Vorheriges Bild", "Nächstes Bild", "{n} von {total}"},
	"ar":    {"فتح الأصل", "الصورة السابقة", "الصورة التالية", "{n} من {total}"},
}

// chatbug083ViewerLabels joins the words the page hands the viewer when it
// opens with the viewer's own, in the language of the page (the workspace's
// lang attribute).
func chatbug083ViewerLabels(locale, title, closeLabel, download, actualSize, fit string) chatbug083Labels {
	language := chatEmojiLocale(locale)
	words := chatbug083Copy[language]
	return chatbug083Labels{Title: title, Close: closeLabel, Download: download, ActualSize: actualSize, Fit: fit,
		OpenOriginal: words[0], Previous: words[1], Next: words[2], Position: words[3], ArabicDigits: language == "ar"}
}

// position fills "{n} of {total}" for the nth of total images.
func (l chatbug083Labels) position(n, total int) string {
	text := strings.NewReplacer("{n}", itoa(n), "{total}", itoa(total)).Replace(l.Position)
	if l.ArabicDigits {
		return arabicDigits(text)
	}
	return text
}

// chatbug083Describe adds who sent an image and when to the data its
// thumbnail carries, for the viewer's caption.
func chatbug083Describe(m Model, msg Message, media map[string]string) {
	media["media-author"] = strings.TrimSpace(msg.Author)
	sent := msg.TimeLabel
	if !msg.SentAt.IsZero() {
		sent = chatux019PinTime(m, msg.SentAt)
	}
	media["media-sent"] = sent
}

// chatImageViewerSize is the size an image is drawn at in the viewer. One
// larger than the room it has is reduced to fit; a smaller one is enlarged to
// fill the room, but never past twice its own size, where it only blurs.
// Unknown sizes answer zero, and the stylesheet's own fitting applies.
func chatImageViewerSize(naturalWidth, naturalHeight, availableWidth, availableHeight int) (width, height int) {
	if naturalWidth <= 0 || naturalHeight <= 0 || availableWidth <= 0 || availableHeight <= 0 {
		return 0, 0
	}
	scale := min(2, float64(availableWidth)/float64(naturalWidth), float64(availableHeight)/float64(naturalHeight))
	return max(1, int(float64(naturalWidth)*scale)), max(1, int(float64(naturalHeight)*scale))
}

// chatImageViewerStep is the direction an arrow key moves through the images:
// the arrow that points to the end of the line goes to the next one, which is
// the left arrow in a right-to-left page.
func chatImageViewerStep(key string, rtl bool) int {
	step := 0
	switch key {
	case "ArrowRight":
		step = 1
	case "ArrowLeft":
		step = -1
	}
	if rtl {
		step = -step
	}
	return step
}

// chatImageViewerNeighbour is the image step places from index among count
// images, when there is one: the first has no previous and the last no next.
func chatImageViewerNeighbour(index, step, count int) (int, bool) {
	next := index + step
	if step == 0 || index < 0 || next < 0 || next >= count {
		return 0, false
	}
	return next, true
}
