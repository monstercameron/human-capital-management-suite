package productui

import (
	"errors"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Document media: images and PDFs attached to a document, and the Export
// menu. The browser client supplies a DocumentMediaPort (the authenticated
// attachment boundary, internal/transport/documentmedia); without one the
// reader shows attachment references as placeholders and nothing uploads.
//
// Markdown references an attachment as attachment:<id>: an image as
// ![alt](attachment:<id>), any other file as a link [name](attachment:<id>),
// which the reader draws as a card.

// DocumentAttachmentScheme prefixes an attachment reference in Markdown.
const DocumentAttachmentScheme = "attachment:"

// Size caps the client checks before sending (the server enforces them).
const (
	docsMediaImageCap = 10 << 20
	docsMediaPDFCap   = 25 << 20
)

// DocumentAttachment is one file attached to a document.
type DocumentAttachment struct {
	ID, DocumentID, Filename, MediaType string
	Size                                int64
	Width, Height, Pages                int
}

// IsImage reports whether the attachment renders inline.
func (a DocumentAttachment) IsImage() bool { return strings.HasPrefix(a.MediaType, "image/") }

// Upload refusals a DocumentMediaPort reports, so the editor can say why.
var (
	ErrDocumentMediaTooLarge    = errors.New("document media: file too large")
	ErrDocumentMediaUnsupported = errors.New("document media: unsupported file type")
	ErrDocumentMediaForbidden   = errors.New("document media: not allowed")
)

// DocumentMediaPort is the client's attachment boundary. Every callback is
// delivered on the render loop. The port must be one stable value for the
// life of the page: the reader memoizes on it.
type DocumentMediaPort struct {
	// List returns the document's attachments (cached per document).
	List func(documentID string, done func([]DocumentAttachment, error))
	// Upload sends one file; progress reports bytes sent of total.
	Upload func(documentID, filename string, content []byte, progress func(sent, total int64), done func(DocumentAttachment, error))
	// ObjectURL resolves an attachment to a blob: URL an <img> can show.
	ObjectURL func(documentID, attachmentID string, done func(url string, err error))
	// Open shows an attachment in a new tab.
	Open func(documentID, attachmentID string, done func(error))
	// Download saves an attachment as a file, without leaving the page.
	Download func(documentID, attachmentID string, done func(error))
	// Export saves the document as txt, md or pdf, without leaving the page.
	Export func(documentID, format string, done func(error))
}

var docsMediaCopy = map[string]map[string]string{
	"en-US": {
		"media_insert_image": "Insert image", "media_attach_pdf": "Attach PDF",
		"media_uploading": "Uploading {name}… {percent}%", "media_uploaded": "{name} added.",
		"media_too_large":   "{name} is too large. Images can be up to 10 MB and PDFs up to 25 MB.",
		"media_unsupported": "{name} can't be added. Use a PNG, JPEG, GIF or WebP image, or a PDF.",
		"media_forbidden":   "You can't add files to this document.", "media_failed": "{name} could not be uploaded. Try again.",
		"media_image_open": "Open image full size: {alt}", "media_image_loading": "Loading image…", "media_image_unavailable": "Image unavailable",
		"media_image": "Image", "media_close": "Close image", "media_attachments": "Attachments",
		"media_open": "Open", "media_download": "Download", "media_open_label": "Open {name}", "media_download_label": "Download {name}",
		"media_pages": "{n} pages", "media_pages_one": "1 page", "media_pdf": "PDF",
		"media_open_failed": "The file could not be opened. Try Download instead.", "media_download_failed": "The file could not be downloaded. Try again.",
		"media_downloaded": "Download started.",
		"export":           "Export", "export_txt": "Plain text (.txt)", "export_md": "Markdown (.md)", "export_pdf": "PDF (.pdf)",
		"export_busy": "Preparing the export…", "export_done": "Export downloaded.", "export_failed": "The export could not be created. Try again.",
		"size_b": "{n} B", "size_kb": "{n} KB", "size_mb": "{n} MB",
	},
	"de-DE": {
		"media_insert_image": "Bild einfügen", "media_attach_pdf": "PDF anhängen",
		"media_uploading": "{name} wird hochgeladen … {percent} %", "media_uploaded": "{name} hinzugefügt.",
		"media_too_large":   "{name} ist zu groß. Bilder dürfen höchstens 10 MB und PDFs höchstens 25 MB groß sein.",
		"media_unsupported": "{name} kann nicht hinzugefügt werden. Verwenden Sie ein PNG-, JPEG-, GIF- oder WebP-Bild oder eine PDF-Datei.",
		"media_forbidden":   "Sie können diesem Dokument keine Dateien hinzufügen.", "media_failed": "{name} konnte nicht hochgeladen werden. Versuchen Sie es erneut.",
		"media_image_open": "Bild in voller Größe öffnen: {alt}", "media_image_loading": "Bild wird geladen …", "media_image_unavailable": "Bild nicht verfügbar",
		"media_image": "Bild", "media_close": "Bild schließen", "media_attachments": "Anhänge",
		"media_open": "Öffnen", "media_download": "Herunterladen", "media_open_label": "{name} öffnen", "media_download_label": "{name} herunterladen",
		"media_pages": "{n} Seiten", "media_pages_one": "1 Seite", "media_pdf": "PDF",
		"media_open_failed": "Die Datei konnte nicht geöffnet werden. Laden Sie sie stattdessen herunter.", "media_download_failed": "Die Datei konnte nicht heruntergeladen werden. Versuchen Sie es erneut.",
		"media_downloaded": "Download gestartet.",
		"export":           "Exportieren", "export_txt": "Nur Text (.txt)", "export_md": "Markdown (.md)", "export_pdf": "PDF (.pdf)",
		"export_busy": "Export wird vorbereitet …", "export_done": "Export heruntergeladen.", "export_failed": "Der Export konnte nicht erstellt werden. Versuchen Sie es erneut.",
		"size_b": "{n} B", "size_kb": "{n} KB", "size_mb": "{n} MB",
	},
	"ar": {
		"media_insert_image": "إدراج صورة", "media_attach_pdf": "إرفاق ملف PDF",
		"media_uploading": "جارٍ رفع {name}… {percent}٪", "media_uploaded": "تمت إضافة {name}.",
		"media_too_large":   "الملف {name} كبير جدًا. الحد الأقصى للصور 10 ميغابايت ولملفات PDF ‏25 ميغابايت.",
		"media_unsupported": "لا يمكن إضافة {name}. استخدم صورة PNG أو JPEG أو GIF أو WebP، أو ملف PDF.",
		"media_forbidden":   "لا يمكنك إضافة ملفات إلى هذا المستند.", "media_failed": "تعذّر رفع {name}. حاول مرة أخرى.",
		"media_image_open": "فتح الصورة بالحجم الكامل: {alt}", "media_image_loading": "جارٍ تحميل الصورة…", "media_image_unavailable": "الصورة غير متاحة",
		"media_image": "صورة", "media_close": "إغلاق الصورة", "media_attachments": "المرفقات",
		"media_open": "فتح", "media_download": "تنزيل", "media_open_label": "فتح {name}", "media_download_label": "تنزيل {name}",
		"media_pages": "{n} صفحات", "media_pages_one": "صفحة واحدة", "media_pdf": "PDF",
		"media_open_failed": "تعذّر فتح الملف. جرّب التنزيل بدلًا من ذلك.", "media_download_failed": "تعذّر تنزيل الملف. حاول مرة أخرى.",
		"media_downloaded": "بدأ التنزيل.",
		"export":           "تصدير", "export_txt": "نص عادي (.txt)", "export_md": "Markdown ‏(.md)", "export_pdf": "PDF ‏(.pdf)",
		"export_busy": "جارٍ تجهيز التصدير…", "export_done": "تم تنزيل الملف المُصدَّر.", "export_failed": "تعذّر إنشاء ملف التصدير. حاول مرة أخرى.",
		"size_b": "{n} بايت", "size_kb": "{n} كيلوبايت", "size_mb": "{n} ميغابايت",
	},
}

// docsMediaText reads the media copy for a locale, falling back to en-US.
func docsMediaText(locale, key string, replacements ...string) string {
	value := docsMediaCopy[locale][key]
	if value == "" {
		value = docsMediaCopy["en-US"][key]
	}
	if len(replacements) > 1 {
		value = strings.NewReplacer(replacements...).Replace(value)
	}
	return value
}

// docsMediaSize writes a byte count for people: B, KB or MB with one
// decimal, in the locale's digits and decimal separator.
func docsMediaSize(locale string, size int64) string {
	key, value := "size_b", strconv.FormatInt(size, 10)
	switch {
	case size >= 1<<20:
		key, value = "size_mb", strconv.FormatFloat(float64(size)/(1<<20), 'f', 1, 64)
	case size >= 1<<10:
		key, value = "size_kb", strconv.FormatFloat(float64(size)/(1<<10), 'f', 1, 64)
	}
	switch {
	case strings.HasPrefix(locale, "de"):
		value = strings.ReplaceAll(value, ".", ",")
	case strings.HasPrefix(locale, "ar"):
		value = strings.ReplaceAll(docsLocaleDigits(locale, value), ".", "٫")
	}
	return docsMediaText(locale, key, "{n}", value)
}

func docsMediaPages(locale string, pages int) string {
	if pages == 1 {
		return docsMediaText(locale, "media_pages_one")
	}
	return docsMediaText(locale, "media_pages", "{n}", docsLocaleDigits(locale, strconv.Itoa(pages)))
}

// docsAttachmentID returns the id in an attachment:<id> destination.
func docsAttachmentID(destination string) (string, bool) {
	id, ok := strings.CutPrefix(strings.TrimSpace(destination), DocumentAttachmentScheme)
	if !ok || id == "" || len(id) > 128 {
		return "", false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", false
		}
	}
	return id, true
}

