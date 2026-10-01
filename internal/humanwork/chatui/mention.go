package chatui

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// mentionLimit is how many people the suggestion list offers at once.
const mentionLimit = 8

// mentionState is the composer's open "@" suggestion list. Start and Caret are
// UTF-16 offsets into the field, the units a textarea's selection reports.
type mentionState struct {
	Target      string
	Query       string
	Start, End  int
	Active      int
	Open        bool
	LoadedFresh bool
}

// mentionCandidate is one person the viewer can mention.
type mentionCandidate struct {
	ID, Name string
	Member   bool
}

// PersonaMentionSuggestion is supplied by the caller after it has resolved
// current, invocable personas for this viewer and conversation. This package
// never discovers personas from message text or from the people directory.
type PersonaMentionSuggestion struct {
	TenantID, ID, Display                                      string
	Purpose, Owner, Version                                    string
	Invocable, AudienceIncludesViewer, InstalledInConversation bool
	Skills                                                     []PersonaMentionSkill
	DataClasses, CannotDo                                      []string
	ReplyPlacement                                             PersonaReplyPlacement
	// Actor carries trusted identity attribution when the server provides it.
	// The menu never derives this from Display.
	Actor *PersonaActor
}

// ChatReference is the typed reference sent alongside a chat post. Display is
// a rendering snapshot; Kind, TenantID and ID carry canonical identity.
type ChatReference struct {
	Kind, TenantID, ID, Display, ConversationID string
}

// ResolvedPersonaMention combines the canonical candidate returned by the
// server's chat reference resolver with the safe profile projection shown in
// the menu. Eligibility is represented by presence in this server-filtered
// list; client-set booleans never grant visibility.
type ResolvedPersonaMention struct {
	Reference               ChatReference
	Purpose, Owner, Version string
	Skills                  []PersonaMentionSkill
	DataClasses, CannotDo   []string
	ReplyPlacement          PersonaReplyPlacement
	Actor                   *PersonaActor
}

type mentionOption struct {
	person  *mentionCandidate
	persona *ResolvedPersonaMention
}

func (m Model) personaMentionsEnabled(target string) bool {
	if target == "thread-composer" {
		return m.Callbacks.ReplyInThreadWithReferences != nil
	}
	return m.Callbacks.SendMessageWithReferences != nil
}

// selectPersonaMention rejects missing, malformed, or stale menu entries. The
// caller must pass only the current authorized suggestions and revalidate the
// returned reference at post commit.
func selectPersonaMention(candidates []ResolvedPersonaMention, index int, conversationID string) (ChatReference, bool) {
	if index < 0 || index >= len(candidates) {
		return ChatReference{}, false
	}
	reference := candidates[index].Reference
	if !validResolvedPersonaReference(reference, conversationID) {
		return ChatReference{}, false
	}
	return reference, true
}

func validResolvedPersonaReference(reference ChatReference, conversationID string) bool {
	return reference.Kind == "AGENT_MENTION" && strings.TrimSpace(reference.TenantID) != "" && strings.TrimSpace(reference.ID) != "" && strings.TrimSpace(reference.Display) != "" && conversationID != "" && reference.ConversationID == conversationID
}

// applyPersonaMention inserts the selected display label while retaining the
// canonical reference separately for the typed send-post path.
func applyPersonaMention(value string, start, end int, candidate ResolvedPersonaMention, conversationID string) (string, int, ChatReference, bool) {
	reference, ok := selectPersonaMention([]ResolvedPersonaMention{candidate}, 0, conversationID)
	if !ok {
		return value, end, ChatReference{}, false
	}
	updated, caret := insertEmojiAtUTF16(value, "@"+strings.TrimSpace(reference.Display)+" ", start, end)
	return updated, caret, reference, true
}

func personaMentionCandidates(m Model, query string) []ResolvedPersonaMention {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]ResolvedPersonaMention, 0, len(m.ResolvedPersonaMentions))
	for _, candidate := range m.ResolvedPersonaMentions {
		reference := candidate.Reference
		display := strings.TrimSpace(reference.Display)
		if !validResolvedPersonaReference(reference, m.SelectedID) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(display), q) && !strings.Contains(strings.ToLower(reference.ID), q) {
			continue
		}
		candidate.Reference.Display = display
		out = append(out, candidate)
		if len(out) == mentionLimit {
			break
		}
	}
	return out
}

