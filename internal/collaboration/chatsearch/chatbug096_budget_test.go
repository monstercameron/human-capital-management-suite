package chatsearch

import (
	"context"
	"testing"
	"time"
)

// rendezvousSource answers only once its peer has started too: a registry
// that asked its sources one after another would never get an answer.
type rendezvousSource struct {
	kind    Kind
	row     Row
	entered chan<- Kind
	release <-chan struct{}
}

func (s *rendezvousSource) Search(ctx context.Context, q Request) ([]Row, error) {
	select {
	case s.entered <- s.kind:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-s.release:
		if !Match(s.row, q) {
			return nil, nil
		}
		return []Row{s.row}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *rendezvousSource) CanOpen(context.Context, Actor, Row) (bool, error) {
	return true, nil
}

// TestTodo_CHATBUG_096_SideBySide: the registry asks its sources side by
// side, so one slow source never delays the others.
func TestTodo_CHATBUG_096_SideBySide(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	entered := make(chan Kind, 2)
	release := make(chan struct{})
	go func() {
		kinds := map[Kind]bool{}
		for kind := range entered {
			kinds[kind] = true
			if len(kinds) == 2 {
				close(release)
				return
			}
		}
	}()
	r := NewRegistry()
	for _, kind := range []Kind{Message, Thread} {
		d, _ := r.Declaration(kind)
		row := Row{Kind: kind, ID: string(kind) + "-a", TenantID: "tenant", Text: "Standup notes", At: at}
		if err := r.Register(d, &rendezvousSource{kind: kind, row: row, entered: entered, release: release}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := r.Search(ctx, Request{Actor: Actor{"tenant", "tenant", "alice"}, Query: "standup", At: at, Limit: 16, Transient: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[Kind]bool{}
	for _, g := range got.Groups {
		seen[g.Kind] = len(g.Rows) == 1
	}
	if !seen[Message] || !seen[Thread] {
		t.Fatalf("the sources were not asked side by side: %+v", got.Groups)
	}
}

// stallSource never answers before the budget ends.
type stallSource struct{ kind Kind }

func (s *stallSource) Search(ctx context.Context, _ Request) ([]Row, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *stallSource) CanOpen(context.Context, Actor, Row) (bool, error) {
	return true, nil
}

// TestTodo_CHATBUG_096_SlowSource: a source that outlasts its budget is
// reported unavailable while the others answer.
func TestTodo_CHATBUG_096_SlowSource(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	r := NewRegistry()
	r.SourceTimeout = 100 * time.Millisecond
	d, _ := r.Declaration(Thread)
	if err := r.Register(d, &stallSource{kind: Thread}); err != nil {
		t.Fatal(err)
	}
	s := &searchFixture{allowed: map[string]bool{"alice:one": true}, rows: []Row{{Kind: Message, ID: "one", TenantID: "tenant", Text: "Standup notes", At: at}}}
	addFixture(t, r, Message, s)
	got, err := r.Search(context.Background(), Request{Actor: Actor{"tenant", "tenant", "alice"}, Query: "standup", At: at, Limit: 16, Transient: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unavailable) != 1 || got.Unavailable[0] != Thread {
		t.Fatalf("the stalled source was not reported: %+v", got.Unavailable)
	}
	if len(got.Groups) != 1 || len(got.Groups[0].Rows) != 1 || got.Groups[0].Rows[0].ID != "one" {
		t.Fatalf("the answering source did not answer: %+v", got.Groups)
	}
}

// TestTodo_CHATBUG_096: a workspace-box search returns a flood of messages and
// starves every other kind. A transient reply rounds each source to its
// budget (2 messages, 3 threads, 2 documents, 1 person, 1 action) so the box
// can show a diverse few; a Chat search page is untouched, and a search
// filtered to one kind still pages through the whole kind.
func TestTodo_CHATBUG_096(t *testing.T) {
	seed := func(r *Registry, kinds map[Kind]int, at time.Time) {
		t.Helper()
		i := 0
		for _, kind := range []Kind{Message, Thread, File, Person, Todo} {
			s := &searchFixture{allowed: map[string]bool{}}
			for n := 0; n < kinds[kind]; n++ {
				id := string(kind) + "-" + string(rune('a'+n))
				s.rows = append(s.rows, Row{Kind: kind, ID: id, TenantID: "tenant", Text: "Standup notes " + id, At: at.Add(-time.Duration(i) * time.Second)})
				s.allowed["alice:"+id] = true
				i++
			}
			addFixture(t, r, kind, s)
		}
	}
	counts := func(t *testing.T, got Response) map[Kind]int {
		t.Helper()
		out := map[Kind]int{}
		for _, g := range got.Groups {
			if g.Count != len(g.Rows) {
				t.Fatalf("dishonest count for %s: %d for %d rows", g.Kind, g.Count, len(g.Rows))
			}
			out[g.Kind] = len(g.Rows)
		}
		return out
	}
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	kinds := map[Kind]int{Message: 5, Thread: 5, File: 3, Person: 2, Todo: 2}

	r := NewRegistry()
	seed(r, kinds, at)
	got, err := r.Search(context.Background(), Request{Actor: Actor{"tenant", "tenant", "alice"}, Query: "standup", At: at, Limit: 16, Transient: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[Kind]int{Message: 2, Thread: 3, File: 2, Person: 1, Todo: 1}
	for kind, n := range want {
		if counts(t, got)[kind] != n {
			t.Fatalf("transient %s rows = %d, want the budget of %d: %+v", kind, counts(t, got)[kind], n, got.Groups)
		}
	}

	// A Chat search page keeps the whole page, and a kind filter pages through
	// its kind past the budget.
	r = NewRegistry()
	seed(r, kinds, at)
	got, err = r.Search(context.Background(), Request{Actor: Actor{"tenant", "tenant", "alice"}, Query: "standup", At: at, Limit: 16})
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range counts(t, got) {
		total += n
	}
	if total != 16 || got.NextCursor == "" {
		t.Fatalf("a Chat page holds %d rows, want the full page of 16: %+v", total, got.Groups)
	}
	r = NewRegistry()
	seed(r, kinds, at)
	got, err = r.Search(context.Background(), Request{Actor: Actor{"tenant", "tenant", "alice"}, Query: "standup", Filters: Filters{Kind: Message}, At: at, Limit: 16, Transient: true})
	if err != nil {
		t.Fatal(err)
	}
	if counts(t, got)[Message] != 5 {
		t.Fatalf("a filtered transient search holds %d messages, want all 5: %+v", counts(t, got)[Message], got.Groups)
	}
}
