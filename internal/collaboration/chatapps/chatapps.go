// Package chatapps owns the capability boundary for installed conversation
// apps and visible agents. It deliberately has no transport or database
// dependency. A durable repository is supplied by the application boundary.
package chatapps

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalid   = errors.New("chatapps: invalid request")
	ErrDenied    = errors.New("chatapps: denied")
	ErrNotFound  = errors.New("chatapps: not found")
	ErrSuspended = errors.New("chatapps: suspended")
	ErrRevoked   = errors.New("chatapps: revoked")
	ErrReplay    = errors.New("chatapps: replay")
	ErrLoop      = errors.New("chatapps: loop stopped")
	ErrBudget    = errors.New("chatapps: budget exceeded")
	ErrUnsafeURL = errors.New("chatapps: unsafe callback URL")
)

type Status string

const (
	Active    Status = "ACTIVE"
	Suspended Status = "SUSPENDED"
	Revoked   Status = "REVOKED"
)

// Manifest is immutable for one app version. Scope strings are capability
// names, never HCM authorities by themselves.
type Manifest struct {
	AppID           string         `json:"app_id"`
	Version         uint64         `json:"version"`
	Scopes          []string       `json:"scopes"`
	Commands        []Command      `json:"commands"`
	Cards           []CardType     `json:"cards"`
	Tabs            []TabType      `json:"tabs"`
	CallbackOrigins []string       `json:"callback_origins"`
	Agent           *AgentManifest `json:"agent,omitempty"`
}
type Command struct {
	Name      string
	Scope     string
	Arguments []Argument
	Risk      string
}
type Argument struct {
	Name     string
	Type     string
	Required bool
}
type CardType struct {
	Kind   string
	Fields []Field
}
type TabType struct {
	Kind  string
	Title string
}
type Field struct {
	Name     string
	Type     string
	Required bool
}
type AgentManifest struct {
	DisplayName   string
	Description   string
	Triggers      []TriggerSource
	MaxDepth      int
	RatePerMinute int
	CostCeiling   int64
}
type TriggerSource string

const (
	TriggerMention       TriggerSource = "MENTION"
	TriggerDirectMessage TriggerSource = "DIRECT_MESSAGE"
	TriggerEvent         TriggerSource = "EVENT"
)

type Actor struct {
	Tenant, Principal, Conversation string
	Scopes                          []string
}

