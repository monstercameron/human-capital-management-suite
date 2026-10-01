package project

import (
	"errors"
	"testing"
)

func usageTestPlan(t *testing.T) UsagePlan {
	t.Helper()
	plan, err := NewUsagePlan("pilot", "2026-09", map[UsageDimension]Quota{
		UsageProjects: {Limit: 10, WarningAt: 8}, UsageTasks: {Limit: 100, WarningAt: 80}, UsageFields: {Limit: 50, WarningAt: 40}, UsageFiles: {Limit: 1000, WarningAt: 800},
		UsageEvents: {Limit: 100, WarningAt: 80}, UsageSearch: {Limit: 100, WarningAt: 80}, UsageAITokens: {Limit: 1000, WarningAt: 800}, UsageAISpendMicros: {Limit: 10000, WarningAt: 8000},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestTodo_PM_068(t *testing.T) {
	meter, err := NewUsageMeter(usageTestPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := meter.Admit(UsageRequest{TenantID: "tenant-a", ProjectID: "project-a", Operation: "task-create", Delta: UsageDelta{UsageTasks: 79, UsageEvents: 1}})
	if err != nil || admitted.Status != UsageAdmitted {
		t.Fatalf("initial usage admission = %+v, %v", admitted, err)
	}
	warning, err := meter.Admit(UsageRequest{TenantID: "tenant-a", ProjectID: "project-a", Operation: "task-batch", Delta: UsageDelta{UsageTasks: 1, UsageSearch: 80}})
	if err != nil || warning.Status != UsageWarning || len(warning.Warnings) != 2 || warning.Warnings[0] != UsageTasks || warning.Warnings[1] != UsageSearch {
		t.Fatalf("warning admission = %+v, %v", warning, err)
	}
	rejected, err := meter.Admit(UsageRequest{TenantID: "tenant-a", ProjectID: "project-b", Operation: "task-batch", Delta: UsageDelta{UsageTasks: 21, UsageFiles: 1001}})
	if !errors.Is(err, ErrUsageLimitReached) || rejected.Status != UsageRejected || len(rejected.Rejected) != 2 {
		t.Fatalf("limit rejection = %+v, %v", rejected, err)
	}
	export, err := meter.Export("tenant-a")
	if err != nil || export.Totals[UsageTasks] != 80 || export.Totals[UsageFiles] != 0 || len(export.Projects) != 1 {
		t.Fatalf("usage export = %+v, %v", export, err)
	}
}

func TestTodo_PM_068_Integration(t *testing.T) {
	meter, err := NewUsageMeter(usageTestPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []UsageRequest{
		{TenantID: "tenant-a", ProjectID: "project-b", Operation: "ai-run", CostMicros: 2000, Delta: UsageDelta{UsageAITokens: 200, UsageAISpendMicros: 2000}},
		{TenantID: "tenant-a", ProjectID: "project-a", Operation: "file-upload", Delta: UsageDelta{UsageFiles: 500, UsageEvents: 1}},
	} {
		if _, err := meter.Admit(request); err != nil {
			t.Fatal(err)
		}
	}
	export, err := meter.Export("tenant-a")
	if err != nil || len(export.Attributions) != 4 || export.Projects[0].ProjectID != "project-a" || export.Projects[1].ProjectID != "project-b" || export.Attributions[2].CostMicros != 2000 {
		t.Fatalf("attributed usage export = %+v, %v", export, err)
	}
}

func TestTodo_PM_068_Golden(t *testing.T) {
	meter, err := NewUsageMeter(usageTestPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := meter.Admit(UsageRequest{TenantID: "tenant-a", ProjectID: "project-a", Operation: "task-create", Delta: UsageDelta{UsageTasks: 2, UsageEvents: 1}}); err != nil {
		t.Fatal(err)
	}
	export, err := meter.Export("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:791eba731247afa73391ea649c81d378e1f7b2df3f1c55701439080eed0410eb"
	if export.Digest != want {
		t.Fatalf("usage export digest = %q, want %q", export.Digest, want)
	}
}

func BenchmarkTodo_PM_068(b *testing.B) {
	plan, err := NewUsagePlan("bench", "v1", map[UsageDimension]Quota{
		UsageProjects: {Limit: 1_000_000, WarningAt: 900_000}, UsageTasks: {Limit: 1_000_000, WarningAt: 900_000}, UsageFields: {Limit: 1_000_000, WarningAt: 900_000}, UsageFiles: {Limit: 1_000_000, WarningAt: 900_000},
		UsageEvents: {Limit: 1_000_000, WarningAt: 900_000}, UsageSearch: {Limit: 1_000_000, WarningAt: 900_000}, UsageAITokens: {Limit: 1_000_000, WarningAt: 900_000}, UsageAISpendMicros: {Limit: 1_000_000, WarningAt: 900_000},
	})
	if err != nil {
		b.Fatal(err)
	}
	meter, err := NewUsageMeter(plan)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := meter.Admit(UsageRequest{TenantID: "tenant-bench", ProjectID: "project-bench", Operation: "task", Delta: UsageDelta{UsageTasks: 1}}); err != nil {
			b.Fatal(err)
		}
	}
}
