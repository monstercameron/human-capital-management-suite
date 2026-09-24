package chatapps

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

type chat040FailGetRepository struct {
	*MemoryRepository
	err    error
	writes int
}

func (r *chat040FailGetRepository) Get(context.Context, string) (Installation, error) {
	return Installation{}, r.err
}

func (r *chat040FailGetRepository) Put(ctx context.Context, v Installation) error {
	r.writes++
	return r.MemoryRepository.Put(ctx, v)
}

func TestTodo_CHAT_040_Security(t *testing.T) {
	s, actor := fixture()
	ctx := context.Background()
	initial, err := s.Repo.Get(ctx, "t1:c1:app")
	if err != nil {
		t.Fatal(err)
	}

	manifest := initial.Manifest
	manifest.Version = 2
	manifest.Scopes = append(manifest.Scopes, "hcm:write")
	requested := []string{"chat:invoke", "hcm:write"}
	if _, err := s.Upgrade(ctx, actor, manifest, requested, "another-manager"); !errors.Is(err, ErrDenied) {
		t.Fatalf("upgrade accepted review attributed to another manager: %v", err)
	}
	unchanged, err := s.Repo.Get(ctx, initial.ID)
	if err != nil || unchanged.Revision != initial.Revision || unchanged.Version != initial.Version || !slices.Equal(unchanged.GrantedScopes, initial.GrantedScopes) {
		t.Fatalf("mismatched reviewer changed installation: %+v err=%v", unchanged, err)
	}

	upgraded, err := s.Upgrade(ctx, actor, manifest, requested, actor.Principal)
	if err != nil {
		t.Fatalf("authorized review of exact manifest and grants failed: %v", err)
	}
	if upgraded.Version != manifest.Version || upgraded.Manifest.Version != manifest.Version || upgraded.Approver != actor.Principal || !slices.Equal(upgraded.Manifest.Scopes, manifest.Scopes) || !slices.Equal(upgraded.GrantedScopes, requested) {
		t.Fatalf("persisted upgrade does not match reviewed manifest and grant: %+v", upgraded)
	}

	suspended, err := s.ChangeStatus(ctx, actor, initial.ID, Suspended)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Version = 3
	if _, err := s.Upgrade(ctx, actor, manifest, requested, actor.Principal); !errors.Is(err, ErrSuspended) {
		t.Fatalf("upgrade implicitly resumed suspended installation: %v", err)
	}
	after, err := s.Repo.Get(ctx, initial.ID)
	if err != nil || after.Status != Suspended || after.Version != suspended.Version || after.Revision != suspended.Revision {
		t.Fatalf("rejected upgrade changed suspended installation: %+v err=%v", after, err)
	}
}

func TestTodo_CHAT_040_Golden(t *testing.T) {
	s := &Service{Repo: NewMemoryRepository(), Authority: authorityStub{}}
	actor := Actor{Tenant: "tenant-a", Principal: "owner-7", Conversation: "room-2"}
	manifest := Manifest{
		AppID:    "leave-request",
		Version:  2,
		Scopes:   []string{"chat:invoke", "hcm:leave:request"},
		Commands: []Command{{Name: "request", Scope: "hcm:leave:request"}},
	}
	installed, err := s.Install(context.Background(), actor, manifest, []string{"hcm:leave:request"}, actor.Principal)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Approver != actor.Principal || installed.Version != manifest.Version || !slices.Equal(installed.GrantedScopes, []string{"hcm:leave:request"}) {
		t.Fatalf("review binding or conversation grant changed: %+v", installed)
	}
	encoded, err := json.Marshal(installed.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"app_id":"leave-request","version":2,"scopes":["chat:invoke","hcm:leave:request"],"commands":[{"Name":"request","Scope":"hcm:leave:request","Arguments":null,"Risk":""}],"cards":null,"tabs":null,"callback_origins":null}`
	if string(encoded) != want {
		t.Fatalf("reviewed manifest bytes changed:\n got %s\nwant %s", encoded, want)
	}
}

func TestTodo_CHAT_040_SecurityRepositoryReadFailure(t *testing.T) {
	storeErr := errors.New("installation store unavailable")
	repo := &chat040FailGetRepository{MemoryRepository: NewMemoryRepository(), err: storeErr}
	s := &Service{Repo: repo, Authority: authorityStub{}}
	actor := Actor{Tenant: "tenant-a", Principal: "owner-7", Conversation: "room-2"}
	manifest := Manifest{AppID: "leave-request", Version: 1}
	if _, err := s.Install(context.Background(), actor, manifest, nil, actor.Principal); !errors.Is(err, storeErr) {
		t.Fatalf("installation store failure was treated as first install: %v", err)
	}
	if repo.writes != 0 {
		t.Fatalf("installation wrote after repository read failure: writes=%d", repo.writes)
	}
}
