package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type directoryDBFake struct{ tx *directoryTxFake }

func (d directoryDBFake) Begin(context.Context) (dbport.Tx, error) { return d.tx, nil }

type directoryTxFake struct {
	results []dbport.Rows
	queries []string
}

func (t *directoryTxFake) Exec(context.Context, string, ...any) (int64, error) { return 1, nil }
func (t *directoryTxFake) Query(_ context.Context, sql string, _ ...any) (dbport.Rows, error) {
	t.queries = append(t.queries, sql)
	if len(t.results) == 0 {
		return nil, errors.New("unexpected query")
	}
	r := t.results[0]
	t.results = t.results[1:]
	return r, nil
}
func (t *directoryTxFake) QueryRow(context.Context, string, ...any) dbport.Row {
	return directoryRowFake{}
}
func (t *directoryTxFake) Commit(context.Context) error   { return nil }
func (t *directoryTxFake) Rollback(context.Context) error { return nil }

type directoryRowsFake struct {
	rows  [][]any
	index int
}

func (r *directoryRowsFake) Next() bool { return r.index < len(r.rows) }
func (r *directoryRowsFake) Scan(dest ...any) error {
	row := r.rows[r.index]
	for i := range dest {
		switch p := dest[i].(type) {
		case *string:
			*p = row[i].(string)
		}
	}
	r.index++
	return nil
}
func (r *directoryRowsFake) Err() error { return nil }
func (r *directoryRowsFake) Close()     {}

type directoryRowFake struct{}

func (directoryRowFake) Scan(...any) error { return dbport.ErrNoRows }

func directoryReader(rows ...dbport.Rows) *AgentDirectoryDB {
	return NewAgentDirectoryDB(directoryDBFake{tx: &directoryTxFake{results: rows}}, func(values.TenantId) uuid.UUID {
		return uuid.MustParse("00000000-0000-4000-8000-000000000001")
	})
}

func TestTodo_AGENT2_005_DirectoryRolesAndOrganizationScopesReadCurrentRows(t *testing.T) {
	roles := &directoryRowsFake{rows: [][]any{{"manager"}, {"hr_partner"}}}
	d := directoryReader(roles)
	gotRoles, err := d.CurrentRoles(context.Background(), values.TenantId("tenant-a"), "user-1")
	if err != nil || !reflect.DeepEqual(gotRoles, []string{"manager", "hr_partner"}) {
		t.Fatalf("CurrentRoles = %#v, %v", gotRoles, err)
	}
	d = directoryReader(&directoryRowsFake{rows: [][]any{{"org-b"}, {"org-a"}, {"org-b"}}})
	gotScopes, err := d.CurrentOrganizationScopes(context.Background(), values.TenantId("tenant-a"), "user-1", []string{"manager"})
	if err != nil || !reflect.DeepEqual(gotScopes, []string{"org-a", "org-b"}) {
		t.Fatalf("CurrentOrganizationScopes = %#v, %v", gotScopes, err)
	}
}

func TestTodo_AGENT2_005_DirectorySubjectsUseExactWorkerFacts(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000002"
	d := directoryReader(&directoryRowsFake{rows: [][]any{{id, "worker-2", "org-a", "user-1"}}})
	got, err := d.CurrentSubjects(context.Background(), values.TenantId("tenant-a"), "user-1", "workforce:read", []string{"manager"}, []string{"org-a"})
	if err != nil || len(got) != 1 || got[0].Ref.Id != id || got[0].Organization.ID != "org-a" {
		t.Fatalf("CurrentSubjects = %#v, %v", got, err)
	}
	if got[0].Ref.Tenant != values.TenantId("tenant-a") {
		t.Fatal("subject was not tenant scoped")
	}
}

func TestTodo_AGENT2_005_DirectoryFieldsComeFromAuthzPolicy(t *testing.T) {
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "user-1", SubjectKind: trust.SubjectKindHuman,
		Roles: []string{"manager"}, Purposes: []string{authz.PurposeCompensationReview},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(2, 0), CredentialDigest: "digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewAgentDirectoryDB(nil, nil).CurrentFields(context.Background(), principal, authz.PurposeCompensationReview, []string{"manager"}, []string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !containsField(got, authz.FieldBaseSalary) || containsField(got, authz.FieldTaxID) {
		t.Fatalf("CurrentFields = %v, want compensation and no tax field", got)
	}
	auditor, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "user-1", SubjectKind: trust.SubjectKindHuman,
		Purposes: []string{authz.PurposeAuditReview}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-audit", IssuedAt: time.Unix(1, 0),
		ExpiresAt: time.Unix(2, 0), CredentialDigest: "digest-audit",
	})
	if err != nil {
		t.Fatal(err)
	}
	auditorRoles := []string{"auditor"}
	redacted, err := NewAgentDirectoryDB(nil, nil).CurrentFields(context.Background(), auditor, authz.PurposeAuditReview, auditorRoles, []string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if containsField(redacted, authz.FieldBaseSalary) {
		t.Fatalf("CurrentFields exposed redacted salary field: %v", redacted)
	}
}

