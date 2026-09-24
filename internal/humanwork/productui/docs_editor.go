package productui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsEditorCopy is the split editor's vocabulary. Keys the plain editor
// already had (edit_save, edit_failed, title…) come from docsCopy.
var docsEditorCopy = map[string]map[string]string{
	"en-US": {
		"editor_label": "Document editor", "editor_toolbar": "Formatting", "editor_view": "View",
		"editor_view_split": "Split", "editor_view_source": "Markdown", "editor_view_rich": "Formatted",
		"editor_source": "Markdown", "editor_rich": "Formatted", "editor_rich_label": "Formatted document",
		"editor_source_help": "Edit the Markdown or the formatted text; each side follows the other. Ctrl+S saves.",
		"editor_style":       "Text style", "editor_paragraph": "Paragraph", "editor_h1": "Heading 1", "editor_h2": "Heading 2", "editor_h3": "Heading 3",
		"editor_bold": "Bold (Ctrl+B)", "editor_italic": "Italic (Ctrl+I)", "editor_strike": "Strikethrough", "editor_code": "Inline code",
		"editor_link": "Link (Ctrl+K)", "editor_ul": "Bulleted list", "editor_ol": "Numbered list", "editor_task": "Task list",
		"editor_quote": "Quote", "editor_codeblock": "Code block", "editor_table": "Insert table", "editor_hr": "Horizontal rule",
		"editor_undo": "Undo (Ctrl+Z)", "editor_redo": "Redo (Ctrl+Shift+Z)",
		"editor_link_url": "Link address", "editor_link_apply": "Add link", "editor_link_cancel": "Cancel",
		"editor_link_invalid": "Use a web address (https://…), a path starting with /, a #section or a doc: reference.",
		"editor_table_column": "Column",
		"editor_counts":       "{words} words · {chars} characters", "editor_counts_one": "1 word · {chars} characters",
		"editor_dirty": "Unsaved changes", "editor_discard_prompt": "Discard your unsaved changes?", "editor_discard": "Discard changes", "editor_keep": "Keep editing",
		"editor_required": "Add a title and some text before saving.",
	},
	"de-DE": {
		"editor_label": "Dokumenteditor", "editor_toolbar": "Formatierung", "editor_view": "Ansicht",
		"editor_view_split": "Geteilt", "editor_view_source": "Markdown", "editor_view_rich": "Formatiert",
		"editor_source": "Markdown", "editor_rich": "Formatiert", "editor_rich_label": "Formatiertes Dokument",
		"editor_source_help": "Bearbeiten Sie das Markdown oder den formatierten Text; beide Seiten bleiben gleich. Strg+S speichert.",
		"editor_style":       "Textformat", "editor_paragraph": "Absatz", "editor_h1": "Überschrift 1", "editor_h2": "Überschrift 2", "editor_h3": "Überschrift 3",
		"editor_bold": "Fett (Strg+B)", "editor_italic": "Kursiv (Strg+I)", "editor_strike": "Durchgestrichen", "editor_code": "Code im Text",
		"editor_link": "Link (Strg+K)", "editor_ul": "Aufzählung", "editor_ol": "Nummerierte Liste", "editor_task": "Aufgabenliste",
		"editor_quote": "Zitat", "editor_codeblock": "Codeblock", "editor_table": "Tabelle einfügen", "editor_hr": "Trennlinie",
		"editor_undo": "Rückgängig (Strg+Z)", "editor_redo": "Wiederholen (Strg+Umschalt+Z)",
		"editor_link_url": "Linkadresse", "editor_link_apply": "Link einfügen", "editor_link_cancel": "Abbrechen",
		"editor_link_invalid": "Verwenden Sie eine Webadresse (https://…), einen Pfad mit /, einen #Abschnitt oder einen doc:-Verweis.",
		"editor_table_column": "Spalte",
		"editor_counts":       "{words} Wörter · {chars} Zeichen", "editor_counts_one": "1 Wort · {chars} Zeichen",
		"editor_dirty": "Nicht gespeicherte Änderungen", "editor_discard_prompt": "Nicht gespeicherte Änderungen verwerfen?", "editor_discard": "Änderungen verwerfen", "editor_keep": "Weiter bearbeiten",
		"editor_required": "Geben Sie vor dem Speichern einen Titel und etwas Text ein.",
	},
	"ar": {
		"editor_label": "محرر المستند", "editor_toolbar": "التنسيق", "editor_view": "العرض",
		"editor_view_split": "مقسّم", "editor_view_source": "Markdown", "editor_view_rich": "منسّق",
		"editor_source": "Markdown", "editor_rich": "منسّق", "editor_rich_label": "المستند المنسّق",
		"editor_source_help": "حرّر نص Markdown أو النص المنسّق؛ يتبع كل جانب الآخر. Ctrl+S للحفظ.",
		"editor_style":       "نمط النص", "editor_paragraph": "فقرة", "editor_h1": "عنوان 1", "editor_h2": "عنوان 2", "editor_h3": "عنوان 3",
		"editor_bold": "غامق (Ctrl+B)", "editor_italic": "مائل (Ctrl+I)", "editor_strike": "يتوسطه خط", "editor_code": "رمز ضمن النص",
		"editor_link": "رابط (Ctrl+K)", "editor_ul": "قائمة نقطية", "editor_ol": "قائمة مرقّمة", "editor_task": "قائمة مهام",
		"editor_quote": "اقتباس", "editor_codeblock": "كتلة رمز", "editor_table": "إدراج جدول", "editor_hr": "خط فاصل",
		"editor_undo": "تراجع (Ctrl+Z)", "editor_redo": "إعادة (Ctrl+Shift+Z)",
		"editor_link_url": "عنوان الرابط", "editor_link_apply": "إضافة الرابط", "editor_link_cancel": "إلغاء",
		"editor_link_invalid": "استخدم عنوان ويب (https://…) أو مسارًا يبدأ بـ / أو #قسمًا أو مرجع doc:.",
		"editor_table_column": "عمود",
		"editor_counts":       "{words} كلمة · {chars} حرفًا", "editor_counts_one": "كلمة واحدة · {chars} حرفًا",
		"editor_dirty": "تغييرات غير محفوظة", "editor_discard_prompt": "هل تريد تجاهل التغييرات غير المحفوظة؟", "editor_discard": "تجاهل التغييرات", "editor_keep": "متابعة التحرير",
		"editor_required": "أضف عنوانًا وبعض النص قبل الحفظ.",
	},
}

