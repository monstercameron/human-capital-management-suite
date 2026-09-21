package deferredschema

import (
	"strings"
	"testing"
)

func TestRenderMigrationPreviewShape(t *testing.T) {
	d := payrollDomain()
	sql := RenderMigrationPreview(d)

	if got := d.PreviewFileName(); got != "001_payroll.sql" {
		t.Fatalf("PreviewFileName() = %q, want 001_payroll.sql", got)
	}

	for _, want := range []string{
		"-- +goose Up",
		"-- +goose Down",
		"CREATE TABLE IF NOT EXISTS payroll_run_preview (",
		"CREATE TABLE IF NOT EXISTS payroll_ledger_entry (",
		"ENABLE ROW LEVEL SECURITY",
		"FORCE ROW LEVEL SECURITY",
		"CREATE POLICY tenant_isolation ON payroll_run_preview",
		"CREATE POLICY tenant_isolation ON payroll_ledger_entry",
		"EXECUTE FUNCTION forbid_mutation()",
		"REFERENCES tenant (tenant_id)",
		"FOREIGN KEY (tenant_id, run_id) REFERENCES payroll_run_preview (tenant_id, run_id)",
		"GRANT SELECT, INSERT, UPDATE ON payroll_run_preview TO hcmnext_app",
		"GRANT SELECT, INSERT ON payroll_ledger_entry TO hcmnext_app",
		"DROP TABLE payroll_run_preview",
		"DROP TABLE payroll_ledger_entry",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("rendered SQL missing %q", want)
		}
	}

	// The mutable Head table must never get a forbid_mutation trigger of its
	// own; only the Evidence table should.
	if strings.Contains(sql, "payroll_run_preview_append_only") {
		t.Error("Head table payroll_run_preview must not carry an append-only trigger")
	}
	if !strings.Contains(sql, "payroll_ledger_entry_append_only") {
		t.Error("Evidence table payroll_ledger_entry must carry an append-only trigger")
	}
}

func TestUpSQLExtractsOnlyUpBody(t *testing.T) {
	sql := RenderMigrationPreview(leaveDomain())
	up := UpSQL(sql)
	if strings.Contains(up, "-- +goose Down") {
		t.Fatal("UpSQL leaked the Down section")
	}
	if !strings.Contains(up, "CREATE TABLE IF NOT EXISTS leave_request_preview") {
		t.Fatal("UpSQL dropped the Up section's own content")
	}
}

func TestStatementsSplitsAndDropsComments(t *testing.T) {
	sql := "-- a comment\nCREATE TABLE x (a int);\n\nALTER TABLE x ENABLE ROW LEVEL SECURITY;\n"
	stmts := Statements(sql)
	if len(stmts) != 2 {
		t.Fatalf("Statements() = %d statements, want 2: %#v", len(stmts), stmts)
	}
	if strings.Contains(stmts[0], "--") {
		t.Fatalf("statement retained a comment: %q", stmts[0])
	}
}
