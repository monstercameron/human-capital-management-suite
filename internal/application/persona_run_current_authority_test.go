package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

type currentAuthorityStoreFake struct {
	tenant string
	reader PersonaRunCurrentAuthorityReader
	err    error
}

func (s *currentAuthorityStoreFake) ForTenant(_ context.Context, tenant string) (PersonaRunCurrentAuthorityReader, error) {
	s.tenant = tenant
	return s.reader, s.err
}

type currentAuthorityReaderFake struct {
	facts PersonaRunCurrentAuthorityFacts
	err   error
	calls int
}

func (r *currentAuthorityReaderFake) ReadCurrentPersonaRunAuthority(_ context.Context, _ agentrun.Request) (PersonaRunCurrentAuthorityFacts, error) {
	r.calls++
	return r.facts, r.err
}

func currentAuthorityFixture() (agentrun.Request, PersonaRunCurrentAuthorityFacts) {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	request := agentrun.Request{
		Source:      agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourcePersonaMention, Key: "invocation-1", Ref: "post-1"},
		Persona:     &agentrun.PersonaRef{ID: "persona-1", Version: "2", Digest: digest},
		LegalEntity: "entity-a", Agent: agentrun.VersionRef{AgentID: "agent-1", Version: "3", Digest: digest}, InstallationID: "installation-1",
		Principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "persona-principal-1", InvokerID: "user-1", DelegatedCredentialRef: "grant-1"},
		Purpose:   "persona-mention", Audience: agentrun.AudienceScope{ID: "conversation-1", SnapshotID: "audience-4", Digest: digest},
		Context:  agentrun.ContextScope{ID: "thread-1", SnapshotID: "context-8", Digest: digest},
		Deadline: time.Now().Add(time.Minute), Budget: agentrun.Budget{MaxCostMicros: 50, MaxInputTokens: 1000, MaxOutputTokens: 250}, CauseID: "invocation-1",
	}
	facts := PersonaRunCurrentAuthorityFacts{
		TenantID: "tenant-a", Persona: *request.Persona, Agent: request.Agent, InstallationID: request.InstallationID,
		Principal: request.Principal, Audience: request.Audience, Context: request.Context,
		BudgetCeiling: agentrun.Budget{MaxCostMicros: 100, MaxInputTokens: 2000, MaxOutputTokens: 500},
		GrantRef:      "current-grant:1", PolicyDigest: digest,
	}
	return request, facts
}

func TestTodo_AGENTP_008_DatabasePersonaRunAdmissionAuthority(t *testing.T) {
	request, facts := currentAuthorityFixture()
	reader := &currentAuthorityReaderFake{facts: facts}
	store := &currentAuthorityStoreFake{reader: reader}
	authority := &DatabasePersonaRunAdmissionAuthority{Store: store}
	snapshot, err := authority.VerifyAdmission(context.Background(), request)
	if err != nil {
		t.Fatalf("VerifyAdmission: %v", err)
	}
	if store.tenant != request.Source.TenantID || reader.calls != 1 {
		t.Fatalf("tenant/read calls = %q/%d", store.tenant, reader.calls)
	}
	if snapshot.Agent != facts.Agent || snapshot.InstallationID != facts.InstallationID || snapshot.Principal != facts.Principal ||
		snapshot.Audience != facts.Audience || snapshot.Context != facts.Context || snapshot.BudgetCeiling != facts.BudgetCeiling ||
		snapshot.GrantRef != facts.GrantRef || snapshot.PolicyDigest != facts.PolicyDigest {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestTodo_AGENTP_008_DatabasePersonaRunAdmissionAuthorityFailsClosed(t *testing.T) {
	request, facts := currentAuthorityFixture()
	digest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tests := []struct {
		name   string
		mutate func(*agentrun.Request, *PersonaRunCurrentAuthorityFacts)
		store  error
		read   error
	}{
		{name: "persona revision changed", mutate: func(_ *agentrun.Request, f *PersonaRunCurrentAuthorityFacts) { f.Persona.Digest = digest }},
		{name: "manifest revision changed", mutate: func(_ *agentrun.Request, f *PersonaRunCurrentAuthorityFacts) { f.Agent.Digest = digest }},
		{name: "installation changed", mutate: func(_ *agentrun.Request, f *PersonaRunCurrentAuthorityFacts) { f.InstallationID = "other-installation" }},
		{name: "policy reference missing", mutate: func(_ *agentrun.Request, f *PersonaRunCurrentAuthorityFacts) { f.PolicyDigest = "" }},
		{name: "grant reference missing", mutate: func(_ *agentrun.Request, f *PersonaRunCurrentAuthorityFacts) { f.GrantRef = " " }},
		{name: "budget exceeded", mutate: func(r *agentrun.Request, _ *PersonaRunCurrentAuthorityFacts) { r.Budget.MaxCostMicros = 101 }},
		{name: "source unavailable", store: errors.New("store unavailable")},
		{name: "current records unavailable", read: errors.New("read unavailable")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, f := request, facts
			if tc.mutate != nil {
				tc.mutate(&r, &f)
			}
			reader := &currentAuthorityReaderFake{facts: f, err: tc.read}
			store := &currentAuthorityStoreFake{reader: reader, err: tc.store}
			_, err := (&DatabasePersonaRunAdmissionAuthority{Store: store}).VerifyAdmission(context.Background(), r)
			if err == nil {
				t.Fatal("VerifyAdmission succeeded without matching current authority")
			}
			if tc.store != nil && reader.calls != 0 {
				t.Fatalf("read called after tenant scope failure: %d", reader.calls)
			}
		})
	}
}

func TestTodo_AGENTP_008_DatabasePersonaRunAdmissionAuthorityRejectsIncompleteRequest(t *testing.T) {
	request, facts := currentAuthorityFixture()
	store := &currentAuthorityStoreFake{reader: &currentAuthorityReaderFake{facts: facts}}
	request.Persona = nil
	if _, err := (&DatabasePersonaRunAdmissionAuthority{Store: store}).VerifyAdmission(context.Background(), request); err == nil {
		t.Fatal("missing exact persona reference was admitted")
	}
}
