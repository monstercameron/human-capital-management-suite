package application

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The fakes of the neighbouring 3995 tests (journeys, chat) are reused: the
// same case, the same approver, the same conversation service.

type issue3998Store struct{ rows []chatstore.InputRequest }

func (s *issue3998Store) RecordInputRequest(_ context.Context, r chatstore.InputRequest) (chatstore.InputRequest, bool, error) {
	for _, e := range s.rows {
		if e.AskedBy == r.AskedBy && e.IdempotencyKey == r.IdempotencyKey {
			return e, false, nil
		}
	}
	s.rows = append(s.rows, r)
	return r, true, nil
}

func (s *issue3998Store) ListInputRequests(_ context.Context, _, journeyID, asker string, _ int) ([]chatstore.InputRequest, error) {
	var out []chatstore.InputRequest
	for _, r := range s.rows {
		if r.JourneyID == journeyID && r.AskedBy == asker {
			out = append(out, r)
		}
	}
	return out, nil
}

type issue3998World struct {
	svc      *InputRequests
	journeys *issue3995Journeys
	chat     *issue3995Chat
	store    *issue3998Store
}

func issue3998Build() *issue3998World {
	w := &issue3998World{
		journeys: &issue3995Journeys{approver: "morgan", detail: issue3995Detail(workspace.JourneyStageFinanceApproval, "rev-1")},
		chat: &issue3995Chat{members: map[string][]chat.Membership{}, unreadable: map[string]bool{}, rooms: []chat.Conversation{
			{ID: "fin", Kind: chat.PrivateChannel, Name: "finance-review", MemberCount: 5},
			{ID: "hr", Kind: chat.PublicChannel, Name: "hr", MemberCount: 120},
			{ID: "dm-fin", Kind: chat.Direct, MemberCount: 2},
		}},
		store: &issue3998Store{},
	}
	w.svc = &InputRequests{Journeys: w.journeys, Chat: w.chat, Store: w.store, Now: func() time.Time { return time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC) }}
	return w
}

const (
	issue3998Question = "Does the Finance band for this level still allow this increase?"
	issue3998Label    = "Informal input, not an approval"
)

func issue3998Ask(conversation, revision, key string) InputRequestAsk {
	return InputRequestAsk{
		JourneyID: "j1", Revision: revision, Question: issue3998Question, ConversationID: conversation,
		Label: issue3998Label, RevisionLabel: "Proposal revision " + revision, IdempotencyKey: key,
	}
}

func TestIssue3998_ReviewerPreviewsTheExactMessageAndAudienceAndNothingIsSent(t *testing.T) {
	w := issue3998Build()
	ask := issue3998Ask("fin", "rev-1", "")
	ask.Preview = true
	got, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), ask)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Preview || got.Item != nil || got.Revision != "rev-1" || got.Audience.Kind != "private_channel" || got.Audience.Name != "finance-review" || got.Audience.Size != 5 {
		t.Fatalf("preview = %+v", got)
	}
	if !strings.HasPrefix(got.Message, issue3998Label+"\n\n"+issue3998Question) || !strings.Contains(got.Message, "Proposal revision rev-1") || !strings.Contains(got.Message, "/workspace/app/journeys?journey=j1") {
		t.Fatalf("message = %q", got.Message)
	}
	if len(w.chat.sent) != 0 || len(w.store.rows) != 0 {
		t.Fatalf("a preview wrote: %d posts, %d records", len(w.chat.sent), len(w.store.rows))
	}
}

func TestIssue3998_SendPostsTheSamePreviewedMessageLabelledInformalAndRecordsIt(t *testing.T) {
	w := issue3998Build()
	prev := issue3998Ask("fin", "rev-1", "")
	prev.Preview = true
	preview, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), prev)
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3998Ask("fin", "rev-1", "key-00001"))
	if err != nil || !got.Created || got.Item == nil {
		t.Fatalf("ask = %+v, %v", got, err)
	}
	if len(w.chat.sent) != 1 || w.chat.sent[0].Body != preview.Message || w.chat.sent[0].ConversationID != "fin" || w.chat.sent[0].Principal.SubjectID != "morgan" {
		t.Fatalf("sent = %+v, wanted the previewed message %q", w.chat.sent, preview.Message)
	}
	// What the reviewer was shown is what was sent, and pay and dates stay out.
	for _, leak := range []string{"123456", "Avery", "2027-01-01"} {
		if strings.Contains(w.chat.sent[0].Body, leak) {
			t.Fatalf("the message carries %q", leak)
		}
	}
	if len(w.store.rows) != 1 {
		t.Fatalf("recorded %d", len(w.store.rows))
	}
	row := w.store.rows[0]
	if row.ProposalRevision != "rev-1" || row.AudienceKind != "private_channel" || row.AudienceSize != 5 || row.SharedCaseName || row.PostID == "" || strings.Contains(row.QuestionDigest, "Finance") {
		t.Fatalf("record = %+v", row)
	}
	if got.Item.Kind != "informal" || got.Item.State != "current" || got.Item.Href != "/workspace/app/chat#channel=fin" {
		t.Fatalf("item = %+v", got.Item)
	}
}

