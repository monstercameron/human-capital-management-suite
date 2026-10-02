package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// agentUXSearchWorkspaceReaders proves the preparation's workspace-wide read
// grants: before it the seeded cell holds no document every member may read,
// after it the two demo documents are readable by everyone through ordinary
// grants, a replay adds nothing, a person's deny removes only that document from
// the workspace-wide set, and keyword search (no model files are installed in
// tests) finds the guide for a member who is not in the conversation.
func agentUXSearchWorkspaceReaders(t *testing.T, ctx context.Context, core dbport.Beginner, mapper func(values.TenantId) uuid.UUID, documents *documenthubstore.Store, conversation string) {
	t.Helper()
	directory := WorkspaceDocumentDirectory{DB: core, TenantUUID: mapper}
	people, err := directory.WorkspaceDocumentMembers(ctx, localAgentDemoTenant)
	if err != nil || len(people) < 4 {
		t.Fatalf("workspace directory: %d members, %v", len(people), err)
	}
	if status, err := documents.WorkspaceIndexStatus(ctx, localAgentDemoTenant, "", people); err != nil || status.Documents != 0 {
		t.Fatalf("a document was already readable by every member: %+v %v", status, err)
	}
	shared, err := ensureLocalAgentDemoWorkspaceReaders(ctx, documents, directory, localAgentDemoTenant, localAgentDemoAdmin, conversation)
	if err != nil || shared != 2 {
		t.Fatalf("workspace readers shared=%d err=%v", shared, err)
	}
	if status, err := documents.WorkspaceIndexStatus(ctx, localAgentDemoTenant, "", people); err != nil || status.Documents != 2 {
		t.Fatalf("workspace-public documents after sharing: %+v %v", status, err)
	}
	if again, err := ensureLocalAgentDemoWorkspaceReaders(ctx, documents, directory, localAgentDemoTenant, localAgentDemoAdmin, conversation); err != nil || again != 0 {
		t.Fatalf("workspace readers replay shared=%d err=%v", again, err)
	}
	// Someone who is not in the conversation reads the guide through the workspace search.
	outsider := ""
	for _, person := range people {
		if person != localAgentDemoAdmin && person != "ir-008-curtis-bell" {
			outsider = person
			break
		}
	}
	hits, err := documents.SearchWorkspaceKeyword(ctx, localAgentDemoTenant, outsider, "which company holidays are coming up in the rest of 2026", people)
	if err != nil || len(hits) != 1 || hits[0].Title != localAgentDemoHolidayTitle || !strings.Contains(hits[0].Text, "Thanksgiving Day") {
		t.Fatalf("keyword workspace search for %s: %+v %v", outsider, hits, err)
	}
	deny, err := documents.GrantAction(ctx, localAgentDemoTenant, documenthubstore.GrantInput{DocumentID: hits[0].DocumentID, SubjectKind: "person", SubjectID: outsider, Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectDeny, Issuer: localAgentDemoAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if status, err := documents.WorkspaceIndexStatus(ctx, localAgentDemoTenant, "", people); err != nil || status.Documents != 1 {
		t.Fatalf("a deny did not remove the guide from the workspace-wide set: %+v %v", status, err)
	}
	if err := documents.RevokeGrant(ctx, localAgentDemoTenant, deny.ID, localAgentDemoAdmin); err != nil {
		t.Fatal(err)
	}
	if status, err := documents.WorkspaceIndexStatus(ctx, localAgentDemoTenant, "", people); err != nil || status.Documents != 2 {
		t.Fatalf("workspace-public documents after the deny was revoked: %+v %v", status, err)
	}
}

// agentUXSearchModel is the deterministic model of this test. It follows the
// instructions it is given and answers only from what a tool returned. Without
// the instruction to search, or without the search tool offered, it gives the
// answer the review cell showed (no documents), so the test fails if either the
// instructions or the tool projection regress.
type agentUXSearchMentionModel struct {
	requests []agentmodel.ModelRequest
}

func (m *agentUXSearchMentionModel) respond(request agentmodel.ModelRequest) agentmodel.ModelResult {
	m.requests = append(m.requests, request)
	var instructions, question string
	for _, message := range request.Messages {
		switch message.Role {
		case agentmodel.RoleDeveloper:
			instructions = message.Content
		case agentmodel.RoleUser:
			question = message.Content
		}
	}
	complete := func(text string) agentmodel.ModelResult {
		return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Finish: agentmodel.FinishComplete, Text: text}
	}
	last := request.Messages[len(request.Messages)-1]
	if last.Role != agentmodel.RoleTool {
		offered := map[string]bool{}
		for _, tool := range request.Tools {
			offered[tool.Name] = true
		}
		tool := ""
		if strings.Contains(instructions, "you MUST search first") {
			if strings.Contains(question, "top 5 policies") && offered[personaWorkspaceSearchTool] {
				tool = personaWorkspaceSearchTool
			} else if offered[personaDocumentSearchTool] {
				tool = personaDocumentSearchTool
			}
		}
		if tool == "" {
			return complete("I don't have any company holiday documents provided in this conversation, so I can't list official company holidays for the rest of 2026.")
		}
		query := "company holidays 2026"
		if tool == personaWorkspaceSearchTool {
			query = "company policies and guides"
		}
		return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Finish: agentmodel.FinishToolCalls, ToolProposals: []agentmodel.ToolProposal{{ID: "search-1", Name: tool, Arguments: json.RawMessage(`{"query":"` + query + `"}`)}}}
	}
	var result PersonaPolicyDocumentSearchResult
	if json.Unmarshal([]byte(last.Content), &result) != nil || len(result.Hits) == 0 {
		return complete("I could not find an answer in documents you can read.")
	}
	var lines []string
	if strings.Contains(question, "top 5 policies") {
		for _, hit := range result.Hits {
			lines = append(lines, fmt.Sprintf("%s: a company document. Source: [%s](document:%s)", hit.Title, hit.Title, hit.DocumentID))
		}
		return complete(strings.Join(lines, "\n"))
	}
	today := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	for _, hit := range result.Hits {
		if !strings.Contains(strings.ToLower(hit.Title), "holiday") {
			continue
		}
		for _, row := range strings.Split(hit.Markdown, "\n") {
			cells := strings.Split(strings.Trim(row, "| "), " | ")
			if len(cells) != 3 {
				continue
			}
			observed, err := time.Parse("Jan 2 2006", cells[1]+" 2026")
			if err != nil || !observed.After(today) {
				continue
			}
			lines = append(lines, strings.Join(cells, " | "))
		}
		lines = append(lines, fmt.Sprintf("Source: [%s](document:%s)", hit.Title, hit.DocumentID))
		return complete(strings.Join(lines, "\n"))
	}
	return complete("I could not find an answer in documents you can read.")
}

