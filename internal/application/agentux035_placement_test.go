package application

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// agentUX035Allowed is a conversation rule that allows the listed classes in
// the listed conversations, and records what it was asked.
type agentUX035Allowed struct {
	allowed map[string][]string
	err     error
	asked   []string
}

func (a *agentUX035Allowed) ConversationAllowsDataClass(_ context.Context, _, conversation, class string) (bool, error) {
	a.asked = append(a.asked, conversation+":"+class)
	if a.err != nil {
		return false, a.err
	}
	for _, held := range a.allowed[conversation] {
		if held == class {
			return true, nil
		}
	}
	return false, nil
}

// TestTodo_AGENTUX_035_Placement: a cited document may be disclosed to a
// conversation on the strength of its placement only when the cited version is
// the one officially placed in that conversation now, the cited bytes are that
// version's, and the conversation allows the document's classification, as
// placed and as it is classified now. Anything else leaves the decision to the
// member-by-member check, which is what keeps the answer private on any doubt.
func TestTodo_AGENTUX_035_Placement(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant = "agentux-035-placement"
	doc, err := svc.store.CreateDocument(ctx, tenant, "owner", "COMPANY")
	if err != nil {
		t.Fatal(err)
	}
	version, err := svc.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: doc, CreatorID: "owner", Title: "2026 holiday guide", Markdown: "Thanksgiving is November 26, 2026.", Classification: "INTERNAL"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: doc, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "reviewer", Authority: "policy-owner", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.store.PlaceDocument(ctx, tenant, documenthubstore.PlaceInput{DocumentID: doc, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ActorID: "owner", CustodianID: "owner", ReviewDueAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	digest := personaRunT0ToolOutputDigest([]byte(version.Markdown))
	rule := &agentUX035Allowed{allowed: map[string][]string{"general": {"PUBLIC", "INTERNAL"}, "leadership": {"PUBLIC", "INTERNAL"}, "lobby": {"PUBLIC"}}}
	authority := AgentAnnouncementHubAuthority{Store: svc.store, Classes: rule}
	placed := func(conversation, pin, cited string) bool {
		t.Helper()
		ok, err := authority.OfficialConversationPlacement(ctx, tenant, conversation, doc, pin, cited, "")
		if err != nil && ok {
			t.Fatalf("a refusal with an error was reported as placed: %v", err)
		}
		return ok
	}

	for _, pin := range []string{version.ID, "1"} {
		if !placed("general", pin, digest) {
			t.Fatalf("the officially placed version %s is not disclosable in the conversation it is placed in", pin)
		}
	}
	// The rule was asked about this conversation and this class, and nothing else.
	for _, asked := range rule.asked {
		if asked != "general:INTERNAL" {
			t.Fatalf("the conversation rule was asked %q", asked)
		}
	}
	// With no conversation rule composed, a placement is never enough.
	if ok, err := (AgentAnnouncementHubAuthority{Store: svc.store}).OfficialConversationPlacement(ctx, tenant, "general", doc, version.ID, digest, ""); err != nil || ok {
		t.Fatalf("a placement was enough with no conversation rule: %t %v", ok, err)
	}
	// Placed elsewhere, or nowhere: not this conversation's placement.
	if placed("leadership", version.ID, digest) {
		t.Fatal("a placement in #general was taken for one in another conversation")
	}
	// The conversation does not allow the class.
	if placed("lobby", version.ID, digest) {
		t.Fatal("a conversation that allows only public data disclosed an internal document")
	}
	rule.allowed["general"] = []string{"PUBLIC"}
	if placed("general", version.ID, digest) {
		t.Fatal("narrowing the conversation's classes did not take the placement's effect away")
	}
	rule.allowed["general"] = []string{"PUBLIC", "INTERNAL"}
	// Other bytes than the placed version's.
	if placed("general", version.ID, "sha256:"+strings.Repeat("a", 64)) {
		t.Fatal("a changed source digest was accepted")
	}
	// A rule that cannot answer is a no, and its cause is not swallowed.
	rule.err = errors.New("policy store offline")
	if ok, err := authority.OfficialConversationPlacement(ctx, tenant, "general", doc, version.ID, digest, ""); ok || err == nil {
		t.Fatalf("an unanswerable rule: %t %v", ok, err)
	}
	rule.err = nil
	// Another workspace has no such placement.
	if ok, _ := authority.OfficialConversationPlacement(ctx, "another-tenant", "general", doc, version.ID, digest, ""); ok {
		t.Fatal("another workspace's conversation took this placement")
	}

	// The document is re-classified: a newer version is confidential. The older
	// version is still the one placed, and it is no longer disclosed on its
	// placement, because the document as classified now is not allowed here.
	newer, err := svc.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: doc, CreatorID: "owner", Title: "2026 holiday guide", Markdown: "Thanksgiving is November 26, 2026. Bonus dates are confidential.", Classification: "CONFIDENTIAL"}, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if placed("general", version.ID, digest) {
		t.Fatal("a document re-classified as confidential is still disclosed on its older placement")
	}
	if placed("general", newer.ID, personaRunT0ToolOutputDigest([]byte(newer.Markdown))) {
		t.Fatal("a version that is not the placed one was disclosed")
	}
	rule.allowed["general"] = []string{"PUBLIC", "INTERNAL", "CONFIDENTIAL"}
	if !placed("general", version.ID, digest) {
		t.Fatal("a conversation that allows both classes refuses the placed version")
	}

	// A withdrawn placement discloses nothing.
	if _, err = svc.store.Withdraw(ctx, tenant, documenthubstore.WithdrawInput{DocumentID: doc, ScopeKind: "placement", ScopeID: "general", ActorID: "owner", ExpectedLive: version.ID, Reason: "Policy withdrawn"}); err != nil {
		t.Fatal(err)
	}
	if placed("general", version.ID, digest) {
		t.Fatal("a withdrawn placement is still disclosed")
	}
}

// agentUX035Documents is a document authority with a placement and
// member-by-member answers, recording who it was asked about.
type agentUX035Documents struct {
	placed  bool
	denied  map[string]bool
	checked []string
}

func (a *agentUX035Documents) OfficialConversationPlacement(context.Context, string, string, string, string, string, string) (bool, error) {
	return a.placed, nil
}

func (a *agentUX035Documents) AuthorizeDocumentRead(_ context.Context, _, _, _, _, _, home, subject string) error {
	a.checked = append(a.checked, home+"/"+subject)
	if a.denied[home+"/"+subject] {
		return documenthubstore.ErrDenied
	}
	return nil
}

// TestTodo_AGENTUX_035_Guests: a placement speaks for the conversation's
// members of its own workspace and for nobody else. A guest in the audience is
// checked on their own; one guest who may not read the document keeps the
// answer private, in a channel of any size.
func TestTodo_AGENTUX_035_Guests(t *testing.T) {
	ctx := context.Background()
	document := AgentAnnouncementResolvedDocument{DocumentID: "holiday-guide", Version: "1", Digest: "sha256:" + strings.Repeat("c", 64)}
	snapshot := func(members ...chatrecipient.AudiencePrincipal) chatrecipient.AudienceSnapshot {
		return chatrecipient.AudienceSnapshot{TenantID: "tenant-a", ConversationID: "general", Revision: 8, CurrentMembers: members, Complete: true, GuestAndExternalComplete: true}
	}
	decide := func(authority *agentUX035Documents, audience chatrecipient.AudienceSnapshot) bool {
		t.Helper()
		allowed, _, _ := authorizeAnnouncementAudience(ctx, "tenant-a", "general", authority, audience, []AgentAnnouncementResolvedDocument{document}, []string{"holiday-guide"})
		return allowed
	}
	owner := chatrecipient.AudiencePrincipal{TenantID: "tenant-a", SubjectID: "owner"}
	employee := chatrecipient.AudiencePrincipal{TenantID: "tenant-a", SubjectID: "employee"}
	guest := chatrecipient.AudiencePrincipal{TenantID: "partner-co", SubjectID: "visitor"}

	// Members of the workspace only: the placement is enough and nobody is
	// looked up one by one.
	authority := &agentUX035Documents{placed: true, denied: map[string]bool{"tenant-a/employee": true}}
	if !decide(authority, snapshot(owner, employee)) || len(authority.checked) != 0 {
		t.Fatalf("a placement did not speak for the workspace's members: checked %v", authority.checked)
	}
	// A guest who may not read the document keeps it private.
	authority = &agentUX035Documents{placed: true, denied: map[string]bool{"partner-co/visitor": true}}
	if decide(authority, snapshot(owner, employee, guest)) {
		t.Fatal("a placed document was disclosed to a guest who may not read it")
	}
	if len(authority.checked) != 1 || authority.checked[0] != "partner-co/visitor" {
		t.Fatalf("checked %v, want the guest alone", authority.checked)
	}
	// A guest who may read it does not.
	authority = &agentUX035Documents{placed: true}
	if !decide(authority, snapshot(owner, employee, guest)) {
		t.Fatal("a placed document every member and guest may read was kept private")
	}
	// Not placed: every member is checked, and one refusal keeps it private.
	authority = &agentUX035Documents{denied: map[string]bool{"tenant-a/employee": true}}
	if decide(authority, snapshot(owner, employee)) || len(authority.checked) == 0 {
		t.Fatal("a document that is not placed was disclosed although a member may not read it")
	}
	// A large channel: a placement is enough for its members, and a document
	// that is not placed there stays private above 200 members, as before.
	large := make([]chatrecipient.AudiencePrincipal, 0, 260)
	for i := 0; i < 260; i++ {
		large = append(large, chatrecipient.AudiencePrincipal{TenantID: "tenant-a", SubjectID: "member-" + strconv.Itoa(i)})
	}
	if !decide(&agentUX035Documents{placed: true}, snapshot(large...)) {
		t.Fatal("an officially placed document cannot be disclosed in a channel of more than 200 members")
	}
	if decide(&agentUX035Documents{}, snapshot(large...)) {
		t.Fatal("a document that is not placed became public above 200 members")
	}
}

// agentUX035Policy is a conversation's agent policy as the chat store answers.
type agentUX035Policy struct {
	snapshot chatstore.PersonaChannelPolicySnapshot
	err      error
}

func (p agentUX035Policy) CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error) {
	return p.snapshot, p.err
}

