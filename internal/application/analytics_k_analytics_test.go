package application

import (
	"testing"

	dataanalytics "github.com/monstercameron/human-capital-management-suite/internal/data/analytics"
)

// TestTodo_ANALYTICS_001_Integration proves the analytics substrate is
// composed into the same serve application that owns the deployed listeners.
func TestTodo_ANALYTICS_001_Integration(t *testing.T) {
	composed, _, _ := composeStub(t, stubServeConfig())
	service := composed.Analytics()
	snapshot, err := service.BuildSnapshot(dataanalytics.SnapshotRequest{
		TenantID: "tenant-served", SnapshotID: "analytics-served-1",
		SourceWatermark: "watermark-1", PolicyDigest: "policy-served-1",
		ProvenanceRef: "ledger-served-1", AllowedFields: []string{"amount"},
		Records: []dataanalytics.Record{{
			TenantID: "tenant-served", Entity: "worker", ID: "worker-1",
			SourceRef: "ledger@1", SourceWatermark: "watermark-1",
			Fields: map[string]string{"amount": "42"},
		}},
	})
	if err != nil {
		t.Fatalf("served analytics snapshot: %v", err)
	}
	result, err := service.Query(snapshot, dataanalytics.Query{TenantID: "tenant-served", Entity: "worker"})
	if err != nil {
		t.Fatalf("served analytics query: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0].ID != "worker-1" || result.SnapshotDigest == "" {
		t.Fatalf("served analytics result = %+v, want one identified row and snapshot digest", result)
	}
}