// docsMediaIcon draws a small line icon from one SVG path.
func docsMediaIcon(path, class string) ui.Node {
	return html.Tag("svg", html.Props{Class: class, Raw: map[string]any{
		"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8",
		"stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", "focusable": "false",
	}}, html.Tag("path", html.Props{Raw: map[string]any{"d": path}}))
}

const (
	docsMediaImagePath    = "M4 5h16v14H4zM4 16l4.5-4.5 3.5 3.5 2.5-2.5L20 17M15.5 9.5h.01"
	docsMediaFilePath     = "M7 3h7l5 5v13H7zM14 3v5h5M9.5 13h5M9.5 16.5h5"
	docsMediaDownloadPath = "M12 4v11M7.5 10.5 12 15l4.5-4.5M5 19h14"
	docsMediaExportPath   = "M12 15V4M7.5 8.5 12 4l4.5 4.5M5 14v5h14v-5"
)

// docsMediaNodes is the reader's rendering of an image or attachment link
// with an attachment:<id> destination; ok is false for anything else.
func docsMediaImageNode(view View, destination, alt string) (ui.Node, bool) {
	id, ok := docsAttachmentID(destination)
	if !ok {
		return nil, false
	}
	return ui.CreateElement(docsAttachmentImage, docsAttachmentImageProps{Locale: view.Locale.Resolved, DocumentID: view.DocumentID, ID: id, Alt: alt, Media: view.DocumentMedia}), true
}

