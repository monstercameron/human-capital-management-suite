// Package projectboard contains pure board view configuration and bounded
// projection logic. It does not own workflow status definitions or task access.
package projectboard

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const MaxPageSize = 100

const (
	CardTitle    = "title"
	CardStatus   = "status"
	CardAssignee = "assignee"
	CardPriority = "priority"
	CardType     = "type"
	CardDueDate  = "due_date"
)

var (
	ErrInvalidView   = errors.New("invalid board view")
	ErrInvalidPage   = errors.New("invalid board page")
	ErrInvalidSource = errors.New("authorized task source is required")
)

type Audience string

const (
	AudiencePersonal Audience = "PERSONAL"
	AudienceProject  Audience = "PROJECT"
)

type Column struct {
	ID        string
	Label     string
	StatusIDs []string
}

type GroupingKind string

const (
	GroupNone     GroupingKind = "NONE"
	GroupAssignee GroupingKind = "ASSIGNEE"
	GroupPriority GroupingKind = "PRIORITY"
	GroupType     GroupingKind = "TYPE"
	GroupEnum     GroupingKind = "ENUM_FIELD"
)

type Grouping struct {
	Kind    GroupingKind
	FieldID string // Required only for GroupEnum; field values are task enum values.
}

type Filter struct {
	StatusIDs  []string
	AssigneeID string
	Priorities []string
	TypeIDs    []string
	EnumFields map[string][]string
}

type OrderField string

const (
	OrderTaskID   OrderField = "TASK_ID"
	OrderTitle    OrderField = "TITLE"
	OrderPriority OrderField = "PRIORITY"
	OrderDueDate  OrderField = "DUE_DATE"
)

type BoardView struct {
	ID                 string
	Name               string `json:"Name,omitempty"`
	Version            uint64
	Audience           Audience
	Columns            []Column
	Filter             Filter
	Grouping           Grouping
	OrderBy            OrderField
	Descending         bool
	CardFields         []string
	SwimlaneValueOrder []string `json:"SwimlaneValueOrder,omitempty"`
}

// Task is the minimal task projection needed to place a card on a board.
// Sources must only return tasks authorized for the current request.
type Task struct {
	ID           string
	Title        string
	StatusID     string
	AssigneeID   string
	AssigneeName string
	Priority     string
	TypeID       string
	DueDate      string
	EnumFields   map[string]string
	// Planning fields shown on cards.
	Labels      []string
	StoryPoints uint32
	StartDate   string
}

type Cursor struct {
	Token string
}

type AuthorizedTaskQuery struct {
	ViewID      string
	ViewVersion uint64
	OrderBy     OrderField
	Descending  bool
	// StatusIDs is an exact allowlist; an empty slice means no task matches.
	StatusIDs []string
	Filter    Filter
	Limit     int
	Cursor    Cursor
}

// AuthorizedTaskSource is the access boundary. Implementations must enforce
// current task authorization first, apply query status/filter predicates to
// that authorized set, and then page in a stable order. The source must cap
// results at Limit and return a continuation cursor for matching tasks only.
type AuthorizedTaskSource interface {
	ListAuthorizedTasks(context.Context, AuthorizedTaskQuery) ([]Task, *Cursor, error)
}

type Card struct{ Task Task }

type Lane struct {
	ID    string
	Label string
	Cards []Card
}

type BoardColumn struct {
	ID    string
	Label string
	Lanes []Lane
	Count int // Count of authorized, filtered cards in this returned page.
}

type Page struct {
	ViewID  string
	Version uint64
	Columns []BoardColumn
	Next    *Cursor
}

