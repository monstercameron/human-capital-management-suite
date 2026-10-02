package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaCatalogMemberReaderFunc func(context.Context, values.TenantId, string) (PersonaCatalogMember, error)

func (f personaCatalogMemberReaderFunc) ResolvePersonaCatalogMember(ctx context.Context, tenant values.TenantId, subject string) (PersonaCatalogMember, error) {
	return f(ctx, tenant, subject)
}

func TestTodo_AGENTP_018_CoreDirectoryResolvesExactCurrentLabel(t *testing.T) {
	var gotTenant values.TenantId
	var gotSubject string
	directory, err := NewCorePersonaCatalogDirectory(personaCatalogMemberReaderFunc(func(_ context.Context, tenant values.TenantId, subject string) (PersonaCatalogMember, error) {
		gotTenant, gotSubject = tenant, subject
		return PersonaCatalogMember{TenantID: tenant, SubjectID: subject, Label: " Ada Lovelace "}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := directory.ResolvePersonaCatalogTarget(context.Background(), values.TenantId("tenant-a"), "worker-a")
	// The target carries a member list now (AGENTUX-034), so it is compared by
	// its fields, not with ==.
	if err != nil || got.ID != "worker-a" || got.Label != "Ada Lovelace" || got.Role != "" || got.Kind != "" || len(got.Members) != 0 {
		t.Fatalf("target = %#v, err = %v", got, err)
	}
	if gotTenant != "tenant-a" || gotSubject != "worker-a" {
		t.Fatalf("lookup scope = %q/%q", gotTenant, gotSubject)
	}
}

func TestTodo_AGENTP_018_CoreDirectoryFailsClosedForInvalidCoreRows(t *testing.T) {
	tests := []struct {
		name   string
		member PersonaCatalogMember
	}{
		{name: "wrong tenant", member: PersonaCatalogMember{TenantID: "tenant-b", SubjectID: "worker-a", Label: "Ada"}},
		{name: "wrong subject", member: PersonaCatalogMember{TenantID: "tenant-a", SubjectID: "worker-b", Label: "Ada"}},
		{name: "missing label", member: PersonaCatalogMember{TenantID: "tenant-a", SubjectID: "worker-a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			directory, err := NewCorePersonaCatalogDirectory(personaCatalogMemberReaderFunc(func(context.Context, values.TenantId, string) (PersonaCatalogMember, error) {
				return tc.member, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := directory.ResolvePersonaCatalogTarget(context.Background(), "tenant-a", "worker-a"); !errors.Is(err, ErrPersonaCatalogDirectoryUnavailable) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestTodo_AGENTP_018_CoreDirectoryRejectsMissingSourceAndNeverEnumerates(t *testing.T) {
	if _, err := NewCorePersonaCatalogDirectory(nil); !errors.Is(err, ErrPersonaCatalogDirectoryUnavailable) {
		t.Fatalf("constructor error = %v", err)
	}
	var calls int
	directory, err := NewCorePersonaCatalogDirectory(personaCatalogMemberReaderFunc(func(context.Context, values.TenantId, string) (PersonaCatalogMember, error) {
		calls++
		return PersonaCatalogMember{}, errors.New("not found")
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		tenant  values.TenantId
		subject string
	}{
		{name: "nil context", tenant: "tenant-a", subject: "worker-a"},
		{name: "invalid tenant", ctx: context.Background(), tenant: "", subject: "worker-a"},
		{name: "trimmed subject", ctx: context.Background(), tenant: "tenant-a", subject: " worker-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := directory.ResolvePersonaCatalogTarget(tc.ctx, tc.tenant, tc.subject); !errors.Is(err, ErrPersonaCatalogDirectoryUnavailable) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("invalid requests called core reader %d times", calls)
	}
}
