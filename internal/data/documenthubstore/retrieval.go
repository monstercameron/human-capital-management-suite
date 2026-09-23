// Access-constrained vector retrieval for HUB-028: authorized exact
// candidate search is the baseline every other index is judged against.
// Retrieval returns at most the requested limit with no totals, and
// unknown, denied and missing documents all read identically empty, so
// result counts cannot oracle private corpus. Approximate indexes serve
// only through the conformance gate: zero leaked hits and a bounded
// result, with recall reported for quality judgement.
package documenthubstore

import (
	"context"
	"errors"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrIndexNonconformant rejects an approximate index that leaks
// unauthorized hits or exceeds the result bound.
var ErrIndexNonconformant = errors.New("document retrieval: approximate index is nonconformant")

// RetrievedSection is one authorized section ranked by cosine similarity.
type RetrievedSection struct {
	DocumentID, VersionID, Title, BlockID string
	Cosine                                float64
}

// ConformanceReport judges one approximate result page against the exact
// baseline: Leaked counts approximate hits outside the authorized set,
// Recall is the covered fraction of the exact set, and WithinBound
// records whether the page respects the limit.
type ConformanceReport struct {
	Recall      float64
	Leaked      int
	WithinBound bool
}

// CheckIndexConformance compares an approximate page with the exact
// baseline over the same query, model and limit.
func CheckIndexConformance(exact, approx []RetrievedSection, limit int) ConformanceReport {
	allowed := map[[3]string]bool{}
	for _, h := range exact {
		allowed[[3]string{h.DocumentID, h.VersionID, h.BlockID}] = true
	}
	rep := ConformanceReport{WithinBound: limit <= 0 || len(approx) <= limit}
	covered := map[[3]string]bool{}
	for _, h := range approx {
		key := [3]string{h.DocumentID, h.VersionID, h.BlockID}
		if !allowed[key] {
			rep.Leaked++
			continue
		}
		covered[key] = true
	}
	if len(exact) > 0 {
		rep.Recall = float64(len(covered)) / float64(len(exact))
	} else {
		rep.Recall = 1
	}
	return rep
}

// RequireIndexConformance admits an approximate page only with zero leaks
// and a bounded length. Recall stays advisory: dropping authorized hits
// is a quality defect, not a leak.
func RequireIndexConformance(exact, approx []RetrievedSection, limit int) error {
	if rep := CheckIndexConformance(exact, approx, limit); rep.Leaked != 0 || !rep.WithinBound {
		return ErrIndexNonconformant
	}
	return nil
}

// exactRetrieve is the baseline: every section vector of a deployed,
// non-retired version the reader may read, ranked by cosine. The grant
// prefilter bounds the scan and each hit is rechecked before return.
func exactRetrieve(ctx context.Context, tx dbport.Tx, tenantID string, qvec []float32, modelID, readerKind, readerID string, limit int) ([]RetrievedSection, error) {
	rows, err := tx.Query(ctx, `SELECT sv.document_id, sv.version_id, v.title, sv.block_id, sv.vector
		FROM document_section_vector sv
		JOIN document_version v ON v.tenant_id=sv.tenant_id AND v.id=sv.version_id
		WHERE sv.tenant_id=$1 AND sv.model_id=$2
		  AND v.status <> 'retired'
		  AND EXISTS (SELECT 1 FROM document_active_pointer p
				JOIN document_deployment d ON d.tenant_id=p.tenant_id AND d.id=p.deployment_id
				WHERE p.tenant_id=sv.tenant_id AND p.document_id=sv.document_id AND d.version_id=sv.version_id)
		  AND EXISTS (SELECT 1 FROM document_grant g
				WHERE g.tenant_id=sv.tenant_id AND g.document_id=sv.document_id
				  AND g.subject_kind=$3 AND g.subject_id=$4 AND g.action='read'
				  AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()))
		  AND NOT EXISTS (SELECT 1 FROM document_grant g
				WHERE g.tenant_id=sv.tenant_id AND g.document_id=sv.document_id
				  AND g.subject_kind=$3 AND g.subject_id=$4 AND g.action='read'
				  AND g.effect='deny' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()))`,
		tenantID, modelID, readerKind, readerID)
	if err != nil {
		return nil, err
	}
	var candidates []RetrievedSection
	for rows.Next() {
		var h RetrievedSection
		var raw []byte
		if err := rows.Scan(&h.DocumentID, &h.VersionID, &h.Title, &h.BlockID, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		h.Cosine = cosine(qvec, SectionVector{raw: raw}.Dimensions())
		candidates = append(candidates, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Cosine > candidates[j].Cosine })
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	var out []RetrievedSection
	for _, h := range candidates {
		if err := authorizeTx(ctx, tx, tenantID, h.DocumentID, readerKind, readerID, ActionRead); err != nil {
			continue
		}
		out = append(out, h)
	}
	return out, nil
}

// RetrieveVectors runs the authorized exact candidate search and returns
// at most limit sections with no totals disclosed.
func (s *Store) RetrieveVectors(ctx context.Context, tenantID string, qvec []float32, modelID, readerKind, readerID string, limit int) ([]RetrievedSection, error) {
	var out []RetrievedSection
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		got, err := exactRetrieve(ctx, tx, tenantID, qvec, modelID, readerKind, readerID, limit)
		if err != nil {
			return err
		}
		out = got
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
