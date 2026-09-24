package productui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsDialog is the shared modal frame for Docs: a scrim that closes on
// click, Escape to close, and a labelled dialog surface. useDocsModal makes
// it modal in the browser (inert page, focus trap, focus restore).
func docsDialog(id, title, closeLabel string, close ui.Handler, keydown ui.Handler, body ...ui.Node) ui.Node {
	return html.Div(html.Props{Class: "docs-dialog-layer", OnKeyDown: keydown},
		html.Div(html.Props{Class: "docs-dialog-scrim", OnClick: close, Raw: map[string]any{"aria-hidden": "true"}}),
		html.Div(html.Props{ID: id, Class: "docs-dialog", Role: "dialog", TabIndex: -1, Aria: map[string]string{"modal": "true", "labelledby": id + "-title"}},
			html.Header(html.Props{Class: "docs-dialog-head"},
				html.H2(html.Props{ID: id + "-title"}, ui.Text(title)),
				html.Button(html.Props{Class: "docs-dialog-close", Type: "button", OnClick: close, Aria: map[string]string{"label": closeLabel}}, productIcon("close", "docs-button-icon")),
			),
			html.Div(html.Props{Class: "docs-dialog-body"}, body...),
		),
	)
}

type docsMoveDialogProps struct {
	Locale       string
	Count        int
	Current      string
	Library      *DocumentLibrary
	MoveTo       func(folderID string)
	Close        func()
	CreateFolder func(string, func(DocumentFolder, error))
}

// docsMoveDialog files documents with one click on a folder. A new folder
// can be made on the spot and receives the documents immediately.
func docsMoveDialog(props docsMoveDialogProps) ui.Node {
	draft := ui.UseState("")
	failed := ui.UseState(false)
	busy := ui.UseState(false)
	close := ui.UseEvent(func(ui.MouseEvent) { props.Close() })
	ui.UseEffect(func() func() { return docsListenEscape(props.Close) })
	useDocsModal(true, "docs-move-dialog", ".docs-move-option:not(:disabled)", docsDialogReturnFallbacks...)
	keydown := ui.UseEvent(func(ui.KeyboardEvent) {})
	pick := ui.UseEvent(func(event ui.MouseEvent) {
		if action, id, _ := docsEventAction(event); action == "move-to" && !busy.Get() {
			busy.Set(true)
			props.MoveTo(id)
		}
	})
	input := ui.UseEvent(func(event ui.InputEvent) { draft.Set(event.GetValue()) })
	create := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		name := strings.TrimSpace(draft.Get())
		if name == "" || props.CreateFolder == nil {
			return
		}
		busy.Set(true)
		props.CreateFolder(name, func(folder DocumentFolder, err error) {
			if err != nil || folder.ID == "" {
				busy.Set(false)
				failed.Set(true)
				return
			}
			props.MoveTo(folder.ID)
		})
	})
	folders := []DocumentFolder{}
	if props.Library != nil {
		folders = append(folders, props.Library.Folders...)
	}
	sort.SliceStable(folders, func(i, j int) bool { return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name) })
	option := func(id, name, icon string) ui.Node {
		current := id == props.Current
		children := []ui.Node{productIcon(icon, "docs-nav-icon"), html.Span(html.Props{Class: "docs-move-name"}, ui.Text(name))}
		if current {
			children = append(children, html.Span(html.Props{Class: "docs-move-current"}, ui.Text(docsText(props.Locale, "move_current"))))
		}
		return html.Li(html.Props{Key: "move:" + id}, html.Button(html.Props{Class: "docs-move-option", Type: "button", Disabled: current || busy.Get(), Data: map[string]string{"docs-action": "move-to", "docs-id": id}}, children...))
	}
	options := []ui.Node{}
	for _, folder := range folders {
		options = append(options, option(folder.ID, folder.Name, "folder"))
	}
	// "Not in a folder" is a place, not a cancel: it shows the plain
	// document glyph, never the close cross. The nav already carries the
	// folders note, so the dialog opens straight on the choices.
	options = append(options, option("", docsText(props.Locale, "move_unfiled"), "document"))
	body := []ui.Node{
		html.Ul(html.Props{Class: "docs-move-list", OnClick: pick, Aria: map[string]string{"label": docsCount(props.Locale, "move_title", props.Count)}}, options...),
	}
	if props.CreateFolder != nil {
		body = append(body, html.Form(html.Props{Class: "docs-move-new", OnSubmit: create},
			html.Label(html.Props{For: "docs-move-folder"}, ui.Text(docsText(props.Locale, "move_new_folder"))),
			html.Div(html.Props{Class: "docs-move-new-row"},
				html.Input(html.Props{ID: "docs-move-folder", Value: draft.Get(), MaxLength: 80, Placeholder: docsText(props.Locale, "folder_name"), OnInput: input, AutoComplete: "off"}),
				html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: busy.Get() || strings.TrimSpace(draft.Get()) == ""}, ui.Text(docsText(props.Locale, "move_create_and_move"))),
			),
		))
	}
	if failed.Get() {
		body = append(body, html.P(html.Props{Class: "docs-notice", Role: "alert"}, ui.Text(docsText(props.Locale, "folder_create_failed"))))
	}
	return docsDialog("docs-move-dialog", docsCount(props.Locale, "move_title", props.Count), docsText(props.Locale, "close"), close, keydown, body...)
}

