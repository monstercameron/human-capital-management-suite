package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ---- fakes ---------------------------------------------------------------

type issue3995Journeys struct {
	approver string
	detail   workspace.JourneyDetail
	err      error
	inspects int
}

func (j *issue3995Journeys) Inspect(ctx context.Context, id string) (workspace.JourneyDetail, error) {
	j.inspects++
	if j.err != nil {
		return workspace.JourneyDetail{}, j.err
	}
	if id != j.detail.Summary.IntentID {
		return workspace.JourneyDetail{}, workspace.ErrJourneyUnknown
	}
	d := j.detail
	viewer, _ := trust.FromContext(ctx)
	d.CanDecide = viewer != nil && viewer.Subject() == j.approver
	return d, nil
}

type issue3995Chat struct {
	rooms       []chat.Conversation
	members     map[string][]chat.Membership
	unreadable  map[string]bool
	created     []chat.CreateConversationRequest
	sent        []chat.SendPostRequest
	sendErr     error
	createErr   error
	nextRoom    string
	sentByKey   map[string]chat.Post
	readsOfRoom int
}

func (c *issue3995Chat) ReadAuthorizedReference(_ context.Context, p chat.Principal, tenant, conversation, _ string) (chat.Conversation, *chat.Post, error) {
	c.readsOfRoom++
	if c.unreadable[conversation] || p.TenantID != tenant {
		return chat.Conversation{}, nil, chat.ErrPermissionDenied
	}
	for _, r := range c.rooms {
		if r.ID == conversation {
			return r, nil, nil
		}
	}
	if conversation == c.nextRoom {
		return chat.Conversation{ID: conversation, Kind: chat.Direct}, nil, nil
	}
	return chat.Conversation{}, nil, chat.ErrNotFound
}

func (c *issue3995Chat) ListConversations(_ context.Context, r chat.ListConversationsRequest) (chat.ListConversationsResponse, error) {
	return chat.ListConversationsResponse{Conversations: c.rooms}, nil
}

func (c *issue3995Chat) ListMemberships(_ context.Context, r chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	return chat.ListMembershipsResponse{Memberships: c.members[r.ConversationID]}, nil
}

func (c *issue3995Chat) CreateConversation(_ context.Context, r chat.CreateConversationRequest) (chat.Conversation, error) {
	if c.createErr != nil {
		return chat.Conversation{}, c.createErr
	}
	c.created = append(c.created, r)
	c.nextRoom = "dm-new"
	return chat.Conversation{ID: "dm-new", TenantID: r.TenantID, Kind: r.Kind, OwnerID: r.OwnerID}, nil
}

func (c *issue3995Chat) SendPost(_ context.Context, r chat.SendPostRequest) (chat.Post, error) {
	if c.sendErr != nil {
		return chat.Post{}, c.sendErr
	}
	if c.sentByKey == nil {
		c.sentByKey = map[string]chat.Post{}
	}
	if p, ok := c.sentByKey[r.IdempotencyKey]; ok {
		return p, nil
	}
	c.sent = append(c.sent, r)
	p := chat.Post{ID: "post-" + r.IdempotencyKey[:6], ConversationID: r.ConversationID, TenantID: r.TenantID, AuthorID: r.Principal.SubjectID, Body: r.Body}
	c.sentByKey[r.IdempotencyKey] = p
	return p, nil
}

type issue3995Store struct {
	rows []chatstore.ApprovalClarification
}

func (s *issue3995Store) RecordApprovalClarification(_ context.Context, c chatstore.ApprovalClarification) (chatstore.ApprovalClarification, bool, error) {
	for _, r := range s.rows {
		if r.AskedBy == c.AskedBy && r.IdempotencyKey == c.IdempotencyKey {
			return r, false, nil
		}
	}
	s.rows = append(s.rows, c)
	return c, true, nil
}

func (s *issue3995Store) ListApprovalClarifications(_ context.Context, _, journeyID, person string, _ int) ([]chatstore.ApprovalClarification, error) {
	var out []chatstore.ApprovalClarification
	for _, r := range s.rows {
		if r.JourneyID == journeyID && (r.AskedBy == person || r.RecipientID == person) {
			out = append(out, r)
		}
	}
	return out, nil
}

