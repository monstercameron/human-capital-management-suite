// Package projectactivity owns bounded, append-oriented project task comments
// and their audit activity. Persistence and current access decisions are ports.
package projectactivity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	DefaultPageSize   = 25
	MaxPageSize       = 100
	MaxTextBytes      = 16 * 1024
	MaxMentionHandles = 32
)

var (
	ErrInvalidRequest = errors.New("projectactivity: invalid request")
	ErrNotFound       = errors.New("projectactivity: comment not found")
	ErrConflict       = errors.New("projectactivity: revision conflict")
	ErrDenied         = errors.New("projectactivity: access denied")
	ErrCursor         = errors.New("projectactivity: invalid cursor")
	ErrPortMissing    = errors.New("projectactivity: required port is missing")
)

type Action string

const (
	ActionRead    Action = "READ_TASK"
	ActionComment Action = "COMMENT_TASK"
	ActionCorrect Action = "CORRECT_COMMENT"
)

type Principal struct{ TenantID, SubjectID string }

type AccessRequest struct {
	Principal         Principal
	ProjectID, TaskID string
	Action            Action
}

// Authorizer must evaluate current project and task access for every call.
type Authorizer interface {
	Authorize(context.Context, AccessRequest) error
}

// TextSanitizer parses bounded rich text and returns an inert rendering.
// Implementations must reject active markup and preserve only approved tags.
type TextSanitizer interface {
	Sanitize(context.Context, string) (SafeText, error)
}

// SafeText is a rendering-safe result from TextSanitizer. Callers must not
// construct it from untrusted input.
type SafeText struct {
	Source, HTML   string
	MentionHandles []string
	Mentions       []Mention
}

// Mention is a safe reference to a member visible to the requesting actor.
type Mention struct{ SubjectID, DisplayName string }

// MentionResolver returns only currently visible project members. Unknown and
// inaccessible handles must be omitted identically to avoid enumeration.
type MentionResolver interface {
	ResolveVisibleMentions(context.Context, AccessRequest, []string) ([]Mention, error)
}

type Request struct {
	Principal        Principal
	ProjectID        string
	TaskID           string
	CommentID        string
	Text             string
	ExpectedRevision uint64
	At               time.Time
}

type Revision struct {
	Number    uint64
	Text      SafeText
	Tombstone bool
	ActorID   string
	At        time.Time
}

type Comment struct {
	TenantID, ProjectID, TaskID, ID string
	CreatedAt                       time.Time
	Revisions                       []Revision
}

func (c Comment) Current() Revision {
	if len(c.Revisions) == 0 {
		return Revision{}
	}
	return c.Revisions[len(c.Revisions)-1]
}

type Activity struct {
	Sequence                                        uint64
	TenantID, ProjectID, TaskID, CommentID, ActorID string
	Kind                                            string
	Revision                                        uint64
	At                                              time.Time
}

type PageRequest struct {
	Principal                 Principal
	ProjectID, TaskID, Cursor string
	PageSize                  int
}
type Page struct {
	Comments   []Comment
	Activity   []Activity
	NextCursor string
}

// Store atomically appends a comment revision and its matching activity row.
// Appended versions are immutable; implementations must retain earlier text.
type Store interface {
	Create(context.Context, Comment, Activity) error
	Correct(context.Context, string, string, string, string, uint64, Revision, Activity) (Comment, error)
	Page(context.Context, string, string, string, uint64, uint64, int) ([]Comment, []Activity, uint64, uint64, bool, error)
}

// TimelineStore supplies the merged immutable task mutation and comment stream.
type TimelineStore interface {
	TimelinePage(context.Context, string, string, string, uint64, uint64, int) ([]Activity, uint64, uint64, bool, error)
}

type Service struct {
	Store      Store
	Authorizer Authorizer
	Sanitizer  TextSanitizer
	Mentions   MentionResolver
	CursorKey  []byte
}

func (s Service) validate() error {
	if s.Store == nil || s.Authorizer == nil || s.Sanitizer == nil || len(s.CursorKey) < 16 {
		return ErrPortMissing
	}
	return nil
}

func (s Service) Add(ctx context.Context, req Request) (Comment, error) {
	if err := s.validate(); err != nil {
		return Comment{}, err
	}
	if err := validateRequest(req, ActionComment); err != nil {
		return Comment{}, err
	}
	if err := s.Authorizer.Authorize(ctx, AccessRequest{req.Principal, req.ProjectID, req.TaskID, ActionComment}); err != nil {
		return Comment{}, fmt.Errorf("%w: %v", ErrDenied, err)
	}
	text, err := s.safeText(ctx, req, req.Text)
	if err != nil {
		return Comment{}, err
	}
	comment := Comment{TenantID: req.Principal.TenantID, ProjectID: req.ProjectID, TaskID: req.TaskID, ID: req.CommentID, CreatedAt: req.At.UTC(), Revisions: []Revision{{Number: 1, Text: text, ActorID: req.Principal.SubjectID, At: req.At.UTC()}}}
	activity := makeActivity(req, req.CommentID, "COMMENT_CREATED", 1)
	if err := s.Store.Create(ctx, comment, activity); err != nil {
		return Comment{}, err
	}
	return cloneComment(comment), nil
}

