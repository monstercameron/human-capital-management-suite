package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// AGENTUX-076 golden suite: twenty everyday questions put to the Assistant's run
// with a scripted model that follows the tool protocol (it reads the context it is
// given, may propose one tool call, and answers from what it read). For each the
// test asserts what the model was given (the messages, the facts block, the
// readable-document list, the tools) and the kind of outcome: an answer, an answer
// marked "Not from your documents", a clarifying question, or a real failure.

type agentux076Outcome int

const (
	agentux076Answer agentux076Outcome = iota
	agentux076Marked
	agentux076Clarifying
	agentux076Failure
)

func (o agentux076Outcome) String() string {
	return [...]string{"answer", "marked answer", "clarifying question", "real failure"}[o]
}

// agentux076Seen is what the scripted model read in the request it is answering.
type agentux076Seen struct {
	question    string
	history     []string
	facts       string
	listing     personaFactsListing
	toolResult  string
	toolsOffer  []string
	instruction string
}

func (s agentux076Seen) titles() []string {
	var out []string
	for _, document := range append(append([]PersonaReadableDocument(nil), s.listing.PlacedHere...), s.listing.Workspace...) {
		out = append(out, document.Title)
	}
	return out
}

func agentux076Read(request AgentModelExecutorRequest) agentux076Seen {
	var seen agentux076Seen
	for _, tool := range request.Model.Tools {
		seen.toolsOffer = append(seen.toolsOffer, tool.Name)
	}
	messages := request.Model.Messages
	seen.question = messages[0].Content
	for i, message := range messages {
		switch {
		case strings.HasPrefix(message.Content, "Facts about you"):
			seen.facts = message.Content
		case strings.HasPrefix(message.Content, personaFactsTitlesBegin):
			body := strings.TrimSuffix(strings.TrimPrefix(message.Content, personaFactsTitlesBegin+"\n"), "\n"+personaFactsTitlesEnd)
			_ = json.Unmarshal([]byte(body), &seen.listing)
		case message.Role == agentmodel.RoleTool:
			seen.toolResult = message.Content
		case message.Role == agentmodel.RoleDeveloper && i == len(messages)-1:
			seen.instruction = message.Content
		}
	}
	// The question is the last user message that is not the list or a tool call.
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == agentmodel.RoleUser && !strings.HasPrefix(messages[i].Content, personaFactsTitlesBegin) {
			seen.question = messages[i].Content
			break
		}
	}
	for _, message := range messages[2:] {
		if message.Role == agentmodel.RoleUser && !strings.HasPrefix(message.Content, personaFactsTitlesBegin) && message.Content != seen.question {
			seen.history = append(seen.history, message.Content)
		}
	}
	return seen
}

// agentux076Case is one everyday question and what the scripted model does with it.
type agentux076Case struct {
	group    string
	question string
	history  []string
	// tool, args and output: the one tool call the model makes before it
	// answers, and what that tool returns. tool "" answers from the context.
	tool, args, output string
	// grounded are the documents the tool's hits stand for; the sealed answer may cite them.
	grounded []agentux076Doc
	answer   func(agentux076Seen) string
	outcome  agentux076Outcome
	// failure is the code of a real failure.
	failure string
	// noResults is true when the continuation must carry the server's instruction.
	noResults bool
}

type agentux076Doc struct{ id, version, title, markdown string }

func (d agentux076Doc) hit() personaQualitySearchedDocument {
	return personaQualitySearchedDocument{DocumentID: d.id, VersionID: d.version, Title: d.title, Markdown: d.markdown, Version: 1}
}

func agentux076Hits(docs ...agentux076Doc) string {
	hits := make([]personaQualitySearchedDocument, 0, len(docs))
	for _, d := range docs {
		hits = append(hits, d.hit())
	}
	encoded, _ := json.Marshal(struct {
		Hits  []personaQualitySearchedDocument
		Total int
	}{hits, len(hits)})
	return string(encoded)
}