func docsMediaLinkNode(view View, destination, label string) (ui.Node, bool) {
	id, ok := docsAttachmentID(destination)
	if !ok {
		return nil, false
	}
	return ui.CreateElement(docsAttachmentCard, docsAttachmentCardProps{Locale: view.Locale.Resolved, DocumentID: view.DocumentID, ID: id, Label: label, Media: view.DocumentMedia}), true
}

type docsAttachmentImageProps struct {
	Locale, DocumentID, ID, Alt string
	Media                       *DocumentMediaPort
}

// docsAttachmentImage is an attached image in the reader: a placeholder
// until it scrolls near the viewport, then the image itself (responsive,
// lazily decoded, with its alt text) inside a button that opens it full
// size in a dialog.
func docsAttachmentImage(props docsAttachmentImageProps) ui.Node {
	ref := ui.UseDOMRef()
	near := ui.UseIntersection(ref)
	wanted := ui.UseRef(false)
	source := ui.UseState("")
	failed := ui.UseState(false)
	open := ui.UseState(false)
	if near {
		wanted.Set(true)
	}
	ui.UseEffect(func() func() {
		if !wanted.Get() || source.Get() != "" || failed.Get() || props.Media == nil || props.Media.ObjectURL == nil {
			return nil
		}
		props.Media.ObjectURL(props.DocumentID, props.ID, func(url string, err error) {
			if err != nil || url == "" {
				failed.Set(true)
				return
			}
			source.Set(url)
		})
		return nil
	}, props.DocumentID, props.ID, wanted.Get())
	openImage := ui.UseEvent(func(ui.MouseEvent) { open.Set(true) })
	closeImage := ui.UseEvent(func(ui.MouseEvent) { open.Set(false) })
	keydown := ui.UseEvent(func(ui.KeyboardEvent) {})
	ui.UseEffect(func() func() {
		if !open.Get() {
			return nil
		}
		return docsListenEscape(func() { open.Set(false) })
	}, open.Get())
	useDocsModal(open.Get(), "docs-media-lightbox", ".docs-dialog-close")

	alt := strings.TrimSpace(props.Alt)
	name := alt
	if name == "" {
		name = docsMediaText(props.Locale, "media_image")
	}
	var body ui.Node
	switch {
	case source.Get() != "":
		body = html.Button(html.Props{Class: "docs-media-open", Type: "button", OnClick: openImage, Aria: map[string]string{"label": docsMediaText(props.Locale, "media_image_open", "{alt}", name), "haspopup": "dialog"}},
			html.Img(html.Props{Class: "docs-media-img", Src: source.Get(), Alt: alt, Loading: "lazy", Raw: map[string]any{"decoding": "async"}}))
	case failed.Get() || props.Media == nil:
		body = html.Span(html.Props{Class: "docs-media-placeholder is-unavailable", Role: "img", Aria: map[string]string{"label": name + " — " + docsMediaText(props.Locale, "media_image_unavailable")}},
			docsMediaIcon(docsMediaImagePath, "docs-media-placeholder-icon"), html.Span(html.Props{}, ui.Text(docsMediaText(props.Locale, "media_image_unavailable"))))
	default:
		body = html.Span(html.Props{Class: "docs-media-placeholder", Role: "img", Aria: map[string]string{"label": name, "busy": "true"}},
			docsMediaIcon(docsMediaImagePath, "docs-media-placeholder-icon"), html.Span(html.Props{}, ui.Text(docsMediaText(props.Locale, "media_image_loading"))))
	}
	children := []ui.Node{body}
	if open.Get() && source.Get() != "" {
		children = append(children, docsDialog("docs-media-lightbox", name, docsMediaText(props.Locale, "media_close"), closeImage, keydown,
			html.Img(html.Props{Class: "docs-lightbox-img", Src: source.Get(), Alt: alt})))
	}
	return html.Span(html.WithProps(html.Props{Class: "docs-media-figure", Data: map[string]string{"attachment-id": props.ID}}, html.Ref(ref)), children...)
}

