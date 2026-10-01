package productui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const personaApprovalLifetime = 15 * time.Minute

var (
	ErrPersonaApprovalInvalid   = errors.New("productui: invalid persona approval")
	ErrPersonaApprovalDenied    = errors.New("productui: persona approval denied")
	ErrPersonaApprovalExpired   = errors.New("productui: persona approval expired")
	ErrPersonaApprovalStale     = errors.New("productui: persona approval is stale")
	ErrPersonaApprovalReplay    = errors.New("productui: persona approval already submitted")
	ErrPersonaApprovalSuspended = errors.New("productui: persona approval suspended")
)

// PersonaApprovalField is a material before/after value supplied by the
// AGENT2-006 AgentActionApproval projection. Product UI displays it; it does
// not decide what is material or how the value was authorized.
type PersonaApprovalField struct {
	Name   string
	Before string
	After  string
}

// PersonaApprovalCard is the typed CHAT-041 card projection of one exact
// AGENT2-006 item. It is not a second approval model: the server supplies the
// digest, provenance and gate verdict, while this package only renders it and
// carries the callback binding to the approval port.
type PersonaApprovalCard struct {
	ID             string
	InvocationID   string
	InvokerID      string
	ItemDigest     string
	IssuedAt       time.Time
	ExpiresAt      time.Time
	Tier           string
	Summary        string
	Definition     string
	Fields         []PersonaApprovalField
	Sources        []string
	Taint          []string
	Uncertainty    string
	BatchSize      int
	HighRisk       bool
	StepUpRequired bool
	DigestFresh    bool
	OpenTaskHref   string
	Ephemeral      bool
	CardLabel      string
	FieldsLabel    string
	ExpiredLabel   string
	TaskViewLabel  string
	ActionLabel    string
}

// PersonaApprovalExpiry is the server-facing default expiry for a chat card.
// Callers may still project an earlier expiry, but a card cannot be extended
// by the renderer or callback controller.
func PersonaApprovalExpiry(issuedAt time.Time) time.Time {
	return issuedAt.Add(personaApprovalLifetime)
}

// ChatApprovalAllowed identifies the only AGENT2-001 cards that may carry a
// direct chat action. T4, high-risk, batches and step-up items must go to the
// task view, where the product approval surface can perform step-up.
func (c PersonaApprovalCard) ChatApprovalAllowed() bool {
	if c.Tier != "T2" && c.Tier != "T3" {
		return false
	}
	return c.BatchSize <= 1 && !c.HighRisk && !c.StepUpRequired
}

func (c PersonaApprovalCard) validBinding() bool {
	return c.Ephemeral && strings.TrimSpace(c.ID) != "" && strings.TrimSpace(c.InvocationID) != "" &&
		strings.TrimSpace(c.InvokerID) != "" && strings.TrimSpace(c.ItemDigest) != "" &&
		!c.ExpiresAt.IsZero() && !c.IssuedAt.IsZero() && !c.ExpiresAt.Before(c.IssuedAt)
}

// PersonaApprovalDecision is the only input accepted by the callback seam.
// A reaction, reply, or persona-authored message cannot construct a valid
// decision because it has no card binding and is never passed to this port.
type PersonaApprovalDecision struct {
	CardID       string
	InvocationID string
	InvokerID    string
	ItemDigest   string
	ActorID      string
	At           time.Time
}

// PersonaApprovalPort is the narrow adapter to AGENT2-006. The adapter must
// re-run the current authority, grant, digest and freshness checks before its
// one idempotent submission.
type PersonaApprovalPort interface {
	RecheckAndSubmit(context.Context, PersonaApprovalDecision) error
}

// PersonaApprovalController owns callback state for one request composition.
// It never grants authority: it binds the invoker and exact digest, serializes
// approve/suspend, and delegates all policy and effect work to PersonaApprovalPort.
type PersonaApprovalController struct {
	mu        sync.Mutex
	port      PersonaApprovalPort
	now       func() time.Time
	cards     map[string]PersonaApprovalCard
	submitted map[string]struct{}
	suspended map[string]struct{}
}

func NewPersonaApprovalController(port PersonaApprovalPort) *PersonaApprovalController {
	return NewPersonaApprovalControllerWithClock(port, time.Now)
}

func NewPersonaApprovalControllerWithClock(port PersonaApprovalPort, clock func() time.Time) *PersonaApprovalController {
	if clock == nil {
		clock = time.Now
	}
	return &PersonaApprovalController{
		port: port, now: clock, cards: make(map[string]PersonaApprovalCard), submitted: make(map[string]struct{}), suspended: make(map[string]struct{}),
	}
}

