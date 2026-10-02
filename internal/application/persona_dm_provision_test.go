package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type personaDMProvisionResolver struct {
	id         string
	createMiss bool
	calls      int
}

func (r *personaDMProvisionResolver) ResolvePersonaDM(context.Context, chatcore.Principal, string) (string, error) {
	r.calls++
	if r.createMiss && r.calls == 1 {
		return "", chatcore.ErrPermissionDenied
	}
	if r.id == "" {
		return "", chatcore.ErrPermissionDenied
	}
	return r.id, nil
}

type personaDMProvisionCreator struct {
	request chatcore.CreateConversationRequest
	err     error
}

type personaDMPolicyFake struct {
	audienceWrites int
	channelWrites  int
	snapshot       *chatstore.PersonaChannelPolicySnapshot
}

func (f *personaDMPolicyFake) PutAudiencePolicy(context.Context, string, string, int64, chatstore.AudiencePolicy) (int64, error) {
	f.audienceWrites++
	return 1, nil
}
func (f *personaDMPolicyFake) CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error) {
	if f.snapshot == nil {
		return chatstore.PersonaChannelPolicySnapshot{}, dbport.ErrNoRows
	}
	return *f.snapshot, nil
}
func (f *personaDMPolicyFake) PutPersonaChannelPolicy(_ context.Context, _, _ string, _ int64, policy chatstore.PersonaChannelPolicy) (int64, error) {
	f.channelWrites++
	f.snapshot = &chatstore.PersonaChannelPolicySnapshot{Policy: policy, PolicyRevision: int64(f.channelWrites)}
	return int64(f.channelWrites), nil
}

func (c *personaDMProvisionCreator) CreateConversation(_ context.Context, request chatcore.CreateConversationRequest) (chatcore.Conversation, error) {
	c.request = request
	if c.err != nil {
		return chatcore.Conversation{}, c.err
	}
	return chatcore.Conversation{ID: request.ConversationID, TenantID: request.TenantID, Kind: request.Kind}, nil
}

func newPersonaDMProvisionerFixture(t *testing.T, resolver *personaDMProvisionResolver, creator *personaDMProvisionCreator) PersonaDMProvisioner {
	t.Helper()
	provisioner, err := NewPersonaDMProvisioner(creator, resolver, chatcore.MemberRef{TenantID: "tenant-a", SubjectID: "persona-1"})
	if err != nil {
		t.Fatal(err)
	}
	return provisioner
}

func TestTodo_AGENTP_011_PersonaDMProvisionerReusesExistingDM(t *testing.T) {
	resolver := &personaDMProvisionResolver{id: "existing"}
	creator := &personaDMProvisionCreator{}
	got, err := newPersonaDMProvisionerFixture(t, resolver, creator).EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a")
	if err != nil || got != "existing" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if creator.request.ConversationID != "" {
		t.Fatal("existing DM unexpectedly created")
	}
}

func TestTodo_AGENTP_011_PersonaDMProvisionerCreatesAndVerifiesDeterministicPair(t *testing.T) {
	wantID, _ := chatcore.DirectPairConversationID("tenant-a", []chatcore.MemberRef{{TenantID: "tenant-a", SubjectID: "human-1"}, {TenantID: "tenant-a", SubjectID: "persona-1"}})
	resolver := &personaDMProvisionResolver{id: wantID, createMiss: true}
	creator := &personaDMProvisionCreator{}
	p := newPersonaDMProvisionerFixture(t, resolver, creator)
	got, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a")
	if err != nil || got != wantID {
		t.Fatalf("got=%q err=%v", got, err)
	}
	// AGENTUX-030: with no published name the conversation has no name of its
	// own; the agent's identifier is never made into one.
	if creator.request.ConversationID != wantID || creator.request.IdempotencyKey == "" || creator.request.Kind != chatcore.Direct || creator.request.Name != "" || len(creator.request.Members) != 1 || creator.request.Members[0] != p.Persona {
		t.Fatalf("unsafe create request=%+v", creator.request)
	}
}