var (
	agentux076PTO      = agentux076Doc{"doc-pto", "v-pto", "Paid time off policy", "# Paid time off policy\n\nEmployees receive twenty days a year. Up to 40 hours carry over. Half days may be taken with approval.\n"}
	agentux076Holidays = agentux076Doc{"doc-holidays", "v-holidays", "Company holidays 2026", "# Company holidays 2026\n\nThe remaining holidays are Thanksgiving on 26 November and 25 December.\n"}
	agentux076Conduct  = agentux076Doc{"doc-conduct", "v-conduct", "Code of conduct", "# Code of conduct\n\nTreat colleagues with respect.\n"}
	// The Assistant reads two documents placed in this conversation and one in the workspace.
	agentux076Reach = PersonaReadableDocuments{
		Placed:    []PersonaReadableDocument{{DocumentID: "doc-pto", Title: "Paid time off policy"}, {DocumentID: "doc-holidays", Title: "Company holidays 2026"}},
		Workspace: []PersonaReadableDocument{{DocumentID: "doc-conduct", Title: "Code of conduct"}},
	}
)

func agentux076Say(text string) func(agentux076Seen) string {
	return func(agentux076Seen) string { return text }
}

func agentux076Cases() []agentux076Case {
	listTitles := func(prefix string) func(agentux076Seen) string {
		return func(s agentux076Seen) string { return prefix + strings.Join(s.titles(), ", ") + "." }
	}
	return []agentux076Case{
		// About itself (4): answered from the facts and the list, with no tool.
		{group: "about itself", question: "Which documents can you read in this conversation?", answer: listTitles("I can read these documents here: ")},
		{group: "about itself", question: "What can you help me with here?", answer: func(s agentux076Seen) string {
			return "I answer short questions, and I can search the " + fmt.Sprint(len(s.titles())) + " documents I can read here, quoting their sources."
		}},
		{group: "about itself", question: "Who looks after you?", answer: func(s agentux076Seen) string {
			if !strings.Contains(s.facts, "Looked after by: Pat") {
				return "I do not know."
			}
			return "Pat looks after me."
		}},
		{group: "about itself", question: "What will you not do?", answer: agentux076Say("I will not act beyond your own access, and I will not change governed records or write to external systems.")},

		// Lists (3).
		{group: "lists", question: "Which policies can you read here?", tool: personaListDocumentsTool, args: `{}`, answer: func(s agentux076Seen) string {
			if !strings.Contains(s.toolResult, "Paid time off policy") {
				return "I could not list them."
			}
			return "I can read: " + strings.Join(s.titles(), ", ") + "."
		}},
		{group: "lists", question: "Rank the top policies in the company.", tool: personaWorkspaceSearchTool, args: `{"query":"policy"}`, output: agentux076Hits(agentux076PTO, agentux076Conduct), grounded: []agentux076Doc{agentux076PTO, agentux076Conduct},
			answer: agentux076Say("1. Paid time off policy [[1]]\n2. Code of conduct [[2]]")},
		{group: "lists", question: "List every policy in the company, top 5.", tool: personaWorkspaceSearchTool, args: `{"query":"policies"}`, output: `{"Hits":[],"Unavailable":"Assistant cannot search workspace documents right now."}`,
			outcome: agentux076Failure, failure: "CONTEXT_UNAVAILABLE"},

		// A fact in a placed document (3). These are the example questions that
		// name a document's subject.
		{group: "fact in a document", question: "Which company holidays are still to come this year?", tool: personaDocumentSearchTool, args: `{"query":"company holidays"}`, output: agentux076Hits(agentux076Holidays), grounded: []agentux076Doc{agentux076Holidays},
			answer: agentux076Say("Thanksgiving on 26 November and 25 December are still to come. [[1]]")},
		{group: "fact in a document", question: "What does our time off policy say about carrying hours over?", tool: personaDocumentSearchTool, args: `{"query":"carry over hours time off"}`, output: agentux076Hits(agentux076PTO), grounded: []agentux076Doc{agentux076PTO},
			answer: agentux076Say("Up to 40 hours carry over. [[1]]")},
		{group: "fact in a document", question: "What should I know before I take time off?", tool: personaDocumentSearchTool, args: `{"query":"before taking time off"}`, output: agentux076Hits(agentux076PTO), grounded: []agentux076Doc{agentux076PTO},
			answer: agentux076Say("You receive twenty days a year, and half days need approval. [[1]]")},

		// A fact in no document (3).
		{group: "fact in no document", question: "What is the capital of France?", outcome: agentux076Marked,
			answer: agentux076Say("Paris is the capital of France.\n" + personaNotFromDocumentsMarker)},
		{group: "fact in no document", question: "How many people work in the Berlin office?", tool: personaDocumentSearchTool, args: `{"query":"Berlin office headcount"}`, output: `{"Hits":[],"Total":0}`, noResults: true, outcome: agentux076Clarifying,
			answer: agentux076Say("I found nothing about the Berlin office. The closest document I can read is Code of conduct. Which office policy do you mean?")},
		{group: "fact in no document", question: "What is our parental leave bonus?", tool: personaDocumentSearchTool, args: `{"query":"parental leave bonus"}`, output: `{"Hits":[],"Total":0}`, noResults: true,
			answer: agentux076Say("I found nothing about a parental leave bonus. The closest documents are Paid time off policy; try asking about leave amounts.")},

		// Follow-ups (2): the earlier question is part of what the model is given.
		{group: "follow-up", question: "And for part-time staff?", history: []string{"What does our time off policy say about carrying hours over?"}, tool: personaDocumentSearchTool, args: `{"query":"part-time carry over"}`, output: agentux076Hits(agentux076PTO), grounded: []agentux076Doc{agentux076PTO},
			answer: func(s agentux076Seen) string {
				if len(s.history) != 1 {
					return "I do not know what you mean."
				}
				return "The policy gives part-time staff the same twenty days, in proportion. [[1]]"
			}},
		{group: "follow-up", question: "Which of those is the newest?", history: []string{"Which documents can you read in this conversation?"}, outcome: agentux076Clarifying,
			answer: func(s agentux076Seen) string {
				if len(s.history) != 1 {
					return "Which ones?"
				}
				return "My list has titles only, with no dates. Would you like me to open one of the " + fmt.Sprint(len(s.titles())) + " documents?"
			}},

		// A greeting, thanks, and requests outside its role.
		{group: "greeting", question: "Hi", answer: agentux076Say("Hello! I am glad to help.")},
		{group: "thanks", question: "Thanks!", answer: agentux076Say("You are welcome.")},
		{group: "outside its role", question: "Write me a short poem about the sea.", outcome: agentux076Marked,
			answer: agentux076Say("The sea keeps time with the moon.\n" + personaNotFromDocumentsMarker)},
		{group: "outside its role", question: "Please approve my expense report.", answer: agentux076Say("I cannot approve anything. I only read documents and answer questions.")},

		// A question in German: answered from the list, in German.
		{group: "German", question: "Welche Dokumente können Sie in dieser Unterhaltung lesen?", answer: func(s agentux076Seen) string {
			if !strings.Contains(s.facts, "in the language of the question") {
				return "I can read: " + strings.Join(s.titles(), ", ")
			}
			return "Ich kann diese Dokumente lesen: " + strings.Join(s.titles(), ", ") + "."
		}},
	}
}

