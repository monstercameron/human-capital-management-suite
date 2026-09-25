// Package projectsearch provides authorized task search and the bounded,
// canonical exact-filter path used while a derived search index is unavailable.
package projectsearch

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrInvalidRequest   = errors.New("projectsearch: invalid request")
	ErrUnavailable      = errors.New("projectsearch: required search port unavailable")
	ErrInvalidCursor    = errors.New("projectsearch: invalid or stale cursor")
	ErrTextUnavailable  = errors.New("projectsearch: free-text search unavailable")
	ErrIndexUnavailable = errors.New("projectsearch: search index unavailable")
)

const (
	MaxPageSize       = 100
	MaxFilterValues   = 100
	MaxCustomFieldIDs = 25
	FreshnessCurrent  = "CURRENT"
	FreshnessDegraded = "DEGRADED_EXACT_FILTER"
)

// Task is a small authorized search result projection.
type Task struct {
	ID, TenantID, ProjectID       string
	Title, Description            string
	StatusID, TypeID              string
	Priority, AssigneeID, DueDate string
	Revision                      uint64
	Archived                      bool
	Fields                        map[string]project.TaskFieldEdit
}

// Filter contains only bounded, exact predicates. Due dates are inclusive
// civil dates in YYYY-MM-DD form.
type Filter struct {
	StatusIDs   []string
	AssigneeID  string
	TypeIDs     []string
	DueDateFrom string
	DueDateTo   string
	Fields      map[string][]string
}

type Request struct {
	ProjectID string
	Text      string
	Filter    Filter
	Limit     int
	Cursor    string
}

type Page struct {
	Tasks     []Task
	Next      string
	Freshness string
}

type Authorizer interface {
	// Implementations must read current membership state on every invocation.
	Authorize(context.Context, string, string, string, projectaccess.Capability) error
}

// EmployeeStanding checks whether the authenticated subject is still an
// active employee. Implementations should consult current tenant-scoped
// workforce state on every call. The shape intentionally matches
// projectservice.WorkforceInviteeEligibility without importing that package.
type EmployeeStanding interface {
	CheckInvitee(context.Context, *trust.Principal, string) (uint8, error)
}

// Repository must apply the exact filters before the row limit, scope by
// tenant and project, and order by task ID ascending.
type Repository interface {
	ListExact(context.Context, string, string, string, Filter, int) ([]Task, error)
	EventSequence(context.Context, string, string) (uint64, error)
}

// IndexHealth reports whether the derived index is usable for the scope.
// Any health failure causes exact-filter fallback, never an unlabelled result.
type IndexHealth interface {
	Available(context.Context, string, string) (bool, error)
}

type Service struct {
	Auth      Authorizer
	Standing  EmployeeStanding
	Tasks     Repository
	Index     IndexHealth
	CursorKey []byte
}

