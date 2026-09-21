package fkindex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type goldenSnapshot struct {
	Before []goldenFK  `json:"before"`
	After  []goldenFK  `json:"after"`
	Plan   []IndexSpec `json:"plan"`
}

type goldenFK struct {
	ForeignKey
	References []Reference `json:"references,omitempty"`
}

func TestTodo_PERFOPT_005(t *testing.T) {
	db := pgtest.New(t)
	fks, err := Audit(context.Background(), db.Conn)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := References(repoRoot(t), fks)
	if err != nil {
		t.Fatal(err)
	}
	for _, fk := range fks {
		if !fk.Covered && len(refs[fkKey(fk)]) != 0 {
			t.Errorf("referenced FK %s remains uncovered: %v", fkKey(fk), refs[fkKey(fk)])
		}
	}
	snapshot := readGolden(t)
	wantUnreferenced := make(map[string]bool)
	// The before snapshot belongs to migration 00261. New migrations are
	// allowed to cover its old unreferenced keys and introduce new ones;
	// pin current intentional omissions against the after snapshot.
	for _, fk := range snapshot.After {
		if !fk.Covered && len(fk.References) == 0 {
			wantUnreferenced[fkKey(fk.ForeignKey)] = true
		}
	}
	gotUnreferenced := make(map[string]bool)
	for _, fk := range fks {
		if !fk.Covered {
			gotUnreferenced[fkKey(fk)] = len(refs[fkKey(fk)]) == 0
		}
	}
	if !reflect.DeepEqual(gotUnreferenced, wantUnreferenced) {
		t.Fatalf("unreferenced FK set differs: got %v want %v", gotUnreferenced, wantUnreferenced)
	}
}

func TestTodo_PERFOPT_005_Integration(t *testing.T) {
	db := pgtest.New(t)
	snapshot := readGolden(t)
	seedExplainRows(t, db)
	wanted := []string{
		"ix_document_tenant_id_current_version_id",
		"ix_document_version_tenant_id_supersedes_version_id",
		"ix_document_version_tenant_id_template_id",
		"ix_document_artifact_reference_tenant_id_version_id",
		"ix_signature_request_tenant_id_version_id",
	}
	byName := make(map[string]IndexSpec, len(snapshot.Plan))
	for _, spec := range snapshot.Plan {
		byName[spec.Name] = spec
	}
	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	for _, name := range wanted {
		spec, ok := byName[name]
		if !ok || spec.Unreferenced || len(spec.References) == 0 {
			t.Fatalf("golden has no referenced plan for %s", name)
		}
		var exists bool
		if err := db.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, spec.Name).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", spec.Name, err)
		}
		if !exists {
			t.Fatalf("planned index %s does not exist", spec.Name)
		}
		// The relation and predicate are reconstructed from the golden's
		// recorded store location. The catalog proof above is independent of
		// the planner; this EXPLAIN proves PostgreSQL can use the new key.
		predicate := quoteIdent(spec.Columns[0]) + " = $1"
		if len(spec.Columns) > 1 {
			predicate += " AND " + quoteIdent(spec.Columns[1]) + " = $2"
		}
		query := "SELECT " + quoteIdent(spec.Columns[0]) + " FROM " + quoteIdent(spec.Table) + " WHERE " + predicate
		if len(spec.Columns) > 1 {
			query += " ORDER BY " + quoteIdent(spec.Columns[1]) + " NULLS FIRST LIMIT 1"
		}
		args := []any{tenantID}
		if strings.Contains(predicate, "$2") {
			secondID := "00000000-0000-0000-0000-000000004001"
			if spec.Columns[1] == "template_id" {
				secondID = "00000000-0000-0000-0000-000000010001"
			}
			args = append(args, uuid.MustParse(secondID))
		}
		var raw string
		if err := db.QueryRow(context.Background(), "EXPLAIN (FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
			t.Fatalf("explain %s: %v", spec.Name, err)
		}
		if !containsIndexScan(raw, spec.Name) {
			t.Fatalf("EXPLAIN for %s did not use its index: %s", spec.Name, raw)
		}
	}
}

