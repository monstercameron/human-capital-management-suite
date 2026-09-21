package deferredschema

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_DB_016_Conformance proves the validator accepts today's generated
// preview set outright: every table declares RLS, every append-only table
// (leave_record_preview, payroll_ledger_entry, ... per the disposition preview)
// carries forbid_mutation, and none of the twenty table names collide with
// migrations/, definitions/storage/storage-disposition.yaml or a
// write-capable Phase 1 capability.
func TestTodo_DB_016_Conformance(t *testing.T) {
	domains := Domains()
	set, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	root := testRepoRoot(t)
	report, err := Validate(root, domains, set)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := report.Error(); err != nil {
		t.Fatalf("expected a clean report, got: %v", err)
	}
}

func TestParseMigrationPreviewFindsBothTables(t *testing.T) {
	sql := RenderMigrationPreview(caseDomain())
	findings := ParseMigrationPreview(sql)
	if len(findings) != 2 {
		t.Fatalf("ParseMigrationPreview found %d tables, want 2", len(findings))
	}
	byName := map[string]TableFinding{}
	for _, f := range findings {
		byName[f.Table] = f
	}
	head, ok := byName["hr_case"]
	if !ok || !head.HasRLSEnable || !head.HasForceRLS || !head.HasTenantIsolationPolicy {
		t.Fatalf("hr_case finding incomplete: %+v (ok=%v)", head, ok)
	}
	if head.HasForbidMutationTrigger {
		t.Fatal("hr_case (mutable Head) must not report a forbid_mutation trigger")
	}
	evidence, ok := byName["case_transition"]
	if !ok || !evidence.HasForbidMutationTrigger {
		t.Fatalf("case_transition finding missing forbid_mutation: %+v (ok=%v)", evidence, ok)
	}
}

// TestTodo_DB_016_Mutation proves the validator is not a rubber stamp: each
// mutation below breaks exactly one GREEN guarantee, and Validate must catch
// every one of them.
func TestTodo_DB_016_Mutation(t *testing.T) {
	domains := Domains()
	root := testRepoRoot(t)

	baseline, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	t.Run("strip RLS from a table", func(t *testing.T) {
		set := cloneSet(baseline)
		mutateFile(t, &set, "004_leave.sql", func(sql string) string {
			return strings.Replace(sql, "ALTER TABLE leave_request_preview FORCE ROW LEVEL SECURITY;\n", "", 1)
		})
		report, err := Validate(root, domains, set)
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if report.Empty() {
			t.Fatal("expected MissingRLS violation, got a clean report")
		}
		if !containsStr(report.MissingRLS, "leave_request_preview") {
			t.Fatalf("expected leave_request_preview in MissingRLS, got %v", report.MissingRLS)
		}
	})

	t.Run("strip forbid_mutation trigger from an append-only table", func(t *testing.T) {
		set := cloneSet(baseline)
		mutateFile(t, &set, "004_leave.sql", func(sql string) string {
			return strings.Replace(sql,
				"CREATE OR REPLACE TRIGGER leave_record_preview_append_only\n    BEFORE UPDATE OR DELETE ON leave_record_preview\n    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();\n\n",
				"", 1)
		})
		report, err := Validate(root, domains, set)
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if report.Empty() {
			t.Fatal("expected MissingForbidMutation violation, got a clean report")
		}
		if !containsStr(report.MissingForbidMutation, "leave_record_preview") {
			t.Fatalf("expected leave_record_preview in MissingForbidMutation, got %v", report.MissingForbidMutation)
		}
	})

	t.Run("rename a preview table to collide with a live migration", func(t *testing.T) {
		set := cloneSet(baseline)
		mutateFile(t, &set, "002_benefits.sql", func(sql string) string {
			return strings.ReplaceAll(sql, "benefit_election_revision", "tenant")
		})
		mutateDisposition(t, &set, func(y string) string {
			return strings.ReplaceAll(y, "benefit_election_revision", "tenant")
		})
		report, err := Validate(root, domains, set)
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if report.Empty() {
			t.Fatal("expected a LiveMigrationCollisions violation, got a clean report")
		}
		if !containsStr(report.LiveMigrationCollisions, "tenant") {
			t.Fatalf("expected tenant in LiveMigrationCollisions, got %v", report.LiveMigrationCollisions)
		}
	})

	t.Run("disposition value outside DRAFT/CONFORMANCE", func(t *testing.T) {
		set := cloneSet(baseline)
		mutateDisposition(t, &set, func(y string) string {
			return strings.Replace(y, "disposition: DRAFT", "disposition: APPROVED", 1)
		})
		report, err := Validate(root, domains, set)
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if report.Empty() {
			t.Fatal("expected an UnknownDispositionValues violation, got a clean report")
		}
	})
}

func cloneSet(p PreviewSet) PreviewSet {
	out := PreviewSet{Files: make([]PreviewFile, len(p.Files))}
	copy(out.Files, p.Files)
	return out
}

func mutateFile(t *testing.T, p *PreviewSet, name string, mutate func(string) string) {
	t.Helper()
	for i, f := range p.Files {
		if f.Name == name {
			before := f.Content
			after := mutate(before)
			if after == before {
				t.Fatalf("mutation for %s was a no-op; test fixture is stale", name)
			}
			p.Files[i].Content = after
			return
		}
	}
	t.Fatalf("no preview file named %s", name)
}

func mutateDisposition(t *testing.T, p *PreviewSet, mutate func(string) string) {
	mutateFile(t, p, "storage-disposition.deferred.yaml", mutate)
}

func TestLiveMigrationTableNamesFindsKnownTables(t *testing.T) {
	root := testRepoRoot(t)
	names, err := LiveMigrationTableNames(filepath.Join(root, "migrations"))
	if err != nil {
		t.Fatalf("LiveMigrationTableNames: %v", err)
	}
	for _, want := range []string{"tenant", "intent_instance", "journey_worker"} {
		if !names[want] {
			t.Errorf("expected %q among live migration table names", want)
		}
	}
	// None of this package's own preview tables should be live yet.
	for _, row := range DispositionRows(Domains()) {
		if names[row.Table] {
			t.Errorf("preview table %q already exists in migrations/; domains.go must pick a new name", row.Table)
		}
	}
}

func TestLiveDispositionTableNamesFindsKnownTables(t *testing.T) {
	root := testRepoRoot(t)
	names, err := LiveDispositionTableNames(filepath.Join(root, "definitions", "storage", "storage-disposition.yaml"))
	if err != nil {
		t.Fatalf("LiveDispositionTableNames: %v", err)
	}
	if !names["tenant"] {
		t.Error("expected tenant among live disposition table names")
	}
	for _, row := range DispositionRows(Domains()) {
		if names[row.Table] {
			t.Errorf("preview table %q already registered in definitions/storage/storage-disposition.yaml", row.Table)
		}
	}
}

func TestCapabilityRegistryWriteAuthorityCleanToday(t *testing.T) {
	slugs := make([]string, 0)
	for _, d := range Domains() {
		slugs = append(slugs, d.Slug)
	}
	violations, err := CapabilityRegistryWriteAuthority(slugs)
	if err != nil {
		t.Fatalf("CapabilityRegistryWriteAuthority: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected zero violations against today's bootstrap registry, got %+v", violations)
	}
}

func TestValidationReportErrorNilWhenEmpty(t *testing.T) {
	var r ValidationReport
	if err := r.Error(); err != nil {
		t.Fatalf("expected nil error for an empty report, got %v", err)
	}
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
