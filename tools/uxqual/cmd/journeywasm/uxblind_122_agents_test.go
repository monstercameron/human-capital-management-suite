package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestTodo_UXBLIND_122_ClientProjection(t *testing.T) {
	var cfg journeyclient.Config
	island := `{"tunnel_url":"ws://x","bearer":"b","agents":{"enabled":true,"viewer_is_admin":false,"service":"available","tasks":[
		{"id":"t1","version":7,"title":"Mine","state":"completed","answer_text":"sanitized answer","actions":{"confirm_plan":true,"cancel":true},"steps":[{"name":"skill.lookup","state":"running","tier":"T0"}],"approvals":[{"id":"t1/a","digest":"sha256:x","summary":"skill.lookup"}]},
		{"id":"t1","title":"Duplicate","state":"running"},
		{"id":"t2","title":"Odd state","state":"exploded"}]}}`
	if err := json.Unmarshal([]byte(island), &cfg); err != nil {
		t.Fatal(err)
	}
	got := projectAgents(cfg.Agents)
	if !got.Enabled || got.Snapshot.Availability != productui.AgentsAvailable || len(got.Snapshot.Tasks) != 2 {
		t.Fatalf("projection = %+v", got)
	}
	if got.Snapshot.Tasks[0].Version != 7 || got.Snapshot.Tasks[0].Title != "Mine" || got.Snapshot.Tasks[0].AnswerText != "sanitized answer" || got.Snapshot.Tasks[0].Steps[0].Tier != "T0" || got.Snapshot.Tasks[0].Approvals[0].Digest != "sha256:x" || !got.Snapshot.Tasks[0].Actions.ConfirmPlan || !got.Snapshot.Tasks[0].Actions.Cancel {
		t.Fatalf("task = %+v", got.Snapshot.Tasks[0])
	}
	if got.Snapshot.Tasks[1].State != productui.AgentTaskUnknown {
		t.Fatalf("unknown state was not bounded: %s", got.Snapshot.Tasks[1].State)
	}
	for state, want := range map[string]productui.AgentTaskState{
		"awaiting_plan_confirmation": productui.AgentTaskAwaitingPlanConfirmation,
		"drafting":                   productui.AgentTaskDrafting, "waiting": productui.AgentTaskWaiting,
		"cancelled": productui.AgentTaskCancelled, "expired": productui.AgentTaskExpired,
	} {
		projected := projectAgents(&journeyclient.Agents{Enabled: true, Tasks: []journeyclient.AgentTask{{ID: state, State: state}}})
		if got := projected.Snapshot.Tasks[0].State; got != want {
			t.Errorf("state %q projected as %q, want %q", state, got, want)
		}
	}
	running := projectAgents(&journeyclient.Agents{Enabled: true, Service: "available", Tasks: []journeyclient.AgentTask{{ID: "running", State: "running", AnswerText: "must stay hidden"}}})
	if answer := running.Snapshot.Tasks[0].AnswerText; answer != "" {
		t.Fatalf("running task exposed answer text: %q", answer)
	}
	// Missing island: fail closed. Disabled island with forged tasks: none kept.
	if missing := projectAgents(nil); missing.Enabled || missing.ViewerIsAdmin || productui.ResolveAgentsSurface(missing).NavVisible {
		t.Fatalf("missing projection = %+v", missing)
	}
	forged := projectAgents(&journeyclient.Agents{ViewerIsAdmin: true, SettingsHref: "javascript:alert(1)", Tasks: []journeyclient.AgentTask{{ID: "x", Title: "forged", State: "running"}}})
	if len(forged.Snapshot.Tasks) != 0 || forged.SettingsHref != productui.AgentsSettingsHref() {
		t.Fatalf("forged projection = %+v", forged)
	}
	if unavailable := projectAgents(&journeyclient.Agents{Enabled: true, Service: "unavailable"}); unavailable.Snapshot.Availability != productui.AgentsUnavailable {
		t.Fatalf("unavailable service = %+v", unavailable)
	}
}