type issue3995World struct {
	svc      *ApprovalClarifications
	journeys *issue3995Journeys
	chat     *issue3995Chat
	store    *issue3995Store
	intents  *issue3791Intents
}

func issue3995Detail(stage workspace.JourneyStage, revision string) workspace.JourneyDetail {
	return workspace.JourneyDetail{Summary: workspace.JourneySummary{IntentID: "j1", WorkerName: "Avery Stone", Stage: stage, ProposalRevisionID: revision, EffectiveDate: "2027-01-01", ProposedBase: "123456"}}
}

func issue3995Build() *issue3995World {
	w := &issue3995World{
		journeys: &issue3995Journeys{approver: "morgan", detail: issue3995Detail(workspace.JourneyStageManagerApproval, "rev-1")},
		chat:     &issue3995Chat{members: map[string][]chat.Membership{}, unreadable: map[string]bool{}},
		store:    &issue3995Store{},
		intents: &issue3791Intents{byID: map[string]*intentsv1.IntentInstance{
			"j1": {IntentId: "j1", Initiator: &intentsv1.PrincipalReference{PrincipalId: "riley", Kind: intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN}},
		}},
	}
	w.svc = &ApprovalClarifications{
		Journeys: w.journeys, Chat: w.chat, Store: w.store, Intents: w.intents,
		Names: func(_ context.Context, _ string, ids []string) (map[string]string, error) {
			names := map[string]string{"morgan": "Morgan Hale", "riley": "Riley Park"}
			out := map[string]string{}
			for _, id := range ids {
				if n, ok := names[id]; ok {
					out[id] = n
				}
			}
			return out, nil
		},
		Now: func() time.Time { return time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC) },
	}
	return w
}

func issue3995Ask(rev, question, key string) ApprovalClarificationAsk {
	return ApprovalClarificationAsk{JourneyID: "j1", Revision: rev, Question: question, IdempotencyKey: key}
}

const issue3995Question = "Why is the effective date the first of January?"

// ---- the approver's view ---------------------------------------------------

func TestIssue3995_ApproverSeesTheCaseRevisionAndWhoCanReadTheQuestion(t *testing.T) {
	w := issue3995Build()
	view, err := w.svc.View(context.Background(), issue3790Principal(t, "morgan"), "j1")
	if err != nil {
		t.Fatal(err)
	}
	if !view.CanAsk || view.Revision != "rev-1" || view.WorkerName != "Avery Stone" || view.Status != "awaiting_you" || !view.CaseOpen {
		t.Fatalf("view = %+v", view)
	}
	if view.Audience == nil || view.Audience.RecipientName != "Riley Park" || !view.Audience.Private || view.Audience.Kind != "direct" {
		t.Fatalf("audience = %+v", view.Audience)
	}
	if len(w.chat.sent) != 0 || len(w.chat.created) != 0 {
		t.Fatalf("previewing wrote to chat: %d sent, %d created", len(w.chat.sent), len(w.chat.created))
	}
}

func TestIssue3995_ABystanderWhoCanReadTheCaseCannotAskOrSeeAnAudience_Security(t *testing.T) {
	w := issue3995Build()
	view, err := w.svc.View(context.Background(), issue3790Principal(t, "casey"), "j1")
	if err != nil {
		t.Fatal(err)
	}
	if view.CanAsk || view.Audience != nil || view.Revision != "" || len(view.Items) != 0 {
		t.Fatalf("a person who does not hold the decision was offered the question: %+v", view)
	}
	if view.Status != "awaiting_other" {
		t.Fatalf("status = %q", view.Status)
	}
}

func TestIssue3995_ACaseTheViewerCannotOpenIsNotFound_Security(t *testing.T) {
	w := issue3995Build()
	w.journeys.err = workspace.ErrJourneyUnknown
	if _, err := w.svc.View(context.Background(), issue3790Principal(t, "morgan"), "j1"); !errors.Is(err, ErrChatWorkNotFound) {
		t.Fatalf("view err = %v", err)
	}
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", issue3995Question, "key-00001")); !errors.Is(err, ErrChatWorkNotFound) {
		t.Fatalf("ask err = %v", err)
	}
	if len(w.chat.sent)+len(w.chat.created) != 0 || len(w.store.rows) != 0 {
		t.Fatal("a case the viewer cannot open led to a write")
	}
}