func TestIssue3998_TheCaseNameIsSharedOnlyByChoiceAndNeverIntoAPublicChannel_Security(t *testing.T) {
	w := issue3998Build()
	ask := issue3998Ask("fin", "rev-1", "key-00001")
	got, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), ask)
	if err != nil || strings.Contains(w.chat.sent[0].Body, "Avery Stone") || got.Item.SharedCaseName {
		t.Fatalf("the name went out without a choice: %v %q", err, w.chat.sent[0].Body)
	}
	chosen := issue3998Ask("dm-fin", "rev-1", "key-00002")
	chosen.IncludeCaseName = true
	got, err = w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), chosen)
	if err != nil || !strings.Contains(w.chat.sent[1].Body, "Avery Stone") || !got.Item.SharedCaseName || !w.store.rows[1].SharedCaseName {
		t.Fatalf("a chosen name was not shared and recorded: %v", err)
	}
	public := issue3998Ask("hr", "rev-1", "key-00003")
	public.IncludeCaseName = true
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), public); !errors.Is(err, ErrInputRequestPublicName) {
		t.Fatalf("a person's name into a public channel: err = %v", err)
	}
	public.Preview = true
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), public); !errors.Is(err, ErrInputRequestPublicName) {
		t.Fatalf("the preview must refuse it too: err = %v", err)
	}
	if len(w.chat.sent) != 2 || len(w.store.rows) != 2 {
		t.Fatalf("a refused ask wrote: %d posts, %d records", len(w.chat.sent), len(w.store.rows))
	}
}

func TestIssue3998_OnlyTheReviewerWhoHoldsTheDecisionMayAsk_Security(t *testing.T) {
	w := issue3998Build()
	view, err := w.svc.View(context.Background(), issue3790Principal(t, "casey"), "j1")
	if err != nil || view.CanAsk || view.Revision != "" || view.CaseName != "" || view.Status != "awaiting_other" {
		t.Fatalf("a bystander's view = %+v, %v", view, err)
	}
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "casey"), issue3998Ask("fin", "rev-1", "key-00001")); !errors.Is(err, ErrChatWorkDenied) {
		t.Fatalf("err = %v", err)
	}
	w.journeys.err = workspace.ErrJourneyUnknown
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3998Ask("fin", "rev-1", "key-00001")); !errors.Is(err, ErrChatWorkNotFound) {
		t.Fatalf("a case the viewer cannot open: err = %v", err)
	}
	if len(w.chat.sent) != 0 || len(w.store.rows) != 0 || w.chat.readsOfRoom != 0 {
		t.Fatal("a refused ask touched a conversation or wrote")
	}
}

func TestIssue3998_AConversationTheReviewerCannotOpenIsRefusedWithoutRevealingIt_Security(t *testing.T) {
	w := issue3998Build()
	w.chat.unreadable["fin"] = true
	for _, id := range []string{"fin", "nowhere"} {
		a := issue3998Ask(id, "rev-1", "key-00001")
		_, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), a)
		if !errors.Is(err, ErrInputRequestConversation) {
			t.Fatalf("%s: err = %v", id, err)
		}
	}
	w.chat.rooms = append(w.chat.rooms, chat.Conversation{ID: "old", Kind: chat.Group, Archived: true})
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3998Ask("old", "rev-1", "key-00001")); !errors.Is(err, ErrInputRequestConversation) {
		t.Fatalf("an archived conversation: err = %v", err)
	}
	if len(w.chat.sent) != 0 || len(w.store.rows) != 0 {
		t.Fatal("a refused ask wrote")
	}
}

func TestIssue3998_ARoomThatRefusesThePostWritesNoRecord(t *testing.T) {
	w := issue3998Build()
	w.chat.sendErr = chat.ErrPermissionDenied
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3998Ask("fin", "rev-1", "key-00001")); !errors.Is(err, ErrInputRequestConversation) {
		t.Fatalf("err = %v", err)
	}
	if len(w.store.rows) != 0 {
		t.Fatal("a record of a question that was never posted")
	}
}

