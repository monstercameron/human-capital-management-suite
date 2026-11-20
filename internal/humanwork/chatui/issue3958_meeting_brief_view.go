package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// SavedSelectView is the Saved panel's "select messages" mode (issue 3958).
type SavedSelectView struct {
	On       bool
	Selected map[string]bool
	Count    int
}

// SavedBriefSource is one selected message in the brief's preview. Reason is
// empty for a message the brief will use.
type SavedBriefSource struct {
	Row    SavedMessageRow
	Reason string
}

// SavedBriefTarget is a conversation a brief can be drafted in.
type SavedBriefTarget struct{ ID, Name string }

// SavedBriefView is the meeting brief page of the Saved panel.
type SavedBriefView struct {
	// Stage is "setup" (purpose, time, notes and the source preview) or "draft"
	// (the editable agenda and sharing).
	Stage string
	// Checking is true while access to the saved messages is being read again.
	Checking bool
	// Error is the code of a failed recheck; the selection is kept.
	Error                string
	Purpose, When        string
	IncludeNotes         bool
	Sources              []SavedBriefSource
	Text                 string
	NotesInText          bool
	Targets              []SavedBriefTarget
	Target               string
	ShareNotes           bool
	Problems             []MeetingBriefProblem
	Status, DraftedIn    string
	UsedCount, LeftCount int
}

// chatsave3958Toolbar is the row under the tabs that starts or ends selecting
// and, while selecting, prepares the brief.
func chatsave3958Toolbar(view SavedMessagesView, all int) ui.Node {
	loc := view.Locale
	if view.Loading || view.Error != "" || (all == 0 && !view.Select.On) {
		return nil
	}
	if !view.Select.On {
		return html.Div(html.Props{Class: "chatsave-select-bar"},
			html.Button(html.Props{ID: "chatsave-select-start", Class: "chatsave-labelchip chatsave-select-start", Type: "button", Text: chatsave002Text(loc, "br_select"), Data: map[string]string{"saved-action": "select-start"}}))
	}
	nodes := []ui.Node{
		html.Span(html.Props{ID: "chatsave-select-count", Class: "chatsave-select-count", Role: "status", Aria: map[string]string{"live": "polite"}, Text: chatsave002Text(loc, "br_selected", "n", chatsave002Num(loc, view.Select.Count))}),
		html.Button(html.Props{ID: "chatsave-prepare", Class: "button small chatsave-prepare", Type: "button", Text: chatsave002Text(loc, "br_prepare"), Disabled: view.Select.Count == 0 || view.Select.Count > BriefMaxSources, Data: map[string]string{"saved-action": "brief-open"}}),
		html.Button(html.Props{ID: "chatsave-select-stop", Class: "button secondary small", Type: "button", Text: chatsave002Text(loc, "br_select_done"), Data: map[string]string{"saved-action": "select-stop"}}),
	}
	bar := []ui.Node{html.Div(html.Props{Class: "chatsave-select-actions"}, nodes...)}
	hint := chatsave002Text(loc, "br_pick_hint")
	if view.Select.Count > BriefMaxSources {
		hint = chatsave002Text(loc, "br_too_many")
	}
	bar = append(bar, html.P(html.Props{ID: "chatsave-select-hint", Class: "chatsave-note-private", Text: hint}))
	return html.Div(html.Props{Class: "chatsave-select-bar is-selecting"}, bar...)
}

// chatsave3958Pick is the checkbox in front of a saved message while selecting.
func chatsave3958Pick(view SavedMessagesView, row SavedMessageRow, key, who string) ui.Node {
	if !view.Select.On {
		return nil
	}
	loc := view.Locale
	label := chatsave002Text(loc, "br_pick_gone")
	if who != "" {
		label = chatsave002Text(loc, "br_pick_one", "who", who)
	}
	return html.Input(html.Props{ID: "chatsave-pick-" + row.PostID, Class: "chatsave-pick-box", Type: "checkbox", Checked: view.Select.Selected[key],
		Data: chatsave3932Data(row, map[string]string{"saved-action": "select-toggle"}), Aria: map[string]string{"label": label, "describedby": "chatsave-select-hint"}})
}

