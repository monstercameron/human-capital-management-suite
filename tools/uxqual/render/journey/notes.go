package journey

import (
	"strconv"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// notesSectionLocale renders a journey's notes and, when the viewer may add
// one, the composer. Notes read oldest first, like a conversation, with the
// composer directly beneath the latest one.
//
// Each note is an <article> in an ordered list: the author, whether it is the
// reader's own, the stage it was written at and a machine-readable <time>
// travel with the text, so a screen reader announces "who, when, at which
// step" before the body. The body keeps its line breaks and is dir="auto" so
// a note in Arabic reads correctly on an English page and vice versa.
func notesSectionLocale(l live, v *NotesView) ui.Node {
	if v == nil {
		return nil
	}
	copy := productui.ResolveProductLocale(l.locale)
	heading := []ui.Node{html.Text(copy.Text("journey.notes_heading"))}
	if n := len(v.Notes); n > 0 {
		heading = append(heading, html.Span(html.Props{Class: "jn-notes-count"},
			visuallyHidden("("), html.Text(strconv.Itoa(n)), visuallyHidden(")")))
	}
	body := []ui.Node{
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "notes-heading"}, heading...),
		),
	}
	if len(v.Notes) == 0 {
		body = append(body, html.P(html.Props{Class: "jn-notes-empty"}, html.Text(copy.Text("journey.notes_empty"))))
	} else {
		body = append(body, html.Ol(html.Props{Class: "jn-notes"},
			html.Map(v.Notes, func(n NoteEntry) ui.Node { return noteNodeLocale(l.locale, n) })...))
	}
	if v.Composer != nil {
		body = append(body, noteComposer(l, *v.Composer))
	}
	return html.Section(html.Props{ID: "notes", Class: "jn-panel jn-notes-panel", Aria: map[string]string{"labelledby": "notes-heading"}}, body...)
}

func noteNodeLocale(locale string, n NoteEntry) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	author := n.Author
	if author == "" {
		author = copy.Text("journey.note_author_unknown")
	}
	initials := n.Initials
	if initials == "" {
		initials = "·"
	}
	meta := []ui.Node{
		html.Span(html.Props{Class: "jn-note-author", Dir: "auto"}, html.Text(author)),
	}
	if n.Own {
		meta = append(meta, html.Span(html.Props{Class: "jn-note-own"}, html.Text(copy.Text("journey.note_you"))))
	}
	if n.At != "" {
		timeProps := html.Props{Class: "jn-note-at"}
		if n.ISO != "" {
			timeProps.Raw = map[string]any{"datetime": n.ISO}
		}
		meta = append(meta, html.Tag("time", timeProps, html.Text(n.At)))
	}
	props := html.Props{Class: "jn-note", ID: "note-" + n.ID}
	if n.Own {
		props.DataAttr = html.DataAttribute{Name: "own", Value: "true"}
	}
	return html.Li(props,
		html.Tag("article", html.Props{Class: "jn-note-card", Aria: map[string]string{"label": copy.Text("journey.note_label", map[string]string{"author": author})}},
			html.Span(html.Props{Class: "jn-note-avatar", Aria: map[string]string{"hidden": "true"}}, html.Text(initials)),
			html.Div(html.Props{Class: "jn-note-main"},
				html.P(html.Props{Class: "jn-note-meta"}, meta...),
				htmlIf(n.Stage != "", func() ui.Node {
					return html.P(html.Props{Class: "jn-note-stage"}, html.Text(copy.Text("journey.note_stage", map[string]string{"stage": n.Stage})))
				}),
				html.P(html.Props{Class: "jn-note-body", Dir: "auto"}, html.Text(n.Body)),
			),
		),
	)
}

// noteComposer is the add-a-note form. The count is announced only as the
// reader approaches the limit, and the submit button stays enabled while the
// text is empty so a keyboard user who presses it hears why nothing was
// added, rather than meeting a silently dead control.
func noteComposer(l live, c NoteComposer) ui.Node {
	copy := productui.ResolveProductLocale(l.locale)
	field := c.Field
	used := utf8.RuneCountInString(l.value(field))
	limit := c.MaxRunes
	countState := "ok"
	switch {
	case limit > 0 && used > limit:
		countState = "over"
	case limit > 0 && used > limit*9/10:
		countState = "near"
	}
	counter := html.P(html.Props{ID: field.ID + "-count", Class: "jn-note-count",
		Data: map[string]string{"state": countState}},
		html.Text(copy.Text("journey.note_count", map[string]string{
			"used": copy.FormatNumber(strconv.Itoa(used), 0), "max": copy.FormatNumber(strconv.Itoa(limit), 0)})))

	btn := html.Props{Class: "jn-btn", Type: submitButtonType(c.OnSubmit),
		DataAttr: html.DataAttribute{Name: "variant", Value: "primary"}}
	label := copy.Text("journey.note_add")
	if c.Busy {
		btn.Disabled = true
		btn.Aria = map[string]string{"busy": "true"}
		label = copy.Text("journey.note_adding")
	} else if c.OnSubmit != nil {
		btn.OnClick = clickHandler(c.OnSubmit, l.collect(nil, []Field{field}))
	}
	props := formProps(l, c.Action, c.OnSubmit, nil, []Field{field})
	props.Class = "jn-note-compose"
	if c.OnSubmit != nil {
		props.Raw = map[string]any{"noValidate": true}
	}
	return html.Form(props,
		fieldNode(l, field, c.Busy),
		html.Div(html.Props{Class: "jn-note-compose-foot"},
			counter,
			html.P(html.Props{Class: "jn-note-status", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, html.Text(c.Status)),
			html.Button(btn, html.Text(label)),
		),
	)
}