type docsAttachmentCardProps struct {
	Locale, DocumentID, ID, Label string
	Media                         *DocumentMediaPort
}

// docsAttachmentCard is a non-image attachment in the reader: its name,
// size and page count, with Open and Download.
func docsAttachmentCard(props docsAttachmentCardProps) ui.Node {
	info := ui.UseState(DocumentAttachment{})
	status := ui.UseState("")
	ui.UseEffect(func() func() {
		if props.Media == nil || props.Media.List == nil {
			return nil
		}
		props.Media.List(props.DocumentID, func(items []DocumentAttachment, err error) {
			for _, item := range items {
				if item.ID == props.ID {
					info.Set(item)
				}
			}
		})
		return nil
	}, props.DocumentID, props.ID)
	act := ui.UseEvent(func(event ui.MouseEvent) {
		action, _ := docsMediaEventTarget(event)
		docsMediaAct(props.Media, action, props.DocumentID, props.ID, props.Locale, status.Set)
	})
	item := info.Get()
	if item.ID == "" {
		item = DocumentAttachment{ID: props.ID, Filename: props.Label}
	}
	return html.Span(html.Props{Class: "docs-media-card", Data: map[string]string{"attachment-id": props.ID}, OnClick: act}, docsAttachmentRow(props.Locale, item, props.Label, props.Media != nil, status.Get())...)
}

