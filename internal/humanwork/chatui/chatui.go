// Package chatui contains the native, server-renderable collaboration workspace.
//
// The package deliberately owns presentation state only. Conversation and post
// authority stays behind Callbacks, which makes the same tree usable by the
// authenticated WASM client and by the script-free shell.
//
// Layout: a three-column room in the shape people already know from Slack —
// a conversation rail, the timeline with its composer pinned to the bottom,
// and an optional side column that shows either conversation details or an
// open thread. The workspace fills the height it is given and only the rail
// and the timeline scroll; the page around it never does.
package chatui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

type LoadState string

const (
	StateReady   LoadState = "ready"
	StateLoading LoadState = "loading"
	StateEmpty   LoadState = "empty"
	StateError   LoadState = "error"
)

// PersonaLookupState is the state of the selected conversation's authorized
// persona directory read. It is separate from the conversation load so a
// failed agent lookup never hides people or disables the composer.
type PersonaLookupState string

const (
	PersonaLookupIdle    PersonaLookupState = "idle"
	PersonaLookupLoading PersonaLookupState = "loading"
	PersonaLookupReady   PersonaLookupState = "ready"
	PersonaLookupFailed  PersonaLookupState = "failed"
)

type ConversationKind string

const (
	PublicChannel  ConversationKind = "public-channel"
	PrivateChannel ConversationKind = "private-channel"
	DirectMessage  ConversationKind = "direct"
	GroupChat      ConversationKind = "group"
)

type Conversation struct {
	Icon         agenticon.Value
	IconRevision int64
	// IconImageURL serves the owner's uploaded image from this origin when
	// the agent has one; empty for a generated icon.
	IconImageURL                     string
	ID, Name, Topic, Avatar, Preview string
	OwnerID, HostTenantID            string
	AgentID                          string
	// AgentPurpose is the agent's own description, read from the same record as
	// its icon; it is the line under the name of a direct conversation with it.
	AgentPurpose     string
	Kind             ConversationKind
	Unread, Mentions int
	MemberCount      int
	// LastActivity is when the newest message was sent, when the listing
	// says; the browse row reads it so a reader can tell a live channel from
	// a dead one before joining.
	LastActivity          time.Time
	Muted, Starred, Agent bool
	// Joined is false for a channel the viewer can browse but has not joined.
	Joined bool
}

type Member struct {
	Icon         agenticon.Value
	IconRevision int64
	// IconImageURL serves the owner's uploaded image from this origin when
	// the agent has one; empty for a generated icon.
	IconImageURL                             string
	ID, HomeTenantID, Name, Subtitle, Avatar string
	Online, Agent                            bool
}

// SearchPerson is a person returned by the caller-authorized chat search.
type SearchPerson struct {
	ID, Name string
	Aliases  []string
}

// SearchMessage carries enough context to open a message in its conversation.
type SearchMessage struct {
	ConversationID, ConversationName string
	Message                          Message
}

// PersonDetails contains only business directory fields authorized for chat.
// Empty fields mean the directory did not provide a value.
type PersonDetails struct {
	ID, Name, JobTitle, Manager, Department, Phone, Email   string
	Location, Company, BusinessUnit, PhotoURL, OrgChartHref string
	ManagerPhotoURL                                         string
	ManagerID                                               string
	DirectReports                                           []PersonLink
	Ready, Unavailable                                      bool
}

// PersonLink is a governed coworker reference shown in another person's
// business details. IDs are stable worker references, never display names.
type PersonLink struct{ ID, Name, PhotoURL string }

type Message struct {
	ID, AuthorID, Author, Avatar, Body, TimeLabel string
	SentAt                                        time.Time
	Revision                                      uint64
	Sequence                                      uint64
	Edited, Pinned                                bool
	Replies, Reactions                            int
	// Reacted is true when the viewer already reacted to this message.
	Reacted bool
	// Attachments are the media the message references. An attachment with
	// an image content type renders inline; anything else renders as a chip.
	// An empty URL means the viewer's grant has not arrived yet.
	Attachments []Attachment
	// Chips are the reactions on this message, one per emoji, in first-seen
	// order. Reactions stays the total for callers that only count.
	Chips             []ReactionChip
	ForwardedAuthor   string
	ForwardedAuthorID string
	// ForwardNote is the sharer's own note beside a forwarded copy (issue 3919).
	ForwardNote string
	// PersonaActor is supplied only from the trusted actor projection. A nil or
	// incomplete value is rendered as unavailable; display names never infer it.
	PersonaActor      *PersonaActor
	PersonaReferences []ChatReference
	// WorkReferences are the typed project references the post carries (issue
	// 3752): data the card is drawn from, whatever the body says.
	WorkReferences []ChatReference
	// AuthorAgent is set when the answer that carried this message said its
	// author is an agent, with the agent's own name already in Author and its
	// stored icon (CHATUX-047). It names; it never attests: the Agent badge still
	// comes from PersonaActor alone.
	AuthorAgent *AuthorAgent
	// Audience is who a message addressed to segments of the channel's gate is
	// for (CHATGATE-014); nil for a message for everyone.
	Audience *MessageAudience
}

// ChannelPin is an authorized pin projection, independent of the timeline page.
type ChannelPin struct {
	PostID, Author, Body string
	Revision             uint64
	Sequence             uint64
	// PinnedBy is the name of whoever pinned the message and PinnedAt when
	// (CHATUX-019); either may be unknown.
	PinnedBy string
	PinnedAt time.Time
}

type ChannelTodoItem struct {
	ID, Text, SourcePostID, CompletedBySubjectID, CompletedByHomeTenantID string
	CompletionMode                                                        string
	SelectedCompleters                                                    []ChannelTodoSelectedMember
	CanToggle, CanManageCompletionPolicy                                  bool
	Completed                                                             bool
	CompletedAtUnix                                                       int64
}

type ChannelTodoSelectedMember struct{ HomeTenantID, SubjectID string }

type ChannelTodoList struct {
	Revision uint64
	Pinned   bool
	Items    []ChannelTodoItem
}

// Channel widgets contain collaboration notes. Member names remain sourced
// from the authorized live chat roster, never copied into widget storage.
type ChannelTeamMember struct{ HomeTenantID, SubjectID, Role, RoleLabel string }
type ChannelTeamWidget struct {
	Revision       uint64
	Pinned, CanPin bool
	Purpose        string
	Members        []ChannelTeamMember
}
type ChannelProjectMilestone struct{ ID, Text, Status, OwnerHomeTenantID, OwnerSubjectID, DueDate string }
type ChannelProjectWidget struct {
	Revision       uint64
	Pinned, CanPin bool
	Title, Summary string
	Milestones     []ChannelProjectMilestone
}
type ChannelPollOption struct {
	ID, Text string
	Count    int
}
type ChannelPoll struct {
	Revision   uint64
	Question   string
	Options    []ChannelPollOption
	MyOptionID string
	TotalVotes int
}

