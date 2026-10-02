package application

import (
	"context"
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

type localPersonaChatPolicyFake struct {
	public  *chatstore.PublicAudiencePolicy
	ceiling *chatstore.PersonaChannelPolicy
	writes  int
}

func (f *localPersonaChatPolicyFake) CapturePublicAudienceSnapshot(context.Context, string, string) (chatstore.PublicAudienceSnapshot, error) {
	if f.public == nil {
		return chatstore.PublicAudienceSnapshot{}, dbport.ErrNoRows
	}
	return chatstore.PublicAudienceSnapshot{Classification: f.public.Classification, Eligible: f.public.Principals}, nil
}
func (f *localPersonaChatPolicyFake) CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error) {
	if f.ceiling == nil {
		return chatstore.PersonaChannelPolicySnapshot{}, dbport.ErrNoRows
	}
	return chatstore.PersonaChannelPolicySnapshot{Policy: *f.ceiling}, nil
}
func (f *localPersonaChatPolicyFake) PutPublicAudiencePolicy(_ context.Context, _, _ string, revision int64, policy chatstore.PublicAudiencePolicy) (int64, error) {
	if revision != 0 || f.public != nil {
		return 0, chatstore.ErrAudiencePolicyConflict
	}
	f.public = &policy
	f.writes++
	return 1, nil
}
func (f *localPersonaChatPolicyFake) PutPersonaChannelPolicy(_ context.Context, _, _ string, revision int64, policy chatstore.PersonaChannelPolicy) (int64, error) {
	if revision != 0 || f.ceiling != nil {
		return 0, chatstore.ErrAudiencePolicyConflict
	}
	f.ceiling = &policy
	f.writes++
	return 1, nil
}

func TestTodo_AGENTP_007_LocalDemoBootstrap(t *testing.T) {
	for _, tenant := range []string{"harborcare-demo", "ironridge-demo"} {
		t.Run(tenant, func(t *testing.T) {
			fake := &localPersonaChatPolicyFake{}
			n, err := ProvisionLocalDevPersonaChatPolicy(context.Background(), fake, ServeProfileLocalDev, tenant, localDevPersonaDemoConversationID(tenant, "general"), "general")
			if err != nil || n != 2 || fake.writes != 2 {
				t.Fatalf("bootstrap writes=%d created=%d err=%v", fake.writes, n, err)
			}
			if fake.public.Classification != "INTERNAL" || fake.ceiling.MaxTier != "T0" || fake.ceiling.AllowExternalMembers || fake.ceiling.AllowCrossCompanyMembers {
				t.Fatalf("unsafe room authority: %+v %+v", fake.public, fake.ceiling)
			}
			pack, _ := demoworkforce.PackFor(tenant)
			workers, err := pack.Plan(pgstore.TenantID(tenant))
			if err != nil {
				t.Fatal(err)
			}
			expected := map[string]bool{}
			for _, employee := range workers {
				if employee.Row.WorkerType == "employee" && employee.Row.LifecycleStatus == "active" {
					expected[employee.Row.WorkerKey] = true
				}
			}
			for _, principal := range fake.public.Principals {
				if principal.HomeTenantID != tenant || principal.Guest || !expected[principal.SubjectID] {
					t.Fatalf("untrusted admission %+v", principal)
				}
				delete(expected, principal.SubjectID)
			}
			if len(expected) != 0 {
				t.Fatalf("omitted %d eligible employees", len(expected))
			}
			n, err = ProvisionLocalDevPersonaChatPolicy(context.Background(), fake, ServeProfileLocalDev, tenant, localDevPersonaDemoConversationID(tenant, "general"), "general")
			if err != nil || n != 0 || fake.writes != 2 {
				t.Fatalf("replay writes=%d created=%d err=%v", fake.writes, n, err)
			}
		})
	}
}

