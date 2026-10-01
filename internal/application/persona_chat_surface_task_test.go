package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

type personaSurfaceTaskFixture struct {
	link  PersonaInvocationTask
	found bool
	err   error
	calls int
}

func (f *personaSurfaceTaskFixture) ReadPersonaInvocationTask(context.Context, agentinvoke.Invocation, runstate.Run) (PersonaInvocationTask, bool, error) {
	f.calls++
	return f.link, f.found, f.err
}

func TestTodo_AGENTP_020_SurfaceTaskSummaryRequiresActualOwnedTaskLink(t *testing.T) {
	s, ctx, room, invocations, execution := personaSurfaceFixture(t)
	reader := &personaSurfaceTaskFixture{found: true, link: PersonaInvocationTask{InvocationID: invocations.rows[0].ID, RunID: execution.run.ID, Task: agentrun.AgentTask{ID: "actual-task", TenantID: "tenant-a", UserID: "user-a", Goal: "Review my request", State: agentrun.StateWaiting, Version: 7, FailureDetail: "never expose owner error payload", Constraints: []string{"never expose task constraints"}}}}
	s.Tasks = reader
	progress, err := s.Progress(ctx, "channel-a")
	if err != nil || len(progress.Invocations) != 1 || progress.Invocations[0].TaskID != "actual-task" || progress.Invocations[0].TaskTitle != "Review my request" || progress.Invocations[0].TaskState != "WAITING" || progress.Invocations[0].TaskRevision != 7 {
		t.Fatalf("actual owned task=%+v %v", progress, err)
	}
	reader.found = false
	progress, err = s.Progress(ctx, "channel-a")
	if err != nil || progress.Invocations[0].TaskID != "" {
		t.Fatalf("absent task invented=%+v %v", progress, err)
	}
	reader.found = true
	for _, mutate := range []func(*PersonaInvocationTask){
		func(link *PersonaInvocationTask) { link.Task.UserID = "another-invoker" },
		func(link *PersonaInvocationTask) { link.Task.TenantID = "another-tenant" },
		func(link *PersonaInvocationTask) { link.InvocationID = "another-invocation" },
		func(link *PersonaInvocationTask) { link.RunID = "another-run" },
		func(link *PersonaInvocationTask) { link.Task.State = "PRIVATE PROVIDER ERROR" },
		func(link *PersonaInvocationTask) { link.Task.Version = 0 },
	} {
		original := reader.link
		mutate(&reader.link)
		if _, err := s.Progress(ctx, "channel-a"); !errors.Is(err, personachat.ErrUnavailable) {
			t.Fatalf("foreign or invalid task accepted=%+v %v", reader.link, err)
		}
		reader.link = original
	}
	reader.err = errors.New("owner failed with private detail")
	if _, err := s.Progress(ctx, "channel-a"); !errors.Is(err, personachat.ErrUnavailable) {
		t.Fatalf("owner error=%v", err)
	}
	reader.err = nil
	before := reader.calls
	room.members = nil
	if _, err := s.Progress(ctx, "channel-a"); !errors.Is(err, personachat.ErrDenied) || reader.calls != before {
		t.Fatalf("task owner reached after membership revoked calls=%d %v", reader.calls, err)
	}
}
