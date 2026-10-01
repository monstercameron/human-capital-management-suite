package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/proofing"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func servedProofingSubject() values.EntityRef {
	return values.EntityRef{Tenant: "tenant-1", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
}

func TestTodo_PROOF_001_Served(t *testing.T) {
	surface := NewServedProofingSurface()
	if surface.Version == nil || surface.NewEvidenceItem == nil || surface.NewProofingSession == nil || surface.NewWorkAuthorizationEvidence == nil || surface.ConformWorkAuthorization == nil {
		t.Fatal("served proofing surface omitted identity-proofing or authorization contracts")
	}
	at := values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	evidence, err := surface.NewEvidenceItem(proofing.EvidencePassport, "sha256:passport-digest", "sha256:provider-digest", "custody://passport-1", proofing.AssuranceIAL2, at)
	if err != nil {
		t.Fatalf("served evidence item: %v", err)
	}
	session, err := surface.NewProofingSession("session-served", servedProofingSubject(), "onboarding", proofing.AssuranceIAL2, []proofing.EvidenceItem{evidence}, "verifier-1", values.NewInstant(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("served proofing session: %v", err)
	}
	explanation, err := surface.ExplainSession(session)
	if err != nil || explanation.EvidenceCount != 1 || explanation.Target != proofing.AssuranceIAL2 {
		t.Fatalf("served proofing explanation = %+v, err=%v", explanation, err)
	}
	if surface.Version() != 1 {
		t.Fatalf("served proofing version = %d, want 1", surface.Version())
	}
	if _, err := surface.ConformWorkAuthorization(proofing.WorkAuthorizationConformanceRequest{}); err == nil {
		t.Fatal("served conformance accepted an empty evidence request")
	}
}

func TestTodo_PROOF_003_Served(t *testing.T) {
	surface := NewServedProofingSurface()
	if surface.NewRepository == nil || surface.NewMemoryRepository == nil {
		t.Fatal("served proofing surface omitted typed repository constructors")
	}
	repository := surface.NewRepository(nil)
	if repository == nil {
		t.Fatal("served proofing surface returned a nil repository")
	}
	err := repository.SaveSession(context.Background(), "", proofing.ProofingSession{}, 0)
	var storeErr *proofing.StoreError
	if !errors.As(err, &storeErr) || storeErr.Code != proofing.StoreInvalidCode {
		t.Fatalf("nil database repository error = %v, typed=%+v; want INVALID", err, storeErr)
	}
	memory := surface.NewMemoryRepository()
	if memory == nil {
		t.Fatal("served proofing surface returned a nil memory repository")
	}
	if err := memory.SaveSession(context.Background(), "tenant-a", proofing.ProofingSession{}, 0); !errors.Is(err, proofing.ErrStoreInvalid) {
		t.Fatalf("memory repository invalid record error = %v, want ErrStoreInvalid", err)
	}
}

func TestServedProofingAppSurface(t *testing.T) {
	if (&App{}).Proofing().NewRepository == nil || (&App{}).Proofing().ConformWorkAuthorization == nil {
		t.Fatal("composed app omitted proofing capabilities")
	}
	var nilApp *App
	if nilApp.Proofing().NewRepository != nil {
		t.Fatal("nil app returned a live proofing repository constructor")
	}
}
