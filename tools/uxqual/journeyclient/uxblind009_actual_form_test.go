package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestTodo_UXBLIND_009_ActualProposalForm(t *testing.T) {
	options := testWorkforceOptions()
	options.PromotionPaths = append(options.PromotionPaths, &journeyv1.PromotionPathOption{
		PathRef: "path-ops-clinic-nurse4", SourceJobCode: "OPS-HRBP2", SourceGrade: "P2",
		TargetJobCode: "CLN-NURSE4", TargetGrade: "N4",
	})
	workers := testWorkers()
	worker := findWorker(workers, "omar-reyes")

	gradeOptions := func(values map[string]string) (string, []journey.Option) {
		t.Helper()
		form := ProposalForm(values, workers, worker.GetWorkerRef(), options)
		field, ok := fieldByID(form.Fields, FieldGrade)
		if !ok {
			t.Fatal("actual proposal form has no target-grade field")
		}
		return field.Value, field.Options
	}

	value, grades := gradeOptions(nil)
	if value != "" || len(grades) != 1 || grades[0].Value != "" {
		t.Fatalf("grade field before role selection = value %q, options %+v; want only an empty prompt", value, grades)
	}

	value, grades = gradeOptions(map[string]string{FieldJobCode: "OPS-HRBP3"})
	if value != "P3" || len(grades) != 2 || grades[1].Value != "P3" || !grades[1].Selected {
		t.Fatalf("grade field for OPS-HRBP3 = value %q, options %+v; want only P3 selected", value, grades)
	}

	value, grades = gradeOptions(map[string]string{FieldJobCode: "CLN-NURSE4", FieldGrade: "P3"})
	if value != "N4" || len(grades) != 2 || grades[1].Value != "N4" || !grades[1].Selected {
		t.Fatalf("grade field after changing role with stale P3 = value %q, options %+v; want only N4 selected", value, grades)
	}
}