// ReactionChip is one emoji on a message with how many people used it and
// whether the viewer is one of them.
type ReactionChip struct {
	Emoji string
	Count int
	Mine  bool
	// PeopleIDs are the others who reacted with this emoji, so the
	// chip can be spoken as "You and Sam reacted with eyes" (CHATUX-019).
	PeopleIDs []string
}

// reactionPalette is the quick picker: the eight reactions people reach for.
var reactionPalette = []string{"👍", "❤️", "😂", "🎉", "👀", "🙏", "✅", "🔥"}

// quickReactions lead the message hover bar as one-click reactions.
var quickReactions = []string{"👍", "✅", "👀"}

// Attachment is one media reference on a message.
type Attachment struct {
	ID, Name, ContentType, URL string
	Width, Height              int
	Bytes                      int64
	PreviewUnavailable         bool
	// AltText is the description the author wrote for an image and Decorative
	// the author's statement that it needs none (issue 3962). The file name stays
	// Name; neither is derived from the other.
	AltText    string
	Decorative bool
}

// IsImage reports whether the attachment renders inline.
func (a Attachment) IsImage() bool {
	return strings.HasPrefix(strings.ToLower(a.ContentType), "image/")
}

// IsGIF reports an animated image, which gets its own badge.
func (a Attachment) IsGIF() bool {
	return strings.EqualFold(a.ContentType, "image/gif")
}

type SidebarSection struct {
	ID, Name  string
	Collapsed bool
	Chats     []Conversation
	// Manual is set once the person has put this section's conversations in
	// their own order; a conversation that comes back to it goes to the end. An
	// untouched section keeps its conversations sorted and a returning one takes
	// its sorted place (CHATSIDE-001).
	Manual bool
}

type PaneSizes struct {
	Rail, Details                   int
	RailCollapsed, DetailsCollapsed bool
}

type NotificationMode string

const (
	NotifyAll     NotificationMode = "all"
	NotifyMention NotificationMode = "mentions"
	NotifyMute    NotificationMode = "mute"
)

type Preferences struct {
	Drafts           map[string]string
	Notifications    map[string]NotificationMode
	Sections         []SidebarSection
	Panes            PaneSizes
	QuietHours       bool
	QuietTimezone    string
	QuietStartMinute int
	QuietEndMinute   int
}

