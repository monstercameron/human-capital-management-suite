package governance

import (
	"context"
	"errors"
	"testing"

	privacy "github.com/monstercameron/human-capital-management-suite/internal/governance/privacy"
	privacydispatch "github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/dispatch"
	privacyinventory "github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/inventory"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type privRuntimePolicy struct{}

func (privRuntimePolicy) Evaluate(dlp.DecisionRequest) (dlp.Evaluation, error) {
	return dlp.Evaluation{Decision: dlp.Refuse}, nil
}

type privRuntimeSender struct{ calls int }

func (*privRuntimeSender) Send(context.Context, []byte) error { return nil }

func TestTodo_PRIV_001_Integration(t *testing.T) {
	result := Compose(baseReq())
	decision := result.Privacy.EvaluateAuthority(privacy.AuthorityInput{})
	if decision.Allowed || decision.Code != privacy.AuthorityNoticeInvalid {
		t.Fatalf("composed privacy authority decision = %+v, want deny-by-default notice invalid", decision)
	}

	if _, err := result.Privacy.ValidateInventory(privacyinventory.Inventory{}); !errors.Is(err, privacyinventory.ErrInvalid) {
		t.Fatalf("composed inventory validation error = %v, want privacy inventory ErrInvalid", err)
	}
}

func TestTodo_PRIV_003_Security(t *testing.T) {
	result := Compose(baseReq())
	sender := &privRuntimeSender{}
	_, err := result.Privacy.Dispatch(context.Background(), privacydispatch.Binding{}, privacydispatch.SnapshotSet{}, privacydispatch.Request{}, privRuntimePolicy{}, sender)
	if !errors.Is(err, privacydispatch.ErrEGRESSBlocked) {
		t.Fatalf("composed dispatch error = %v, want EGRESS_BLOCKED", err)
	}
	if sender.calls != 0 {
		t.Fatalf("composed dispatch called sender %d times, want zero", sender.calls)
	}
}