type docsShareDialogProps struct {
	Locale, DocumentID, Title, Origin, Principal string
	People                                       []Person
	Share                                        func(DocumentShareRequest, func(error))
	ListAccess                                   func(string, func([]DocumentAccessEntry, error))
	Revoke                                       func(documentID, subjectID string, done func(error))
	Close                                        func()
}

type docsShareState struct {
	loaded, loadFailed, busy, failed, copied bool
	copyFailed                               bool
	entries                                  []DocumentAccessEntry
	picked                                   []Person
	query, role, message                     string
	active                                   int
}

// docsShareDialog adds people to a document and shows everyone who can read
// it. Sharing publishes the current version to the people added; it never
// reaches anyone not listed here, and a link alone grants nothing.
func docsShareDialog(props docsShareDialogProps) ui.Node {
	state := ui.UseState(docsShareState{role: "commenter"})
	requested := ui.UseState(false)
	update := func(change func(*docsShareState)) {
		next := state.Get()
		change(&next)
		state.Set(next)
	}
	load := func() {
		if props.ListAccess == nil {
			update(func(s *docsShareState) { s.loaded = true })
			return
		}
		props.ListAccess(props.DocumentID, func(entries []DocumentAccessEntry, err error) {
			update(func(s *docsShareState) {
				s.loaded, s.loadFailed = true, err != nil
				if err == nil {
					s.entries = docsNamedAccess(entries, props.People, props.Principal, props.Locale)
				}
			})
		})
	}
	if !requested.Get() {
		requested.Set(true)
		load()
	}
	current := state.Get()
	hasAccess := map[string]bool{props.Principal: true}
	for _, entry := range current.entries {
		hasAccess[entry.SubjectID] = true
	}
	for _, person := range current.picked {
		hasAccess[docsPersonSubject(person)] = true
	}
	suggestions := docsPeopleMatches(props.People, current.query, hasAccess, 6)
	active := min(current.active, max(len(suggestions)-1, 0))

	close := ui.UseEvent(func(ui.MouseEvent) { props.Close() })
	focus := useDocsFocus()
	useDocsModal(true, "docs-share-dialog", "#docs-share-people", docsDialogReturnFallbacks...)
	// Picking or removing a person keeps the typing position in the people
	// field, so several people can be added from the keyboard in a row.
	pickPerson := func(person Person) {
		update(func(s *docsShareState) {
			s.picked = append(s.picked, person)
			s.query, s.active, s.message, s.failed = "", 0, "", false
		})
		focus(false, "id:docs-share-people")
	}
	// Escape first closes an open suggestion list (clearing what was typed),
	// and only then the dialog. One document listener decides, so the two
	// never race.
	query := current.query
	ui.UseEffect(func() func() {
		return docsListenEscape(func() {
			if query != "" {
				update(func(s *docsShareState) { s.query, s.active = "", 0 })
				focus(false, "id:docs-share-people")
				return
			}
			props.Close()
		})
	})
	keydown := ui.UseEvent(func(ui.KeyboardEvent) {})
	input := ui.UseEvent(func(event ui.InputEvent) {
		value := event.GetValue()
		update(func(s *docsShareState) { s.query, s.active = value, 0 })
	})
	fieldKey := ui.UseEvent(func(event ui.KeyboardEvent) {
		switch event.GetKey() {
		case "ArrowDown":
			event.PreventDefault()
			update(func(s *docsShareState) { s.active = min(active+1, max(len(suggestions)-1, 0)) })
		case "ArrowUp":
			event.PreventDefault()
			update(func(s *docsShareState) { s.active = max(active-1, 0) })
		case "Enter":
			if len(suggestions) > 0 && strings.TrimSpace(state.Get().query) != "" {
				event.PreventDefault()
				pickPerson(suggestions[active])
			}
		case "Backspace":
			if state.Get().query == "" && len(state.Get().picked) > 0 {
				update(func(s *docsShareState) { s.picked = s.picked[:len(s.picked)-1] })
			}
		}
	})
	click := ui.UseEvent(func(event ui.MouseEvent) {
		action, id, _ := docsEventAction(event)
		switch action {
		case "share-pick":
			for _, person := range suggestions {
				if person.ID == id {
					pickPerson(person)
					return
				}
			}
		case "share-unpick":
			update(func(s *docsShareState) {
				kept := s.picked[:0:0]
				for _, person := range s.picked {
					if person.ID != id {
						kept = append(kept, person)
					}
				}
				s.picked = kept
			})
			focus(false, "id:docs-share-people")
		case "share-copy":
			if href := docsShareableHref(props.Origin, props.DocumentID); href != "" {
				copyToClipboard(href, func(err error) {
					update(func(s *docsShareState) { s.copied, s.copyFailed = err == nil, err != nil })
					if err != nil {
						// The link is shown selected, so Ctrl+C copies it.
						focus(true, "id:docs-share-link")
					}
				})
			}
		case "share-revoke":
			if props.Revoke == nil {
				return
			}
			name := id
			for _, entry := range state.Get().entries {
				if entry.SubjectID == id {
					name = entry.Name
				}
			}
			update(func(s *docsShareState) { s.busy = true })
			props.Revoke(props.DocumentID, id, func(err error) {
				update(func(s *docsShareState) {
					s.busy = false
					if err != nil {
						s.failed, s.message = true, docsText(props.Locale, "revoke_failed")
						return
					}
					kept := s.entries[:0:0]
					for _, entry := range s.entries {
						if entry.SubjectID != id {
							kept = append(kept, entry)
						}
					}
					s.entries, s.failed = kept, false
					s.message = strings.ReplaceAll(docsText(props.Locale, "revoked"), "{name}", name)
				})
			})
		}
	})
	roleChange := ui.UseEvent(func(event ui.ChangeEvent) {
		value := event.GetValue()
		update(func(s *docsShareState) { s.role = value })
	})
	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		value := state.Get()
		if len(value.picked) == 0 || props.Share == nil || value.busy {
			return
		}
		update(func(s *docsShareState) { s.busy, s.failed, s.message = true, false, "" })
		remaining := len(value.picked)
		var failures []string
		for _, person := range value.picked {
			person := person
			props.Share(DocumentShareRequest{DocumentID: props.DocumentID, RecipientID: docsPersonSubject(person), Role: value.role}, func(err error) {
				if err != nil {
					failures = append(failures, docsPersonName(person))
				}
				remaining--
				if remaining > 0 {
					return
				}
				update(func(s *docsShareState) {
					s.busy = false
					if len(failures) > 0 {
						s.failed = true
						s.message = strings.ReplaceAll(docsText(props.Locale, "share_failed_names"), "{names}", strings.Join(failures, ", "))
						return
					}
					s.message = docsCount(props.Locale, "shared_n", len(value.picked))
					s.picked = nil
				})
				load()
			})
		}
	})

	chips := make([]ui.Node, 0, len(current.picked)+1)
	for _, person := range current.picked {
		name := docsPersonName(person)
		chips = append(chips, html.Span(html.Props{Key: "pick:" + person.ID, Class: "docs-pick-chip"},
			personAvatar(name, person.Initials, person.PhotoURL, "tiny"),
			html.Span(html.Props{}, ui.Text(name)),
			html.Button(html.Props{Class: "docs-pick-remove", Type: "button", Aria: map[string]string{"label": docsText(props.Locale, "remove") + ": " + name}, Data: map[string]string{"docs-action": "share-unpick", "docs-id": person.ID}}, productIcon("close", "docs-chip-icon")),
		))
	}
	listID := "docs-share-suggestions"
	fieldProps := html.Props{ID: "docs-share-people", Value: current.query, AutoComplete: "off", Placeholder: docsText(props.Locale, "share_placeholder"), OnInput: input, OnKeyDown: fieldKey,
		Role: "combobox", Aria: map[string]string{"expanded": strconv.FormatBool(len(suggestions) > 0 && current.query != ""), "controls": listID, "autocomplete": "list"}}
	if len(suggestions) > 0 && current.query != "" {
		fieldProps.Aria["activedescendant"] = "docs-share-option-" + suggestions[active].ID
	}
	chips = append(chips, html.WithKey(html.Input(fieldProps), "field"))
	options := []ui.Node{}
	if current.query != "" {
		for index, person := range suggestions {
			name := docsPersonName(person)
			class := "docs-share-option"
			if index == active {
				class += " is-active"
			}
			detail := strings.TrimSpace(person.Role)
			if team := strings.TrimSpace(person.Team); team != "" {
				if detail != "" {
					detail += ", "
				}
				detail += team
			}
			options = append(options, html.Li(html.Props{Key: "opt:" + person.ID, ID: "docs-share-option-" + person.ID, Class: class, Role: "option", Aria: map[string]string{"selected": strconv.FormatBool(index == active)}, Data: map[string]string{"docs-action": "share-pick", "docs-id": person.ID}},
				personAvatar(name, person.Initials, person.PhotoURL, "small"),
				html.Span(html.Props{Class: "docs-share-option-text"}, html.Span(html.Props{Class: "docs-share-option-name"}, ui.Text(name)), html.Span(html.Props{Class: "docs-share-option-detail"}, ui.Text(detail))),
			))
		}
		if len(options) == 0 {
			options = append(options, html.Li(html.Props{Key: "none", Class: "docs-share-none"}, ui.Text(docsText(props.Locale, "share_no_people"))))
		}
	}
	shareLabel := docsText(props.Locale, "share_action")
	if n := len(current.picked); n > 0 {
		shareLabel = docsCount(props.Locale, "share_with_n", n)
	}
	form := html.Form(html.Props{Class: "docs-share-form", OnSubmit: submit},
		html.Label(html.Props{For: "docs-share-people", Class: "docs-share-label"}, ui.Text(docsText(props.Locale, "share_add_people"))),
		html.Div(html.Props{Class: "docs-share-field"}, chips...),
		html.Ul(html.Props{ID: listID, Class: "docs-share-options", Role: "listbox", Hidden: len(options) == 0}, options...),
		html.Div(html.Props{Class: "docs-share-submit"},
			html.Label(html.Props{Class: "docs-role"},
				html.Span(html.Props{Class: "sr-only"}, ui.Text(docsText(props.Locale, "role_label"))),
				html.Select(html.Props{Name: "role", OnChange: roleChange},
					html.Option(html.Props{Value: "commenter", Selected: current.role != "viewer"}, ui.Text(docsText(props.Locale, "role_commenter"))),
					html.Option(html.Props{Value: "viewer", Selected: current.role == "viewer"}, ui.Text(docsText(props.Locale, "role_viewer"))),
				),
			),
			html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: current.busy || len(current.picked) == 0}, ui.Text(shareLabel)),
		),
	)
	access := []ui.Node{}
	switch {
	case !current.loaded:
		access = append(access, html.Li(html.Props{Key: "loading", Class: "docs-access-loading"}, ui.Text(docsText(props.Locale, "access_loading"))))
	case current.loadFailed:
		access = append(access, html.Li(html.Props{Key: "failed", Class: "docs-notice"}, ui.Text(docsText(props.Locale, "access_failed"))))
	default:
		for _, entry := range current.entries {
			controls := html.Span(html.Props{Class: "docs-access-role"}, ui.Text(docsText(props.Locale, "role_"+entry.Role)))
			row := []ui.Node{personAvatar(entry.Name, "", entry.PhotoURL, "small"), html.Span(html.Props{Class: "docs-access-name"}, ui.Text(entry.Name)), controls}
			if entry.Removable && props.Revoke != nil {
				row = append(row, html.Button(html.Props{Class: "docs-access-remove", Type: "button", Disabled: current.busy, Aria: map[string]string{"label": docsText(props.Locale, "remove") + ": " + entry.Name}, Data: map[string]string{"docs-action": "share-revoke", "docs-id": entry.SubjectID}}, ui.Text(docsText(props.Locale, "remove"))))
			}
			access = append(access, html.Li(html.Props{Key: "access:" + entry.SubjectID, Class: "docs-access-row"}, row...))
		}
	}
	status := ui.Node(nil)
	if current.message != "" {
		class, role := "docs-share-message", "status"
		if current.failed {
			class, role = "docs-notice", "alert"
		}
		status = html.P(html.Props{Class: class, Role: role}, ui.Text(current.message))
	}
	footer := ui.Node(nil)
	if href := docsShareableHref(props.Origin, props.DocumentID); href != "" {
		copyLabel := docsText(props.Locale, "copy_link")
		if current.copied {
			copyLabel = docsText(props.Locale, "link_copied")
		}
		footerChildren := []ui.Node{
			html.P(html.Props{}, ui.Text(docsText(props.Locale, "link_note"))),
			html.Button(html.Props{Class: "button secondary", Type: "button", Data: map[string]string{"docs-action": "share-copy"}}, productIcon("link", "docs-button-icon"), ui.Text(copyLabel)),
		}
		if current.copyFailed {
			footerChildren = append(footerChildren, html.Div(html.Props{Class: "docs-share-copy-fallback"},
				html.P(html.Props{Class: "docs-notice", Role: "alert"}, ui.Text(docsText(props.Locale, "copy_failed_link"))),
				html.Label(html.Props{For: "docs-share-link", Class: "sr-only"}, ui.Text(docsText(props.Locale, "share_link_label"))),
				html.Input(html.Props{ID: "docs-share-link", Class: "docs-share-link", ReadOnly: true, Value: href, Dir: "ltr"}),
			))
		}
		footer = html.Footer(html.Props{Class: "docs-share-footer"}, footerChildren...)
	}
	title := strings.ReplaceAll(docsText(props.Locale, "share_title"), "{title}", props.Title)
	return html.Div(html.Props{Class: "docs-share-root", OnClick: click},
		docsDialog("docs-share-dialog", title, docsText(props.Locale, "close"), close, keydown,
			form, status,
			html.H3(html.Props{Class: "docs-access-heading"}, ui.Text(docsText(props.Locale, "access_heading"))),
			html.Ul(html.Props{Class: "docs-access-list"}, access...),
			footer,
		))
}