func TestTodo_UXBLIND_122_AgentServiceRequests(t *testing.T) {
	if on, off := newSetAgentsEnabledRequest(true), newSetAgentsEnabledRequest(false); !on.GetEnabled() || off.GetEnabled() {
		t.Fatalf("set requests = %v %v", on, off)
	}
	request, ok := newStartAgentTaskRequest("  summarise my week \n")
	if !ok || request.GetPrompt() != "summarise my week" {
		t.Fatalf("start request = %v %v", request, ok)
	}
	for name, text := range map[string]string{
		"empty": "", "blank": "  \n\t", "invalid utf8": "a\xff", "over the cap": strings.Repeat("a", agentPromptMaxBytes+1),
	} {
		if _, ok := newStartAgentTaskRequest(text); ok {
			t.Fatalf("%s text was accepted", name)
		}
	}
	if _, ok := newStartAgentTaskRequest(strings.Repeat("a", agentPromptMaxBytes)); !ok {
		t.Fatal("text at the cap was refused")
	}
	// The server-side cap this mirrors.
	if agentPromptMaxBytes != 8<<10 {
		t.Fatalf("agentPromptMaxBytes = %d; keep it equal to internal/transport/agents.MaxPromptBytes", agentPromptMaxBytes)
	}
	// The request messages carry no tenant, actor or user, so the browser has
	// nothing to forge; only the session bearer rides in metadata.
	for _, field := range []string{"tenant_id", "actor", "user_id"} {
		if newSetAgentsEnabledRequest(true).ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(field)) != nil ||
			request.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(field)) != nil {
			t.Fatalf("request carries %s", field)
		}
	}
}

// fakeAgentClient answers with a scripted response or error.
type fakeAgentClient struct {
	agentv1.AgentServiceClient
	set        *agentv1.SetAgentsEnabledResponse
	start      *agentv1.StartAgentTaskResponse
	err        error
	bearer     string
	gotStart   *agentv1.StartAgentTaskRequest
	control    *agentv1.ControlAgentTaskResponse
	gotControl *agentv1.ControlAgentTaskRequest
}

func (c *fakeAgentClient) capture(ctx context.Context) {
	md, _ := metadata.FromOutgoingContext(ctx)
	if v := md.Get(journeyclient.AuthorizationHeader); len(v) > 0 {
		c.bearer = v[0]
	}
}

func (c *fakeAgentClient) SetAgentsEnabled(ctx context.Context, _ *agentv1.SetAgentsEnabledRequest, _ ...grpc.CallOption) (*agentv1.SetAgentsEnabledResponse, error) {
	c.capture(ctx)
	return c.set, c.err
}

func (c *fakeAgentClient) StartAgentTask(ctx context.Context, in *agentv1.StartAgentTaskRequest, _ ...grpc.CallOption) (*agentv1.StartAgentTaskResponse, error) {
	c.capture(ctx)
	c.gotStart = in
	return c.start, c.err
}

func (c *fakeAgentClient) ControlAgentTask(ctx context.Context, in *agentv1.ControlAgentTaskRequest, _ ...grpc.CallOption) (*agentv1.ControlAgentTaskResponse, error) {
	c.capture(ctx)
	c.gotControl = in
	return c.control, c.err
}