func (s Service) Correct(ctx context.Context, req Request) (Comment, error) {
	if err := s.validate(); err != nil {
		return Comment{}, err
	}
	if err := validateRequest(req, ActionCorrect); err != nil {
		return Comment{}, err
	}
	if err := s.Authorizer.Authorize(ctx, AccessRequest{req.Principal, req.ProjectID, req.TaskID, ActionCorrect}); err != nil {
		return Comment{}, fmt.Errorf("%w: %v", ErrDenied, err)
	}
	text, err := s.safeText(ctx, req, req.Text)
	if err != nil {
		return Comment{}, err
	}
	revision := Revision{Number: req.ExpectedRevision + 1, Text: text, ActorID: req.Principal.SubjectID, At: req.At.UTC()}
	return s.correct(ctx, req, revision, "COMMENT_CORRECTED")
}

func (s Service) Tombstone(ctx context.Context, req Request) (Comment, error) {
	if err := s.validate(); err != nil {
		return Comment{}, err
	}
	if err := validateRequest(req, ActionCorrect); err != nil {
		return Comment{}, err
	}
	if err := s.Authorizer.Authorize(ctx, AccessRequest{req.Principal, req.ProjectID, req.TaskID, ActionCorrect}); err != nil {
		return Comment{}, fmt.Errorf("%w: %v", ErrDenied, err)
	}
	revision := Revision{Number: req.ExpectedRevision + 1, Tombstone: true, ActorID: req.Principal.SubjectID, At: req.At.UTC()}
	return s.correct(ctx, req, revision, "COMMENT_TOMBSTONED")
}

func (s Service) correct(ctx context.Context, req Request, revision Revision, kind string) (Comment, error) {
	activity := makeActivity(req, req.CommentID, kind, revision.Number)
	return s.Store.Correct(ctx, req.Principal.TenantID, req.ProjectID, req.TaskID, req.CommentID, req.ExpectedRevision, revision, activity)
}

func (s Service) List(ctx context.Context, req PageRequest) (Page, error) {
	if err := s.validate(); err != nil {
		return Page{}, err
	}
	if req.Principal.TenantID == "" || req.Principal.SubjectID == "" || req.ProjectID == "" || req.TaskID == "" {
		return Page{}, ErrInvalidRequest
	}
	size := req.PageSize
	if size == 0 {
		size = DefaultPageSize
	}
	if size < 1 || size > MaxPageSize {
		return Page{}, ErrInvalidRequest
	}
	// Access is deliberately decided before cursor decoding or storage reads.
	if err := s.Authorizer.Authorize(ctx, AccessRequest{req.Principal, req.ProjectID, req.TaskID, ActionRead}); err != nil {
		return Page{}, fmt.Errorf("%w: %v", ErrDenied, err)
	}
	after, ceiling, err := s.decodeCursor(req.Cursor, req.Principal.TenantID, req.ProjectID, req.TaskID)
	if err != nil {
		return Page{}, err
	}
	comments, activity, last, snapshot, more, err := s.Store.Page(ctx, req.Principal.TenantID, req.ProjectID, req.TaskID, after, ceiling, size)
	if err != nil {
		return Page{}, err
	}
	if req.Cursor == "" {
		ceiling = snapshot
	}
	var next string
	if more {
		next = s.encodeCursor(last, ceiling, req.Principal.TenantID, req.ProjectID, req.TaskID)
	}
	return Page{Comments: cloneComments(comments), Activity: append([]Activity(nil), activity...), NextCursor: next}, nil
}

// Timeline returns the bounded merged stream. Authorization runs before cursor
// parsing and storage access, and cursor ceilings freeze each traversal.
func (s Service) Timeline(ctx context.Context, req PageRequest) ([]Activity, string, error) {
	if err := s.validate(); err != nil {
		return nil, "", err
	}
	if req.Principal.TenantID == "" || req.Principal.SubjectID == "" || req.ProjectID == "" || req.TaskID == "" {
		return nil, "", ErrInvalidRequest
	}
	size := req.PageSize
	if size == 0 {
		size = DefaultPageSize
	}
	if size < 1 || size > MaxPageSize {
		return nil, "", ErrInvalidRequest
	}
	if err := s.Authorizer.Authorize(ctx, AccessRequest{req.Principal, req.ProjectID, req.TaskID, ActionRead}); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrDenied, err)
	}
	store, ok := s.Store.(TimelineStore)
	if !ok {
		return nil, "", ErrPortMissing
	}
	after, ceiling, err := s.decodeCursor(req.Cursor, req.Principal.TenantID, req.ProjectID, req.TaskID)
	if err != nil {
		return nil, "", err
	}
	entries, last, snapshot, more, err := store.TimelinePage(ctx, req.Principal.TenantID, req.ProjectID, req.TaskID, after, ceiling, size)
	if err != nil {
		return nil, "", err
	}
	if req.Cursor == "" {
		ceiling = snapshot
	}
	var next string
	if more {
		next = s.encodeCursor(last, ceiling, req.Principal.TenantID, req.ProjectID, req.TaskID)
	}
	return entries, next, nil
}