// docsAttachmentRow is the shared body of an attachment card and a row in
// the Attachments section.
func docsAttachmentRow(locale string, item DocumentAttachment, label string, live bool, status string) []ui.Node {
	name := strings.TrimSpace(item.Filename)
	if name == "" {
		name = strings.TrimSpace(label)
	}
	if name == "" {
		name = item.ID
	}
	meta := []string{}
	if item.MediaType == "application/pdf" {
		meta = append(meta, docsMediaText(locale, "media_pdf"))
	}
	if item.Size > 0 {
		meta = append(meta, docsMediaSize(locale, item.Size))
	}
	if item.Pages > 0 {
		meta = append(meta, docsMediaPages(locale, item.Pages))
	}
	icon := docsMediaFilePath
	if item.IsImage() {
		icon = docsMediaImagePath
	}
	nodes := []ui.Node{
		docsMediaIcon(icon, "docs-media-card-icon"),
		html.Span(html.Props{Class: "docs-media-card-body"},
			html.Span(html.Props{Class: "docs-media-card-name", Dir: "auto"}, ui.Text(name)),
			html.Span(html.Props{Class: "docs-media-card-meta"}, ui.Text(strings.Join(meta, " · ")))),
	}
	if live {
		nodes = append(nodes, html.Span(html.Props{Class: "docs-media-card-actions"},
			html.Button(html.Props{Class: "button secondary docs-media-action", Type: "button", Aria: map[string]string{"label": docsMediaText(locale, "media_open_label", "{name}", name)}, Data: map[string]string{"media-action": "open"}}, ui.Text(docsMediaText(locale, "media_open"))),
			html.Button(html.Props{Class: "button secondary docs-media-action", Type: "button", Aria: map[string]string{"label": docsMediaText(locale, "media_download_label", "{name}", name)}, Data: map[string]string{"media-action": "download"}}, docsMediaIcon(docsMediaDownloadPath, "docs-button-icon"), ui.Text(docsMediaText(locale, "media_download"))),
		))
	}
	nodes = append(nodes, html.Span(html.Props{Class: "docs-media-card-status", Role: "status", Aria: map[string]string{"live": "polite"}}, ui.Text(status)))
	return nodes
}

// docsMediaAct runs an Open or Download action and reports failures.
func docsMediaAct(media *DocumentMediaPort, action, documentID, id, locale string, report func(string)) {
	if media == nil {
		return
	}
	switch action {
	case "open":
		if media.Open != nil {
			report("")
			media.Open(documentID, id, func(err error) {
				if err != nil {
					report(docsMediaText(locale, "media_open_failed"))
				}
			})
		}
	case "download":
		if media.Download != nil {
			report("")
			media.Download(documentID, id, func(err error) {
				if err != nil {
					report(docsMediaText(locale, "media_download_failed"))
					return
				}
				report(docsMediaText(locale, "media_downloaded"))
			})
		}
	}
}

type docsAttachmentsSectionProps struct {
	Locale, DocumentID, VersionID string
	Media                         *DocumentMediaPort
}

// docsAttachmentsSection lists every attachment of the open document at
// the end of the reader. It stays hidden (and empty) until there is
// something to list, so the server render and the first client render
// agree.
func docsAttachmentsSection(props docsAttachmentsSectionProps) ui.Node {
	items := ui.UseState([]DocumentAttachment(nil))
	status := ui.UseState("")
	ui.UseEffect(func() func() {
		if props.Media == nil || props.Media.List == nil {
			return nil
		}
		props.Media.List(props.DocumentID, func(list []DocumentAttachment, err error) {
			if err == nil {
				items.Set(list)
			}
		})
		return nil
	}, props.DocumentID, props.VersionID)
	act := ui.UseEvent(func(event ui.MouseEvent) {
		action, id := docsMediaEventTarget(event)
		if id != "" {
			docsMediaAct(props.Media, action, props.DocumentID, id, props.Locale, status.Set)
		}
	})
	rows := make([]ui.Node, 0, len(items.Get()))
	for _, item := range items.Get() {
		rows = append(rows, html.Li(html.Props{Key: item.ID, Class: "docs-media-card", Data: map[string]string{"attachment-id": item.ID}}, docsAttachmentRow(props.Locale, item, "", props.Media != nil, "")...))
	}
	return html.Section(html.Props{Class: "docs-attachments", Hidden: len(rows) == 0, Aria: map[string]string{"labelledby": "docs-attachments-heading"}, OnClick: act},
		html.H2(html.Props{ID: "docs-attachments-heading"}, ui.Text(docsMediaText(props.Locale, "media_attachments"))),
		html.Ul(html.Props{Class: "docs-attachments-list"}, rows...),
		html.P(html.Props{Class: "docs-media-card-status", Role: "status", Aria: map[string]string{"live": "polite"}}, ui.Text(status.Get())),
	)
}

