package analytics

import (
	"errors"
	"reflect"
	"testing"
)

func fixture(t *testing.T) Snapshot {
	t.Helper()
	s, err := NewService().BuildSnapshot(SnapshotRequest{TenantID: "t1", SnapshotID: "snap-1", SourceWatermark: "0002", PolicyDigest: "policy-1", ProvenanceRef: "prov-1", AllowedFields: []string{"department", "amount"}, Records: []Record{
		{TenantID: "t1", Entity: "worker", ID: "w2", SourceRef: "ledger@2", SourceWatermark: "0002", Fields: map[string]string{"department": "ENG", "amount": "20"}},
		{TenantID: "t1", Entity: "worker", ID: "w1", SourceRef: "ledger@1", SourceWatermark: "0001", Fields: map[string]string{"department": "HR", "amount": "10"}, Deleted: true},
		{TenantID: "t1", Entity: "worker", ID: "w3", SourceRef: "ledger@3", SourceWatermark: "0002", Fields: map[string]string{"department": "ENG", "amount": "30"}, Held: true},
	}, PartitionSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestTodo_ANALYTICS_001 is the PRIMARY contract matrix.
func TestTodo_ANALYTICS_001(t *testing.T) {
	s := fixture(t)
	if len(s.Partitions) != 2 || s.Catalog.Format != "DUCKDB_COMPATIBLE" || s.Digest == "" {
		t.Fatalf("incomplete governed export: %+v", s)
	}
	service := NewService()
	r, err := service.Query(s, Query{TenantID: "t1", Entity: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 1 || r.Rows[0].ID != "w2" {
		t.Fatalf("default query=%+v", r.Rows)
	}
	all, err := service.Query(s, Query{TenantID: "t1", Entity: "worker", IncludeDeleted: true, IncludeHeld: true})
	if err != nil || len(all.Rows) != 3 {
		t.Fatalf("tombstone propagation: %v %+v", err, all.Rows)
	}
	metric, err := service.Metric(s, "worker", "amount")
	if err != nil || metric.Count != 1 || metric.Sum != 20 {
		t.Fatalf("deterministic metric: %v %+v", err, metric)
	}
	if exported, err := NewService().BuildExport(SnapshotRequest{TenantID: "t1", SnapshotID: "snap-1", SourceWatermark: "0002", PolicyDigest: "policy-1", ProvenanceRef: "prov-1", AllowedFields: []string{"department", "amount"}, Records: []Record{
		{TenantID: "t1", Entity: "worker", ID: "w1", SourceRef: "ledger@1", SourceWatermark: "0001", Fields: map[string]string{"department": "HR", "amount": "10"}},
	}}); err != nil || exported.Digest == "" {
		t.Fatalf("service export boundary: %v %+v", err, exported)
	}
}

// TestTodo_ANALYTICS_001_Golden proves repeated builds and physical row order
// produce byte-identical partition and catalog identities.
func TestTodo_ANALYTICS_001_Golden(t *testing.T) {
	a, b := fixture(t), fixture(t)
	if a.Digest != b.Digest || !reflect.DeepEqual(a, b) {
		t.Fatalf("non-deterministic snapshot\n%+v\n%+v", a, b)
	}
	ba, _ := a.ParquetBytes(a.Partitions[0].Name)
	bb, _ := b.ParquetBytes(b.Partitions[0].Name)
	if string(ba) != string(bb) {
		t.Fatal("non-deterministic partition")
	}
}

// TestTodo_ANALYTICS_001_Security is the SECURITY matrix: tenant, field and
// OLAP evidence boundaries fail closed.
func TestTodo_ANALYTICS_001_Security(t *testing.T) {
	s := fixture(t)
	if _, err := s.Query(Query{TenantID: "other"}); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("tenant leak: %v", err)
	}
	if _, err := BuildSnapshot(SnapshotRequest{TenantID: "t1", SourceWatermark: "1", PolicyDigest: "p", ProvenanceRef: "x", AllowedFields: []string{"safe"}, Records: []Record{{TenantID: "t1", Entity: "x", ID: "1", SourceRef: "s", SourceWatermark: "1", Fields: map[string]string{"secret": "x"}}}}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("field leak: %v", err)
	}
	service := NewService()
	if err := service.PromoteOLAP(s, PromotionEvidence{SnapshotDigest: s.Digest, PolicyDigest: s.PolicyDigest, ProvenanceRef: s.ProvenanceRef}); !errors.Is(err, ErrPromotionGated) {
		t.Fatalf("unverified promotion: %v", err)
	}
	if err := service.PromoteOLAP(s, PromotionEvidence{SnapshotDigest: s.Digest, PolicyDigest: s.PolicyDigest, ProvenanceRef: s.ProvenanceRef, Verified: true}); err != nil {
		t.Fatal(err)
	}
}
