package chatui

import (
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// localUI is presentation state the chat client never needs to know about:
// which inline channel tray is open and the create dialog's kind and picked
// members. Like mentionBox it lives in a ref, so a handler always reads its own
// latest write even when events outrun renders; the tick only asks for one.
type localUI struct {
	chatcmd003   chatcmd003Preview
	searchOpen   bool
	archivedOpen bool
	// unreadOnly is the sidebar's Unread filter and unreadKept the conversations
	// that were unread when it was turned on (issue3936_unread_filter.go).
	unreadOnly bool
	// draftsOpen, draftsAsk and draftsNote are the sidebar Drafts list: open,
	// the conversation whose discard awaits confirmation, and the polite status
	// text (issue3915_drafts.go).
	draftsOpen            bool
	draftsAsk, draftsNote string
	// followedOpen is the Followed threads panel (issue3916_followed_threads.go).
	followedOpen bool
	// followUpsOpen is the Follow-ups panel (issue3787_followups.go).
	followUpsOpen        bool
	unreadKept           map[string]bool
	pointerRow, focusRow string
	room                 string
	tray                 string
	createKind           ConversationKind
	pickQuery            string
	picked               []mentionCandidate
	pickActive           int
	pickDismissed        bool
	pickNote             string
	nameRefused          bool
	nameMissing          bool
	nameTyped            string
	// groupName and groupPicked are the Group message draft (ISSUE-3865): the
	// name as typed, and the recipients set aside while another type is shown.
	groupName      string
	groupPicked    []mentionCandidate
	purposeEditing bool
	purposeDraft   string
	// purposeByRoom keeps each conversation's unsaved purpose while another is
	// open (issue3909_purpose_draft.go).
	purposeByRoom map[string]purposeKept
	railLeave     string
	// side is the sidebar's own sections state (CHATSIDE-001).
	side chatside001UI
	// composerNotice explains a composer command that could not run, such as
	// /giphy without a configured GIPHY key. The next keystroke clears it.
	composerNotice string
	// generalAsk is the card for the question last put to the General agent
	// (issue3756_general_ask.go).
	generalAsk issue3756State
	// pollReady is whether the channel poll form holds a question and at
	// least two options, so Create can be enabled (C-10, round 3).
	pollReady bool
	// docSuggest is the composer's "[[" / "doc:" list (chat_doc_suggest.go).
	docSuggest docSuggestState
	// commandMenu is the composer's "/" command list (composer_commands.go).
	commandMenu composerCommandMenu
	// formatRow is the viewer's choice this session for the composer's
	// formatting row: "shown", "hidden" or "" for the default.
	formatRow string
	// threadFormatRow is the same choice for the thread's reply field, which
	// opens closed whatever the viewer chose for the channel composer.
	threadFormatRow string
	// composerPreview shows the main composer's draft as formatted text in the
	// field's own slot; threadComposerPreview does the same for the thread's
	// reply field (AGENTUX-063). Typing or sending clears either: preview never
	// traps an empty or edited draft.
	composerPreview, threadComposerPreview bool
	// sentCount is how many messages the viewer has sent from the composer this
	// page session; the Enter hint goes after three (composerHintVisible).
	sentCount      int
	agentFeedback  map[string]string
	agentProfileID string
	// reactionsFor is the message whose View reactions list is open (issue3938_reactions_view.go).
	reactionsFor string
	// threadPeek is the thread set aside so the channel shows on a narrow
	// screen after View in channel (issue3894_thread_peek.go).
	threadPeek      string
	threadUI        threadChoiceUI
	emojiCompletion emojiCompletion
	// channelSuggest is the composer's "#" channel list (chatux051_channels.go).
	channelSuggest channelSuggestState
	// emoji is the open emoji picker's state (chatemoji_state.go).
	emoji emojiPickerState
	// detailGroups is which collapsed groups of Conversation details the viewer
	// has opened this page session (chatux005_details.go).
	detailGroups map[string]bool
	// roleQuery is the Role labels filter's own text, kept apart from the
	// Members filter (issue3881_role_filter.go).
	roleQuery string
	// modAuthorHidden is the stamp of each CHATMOD-002 refusal line the author
	// has typed past (chatmod002_author.go).
	modAuthorHidden map[string]int64
	// chatlang is which originals the reader opened and the writer's language
	// picker (chatlang004_view.go).
	chatlang chatlangLocal
	// deleteAsk is the message menu that is asking whether to delete its
	// message (chatbug081_delete.go).
	deleteAsk string
	// cardEdit is the poll or list message its author is rewording
	// (chatcmd002_edit.go).
	cardEdit string
	// plan is the poll card's Plan next step panel (issue3954_plan.go).
	plan issue3954State
	// task is the to-do task form a card has open (issue3947_task_change.go).
	task issue3947State
	// agentShareAsk is the agent run whose private answer card is asking whether
	// to share it with the channel (chatux026_share.go).
	agentShareAsk string
	// handoff is the answer card reviewing "Continue with another agent" (issue3977_handoff.go).
	handoff issue3977State
	// attention is the "What needs my attention?" panel (issue3986_attention.go).
	attention issue3986UI
	// enroll is the "Help me understand my enrollment options" guide (issue3990_enrollment_guide.go).
	enroll issue3990UI
	// find is the "Find the right contact or channel" card (issue3963_find_contact.go).
	find issue3963UI
	// privateAnswer is the composer's "Private answer" switch (agentux070_private_answer.go).
	privateAnswer bool
	// privateByRoom keeps the switch of each conversation's unsent draft while
	// another conversation is open (issue3992_private_answer.go).
	privateByRoom map[string]bool
	// privateAsk is, per private conversation, the channel question moved into it
	// and still unsent (issue3969_private_question.go).
	privateAsk map[string]issue3969Origin
	seq        uint64
	// menuSeq is seq as of the last change to commandMenu, which is what the
	// command list writes into data-open (issue 3827: not every local change).
	menuSeq uint64

	// dateJumpOpen is the Jump to date bar (issue3930_date_jump.go); dateJumpInvalid
	// is a Go press with no usable day in the field.
	dateJumpOpen, dateJumpInvalid bool

	// sharedFilesOpen is the Shared files entry of the details panel and
	// sharedFilesType the type chip chosen in it (issue3917_shared_files.go).
	sharedFilesOpen bool
	sharedFilesType string

	// The Event timeline entry of the details panel (issue3964_timeline.go): open,
	// the entries the person removed, the note under the form (a copy key) and the
	// access check before a draft.
	timelineOpen, timelineChecking bool
	timelineRemoved                map[string]bool
	timelineNote                   string
	timelineChanged                int

	// Issue 3788: Send later. sendLaterOpen is the panel above the message box;
	// scheduledOpen is the Scheduled tray and scheduledEdit the row being changed.
	sendLaterOpen, sendLaterBusy                                                   bool
	sendLaterNote, sendLaterDate, sendLaterClock                                   string
	sendLaterSaved                                                                 time.Time
	sendLaterQuiet                                                                 int
	scheduledOpen                                                                  bool
	scheduledEdit, scheduledBody, scheduledDate, scheduledClock, scheduledEditNote string
}

type localStore struct {
	box  *localUI
	tick ui.State[uint64]
}

func (s localStore) get() localUI { return *s.box }

func (s localStore) update(change func(*localUI)) {
	menu := s.box.commandMenu
	change(s.box)
	s.box.seq++
	if s.box.commandMenu != menu {
		s.box.menuSeq = s.box.seq
	}
	s.tick.Set(s.box.seq)
}

// forRoom drops room-scoped state (the open tray) when the conversation
// changes, without asking for another render: the caller is rendering already.
func (s localStore) forRoom(room string) {
	if s.box.room != room {
		s.box.keepPrivateAnswer(s.box.room)
		s.box.keepPurposeEdit(s.box.room)
		s.box.room = room
		s.box.chatcmd003 = chatcmd003Preview{Generation: s.box.chatcmd003.Generation + 1}
		s.box.tray = ""
		s.box.pointerRow, s.box.focusRow = "", ""
		s.box.searchOpen = false
		s.box.dateJumpOpen, s.box.dateJumpInvalid = false, false
		s.box.sharedFilesOpen, s.box.sharedFilesType = false, ""
		s.box.timelineOpen, s.box.timelineChecking, s.box.timelineRemoved, s.box.timelineNote, s.box.timelineChanged = false, false, nil, "", 0
		s.box.pollReady = false
		s.box.roleQuery = ""
		s.box.agentProfileID = ""
		s.box.reactionsFor = ""
		s.box.threadPeek = ""
		s.box.agentShareAsk = ""
		s.box.handoff = issue3977State{}
		s.box.privateAnswer = s.box.privateAnswerForRoom(room)
		s.box.cardEdit = ""
		s.box.plan = issue3954State{}
		s.box.task = issue3947State{}
		s.box.purposeEditing, s.box.purposeDraft = false, ""
		s.box.restorePurposeEdit(room)
		s.box.emojiCompletion = emojiCompletion{Active: -1}
		s.box.commandMenu = composerCommandMenu{}
	}
}

// resetCreate clears the create dialog's picker for its next opening.
func (s localStore) resetCreate() {
	s.box.createKind = ""
	s.box.pickQuery = ""
	s.box.picked = nil
	s.box.pickActive = 0
	s.box.pickDismissed = false
	s.box.pickNote = ""
	s.box.nameRefused, s.box.nameMissing, s.box.nameTyped = false, false, ""
	s.box.groupName, s.box.groupPicked = "", nil
}

// pickCandidates lists people the viewer can add to a new conversation: the
// directory, the current room's members and anyone already on screen, minus
// the viewer and whoever is already picked, best prefix matches first.
func pickCandidates(m Model, query string, picked []mentionCandidate) []mentionCandidate {
	q := strings.ToLower(strings.TrimSpace(query))
	skip := map[string]bool{m.CurrentUser: true}
	limit := 6
	if m.ShowAddMembers {
		// Adding to an existing conversation never offers someone already
		// in it; before any typing the list is a short starter set.
		for _, p := range m.Members {
			skip[p.ID] = true
		}
		if q == "" {
			limit = 5
		}
	}
	for _, p := range picked {
		skip[p.ID] = true
		skip["name:"+strings.ToLower(strings.TrimSpace(p.Name))] = true
	}
	type scored struct {
		c    mentionCandidate
		rank int
	}
	var out []scored
	add := func(id, name string) {
		name = strings.TrimSpace(name)
		nameKey := "name:" + strings.ToLower(name)
		if id == "" || name == "" || name == id || skip[id] || skip[nameKey] {
			return
		}
		skip[nameKey] = true
		lower := strings.ToLower(name)
		rank := -1
		if q == "" || strings.HasPrefix(lower, q) {
			rank = 0
		} else {
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
		skip[id] = true
		out = append(out, scored{mentionCandidate{ID: id, Name: name, Member: true}, rank})
	}
	for _, p := range m.SearchDirectory {
		add(p.ID, p.Name)
	}
	for _, p := range m.Members {
		add(p.ID, p.Name)
	}
	for _, msg := range m.Messages {
		add(msg.AuthorID, msg.Author)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return strings.ToLower(out[i].c.Name) < strings.ToLower(out[j].c.Name)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	result := make([]mentionCandidate, len(out))
	for i := range out {
		result[i] = out[i].c
	}
	return result
}

// pickedIDs is the members field's submitted value: stable subject IDs, never
// display names, so two people with one name cannot be confused.
func pickedIDs(picked []mentionCandidate) string {
	ids := make([]string, 0, len(picked))
	for _, p := range picked {
		ids = append(ids, p.ID)
	}
	return strings.Join(ids, ",")
}
