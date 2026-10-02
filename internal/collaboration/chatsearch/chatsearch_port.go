// Package chatsearch owns the current-access search contract for Chat.
package chatsearch

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalid           = errors.New("chat search: invalid request")
	ErrUnavailable       = errors.New("chat search: meaning search unavailable")
	ErrRegistry          = errors.New("chat search: missing or duplicate source declaration")
	ErrSourceUnavailable = errors.New("chat search: content source unavailable")
)

type Kind string

const (
	Message          Kind = "message"
	Thread           Kind = "thread"
	Conversation     Kind = "conversation"
	Person           Kind = "person"
	File             Kind = "file"
	Pin              Kind = "pin"
	Todo             Kind = "todo"
	Poll             Kind = "poll"
	AgentAnswer      Kind = "agent_answer"
	SourceTitle      Kind = "source"
	Voice            Kind = "voice"
	VoiceCorrection  Kind = "voice_correction"
	Announcement     Kind = "announcement"
	Reminder         Kind = "reminder"
	PrivateTask      Kind = "private_task"
	PrivateReminder  Kind = "private_reminder"
	GateQuestion     Kind = "gate_question"
	GateAnswer       Kind = "gate_answer"
	Saved            Kind = "saved"
	Location         Kind = "location"
	GateDefinition   Kind = "gate_definition"
	FilterDefinition Kind = "filter"
	Moderation       Kind = "moderation"
)

type Declaration struct {
	Kind                  Kind
	Text, Audience, Opens string
}

// Declarations is a fresh value, never a mutable global registry. Additional
// stores declare their kinds when they register their source on a Registry.
func Declarations() []Declaration {
	return []Declaration{
		{Message, "current reader rendering", "current members and history visibility", "message and context"},
		{Thread, "current reply rendering", "current members and history visibility", "thread reply and context"},
		{Conversation, "name, topic and purpose", "current readers", "conversation details"},
		{Person, "directory display names", "current directory audience", "person"},
		{File, "attachment and document names", "current message and artifact readers", "attachment on message"},
		{Pin, "pinned message rendering", "current message readers", "pinned message"},
		{Todo, "current item text", "current channel readers", "list item"},
		{Poll, "question and option text", "current channel readers", "poll"},
		{AgentAnswer, "answer rendering", "current members for public; current recipient for private", "answer message"},
		{SourceTitle, "source title and cited section", "current answer and document readers", "document section and version"},
		{Voice, "current transcript", "current message readers", "matching sentence"},
		{VoiceCorrection, "current author correction", "current message readers", "matching sentence"},
		{Announcement, "announcement rendering", "current audience", "announcement message"},
		{Reminder, "reminder rendering", "current audience", "reminder item"},
		{PrivateTask, "task text", "owner only", "task item"},
		{PrivateReminder, "reminder text", "owner only", "reminder item"},
		{GateQuestion, "current question", "members and applicants", "gate question"},
		{GateAnswer, "own answer", "answer owner only; administrators use gate answers view", "own gate answer"},
		{Saved, "saved rendering", "owner and current source readers", "saved item"},
		{Location, "label and address; never coordinates", "current unexpired message audience", "location message"},
		{GateDefinition, "gate name and description", "current gate audience", "gate"},
		{FilterDefinition, "filter name and description", "current filter audience", "filter"},
		{Moderation, "queue item rendering", "current moderators who can read source", "queue item"},
	}
}

type Actor struct{ TenantID, HomeTenantID, PersonID string }
type Target struct {
	ConversationID, MessageID, ThreadID, ItemID, DocumentID, Section, Version string
	Sequence                                                                  uint64
	ThreadSequence                                                            uint64
	Sentence                                                                  int
}
type Row struct {
	Kind                                                                             Kind
	ID, TenantID, Text, AuthorID, OwnerID                                            string
	At                                                                               time.Time
	Target                                                                           Target
	Private, HasFile, HasLink, HasReactions, InThread, MentionsMe, ByAgent, HasVoice bool
	Deleted, Removed                                                                 bool
	ExpiresAt                                                                        time.Time
}

