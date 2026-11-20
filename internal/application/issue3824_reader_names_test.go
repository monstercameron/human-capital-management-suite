package application

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3824Directory struct{ chatgateAppDirectory }

func (issue3824Directory) GateReaderNames(_ context.Context, _ chatgate.Actor, _ chatgate.Scope, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if id == "admin" {
			out[id] = "Dana Whitfield"
		}
	}
	return out, nil
}

// TestIssue3824_ApplicantSeesReadersByFullNameNotTheirOwn: after an administrator
// reads the applicant's answers, the applicant's own list names that person in
// full and holds no read of their own.
func TestIssue3824_ApplicantSeesReadersByFullNameNotTheirOwn(t *testing.T) {
	now := time.Now()
	p, e := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant"), Subject: "member", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:fixture"})
	if e != nil {
		t.Fatal(e)
	}
	s := &chatgate.Service{Repository: chatgate.NewMemoryRepository(), Authority: chatgateAppAuthority{}, Registry: chatgate.NewRegistry()}
	scope := chatgate.Scope{Tenant: "tenant", Conversation: "room"}
	c := chatgate.Command{Scope: scope, Actor: chatgate.Actor{Tenant: "tenant", Person: "admin"}, Key: "define"}
	d := chatgate.Definition{Mode: "review", Purpose: "Join", Fields: []chatgate.Field{{ID: "team", Kind: "short_text", KindVersion: "1.0.0", Label: "Team", Purpose: "Welcome", DataClass: "INTERNAL", RetentionDays: 30, Required: true, Visibility: chatgate.Visibility{Administrators: true}}}}
	if e = s.Define(t.Context(), c, d); e != nil {
		t.Fatal(e)
	}
	c.Key, c.ExpectedRevision = "publish", 1
	if _, e = s.Publish(t.Context(), c, chatgate.Version{Major: 1}); e != nil {
		t.Fatal(e)
	}
	app := &ChatgateApplication{Service: s, Directory: issue3824Directory{}}
	ctx := trust.WithPrincipal(t.Context(), p)
	if _, e = app.GateRequest(ctx, ChatgateRequest{Conversation: "room", Action: "submit", Version: "1.0.0", Key: "submit", ExpectedRevision: 2, Answers: map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadAnswers(t.Context(), chatgate.ReadRequest{Actor: chatgate.Actor{Tenant: "tenant", Person: "admin"}, Scope: scope, Person: "member", Purpose: "Gate answers view"}); e != nil {
		t.Fatal(e)
	}
	reply, e := app.GateRequest(ctx, ChatgateRequest{Conversation: "room", Action: "get", Locale: "en-US"})
	if e != nil || reply.View == nil {
		t.Fatalf("get: %+v %v", reply, e)
	}
	if len(reply.View.Reads) != 1 || reply.View.Reads[0].Reader != "admin" {
		t.Fatalf("reads: %+v", reply.View.Reads)
	}
	if got := reply.View.Names["admin"]; got != "Dana Whitfield" {
		t.Fatalf("the reader is named %q", got)
	}
}
