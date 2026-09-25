package projectlink

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeAuthorization struct {
	allowed []bool
	calls   int
	order   *[]string
}

func (f *fakeAuthorization) Authorize(_ context.Context, _ string, _ Reference) (bool, error) {
	f.calls++
	if f.order != nil {
		*f.order = append(*f.order, "authorize")
	}
	if len(f.allowed) == 0 {
		return false, nil
	}
	allowed := f.allowed[0]
	f.allowed = f.allowed[1:]
	return allowed, nil
}

type fakeTargets struct {
	preview Preview
	found   bool
	err     error
	order   *[]string
	calls   int
}

func (f *fakeTargets) ReadAuthorized(_ context.Context, _ Reference) (Preview, bool, error) {
	f.calls++
	if f.order != nil {
		*f.order = append(*f.order, "read")
	}
	return f.preview, f.found, f.err
}

func TestResolveAllowlistedTargetKinds(t *testing.T) {
	tests := []struct {
		name    string
		ref     Reference
		preview Preview
	}{
		{"conversation", Reference{Kind: ChatConversation, ID: "c-1"}, Preview{Kind: ChatConversation, ID: "c-1", Title: "Team"}},
		{"post", Reference{Kind: ChatPost, ID: "p-1", ConversationID: "c-1"}, Preview{Kind: ChatPost, ID: "p-1", ConversationID: "c-1", Snippet: "Update"}},
		{"deployed document", Reference{Kind: DeployedDocument, ID: "d-1", Version: "v-3", ScopeID: "team-1"}, Preview{Kind: DeployedDocument, ID: "d-1", Version: "v-3", ScopeID: "team-1", Title: "Guide"}},
		{"safe WorkItem", Reference{Kind: WorkItem, ID: "w-1"}, Preview{Kind: WorkItem, ID: "w-1", Version: "12", Status: "OPEN", Freshness: "CURRENT"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := Resolver{Authorization: &fakeAuthorization{allowed: []bool{true, true}}, Targets: &fakeTargets{preview: tt.preview, found: true}}
			got, err := resolver.Resolve(context.Background(), "person-1", tt.ref)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != Available || got.Preview == nil || !reflect.DeepEqual(*got.Preview, tt.preview) {
				t.Fatalf("Resolve() = %#v", got)
			}
		})
	}
}

func TestResolveAuthorizationPrecedesReadAndDenialIsNeutral(t *testing.T) {
	order := []string{}
	targets := &fakeTargets{preview: Preview{Kind: ChatConversation, ID: "c-secret", Title: "Secret title"}, found: true, order: &order}
	resolver := Resolver{Authorization: &fakeAuthorization{allowed: []bool{false}, order: &order}, Targets: targets}
	got, err := resolver.Resolve(context.Background(), "person-1", Reference{Kind: ChatConversation, ID: "c-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != Restricted || got.Preview != nil {
		t.Fatalf("restricted result disclosed target: %#v", got)
	}
	if targets.calls != 0 || !reflect.DeepEqual(order, []string{"authorize"}) {
		t.Fatalf("target read before authorization: calls=%d order=%v", targets.calls, order)
	}
}

func TestResolveRevocationDuringReadDiscardsPreview(t *testing.T) {
	order := []string{}
	resolver := Resolver{
		Authorization: &fakeAuthorization{allowed: []bool{true, false}, order: &order},
		Targets:       &fakeTargets{preview: Preview{Kind: DeployedDocument, ID: "d-1", Version: "v-2", ScopeID: "team-1", Title: "Private"}, found: true, order: &order},
	}
	got, err := resolver.Resolve(context.Background(), "person-1", Reference{Kind: DeployedDocument, ID: "d-1", Version: "v-2", ScopeID: "team-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != Restricted || got.Preview != nil {
		t.Fatalf("revoked target leaked: %#v", got)
	}
	want := []string{"authorize", "read", "authorize"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("call order = %v, want %v", order, want)
	}
}

func TestResolveMissingAndDeniedHaveSamePublicShape(t *testing.T) {
	ref := Reference{Kind: WorkItem, ID: "w-1"}
	denied, err := (Resolver{Authorization: &fakeAuthorization{allowed: []bool{false}}, Targets: &fakeTargets{}}).Resolve(context.Background(), "person-1", ref)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := (Resolver{Authorization: &fakeAuthorization{allowed: []bool{true, true}}, Targets: &fakeTargets{found: false}}).Resolve(context.Background(), "person-1", ref)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(denied, missing) || denied.State != Restricted || denied.Preview != nil {
		t.Fatalf("denied=%#v missing=%#v", denied, missing)
	}
}

func TestReferenceValidationRejectsUnallowlistedAndMutableTargets(t *testing.T) {
	bad := []Reference{
		{Kind: "URL", ID: "https://example.test"},
		{Kind: ChatConversation, ID: "c-1", Version: "v-1"},
		{Kind: ChatPost, ID: "p-1"},
		{Kind: DeployedDocument, ID: "d-1"},
		{Kind: DeployedDocument, ID: "d-1", Version: "v-1"},
		{Kind: DeployedDocument, ID: "d-1", Version: "v-1", ScopeID: "../other"},
		{Kind: WorkItem, ID: "w-1", ConversationID: "private"},
		{Kind: WorkItem, ID: "../secret"},
	}
	for _, ref := range bad {
		if err := ref.Validate(); !errors.Is(err, ErrInvalidReference) {
			t.Errorf("Validate(%#v) = %v", ref, err)
		}
	}
	got, err := (Resolver{Authorization: &fakeAuthorization{}, Targets: &fakeTargets{}}).Resolve(context.Background(), " ", Reference{Kind: ChatConversation, ID: "c-1"})
	if !errors.Is(err, ErrInvalidReference) || got.Preview != nil {
		t.Fatalf("invalid principal result = %#v, %v", got, err)
	}
}

func TestResolveRejectsUnsafeWorkItemProjection(t *testing.T) {
	resolver := Resolver{
		Authorization: &fakeAuthorization{allowed: []bool{true, true}},
		Targets:       &fakeTargets{found: true, preview: Preview{Kind: WorkItem, ID: "w-1", Version: "4", Status: "OPEN", Freshness: "CURRENT", Title: "Evidence details"}},
	}
	if _, err := resolver.Resolve(context.Background(), "person-1", Reference{Kind: WorkItem, ID: "w-1"}); !errors.Is(err, ErrResolverUnavailable) {
		t.Fatalf("unsafe WorkItem detail accepted: %v", err)
	}
}

func TestResolveRejectsDeploymentFromDifferentScope(t *testing.T) {
	resolver := Resolver{
		Authorization: &fakeAuthorization{allowed: []bool{true, true}},
		Targets: &fakeTargets{found: true, preview: Preview{
			Kind: DeployedDocument, ID: "d-1", Version: "v-4", ScopeID: "team-other", Title: "Private guide",
		}},
	}
	ref := Reference{Kind: DeployedDocument, ID: "d-1", Version: "v-4", ScopeID: "team-1"}
	if _, err := resolver.Resolve(context.Background(), "person-1", ref); !errors.Is(err, ErrResolverUnavailable) {
		t.Fatalf("cross-scope deployment accepted: %v", err)
	}
}
