package application

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type chatcmd003FixtureModel func(context.Context, chatrewrite.Prompt) (string, error)

func (f chatcmd003FixtureModel) Rewrite(ctx context.Context, p chatrewrite.Prompt) (string, error) {
	return f(ctx, p)
}
func chatcmd003AppDraft(t *testing.T) chat.Chatcmd003Draft {
	t.Helper()
	d, err := chat.Chatcmd003ParsePoll(`"where?" 1="here" 2="there" multiple=yes anonymous=yes`, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), chat.Chatcmd004ResolveDate)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestTodo_CHATCMD_003(t *testing.T) {
	s, ctx, _, _, _, ledger := chattoneFixture(t)
	d := chatcmd003AppDraft(t)
	calls := 0
	s.Rewrite.Model = chatcmd003FixtureModel(func(_ context.Context, p chatrewrite.Prompt) (string, error) {
		calls++
		if p.Identity.Person != "person" || p.Identity.Tenant != "tenant" || p.TaskProfile != chatrewrite.TaskProfileID || !strings.HasPrefix(p.Data, "<untrusted_data>\n") {
			t.Error("governed identity/data missing")
		}
		return `{"title":"Where?","items":[{"source":0,"text":"Here"},{"source":1,"text":"There"}]}`, nil
	})
	result, err := s.Chatcmd003TidyDraft(ctx, Chatcmd003TidyRequest{Conversation: "room", Draft: d})
	if err != nil || calls != 1 || result.Card.Title != "Where?" || len(result.Changes) != 3 || !result.Card.Poll.Multiple || !result.Card.Poll.Anonymous || d.Card.Title != "where?" || !reflect.DeepEqual(result.Original, d.Original) {
		t.Fatalf("preview %+v calls %d err %v", result, calls, err)
	}
	lines := ledger.Lines()
	if len(lines) != 1 || lines[0].Operation != "card-tidy" || !lines[0].Succeeded {
		t.Fatalf("usage %+v", lines)
	}
}
func TestTodo_CHATCMD_003_Property(t *testing.T) {
	t.Run("loose settings retained", func(t *testing.T) {
		d, err := chat.Chatcmd003ParsePoll("where should we go lisbon, porto or stay remote? multiple=yes anonymous=yes closes=friday", time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), chat.Chatcmd004ResolveDate)
		if err != nil {
			t.Fatal(err)
		}
		out, err := chatcmd003ApplyLoose(d, chatcmd003TidyOutput{Title: "Where should we go?", Items: []chatcmd003TidyItem{{0, "Lisbon"}, {1, "Porto"}, {2, "Stay remote"}}})
		if err != nil || !out.Card.Poll.Multiple || !out.Card.Poll.Anonymous || out.Card.Poll.ClosesAt == nil || !out.Card.Poll.ClosesAt.Equal(*d.Card.Poll.ClosesAt) {
			t.Fatalf("settings lost %+v %v", out, err)
		}
	})
	d := chatcmd003AppDraft(t)
	out := chatcmd003TidyOutput{Title: "Where?", Items: []chatcmd003TidyItem{{1, "There"}, {0, "Here"}}}
	result, err := chatcmd003ApplyTidy(d, out)
	if err != nil || result.Card.Poll.Options[0].Text != "There" || d.Card.Poll.Options[0].Text != "here" {
		t.Fatalf("reorder %+v %v", result, err)
	}
	duplicate, _ := chat.Chatcmd003ParsePoll(`"Q?" 1="A" 2="a" 3="B"`, time.Now(), nil)
	merged, err := chatcmd003ApplyTidy(duplicate, chatcmd003TidyOutput{Title: "Q?", Items: []chatcmd003TidyItem{{0, "A"}, {2, "B"}}})
	if err != nil || len(merged.Card.Poll.Options) != 2 || len(merged.Changes) == 0 {
		t.Fatalf("duplicate %+v %v", merged, err)
	}
	for _, pair := range [][2]string{{"teh room", "The room"}, {"Recieve invites", "Receive invites"}, {"Keep 1.5", "Keep 1.5"}, {"@Dana by 2026-10-02", "@Dana by 2026-10-02"}} {
		if !chatcmd003SameMeaning(pair[0], pair[1]) {
			t.Errorf("safe wording rejected %q", pair)
		}
	}
}
func TestTodo_CHATCMD_003_Security(t *testing.T) {
	t.Run("currency is distinct meaning", func(t *testing.T) {
		d, _ := chat.Chatcmd003ParsePoll(`"Q?" 1="Pay $10" 2="Pay 10" 3="Pay $20"`, time.Now(), nil)
		_, err := chatcmd003ApplyTidy(d, chatcmd003TidyOutput{Title: "Q?", Items: []chatcmd003TidyItem{{1, "Pay 10"}, {2, "Pay $20"}}})
		if !errors.Is(err, chatrewrite.ErrPreservation) {
			t.Fatal("distinct currency option merged")
		}
	})
	t.Run("HTTP admission", chatcmd003HTTP)
	d := chatcmd003AppDraft(t)
	invalid := []chatcmd003TidyOutput{
		{"Where?", []chatcmd003TidyItem{{0, "Here"}}},
		{"Where?", []chatcmd003TidyItem{{0, "Here"}, {1, "New option"}}},
		{"Where?", []chatcmd003TidyItem{{0, "Here"}, {0, "Here"}}},
		{"A different question?", []chatcmd003TidyItem{{0, "Here"}, {1, "There"}}},
	}
	for _, out := range invalid {
		if _, err := chatcmd003ApplyTidy(d, out); !errors.Is(err, chatrewrite.ErrPreservation) {
			t.Fatalf("unsafe output %+v %v", out, err)
		}
	}
	for _, pair := range [][2]string{{"No Paris", "Paris"}, {"Keep 1.5", "Keep 1 5"}, {"Pay $10", "Pay 10"}, {"@Dana", "@Omar"}, {"Keep https://example.com/a", "Keep https example com a"}} {
		if chatcmd003SameMeaning(pair[0], pair[1]) {
			t.Errorf("literal/meaning changed %q", pair)
		}
	}
	s, ctx, _, _, auth, ledger := chattoneFixture(t)
	calls := 0
	s.Rewrite.Model = chatcmd003FixtureModel(func(context.Context, chatrewrite.Prompt) (string, error) { calls++; return "", nil })
	auth.denied = true
	if _, err := s.Chatcmd003TidyDraft(ctx, Chatcmd003TidyRequest{Conversation: "room", Draft: d}); err == nil || calls != 0 || len(ledger.Lines()) != 0 {
		t.Fatal("denied request spent budget or reached model")
	}
	auth.denied = false
	_ = s.Rewrite.Registry.Configure("tenant", false, chatrewrite.DefaultStyles())
	if _, err := s.Chatcmd003TidyDraft(ctx, Chatcmd003TidyRequest{Conversation: "room", Draft: d}); !errors.Is(err, chatrewrite.ErrDisabled) || calls != 0 {
		t.Fatal("disabled workspace reached model")
	}
}
func TestTodo_CHATCMD_003_Golden(t *testing.T) {
	t.Run("twenty explicit polls", chatcmd003GoldenCorpus)
	s, ctx, _, _, _, _ := chattoneFixture(t)
	d, err := chat.Chatcmd003ParsePoll("where should we go lisbon, porto or stay remote?", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Rewrite.Model = chatcmd003FixtureModel(func(context.Context, chatrewrite.Prompt) (string, error) {
		return `{"title":"Where should we go?","items":[{"source":0,"text":"Lisbon"},{"source":1,"text":"Porto"},{"source":2,"text":"Stay remote"}]}`, nil
	})
	result, err := s.Chatcmd003TidyDraft(ctx, Chatcmd003TidyRequest{Conversation: "room", Draft: d})
	if err != nil || result.Card.Title != "Where should we go?" || len(result.Card.Poll.Options) != 3 || len(result.Issues) != 0 {
		t.Fatalf("loose %+v %v", result, err)
	}
	bytes, _ := json.Marshal(result.Card.Poll.Options)
	if string(bytes) != `[{"ID":"","Text":"Lisbon","Count":0},{"ID":"","Text":"Porto","Count":0},{"ID":"","Text":"Stay remote","Count":0}]` {
		t.Fatalf("golden options %s", bytes)
	}
	if _, err := chatcmd003ApplyLoose(d, chatcmd003TidyOutput{Title: "Where should we go?", Items: []chatcmd003TidyItem{{0, "Lisbon"}, {1, "Paris"}, {2, "Stay remote"}}}); !errors.Is(err, chatrewrite.ErrPreservation) {
		t.Fatalf("invented loose option %v", err)
	}
}
func TestTodo_CHATCMD_004(t *testing.T) {
	s, ctx, _, _, _, _ := chattoneFixture(t)
	d, err := chat.Chatcmd004ParseTodo(`"launch" 1="send invites @Dana by friday"`, []chat.Chatcmd004Member{{HomeTenantID: "tenant", ID: "dana", Name: "Dana"}}, "person", time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), chat.Chatcmd004ResolveDate)
	if err != nil {
		t.Fatal(err)
	}
	s.Rewrite.Model = chatcmd003FixtureModel(func(context.Context, chatrewrite.Prompt) (string, error) {
		return `{"title":"Launch","items":[{"source":0,"text":"Send invites"}]}`, nil
	})
	out, err := s.Chatcmd003TidyDraft(ctx, Chatcmd003TidyRequest{Conversation: "room", Draft: d})
	if err != nil || out.Card.Todo.Items[0].AssigneeID != "dana" || !out.Card.Todo.Items[0].DueAt.Equal(*d.Card.Todo.Items[0].DueAt) {
		t.Fatalf("metadata %+v %v", out, err)
	}
}
func TestTodo_CHATCMD_004_Security(t *testing.T) {
	s, ctx, _, _, _, _ := chattoneFixture(t)
	s.Rewrite.Ledger = chatrewrite.NewMemoryLedger(1)
	calls := 0
	s.Rewrite.Model = chatcmd003FixtureModel(func(context.Context, chatrewrite.Prompt) (string, error) {
		calls++
		return `{"title":"Where?","items":[{"source":0,"text":"Here"},{"source":1,"text":"There"}]}`, nil
	})
	in := Chatcmd003TidyRequest{Conversation: "room", Draft: chatcmd003AppDraft(t)}
	if _, err := s.Chatcmd003TidyDraft(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chatcmd003TidyDraft(ctx, in); !errors.Is(err, chatrewrite.ErrLimit) || calls != 1 {
		t.Fatalf("budget bypass calls %d %v", calls, err)
	}
	s.Rewrite.Ledger = chatrewrite.NewMemoryLedger(1)
	s.Rewrite.Model = chatcmd003FixtureModel(func(ctx context.Context, _ chatrewrite.Prompt) (string, error) { <-ctx.Done(); return "", ctx.Err() })
	if out, err := s.Chatcmd003TidyDraft(ctx, in); !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(out, in.Draft) {
		t.Fatalf("timeout lost draft %+v %v", out, err)
	}
}
func chatcmd003HTTP(t *testing.T) {
	called := 0
	fallback := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called++; w.WriteHeader(http.StatusNoContent) })
	h := Chatcmd003OverlayTidy(fallback, nil, transport.Config{})
	other := httptest.NewRecorder()
	h.ServeHTTP(other, httptest.NewRequest(http.MethodGet, "/other", nil))
	if other.Code != 204 || called != 1 {
		t.Fatal("overlay swallowed unrelated path")
	}
	wrong := httptest.NewRecorder()
	h.ServeHTTP(wrong, httptest.NewRequest(http.MethodGet, Chatcmd003TidyPath, nil))
	if wrong.Code != 405 || wrong.Header().Get("Allow") != "POST" {
		t.Fatal("tidy accepted read as action")
	}
	denied := httptest.NewRecorder()
	h.ServeHTTP(denied, httptest.NewRequest(http.MethodPost, Chatcmd003TidyPath, strings.NewReader(`{}`)))
	if denied.Code != 401 || denied.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("admission %d", denied.Code)
	}
}

