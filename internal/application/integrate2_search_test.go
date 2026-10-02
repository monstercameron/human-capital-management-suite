package application

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func TestIntegrate2ServedFilterSearch(t *testing.T) {
	store := &chatfilterHTTPStore{defs: []chatfilter.Definition{{ID: "project", Version: "1.0.0", Name: "Project rule", Kind: "words", Action: "block"}}}
	service := &chatfilter.Service{Store: store, Registry: chatfilter.NewRegistry(), Authority: chatfilterHTTPAuthority{}}
	registry := chatsearch.NewRegistry()
	if err := integrate2RegisterFilters(registry, service); err != nil {
		t.Fatal(err)
	}
	admission, bearer := integrate1Admission(t, "tenant-a", "alice", time.Now)
	handler := (&agentServedAssembly{ChatSearch: ChatSearchHTTP{Port: registry}}).Overlay(nil, admission)
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", ChatSearchPath, strings.NewReader(`{"Query":"Project kind:filter"}`))
		r.Header.Set("Authorization", bearer)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := request()
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Project rule") {
		t.Fatal(w.Code, w.Body)
	}
	service.Authority = chatfilterHTTPAuthority{denied: true}
	w = request()
	if w.Code != 200 || strings.Contains(w.Body.String(), "Project rule") {
		t.Fatal("denied filter name leaked", w.Code, w.Body)
	}
}

type integrate2SearchFilterStore struct{ chatfilterHTTPStore }

func (s *integrate2SearchFilterStore) Enablements(context.Context, string) ([]chatfilter.Enablement, error) {
	return []chatfilter.Enablement{{RuleID: "rule", Enabled: true}}, nil
}
func TestIntegrate2VoiceSearchMasksBeforeMatch(t *testing.T) {
	reader := &integrate2ReaderFixture{post: chat.Post{ID: "post", TenantID: "tenant-a", ConversationID: "room", AuthorID: "bob", AuthorHomeTenantID: "tenant-a", Body: "Voice", Revision: 1}}
	row := chatsearch.Row{Kind: chatsearch.Voice, ID: "voice", TenantID: "tenant-a", Text: "quartz", Target: chatsearch.Target{ConversationID: "room", MessageID: "post"}}
	base := chatsearch.SourceFuncs{OpenRow: func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }, SearchRows: func(_ context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
		if q.Query != "" {
			t.Fatal("matched before masking")
		}
		return []chatsearch.Row{row}, nil
	}}
	store := &integrate2SearchFilterStore{chatfilterHTTPStore: chatfilterHTTPStore{defs: []chatfilter.Definition{{ID: "rule", Version: "1.0.0", Name: "Rule", Kind: "words", Match: []string{"quartz"}, Action: "mask"}}}}
	policy := &chat.FilterContentPolicy{Filters: &chatfilter.Service{Store: store, Registry: chatfilter.NewRegistry()}}
	source := integrate2VoiceSource{Source: base, Reader: reader, Filter: policy}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "alice"}, Query: "quartz", At: time.Now()}
	if rows, err := source.Search(t.Context(), q); err != nil || len(rows) != 0 {
		t.Fatal("masked transcript matched authored secret", rows, err)
	}
	q.Query = "removed"
	rows, err := source.Search(t.Context(), q)
	if err != nil || len(rows) != 1 || strings.Contains(rows[0].Text, "quartz") {
		t.Fatal(rows, err)
	}
	if allowed, err := source.CanOpen(t.Context(), q.Actor, rows[0]); err != nil || !allowed {
		t.Fatal(allowed, err)
	}
	reader.denied = true
	if rows, err := source.Search(t.Context(), q); err != nil || len(rows) != 0 {
		t.Fatal("revoked reader saw transcript", rows, err)
	}
	registry := chatsearch.NewRegistry()
	if err := integrate2RegisterVoice(registry, nil, reader, policy); err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateStoredKinds([]chatsearch.Kind{chatsearch.Voice, chatsearch.VoiceCorrection}); err != nil {
		t.Fatal("voice sources not composed", err)
	}
}