type agentUXSearchMentionWorker struct {
	t         *testing.T
	chat      chatcore.ConversationService
	personas  *agentpersonastore.Store
	principal chatcore.Principal
	tools     func(profile agentpersona.PersonaProfile, request agentinvoke.RunRequest) (*PersonaRuntimeTools, agentrun.Record, runstate.Run)
	model     agentUXSearchMentionModel
	// Order of events, so the test can prove the search came before the answer.
	events   []string
	toolRows []agentpersonastore.ToolResultRecord
	cited    []string
}

func (w *agentUXSearchMentionWorker) Start(ctx context.Context, request agentinvoke.RunRequest) error {
	t := w.t
	t.Helper()
	version, err := strconv.ParseInt(request.PersonaVersion, 10, 64)
	if err != nil || request.PersonaID != localAgentDemoAssistantPersonaID {
		return errors.New("unexpected run")
	}
	scoped, _ := w.personas.Scoped(values.TenantId(request.TenantID))
	row, err := scoped.GetVersion(ctx, request.PersonaID, version)
	if err != nil {
		return err
	}
	var profile agentpersona.PersonaProfile
	if err := json.Unmarshal(row.Profile, &profile); err != nil {
		return err
	}
	posts, err := w.chat.ListPosts(ctx, chatcore.ListPostsRequest{Principal: w.principal, TenantID: request.TenantID, ConversationID: request.ConversationID, Page: chatcore.Page{PageSize: 50}})
	if err != nil {
		return err
	}
	question := ""
	for _, post := range posts.Posts {
		if post.ID == request.InvokingPostID {
			question = post.Body
		}
	}
	tools, record, run := w.tools(profile, request)
	schemas, err := tools.ToolSchemas(ctx, record, run)
	if err != nil {
		return err
	}
	modelRequest := agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, Tools: schemas, Messages: []agentmodel.ModelMessage{
		{Role: agentmodel.RoleSystem, Content: "You are Assistant."},
		{Role: agentmodel.RoleDeveloper, Content: profile.Instructions},
		{Role: agentmodel.RoleUser, Content: "Today is 2026-10-01.\n" + question},
	}}
	first := w.model.respond(modelRequest)
	if first.Finish != agentmodel.FinishToolCalls || len(first.ToolProposals) != 1 {
		w.events = append(w.events, "answer-without-search")
		_, err = w.chat.SendPost(ctx, chatcore.SendPostRequest{Principal: w.principal, TenantID: request.TenantID, ConversationID: request.ConversationID, ParentID: request.ThreadID, Body: first.Text, IdempotencyKey: "search-mention-reply:" + request.InvocationID})
		return err
	}
	proposal := first.ToolProposals[0]
	w.events = append(w.events, "search:"+proposal.Name)
	output, _, _, err := tools.Execute(ctx, record, run, proposal)
	if err != nil {
		return err
	}
	modelRequest.Messages = append(modelRequest.Messages, agentmodel.ModelMessage{Role: agentmodel.RoleTool, ToolCallID: proposal.ID, ToolName: proposal.Name, Content: string(output)})
	modelRequest.Tools = nil
	second := w.model.respond(modelRequest)
	w.events = append(w.events, "answer")
	// The sealed sources the answer may cite come from the journaled result,
	// re-verified against the asker's current access.
	gateway, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{PersonaChatReplyToolDescriptor()})
	if err != nil {
		return err
	}
	grounding, err := tools.ReadPersonaRunToolGrounding(ctx, record, run, gateway)
	if err != nil {
		return err
	}
	sealed, err := gateway.BuildAnswer(grounding)
	if err != nil {
		return err
	}
	for _, part := range sealed.Parts {
		for _, citation := range part.Citations {
			w.cited = append(w.cited, citation.SourceID)
		}
	}
	rows, err := tools.cfg.Journal.ListPersonaRuntimeToolResults(ctx, values.TenantId(request.TenantID), run.ID)
	if err != nil {
		return err
	}
	w.toolRows = append(w.toolRows, rows...)
	_, err = w.chat.SendPost(ctx, chatcore.SendPostRequest{Principal: w.principal, TenantID: request.TenantID, ConversationID: request.ConversationID, ParentID: request.ThreadID, Body: second.Text, IdempotencyKey: "search-mention-reply:" + request.InvocationID})
	return err
}

