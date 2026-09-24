package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

type fakeChatInstallations map[string]chatapps.Installation

func (f fakeChatInstallations) Get(_ context.Context, id string) (chatapps.Installation, error) {
	v, ok := f[id]
	if !ok {
		return chatapps.Installation{}, chatapps.ErrNotFound
	}
	return v, nil
}

type recordingFilteredSearcher struct {
	calls  int
	reader string
}

func (r *recordingFilteredSearcher) SearchLexicalFiltered(_ context.Context, _, _, _, readerID string, _ documenthubstore.SearchFilters) (documenthubstore.FilteredSearchResult, error) {
	r.calls++
	r.reader = readerID
	return documenthubstore.FilteredSearchResult{Hits: []documenthubstore.FilteredSearchHit{{DocumentID: "doc-1", VersionID: "v-3", Status: "DEPLOYED"}}, Total: 1}, nil
}

// TestDocumentAgentChatInstallationAuthority proves the HUB-031 agent search
// is admitted by the same chat installation chat itself admits the agent with,
// and that every non-current installation is refused before the store is read.
func TestDocumentAgentChatInstallationAuthority(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	current := chatapps.Installation{Tenant: "t1", Conversation: "c1", AppID: "agent-1", Version: 1, Revision: 2, Status: chatapps.Active,
		GrantedScopes: []string{"chat.posts.read", DocumentSearchScope}, CreatedAt: now.Add(-time.Hour)}
	with := func(mut func(*chatapps.Installation)) fakeChatInstallations {
		v := current
		mut(&v)
		return fakeChatInstallations{"t1:c1:agent-1": v}
	}

	store := &recordingFilteredSearcher{}
	svc := newDocumentAgentService(store, newChatInstallationSearchAuthority(with(func(*chatapps.Installation) {}), "c1", clock))
	res, err := svc.AgentSearchDocuments(context.Background(), "t1", "agent-1", "u-requester", "leave", DocumentSearchFilters{})
	if err != nil {
		t.Fatalf("current installation refused: %v", err)
	}
	if store.calls != 1 || store.reader != "u-requester" || len(res.Hits) != 1 || res.Hits[0].VersionID != "v-3" {
		t.Fatalf("calls=%d reader=%q hits=%+v, want the requester's own grants and the deployed citation", store.calls, store.reader, res.Hits)
	}

	refusals := map[string]fakeChatInstallations{
		"revoked":          with(func(v *chatapps.Installation) { v.Status = chatapps.Revoked }),
		"suspended":        with(func(v *chatapps.Installation) { v.Status = chatapps.Suspended }),
		"foreign tenant":   with(func(v *chatapps.Installation) { v.Tenant = "t2" }),
		"other app":        with(func(v *chatapps.Installation) { v.AppID = "agent-2" }),
		"other room":       with(func(v *chatapps.Installation) { v.Conversation = "c2" }),
		"unrevised":        with(func(v *chatapps.Installation) { v.Revision = 0 }),
		"future":           with(func(v *chatapps.Installation) { v.CreatedAt = now.Add(time.Hour) }),
		"not installed":    {},
		"no search scope":  with(func(v *chatapps.Installation) { v.GrantedScopes = []string{"chat.posts.read"} }),
		"other room scope": nil,
	}
	for name, apps := range refusals {
		store := &recordingFilteredSearcher{}
		conversation := "c1"
		if name == "other room scope" {
			apps, conversation = with(func(*chatapps.Installation) {}), "c9"
		}
		svc := newDocumentAgentService(store, newChatInstallationSearchAuthority(apps, conversation, clock))
		_, err := svc.AgentSearchDocuments(context.Background(), "t1", "agent-1", "u-requester", "leave", DocumentSearchFilters{})
		want := ErrAgentNotInstalled
		if name == "no search scope" {
			want = ErrAgentSearchEscalation
		}
		if !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", name, err, want)
		}
		if store.calls != 0 {
			t.Errorf("%s: the store was read %d times before refusal", name, store.calls)
		}
	}

	if _, err := newChatInstallationSearchAuthority(nil, "c1", clock).InstalledScopes(context.Background(), "t1", "agent-1"); !errors.Is(err, ErrAgentNotInstalled) {
		t.Fatalf("nil installation store = %v, want not installed", err)
	}
}
