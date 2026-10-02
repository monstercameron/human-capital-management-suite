package chatfilter

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ManageFilters = "Manage filters"

type Actor struct{ Tenant, Subject, Channel string }
type Authority interface {
	AuthorizeFilters(context.Context, Actor, string) error
	CanReadFilterConversation(context.Context, Actor, string) bool
}
type Enablement struct {
	RuleID, Channel string
	Action          string
	Enabled         bool
	DryRunUntil     time.Time
}
type Record struct {
	ID                       int64
	Tenant, Channel, Subject string
	At                       time.Time
	Hit                      Hit
}
type Store interface {
	Definitions(context.Context, string) ([]Definition, error)
	CreateVersion(context.Context, string, Definition) error
	Enablements(context.Context, string) ([]Enablement, error)
	PutEnablement(context.Context, string, Enablement) error
	RecordHits(context.Context, string, []Record) error
	Hits(context.Context, string) ([]Record, error)
}

// Delivery owns durable notification/review delivery. No message or matched
// secret crosses this port; consumers receive the versioned digest-only hit.
type Delivery interface {
	DeliverFilterHit(context.Context, Record) error
}
type Service struct {
	Store     Store
	Registry  *Registry
	Authority Authority
	Delivery  Delivery
	Now       func() time.Time

	// Evaluation caches (chatmod002_cache.go). The zero value works: the maps
	// are created on first use and the lock needs no setup.
	cacheMu    sync.Mutex
	tenants    map[string]*tenantRules
	evaluators map[string]*Evaluator
	evalOrder  []string
	cacheClock func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) authorized(ctx context.Context, a Actor, channel string) error {
	if a.Tenant == "" || a.Subject == "" {
		return ErrDenied
	}
	if s.Authority == nil || s.Store == nil || s.Registry == nil {
		return ErrUnavailable
	}
	return s.Authority.AuthorizeFilters(ctx, a, channel)
}
func (s *Service) List(ctx context.Context, a Actor) ([]Definition, error) {
	if err := s.authorized(ctx, a, a.Channel); err != nil {
		return nil, err
	}
	defs, err := s.Store.Definitions(ctx, a.Tenant)
	if err != nil {
		return nil, err
	}
	all := latest(append(Builtins(), defs...))
	if a.Channel == "" {
		return all, nil
	}
	var out []Definition
	for _, d := range all {
		if len(d.Channels) == 0 || contains(d.Channels, a.Channel) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (s *Service) ListEnablements(ctx context.Context, a Actor) ([]Enablement, error) {
	if err := s.authorized(ctx, a, a.Channel); err != nil {
		return nil, err
	}
	rows, err := s.Store.Enablements(ctx, a.Tenant)
	if err != nil {
		return nil, err
	}
	if a.Channel == "" {
		return rows, nil
	}
	var out []Enablement
	for _, row := range rows {
		if row.Channel == "" || row.Channel == a.Channel {
			out = append(out, row)
		}
	}
	return out, nil
}
func latest(defs []Definition) []Definition {
	byID := map[string]Definition{}
	for _, d := range defs {
		old, ok := byID[d.ID]
		if !ok || compareVersion(d.Version, old.Version) > 0 {
			byID[d.ID] = d
		}
	}
	out := make([]Definition, 0, len(byID))
	for _, d := range byID {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func compareVersion(a, b string) int {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	if len(x) != 3 || len(y) != 3 {
		return strings.Compare(a, b)
	}
	for i := range x {
		n, _ := strconv.ParseUint(x[i], 10, 64)
		m, _ := strconv.ParseUint(y[i], 10, 64)
		if n < m {
			return -1
		}
		if n > m {
			return 1
		}
	}
	return 0
}
func (s *Service) CreateVersion(ctx context.Context, a Actor, d Definition) error {
	channel := ""
	if len(d.Channels) == 1 {
		channel = d.Channels[0]
	}
	if err := s.authorized(ctx, a, channel); err != nil {
		return err
	}
	if d.Product || strings.HasPrefix(d.ID, "builtin-") {
		return ErrDenied
	}
	if _, err := s.Registry.Compile([]Definition{d}); err != nil {
		return err
	}
	// Workspace authority is required for workspace scope, hard rules and
	// multi-channel scope. Channel managers cannot assert workspace authority.
	if len(d.Channels) != 1 || d.Hard {
		if err := s.authorized(ctx, a, ""); err != nil {
			return err
		}
	}
	defs, err := s.Store.Definitions(ctx, a.Tenant)
	if err != nil {
		return err
	}
	for _, old := range defs {
		if old.ID == d.ID && (compareVersion(d.Version, old.Version) <= 0 || old.Hard && !d.Hard || strings.Join(old.Channels, "\x00") != strings.Join(d.Channels, "\x00")) {
			return ErrConflict
		}
	}
	err = s.Store.CreateVersion(ctx, a.Tenant, d)
	s.invalidate(a.Tenant)
	return err
}
func (s *Service) Enable(ctx context.Context, a Actor, e Enablement, dryRun bool) error {
	if err := s.authorized(ctx, a, e.Channel); err != nil {
		return err
	}
	defs, err := s.Store.Definitions(ctx, a.Tenant)
	if err != nil {
		return err
	}
	defs = latest(append(Builtins(), defs...))
	found := false
	for _, d := range defs {
		if d.ID == e.RuleID {
			found = true
			if d.Hard {
				if err := s.authorized(ctx, a, ""); err != nil {
					return err
				}
			}
			if e.Action != "" {
				if _, ok := s.Registry.actions[e.Action]; !ok {
					return ErrInvalid
				}
				if !d.Product && e.Action != d.Action {
					return ErrDenied
				}
			}
			if e.Channel != "" && !d.Product && len(d.Channels) != 1 {
				return ErrDenied
			}
			if len(d.Channels) > 0 && e.Channel != "" && !contains(d.Channels, e.Channel) {
				return ErrDenied
			}
		}
	}
	if !found {
		return ErrInvalid
	}
	e.DryRunUntil = time.Time{}
	if dryRun && e.Enabled {
		e.DryRunUntil = s.now().Add(7 * 24 * time.Hour)
	}
	err = s.Store.PutEnablement(ctx, a.Tenant, e)
	s.invalidate(a.Tenant)
	return err
}
func (s *Service) Try(ctx context.Context, a Actor, d Definition, in Input) (Result, error) {
	if err := s.authorized(ctx, a, in.Channel); err != nil {
		return Result{}, err
	}
	e, err := s.Registry.Compile([]Definition{d})
	if err != nil {
		return Result{}, err
	}
	return e.Evaluate(in)
}
func (s *Service) Evaluate(ctx context.Context, in Input, record bool) (Result, error) {
	at := s.now()
	if s.Store == nil || s.Registry == nil || in.Tenant == "" {
		return Result{}, ErrUnavailable
	}
	rules, err := s.resolve(ctx, in.Tenant, in.Channel)
	if err != nil {
		return Result{}, err
	}
	e, err := s.evaluator(in.Tenant, rules)
	if err != nil {
		return Result{}, err
	}
	dry := func(id string) bool { return rules.dry(id, at) }
	all, out, err := e.evaluate(in, dry)
	if err != nil {
		return Result{}, err
	}
	var records []Record
	for _, hit := range all.Hits {
		hit.DryRun = dry(hit.RuleID)
		records = append(records, Record{Tenant: in.Tenant, Channel: in.Channel, Subject: in.Subject, At: at, Hit: hit})
	}
	if record && len(records) > 0 {
		if err = s.Store.RecordHits(ctx, in.Tenant, records); err != nil {
			return Result{}, err
		}
		for _, r := range records {
			if !r.Hit.DryRun && s.Registry.actions[r.Hit.Action].Deliver {
				if s.Delivery == nil {
					return Result{}, ErrUnavailable
				}
				if err = s.Delivery.DeliverFilterHit(ctx, r); err != nil {
					return Result{}, err
				}
			}
		}
	}
	out.Hits = all.Hits
	for i := range out.Hits {
		out.Hits[i].DryRun = dry(out.Hits[i].RuleID)
	}
	return out, nil
}
func (s *Service) ReadHits(ctx context.Context, a Actor, query string) ([]Record, error) {
	return s.ReadHitsBefore(ctx, a, query, 0)
}
func (s *Service) ReadHitsBefore(ctx context.Context, a Actor, query string, before int64) ([]Record, error) {
	if err := s.authorized(ctx, a, a.Channel); err != nil {
		return nil, err
	}
	if len(query) > 512 || before < 0 {
		return nil, ErrInvalid
	}
	var rows []Record
	var err error
	if search, ok := s.Store.(interface {
		SearchHitRecords(context.Context, string, string, string, int64) ([]Record, error)
	}); ok {
		rows, err = search.SearchHitRecords(ctx, a.Tenant, query, a.Channel, before)
	} else {
		rows, err = s.Store.Hits(ctx, a.Tenant)
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, r := range rows {
		if a.Channel != "" && a.Channel != r.Channel {
			continue
		}
		if !strings.Contains(strings.ToLower(r.Hit.RuleName+" "+r.Hit.RuleID+" "+r.Hit.Version+" "+r.Hit.Action), strings.ToLower(query)) {
			continue
		}
		if !s.Authority.CanReadFilterConversation(ctx, a, r.Channel) {
			r.Hit.Masked = ""
			r.Hit.Span = Span{}
			r.Hit.Digest = ""
			r.Subject = ""
		}
		out = append(out, r)
	}
	return out, nil
}
func (s *Service) SearchFilters(ctx context.Context, a Actor, query string) ([]Definition, error) {
	defs, err := s.List(ctx, a)
	if err != nil {
		return nil, err
	}
	var out []Definition
	for _, d := range defs {
		if strings.Contains(strings.ToLower(d.Name+" "+d.Kind+" "+d.Action+" "+strings.Join(d.Match, " ")), strings.ToLower(query)) {
			out = append(out, d)
		}
	}
	return out, nil
}
func (r Result) Refusal() error {
	if r.blocked != nil {
		return r.blocked
	}
	if r.Action != "block" {
		return nil
	}
	var spans []Span
	name := ""
	for _, h := range r.Hits {
		if h.Action == "block" && !h.DryRun {
			if name == "" {
				name = h.RuleName
			}
			spans = append(spans, h.Span)
		}
	}
	if len(spans) > 0 {
		spans = normalizeSpans(spans, maxBlockedSpans)
		return &BlockedError{RuleName: name, Span: spans[0], Spans: spans}
	}
	return fmt.Errorf("%w", ErrBlocked)
}