// agentux076Scripted is the model: it reads what it is given and, when the case
// calls for a tool, proposes it on the first turn and answers on the second.
type agentux076Scripted struct {
	c        agentux076Case
	requests []AgentModelExecutorRequest
}

func (m *agentux076Scripted) Execute(_ context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	m.requests = append(m.requests, request)
	if m.c.tool != "" && len(m.requests) == 1 {
		return AgentModelExecutorResult{Result: agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Finish: agentmodel.FinishToolCalls, ToolProposals: []agentmodel.ToolProposal{{ID: "call-1", Name: m.c.tool, Arguments: []byte(m.c.args)}}}}, nil
	}
	text := m.c.answer(agentux076Read(request))
	return AgentModelExecutorResult{Result: agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Text: text, Finish: agentmodel.FinishComplete}}, nil
}

// agentux076GoldenTools offers the Assistant's three tools and returns the case's output.
type agentux076GoldenTools struct {
	c        agentux076Case
	listing  []byte
	executed []string
}

func (agentux076GoldenTools) ToolSchemas(context.Context, agentrun.Record, runstate.Run) ([]agentmodel.ToolSchema, error) {
	return []agentmodel.ToolSchema{
		{Name: personaDocumentSearchTool, Description: "Search.", InputSchema: []byte(`{"type":"object"}`)},
		{Name: personaWorkspaceSearchTool, Description: "Search the workspace.", InputSchema: []byte(`{"type":"object"}`)},
		listDocumentsToolSchema(),
	}, nil
}