func mentionOptions(m Model, query, target string) []mentionOption {
	people := mentionCandidates(m, query)
	var personas []ResolvedPersonaMention
	if m.personaMentionsEnabled(target) {
		personas = personaMentionCandidates(m, query)
	}
	options := make([]mentionOption, 0, len(people)+len(personas))
	for i := range people {
		person := people[i]
		options = append(options, mentionOption{person: &person})
	}
	for i := range personas {
		persona := personas[i]
		options = append(options, mentionOption{persona: &persona})
	}
	return options
}

// mentionTokenAt finds the "@query" the caret sits at the end of. The "@" must
// start a word, and the query is the run of name characters typed after it,
// so an email address or a finished mention followed by a space is ignored.
func mentionTokenAt(value string, caret int) (query string, start int, ok bool) {
	units := utf16.Encode([]rune(value))
	if caret < 0 || caret > len(units) {
		return "", 0, false
	}
	i := caret
	for i > 0 {
		r := rune(units[i-1])
		if r == '@' {
			break
		}
		if !mentionRune(r) || caret-i >= 40 {
			return "", 0, false
		}
		i--
	}
	if i == 0 || rune(units[i-1]) != '@' {
		return "", 0, false
	}
	at := i - 1
	if at > 0 {
		prev := rune(units[at-1])
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' || prev == '@' {
			return "", 0, false
		}
	}
	return string(utf16.Decode(units[i:caret])), at, true
}

func mentionRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' || r == '\''
}

// mentionCandidates ranks the room's members ahead of the wider directory and
// names that start with the query ahead of names with a later word that does.
func mentionCandidates(m Model, query string) []mentionCandidate {
	q := strings.ToLower(strings.TrimSpace(query))
	type scored struct {
		c    mentionCandidate
		rank int
	}
	seen := map[string]bool{}
	var out []scored
	consider := func(id, name string, member bool) {
		name = strings.TrimSpace(name)
		// Members carry subject IDs and the directory carries worker refs, so
		// the same person can arrive under two IDs; the name key merges them.
		nameKey := "name:" + strings.ToLower(name)
		if id == "" || name == "" || seen[id] || seen[nameKey] || name == id {
			return
		}
		seen[nameKey] = true
		lower := strings.ToLower(name)
		rank := -1
		switch {
		case q == "" || strings.HasPrefix(lower, q):
			rank = 0
		default:
			for _, word := range strings.Fields(lower)[1:] {
				if strings.HasPrefix(word, q) {
					rank = 1
					break
				}
			}
		}
		if rank < 0 {
			return
		}
		if !member {
			rank += 2
		}
		seen[id] = true
		out = append(out, scored{mentionCandidate{ID: id, Name: name, Member: member}, rank})
	}
	// Authors already on screen carry resolved names even when the member
	// list has not finished resolving its own, and they are in the room.
	authors := map[string]string{}
	for _, list := range [][]Message{m.Messages, m.ThreadMessages} {
		for _, msg := range list {
			if msg.AuthorID != "" && strings.TrimSpace(msg.Author) != "" && msg.Author != msg.AuthorID {
				authors[msg.AuthorID] = msg.Author
			}
		}
	}
	for _, member := range m.Members {
		name := member.Name
		if (strings.TrimSpace(name) == "" || name == member.ID) && authors[member.ID] != "" {
			name = authors[member.ID]
		}
		consider(member.ID, name, true)
	}
	ids := make([]string, 0, len(authors))
	for id := range authors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		consider(id, authors[id], true)
	}
	for _, person := range m.SearchDirectory {
		consider(person.ID, person.Name, false)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return strings.ToLower(out[i].c.Name) < strings.ToLower(out[j].c.Name)
	})
	if len(out) > mentionLimit {
		out = out[:mentionLimit]
	}
	result := make([]mentionCandidate, len(out))
	for i := range out {
		result[i] = out[i].c
	}
	return result
}

// applyMention replaces the "@query" between start and end with the person's
// full name and a trailing space, the form chatBodyHasMention matches.
func applyMention(value string, start, end int, name string) (string, int) {
	return insertEmojiAtUTF16(value, "@"+name+" ", start, end)
}

// nextMention moves the highlighted suggestion, wrapping at either end.
func nextMention(active, delta, count int) int {
	if count <= 0 {
		return 0
	}
	return ((active+delta)%count + count) % count
}