func TestTodo_UXBLIND_122_AgentServiceBinding(t *testing.T) {
	binding := &agentServiceBinding{cfg: journeyclient.Config{Bearer: "tok"}}
	// No tunnel: every write fails closed.
	if err := binding.SetAgentsEnabled(context.Background(), true); !errors.Is(err, errAgentSetting) {
		t.Fatalf("no client toggle = %v", err)
	}
	if _, err := binding.StartTask(context.Background(), "hello"); !errors.Is(err, errAgentTask) {
		t.Fatalf("no client start = %v", err)
	}
	client := &fakeAgentClient{set: &agentv1.SetAgentsEnabledResponse{Enabled: true}, start: &agentv1.StartAgentTaskResponse{TaskId: "task-9", State: "running"}}
	binding.client = client
	if err := binding.SetAgentsEnabled(context.Background(), true); err != nil || client.bearer != journeyclient.BearerScheme+"tok" {
		t.Fatalf("toggle = %v bearer=%q", err, client.bearer)
	}
	// A server that echoes the opposite value is a failed save.
	if err := binding.SetAgentsEnabled(context.Background(), false); !errors.Is(err, errAgentSetting) {
		t.Fatalf("mismatched echo = %v", err)
	}
	client.err = status.Error(codes.PermissionDenied, "no")
	if err := binding.SetAgentsEnabled(context.Background(), true); !errors.Is(err, errAgentSetting) {
		t.Fatalf("refused toggle = %v", err)
	}
	client.err = nil
	if id, err := binding.StartTask(context.Background(), " hi "); err != nil || id != "task-9" || client.gotStart.GetPrompt() != "hi" {
		t.Fatalf("start = %q %v %v", id, err, client.gotStart)
	}
	client.gotStart = nil
	if _, err := binding.StartTask(context.Background(), "   "); !errors.Is(err, errAgentPromptGone) || client.gotStart != nil {
		t.Fatalf("blank start = %v sent=%v", err, client.gotStart)
	}
	client.start = &agentv1.StartAgentTaskResponse{}
	if _, err := binding.StartTask(context.Background(), "hi"); !errors.Is(err, errAgentTask) {
		t.Fatalf("empty task id = %v", err)
	}
	if _, err := binding.ControlTask(context.Background(), "task-9", 4, agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE); !errors.Is(err, errAgentTask) {
		t.Fatalf("missing control response = %v", err)
	}
	client.control = &agentv1.ControlAgentTaskResponse{TaskId: "task-9", State: "paused", Version: 5}
	if id, err := binding.ControlTask(context.Background(), "task-9", 4, agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE); err != nil || id != "task-9" ||
		client.gotControl.GetTaskId() != "task-9" || client.gotControl.GetExpectedVersion() != 4 || client.gotControl.GetAction() != agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE || client.bearer != journeyclient.BearerScheme+"tok" {
		t.Fatalf("control = %q, %v request=%+v bearer=%q", id, err, client.gotControl, client.bearer)
	}
	client.gotControl = nil
	if _, err := binding.ControlTask(context.Background(), " ", 4, agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE); !errors.Is(err, errAgentTask) || client.gotControl != nil {
		t.Fatalf("invalid control request = %v sent=%v", err, client.gotControl)
	}
}

func TestTodo_UXBLIND_122_ControlMessageKey(t *testing.T) {
	for err, want := range map[error]string{
		nil: "done",
		status.Error(codes.Aborted, "revision changed"):           "conflict",
		status.Error(codes.PermissionDenied, "no"):                "denied",
		status.Error(codes.Unauthenticated, "no session"):         "denied",
		status.Error(codes.FailedPrecondition, "agents disabled"): "disabled",
		status.Error(codes.Unavailable, "store down"):             "failed",
		errors.New("task version conflict"):                       "conflict",
		errors.New("tenant agents disabled"):                      "disabled",
		errors.New("not authorized"):                              "denied",
		errors.New("unexpected failure"):                          "failed",
	} {
		if got := agentControlMessageKey(err); got != want {
			t.Errorf("agentControlMessageKey(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestTodo_UXBLIND_122_AgentStartMessageKey(t *testing.T) {
	for err, want := range map[error]string{
		errAgentPromptGone:                          "empty",
		status.Error(codes.InvalidArgument, "x"):    "empty",
		status.Error(codes.FailedPrecondition, "x"): "disabled",
		status.Error(codes.PermissionDenied, "x"):   "denied",
		status.Error(codes.Unauthenticated, "x"):    "denied",
		status.Error(codes.Internal, "x"):           "failed",
		status.Error(codes.Unavailable, "x"):        "failed",
		errors.New("plain"):                         "failed",
	} {
		if got := agentStartMessageKey(err); got != want {
			t.Fatalf("agentStartMessageKey(%v) = %q, want %q", err, got, want)
		}
	}
}