type Request struct {
	Actor        Actor `json:"-"`
	Query        string
	DisplayQuery string
	Filters      Filters
	Mode, Cursor string
	Limit        int
	// Transient marks a search asked while a person types in the workspace
	// search box (CHATSEARCH-002). It is answered like any other and is not
	// remembered among their recent Chat searches.
	Transient bool
	// At is supplied by the server, never by an HTTP client.
	At             time.Time `json:"-"`
	BeforeAt       time.Time `json:"-"`
	BeforeKey      string    `json:"-"`
	OpenMessageID  string    `json:"-"`
	OpenIDs        []string  `json:"-"`
	OpenMessageIDs []string  `json:"-"`
}
type Group struct {
	Kind  Kind
	Count int
	Rows  []Row
}
type Response struct {
	Mode        string
	Groups      []Group
	NextCursor  string
	Unavailable []Kind
	CountScope  string
	// Kinds are the kinds a source is registered for, in order. A declared
	// kind that is not among them cannot be found here.
	Kinds []Kind
	// Failures holds the cause behind each Unavailable kind. It never leaves the
	// server: the handler may log it, and the JSON body never carries it.
	Failures []SourceFailure `json:"-"`
}

// SourceFailure is why one registered source could not answer.
type SourceFailure struct {
	Kind Kind
	Err  error
}

// DefaultSourceTimeout bounds how long any one source may hold a search. A
// source that is slow or failing is reported as unavailable; the others answer.
const DefaultSourceTimeout = 5 * time.Second

// Source must narrow by current access BEFORE matching, counting, or producing
// snippets. CanOpen must reload current access and current row state, including
// deletion, expiry, segment and document grants. False reveals no reason.
// Search streams newest-first by (At, Kind, ID), honors BeforeAt/BeforeKey,
// and returns at most Limit+1 authorized matches. No total is inferred from
// a source's candidate count.
type Source interface {
	Search(context.Context, Request) ([]Row, error)
	CanOpen(context.Context, Actor, Row) (bool, error)
}

// BatchRechecker reloads current rows in one read, then checks every row's
// current authority. Keys are row IDs within this source, never totals.
type BatchRechecker interface {
	CanOpenRows(context.Context, Actor, []Row) (map[string]bool, error)
}
type Meaning interface {
	SearchMeaning(context.Context, Request) ([]Row, error)
}
type UnavailableMeaning struct{}

func (UnavailableMeaning) SearchMeaning(context.Context, Request) ([]Row, error) {
	return nil, ErrUnavailable
}

type Registry struct {
	mu           sync.RWMutex
	declarations map[Kind]Declaration
	sources      map[Kind]Source
	// SourceTimeout bounds each phase of a search; zero means DefaultSourceTimeout.
	SourceTimeout time.Duration
}

func NewRegistry() *Registry {
	r := &Registry{declarations: map[Kind]Declaration{}, sources: map[Kind]Source{}}
	for _, d := range Declarations() {
		r.declarations[d.Kind] = d
	}
	return r
}
func (r *Registry) Register(d Declaration, source Source) error {
	if d.Kind == "" || d.Text == "" || d.Audience == "" || d.Opens == "" || source == nil {
		return ErrRegistry
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sources[d.Kind] != nil {
		return ErrRegistry
	}
	if known, ok := r.declarations[d.Kind]; ok && known != d {
		return ErrRegistry
	}
	r.declarations[d.Kind], r.sources[d.Kind] = d, source
	return nil
}
func (r *Registry) Declaration(kind Kind) (Declaration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.declarations[kind]
	return d, ok
}
func (r *Registry) ValidateStoredKinds(kinds []Kind) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, kind := range kinds {
		if _, ok := r.declarations[kind]; !ok || r.sources[kind] == nil {
			return ErrRegistry
		}
	}
	return nil
}

type cursor struct {
	Scope string
	At    time.Time
	Key   string
}

func rowKey(row Row) string { return string(row.Kind) + "\x00" + row.ID }
func requestScope(q Request) string {
	b, _ := json.Marshal(struct {
		Actor   Actor
		Query   string
		Filters Filters
		Mode    string
	}{q.Actor, q.Query, q.Filters, q.Mode})
	h := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(h[:])
}
func (r *Registry) sourceTimeout() time.Duration {
	if r.SourceTimeout > 0 {
		return r.SourceTimeout
	}
	return DefaultSourceTimeout
}