// mentionMenu is the suggestion list above a composer. The field keeps focus
// and owns the keys; aria-activedescendant names the highlighted person.
func mentionMenu(m Model, state mentionState, target string) ui.Node {
	if !state.Open || state.Target != target {
		return html.Div(html.Props{Class: "mention-slot"})
	}
	options := mentionOptions(m, state.Query, target)
	if len(options) == 0 {
		empty := m.tf(KeyMentionNone, map[string]string{"query": state.Query})
		if m.personaMentionsEnabled(target) {
			empty = personaMentionText(m.Locale, "none")
		}
		return html.Div(html.Props{ID: target + "-mentions", Class: "mention-menu empty", Role: "status", Aria: map[string]string{"live": "polite"}}, html.P(html.Props{Class: "mention-empty", Text: empty}))
	}
	rows := make([]ui.Node, 0, len(options)+2)
	peopleHeading, agentsHeading := false, false
	outsiders := false
	for i, option := range options {
		class := "mention-option"
		if i == state.Active {
			class += " active"
		}
		// People outside the room are listed once under their own heading
		// rather than carrying the same note on every row.
		if option.person != nil && !option.person.Member && !outsiders {
			outsiders = true
			rows = append(rows, html.WithKey(html.P(html.Props{Class: "mention-heading outside", Aria: map[string]string{"hidden": "true"}, Text: m.t(KeyMentionNotMember)}), "outside"))
		}
		detail, label, key := "", "", ""
		avatar := ui.Node(nil)
		if option.person != nil {
			person := *option.person
			if !peopleHeading {
				peopleHeading = true
				rows = append(rows, html.P(html.Props{Class: "mention-heading", Aria: map[string]string{"hidden": "true"}, Text: m.t(KeyMentionTitle)}))
			}
			label, key = person.Name, "person:"+person.ID
			avatar = personAvatar(m, person.ID, person.Name, "avatar small")
		} else if option.persona != nil {
			persona := *option.persona
			if !agentsHeading {
				agentsHeading = true
				rows = append(rows, html.P(html.Props{Class: "mention-heading agents", Aria: map[string]string{"hidden": "true"}, Text: personaMentionText(m.Locale, "agents")}))
			}
			label, key, detail = persona.Reference.Display, "agent:"+persona.Reference.TenantID+":"+persona.Reference.ID, personaMentionText(m.Locale, "agent_badge")
			class += " persona"
			pick := html.Button(html.Props{ID: target + "-mention-" + itoa(i+1), Class: class, Type: "button", Role: "option", TabIndex: -1,
				Data: map[string]string{"action": "mention-pick", "id": target, "extra": itoa(i + 1)},
				Aria: map[string]string{"selected": boolString(i == state.Active)}},
				html.Span(html.Props{Class: "mention-name", Text: label}),
				html.Span(html.Props{Class: "mention-detail agent-badge", Text: detail}),
				personaMentionAttribution(persona.Actor),
				html.Span(html.Props{Class: "mention-purpose", Text: persona.Purpose}))
			rows = append(rows, html.WithKey(html.Div(html.Props{Class: "mention-agent-row", Role: "group", Aria: map[string]string{"label": persona.Reference.Display}}, pick,
				html.Details(html.Props{Class: "mention-agent-profile"},
					html.Summary(html.Props{Class: "mention-profile-trigger", Text: personaMentionText(m.Locale, "profile")}),
					personaProfileCard(m.Locale, persona))), key))
			continue
		}
		rows = append(rows, html.WithKey(html.Button(html.Props{ID: target + "-mention-" + itoa(i+1), Class: class, Type: "button", Role: "option", TabIndex: -1,
			Data: map[string]string{"action": "mention-pick", "id": target, "extra": itoa(i + 1)},
			Aria: map[string]string{"selected": boolString(i == state.Active)}},
			avatar,
			html.Span(html.Props{Class: "mention-name", Text: label}),
			html.Span(html.Props{Class: "mention-detail", Text: detail})), key))
	}
	return html.Div(html.Props{ID: target + "-mentions", Class: "mention-menu", Role: "listbox", Aria: map[string]string{"label": personaMentionText(m.Locale, "menu")}},
		append(rows, html.P(html.Props{Class: "mention-hint kbd-hint", Aria: map[string]string{"hidden": "true"}, Text: m.t(KeyMentionHint)}))...)
}