func TestTodo_AGENTUX_025_PersonaDMProvisionerCreatesAndRepairsPolicies(t *testing.T) {
	wantID, _ := chatcore.DirectPairConversationID("tenant-a", []chatcore.MemberRef{{TenantID: "tenant-a", SubjectID: "human-1"}, {TenantID: "tenant-a", SubjectID: "persona-1"}})
	resolver := &personaDMProvisionResolver{id: wantID, createMiss: true}
	policies := &personaDMPolicyFake{}
	p := newPersonaDMProvisionerFixture(t, resolver, &personaDMProvisionCreator{})
	p.Policies = policies
	got, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a")
	if err != nil || got != wantID {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if policies.audienceWrites != 1 || policies.channelWrites != 1 || policies.snapshot == nil || !policies.snapshot.Policy.ConversationSearchAllowed || !policies.snapshot.Policy.AlwaysPrivate {
		t.Fatalf("policy writes audience=%d channel=%d snapshot=%+v", policies.audienceWrites, policies.channelWrites, policies.snapshot)
	}

	existing := &personaDMPolicyFake{}
	p = newPersonaDMProvisionerFixture(t, &personaDMProvisionResolver{id: wantID}, &personaDMProvisionCreator{})
	p.Policies = existing
	if _, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	if existing.audienceWrites != 1 || existing.channelWrites != 1 {
		t.Fatalf("existing conversation without policies was not repaired: %+v", existing)
	}
}

func TestTodo_AGENTUX_025_PersonaDMProvisionerLeavesAdministratorPolicyAlone(t *testing.T) {
	policies := &personaDMPolicyFake{snapshot: &chatstore.PersonaChannelPolicySnapshot{Policy: chatstore.PersonaChannelPolicy{MaxTier: "T1", PlacementClass: "ONE_TO_ONE_DM", AllowedDataClasses: []string{"PUBLIC"}, AllowedChannelClasses: []string{"ONE_TO_ONE"}, AlwaysPrivate: true}}}
	p := newPersonaDMProvisionerFixture(t, &personaDMProvisionResolver{id: "existing"}, &personaDMProvisionCreator{})
	p.Policies = policies
	if _, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	if policies.channelWrites != 0 {
		t.Fatalf("administrator-authored channel policy was overwritten")
	}
}

// TestTodo_AGENTUX_030_PersonaDMUsesAgentDisplayName: a new direct conversation
// with an agent is named after the agent's published name, whatever its
// identifier looks like; with no published name it has no name of its own. The
// identifier is never turned into a name, which is how a conversation came to
// be titled "673214ec-4402-5f09-bf93-0d42e691712f".
func TestTodo_AGENTUX_030_PersonaDMUsesAgentDisplayName(t *testing.T) {
	const agentID = "673214ec-4402-5f09-bf93-0d42e691712f"
	wantID, _ := chatcore.DirectPairConversationID("tenant-a", []chatcore.MemberRef{{TenantID: "tenant-a", SubjectID: "human-1"}, {TenantID: "tenant-a", SubjectID: agentID}})
	for published, want := range map[string]string{"Policy Helper": "Policy Helper", "  Policy \t Helper \n": "Policy Helper", "": "", "   ": ""} {
		resolver := &personaDMProvisionResolver{id: wantID, createMiss: true}
		creator := &personaDMProvisionCreator{}
		provisioner, err := NewPersonaDMProvisioner(creator, resolver, chatcore.MemberRef{TenantID: "tenant-a", SubjectID: agentID})
		if err != nil {
			t.Fatal(err)
		}
		provisioner.DisplayName = published
		if _, err = provisioner.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a"); err != nil {
			t.Fatal(err)
		}
		if creator.request.Name != want {
			t.Fatalf("published name %q: conversation name=%q, want %q", published, creator.request.Name, want)
		}
		for _, part := range []string{"673214ec", "0d42e691712f", "Bf93"} {
			if strings.Contains(creator.request.Name, part) {
				t.Fatalf("the conversation is named after its agent's identifier: %q", creator.request.Name)
			}
		}
	}
}

// personaDMPolicyPairFake is a policy store that writes the two policies of a
// direct conversation with an agent together.
type personaDMPolicyPairFake struct {
	personaDMPolicyFake
	pairs int
	err   error
}