func TestTodo_AGENTP_007_LocalDemoBootstrapSecurity(t *testing.T) {
	for _, tc := range []struct{ profile, tenant, room string }{{ServeProfileStandard, "ironridge-demo", "general"}, {ServeProfileLocalDev, "production-tenant", "general"}, {ServeProfileLocalDev, "ironridge-demo", "arbitrary-channel"}} {
		fake := &localPersonaChatPolicyFake{}
		_, err := ProvisionLocalDevPersonaChatPolicy(context.Background(), fake, tc.profile, tc.tenant, "conversation", tc.room)
		if !errors.Is(err, ErrLocalDevPersonaChatBootstrap) || fake.writes != 0 {
			t.Fatalf("unauthorized setup %+v writes=%d err=%v", tc, fake.writes, err)
		}
	}
	fake := &localPersonaChatPolicyFake{}
	if _, err := ProvisionLocalDevPersonaChatPolicy(context.Background(), fake, ServeProfileLocalDev, "ironridge-demo", "arbitrary-conversation", "general"); !errors.Is(err, ErrLocalDevPersonaChatBootstrap) || fake.writes != 0 {
		t.Fatalf("room facts assigned to unrelated conversation writes=%d err=%v", fake.writes, err)
	}
	if _, err := ProvisionLocalDevPersonaChatPolicy(context.Background(), fake, ServeProfileLocalDev, "ironridge-demo", localDevPersonaDemoConversationID("ironridge-demo", "general"), "general"); err != nil {
		t.Fatal(err)
	}
	fake.public.Principals = fake.public.Principals[1:]
	if _, err := ProvisionLocalDevPersonaChatPolicy(context.Background(), fake, ServeProfileLocalDev, "ironridge-demo", localDevPersonaDemoConversationID("ironridge-demo", "general"), "general"); !errors.Is(err, ErrLocalDevPersonaChatBootstrap) || fake.writes != 2 {
		t.Fatalf("changed authority accepted or mutated writes=%d err=%v", fake.writes, err)
	}
	private := &localPersonaChatPolicyFake{}
	if n, err := ProvisionLocalDevPersonaChatPolicy(context.Background(), private, ServeProfileLocalDev, "ironridge-demo", localDevPersonaDemoConversationID("ironridge-demo", "leadership-private"), "leadership-private"); err != nil || n != 1 || private.public != nil || !private.ceiling.AlwaysPrivate || private.ceiling.MaxTier != "T1" {
		t.Fatalf("private setup created=%d err=%v state=%+v", n, err, private)
	}
}

func TestTodo_AGENTUX_005_DirectConversationPolicy(t *testing.T) {
	conversation, err := chatcore.DirectPairConversationID(localAgentDemoTenant, []chatcore.MemberRef{{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}, {TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAgentID}})
	if err != nil {
		t.Fatal(err)
	}
	fake := &localPersonaChatPolicyFake{}
	created, err := ProvisionLocalDevPersonaDirectPolicy(context.Background(), fake, ServeProfileLocalDev, localAgentDemoTenant, conversation, localAgentDemoAdmin)
	if err != nil || created != 1 || fake.writes != 1 || fake.ceiling.MaxTier != "T3" || !fake.ceiling.AlwaysPrivate || len(fake.ceiling.AllowedChannelClasses) != 1 || fake.ceiling.AllowedChannelClasses[0] != "ONE_TO_ONE" {
		t.Fatalf("direct policy created=%d writes=%d policy=%+v err=%v", created, fake.writes, fake.ceiling, err)
	}
	if created, err = ProvisionLocalDevPersonaDirectPolicy(context.Background(), fake, ServeProfileLocalDev, localAgentDemoTenant, conversation, localAgentDemoAdmin); err != nil || created != 0 || fake.writes != 1 {
		t.Fatalf("direct policy replay created=%d writes=%d err=%v", created, fake.writes, err)
	}
	for _, administrator := range []string{"different", ""} {
		blocked := &localPersonaChatPolicyFake{}
		if _, err := ProvisionLocalDevPersonaDirectPolicy(context.Background(), blocked, ServeProfileLocalDev, localAgentDemoTenant, conversation, administrator); !errors.Is(err, ErrLocalDevPersonaChatBootstrap) || blocked.writes != 0 {
			t.Fatalf("unbound direct policy administrator=%q writes=%d err=%v", administrator, blocked.writes, err)
		}
	}
}
