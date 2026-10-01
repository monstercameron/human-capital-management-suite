package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaCatalogWorkerFactsFunc func(context.Context, people.FactQuery) (people.FactSet, error)

const personaCatalogTestWorkerID = "00000000-0000-4000-8000-000000000001"

func (f personaCatalogWorkerFactsFunc) WorkerFactsAt(ctx context.Context, query people.FactQuery) (people.FactSet, error) {
	return f(ctx, query)
}

func TestTodo_AGENTP_018_CoreMemberReaderUsesExactTenantSubjectAndPreferredLabel(t *testing.T) {
	var got people.FactQuery
	reader, err := NewCorePersonaCatalogMemberReader(personaCatalogWorkerFactsFunc(func(_ context.Context, query people.FactQuery) (people.FactSet, error) {
		got = query
		return people.FactSet{Worker: query.Worker, Exists: true, Facts: []people.Fact{
			{Field: people.FieldPreferredName, Value: values.Value(" Ada Lovelace ")},
		}}, nil
	}), func() time.Time { return time.Date(2026, time.September, 29, 15, 4, 5, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	gotMember, err := reader.ResolvePersonaCatalogMember(context.Background(), "tenant-a", personaCatalogTestWorkerID)
	if err != nil {
		t.Fatal(err)
	}
	if gotMember != (PersonaCatalogMember{TenantID: "tenant-a", SubjectID: personaCatalogTestWorkerID, Label: "Ada Lovelace"}) {
		t.Fatalf("member = %+v", gotMember)
	}
	if got.Tenant != "tenant-a" || got.Worker.Tenant != "tenant-a" || got.Worker.Kind != people.KindWorker || got.Worker.Id != personaCatalogTestWorkerID {
		t.Fatalf("query scope = %+v", got)
	}
	if len(got.Fields) != 2 || got.Fields[0] != people.FieldPreferredName || got.Fields[1] != people.FieldLegalName {
		t.Fatalf("query fields = %v", got.Fields)
	}
}

func TestTodo_AGENTP_018_CoreMemberReaderFallsBackToLegalAndFailsClosed(t *testing.T) {
	tests := []struct {
		name  string
		facts []people.Fact
		want  string
	}{
		{name: "legal fallback", facts: []people.Fact{{Field: people.FieldLegalName, Value: values.Value("Jane Doe")}}, want: "Jane Doe"},
		{name: "missing names", facts: nil},
		{name: "redacted name", facts: []people.Fact{{Field: people.FieldPreferredName, Value: values.Redacted[string]("policy")}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader, err := NewCorePersonaCatalogMemberReader(personaCatalogWorkerFactsFunc(func(_ context.Context, query people.FactQuery) (people.FactSet, error) {
				return people.FactSet{Worker: query.Worker, Exists: true, Facts: tc.facts}, nil
			}), func() time.Time { return time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC) })
			if err != nil {
				t.Fatal(err)
			}
			got, err := reader.ResolvePersonaCatalogMember(context.Background(), "tenant-a", personaCatalogTestWorkerID)
			if tc.want == "" {
				if !errors.Is(err, errPersonaCatalogMemberReader) || got != (PersonaCatalogMember{}) {
					t.Fatalf("member = %+v, err = %v; want fail closed", got, err)
				}
				return
			}
			if err != nil || got.Label != tc.want {
				t.Fatalf("member = %+v, err = %v", got, err)
			}
		})
	}
}

func TestTodo_AGENTP_018_CoreMemberReaderRejectsInvalidRequestsAndSourceFailures(t *testing.T) {
	calls := 0
	reader, err := NewCorePersonaCatalogMemberReader(personaCatalogWorkerFactsFunc(func(context.Context, people.FactQuery) (people.FactSet, error) {
		calls++
		return people.FactSet{}, errors.New("database unavailable")
	}), func() time.Time { return time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC) })
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
		{name: "missing tenant", ctx: context.Background(), subject: "worker-a"},
		{name: "trimmed subject", ctx: context.Background(), tenant: "tenant-a", subject: " worker-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := reader.ResolvePersonaCatalogMember(tc.ctx, tc.tenant, tc.subject); !errors.Is(err, errPersonaCatalogMemberReader) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("invalid requests reached worker facts %d times", calls)
	}
	_, sourceErr := reader.ResolvePersonaCatalogMember(context.Background(), "tenant-a", personaCatalogTestWorkerID)
	if !errors.Is(sourceErr, errPersonaCatalogMemberReader) {
		t.Fatalf("source error = %v", sourceErr)
	}
	var staged interface{ PersonaCatalogFailureStage() string }
	if !errors.As(sourceErr, &staged) || staged.PersonaCatalogFailureStage() != "member_facts_read" {
		t.Fatalf("source error stage = %v, want member_facts_read", staged)
	}
	if calls != 1 {
		t.Fatalf("source calls = %d, want 1", calls)
	}
	if _, err := NewCorePersonaCatalogMemberReader(nil, time.Now); !errors.Is(err, errPersonaCatalogMemberReader) {
		t.Fatalf("nil workers constructor error = %v", err)
	}
	if _, err := NewCorePersonaCatalogMemberReader(personaCatalogWorkerFactsFunc(nil), nil); !errors.Is(err, errPersonaCatalogMemberReader) {
		t.Fatalf("nil clock constructor error = %v", err)
	}
}
