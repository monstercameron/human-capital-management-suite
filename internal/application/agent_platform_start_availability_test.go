package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENT_041_StartAvailabilityUsesCurrentRuntimeAuthority(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	principal := agentTestPrincipal(t, f.tenant, agentTestWorker)
	ctx := trust.WithPrincipal(context.Background(), principal)
	request := productui.AgentSnapshotRequest{TenantID: f.tenant, Principal: agentTestWorker}
	client := agentStartAvailabilityClient{AgentClient: agentclient.FromPlatform(f.runtime.Platform), starter: f.runtime.Starter}
	snapshot, err := client.Snapshot(ctx, request)
	if err != nil || !snapshot.StartAvailable {
		t.Fatalf("ready snapshot=%+v err=%v", snapshot, err)
	}
	f.runtime.Starter.model = agentModel{Kind: agentModelUnavailable}
	snapshot, err = client.Snapshot(ctx, request)
	if err != nil || snapshot.StartAvailable || snapshot.StartUnavailableReason != "model_unavailable" {
		t.Fatalf("no model snapshot=%+v err=%v", snapshot, err)
	}
	f.runtime.Starter.model = agentModel{Kind: agentModelFake}
	f.setEnabled(t, false)
	snapshot, err = client.Snapshot(ctx, request)
	if err != nil || snapshot.StartAvailable || snapshot.StartUnavailableReason != "disabled" {
		t.Fatalf("disabled snapshot=%+v err=%v", snapshot, err)
	}
	snapshot, err = client.Snapshot(context.Background(), request)
	if err != nil || snapshot.StartAvailable || snapshot.StartUnavailableReason != "denied" {
		t.Fatalf("unverified snapshot=%+v err=%v", snapshot, err)
	}
}
