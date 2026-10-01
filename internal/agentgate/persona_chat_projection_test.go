package agentgate

import (
	"context"
	"testing"
)

type personaChatPolicyFake struct{ *privateChatPolicyFake }

func (f personaChatPolicyFake) AuthorizePersonaChat(ctx context.Context, req PrivateChatScopeRequest) (PrivateChatScopeEvidence, error) {
	return f.AuthorizePrivateChat(ctx, req)
}

func TestTodo_AGENTP_008_PublicChatProjectionRequiresCurrentAudienceAuthority(t *testing.T) {
	gate, req, private, grants := privateChatFixture(t)
	policy := personaChatPolicyFake{private}
	pdp := &recordingPDP{}
	gate.pdp = pdp
	if _, err := gate.ProjectPersonaChatSkillAuthorization(context.Background(), req, policy); err != nil {
		t.Fatalf("private scope through general path: %v", err)
	}
	private.evidence.PrivateConversation = false
	private.evidence.PublicConversation = true
	private.evidence.PublicPolicyRev = 7
	projection, err := gate.ProjectPersonaChatSkillAuthorization(context.Background(), req, policy)
	if err != nil || projection.Scope != "chat.current" || !projection.Evidence.PublicConversation || len(pdp.calls) != 0 {
		t.Fatalf("public domain projection=%#v err=%v PDP calls=%d", projection, err, len(pdp.calls))
	}
	if _, err := gate.ProjectPrivateChatSkillAuthorization(context.Background(), req, private); deniedCode(t, err) != DenySubject {
		t.Fatalf("private-only path admitted public evidence: %v", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*PrivateChatScopeEvidence)
	}{
		{"no public policy", func(e *PrivateChatScopeEvidence) { e.PublicPolicyRev = 0 }},
		{"ambiguous channel kind", func(e *PrivateChatScopeEvidence) { e.PrivateConversation = true }},
		{"unknown channel kind", func(e *PrivateChatScopeEvidence) { e.PublicConversation = false }},
		{"membership revoked", func(e *PrivateChatScopeEvidence) { e.ActiveMember = false }},
		{"post hidden", func(e *PrivateChatScopeEvidence) { e.PostVisible = false }},
		{"foreign post", func(e *PrivateChatScopeEvidence) { e.InvokingPostID = "another-post" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := private.evidence
			tc.change(&private.evidence)
			defer func() { private.evidence = before }()
			if _, err := gate.ProjectPersonaChatSkillAuthorization(context.Background(), req, policy); deniedCode(t, err) != DenySubject {
				t.Fatalf("invalid public authority: %v", err)
			}
		})
	}
	grants.grants[0].Roles = []string{"other-role"}
	if _, err := gate.ProjectPersonaChatSkillAuthorization(context.Background(), req, policy); deniedCode(t, err) != DenyRole {
		t.Fatalf("revoked administrator grant: %v", err)
	}
	if _, err := gate.ProjectPersonaChatSkillAuthorization(context.Background(), req, nil); deniedCode(t, err) != DenyInvalid {
		t.Fatalf("missing current policy owner: %v", err)
	}
}
