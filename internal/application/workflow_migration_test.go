package application

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type migrationVersionReader struct {
	version.Store
	records map[string]version.CompiledVersion
	errors  map[string]error
}

func (r migrationVersionReader) GetByDigest(digest string) (version.CompiledVersion, bool, error) {
	v, found := r.records[digest]
	return v, found, r.errors[digest]
}

func TestResolveWorkflowMigrationPlans(t *testing.T) {
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	record := version.CompiledVersion{WorkflowID: plan.WorkflowID, CompiledPlanDigest: plan.Digest(), CanonicalPlanBytes: raw}
	storeErr := errors.New("registry unavailable")
	for _, name := range []string{"valid", "source missing", "target missing", "source storage", "target storage", "different workflow", "source decode", "target decode", "digest mismatch"} {
		t.Run(name, func(t *testing.T) {
			r := migrationVersionReader{records: map[string]version.CompiledVersion{"source": record, "target": record}, errors: map[string]error{}}
			want := ""
			switch name {
			case "source missing":
				delete(r.records, "source")
				want = "source digest"
			case "target missing":
				delete(r.records, "target")
				want = "target digest"
			case "source storage":
				r.errors["source"] = storeErr
				want = "load source"
			case "target storage":
				r.errors["target"] = storeErr
				want = "load target"
			case "different workflow":
				v := record
				v.WorkflowID = "other"
				r.records["target"] = v
				want = "differ"
			case "source decode":
				v := record
				v.CanonicalPlanBytes = []byte("invalid")
				r.records["source"] = v
				want = "decode source"
			case "target decode":
				v := record
				v.CanonicalPlanBytes = []byte("invalid")
				r.records["target"] = v
				want = "decode target"
			case "digest mismatch":
				v := record
				v.CompiledPlanDigest = "wrong"
				r.records["target"] = v
				want = "digest does not match"
			}
			source, target, err := ResolveWorkflowMigrationPlans("source", "target", r)
			if want == "" {
				if err != nil || source == nil || target == nil || source.Digest() != plan.Digest() || target.Digest() != plan.Digest() {
					t.Fatalf("resolved source=%v target=%v err=%v", source, target, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), want) || source != nil || target != nil {
				t.Fatalf("source=%v target=%v err=%v, want %s", source, target, err, want)
			}
			if strings.HasSuffix(name, "storage") && !errors.Is(err, storeErr) {
				t.Fatalf("lost storage error: %v", err)
			}
		})
	}
}
