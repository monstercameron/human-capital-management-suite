// Package analytics owns the rebuildable analytical export/query contract.
// It deliberately has no database or OLAP dependency: a durable adapter can
// persist the immutable Artifact produced here without changing its meaning.
package analytics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrInvalidSnapshot = errors.New("analytics: invalid snapshot")
	ErrUnauthorized    = errors.New("analytics: unauthorized analytics access")
	ErrTenantMismatch  = errors.New("analytics: tenant mismatch")
	ErrPromotionGated  = errors.New("analytics: OLAP promotion requires evidence")
)

// Record is a source row. Deleted and Held rows are retained as tombstone
// metadata in an export, never silently omitted during a rebuild.
type Record struct {
	TenantID        string            `json:"tenant_id"`
	Entity          string            `json:"entity"`
	ID              string            `json:"id"`
	SourceRef       string            `json:"source_ref"`
	SourceWatermark string            `json:"source_watermark"`
	Fields          map[string]string `json:"fields"`
	Deleted         bool              `json:"deleted,omitempty"`
	Held            bool              `json:"held,omitempty"`
}

type SnapshotRequest struct {
	TenantID        string
	SnapshotID      string
	SourceWatermark string
	PolicyDigest    string
	ProvenanceRef   string
	AllowedFields   []string
	Records         []Record
	PartitionSize   int
}

type Snapshot struct {
	TenantID        string      `json:"tenant_id"`
	SnapshotID      string      `json:"snapshot_id"`
	SourceWatermark string      `json:"source_watermark"`
	PolicyDigest    string      `json:"policy_digest"`
	ProvenanceRef   string      `json:"provenance_ref"`
	Partitions      []Partition `json:"partitions"`
	Catalog         Catalog     `json:"catalog"`
	Digest          string      `json:"digest"`
}

// Export is retained as a descriptive alias for callers that treat a
// snapshot as an exported artifact.
type Export = Snapshot

type Partition struct {
	Name   string   `json:"name"`
	Entity string   `json:"entity"`
	Rows   []Record `json:"rows"`
	Digest string   `json:"digest"`
}

type Catalog struct {
	Format  string         `json:"format"`
	Columns []Column       `json:"columns"`
	Tables  []CatalogTable `json:"tables"`
}
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type CatalogTable struct {
	Name           string   `json:"name"`
	PartitionNames []string `json:"partition_names"`
}

// Service is the application-facing analytics boundary. It is intentionally
// stateless: snapshots remain immutable values and callers supply the
// governed source rows and policy evidence for each rebuild.
type Service struct{}

// NewService constructs the analytics boundary used by a composed process.
func NewService() Service { return Service{} }

// BuildSnapshot delegates snapshot construction to the semantic owner.
func (Service) BuildSnapshot(req SnapshotRequest) (Snapshot, error) {
	return BuildSnapshot(req)
}

// BuildExport delegates export construction to the semantic owner.
func (Service) BuildExport(req SnapshotRequest) (Export, error) {
	return BuildExport(req)
}

// Query reads an immutable snapshot without changing authoritative state.
func (Service) Query(s Snapshot, q Query) (Result, error) {
	return s.Query(q)
}

// Metric computes a deterministic metric over an immutable snapshot.
func (Service) Metric(s Snapshot, entity, field string) (Metric, error) {
	return s.Metric(entity, field)
}

// PromoteOLAP delegates evidence-gated promotion to the semantic owner.
func (Service) PromoteOLAP(s Snapshot, e PromotionEvidence) error {
	return PromoteOLAP(s, e)
}

// BuildSnapshot validates governance before producing any artifact bytes.
func BuildSnapshot(req SnapshotRequest) (Snapshot, error) {
	if req.TenantID == "" || req.SourceWatermark == "" || req.PolicyDigest == "" || req.ProvenanceRef == "" {
		return Snapshot{}, ErrInvalidSnapshot
	}
	if req.SnapshotID == "" {
		req.SnapshotID = req.SourceWatermark
	}
	allowed := make(map[string]bool, len(req.AllowedFields))
	for _, f := range req.AllowedFields {
		if f == "" {
			return Snapshot{}, ErrInvalidSnapshot
		}
		allowed[f] = true
	}
	if len(req.Records) == 0 {
		return Snapshot{}, ErrInvalidSnapshot
	}
	byEntity := map[string][]Record{}
	for _, r := range req.Records {
		if r.TenantID != req.TenantID {
			return Snapshot{}, ErrTenantMismatch
		}
		if r.Entity == "" || r.ID == "" || r.SourceRef == "" || r.SourceWatermark == "" {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if r.SourceWatermark > req.SourceWatermark {
			return Snapshot{}, ErrInvalidSnapshot
		}
		for f := range r.Fields {
			if !allowed[f] {
				return Snapshot{}, fmt.Errorf("%w: field %s", ErrUnauthorized, f)
			}
		}
		if r.Fields == nil {
			r.Fields = map[string]string{}
		}
		byEntity[r.Entity] = append(byEntity[r.Entity], r)
	}
	size := req.PartitionSize
	if size <= 0 {
		size = 1000
	}
	entities := make([]string, 0, len(byEntity))
	for e := range byEntity {
		entities = append(entities, e)
	}
	sort.Strings(entities)
	s := Snapshot{TenantID: req.TenantID, SnapshotID: req.SnapshotID, SourceWatermark: req.SourceWatermark, PolicyDigest: req.PolicyDigest, ProvenanceRef: req.ProvenanceRef}
	for _, e := range entities {
		rs := byEntity[e]
		sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID })
		for n := 0; n < len(rs); n += size {
			end := n + size
			if end > len(rs) {
				end = len(rs)
			}
			rows := append([]Record(nil), rs[n:end]...)
			p := Partition{Name: fmt.Sprintf("%s/%04d.parquet", e, n/size), Entity: e, Rows: rows}
			p.Digest = digest(p.Rows)
			s.Partitions = append(s.Partitions, p)
		}
	}
	for _, e := range entities {
		t := CatalogTable{Name: e}
		for _, p := range s.Partitions {
			if p.Entity == e {
				t.PartitionNames = append(t.PartitionNames, p.Name)
			}
		}
		s.Catalog.Tables = append(s.Catalog.Tables, t)
	}
	s.Catalog.Format = "DUCKDB_COMPATIBLE"
	s.Catalog.Columns = []Column{{"tenant_id", "VARCHAR"}, {"entity", "VARCHAR"}, {"id", "VARCHAR"}, {"source_ref", "VARCHAR"}, {"source_watermark", "VARCHAR"}, {"deleted", "BOOLEAN"}, {"held", "BOOLEAN"}, {"fields", "JSON"}}
	s.Digest = digest(struct {
		Meta       Snapshot
		Partitions []Partition
	}{s, s.Partitions})
	return s, nil
}

