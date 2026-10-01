package promotioncommit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
)

type proofReader struct {
	queryErr error
	rows     dbport.Rows
}

func (r proofReader) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return r.rows, r.queryErr
}

func (proofReader) QueryRow(context.Context, string, ...any) dbport.Row { return nil }

type emptyProofRows struct{}

func (emptyProofRows) Next() bool        { return false }
func (emptyProofRows) Scan(...any) error { return nil }
func (emptyProofRows) Err() error        { return nil }
func (emptyProofRows) Close()            {}

func TestReadAssignmentWriteEvidence_DistinguishesMissingFromQueryFailure(t *testing.T) {
	req := promotioncommit.AssignmentWriteEvidenceRequest{
		TenantID: uuid.New(), IntentID: uuid.New(), ProposalRevisionID: uuid.New(), WorkerID: uuid.New(), AssignmentID: uuid.New(),
		ProposalRevisionNumber: 1, ProposalDigest: "sha256:material", AssignmentRowID: uuid.NewString(), AssignmentDigest: "assignment-digest",
	}
	t.Run("no matching proof", func(t *testing.T) {
		_, err := promotioncommit.ReadAssignmentWriteEvidence(context.Background(), proofReader{rows: emptyProofRows{}}, req)
		if !errors.Is(err, promotioncommit.ErrAssignmentWriteEvidenceMissing) {
			t.Fatalf("error = %v, want ErrAssignmentWriteEvidenceMissing", err)
		}
	})
	t.Run("query failed", func(t *testing.T) {
		injected := errors.New("database unavailable")
		_, err := promotioncommit.ReadAssignmentWriteEvidence(context.Background(), proofReader{queryErr: injected}, req)
		if !errors.Is(err, injected) {
			t.Fatalf("error = %v, want wrapped query error", err)
		}
		if errors.Is(err, promotioncommit.ErrAssignmentWriteEvidenceMissing) {
			t.Fatalf("query failure was mislabeled as missing proof: %v", err)
		}
	})
}