// ---- asking ------------------------------------------------------------------

func TestIssue3995_AskPostsOnlyToThePrivateConversationWithTheRequester_Integration(t *testing.T) {
	w := issue3995Build()
	asked, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", "  "+issue3995Question+"  ", "key-00001"))
	if err != nil {
		t.Fatal(err)
	}
	if len(w.chat.created) != 1 {
		t.Fatalf("created %d conversations", len(w.chat.created))
	}
	created := w.chat.created[0]
	if created.Kind != chat.Direct || created.Principal.SubjectID != "morgan" || len(created.Members) != 1 || created.Members[0].SubjectID != "riley" {
		t.Fatalf("the conversation is not a private direct message to the requester: %+v", created)
	}
	if len(w.chat.sent) != 1 {
		t.Fatalf("sent %d posts", len(w.chat.sent))
	}
	post := w.chat.sent[0]
	if post.ConversationID != "dm-new" || post.Principal.SubjectID != "morgan" || post.Audience != nil || post.ParentID != "" {
		t.Fatalf("post = %+v", post)
	}
	if !strings.HasPrefix(post.Body, issue3995Question) || !strings.Contains(post.Body, "/workspace/app/journeys?journey=j1") {
		t.Fatalf("body = %q", post.Body)
	}
	// Nothing about pay or the person goes into the message: only what the
	// approver wrote and the way back to the case.
	for _, leak := range []string{"123456", "Avery", "2027-01-01"} {
		if strings.Contains(post.Body, leak) {
			t.Fatalf("the message carries %q: %q", leak, post.Body)
		}
	}
	if len(w.store.rows) != 1 {
		t.Fatalf("recorded %d", len(w.store.rows))
	}
	row := w.store.rows[0]
	if row.JourneyID != "j1" || row.ProposalRevision != "rev-1" || row.AskedBy != "morgan" || row.RecipientID != "riley" || row.ConversationID != "dm-new" || row.PostID == "" || row.QuestionDigest == "" {
		t.Fatalf("record = %+v", row)
	}
	if strings.Contains(row.QuestionDigest, "effective") {
		t.Fatalf("the record holds the question's words: %q", row.QuestionDigest)
	}
	if asked.Item.Revision != "rev-1" || asked.Item.State != "current" || asked.Item.Role != "asker" || asked.Item.Href != "/workspace/app/chat#channel=dm-new" || !asked.Created {
		t.Fatalf("asked = %+v", asked)
	}
}

func TestIssue3995_AskReusesAnExistingDirectConversationEvenOneTheRequesterStarted(t *testing.T) {
	w := issue3995Build()
	w.chat.rooms = []chat.Conversation{
		{ID: "chan", Kind: chat.PublicChannel, Name: "hr"},
		{ID: "dm-other", Kind: chat.Direct, OwnerID: "casey"},
		{ID: "dm-old", Kind: chat.Direct, OwnerID: "riley"},
	}
	joined := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w.chat.members["dm-other"] = []chat.Membership{{SubjectID: "morgan", JoinedAt: &joined}, {SubjectID: "casey", JoinedAt: &joined}}
	w.chat.members["dm-old"] = []chat.Membership{{SubjectID: "riley", JoinedAt: &joined}, {SubjectID: "morgan", JoinedAt: &joined}}
	asked, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", issue3995Question, "key-00001"))
	if err != nil {
		t.Fatal(err)
	}
	if len(w.chat.created) != 0 || len(w.chat.sent) != 1 || w.chat.sent[0].ConversationID != "dm-old" || asked.Item.Href != "/workspace/app/chat#channel=dm-old" {
		t.Fatalf("created %d, sent %+v", len(w.chat.created), w.chat.sent)
	}
}

