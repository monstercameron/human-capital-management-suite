package chatui

// Issue 3917, image viewer entry point: View message.
//
// The viewer names an image, its sender and its time, but not what the sender
// said about it. View message closes the viewer and brings the message the
// image was sent in into view, with a Back to image bar that reopens the viewer
// on the same image. If the message has gone from the page meanwhile, the viewer
// stays open and says so, without showing anything of the message.
// image_viewer_js.go builds the button; issue3917_viewer_message_js.go does the
// move and the return bar; the words are here.

var issue3917ViewerCopy = map[string][3]string{
	"view_message": {"View message", "Nachricht anzeigen", "عرض الرسالة"},
	"back":         {"Back to image", "Zurück zum Bild", "العودة إلى الصورة"},
	"bar":          {"Return to the image viewer", "Zurück zur Bildansicht", "العودة إلى عارض الصور"},
	"dismiss":      {"Dismiss", "Schließen", "إغلاق"},
	"gone":         {"That message is no longer available.", "Diese Nachricht ist nicht mehr verfügbar.", "هذه الرسالة لم تعد متاحة."},
}

// issue3917ViewerText is one word of the viewer's View message flow in a locale.
func issue3917ViewerText(locale, key string) string {
	copy := issue3917ViewerCopy[key]
	return chatbug039Text("chat.issue3917.viewer."+key, copy[chatbug039LocaleIndex(locale)], copy[0])
}

const issue3917ViewerStyles = `.chat-image-viewer-note{position:absolute;z-index:2;inset-inline:var(--hcm-space-3);bottom:var(--hcm-space-3);margin-block:0;margin-inline:auto;inline-size:max-content;max-inline-size:calc(100% - 2*var(--hcm-space-3));padding:6px 12px;border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text);text-align:center;font-size:.875rem}.chat-image-viewer-note:empty{display:none}` +
	`.chat-image-return{position:fixed;z-index:60;inset-inline:var(--hcm-space-3);bottom:var(--hcm-space-3);margin-inline:auto;inline-size:max-content;max-inline-size:calc(100% - 2*var(--hcm-space-3));display:flex;align-items:center;gap:8px;padding:8px 12px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text);box-shadow:var(--hcm-shadow-raised)}` +
	`.chat-image-return button{min-block-size:44px;min-inline-size:44px;padding:0 12px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:inherit;font:inherit;cursor:pointer}` +
	`.chat-image-return button:focus-visible{outline:3px solid var(--hcm-color-focus);outline-offset:2px}`
