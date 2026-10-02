package chatui

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// mentionLimit is how many people the suggestion list offers at once.
const mentionLimit = 8

// mentionState is the composer's open "@" suggestion list. Start and Caret are
// UTF-16 offsets into the field, the units a textarea's selection reports.
type mentionState struct {
	Target        string
	Query         string
	Start, End    int
	Active        int
	Open          bool
	Details       bool
	LoadedFresh   bool
	ShowAllPeople bool
}

// mentionCandidate is one person the viewer can mention.
type mentionCandidate struct {
	ID, HomeTenantID, Name string
	Member                 bool
}

// PersonaMentionSuggestion is supplied by the caller after it has resolved
// current, invocable personas for this viewer and conversation. This package
// never discovers personas from message text or from the people directory.
type PersonaMentionSuggestion struct {
	Icon                                                       agenticon.Value
	IconRevision                                               int64
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
	Icon                    agenticon.Value
	IconRevision            int64
	Reference               ChatReference
	Handle, Initials        string
	AvatarURL               string
	Purpose, Owner, Version string
	DocumentScope           string
	Skills                  []PersonaMentionSkill
	DataClasses, CannotDo   []string
	ReplyPlacement          PersonaReplyPlacement
	Actor                   *PersonaActor
}

type mentionOption struct {
	person  *mentionCandidate
	persona *ResolvedPersonaMention
}

// mentionDecision is the complete autocomplete decision for one composer
// snapshot. Keeping this independent of browser events makes every ordering
// of typing, member projection and agent lookup directly testable.
type mentionDecision struct {
	State   mentionState
	Options []mentionOption
	Lookup  PersonaLookupState
}

type mentionKeyAction string

const (
	mentionKeyNone     mentionKeyAction = ""
	mentionKeyNext     mentionKeyAction = "next"
	mentionKeyPrevious mentionKeyAction = "previous"
	mentionKeySelect   mentionKeyAction = "select"
	mentionKeyDetails  mentionKeyAction = "details"
	mentionKeyBack     mentionKeyAction = "back"
	mentionKeyClose    mentionKeyAction = "close"
)

