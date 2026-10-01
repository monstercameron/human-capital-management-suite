package modelgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/storagemanifest"
	"gopkg.in/yaml.v3"
)

// TestTodo_MSRC_008 is the MSRC-008 primary test. It proves the model
// registry yields a complete typed SQL policy row for every entity and that
// review previews are CREATE TABLE inputs owned by this generator, never
// applied migrations.
func TestTodo_MSRC_008(t *testing.T) {
	reg, err := model.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := BuildSQLArtifacts(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts.Disposition.Entities) != len(reg.Entities()) {
		t.Fatalf("disposition rows = %d, want %d", len(artifacts.Disposition.Entities), len(reg.Entities()))
	}
	if artifacts.Disposition.SpecificationOwner != SpecificationOwner {
		t.Fatalf("specification owner = %q, want %q", artifacts.Disposition.SpecificationOwner, SpecificationOwner)
	}
	if len(artifacts.Previews) == 0 {
		t.Fatal("generated no migration previews")
	}
	for _, row := range artifacts.Disposition.Entities {
		if row.SourceRef == "" || row.EntityKey == "" || row.TableName == "" || row.Disposition == "" || row.RetentionClass == "" || row.EncryptionClass == "" {
			t.Fatalf("incomplete SQL disposition row: %+v", row)
		}
		if row.TenantScoped && row.TenantColumn != "tenant_id" {
			t.Fatalf("%s tenant column = %q, want tenant_id", row.SourceRef, row.TenantColumn)
		}
	}
	for _, preview := range artifacts.Previews {
		if strings.Contains(preview.FileName, "migrations") || strings.Contains(preview.FileName, "..") {
			t.Fatalf("preview escaped its owned testdata directory: %+v", preview)
		}
		if !strings.Contains(preview.SQL, "CREATE TABLE IF NOT EXISTS "+preview.TableName) {
			t.Fatalf("preview %s does not create %s", preview.FileName, preview.TableName)
		}
	}
	byKey := map[string]SQLDisposition{}
	for _, row := range artifacts.Disposition.Entities {
		byKey[row.EntityKey] = row
	}
	if !byKey["person"].AppendOnly || byKey["person"].TableName != "ledger_event" {
		t.Fatalf("person disposition lost append-only ledger semantics: %+v", byKey["person"])
	}
	if !byKey["evidence_record"].AppendOnly || byKey["evidence_record"].EncryptionClass != "PLAINTEXT" {
		t.Fatalf("evidence record policy = %+v, want append-only artifact with its source classification", byKey["evidence_record"])
	}
}

// TestTodo_MSRC_008_Golden compares every non-mismatch row in the checked-in
// DB-002 storage disposition against the model-derived set and pins the full
// disposition/preview output digest in testdata/preview.
func TestTodo_MSRC_008_Golden(t *testing.T) {
	root := repoRootForTest(t)
	reg, err := model.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := BuildSQLArtifacts(reg)
	if err != nil {
		t.Fatal(err)
	}
	current, err := readStorageDisposition(filepath.Join(root, "definitions", "model", "storage-disposition.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if errs := CompareStorageDisposition(artifacts.Disposition, current); len(errs) != 0 {
		t.Fatalf("model/storage disposition overlap drift: %v", errs)
	}
	golden, err := os.ReadFile(filepath.Join(root, OutputPreviewDir, "generated-set.golden"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("digest: %s\npreviews: %d\n", SQLArtifactsDigest(artifacts), len(artifacts.Previews))
	if string(golden) != want {
		t.Fatalf("generated set golden differs:\n got %q\nwant %q", string(golden), want)
	}
	files, err := RenderPreviewFiles(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(artifacts.Previews) {
		t.Fatalf("rendered preview file count = %d, want %d", len(files), len(artifacts.Previews))
	}
}

// TestTodo_MSRC_008_Mutation proves the overlap check notices a generated
// table/target conflict instead of accepting a stale storage disposition.
func TestTodo_MSRC_008_Mutation(t *testing.T) {
	root := repoRootForTest(t)
	reg, err := model.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := BuildSQLArtifacts(reg)
	if err != nil {
		t.Fatal(err)
	}
	ownerless := artifacts
	ownerless.Disposition.SpecificationOwner = ""
	if SQLArtifactsDigest(ownerless) == SQLArtifactsDigest(artifacts) {
		t.Fatal("removing the specification owner did not change the generated artifact digest")
	}
	current, err := readStorageDisposition(filepath.Join(root, "definitions", "model", "storage-disposition.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range current.Entities {
		if current.Entities[i].Disposition != storagemanifest.DispositionMismatch {
			current.Entities[i].Target = "table mutated_by_test"
			if errs := CompareStorageDisposition(artifacts.Disposition, current); len(errs) == 0 {
				t.Fatalf("mutating %s target did not change overlap result", current.Entities[i].EntityRef)
			}
			return
		}
	}
	t.Fatal("storage disposition has no non-mismatch row to mutate")
}

func TestWritePreviewFilesStaysUnderOwnedDirectory(t *testing.T) {
	root := t.TempDir()
	if err := WritePreviewFiles(root, map[string][]byte{"sample.sql": []byte("-- preview\n")}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(OutputPreviewDir), "sample.sql")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("preview was not written under %s: %v", OutputPreviewDir, err)
	}
	if err := WritePreviewFiles(root, map[string][]byte{"..\\migrations\\bad.sql": nil}); err == nil {
		t.Fatal("WritePreviewFiles accepted a path outside the preview directory")
	}
}

func readStorageDisposition(path string) (storagemanifest.DispositionManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return storagemanifest.DispositionManifest{}, err
	}
	var manifest storagemanifest.DispositionManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return storagemanifest.DispositionManifest{}, err
	}
	return manifest, nil
}