// agentUXSearchHolidayMention drives the real chat mention path, the real
// Assistant version the preparation published, the real tool projection,
// execution, journal and grounding, and the real document hub, with the
// deterministic model above, for the exact question the review cell got wrong.
func agentUXSearchHolidayMention(t *testing.T, ctx context.Context, chat chatcore.ConversationService, personas *agentpersonastore.Store, core dbport.Beginner, documents *documenthubstore.Store, conversation string, now time.Time, mapper func(values.TenantId) uuid.UUID) {
	t.Helper()
	_, _, _, _, _, _, baseRecord, baseRun := runtimeToolFixture(t)
	evidence := app.NewMemoryEvidenceSink()
	caps, _, err := newAgentCapabilities(ownWorkerReader{}, evidence, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := newAgentSkills(caps)
	if err != nil {
		t.Fatal(err)
	}
	searcher, err := NewPersonaPolicyDocumentSearcher(documentService{store: documents}, documents, WorkspaceDocumentDirectory{DB: core, TenantUUID: mapper})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bindPersonaPolicySearchSkill(caps, skills, searcher); err != nil {
		t.Fatal(err)
	}
	workspacePin, err := BindPersonaWorkspaceSearchSkill(caps, skills, searcher)
	if err != nil {
		t.Fatal(err)
	}
	scoped, _ := personas.Scoped(values.TenantId(localAgentDemoTenant))
	rows, err := scoped.ListActiveInstallations(ctx, conversation)
	if err != nil {
		t.Fatal(err)
	}
	installation, personaVersion := "", ""
	for _, row := range rows {
		if row.PersonaID == localAgentDemoAssistantPersonaID {
			installation, personaVersion = row.InstallationID, fmt.Sprint(row.PersonaVersion)
		}
	}
	version, _ := strconv.ParseInt(personaVersion, 10, 64)
	versionRow, err := scoped.GetVersion(ctx, localAgentDemoAssistantPersonaID, version)
	if err != nil {
		t.Fatal(err)
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(versionRow.Profile, &profile) != nil {
		t.Fatal("Assistant profile unavailable")
	}
	pinned := false
	for _, pin := range profile.SkillPins {
		if pin.ID == personaWorkspaceSearchSkillID {
			pinned = pin == workspacePin
		}
	}
	if !pinned || profile.Version < 3 || !strings.Contains(profile.Instructions, "you MUST search first") {
		t.Fatalf("the installed Assistant is not the workspace-search version: version=%d pinned=%t", profile.Version, pinned)
	}
	principal := chatcore.Principal{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}
	worker := &agentUXSearchMentionWorker{t: t, chat: chat, personas: personas, principal: principal}
	worker.tools = func(profile agentpersona.PersonaProfile, request agentinvoke.RunRequest) (*PersonaRuntimeTools, agentrun.Record, runstate.Run) {
		record, run := baseRecord, baseRun
		record.ID, record.Request.Source = "run-"+request.InvocationID, agentrun.SourceIdentity{TenantID: request.TenantID, Kind: agentrun.SourcePersonaMention, Key: request.InvocationID, Ref: request.InvokingPostID}
		record.Request.Persona = &agentrun.PersonaRef{ID: request.PersonaID, Version: request.PersonaVersion, Digest: versionRow.ContentDigest}
		record.Request.InstallationID = request.InstallationID
		record.Request.Principal.InvokerID = request.InvokerID
		record.Request.Audience.ID, record.Request.Context.ID = request.ConversationID, request.ThreadID
		record.Authority.InstallationID = request.InstallationID
		record.Authority.Principal, record.Authority.Audience, record.Authority.Context = record.Request.Principal, record.Request.Audience, record.Request.Context
		run.ID, run.AdmissionID, run.TenantID = record.ID, record.ID, request.TenantID
		tools, err := NewPersonaRuntimeTools(PersonaRuntimeToolsConfig{Authority: &runtimeToolAdmission{snapshot: record.Authority}, Pins: &agentUXSearchRuntimePins{pins: profile.SkillPins}, Catalog: skills, DocumentScope: agentUXSearchRuntimeScope{}, Gateway: capability.NewGateway(caps, evidence), Journal: DatabasePersonaRuntimeToolJournal{Store: personas}, Documents: DatabasePersonaRuntimeDocumentVersions{Store: documents}, Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		return tools, record, run
	}
	identities, err := newProductionPersonaChatIdentityDirectory(personas)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := newProductionPersonaReferenceLookup(identities, personas)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := newPersonaChatReferenceResolver(lookup, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	scopes := agentinvoke.SkillScopes{personaPolicyHelperSkillID: {personaPolicySearchScope}, personaWorkspaceSearchSkillID: {personaPolicySearchScope}}
	invocation, err := newPersonaChatInvocation(personaChatInvocationConfig{Chat: chat, References: resolver, Authority: agentUXLiveAuthority{personaID: localAgentDemoAssistantPersonaID, version: personaVersion, installationID: installation, skills: scopes}, Grants: agentUXLiveGrant{}, Runs: worker, T0Skills: agentUXSearchMentionT0{}, Repository: agentinvoke.NewMemoryRepository()})
	if err != nil {
		t.Fatal(err)
	}
	ask := func(key, body string) chatcore.Post {
		t.Helper()
		before := len(worker.events)
		post, err := invocation.SendPost(ctx, chatcore.SendPostRequest{Principal: principal, TenantID: localAgentDemoTenant, ConversationID: conversation, Body: body, IdempotencyKey: key, References: []chatcore.Reference{{Kind: chatcore.AgentMention, TenantID: localAgentDemoTenant, ID: localAgentDemoAssistantAgentID, Display: "Assistant"}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(worker.events) == before {
			t.Fatal("the mention did not start a run")
		}
		return post
	}
	reply := func(post chatcore.Post) string {
		t.Helper()
		listed, err := chat.ListPosts(ctx, chatcore.ListPostsRequest{Principal: principal, TenantID: localAgentDemoTenant, ConversationID: conversation, Page: chatcore.Page{PageSize: 50}})
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range listed.Posts {
			if candidate.ParentID == post.ID {
				return candidate.Body
			}
		}
		t.Fatalf("no reply to %s: %+v", post.ID, listed.Posts)
		return ""
	}

	// The headline question, word for word.
	post := ask("search-holiday-mention", "@Assistant which company holidays are coming up in the rest of 2026")
	body := reply(post)
	if len(worker.events) != 2 || worker.events[0] != "search:"+personaDocumentSearchTool || worker.events[1] != "answer" {
		t.Fatalf("the Assistant answered without searching first: %v\n%s", worker.events, body)
	}
	for _, want := range []string{"Thanksgiving Day | Nov 26 | Thursday", "Day after Thanksgiving | Nov 27 | Friday", "Christmas Day | Dec 25 | Friday", "Source: [2026 holiday guide](document:"} {
		if !strings.Contains(body, want) {
			t.Fatalf("holiday answer lacks %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"Labor Day", "Juneteenth", "Independence Day", "don't have any company holiday documents"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("holiday answer contains %q:\n%s", unwanted, body)
		}
	}
	if len(worker.toolRows) != 1 || worker.toolRows[0].ToolName != personaDocumentSearchTool || len(worker.cited) == 0 || !strings.HasPrefix(worker.cited[0], "document:") || !strings.Contains(body, strings.TrimPrefix(strings.SplitN(worker.cited[0], "/version:", 2)[0], "document:")) {
		t.Fatalf("the answer's source is not the sealed search result: rows=%+v cited=%v", worker.toolRows, worker.cited)
	}
	if len(worker.model.requests) != 2 || len(worker.model.requests[0].Tools) != 2 || worker.model.requests[0].Tools[0].Name != personaDocumentSearchTool || worker.model.requests[0].Tools[1].Name != personaWorkspaceSearchTool || len(worker.model.requests[1].Tools) != 0 {
		t.Fatalf("tool projection: %+v", worker.model.requests)
	}

	// Documents across the workspace, found by keyword because no model files are installed.
	worker.events, worker.toolRows, worker.cited = nil, nil, nil
	list := ask("search-top-policies-mention", "@Assistant give me a list of the top 5 policies here")
	body = reply(list)
	if len(worker.events) != 2 || worker.events[0] != "search:"+personaWorkspaceSearchTool || len(worker.toolRows) != 1 || worker.toolRows[0].ToolName != personaWorkspaceSearchTool || len(worker.cited) < 2 {
		t.Fatalf("workspace search not used: %v rows=%+v cited=%v\n%s", worker.events, worker.toolRows, worker.cited, body)
	}
	for _, want := range []string{"Paid time off policy: ", "2026 holiday guide: ", "(document:"} {
		if !strings.Contains(body, want) {
			t.Fatalf("policy list lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "cannot search workspace documents") {
		t.Fatalf("keyword fallback reported itself as unavailable:\n%s", body)
	}
}

type agentUXSearchMentionT0 struct{}

func (agentUXSearchMentionT0) IsBoundT0Run(_ context.Context, request agentinvoke.RunRequest) (bool, error) {
	return request.PersonaID == localAgentDemoAssistantPersonaID && request.TenantID == localAgentDemoTenant && request.InvokerID == localAgentDemoAdmin && len(request.Skills) == 2, nil
}

// TestTodo_CHATBUG_015 is the review cell's failure end to end on the shared
// test database: a cell holding earlier Assistant and Policy Helper versions
// whose review evidence is stale is prepared (published versions left alone,
// Assistant upgraded with a fresh reviewer grant, review, evaluation and
// publication, rerun clean), the demo documents become readable by every
// member, and the exact holiday question in #general is answered after a search
// of the placed guide, with a source.
func TestTodo_CHATBUG_015(t *testing.T) {
	agentUXGeneralPublishRunnable(t, false, true)
}