func mentionActionForKey(key string) mentionKeyAction {
	switch key {
	case "ArrowDown":
		return mentionKeyNext
	case "ArrowUp":
		return mentionKeyPrevious
	case "Enter":
		return mentionKeySelect
	case "Tab":
		return mentionKeyDetails
	case "Escape":
		return mentionKeyClose
	default:
		return mentionKeyNone
	}
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

func selectedAgentReference(m Model) (ChatReference, bool) {
	conversation := m.selected()
	if !conversation.Agent || strings.TrimSpace(conversation.AgentID) == "" {
		return ChatReference{}, false
	}
	for _, persona := range m.ResolvedPersonaMentions {
		if persona.Reference.ID == conversation.AgentID && validResolvedPersonaReference(persona.Reference, m.SelectedID) {
			return persona.Reference, true
		}
	}
	return ChatReference{}, false
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

// composerSendPayload resolves the live textarea value into the payload used
// by both keyboard and form submission. References remain separate from the
// visible text so a reader can continue typing directly after a selected
// mention without replacing the textarea with a second editing surface.
func composerSendPayload(raw, fallback string, references []ChatReference) (string, []ChatReference, bool) {
	body := strings.TrimSpace(raw)
	if body == "" {
		body = strings.TrimSpace(fallback)
	}
	if !composerMessageReady(body) {
		return "", nil, false
	}
	return body, append([]ChatReference(nil), references...), true
}

func composerMessageReady(body string) bool {
	body = strings.TrimSpace(body)
	return body != "" && body != "@"
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
		handle := strings.TrimSpace(strings.TrimPrefix(candidate.Handle, "@"))
		if q != "" && !strings.Contains(strings.ToLower(display), q) && !strings.Contains(strings.ToLower(reference.ID), q) && !strings.Contains(strings.ToLower(handle), q) {
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
	options, _ := mentionOptionsForState(m, query, target, false)
	return options
}

func mentionOptionsForState(m Model, query, target string, showAllPeople bool) ([]mentionOption, bool) {
	var people []mentionCandidate
	if !m.selected().Agent {
		people = mentionCandidates(m, query)
	}
	var personas []ResolvedPersonaMention
	var allPersonas []ResolvedPersonaMention
	if m.personaMentionsEnabled(target) {
		personas = personaMentionCandidates(m, query)
		allPersonas = personaMentionCandidates(m, "")
	}
	people = excludeAgentIdentities(people, allPersonas)
	return combineMentionOptions(query, people, personas, showAllPeople)
}

// decideMentionMenu derives both whether the list is open and every selectable
// row from the current field text, caret, lookup state and authorized member
// projections. A lookup completion can therefore rerun the same decision as a
// keystroke instead of depending on another character being typed.
func decideMentionMenu(value string, caret int, target string, lookup PersonaLookupState, people []mentionCandidate, personas []ResolvedPersonaMention) mentionDecision {
	query, start, ok := mentionTokenAt(value, caret)
	if !ok {
		return mentionDecision{Lookup: lookup}
	}
	people = excludeAgentIdentities(people, personas)
	people = filterMentionPeople(people, query)
	personas = filterMentionPersonas(personas, query)
	options, _ := combineMentionOptions(query, people, personas, false)
	return mentionDecision{
		State:   mentionState{Target: target, Query: query, Start: start, End: caret, Open: true},
		Options: options,
		Lookup:  lookup,
	}
}

func filterMentionPeople(people []mentionCandidate, query string) []mentionCandidate {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return people
	}
	filtered := make([]mentionCandidate, 0, len(people))
	for _, person := range people {
		name := strings.ToLower(strings.TrimSpace(person.Name))
		matched := strings.HasPrefix(name, query)
		words := strings.Fields(name)
		for _, word := range words[min(1, len(words)):] {
			matched = matched || strings.HasPrefix(word, query)
		}
		if matched {
			filtered = append(filtered, person)
		}
	}
	return filtered
}

func filterMentionPersonas(personas []ResolvedPersonaMention, query string) []ResolvedPersonaMention {
	if strings.TrimSpace(query) == "" {
		return personas
	}
	filtered := make([]ResolvedPersonaMention, 0, len(personas))
	for _, persona := range personas {
		if mentionTextScore(query, persona.Reference.Display, persona.Handle, persona.Reference.ID) < 3 {
			filtered = append(filtered, persona)
		}
	}
	return filtered
}

func excludeAgentIdentities(people []mentionCandidate, personas []ResolvedPersonaMention) []mentionCandidate {
	if len(people) == 0 || len(personas) == 0 {
		return people
	}
	agentIDs := make(map[string]bool, len(personas)*2)
	for _, persona := range personas {
		for _, identity := range []string{persona.Reference.ID, strings.TrimPrefix(persona.Handle, "@")} {
			if identity = strings.ToLower(strings.TrimSpace(identity)); identity != "" {
				agentIDs[identity] = true
			}
		}
	}
	filtered := make([]mentionCandidate, 0, len(people))
	for _, person := range people {
		if agentIDs[strings.ToLower(strings.TrimSpace(person.ID))] || agentIDs[strings.ToLower(strings.TrimSpace(person.Name))] {
			continue
		}
		filtered = append(filtered, person)
	}
	return filtered
}

func combineMentionOptions(query string, people []mentionCandidate, personas []ResolvedPersonaMention, showAllPeople bool) ([]mentionOption, bool) {
	options := make([]mentionOption, 0, len(people)+len(personas))
	appendPeople := func(candidates []mentionCandidate) {
		for i := range candidates {
			person := candidates[i]
			options = append(options, mentionOption{person: &person})
		}
	}
	appendAgents := func() {
		for i := range personas {
			persona := personas[i]
			options = append(options, mentionOption{persona: &persona})
		}
	}
	if strings.TrimSpace(query) != "" {
		peopleScore, agentScore := mentionGroupScore(query, people, personas)
		if agentScore < peopleScore {
			appendAgents()
			appendPeople(people)
		} else {
			appendPeople(people)
			appendAgents()
		}
		return options, false
	}

	appendAgents()
	members := make([]mentionCandidate, 0, len(people))
	outsiders := make([]mentionCandidate, 0, len(people))
	for _, person := range people {
		if person.Member {
			members = append(members, person)
		} else {
			outsiders = append(outsiders, person)
		}
	}
	appendPeople(members)
	hasMore := !showAllPeople && len(outsiders) > 2
	if hasMore {
		outsiders = outsiders[:2]
	}
	appendPeople(outsiders)
	return options, hasMore
}

func mentionGroupScore(query string, people []mentionCandidate, personas []ResolvedPersonaMention) (int, int) {
	const noMatch = int(^uint(0) >> 1)
	peopleScore, agentScore := noMatch, noMatch
	for _, person := range people {
		peopleScore = min(peopleScore, mentionTextScore(query, person.Name))
	}
	for _, persona := range personas {
		agentScore = min(agentScore, mentionTextScore(query, persona.Reference.Display, persona.Handle, persona.Reference.ID))
	}
	return peopleScore, agentScore
}

func mentionTextScore(query string, values ...string) int {
	query = strings.ToLower(strings.TrimSpace(query))
	best := 3
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
		switch {
		case value == query:
			best = min(best, 0)
		case strings.HasPrefix(value, query):
			best = min(best, 1)
		case strings.Contains(value, query):
			best = min(best, 2)
		}
	}
	return best
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
		if id == "" || id == m.CurrentUser || name == "" || seen[id] || seen[nameKey] || name == id {
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
		for _, member := range m.Members {
			if member.ID == result[i].ID {
				result[i].HomeTenantID = member.HomeTenantID
				break
			}
		}
		if result[i].HomeTenantID == "" {
			result[i].HomeTenantID = m.CurrentTenantID
		}
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
	options, hasMorePeople := mentionOptionsForState(m, state.Query, target, state.ShowAllPeople)
	peopleCount := 0
	for _, option := range options {
		if option.person != nil {
			peopleCount++
		}
	}
	agentCount := len(options) - peopleCount
	lookupState := m.PersonaLookup
	if m.PersonaLookupConversationID != "" && m.PersonaLookupConversationID != m.SelectedID {
		lookupState = PersonaLookupIdle
	}
	// A directory that answered and holds no agent for this conversation is
	// not an agents section: the menu is the people list alone, with no
	// heading, no explanation and no error (CHATBUG-028).
	agentsEnabled := m.personaMentionsEnabled(target) && !(lookupState == PersonaLookupReady && len(m.ResolvedPersonaMentions) == 0)
	emptyQuery := strings.TrimSpace(state.Query) == ""
	agentGroupVisible := agentsEnabled && (agentCount > 0 || lookupState != PersonaLookupIdle || emptyQuery)
	if len(options) == 0 && !agentGroupVisible {
		return html.Div(html.Props{ID: target + "-mentions", Class: "mention-menu empty", Role: "status", Aria: map[string]string{"live": "polite"}}, html.P(html.Props{Class: "mention-empty", Text: m.tf(KeyMentionNone, map[string]string{"query": state.Query})}))
	}
	rows := make([]ui.Node, 0, len(options)+8)
	lastGroup := ""
	agentHeadingShown := false
	if agentsEnabled && agentCount == 0 && emptyQuery {
		rows = append(rows, mentionGroupHeading("agents", mentionMenuText(m, KeyMentionAgents)))
		agentHeadingShown = true
		switch lookupState {
		case PersonaLookupIdle:
			rows = append(rows, mentionLookupStatus("idle", mentionMenuText(m, KeyMentionAgentsIdle), "polite"))
		case PersonaLookupLoading:
			rows = append(rows, mentionLookupStatus("loading", mentionMenuText(m, KeyMentionAgentsLoading), "polite"))
		case PersonaLookupFailed:
			rows = append(rows, mentionAgentFailureState(m))
		}
	}
	for i, option := range options {
		group := "agents"
		if option.person != nil && option.person.Member {
			group = "people"
		} else if option.person != nil {
			group = "outside"
		}
		if group != lastGroup {
			switch group {
			case "agents":
				rows = append(rows, mentionGroupHeading("agents", mentionMenuText(m, KeyMentionAgents)))
				agentHeadingShown = true
			case "people":
				rows = append(rows, mentionGroupHeading("people", mentionMenuText(m, KeyMentionTitle)))
			case "outside":
				rows = append(rows,
					mentionGroupHeading("outside", mentionMenuText(m, KeyMentionNotMember)),
					html.P(html.Props{Class: "mention-group-note", Text: mentionMenuText(m, KeyMentionOutsideNote)}))
			}
			lastGroup = group
		}
		class := "mention-option"
		if i == state.Active {
			class += " active"
		}
		detail, label, key := "", "", ""
		avatar := ui.Node(nil)
		if option.person != nil {
			person := *option.person
			label, key = person.Name, "person:"+person.ID
			avatar = personAvatar(m, person.ID, person.Name, "avatar small")
		} else if option.persona != nil {
			persona := *option.persona
			label, key, detail = persona.Reference.Display, "agent:"+persona.Reference.TenantID+":"+persona.Reference.ID, mentionMenuText(m, KeyMentionAgentBadge)
			class += " persona"
			handle := strings.TrimSpace(strings.TrimPrefix(persona.Handle, "@"))
			if handle == "" {
				handle = persona.Reference.ID
			}
			pick := mentionOptionButton(target, class, i, state.Active, label+", @"+handle+", "+detail,
				personaMentionAvatarIn(m, persona),
				html.Span(html.Props{Class: "mention-agent-identity"},
					html.Span(html.Props{Class: "mention-name", Text: label}),
					html.Span(html.Props{Class: "mention-handle", Dir: "ltr", Text: "@" + handle})),
				html.Span(html.Props{Class: "mention-detail agent-badge", Text: detail}),
				personaMentionAttribution(persona.Actor),
				html.Span(html.Props{Class: "mention-purpose", Dir: "auto", Text: persona.Purpose}))
			info := html.Button(html.Props{Class: "mention-agent-info", Type: "button", TabIndex: -1,
				Data: map[string]string{"action": "mention-details", "id": target, "extra": itoa(i + 1)},
				Aria: map[string]string{"label": persona.Reference.Display + ": " + personaMentionText(m.Locale, "profile"), "expanded": boolString(i == state.Active && state.Details)}}, icon("info"))
			children := []ui.Node{html.Div(html.Props{Class: "mention-agent-option"}, pick, info)}
			if i == state.Active && state.Details {
				children = append(children, html.Div(html.Props{Class: "mention-agent-preview"}, personaProfileCard(m.Locale, persona)))
			}
			rows = append(rows, html.WithKey(html.Div(html.Props{Class: "mention-agent-row", Role: "group", Aria: map[string]string{"label": persona.Reference.Display}}, children...), key))
			continue
		}
		rows = append(rows, html.WithKey(mentionOptionButton(target, class, i, state.Active, "",
			avatar,
			html.Span(html.Props{Class: "mention-name", Text: label}),
			html.Span(html.Props{Class: "mention-detail", Text: detail})), key))
	}
	if hasMorePeople {
		rows = append(rows, html.Button(html.Props{Class: "mention-show-more", Type: "button", Data: map[string]string{"action": "mention-show-more", "id": target}, Text: mentionMenuText(m, KeyMentionShowMorePeople)}))
	}
	if agentsEnabled && agentCount == 0 && !emptyQuery && peopleCount == 0 {
		if agentGroupVisible && !agentHeadingShown {
			rows = append(rows, mentionGroupHeading("agents", mentionMenuText(m, KeyMentionAgents)))
		}
		switch lookupState {
		case PersonaLookupLoading:
			rows = append(rows, mentionLookupStatus("loading", mentionMenuText(m, KeyMentionAgentsLoading), "polite"))
		case PersonaLookupFailed:
			rows = append(rows, mentionAgentFailureState(m))
		case PersonaLookupReady:
			rows = append(rows, mentionLookupStatus("empty", mentionMenuText(m, KeyMentionAgentsNoMatch), "polite"))
		}
	}
	direction := "ltr"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(m.Locale)), "ar") {
		direction = "rtl"
	}
	// CHATBUG-044: the rows scroll above a fixed hint line.
	return chatbug044Menu(target+"-mentions", direction, mentionMenuText(m, KeyMentionMenu), rows, agentMentionKeyboardHint(m.Locale, state.Details))
}

