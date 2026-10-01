package project

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/records"
)

func pm025Inventory(t *testing.T) Inventory {
	t.Helper()
	return Inventory{
		TenantID:      "tenant-project",
		ProjectID:     "project-1",
		CoveredSeries: RequiredRecordSeries(),
		Records: []Record{
			{ID: "task-visible", TenantID: "tenant-project", ProjectID: "project-1", Series: RecordSeriesTasks, Readers: []string{"member-1"}, Versions: []RecordVersion{{ID: "task-visible:v1", Revision: 1}}, Copies: pm025Copies("task-visible")},
			{ID: "task-hidden", TenantID: "tenant-project", ProjectID: "project-1", Series: RecordSeriesTasks, Readers: []string{"member-2"}, Versions: []RecordVersion{{ID: "task-hidden:v1", Revision: 1}}, Copies: pm025Copies("task-hidden")},
			{ID: "comment-held", TenantID: "tenant-project", ProjectID: "project-1", Series: RecordSeriesComments, Readers: []string{"member-1"}, Versions: []RecordVersion{{ID: "comment-held:v1", Revision: 1}, {ID: "comment-held:v2", Revision: 2, Tombstone: true}}, Copies: pm025Copies("comment-held")},
		},
		Holds: []Hold{{ID: "hold-1", TenantID: "tenant-project", ProjectID: "project-1", RecordID: "comment-held", VersionIDs: []string{"comment-held:v1", "comment-held:v2"}, Authority: "records-counsel", Reason: "litigation", Active: true}},
	}
}

func pm025Copies(id string) []records.DeletableCopy {
	return []records.DeletableCopy{
		{ID: id + ":canonical", Kind: records.CopyKindCanonical, Tenant: "tenant-project"},
		{ID: id + ":derived", Kind: records.CopyKindDerived, Tenant: "tenant-project"},
		{ID: id + ":external", Kind: records.CopyKindExternal, Tenant: "tenant-project"},
		{ID: id + ":backup", Kind: records.CopyKindBackup, Tenant: "tenant-project", ReDeleted: true, ReDeleteRef: "redelete-1"},
		{ID: id + ":restored", Kind: records.CopyKindRestored, Tenant: "tenant-project", TombstoneDigest: "tombstone-1"},
	}
}

func TestTodo_PM_025(t *testing.T) {
	inventory := pm025Inventory(t)
	if err := inventory.Validate(); err != nil {
		t.Fatal(err)
	}
	export, err := inventory.Export(ExportRequest{TenantID: "tenant-project", ProjectID: "project-1", Requester: "member-1"})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(export.Records, func(record ExportRecord) bool { return record.ID == "task-hidden" }) {
		t.Fatal("export included an inaccessible task")
	}

	result, err := inventory.VerifyDisposition("comment-held", "records-operator", time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DispositionHeld || !result.Certificate.Complete || !slices.Equal(result.RetainedVersion, []string{"comment-held:v1", "comment-held:v2"}) {
		t.Fatalf("held disposition = %+v", result)
	}
	for _, outcome := range result.Certificate.Outcomes {
		if outcome.Outcome != records.OutcomeException {
			t.Fatalf("held version copy was disposed: %+v", outcome)
		}
	}
}

func TestTodo_PM_025_Integration(t *testing.T) {
	inventory := pm025Inventory(t)
	if got := RequiredRecordSeries(); len(got) != 7 || !slices.Contains(got, RecordSeriesAIEvidence) || !slices.Contains(got, RecordSeriesAttachments) || !slices.Contains(got, RecordSeriesExports) {
		t.Fatalf("required project series = %v", got)
	}
	if err := inventory.Validate(); err != nil {
		t.Fatalf("project series inventory rejected: %v", err)
	}
	if _, err := inventory.VerifyDisposition("task-visible", "records-operator", time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("unheld project record should produce a verified disposal certificate: %v", err)
	}
}

func TestTodo_PM_025_Security(t *testing.T) {
	inventory := pm025Inventory(t)
	if _, err := inventory.Export(ExportRequest{TenantID: "other-tenant", ProjectID: "project-1", Requester: "member-1"}); !errors.Is(err, ErrRecordUnauthorized) {
		t.Fatalf("cross-tenant export error = %v", err)
	}
	if _, err := inventory.Export(ExportRequest{TenantID: "tenant-project", ProjectID: "other-project", Requester: "member-1"}); !errors.Is(err, ErrRecordUnauthorized) {
		t.Fatalf("cross-project export error = %v", err)
	}
}

func TestTodo_PM_025_Golden(t *testing.T) {
	inventory := pm025Inventory(t)
	one, err := inventory.Export(ExportRequest{TenantID: "tenant-project", ProjectID: "project-1", Requester: "member-1"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := inventory.Export(ExportRequest{TenantID: "tenant-project", ProjectID: "project-1", Requester: "member-1"})
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest == "" || one.Digest != two.Digest {
		t.Fatalf("export digest is not stable: %q vs %q", one.Digest, two.Digest)
	}
	raw, err := json.Marshal(one.Records)
	if err != nil {
		t.Fatal(err)
	}
	const want = `[{"ID":"comment-held","Series":"project.comments","Versions":[{"ID":"comment-held:v1","Revision":1,"Tombstone":false},{"ID":"comment-held:v2","Revision":2,"Tombstone":true}]},{"ID":"task-visible","Series":"project.tasks","Versions":[{"ID":"task-visible:v1","Revision":1,"Tombstone":false}]}]`
	if string(raw) != want {
		t.Fatalf("authorized export bytes = %s, want %s", raw, want)
	}
}