func seedExplainRows(t *testing.T, db *pgtest.DB) {
	t.Helper()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ('00000000-0000-0000-0000-000000000001', 'fkindex', 'cell-local', 'fkindex', 'ACTIVE', now())`)
	db.Exec(t, `
		INSERT INTO document_template (
			tenant_id, template_id, template_key, template_version, purpose,
			source_locale, content_digest, effective_from
		)
		SELECT '00000000-0000-0000-0000-000000000001', lpad((i + 10000)::text, 32, '0')::uuid,
		       'template-' || i, 1, 'EMPLOYEE', 'en-US',
		       repeat(md5('template-' || i), 2)::content_digest, now()
		FROM generate_series(1, 2000) AS s(i)`)
	db.Exec(t, `
		INSERT INTO document (tenant_id, document_id, document_type, owner_ref, subject_ref, classification)
		SELECT '00000000-0000-0000-0000-000000000001', lpad((i + 2000)::text, 32, '0')::uuid,
		       'EMPLOYEE', 'owner-' || i, 'subject-' || i, 'PUBLIC'
		FROM generate_series(1, 2000) AS s(i)`)
	db.Exec(t, `
		INSERT INTO document_version (
			tenant_id, version_id, document_id, version_number, template_id, template_version,
			render_version, canonicalization_version, source_artifact_digest, rendered_artifact_digest,
			locale, classification, supersedes_version_id, created_at
		)
		SELECT '00000000-0000-0000-0000-000000000001', lpad((i + 4000)::text, 32, '0')::uuid,
		       lpad((i + 2000)::text, 32, '0')::uuid, 1,
		       lpad((i + 10000)::text, 32, '0')::uuid, 1, 1, 1,
		       repeat(md5('source-' || i), 2)::content_digest,
		       repeat(md5('rendered-' || i), 2)::content_digest,
		       'en-US', 'PUBLIC',
		       CASE WHEN i = 1 THEN NULL ELSE lpad((i + 3999)::text, 32, '0')::uuid END,
		       now()
		FROM generate_series(1, 2000) AS s(i)`)
	db.Exec(t, `
		UPDATE document d
		SET current_version_id = lpad((s.i + 4000)::text, 32, '0')::uuid
		FROM generate_series(1, 2000) AS s(i)
		WHERE d.document_id = lpad((s.i + 2000)::text, 32, '0')::uuid`)
	db.Exec(t, `
		INSERT INTO document_artifact_reference (
			tenant_id, reference_id, document_id, version_id, content_id,
			purpose, classification, retention_class, referenced_at
		)
		SELECT '00000000-0000-0000-0000-000000000001', lpad((i + 6000)::text, 32, '0')::uuid,
		       lpad((i + 2000)::text, 32, '0')::uuid, lpad((i + 4000)::text, 32, '0')::uuid,
		       repeat(md5('artifact-' || i), 2)::content_digest,
		       'SOURCE', 'PUBLIC', 'REBUILDABLE', now()
		FROM generate_series(1, 2000) AS s(i)`)
	db.Exec(t, `
		INSERT INTO signature_request (
			tenant_id, request_id, version_id, assurance_mode, deadline_at, created_at
		)
		SELECT '00000000-0000-0000-0000-000000000001', lpad((i + 8000)::text, 32, '0')::uuid,
		       lpad((i + 4000)::text, 32, '0')::uuid, 'NATIVE_EVIDENCE', now() + interval '1 day', now()
		FROM generate_series(1, 2000) AS s(i)`)
	for _, table := range []string{"document", "document_version", "document_artifact_reference", "signature_request"} {
		db.Exec(t, "ANALYZE "+quoteIdent(table))
	}
}

func TestTodo_PERFOPT_005_Golden(t *testing.T) {
	before := pgtest.NewEmpty(t)
	if _, err := before.Provider(t).UpTo(context.Background(), 261); err != nil {
		t.Fatalf("apply migrations through 00261: %v", err)
	}
	beforeFKs, err := Audit(context.Background(), before.Conn)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := References(repoRoot(t), beforeFKs)
	if err != nil {
		t.Fatal(err)
	}
	planned := Plan(beforeFKs, refs)
	after := pgtest.New(t)
	afterFKs, err := Audit(context.Background(), after.Conn)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := goldenSnapshot{Plan: planned}
	snapshot.Before = snapshotFKs(beforeFKs, refs)
	afterRefs, err := References(repoRoot(t), afterFKs)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.After = snapshotFKs(afterFKs, afterRefs)
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	path := filepath.Join(repoRoot(t), "tools", "policy", "fkindex", "testdata", "fkindex.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create)", err)
	}
	if !reflect.DeepEqual(want, body) {
		t.Fatalf("fkindex golden is stale; set HCMNEXT_UPDATE_GOLDEN=1 to update")
	}
}

func TestAuditMatchesLeadingIndexColumns(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{values: [][]any{{"public", "child", "child_parent_fk", []string{"tenant_id", "parent_id"}, false, false}}}}
	got, err := Audit(context.Background(), q)
	if err != nil || len(got) != 1 || got[0].Table != "child" || got[0].Columns[1] != "parent_id" || got[0].Covered {
		t.Fatalf("Audit got %#v, err %v", got, err)
	}
}

func TestReferencesAndPlanAreDeterministic(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "data", "x")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "store.go"), []byte("package x\nvar q = `SELECT 1 FROM child JOIN parent ON child.parent_id = parent.id WHERE child.tenant_id = $1`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fks := []ForeignKey{{Schema: "s", Table: "child", Name: "fk", Columns: []string{"tenant_id", "parent_id"}}}
	refs, err := References(root, fks)
	if err != nil || len(refs["s.child.fk"]) != 1 {
		t.Fatalf("references=%v err=%v", refs, err)
	}
	plan := Plan(fks, refs)
	if len(plan) != 1 || plan[0].Name != "ix_child_tenant_id_parent_id" || plan[0].Unreferenced {
		t.Fatalf("plan=%#v", plan)
	}
	long := ForeignKey{Table: "t", Columns: []string{"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"}}
	if len(indexName(long.Table, long.Columns)) != 63 {
		t.Fatalf("index name was not truncated: %q", indexName(long.Table, long.Columns))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func snapshotFKs(fks []ForeignKey, refs map[string][]Reference) []goldenFK {
	out := make([]goldenFK, 0, len(fks))
	for _, fk := range fks {
		out = append(out, goldenFK{ForeignKey: fk, References: refs[fkKey(fk)]})
	}
	sort.Slice(out, func(i, j int) bool { return fkKey(out[i].ForeignKey) < fkKey(out[j].ForeignKey) })
	return out
}

func readGolden(t *testing.T) goldenSnapshot {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "policy", "fkindex", "testdata", "fkindex.golden"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot goldenSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func quoteIdent(name string) string { return `"` + name + `"` }

func containsIndexScan(raw, name string) bool {
	return strings.Contains(raw, name) && (strings.Contains(raw, `"Index Scan"`) || strings.Contains(raw, `"Index Only Scan"`) || strings.Contains(raw, `"Bitmap Index Scan"`))
}

type fakeQuerier struct{ rows dbport.Rows }

func (f *fakeQuerier) Query(context.Context, string, ...any) (dbport.Rows, error) { return f.rows, nil }
func (f *fakeQuerier) QueryRow(context.Context, string, ...any) dbport.Row        { return nil }

type fakeRows struct {
	values [][]any
	i      int
}

func (r *fakeRows) Next() bool { return r.i < len(r.values) }
func (r *fakeRows) Scan(dest ...any) error {
	row := r.values[r.i]
	r.i++
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			*d = row[i].(string)
		case *[]string:
			*d = row[i].([]string)
		case *bool:
			*d = row[i].(bool)
		}
	}
	return nil
}
func (r *fakeRows) Err() error { return nil }
func (r *fakeRows) Close()     {}