func mentionHintText(locale string, details bool) string {
	if details {
		switch {
		case strings.HasPrefix(strings.ToLower(locale), "de"):
			return "Esc zur\u00fcck"
		case strings.HasPrefix(strings.ToLower(locale), "ar"):
			return "Esc \u0631\u062c\u0648\u0639"
		default:
			return "Esc back"
		}
	}
	switch {
	case strings.HasPrefix(strings.ToLower(locale), "de"):
		return "↑↓ auswählen · Tab Details · Enter einfügen · Esc schließen"
	case strings.HasPrefix(strings.ToLower(locale), "ar"):
		return "↑↓ اختيار · Tab التفاصيل · Enter إدراج · Esc إغلاق"
	default:
		return englishCopy[KeyMentionHint]
	}
}

func mentionAgentFailureState(m Model) ui.Node {
	return html.Div(html.Props{Class: "mention-agent-state error", Role: "status", Aria: map[string]string{"live": "assertive"}},
		html.P(html.Props{Text: mentionMenuText(m, KeyMentionAgentsFailed)}),
		html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "persona-mention-retry"}, Text: mentionMenuText(m, KeyMentionAgentsRetry)}))
}

func mentionGroupHeading(class, text string) ui.Node {
	return html.P(html.Props{Class: "mention-heading " + class, Text: text})
}