// docsNamedAccess orders the access list owner first and resolves names
// through the authorized directory.
func docsNamedAccess(entries []DocumentAccessEntry, people []Person, principal, locale string) []DocumentAccessEntry {
	out := make([]DocumentAccessEntry, 0, len(entries))
	for _, entry := range entries {
		for _, person := range people {
			if docsPersonIs(person, entry.SubjectID) {
				entry.Name, entry.PhotoURL = docsPersonName(person), person.PhotoURL
				break
			}
		}
		if entry.Name == "" {
			entry.Name = humanizeSubject(entry.SubjectID)
		}
		if entry.SubjectID == principal {
			entry.Name = strings.ReplaceAll(docsText(locale, "you_named"), "{name}", entry.Name)
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Role == "owner") != (out[j].Role == "owner") {
			return out[i].Role == "owner"
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// docsPersonName is how Docs names a person: the preferred name completed
// with the legal surname ("Selene" + "Selene Navarro" reads "Selene
// Navarro"), the same rule chat and the directory use.
func docsPersonName(person Person) string {
	preferred := strings.TrimSpace(person.PreferredName)
	if preferred == "" {
		preferred = strings.TrimSpace(person.Name)
	}
	legal := strings.Fields(person.LegalName)
	if preferred != "" && len(strings.Fields(preferred)) == 1 && len(legal) > 1 {
		return preferred + " " + legal[len(legal)-1]
	}
	for _, name := range []string{person.Name, person.PreferredName, person.LegalName, person.ID} {
		if name = strings.TrimSpace(name); name != "" {
			return name
		}
	}
	return ""
}

// docsPeopleMatches finds active people whose name starts with the query or
// has a word that does, best matches first, excluding anyone who already has
// access or is already picked.
func docsPeopleMatches(people []Person, query string, exclude map[string]bool, limit int) []Person {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	type scored struct {
		person Person
		rank   int
	}
	var out []scored
	for _, person := range people {
		if person.LifecycleStatus != "" && !strings.EqualFold(person.LifecycleStatus, "active") {
			continue
		}
		if strings.TrimSpace(docsPersonSubject(person)) == "" || exclude[docsPersonSubject(person)] || exclude[person.ID] || exclude[person.WorkerID] {
			continue
		}
		name := strings.ToLower(docsPersonName(person))
		rank := -1
		if strings.HasPrefix(name, q) {
			rank = 0
		} else {
			for _, word := range strings.Fields(name) {
				if strings.HasPrefix(word, q) {
					rank = 1
					break
				}
			}
		}
		if rank < 0 {
			continue
		}
		out = append(out, scored{person, rank})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return docsPersonName(out[i].person) < docsPersonName(out[j].person)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	result := make([]Person, len(out))
	for index := range out {
		result[index] = out[index].person
	}
	return result
}

// docsPersonSubject is the identity grants and owners use for a person: the
// sign-in subject when the directory sends it, the directory id otherwise.
func docsPersonSubject(person Person) string {
	if subject := strings.TrimSpace(person.SubjectID); subject != "" {
		return subject
	}
	return strings.TrimSpace(person.ID)
}

func docsPersonIs(person Person, subject string) bool {
	return subject != "" && (person.SubjectID == subject || person.ID == subject || person.WorkerID == subject)
}