// docsEditorText reads the editor's own copy, then the Docs copy.
func docsEditorText(locale, key string) string {
	if copy, ok := docsEditorCopy[locale]; ok && copy[key] != "" {
		return copy[key]
	}
	if strings.HasPrefix(key, "editor_") {
		if value := docsEditorCopy["en-US"][key]; value != "" {
			return value
		}
	}
	return docsText(locale, key)
}

func docsEditorCounts(locale string, stats docsEditorStats) string {
	key := "editor_counts"
	if stats.Words == 1 {
		key = "editor_counts_one"
	}
	return strings.NewReplacer("{words}", strconv.Itoa(stats.Words), "{chars}", strconv.Itoa(stats.Characters)).Replace(docsEditorText(locale, key))
}

// docsSplitEditorProps opens the split editor on one document version.
// Save receives the new title and Markdown and reports back through its
// callback; Cancel leaves edit mode (the editor asks first when there are
// unsaved changes).
type docsSplitEditorProps struct {
	Locale                                     string
	DocumentID, BaseVersionID, Title, Markdown string
	Save                                       func(DocumentEditRequest, func(error))
	Cancel                                     func()
	// Media uploads images and PDFs (docs_media.go); nil hides the tools.
	Media *DocumentMediaPort
	// Suggest answers the "@", "#" and "[[" autocomplete
	// (docs_editor_suggest.go); nil turns it off.
	Suggest DocsReferenceSuggester
}

// docsEditorController is the editor's live state. It lives in a ref, so
// browser listeners and GWC handlers see the same text without waiting
// for a render; the component copies what it displays (counts, dirty,
// pressed buttons) into state through the on* callbacks.
type docsEditorController struct {
	locale                   string
	original, originalTitle  string
	markdown, title          string
	pane                     string // "source" or "rich": where the person last worked
	mode                     string // "split", "source" or "rich"
	dirty                    bool
	formats                  string
	history                  []string
	historyAt                int
	lastPush                 time.Time
	pendingLink              docsEditorCapture
	onChange, onSave, onLink func()
	onEscape                 func()
	onFormats                func(string)
	// flush runs any pending pane-to-pane sync at once (set by the browser
	// half); save and undo call it so they never act on stale text.
	flush func()
}