func TestIssue3995_AGroupWithTheRequesterIsNotAPrivateAudience_Security(t *testing.T) {
	w := issue3995Build()
	w.chat.rooms = []chat.Conversation{{ID: "grp", Kind: chat.Group}, {ID: "dm-three", Kind: chat.Direct}}
	joined := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w.chat.members["grp"] = []chat.Membership{{SubjectID: "riley", JoinedAt: &joined}, {SubjectID: "morgan", JoinedAt: &joined}, {SubjectID: "casey", JoinedAt: &joined}}
	// A direct conversation that also holds a third person must not be used.
	w.chat.members["dm-three"] = []chat.Membership{{SubjectID: "riley", JoinedAt: &joined}, {SubjectID: "morgan", JoinedAt: &joined}, {SubjectID: "casey", JoinedAt: &joined}}
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", issue3995Question, "key-00001")); err != nil {
		t.Fatal(err)
	}
	if len(w.chat.sent) != 1 || w.chat.sent[0].ConversationID != "dm-new" {
		t.Fatalf("the question went to %+v", w.chat.sent)
	}
}

func TestIssue3995_OnlyTheRoutedApproverMayAsk_Security(t *testing.T) {
	w := issue3995Build()
	for _, who := range []string{"casey", "riley"} {
		_, err := w.svc.Ask(context.Background(), issue3790Principal(t, who), issue3995Ask("rev-1", issue3995Question, "key-00001"))
		if !errors.Is(err, ErrChatWorkDenied) {
			t.Fatalf("%s: err = %v", who, err)
		}
	}
	if len(w.chat.sent)+len(w.chat.created) != 0 || len(w.store.rows) != 0 {
		t.Fatal("a refused ask still wrote")
	}
}

func TestIssue3995_AskRefusesAProposalThatChangedSinceItWasReviewed(t *testing.T) {
	w := issue3995Build()
	w.journeys.detail.Summary.ProposalRevisionID = "rev-2"
	_, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", issue3995Question, "key-00001"))
	if !errors.Is(err, ErrApprovalClarificationStale) {
		t.Fatalf("err = %v", err)
	}
	if len(w.chat.sent)+len(w.chat.created) != 0 || len(w.store.rows) != 0 {
		t.Fatal("a stale ask still wrote")
	}
}

func TestIssue3995_AskRefusesAClosedCase(t *testing.T) {
	for _, stage := range []workspace.JourneyStage{workspace.JourneyStageCompleted, workspace.JourneyStageRejected, workspace.JourneyStageFailed} {
		w := issue3995Build()
		w.journeys.detail.Summary.Stage = stage
		_, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", issue3995Question, "key-00001"))
		if !errors.Is(err, ErrApprovalClarificationClosed) {
			t.Fatalf("%s: err = %v", stage, err)
		}
		if len(w.chat.sent)+len(w.chat.created) != 0 {
			t.Fatalf("%s: a closed case still wrote", stage)
		}
	}
}

func TestIssue3995_ARetryWithTheSameKeySendsOneQuestion_Integration(t *testing.T) {
	w := issue3995Build()
	first, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", issue3995Question, "key-00001"))
	if err != nil {
		t.Fatal(err)
	}
	// The retry arrives after the proposal changed: the question was already
	// asked, so the first answer comes back and nothing is sent again.
	w.journeys.detail.Summary.ProposalRevisionID = "rev-2"
	again, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", issue3995Question, "key-00001"))
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || again.Item.ID != first.Item.ID || len(w.chat.sent) != 1 || len(w.store.rows) != 1 {
		t.Fatalf("retry = %+v; %d sent, %d recorded", again, len(w.chat.sent), len(w.store.rows))
	}
	// The same key with different words is a mistake, not a second question.
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", "A different question?", "key-00001")); !errors.Is(err, ErrChatWorkConflict) {
		t.Fatalf("key reuse err = %v", err)
	}
	if len(w.chat.sent) != 1 {
		t.Fatalf("key reuse sent a message")
	}
}

func TestIssue3995_AskWhenTheRequesterCannotBeMessagedWritesNothing(t *testing.T) {
	w := issue3995Build()
	w.chat.createErr = chat.ErrPermissionDenied
	_, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", issue3995Question, "key-00001"))
	if !errors.Is(err, ErrApprovalClarificationRecipient) {
		t.Fatalf("err = %v", err)
	}
	if len(w.chat.sent) != 0 || len(w.store.rows) != 0 {
		t.Fatal("a refused message was still recorded")
	}
}