func chatcmd003GoldenCorpus(t *testing.T) {
	cases := []struct {
		raw, title string
		items      []string
		valid      bool
	}{
		{`"where?" 1="here" 2="there"`, "Where?", []string{"Here", "There"}, true},
		{`"offsite?" 1="Lisbon" 2="Porto"`, "Offsite?", []string{"Lisbon", "Porto"}, true},
		{`"where?" 1="A ""quoted"" option" 2="B"`, "Where?", []string{`A "quoted" option`, "B"}, true},
		{`"where = next?" 1="a=b" 2="c=d"`, "Where = next?", []string{"a=b", "c=d"}, true},
		{`"where?" 1="🥳" 2="🙂"`, "Where?", []string{"🥳", "🙂"}, true},
		{`"أين؟" 1="هنا" 2="هناك"`, "أين؟", []string{"هنا", "هناك"}, true},
		{`"wohin?" 1="Köln" 2="München"`, "Wohin?", []string{"Köln", "München"}, true},
		{`"who?" 1="@Dana" 2="@Omar"`, "Who?", []string{"@Dana", "@Omar"}, true},
		{`"where?" 8="B" 3="A"`, "Where?", []string{"A", "B"}, true},
		{`"where?" 1="here" 2="there" multiple=yes`, "Where?", []string{"Here", "There"}, true},
		{`"where?" 1="here" 2="there" anonymous=yes`, "Where?", []string{"Here", "There"}, true},
		{`"where?" 1="here" 2="there" results=after-voting`, "Where?", []string{"Here", "There"}, true},
		{`"where?" 1="here" 2="there" results=after-closing`, "Where?", []string{"Here", "There"}, true},
		{`"where?" 1="here" 2="there" closes=friday`, "Where?", []string{"Here", "There"}, true},
		{`"where?" 1="here" 2="there" add=members`, "Where?", []string{"Here", "There"}, true},
		{`"where?" 1="recieve invites" 2="book teh room"`, "Where?", []string{"Receive invites", "Book the room"}, true},
		{`"where?" 1="One"`, "Where?", []string{"One"}, false},
		{`"where?" 1="" 2="B"`, "Where?", []string{"", "B"}, false},
		{`"where?" 1="A" 2="B" 3="C" 4="D" 5="E" 6="F" 7="G" 8="H" 9="I" 10="J" 11="K" 12="L" 13="M"`, "Where?", []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M"}, false},
		{`"price?" 1="Pay $10" 2="Pay €15"`, "Price?", []string{"Pay $10", "Pay €15"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			d, err := chat.Chatcmd003ParsePoll(tc.raw, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), chat.Chatcmd004ResolveDate)
			if err != nil {
				t.Fatal(err)
			}
			out := chatcmd003TidyOutput{Title: tc.title}
			for i, text := range tc.items {
				out.Items = append(out.Items, chatcmd003TidyItem{Source: i, Text: text})
			}
			wire, _ := json.Marshal(out)
			service, ctx, _, _, _, ledger := chattoneFixture(t)
			calls := 0
			service.Rewrite.Model = chatcmd003FixtureModel(func(context.Context, chatrewrite.Prompt) (string, error) { calls++; return string(wire), nil })
			result, err := service.Chatcmd003TidyDraft(ctx, Chatcmd003TidyRequest{Conversation: "room", Draft: d})
			if !tc.valid {
				if !errors.Is(err, chatrewrite.ErrPreservation) {
					t.Fatalf("invalid draft accepted %v", err)
				}
				return
			}
			want := tc.title + "\n" + strings.Join(tc.items, "\n")
			if err != nil || result.Card.SearchText() != want || calls != 1 || len(ledger.Lines()) != 1 {
				t.Fatalf("golden %q expected %q calls %d error %v", result.Card.SearchText(), want, calls, err)
			}
		})
	}
}
