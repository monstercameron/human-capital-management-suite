package chatstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func personaReplyContext(t *testing.T, subject string) context.Context {
	t.Helper()
	now := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: subject, SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "persona-reply-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

func personaReplyFixture(t *testing.T) (*Adapter, chat.Principal) {
	t.Helper()
	s := adapterDB(t)
	owner := chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	if _, err := s.CreateConversation(context.Background(), chat.Conversation{ID: "persona-reply", TenantID: owner.TenantID, Kind: chat.PublicChannel, OwnerID: owner.SubjectID, Revision: 1}, []chat.Membership{{TenantID: owner.TenantID, HomeTenantID: owner.TenantID, ConversationID: "persona-reply", SubjectID: owner.SubjectID, Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	return s, owner
}

func personaReplyRequest(revision uint64) chat.PersonaReplyCommitRequest {
	return chat.PersonaReplyCommitRequest{TenantID: "tenant-a", ConversationID: "persona-reply", AuthorID: "persona", AuthorHomeTenantID: "tenant-a", Body: "answer", IdempotencyKey: "output-1", ExpectedAudienceRevision: revision, OutputDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"}
}

func TestPersonaReplyCommit_FailsClosedWithoutServerProof(t *testing.T) {
	s, _ := personaReplyFixture(t)
	revision, err := s.AudienceRevision(context.Background(), "tenant-a", "persona-reply")
	if err != nil || revision == 0 {
		t.Fatalf("audience revision = %d, err=%v", revision, err)
	}
	if _, err := s.CommitPersonaReply(personaReplyContext(t, "persona"), personaReplyRequest(revision)); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("missing opaque proof err=%v", err)
	}
}

func TestPersonaReplyCommit_RevisionAdvancesOnlyForMembership(t *testing.T) {
	s, owner := personaReplyFixture(t)
	before, err := s.AudienceRevision(context.Background(), owner.TenantID, "persona-reply")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutMembership(context.Background(), owner, chat.Membership{TenantID: owner.TenantID, HomeTenantID: owner.TenantID, ConversationID: "persona-reply", SubjectID: "guest", Role: chat.Member, HistoryVisibility: chat.FullHistory}); err != nil {
		t.Fatal(err)
	}
	after, err := s.AudienceRevision(context.Background(), owner.TenantID, "persona-reply")
	if err != nil || after <= before {
		t.Fatalf("membership audience revision before=%d after=%d err=%v", before, after, err)
	}
}

func TestPersonaReplyCommit_MembershipRaceFailsClosedWithoutProof(t *testing.T) {
	s, owner := personaReplyFixture(t)
	revision, err := s.AudienceRevision(context.Background(), owner.TenantID, "persona-reply")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var addErr, commitErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, addErr = s.PutMembership(context.Background(), owner, chat.Membership{TenantID: owner.TenantID, HomeTenantID: owner.TenantID, ConversationID: "persona-reply", SubjectID: "race-guest", Role: chat.Member, HistoryVisibility: chat.FullHistory})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, commitErr = s.CommitPersonaReply(personaReplyContext(t, "persona"), personaReplyRequest(revision))
	}()
	close(start)
	wg.Wait()
	if addErr != nil {
		t.Fatal(addErr)
	}
	if !errors.Is(commitErr, chat.ErrPermissionDenied) {
		t.Fatalf("proofless raced commit err=%v", commitErr)
	}
}
