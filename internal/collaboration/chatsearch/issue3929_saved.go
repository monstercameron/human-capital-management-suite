package chatsearch

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ISSUE-3929: searches a person chose to keep. A saved search is a private
// shortcut: a name and the query text (words plus filters) exactly as typed.
// It is not a grant and not a snapshot. Opening one is an ordinary search that
// checks the person's access again, so a conversation that was deleted or a
// room the person has left simply stops matching; nothing here widens a query.

var (
	ErrSavedNotFound = errors.New("chat search: saved search not found")
	ErrSavedNameUsed = errors.New("chat search: a saved search already has that name")
	ErrSavedLimit    = errors.New("chat search: too many saved searches")
)

const (
	SavedNameMax  = 80
	SavedQueryMax = 512
	// SavedLimit is how many searches one person may keep.
	SavedLimit = 50
)

// SavedSearch is one kept search. Only its owner ever sees it.
type SavedSearch struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Query     string    `json:"query"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SavedStore is the durable, person-scoped home of saved searches. Every call
// names the owner and can reach only that owner's rows in that workspace.
type SavedStore interface {
	ListSaved(context.Context, Actor) ([]SavedSearch, error)
	CreateSaved(ctx context.Context, actor Actor, saved SavedSearch, limit int) (SavedSearch, error)
	UpdateSaved(ctx context.Context, actor Actor, saved SavedSearch) (SavedSearch, error)
	DeleteSaved(ctx context.Context, actor Actor, id string) error
}

// SavedService validates a shortcut before it is stored.
type SavedService struct{ Store SavedStore }

func NewSavedService(store SavedStore) *SavedService { return &SavedService{Store: store} }

func savedActorOK(a Actor) bool {
	return a.TenantID != "" && a.HomeTenantID != "" && a.PersonID != ""
}

// CleanSaved trims and checks a name and query. The query must parse with the
// same rules a search uses, so a shortcut can never hold a filter the search
// would refuse, and it must hold at least a word or a filter.
func CleanSaved(name, query string) (string, string, error) {
	name, query = strings.Join(strings.Fields(name), " "), strings.TrimSpace(query)
	if name == "" || utf8.RuneCountInString(name) > SavedNameMax || query == "" || len(query) > SavedQueryMax {
		return "", "", ErrInvalid
	}
	parsed, err := Parse(query)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(parsed.Text) == "" && parsed.Filters.Empty() {
		return "", "", ErrInvalid
	}
	return name, query, nil
}

func (s *SavedService) List(ctx context.Context, actor Actor) ([]SavedSearch, error) {
	if s == nil || s.Store == nil {
		return nil, ErrUnavailable
	}
	if !savedActorOK(actor) {
		return nil, ErrInvalid
	}
	return s.Store.ListSaved(ctx, actor)
}

func (s *SavedService) Create(ctx context.Context, actor Actor, name, query string) (SavedSearch, error) {
	if s == nil || s.Store == nil {
		return SavedSearch{}, ErrUnavailable
	}
	if !savedActorOK(actor) {
		return SavedSearch{}, ErrInvalid
	}
	name, query, err := CleanSaved(name, query)
	if err != nil {
		return SavedSearch{}, err
	}
	return s.Store.CreateSaved(ctx, actor, SavedSearch{Name: name, Query: query}, SavedLimit)
}

func (s *SavedService) Update(ctx context.Context, actor Actor, id, name, query string) (SavedSearch, error) {
	if s == nil || s.Store == nil {
		return SavedSearch{}, ErrUnavailable
	}
	if !savedActorOK(actor) || id == "" {
		return SavedSearch{}, ErrInvalid
	}
	name, query, err := CleanSaved(name, query)
	if err != nil {
		return SavedSearch{}, err
	}
	return s.Store.UpdateSaved(ctx, actor, SavedSearch{ID: id, Name: name, Query: query})
}

func (s *SavedService) Delete(ctx context.Context, actor Actor, id string) error {
	if s == nil || s.Store == nil {
		return ErrUnavailable
	}
	if !savedActorOK(actor) || id == "" {
		return ErrInvalid
	}
	return s.Store.DeleteSaved(ctx, actor, id)
}

// MemorySaved is an in-process SavedStore for tests and for hosts with no
// database. It keeps the same owner scoping as the durable store.
type MemorySaved struct {
	mu   sync.Mutex
	next int
	rows map[Actor][]SavedSearch
}

func NewMemorySaved() *MemorySaved { return &MemorySaved{rows: map[Actor][]SavedSearch{}} }

func (m *MemorySaved) ListSaved(_ context.Context, a Actor) ([]SavedSearch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]SavedSearch{}, m.rows[a]...), nil
}

func (m *MemorySaved) CreateSaved(_ context.Context, a Actor, s SavedSearch, limit int) (SavedSearch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows[a] {
		if strings.EqualFold(row.Name, s.Name) {
			return SavedSearch{}, ErrSavedNameUsed
		}
	}
	if limit > 0 && len(m.rows[a]) >= limit {
		return SavedSearch{}, ErrSavedLimit
	}
	m.next++
	s.ID, s.UpdatedAt = "saved-"+strconv.Itoa(m.next), time.Now().UTC()
	m.rows[a] = append(m.rows[a], s)
	return s, nil
}

func (m *MemorySaved) UpdateSaved(_ context.Context, a Actor, s SavedSearch) (SavedSearch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	at := -1
	for i, row := range m.rows[a] {
		if row.ID == s.ID {
			at = i
		} else if strings.EqualFold(row.Name, s.Name) {
			return SavedSearch{}, ErrSavedNameUsed
		}
	}
	if at < 0 {
		return SavedSearch{}, ErrSavedNotFound
	}
	s.UpdatedAt = time.Now().UTC()
	m.rows[a][at] = s
	return s, nil
}

func (m *MemorySaved) DeleteSaved(_ context.Context, a Actor, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, row := range m.rows[a] {
		if row.ID == id {
			m.rows[a] = append(m.rows[a][:i:i], m.rows[a][i+1:]...)
			return nil
		}
	}
	return ErrSavedNotFound
}
