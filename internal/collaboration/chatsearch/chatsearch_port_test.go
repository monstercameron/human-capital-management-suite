package chatsearch

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type searchFixture struct {
	rows        []Row
	allowed     map[string]bool
	calls       int
	revokeAfter int
}

func (s *searchFixture) Search(_ context.Context, q Request) ([]Row, error) {
	out := []Row{}
	for _, r := range s.rows {
		if q.BeforeKey != "" && (r.At.After(q.BeforeAt) || r.At.Equal(q.BeforeAt) && rowKey(r) >= q.BeforeKey) {
			continue
		}
		ok, err := s.CanOpen(context.Background(), q.Actor, r)
		if err != nil {
			return nil, err
		}
		if ok && Match(r, q) {
			out = append(out, r)
			if q.Limit > 0 && len(out) > q.Limit {
				break
			}
		}
	}
	return out, nil
}
func (s *searchFixture) CanOpen(_ context.Context, a Actor, r Row) (bool, error) {
	s.calls++
	return s.allowed[a.PersonID+":"+r.ID] && (s.revokeAfter == 0 || s.calls < s.revokeAfter), nil
}
func searchRequest() Request {
	return Request{Actor: Actor{"tenant", "tenant", "alice"}, Query: "budget", At: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Limit: 2}
}
func addFixture(t *testing.T, r *Registry, kind Kind, s Source) {
	t.Helper()
	d, _ := r.Declaration(kind)
	if e := r.Register(d, s); e != nil {
		t.Fatal(e)
	}
}
func resultIDs(r Response) []string {
	var ids []string
	for _, g := range r.Groups {
		if g.Count != len(g.Rows) {
			panic("dishonest page count")
		}
		for _, row := range g.Rows {
			ids = append(ids, row.ID)
		}
	}
	return ids
}