func (s Service) Search(ctx context.Context, p *trust.Principal, req Request) (Page, error) {
	tenant, actor, err := principalIDs(p)
	if err != nil {
		return Page{}, err
	}
	if s.Auth == nil || s.Standing == nil || s.Tasks == nil || len(s.CursorKey) < 32 {
		return Page{}, ErrUnavailable
	}
	if strings.TrimSpace(req.ProjectID) == "" || req.Limit < 1 || req.Limit > MaxPageSize {
		return Page{}, ErrInvalidRequest
	}
	if err := validateFilter(req.Filter); err != nil {
		return Page{}, err
	}
	if err := s.checkEmployeeStanding(ctx, p); err != nil {
		return Page{}, err
	}
	if err := s.Auth.Authorize(ctx, tenant, req.ProjectID, actor, projectaccess.ReadTask); err != nil {
		return Page{}, err
	}
	if strings.TrimSpace(req.Text) != "" {
		// The current index has no canonical per-hit authorization adapter.
		// Fail closed until one is supplied; project membership alone does not
		// prove that every indexed hit is still current and readable.
		if err := s.checkEmployeeStanding(ctx, p); err != nil {
			return Page{}, err
		}
		return Page{}, ErrTextUnavailable
	}
	after := ""
	filterDigest := digestFilter(req.Filter)
	sequence, err := s.Tasks.EventSequence(ctx, tenant, req.ProjectID)
	if err != nil {
		return Page{}, err
	}
	if req.Cursor != "" {
		c, err := s.decodeCursor(req.Cursor)
		if err != nil || c.TenantID != tenant || c.ProjectID != req.ProjectID || c.ActorID != actor || c.FilterDigest != filterDigest || c.Limit != req.Limit || c.EventSequence != sequence {
			return Page{}, ErrInvalidCursor
		}
		after = c.AfterID
	}
	rows, err := s.Tasks.ListExact(ctx, tenant, req.ProjectID, after, req.Filter, req.Limit+1)
	if err != nil {
		return Page{}, err
	}
	hasMore := len(rows) > req.Limit
	if hasMore {
		rows = rows[:req.Limit]
	}
	if err := s.checkEmployeeStanding(ctx, p); err != nil {
		return Page{}, err
	}
	// Access is checked again after the read so revocation during a query cannot
	// turn a previously admitted result into a response or continuation cursor.
	if err := s.Auth.Authorize(ctx, tenant, req.ProjectID, actor, projectaccess.ReadTask); err != nil {
		return Page{}, err
	}
	currentSequence, err := s.Tasks.EventSequence(ctx, tenant, req.ProjectID)
	if err != nil {
		return Page{}, err
	}
	if currentSequence != sequence {
		return Page{}, ErrInvalidCursor
	}
	page := Page{Tasks: rows, Freshness: FreshnessCurrent}
	if s.Index != nil {
		available, healthErr := s.Index.Available(ctx, tenant, req.ProjectID)
		if healthErr != nil || !available {
			page.Freshness = FreshnessDegraded
		}
	} else {
		page.Freshness = FreshnessDegraded
	}
	// Fetch limit+1 above and retain the extra row only as continuation proof.
	// The cursor starts after the last returned row, so no matching task is lost.
	if hasMore {
		cursor, err := s.encodeCursor(searchCursor{TenantID: tenant, ProjectID: req.ProjectID, ActorID: actor, FilterDigest: filterDigest, EventSequence: sequence, Limit: req.Limit, AfterID: rows[len(rows)-1].ID})
		if err != nil {
			return Page{}, err
		}
		page.Next = cursor
	}
	if err := s.checkEmployeeStanding(ctx, p); err != nil {
		return Page{}, err
	}
	if err := s.Auth.Authorize(ctx, tenant, req.ProjectID, actor, projectaccess.ReadTask); err != nil {
		return Page{}, err
	}
	return page, nil
}

func (s Service) checkEmployeeStanding(ctx context.Context, p *trust.Principal) error {
	if s.Standing == nil {
		return ErrUnavailable
	}
	_, err := s.Standing.CheckInvitee(ctx, p, p.Subject())
	return err
}

func principalIDs(p *trust.Principal) (string, string, error) {
	if p == nil || string(p.Tenant()) == "" || strings.TrimSpace(p.Subject()) == "" {
		return "", "", ErrInvalidRequest
	}
	return string(p.Tenant()), p.Subject(), nil
}

func validateFilter(f Filter) error {
	if len(f.StatusIDs) > MaxFilterValues || len(f.TypeIDs) > MaxFilterValues || len(f.Fields) > MaxCustomFieldIDs {
		return ErrInvalidRequest
	}
	for _, values := range [][]string{f.StatusIDs, f.TypeIDs} {
		for _, v := range values {
			if strings.TrimSpace(v) == "" || len(v) > 128 {
				return ErrInvalidRequest
			}
		}
	}
	if len(f.AssigneeID) > 128 {
		return ErrInvalidRequest
	}
	for id, values := range f.Fields {
		if strings.TrimSpace(id) == "" || len(id) > 64 || len(values) == 0 || len(values) > MaxFilterValues {
			return ErrInvalidRequest
		}
		for _, v := range values {
			if len(v) > 256 {
				return ErrInvalidRequest
			}
		}
	}
	from, err := parseDate(f.DueDateFrom)
	if err != nil {
		return ErrInvalidRequest
	}
	to, err := parseDate(f.DueDateTo)
	if err != nil || (from != nil && to != nil && from.After(*to)) {
		return ErrInvalidRequest
	}
	return nil
}

func parseDate(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil || t.Format("2006-01-02") != s {
		return nil, ErrInvalidRequest
	}
	return &t, nil
}

type searchCursor struct {
	TenantID, ProjectID, ActorID string
	FilterDigest                 string
	EventSequence                uint64
	Limit                        int
	AfterID                      string
}

func (s Service) encodeCursor(c searchCursor) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.CursorKey)
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s Service) decodeCursor(token string) (searchCursor, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return searchCursor{}, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return searchCursor{}, ErrInvalidCursor
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return searchCursor{}, ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, s.CursorKey)
	_, _ = mac.Write(raw)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return searchCursor{}, ErrInvalidCursor
	}
	var c searchCursor
	if err := json.Unmarshal(raw, &c); err != nil || c.AfterID == "" {
		return searchCursor{}, ErrInvalidCursor
	}
	return c, nil
}

func digestFilter(f Filter) string {
	raw, _ := json.Marshal(f)
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:])
}
