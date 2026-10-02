package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chatbug049Script is a model fixture that answers each call, in order, with the
// reply it was given; a nil reply is a provider error. It records every request.
// No provider is called.
type chatbug049Script struct {
	replies  []*agentmodel.ModelResult
	requests []AgentModelExecutorRequest
}

func (m *chatbug049Script) Execute(_ context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	m.requests = append(m.requests, request)
	index := len(m.requests) - 1
	if index >= len(m.replies) || m.replies[index] == nil {
		return AgentModelExecutorResult{}, errors.New("provider unavailable")
	}
	reply := *m.replies[index]
	reply.ContractVersion = agentmodel.ContractVersion
	return AgentModelExecutorResult{Result: reply}, nil
}

func chatbug049Search() *agentmodel.ModelResult {
	return &agentmodel.ModelResult{Text: "I will search.", Finish: agentmodel.FinishToolCalls, ToolProposals: []agentmodel.ToolProposal{{ID: "search-1", Name: personaDocumentSearchTool, Arguments: []byte(`{"query":"policies"}`)}}}
}

func chatbug049Say(text string) *agentmodel.ModelResult {
	return &agentmodel.ModelResult{Text: text, Finish: agentmodel.FinishComplete}
}

// chatbug049Tools returns two documents for every search.
type chatbug049Tools struct{ agentUXSpeedTools }

func (chatbug049Tools) Execute(context.Context, agentrun.Record, runstate.Run, agentmodel.ToolProposal) ([]byte, string, string, error) {
	result := []byte(`{"Hits":[{"DocumentID":"pto","VersionID":"one","Title":"Paid time off policy","Version":1,"SectionAnchor":"carryover","Markdown":"## Carryover\n40 hours."},{"DocumentID":"holidays","VersionID":"two","Title":"Holiday guide","Version":1,"Markdown":"Ten paid holidays."}]}`)
	return result, "chatbug049-tool-result", personaRunBytesDigest(result), nil
}

// chatbug049Run admits one mention on the real admission and run stores and
// executes it with the scripted model. It returns the run, the model, and the
// words of the answer that was sealed for delivery ("" when none was).
func chatbug049Run(t *testing.T, replies ...*agentmodel.ModelResult) (runstate.Run, *chatbug049Script, string, error) {
	t.Helper()
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantID)
	store, err := agentstore.New(ctx, agentstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema), CoreDSN: "postgres://unused@127.0.0.1:1/unused", MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	mapper := func(tenant string) uuid.UUID {
		if tenant == "tenant-a" {
			return tenantID
		}
		return uuid.Nil
	}
	admissions, err := agentrunstore.NewAdmissionRepository(store, tenantID, values.TenantId("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	request := foregroundAuthorityRequest(now)
	request.Source.Key, request.Source.Ref, request.CauseID = "invocation-a", "post-a", "invocation-a"
	request.Persona = &agentrun.PersonaRef{ID: "persona-a", Version: "7", Digest: "sha256:" + strings.Repeat("a", 64)}
	request.Agent = agentrun.VersionRef{AgentID: "agent-a", Version: "4", Digest: "sha256:" + strings.Repeat("d", 64)}
	request.Principal.InvokerID, request.Purpose = "alice", "persona-chat"
	request.Audience.ID, request.Context.ID = "room-a", "thread-a"
	admissionService, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: personaChatAdmissionAuthorityFake{}, Store: admissions, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	admission, _, err := admissionService.Admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	runStores, err := agentrunstate.New(store, mapper)
	if err != nil {
		t.Fatal(err)
	}
	runStore, err := runStores.ForTenant("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	state, err := runstate.New(runStore, personaChatAdmissionRecheckerFake{})
	if err != nil {
		t.Fatal(err)
	}
	validator, _, _, persister, _ := personaRunOutputFixture(t)
	model := &chatbug049Script{replies: replies}
	executor := &personaAdmittedRunExecutor{state: state, store: runStore, model: model, work: agentux076Work{t: t}, tools: chatbug049Tools{}, output: validator, reply: agentUXSpeedReply{}, workerID: "chatbug049-worker", leaseTTL: time.Minute, now: func() time.Time { return now }}
	principal := foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), request.Purpose, "alice", "tenant-a")
	run, runErr := executor.Start(trust.WithPrincipal(ctx, principal), admission)
	sealed := ""
	if persister.calls > 0 {
		if _, answer, payloadErr := persister.projection.Payload(); payloadErr == nil && len(answer.Parts) > 0 {
			sealed = answer.Parts[0].Text
		}
	}
	return run, model, sealed, runErr
}