func (s Service) safeText(ctx context.Context, req Request, raw string) (SafeText, error) {
	if len(raw) == 0 || len(raw) > MaxTextBytes || strings.TrimSpace(raw) == "" {
		return SafeText{}, ErrInvalidRequest
	}
	text, err := s.Sanitizer.Sanitize(ctx, raw)
	if err != nil {
		return SafeText{}, fmt.Errorf("projectactivity: sanitize text: %w", err)
	}
	if text.Source == "" || text.HTML == "" || len(text.Source) > MaxTextBytes || len(text.HTML) > MaxTextBytes {
		return SafeText{}, ErrInvalidRequest
	}
	if len(text.MentionHandles) > MaxMentionHandles {
		return SafeText{}, ErrInvalidRequest
	}
	if len(text.MentionHandles) > 0 {
		if s.Mentions == nil {
			return SafeText{}, ErrPortMissing
		}
		access := AccessRequest{req.Principal, req.ProjectID, req.TaskID, ActionRead}
		mentions, resolveErr := s.Mentions.ResolveVisibleMentions(ctx, access, append([]string(nil), text.MentionHandles...))
		if resolveErr != nil {
			return SafeText{}, fmt.Errorf("projectactivity: resolve mentions: %w", resolveErr)
		}
		text.Mentions = append([]Mention(nil), mentions...)
	}
	return text, nil
}

func validateRequest(req Request, action Action) error {
	if req.Principal.TenantID == "" || req.Principal.SubjectID == "" || req.ProjectID == "" || req.TaskID == "" || req.CommentID == "" || req.At.IsZero() {
		return ErrInvalidRequest
	}
	if action == ActionComment && req.ExpectedRevision != 0 {
		return ErrInvalidRequest
	}
	if action == ActionCorrect && req.ExpectedRevision == 0 {
		return ErrInvalidRequest
	}
	return nil
}

func makeActivity(req Request, commentID, kind string, revision uint64) Activity {
	return Activity{TenantID: req.Principal.TenantID, ProjectID: req.ProjectID, TaskID: req.TaskID, CommentID: commentID, ActorID: req.Principal.SubjectID, Kind: kind, Revision: revision, At: req.At.UTC()}
}

func (s Service) encodeCursor(after, ceiling uint64, tenant, project, task string) string {
	payload := make([]byte, 16)
	binary.BigEndian.PutUint64(payload, after)
	binary.BigEndian.PutUint64(payload[8:], ceiling)
	mac := hmac.New(sha256.New, s.CursorKey)
	mac.Write([]byte(tenant))
	mac.Write([]byte{0})
	mac.Write([]byte(project))
	mac.Write([]byte{0})
	mac.Write([]byte(task))
	mac.Write([]byte{0})
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...))
}
func (s Service) decodeCursor(token, tenant, project, task string) (uint64, uint64, error) {
	if token == "" {
		return 0, 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 48 {
		return 0, 0, ErrCursor
	}
	payload, signature := raw[:16], raw[16:]
	mac := hmac.New(sha256.New, s.CursorKey)
	mac.Write([]byte(tenant))
	mac.Write([]byte{0})
	mac.Write([]byte(project))
	mac.Write([]byte{0})
	mac.Write([]byte(task))
	mac.Write([]byte{0})
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return 0, 0, ErrCursor
	}
	after, ceiling := binary.BigEndian.Uint64(payload), binary.BigEndian.Uint64(payload[8:])
	if after > ceiling {
		return 0, 0, ErrCursor
	}
	return after, ceiling, nil
}

func cloneComment(c Comment) Comment {
	c.Revisions = append([]Revision(nil), c.Revisions...)
	for i := range c.Revisions {
		c.Revisions[i].Text.MentionHandles = append([]string(nil), c.Revisions[i].Text.MentionHandles...)
		c.Revisions[i].Text.Mentions = append([]Mention(nil), c.Revisions[i].Text.Mentions...)
	}
	return c
}
func cloneComments(in []Comment) []Comment {
	out := make([]Comment, len(in))
	for i := range in {
		out[i] = cloneComment(in[i])
	}
	return out
}

