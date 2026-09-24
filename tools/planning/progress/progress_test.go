package progress

import (
	"strings"
	"testing"
	"time"
)

func TestProgressRejectsUncheckedEvidence(t *testing.T) {
	const markdown = `## Example

- [ ] ` + "`OPEN-001`" + ` **[P0][LUNA] An authored item.**
  - **Depends:** none.
  - **TEST:** ` + "`TestOpen`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=TestOpen`" + `.
  - **RED:** the missing behavior is detected.
  - **GREEN:** the behavior is implemented.
  - **REFACTOR:** keep the implementation small.
  - **Refs:** [example](plan.md).
- [x] ` + "`NO-EVIDENCE-001`" + ` **[P0][LUNA] A checked item without evidence.**
  - **Depends:** none.
  - **TEST:** ` + "`TestNoEvidence`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=TestNoEvidence`" + `.
  - **RED:** the missing behavior is detected.
  - **GREEN:** the behavior is implemented.
  - **REFACTOR:** keep the implementation small.
  - **Refs:** [example](plan.md).
- [x] ` + "`STALE-001`" + ` **[P0][LUNA] A checked item with stale evidence.**
  - **Depends:** none.
  - **TEST:** ` + "`TestStale`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=TestStale`" + `.
  - **RED:** the missing behavior is detected.
  - **GREEN:** the behavior is implemented.
  - **REFACTOR:** keep the implementation small.
  - **Refs:** [example](plan.md).
  - **Evidence (2026-07-01):** ` + "`TestStale`" + ` in ` + "`tools/planning/progress`" + `; ` + "`go test -count=1 ./tools/planning/progress/...`" + ` PASS on windows/arm64 (Go 1.26.3); branch example.
- [x] ` + "`EVIDENCED-001`" + ` **[P0][LUNA] A checked item with fresh evidence.**
  - **Depends:** none.
  - **TEST:** ` + "`TestEvidence`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=TestEvidence`" + `.
  - **RED:** the missing behavior is detected.
  - **GREEN:** the behavior is implemented.
  - **REFACTOR:** keep the implementation small.
  - **Refs:** [example](plan.md).
  - **Evidence (2026-09-03):** ` + "`TestEvidence`" + ` in ` + "`tools/planning/progress`" + `; ` + "`go test -count=1 ./tools/planning/progress/...`" + ` PASS on windows/arm64 (Go 1.26.3); branch example; refactored.
- [x] ` + "`ACCEPTED-001`" + ` **[P0][LUNA] A fully accepted item.**
  - **Depends:** none.
  - **TEST:** ` + "`TestAccepted`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=TestAccepted`" + `.
  - **RED:** the missing behavior is detected.
  - **GREEN:** the behavior is implemented.
  - **REFACTOR:** keep the implementation small.
  - **Refs:** [example](plan.md).
  - **Evidence (2026-09-03):** ` + "`TestAccepted`" + ` in ` + "`tools/planning/progress`" + `; ` + "`go test -count=1 ./tools/planning/progress/...`" + ` PASS on windows/arm64 (Go 1.26.3); branch example; refactored; gate-accepted.
- [x] ` + "`FAILED-001`" + ` **[P0][LUNA] A checked item with a failed fresh run.**
  - **Depends:** none.
  - **TEST:** ` + "`TestFailed`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=TestFailed`" + `.
  - **RED:** a failing run remains red.
  - **GREEN:** a later passing run completes the behavior.
  - **REFACTOR:** keep the implementation small.
  - **Refs:** [example](plan.md).
  - **Evidence (2026-09-03):** ` + "`TestFailed`" + ` in ` + "`tools/planning/progress`" + `; ` + "`go test -count=1 ./tools/planning/progress/...`" + ` FAIL on windows/arm64 (Go 1.26.3); branch example; refactored; gate-accepted.
`

	report, err := SummarizeMarkdown(markdown, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SummarizeMarkdown: %v", err)
	}

	if got := report.Count(StageAuthored); got != 6 {
		t.Errorf("authored count = %d, want 6", got)
	}
	if got := report.Count(StageRed); got != 2 {
		t.Errorf("red count = %d, want 2 (unchecked and failed items), got report %s", got, report)
	}
	if got := report.Count(StageGreen); got != 5 {
		t.Errorf("green count = %d, want 5", got)
	}
	if got := report.Count(StageEvidenced); got != 3 {
		t.Errorf("evidenced count = %d, want 3", got)
	}
	if got := report.Count(StageGateAccepted); got != 1 {
		t.Errorf("gate-accepted count = %d, want 1", got)
	}
	if got := report.Count(StageComplete); got != 2 {
		t.Errorf("complete count = %d, want 2", got)
	}
	failed, ok := report.Item("FAILED-001")
	if !ok {
		t.Fatal("missing report item FAILED-001")
	}
	if !failed.Green || !failed.Red || !failed.Evidenced || failed.Refactored || failed.GateAccepted || failed.Complete {
		t.Errorf("fresh failed run did not fail closed: %+v", failed)
	}

	for _, id := range []string{"NO-EVIDENCE-001", "STALE-001"} {
		item, ok := report.Item(id)
		if !ok {
			t.Fatalf("missing report item %s", id)
		}
		if !item.Green {
			t.Errorf("%s: Green = false, want true", id)
		}
		if !item.Complete {
			// This is the core GOV-015 invariant: a checkbox alone never
			// turns a todo into a completed/evidenced item.
			continue
		}
		t.Errorf("%s was falsely reported complete: %+v", id, item)
	}

	item, _ := report.Item("NO-EVIDENCE-001")
	if !containsIssue(item, "missing evidence") {
		t.Errorf("missing-evidence item issues = %v", item.Issues)
	}
	item, _ = report.Item("STALE-001")
	if !containsIssue(item, "stale evidence") {
		t.Errorf("stale item issues = %v", item.Issues)
	}
	item, _ = report.Item("EVIDENCED-001")
	if !item.Evidenced || item.GateAccepted || !item.Complete {
		t.Errorf("fresh but unaccepted item = %+v, want completed implementation without gate acceptance", item)
	}
}

func TestTodo_GOV_015_Golden(t *testing.T) {
	const markdown = `- [x] ` + "`GOLDEN-015`" + ` **[P0][LUNA] Complete.**
  - **Depends:** none.
  - **TEST:** ` + "`TestGolden`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=TestGolden`" + `.
  - **RED:** detect the gap.
  - **GREEN:** report the state.
  - **REFACTOR:** preserve stable output.
  - **Refs:** [example](plan.md).
  - **Evidence (2026-09-03):** ` + "`TestGolden`" + ` in ` + "`tools/planning/progress`" + `; ` + "`go test -count=1 ./tools/planning/progress/...`" + ` PASS on windows/arm64 (Go 1.26.3); branch example; refactored; gate-accepted.
`

	report, err := SummarizeMarkdown(markdown, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SummarizeMarkdown: %v", err)
	}
	const want = "AUTHORED=1 RED=0 GREEN=1 REFACTORED=1 EVIDENCED=1 GATE_ACCEPTED=1 COMPLETE=1"
	if got := report.String(); got != want {
		t.Errorf("report changed:\n got:  %s\n want: %s", got, want)
	}
}

func containsIssue(item Item, want string) bool {
	for _, issue := range item.Issues {
		if strings.Contains(issue, want) {
			return true
		}
	}
	return false
}