func mentionLookupStatus(class, text, live string) ui.Node {
	return html.P(html.Props{Class: "mention-agent-state " + class, Role: "status", Aria: map[string]string{"live": live}, Text: text})
}

// mentionOptionButton is the single selectable row contract for people and
// agents. Both kinds therefore share keyboard indices, option semantics and
// delegated selection data even though their visible details differ.
func mentionOptionButton(target, class string, index, active int, label string, children ...ui.Node) ui.Node {
	aria := map[string]string{"selected": boolString(index == active)}
	if label != "" {
		aria["label"] = label
	}
	return html.Button(html.Props{
		ID: target + "-mention-" + itoa(index+1), Class: class, Type: "button", Role: "option", TabIndex: -1,
		Data: map[string]string{"action": "mention-pick", "id": target, "extra": itoa(index + 1)}, Aria: aria,
	}, children...)
}

func personaMentionAvatar(persona ResolvedPersonaMention) ui.Node {
	return personaMentionAvatarIn(Model{AgentIconsReady: true}, persona)
}

// personaMentionAvatarIn draws the agent's own icon in the mention menu: its
// stored icon, or its own fallback among the agents the model knows.
func personaMentionAvatarIn(m Model, persona ResolvedPersonaMention) ui.Node {
	return agentDMAvatar(persona.Reference.Display, "avatar small agent-dm-avatar", agentIconFor(m, []string{persona.Reference.ID}, persona.Reference.Display, persona.Icon))
}