func TestTodo_CHATSEARCH_001(t *testing.T) {
	r := NewRegistry()
	q := searchRequest()
	s := &searchFixture{allowed: map[string]bool{"alice:one": true}, rows: []Row{{Kind: Message, ID: "one", TenantID: "tenant", Text: "Budget plan", At: q.At, Target: Target{ConversationID: "channel", MessageID: "one", Sequence: 42}}}}
	addFixture(t, r, Message, s)
	got, e := r.Search(context.Background(), q)
	if e != nil || !reflect.DeepEqual(resultIDs(got), []string{"one"}) || got.Groups[0].Rows[0].Target.Sequence != 42 {
		t.Fatalf("result=%+v %v", got, e)
	}
	if e = r.ValidateStoredKinds([]Kind{Message}); e != nil {
		t.Fatal(e)
	}
	if !errors.Is(r.ValidateStoredKinds([]Kind{"unregistered"}), ErrRegistry) {
		t.Fatal("unregistered stored kind accepted")
	}
}
func TestTodo_CHATSEARCH_001_Security(t *testing.T) {
	for _, d := range Declarations() {
		t.Run(string(d.Kind), func(t *testing.T) {
			q := searchRequest()
			r := NewRegistry()
			s := &searchFixture{allowed: map[string]bool{}}
			for _, id := range []string{"alice", "bob", "deleted", "removed", "expired", "foreign", "private"} {
				row := Row{Kind: d.Kind, ID: id, TenantID: "tenant", Text: "budget secret " + id, At: q.At, OwnerID: "bob"}
				switch id {
				case "deleted":
					row.Deleted = true
				case "removed":
					row.Removed = true
				case "expired":
					row.ExpiresAt = q.At
				case "foreign":
					row.TenantID = "elsewhere"
				case "private":
					row.Private = true
				}
				s.rows = append(s.rows, row)
				s.allowed["bob:"+id] = true
				if id != "bob" {
					s.allowed["alice:"+id] = true
				}
			}
			addFixture(t, r, d.Kind, s)
			got, e := r.Search(context.Background(), q)
			if e != nil || !reflect.DeepEqual(resultIDs(got), []string{"alice"}) {
				t.Fatalf("leaked result/count: %+v %v", got, e)
			}
			q.Query = "secret bob"
			got, e = r.Search(context.Background(), q)
			if e != nil || len(got.Groups) != 0 || got.NextCursor != "" || len(got.Unavailable) != 0 {
				t.Fatalf("hidden-content hint: %+v %v", got, e)
			}
			q.Actor.PersonID = "bob"
			q.Query = "budget"
			q.Limit = 50
			got, e = r.Search(context.Background(), q)
			if e != nil || len(resultIDs(got)) != 3 {
				t.Fatalf("bob's current rows: %+v %v", got, e)
			}
		})
	}
	q := searchRequest()
	r := NewRegistry()
	s := &searchFixture{rows: []Row{{Kind: Message, ID: "row", TenantID: "tenant", Text: "budget", At: q.At}}, allowed: map[string]bool{"alice:row": true}, revokeAfter: 2}
	addFixture(t, r, Message, s)
	got, e := r.Search(context.Background(), q)
	if e != nil || len(got.Groups) != 0 {
		t.Fatalf("revoked between reads: %+v %v", got, e)
	}
}
func TestTodo_CHATSEARCH_001_Property(t *testing.T) {
	q := searchRequest()
	q.Limit = 50
	rng := rand.New(rand.NewSource(42))
	for history := 0; history < 100; history++ {
		r := NewRegistry()
		s := &searchFixture{allowed: map[string]bool{}}
		membership, visibility, segment := make([]bool, 30), make([]bool, 30), make([]bool, 30)
		for i := 0; i < 30; i++ {
			id := fmt.Sprint(i)
			s.rows = append(s.rows, Row{Kind: Message, ID: id, TenantID: "tenant", Text: "budget", At: q.At})
			membership[i], visibility[i], segment[i] = true, true, true
		}
		addFixture(t, r, Message, s)
		for step := 0; step < 30; step++ {
			i := rng.Intn(30)
			switch rng.Intn(5) {
			case 0: // Join/leave the current audience.
				membership[i] = !membership[i]
			case 1:
				visibility[i] = !visibility[i]
			case 2:
				segment[i] = !segment[i]
			case 3:
				s.rows[i].Deleted = !s.rows[i].Deleted
			case 4:
				s.rows[i].Removed = !s.rows[i].Removed
			}
			var expected []string
			for j, row := range s.rows {
				allowed := membership[j] && visibility[j] && segment[j]
				s.allowed["alice:"+row.ID] = allowed
				if allowed && !row.Removed && !row.Deleted {
					expected = append(expected, row.ID)
				}
			}
			got, e := r.Search(context.Background(), q)
			ids := resultIDs(got)
			sort.Strings(ids)
			sort.Strings(expected)
			if e != nil || !reflect.DeepEqual(ids, expected) {
				t.Fatalf("history %d step %d: %+v %v expected %v", history, step, got, e, expected)
			}
		}

	}
}
func TestTodo_CHATSEARCH_001_Contract(t *testing.T) {
	want := []Kind{Message, Thread, Conversation, Person, File, Pin, Todo, Poll, AgentAnswer, SourceTitle, Voice, VoiceCorrection, Announcement, Reminder, PrivateTask, PrivateReminder, GateQuestion, GateAnswer, Saved, Location, GateDefinition, FilterDefinition, Moderation}
	got := []Kind{}
	r := NewRegistry()
	for _, d := range Declarations() {
		got = append(got, d.Kind)
		if d.Text == "" || d.Audience == "" || d.Opens == "" {
			t.Fatalf("missing facts: %+v", d)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registry changed: %v", got)
	}
	s := &searchFixture{}
	addFixture(t, r, Message, s)
	d, _ := r.Declaration(Message)
	if !errors.Is(r.Register(d, s), ErrRegistry) {
		t.Fatal("duplicate accepted")
	}
	if !errors.Is(r.Register(Declaration{Kind: "new"}, s), ErrRegistry) {
		t.Fatal("missing declaration accepted")
	}
	if _, e := (UnavailableMeaning{}).SearchMeaning(context.Background(), searchRequest()); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}
func TestTodo_CHATSEARCH_002(t *testing.T) {
	query := `budget in:#general from:"Full Name" has:file has:link before:2026-11-01 after:2026-09-01 on:2026-10-01 has:reactions is:thread mentions:me from:agent is:mine kind:thread`
	p, e := Parse(query)
	if e != nil || p.Text != "budget" || len(p.Chips) != 13 {
		t.Fatalf("parse %+v %v", p, e)
	}
	q := searchRequest()
	q.Filters = p.Filters
	row := Row{Kind: Thread, ID: "row", TenantID: "tenant", Text: "BUDGET plan", AuthorID: "Full Name", OwnerID: "alice", At: q.At, Target: Target{ConversationID: "general"}, HasFile: true, HasLink: true, HasReactions: true, InThread: true, MentionsMe: true, ByAgent: true}
	if !Match(row, q) {
		t.Fatal("combined filters did not match")
	}
	row.HasLink = false
	if Match(row, q) {
		t.Fatal("combined filter widened")
	}
	removed, e := RemoveChip(query, `from:Full Name`)
	if e != nil || strings.Contains(removed, "Full Name") {
		t.Fatalf("remove %q %v", removed, e)
	}
	for _, bad := range []string{`from:"unfinished`, `on:wrong`, `has:unknown`, `in:`, `in:a in:b`, `before:2026-01-01 after:2026-02-01`} {
		if _, e := Parse(bad); !errors.Is(e, ErrInvalid) {
			t.Fatalf("accepted %q: %v", bad, e)
		}
	}
	r := NewRecent()
	a := q.Actor
	b := a
	b.PersonID = "bob"
	for i := 0; i < 12; i++ {
		if e := r.Remember(a, fmt.Sprint(i)); e != nil {
			t.Fatal(e)
		}
	}
	if len(r.List(a)) != 10 || len(r.List(b)) != 0 {
		t.Fatal("history not bounded/private")
	}
	_ = r.Remember(a, "11")
	if len(r.List(a)) != 10 {
		t.Fatal("duplicate history")
	}
	r.Clear(b)
	if len(r.List(a)) != 10 {
		t.Fatal("foreign clear")
	}
	r.Clear(a)
	if len(r.List(a)) != 0 {
		t.Fatal("clear failed")
	}
}
func TestTodo_CHATSEARCH_002_Security(t *testing.T) {
	r := NewRegistry()
	q := searchRequest()
	s := &searchFixture{allowed: map[string]bool{}}
	for i := 0; i < 5; i++ {
		id := fmt.Sprint(i)
		s.rows = append(s.rows, Row{Kind: Message, ID: id, TenantID: "tenant", Text: "budget", At: q.At.Add(time.Duration(i) * time.Second)})
		s.allowed["alice:"+id] = true
	}
	addFixture(t, r, Message, s)
	sort.Slice(s.rows, func(i, j int) bool { return s.rows[i].At.After(s.rows[j].At) })
	first, e := r.Search(context.Background(), q)
	if e != nil || first.NextCursor == "" || len(resultIDs(first)) != 2 {
		t.Fatalf("first %+v %v", first, e)
	}
	q.Cursor = first.NextCursor
	second, e := r.Search(context.Background(), q)
	if e != nil || !reflect.DeepEqual(resultIDs(second), []string{"2", "1"}) {
		t.Fatalf("second %+v %v", second, e)
	}
	q.Actor.PersonID = "bob"
	if _, e = r.Search(context.Background(), q); !errors.Is(e, ErrInvalid) {
		t.Fatal("cross-person cursor accepted")
	}
	q = searchRequest()
	q.Mode = "meaning"
	// No meaning index is installed: the request answers from keywords, quietly.
	if got, e := r.Search(context.Background(), q); e != nil || got.Mode != "keyword" {
		t.Fatalf("meaning mode did not degrade to keywords: %+v %v", got, e)
	}
}
func TestTodo_CHATSEARCH_002_Performance(t *testing.T) {
	r := NewRegistry()
	q := searchRequest()
	s := &searchFixture{allowed: map[string]bool{}}
	for i := 0; i < 100000; i++ {
		id := fmt.Sprint(i)
		text := "budget"
		s.rows = append(s.rows, Row{Kind: Message, ID: id, TenantID: "tenant", Text: text, At: q.At})
		s.allowed["alice:"+id] = true
	}
	addFixture(t, r, Message, s)
	sort.Slice(s.rows, func(i, j int) bool { return rowKey(s.rows[i]) > rowKey(s.rows[j]) })
	start := time.Now()
	got, e := r.Search(context.Background(), q)
	if e != nil || len(resultIDs(got)) != 2 {
		t.Fatalf("result %+v %v", got, e)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("fixture search exceeded budget: %v", elapsed)
	}
}
