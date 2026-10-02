package application

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_CHATGATE_005_Security_ApplicantReply: a person who does not
// administer the gate is sent the questions of the version in force and of the
// version they answered, and never the rules that judge the answers, nor the
// versions before. An administrator is still sent all of it.
func TestTodo_CHATGATE_005_Security_ApplicantReply(t *testing.T) {
	now := time.Now()
	principal := func(subject string) *trust.Principal {
		p, e := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant"), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:fixture"})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	s := &chatgate.Service{Repository: chatgate.NewMemoryRepository(), Authority: chatgateAppAuthority{}, Registry: chatgate.NewRegistry()}
	scope := chatgate.Scope{Tenant: "tenant", Conversation: "room"}
	c := chatgate.Command{Scope: scope, Actor: chatgate.Actor{Tenant: "tenant", Person: "admin"}, Key: "define-1"}
	field := chatgate.Field{ID: "team", Kind: "single_choice", KindVersion: "1.0.0", Label: "Team", Purpose: "Welcome", DataClass: "INTERNAL", RetentionDays: 30, Required: true, Options: []string{"Payroll", "Finance", "Sales"}, Visibility: chatgate.Visibility{Administrators: true}}
	rules := func(reason string) []chatgate.Rule {
		return []chatgate.Rule{{When: chatgate.Expression{Operator: "in", Field: "team", Values: []string{"Payroll", "Finance"}}, Outcome: "admitted", Reason: reason}}
	}
	// Two published versions: the first is history, the second is in force.
	for i, reason := range []string{"SECRET-RULE-ONE", "SECRET-RULE-TWO"} {
		d := chatgate.Definition{Mode: "rule", Purpose: "Join the payroll discussion", Fields: []chatgate.Field{field}, Rules: rules(reason)}
		if e := s.Define(t.Context(), c, d); e != nil {
			t.Fatal(e)
		}
		c.ExpectedRevision++
		c.Key = "publish-" + reason
		if _, e := s.Publish(t.Context(), c, chatgate.Version{Major: uint32(i + 1)}); e != nil {
			t.Fatal(e)
		}
		c.ExpectedRevision++
		c.Key = "define-next"
	}
	app := &ChatgateApplication{Service: s, Directory: chatgateAppDirectory{}}

	reply, e := app.GateRequest(trust.WithPrincipal(t.Context(), principal("member")), ChatgateRequest{Conversation: "room"})
	if e != nil || reply.View == nil || reply.View.Administrator {
		t.Fatalf("applicant %+v %v", reply, e)
	}
	body, e := json.Marshal(reply)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"SECRET-RULE-ONE", "SECRET-RULE-TWO"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("the reply to a person who is not an administrator carries the admission rule %q", secret)
		}
	}
	versions := reply.View.Gate.Versions
	if len(versions) != 1 || versions[0].Version.String() != reply.View.Gate.Current || len(versions[0].Rules) != 0 {
		t.Fatalf("an applicant is sent %d versions, want the one in force without its rules: %+v", len(versions), versions)
	}
	// The questions are all there: the form can be drawn and answered.
	if len(versions[0].Fields) != 1 || versions[0].Fields[0].ID != "team" || len(versions[0].Fields[0].Options) != 3 || len(reply.View.Controls) == 0 {
		t.Fatalf("the applicant's form lost its questions: %+v", versions[0].Fields)
	}

	// The administrator still reads every version with its rules.
	admin, e := app.GateRequest(trust.WithPrincipal(t.Context(), principal("admin")), ChatgateRequest{Conversation: "room"})
	if e != nil || !admin.View.Administrator || len(admin.View.Gate.Versions) != 2 || len(admin.View.Gate.Versions[1].Rules) != 1 {
		t.Fatalf("the administrator's view lost versions or rules: %+v %v", admin.View, e)
	}

	// The filter itself: the version a person answered stays beside the one in force.
	g := chatgate.Gate{Current: "2.0.0", Versions: []chatgate.Definition{{Version: chatgate.Version{Major: 1}, Rules: rules("a")}, {Version: chatgate.Version{Major: 2}, Rules: rules("b")}, {Version: chatgate.Version{Major: 3}}}}
	kept := chatgateApplicantGate(g, "1.0.0")
	if len(kept.Versions) != 2 || kept.Versions[0].Version.String() != "1.0.0" || kept.Versions[1].Version.String() != "2.0.0" || len(kept.Versions[0].Rules)+len(kept.Versions[1].Rules) != 0 {
		t.Fatalf("applicant gate = %+v", kept.Versions)
	}
	if len(g.Versions) != 3 || len(g.Versions[0].Rules) != 1 {
		t.Fatal("filtering the reply changed the stored gate")
	}
}
