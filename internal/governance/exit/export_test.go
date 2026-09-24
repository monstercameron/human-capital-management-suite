package exit

import (
	"encoding/json"
	"errors"
	"testing"
)

func exportFixture() ExportBuildRequest {
	req := validRequest()
	categories := make([]ExportCategory, 0, len(RequiredCategories))
	for i, category := range RequiredCategories {
		categories = append(categories, ExportCategory{Category: category, CopyID: req.Copies[i].ID, Fields: []string{"id", "value"}, Records: []json.RawMessage{json.RawMessage(`{"id":"record-1","value":"tenant-data"}`)}})
	}
	return ExportBuildRequest{ID: "package-1", Tenant: req.Tenant, Recipient: "customer-1", Copies: req.Copies, Categories: categories}
}

func TestTodo_REV_098_01(t *testing.T) {
	req := exportFixture()
	pkg, err := BuildExportPackage(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyExportPackage(pkg); err != nil {
		t.Fatalf("VerifyExportPackage() error = %v", err)
	}
	if pkg.Manifest.SchemaVersion != ExportSchemaVersion || len(pkg.Manifest.Categories) != len(RequiredCategories) || pkg.Receipt.Checksum != pkg.Checksum {
		t.Fatalf("package manifest/receipt incomplete: %+v", pkg)
	}
}

func TestTodo_REV_098_01_Security(t *testing.T) {
	req := exportFixture()
	req.Copies[0].Tenant = "other-tenant"
	if _, err := BuildExportPackage(req); !errors.Is(err, ErrExportPackage) {
		t.Fatalf("cross-tenant inventory error = %v, want ErrExportPackage", err)
	}

	req = exportFixture()
	pkg, err := BuildExportPackage(req)
	if err != nil {
		t.Fatal(err)
	}
	pkg.Bytes = append(pkg.Bytes, []byte(" forged")...)
	if err := VerifyExportPackage(pkg); !errors.Is(err, ErrExportPackage) {
		t.Fatalf("tampered package error = %v, want ErrExportPackage", err)
	}
}

func TestTodo_REV_098_01_Integration(t *testing.T) {
	req := validRequest()
	export := exportFixture()
	plan, pkg, err := ExecuteExit(req, export)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusCertifiable || plan.Export.Checksum != pkg.Checksum || pkg.Manifest.InventoryDigest != plan.InventoryDigest {
		t.Fatalf("exit plan did not accept exporter package digest: status=%s export=%+v", plan.Status, plan.Export)
	}

	forged := req
	forged.Export.Checksum = "sha256:caller-invented"
	forged.Export.ExpectedDigest = forged.Export.Checksum
	forgedPlan, err := Build(forged)
	if err != nil {
		t.Fatal(err)
	}
	if forgedPlan.Status != StatusBlocked || !hasCode(forgedPlan.Blockers, "EXPORT_PACKAGE_UNVERIFIED") {
		t.Fatalf("exit plan accepted receipt detached from exporter package: %+v", forgedPlan.Blockers)
	}

	export.Copies[0].Location = "other-store"
	if _, _, err := ExecuteExit(req, export); !errors.Is(err, ErrExportPackage) {
		t.Fatalf("mismatched export inventory error = %v, want ErrExportPackage", err)
	}
}
