// Package projectui renders presentation-only project board and task detail
// components. Callers must build Model from the current authorized projection;
// this package has no persistence or authorization boundary.
package projectui

import "time"

// ViewMode selects the board or accessible list presentation.
type ViewMode string

const (
	ViewBoard ViewMode = "board"
	ViewList  ViewMode = "list"
	// ViewTask is one task's full page; the host renders it with TaskPage.
	ViewTask ViewMode = "task"
)

// Lane grouping kinds decide what a drop into another swimlane changes.
const (
	LaneKindAssignee = "assignee"
	LaneKindPriority = "priority"
	LaneKindField    = "field"
)

// Model contains only the authorized cards for one already-filtered page.
// Cards beyond the first 100 are ignored as a defense against unbounded input.
type Model struct {
	Title            string
	Description      string
	Mode             ViewMode
	ViewID           string
	ViewRevision     uint64
	WorkflowRevision uint64
	SwimlaneGrouping string
	SwimlaneFieldID  string
	Columns          []Column
	Lanes            []Lane
	Cards            []Card
	Page             Page
	PendingText      string
	Copy             Copy
	// Today anchors due-date tones (overdue, due soon). The zero value
	// means the renderer's current local date.
	Today time.Time
	// ProjectID and ProjectName identify the board for breadcrumbs and the
	// per-viewer lane collapse memory; they grant nothing.
	ProjectID   string
	ProjectName string
	// LaneKind is one of the LaneKind* constants when the board has
	// swimlanes; LaneFieldID names the enum field for LaneKindField.
	LaneKind    string
	LaneFieldID string
	// ProjectsHref, when set, renders the "Projects > project" breadcrumb.
	ProjectsHref string
	// Members are the project's active members, offered as assignees when a
	// task is created; EnumFields are the enum custom fields a board can
	// group its lanes by.
	Members    []Person
	EnumFields []Status
	// ListSort orders the list view ("task", "-due", ...); SortHrefs holds
	// the address for each column header, which toggles the direction when
	// that column is already the sort. Without hrefs the headers are text.
	ListSort  string
	SortHrefs map[string]string
	// Filters, when set, renders the quick-filter bar; cards it hides are
	// marked FilteredOut by the host. RouteFilter and RouteQuery are the
	// address values, carried into every board, list and task link.
	Filters     *FilterBar
	RouteFilter string
	RouteQuery  string
	// columnTotals counts every card per column before filtering.
	columnTotals map[string]int
	// Share, when set, renders the board's Share menu and the send-to-chat
	// dialog the board and its cards use.
	Share *Share
	// HomeTab and Tickets carry the Projects home's tickets tab when the
	// home is on it.
	HomeTab string
	Tickets *TicketList
	// ListPager pages the list view; nil shows every card.
	ListPager *Pager
	// Workflows are the journeys linked from this project's tickets, for
	// the board's Workflows panel.
	Workflows []WorkflowLink
}

// Pager is a paged table's footer: the range shown, the rows-per-page
// choice and the page links. From and To are 1-based and inclusive.
type Pager struct {
	From, To, Total    int
	Page, Pages        int
	PrevHref, NextHref string
	// Sizes are the rows-per-page choices; each Href is the address with
	// that size, back on page one.
	Sizes []FilterChoice
	// SizeParam is the page size the address named (0 for the default),
	// carried into links that return to this page.
	SizeParam int
}