func TestIssue3995_AskNeedsAKnownHumanRequester(t *testing.T) {
	for name, initiator := range map[string]*intentsv1.PrincipalReference{
		"none":    nil,
		"agent":   {PrincipalId: "bot", Kind: intentsv1.InitiatorKind_INITIATOR_KIND_AGENT},
		"self":    {PrincipalId: "morgan", Kind: intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN},
		"no name": {PrincipalId: " ", Kind: intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN},
	} {
		w := issue3995Build()
		w.intents.byID["j1"].Initiator = initiator
		view, err := w.svc.View(context.Background(), issue3790Principal(t, "morgan"), "j1")
		if err != nil || view.CanAsk || view.AskBlocked != "no_recipient" {
			t.Fatalf("%s: view = %+v, %v", name, view, err)
		}
		if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", issue3995Question, "key-00001")); !errors.Is(err, ErrApprovalClarificationRecipient) {
			t.Fatalf("%s: ask err = %v", name, err)
		}
		if len(w.chat.sent)+len(w.chat.created) != 0 {
			t.Fatalf("%s: wrote to chat", name)
		}
	}
}

func TestIssue3995_QuestionIsValidatedBeforeAnyWrite(t *testing.T) {
	long := strings.Repeat("x", approvalClarificationQuestionMax+1)
	for name, req := range map[string]ApprovalClarificationAsk{
		"empty":       issue3995Ask("rev-1", "   ", "key-00001"),
		"too long":    issue3995Ask("rev-1", long, "key-00001"),
		"control":     issue3995Ask("rev-1", "bad\x00text", "key-00001"),
		"no revision": issue3995Ask(" ", issue3995Question, "key-00001"),
		"short key":   issue3995Ask("rev-1", issue3995Question, "k"),
		"no journey":  {JourneyID: "", Revision: "rev-1", Question: issue3995Question, IdempotencyKey: "key-00001"},
	} {
		w := issue3995Build()
		if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), req); !errors.Is(err, ErrChatWorkInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
		if w.journeys.inspects != 0 || len(w.chat.sent)+len(w.chat.created) != 0 {
			t.Errorf("%s: acted on an invalid request", name)
		}
	}
}

// ---- what each person sees afterwards ------------------------------------------

func TestIssue3995_RequesterSeesTheQuestionAndAChangedProposalIsFlagged(t *testing.T) {
	w := issue3995Build()
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", issue3995Question, "key-00001")); err != nil {
		t.Fatal(err)
	}
	requester := issue3790Principal(t, "riley")
	view, err := w.svc.View(context.Background(), requester, "j1")
	if err != nil {
		t.Fatal(err)
	}
	if view.CanAsk || len(view.Items) != 1 {
		t.Fatalf("requester view = %+v", view)
	}
	item := view.Items[0]
	if item.Role != "recipient" || item.PeerName != "Morgan Hale" || item.State != "current" || item.Href != "/workspace/app/chat#channel=dm-new" || item.Revision != "rev-1" {
		t.Fatalf("item = %+v", item)
	}
	// The proposal is edited: the old question now concerns an earlier revision.
	w.journeys.detail.Summary.ProposalRevisionID = "rev-2"
	for _, who := range []string{"riley", "morgan"} {
		view, err = w.svc.View(context.Background(), issue3790Principal(t, who), "j1")
		if err != nil || len(view.Items) != 1 || view.Items[0].State != "outdated" {
			t.Fatalf("%s after the edit: %+v, %v", who, view.Items, err)
		}
	}
	// The case closes: the record stays and says so.
	w.journeys.detail.Summary.Stage = workspace.JourneyStageCompleted
	view, _ = w.svc.View(context.Background(), requester, "j1")
	if view.CaseOpen || view.Status != "closed" || len(view.Items) != 1 || view.Items[0].State != "closed" {
		t.Fatalf("closed view = %+v", view)
	}
}

func TestIssue3995_APersonWhoLostTheConversationKeepsTheRecordButGetsNoLink_Security(t *testing.T) {
	w := issue3995Build()
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3995Ask("rev-1", issue3995Question, "key-00001")); err != nil {
		t.Fatal(err)
	}
	w.chat.unreadable["dm-new"] = true
	view, err := w.svc.View(context.Background(), issue3790Principal(t, "riley"), "j1")
	if err != nil || len(view.Items) != 1 {
		t.Fatalf("view = %+v, %v", view, err)
	}
	if item := view.Items[0]; !item.Unavailable || item.Href != "" || item.ConversationID != "" {
		t.Fatalf("a person without access was given the conversation: %+v", item)
	}
	body, _ := json.Marshal(view)
	if strings.Contains(string(body), "dm-new") {
		t.Fatalf("the conversation id leaked: %s", body)
	}
}