func (f *personaDMPolicyPairFake) EnsurePersonaDirectPolicies(_ context.Context, tenant, conversation string, audience chatstore.AudiencePolicy, agent chatstore.PersonaChannelPolicy) (bool, bool, error) {
	f.pairs++
	if f.err != nil {
		return false, false, f.err
	}
	if tenant != "tenant-a" || conversation == "" || audience.Classification != "INTERNAL" || audience.RoleMode != 1 || agent.PlacementClass != "ONE_TO_ONE_DM" || !agent.AlwaysPrivate || len(agent.AllowedChannelClasses) != 1 || agent.AllowedChannelClasses[0] != "ONE_TO_ONE" {
		return false, false, errors.New("unexpected policies")
	}
	return true, true, nil
}

// TestTodo_AGENTUX_038: the path that creates a person's direct conversation
// with an agent gives it its audience policy and its one-to-one agent policy
// in one write, so a question asked there is not refused for want of either;
// when that write fails the conversation is not handed out as ready.
func TestTodo_AGENTUX_038(t *testing.T) {
	wantID, _ := chatcore.DirectPairConversationID("tenant-a", []chatcore.MemberRef{{TenantID: "tenant-a", SubjectID: "human-1"}, {TenantID: "tenant-a", SubjectID: "persona-1"}})
	human := chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}

	policies := &personaDMPolicyPairFake{}
	p := newPersonaDMProvisionerFixture(t, &personaDMProvisionResolver{id: wantID, createMiss: true}, &personaDMProvisionCreator{})
	p.Policies = policies
	got, err := p.EnsurePersonaDM(context.Background(), human, "tenant-a")
	if err != nil || got != wantID {
		t.Fatalf("got=%q err=%v", got, err)
	}
	// One write for both, and none of the separate writes it replaces.
	if policies.pairs != 1 || policies.audienceWrites != 0 || policies.channelWrites != 0 {
		t.Fatalf("pairs=%d audience=%d channel=%d", policies.pairs, policies.audienceWrites, policies.channelWrites)
	}
	// A conversation that already exists is repaired the same way.
	existing := &personaDMPolicyPairFake{}
	p = newPersonaDMProvisionerFixture(t, &personaDMProvisionResolver{id: wantID}, &personaDMProvisionCreator{})
	p.Policies = existing
	if _, err = p.EnsurePersonaDM(context.Background(), human, "tenant-a"); err != nil || existing.pairs != 1 {
		t.Fatalf("an existing conversation: pairs=%d err=%v", existing.pairs, err)
	}
	// The write fails: no conversation is reported ready, and nothing is
	// written piecemeal behind it.
	failing := &personaDMPolicyPairFake{err: errors.New("store offline")}
	p = newPersonaDMProvisionerFixture(t, &personaDMProvisionResolver{id: wantID, createMiss: true}, &personaDMProvisionCreator{})
	p.Policies = failing
	if got, err = p.EnsurePersonaDM(context.Background(), human, "tenant-a"); !errors.Is(err, errPersonaDMProvisionUnavailable) || got != "" {
		t.Fatalf("a failed policy write: got=%q err=%v", got, err)
	}
	if failing.audienceWrites != 0 || failing.channelWrites != 0 {
		t.Fatalf("policies were written one by one after the pair failed: %+v", failing)
	}
	// The served chat store is such a store.
	var _ personaDMPolicyPair = (*chatstore.Store)(nil)
	var _ personaDMPolicyPair = (*chatstore.Adapter)(nil)
}

func TestTodo_AGENTP_011_PersonaDMProvisionerFailsClosed(t *testing.T) {
	resolver := &personaDMProvisionResolver{id: "", createMiss: false}
	creator := &personaDMProvisionCreator{}
	p := newPersonaDMProvisionerFixture(t, resolver, creator)
	if _, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-b"); !errors.Is(err, errPersonaDMProvisionInvalid) {
		t.Fatalf("wrong tenant error=%v", err)
	}
	resolver.id = ""
	creator.err = chatcore.ErrUnavailable
	if _, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a"); !errors.Is(err, errPersonaDMProvisionUnavailable) {
		t.Fatalf("create failure error=%v", err)
	}
}
