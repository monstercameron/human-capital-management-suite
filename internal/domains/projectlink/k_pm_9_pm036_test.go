package projectlink

import (
	"context"
	"testing"
)

func TestTodo_PM_036_Conformance(t *testing.T) {
	resolver := Resolver{
		Authorization: &fakeAuthorization{allowed: []bool{true, false}},
		Targets:       &fakeTargets{found: true, preview: Preview{Kind: DeployedDocument, ID: "doc-1", Version: "v-2", ScopeID: "team-a", Title: "private"}},
	}
	got, err := resolver.Resolve(context.Background(), "person-1", Reference{Kind: DeployedDocument, ID: "doc-1", Version: "v-2", ScopeID: "team-a"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != Restricted || got.Preview != nil {
		t.Fatalf("revoked link returned a preview: %#v", got)
	}
}

func TestTodo_PM_036_Security(t *testing.T) {
	resolver := Resolver{Authorization: &fakeAuthorization{allowed: []bool{false}}, Targets: &fakeTargets{found: true, preview: Preview{Kind: ChatConversation, ID: "secret", Title: "private"}}}
	got, err := resolver.Resolve(context.Background(), "person-1", Reference{Kind: ChatConversation, ID: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != Restricted || got.Preview != nil {
		t.Fatalf("unauthorized link disclosed target: %#v", got)
	}
}