// Authority is supplied by the identity/conversation boundary. Apps never
// infer installer or manager authority from a principal string.
type Authority interface {
	CanManageApp(context.Context, Actor, string) error
	CanUseConversation(context.Context, Actor, string) error
}
type Installation struct {
	ID            string    `json:"id"`
	Tenant        string    `json:"tenant"`
	Conversation  string    `json:"conversation"`
	AppID         string    `json:"app_id"`
	Version       uint64    `json:"version"`
	Manifest      Manifest  `json:"manifest"`
	GrantedScopes []string  `json:"granted_scopes"`
	Status        Status    `json:"status"`
	Approver      string    `json:"approver"`
	Revision      uint64    `json:"revision"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (i Installation) Current(now time.Time) bool {
	return i.Status == Active && i.Version > 0 && !i.CreatedAt.After(now)
}

type Repository interface {
	Get(context.Context, string) (Installation, error)
	Put(context.Context, Installation) error
	ByConversation(context.Context, string, string) ([]Installation, error)
	Seen(context.Context, string) (bool, error)
	MarkSeen(context.Context, string, time.Time) error
	AppendEvent(context.Context, Event) error
	Events(context.Context, string, uint64, int) ([]Event, error)
}

// AuditedRepository commits an installation and its governance projection together.
type AuditedRepository interface {
	PutAudited(context.Context, Installation, Actor, string, string, uint64) error
}

type auditContextKey struct{}
type auditContext struct {
	home, tenant, conversation, principal string
	policyRevision                        uint64
}

// WithAudit binds the live application authorization to a durable write.
func WithAudit(ctx context.Context, actor Actor, home string, policyRevision uint64) context.Context {
	return context.WithValue(ctx, auditContextKey{}, auditContext{home, actor.Tenant, actor.Conversation, actor.Principal, policyRevision})
}

func (s *Service) put(ctx context.Context, v Installation, actor Actor, action string) error {
	if r, ok := s.Repo.(AuditedRepository); ok {
		a, ok := ctx.Value(auditContextKey{}).(auditContext)
		if !ok || a.policyRevision == 0 || a.home == "" || actor.Principal == "" || actor.Tenant != v.Tenant || actor.Conversation != v.Conversation || a.tenant != v.Tenant || a.conversation != v.Conversation || a.principal != actor.Principal {
			return ErrDenied
		}
		return r.PutAudited(ctx, v, actor, a.home, action, a.policyRevision)
	}
	return s.Repo.Put(ctx, v)
}

// EventRecorder atomically appends an event and records its delivery key.
// Durable adapters implement this in one transaction with a unique key.
type EventRecorder interface {
	AppendOnce(context.Context, Event, time.Time) error
}

// MemoryRepository is a deterministic contract adapter. Production wiring
// supplies a chat-database repository implementing the same interface.
type MemoryRepository struct {
	mu       sync.Mutex
	installs map[string]Installation
	seen     map[string]time.Time
	events   map[string][]Event
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{installs: map[string]Installation{}, seen: map[string]time.Time{}, events: map[string][]Event{}}
}
func (r *MemoryRepository) Get(_ context.Context, id string) (Installation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.installs[id]
	if !ok {
		return Installation{}, ErrNotFound
	}
	return v, nil
}
func (r *MemoryRepository) Put(_ context.Context, v Installation) error {
	if v.ID == "" {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.installs[v.ID] = v
	return nil
}
func (r *MemoryRepository) ByConversation(_ context.Context, tenant, conv string) ([]Installation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Installation{}
	for _, v := range r.installs {
		if v.Tenant == tenant && v.Conversation == conv {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (r *MemoryRepository) Seen(_ context.Context, id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.seen[id]
	return ok, nil
}
func (r *MemoryRepository) MarkSeen(_ context.Context, id string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.seen[id]; ok {
		return ErrReplay
	}
	r.seen[id] = at
	return nil
}
func (r *MemoryRepository) AppendEvent(_ context.Context, e Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[e.Conversation] = append(r.events[e.Conversation], e)
	return nil
}
func (r *MemoryRepository) AppendOnce(_ context.Context, e Event, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.seen[e.ID]; ok {
		return ErrReplay
	}
	r.events[e.Conversation] = append(r.events[e.Conversation], e)
	r.seen[e.ID] = at
	return nil
}
func (r *MemoryRepository) Events(_ context.Context, conv string, after uint64, limit int) ([]Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	out := []Event{}
	for _, e := range r.events[conv] {
		if e.Sequence > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

type Service struct {
	Repo      Repository
	Secret    []byte
	Now       func() time.Time
	Callback  CallbackClient
	Intent    BusinessIntentPort
	Authority Authority
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}
func (s *Service) Install(ctx context.Context, a Actor, m Manifest, scopes []string, approver string) (Installation, error) {
	if err := validateManifest(m); err != nil || a.Tenant == "" || a.Conversation == "" || approver == "" {
		return Installation{}, ErrInvalid
	}
	if s.Authority == nil {
		return Installation{}, ErrDenied
	}
	if err := s.Authority.CanManageApp(ctx, a, m.AppID); err != nil {
		return Installation{}, ErrDenied
	}
	if err := s.Authority.CanUseConversation(ctx, a, a.Conversation); err != nil {
		return Installation{}, ErrDenied
	}
	if !subset(scopes, m.Scopes) {
		return Installation{}, ErrDenied
	}
	if err := validateOrigins(m.CallbackOrigins); err != nil {
		return Installation{}, err
	}
	id := a.Tenant + ":" + a.Conversation + ":" + m.AppID
	now := s.now()
	old, err := s.Repo.Get(ctx, id)
	if err == nil {
		if old.Status == Revoked {
			return Installation{}, ErrRevoked
		}
		if m.Version <= old.Version {
			return Installation{}, ErrInvalid
		}
	}
	v := Installation{ID: id, Tenant: a.Tenant, Conversation: a.Conversation, AppID: m.AppID, Version: m.Version, Manifest: m, GrantedScopes: sorted(scopes), Status: Active, Approver: approver, Revision: old.Revision + 1, CreatedAt: now, UpdatedAt: now}
	if old.CreatedAt.IsZero() {
		v.Revision = 1
	}
	return v, s.put(ctx, v, a, "chat.app.install")
}
func (s *Service) Upgrade(ctx context.Context, a Actor, m Manifest, scopes []string, approver string) (Installation, error) {
	return s.Install(ctx, a, m, scopes, approver)
}
func (s *Service) ChangeStatus(ctx context.Context, a Actor, id string, status Status) (Installation, error) {
	v, err := s.Repo.Get(ctx, id)
	if err != nil {
		return Installation{}, err
	}
	if v.Tenant != a.Tenant || v.Conversation != a.Conversation || a.Principal == "" {
		return Installation{}, ErrDenied
	}
	if s.Authority == nil {
		return Installation{}, ErrDenied
	}
	if err := s.Authority.CanManageApp(ctx, a, v.AppID); err != nil {
		return Installation{}, ErrDenied
	}
	if err := s.Authority.CanUseConversation(ctx, a, v.Conversation); err != nil {
		return Installation{}, ErrDenied
	}
	if status != Suspended && status != Revoked && status != Active {
		return Installation{}, ErrInvalid
	}
	if v.Status == Revoked && status != Revoked {
		return Installation{}, ErrRevoked
	}
	v.Status = status
	v.Revision++
	v.UpdatedAt = s.now()
	return v, s.put(ctx, v, a, "chat.app.status")
}
func (s *Service) EffectiveScopes(ctx context.Context, a Actor, installID, command string) ([]string, error) {
	v, err := s.Repo.Get(ctx, installID)
	if err != nil {
		return nil, err
	}
	if v.Tenant != a.Tenant || v.Conversation != a.Conversation || v.Status != Active {
		return nil, ErrDenied
	}
	var c *Command
	for i := range v.Manifest.Commands {
		if v.Manifest.Commands[i].Name == command {
			c = &v.Manifest.Commands[i]
			break
		}
	}
	if c == nil {
		return nil, ErrDenied
	}
	if !contains(a.Scopes, c.Scope) || !contains(v.GrantedScopes, c.Scope) {
		return nil, ErrDenied
	}
	return []string{c.Scope}, nil
}

type Card struct {
	Kind   string
	Values map[string]string
}
type Tab struct{ Kind, Title string }
type Callback struct {
	InstallationID, Command, IdempotencyKey string
	Actor                                   Actor
	Card                                    *Card
	Args                                    map[string]string
}
type CallbackResult struct {
	Accepted   bool
	Message    string
	ProposalID string
}
type CallbackClient interface {
	Call(context.Context, Installation, Callback) (CallbackResult, error)
}

func (s *Service) Invoke(ctx context.Context, a Actor, id string, cb Callback) (CallbackResult, error) {
	if cb.IdempotencyKey == "" || cb.Command == "" {
		return CallbackResult{}, ErrInvalid
	}
	if cb.Actor.Principal != "" && (cb.Actor.Principal != a.Principal || cb.Actor.Tenant != a.Tenant || cb.Actor.Conversation != a.Conversation) {
		return CallbackResult{}, ErrDenied
	}
	cb.Actor = a
	if cb.InstallationID != "" && cb.InstallationID != id {
		return CallbackResult{}, ErrDenied
	}
	cb.InstallationID = id
	if s.Authority == nil || s.Authority.CanUseConversation(ctx, a, a.Conversation) != nil {
		return CallbackResult{}, ErrDenied
	}
	if _, err := s.EffectiveScopes(ctx, a, id, cb.Command); err != nil {
		return CallbackResult{}, err
	}
	v, err := s.Repo.Get(ctx, id)
	if err != nil {
		return CallbackResult{}, err
	}
	if cb.Card != nil && !validCard(v.Manifest, *cb.Card) {
		return CallbackResult{}, ErrInvalid
	}
	if s.Callback == nil {
		return CallbackResult{}, ErrDenied
	}
	return s.Callback.Call(ctx, v, cb)
}
func validCard(m Manifest, c Card) bool {
	for _, k := range m.Cards {
		if k.Kind == c.Kind {
			for _, f := range k.Fields {
				if f.Required && c.Values[f.Name] == "" {
					return false
				}
			}
			return true
		}
	}
	return false
}

type Event struct {
	ID, Tenant, Conversation, InstallationID, Type string
	Sequence                                       uint64
	Payload                                        json.RawMessage
	At                                             time.Time
	ExpiresAt                                      time.Time
	Signature                                      string
}

func (s *Service) SignEvent(e Event) string {
	b, _ := json.Marshal(struct {
		ID, Tenant, Conversation, InstallationID, Type string
		Sequence                                       uint64
		Payload                                        json.RawMessage
		ExpiresAt                                      time.Time
	}{e.ID, e.Tenant, e.Conversation, e.InstallationID, e.Type, e.Sequence, e.Payload, e.ExpiresAt})
	h := hmac.New(sha256.New, s.Secret)
	h.Write(b)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Service) Deliver(ctx context.Context, e Event) error {
	if e.ID == "" || e.InstallationID == "" || e.Conversation == "" {
		return ErrInvalid
	}
	v, err := s.Repo.Get(ctx, e.InstallationID)
	if err != nil {
		return err
	}
	if v.Status != Active {
		return ErrRevoked
	}
	if v.Conversation != e.Conversation || v.Tenant == "" {
		return ErrDenied
	}
	if e.Tenant != v.Tenant || (!e.ExpiresAt.IsZero() && !e.ExpiresAt.After(s.now())) {
		return ErrDenied
	}
	if subtle.ConstantTimeCompare([]byte(e.Signature), []byte(s.SignEvent(e))) != 1 {
		return ErrDenied
	}
	if recorder, ok := s.Repo.(EventRecorder); ok {
		return recorder.AppendOnce(ctx, e, s.now())
	}
	return ErrDenied
	/*seen, err := s.Repo.Seen(ctx, e.ID)
	if err != nil {
		return err
	}
	if seen {
		return ErrReplay
	}
	if err = s.Repo.AppendEvent(ctx, e); err != nil {
		return err
	}
	// A durable implementation should make this mark and the append one
	// transaction. Keeping the mark after append preserves at-least-once
	// recovery if an adapter cannot provide that transaction.
	return s.Repo.MarkSeen(ctx, e.ID, s.now())*/
}

type Cursor struct {
	Tenant, Principal, Conversation, Installation string
	After, Expires                                int64
}

func (s *Service) IssueCursor(c Cursor) (string, error) {
	if c.Tenant == "" || c.Principal == "" || c.Conversation == "" || c.Expires <= s.now().Unix() {
		return "", ErrInvalid
	}
	b, _ := json.Marshal(c)
	h := hmac.New(sha256.New, s.Secret)
	h.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil)), nil
}
func (s *Service) Pull(ctx context.Context, a Actor, token string, limit int) ([]Event, string, error) {
	if s.Authority == nil || s.Authority.CanUseConversation(ctx, a, a.Conversation) != nil {
		return nil, "", ErrDenied
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, "", ErrDenied
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, "", ErrDenied
	}
	h := hmac.New(sha256.New, s.Secret)
	h.Write(b)
	sig, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if subtle.ConstantTimeCompare(h.Sum(nil), sig) != 1 {
		return nil, "", ErrDenied
	}
	var c Cursor
	if json.Unmarshal(b, &c) != nil || c.Tenant != a.Tenant || c.Principal != a.Principal || c.Conversation != a.Conversation || c.Expires <= s.now().Unix() {
		return nil, "", ErrDenied
	}
	if c.Installation != "" {
		v, e := s.Repo.Get(ctx, c.Installation)
		if e != nil || v.Status != Active || v.Tenant != c.Tenant || v.Conversation != c.Conversation {
			return nil, "", ErrRevoked
		}
	}
	events, err := s.Repo.Events(ctx, c.Conversation, uint64(c.After), limit)
	if err != nil {
		return nil, "", err
	}
	authorized := make([]Event, 0, len(events))
	for _, e := range events {
		if c.Installation != "" && e.InstallationID != c.Installation {
			continue
		}
		if e.InstallationID != "" {
			v, x := s.Repo.Get(ctx, e.InstallationID)
			if x != nil || v.Status != Active || v.Tenant != c.Tenant || v.Conversation != c.Conversation {
				continue
			}
		}
		authorized = append(authorized, e)
	}
	if len(authorized) > 0 {
		c.After = int64(authorized[len(authorized)-1].Sequence)
	}
	next, _ := s.IssueCursor(c)
	return authorized, next, nil
}

type Agent struct {
	ID, DisplayName, Description string
	InstallationID               string
	Status                       Status
	Capabilities                 []string
}

func (s *Service) Agent(ctx context.Context, id string) (Agent, error) {
	v, e := s.Repo.Get(ctx, id)
	if e != nil {
		return Agent{}, e
	}
	if v.Manifest.Agent == nil {
		return Agent{}, ErrNotFound
	}
	return Agent{ID: v.AppID, DisplayName: v.Manifest.Agent.DisplayName, Description: v.Manifest.Agent.Description, InstallationID: v.ID, Status: v.Status, Capabilities: append([]string(nil), v.GrantedScopes...)}, nil
}

type Trigger struct {
	Tenant, AgentInstallation, Conversation, Source string
	Depth                                           int
	Cost                                            int64
	IdempotencyKey                                  string
}

func (s *Service) AdmitTrigger(ctx context.Context, t Trigger) error {
	if t.Depth < 0 || t.IdempotencyKey == "" || t.Conversation == "" {
		return ErrInvalid
	}
	v, e := s.Repo.Get(ctx, t.AgentInstallation)
	if e != nil {
		return e
	}
	if v.Status != Active || v.Manifest.Agent == nil {
		return ErrDenied
	}
	if t.Tenant == "" || v.Tenant != t.Tenant || v.Conversation != t.Conversation {
		return ErrDenied
	}
	a := v.Manifest.Agent
	if t.Depth >= a.MaxDepth && a.MaxDepth > 0 {
		return ErrLoop
	}
	if a.CostCeiling > 0 && t.Cost > a.CostCeiling {
		return ErrBudget
	}
	ok := false
	for _, x := range a.Triggers {
		if string(x) == t.Source {
			ok = true
		}
	}
	if !ok {
		return ErrDenied
	}
	return nil
}

type BusinessIntentPort interface {
	Propose(context.Context, Proposal) (ProposalReceipt, error)
}
type Proposal struct {
	Tenant, Principal, Conversation, AgentInstallation, IntentType, IdempotencyKey string
	Arguments                                                                      map[string]string
	Evidence                                                                       []string
}
type ProposalReceipt struct{ IntentID, Status string }

func (s *Service) ProposeIntent(ctx context.Context, a Actor, p Proposal) (ProposalReceipt, error) {
	if s.Intent == nil || s.Authority == nil || s.Authority.CanUseConversation(ctx, a, a.Conversation) != nil || p.Tenant != a.Tenant || p.Principal != a.Principal || p.Conversation != a.Conversation || p.IdempotencyKey == "" {
		return ProposalReceipt{}, ErrDenied
	}
	return s.Intent.Propose(ctx, p)
}

func validateManifest(m Manifest) error {
	if strings.TrimSpace(m.AppID) == "" || m.Version == 0 {
		return ErrInvalid
	}
	for _, c := range m.Commands {
		if c.Name == "" || c.Scope == "" {
			return ErrInvalid
		}
	}
	return nil
}
func validateOrigins(xs []string) error {
	for _, raw := range xs {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Host == "" {
			return ErrUnsafeURL
		}
		h := u.Hostname()
		if strings.EqualFold(h, "localhost") || net.ParseIP(h) != nil && isPrivate(net.ParseIP(h)) {
			return ErrUnsafeURL
		}
	}
	return nil
}

// ResolveOrigin performs the runtime DNS check that must precede an outbound
// callback. Checking only the URL literal is insufficient because a public
// hostname can resolve to loopback or RFC1918 space (and can change between
// requests). Callback clients must disable redirects and repeat this check
// for every redirect target before dialing it.
type OriginResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

func ValidateResolvedOrigin(ctx context.Context, raw string, resolver OriginResolver) error {
	if err := validateOrigins([]string{raw}); err != nil {
		return err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrUnsafeURL
	}
	if resolver == nil {
		return ErrUnsafeURL
	}
	ips, err := resolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil || len(ips) == 0 {
		return ErrUnsafeURL
	}
	for _, ip := range ips {
		if isPrivate(ip.IP) {
			return ErrUnsafeURL
		}
	}
	return nil
}
func isPrivate(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}
func subset(a, b []string) bool {
	for _, x := range a {
		if !contains(b, x) {
			return false
		}
	}
	return true
}
func contains(a []string, x string) bool {
	for _, v := range a {
		if v == x {
			return true
		}
	}
	return false
}
func sorted(a []string) []string { r := append([]string(nil), a...); sort.Strings(r); return r }
