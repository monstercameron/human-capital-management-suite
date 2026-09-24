package exit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validRequest() Request {
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	copies := make([]Copy, 0, len(RequiredCategories))
	for i, category := range RequiredCategories {
		id := "copy-" + string(category)
		copies = append(copies, Copy{ID: id, Tenant: "tenant-1", Category: category, Location: "store-" + string(rune('a'+i)), Owner: "records", Region: "us-east", KeyRef: "key-1", RetentionPolicy: "retain-v1", RestorePolicy: "redelete-v1", FreshAt: at.Add(-time.Hour), Known: true, DeletionSupported: true})
	}
	copies[4].ActiveAuthority = true
	copies[5].ActiveAuthority = true
	copies[6].ActiveAuthority = true
	copies[7].ActiveAuthority = true
	copies[3].RestoreReDeleteNeeded = true
	req := Request{Tenant: "tenant-1", RequestedBy: "operator-1", At: at, Copies: copies, ShutdownComplete: true,
		Revocations: []RevocationReceipt{{ID: "r-provider", Target: "copy-PROVIDER", Kind: "provider", At: at, Success: true}, {ID: "r-credential", Target: "copy-CREDENTIAL", Kind: "credential", At: at, Success: true}, {ID: "r-webhook", Target: "copy-WEBHOOK", Kind: "webhook", At: at, Success: true}, {ID: "r-support", Target: "copy-SUPPORT_GRANT", Kind: "support", At: at, Success: true}},
		PendingWork: []PendingWork{{ID: "work-1", Disposition: "FROZEN", Reason: "exit"}}, HoldExceptions: []HoldException{{ID: "hold-1", CopyID: "copy-CANONICAL", Authority: "legal-1", Reason: "litigation hold"}}, RestoreReDeletes: []RestoreReDelete{{ID: "rd-1", CopyID: "copy-BACKUP", TombstoneDigest: "sha256:tombstone", Watermark: "42", Reapplied: true}}}
	refreshExport(&req)
	return req
}

func refreshExport(req *Request) {
	categories := make([]ExportCategory, 0, len(RequiredCategories))
	for _, category := range RequiredCategories {
		for _, copy := range req.Copies {
			if copy.Category != category {
				continue
			}
			categories = append(categories, ExportCategory{Category: category, CopyID: copy.ID, Fields: []string{"id", "value"}, Records: []json.RawMessage{json.RawMessage(`{"id":"record-1","value":"tenant-data"}`)}})
			break
		}
	}
	pkg, err := BuildExportPackage(ExportBuildRequest{ID: "export-1", Tenant: req.Tenant, Recipient: "customer-1", Copies: req.Copies, Categories: categories})
	if err != nil {
		panic(err)
	}
	req.Export, req.ExportPackage = pkg.Receipt, &pkg
}

func TestTodo_PRIV_EXIT_001(t *testing.T) {
	plan, err := Build(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusCertifiable || len(plan.Blockers) != 0 {
		t.Fatalf("plan=%+v, want certifiable", plan)
	}
	if !strings.Contains(plan.Explain(), "CERTIFIABLE") {
		t.Fatalf("explanation=%q", plan.Explain())
	}
}

func TestTodo_PRIV_EXIT_001_Integration(t *testing.T) {
	req := validRequest()
	req.Copies[0].Tenant = "other-tenant"
	plan, err := Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusBlocked || !hasCode(plan.Blockers, "TENANT_MISMATCH") {
		t.Fatalf("blockers=%+v", plan.Blockers)
	}
}

func TestTodo_PRIV_EXIT_001_Security(t *testing.T) {
	req := validRequest()
	req.Copies[5].ActiveAuthority = true
	req.Revocations[1].Success = false
	plan, err := Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(plan.Blockers, "ACTIVE_AUTHORITY") || !hasCode(plan.Blockers, "REVOCATION_RECEIPT_INVALID") {
		t.Fatalf("blockers=%+v", plan.Blockers)
	}
}

func TestTodo_PRIV_EXIT_001_Recovery(t *testing.T) {
	req := validRequest()
	req.Copies[3].RestoreReDeleteNeeded = true
	req.RestoreReDeletes = nil
	plan, err := Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(plan.Blockers, "RESTORE_REDELETE_MISSING") {
		t.Fatalf("blockers=%+v", plan.Blockers)
	}
}

func TestTodo_PRIV_EXIT_001_Mutation(t *testing.T) {
	req := validRequest()
	first, err := Build(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Copies[0].ID = "mutated"
	second, err := Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.InventoryDigest == second.InventoryDigest || first.Digest == second.Digest {
		t.Fatal("digest did not bind inventory mutation")
	}
}

func FuzzTodo_PRIV_EXIT_001(f *testing.F) {
	f.Add("tenant-1", "operator-1")
	f.Fuzz(func(t *testing.T, tenant, operator string) {
		req := validRequest()
		req.Tenant, req.RequestedBy = tenant, operator
		_, err := Build(req)
		if strings.TrimSpace(tenant) != "" && strings.TrimSpace(operator) != "" {
			if err != nil {
				t.Fatalf("valid-shaped request failed: %v", err)
			}
		}
	})
}

func TestVersionAndExplain(t *testing.T) {
	if Version() != 1 || Explain(validRequestPlan(t)) == "" {
		t.Fatal("contract symbols are not usable")
	}
}

func validRequestPlan(t *testing.T) ExitPlan {
	t.Helper()
	p, err := Build(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func hasCode(blockers []Blocker, code string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}
