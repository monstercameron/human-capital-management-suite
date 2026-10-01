package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type missingPunchProposalDigester struct{}

func (missingPunchProposalDigester) RequestDigest(intent.Instance) (digest.Reference, error) {
	return digest.Reference{Digest: "sha256:request"}, nil
}

func (missingPunchProposalDigester) ProposalDigest(p intent.ProposalRevision) (digest.Reference, error) {
	if p.MaterialDigest.Digest == "" {
		return digest.Reference{}, errors.New("digest absent")
	}
	return p.MaterialDigest, nil
}

func TestTodo_TCLOCK011_MissingPunchProposalRejectsUnboundInputs(t *testing.T) {
	base := MissingPunchProposalCapture{Request: clockservice.MissingPunchWorkflowRequest{Action: "REQUEST", TenantID: "tenant", SessionID: "session", OriginalObservationID: "observation", OriginalWorkflowInstanceRef: uuid.New().String(), ClaimedOutAt: time.Unix(100, 0).UTC(), At: time.Unix(101, 0).UTC(), Reason: "forgot", ExpectedRevision: 2}, Tenant: "tenant", Digester: missingPunchProposalDigester{}, Clock: func() values.Instant { return values.NewInstant(time.Unix(101, 0).UTC()) }}
	if _, err := CaptureMissingPunchProposal(base); !errors.Is(err, errMissingPunchProposalInput) {
		t.Fatalf("missing organization/legal context error = %v, want input refusal", err)
	}
	base.Request.OriginalWorkflowInstanceRef = "not-a-uuid"
	base.OrganizationScopeID, base.LegalEntityID = "org:time", "le:time"
	if _, err := CaptureMissingPunchProposal(base); !errors.Is(err, errMissingPunchProposalInput) {
		t.Fatalf("invalid original workflow error = %v, want input refusal", err)
	}
}

func TestTodo_TCLOCK011_MissingPunchProposalVerifierUsesCanonicalDigest(t *testing.T) {
	v := proposalDigestVerifier{missingPunchProposalDigester{}}
	if err := v.VerifyProposalDigest(intent.ProposalRevision{}); err == nil {
		t.Fatal("empty proposal digest was accepted")
	}
	if err := (MissingPunchProposalStore{}).PersistMissingPunchProposal(context.Background(), intent.ProposalRevision{}); !errors.Is(err, errMissingPunchProposalConfig) {
		t.Fatalf("incomplete store error = %v, want composition refusal", err)
	}
}

func TestTodo_TCLOCK011_MissingPunchProposalStartRequiresDurableFacts(t *testing.T) {
	p := intent.ProposalRevision{ProposalRevisionID: "revision-1", IntentID: "intent-1", Tenant: "tenant", MaterialDigest: digest.Reference{Digest: "sha256:proposal"}}
	base := runtime.StartRequest{TenantID: uuid.New()}
	if _, err := BindMissingPunchProposalStart(base, p, nil, nil, func(values.TenantId) (uuid.UUID, error) { return uuid.Nil, nil }, "hcmnext.time.fix_missing_punch/v1"); !errors.Is(err, errMissingPunchProposalBind) {
		t.Fatalf("missing facts error = %v, want binding refusal", err)
	}
}