// Callbacks are the narrow bridge into the authenticated chat client. Controls
// whose callbacks are absent render disabled so the UI never offers a no-op.
type Callbacks struct {
	SelectConversation func(string)
	Search             func(string)
	// SearchFocus is called when the search box takes the cursor, so the client
	// can read the person's recent searches into Model.SearchRecent.
	SearchFocus        func()
	SearchMore         func()
	SearchMoreChannels func()
	SearchClearRecent  func() // deletes the stored recent searches (ISSUE-3928)
	// Saved searches (ISSUE-3929): keep the search on screen under a name, open
	// the editor of one (empty id closes it), save its changes, or delete it.
	SavedSearchSave   func(name, query string)
	SavedSearchEdit   func(id string)
	SavedSearchUpdate func(id, name, query string)
	SavedSearchDelete func(id string)
	OpenSearchMessage func(conversationID, postID string, sequence uint64)
	OpenSearchChannel func(conversationID string)
	LoadMembers       func()
	// ACCESS-01: opening/closing the "Add people" picker and submitting it
	// for the selected conversation.
	OpenAddMembers          func()
	CloseAddMembers         func()
	AddMembers              func(subjectIDs []string)
	LoadOlder               func()
	LoadNewer               func()
	VisibleMessageIDs       func([]string)
	DownloadAttachment      func(string, string)
	OpenCreate              func()
	CloseCreate             func()
	CreateConversation      func(ConversationKind, string, []string)
	OpenBrowse              func()
	LoadFindChannels        func() // reads the contact finder's channels and purposes (issue 3963)
	CloseBrowse             func()
	RequestJoinConversation func(string)
	JoinConversation        func(string)
	DismissJoinPrompt       func()
	ToggleSection           func(string)
	ReorderSection          func(string, int)
	CreateSection           func(string)
	RemoveSection           func(string)
	MoveConversationSection func(string, string)
	MoveConversationOrder   func(string, int)
	// RenameSection renames a section the person made; SetFavorite adds a
	// conversation to Favorites or takes it out; CreateSectionFor makes a section
	// and moves the conversation into it (CHATSIDE-001).
	RenameSection    func(string, string)
	SetFavorite      func(string, bool)
	CreateSectionFor func(string, string)
	// MarkConversationRead and MarkConversationUnread are the sidebar row menu's
	// Mark as read and Mark as unread; LeaveConversation is its Leave, for
	// channels. A nil one leaves its menu item disabled (CHATUX-020).
	MarkConversationRead   func(string)
	MarkConversationUnread func(string)
	LeaveConversation      func(string)
	SendMessage            func(string, string)
	// AskGeneralAgent starts a task for the built-in General agent with the
	// person's question and calls done, on the page's thread, with the task or
	// the failure. Nothing is posted to the conversation (issue 3756). When nil,
	// "/ask" with no agent name is refused as before.
	AskGeneralAgent func(question string, done func(GeneralAgentAnswer, error))
	// ReadEnrollmentGuide reads the viewer's own benefits eligibility and
	// enrollment-window status for one kind of question from the benefits
	// service and calls done, on the page's thread (issue 3990). It reads only.
	// When nil, the guide says the status cannot be checked here.
	ReadEnrollmentGuide func(kind string, done func(EnrollmentGuideStatus, error))
	// SendMessageWithReferences submits the body and its canonical references
	// together. Persona suggestions are unavailable when this callback is nil.
	SendMessageWithReferences func(string, string, []ChatReference)
	RetryPersonaMentions      func()
	// RetryWorkPreviews reads the ticket and document cards that failed to load
	// again (issue 3750); nil leaves the retry button inert.
	RetryWorkPreviews func()
	// RetryJourney reads one workflow card again after a temporary failure
	// (issue 3729).
	RetryJourney func(intentID string)
	// DecideJourney sends the routed approver's decision on a workflow card
	// (issue 3724); the client reads the reason draft and the reviewed
	// revision itself. ToggleJourneyDecision opens or closes the review and
	// SetJourneyDecisionDraft keeps the typed reason. All three nil leaves the
	// card read-only.
	DecideJourney           func(intentID string, approve bool)
	ToggleJourneyDecision   func(intentID string, open bool)
	SetJourneyDecisionDraft func(intentID, text string)
	// RequestAttention reads the viewer's own work for the private "What needs my
	// attention?" brief (issue 3986). It reads only; it never changes work.
	RequestAttention func()
	// SetChannelAgentPrivacy requires, or stops requiring, private agent answers
	// in the open channel (AGENTUX-070).
	SetChannelAgentPrivacy func(private bool)
	DraftChanged           func(string, string)
	// DraftConflictResolved says the person settled the other version of a
	// conversation's draft (issue 3953); RetryDraftSave asks for the unsaved
	// drafts to be written now (issue 3950).
	DraftConflictResolved func(conversationID string)
	RetryDraftSave        func()
	MentionPicked         func() // the "@" menu's choice is in the draft: the page draws the note under the composer
	OpenThread            func(string)
	// Issue 3916: load the followed-thread list, open one in its conversation,
	// and stop following one.
	LoadFollowedThreads func()
	OpenFollowedThread  func(conversationID, rootID string, sequence uint64)
	UnfollowThread      func(conversationID, rootID string)
	// Issue 3787: load the follow-ups, open one in context, and record the
	// person's choice for one (action: snooze, done, dismiss or keep).
	LoadFollowUps func()
	OpenFollowUp  func(conversationID, postID string, sequence uint64, threadRootID string)
	SetFollowUp   func(conversationID, postID, direction, action string)
	// Issue 3788: Send later. ScheduleSend keeps the message for the time and
	// reports the outcome through done; EditScheduled changes one; CancelScheduled
	// removes one with its text; LoadScheduled reads the tray.
	LoadScheduled   func()
	ScheduleSend    func(conversationID, body string, at time.Time, done func(SendLaterResult))
	EditScheduled   func(id, body string, at time.Time, revision uint64, done func(SendLaterResult))
	CancelScheduled func(id string)
	// Unsent messages (issue 3737): Retry, Edit and Discard take the row id.
	RetryUnsent, EditUnsent, DiscardUnsent func(id string)
	CloseThread                            func()
	LoadOlderThread                        func()
	LoadNewerThread                        func()
	SetThreadFollow                        func(bool)
	React                                  func(string)
	RemoveReaction                         func(string)
	Pin                                    func(string)
	Unpin                                  func(string)
	JumpToPin                              func(string, uint64)
	CopyPinReference                       func(string)
	AddChannelTodo                         func(string, string)
	SetChannelTodoDraft                    func(string)
	SetChannelTodoSourcePin                func(string)
	SetChannelTodoCompleted                func(string, bool)
	DeleteChannelTodo                      func(string)
	SetChannelTodoPinned                   func(bool)
	SetChannelTodoPolicy                   func(string, string, []ChannelTodoSelectedMember)
	SetChannelTodoNewPolicy                func(string, []ChannelTodoSelectedMember)
	RetryChannelTodo                       func()
	OpenChannelTodo                        func()
	RetryChannelWidgets                    func()
	SetChannelWidgetPinned                 func(string, bool)
	SetChannelTeamPurpose                  func(string)
	SetChannelTeamRoleLabel                func(string, string, string)
	SetChannelProjectDetails               func(string, string)
	AddChannelProjectMilestone             func(ChannelProjectMilestone)
	UpdateChannelProjectMilestone          func(ChannelProjectMilestone)
	DeleteChannelProjectMilestone          func(string)
	RetryChannelPoll                       func()
	OpenChannelPoll                        func(string)
	CreateChannelPoll                      func(string, []string)
	VoteChannelPoll                        func(string)
	SavePreferences                        func(Preferences)
	SetConversationNotification            func(string, NotificationMode)
	OpenConversationDetails                func(string)
	OpenRailMenu                           func(string)
	Retry                                  func()
	ToggleDetails                          func(bool)
	ToggleSidebar                          func(bool)
	BeginEdit                              func(string)
	CancelEdit                             func()
	SetEditDraft                           func(string, string)
	// ThreadDraftChanged reports the reply box's text under one root message.
	ThreadDraftChanged func(parentID, value string)
	// ThreadAlsoChanged reports the "Also send to the conversation" choice of
	// one thread's reply box (#3901).
	ThreadAlsoChanged func(parentID string, also bool)
	EditMessage       func(string, string, uint64)
	DeleteMessage     func(string, uint64)
	ResizeRail        func(int)
	ResizeDetails     func(int)
	RestorePanes      func()
	DismissNotice     func()
	// MarkUnreadFrom moves the viewer's read position to just before a
	// message. When nil the message menu does not offer it (CHATUX-022).
	MarkUnreadFrom func(postID string)
	// ReplyInThread posts a reply under the open thread's root message. When
	// nil the thread pane has no composer of its own.
	ReplyInThread func(parentID, body string)
	// ReplyInThreadWithReferences posts a reply with canonical typed references.
	ReplyInThreadWithReferences func(parentID, body string, refs []ChatReference)
	// ReplyInThreadIn posts a reply under parentID in conversationID, both
	// captured when the reader pressed Send, and, when alsoChannel is set, the
	// same words in the conversation. The owner reports each destination's
	// outcome and retries only the missing one (#3997). When nil, ReplyInThread
	// and SendMessage are used.
	ReplyInThreadIn func(conversationID, parentID, body string, alsoChannel bool)
	// FilterBrowse narrows Model.Browse by a typed query.
	FilterBrowse func(query string)
	// FilterMembers narrows the details member list by a typed query.
	FilterMembers func(query string)
	// ReactWith adds one emoji to a message; RemoveReactionWith takes the
	// viewer's own off again. React/RemoveReaction remain the single-emoji
	// (thumbs up) fallbacks.
	ReactWith          func(postID, emoji string)
	RemoveReactionWith func(postID, emoji string)
	// OpenPicker shows the reaction picker under one message ("" closes it).
	OpenPicker func(postID string)
	// OpenMenu shows the overflow menu on one message ("" closes it).
	OpenMenu func(postID string)
	// CopyLink puts a link to the message on the clipboard.
	CopyLink func(postID string)
	// CopyContents puts the message's plain text on the clipboard.
	CopyContents func(postID string)
	// CopyConversationReference puts a labeled conversation link on the clipboard.
	CopyConversationReference func(conversationID, label string)
	// CopyConversationAPICurl copies a POST example scoped to this conversation.
	CopyConversationAPICurl func(conversationID string)
	OpenShare               func(postID string)
	CloseShare              func()
	FilterShare             func(string)
	SelectShareDestination  func(string)
	ShareMessage            func()
	// ChangeShareNote records the note typed in the share dialog (issue 3919).
	ChangeShareNote     func(string)
	OpenEmbeddedMessage func(string)
	// JumpToNewest scrolls the timeline to its newest message.
	JumpToNewest func()

	// JumpToDate loads the timeline at the first message on or after a local day
	// (YYYY-MM-DD); JumpBackFromDate returns to where the person jumped from
	// (issue 3930).
	JumpToDate       func(date string)
	JumpBackFromDate func()

	// JumpFromReadingResume leaves the place a return to a conversation restored
	// and goes to the newest messages; DismissReadingResume only closes the bar
	// (issue 3942).
	JumpFromReadingResume func()
	DismissReadingResume  func()

	// Handover runs one step of the work handover panel (issue 3973).
	Handover func(HandoverCommand)

	// LoadSharedFiles reads the first page of the open conversation's files,
	// narrowed by a name; MoreSharedFiles reads the next older page;
	// OpenSharedFileSource shows the message a file was shared in (issue 3917).
	LoadSharedFiles      func(name string)
	MoreSharedFiles      func()
	OpenSharedFileSource func(id string)

	// LoadTimeline reads the event timeline of the open conversation for a range
	// of days; OpenTimelineSource shows the message of one entry;
	// CheckTimelineEntries reads access to entries again before a draft is made
	// and calls done with the answer (issue 3964).
	LoadTimeline         func(from, to string, replies bool)
	OpenTimelineSource   func(id string)
	CheckTimelineEntries func(ids []string, done func(TimelineCheck))

	OpenPerson func(string)
	// Navigate moves the whole application to an in-app address through
	// the global history (a document a message links to); nil leaves the
	// link to the browser.
	Navigate func(string)
	// SuggestDocuments lists documents the reader can open whose title
	// matches query, for the composer's "[[" and "doc:" autocomplete; done
	// runs once on the UI loop with the outcome (issue 3746: a failed or
	// unconfigured search is not "no match"). nil turns the list off.
	SuggestDocuments func(query string, done func([]DocSuggestion, DocSuggestStatus))
	// SuggestTasks and SuggestProjects list tickets and project boards the
	// reader can open, for the "ticket:" and "board:" composer pickers.
	SuggestTasks, SuggestProjects func(query string, done func([]DocSuggestion, DocSuggestStatus))
	ClosePerson                   func()
	StartDirectMessage            func(string)
	// SubmitAgentFeedback records feedback against the authenticated invocation.
	// The application boundary owns persistence and owner notification.
	SubmitAgentFeedback func(invocationID string, helpful bool)
	UndoAgentFeedback   func(invocationID string)
	// ShareAgentAnswer asks the server to post the asker's private answer to the
	// channel it was asked in (AGENTUX-070). The outcome comes back as
	// Model.AgentShare. It is nil until the application boundary exposes it.
	ShareAgentAnswer func(invocationID string)
	// RemoveSharedAgentAnswer takes back the copy of the asker's answer that was
	// shared to the channel (CHATUX-026): the copy is deleted and the card is
	// private again. The outcome comes back as Model.AgentShare.
	RemoveSharedAgentAnswer func(invocationID string)
	// CancelPersonaInvocation requests cancellation of a still-running answer.
	// It is nil until the application boundary exposes an authorized endpoint.
	CancelPersonaInvocation func(invocationID string)
	// SetComposerFormatRow keeps the viewer's choice to show (true) or hide the
	// composer's formatting row. nil keeps the choice for the session only.
	SetComposerFormatRow func(shown bool)
	// SetComposerEnter keeps the viewer's choice of what Enter does in message
	// boxes: true makes Enter add a line (Ctrl or Cmd+Enter sends), false is the
	// default, Enter sends (ISSUE-3934). nil hides the preference.
	SetComposerEnter func(newline bool)
	// SaveQuietSchedule keeps the person's weekly quiet-hours schedule and
	// one-off pause (ISSUE-3920). nil hides the schedule row.
	SaveQuietSchedule func(QuietScheduleEdit)
	// SaveAway keeps the person's away-until date, cover and choices
	// (ISSUE-3789). nil hides the Away row.
	SaveAway func(AwayEdit)
	// SaveEmojiPrefs stores the viewer's emoji choices (skin tone and most used,
	// encoded by the picker) with their other personal Chat preferences. The
	// picker calls it at most once every few seconds. nil keeps them for the
	// life of the page only (CHATEMOJI-004).
	SaveEmojiPrefs func(encoded string)

	// SetAmbientOptOut turns the viewer's "Don't act on my messages" on or off.
	SetAmbientOptOut func(bool)
}