type docsExportMenuProps struct {
	Locale, DocumentID string
	Media              *DocumentMediaPort
	Notify             func(string)
}

// docsExportMenu is the Export group in the document's actions menu: plain
// text, Markdown or PDF, each saved as a download without leaving the page.
func docsExportMenu(props docsExportMenuProps) ui.Node {
	pick := ui.UseEvent(func(event ui.MouseEvent) {
		format, _ := docsMediaEventTarget(event)
		if props.Media == nil || props.Media.Export == nil || (format != "txt" && format != "md" && format != "pdf") {
			return
		}
		notify := props.Notify
		if notify == nil {
			notify = func(string) {}
		}
		notify(docsMediaText(props.Locale, "export_busy"))
		props.Media.Export(props.DocumentID, format, func(err error) {
			if err != nil {
				notify(docsMediaText(props.Locale, "export_failed"))
				return
			}
			notify(docsMediaText(props.Locale, "export_done"))
		})
	})
	item := func(format string) ui.Node {
		return html.Button(html.Props{Key: format, Class: "docs-menu-item", Type: "button", Data: map[string]string{"media-action": format}},
			docsMediaIcon(docsMediaExportPath, "docs-menu-icon"), html.Span(html.Props{}, ui.Text(docsMediaText(props.Locale, "export_"+format))))
	}
	return html.Div(html.Props{Class: "docs-export-menu", Role: "group", Aria: map[string]string{"labelledby": "docs-export-label"}, OnClick: pick},
		html.Span(html.Props{ID: "docs-export-label", Class: "docs-menu-label"}, ui.Text(docsMediaText(props.Locale, "export"))),
		item("txt"), item("md"), item("pdf"))
}

// docsEditorMediaUI is what the split editor shows for attachments: two
// toolbar buttons and a status line for uploads.
type docsEditorMediaUI struct {
	Tools  []ui.Node
	Status ui.Node
}

type docsEditorUploadStatus struct {
	text  string
	alert bool
}

// useDocsEditorMedia adds "Insert image" and "Attach PDF" to the editor:
// the file picker, drag-and-drop and paste of files into either pane,
// upload progress and errors announced in a live region, and the finished
// attachment inserted as Markdown at the caret. Every hook runs on every
// render, whether or not a media port is present.
func useDocsEditorMedia(c *docsEditorController, locale, documentID string, media *DocumentMediaPort) docsEditorMediaUI {
	status := ui.UseState(docsEditorUploadStatus{})
	upload := ui.UseRef[func(string, []byte)](nil)
	upload.Set(func(name string, content []byte) {
		docsEditorUpload(c, locale, documentID, media, name, content, status.Set)
	})
	ui.UseEffect(func() func() {
		if media == nil {
			return nil
		}
		return docsEditorMediaListen(func(name string, content []byte) {
			if fn := upload.Get(); fn != nil {
				fn(name, content)
			}
		})
	}, documentID, media != nil)
	pickImage := ui.UseEvent(func(ui.MouseEvent) {
		ui.PickFile("image/png,image/jpeg,image/gif,image/webp", func(file ui.PickedFile) { upload.Get()(file.Name, file.Data) })
	})
	pickPDF := ui.UseEvent(func(ui.MouseEvent) {
		ui.PickFile("application/pdf,.pdf", func(file ui.PickedFile) { upload.Get()(file.Name, file.Data) })
	})
	out := docsEditorMediaUI{}
	current := status.Get()
	role, class := "status", "docs-editor-media-status"
	if current.alert {
		role, class = "alert", class+" is-alert"
	}
	out.Status = html.P(html.Props{Key: "media-status", ID: "docs-editor-media-status", Class: class, Raw: map[string]any{"role": role, "aria-live": "polite"}}, ui.Text(current.text))
	if media == nil || media.Upload == nil {
		return out
	}
	out.Tools = []ui.Node{
		html.Span(html.Props{Key: "sep-media", Class: "docs-editor-sep", Raw: map[string]any{"aria-hidden": "true"}}),
		html.Button(html.Props{Key: "media-image", ID: "docs-editor-insert-image", Class: "docs-editor-tool", Type: "button", Title: docsMediaText(locale, "media_insert_image"), OnClick: pickImage, Aria: map[string]string{"label": docsMediaText(locale, "media_insert_image"), "describedby": "docs-editor-media-status"}},
			docsEditorIcon(docsMediaImagePath)),
		html.Button(html.Props{Key: "media-pdf", ID: "docs-editor-attach-pdf", Class: "docs-editor-tool", Type: "button", Title: docsMediaText(locale, "media_attach_pdf"), OnClick: pickPDF, Aria: map[string]string{"label": docsMediaText(locale, "media_attach_pdf"), "describedby": "docs-editor-media-status"}},
			docsEditorIcon(docsMediaFilePath)),
	}
	return out
}