func (t *agentux076GoldenTools) Execute(_ context.Context, _ agentrun.Record, _ runstate.Run, proposal agentmodel.ToolProposal) ([]byte, string, string, error) {
	t.executed = append(t.executed, proposal.Name)
	result := []byte(t.c.output)
	if proposal.Name == personaListDocumentsTool {
		result = t.listing
	}
	return result, "golden-result", personaRunBytesDigest(result), nil
}

// agentux076GoldenWork builds the request a served run builds: system, developer,
// the person's earlier questions, then the facts block, then the question.
type agentux076GoldenWork struct {
	t     *testing.T
	c     agentux076Case
	facts personaAgentFacts
}

func (w agentux076GoldenWork) BuildPersonaRunModelWork(ctx context.Context, admission agentrun.Record, run runstate.Run) (PersonaRunModelWork, error) {
	work, err := agentUXSpeedWork{t: w.t}.BuildPersonaRunModelWork(ctx, admission, run)
	if err != nil {
		return work, err
	}
	req := &work.Request
	class := trustdlp.ClassInternal
	provenance := []string{"persona-run:" + run.ID}
	messages := []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem, Content: "Answers everyday questions."}, {Role: agentmodel.RoleDeveloper, Content: "Answer in plain, short language."}}
	for _, earlier := range w.c.history {
		messages = append(messages, agentmodel.ModelMessage{Role: agentmodel.RoleUser, Content: earlier})
	}
	messages = append(messages, agentmodel.ModelMessage{Role: agentmodel.RoleUser, Content: w.c.question})
	req.Model.Messages = messages
	req.Outbound.Fields, req.Outbound.DeclaredFields, req.FieldSources = nil, nil, map[string]string{}
	for i, message := range messages {
		name := fmt.Sprintf("model.message.%d", i)
		source, taint := "persona-profile", []string{"PERSONA_PROFILE"}
		if message.Role == agentmodel.RoleUser {
			source, taint = "persona-invoking-post", []string{"PERSONA_INVOKING_POST"}
		}
		req.Outbound.Fields = append(req.Outbound.Fields, agentegress.Field{Name: name, Value: message.Content, Class: class, Taint: taint, Provenance: provenance})
		req.Outbound.DeclaredFields = append(req.Outbound.DeclaredFields, name)
		req.FieldSources[name] = source
	}
	if err := insertPersonaAgentFacts(req, w.facts, PersonaRunModelRoute{ProfileClass: class, InvokerClass: class, ThreadClass: class}); err != nil {
		return work, err
	}
	work.Facts, work.HasFacts, work.General = w.facts, true, true
	return work, nil
}

// agentux076GoldenReply captures what would be posted.
type agentux076GoldenReply struct{ request *PersonaReplyDeliveryRequest }

func (r agentux076GoldenReply) Deliver(_ context.Context, request PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error) {
	*r.request = request
	return PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: "golden-reply"}, nil
}

// agentux076Env is one real run store the whole suite shares; every question is
// its own admission.
type agentux076Env struct {
	store    *agentstore.Store
	tenantID uuid.UUID
	runs     runstate.Store
	state    *runstate.Service
	now      time.Time
}

func agentux076NewEnv(t *testing.T) *agentux076Env {
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
	return &agentux076Env{store: store, tenantID: tenantID, runs: runStore, state: state, now: time.Now().UTC().Truncate(time.Microsecond)}
}