// Copy contains localized user-facing strings. Empty values use English
// defaults; applications should fill these from their locale catalog.
type Copy struct {
	Pending, Conflict                               string
	Status, Lane                                    string
	StatusFor, LaneFor                              string
	BoardLabel, TasksLabel                          string
	StatusField, AssigneeField, DueField            string
	NoTasksLane, NoTasksColumn, NoTasksPage         string
	PreviousPage, NextPage, TaskPages, MoreInLane   string
	TaskDetails, LinkedItems, NoLinkedItems         string
	Comments, NoComments, MoreComments              string
	Activity, NoActivity, MoreActivity              string
	Loading, Restricted, Unavailable                string
	LinkedItem, LinkedChatItem, LinkedDocsItem      string
	PriorityField, TypeField, TaskField             string
	DueToday, Overdue, DueSoon                      string
	CollapseAll, ExpandAll, ToggleLane, MoveTask    string
	LaneField, Unassigned, OpenTaskPage, Close      string
	Description, NoDescription, AddDescription      string
	Save, Cancel, Edit, Delete, AddComment          string
	CommentPlaceholder, Edited, Saving, Saved       string
	SaveFailed, Projects, TaskKey, Details          string
	TouchMoveHint, LatestComments, AllComments      string
	OpenTask, MoveTo, AssignTo, CopyLink            string
	LinkCopied, MoreActions, NoDate, NoCommentsHint string
	// StatusLocked explains a finished task whose workflow has no step
	// back; StatusReadOnly explains any other status the viewer cannot move.
	StatusLocked, StatusReadOnly string
	// StatusLockedShort is the one-line caption under a locked status.
	StatusLockedShort string
	// MoveNotAllowed labels a status the workflow does not offer next;
	// LaneEmptyHere is the phone placeholder in an empty swimlane cell;
	// AddDueDate prompts for a missing due date.
	MoveNotAllowed, LaneEmptyHere, AddDueDate string
	PageSummary                               func(page, total int) string
	// ColumnCount is the screen-reader phrase for a column or lane count.
	ColumnCount func(count int) string
	// FormatDate renders a civil due date; nil uses a short English form.
	FormatDate func(date time.Time) string
	// FormatNumber renders a count in the locale's digits; nil uses ASCII.
	FormatNumber func(n int) string
	// SortedAscending and SortedDescending describe the list's sort
	// column to assistive technology.
	SortedAscending, SortedDescending string
	// Board holds the filter bar, first-run and shortcut strings.
	Board BoardCopy
	// Workflow holds the linked-workflow strings.
	Workflow WorkflowCopy
	// ActivityRepeat is the suffix for folded activity ("3 times").
	ActivityRepeat func(n int) string
}

// Column maps a stable workflow status ID to a display column. The renderer
// treats statuses as identifiers and never infers workflow transitions.
type Column struct {
	ID       string
	Label    string
	Statuses []Status
	// Tone optionally names the workflow category the column represents:
	// "planned", "todo", "active", "review", "blocked", "done" or
	// "cancelled". When empty the renderer infers one from stable status
	// IDs and the column's position; it is presentation only.
	Tone string
}

// Status is a stable workflow status choice.
type Status struct {
	ID    string
	Label string
}

// Lane identifies a stable value in the optional grouping field.
type Lane struct {
	ID    string
	Label string
	// Count is computed over the authorized, filtered projection only.
	Count   int
	MayEdit bool
	// Collapsed is the viewer's remembered presentation for this lane.
	Collapsed bool
}

// Card is a presentation projection. Its fields must already be authorized
// and localized by the caller before being supplied here.
type Card struct {
	ID               string
	TaskRevision     uint64
	WorkflowRevision uint64
	Title            string
	Summary          string
	Assignee         string
	DueDate          string
	ColumnID         string
	StatusID         string
	// StatusOptions lists only valid target statuses for this task at the
	// current workflow/configuration revision.
	StatusOptions []Status
	LaneID        string
	Type          string
	Priority      string
	DetailHref    string
	Pending       bool
	Conflict      string
	ConflictState bool
	FailedAction  string // "status" or "lane"; the host uses this to restore focus after rollback.
	CanMoveStatus bool
	CanMoveLane   bool
	StatusLabel   string
	LaneLabel     string
	// Selected marks the card whose detail is open beside the board.
	Selected bool
	// AssigneePhoto is an optional same-origin portrait URL.
	AssigneePhoto string
	// PriorityID is the stable priority (TASK_PRIORITY_HIGH). Rendering
	// logic keys on it; Priority is only the translated label.
	PriorityID string
	// FilteredOut hides the card: the quick filter does not match it.
	FilteredOut bool
	// Labels and StoryPoints are the task's planning fields.
	Labels      []string
	StoryPoints int
	// Workflows names the workflows this task links ("Promotion").
	Workflows []string
}

// Page describes one globally bounded page over every lane.
type Page struct {
	Number       int
	Total        int
	HasNext      bool
	HasPrevious  bool
	MoreInLane   bool
	PreviousHref string
	NextHref     string
}

// ReferenceState is the disclosure state for a linked Chat or Docs item.
type ReferenceState string

