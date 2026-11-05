package agents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// errAgent042Backend is a backend-shaped failure carrying a secret and a
// foreign-tenant marker. No projection may contain either.
var errAgent042Backend = errors.New("pq: tenant-z row 7 secret-token")

// parityTaskReader is an owner-scoped TaskReader fake that records the
// principal the transport resolved, so parity tests can prove the caller is
// derived server-side on both protocols.
type parityTaskReader struct {
	tasks     map[string]agentrun.AgentTask
	list      []agentrun.AgentTask
	err       error
	principal *trust.Principal
	calls     int
}

func (r *parityTaskReader) GetAgentTask(_ context.Context, p *trust.Principal, id string) (agentrun.AgentTask, error) {
	r.calls++
	r.principal = p
	if r.err != nil {
		return agentrun.AgentTask{}, r.err
	}
	task, ok := r.tasks[id]
	if !ok {
		return agentrun.AgentTask{}, agentrun.ErrNotFound
	}
	return task, nil
}

func (r *parityTaskReader) ListAgentTasks(_ context.Context, p *trust.Principal) ([]agentrun.AgentTask, error) {
	r.calls++
	r.principal = p
	if r.err != nil {
		return nil, r.err
	}
	return r.list, nil
}

// agent042Context is the admitted human caller both protocols resolve.
func agent042Context(t *testing.T) context.Context {
	t.Helper()
	return trust.WithPrincipal(context.Background(), principalOf(t, "tenant-a", trust.SubjectKindHuman))
}

// agent042Server mounts [NewHandler] on a real httptest server, admitting
// every request with the same trusted context a caller would already carry
// past transport.Admit. This isolates the Connect routing, decoding and
// response marshaling this todo owns from the shared admission stack, and
// mirrors the HUB-041 parity harness exactly.
func agent042Server(t *testing.T, deps Dependencies) (context.Context, *httptest.Server) {
	t.Helper()
	admitted := agent042Context(t)
	inner := NewHandler(deps)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(admitted))
	}))
	t.Cleanup(httpServer.Close)
	return admitted, httpServer
}

func agent042Task(id string, version uint64) agentrun.AgentTask {
	when := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	return agentrun.AgentTask{
		ID: id, TenantID: "tenant-a", UserID: "subject-1",
		Goal: "summarise my week", State: agentrun.StateRunning, Version: version,
		CreatedAt: when, UpdatedAt: when,
	}
} // TestTodo_AGENT_042 exercises the revision-bearing ControlAgentTask through
// both the native RPC method and the real mounted Connect route. A caller who
// supplies the current expected version must get the identical response and
// see the same owner call whichever protocol carried the request, and a stale
// revision must fail with the identical typed conflict on both paths instead
// of overwriting the task.
func TestTodo_AGENT_042(t *testing.T) {
	request := &agentv1.ControlAgentTaskRequest{
		TaskId: "task-1", ExpectedVersion: 8, Action: agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE,
	}
	controlled := agentclient.ControlledTask{ID: "task-1", State: "paused", Version: 9}

	nativeController := &fakeController{result: controlled}
	nativeResponse, err := NewServer(Dependencies{Controller: nativeController}).ControlAgentTask(agent042Context(t), request)
	if err != nil {
		t.Fatal(err)
	}

	httpController := &fakeController{result: controlled}
	_, httpServer := agent042Server(t, Dependencies{Controller: httpController})
	client := connect.NewClient[agentv1.ControlAgentTaskRequest, agentv1.ControlAgentTaskResponse](
		httpServer.Client(), httpServer.URL+agentv1.AgentService_ControlAgentTask_FullMethodName, connect.WithProtoJSON(),
	)
	httpResponse, err := client.CallUnary(context.Background(), connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeResponse, httpResponse.Msg) {
		t.Fatalf("HTTP response = %v, RPC response = %v", httpResponse.Msg, nativeResponse)
	}
	for name, controller := range map[string]*fakeController{"native": nativeController, "HTTP": httpController} {
		if controller.principal == nil || controller.principal.Subject() != "subject-1" || string(controller.principal.Tenant()) != "tenant-a" {
			t.Fatalf("%s caller not derived server-side: %+v", name, controller.principal)
		}
		if controller.request != (agentclient.TaskControl{TaskID: "task-1", ExpectedVersion: 8, Action: agentclient.TaskPause}) {
			t.Fatalf("%s forwarded %+v, want the exact revision-checked control", name, controller.request)
		}
	}

	// A stale expected version reaches the owner on both protocols and fails
	// with the same typed conflict; neither path overwrites the task.
	nativeController.err = agentrun.ErrConflict
	httpController.err = agentrun.ErrConflict
	_, nativeErr := NewServer(Dependencies{Controller: nativeController}).ControlAgentTask(agent042Context(t), request)
	if status.Code(nativeErr) != codes.Aborted {
		t.Fatalf("native stale revision = %v, want Aborted", nativeErr)
	}
	_, httpErr := client.CallUnary(context.Background(), connect.NewRequest(request))
	if connect.CodeOf(httpErr) != connect.CodeAborted {
		t.Fatalf("HTTP stale revision = %v, want Aborted", httpErr)
	}
	if nativeController.calls != 2 || httpController.calls != 2 {
		t.Fatalf("controller calls native=%d HTTP=%d, want one per protocol per attempt", nativeController.calls, httpController.calls)
	}
}