type Model struct {
	Chatcmd002         Chatcmd002Callbacks
	Chatcmd002Views    map[string]Chatcmd002View
	Chatcmd002TimeZone string

	ChatFeatures *ChatFeatures
	// EmojiPrefs is the viewer's saved emoji choices as the server holds them
	// (CHATEMOJI-004); "" before they have any or before the read has landed.
	EmojiPrefs string
	// ComposerFormatRow is the viewer's saved choice for the formatting row:
	// "shown", "hidden", or "" for automatic visibility (shown from 800 px up
	// once the draft has content; an empty draft keeps the composer compact).
	ComposerFormatRow string
	// ComposerEnter is the viewer's saved Enter-key choice: "newline" makes Enter
	// add a line; "" or "send" keeps Enter sending (ISSUE-3934).
	ComposerEnter string

	ReaderRenderings     map[string]ReaderRendering
	ReaderSelections     map[string]ReaderSelection
	MessageLocations     map[string][]ChatmapEmbed
	ReaderPolicyRequired bool
	// Chatlang is who reads this conversation in which language (CHATLANG-004).
	Chatlang       ChatlangModel
	ReaderPending  bool
	SavedOpenCount int
	// Followed threads (issue 3916): the list, whether it has loaded and whether
	// loading failed. FollowedNote is set when a Stop following was refused.
	FollowedThreads                                []FollowedThreadRow
	FollowedLoading, FollowedLoaded, FollowedError bool
	FollowedNote                                   string
	// Follow-ups (issue 3787): the rows, whether they have loaded and whether
	// loading failed. FollowUpsNote is set when a choice could not be saved.
	FollowUps                                         []FollowUpRow
	FollowUpsLoading, FollowUpsLoaded, FollowUpsError bool
	FollowUpsNote                                     string
	// SendLater is the Scheduled tray and Send later state (issue 3788).
	SendLater SendLaterState
	// Unsent are the sender's own messages not yet delivered (issue 3737), in
	// the order they were sent; each shows in its own conversation.
	Unsent []UnsentMessage
	// Moderation is what the server said about moderation for the viewer
	// (CHATMOD-004, CHATMOD-005): the sidebar entry, the Remove offer, the notices.
	Moderation ModerationState
	// SavedBodies is the text of the saved messages the Saved panel lists, so the
	// documents they refer to are read and shown by title like any message's.
	SavedBodies []string
	// AgentRailPending is true from the first read of the conversation list until
	// the server has said which direct conversations are with agents and what
	// each agent's stored icon is: a direct row that is not yet known to be a
	// person's or an agent's draws no avatar and no letter, and an agent whose
	// icon is not known yet draws an empty slot. AgentIconsReady is set by the
	// client once that answer (or its failure) has been applied.
	AgentRailPending, AgentIconsReady bool
	ChannelStatuses                   map[string]ChannelStatusView
	ChangeChannelStatus               func(chat.ChangeChannelStatusRequest)
	RetryChannelStatus                func()

	// VoiceAccess is where voice messages may be sent, per conversation, as the
	// server last said (CHATVOICE-002): the Add menu offers the voice item, or
	// shows it off with the reason, from this.
	VoiceAccess map[string]VoiceAccessView

	// FilterSettings builds the message-filter settings for the selected
	// conversation. The client supplies it; without one the details panel
	// shows only the read-only summary and no control.
	FilterSettings     func() ui.Node
	ShowFilterSettings bool
	// WorkspaceFilterSettings builds the same settings for the whole workspace.
	// It is offered to a workspace administrator only.
	WorkspaceFilterSettings func() ui.Node
	// AuthorBlocked holds the refusals (CHATMOD-002) a language filter gave this
	// author, keyed by ModAuthorKey*; each shows one line under its field.
	AuthorBlocked map[string]AuthorBlocked
	// ThreadDrafts is the text of each open thread's reply box by root message, so
	// a reply that failed keeps its text (CHATMOD-002).
	ThreadDrafts map[string]string
	// ThreadAlso is, by root message, whether that thread's reply is also to be
	// sent to the conversation (#3901). It is kept with the draft.
	ThreadAlso map[string]bool

	State  LoadState
	Error  string
	Notice string
	// NoticeRetry offers a retry control beside the notice (for example when
	// the live feed has stopped and Callbacks.Retry resubscribes).
	NoticeRetry bool
	// DraftConflicts holds, per conversation, a different draft another tab or
	// device saved while this one had its own (issue 3953). DraftUnsaved says the
	// latest edits could not be saved yet (issue 3950).
	DraftConflicts map[string]string
	DraftUnsaved   bool
	Conversations  []Conversation
	// PreviewConversation is an authorized public channel the viewer can read
	// before joining. It is intentionally kept out of the persistent rail.
	PreviewConversation *Conversation
	JoinPromptID        string
	JoinPromptPending   bool
	JoinPromptSeen      map[string]bool
	Sections            []SidebarSection
	// FavoritesCollapsed is whether the person has folded Favorites, which is
	// drawn from the starred conversations and has no section of its own
	// (CHATSIDE-001).
	FavoritesCollapsed bool
	SelectedID         string
	// RevokedConversationID asks the workspace to discard only this room's
	// unsent draft after a conversation-level authorization refusal.
	RevokedConversationID string
	Messages              []Message
	// EphemeralMessages are recipient-only stream deliveries. They are kept
	// outside Messages so shared history, search, unread counts, and exports
	// can never accidentally consume private persona content.
	EphemeralMessages       []EphemeralMessage
	ChannelPins             []ChannelPin
	ChannelTodo             ChannelTodoList
	ChannelTodoLoading      bool
	ChannelTodoPending      bool
	ChannelTodoError        string
	ChannelTodoDraft        string
	ChannelTodoSourcePin    string
	ChannelTodoNewMode      string
	ChannelTodoNewSelected  []ChannelTodoSelectedMember
	CanPinChannelTodo       bool
	ChannelTeam             ChannelTeamWidget
	ChannelProject          ChannelProjectWidget
	ChannelWidgetsLoading   bool
	ChannelWidgetsPending   bool
	ChannelWidgetsError     string
	ChannelPoll             ChannelPoll
	ChannelPollLoading      bool
	ChannelPollPending      bool
	ChannelPollError        string
	PinReferenceUnavailable bool
	HasOlder                bool
	HasNewer                bool
	Members                 []Member
	// MembersFailed is true when the read of this conversation's members failed
	// and has not been retried; the "@" menu says so instead of waiting.
	MembersFailed bool
	// PersonaMentions contains only currently invocable personas authorized by
	// the caller for this viewer and selected conversation. It is retained for
	// source compatibility only; mention UI deliberately ignores it.
	PersonaMentions []PersonaMentionSuggestion
	// ResolvedPersonaMentions contains the server-resolved, conversation-bound
	// references eligible for this viewer. The caller must omit ineligible
	// candidates and must populate profile facts from the same authorized read.
	ResolvedPersonaMentions []ResolvedPersonaMention
	// ChannelAgentPrivacy is the channel's requirement on how agents answer in it
	// (AGENTUX-070).
	ChannelAgentPrivacy ChannelAgentPrivacyState
	// AgentCounts is how many agents the last good agent read found in each
	// conversation. The header subtitle ("Public · 18 members · 2 agents") draws
	// from the roster above when it holds agents and from this count when it does
	// not, so a roster that is not here yet, or that a failed or late read cleared,
	// never drops the agent count once it was known. The page replaces the map
	// whole, never edits it in place.
	AgentCounts   map[string]int
	PersonaLookup PersonaLookupState
	// PersonaLookupConversationID prevents a late state from one room from
	// rendering under the next room while its own lookup starts.
	PersonaLookupConversationID string
	// PersonaInvocations is an authenticated, invoker-only projection. Entries
	// are keyed by their invoking post, so updates stay in that thread.
	PersonaInvocations []PersonaThreadInvocation
	// PersonaOutcomes is how each finished agent run of the open conversation
	// ended, for any asker, each under the message that asked for it (#3803).
	PersonaOutcomes []PersonaOutcomeReceipt
	// PersonaActivityReady is true once the agent activity for the open
	// conversation has answered (or has been refused), so PersonaInvocations
	// being empty means "none" and not "not asked yet". Until then a message that
	// asked an agent keeps room for the card that will answer it (CHATBUG-040).
	PersonaActivityReady bool
	// AgentCardHeights is the height, in pixels, each answer card had the last
	// time this browser drew it, by the message it sits under. A placeholder
	// keeps exactly that room until the answer is back (CHATBUG-061).
	AgentCardHeights map[string]int
	// AgentRetries is where each question the person asked again stands, by the
	// question's message (CHATBUG-047).
	AgentRetries map[string]AgentRetryState
	// AgentDismissed holds the failed answers the person dismissed on this page,
	// by the card's run or question (CHATBUG-054).
	AgentDismissed map[string]bool
	// AgentFeedbackSaved is the reader's own stored rating of each answer, by
	// invocation, as the server sent it with the agent activity: it is what the
	// rating controls show from the first paint after a load (CHATBUG-066).
	AgentFeedbackSaved map[string]string
	// AgentFeedbackRestored maps an invocation to the rating that stands after a
	// rating change could not be saved: AgentFeedbackHelpful, AgentFeedbackNotRight
	// or "" for none. It overrides the optimistic choice shown after a click and
	// is cleared by the client when the person rates again.
	AgentFeedbackRestored map[string]string
	// AgentShare is where each private answer stands after "Share to channel",
	// by invocation (AGENTUX-070). The client sets it as the request runs.
	AgentShare                map[string]AgentShareState
	PersonaPostActors         map[string]PersonaPostActor
	RenderPersonaTask         func(PersonaTaskCardProps) ui.Node
	Draft                     string
	Chatattach001             *Chatattach001Composer
	GateJoins                 map[string]GateJoin
	GateLabels                map[string][]GateLabel
	GateAudience              map[string]GateAudienceOptions
	Search                    string
	SearchRecent              []string          // newest first; offered under the search box while it is empty
	SearchRecentStatus        string            // "cleared" or "failed" after Clear was pressed in the recent-searches popover (ISSUE-3928)
	SearchSaved               []SavedSearchItem // the person's kept searches, shown above the recent ones (ISSUE-3929)
	SavedSearchEditing        string            // id of the saved search being edited in the popover
	SavedSearchStatus         string            // how the last change went: saved, updated, deleted, failed, nameTaken, limit, invalid
	SavedSearchName           string            // the name the status speaks of
	SearchLoading             bool
	SearchLoadingMore         bool
	SearchLoadingMoreChannels bool
	SearchError               string
	// ChatSearch is the one search outcome (progress, results or the failure
	// line) drawn in the results area. SearchOpened is true while a result's
	// conversation is on screen: the query stays in the search box, the results
	// give way to the conversation and a "Return to results" bar leads back.
	ChatSearch              *ChatSearchView
	SearchOpened            bool
	SearchMoreError         string
	SearchMoreChannelsError string
	SearchNextCursor        string
	SearchChannelNextCursor string
	SearchHasMore           bool
	SearchHasMoreChannels   bool
	FocusMessageID          string
	SearchChannels          []Conversation
	SearchPeople            []SearchPerson
	SearchMessages          []SearchMessage
	// SearchDirectory is the session's already-authorized worker projection.
	// It is retained separately from active chat memberships so search can
	// find a visible coworker before the viewer has a direct message with them.
	SearchDirectory []SearchPerson
	ShowCreate      bool
	// ShowAddMembers opens the "Add people" picker over Details (ACCESS-01).
	ShowAddMembers    bool
	AddMembersPending bool
	AddMembersError   string
	ShowBrowse        bool
	Browse            []Conversation
	BrowseQuery       string
	BrowseAnnounce    bool // the person stopped typing in the Browse filter: the status says how many match (#3922)
	SidebarOpen       bool
	ShowDetails       bool
	ShowThread        bool
	ShowPerson        bool
	PersonDetails     *PersonDetails
	ThreadFollowed    bool
	ThreadParentID    string
	ThreadParent      *Message
	ThreadMessages    []Message
	ThreadLoading     bool
	ThreadHasOlder    bool
	ThreadHasNewer    bool
	EditingID         string
	EditDrafts        map[string]string
	NewKind           ConversationKind
	NewName           string
	NewNameRefused    string // the channel name the service refused; see chatbug072_name.go
	NewNameTaken      string // the public channel name another channel already holds (ISSUE-3706)
	PurposeSaved      bool   // a purpose save went through and "Saved" shows for two seconds
	Pane              PaneSizes
	Preferences       Preferences
	QuietSchedule     QuietScheduleView         // the weekly quiet-hours schedule and pause (ISSUE-3920)
	Away              AwayView                  // the signed-in person's own away setting (ISSUE-3789)
	AwayPeople        map[string]AwayPersonView // what colleagues may see of others' absences, by person (ISSUE-3789)
	Callbacks         Callbacks
	Locale            string
	Direction         string

	FindChannels       []Conversation // browsable public channels with purposes, for the contact finder (issue 3963)
	FindChannelsReady  bool           // false while FindChannels is being read
	FindChannelsFailed bool           // the read failed: only the channels already listed were searched

	// GiphyAPIKey is the public browser key supplied by the authenticated shell.
	GiphyAPIKey                  string
	CurrentUser, CurrentTenantID string
	// IsTenantAdmin gates chat surfaces that are an administrative or
	// developer concern rather than an everyday one (CHAT-08: the API curl
	// integration entry point). The chat model carries no membership role
	// beyond Conversation.OwnerID, so this is the shell's own session role
	// (hcm_admin), set once per render from productui's already-authorized
	// view -- never derived in chatui itself.
	IsTenantAdmin bool
	// AmbientReads are the agents the conversation's administrator let read
	// every message here, and AmbientOptOut is this viewer's own "Don't act on my
	// messages" (AGENTUX-066).
	AmbientReads  []AmbientRead
	AmbientOptOut bool
	// Ambient is the rest of the ambient agents' read: the installed agents the
	// administrator may switch, and the offers shown under their messages.
	Ambient AmbientState
	// PhotoURLs contains authorized worker profile images keyed by subject ID.
	PhotoURLs map[string]string
	// PeerIDs comes from authorized direct-message memberships, keyed by room ID.
	PeerIDs map[string]string
	// CurrentUserName is the viewer's display name; it labels their avatar.
	CurrentUserName string
	// UnreadFromID is the first message the viewer had not read when the
	// room opened; the timeline draws the "New" line above it.
	UnreadFromID string
	// PickerID is the message whose reaction picker is open.
	PickerID string
	// MenuID is the message whose overflow menu is open.
	MenuID                                                                     string
	RailMenuID                                                                 string
	SharePostID, ShareSourceRoomID, ShareDestinationID, ShareQuery, ShareError string
	ShareSource                                                                *Message
	ShareDestinations                                                          []Conversation
	ShareLoading, SharePending                                                 bool
	ShareVersion                                                               uint64
	// ShareNote is the unsent note in the share dialog; it survives a refusal.
	ShareNote     string
	EmbedOrigin   string
	Embeds        map[string]LinkEmbed
	EmbedRevision uint64
	// DocPreviews contains viewer-authorized document metadata for chat unfurls,
	// keyed by document ID. It is transient client state, never message content.
	DocPreviews map[string]DocPreview
	// ProjectTaskPreviews contains viewer-authorized project task titles for
	// message references, keyed by ProjectTaskPreviewKey. It is transient and
	// must be cleared or revalidated when the viewer changes.
	ProjectTaskPreviews map[string]ProjectTaskPreview
	// MessageWork is the Create task action and the work each message became
	// (issue 3790). It is transient client state, never message content.
	MessageWork MessageWorkState
	// WorkHandoff is the hand-off of one linked work item to another person
	// (issue 3794). Transient client state.
	WorkHandoff WorkHandoffState
	// Decisions is /decide and the Decisions list (issue 3792). Transient client state.
	Decisions DecisionsState
	// MessageWorkflow is the Start a promotion handoff of the message menu and /promote
	// (issue 3723). Transient client state.
	MessageWorkflow MessageWorkflowState
	// Memory is what the agents of the open channel remember (issue 3804). Transient client state.
	Memory MemoryState
	// LinkedWork is the Work from this conversation list (issue 3791). Transient client state.
	LinkedWork LinkedWorkState
	// Forms is what the server said this reader may do with each form card on
	// screen (issue 3795). Transient client state, never message content.
	Forms FormCardsState
	// Onboarding is what the onboarding owner said of each welcome card on
	// screen (issue 3811). Transient client state, never message content.
	Onboarding OnboardingCardsState
	// IntentRequests is /leave and the typed requests it opens (issue 3793).
	// Transient client state, never message content.
	IntentRequests IntentRequestsState
	// Acks is what the server said about the posts of the open conversation that
	// ask for acknowledgement (issue 3815). Transient client state.
	Acks AcksState
	// Case is what the server said about the open conversation's governed case
	// (issue 3812). Transient client state.
	Case CaseState
	// PinReview is the private review of a pinned message against later ones
	// (issue 3960). Transient client state, never stored.
	PinReview PinReviewState
	// Analysis is the private Prepare an analysis builder (issue 3959). Transient client state, never stored.
	Analysis AnalysisState
	// Impact is the private Check downstream impact panel of a thread (issue 3957). Transient client state, never stored.
	Impact ImpactState
	// JourneyPreviews holds viewer-authorized quick looks at shared
	// in-progress workflows (promotion journeys), keyed by intent ID.
	JourneyPreviews map[string]JourneyPreview
	// JourneyDecisions is each workflow card's decision review, keyed by
	// journey id (issue 3724). Transient client state, cleared with the viewer.
	JourneyDecisions map[string]JourneyDecisionState
	// Attention is the last read of the viewer's own work (issue3986_attention.go).
	Attention AttentionBrief
	// Text resolves a catalog key to the viewer's language. When nil, or when
	// the key is unknown to the caller's catalog, the reviewed English copy in
	// this package is used so the tree never shows a raw key.
	Text func(key string) string
	// Number writes a count in the viewer's numerals. When nil the ASCII
	// digits are used. Every visible count goes through it so an Arabic
	// session does not mix numeral systems between a timestamp and a badge.
	Number func(n int) string
	// MemberQuery narrows the details member list by a typed filter.
	MemberQuery string
	// mentions is the render's people index for mention chips, built once per
	// Workspace render instead of once per text fragment of every message.
	mentions      []mentionTarget
	mentionsReady bool
	// renderReferences is scoped to the message currently being rendered.
	// Only canonical post references become chips; matching display text alone
	// never gains identity or interaction semantics.
	renderReferences []ChatReference
	// renderWorkReferences are the typed project references of that message.
	renderWorkReferences []ChatReference
	// renderHighlight holds the searched words while a search result is drawn:
	// the text the message renderer prints is tinted where it matches.
	renderHighlight string
	// deleteAsk is the open message menu that is asking whether to delete its
	// message; the workspace copies it from its local state (CHATBUG-081).
	deleteAsk string
	// cardEditing is the poll or list message whose author is rewording it; the
	// workspace copies it from its local state (CHATCMD-002).
	cardEditing string
	// issue3954Plan is the Plan next step panel a poll card has open; the
	// workspace copies it from its local state (issue3954_plan.go).
	issue3954Plan issue3954State
	// issue3947 is the to-do task form a card has open; the workspace copies it
	// from its local state (issue3947_task_change.go).
	issue3947 issue3947State
	// renderInLink is set while the label of a link is drawn, so that an
	// address in the label is not made a second link inside it (CHATBUG-053).
	renderInLink bool
	// railDrafts is the conversations holding an unsent draft, for the sidebar's
	// pencil; the workspace fills it from the draft store each render (CHATUX-020).
	railDrafts map[string]bool
	// railDraftBodies is the text behind railDrafts, for the Drafts list (issue 3915).
	railDraftBodies map[string]string
	// side is the sidebar's section state the workspace copies from its local
	// state each render (CHATSIDE-001).
	side chatside001UI
	// DateJump is the last Jump to date in the open conversation (issue 3930).
	DateJump DateJumpView
	// ReadingResume is the last return to a remembered reading position in the
	// open conversation (issue 3942).
	ReadingResume ReadingResumeView
	// Handover is the work handover panel of the open conversation (issue 3973).
	Handover HandoverView
	// SharedFiles is the Shared files list of the open conversation (issue 3917).
	SharedFiles SharedFilesView
	// Timeline is the event timeline of the open conversation (issue 3964).
	Timeline TimelineView
}