func TestIssue3995_AnotherPersonsRequestIsNeverListed_Security(t *testing.T) {
	w := issue3995Build()
	w.store.rows = []chatstore.ApprovalClarification{{TenantID: "tenant-a", ID: "x", JourneyID: "j1", ProposalRevision: "rev-1", ConversationID: "dm-x", AskedBy: "casey", RecipientID: "dana", QuestionDigest: "d", IdempotencyKey: "key-xxxxx"}}
	view, err := w.svc.View(context.Background(), issue3790Principal(t, "morgan"), "j1")
	if err != nil || len(view.Items) != 0 {
		t.Fatalf("view = %+v, %v", view, err)
	}
}

// ---- the HTTP edge ---------------------------------------------------------------

type issue3995Port struct{ svc *ApprovalClarifications }

func (p issue3995Port) View(ctx context.Context, who *trust.Principal, id string) (ApprovalClarificationView, error) {
	return p.svc.View(ctx, who, id)
}

func (p issue3995Port) Ask(ctx context.Context, who *trust.Principal, r ApprovalClarificationAsk) (ApprovalClarificationAsked, error) {
	return p.svc.Ask(ctx, who, r)
}

func issue3995Serve(t *testing.T, w *issue3995World, who, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	handler := ApprovalClarificationsHandler{Clarifications: issue3995Port{w.svc}}
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req = req.WithContext(trust.WithPrincipal(req.Context(), issue3790Principal(t, who)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestIssue3995_RoutesAnswerWithTheStatusesTheClientReads_Integration(t *testing.T) {
	w := issue3995Build()
	rec := issue3995Serve(t, w, "morgan", http.MethodGet, "/api/chat/journeys/j1/clarification", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Body.String(), `"can_ask":true`) {
		t.Fatalf("get = %d %s", rec.Code, rec.Body.String())
	}
	ask := `{"revision":"rev-1","question":"` + issue3995Question + `","idempotency_key":"key-00001"}`
	rec = issue3995Serve(t, w, "morgan", http.MethodPost, "/api/chat/journeys/j1/clarification", ask)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"state":"current"`) {
		t.Fatalf("post = %d %s", rec.Code, rec.Body.String())
	}
	rec = issue3995Serve(t, w, "morgan", http.MethodPost, "/api/chat/journeys/j1/clarification", ask)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry = %d %s", rec.Code, rec.Body.String())
	}
	for name, tc := range map[string]struct {
		who, method, path, body string
		status                  int
		code                    string
	}{
		"not the approver": {"casey", http.MethodPost, "/api/chat/journeys/j1/clarification", ask, http.StatusForbidden, "permission_denied"},
		"unknown case":     {"morgan", http.MethodPost, "/api/chat/journeys/zz/clarification", ask, http.StatusNotFound, "not_found"},
		"changed":          {"morgan", http.MethodPost, "/api/chat/journeys/j1/clarification", `{"revision":"rev-0","question":"Why?","idempotency_key":"key-00002"}`, http.StatusConflict, "revision_changed"},
		"unknown field":    {"morgan", http.MethodPost, "/api/chat/journeys/j1/clarification", `{"revision":"rev-1","question":"Why?","idempotency_key":"key-00003","recipient":"x"}`, http.StatusBadRequest, "invalid_argument"},
		"wrong method":     {"morgan", http.MethodDelete, "/api/chat/journeys/j1/clarification", "", http.StatusMethodNotAllowed, "method_not_allowed"},
	} {
		rec := issue3995Serve(t, w, tc.who, tc.method, tc.path, tc.body)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestIssue3995_RouteRefusesAPostWithNoSignedInPerson_Security(t *testing.T) {
	w := issue3995Build()
	handler := ApprovalClarificationsHandler{Clarifications: issue3995Port{w.svc}}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/chat/journeys/j1/clarification", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}