type agentux076Result struct {
	run      runstate.Run
	err      error
	model    *agentux076Scripted
	tools    *agentux076GoldenTools
	delivery PersonaReplyDeliveryRequest
	text     string
	sealed   bool
}

func (env *agentux076Env) ask(t *testing.T, key string, c agentux076Case, facts personaAgentFacts) agentux076Result {
	t.Helper()
	ctx := context.Background()
	admissions, err := agentrunstore.NewAdmissionRepository(env.store, env.tenantID, values.TenantId("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	request := foregroundAuthorityRequest(env.now)
	request.Source.Key, request.Source.Ref, request.CauseID = key, "post-"+key, key
	request.Persona = &agentrun.PersonaRef{ID: "persona-a", Version: "7", Digest: "sha256:" + strings.Repeat("a", 64)}
	request.Agent = agentrun.VersionRef{AgentID: "agent-a", Version: "4", Digest: "sha256:" + strings.Repeat("d", 64)}
	request.Principal.InvokerID, request.Purpose = "alice", "persona-chat"
	request.Audience.ID, request.Context.ID = "room-a", "thread-a"
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: personaChatAdmissionAuthorityFake{}, Store: admissions, Now: func() time.Time { return env.now }})
	if err != nil {
		t.Fatal(err)
	}
	admission, _, err := service.Admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	validator, _, _, _, source := personaRunOutputFixture(t)
	for _, doc := range c.grounded {
		id := "document:" + doc.id + "/version:" + doc.version
		datum, err := source.authority.Gateway.Observe(agentsecurity.SourceDocument, doc.markdown, agentsecurity.KindObservation, agentsecurity.Citation{SourceID: id, Location: id, Digest: personaRunBytesDigest([]byte(doc.markdown))})
		if err != nil {
			t.Fatal(err)
		}
		source.authority.Grounding = append(source.authority.Grounding, datum)
	}
	listing, err := facts.personaListDocumentsResult()
	if err != nil {
		t.Fatal(err)
	}
	result := agentux076Result{model: &agentux076Scripted{c: c}, tools: &agentux076GoldenTools{c: c, listing: listing}}
	executor := &personaAdmittedRunExecutor{state: env.state, store: env.runs, model: result.model, work: agentux076GoldenWork{t: t, c: c, facts: facts}, tools: result.tools, output: validator, reply: agentux076GoldenReply{request: &result.delivery}, workerID: "w", leaseTTL: time.Minute, now: func() time.Time { return env.now }}
	principal := foregroundPrincipal(t, env.now.Add(-time.Minute), env.now.Add(time.Hour), request.Purpose, "alice", "tenant-a")
	result.run, result.err = executor.Start(trust.WithPrincipal(ctx, principal), admission)
	if result.delivery.IdempotencyKey != "" {
		result.sealed = true
		if draft, answer, perr := result.delivery.Output.Payload(); perr == nil {
			result.text = strings.TrimSpace(draft.Narrative)
			if len(answer.Parts) > 0 && result.text == "" {
				result.text = answer.Parts[0].Text
			}
		}
	}
	return result
}

// agentux076Facts is the Assistant's facts and reach, as the server writes them.
func agentux076Facts() personaAgentFacts {
	profile := agentux076AssistantProfile()
	placed, workspace := personaDocumentReach(profile.SkillPins)
	facts := personaAgentFacts{Reads: placed || workspace, General: true, Documents: agentux076Reach}
	facts.Documents.PlacedTotal, facts.Documents.WorkspaceTotal = len(facts.Documents.Placed), len(facts.Documents.Workspace)
	facts.Text = personaAgentFactsText(profile, placed, workspace, true)
	return facts
}

