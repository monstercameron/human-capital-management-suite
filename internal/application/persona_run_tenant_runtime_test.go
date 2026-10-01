package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

type personaRunAdmissionReaderFake struct {
	record agentrun.Record
	err    error
}

func (r personaRunAdmissionReaderFake) GetByID(context.Context, string) (agentrun.Record, error) {
	return r.record, r.err
}

type personaRunCurrentAuthorityFake struct {
	snapshot agentrun.AuthoritySnapshot
	err      error
	calls    int
}

func (a *personaRunCurrentAuthorityFake) VerifyAdmission(_ context.Context, _ agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	a.calls++
	return a.snapshot, a.err
}

func TestTodo_AGENTP_008_AdmissionRecheckRequiresCurrentPinnedAuthority(t *testing.T) {
	request := agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "tenant-a"}}
	pinned := agentrun.AuthoritySnapshot{GrantRef: "grant-a", PolicyDigest: "sha256:policy"}
	authority := &personaRunCurrentAuthorityFake{snapshot: pinned}
	rechecker, err := NewPersonaRunAdmissionRechecker("tenant-a", personaRunAdmissionReaderFake{record: agentrun.Record{
		ID: "admission-a", Decision: agentrun.DecisionAccepted, Request: request, Authority: pinned,
	}}, authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := rechecker.Recheck(context.Background(), "tenant-a", "admission-a"); err != nil || authority.calls != 1 {
		t.Fatalf("current matching authority recheck = %v, calls %d", err, authority.calls)
	}
	if err := rechecker.Recheck(context.Background(), "tenant-b", "admission-a"); !errors.Is(err, errPersonaRunTenantRuntime) {
		t.Fatalf("cross-tenant recheck = %v, want fail closed", err)
	}
	authority.snapshot.PolicyDigest = "sha256:revoked"
	if err := rechecker.Recheck(context.Background(), "tenant-a", "admission-a"); !errors.Is(err, errPersonaRunTenantRuntime) {
		t.Fatalf("changed current authority = %v, want fail closed", err)
	}
}