func chatsave3958Source(view SavedBriefView, loc string, source SavedBriefSource, now time.Time) ui.Node {
	row := source.Row
	nodes := []ui.Node{}
	if source.Reason != "" {
		nodes = append(nodes, html.P(html.Props{Class: "chatsave-unavailable", Text: MeetingBriefReasonText(loc, source.Reason)}))
		return html.Li(html.Props{Class: "chatsave-brief-source is-unavailable"}, nodes...)
	}
	meta := []string{}
	if row.Author != "" {
		meta = append(meta, row.Author)
	}
	if row.Channel != "" {
		channel := row.Channel
		if row.InChannel {
			channel = "#" + strings.TrimPrefix(channel, "#")
		}
		meta = append(meta, channel)
	}
	if !row.SentAt.IsZero() {
		meta = append(meta, SavedTimeLabel(loc, row.SentAt, now))
	}
	nodes = append(nodes,
		html.P(html.Props{Class: "chatsave-brief-meta", Dir: "auto", Text: strings.Join(meta, " · ")}),
		html.P(html.Props{Class: "chatsave-brief-excerpt", Dir: "auto", Text: MeetingBriefExcerpt(row.Body)}))
	if strings.TrimSpace(row.Note) != "" {
		note := chatsave002Text(loc, "br_has_note")
		if !view.IncludeNotes {
			note += " · " + chatsave002Text(loc, "br_notes_hint")
		}
		nodes = append(nodes, html.P(html.Props{Class: "chatsave-brief-notechip", Text: note}))
	}
	// When the list could not be read again, the original is reached through
	// its conversation, which the service checks when it opens.
	action, open := "open", chatsave002Text(loc, "br_open_source")
	if view.Error != "" {
		action, open = "open-channel", chatsave002Text(loc, "br_open_conv")
	}
	nodes = append(nodes, html.Button(html.Props{ID: "chatsave-brief-open-" + row.PostID, Class: "chatsave-labelaction", Type: "button", Text: open, Data: map[string]string{"saved-action": action, "saved-host": row.TenantID, "saved-conversation": row.ConversationID, "saved-post": row.PostID}}))
	return html.Li(html.Props{Class: "chatsave-brief-source"}, nodes...)
}

func chatsave3958Field(id, label, hint, value string, max int) ui.Node {
	return html.Div(html.Props{Class: "chatsave-brief-field"},
		html.Label(html.Props{For: id, Class: "chatsave-brief-label", Text: label}),
		html.Input(html.Props{ID: id, Class: "chatsave-note-input", Name: id, Type: "text", MaxLength: max, AutoComplete: "off", Dir: "auto", Data: map[string]string{"chat-value": value}, Value: value, Aria: map[string]string{"describedby": hint}}))
}