// n formats a count for display. The client's Number answers "" for zero, so
// a zero is written here in the locale's numerals instead of printing blank
// (#3632); a surface that means "no value" hides its element, as the badges do.
func (m Model) n(v int) string {
	if m.Number != nil {
		if s := m.Number(v); s != "" || v != 0 {
			return s
		}
		return chatNumeral(m.Locale, "0")
	}
	return chatNumeral(m.Locale, itoa(v))
}

func (m Model) selected() Conversation {
	for _, c := range m.Conversations {
		if c.ID == m.SelectedID {
			return c
		}
	}
	if m.PreviewConversation != nil && m.PreviewConversation.ID == m.SelectedID {
		return *m.PreviewConversation
	}
	return Conversation{ID: m.SelectedID, Kind: PublicChannel}
}

func (m *Model) Select(id string) {
	if m.Preferences.Drafts == nil {
		m.Preferences.Drafts = map[string]string{}
	}
	if m.SelectedID != "" {
		if m.Draft == "" {
			delete(m.Preferences.Drafts, m.SelectedID)
		} else {
			m.Preferences.Drafts[m.SelectedID] = m.Draft
		}
	}
	m.SelectedID = id
	m.Draft = m.Preferences.Drafts[id]
	if m.Callbacks.DraftChanged != nil {
		m.Callbacks.DraftChanged(id, m.Draft)
	}
	if m.Callbacks.SelectConversation != nil {
		m.Callbacks.SelectConversation(id)
	}
}
func (m *Model) SetDraft(value string) {
	m.Draft = value
	if m.Preferences.Drafts == nil {
		m.Preferences.Drafts = map[string]string{}
	}
	if m.SelectedID != "" {
		if value == "" {
			delete(m.Preferences.Drafts, m.SelectedID)
		} else {
			m.Preferences.Drafts[m.SelectedID] = value
		}
	}
	if m.Callbacks.DraftChanged != nil {
		m.Callbacks.DraftChanged(m.SelectedID, value)
	}
}