func TestTodo_AGENT2_005_DirectoryFailsClosedWhenPopulationHasNoSource(t *testing.T) {
	_, err := NewAgentDirectoryDB(nil, nil).CurrentPopulation(context.Background(), values.TenantId("tenant-a"), "user-1")
	if !errors.Is(err, ErrAgentDirectoryUnavailable) {
		t.Fatalf("CurrentPopulation error = %v, want ErrAgentDirectoryUnavailable", err)
	}
	_, err = NewAgentDirectoryDB(nil, nil).CurrentRoles(context.Background(), values.TenantId("tenant-a"), "user-1")
	if !errors.Is(err, ErrAgentDirectoryUnavailable) {
		t.Fatalf("nil DB error = %v, want ErrAgentDirectoryUnavailable", err)
	}
}

func TestTodo_AGENT2_027(t *testing.T) {
	d := directoryReader(&directoryRowsFake{rows: [][]any{{"population-a"}, {"population-b"}}})
	got, err := d.CurrentPopulations(context.Background(), values.TenantId("tenant-a"), "user-1")
	if err != nil || !reflect.DeepEqual(got, []string{"population-a", "population-b"}) {
		t.Fatalf("CurrentPopulations = %#v, %v", got, err)
	}
	d = directoryReader(&directoryRowsFake{rows: [][]any{{"population-a"}, {"population-b"}}}, &directoryRowsFake{rows: [][]any{{"population-a"}, {"population-b"}}})
	if _, err := d.CurrentPopulation(context.Background(), values.TenantId("tenant-a"), "user-1"); !errors.Is(err, ErrAgentDirectoryUnavailable) {
		t.Fatalf("CurrentPopulation ambiguity error = %v, want ErrAgentDirectoryUnavailable", err)
	}
}

func TestTodo_AGENT2_027_CurrentPopulationsRejectEmptySubject(t *testing.T) {
	if _, err := directoryReader().CurrentPopulations(context.Background(), values.TenantId("tenant-a"), " "); !errors.Is(err, ErrAgentDirectoryUnavailable) {
		t.Fatalf("CurrentPopulations empty subject error = %v, want ErrAgentDirectoryUnavailable", err)
	}
}

func containsField(fields []authz.FieldID, want authz.FieldID) bool {
	for _, field := range fields {
		if field == want {
			return true
		}
	}
	return false
}

func TestTodo_AGENT2_029_PersonaSubjectsUseCurrentHomeOrganization(t *testing.T) {
	workerID := uuid.New().String()
	organization := "org:tenant-a:field-operations"
	directory := directoryReader(&directoryRowsFake{rows: [][]any{{workerID, "worker-1", organization, ""}}})
	source := personaHomeOrganizationSubjects{directory: directory}
	subjects, err := source.CurrentSubjects(context.Background(), "tenant-a", "worker-1", "persona-mention", []string{"worker_self"}, []string{organization})
	if err != nil || len(subjects) != 1 || subjects[0].Ref.Id != workerID || subjects[0].Ref.Tenant != "tenant-a" || subjects[0].Organization.ID != organization || subjects[0].Organization.Tenant != "tenant-a" {
		t.Fatalf("canonical subject authority = %+v %v", subjects, err)
	}
	query := directory.DB.(directoryDBFake).tx.queries[0]
	for _, required := range []string{"agent_current_home_organization", "h.tenant_id=w.tenant_id", "h.subject_ref=w.worker_key", "h.revoked_at IS NULL", "h.superseded_at IS NULL", "h.effective_from <= CURRENT_TIMESTAMP", "CURRENT_TIMESTAMP < h.effective_until"} {
		if !strings.Contains(query, required) {
			t.Fatalf("canonical subject query omits current fact condition %s", required)
		}
	}
	if strings.Contains(query, "org_unit") {
		t.Fatal("workforce unit code cannot substitute for missing home organization authority")
	}
	missing := personaHomeOrganizationSubjects{directory: directoryReader(&directoryRowsFake{rows: [][]any{{workerID, "worker-1", "", ""}}})}
	if _, err := missing.CurrentSubjects(context.Background(), "tenant-a", "worker-1", "persona-mention", []string{"worker_self"}, []string{organization}); !errors.Is(err, ErrAgentDirectoryUnavailable) {
		t.Fatalf("missing canonical home accepted: %v", err)
	}
	if _, err := (personaHomeOrganizationSubjects{}).CurrentSubjects(context.Background(), "tenant-a", "worker-1", "persona-mention", []string{}, []string{}); !errors.Is(err, ErrAgentDirectoryUnavailable) {
		t.Fatalf("missing subject owner accepted: %v", err)
	}
}
