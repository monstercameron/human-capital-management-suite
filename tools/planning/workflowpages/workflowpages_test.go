package workflowpages

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const recordPath = "../../../definitions/planning/workflow-pages-decisions.yaml"

func TestTodo_WFPAGE_001(t *testing.T) {
	record, err := Load(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if record.Routes.Start != "/workspace/app/workflows/start/{workflow}" || record.Defaults.Draft.ExpiryDays != 30 ||
		record.Defaults.HistoryExport.MaxRows != 10000 || record.Guard.AddNodes {
		t.Fatalf("record = %+v, want the WFPAGE-001 decisions", record)
	}
}

func TestTodo_WFPAGE_001_Golden(t *testing.T) {
	record, err := Load(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join([]string{
		record.Routes.StartCatalog, record.Routes.Start, record.Routes.History,
		record.Defaults.DefaultPage, record.Defaults.DesignerRole, record.Defaults.Validations,
		record.Defaults.Draft.Scope, record.Defaults.SOPLinks, record.Defaults.SupportChannel,
		record.Guard.CustomPagesServe,
	}, "\n")
	const want = "/workspace/app/workflows\n" +
		"/workspace/app/workflows/start/{workflow}\n" +
		"/workspace/app/workflows/history\n" +
		"one generated default page per published workflow version\n" +
		"override layer only (layout, widget choice, notes, guides, links, extra validations)\n" +
		"declarative data evaluated by one rule engine on client and server, server authoritative\n" +
		"one server-side draft per user, workflow and subject\n" +
		"follow the latest deployed version unless pinned\n" +
		"one per workflow with a tenant default\n" +
		"existing governed workflows only"
	if got != want {
		t.Fatalf("record bytes changed:\n%s", got)
	}
}

func TestLoadRefusesBrokenRecords(t *testing.T) {
	dir := t.TempDir()
	valid, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(valid)
	cases := map[string]string{
		"unknown-field":  text + "extra: 1\n",
		"designer-nodes": strings.Replace(text, "page_designer_may_add_nodes: false", "page_designer_may_add_nodes: true", 1),
		"draft-expiry":   strings.Replace(text, "expiry_days: 30", "expiry_days: 7", 1),
		"bad-route":      strings.Replace(text, "/workspace/app/workflows/history", "/history", 1),
	}
	for name, body := range cases {
		if body == text {
			t.Fatalf("%s: mutation did not change the record", name)
		}
		path := filepath.Join(dir, name+".yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := Load(filepath.Join(dir, "missing.yaml")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing file: err = %v, want ErrInvalid", err)
	}
}