func TestIssue3998_AChangedProposalAndAClosedCaseAreRefusedBeforeAnyWrite(t *testing.T) {
	w := issue3998Build()
	w.journeys.detail.Summary.ProposalRevisionID = "rev-2"
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3998Ask("fin", "rev-1", "key-00001")); !errors.Is(err, ErrInputRequestStale) {
		t.Fatalf("stale: err = %v", err)
	}
	prev := issue3998Ask("fin", "rev-1", "")
	prev.Preview = true
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), prev); !errors.Is(err, ErrInputRequestStale) {
		t.Fatalf("a preview of a stale revision: err = %v", err)
	}
	for _, stage := range []workspace.JourneyStage{workspace.JourneyStageCompleted, workspace.JourneyStageRejected} {
		w.journeys.detail.Summary.Stage = stage
		if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3998Ask("fin", "rev-2", "key-00001")); !errors.Is(err, ErrInputRequestClosed) {
			t.Fatalf("%v: err = %v", stage, err)
		}
	}
	if len(w.chat.sent) != 0 || len(w.store.rows) != 0 {
		t.Fatal("a refused ask wrote")
	}
}

func TestIssue3998_ARetryWithTheSameKeySendsOneQuestion(t *testing.T) {
	w := issue3998Build()
	first, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3998Ask("fin", "rev-1", "key-00001"))
	if err != nil || !first.Created {
		t.Fatal(first, err)
	}
	// Even after the proposal moved on, the retry returns the first request.
	w.journeys.detail.Summary.ProposalRevisionID = "rev-2"
	again, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3998Ask("fin", "rev-1", "key-00001"))
	if err != nil || again.Created || again.Item == nil || again.Item.ID != first.Item.ID || again.Item.State != "outdated" {
		t.Fatalf("retry = %+v, %v", again, err)
	}
	other := issue3998Ask("fin", "rev-1", "key-00001")
	other.Question = "A different question?"
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), other); !errors.Is(err, ErrChatWorkConflict) {
		t.Fatalf("same key, other words: err = %v", err)
	}
	if len(w.chat.sent) != 1 || len(w.store.rows) != 1 {
		t.Fatalf("%d posts, %d records", len(w.chat.sent), len(w.store.rows))
	}
}

func TestIssue3998_TheQuestionAndTheStampAreValidatedBeforeAnyRead(t *testing.T) {
	w := issue3998Build()
	bad := map[string]func(*InputRequestAsk){
		"empty question":        func(a *InputRequestAsk) { a.Question = "  " },
		"long question":         func(a *InputRequestAsk) { a.Question = strings.Repeat("x", 1001) },
		"control character":     func(a *InputRequestAsk) { a.Question = "Why\x00?" },
		"no informal label":     func(a *InputRequestAsk) { a.Label = " " },
		"stamp of other copy":   func(a *InputRequestAsk) { a.RevisionLabel = "Proposal revision rev-7" },
		"no conversation":       func(a *InputRequestAsk) { a.ConversationID = "" },
		"short idempotency key": func(a *InputRequestAsk) { a.IdempotencyKey = "k" },
	}
	for name, mutate := range bad {
		a := issue3998Ask("fin", "rev-1", "key-00001")
		mutate(&a)
		if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), a); !errors.Is(err, ErrChatWorkInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if w.journeys.inspects != 0 || len(w.chat.sent) != 0 {
		t.Fatal("an invalid ask reached the case or a conversation")
	}
}

// The reviewer's list says whether each question is still current, and a reply
// or a lost conversation never turns old advice into current advice.
func TestIssue3998_AdviceThatIsNotCurrentIsSaidSoAndNeverTreatedAsAnApproval(t *testing.T) {
	w := issue3998Build()
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3998Ask("fin", "rev-1", "key-00001")); err != nil {
		t.Fatal(err)
	}
	view, err := w.svc.View(context.Background(), issue3790Principal(t, "morgan"), "j1")
	if err != nil || len(view.Items) != 1 || view.Items[0].State != "current" || view.Items[0].Kind != "informal" || view.Items[0].ConversationName != "finance-review" {
		t.Fatalf("view = %+v, %v", view, err)
	}
	w.journeys.detail.Summary.ProposalRevisionID = "rev-2"
	view, _ = w.svc.View(context.Background(), issue3790Principal(t, "morgan"), "j1")
	if view.Items[0].State != "outdated" || !view.CanAsk || view.Revision != "rev-2" {
		t.Fatalf("after the proposal changed: %+v", view)
	}
	w.chat.unreadable["fin"] = true
	view, _ = w.svc.View(context.Background(), issue3790Principal(t, "morgan"), "j1")
	if view.Items[0].State != "unavailable" || view.Items[0].Href != "" || view.Items[0].ConversationID != "" || view.Items[0].ConversationName != "" {
		t.Fatalf("a conversation the reviewer lost is still named or current: %+v", view.Items[0])
	}
	w.journeys.detail.Summary.Stage = workspace.JourneyStageCompleted
	view, _ = w.svc.View(context.Background(), issue3790Principal(t, "morgan"), "j1")
	if view.CanAsk || view.Items[0].State != "closed" {
		t.Fatalf("after the case closed: %+v", view)
	}
}

