package chatsearch

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func issue3929Actor(person string) Actor {
	return Actor{TenantID: "t-1", HomeTenantID: "t-1", PersonID: person}
}

func TestIssue3929_SavedSearchKeepsTheQueryAsTyped(t *testing.T) {
	svc := NewSavedService(NewMemorySaved())
	ctx, me := context.Background(), issue3929Actor("p-1")
	got, err := svc.Create(ctx, me, "  Onboarding   questions ", `onboarding in:general from:"Ana Silva" has:file sort:oldest`)
	if err != nil || got.ID == "" || got.Name != "Onboarding questions" || got.Query != `onboarding in:general from:"Ana Silva" has:file sort:oldest` {
		t.Fatalf("create = %+v err=%v", got, err)
	}
	list, err := svc.List(ctx, me)
	if err != nil || len(list) != 1 || list[0].Query != got.Query {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	edited, err := svc.Update(ctx, me, got.ID, "Onboarding", "onboarding")
	if err != nil || edited.Name != "Onboarding" || edited.Query != "onboarding" {
		t.Fatalf("update = %+v err=%v", edited, err)
	}
	if err := svc.Delete(ctx, me, got.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.List(ctx, me); len(list) != 0 {
		t.Fatalf("still listed: %+v", list)
	}
}

func TestIssue3929_Security_SavedSearchesArePrivateToTheirOwner(t *testing.T) {
	svc := NewSavedService(NewMemorySaved())
	ctx := context.Background()
	mine, other := issue3929Actor("p-1"), issue3929Actor("p-2")
	saved, err := svc.Create(ctx, mine, "Leave", "parental leave")
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.List(ctx, other); len(list) != 0 {
		t.Fatalf("another person lists %+v", list)
	}
	if _, err := svc.Update(ctx, other, saved.ID, "x", "y"); !errors.Is(err, ErrSavedNotFound) {
		t.Fatalf("another person edited: %v", err)
	}
	if err := svc.Delete(ctx, other, saved.ID); !errors.Is(err, ErrSavedNotFound) {
		t.Fatalf("another person deleted: %v", err)
	}
	// The same person in another workspace is another owner.
	elsewhere := Actor{TenantID: "t-9", HomeTenantID: "t-9", PersonID: "p-1"}
	if list, _ := svc.List(ctx, elsewhere); len(list) != 0 {
		t.Fatalf("another workspace lists %+v", list)
	}
}

func TestIssue3929_RefusesWhatASearchWouldRefuse(t *testing.T) {
	svc := NewSavedService(NewMemorySaved())
	ctx, me := context.Background(), issue3929Actor("p-1")
	for name, c := range map[string][2]string{
		"no name":    {" ", "budget"},
		"no query":   {"Budget", "   "},
		"bad filter": {"Budget", "budget before:not-a-date"},
		"sort only":  {"Budget", "sort:oldest"},
		"long name":  {strings.Repeat("a", SavedNameMax+1), "budget"},
		"long query": {"Budget", strings.Repeat("a", SavedQueryMax+1)},
	} {
		if _, err := svc.Create(ctx, me, c[0], c[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := svc.Create(ctx, Actor{}, "Budget", "budget"); !errors.Is(err, ErrInvalid) {
		t.Errorf("no actor: %v", err)
	}
}

func TestIssue3929_NamesAreUniqueIgnoringCaseAndLimited(t *testing.T) {
	svc := NewSavedService(NewMemorySaved())
	ctx, me := context.Background(), issue3929Actor("p-1")
	a, _ := svc.Create(ctx, me, "Budget", "budget")
	if _, err := svc.Create(ctx, me, "budget", "other"); !errors.Is(err, ErrSavedNameUsed) {
		t.Fatalf("duplicate name: %v", err)
	}
	b, _ := svc.Create(ctx, me, "Other", "other")
	if _, err := svc.Update(ctx, me, b.ID, "BUDGET", "other"); !errors.Is(err, ErrSavedNameUsed) {
		t.Fatalf("rename into a used name: %v", err)
	}
	if _, err := svc.Update(ctx, me, a.ID, "budget", "budget again"); err != nil {
		t.Fatalf("keeping its own name: %v", err)
	}
	for i := 2; i < SavedLimit; i++ {
		if _, err := svc.Create(ctx, me, "n"+strings.Repeat("x", i), "q"); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if _, err := svc.Create(ctx, me, "one too many", "q"); !errors.Is(err, ErrSavedLimit) {
		t.Fatalf("over the limit: %v", err)
	}
}

func TestIssue3929_NoStoreIsUnavailable(t *testing.T) {
	var svc *SavedService
	if _, err := svc.List(context.Background(), issue3929Actor("p")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil service: %v", err)
	}
}