// TestTodo_AGENTUX_035_Classes: which classes a conversation allows is read
// from its current agent policy; no policy, an always-private conversation and
// an unreadable policy allow nothing.
func TestTodo_AGENTUX_035_Classes(t *testing.T) {
	ctx := context.Background()
	held := chatstore.PersonaChannelPolicySnapshot{TenantID: "tenant-a", ConversationID: "general", PolicyRevision: 3, Policy: chatstore.PersonaChannelPolicy{AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}}}
	rule := AgentUX035ConversationClasses{Policies: agentUX035Policy{snapshot: held}}
	for class, want := range map[string]bool{"INTERNAL": true, " internal ": true, "PUBLIC": true, "CONFIDENTIAL": false, "": false} {
		if got, err := rule.ConversationAllowsDataClass(ctx, "tenant-a", "general", class); err != nil || got != want {
			t.Errorf("class %q: %t %v, want %t", class, got, err, want)
		}
	}
	private := held
	private.Policy.AlwaysPrivate = true
	other := held
	other.ConversationID = "random"
	unrevised := held
	unrevised.PolicyRevision = 0
	for name, policy := range map[string]agentUX035Policy{
		"always private":       {snapshot: private},
		"another conversation": {snapshot: other},
		"no revision":          {snapshot: unrevised},
		"no policy":            {err: dbport.ErrNoRows},
		"archived":             {err: chatstore.ErrAudienceEligibilityUnavailable},
	} {
		if got, err := (AgentUX035ConversationClasses{Policies: policy}).ConversationAllowsDataClass(ctx, "tenant-a", "general", "INTERNAL"); err != nil || got {
			t.Errorf("%s: %t %v", name, got, err)
		}
	}
	if got, err := (AgentUX035ConversationClasses{Policies: agentUX035Policy{err: errors.New("store offline")}}).ConversationAllowsDataClass(ctx, "tenant-a", "general", "INTERNAL"); got || err == nil {
		t.Errorf("an unreadable policy: %t %v", got, err)
	}
	if got, _ := (AgentUX035ConversationClasses{}).ConversationAllowsDataClass(ctx, "tenant-a", "general", "INTERNAL"); got {
		t.Error("a rule with no policy reader allowed a class")
	}
	if agentUX035Classes(nil) != nil {
		t.Error("a rule was composed with no chat store")
	}
}