// docsEditorUpload checks, sends and inserts one file: an oversized file is
// refused before it is sent, progress is reported in steps of ten percent,
// a refusal says why, and the finished attachment goes in at the caret.
func docsEditorUpload(c *docsEditorController, locale, documentID string, media *DocumentMediaPort, name string, content []byte, report func(docsEditorUploadStatus)) {
	name = docsMediaDisplayName(name)
	if media == nil || media.Upload == nil {
		return
	}
	if len(content) > docsMediaPDFCap || (!docsMediaLooksPDF(content) && len(content) > docsMediaImageCap) {
		report(docsEditorUploadStatus{text: docsMediaText(locale, "media_too_large", "{name}", name), alert: true})
		return
	}
	report(docsEditorUploadStatus{text: docsMediaText(locale, "media_uploading", "{name}", name, "{percent}", docsLocaleDigits(locale, "0"))})
	lastPercent := -1
	media.Upload(documentID, name, content, func(sent, total int64) {
		if total <= 0 {
			return
		}
		percent := int(sent * 100 / total)
		if percent/10 == lastPercent/10 {
			return
		}
		lastPercent = percent
		report(docsEditorUploadStatus{text: docsMediaText(locale, "media_uploading", "{name}", name, "{percent}", docsLocaleDigits(locale, strconv.Itoa(percent)))})
	}, func(attachment DocumentAttachment, err error) {
		if err != nil {
			key := "media_failed"
			switch {
			case errors.Is(err, ErrDocumentMediaTooLarge):
				key = "media_too_large"
			case errors.Is(err, ErrDocumentMediaUnsupported):
				key = "media_unsupported"
			case errors.Is(err, ErrDocumentMediaForbidden):
				key = "media_forbidden"
			}
			report(docsEditorUploadStatus{text: docsMediaText(locale, key, "{name}", name), alert: true})
			return
		}
		if c.flush != nil {
			c.flush()
		}
		capture := docsEditorCaptureSelection(c)
		next, start, end := docsMediaInsertBlock(capture.Markdown, capture.Start, capture.End, docsMediaMarkdown(attachment, name))
		docsEditorCommit(c, next, start, end, capture.Pane)
		report(docsEditorUploadStatus{text: docsMediaText(locale, "media_uploaded", "{name}", name)})
	})
}

// docsMediaInsertBlock puts an attachment reference in a paragraph of its
// own at the caret (replacing any selection), so an image or card never
// runs into the text around it. The caret lands after it.
func docsMediaInsertBlock(value string, start, end int, fragment string) (string, int, int) {
	start = docsEditorClamp(value, start)
	end = max(start, docsEditorClamp(value, end))
	before := strings.TrimRight(value[:start], " \n")
	after := strings.TrimLeft(value[end:], " \n")
	if before != "" {
		before += "\n\n"
	}
	next := before + fragment
	caret := len(next)
	if after != "" {
		next += "\n\n" + after
	} else {
		next += "\n"
	}
	return next, caret, caret
}