func mentionMenuText(m Model, key string) string {
	language := strings.ToLower(strings.TrimSpace(m.Locale))
	if index := strings.IndexByte(language, '-'); index > 0 {
		language = language[:index]
	}
	localized := map[string]map[string]string{
		"de": {KeyMentionTitle: "Personen in dieser Unterhaltung", KeyMentionNotMember: "Nicht in dieser Unterhaltung", KeyMentionMenu: "Person erwähnen", KeyMentionAgents: "Agenten", KeyMentionAgentBadge: "Agent", KeyMentionAgentsLoading: "Agenten werden geladen…", KeyMentionAgentsIdle: "Agenten in dieser Unterhaltung werden hier angezeigt.", KeyMentionAgentsEmpty: "In dieser Unterhaltung gibt es noch keine Agenten. Sie können trotzdem auf der Seite „Agenten“ fragen.", KeyMentionAgentsNoMatch: "Keine Agenten passen zu dieser Suche.", KeyMentionAgentsFailed: "Die Agentenliste konnte nicht geladen werden.", KeyMentionAgentsRetry: "Erneut versuchen", KeyMentionAgentsPage: "Seite „Agenten“", KeyMentionAddAgent: "Agent zu dieser Unterhaltung hinzufügen", KeyMentionOutsideNote: "Sie sind nicht in dieser Unterhaltung und werden nicht benachrichtigt, solange Sie sie nicht hinzufügen.", KeyMentionShowMorePeople: "Weitere Personen anzeigen", KeyMentionAgentReplyHint: "{name} antwortet hier. Der Agent verwendet nur Inhalte, die Sie bereits sehen können."},
		"ar": {KeyMentionTitle: "الأشخاص في هذه المحادثة", KeyMentionNotMember: "ليسوا في هذه المحادثة", KeyMentionMenu: "الإشارة إلى شخص", KeyMentionAgents: "الوكلاء", KeyMentionAgentBadge: "وكيل", KeyMentionAgentsLoading: "جارٍ تحميل الوكلاء…", KeyMentionAgentsIdle: "يظهر هنا الوكلاء الموجودون في هذه المحادثة.", KeyMentionAgentsEmpty: "لا يوجد وكلاء في هذه المحادثة حتى الآن. لا يزال بإمكانك طرح سؤالك في صفحة الوكلاء.", KeyMentionAgentsNoMatch: "لا يوجد وكلاء يطابقون هذا البحث.", KeyMentionAgentsFailed: "تعذر تحميل قائمة الوكلاء.", KeyMentionAgentsRetry: "حاول مرة أخرى", KeyMentionAgentsPage: "صفحة الوكلاء", KeyMentionAddAgent: "إضافة وكيل إلى هذه المحادثة", KeyMentionOutsideNote: "هؤلاء الأشخاص ليسوا في هذه المحادثة ولن يتلقوا إشعارًا ما لم تضفهم.", KeyMentionShowMorePeople: "عرض المزيد من الأشخاص", KeyMentionAgentReplyHint: "سيرد {name} هنا. ولا يستخدم إلا ما يمكنك رؤيته بالفعل."},
	}
	return chatbug039Text(key, localized[language][key], englishCopy[key])
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
	out := map[string]string{"autocomplete": "list", "haspopup": "listbox", "expanded": "false", "keyshortcuts": "ArrowDown ArrowUp Enter Tab Escape"}
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
	if state.Open && state.Target == target {
		options, _ := mentionOptionsForState(model, state.Query, target, state.ShowAllPeople)
		if len(options) != 0 {
			return out
		}
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
	state          mentionState
	personas       []personaDraftMention
	conversationID string
	seq            uint64
}

type personaDraftMention struct {
	Target         string
	ConversationID string
	Start, End     int
	Reference      ChatReference
	Detached       bool
}

type mentionStore struct {
	box  *mentionBox
	tick ui.State[uint64]
}

func (s mentionStore) Get() mentionState { return s.box.state }

// ForConversation drops transient menu state when the composer moves to a
// different conversation. It mutates the ref before the current render reads
// it, so it does not schedule a second render from inside a render.
func (s mentionStore) ForConversation(conversationID string) {
	if s.box == nil || s.box.conversationID == conversationID {
		return
	}
	s.box.conversationID = conversationID
	s.box.state = mentionState{}
}

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

func (s mentionStore) AddPersonaToken(target, conversationID string, reference ChatReference) {
	s.box.personas = append(s.box.personas, personaDraftMention{Target: target, ConversationID: conversationID, Reference: reference, Detached: true})
	s.box.seq++
	s.tick.Set(s.box.seq)
}

func (s mentionStore) ReconcilePersonas(target, value string) {
	kept := s.box.personas[:0]
	for _, item := range s.box.personas {
		if item.Target == target && !item.Detached && !personaReferenceAt(value, item) {
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
		if item.Target == target && item.ConversationID == conversationID && (item.Detached || personaReferenceAt(value, item)) {
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

func (s mentionStore) PersonaDisplay(target, conversationID string) string {
	for i := len(s.box.personas) - 1; i >= 0; i-- {
		item := s.box.personas[i]
		if item.Target == target && item.ConversationID == conversationID {
			return strings.TrimSpace(item.Reference.Display)
		}
	}
	return ""
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

func mentionReplyHint(m Model, mentions mentionStore, target string) string {
	name := mentions.PersonaDisplay(target, m.SelectedID)
	if name == "" {
		return ""
	}
	if channel := m.selected(); channel.Kind == PublicChannel || channel.Kind == PrivateChannel {
		return chatux003ChannelHint(m, mentions, target, name, channel)
	}
	if m.Text != nil {
		return m.tf(KeyMentionAgentReplyHint, map[string]string{"name": name})
	}
	return strings.ReplaceAll(mentionMenuText(m, KeyMentionAgentReplyHint), "{name}", name)
}

func composerAgentReplyHint(text string) ui.Node {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return html.P(html.Props{Class: "composer-agent-reply-hint", Role: "status", Aria: map[string]string{"live": "polite"}, Text: text})
}
