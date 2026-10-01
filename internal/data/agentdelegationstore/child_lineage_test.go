package agentdelegationstore

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
)

func TestTodo_AGENT_050_Grant_Integration(t *testing.T) {
	const tenant = "child-one"
	e := newEnv(t, tenant, "child-two")
	store := e.grants(t, tenant)
	service := newService(t, tenant, store)
	parent, err := service.CreateGrant(grantRequest(tenant, "grant-parent"))
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.Exchange(exchange(parent.GrantID))
	if err != nil {
		t.Fatal(err)
	}
	req := grantRequest(tenant, "grant-child")
	req.AgentVersion = "specialist-v1"
	req.TaskID = "child-task"
	req.CommonAdmissionID = "arreq_child_admission"
	req.ExpiresAt = storeNow.Add(time.Hour)
	req.Skills = []string{"people.lookup"}
	req.SkillScopes = map[string][]string{"people.lookup": {"people.read"}}
	boundary := agentdelegation.VerifyRequest{Audience: token.Claims.Audience, Sender: token.Claims.SenderConstraint}
	child, err := service.CreateChildGrant(req, token.Raw, boundary)
	if err != nil {
		t.Fatal(err)
	}
	fresh := e.grants(t, tenant)
	got, err := fresh.Get(child.GrantID)
	if err != nil || !reflect.DeepEqual(got, child) {
		t.Fatalf("restarted lineage = %+v, %v; want %+v", got, err, child)
	}
	service2 := newService(t, tenant, fresh)
	step := exchange(child.GrantID)
	step.RunID = child.TaskID
	credential, err := service2.Exchange(step)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Claims.Actor.Act == nil || credential.Claims.Actor.Act.AgentVersion != parent.AgentVersion {
		t.Fatalf("restart actor = %+v", credential.Claims.Actor)
	}
	if err := store.Revoke(parent.GrantID, "parent stopped"); err != nil {
		t.Fatal(err)
	}
	if _, err := service2.Verify(credential.Raw, boundary); !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("parent revoke after restart = %v", err)
	}
	other := e.grants(t, "child-two")
	if _, err := other.Get(child.GrantID); !errors.Is(err, agentdelegation.ErrGrantNotFound) {
		t.Fatalf("cross tenant child = %v", err)
	}
}