const (
	ReferenceReady       ReferenceState = "ready"
	ReferenceLoading     ReferenceState = "loading"
	ReferenceRestricted  ReferenceState = "restricted"
	ReferenceUnavailable ReferenceState = "unavailable"
)

// Reference is safe link presentation. Titles and URLs are populated only
// for ReferenceReady; other states render a neutral localized status.
type Reference struct {
	Kind  string
	State ReferenceState
	// Title and Href may be rendered only when State is ReferenceReady.
	Title string
	Href  string
}

// Comment is an authorized, already-paginated detail entry.
type Comment struct {
	Author string
	Time   string
	Body   string
	// ID and Revision address an editable comment; Own marks the viewer's
	// own comment, the only kind the page offers to edit or delete.
	ID          string
	Revision    uint64
	Own         bool
	Edited      bool
	AuthorPhoto string
	// DateTime is the machine-readable timestamp for the <time> element.
	DateTime string
}

// Activity is an authorized, already-paginated activity entry.
type Activity struct {
	Label string
	Time  string
	Actor string
}

// Person is an authorized project member offered as an assignee.
type Person struct {
	ID       string
	Name     string
	PhotoURL string
}

// DetailModel supplies authorized task fields and bounded comment/activity
// pages. As with Model, authorization and pagination happen upstream.
type DetailModel struct {
	Title            string
	Description      string
	Status           string
	Assignee         string
	DueDate          string
	Fields           []Fact
	Comments         []Comment
	Activity         []Activity
	Links            []Reference
	CommentsMoreHref string
	ActivityMoreHref string
	Copy             Copy

	// Editing identity. Zero values render the detail read-only.
	ProjectID        string
	ProjectName      string
	TaskID           string
	TaskRevision     uint64
	WorkflowRevision uint64
	StatusID         string
	// StatusOptions lists the current status and its allowed targets.
	StatusOptions   []Status
	AssigneeID      string
	AssigneePhoto   string
	Members         []Person
	Priority        string
	PriorityID      string
	PriorityOptions []Status
	Type            string
	// LaneLabel and LaneOptions describe an enum-field swimlane; empty
	// when the board groups by assignee or priority (those fields are the
	// lane) or has no lanes.
	LaneFieldName string
	LaneID        string
	LaneOptions   []Status
	CanEdit       bool
	CanMoveStatus bool
	CanComment    bool
	// FieldStates maps a field ("status", "assignee", "priority", "due",
	// "title", "description", "comment", "lane") to "saving", "saved" or
	// "error"; FieldErrors carries the error sentence for "error".
	FieldStates map[string]string
	FieldErrors map[string]string
	// Hrefs for navigation between the modal, the task page and the board.
	ProjectsHref string
	BoardHref    string
	PageHref     string
	// CommentTotal counts every visible comment when Comments is a subset.
	CommentTotal int
	Today        time.Time
	// Planning fields. StoryPoints 0 means no estimate.
	StartDate   string
	StoryPoints int
	Labels      []string
	// LabelSuggestions are labels already used in the project.
	LabelSuggestions []string
	// Reporter created the task; CreatedAt and UpdatedAt are RFC 3339,
	// CreatedText and UpdatedText their localized relative forms.
	Reporter, ReporterPhoto  string
	CreatedAt, UpdatedAt     string
	CreatedText, UpdatedText string
	// Share, when set, renders the Share menu and the send-to-chat dialog.
	Share *Share
	// Workflows are the linked workflow items; WorkflowOptions feed the
	// "Link workflow" picker (loading while the Work read is in flight).
	Workflows              []WorkflowLink
	WorkflowOptions        []WorkflowOption
	WorkflowOptionsLoading bool
}

// Share is what the Share menu and the send-to-chat dialog need: the
// in-app address of the thing shared and the conversations it can go to.
type Share struct {
	Href, Title string
	Targets     []ShareTarget
	// Fill asks the host to read the conversations when the dialog opens
	// (a page whose loader does not supply them).
	Fill bool
}

// ShareTarget is one conversation a link can be sent to.
type ShareTarget struct {
	ID, Label string
	// Direct marks a direct message rather than a channel.
	Direct bool
}

// Fact is a visible task field.
type Fact struct {
	Label string
	Value string
}