// docsEditorCapture is a selection in Markdown terms: the text, byte
// offsets into it, and the pane it came from.
type docsEditorCapture struct {
	Markdown   string
	Start, End int
	Pane       string
}

func newDocsEditorController(locale, title, markdown string) *docsEditorController {
	return &docsEditorController{
		locale: locale, original: markdown, originalTitle: title, markdown: markdown, title: title,
		pane: "rich", mode: "split", history: []string{markdown},
	}
}

// record adds a version of the text to the undo history. Typing that
// arrives within a second of the last change folds into it, so undo
// steps back a phrase at a time rather than a letter.
func (c *docsEditorController) record(markdown string, typing bool, now time.Time) {
	if c.historyAt < len(c.history) && c.history[c.historyAt] == markdown {
		return
	}
	c.history = c.history[:c.historyAt+1]
	if typing && c.historyAt > 0 && now.Sub(c.lastPush) < time.Second {
		c.history[c.historyAt] = markdown
	} else {
		c.history = append(c.history, markdown)
		c.historyAt = len(c.history) - 1
	}
	if len(c.history) > 200 {
		drop := len(c.history) - 200
		c.history = c.history[drop:]
		c.historyAt -= drop
	}
	c.lastPush = now
}

func (c *docsEditorController) step(delta int) (string, bool) {
	at := c.historyAt + delta
	if at < 0 || at >= len(c.history) {
		return "", false
	}
	c.historyAt = at
	c.lastPush = time.Time{}
	return c.history[at], true
}

// setMarkdown takes a new text from either pane or a command. typing
// folds it into the last undo step; fromHistory leaves the history alone.
func (c *docsEditorController) setMarkdown(markdown string, typing, fromHistory bool) {
	c.markdown = markdown
	if !fromHistory {
		c.record(markdown, typing, time.Now())
	}
	c.changed()
}

// changed is called after every edit to either pane or the title.
func (c *docsEditorController) changed() {
	c.dirty = c.markdown != c.original || c.title != c.originalTitle
	if c.onChange != nil {
		c.onChange()
	}
}

// apply runs one toolbar command on the pane the person is working in.
func (c *docsEditorController) apply(command, arg string) {
	if c.flush != nil {
		c.flush()
	}
	capture := docsEditorCaptureSelection(c)
	next, start, end := docsEditorTransform(capture.Markdown, capture.Start, capture.End, command, arg)
	docsEditorCommit(c, next, start, end, capture.Pane)
}

// undo and redo move through the editor's own history, which covers both
// panes; each pane's native history would only know its own edits.
func (c *docsEditorController) undo(delta int) {
	if c.flush != nil {
		c.flush()
	}
	previous := c.markdown
	next, ok := c.step(delta)
	if !ok {
		return
	}
	at := docsEditorFirstDifference(previous, next)
	docsEditorCommitHistory(c, next, at)
}

var docsEditorFormatButtons = []struct{ command, key, glyph, icon string }{
	{"bold", "editor_bold", "B", ""},
	{"italic", "editor_italic", "I", ""},
	{"strike", "editor_strike", "S", ""},
	{"code", "editor_code", "", "M9 7l-5 5 5 5M15 7l5 5-5 5"},
	{"link", "editor_link", "", ""},
	{"|", "", "", ""},
	{"ul", "editor_ul", "", "M9 6h11M9 12h11M9 18h11M4.5 6h.01M4.5 12h.01M4.5 18h.01"},
	{"ol", "editor_ol", "", "M10 6h10M10 12h10M10 18h10M4 4.5l1.5-.5v4.5M3.6 13.2c.4-.9 2.4-1 2.4.3 0 1-2.4 1.9-2.4 3h2.6"},
	{"task", "editor_task", "", "M3.5 4.5h5v5h-5zM3.5 14.5h5v5h-5zM12 7h9M12 17h9M4.8 16.9l1.2 1.2 2-2.3"},
	{"quote", "editor_quote", "", "M9.5 7H5v5h4.5v-.5c0 2.4-1.3 4.2-3.5 5M19 7h-4.5v5H19v-.5c0 2.4-1.3 4.2-3.5 5"},
	{"codeblock", "editor_codeblock", "", "M4 4.5h16v15H4zM10 9.5 7.5 12l2.5 2.5M14 9.5l2.5 2.5-2.5 2.5"},
	{"table", "editor_table", "", "M4 5h16v14H4zM4 10h16M4 15h16M10 5v14"},
	{"hr", "editor_hr", "", "M3 12h18M7 7.5h10M7 16.5h10"},
	{"|", "", "", ""},
	{"undo", "editor_undo", "", ""},
	{"redo", "editor_redo", "", ""},
}