// MemoryStore provides atomic append and keyset paging for domain tests and
// local composition. Production storage can implement Store without changing
// the service contract.
type MemoryStore struct {
	mu       sync.RWMutex
	comments map[string]Comment
	events   map[string][]Activity
	sequence map[string]uint64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{comments: map[string]Comment{}, events: map[string][]Activity{}, sequence: map[string]uint64{}}
}
func compound(tenant, project, task, comment string) string {
	return tenant + "\x00" + project + "\x00" + task + "\x00" + comment
}
func taskKey(tenant, project, task string) string { return tenant + "\x00" + project + "\x00" + task }
func (m *MemoryStore) Create(ctx context.Context, c Comment, a Activity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.TenantID == "" || c.ProjectID == "" || c.TaskID == "" || c.ID == "" || c.CreatedAt.IsZero() || len(c.Revisions) != 1 || c.Current().Number != 1 || c.Current().Tombstone || !validActivityLink(c, a, 1) {
		return ErrInvalidRequest
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := compound(c.TenantID, c.ProjectID, c.TaskID, c.ID)
	if _, exists := m.comments[key]; exists {
		return ErrConflict
	}
	a.Sequence = m.sequence[taskKey(c.TenantID, c.ProjectID, c.TaskID)] + 1
	m.sequence[taskKey(c.TenantID, c.ProjectID, c.TaskID)] = a.Sequence
	m.comments[key] = cloneComment(c)
	m.events[taskKey(c.TenantID, c.ProjectID, c.TaskID)] = append(m.events[taskKey(c.TenantID, c.ProjectID, c.TaskID)], a)
	return nil
}
func (m *MemoryStore) Correct(ctx context.Context, tenant, project, task, id string, expected uint64, r Revision, a Activity) (Comment, error) {
	if err := ctx.Err(); err != nil {
		return Comment{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := compound(tenant, project, task, id)
	c, found := m.comments[key]
	if !found {
		return Comment{}, ErrNotFound
	}
	current := c.Current()
	if current.Number != expected || current.Tombstone {
		return Comment{}, ErrConflict
	}
	if r.Number != expected+1 || r.ActorID == "" || r.At.IsZero() || (!r.Tombstone && (r.Text.Source == "" || r.Text.HTML == "")) || !validActivityLink(c, a, r.Number) {
		return Comment{}, ErrInvalidRequest
	}
	c.Revisions = append(c.Revisions, r)
	m.comments[key] = cloneComment(c)
	tk := taskKey(c.TenantID, c.ProjectID, c.TaskID)
	a.Sequence = m.sequence[tk] + 1
	m.sequence[tk] = a.Sequence
	m.events[tk] = append(m.events[tk], a)
	return cloneComment(c), nil
}

func validActivityLink(c Comment, a Activity, revision uint64) bool {
	return a.TenantID == c.TenantID && a.ProjectID == c.ProjectID && a.TaskID == c.TaskID && a.CommentID == c.ID && a.ActorID != "" && a.Revision == revision && !a.At.IsZero()
}
func (m *MemoryStore) Page(ctx context.Context, tenant, project, task string, after, ceiling uint64, limit int) ([]Comment, []Activity, uint64, uint64, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, 0, 0, false, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := taskKey(tenant, project, task)
	events := m.events[key]
	if ceiling == 0 {
		ceiling = m.sequence[key]
	}
	selected := make([]Activity, 0, limit+1)
	for _, a := range events {
		if a.Sequence > after && a.Sequence <= ceiling {
			selected = append(selected, a)
		}
	}
	more := len(selected) > limit
	if more {
		selected = selected[:limit]
	}
	comments := make([]Comment, 0, len(selected))
	for _, a := range selected {
		c, ok := m.comments[compound(tenant, project, task, a.CommentID)]
		if !ok {
			continue
		}
		for _, revision := range c.Revisions {
			if revision.Number == a.Revision {
				comments = append(comments, Comment{TenantID: c.TenantID, ProjectID: c.ProjectID, TaskID: c.TaskID, ID: c.ID, CreatedAt: c.CreatedAt, Revisions: []Revision{revision}})
				break
			}
		}
	}
	var last uint64
	if len(selected) > 0 {
		last = selected[len(selected)-1].Sequence
	}
	return comments, append([]Activity(nil), selected...), last, ceiling, more, nil
}

func (m *MemoryStore) TimelinePage(ctx context.Context, tenant, project, task string, after, ceiling uint64, limit int) ([]Activity, uint64, uint64, bool, error) {
	_, entries, last, snapshot, more, err := m.Page(ctx, tenant, project, task, after, ceiling, limit)
	return entries, last, snapshot, more, err
}
