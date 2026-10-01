package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdeliver"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaFinalOutputRecoveryStoreFake struct {
	projection agentsecurity.FinalOutputPersistence
	expected   agentsecurity.FinalOutputIdentity
	calls      int
	reads      int
	err        error
}

func (f *personaFinalOutputRecoveryStoreFake) GetFinalOutput(_ context.Context, outputID string) (agentpersonastore.FinalOutputRecord, error) {
	f.reads++
	if outputID != f.expected.OutputID {
		return agentpersonastore.FinalOutputRecord{}, errors.New("incorrect output key")
	}
	i := f.expected
	return agentpersonastore.FinalOutputRecord{TenantID: values.TenantId(i.TenantID), OutputID: i.OutputID, InvocationID: i.InvocationID,
		AdmissionID: i.AdmissionID, RunID: i.RunID, InvokerID: i.InvokerID, ConversationID: i.ConversationID, ThreadID: i.ThreadID,
		ParentPostID: i.PostID, PersonaID: i.PersonaID, PersonaVersion: i.PersonaVersion, InstallationID: i.InstallationID}, nil
}

func (f *personaFinalOutputRecoveryStoreFake) RecoverFinalOutputForIdentity(_ context.Context, identity agentsecurity.FinalOutputIdentity, verifier *agentsecurity.FinalOutputRecoveryVerifier, _ agentsecurity.FinalOutputRecoveryRehydrator) (agentsecurity.FinalOutputPersistence, error) {
	f.calls++
	if identity != f.expected || verifier == nil {
		return agentsecurity.FinalOutputPersistence{}, errors.New("incorrect identity-bound recovery")
	}
	return f.projection, f.err
}

type personaFinalOutputRecoveryFactoryFake struct {
	store personaFinalOutputRecoveryStore
}

func (f personaFinalOutputRecoveryFactoryFake) ForTenant(_ context.Context, tenant values.TenantId) (personaFinalOutputRecoveryStore, error) {
	if string(tenant) != "tenant-a" {
		return nil, errors.New("wrong tenant scope")
	}
	return f.store, nil
}

type personaFinalOutputRehydratorFake struct{}

func (personaFinalOutputRehydratorFake) RehydrateFinalOutput(context.Context, agentsecurity.FinalOutputRecoveryRecord) (agentsecurity.FinalOutputPersistence, error) {
	return agentsecurity.FinalOutputPersistence{}, errors.New("store fake owns recovery")
}

func TestPersonaFinalOutputSourceRecoversExactIdentityAndReturnsPlainText(t *testing.T) {
	projection := personaFinalOutputProjection(t)
	identity := projection.Identity()
	request := PersonaOutputRequest{TenantID: "tenant-a", ConversationID: "room-a", ParentPostID: "thread-a", OutputID: identity.OutputID,
		Invoker: agentdeliver.AudienceMember{TenantID: "tenant-a", SubjectID: "alice"}}
	store := &personaFinalOutputRecoveryStoreFake{projection: projection, expected: identity}
	source := personaFinalOutputSourceFixture(t, store)
	got, err := source.LoadPersonaOutput(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got.PersonaLabel != "Persona" || len(got.Items) != 1 || got.Items[0].ID != "answer" || got.Items[0].Text != "I can help explain the policy." {
		t.Fatalf("delivered projection = %#v", got)
	}
	if len(got.Items[0].Citations) != 0 || len(got.Items[0].Materials) != 0 || store.calls != 1 {
		t.Fatalf("source exposed unverified citation/material data or skipped exact recovery: %#v calls=%d", got, store.calls)
	}
}

func TestPersonaFinalOutputSourceRejectsRequestIdentityMismatchBeforeRecovery(t *testing.T) {
	projection := personaFinalOutputProjection(t)
	identity := projection.Identity()
	store := &personaFinalOutputRecoveryStoreFake{projection: projection, expected: identity}
	source := personaFinalOutputSourceFixture(t, store)
	request := PersonaOutputRequest{TenantID: "tenant-a", ConversationID: "room-a", ParentPostID: "different-thread", OutputID: identity.OutputID,
		Invoker: agentdeliver.AudienceMember{TenantID: "tenant-a", SubjectID: "alice"}}
	if _, err := source.LoadPersonaOutput(context.Background(), request); !errors.Is(err, errPersonaFinalOutputSourceUnavailable) {
		t.Fatalf("mismatched thread error = %v", err)
	}
	if store.calls != 0 || store.reads != 1 {
		t.Fatalf("identity mismatch reached recovery store: reads=%d recoveries=%d", store.reads, store.calls)
	}
}

func TestPersonaFinalOutputSourceRejectsUnsealedProjection(t *testing.T) {
	store := &personaFinalOutputRecoveryStoreFake{expected: agentsecurity.FinalOutputIdentity{}}
	store.expected = agentsecurity.FinalOutputIdentity{TenantID: "tenant-a", OutputID: "out", InvocationID: "inv", AdmissionID: "admission", RunID: "run", InvokerID: "alice",
		ConversationID: "room-a", ThreadID: "thread-a", PostID: "post-a", PersonaID: "persona-a", PersonaVersion: "1", InstallationID: "install-a"}
	source := personaFinalOutputSourceFixture(t, store)
	request := PersonaOutputRequest{TenantID: "tenant-a", ConversationID: "room-a", ParentPostID: "thread-a", OutputID: "out",
		Invoker: agentdeliver.AudienceMember{TenantID: "tenant-a", SubjectID: "alice"}}
	if _, err := source.LoadPersonaOutput(context.Background(), request); !errors.Is(err, errPersonaFinalOutputSourceUnavailable) {
		t.Fatalf("unsealed projection error = %v", err)
	}
}

func personaFinalOutputSourceFixture(t *testing.T, store personaFinalOutputRecoveryStore) *PersonaFinalOutputSource {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := agentsecurity.NewFinalOutputRecoveryVerifier(map[string]ed25519.PublicKey{"test": publicKey})
	if err != nil {
		t.Fatal(err)
	}
	source, err := newPersonaFinalOutputSource(personaFinalOutputRecoveryFactoryFake{store: store}, verifier, personaFinalOutputRehydratorFake{})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func personaFinalOutputProjection(t *testing.T) agentsecurity.FinalOutputPersistence {
	t.Helper()
	validator, admission, run, _, _ := personaRunOutputFixture(t)
	projection, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: "I can help explain the policy.", Finish: agentmodel.FinishComplete})
	if err != nil {
		t.Fatal(err)
	}
	return projection
}
