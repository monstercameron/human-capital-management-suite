package draftstore

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// ALIGN-026: a product draft is bound to the immutable proposal revision it
// produced. The binding pins the exact stored draft revision and its input
// digest on one side, and the proposal revision id, number and kernel-minted
// material digest on the other. Nothing about the draft can change the
// revision: a later draft edit, a superseding proposal revision or a revision
// whose content no longer digests to its recorded material digest each makes
// the binding non-current, so a decision can never be taken against a
// proposal the draft no longer describes.

// BindingStatus is the currency of a draft-to-proposal binding.
type BindingStatus string

// Binding statuses.
const (
	BindingCurrent      BindingStatus = "CURRENT"
	BindingDraftChanged BindingStatus = "DRAFT_CHANGED"
	BindingSuperseded   BindingStatus = "SUPERSEDED"
	BindingTampered     BindingStatus = "TAMPERED"
)

// ErrBinding reports a binding that cannot be formed.
var ErrBinding = errors.New("draftstore: draft cannot be bound to this proposal revision")

// ProposalBinding is the durable link between a submitted draft revision and
// one immutable proposal revision.
type ProposalBinding struct {
	DraftID            string `json:"draft_id"`
	DraftRevision      uint64 `json:"draft_revision"`
	InputDigest        string `json:"input_digest"`
	IntentID           string `json:"intent_id"`
	ProposalRevisionID string `json:"proposal_revision_id"`
	ProposalRevision   uint64 `json:"proposal_revision"`
	MaterialDigest     string `json:"material_digest"`
	Tenant             string `json:"tenant"`
}

// Bind forms the binding. The draft must be the stored, submitted record
// whose intent the revision belongs to, in the same tenant, and the revision
// must still digest to the material digest the kernel minted.
func Bind(rec Record, rev intent.ProposalRevision, d intent.Digester) (ProposalBinding, error) {
	if d == nil {
		return ProposalBinding{}, fmt.Errorf("%w: no digester", ErrBinding)
	}
	switch {
	case rec.Revision == 0 || rec.InputDigest == "":
		return ProposalBinding{}, fmt.Errorf("%w: draft is not a stored record", ErrBinding)
	case !rec.Draft.Submitted():
		return ProposalBinding{}, fmt.Errorf("%w: draft %s has not been submitted", ErrBinding, rec.Draft.DraftID)
	case rec.Draft.SubmittedIntentID != rev.IntentID:
		return ProposalBinding{}, fmt.Errorf("%w: draft became %s, revision belongs to %s", ErrBinding, rec.Draft.SubmittedIntentID, rev.IntentID)
	case rec.Draft.Tenant != rev.Tenant:
		return ProposalBinding{}, fmt.Errorf("%w: tenant mismatch", ErrBinding)
	case rev.ProposalRevisionID == "" || rev.Revision == 0 || rev.MaterialDigest.Digest == "":
		return ProposalBinding{}, fmt.Errorf("%w: revision has no minted identity", ErrBinding)
	}
	if intact, err := revisionIntact(rev, d); err != nil || !intact {
		return ProposalBinding{}, fmt.Errorf("%w: revision content does not match its material digest", ErrBinding)
	}
	return ProposalBinding{
		DraftID: rec.Draft.DraftID, DraftRevision: rec.Revision, InputDigest: rec.InputDigest,
		IntentID: rev.IntentID, ProposalRevisionID: rev.ProposalRevisionID, ProposalRevision: rev.Revision,
		MaterialDigest: rev.MaterialDigest.Digest, Tenant: string(rev.Tenant),
	}, nil
}

// Check re-evaluates a binding against the current stored draft and the
// latest proposal revision for the intent.
func (b ProposalBinding) Check(rec Record, latest intent.ProposalRevision, d intent.Digester) BindingStatus {
	switch {
	case rec.Draft.DraftID != b.DraftID || rec.Revision != b.DraftRevision || rec.InputDigest != b.InputDigest:
		return BindingDraftChanged
	case latest.ProposalRevisionID != b.ProposalRevisionID || latest.Revision != b.ProposalRevision:
		if latest.Revision > b.ProposalRevision {
			return BindingSuperseded
		}
		return BindingTampered
	case latest.MaterialDigest.Digest != b.MaterialDigest:
		return BindingTampered
	}
	if intact, err := revisionIntact(latest, d); err != nil || !intact {
		return BindingTampered
	}
	return BindingCurrent
}

func revisionIntact(rev intent.ProposalRevision, d intent.Digester) (bool, error) {
	if d == nil {
		return false, errors.New("draftstore: no digester")
	}
	recomputed, err := d.ProposalDigest(rev)
	if err != nil {
		return false, err
	}
	return recomputed.Digest == rev.MaterialDigest.Digest, nil
}
