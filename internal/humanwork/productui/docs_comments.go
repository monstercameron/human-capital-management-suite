package productui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsAnchorDraft is a passage the reader selected to comment on.
type docsAnchorDraft struct {
	Quote, Prefix, Suffix string
	// Derived marks visible Chat text expanded from a reference. It cannot be
	// stored as a version anchor because the text is absent from the Markdown.
	Derived bool
}

func docsCommentRequest(documentID, versionID, body, locale string, anchor docsAnchorDraft) DocumentCommentCreateRequest {
	request := DocumentCommentCreateRequest{DocumentID: documentID, VersionID: versionID, Body: body}
	if anchor.Derived {
		intro := "Selected Chat text: “" + anchor.Quote + "”"
		switch locale {
		case "de-DE":
			intro = "Ausgewählter Chat-Text: „" + anchor.Quote + "“"
		case "ar":
			intro = "نص الدردشة المحدد: «" + anchor.Quote + "»"
		}
		request.Body = intro + "\n\n" + body
		return request
	}
	request.Quote, request.Prefix, request.Suffix = anchor.Quote, anchor.Prefix, anchor.Suffix
	return request
}

func docsDerivedCommentCopy(locale string) (label, hint string) {
	switch locale {
	case "de-DE":
		return "Zum ausgewählten Chat-Text", "Der Auszug wird in den Kommentar aufgenommen. Der Kommentar gilt für das gesamte Dokument; antworten Sie über „Zur Nachricht“, wenn Sie den Chat besprechen möchten."
	case "ar":
		return "حول نص الدردشة المحدد", "سيُدرج المقتطف في التعليق. ينطبق التعليق على المستند كله؛ استخدم الانتقال إلى الرسالة للرد في الدردشة."
	default:
		return "About selected Chat text", "The excerpt will be included in your comment. The comment applies to the whole document; use Jump to message to reply in Chat."
	}
}

type docsCommentsProps struct {
	Locale              string
	LocaleContext       LocaleContext
	DocumentID          string
	VersionID           string
	Principal           string
	Comments            []DocumentComment
	People              []Person
	CanComment          bool
	Unavailable         bool
	Add                 func(DocumentCommentCreateRequest, func(error))
	Resolve             func(documentID, commentID string, resolved bool, done func(error))
	Anchor              docsAnchorDraft
	ClearAnchor         func()
	Active              string
	Linked              string
	Numbers             map[string]int
	Activate            func(id string)
	ComposerFocusSignal int
}