// NewSnapshot is an explicit constructor spelling for adapters and tests.
func NewSnapshot(req SnapshotRequest) (Snapshot, error) { return BuildSnapshot(req) }

// BuildExport is an alias that emphasizes the immutable export boundary.
func BuildExport(req SnapshotRequest) (Export, error) { return BuildSnapshot(req) }

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ParquetBytes is a deterministic interchange representation. Adapters may
// encode these rows as physical Parquet; the contract remains content-addressed.
func (s Snapshot) ParquetBytes(partition string) ([]byte, error) {
	for _, p := range s.Partitions {
		if p.Name == partition {
			return json.Marshal(p.Rows)
		}
	}
	return nil, ErrInvalidSnapshot
}

// CatalogSQL returns stable DuckDB-compatible view definitions over the
// partition files. It is intentionally declarative; execution is an OLAP
// adapter concern and cannot mutate the authoritative store.
func (s Snapshot) CatalogSQL() string {
	var b strings.Builder
	b.WriteString("CREATE TABLE analytics_export (tenant_id VARCHAR, entity VARCHAR, id VARCHAR, source_ref VARCHAR, source_watermark VARCHAR, deleted BOOLEAN, held BOOLEAN, fields JSON);\n")
	for _, p := range s.Partitions {
		fmt.Fprintf(&b, "-- partition %s digest %s\n", p.Name, p.Digest)
	}
	return b.String()
}

type Query struct {
	TenantID       string
	Entity         string
	Fields         []string
	IDEquals       string
	IncludeDeleted bool
	IncludeHeld    bool
}
type Result struct {
	SnapshotDigest string
	Rows           []Record
}

func (s Snapshot) Query(q Query) (Result, error) {
	if q.TenantID != s.TenantID {
		return Result{}, ErrTenantMismatch
	}
	allowed := map[string]bool{}
	for _, c := range s.Catalog.Columns {
		allowed[c.Name] = true
	}
	for _, f := range q.Fields {
		if !allowed[f] && f != "" {
			return Result{}, ErrUnauthorized
		}
	}
	var out []Record
	for _, p := range s.Partitions {
		if q.Entity != "" && p.Entity != q.Entity {
			continue
		}
		for _, r := range p.Rows {
			if q.IDEquals != "" && r.ID != q.IDEquals {
				continue
			}
			if r.Deleted && !q.IncludeDeleted {
				continue
			}
			if r.Held && !q.IncludeHeld {
				continue
			}
			if len(q.Fields) > 0 {
				nf := map[string]string{}
				for _, f := range q.Fields {
					if v, ok := r.Fields[f]; ok {
						nf[f] = v
					}
				}
				r.Fields = nf
			}
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return Result{s.Digest, out}, nil
}

type Metric struct {
	Name   string
	Entity string
	Field  string
	Count  int
	Sum    float64
}

func (s Snapshot) Metric(entity, field string) (Metric, error) {
	if strings.TrimSpace(entity) == "" {
		return Metric{}, ErrInvalidSnapshot
	}
	q, err := s.Query(Query{TenantID: s.TenantID, Entity: entity})
	if err != nil {
		return Metric{}, err
	}
	m := Metric{Name: "count", Entity: entity, Field: field, Count: len(q.Rows)}
	if field != "" {
		m.Name = "sum"
		for _, r := range q.Rows {
			var v float64
			if _, e := fmt.Sscan(r.Fields[field], &v); e != nil {
				return Metric{}, ErrInvalidSnapshot
			}
			m.Sum += v
		}
	}
	return m, nil
}

type PromotionEvidence struct {
	SnapshotDigest string
	PolicyDigest   string
	ProvenanceRef  string
	Verified       bool
}

func PromoteOLAP(s Snapshot, e PromotionEvidence) error {
	if !e.Verified || e.SnapshotDigest != s.Digest || e.PolicyDigest != s.PolicyDigest || e.ProvenanceRef != s.ProvenanceRef {
		return ErrPromotionGated
	}
	return nil
}