func personaMentionAttribution(actor *PersonaActor) ui.Node {
	if actor == nil {
		return nil
	}
	return html.Span(html.Props{Class: "mention-attribution", Text: actor.attribution()})
}

// mentionFieldAria points a composer at its open suggestion list so a screen
// reader announces the highlighted person while focus stays in the field.
func mentionFieldAria(state mentionState, target string, aria map[string]string) map[string]string {
	out := map[string]string{"autocomplete": "list", "haspopup": "listbox", "expanded": "false"}
	for k, v := range aria {
		out[k] = v
	}
	if state.Open && state.Target == target {
		out["expanded"] = "true"
		out["controls"] = target + "-mentions"
		out["activedescendant"] = target + "-mention-" + itoa(state.Active+1)
	}
	return out
}

func mentionModelFieldAria(model Model, state mentionState, target string, aria map[string]string) map[string]string {
	out := mentionFieldAria(state, target, aria)
	if state.Open && state.Target == target && len(mentionOptions(model, state.Query, target)) == 0 {
		delete(out, "activedescendant")
	}
	return out
}

// mentionBox holds the live suggestion state. Keystrokes can arrive faster than
// renders, and a state hook read inside a handler returns the value of the
// last render: typing " in @a" then read a list that had already closed and
// the new "@a" never opened it. Handlers read and write the box; the tick
// only asks for a render.
type mentionBox struct {
	state    mentionState
	personas []personaDraftMention
	seq      uint64
}

type personaDraftMention struct {
	Target         string
	ConversationID string
	Start, End     int
	Reference      ChatReference
}

type mentionStore struct {
	box  *mentionBox
	tick ui.State[uint64]
}

func (s mentionStore) Get() mentionState { return s.box.state }

func (s mentionStore) Set(v mentionState) {
	if s.box.state == v {
		return
	}
	s.box.state = v
	s.box.seq++
	s.tick.Set(s.box.seq)
}

func (s mentionStore) AddPersona(target, conversationID string, start, end int, reference ChatReference) {
	s.box.personas = append(s.box.personas, personaDraftMention{Target: target, ConversationID: conversationID, Start: start, End: end, Reference: reference})
	s.box.seq++
	s.tick.Set(s.box.seq)
}

func (s mentionStore) ReconcilePersonas(target, value string) {
	kept := s.box.personas[:0]
	for _, item := range s.box.personas {
		if item.Target == target && !personaReferenceAt(value, item) {
			continue
		}
		kept = append(kept, item)
	}
	if len(kept) != len(s.box.personas) {
		s.box.personas = kept
		s.box.seq++
		s.tick.Set(s.box.seq)
	}
}

func (s mentionStore) PersonaReferences(target, conversationID, value string) []ChatReference {
	var out []ChatReference
	for _, item := range s.box.personas {
		if item.Target == target && item.ConversationID == conversationID && personaReferenceAt(value, item) {
			out = append(out, item.Reference)
		}
	}
	return out
}

func (s mentionStore) RemovePersonas(target, conversationID string) {
	kept := s.box.personas[:0]
	for _, item := range s.box.personas {
		if item.Target == target && item.ConversationID == conversationID {
			continue
		}
		kept = append(kept, item)
	}
	if len(kept) != len(s.box.personas) {
		s.box.personas = kept
		s.box.seq++
		s.tick.Set(s.box.seq)
	}
}

func personaReferenceAt(value string, item personaDraftMention) bool {
	units := utf16.Encode([]rune(value))
	if item.Start < 0 || item.End > len(units) || item.End <= item.Start {
		return false
	}
	want := utf16.Encode([]rune("@" + item.Reference.Display))
	if item.End-item.Start != len(want) {
		return false
	}
	for i := range want {
		if units[item.Start+i] != want[i] {
			return false
		}
	}
	return item.End == len(units) || unicode.IsSpace(rune(units[item.End]))
}

// composerNotice is a one-line status above the composer for a command that
// could not run; an empty slot keeps the textarea's position stable.
func composerNotice(text string) ui.Node {
	if text == "" {
		return html.Div(html.Props{Class: "composer-notice-slot"})
	}
	return html.P(html.Props{Class: "composer-notice", Role: "status", Aria: map[string]string{"live": "polite"}, Text: text})
}
