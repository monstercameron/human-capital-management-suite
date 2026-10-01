package agentdelegation_test

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
)

func TestTodo_AGENT_050_Golden(t *testing.T) {
	service, _, parent := fixture(t, nil)
	credential, err := service.Exchange(exchangeRequest(parent))
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.CreateChildGrant(childRequest(parent), credential.Raw, agentdelegation.VerifyRequest{Audience: credential.Claims.Audience, Sender: credential.Claims.SenderConstraint})
	if err != nil {
		t.Fatal(err)
	}
	req := exchangeRequest(child)
	req.RunID = child.TaskID
	token, err := service.Exchange(req)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(token.Claims.Actor)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"agent_version":"specialist-v1","installation_id":"specialist-install","run_id":"child-task","step_id":"step-1","act":{"agent_version":"agent-v3","installation_id":"install-7","run_id":"run-1","step_id":"step-1"}}`
	if string(encoded) != want {
		t.Fatalf("child actor golden = %s", encoded)
	}
}

func childRequest(parent agentdelegation.Grant) agentdelegation.GrantRequest {
	return agentdelegation.GrantRequest{GrantID: "grant-child", UserID: parent.UserID, Tenant: parent.Tenant, AgentVersion: "specialist-v1", InstallationID: "specialist-install", TaskID: "child-task", PlanSkillSetDigest: "sha256:child-plan", Purpose: parent.Purpose, OrganizationScopeID: parent.OrganizationScopeID, Skills: []string{"people.lookup"}, SkillScopes: map[string][]string{"people.lookup": {"people.read"}}, NotBefore: delegationNow, ExpiresAt: delegationNow.Add(time.Hour), UserAuthority: agentdelegation.InheritedAuthority(parent)}
}

func TestTodo_AGENT_050(t *testing.T) {
	service, store, parent := fixture(t, nil)
	credential, err := service.Exchange(exchangeRequest(parent))
	if err != nil {
		t.Fatal(err)
	}
	boundary := agentdelegation.VerifyRequest{Audience: credential.Claims.Audience, Sender: credential.Claims.SenderConstraint}
	child, err := service.CreateChildGrant(childRequest(parent), credential.Raw, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentGrantID != parent.GrantID || child.ParentActor == nil || child.ParentActor.AgentVersion != parent.AgentVersion {
		t.Fatalf("child lineage = %+v", child)
	}
	req := exchangeRequest(child)
	req.RunID = child.TaskID
	token, err := service.Exchange(req)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.Verify(token.Raw, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != parent.UserID || claims.Actor.RunID != child.TaskID || claims.Actor.AgentVersion != child.AgentVersion || claims.Actor.Act == nil || claims.Actor.Act.AgentVersion != parent.AgentVersion {
		t.Fatalf("child actor = %+v", claims)
	}
	current, err := service.CurrentAuthority(child.GrantID)
	if err != nil || current.Tenant != parent.Tenant || len(current.Resources) == 0 {
		t.Fatalf("current child authority = %+v, %v", current, err)
	}
	verified, effective, err := service.VerifyAuthority(token.Raw, boundary)
	if err != nil || verified.Subject != parent.UserID || effective.Tenant != parent.Tenant || len(effective.Resources) == 0 {
		t.Fatalf("verified current child authority = %+v, %+v, %v", verified, effective, err)
	}
	if err := store.Revoke(parent.GrantID, "parent revoked"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(token.Raw, boundary); !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("revoked parent = %v", err)
	}
	if _, err := service.CurrentAuthority(child.GrantID); !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("current authority with revoked parent = %v", err)
	}
}

func TestTodo_AGENT_050_Security(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*agentdelegation.GrantRequest)
	}{
		{"tenant", func(r *agentdelegation.GrantRequest) { r.Tenant = "other" }},
		{"subject", func(r *agentdelegation.GrantRequest) { r.UserID = "other" }},
		{"resource", func(r *agentdelegation.GrantRequest) { r.UserAuthority.Resources = []string{"worker:99"} }},
		{"scope", func(r *agentdelegation.GrantRequest) { r.SkillScopes["people.lookup"] = []string{"people.write"} }},
		{"deadline", func(r *agentdelegation.GrantRequest) { r.ExpiresAt = delegationNow.Add(48 * time.Hour) }},
		{"cycle", func(r *agentdelegation.GrantRequest) { r.AgentVersion = "agent-v3" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, _, parent := fixture(t, nil)
			token, err := service.Exchange(exchangeRequest(parent))
			if err != nil {
				t.Fatal(err)
			}
			req := childRequest(parent)
			test.change(&req)
			_, err = service.CreateChildGrant(req, token.Raw, agentdelegation.VerifyRequest{Audience: token.Claims.Audience, Sender: token.Claims.SenderConstraint})
			if err == nil {
				t.Fatal("expanded child grant admitted")
			}
		})
	}
}

func TestTodo_AGENT_050_Race(t *testing.T) {
	service, store, parent := fixture(t, nil)
	token, err := service.Exchange(exchangeRequest(parent))
	if err != nil {
		t.Fatal(err)
	}
	boundary := agentdelegation.VerifyRequest{Audience: token.Claims.Audience, Sender: token.Claims.SenderConstraint}
	child, err := service.CreateChildGrant(childRequest(parent), token.Raw, boundary)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			req := exchangeRequest(child)
			req.RunID = child.TaskID
			_, _ = service.Exchange(req)
		}()
	}
	if err := store.Revoke(parent.GrantID, "race"); err != nil {
		t.Fatal(err)
	}
	wait.Wait()
	req := exchangeRequest(child)
	req.RunID = child.TaskID
	if _, err := service.Exchange(req); !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("exchange after revoke = %v", err)
	}
}