func TestIssue3998_AnotherReviewersRequestsAreNeverListed_Security(t *testing.T) {
	w := issue3998Build()
	if _, err := w.svc.Ask(context.Background(), issue3790Principal(t, "morgan"), issue3998Ask("fin", "rev-1", "key-00001")); err != nil {
		t.Fatal(err)
	}
	view, err := w.svc.View(context.Background(), issue3790Principal(t, "casey"), "j1")
	if err != nil || len(view.Items) != 0 {
		t.Fatalf("casey sees %+v, %v", view, err)
	}
}

// ---- routes ---------------------------------------------------------------

type issue3998Port struct{ svc *InputRequests }

func (p issue3998Port) View(ctx context.Context, who *trust.Principal, id string) (InputRequestView, error) {
	return p.svc.View(ctx, who, id)
}

func (p issue3998Port) Ask(ctx context.Context, who *trust.Principal, r InputRequestAsk) (InputRequestAsked, error) {
	return p.svc.Ask(ctx, who, r)
}

func issue3998Serve(t *testing.T, w *issue3998World, who, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	handler := InputRequestsHandler{Requests: issue3998Port{w.svc}}
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req = req.WithContext(trust.WithPrincipal(req.Context(), issue3790Principal(t, who)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestIssue3998_RoutesAnswerWithTheStatusesTheClientReads(t *testing.T) {
	w := issue3998Build()
	path := "/api/chat/journeys/j1/input-request"
	rec := issue3998Serve(t, w, "morgan", http.MethodGet, path, "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Body.String(), `"can_ask":true`) {
		t.Fatalf("get = %d %s", rec.Code, rec.Body.String())
	}
	base := `"revision":"rev-1","question":"Is this in band?","conversation_id":"fin","label":"Informal input, not an approval","revision_label":"Proposal revision rev-1"`
	rec = issue3998Serve(t, w, "morgan", http.MethodPost, path, `{`+base+`,"preview":true}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"preview":true`) || len(w.chat.sent) != 0 {
		t.Fatalf("preview = %d %s", rec.Code, rec.Body.String())
	}
	send := `{` + base + `,"idempotency_key":"key-00001"}`
	rec = issue3998Serve(t, w, "morgan", http.MethodPost, path, send)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"kind":"informal"`) {
		t.Fatalf("send = %d %s", rec.Code, rec.Body.String())
	}
	if rec = issue3998Serve(t, w, "morgan", http.MethodPost, path, send); rec.Code != http.StatusOK {
		t.Fatalf("retry = %d %s", rec.Code, rec.Body.String())
	}
	for name, tc := range map[string]struct {
		who, method, path, body string
		status                  int
		code                    string
	}{
		"not the approver":    {"casey", http.MethodPost, path, send, http.StatusForbidden, "permission_denied"},
		"unknown case":        {"morgan", http.MethodPost, "/api/chat/journeys/zz/input-request", send, http.StatusNotFound, "not_found"},
		"changed":             {"morgan", http.MethodPost, path, strings.ReplaceAll(strings.ReplaceAll(send, "rev-1", "rev-0"), "key-00001", "key-00002"), http.StatusConflict, "revision_changed"},
		"unusable room":       {"morgan", http.MethodPost, path, strings.ReplaceAll(strings.ReplaceAll(send, `"fin"`, `"zzz"`), "key-00001", "key-00003"), http.StatusUnprocessableEntity, "no_conversation"},
		"name into a channel": {"morgan", http.MethodPost, path, strings.ReplaceAll(strings.ReplaceAll(send, `"fin"`, `"hr"`), "key-00001", "key-00004") + "", http.StatusUnprocessableEntity, "name_in_public"},
		"unknown field":       {"morgan", http.MethodPost, path, `{` + base + `,"idempotency_key":"key-00005","approver":"x"}`, http.StatusBadRequest, "invalid_argument"},
		"wrong method":        {"morgan", http.MethodDelete, path, "", http.StatusMethodNotAllowed, "method_not_allowed"},
	} {
		body := tc.body
		if name == "name into a channel" {
			body = strings.Replace(body, `"idempotency_key"`, `"include_case_name":true,"idempotency_key"`, 1)
		}
		rec := issue3998Serve(t, w, tc.who, tc.method, tc.path, body)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestIssue3998_RouteRefusesARequestWithNoSignedInPerson_Security(t *testing.T) {
	w := issue3998Build()
	rec := httptest.NewRecorder()
	InputRequestsHandler{Requests: issue3998Port{w.svc}}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/chat/journeys/j1/input-request", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}
