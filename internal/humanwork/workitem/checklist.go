package workitem

import (
	"time"

	"github.com/google/uuid"
)

// Repeatable procedures (SMB-003): reusable checklist templates for
// onboarding, offboarding and team procedures, instantiated per person with
// ordered items of every kind. This file owns the kernel only: template and
// checklist shapes, instantiation, and item transitions. Persistence and
// serving belong to the stores and shells, which also own sharing
// enforcement; the RoleRefs recorded here are the sharing intent those
// layers enforce.

// ItemKind declares what completing an item means.
type ItemKind string

const (
	// ItemKindDo is an action in another system (provision, create, file).
	ItemKindDo ItemKind = "do"
	// ItemKindRead is reading a document or page to acknowledge it.
	ItemKindRead ItemKind = "read"
	// ItemKindSign is recording a signature or agreement.
	ItemKindSign ItemKind = "sign"
	// ItemKindUpload is attaching evidence or a document.
	ItemKindUpload ItemKind = "upload"
	// ItemKindConfirm is confirming a fact with another person or system.
	ItemKindConfirm ItemKind = "confirm"
)

// Valid reports whether k is a declared item kind.
func (k ItemKind) Valid() bool {
	switch k {
	case ItemKindDo, ItemKindRead, ItemKindSign, ItemKindUpload, ItemKindConfirm:
		return true
	default:
		return false
	}
}

// StepState declares where an item stands.
type StepState string

const (
	// StepStateOpen is actionable and incomplete.
	StepStateOpen StepState = "open"
	// StepStateDone is completed with an actor and timestamp.
	StepStateDone StepState = "done"
	// StepStateSkipped is deliberately waived; only non-required items move
	// here.
	StepStateSkipped StepState = "skipped"
)

// Valid reports whether s is a declared item state.
func (s StepState) Valid() bool {
	switch s {
	case StepStateOpen, StepStateDone, StepStateSkipped:
		return true
	default:
		return false
	}
}

// TemplateItem is one ordered step of a reusable procedure.
type TemplateItem struct {
	Kind     ItemKind
	Title    string
	Detail   string
	Required bool
}

// ChecklistTemplate is a reusable repeatable procedure: onboarding,
// offboarding or a team routine. Templates are validated before storage;
// instantiation never mutates the template.
type ChecklistTemplate struct {
	TemplateID uuid.UUID
	TenantID   uuid.UUID
	Title      string
	RoleRefs   []string
	Items      []TemplateItem
	CreatedAt  time.Time
}

// Validate reports whether the template is storable: identity and title
// present, sharing roles named and unique, at least one item, every kind
// declared, every title non-blank.
func (t ChecklistTemplate) Validate() error {
	id := t.TemplateID.String()
	switch {
	case t.TemplateID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "template id must not be the nil UUID")
	case t.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "tenant id must not be the nil UUID")
	case !semanticKey(t.Title):
		return refuse(CodeInvalidRecord, id, "template title is required")
	case len(t.Items) == 0:
		return refuse(CodeInvalidRecord, id, "a template must carry at least one item")
	case t.CreatedAt.IsZero():
		return refuse(CodeInvalidRecord, id, "created_at must be supplied; this package never reads a wall clock")
	}
	seen := map[string]bool{}
	for _, ref := range t.RoleRefs {
		if !semanticKey(ref) {
			return refuse(CodeInvalidRecord, id, "role refs may not be blank")
		}
		if seen[ref] {
			return refuse(CodeInvalidRecord, id, "role ref %q is shared twice", ref)
		}
		seen[ref] = true
	}
	for i, item := range t.Items {
		if !item.Kind.Valid() {
			return refuse(CodeInvalidRecord, id, "item %d kind %q is not declared", i, string(item.Kind))
		}
		if !semanticKey(item.Title) {
			return refuse(CodeInvalidRecord, id, "item %d title is required", i)
		}
	}
	return nil
}

// ChecklistInput names a fresh instantiation.
type ChecklistInput struct {
	ChecklistID uuid.UUID
	Title       string
	CreatedAt   time.Time
}

// ChecklistItem is one live step of an instantiated procedure.
type ChecklistItem struct {
	ItemID      uuid.UUID
	ChecklistID uuid.UUID
	Position    int
	Kind        ItemKind
	Title       string
	Detail      string
	Required    bool
	State       StepState
	CompletedBy string
	CompletedAt *time.Time
}

// Checklist is one person's live copy of a template: fresh ids, every item
// open, positions 1..N in template order.
type Checklist struct {
	ChecklistID uuid.UUID
	TenantID    uuid.UUID
	Title       string
	TemplateID  uuid.UUID
	RoleRefs    []string
	Items       []ChecklistItem
	CreatedAt   time.Time
}

// Instantiate copies the template into a live checklist. The template is
// validated first and never mutated; every item starts open.
func (t ChecklistTemplate) Instantiate(in ChecklistInput) (Checklist, error) {
	if err := t.Validate(); err != nil {
		return Checklist{}, err
	}
	id := in.ChecklistID
	if id == uuid.Nil {
		id = uuid.New()
	}
	if !semanticKey(in.Title) {
		return Checklist{}, refuse(CodeInvalidRecord, id.String(), "checklist title is required")
	}
	if in.CreatedAt.IsZero() {
		return Checklist{}, refuse(CodeInvalidRecord, id.String(), "created_at must be supplied; this package never reads a wall clock")
	}
	out := Checklist{
		ChecklistID: id,
		TenantID:    t.TenantID,
		Title:       in.Title,
		TemplateID:  t.TemplateID,
		RoleRefs:    append([]string(nil), t.RoleRefs...),
		CreatedAt:   in.CreatedAt,
	}
	for i, item := range t.Items {
		out.Items = append(out.Items, ChecklistItem{
			ItemID:      uuid.New(),
			ChecklistID: id,
			Position:    i + 1,
			Kind:        item.Kind,
			Title:       item.Title,
			Detail:      item.Detail,
			Required:    item.Required,
			State:       StepStateOpen,
		})
	}
	return out, nil
}