// chatsave3958Render is the whole panel while a brief is open.
func chatsave3958Render(view SavedMessagesView) ui.Node {
	loc := view.Locale
	brief := view.Brief
	dir := "ltr"
	if strings.HasPrefix(loc, "ar") {
		dir = "rtl"
	}
	now := view.Now
	if now.IsZero() {
		now = time.Now()
	}
	copy := SavedMessagesCopy(loc)
	top := chatux032Header(chatux032HeaderProps{Class: "chatsave-header", Title: chatsave002Text(loc, "br_page_title"),
		Leading: []ui.Node{html.Button(html.Props{ID: "chatsave-brief-back", Class: "icon-button chatsave-brief-back", Type: "button", Title: chatsave002Text(loc, "br_back"), Data: map[string]string{"saved-action": "brief-back"}, Aria: map[string]string{"label": chatsave002Text(loc, "br_back")}}, icon("arrow-left"))},
		Close:   chatux032CloseButton(copy.Close, false, map[string]string{"saved-action": "close"}, "")})
	nodes := []ui.Node{html.Div(html.Props{Class: "chatsave-top"}, top),
		html.P(html.Props{ID: "chatsave-brief-private", Class: "chatsave-privacy", Text: chatsave002Text(loc, "br_private")})}

	status, role := "", "status"
	switch {
	case brief.Checking:
		status = chatsave002Text(loc, "br_checking")
	case brief.Error != "":
		status, role = chatsave002Text(loc, "br_error"), "alert"
	case brief.Stage != "draft" && brief.UsedCount == 0:
		status = chatsave002Text(loc, "br_none_usable")
	case brief.Status == "copied":
		status = chatsave002Text(loc, "br_copied")
	case brief.Status == "copy_failed":
		status, role = chatsave002Text(loc, "br_copy_failed"), "alert"
	case brief.Status == "choose":
		status, role = chatsave002Text(loc, "br_choose_first"), "alert"
	case brief.Status == "drafted":
		status = chatsave002Text(loc, "br_drafted", "name", brief.DraftedIn)
	}
	nodes = append(nodes, html.P(html.Props{ID: "chatsave-brief-status", Class: "chatsave-brief-status", Role: role, Text: status, Aria: map[string]string{"live": briefLive(role)}}))
	if brief.Error != "" {
		nodes = append(nodes, html.Button(html.Props{ID: "chatsave-brief-retry", Class: "button secondary small", Type: "button", Text: chatsave002Text(loc, "br_retry"), Data: map[string]string{"saved-action": "brief-retry"}}))
	}

	used, left := []ui.Node{}, []ui.Node{}
	for _, source := range brief.Sources {
		if source.Reason == "" {
			used = append(used, chatsave3958Source(*brief, loc, source, now))
		} else {
			left = append(left, chatsave3958Source(*brief, loc, source, now))
		}
	}
	sources := []ui.Node{}
	if len(used) > 0 {
		sources = append(sources, html.H3(html.Props{Class: "chatsave-brief-heading", Text: chatsave002Text(loc, "br_sources") + " (" + chatsave002Num(loc, len(used)) + ")"}), html.Ul(html.Props{Class: "chatsave-brief-sources"}, used...))
	}
	if len(left) > 0 {
		sources = append(sources, html.H3(html.Props{Class: "chatsave-brief-heading", Text: chatsave002Text(loc, "br_left") + " (" + chatsave002Num(loc, len(left)) + ")"}),
			html.P(html.Props{Class: "chatsave-note-private", Text: chatsave002Text(loc, "br_left_hint")}), html.Ul(html.Props{Class: "chatsave-brief-sources", Aria: map[string]string{"label": chatsave002Text(loc, "br_left")}}, left...))
	}

	if brief.Stage == "draft" {
		nodes = append(nodes, chatsave3958Draft(view, brief, loc))
		nodes = append(nodes, html.Div(html.Props{Class: "chatsave-brief-sourcesbox"}, sources...))
	} else {
		fields := []ui.Node{
			chatsave3958Field("chatsave-brief-purpose", chatsave002Text(loc, "br_purpose_label"), "", brief.Purpose, 200),
			chatsave3958Field("chatsave-brief-when", chatsave002Text(loc, "br_when_label"), "chatsave-brief-when-hint", brief.When, 80),
			html.P(html.Props{ID: "chatsave-brief-when-hint", Class: "chatsave-note-private", Text: chatsave002Text(loc, "br_when_hint")}),
			html.Div(html.Props{Class: "chatsave-brief-check"},
				html.Input(html.Props{ID: "chatsave-brief-notes", Class: "chatsave-pick-box", Type: "checkbox", Checked: brief.IncludeNotes, Data: map[string]string{"saved-action": "brief-notes"}, Aria: map[string]string{"describedby": "chatsave-brief-notes-hint"}}),
				html.Label(html.Props{For: "chatsave-brief-notes", Text: chatsave002Text(loc, "br_notes_label")})),
			html.P(html.Props{ID: "chatsave-brief-notes-hint", Class: "chatsave-note-private", Text: chatsave002Text(loc, "br_notes_hint")}),
		}
		fields = append(fields, sources...)
		fields = append(fields, html.Div(html.Props{Class: "chatsave-note-buttons"},
			html.Button(html.Props{ID: "chatsave-brief-generate", Class: "button small", Type: "submit", Disabled: brief.Checking || brief.UsedCount == 0, Text: chatsave002Text(loc, "br_generate")}),
			html.Button(html.Props{ID: "chatsave-brief-cancel", Class: "button secondary small", Type: "button", Text: chatsave002Text(loc, "note_cancel"), Data: map[string]string{"saved-action": "brief-back"}})))
		nodes = append(nodes, html.Form(html.Props{Class: "chatsave-brief-form", Data: map[string]string{"saved-form": "brief"}, Aria: map[string]string{"busy": boolString(brief.Checking)}}, fields...))
	}
	return html.Div(html.Props{Class: "chatsave-content chatsave-brief", Dir: dir, Data: map[string]string{"saved-brief": brief.Stage}, Aria: map[string]string{"busy": boolString(brief.Checking)}}, nodes...)
}