func Validate(view BoardView) error {
	if strings.TrimSpace(view.ID) == "" || view.Version == 0 {
		return fmt.Errorf("%w: ID and positive version are required", ErrInvalidView)
	}
	if view.Name != "" && strings.TrimSpace(view.Name) == "" {
		return fmt.Errorf("%w: name cannot be whitespace", ErrInvalidView)
	}
	if view.Audience != AudiencePersonal && view.Audience != AudienceProject {
		return fmt.Errorf("%w: unknown audience", ErrInvalidView)
	}
	if len(view.Columns) == 0 {
		return fmt.Errorf("%w: at least one column is required", ErrInvalidView)
	}
	columnIDs, mappedStatuses := map[string]bool{}, map[string]bool{}
	for _, column := range view.Columns {
		if strings.TrimSpace(column.ID) == "" || strings.TrimSpace(column.Label) == "" || columnIDs[column.ID] {
			return fmt.Errorf("%w: column IDs and labels must be nonempty and IDs unique", ErrInvalidView)
		}
		columnIDs[column.ID] = true
		for _, statusID := range column.StatusIDs {
			if strings.TrimSpace(statusID) == "" || mappedStatuses[statusID] {
				return fmt.Errorf("%w: status IDs must be nonempty and map to one column", ErrInvalidView)
			}
			mappedStatuses[statusID] = true
		}
	}
	cardFields := map[string]bool{}
	for _, field := range view.CardFields {
		if strings.TrimSpace(field) == "" || cardFields[field] {
			return fmt.Errorf("%w: card fields must be nonempty and unique", ErrInvalidView)
		}
		cardFields[field] = true
	}
	laneValues := map[string]bool{}
	for _, value := range view.SwimlaneValueOrder {
		if strings.TrimSpace(value) == "" || laneValues[value] {
			return fmt.Errorf("%w: swimlane value order must contain unique nonempty stable IDs", ErrInvalidView)
		}
		laneValues[value] = true
	}
	switch view.Grouping.Kind {
	case GroupNone, GroupAssignee, GroupPriority, GroupType:
		if view.Grouping.FieldID != "" {
			return fmt.Errorf("%w: field ID only applies to enum grouping", ErrInvalidView)
		}
	case GroupEnum:
		if strings.TrimSpace(view.Grouping.FieldID) == "" {
			return fmt.Errorf("%w: enum grouping requires field ID", ErrInvalidView)
		}
	default:
		return fmt.Errorf("%w: unsupported grouping", ErrInvalidView)
	}
	if view.OrderBy != "" && view.OrderBy != OrderTaskID && view.OrderBy != OrderTitle && view.OrderBy != OrderPriority && view.OrderBy != OrderDueDate {
		return fmt.Errorf("%w: unsupported order field", ErrInvalidView)
	}
	return nil
}

// BuildPage requests at most MaxPageSize tasks and only builds lanes and counts
// from the authorized page returned by source. It intentionally exposes no
// total count because that would require reading tasks outside the page.
func BuildPage(ctx context.Context, source AuthorizedTaskSource, view BoardView, limit int, cursor *Cursor) (Page, error) {
	if err := Validate(view); err != nil {
		return Page{}, err
	}
	if source == nil {
		return Page{}, ErrInvalidSource
	}
	if limit < 1 || limit > MaxPageSize {
		return Page{}, fmt.Errorf("%w: limit must be 1..%d", ErrInvalidPage, MaxPageSize)
	}
	query := AuthorizedTaskQuery{
		ViewID: view.ID, ViewVersion: view.Version,
		OrderBy: view.OrderBy, Descending: view.Descending,
		StatusIDs: mappedStatuses(view), Filter: view.Filter, Limit: limit,
	}
	if cursor != nil {
		query.Cursor = *cursor
	}
	tasks, next, err := source.ListAuthorizedTasks(ctx, query)
	if err != nil {
		return Page{}, err
	}
	if len(tasks) > limit {
		return Page{}, fmt.Errorf("%w: source returned more than requested limit", ErrInvalidPage)
	}

	cols := make([]BoardColumn, len(view.Columns))
	statusColumns := map[string]int{}
	for i, c := range view.Columns {
		cols[i] = BoardColumn{ID: c.ID, Label: c.Label}
		for _, status := range c.StatusIDs {
			statusColumns[status] = i
		}
	}
	for _, task := range tasks {
		if !matches(view.Filter, task) {
			continue
		}
		col, ok := statusColumns[task.StatusID]
		if !ok {
			continue
		}
		cols[col].Count++
		laneID, laneLabel := laneFor(view.Grouping, task)
		lane := findOrAddLane(&cols[col], laneID, laneLabel)
		lane.Cards = append(lane.Cards, Card{Task: task})
	}
	for i := range cols {
		sort.Slice(cols[i].Lanes, func(a, b int) bool {
			return laneLess(view.SwimlaneValueOrder, cols[i].Lanes[a].ID, cols[i].Lanes[b].ID)
		})
		for j := range cols[i].Lanes {
			sort.Slice(cols[i].Lanes[j].Cards, func(a, b int) bool { return less(view, cols[i].Lanes[j].Cards[a].Task, cols[i].Lanes[j].Cards[b].Task) })
			for k := range cols[i].Lanes[j].Cards {
				cols[i].Lanes[j].Cards[k].Task = projectCard(cols[i].Lanes[j].Cards[k].Task, view.CardFields)
			}
		}
	}
	return Page{ViewID: view.ID, Version: view.Version, Columns: cols, Next: next}, nil
}

