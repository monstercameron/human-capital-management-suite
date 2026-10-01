package application

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentControlsSurfaceFixture struct {
	reply    agentcontrols.Reply
	err      error
	commands []productui.AgentControlsCommand
	drafts   []productui.AgentScheduleDraft
	reads    int
}

func (f *agentControlsSurfaceFixture) Snapshot(context.Context) (agentcontrols.Reply, error) {
	f.reads++
	return f.reply, f.err
}

func TestTodo_AGENT_041_ControlsAdmission(t *testing.T) {
	owner := &agentControlsSurfaceFixture{reply: agentcontrols.Reply{Snapshot: productui.AgentControlsSnapshot{Available: true}}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })
	handler := OverlayAgentControlsSurface(next, &AgentControlsSurface{Operations: owner}, transport.Config{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, agentcontrols.Path, nil))
	if response.Code == http.StatusOK || owner.reads != 0 {
		t.Fatal("unadmitted request reached owner projection")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/workspace/app/agents", nil))
	if response.Code != http.StatusAccepted {
		t.Fatal("controls overlay captured a product route")
	}
}
func (f *agentControlsSurfaceFixture) Control(_ context.Context, c productui.AgentControlsCommand) (agentcontrols.Reply, error) {
	f.commands = append(f.commands, c)
	return f.reply, f.err
}
func (f *agentControlsSurfaceFixture) Draft(_ context.Context, d productui.AgentScheduleDraft) (agentcontrols.Reply, error) {
	f.drafts = append(f.drafts, d)
	return f.reply, f.err
}
func (f *agentControlsSurfaceFixture) Preview(ctx context.Context, d productui.AgentScheduleDraft) (agentcontrols.Reply, error) {
	return f.Draft(ctx, d)
}

func TestTodo_AGENT_041_ControlsComposition(t *testing.T) {
	ctx := trust.WithPrincipal(context.Background(), agentUserCatalogPrincipal(t))
	schedules := &agentControlsSurfaceFixture{reply: agentcontrols.Reply{Snapshot: productui.AgentControlsSnapshot{Available: true, CanDraft: true, Schedules: []productui.AgentControlSchedule{{ID: "schedule"}}}}}
	owners := &agentControlsSurfaceFixture{reply: agentcontrols.Reply{Snapshot: productui.AgentControlsSnapshot{Available: true, CanExport: true, Runs: []productui.AgentControlRun{{ID: "run"}}, Memory: []productui.AgentControlMemory{{ID: "memory"}}}}}
	surface := &AgentControlsSurface{Schedules: schedules, Operations: owners}
	reply, err := surface.Snapshot(ctx)
	if err != nil || !reply.Snapshot.CanDraft || !reply.Snapshot.CanExport || len(reply.Snapshot.Schedules) != 1 || len(reply.Snapshot.Runs) != 1 || len(reply.Snapshot.Memory) != 1 {
		t.Fatalf("owner projections lost: %#v %v", reply, err)
	}
	for _, kind := range []string{"schedule", "run", "memory"} {
		_, err := surface.Control(ctx, productui.AgentControlsCommand{Kind: kind, ID: kind, ExpectedRevision: 9})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(schedules.commands) != 1 || len(owners.commands) != 2 || owners.commands[0].ExpectedRevision != 9 {
		t.Fatal("command routed to wrong authority")
	}
	for _, call := range []func(context.Context, productui.AgentScheduleDraft) (agentcontrols.Reply, error){surface.Draft, surface.Preview} {
		if _, err := call(ctx, productui.AgentScheduleDraft{ID: "weekly", ExpectedRevision: 2}); err != nil {
			t.Fatal(err)
		}
	}
	if len(schedules.drafts) != 2 || schedules.drafts[1].ExpectedRevision != 2 {
		t.Fatal("draft lost revision")
	}
}

func TestTodo_AGENT_041_ControlsCompositionSecurity(t *testing.T) {
	owner := &agentControlsSurfaceFixture{}
	surface := &AgentControlsSurface{Operations: owner}
	if _, err := surface.Control(context.Background(), productui.AgentControlsCommand{Kind: "run"}); !errors.Is(err, agentcontrols.ErrUnauthenticated) || len(owner.commands) != 0 {
		t.Fatal("unauthenticated mutation reached owner")
	}
	ctx := trust.WithPrincipal(context.Background(), agentUserCatalogPrincipal(t))
	if _, err := surface.Control(ctx, productui.AgentControlsCommand{Kind: "arbitrary"}); !errors.Is(err, agentcontrols.ErrInvalid) || len(owner.commands) != 0 {
		t.Fatal("unbounded operation reached owner")
	}
	if _, err := surface.Draft(ctx, productui.AgentScheduleDraft{}); !errors.Is(err, agentcontrols.ErrUnavailable) {
		t.Fatal("missing schedule owner fabricated success")
	}
	owner.err = agentcontrols.ErrDenied
	reply, err := surface.Snapshot(ctx)
	if err != nil || reply.Snapshot.Available || len(reply.Snapshot.Runs) != 0 {
		t.Fatal("denied projection leaked owner records")
	}
}

func TestTodo_AGENT_030_ControlsPreview(t *testing.T) {
	reply := agentcontrols.Reply{Snapshot: productui.AgentControlsSnapshot{Available: true, CanDraft: true, Schedules: []productui.AgentControlSchedule{{ID: "weekly", Revision: 9, Actions: []string{"pause"}}}}}
	preview := agentcontrols.Reply{Snapshot: productui.AgentControlsSnapshot{Schedules: []productui.AgentControlSchedule{{ID: "weekly", Revision: 1, Occurrences: []string{"2026-11-01T01:30:00-04:00"}, OccurrenceKeys: []string{"exact-fold-occurrence"}, Actions: []string{"publish"}}, {ID: "draft", Occurrences: []string{"2026-11-02T09:00:00-05:00"}, Actions: []string{"publish"}}}}}
	mergeAgentOccurrencePreview(&reply, preview)
	if !reply.Snapshot.CanDraft || reply.Snapshot.Schedules[0].Revision != 9 || reply.Snapshot.Schedules[0].Actions[0] != "pause" || reply.Snapshot.Schedules[0].OccurrenceKeys[0] != "exact-fold-occurrence" || len(reply.Snapshot.Schedules) != 2 || len(reply.Snapshot.Schedules[1].Actions) != 0 {
		t.Fatal("occurrence preview overwrote authority or revision")
	}
}
