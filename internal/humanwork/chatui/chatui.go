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
)

type LoadState string

const (
	StateReady   LoadState = "ready"
	StateLoading LoadState = "loading"
	StateEmpty   LoadState = "empty"
	StateError   LoadState = "error"
)

type ConversationKind string

const (
	PublicChannel  ConversationKind = "public-channel"
	PrivateChannel ConversationKind = "private-channel"
	DirectMessage  ConversationKind = "direct"
	GroupChat      ConversationKind = "group"
)

type Conversation struct {
	ID, Name, Topic, Avatar, Preview string
	OwnerID, HostTenantID            string
	Kind                             ConversationKind
	Unread, Mentions                 int
	MemberCount                      int
	// LastActivity is when the newest message was sent, when the listing
	// says; the browse row reads it so a reader can tell a live channel from
	// a dead one before joining.
	LastActivity   time.Time
	Muted, Starred bool
	// Joined is false for a channel the viewer can browse but has not joined.
	Joined bool
}

type Member struct {
	ID, HomeTenantID, Name, Subtitle, Avatar string
	Online                                   bool
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
	ID, Name, JobTitle, Manager, Department, Phone, Email string
	Location, Company, BusinessUnit, PhotoURL             string
	Ready                                                 bool
}

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
}

// ChannelPin is an authorized pin projection, independent of the timeline page.
type ChannelPin struct {
	PostID, Author, Body string
	Sequence             uint64
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
}

// reactionPalette is the quick picker: the eight reactions people reach for.
var reactionPalette = []string{"👍", "❤️", "😂", "🎉", "👀", "🙏", "✅", "🔥"}

// Attachment is one media reference on a message.
type Attachment struct {
	ID, Name, ContentType, URL string
	Width, Height              int
	Bytes                      int64
	PreviewUnavailable         bool
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
	SelectConversation            func(string)
	Search                        func(string)
	SearchMore                    func()
	SearchMoreChannels            func()
	OpenSearchMessage             func(conversationID, postID string, sequence uint64)
	OpenSearchChannel             func(conversationID string)
	LoadMembers                   func()
	LoadOlder                     func()
	LoadNewer                     func()
	VisibleMessageIDs             func([]string)
	DownloadAttachment            func(string, string)
	OpenCreate                    func()
	CloseCreate                   func()
	CreateConversation            func(ConversationKind, string, []string)
	OpenBrowse                    func()
	CloseBrowse                   func()
	JoinConversation              func(string)
	ToggleSection                 func(string)
	ReorderSection                func(string, int)
	CreateSection                 func(string)
	RemoveSection                 func(string)
	MoveConversationSection       func(string, string)
	SendMessage                   func(string, string)
	DraftChanged                  func(string, string)
	OpenThread                    func(string)
	CloseThread                   func()
	LoadOlderThread               func()
	LoadNewerThread               func()
	SetThreadFollow               func(bool)
	React                         func(string)
	RemoveReaction                func(string)
	Pin                           func(string)
	Unpin                         func(string)
	JumpToPin                     func(string, uint64)
	CopyPinReference              func(string)
	AddChannelTodo                func(string, string)
	SetChannelTodoDraft           func(string)
	SetChannelTodoSourcePin       func(string)
	SetChannelTodoCompleted       func(string, bool)
	DeleteChannelTodo             func(string)
	SetChannelTodoPinned          func(bool)
	SetChannelTodoPolicy          func(string, string, []ChannelTodoSelectedMember)
	SetChannelTodoNewPolicy       func(string, []ChannelTodoSelectedMember)
	RetryChannelTodo              func()
	OpenChannelTodo               func()
	RetryChannelWidgets           func()
	SetChannelWidgetPinned        func(string, bool)
	SetChannelTeamPurpose         func(string)
	SetChannelTeamRoleLabel       func(string, string, string)
	SetChannelProjectDetails      func(string, string)
	AddChannelProjectMilestone    func(ChannelProjectMilestone)
	UpdateChannelProjectMilestone func(ChannelProjectMilestone)
	DeleteChannelProjectMilestone func(string)
	RetryChannelPoll              func()
	OpenChannelPoll               func(string)
	CreateChannelPoll             func(string, []string)
	VoteChannelPoll               func(string)
	SavePreferences               func(Preferences)
	SetConversationNotification   func(string, NotificationMode)
	OpenConversationDetails       func(string)
	OpenRailMenu                  func(string)
	Retry                         func()
	ToggleDetails                 func(bool)
	ToggleSidebar                 func(bool)
	BeginEdit                     func(string)
	CancelEdit                    func()
	SetEditDraft                  func(string, string)
	EditMessage                   func(string, string, uint64)
	DeleteMessage                 func(string, uint64)
	ResizeRail                    func(int)
	ResizeDetails                 func(int)
	RestorePanes                  func()
	DismissNotice                 func()
	// ReplyInThread posts a reply under the open thread's root message. When
	// nil the thread pane has no composer of its own.
	ReplyInThread func(parentID, body string)
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
	OpenEmbeddedMessage     func(string)
	// JumpToNewest scrolls the timeline to its newest message.
	JumpToNewest       func()
	OpenPerson         func(string)
	ClosePerson        func()
	StartDirectMessage func(string)
}