// docsEditorStatusNode renders the editor's own status/alert line. It is
// shared by the live component and by tools/uxqual/cmd/genfixtures'
// conflict fixture (docs_fixtures.go), so a static Playwright fixture shows
// exactly the markup, class and aria-live behavior the live editor uses,
// never a hand-copied approximation of it.
func docsEditorStatusNode(locale, statusKey, statusRole string) ui.Node {
	if statusRole == "" {
		statusRole = "status"
	}
	statusText := ""
	if statusKey != "" {
		statusText = docsEditorText(locale, statusKey)
	}
	statusClass := "docs-editor-status"
	if statusRole == "alert" {
		statusClass += " is-alert"
	}
	return html.P(html.Props{ID: "docs-editor-status", Class: statusClass, Raw: map[string]any{"role": statusRole, "aria-live": "polite"}}, ui.Text(statusText))
}

func docsEditorIcon(path string) ui.Node {
	return html.Tag("svg", html.Props{Class: "docs-editor-icon", Raw: map[string]any{
		"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8",
		"stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", "focusable": "false",
	}}, html.Tag("path", html.Props{Raw: map[string]any{"d": path}}))
}

// docsSplitEditor edits a document as Markdown and as formatted text side
// by side. The two panes are never GWC children: the textarea holds its
// text as a DOM value and the formatted pane as imperatively set HTML, so
// a re-render cannot move the caret or eat a keystroke. The browser half
// (docs_editor_wasm.go) keeps them in step.
func docsSplitEditor(props docsSplitEditorProps) ui.Node {
	locale := props.Locale
	controller := ui.UseRef[*docsEditorController](nil)
	if controller.Get() == nil {
		controller.Set(newDocsEditorController(locale, props.Title, props.Markdown))
	}
	c := controller.Get()
	mode := ui.UseState("split")
	stats := ui.UseState(docsEditorStatsOf(props.Markdown))
	dirty := ui.UseState(false)
	formats := ui.UseState("")
	headingOpen := ui.UseState(false)
	linkOpen := ui.UseState(false)
	linkInvalid := ui.UseState(false)
	linkValue := ui.UseRef("")
	confirmCancel := ui.UseState(false)
	busy := ui.UseState(false)
	failed := ui.UseState(false)
	conflict := ui.UseState(false)
	saved := ui.UseState(false)
	missing := ui.UseState(false)
	suggestRef := ui.UseRef[*docsSuggestController](nil)
	if suggestRef.Get() == nil {
		suggestRef.Set(&docsSuggestController{})
	}
	suggest := suggestRef.Get()
	suggest.fetch = props.Suggest
	media := useDocsEditorMedia(c, locale, props.DocumentID, props.Media)

	c.mode = mode.Get()
	c.onChange = func() {
		stats.Set(docsEditorStatsOf(c.markdown))
		if c.dirty != dirty.Get() {
			dirty.Set(c.dirty)
		}
		if c.dirty && saved.Get() {
			saved.Set(false)
		}
		if missing.Get() {
			missing.Set(false)
		}
	}
	c.onFormats = func(value string) {
		if value != c.formats {
			c.formats = value
			formats.Set(value)
		}
	}
	c.onLink = func() {
		c.pendingLink = docsEditorCaptureSelection(c)
		linkInvalid.Set(false)
		headingOpen.Set(false)
		linkOpen.Set(true)
	}
	c.onEscape = func() {
		if linkOpen.Get() {
			linkOpen.Set(false)
			docsEditorFocus(c.pane)
		}
		headingOpen.Set(false)
		confirmCancel.Set(false)
	}
	c.onSave = func() {
		if c.flush != nil {
			c.flush()
		}
		if busy.Get() || props.Save == nil {
			return
		}
		if strings.TrimSpace(c.title) == "" || strings.TrimSpace(c.markdown) == "" {
			missing.Set(true)
			return
		}
		busy.Set(true)
		failed.Set(false)
		conflict.Set(false)
		saved.Set(false)
		missing.Set(false)
		title, body := c.title, c.markdown
		props.Save(DocumentEditRequest{DocumentID: props.DocumentID, BaseVersionID: props.BaseVersionID, Title: title, Markdown: body}, func(err error) {
			busy.Set(false)
			if err != nil {
				failed.Set(true)
				conflict.Set(isDocumentVersionConflict(err))
				return
			}
			c.original, c.originalTitle = body, title
			c.changed()
			saved.Set(true)
		})
	}

	// The browser half attaches its listeners once per document version and
	// re-seeds the fields whenever a render has replaced them.
	ui.UseEffect(func() func() {
		if docsEditorNarrow() {
			mode.Set("rich")
			c.mode = "rich"
		}
		return docsEditorMount(c)
	}, props.DocumentID+"@"+props.BaseVersionID)
	ui.UseEffect(func() func() { return docsSuggestMount(c, suggest) }, props.DocumentID+"@"+props.BaseVersionID)
	ui.UseLayoutEffect(func() func() {
		docsEditorEnsure(c)
		return nil
	})
	ui.UseEffect(func() func() {
		if linkOpen.Get() {
			setDocsFieldValue("docs-editor-link-url", "")
			linkValue.Set("")
			docsFocusElement("docs-editor-link-url")
		}
		return nil
	}, linkOpen.Get())

	titleInput := ui.UseEvent(func(event ui.InputEvent) {
		c.title = event.GetValue()
		c.changed()
	})
	keepFocus := ui.UseEvent(func(event ui.MouseEvent) {
		// Toolbar buttons must not take focus or the selection away from the
		// pane they act on.
		event.PreventDefault()
	})
	toolbarClick := ui.UseEvent(func(event ui.MouseEvent) {
		command := docsEditorCommandAt(event)
		switch command {
		case "":
			return
		case "style":
			headingOpen.Set(!headingOpen.Get())
		case "link":
			c.onLink()
		case "undo":
			c.undo(-1)
		case "redo":
			c.undo(1)
		case "table":
			c.apply("table", docsEditorText(locale, "editor_table_column"))
		default:
			if strings.HasPrefix(command, "h") {
				headingOpen.Set(false)
			}
			c.apply(command, "")
		}
	})
	toolbarKey := ui.UseEvent(func(event ui.KeyboardEvent) { docsEditorToolbarKey(event) })
	linkInput := ui.UseEvent(func(event ui.InputEvent) {
		linkValue.Set(event.GetValue())
		if linkInvalid.Get() {
			linkInvalid.Set(false)
		}
	})
	linkSubmit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		href, ok := docsEditorNormalizeHref(linkValue.Get())
		if !ok {
			linkInvalid.Set(true)
			return
		}
		linkOpen.Set(false)
		capture := c.pendingLink
		if capture.Markdown != c.markdown {
			capture = docsEditorCapture{Markdown: c.markdown, Start: len(c.markdown), End: len(c.markdown), Pane: c.pane}
		}
		next, start, end := docsEditorTransform(capture.Markdown, capture.Start, capture.End, "link", href)
		docsEditorCommit(c, next, start, end, capture.Pane)
	})
	linkCancel := ui.UseEvent(func(ui.MouseEvent) {
		linkOpen.Set(false)
		docsEditorFocus(c.pane)
	})
	linkKey := ui.UseEvent(func(event ui.KeyboardEvent) {
		if event.GetKey() == "Escape" {
			event.PreventDefault()
			linkOpen.Set(false)
			docsEditorFocus(c.pane)
		}
	})
	chooseSplit := ui.UseEvent(func(ui.ChangeEvent) { mode.Set("split") })
	chooseSource := ui.UseEvent(func(ui.ChangeEvent) {
		mode.Set("source")
		c.pane = "source"
	})
	chooseRich := ui.UseEvent(func(ui.ChangeEvent) {
		mode.Set("rich")
		c.pane = "rich"
	})
	save := ui.UseEvent(func(ui.MouseEvent) { c.onSave() })
	cancel := ui.UseEvent(func(ui.MouseEvent) {
		if c.dirty {
			confirmCancel.Set(true)
			docsFocusElement("docs-editor-keep")
			return
		}
		if props.Cancel != nil {
			props.Cancel()
		}
	})
	discard := ui.UseEvent(func(ui.MouseEvent) {
		confirmCancel.Set(false)
		if props.Cancel != nil {
			props.Cancel()
		}
	})
	keep := ui.UseEvent(func(ui.MouseEvent) {
		confirmCancel.Set(false)
		docsEditorFocus(c.pane)
	})

	pressed := map[string]bool{}
	for _, name := range strings.Split(formats.Get(), ",") {
		if name != "" {
			pressed[name] = true
		}
	}
	text := func(key string) string { return docsEditorText(locale, key) }

	// Toolbar: a text-style menu, then one button per command.
	styleLabel := text("editor_paragraph")
	for _, level := range []string{"h1", "h2", "h3"} {
		if pressed[level] {
			styleLabel = text("editor_" + level)
		}
	}
	styleItems := []ui.Node{}
	for _, item := range []struct{ command, key string }{{"h0", "editor_paragraph"}, {"h1", "editor_h1"}, {"h2", "editor_h2"}, {"h3", "editor_h3"}} {
		current := pressed[item.command] || (item.command == "h0" && !pressed["h1"] && !pressed["h2"] && !pressed["h3"])
		styleItems = append(styleItems, html.Button(html.Props{
			Key: item.command, Class: "docs-editor-style-item docs-editor-style-" + item.command, Type: "button", Role: "menuitemradio",
			Aria: map[string]string{"checked": strconv.FormatBool(current)}, Data: map[string]string{"editor-cmd": item.command},
		}, ui.Text(text(item.key))))
	}
	tools := []ui.Node{
		html.Div(html.Props{Key: "style", Class: "docs-editor-style"},
			html.Button(html.Props{
				Class: "docs-editor-tool docs-editor-style-trigger", Type: "button", Title: text("editor_style"),
				Aria: map[string]string{"label": text("editor_style") + ": " + styleLabel, "haspopup": "menu", "expanded": strconv.FormatBool(headingOpen.Get()), "controls": "docs-editor-style-menu"},
				Data: map[string]string{"editor-cmd": "style"},
			}, html.Span(html.Props{Class: "docs-editor-style-label"}, ui.Text(styleLabel)), docsEditorIcon("M7 10l5 5 5-5")),
			html.Div(html.Props{ID: "docs-editor-style-menu", Class: "docs-editor-style-menu", Role: "menu", Hidden: !headingOpen.Get(), Aria: map[string]string{"label": text("editor_style")}}, styleItems...),
		),
	}
	for index, button := range docsEditorFormatButtons {
		if button.command == "|" {
			tools = append(tools, html.Span(html.Props{Key: "sep" + strconv.Itoa(index), Class: "docs-editor-sep", Raw: map[string]any{"aria-hidden": "true"}}))
			continue
		}
		var glyph ui.Node
		switch {
		case button.glyph != "":
			glyph = html.Span(html.Props{Class: "docs-editor-glyph docs-editor-glyph-" + button.command, Raw: map[string]any{"aria-hidden": "true"}}, ui.Text(button.glyph))
		case button.command == "link":
			glyph = productIcon("link", "docs-editor-icon")
		case button.command == "undo" || button.command == "redo":
			glyph = productIcon(button.command, "docs-editor-icon docs-editor-flip")
		default:
			glyph = docsEditorIcon(button.icon)
		}
		aria := map[string]string{"label": text(button.key)}
		if button.command != "undo" && button.command != "redo" && button.command != "table" && button.command != "hr" {
			aria["pressed"] = strconv.FormatBool(pressed[button.command])
		}
		tools = append(tools, html.Button(html.Props{
			Key: button.command, Class: "docs-editor-tool", Type: "button", Title: text(button.key), Aria: aria,
			Data: map[string]string{"editor-cmd": button.command},
		}, glyph))
	}

	tools = append(tools, media.Tools...)

	// Link popover, always rendered so the panes' siblings never change.
	linkForm := html.Form(html.Props{Key: "link", ID: "docs-editor-link", Class: "docs-editor-link", Hidden: !linkOpen.Get(), OnSubmit: linkSubmit, OnKeyDown: linkKey, Aria: map[string]string{"label": text("editor_link_url")}},
		html.Label(html.Props{For: "docs-editor-link-url"}, ui.Text(text("editor_link_url"))),
		html.Input(html.Props{ID: "docs-editor-link-url", Type: "text", Placeholder: "https://", OnInput: linkInput, Raw: map[string]any{"dir": "ltr", "autocomplete": "off", "spellcheck": "false", "inputmode": "url", "aria-invalid": strconv.FormatBool(linkInvalid.Get()), "aria-describedby": "docs-editor-link-error"}}),
		html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(text("editor_link_apply"))),
		html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: linkCancel}, ui.Text(text("editor_link_cancel"))),
		html.P(html.Props{ID: "docs-editor-link-error", Class: "docs-editor-link-error", Hidden: !linkInvalid.Get(), Raw: map[string]any{"role": "alert"}}, ui.Text(text("editor_link_invalid"))),
	)

	radio := func(value, key string, handler ui.Handler) ui.Node {
		id := "docs-editor-view-" + value
		return html.Label(html.Props{Key: value, Class: "docs-editor-view-option", For: id},
			html.Input(html.Props{ID: id, Type: "radio", Name: "docs-editor-view", Value: value, Checked: mode.Get() == value, OnChange: handler}),
			html.Span(html.Props{}, ui.Text(text(key))))
	}
	viewSwitch := html.Div(html.Props{Class: "docs-editor-view", Role: "radiogroup", Aria: map[string]string{"label": text("editor_view")}},
		radio("split", "editor_view_split", chooseSplit),
		radio("source", "editor_view_source", chooseSource),
		radio("rich", "editor_view_rich", chooseRich))

	paneClass := func(name string) string {
		class := "docs-editor-pane docs-editor-pane-" + name
		if mode.Get() != "split" && mode.Get() != name {
			class += " is-hidden"
		}
		return class
	}
	panes := html.Div(html.Props{Key: "panes", Class: "docs-editor-panes is-" + mode.Get()},
		html.Section(html.Props{Key: "source", Class: paneClass("source"), Aria: map[string]string{"labelledby": "docs-editor-source-label"}},
			html.Div(html.Props{Class: "docs-editor-pane-head"}, html.Span(html.Props{ID: "docs-editor-source-label"}, ui.Text(text("editor_source")))),
			html.Div(html.Props{Class: "docs-editor-host"},
				html.Textarea(html.Props{ID: "docs-editor-source", Class: "docs-editor-source", Name: "markdown", Raw: map[string]any{
					"dir": "ltr", "spellcheck": "false", "autocapitalize": "off", "autocomplete": "off", "aria-labelledby": "docs-editor-source-label", "aria-describedby": "docs-editor-help",
				}})),
		),
		html.Section(html.Props{Key: "rich", Class: paneClass("rich"), Aria: map[string]string{"labelledby": "docs-editor-rich-label"}},
			html.Div(html.Props{Class: "docs-editor-pane-head"}, html.Span(html.Props{ID: "docs-editor-rich-label"}, ui.Text(text("editor_rich")))),
			html.Div(html.Props{Class: "docs-editor-host"},
				html.Div(html.Props{ID: "docs-editor-rich", Class: "docs-editor-rich docs-markdown", Role: "textbox", Raw: map[string]any{
					"contenteditable": "true", "aria-multiline": "true", "aria-label": text("editor_rich_label"), "aria-describedby": "docs-editor-help", "spellcheck": "true", "dir": "auto",
				}})),
		),
	)

	statusKey, statusRole := "", "status"
	switch {
	case failed.Get() && conflict.Get():
		statusKey, statusRole = "edit_conflict", "alert"
	case failed.Get():
		statusKey, statusRole = "edit_failed", "alert"
	case missing.Get():
		statusKey, statusRole = "editor_required", "alert"
	case busy.Get():
		statusKey = "edit_busy"
	case saved.Get():
		statusKey = "edit_saved"
	}
	return html.Section(html.Props{
		Key: "docs-editor-" + props.DocumentID + "@" + props.BaseVersionID, ID: "docs-editor", Class: "docs-editor",
		Aria: map[string]string{"label": text("editor_label")},
		Data: map[string]string{"document-id": props.DocumentID, "base-version-id": props.BaseVersionID},
	},
		html.Div(html.Props{Key: "head", Class: "docs-editor-head"},
			html.Div(html.Props{Class: "docs-editor-title-field"},
				html.Label(html.Props{For: "docs-editor-title"}, ui.Text(text("title"))),
				html.Input(html.Props{ID: "docs-editor-title", Class: "docs-editor-title", Name: "title", Required: true, MaxLength: 200, OnInput: titleInput, Raw: map[string]any{"dir": "auto", "autocomplete": "off"}}),
			),
			viewSwitch,
		),
		html.P(html.Props{Key: "help", ID: "docs-editor-help", Class: "docs-editor-help"}, ui.Text(text("editor_source_help"))),
		html.Div(html.Props{Key: "toolbar", Class: "docs-editor-toolbar", Role: "toolbar", Aria: map[string]string{"label": text("editor_toolbar"), "controls": "docs-editor-source docs-editor-rich"}, OnClick: toolbarClick, OnMouseDown: keepFocus, OnKeyDown: toolbarKey}, tools...),
		linkForm,
		media.Status,
		panes,
		html.Div(html.Props{Key: "suggest", Class: "docs-suggest-host"}, ui.CreateElement(docsSuggestList, docsSuggestListProps{Locale: locale, Controller: suggest})),
		html.Div(html.Props{Key: "foot", Class: "docs-editor-foot"},
			html.Div(html.Props{Class: "docs-editor-meta"},
				docsEditorStatusNode(locale, statusKey, statusRole),
				html.Span(html.Props{Class: "docs-editor-dirty", Hidden: !dirty.Get()}, ui.Text(text("editor_dirty"))),
				html.Span(html.Props{Class: "docs-editor-counts"}, ui.Text(docsEditorCounts(locale, stats.Get()))),
			),
			html.Div(html.Props{Class: "docs-editor-confirm", Hidden: !confirmCancel.Get(), Role: "group", Aria: map[string]string{"labelledby": "docs-editor-confirm-text"}},
				html.Span(html.Props{ID: "docs-editor-confirm-text"}, ui.Text(text("editor_discard_prompt"))),
				html.Button(html.Props{ID: "docs-editor-discard", Class: "button secondary docs-editor-discard", Type: "button", OnClick: discard}, ui.Text(text("editor_discard"))),
				html.Button(html.Props{ID: "docs-editor-keep", Class: "button primary", Type: "button", OnClick: keep}, ui.Text(text("editor_keep"))),
			),
			html.Div(html.Props{Class: "docs-editor-actions", Hidden: confirmCancel.Get()},
				html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: busy.Get(), OnClick: cancel}, ui.Text(docsText(locale, "edit_cancel"))),
				html.Button(html.Props{Class: "button primary", Type: "button", Disabled: busy.Get() || (saved.Get() && !dirty.Get()), OnClick: save, Raw: map[string]any{"aria-keyshortcuts": "Control+S Meta+S"}}, ui.Text(docsText(locale, "edit_save"))),
			),
		),
	)
}

// docsEditorNormalizeHref accepts what the link box may hold: a safe
// address as typed, or a bare host name, which gets https://.
func docsEditorNormalizeHref(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if docsEditorSafeHref(value) {
		return value, true
	}
	if !strings.Contains(value, ":") && strings.Contains(value, ".") && !strings.HasPrefix(value, "/") && !strings.ContainsAny(value, " \\") {
		candidate := "https://" + value
		if docsEditorSafeHref(candidate) {
			return candidate, true
		}
	}
	return "", false
}

// docsEditorFormatsKey is the pressed-state set as the component stores
// it: sorted names joined by commas.
func docsEditorFormatsKey(formats map[string]bool) string {
	names := make([]string, 0, len(formats))
	for _, name := range []string{"bold", "italic", "strike", "code", "link", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "task", "quote", "codeblock"} {
		if formats[name] {
			names = append(names, name)
		}
	}
	return strings.Join(names, ",")
}