func TestTodo_AGENTUX_076_Golden(t *testing.T) {
	cases := agentux076Cases()
	if len(cases) != 20 {
		t.Fatalf("the golden suite holds %d questions, want twenty", len(cases))
	}
	groups := map[string]int{}
	asked := map[string]bool{}
	for _, c := range cases {
		groups[c.group]++
		asked[c.question] = true
	}
	for group, want := range map[string]int{"about itself": 4, "lists": 3, "fact in a document": 3, "fact in no document": 3, "follow-up": 2, "greeting": 1, "thanks": 1, "outside its role": 2, "German": 1} {
		if groups[group] != want {
			t.Fatalf("%d questions in %q, want %d", groups[group], group, want)
		}
	}
	// Every example question the empty conversation with an agent offers is asked, and answered.
	for _, question := range chatui.AgentExampleQuestions("en-US") {
		if !asked[question] {
			t.Errorf("the empty conversation suggests %q, and the golden suite does not ask it", question)
		}
	}

	env := agentux076NewEnv(t)
	facts := agentux076Facts()
	toolNames := []string{personaDocumentSearchTool, personaWorkspaceSearchTool, personaListDocumentsTool}
	for i, c := range cases {
		c := c
		t.Run(fmt.Sprintf("%02d %s: %s", i+1, c.group, c.question), func(t *testing.T) {
			r := env.ask(t, fmt.Sprintf("golden-%02d", i+1), c, facts)

			// What the model was given on its first turn.
			if len(r.model.requests) == 0 {
				t.Fatalf("the model was never asked: %v", r.err)
			}
			first := r.model.requests[0]
			messages := first.Model.Messages
			wantRoles := []agentmodel.MessageRole{agentmodel.RoleSystem, agentmodel.RoleDeveloper}
			for range c.history {
				wantRoles = append(wantRoles, agentmodel.RoleUser)
			}
			wantRoles = append(wantRoles, agentmodel.RoleDeveloper, agentmodel.RoleDeveloper, agentmodel.RoleUser, agentmodel.RoleUser)
			if len(messages) != len(wantRoles) {
				t.Fatalf("the model was given %d messages, want %d", len(messages), len(wantRoles))
			}
			for i, role := range wantRoles {
				if messages[i].Role != role {
					t.Fatalf("message %d has role %s, want %s", i, messages[i].Role, role)
				}
			}
			factsAt := 2 + len(c.history)
			for i, earlier := range c.history {
				if messages[2+i].Content != earlier {
					t.Fatalf("history %d = %q, want %q", i, messages[2+i].Content, earlier)
				}
			}
			if messages[len(messages)-1].Content != c.question {
				t.Fatalf("the question reached the model as %q", messages[len(messages)-1].Content)
			}
			if messages[factsAt].Content != facts.Text || messages[factsAt+1].Content != personaFactsContainment {
				t.Fatal("the facts or the containment line are not the server's own")
			}
			for _, want := range []string{"Your name: Assistant", "Looked after by: Pat", personaListDocumentsTool, "never call a search tool", "language of the question", personaNotFromDocumentsMarker, "not an error"} {
				if !strings.Contains(messages[factsAt].Content, want) {
					t.Fatalf("the facts do not say %q", want)
				}
			}
			seen := agentux076Read(first)
			if got := strings.Join(seen.toolsOffer, ","); got != strings.Join(toolNames, ",") {
				t.Fatalf("the model was offered tools %s, want %s", got, strings.Join(toolNames, ","))
			}
			if len(seen.listing.PlacedHere) != 2 || seen.listing.PlacedHere[0].Title != "Paid time off policy" || len(seen.listing.Workspace) != 1 || seen.listing.Workspace[0].Title != "Code of conduct" || seen.listing.PlacedHereTotal != 2 || seen.listing.WorkspaceTotal != 1 || !seen.listing.ReadsDocuments || seen.listing.Unavailable {
				t.Fatalf("the model was given the list %+v", seen.listing)
			}
			if first.FieldSources[fmt.Sprintf("model.message.%d", factsAt+2)] != "persona-untrusted-reference-document" || first.FieldSources[fmt.Sprintf("model.message.%d", len(messages)-1)] != "persona-invoking-post" {
				t.Fatalf("the list is not untrusted reference data or the question lost its source: %v", first.FieldSources)
			}

			// The tool protocol: one tool call at most, never offered again.
			wantCalls := 0
			if c.tool != "" {
				wantCalls = 1
			}
			if len(r.tools.executed) != wantCalls || (wantCalls == 1 && r.tools.executed[0] != c.tool) {
				t.Fatalf("tools executed = %v, want %d of %q", r.tools.executed, wantCalls, c.tool)
			}
			switch {
			case c.tool == "":
				if len(r.model.requests) != 1 {
					t.Fatalf("model turns = %d, want 1", len(r.model.requests))
				}
			case c.outcome == agentux076Failure:
				if len(r.model.requests) != 1 {
					t.Fatalf("a search that could not run was followed by %d model turns", len(r.model.requests))
				}
			default:
				if len(r.model.requests) != 2 {
					t.Fatalf("model turns = %d, want 2", len(r.model.requests))
				}
				second := r.model.requests[1]
				if len(second.Model.Tools) != 0 {
					t.Fatal("the answering turn was offered tools again")
				}
				messages := second.Model.Messages
				result := c.output
				if c.tool == personaListDocumentsTool {
					result = string(r.tools.listing)
				}
				if c.noResults {
					if last := messages[len(messages)-1]; last.Role != agentmodel.RoleDeveloper || last.Content != personaNoResultsInstruction {
						t.Fatalf("the model was not told the search found nothing: %+v", last)
					}
					messages = messages[:len(messages)-1]
				}
				if last := messages[len(messages)-1]; last.Role != agentmodel.RoleTool || last.Content != result {
					t.Fatalf("the model was not given the tool's result: %+v", last)
				}
			}

			// The kind of outcome.
			if c.outcome == agentux076Failure {
				var failure *PersonaRunFailure
				if !errors.As(r.err, &failure) || failure.Code != c.failure || r.run.State != runstate.StateFailed {
					t.Fatalf("outcome = %s %v, want a failure %s", r.run.State, r.err, c.failure)
				}
				if r.sealed {
					t.Fatal("a failed run delivered an answer")
				}
				return
			}
			if r.err != nil || r.run.State != runstate.StateCompleted || !r.sealed || strings.TrimSpace(r.text) == "" {
				t.Fatalf("the run ended %s with %v and no answer", r.run.State, r.err)
			}
			if strings.Contains(r.text, personaNotFromDocumentsMarker) {
				t.Fatalf("the not-from-documents marker reached the answer: %q", r.text)
			}
			marked := c.outcome == agentux076Marked
			if r.delivery.NotFromDocuments != marked {
				t.Fatalf("not-from-documents = %t, want %t for a %s", r.delivery.NotFromDocuments, marked, c.outcome)
			}
			if clarifying := strings.HasSuffix(strings.TrimSpace(r.text), "?"); clarifying != (c.outcome == agentux076Clarifying) {
				t.Fatalf("an answer reading %q is not a %s", r.text, c.outcome)
			}
			// Documents the answer cites, and the listed ones it names, are linked
			// from the ids the server read.
			linked := map[string]bool{}
			for _, document := range r.delivery.Documents {
				linked[document.Reference.DocumentID] = true
			}
			for _, doc := range c.grounded {
				if !linked[doc.id] {
					t.Fatalf("the cited %q is not among the answer's sources %v", doc.title, linked)
				}
			}
			for _, document := range append(append([]PersonaReadableDocument(nil), agentux076Reach.Placed...), agentux076Reach.Workspace...) {
				if strings.Contains(r.text, document.Title) && !linked[document.DocumentID] {
					t.Fatalf("the answer names %q and does not link it: %v", document.Title, linked)
				}
			}
		})
	}
}

// agentux076AssistantProfile is the Assistant as published: both search skills,
// looked after by Pat.
func agentux076AssistantProfile() agentpersona.PersonaProfile {
	return agentpersona.PersonaProfile{DisplayName: "Assistant", Purpose: "Answers everyday questions.", Owner: "Pat", EvalSuiteRef: localAgentDemoAssistantSuiteID,
		SkillPins: []agentskills.SkillPin{{ID: personaPolicyHelperSkillID}, {ID: personaWorkspaceSearchSkillID}}}
}