type Model struct {
	State  LoadState
	Error  string
	Notice string
	// NoticeRetry offers a retry control beside the notice (for example when
	// the live feed has stopped and Callbacks.Retry resubscribes).
	NoticeRetry               bool
	Conversations             []Conversation
	Sections                  []SidebarSection
	SelectedID                string
	Messages                  []Message
	ChannelPins               []ChannelPin
	ChannelTodo               ChannelTodoList
	ChannelTodoLoading        bool
	ChannelTodoPending        bool
	ChannelTodoError          string
	ChannelTodoDraft          string
	ChannelTodoSourcePin      string
	ChannelTodoNewMode        string
	ChannelTodoNewSelected    []ChannelTodoSelectedMember
	CanPinChannelTodo         bool
	ChannelTeam               ChannelTeamWidget
	ChannelProject            ChannelProjectWidget
	ChannelWidgetsLoading     bool
	ChannelWidgetsPending     bool
	ChannelWidgetsError       string
	ChannelPoll               ChannelPoll
	ChannelPollLoading        bool
	ChannelPollPending        bool
	ChannelPollError          string
	PinReferenceUnavailable   bool
	HasOlder                  bool
	HasNewer                  bool
	Members                   []Member
	Draft                     string
	Search                    string
	SearchLoading             bool
	SearchLoadingMore         bool
	SearchLoadingMoreChannels bool
	SearchError               string
	SearchMoreError           string
	SearchMoreChannelsError   string
	SearchNextCursor          string
	SearchChannelNextCursor   string
	SearchHasMore             bool
	SearchHasMoreChannels     bool
	FocusMessageID            string
	SearchChannels            []Conversation
	SearchPeople              []SearchPerson
	SearchMessages            []SearchMessage
	// SearchDirectory is the session's already-authorized worker projection.
	// It is retained separately from active chat memberships so search can
	// find a visible coworker before the viewer has a direct message with them.
	SearchDirectory []SearchPerson
	ShowCreate      bool
	ShowBrowse      bool
	Browse          []Conversation
	BrowseQuery     string
	SidebarOpen     bool
	ShowDetails     bool
	ShowThread      bool
	ShowPerson      bool
	PersonDetails   *PersonDetails
	ThreadFollowed  bool
	ThreadParentID  string
	ThreadParent    *Message
	ThreadMessages  []Message
	ThreadLoading   bool
	ThreadHasOlder  bool
	ThreadHasNewer  bool
	EditingID       string
	EditDrafts      map[string]string
	NewKind         ConversationKind
	NewName         string
	Pane            PaneSizes
	Preferences     Preferences
	Callbacks       Callbacks
	Locale          string
	Direction       string
	// GiphyAPIKey is the public browser key supplied by the authenticated shell.
	GiphyAPIKey                  string
	CurrentUser, CurrentTenantID string
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
	EmbedOrigin                                                                string
	Embeds                                                                     map[string]LinkEmbed
	EmbedRevision                                                              uint64
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
}

// n formats a count for display.
func (m Model) n(v int) string {
	if m.Number != nil {
		return m.Number(v)
	}
	return itoa(v)
}

func (m Model) selected() Conversation {
	for _, c := range m.Conversations {
		if c.ID == m.SelectedID {
			return c
		}
	}
	return Conversation{ID: m.SelectedID, Kind: PublicChannel}
}

func (m *Model) Select(id string) {
	m.SelectedID = id
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
		m.Preferences.Drafts[m.SelectedID] = value
	}
	if m.Callbacks.DraftChanged != nil {
		m.Callbacks.DraftChanged(m.SelectedID, value)
	}
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
	RailMin, RailMax, RailDefault          = 220, 420, 280
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
	if m.Text != nil {
		if v := strings.TrimSpace(m.Text(key)); v != "" && v != key {
			return v
		}
	}
	if v, ok := englishCopy[key]; ok {
		return v
	}
	return key
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
	switch {
	case msg.Replies == 1:
		return m.t(KeyRepliesOne)
	case msg.Replies > 1:
		return m.tf(KeyReplies, map[string]string{"n": m.n(msg.Replies)})
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