func (c *PersonaApprovalController) Register(card PersonaApprovalCard) error {
	if c == nil || !card.validBinding() || card.ExpiresAt.After(PersonaApprovalExpiry(card.IssuedAt)) {
		return ErrPersonaApprovalInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.cards[card.ID]; exists {
		return ErrPersonaApprovalReplay
	}
	c.cards[card.ID] = card
	return nil
}

// Approve is deliberately serialized with Suspend. Holding the lock through
// the adapter call gives concurrent approve/suspend exactly one committed
// outcome and prevents two callbacks from submitting the same digest.
func (c *PersonaApprovalController) Approve(ctx context.Context, decision PersonaApprovalDecision) error {
	if c == nil {
		return ErrPersonaApprovalInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	card, ok := c.cards[decision.CardID]
	if !ok || decision.InvocationID != card.InvocationID || decision.InvokerID != card.InvokerID || decision.ActorID != card.InvokerID || decision.ItemDigest != card.ItemDigest {
		return ErrPersonaApprovalDenied
	}
	if _, ok := c.suspended[card.ID]; ok {
		return ErrPersonaApprovalSuspended
	}
	if _, ok := c.submitted[card.ID]; ok {
		return ErrPersonaApprovalReplay
	}
	observedAt := c.now()
	if observedAt.IsZero() || !observedAt.Before(card.ExpiresAt) {
		return ErrPersonaApprovalExpired
	}
	if !card.DigestFresh {
		return ErrPersonaApprovalStale
	}
	if !card.ChatApprovalAllowed() || c.port == nil {
		return ErrPersonaApprovalDenied
	}
	decision.At = observedAt
	if err := c.port.RecheckAndSubmit(ctx, decision); err != nil {
		return err
	}
	c.submitted[card.ID] = struct{}{}
	return nil
}

func (c *PersonaApprovalController) Suspend(cardID string) error {
	if c == nil || strings.TrimSpace(cardID) == "" {
		return ErrPersonaApprovalInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.cards[cardID]; !ok {
		return ErrPersonaApprovalDenied
	}
	if _, ok := c.submitted[cardID]; ok {
		return ErrPersonaApprovalReplay
	}
	c.suspended[cardID] = struct{}{}
	return nil
}

// RenderPersonaApprovalCard renders only a server-owned, invoker-visible
// ephemeral card. An unavailable or non-invoker projection is an empty node so
// another member cannot infer the existence or contents of the approval.
func RenderPersonaApprovalCard(view View, card PersonaApprovalCard, now time.Time) ui.Node {
	if !card.validBinding() || view.Principal == "" || view.Principal != card.InvokerID {
		return ui.Fragment()
	}
	locale := view.Locale
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	active := card.DigestFresh && !now.IsZero() && now.Before(card.ExpiresAt)
	children := []ui.Node{
		html.H3(html.Props{}, ui.Text(card.Summary)),
		html.Span(html.Props{Class: "status persona-approval-tier"}, ui.Text(card.Tier)),
		html.Code(html.Props{Class: "persona-approval-digest"}, ui.Text(card.ItemDigest)),
	}
	if card.Definition != "" {
		children = append(children, html.P(html.Props{Class: "muted persona-approval-definition"}, ui.Text(card.Definition)))
	}
	if len(card.Fields) > 0 {
		fields := make([]ui.Node, 0, len(card.Fields))
		for _, field := range card.Fields {
			fields = append(fields, html.Li(html.Props{}, html.Strong(html.Props{}, ui.Text(field.Name)), html.Span(html.Props{Class: "persona-approval-before"}, ui.Text(field.Before)), html.Span(html.Props{Class: "persona-approval-after"}, ui.Text(field.After))))
		}
		fieldsLabel := card.FieldsLabel
		if fieldsLabel == "" {
			fieldsLabel = locale.Text("agents.approvals")
		}
		children = append(children, html.Ul(html.Props{Class: "persona-approval-fields", Aria: map[string]string{"label": fieldsLabel}}, fields...))
	}
	if len(card.Sources) > 0 {
		sources := make([]ui.Node, 0, len(card.Sources))
		for _, source := range card.Sources {
			sources = append(sources, html.Li(html.Props{}, ui.Text(source)))
		}
		children = append(children, html.Details(html.Props{Class: "persona-approval-sources"}, html.Summary(html.Props{}, ui.Text(locale.Text("agents.sources"))), html.Ul(html.Props{}, sources...)))
	}
	if len(card.Taint) > 0 {
		children = append(children, html.P(html.Props{Class: "status persona-approval-taint"}, ui.Text(strings.Join(card.Taint, ", "))))
	}
	if !active {
		expiredLabel := card.ExpiredLabel
		if expiredLabel == "" {
			expiredLabel = locale.Text("agents.unavailable_detail")
		}
		children = append(children, html.P(html.Props{Class: "status persona-approval-unavailable", Role: "status"}, ui.Text(expiredLabel)))
		return personaApprovalCardShell(view, card, children, false)
	}
	if !card.ChatApprovalAllowed() {
		taskViewLabel := card.TaskViewLabel
		if taskViewLabel == "" {
			taskViewLabel = locale.Text("agents.open_task")
		}
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(taskViewLabel)))
		return personaApprovalCardShell(view, card, children, true)
	}
	label := card.ActionLabel
	if label == "" {
		label = locale.Text("agents.open_task")
	}
	children = append(children, html.Button(html.Props{Class: "button primary persona-approval-action", Type: "button", Aria: map[string]string{"label": label}, Data: map[string]string{
		"agent-approval-action": "approve", "agent-approval-id": card.ID, "agent-invocation-id": card.InvocationID, "agent-invoker-id": card.InvokerID, "agent-item-digest": card.ItemDigest,
	}}, ui.Text(label)))
	return personaApprovalCardShell(view, card, children, true)
}

func personaApprovalCardShell(view View, card PersonaApprovalCard, children []ui.Node, active bool) ui.Node {
	label := card.CardLabel
	if label == "" {
		label = view.Locale.Text("agents.approvals")
	}
	props := html.Props{Class: "persona-chat-approval-card", Role: "region", Aria: map[string]string{"label": label}, Data: map[string]string{
		"agent-card-kind": "action-approval", "agent-delivery": "ephemeral", "agent-visibility": "invoker", "agent-approval-id": card.ID, "agent-invocation-id": card.InvocationID, "agent-invoker-id": card.InvokerID, "agent-item-digest": card.ItemDigest,
	}}
	if !active || !card.ChatApprovalAllowed() {
		if card.OpenTaskHref != "" {
			children = append(children, html.A(html.Props{Class: "button secondary persona-open-task", Href: card.OpenTaskHref, Data: map[string]string{"agent-task-link": card.OpenTaskHref}}, ui.Text(view.Locale.Text("agents.open_task"))))
		}
	}
	return html.Article(props, children...)
}