// TestTodo_AGENT_042_Conformance proves the HTTP projection carries exactly
// the canonical agent contract: every prompt and mode the RPC refuses is
// refused identically over HTTP with no raw-prompt-only path, valid starts
// and reads are proto-identical, and the mounted surface is exactly the five
// canonical procedures.
func TestTodo_AGENT_042_Conformance(t *testing.T) {
	t.Run("prompt and mode refusals match", func(t *testing.T) {
		overCap := strings.Repeat("a", MaxPromptBytes+1)
		cases := map[string]*agentv1.StartAgentTaskRequest{
			"empty":        {Prompt: ""},
			"blank":        {Prompt: " \n\t "},
			"over the cap": {Prompt: overCap},
			"unknown mode": {Prompt: "hello", Mode: agentv1.AgentStartMode(99)},
		}
		for name, request := range cases {
			t.Run(name, func(t *testing.T) {
				nativeStarter := &fakeStarter{}
				_, nativeErr := NewServer(Dependencies{Starter: nativeStarter}).StartAgentTask(agent042Context(t), request)
				if status.Code(nativeErr) != codes.InvalidArgument {
					t.Fatalf("native = %v, want InvalidArgument", nativeErr)
				}
				httpStarter := &fakeStarter{}
				_, httpServer := agent042Server(t, Dependencies{Starter: httpStarter})
				start := connect.NewClient[agentv1.StartAgentTaskRequest, agentv1.StartAgentTaskResponse](
					httpServer.Client(), httpServer.URL+agentv1.AgentService_StartAgentTask_FullMethodName, connect.WithProtoJSON(),
				)
				_, httpErr := start.CallUnary(context.Background(), connect.NewRequest(request))
				if connect.CodeOf(httpErr) != connect.CodeInvalidArgument {
					t.Fatalf("HTTP = %v, want InvalidArgument", httpErr)
				}
				if nativeStarter.calls != 0 || httpStarter.calls != 0 {
					t.Fatalf("a refused prompt reached the starter: native=%d HTTP=%d", nativeStarter.calls, httpStarter.calls)
				}
			})
		}
		t.Run("invalid utf8", func(t *testing.T) {
			nativeStarter := &fakeStarter{}
			_, nativeErr := NewServer(Dependencies{Starter: nativeStarter}).StartAgentTask(agent042Context(t), &agentv1.StartAgentTaskRequest{Prompt: "ok\xff"})
			if status.Code(nativeErr) != codes.InvalidArgument {
				t.Fatalf("native = %v, want InvalidArgument", nativeErr)
			}
			httpStarter := &fakeStarter{}
			_, httpServer := agent042Server(t, Dependencies{Starter: httpStarter})
			// Invalid UTF-8 is unrepresentable in proto JSON, so the typed
			// client refuses before sending; the starter never runs.
			start := connect.NewClient[agentv1.StartAgentTaskRequest, agentv1.StartAgentTaskResponse](
				httpServer.Client(), httpServer.URL+agentv1.AgentService_StartAgentTask_FullMethodName, connect.WithProtoJSON(),
			)
			if _, err := start.CallUnary(context.Background(), connect.NewRequest(&agentv1.StartAgentTaskRequest{Prompt: "ok\xff"})); err == nil {
				t.Fatal("an invalid-UTF-8 prompt was accepted over HTTP")
			}
			// A hand-crafted invalid body reaches the server and is refused
			// as InvalidArgument by strict decoding, exactly like native.
			resp, err := http.Post(httpServer.URL+agentv1.AgentService_StartAgentTask_FullMethodName, "application/json", strings.NewReader("{\"prompt\": \"ok\xff\"}"))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("HTTP raw invalid body = %d, want 400", resp.StatusCode)
			}
			if nativeStarter.calls != 0 || httpStarter.calls != 0 {
				t.Fatalf("a refused prompt reached the starter: native=%d HTTP=%d", nativeStarter.calls, httpStarter.calls)
			}
		})
	})

	t.Run("valid starts and reads match", func(t *testing.T) {
		request := &agentv1.StartAgentTaskRequest{Prompt: "  summarise my week  "}
		reader := func() *parityTaskReader {
			return &parityTaskReader{
				tasks: map[string]agentrun.AgentTask{"task-1": agent042Task("task-1", 3)},
				list:  []agentrun.AgentTask{agent042Task("task-1", 3), agent042Task("task-2", 5)},
			}
		}

		nativeStarter, nativeReader := &fakeStarter{}, reader()
		native, err := NewServer(Dependencies{Starter: nativeStarter, Tasks: nativeReader}).StartAgentTask(agent042Context(t), request)
		if err != nil {
			t.Fatal(err)
		}

		httpStarter, httpReader := &fakeStarter{}, reader()
		_, httpServer := agent042Server(t, Dependencies{Starter: httpStarter, Tasks: httpReader})
		start := connect.NewClient[agentv1.StartAgentTaskRequest, agentv1.StartAgentTaskResponse](
			httpServer.Client(), httpServer.URL+agentv1.AgentService_StartAgentTask_FullMethodName, connect.WithProtoJSON(),
		)
		http, err := start.CallUnary(context.Background(), connect.NewRequest(request))
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(native, http.Msg) {
			t.Fatalf("HTTP start = %v, RPC start = %v", http.Msg, native)
		}
		if nativeStarter.prompt != request.GetPrompt() || httpStarter.prompt != request.GetPrompt() {
			t.Fatalf("starter prompts native=%q HTTP=%q, want the exact request bytes %q",
				nativeStarter.prompt, httpStarter.prompt, request.GetPrompt())
		}

		// Neither transport dedups: a repeated identical start reaches the
		// owner on both paths, so HTTP adds no second request-id semantic.
		if _, err := NewServer(Dependencies{Starter: nativeStarter, Tasks: nativeReader}).StartAgentTask(agent042Context(t), request); err != nil {
			t.Fatal(err)
		}
		if _, err := start.CallUnary(context.Background(), connect.NewRequest(request)); err != nil {
			t.Fatal(err)
		}
		if nativeStarter.calls != 2 || httpStarter.calls != 2 {
			t.Fatalf("starter calls native=%d HTTP=%d, want one per protocol per attempt", nativeStarter.calls, httpStarter.calls)
		}

		getRequest := &agentv1.GetAgentTaskRequest{TaskId: "task-1"}
		nativeGet, err := NewServer(Dependencies{Tasks: nativeReader}).GetAgentTask(agent042Context(t), getRequest)
		if err != nil {
			t.Fatal(err)
		}
		get := connect.NewClient[agentv1.GetAgentTaskRequest, agentv1.GetAgentTaskResponse](
			httpServer.Client(), httpServer.URL+agentv1.AgentService_GetAgentTask_FullMethodName, connect.WithProtoJSON(),
		)
		httpGet, err := get.CallUnary(context.Background(), connect.NewRequest(getRequest))
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(nativeGet, httpGet.Msg) {
			t.Fatalf("HTTP get = %v, RPC get = %v", httpGet.Msg, nativeGet)
		}

		nativeList, err := NewServer(Dependencies{Tasks: nativeReader}).ListAgentTasks(agent042Context(t), &agentv1.ListAgentTasksRequest{})
		if err != nil {
			t.Fatal(err)
		}
		list := connect.NewClient[agentv1.ListAgentTasksRequest, agentv1.ListAgentTasksResponse](
			httpServer.Client(), httpServer.URL+agentv1.AgentService_ListAgentTasks_FullMethodName, connect.WithProtoJSON(),
		)
		httpList, err := list.CallUnary(context.Background(), connect.NewRequest(&agentv1.ListAgentTasksRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(nativeList, httpList.Msg) {
			t.Fatalf("HTTP list = %v, RPC list = %v", httpList.Msg, nativeList)
		}
		if len(httpList.Msg.GetTasks()) != 2 || httpList.Msg.GetTasks()[0].GetTaskId() != "task-1" {
			t.Fatalf("HTTP list order = %v, want the stable owner order", httpList.Msg.GetTasks())
		}
	})

	t.Run("oversized bodies are refused before the owner", func(t *testing.T) {
		httpStarter := &fakeStarter{}
		_, httpServer := agent042Server(t, Dependencies{Starter: httpStarter})
		resp, err := http.Post(httpServer.URL+agentv1.AgentService_StartAgentTask_FullMethodName, "application/json", strings.NewReader(`{"prompt": "`+strings.Repeat("a", MaxRequestBytes)+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge && resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("oversized body = %d, want a refusal status", resp.StatusCode)
		}
		if httpStarter.calls != 0 {
			t.Fatal("an oversized body reached the starter")
		}
	})

	t.Run("typed errors match", func(t *testing.T) {
		cases := []struct {
			name    string
			starter error
			native  codes.Code
			http    connect.Code
		}{
			{"disabled", agentclient.ErrDisabled, codes.FailedPrecondition, connect.CodeFailedPrecondition},
			{"invalid prompt", agentclient.ErrInvalidPrompt, codes.InvalidArgument, connect.CodeInvalidArgument},
			{"not authorized", agentclient.ErrNotAuthorized, codes.PermissionDenied, connect.CodePermissionDenied},
			{"deadline", context.DeadlineExceeded, codes.DeadlineExceeded, connect.CodeDeadlineExceeded},
			{"backend failure", errAgent042Backend, codes.Unavailable, connect.CodeUnavailable},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				request := &agentv1.StartAgentTaskRequest{Prompt: "hello"}
				_, nativeErr := NewServer(Dependencies{Starter: &fakeStarter{err: tc.starter}}).StartAgentTask(agent042Context(t), request)
				if status.Code(nativeErr) != tc.native {
					t.Fatalf("native = %v, want %v", nativeErr, tc.native)
				}
				_, httpServer := agent042Server(t, Dependencies{Starter: &fakeStarter{err: tc.starter}})
				start := connect.NewClient[agentv1.StartAgentTaskRequest, agentv1.StartAgentTaskResponse](
					httpServer.Client(), httpServer.URL+agentv1.AgentService_StartAgentTask_FullMethodName, connect.WithProtoJSON(),
				)
				_, httpErr := start.CallUnary(context.Background(), connect.NewRequest(request))
				if connect.CodeOf(httpErr) != tc.http {
					t.Fatalf("HTTP = %v, want %v", httpErr, tc.http)
				}
			})
		}
	})

	t.Run("surface is exactly the canonical procedures", func(t *testing.T) {
		methods := agentv1.File_hcmnext_agent_v1_agent_service_proto.Services().ByName("AgentService").Methods()
		var names []string
		for i := 0; i < methods.Len(); i++ {
			names = append(names, string(methods.Get(i).Name()))
		}
		want := []string{"SetAgentsEnabled", "StartAgentTask", "GetAgentTask", "ListAgentTasks", "ControlAgentTask"}
		if len(names) != len(want) {
			t.Fatalf("AgentService methods = %v, want %v", names, want)
		}
		for _, name := range want {
			found := false
			for _, got := range names {
				found = found || got == name
			}
			if !found {
				t.Fatalf("AgentService methods = %v, want %v", names, want)
			}
		}
		for _, procedure := range []string{
			agentv1.AgentService_SetAgentsEnabled_FullMethodName,
			agentv1.AgentService_StartAgentTask_FullMethodName,
			agentv1.AgentService_GetAgentTask_FullMethodName,
			agentv1.AgentService_ListAgentTasks_FullMethodName,
			agentv1.AgentService_ControlAgentTask_FullMethodName,
		} {
			if !strings.HasPrefix(procedure, ProcedurePrefix) {
				t.Fatalf("procedure %q is not under the versioned prefix %q", procedure, ProcedurePrefix)
			}
		}

		// Unrestricted provider or tool endpoints do not exist: anything
		// outside the canonical procedures is not mounted.
		handler := NewHandler(Dependencies{Starter: &fakeStarter{}})
		for _, procedure := range []string{"RunRawPrompt", "InvokeProvider", "CallTool", "StartAgentTaskExtra"} {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, ProcedurePrefix+procedure, strings.NewReader("{}")))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("POST %s = %d, want 404 for an unmounted procedure", procedure, recorder.Code)
			}
		}

		// No request message carries a caller-selected authority field on
		// either protocol; tenant and actor always come from the principal.
		messages := agentv1.File_hcmnext_agent_v1_agent_service_proto.Messages()
		for _, message := range []string{
			"SetAgentsEnabledRequest", "StartAgentTaskRequest", "GetAgentTaskRequest",
			"ListAgentTasksRequest", "ControlAgentTaskRequest",
		} {
			for _, name := range []string{"tenant_id", "tenant", "actor", "user_id", "subject", "principal", "scope"} {
				if messages.ByName(protoreflect.Name(message)).Fields().ByName(protoreflect.Name(name)) != nil {
					t.Fatalf("%s carries a %s field", message, name)
				}
			}
		}
	})
} // TestTodo_AGENT_042_Security proves the HTTP projection cannot be used to
// reach an outcome the native RPC methods refuse: unauthenticated and
// non-human callers are refused identically on both paths, authorization
// refusals carry the same typed code, and backend detail never leaks into
// either error.
func TestTodo_AGENT_042_Security(t *testing.T) {
	deps := func() Dependencies {
		return Dependencies{
			Settings:   &fakeSettings{},
			Admin:      allow(),
			Starter:    &fakeStarter{},
			Controller: &fakeController{result: agentclient.ControlledTask{ID: "task-1", State: "paused", Version: 9}},
			Tasks: &parityTaskReader{tasks: map[string]agentrun.AgentTask{
				"task-1": agent042Task("task-1", 9),
			}},
		}
	}
	methods := []struct {
		name   string
		native func(s *Server, ctx context.Context) error
		call   func(t *testing.T, server *httptest.Server) error
	}{
		{
			name: "SetAgentsEnabled",
			native: func(s *Server, ctx context.Context) error {
				_, err := s.SetAgentsEnabled(ctx, &agentv1.SetAgentsEnabledRequest{Enabled: true})
				return err
			},
			call: func(t *testing.T, server *httptest.Server) error {
				t.Helper()
				client := connect.NewClient[agentv1.SetAgentsEnabledRequest, agentv1.SetAgentsEnabledResponse](
					server.Client(), server.URL+agentv1.AgentService_SetAgentsEnabled_FullMethodName, connect.WithProtoJSON(),
				)
				_, err := client.CallUnary(context.Background(), connect.NewRequest(&agentv1.SetAgentsEnabledRequest{Enabled: true}))
				return err
			},
		},
		{
			name: "StartAgentTask",
			native: func(s *Server, ctx context.Context) error {
				_, err := s.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "hello"})
				return err
			},
			call: func(t *testing.T, server *httptest.Server) error {
				t.Helper()
				client := connect.NewClient[agentv1.StartAgentTaskRequest, agentv1.StartAgentTaskResponse](
					server.Client(), server.URL+agentv1.AgentService_StartAgentTask_FullMethodName, connect.WithProtoJSON(),
				)
				_, err := client.CallUnary(context.Background(), connect.NewRequest(&agentv1.StartAgentTaskRequest{Prompt: "hello"}))
				return err
			},
		},
		{
			name: "GetAgentTask",
			native: func(s *Server, ctx context.Context) error {
				_, err := s.GetAgentTask(ctx, &agentv1.GetAgentTaskRequest{TaskId: "task-1"})
				return err
			},
			call: func(t *testing.T, server *httptest.Server) error {
				t.Helper()
				client := connect.NewClient[agentv1.GetAgentTaskRequest, agentv1.GetAgentTaskResponse](
					server.Client(), server.URL+agentv1.AgentService_GetAgentTask_FullMethodName, connect.WithProtoJSON(),
				)
				_, err := client.CallUnary(context.Background(), connect.NewRequest(&agentv1.GetAgentTaskRequest{TaskId: "task-1"}))
				return err
			},
		},
		{
			name: "ControlAgentTask",
			native: func(s *Server, ctx context.Context) error {
				_, err := s.ControlAgentTask(ctx, &agentv1.ControlAgentTaskRequest{
					TaskId: "task-1", ExpectedVersion: 9, Action: agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE,
				})
				return err
			},
			call: func(t *testing.T, server *httptest.Server) error {
				t.Helper()
				client := connect.NewClient[agentv1.ControlAgentTaskRequest, agentv1.ControlAgentTaskResponse](
					server.Client(), server.URL+agentv1.AgentService_ControlAgentTask_FullMethodName, connect.WithProtoJSON(),
				)
				_, err := client.CallUnary(context.Background(), connect.NewRequest(&agentv1.ControlAgentTaskRequest{
					TaskId: "task-1", ExpectedVersion: 9, Action: agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE,
				}))
				return err
			},
		},
	}

	t.Run("unauthenticated and machine callers refused identically", func(t *testing.T) {
		for _, method := range methods {
			t.Run(method.name, func(t *testing.T) {
				d := deps()
				if err := method.native(NewServer(d), context.Background()); status.Code(err) != codes.Unauthenticated {
					t.Fatalf("native unauthenticated = %v, want Unauthenticated", err)
				}
				// No admitted context is injected here: the bare handler
				// sees the anonymous caller exactly as the native method does.
				bare := httptest.NewServer(NewHandler(d))
				t.Cleanup(bare.Close)
				if err := method.call(t, bare); connect.CodeOf(err) != connect.CodeUnauthenticated {
					t.Fatalf("HTTP unauthenticated = %v, want Unauthenticated", err)
				}
				for _, kind := range []trust.SubjectKind{trust.SubjectKindService, trust.SubjectKindAgent, trust.SubjectKindIntegration} {
					machine := trust.WithPrincipal(context.Background(), principalOf(t, "tenant-a", kind))
					if err := method.native(NewServer(d), machine); status.Code(err) != codes.PermissionDenied {
						t.Fatalf("native %s = %v, want PermissionDenied", kind, err)
					}
					wrapped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						NewHandler(d).ServeHTTP(w, r.WithContext(machine))
					}))
					t.Cleanup(wrapped.Close)
					if err := method.call(t, wrapped); connect.CodeOf(err) != connect.CodePermissionDenied {
						t.Fatalf("HTTP %s = %v, want PermissionDenied", kind, err)
					}
				}
				settings, starter, controller, tasks := d.Settings.(*fakeSettings), d.Starter.(*fakeStarter), d.Controller.(*fakeController), d.Tasks.(*parityTaskReader)
				if settings.writes != 0 || starter.calls != 0 || controller.calls != 0 || tasks.calls != 0 {
					t.Fatalf("refused callers reached a port: writes=%d starts=%d controls=%d reads=%d",
						settings.writes, starter.calls, controller.calls, tasks.calls)
				}
			})
		}
	})

	t.Run("authorization refusals match", func(t *testing.T) {
		denied := deps()
		denied.Admin = deny()
		if err := methods[0].native(NewServer(denied), agent042Context(t)); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("native denied toggle = %v, want PermissionDenied", err)
		}
		_, httpServer := agent042Server(t, denied)
		if err := methods[0].call(t, httpServer); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("HTTP denied toggle = %v, want PermissionDenied", err)
		}
		if denied.Settings.(*fakeSettings).writes != 0 {
			t.Fatal("a refused toggle reached the store")
		}

		for name, tc := range map[string]struct {
			method int
			setup  func(d Dependencies)
			native codes.Code
			http   connect.Code
		}{
			"start not authorized": {1, func(d Dependencies) { d.Starter.(*fakeStarter).err = agentclient.ErrNotAuthorized }, codes.PermissionDenied, connect.CodePermissionDenied},
			"control denied":       {3, func(d Dependencies) { d.Controller.(*fakeController).err = agentsystem.ErrDenied }, codes.PermissionDenied, connect.CodePermissionDenied},
		} {
			t.Run(name, func(t *testing.T) {
				d := deps()
				tc.setup(d)
				if err := methods[tc.method].native(NewServer(d), agent042Context(t)); status.Code(err) != tc.native {
					t.Fatalf("native = %v, want %v", err, tc.native)
				}
				_, server := agent042Server(t, d)
				if err := methods[tc.method].call(t, server); connect.CodeOf(err) != tc.http {
					t.Fatalf("HTTP = %v, want %v", err, tc.http)
				}
			})
		}
	})

	t.Run("backend detail never leaks", func(t *testing.T) {
		d := deps()
		d.Starter.(*fakeStarter).err = errAgent042Backend
		nativeErr := methods[1].native(NewServer(d), agent042Context(t))
		if status.Code(nativeErr) != codes.Unavailable || strings.Contains(nativeErr.Error(), "secret-token") || strings.Contains(nativeErr.Error(), "tenant-z") {
			t.Fatalf("native backend failure = %v", nativeErr)
		}
		_, server := agent042Server(t, d)
		httpErr := methods[1].call(t, server)
		if connect.CodeOf(httpErr) != connect.CodeUnavailable || strings.Contains(httpErr.Error(), "secret-token") || strings.Contains(httpErr.Error(), "tenant-z") {
			t.Fatalf("HTTP backend failure = %v", httpErr)
		}
	})
}