// Validate reports whether the checklist is storable: identity, title and
// template link present, positions exactly 1..N in order, every kind and
// state declared, and completion pairs (actor and timestamp) present iff the
// item is done.
func (c Checklist) Validate() error {
	id := c.ChecklistID.String()
	switch {
	case c.ChecklistID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "checklist id must not be the nil UUID")
	case c.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "tenant id must not be the nil UUID")
	case !semanticKey(c.Title):
		return refuse(CodeInvalidRecord, id, "checklist title is required")
	case c.TemplateID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "a checklist must name the template it instantiates")
	case c.CreatedAt.IsZero():
		return refuse(CodeInvalidRecord, id, "created_at must be supplied; this package never reads a wall clock")
	}
	for i, item := range c.Items {
		if item.ItemID == uuid.Nil {
			return refuse(CodeInvalidRecord, id, "item %d id must not be the nil UUID", i)
		}
		if item.ChecklistID != c.ChecklistID {
			return refuse(CodeInvalidRecord, id, "item %d belongs to another checklist", i)
		}
		if item.Position != i+1 {
			return refuse(CodeInvalidRecord, id, "item %d position = %d, want %d", i, item.Position, i+1)
		}
		if !item.Kind.Valid() {
			return refuse(CodeInvalidRecord, id, "item %d kind %q is not declared", i, string(item.Kind))
		}
		if !semanticKey(item.Title) {
			return refuse(CodeInvalidRecord, id, "item %d title is required", i)
		}
		if !item.State.Valid() {
			return refuse(CodeInvalidRecord, id, "item %d state %q is not declared", i, string(item.State))
		}
		done := item.State == StepStateDone
		complete := item.CompletedBy != "" && item.CompletedAt != nil
		partial := item.CompletedBy != "" || item.CompletedAt != nil
		if partial && !complete {
			return refuse(CodeInvalidRecord, id, "item %d completion actor and timestamp move together or not at all", i)
		}
		if complete != done {
			return refuse(CodeInvalidRecord, id, "item %d completion is present iff state is done", i)
		}
	}
	return nil
}

func (c *Checklist) find(itemID uuid.UUID) (*ChecklistItem, error) {
	for i := range c.Items {
		if c.Items[i].ItemID == itemID {
			return &c.Items[i], nil
		}
	}
	return nil, refuse(CodeWorkItemNotFound, c.ChecklistID.String(), "checklist item %s is not on this checklist", itemID)
}

// CompleteItem marks an open item done. Completing a non-open item or an
// anonymous completion is refused and mutates nothing.
func (c *Checklist) CompleteItem(itemID uuid.UUID, actor string, at time.Time) error {
	item, err := c.find(itemID)
	if err != nil {
		return err
	}
	if item.State != StepStateOpen {
		return refuse(CodeIllegalTransition, c.ChecklistID.String(), "item %s is %q, only an open item completes", itemID, string(item.State))
	}
	if !semanticKey(actor) {
		return refuse(CodeInvalidRecord, c.ChecklistID.String(), "a completion must name its actor")
	}
	if at.IsZero() {
		return refuse(CodeInvalidRecord, c.ChecklistID.String(), "a completion must carry its timestamp")
	}
	item.State = StepStateDone
	item.CompletedBy = actor
	stamp := at
	item.CompletedAt = &stamp
	return nil
}

// SkipItem waives an open, non-required item. Required items cannot be
// skipped: reopen the procedure or complete them.
func (c *Checklist) SkipItem(itemID uuid.UUID, actor string, at time.Time) error {
	item, err := c.find(itemID)
	if err != nil {
		return err
	}
	if item.State != StepStateOpen {
		return refuse(CodeIllegalTransition, c.ChecklistID.String(), "item %s is %q, only an open item skips", itemID, string(item.State))
	}
	if item.Required {
		return refuse(CodeIllegalTransition, c.ChecklistID.String(), "item %s is required and cannot be skipped", itemID)
	}
	if !semanticKey(actor) {
		return refuse(CodeInvalidRecord, c.ChecklistID.String(), "a skip must name its actor")
	}
	if at.IsZero() {
		return refuse(CodeInvalidRecord, c.ChecklistID.String(), "a skip must carry its timestamp")
	}
	item.State = StepStateSkipped
	return nil
}

// ReopenItem returns a done or skipped item to open, clearing its
// completion. Opening an open item is refused.
func (c *Checklist) ReopenItem(itemID uuid.UUID, at time.Time) error {
	item, err := c.find(itemID)
	if err != nil {
		return err
	}
	if item.State == StepStateOpen {
		return refuse(CodeIllegalTransition, c.ChecklistID.String(), "item %s is already open", itemID)
	}
	if at.IsZero() {
		return refuse(CodeInvalidRecord, c.ChecklistID.String(), "a reopen must carry its timestamp")
	}
	item.State = StepStateOpen
	item.CompletedBy = ""
	item.CompletedAt = nil
	return nil
}
