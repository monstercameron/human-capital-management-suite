// Tenant-scoped section embeddings for HUB-026: versioned vectors bound to
// exact section hashes, created only for deployed versions through an
// approved model and egress policy. The provider call arrives as an
// argument, so no network lives in this package; restricted classifications
// never reach external models, and the refusal happens before any provider
// call. Vectors persist as raw bytes, keeping the store free of index
// extensions.
package documenthubstore

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// SectionParserVersion versions the heading splitter feeding embeddings.
const SectionParserVersion = "hub-sections-v1"

var (
	ErrEmbeddingModel  = errors.New("document embedding: model is not approved")
	ErrEmbeddingEgress = errors.New("document embedding: classification may not leave the tenant")
	ErrEmbeddingDim    = errors.New("document embedding: inconsistent dimensions")
)

// Section is one heading-addressed chunk of a version.
type Section struct {
	BlockID, Heading string
	Level            int
	Text             string
}

// SplitSections chunks markdown at headings. Each section opens with its
// heading line and runs to the next heading; prose before the first
// heading belongs to no addressable section. IDs use the same slug scheme
// as block anchors, so section vectors and block links agree.
func SplitSections(markdown string) []Section {
	var out []Section
	seen := map[string]int{}
	for _, line := range strings.Split(markdown, "\n") {
		if level, text, ok := headingLevel(line); ok && text != "" {
			base := slugifyHeading(text)
			seen[base]++
			id := base
			if seen[base] > 1 {
				id = fmt.Sprintf("%s-%d", base, seen[base])
			}
			out = append(out, Section{BlockID: id, Heading: text, Level: level, Text: line + "\n"})
			continue
		}
		if len(out) == 0 {
			continue
		}
		out[len(out)-1].Text += line + "\n"
	}
	return out
}

// EmbeddingModel is one approved embedding backend. External marks models
// whose provider call leaves the tenant boundary.
type EmbeddingModel struct {
	ID, Version string
	External    bool
}

// EmbeddingPolicy is the caller-owned approval set: which models may run
// and, through External, where their inputs may travel.
type EmbeddingPolicy struct {
	Models []EmbeddingModel
}

func (p EmbeddingPolicy) resolve(modelID, classification string) (EmbeddingModel, error) {
	for _, m := range p.Models {
		if m.ID != modelID {
			continue
		}
		if m.External && classification != "PUBLIC" && classification != "INTERNAL" {
			return EmbeddingModel{}, ErrEmbeddingEgress
		}
		return m, nil
	}
	return EmbeddingModel{}, ErrEmbeddingModel
}

// SectionVector is one stored section embedding with its provenance.
type SectionVector struct {
	ID, TenantID, DocumentID, VersionID, BlockID string
	ModelID, ModelVersion, ParserVersion         string
	ContentHash                                  string
	raw                                          []byte
}

// Dimensions decodes the stored vector bytes.
func (v SectionVector) Dimensions() []float32 {
	out := make([]float32, 0, len(v.raw)/4)
	for i := 0; i+4 <= len(v.raw); i += 4 {
		out = append(out, float32FromBytes(v.raw[i:i+4]))
	}
	return out
}

func float32ToBytes(f float32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], math.Float32bits(f))
	return b[:]
}

func float32FromBytes(b []byte) float32 {
	var f float32
	_ = binary.Read(bytes.NewReader(b), binary.LittleEndian, &f)
	return f
}

// EmbedSections creates versioned section vectors for a deployed version.
// Candidate drafts are refused, unknown models are refused, and restricted
// classifications are refused before the provider is called.
func (s *Store) EmbedSections(ctx context.Context, tenantID, docID, versionID string, policy EmbeddingPolicy, modelID string, embed func(string) ([]float32, error)) ([]SectionVector, error) {
	var out []SectionVector
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		version, err := loadVersion(ctx, tx, tenantID, docID, versionID)
		if err != nil {
			return err
		}
		var live int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_active_pointer p
			JOIN document_deployment d ON d.tenant_id=p.tenant_id AND d.id=p.deployment_id
			WHERE p.tenant_id=$1 AND p.document_id=$2 AND d.version_id=$3`,
			tenantID, docID, versionID).Scan(&live); err != nil {
			return err
		}
		if live == 0 {
			return ErrNoDeployment
		}
		model, err := policy.resolve(modelID, version.Classification)
		if err != nil {
			return err
		}
		vecs, err := embedSectionsTx(ctx, tx, tenantID, docID, version, model, embed)
		if err != nil {
			return err
		}
		out = append(out, vecs...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// embedSectionsTx embeds every section of one loaded version with replace
// semantics, shared by direct embedding and the reconciler.
func embedSectionsTx(ctx context.Context, tx dbport.Tx, tenantID, docID string, version Version, model EmbeddingModel, embed func(string) ([]float32, error)) ([]SectionVector, error) {
	var out []SectionVector
	sections := SplitSections(version.Markdown)
	type embedded struct {
		sec Section
		vec []float32
	}
	var done []embedded
	dim := -1
	for _, sec := range sections {
		vec, err := embed(sec.Text)
		if err != nil {
			return nil, err
		}
		if len(vec) == 0 || (dim != -1 && len(vec) != dim) {
			return nil, ErrEmbeddingDim
		}
		dim = len(vec)
		done = append(done, embedded{sec: sec, vec: vec})
	}
	if _, err := tx.Exec(ctx, `DELETE FROM document_section_vector WHERE tenant_id=$1 AND version_id=$2 AND model_id=$3`,
		tenantID, version.ID, model.ID); err != nil {
		return nil, err
	}
	for _, e := range done {
		var raw []byte
		for _, f := range e.vec {
			raw = append(raw, float32ToBytes(f)...)
		}
		sv := SectionVector{
			ID: "doce-" + uuid.NewString(), TenantID: tenantID, DocumentID: docID, VersionID: version.ID,
			BlockID: e.sec.BlockID, ModelID: model.ID, ModelVersion: model.Version,
			ParserVersion: SectionParserVersion, ContentHash: HashContent(e.sec.Text), raw: raw,
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_section_vector(id,tenant_id,document_id,version_id,block_id,model_id,model_version,parser_version,dim,vector,content_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			sv.ID, tenantID, docID, version.ID, sv.BlockID, sv.ModelID, sv.ModelVersion, sv.ParserVersion, len(e.vec), raw, sv.ContentHash); err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, nil
}

// SectionVectors lists the stored vectors for one version and model.
// Tenant scoping is by predicate and row policy, so foreign tenants read
// zero rows.
func (s *Store) SectionVectors(ctx context.Context, tenantID, docID, versionID, modelID string) ([]SectionVector, error) {
	var out []SectionVector
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,document_id,version_id,block_id,model_id,model_version,parser_version,vector,content_hash FROM document_section_vector WHERE tenant_id=$1 AND document_id=$2 AND version_id=$3 AND model_id=$4 ORDER BY block_id`,
			tenantID, docID, versionID, modelID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sv SectionVector
			if err := rows.Scan(&sv.ID, &sv.TenantID, &sv.DocumentID, &sv.VersionID, &sv.BlockID,
				&sv.ModelID, &sv.ModelVersion, &sv.ParserVersion, &sv.raw, &sv.ContentHash); err != nil {
				return err
			}
			out = append(out, sv)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