func briefLive(role string) string {
	if role == "alert" {
		return "assertive"
	}
	return "polite"
}

func chatsave3958Draft(view SavedMessagesView, brief *SavedBriefView, loc string) ui.Node {
	nodes := []ui.Node{
		html.Label(html.Props{For: "chatsave-brief-text", Class: "chatsave-brief-label", Text: chatsave002Text(loc, "br_agenda")}),
		html.Textarea(html.Props{ID: "chatsave-brief-text", Class: "chatsave-brief-text", Name: "brief", Rows: 18, Dir: "auto", Text: brief.Text, Data: map[string]string{"chat-value": brief.Text}, Aria: map[string]string{"describedby": "chatsave-brief-text-hint"}}),
		html.P(html.Props{ID: "chatsave-brief-text-hint", Class: "chatsave-note-private", Text: chatsave002Text(loc, "br_agenda_hint")}),
		html.Div(html.Props{Class: "chatsave-note-buttons"},
			html.Button(html.Props{ID: "chatsave-brief-copy", Class: "button small", Type: "button", Text: chatsave002Text(loc, "br_copy"), Data: map[string]string{"saved-action": "brief-copy"}}),
			html.Button(html.Props{ID: "chatsave-brief-edit", Class: "button secondary small", Type: "button", Text: chatsave002Text(loc, "br_restart"), Data: map[string]string{"saved-action": "brief-setup"}})),
	}
	options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: chatsave002Text(loc, "br_share_choose")})}
	for _, target := range brief.Targets {
		options = append(options, html.Option(html.Props{Value: target.ID, Text: target.Name, Selected: target.ID == brief.Target}))
	}
	share := []ui.Node{
		html.H3(html.Props{Class: "chatsave-brief-heading", Text: chatsave002Text(loc, "br_share")}),
		html.Label(html.Props{For: "chatsave-brief-target", Class: "chatsave-brief-label", Text: chatsave002Text(loc, "br_share_pick")}),
		html.Select(html.Props{ID: "chatsave-brief-target", Class: "chatsave-brief-select", Name: "target", Value: brief.Target, Aria: map[string]string{"describedby": "chatsave-brief-share-hint"}}, options...),
	}
	if brief.NotesInText {
		share = append(share, html.Div(html.Props{Class: "chatsave-brief-check"},
			html.Input(html.Props{ID: "chatsave-brief-sharenotes", Class: "chatsave-pick-box", Type: "checkbox", Checked: brief.ShareNotes, Data: map[string]string{"saved-action": "brief-sharenotes"}}),
			html.Label(html.Props{For: "chatsave-brief-sharenotes", Text: chatsave002Text(loc, "br_share_notes")})))
	}
	share = append(share, html.P(html.Props{ID: "chatsave-brief-share-hint", Class: "chatsave-note-private", Text: chatsave002Text(loc, "br_share_hint")}))
	if len(brief.Problems) > 0 {
		items := []ui.Node{}
		for _, problem := range brief.Problems {
			key := "br_problem_source"
			switch problem.Kind {
			case "note":
				key = "br_problem_note"
			case "gone":
				key = "br_problem_gone"
			}
			items = append(items, html.Li(html.Props{Dir: "auto", Text: chatsave002Text(loc, key, "n", chatsave002Num(loc, problem.N))}))
		}
		share = append(share, html.Div(html.Props{ID: "chatsave-brief-problems", Class: "chatsave-brief-problems", Role: "alert", TabIndex: -1},
			html.P(html.Props{Text: chatsave002Text(loc, "br_problem_head")}), html.Ul(html.Props{}, items...)))
	}
	share = append(share, html.Button(html.Props{ID: "chatsave-brief-draft", Class: "button small", Type: "button", Disabled: len(brief.Targets) == 0, Text: chatsave002Text(loc, "br_share_button"), Data: map[string]string{"saved-action": "brief-draft"}}))
	nodes = append(nodes, html.Div(html.Props{Class: "chatsave-brief-share", Role: "group", Aria: map[string]string{"label": chatsave002Text(loc, "br_share")}}, share...))
	return html.Div(html.Props{Class: "chatsave-brief-draft"}, nodes...)
}