type fanAnswer[T any] struct {
	value T
	err   error
}

// fanOut runs fn for every kind at the same moment and gives up on any that is
// still running when the budget ends, so one slow source costs the budget and
// nothing more. A panic inside a source is that source's failure, not the
// server's.
func fanOut[T any](ctx context.Context, budget time.Duration, kinds []Kind, fn func(context.Context, Kind) (T, error)) map[Kind]fanAnswer[T] {
	got := map[Kind]fanAnswer[T]{}
	if len(kinds) == 0 {
		return got
	}
	fctx, cancel := context.WithTimeout(ctx, budget)
	fctx = WithScope(fctx)
	defer cancel()
	type named struct {
		kind   Kind
		answer fanAnswer[T]
	}
	done := make(chan named, len(kinds))
	for _, kind := range kinds {
		go func(kind Kind) {
			defer func() {
				if recover() != nil {
					done <- named{kind, fanAnswer[T]{err: ErrSourceUnavailable}}
				}
			}()
			value, err := fn(fctx, kind)
			done <- named{kind, fanAnswer[T]{value: value, err: err}}
		}(kind)
	}
	for len(got) < len(kinds) {
		select {
		case n := <-done:
			got[n.kind] = n.answer
		case <-fctx.Done():
			for _, kind := range kinds {
				if _, ok := got[kind]; !ok {
					got[kind] = fanAnswer[T]{err: fctx.Err()}
				}
			}
		}
	}
	return got
}