// ClearDrafts discards every in-memory conversation draft and asks the owner
// to clear each persisted projection. Call it when chat access is revoked or
// the authenticated identity changes.
func (m *Model) ClearDrafts() {
	if m == nil {
		return
	}
	if m.Callbacks.DraftChanged != nil {
		for id := range m.Preferences.Drafts {
			m.Callbacks.DraftChanged(id, "")
		}
		if m.SelectedID != "" && m.Draft != "" {
			m.Callbacks.DraftChanged(m.SelectedID, "")
		}
	}
	for id := range m.Preferences.Drafts {
		delete(m.Preferences.Drafts, id)
	}
	m.Draft = ""
}
func (m *Model) Send() {
	body := strings.TrimSpace(m.Draft)
	if body == "" || m.SelectedID == "" {
		return
	}
	if m.Callbacks.SendMessage != nil {
		m.Callbacks.SendMessage(m.SelectedID, body)
	}
	m.SetDraft("")
}

// Column bounds. The rail and the side column (details or thread) can be
// dragged between these, and RestorePanes returns them to the defaults.
const (
	// RailDefault: C-13 -- 280px truncated common rail names ("comp cycle
	// working gr…") at a 1440px viewport; 288 stays well inside RailMax.
	RailMin, RailMax, RailDefault          = 220, 420, 288
	DetailsMin, DetailsMax, DetailsDefault = 240, 440, 320
)

