package orggraph

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/organization"
)

type snapshotRow []any

func (r snapshotRow) Scan(dest ...any) error {
	if len(dest) != len(r) {
		return errors.New("unexpected scan arity")
	}
	for index := range dest {
		out := reflect.ValueOf(dest[index])
		if out.Kind() != reflect.Pointer || out.IsNil() {
			return errors.New("scan target must be a non-nil pointer")
		}
		out = out.Elem()
		if r[index] == nil {
			out.SetZero()
			continue
		}
		value := reflect.ValueOf(r[index])
		if !value.Type().AssignableTo(out.Type()) {
			return errors.New("scan source has incompatible type")
		}
		out.Set(value)
	}
	return nil
}

type snapshotRows struct {
	values [][]any
	index  int
	closed bool
}

func (r *snapshotRows) Next() bool { return r.index < len(r.values) }
func (r *snapshotRows) Scan(dest ...any) error {
	err := snapshotRow(r.values[r.index]).Scan(dest...)
	r.index++
	return err
}
func (r *snapshotRows) Err() error { return nil }
func (r *snapshotRows) Close()     { r.closed = true }

type snapshotQueryer struct {
	rows   *snapshotRows
	query  string
	tenant any
}

func (q *snapshotQueryer) Query(_ context.Context, query string, args ...any) (dbport.Rows, error) {
	q.query = query
	if len(args) > 0 {
		q.tenant = args[0]
	}
	return q.rows, nil
}
func (*snapshotQueryer) QueryRow(context.Context, string, ...any) dbport.Row { panic("unused") }

func TestLoadBuildsEffectiveDatedSnapshotFromCanonicalUnits(t *testing.T) {
	tenant, rootID, childID := uuid.New(), uuid.New(), uuid.New()
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := &snapshotRows{values: [][]any{
		{rootID, "ROOT", "Company", "BUSINESS_UNIT", (*uuid.UUID)(nil), from, (*time.Time)(nil), "root-digest"},
		{childID, "TEAM", "Engineering", "TEAM", &rootID, from, &to, "child-digest"},
	}}
	queryer := &snapshotQueryer{rows: rows}
	snapshot, err := Load(context.Background(), queryer, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(queryer.query, "tenant_id = $1 AND superseded_at IS NULL") || queryer.tenant != tenant {
		t.Fatalf("query did not enforce current tenant facts: query=%q tenant=%v", queryer.query, queryer.tenant)
	}
	if !rows.closed || snapshot.Tenant != tenant.String() || snapshot.Watermark == "" {
		t.Fatalf("snapshot boundary incomplete: closed=%v snapshot=%+v", rows.closed, snapshot)
	}
	if len(snapshot.Units) != 2 || len(snapshot.Edges) != 1 || snapshot.Edges[0].Type != organization.Hierarchy {
		t.Fatalf("snapshot units/edges = %d/%+v, want two units and one hierarchy edge", len(snapshot.Units), snapshot.Edges)
	}
	asOf := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	request := organization.ReadRequest{Tenant: tenant.String(), Root: childID.String(), AsOf: asOf}
	ancestors, err := organization.Ancestry(snapshot, request)
	if err != nil {
		t.Fatal(err)
	}
	descendants, err := organization.Descendency(snapshot, organization.ReadRequest{Tenant: tenant.String(), Root: rootID.String(), AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(ancestors.Units) != 2 || len(descendants.Units) != 2 {
		t.Fatalf("as-of traversal returned ancestors=%v descendants=%v", ancestors.Units, descendants.Units)
	}
}

func TestLoadRejectsMissingTenantBeforeQuery(t *testing.T) {
	queryer := &snapshotQueryer{rows: &snapshotRows{}}
	if _, err := Load(context.Background(), queryer, uuid.Nil); !errors.Is(err, organization.ErrTenantBoundary) {
		t.Fatalf("Load(nil tenant) error = %v, want ErrTenantBoundary", err)
	}
	if queryer.query != "" {
		t.Fatal("Load queried the database without a tenant")
	}
}

func TestSnapshotPreservesParentEdgeIntervalsAcrossUnitRevisions(t *testing.T) {
	tenant, parentID, childID := uuid.New(), uuid.New(), uuid.New()
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	change := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	childEnd := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snapshot := snapshotFromRows(tenant, []row{
		{id: parentID.String(), revision: "parent-v1", name: "Parent", typ: "DIVISION", from: start, to: &change},
		{id: parentID.String(), revision: "parent-v2", name: "Parent", typ: "DIVISION", from: change, to: &end},
		{id: childID.String(), revision: "child-v1", name: "Child", typ: "TEAM", parent: &parentID, from: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), to: &childEnd},
	})
	if len(snapshot.Edges) != 2 {
		t.Fatalf("parent revision edges = %d, want 2 interval pieces", len(snapshot.Edges))
	}
	for _, asOf := range []time.Time{
		time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
	} {
		if err := snapshot.Validate(asOf); err != nil {
			t.Fatalf("snapshot invalid as of %s: %v", asOf, err)
		}
		result, err := organization.Ancestry(snapshot, organization.ReadRequest{Tenant: tenant.String(), Root: childID.String(), AsOf: asOf})
		if err != nil || len(result.Units) != 2 {
			t.Fatalf("ancestry as of %s = %+v, %v", asOf, result, err)
		}
	}
}