func (r *Registry) Search(ctx context.Context, q Request) (Response, error) {
	out := Response{Mode: "keyword", CountScope: "page"}
	if q.Actor.TenantID == "" || q.Actor.HomeTenantID == "" || q.Actor.PersonID == "" || len(q.Query) > 512 || q.Limit < 0 || q.Limit > 50 || len(q.Cursor) > 4096 || q.At.IsZero() {
		return Response{}, ErrInvalid
	}
	if len(q.DisplayQuery) > 512 {
		return Response{}, ErrInvalid
	}
	if _, err := Parse(q.DisplayQuery); err != nil {
		return Response{}, err
	}
	if q.Mode != "" && q.Mode != "keyword" && q.Mode != "meaning" {
		return Response{}, ErrInvalid
	}
	// No meaning index is installed here, so a meaning request quietly answers
	// from the keyword path instead of reporting a failure.
	q.Mode = "keyword"
	parsed, err := Parse(q.Query)
	if err != nil {
		return Response{}, err
	}
	q.Query = parsed.Text
	q.Filters, err = Combine(parsed.Filters, q.Filters)
	if err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(q.Query) == "" && q.Filters.Empty() {
		return Response{}, ErrInvalid
	}
	if q.Limit == 0 {
		q.Limit = 20
	}
	scope := requestScope(q)
	var after cursor
	if q.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil || json.Unmarshal(b, &after) != nil || after.Scope != scope || after.Key == "" {
			return Response{}, ErrInvalid
		}
	}
	q.BeforeAt, q.BeforeKey = after.At, after.Key
	r.mu.RLock()
	sources := map[Kind]Source{}
	for k, s := range r.sources {
		sources[k] = s
	}
	decls := map[Kind]Declaration{}
	for k, d := range r.declarations {
		decls[k] = d
	}
	r.mu.RUnlock()
	if q.Filters.Kind != "" {
		if _, ok := decls[q.Filters.Kind]; !ok {
			return Response{}, ErrInvalid
		}
	}
	kinds := make([]Kind, 0, len(decls))
	for k := range decls {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	// The kinds that can be found in this deployment, so the page's kind filter
	// offers none that nothing here can answer.
	for _, kind := range kinds {
		if sources[kind] != nil {
			out.Kinds = append(out.Kinds, kind)
		}
	}
	var rows []Row
	seen := map[string]bool{}
	failed := map[Kind]bool{}
	fail := func(kind Kind, err error) {
		if failed[kind] {
			return
		}
		failed[kind] = true
		out.Unavailable = append(out.Unavailable, kind)
		out.Failures = append(out.Failures, SourceFailure{Kind: kind, Err: err})
	}
	// A declared kind nobody registered a source for has nothing to search in
	// this deployment; that is not an outage and is never reported as one. Every
	// registered source runs at the same moment under its own deadline, so a
	// slow or failing one never delays or fails the others.
	searchKinds := []Kind{}
	for _, kind := range kinds {
		if (q.Filters.Kind == "" || q.Filters.Kind == kind) && sources[kind] != nil {
			searchKinds = append(searchKinds, kind)
		}
	}
	answers := fanOut(ctx, r.sourceTimeout(), searchKinds, func(sctx context.Context, kind Kind) ([]Row, error) {
		return sources[kind].Search(sctx, q)
	})
	if ctx.Err() != nil {
		return Response{}, ctx.Err()
	}
	for _, kind := range searchKinds {
		answer := answers[kind]
		if answer.err != nil {
			fail(kind, answer.err)
			continue
		}
		candidates := answer.value
		for _, row := range candidates {
			if row.Kind != kind || row.TenantID != q.Actor.TenantID || row.ID == "" || row.Deleted || row.Removed || !row.ExpiresAt.IsZero() && !q.At.Before(row.ExpiresAt) || row.Private && row.OwnerID != q.Actor.PersonID {
				continue
			}
			if seen[rowKey(row)] {
				continue
			}

			seen[rowKey(row)] = true
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].At.Equal(rows[j].At) {
			return rowKey(rows[i]) > rowKey(rows[j])
		}
		return rows[i].At.After(rows[j].At)
	})
	batch := map[Kind]map[string]bool{}
	batchKinds := []Kind{}
	batchRows := map[Kind][]Row{}
	for _, kind := range kinds {
		if _, ok := sources[kind].(BatchRechecker); !ok {
			continue
		}
		for _, row := range rows {
			if row.Kind == kind {
				batchRows[kind] = append(batchRows[kind], row)
			}
		}
		if len(batchRows[kind]) > 0 {
			batchKinds = append(batchKinds, kind)
		}
	}
	recheck := fanOut(ctx, r.sourceTimeout(), batchKinds, func(sctx context.Context, kind Kind) (map[string]bool, error) {
		return sources[kind].(BatchRechecker).CanOpenRows(sctx, q.Actor, batchRows[kind])
	})
	if ctx.Err() != nil {
		return Response{}, ctx.Err()
	}
	for _, kind := range batchKinds {
		if recheck[kind].err != nil {
			fail(kind, recheck[kind].err)
			continue
		}
		batch[kind] = recheck[kind].value
	}
	// Rows of a source that cannot be rechecked are withheld, never shown on
	// trust: current access is verified before a row leaves the server.
	single, singleCancel := context.WithTimeout(ctx, r.sourceTimeout())
	single = WithScope(single)
	defer singleCancel()
	var page []Row
	for _, row := range rows {
		if after.Key != "" && (row.At.After(after.At) || row.At.Equal(after.At) && rowKey(row) >= after.Key) {
			continue
		}
		if failed[row.Kind] {
			continue
		}
		// Recheck every returned row, even if membership changed during another
		// source's search. Counts include only this same revalidated set.
		var ok bool
		var e error
		if allowed, batched := batch[row.Kind]; batched {
			ok = allowed[row.ID]
		} else {
			ok, e = sources[row.Kind].CanOpen(single, q.Actor, row)
		}
		if e != nil {
			if ctx.Err() != nil {
				return Response{}, ctx.Err()
			}
			fail(row.Kind, e)
			continue
		}
		if !ok || !Match(row, q) {
			continue
		}
		page = append(page, row)
		if len(page) > q.Limit {
			break
		}
	}
	if len(page) > q.Limit {
		last := page[q.Limit-1]
		b, _ := json.Marshal(cursor{scope, last.At, rowKey(last)})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(b)
		page = page[:q.Limit]
	}
	// Counts describe only the rows shown on this page. This stays honest under
	// keyset pagination without scanning or hinting at withheld content.
	counts := map[Kind]int{}
	for _, row := range page {
		counts[row.Kind]++
	}
	for _, kind := range kinds {
		if counts[kind] == 0 {
			continue
		}
		g := Group{Kind: kind, Count: counts[kind], Rows: []Row{}}
		for _, row := range page {
			if row.Kind == kind {
				g.Rows = append(g.Rows, row)
			}
		}
		out.Groups = append(out.Groups, g)
	}
	return out, nil
}