// docsComments is the discussion beside a document: open threads first,
// each led by the passage it is about, with replies under it; resolved
// threads one click away; one composer that knows which passage it is
// commenting on.
func docsComments(props docsCommentsProps) ui.Node {
	locale := props.Locale
	body := ui.UseRef("")
	bodyTick := ui.UseState(0)
	replyTo := ui.UseState("")
	replyBody := ui.UseRef("")
	showResolved := ui.UseState(false)
	busy := ui.UseState(false)
	failed := ui.UseState("")
	composerRef := ui.UseDOMRef()
	focus := useDocsFocus()
	ui.UseEffect(func() func() {
		if props.ComposerFocusSignal > 0 {
			composerRef.Focus()
		}
		return nil
	}, props.ComposerFocusSignal)

	threads, replies := docsThreads(props.Comments)
	open, resolved := 0, 0
	for _, thread := range threads {
		if thread.Resolved {
			resolved++
		} else {
			open++
		}
	}
	now := time.Now()

	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		text := strings.TrimSpace(body.Get())
		if text == "" || props.Add == nil || busy.Get() {
			return
		}
		busy.Set(true)
		failed.Set("")
		request := docsCommentRequest(props.DocumentID, props.VersionID, text, locale, props.Anchor)
		props.Add(request, func(err error) {
			busy.Set(false)
			if err != nil {
				failed.Set("comment_failed")
				return
			}
			body.Set("")
			bodyTick.Set(bodyTick.Get() + 1)
			setDocsFieldValue("docs-comment-body", "")
			if props.ClearAnchor != nil {
				props.ClearAnchor()
			}
		})
	})
	input := ui.UseEvent(func(event ui.InputEvent) {
		body.Set(event.GetValue())
		bodyTick.Set(bodyTick.Get() + 1)
	})
	composerKey := ui.UseEvent(func(event ui.KeyboardEvent) {
		if event.GetKey() == "Enter" && docsModifierHeld(event) {
			event.PreventDefault()
			docsSubmitForm("docs-comment-form")
		}
	})
	replyInput := ui.UseEvent(func(event ui.InputEvent) { replyBody.Set(event.GetValue()) })
	replySubmit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		text := strings.TrimSpace(replyBody.Get())
		parent := replyTo.Get()
		if text == "" || parent == "" || props.Add == nil || busy.Get() {
			return
		}
		busy.Set(true)
		failed.Set("")
		props.Add(DocumentCommentCreateRequest{DocumentID: props.DocumentID, VersionID: props.VersionID, Body: text, ParentID: parent}, func(err error) {
			busy.Set(false)
			if err != nil {
				failed.Set("comment_failed")
				return
			}
			replyBody.Set("")
			replyTo.Set("")
			focus(false, "id:"+docsReplyOpenID(parent), "id:docs-thread-"+parent)
		})
	})
	click := ui.UseEvent(func(event ui.MouseEvent) {
		action, id, _ := docsEventAction(event)
		switch action {
		case "comment-reveal":
			if props.Activate != nil {
				props.Activate(id)
			}
			if docsRevealAnchor(id) {
				docsFlashAnchor(id)
			}
		case "comment-reply":
			replyBody.Set("")
			replyTo.Set(id)
			focus(false, "id:docs-reply-"+id)
		case "comment-reply-cancel":
			replyTo.Set("")
			focus(false, "id:"+docsReplyOpenID(id), "id:docs-thread-"+id)
		case "comment-resolve", "comment-reopen":
			if props.Resolve == nil {
				return
			}
			// The thread leaves this list once it is resolved (or reopened);
			// focus moves on to its neighbour instead of falling to <body>.
			next := docsThreadNeighbour(threads, showResolved.Get(), id)
			props.Resolve(props.DocumentID, id, action == "comment-resolve", func(err error) {
				if err != nil {
					failed.Set("resolve_failed")
					return
				}
				focus(false, docsThreadFocusTargets(next)...)
			})
		case "comment-show-resolved":
			showResolved.Set(!showResolved.Get())
		case "comment-clear-anchor":
			if props.ClearAnchor != nil {
				props.ClearAnchor()
			}
		}
	})

	items := []ui.Node{}
	for _, thread := range threads {
		if thread.Resolved != showResolved.Get() {
			continue
		}
		items = append(items, html.WithKey(docsThreadCard(props, thread, replies[thread.ID], now, replyTo.Get(), replyInput, replySubmit, busy.Get()), "thread:"+thread.ID))
	}
	canCompose := props.CanComment && props.Add != nil
	emptyShown := len(items) == 0
	// r4 D-8: with a composer on screen, "no open comments" is the
	// composer's own helper line, not a dashed box stacked above it.
	emptyInComposer := emptyShown && canCompose && !showResolved.Get() && !props.Unavailable
	if len(items) == 0 && !emptyInComposer {
		key := "comments_none_open"
		switch {
		case props.Unavailable:
			key = "comments_unavailable"
		case showResolved.Get():
			key = "comments_none_resolved"
		case !canCompose:
			// "Select text to comment" is an instruction this reader cannot
			// follow; the read-only note below says why (D-8).
			key = "comments_none_readonly"
		}
		items = append(items, html.WithKey(html.Li(html.Props{Class: "docs-thread-empty", Role: "status"}, ui.Text(docsText(locale, key))), "empty"))
	}

	toggle := ui.Node(nil)
	if resolved > 0 || showResolved.Get() {
		label := docsCount(locale, "comments_show_resolved", resolved)
		if showResolved.Get() {
			label = docsCount(locale, "comments_show_open", open)
		}
		toggle = html.Button(html.Props{Class: "docs-comments-toggle", Type: "button", Aria: map[string]string{"pressed": strconv.FormatBool(showResolved.Get())}, Data: map[string]string{"docs-action": "comment-show-resolved"}}, ui.Text(label))
	}
	children := []ui.Node{
		html.Header(html.Props{Class: "docs-comments-head"},
			html.H2(html.Props{ID: "docs-comments-heading"}, ui.Text(docsText(locale, "comments_heading")), docsCommentsCount(locale, open)),
			toggle,
		),
		html.Ol(html.Props{Class: "docs-threads", Hidden: len(items) == 0}, items...),
	}
	if !canCompose && !props.Unavailable {
		// A shared-with-you reader had no composer and no word on why (D-8).
		children = append(children, html.P(html.Props{Class: "docs-compose-hint docs-comments-readonly"}, ui.Text(docsText(locale, "comments_readonly_note"))))
	}
	if canCompose {
		// With the empty state already saying "select text to comment", the
		// composer hint keeps only the send shortcut instead of repeating it.
		// The shortcut is a .kbd-hint, hidden on touch screens (r4 C-8).
		hint := []ui.Node{html.Span(html.Props{Class: "kbd-hint"}, ui.Text(docsText(locale, "comment_hint_send")))}
		if !(emptyShown && !showResolved.Get() && !props.Unavailable) {
			hint = append([]ui.Node{ui.Text(docsText(locale, "comment_hint_select") + " ")}, hint...)
		}
		composer := []ui.Node{}
		if emptyInComposer {
			composer = append(composer, html.P(html.Props{Class: "docs-compose-empty", Role: "status"}, ui.Text(docsText(locale, "comments_none_open"))))
		}
		if props.Anchor.Quote != "" {
			label := docsText(locale, "comment_on")
			if props.Anchor.Derived {
				label, _ = docsDerivedCommentCopy(locale)
			}
			composer = append(composer, html.Div(html.Props{Class: "docs-compose-quote"},
				html.Span(html.Props{Class: "docs-compose-quote-label"}, ui.Text(label)),
				html.Tag("q", html.Props{Dir: "auto"}, ui.Text(docsClip(props.Anchor.Quote, 140))),
				html.Button(html.Props{Class: "docs-pick-remove", Type: "button", Aria: map[string]string{"label": docsText(locale, "comment_clear_quote")}, Data: map[string]string{"docs-action": "comment-clear-anchor"}}, productIcon("close", "docs-chip-icon")),
			))
			if props.Anchor.Derived {
				_, hint := docsDerivedCommentCopy(locale)
				composer = append(composer, html.P(html.Props{Class: "docs-compose-hint"}, ui.Text(hint)))
			}
		}
		placeholder := docsText(locale, "comment_placeholder")
		if props.Anchor.Quote != "" {
			placeholder = docsText(locale, "comment_placeholder_quote")
		}
		composer = append(composer,
			html.Label(html.Props{For: "docs-comment-body", Class: "sr-only"}, ui.Text(docsText(locale, "comment_body"))),
			html.Textarea(html.WithProps(html.Props{ID: "docs-comment-body", Name: "body", Dir: "auto", Rows: 2, MaxLength: 4000, Placeholder: placeholder, OnInput: input, OnKeyDown: composerKey, Raw: map[string]any{"aria-describedby": "docs-comment-help"}}, html.Ref(composerRef))),
			html.Div(html.Props{Class: "docs-compose-foot"},
				html.P(html.Props{ID: "docs-comment-help", Class: "docs-compose-hint"}, hint...),
				html.Button(html.Props{Class: "button primary compact", Type: "submit", Disabled: busy.Get() || strings.TrimSpace(body.Get()) == ""}, ui.Text(docsText(locale, "comment_action"))),
			),
		)
		if failed.Get() != "" {
			composer = append(composer, html.P(html.Props{Class: "docs-notice", Role: "alert"}, ui.Text(docsText(locale, failed.Get()))))
		}
		children = append(children, html.Form(html.Props{ID: "docs-comment-form", Class: "docs-compose", OnSubmit: submit}, composer...))
	}
	return html.Section(html.Props{Class: "docs-comments", OnClick: click, Aria: map[string]string{"labelledby": "docs-comments-heading"}}, children...)
}