func (m *Model) ResizeRail(px int) {
	if px < RailMin {
		px = RailMin
	}
	if px > RailMax {
		px = RailMax
	}
	m.Pane.Rail = px
	m.save()
}
func (m *Model) ResizeDetails(px int) {
	if px < DetailsMin {
		px = DetailsMin
	}
	if px > DetailsMax {
		px = DetailsMax
	}
	m.Pane.Details = px
	m.save()
}
func (m *Model) save() {
	m.Preferences.Panes = m.Pane
	if m.Callbacks.SavePreferences != nil {
		m.Callbacks.SavePreferences(m.Preferences)
	}
}

// t resolves visible copy. Callers pass a key from Keys; the English default
// is the reviewed source string every other locale translates.
func (m Model) t(key string) string {
	return chatbug039CatalogText(m, key, englishCopy[key])
}

func (m Model) tf(key string, vars map[string]string) string {
	out := m.t(key)
	for name, value := range vars {
		out = strings.ReplaceAll(out, "{"+name+"}", value)
	}
	return out
}

// Build renders the complete workspace as a native GoWebComponents tree.
func Build(model Model) ui.Node { return ui.CreateElement(Workspace, model) }

func splitMembers(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(strings.ReplaceAll(raw, ";", ","), ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func kindKey(k ConversationKind) string {
	switch k {
	case DirectMessage:
		return KeyKindDirect
	case GroupChat:
		return KeyKindGroup
	case PrivateChannel:
		return KeyKindPrivate
	}
	return KeyKindPublic
}

func followLabel(v bool) string {
	if v {
		return englishCopy[KeyFollowing]
	}
	return englishCopy[KeyFollow]
}
func followLabelFor(m Model, v bool) string {
	if v {
		return m.t(KeyFollowing)
	}
	return m.t(KeyFollow)
}
func minuteClock(v int) string {
	if v < 0 {
		v = 0
	}
	if v > 1439 {
		v = 1439
	}
	return fmt.Sprintf("%02d:%02d", v/60, v%60)
}
func parseClock(s string) int {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	if h < 0 {
		h = 0
	}
	if h > 23 {
		h = 23
	}
	if m < 0 {
		m = 0
	}
	if m > 59 {
		m = 59
	}
	return h*60 + m
}
func direction(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "ar") {
		return "rtl"
	}
	return "ltr"
}

// iconFor keeps the text glyph vocabulary for callers that need a plain-text
// marker (accessible names, notifications); the rendered tree uses icon().
func iconFor(k ConversationKind) string {
	if k == DirectMessage {
		return "@"
	}
	if k == GroupChat {
		return "@@"
	}
	if k == PrivateChannel {
		return "🔒"
	}
	return "#"
}
func initials(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "?"
	}
	parts := strings.Fields(s)
	if len(parts) == 1 {
		r := []rune(parts[0])
		return strings.ToUpper(string(r[0]))
	}
	return strings.ToUpper(string([]rune(parts[0])[0]) + string([]rune(parts[len(parts)-1])[0]))
}
func statusText(v bool) string {
	if v {
		return englishCopy[KeyOnline]
	}
	return englishCopy[KeyOffline]
}
func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// stats is the thread line under a message. Reactions are chips now, not a
// count in prose, so the only statistic left is the reply count.
func stats(m Model, msg Message) string {
	if msg.Replies >= 1 {
		return chatPlural(m.Locale, "replies", msg.Replies)
	}
	return ""
}
func itoa(n int) string {
	if n == 0 {
		return ""
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	b := [20]byte{}
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