func laneLess(order []string, a, b string) bool {
	positions := make(map[string]int, len(order))
	for i, id := range order {
		positions[id] = i
	}
	ai, aok := positions[a]
	bi, bok := positions[b]
	if aok && bok {
		return ai < bi
	}
	if aok {
		return true
	}
	if bok {
		return false
	}
	return a < b
}

func mappedStatuses(view BoardView) []string {
	statuses := make([]string, 0)
	filterSet := make(map[string]bool, len(view.Filter.StatusIDs))
	for _, status := range view.Filter.StatusIDs {
		filterSet[status] = true
	}
	for _, column := range view.Columns {
		for _, status := range column.StatusIDs {
			if len(filterSet) == 0 || filterSet[status] {
				statuses = append(statuses, status)
			}
		}
	}
	return statuses
}

// projectCard always retains the stable task ID and returns only configured
// standard fields and enum custom fields. Empty CardFields therefore exposes
// IDs only.
func projectCard(task Task, fields []string) Task {
	card := Task{ID: task.ID}
	for _, field := range fields {
		switch field {
		case CardTitle:
			card.Title = task.Title
		case CardStatus:
			card.StatusID = task.StatusID
		case CardAssignee:
			card.AssigneeID, card.AssigneeName = task.AssigneeID, task.AssigneeName
		case CardPriority:
			card.Priority = task.Priority
		case CardType:
			card.TypeID = task.TypeID
		case CardDueDate:
			card.DueDate = task.DueDate
		default:
			if value, ok := task.EnumFields[field]; ok {
				if card.EnumFields == nil {
					card.EnumFields = make(map[string]string)
				}
				card.EnumFields[field] = value
			}
		}
	}
	return card
}

func matches(f Filter, t Task) bool {
	if len(f.StatusIDs) > 0 && !contains(f.StatusIDs, t.StatusID) {
		return false
	}
	if f.AssigneeID != "" && f.AssigneeID != t.AssigneeID {
		return false
	}
	if len(f.Priorities) > 0 && !contains(f.Priorities, t.Priority) {
		return false
	}
	if len(f.TypeIDs) > 0 && !contains(f.TypeIDs, t.TypeID) {
		return false
	}
	for id, values := range f.EnumFields {
		if len(values) > 0 && !contains(values, t.EnumFields[id]) {
			return false
		}
	}
	return true
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func laneFor(g Grouping, t Task) (string, string) {
	var id, label string
	switch g.Kind {
	case GroupAssignee:
		id, label = t.AssigneeID, t.AssigneeName
		if id == "" {
			id, label = "_unassigned", "Unassigned"
		}
	case GroupPriority:
		id, label = t.Priority, t.Priority
	case GroupType:
		id, label = t.TypeID, t.TypeID
	case GroupEnum:
		id, label = t.EnumFields[g.FieldID], t.EnumFields[g.FieldID]
	default:
		return "_all", "All tasks"
	}
	if id == "" {
		return "_none", "(none)"
	}
	if label == "" {
		label = id
	}
	return id, label
}

func findOrAddLane(c *BoardColumn, id, label string) *Lane {
	for i := range c.Lanes {
		if c.Lanes[i].ID == id {
			return &c.Lanes[i]
		}
	}
	c.Lanes = append(c.Lanes, Lane{ID: id, Label: label})
	return &c.Lanes[len(c.Lanes)-1]
}

func less(view BoardView, a, b Task) bool {
	var av, bv string
	switch view.OrderBy {
	case OrderTitle:
		av, bv = FoldTitleForOrder(a.Title), FoldTitleForOrder(b.Title)
	case OrderPriority:
		av, bv = a.Priority, b.Priority
	case OrderDueDate:
		av, bv = a.DueDate, b.DueDate
	default:
		av, bv = a.ID, b.ID
	}
	if av == bv {
		av, bv = a.ID, b.ID
	}
	if view.Descending {
		return av > bv
	}
	return av < bv
}

// FoldTitleForOrder folds ASCII uppercase letters only. Board title ordering
// uses this same key in PostgreSQL under the C collation, so cursor boundaries
// remain stable across database locale and Go Unicode case-folding differences.
func FoldTitleForOrder(title string) string {
	b := []byte(title)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