// docsThreads splits top-level comments from replies and orders both:
// threads by where they sit in time (oldest first, like reading order of a
// discussion), replies chronologically under their parent.
func docsThreads(comments []DocumentComment) ([]DocumentComment, map[string][]DocumentComment) {
	threads := []DocumentComment{}
	replies := map[string][]DocumentComment{}
	for _, comment := range comments {
		if comment.ParentID != "" {
			replies[comment.ParentID] = append(replies[comment.ParentID], comment)
			continue
		}
		threads = append(threads, comment)
	}
	sort.SliceStable(threads, func(i, j int) bool { return threads[i].CreatedAt < threads[j].CreatedAt })
	for id := range replies {
		list := replies[id]
		sort.SliceStable(list, func(i, j int) bool { return list[i].CreatedAt < list[j].CreatedAt })
	}
	return threads, replies
}

func docsThreadCard(props docsCommentsProps, thread DocumentComment, replies []DocumentComment, now time.Time, replyTo string, replyInput, replySubmit ui.Handler, busy bool) ui.Node {
	locale := props.Locale
	class := "docs-thread"
	if thread.ID == props.Active {
		class += " is-active"
	}
	if thread.ID == props.Linked {
		class += " is-linked"
	}
	if thread.Resolved {
		class += " is-resolved"
	}
	children := []ui.Node{}
	if thread.Quote != "" {
		quoteClass := "docs-thread-quote"
		label := docsText(locale, "comment_jump")
		if thread.Orphaned {
			quoteClass += " is-orphaned"
			label = docsText(locale, "comment_orphaned")
		}
		quote := []ui.Node{}
		if n := props.Numbers[thread.ID]; n > 0 {
			quote = append(quote, html.Span(html.Props{Class: "docs-thread-num", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text(docsLocaleDigits(locale, strconv.Itoa(n)))))
		}
		quote = append(quote, html.Tag("q", html.Props{Dir: "auto"}, ui.Text(docsClip(thread.Quote, 180))))
		children = append(children, html.Button(html.Props{Class: quoteClass, Type: "button", Raw: map[string]any{"title": label}, Disabled: thread.Orphaned, Data: map[string]string{"docs-action": "comment-reveal", "docs-id": thread.ID}}, quote...))
		if thread.Orphaned {
			children = append(children, html.P(html.Props{Class: "docs-thread-orphan-note"}, ui.Text(docsText(locale, "comment_orphaned"))))
		}
	}
	children = append(children, docsCommentEntry(props, thread, now, true))
	if len(replies) > 0 {
		list := make([]ui.Node, 0, len(replies))
		for _, reply := range replies {
			list = append(list, html.Li(html.Props{Key: "reply:" + reply.ID}, docsCommentEntry(props, reply, now, false)))
		}
		children = append(children, html.Ol(html.Props{Class: "docs-replies"}, list...))
	}
	if replyTo == thread.ID {
		children = append(children, html.Form(html.Props{Class: "docs-reply-form", OnSubmit: replySubmit},
			html.Label(html.Props{For: "docs-reply-" + thread.ID, Class: "sr-only"}, ui.Text(docsText(locale, "comment_reply"))),
			html.Textarea(html.Props{ID: "docs-reply-" + thread.ID, Dir: "auto", Rows: 2, MaxLength: 4000, Placeholder: docsText(locale, "comment_reply_placeholder"), OnInput: replyInput}),
			html.Div(html.Props{Class: "docs-reply-actions"},
				html.Button(html.Props{Class: "button secondary compact", Type: "button", Data: map[string]string{"docs-action": "comment-reply-cancel", "docs-id": thread.ID}}, ui.Text(docsText(locale, "create_cancel"))),
				html.Button(html.Props{Class: "button primary compact", Type: "submit", Disabled: busy}, ui.Text(docsText(locale, "comment_reply"))),
			)))
	}
	actions := []ui.Node{}
	if props.CanComment && props.Add != nil && replyTo != thread.ID && !thread.Resolved {
		actions = append(actions, html.Button(html.Props{ID: docsReplyOpenID(thread.ID), Class: "docs-thread-action", Type: "button", Data: map[string]string{"docs-action": "comment-reply", "docs-id": thread.ID}}, ui.Text(docsText(locale, "comment_reply"))))
	}
	if props.Resolve != nil {
		if thread.Resolved {
			actions = append(actions, html.Button(html.Props{Class: "docs-thread-action", Type: "button", Data: map[string]string{"docs-action": "comment-reopen", "docs-id": thread.ID}}, ui.Text(docsText(locale, "comment_reopen"))))
		} else {
			actions = append(actions, html.Button(html.Props{Class: "docs-thread-action docs-thread-resolve", Type: "button", Data: map[string]string{"docs-action": "comment-resolve", "docs-id": thread.ID}}, productIcon("check", "docs-menu-icon"), ui.Text(docsText(locale, "comment_resolve"))))
		}
	}
	if len(actions) > 0 {
		children = append(children, html.Div(html.Props{Class: "docs-thread-actions"}, actions...))
	}
	return html.Li(html.Props{ID: "docs-thread-" + thread.ID, Class: class, TabIndex: -1, Data: map[string]string{"comment-id": thread.ID, "version-id": thread.VersionID}}, children...)
}

func docsCommentEntry(props docsCommentsProps, comment DocumentComment, now time.Time, lead bool) ui.Node {
	author := strings.TrimSpace(comment.Author)
	if author == "" {
		author = humanizeSubject(comment.AuthorID)
	}
	photo := ""
	for _, person := range props.People {
		if docsPersonIs(person, comment.AuthorID) {
			photo = person.PhotoURL
			// The directory's full name always wins over whatever the
			// comment's own Author field carries (sometimes only a first
			// name), so every entry in a thread reads the same way.
			if name := docsPersonName(person); name != "" {
				author = name
			}
			break
		}
	}
	if comment.AuthorID != "" && comment.AuthorID == props.Principal {
		author = strings.ReplaceAll(docsText(props.Locale, "you_named"), "{name}", author)
	}
	class := "docs-comment-entry"
	if !lead {
		class += " is-reply"
	}
	return html.Div(html.Props{Class: class},
		personAvatar(author, "", photo, "tiny"),
		html.Div(html.Props{Class: "docs-comment-main"},
			html.Div(html.Props{Class: "docs-comment-meta"},
				html.Span(html.Props{Class: "docs-comment-author"}, ui.Text(author)),
				docsWhen(props.LocaleContext, comment.CreatedAt, now),
			),
			html.P(html.Props{Class: "docs-comment-body", Dir: "auto"}, ui.Text(comment.Body)),
		),
	)
}

func docsClip(text string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) <= limit {
		return string(runes)
	}
	cut := string(runes[:limit])
	if i := strings.LastIndex(cut, " "); i > limit/2 {
		cut = cut[:i]
	}
	return cut + "…"
}

// docsNumberedThreads lists the open threads that point at a passage still
// present in this version, in the order the passages appear.
func docsNumberedThreads(comments []DocumentComment) []DocumentComment {
	out := []DocumentComment{}
	for _, comment := range comments {
		if comment.ParentID == "" && comment.Quote != "" && !comment.Resolved && !comment.Orphaned {
			out = append(out, comment)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Start, out[j].Start
		if a < 0 {
			a = 1 << 30
		}
		if b < 0 {
			b = 1 << 30
		}
		if a != b {
			return a < b
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

// docsCommentsCount is the open-comment count beside the heading, left out
// at zero: "Comments 0" put a badge on nothing, and the empty state below
// already says there are none (D-25).
func docsCommentsCount(locale string, open int) ui.Node {
	if open <= 0 {
		return nil
	}
	return html.Span(html.Props{Class: "docs-comments-count"}, ui.Text(docsLocaleDigits(locale, strconv.Itoa(open))))
}