// SavedBriefDraftedText is the notice that a brief is waiting in a message box.
func SavedBriefDraftedText(locale, name string) string {
	return chatsave002Text(locale, "br_drafted", "name", name)
}

// SavedBriefTargets are the conversations a brief can be drafted in: the ones
// the person has joined, by the name the sidebar shows. Agents are left out,
// since a brief is for people.
func SavedBriefTargets(conversations []Conversation) []SavedBriefTarget {
	var out []SavedBriefTarget
	for _, conversation := range conversations {
		name := strings.TrimSpace(conversation.Name)
		if conversation.ID == "" || name == "" || !conversation.Joined || conversation.Agent || chatsaveBriefLooksInternal(name) {
			continue
		}
		out = append(out, SavedBriefTarget{ID: conversation.ID, Name: name})
	}
	return out
}

func chatsaveBriefLooksInternal(name string) bool { return strings.HasPrefix(name, "hcmnext.") }

const Issue3958Styles = `
.chatsave-select-bar{margin-block-start:8px;min-width:0}
.chatsave-select-actions{display:flex;flex-wrap:wrap;align-items:center;gap:8px}
.chatsave-select-count{margin-inline-end:auto;color:var(--muted);font-size:.8125rem;font-variant-numeric:tabular-nums}
.chatsave-pick-box{flex:none;inline-size:18px;block-size:18px;margin:0;accent-color:var(--accent);cursor:pointer}
.chatsave-item.is-selecting{cursor:pointer}
.chatsave-brief-field{display:flex;flex-direction:column;gap:4px;margin-block-start:12px}
.chatsave-brief-label{color:var(--ink);font-size:.8125rem;font-weight:600}
.chatsave-brief-check{display:flex;align-items:center;gap:8px;margin-block-start:12px;font-size:.875rem}
.chatsave-brief-heading{margin:16px 0 6px;font-size:.875rem}
.chatsave-brief-sources{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:8px}
.chatsave-brief-source{padding:8px 10px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);min-width:0}
.chatsave-brief-meta{margin:0;color:var(--muted);font-size:.75rem}
.chatsave-brief-excerpt{margin:2px 0 0;overflow-wrap:anywhere;font-size:.875rem}
.chatsave-brief-notechip{margin:4px 0 0;color:var(--muted);font-size:.75rem}
.chatsave-brief-status{min-height:1.25em;margin:8px 0 0;font-size:.8125rem}
.chatsave-brief-text{inline-size:100%;box-sizing:border-box;min-block-size:240px;padding:10px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem;line-height:1.45;resize:vertical}
.chatsave-brief-share{margin-block-start:16px;display:flex;flex-direction:column;gap:8px;align-items:flex-start}
.chatsave-brief-select{max-inline-size:100%;min-block-size:32px;padding:4px 8px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit}
.chatsave-brief-problems{align-self:stretch;padding:8px 12px;border:1px solid var(--danger,var(--line));border-radius:var(--hcm-radius-control);font-size:.8125rem}
.chatsave-brief-problems ul{margin:4px 0 0;padding-inline-start:18px}
.chatsave-brief-text-hint,.chatsave-brief .chatsave-note-private{margin:4px 0 0}
@media(max-width:760px),(pointer:coarse){
.chatsave-pick-box{inline-size:24px;block-size:24px}
.chatsave-brief-select,.chatsave-brief .chatsave-note-input,.chatsave-brief .button,.chatsave-select-bar .button{min-block-size:44px}
.chatsave-brief-check{min-block-size:44px}
.chatsave-brief-back.icon-button{inline-size:44px;min-inline-size:44px;block-size:44px;min-block-size:44px}
}
@media(prefers-reduced-motion:reduce){.chatsave-brief *{transition:none!important;animation:none!important}}
`