// Asked for a list, the agent first answers with a document's title. The model
// is asked once more, in the same run, to answer from the documents' content;
// its list is what is delivered.
func TestTodo_CHATBUG_049_Regenerated(t *testing.T) {
	list := "I can read two policies here: Paid time off policy (40 hours carry over) and Holiday guide (ten paid holidays)."
	run, model, sealed, err := chatbug049Run(t, chatbug049Search(), chatbug049Say("Paid time off policy"), chatbug049Say(list))
	if err != nil || run.State != runstate.StateCompleted || len(model.requests) != 3 {
		t.Fatalf("run=%s calls=%d err=%v", run.State, len(model.requests), err)
	}
	if sealed != list {
		t.Fatalf("delivered %q, want the regenerated list", sealed)
	}
	second, third := model.requests[1], model.requests[2]
	if third.StepID != second.StepID+personaRegenerationSuffix || third.Route.TraceID != third.StepID || third.Model.TraceID != third.StepID {
		t.Fatalf("the third call is not its own admitted step: second=%s third=%s", second.StepID, third.StepID)
	}
	last := third.Model.Messages[len(third.Model.Messages)-1]
	if last.Role != agentmodel.RoleDeveloper || last.Content != personaRegenerationInstruction || len(third.Model.Messages) != len(second.Model.Messages)+1 {
		t.Fatalf("the instruction was not added to the same conversation: %+v", third.Model.Messages)
	}
	if len(third.Outbound.Fields) != len(second.Outbound.Fields)+1 || len(third.Outbound.DeclaredFields) != len(second.Outbound.DeclaredFields)+1 {
		t.Fatalf("the instruction was not declared on the outbound request: %d vs %d fields", len(third.Outbound.Fields), len(second.Outbound.Fields))
	}
	// The request the first answer was made from is untouched by the second.
	if len(second.Model.Messages) != 5 || strings.Contains(second.Model.Messages[len(second.Model.Messages)-1].Content, "only named a document") {
		t.Fatalf("regeneration changed the request it was made from: %+v", second.Model.Messages)
	}
}

// A good first reply costs no extra call.
func TestTodo_CHATBUG_049_Regenerated_NoExtraCall(t *testing.T) {
	good := "I can read two policies here: Paid time off policy and Holiday guide."
	run, model, sealed, err := chatbug049Run(t, chatbug049Search(), chatbug049Say(good))
	if err != nil || run.State != runstate.StateCompleted || len(model.requests) != 2 || sealed != good {
		t.Fatalf("run=%s calls=%d sealed=%q err=%v", run.State, len(model.requests), sealed, err)
	}
}

// When the second reply says nothing either, or the second call fails, the
// server's own sentence is delivered, as before.
func TestTodo_CHATBUG_049_Regenerated_FallsBack(t *testing.T) {
	want := "I can read 2 documents here: Paid time off policy, Holiday guide."
	for name, second := range map[string]*agentmodel.ModelResult{
		"title again":    chatbug049Say("Holiday guide"),
		"provider error": nil,
	} {
		run, model, sealed, err := chatbug049Run(t, chatbug049Search(), chatbug049Say("Paid time off policy"), second)
		if err != nil || run.State != runstate.StateCompleted || len(model.requests) != 3 || sealed != want {
			t.Fatalf("%s: run=%s calls=%d sealed=%q err=%v", name, run.State, len(model.requests), sealed, err)
		}
	}
}
