package documenthubstore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrDraftEgress refuses to send an unpublished draft to a model that runs
// outside the deployment.
var ErrDraftEgress = errors.New("document embedding: drafts are embedded only by in-deployment models")

// VersionRef names one version meaning search may need vectors for.
type VersionRef struct {
	DocumentID, VersionID string
	Deployed              bool
}

// IndexVersionVectors stores section vectors for one version under model,
// unless that version already has vectors for it. Deployed versions may
// use any approved model; an unpublished draft, which only its owner can
// search, is embedded only by a model that is not External. The provider
// call runs outside any transaction. It returns how many sections were
// stored.
func (s *Store) IndexVersionVectors(ctx context.Context, tenantID, docID, versionID string, model EmbeddingModel, embed func(context.Context, []string) ([][]float32, error)) (int, error) {
	if model.ID == "" || embed == nil {
		return 0, ErrEmbeddingModel
	}
	var version Version
	var existing int
	var deployed bool
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var err error
		version, err = loadVersion(ctx, tx, tenantID, docID, versionID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_section_vector WHERE tenant_id=$1 AND version_id=$2 AND model_id=$3`, tenantID, versionID, model.ID).Scan(&existing); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2 AND version_id=$3)`, tenantID, docID, versionID).Scan(&deployed)
	})
	if err != nil || existing > 0 {
		return 0, err
	}
	if !deployed && model.External {
		return 0, ErrDraftEgress
	}
	if _, err := (EmbeddingPolicy{Models: []EmbeddingModel{model}}).resolve(model.ID, version.Classification); err != nil {
		return 0, err
	}
	sections := SplitSections(version.Markdown)
	if len(sections) == 0 {
		sections = []Section{{BlockID: "", Text: version.Markdown}}
	}
	texts := make([]string, len(sections))
	for i, sec := range sections {
		texts[i] = sec.Text
	}
	vecs, err := embed(ctx, texts)
	if err != nil {
		return 0, err
	}
	if len(vecs) != len(sections) {
		return 0, ErrEmbeddingDim
	}
	for _, v := range vecs {
		if len(v) == 0 || len(v) != len(vecs[0]) {
			return 0, ErrEmbeddingDim
		}
	}
	stored := 0
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		for i, sec := range sections {
			raw := make([]byte, 0, 4*len(vecs[i]))
			for _, f := range vecs[i] {
				raw = append(raw, float32ToBytes(f)...)
			}
			n, err := tx.Exec(ctx, `INSERT INTO document_section_vector(id,tenant_id,document_id,version_id,block_id,model_id,model_version,parser_version,dim,vector,content_hash)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (tenant_id,version_id,block_id,model_id) DO NOTHING`,
				"doce-"+uuid.NewString(), tenantID, docID, versionID, sec.BlockID, model.ID, model.Version, SectionParserVersion, len(vecs[i]), raw, HashContent(sec.Text))
			if err != nil {
				return err
			}
			stored += int(n)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return stored, nil
}

// VersionsToIndex lists every version someone can search in a tenant: each
// personal document's latest non-retired version (what its owner reads)
// and its active default deployment (what everyone else reads).
func (s *Store) VersionsToIndex(ctx context.Context, tenantID string) ([]VersionRef, error) {
	var out []VersionRef
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (x.document_id, x.version_id) x.document_id, x.version_id, x.deployed FROM (
				SELECT d.id AS document_id, v.id AS version_id, EXISTS (SELECT 1 FROM document_active_pointer p WHERE p.tenant_id=d.tenant_id AND p.document_id=d.id AND p.version_id=v.id) AS deployed
				FROM document d JOIN LATERAL (SELECT id FROM document_version x WHERE x.tenant_id=d.tenant_id AND x.document_id=d.id AND x.status<>'retired' ORDER BY x.created_at DESC, x.id DESC LIMIT 1) v ON true
				WHERE d.tenant_id=$1 AND d.home='PERSONAL' AND d.lifecycle<>'DISPOSED'
				UNION ALL
				SELECT p.document_id, p.version_id, true FROM document_active_pointer p JOIN document d ON d.tenant_id=p.tenant_id AND d.id=p.document_id
				WHERE p.tenant_id=$1 AND p.scope_kind='default' AND p.scope_id='' AND d.lifecycle<>'DISPOSED'
			) x ORDER BY x.document_id, x.version_id, x.deployed DESC`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ref VersionRef
			if err := rows.Scan(&ref.DocumentID, &ref.VersionID, &ref.Deployed); err != nil {
				return err
			}
			out = append(out, ref)
		}
		return rows.Err()
	})
	return out, err
}