// docsMediaMarkdown is the Markdown an uploaded attachment inserts: an
// image with its name (less the extension) as alt text, or a link.
func docsMediaMarkdown(attachment DocumentAttachment, name string) string {
	label := strings.NewReplacer("[", "", "]", "", "\n", " ").Replace(strings.TrimSpace(attachment.Filename))
	if label == "" {
		label = strings.NewReplacer("[", "", "]", "").Replace(name)
	}
	if attachment.IsImage() {
		alt := label
		if dot := strings.LastIndex(alt, "."); dot > 0 {
			alt = alt[:dot]
		}
		return "![" + alt + "](" + DocumentAttachmentScheme + attachment.ID + ")"
	}
	return "[" + label + "](" + DocumentAttachmentScheme + attachment.ID + ")"
}

func docsMediaDisplayName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimSpace(name[strings.LastIndex(name, "/")+1:])
	if name == "" {
		return "file"
	}
	return name
}

func docsMediaLooksPDF(content []byte) bool {
	return len(content) >= 5 && string(content[:5]) == "%PDF-"
}

// docsMediaStylesheet styles attachments in the reader and the editor.
func docsMediaStylesheet() string {
	return `
.docs-media-figure{display:block;margin-block:var(--hcm-space-2,.5rem);max-inline-size:100%}
.docs-media-open{display:block;padding:0;margin:0;border:0;background:none;cursor:zoom-in;max-inline-size:100%;border-radius:8px}
.docs-media-open:focus-visible,.docs-media-action:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-media-img{display:block;max-inline-size:100%;block-size:auto;border-radius:8px;border:1px solid var(--line);background:var(--surface)}
.docs-media-placeholder{display:flex;align-items:center;justify-content:center;gap:.5rem;min-block-size:6rem;max-inline-size:32rem;padding:1rem;border:1px dashed var(--line);border-radius:8px;background:var(--soft);color:var(--muted);text-align:center}
.docs-media-placeholder-icon{inline-size:1.5rem;block-size:1.5rem;flex:none}
.docs-media-card{display:flex;align-items:center;gap:.75rem;max-inline-size:36rem;margin-block:.5rem;padding:.625rem .875rem;border:1px solid var(--line);border-radius:10px;background:var(--surface);color:var(--ink);text-align:start}
.docs-media-card-icon{flex:none;inline-size:2rem;block-size:2rem;color:var(--accent)}
.docs-media-card-body{display:flex;flex:1 1 10rem;flex-direction:column;min-inline-size:0}
.docs-media-card-name{font-weight:600;overflow-wrap:anywhere}
.docs-media-card-meta{color:var(--muted);font-size:.85em}
.docs-media-card-actions{display:flex;flex-wrap:wrap;gap:.375rem}
.docs-media-card-status:empty{display:none}
.docs-media-card-status{flex-basis:100%;margin:0;color:var(--muted);font-size:.85em}
.docs-media-action{display:inline-flex;align-items:center;gap:.25rem;min-block-size:2rem;padding-block:.25rem;padding-inline:.625rem}
.docs-attachments{margin-block-start:2rem;padding-block-start:1rem;border-block-start:1px solid var(--line)}
.docs-attachments h2{margin:0 0 .5rem;font-size:1rem}
.docs-attachments-list{display:grid;gap:.5rem;margin:0;padding:0;list-style:none}
.docs-attachments-list .docs-media-card{flex-wrap:wrap;margin:0}
#docs-media-lightbox{inline-size:min(96vw,1200px);max-inline-size:96vw}
.docs-lightbox-img{display:block;max-inline-size:100%;max-block-size:78vh;margin-inline:auto;object-fit:contain}
.docs-export-menu{display:flex;flex-direction:column;border-block-start:1px solid var(--line);margin-block-start:.25rem;padding-block-start:.25rem}
.docs-menu-label{padding:.25rem .75rem;color:var(--muted);font-size:.8em;font-weight:600;text-align:start}
.docs-editor-media-status{margin:0;min-block-size:1.25em;color:var(--muted);font-size:.85em;text-align:start}
.docs-editor-media-status.is-alert{color:var(--hcm-color-danger,var(--danger,var(--ink)))}
.docs-editor.is-dropping .docs-editor-panes{outline:2px dashed var(--accent);outline-offset:4px}
@media (max-width:480px){.docs-media-card{flex-wrap:wrap}.docs-media-card-actions{flex-basis:100%}}
@media (prefers-reduced-motion:reduce){.docs-media-open,.docs-media-img{transition:none}}
`
}
