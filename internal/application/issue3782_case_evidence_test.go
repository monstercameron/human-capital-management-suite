package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// evidenceStoreFake keeps the attachments in memory and answers the binding.
type evidenceStoreFake struct {
	caseStoreFake
	excerpts []chat.CaseEvidenceExcerpt
	attached int
}

func (f *evidenceStoreFake) AttachCaseEvidence(_ context.Context, tenant string, e chat.CaseEvidenceExcerpt) (chat.CaseEvidenceExcerpt, error) {
	if !f.bound || f.binding.CaseRef != e.CaseRef {
		return chat.CaseEvidenceExcerpt{}, chat.ErrCaseEvidenceCaseChanged
	}
	f.attached++
	e.TenantID, e.AttachedAt = tenant, time.Unix(1_700_000_000, 0).UTC()
	for i := range e.Items {
		e.Items[i].ExcerptID, e.Items[i].Ordinal, e.Items[i].AttachedAt = e.ID, i, e.AttachedAt
		e.Items[i].CaseRef, e.Items[i].CaseRevision, e.Items[i].Note, e.Items[i].AttachedBy = e.CaseRef, e.CaseRevision, e.Note, e.AttachedBy
	}
	f.excerpts = append(f.excerpts, e)
	return e, nil
}

func (f *evidenceStoreFake) CaseEvidence(_ context.Context, _, _, ref string) ([]chat.CaseEvidenceExcerpt, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []chat.CaseEvidenceExcerpt
	for _, e := range f.excerpts {
		if e.CaseRef == ref {
			out = append(out, e)
		}
	}
	return out, nil
}

// evidenceRoom opens "thread" and the posts a person may read.
type evidenceRoom struct {
	readers map[string]bool
	posts   map[string]chat.Post
	// hidden lists "person/post" pairs the person may not read.
	hidden map[string]bool
}

func (r *evidenceRoom) ReadAuthorizedReference(_ context.Context, p chat.Principal, tenant, conversation, post string) (chat.Conversation, *chat.Post, error) {
	if conversation != "thread" {
		return chat.Conversation{}, nil, chat.ErrNotFound
	}
	if !r.readers[p.SubjectID] {
		return chat.Conversation{}, nil, chat.ErrPermissionDenied
	}
	c := chat.Conversation{ID: "thread", TenantID: tenant}
	if post == "" {
		return c, nil, nil
	}
	got, ok := r.posts[post]
	if !ok || got.Deleted || r.hidden[p.SubjectID+"/"+post] {
		return chat.Conversation{}, nil, chat.ErrNotFound
	}
	return c, &got, nil
}

func evidenceApp(store *evidenceStoreFake, room *evidenceRoom, admin string) chatCaseEvidenceApplication {
	n := 0
	return chatCaseEvidenceApplication{Store: store, Reader: room, NewID: func() string { n++; return "id" + string(rune('0'+n)) },
		Admin: func(context.Context, string) (string, error) {
			if admin == "" {
				return "", chat.ErrPermissionDenied
			}
			return admin, nil
		}}
}

func evidenceFixture() (*evidenceStoreFake, *evidenceRoom) {
	store := &evidenceStoreFake{}
	store.bound, store.binding = true, chat.CaseBinding{ConversationID: "thread", CaseRef: "ER-2041", OwnerID: "hr", Restricted: true}
	room := &evidenceRoom{readers: map[string]bool{"hr": true, "emp": true, "admin": true, "late": true}, hidden: map[string]bool{"late/m1": true},
		posts: map[string]chat.Post{
			"m1": {ID: "m1", ConversationID: "thread", AuthorID: "emp", Body: "I asked on Monday", Revision: 1, Sequence: 1, CreatedAt: time.Unix(1_699_000_000, 0).UTC()},
			"m2": {ID: "m2", ConversationID: "thread", AuthorID: "mgr", Body: "Fine by me", Revision: 1, Sequence: 2, CreatedAt: time.Unix(1_699_000_100, 0).UTC()},
		}}
	return store, room
}

func TestIssue3782_OwnerAttachesReviewedMessages(t *testing.T) {
	store, room := evidenceFixture()
	app := evidenceApp(store, room, "admin")
	hr := chat.Principal{TenantID: "t", SubjectID: "hr"}
	// Selected out of order: the copy keeps the conversation's order.
	view, err := app.Attach(t.Context(), hr, ChatCaseEvidenceAttach{ConversationID: "thread", CaseRef: "ER-2041", CaseRevision: " rev-3 ", Note: " the request ", MessageIDs: []string{"m2", "m1"}})
	if err != nil || len(view.Excerpts) != 1 || len(view.Excerpts[0].Items) != 2 {
		t.Fatalf("attach = %+v err=%v", view, err)
	}
	e := store.excerpts[0]
	if e.AttachedBy != "hr" || e.CaseRef != "ER-2041" || e.CaseRevision != "rev-3" || e.Note != "the request" || e.Items[0].MessageID != "m1" || e.Items[1].MessageID != "m2" {
		t.Fatalf("stored = %+v; the attacher is the verified caller and the order is the conversation's", e)
	}
	first := e.Items[0]
	if first.Body != "I asked on Monday" || first.ContentHash != chat.EvidenceContentHash("I asked on Monday") || first.SourceRevision != 1 || first.AuthorID != "emp" {
		t.Fatalf("frozen item = %+v", first)
	}
	item := view.Excerpts[0].Items[0]
	if item.Status != chat.EvidenceSourceCurrent || item.Body != "I asked on Monday" || item.AuthorID != "emp" || !view.CanAttach || !view.Bound || view.CaseRef != "ER-2041" {
		t.Fatalf("view item = %+v", item)
	}
	// An administrator who can open the conversation may attach too.
	if _, err := app.Attach(t.Context(), chat.Principal{TenantID: "t", SubjectID: "admin"}, ChatCaseEvidenceAttach{ConversationID: "thread", CaseRef: "ER-2041", MessageIDs: []string{"m1"}}); err != nil {
		t.Fatalf("administrator attach: %v", err)
	}
}

func TestIssue3782_AttachRefusals_Security(t *testing.T) {
	store, room := evidenceFixture()
	app := evidenceApp(store, room, "admin")
	attach := func(who string, mutate func(*ChatCaseEvidenceAttach)) error {
		in := ChatCaseEvidenceAttach{ConversationID: "thread", CaseRef: "ER-2041", MessageIDs: []string{"m1"}}
		if mutate != nil {
			mutate(&in)
		}
		_, err := app.Attach(t.Context(), chat.Principal{TenantID: "t", SubjectID: who}, in)
		return err
	}
	// A participant who owns nothing may not file evidence.
	if err := attach("emp", nil); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("participant: %v", err)
	}
	// A person who cannot open the conversation is not told it exists.
	if err := attach("stranger", nil); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("stranger: %v", err)
	}
	// The case reviewed is not the one in force.
	if err := attach("hr", func(in *ChatCaseEvidenceAttach) { in.CaseRef = "ER-9999" }); !errors.Is(err, chat.ErrCaseEvidenceCaseChanged) {
		t.Fatalf("wrong case: %v", err)
	}
	store.bound = false
	if err := attach("admin", nil); !errors.Is(err, chat.ErrCaseEvidenceCaseChanged) {
		t.Fatalf("no case in force: %v", err)
	}
	// With no case in force nobody owns one, so only an administrator is told so.
	if err := attach("hr", nil); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("former owner with no case in force: %v", err)
	}
	store.bound = true
	// A message the owner cannot read is not frozen, and reads as absent.
	room.hidden["hr/m2"] = true
	if err := attach("hr", func(in *ChatCaseEvidenceAttach) { in.MessageIDs = []string{"m1", "m2"} }); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("unreadable message: %v", err)
	}
	if err := attach("hr", func(in *ChatCaseEvidenceAttach) { in.MessageIDs = []string{"nope"} }); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("missing message: %v", err)
	}
	many := make([]string, chat.MaxEvidenceMessages+1)
	for i := range many {
		many[i] = "m" + string(rune('a'+i))
	}
	for name, mutate := range map[string]func(*ChatCaseEvidenceAttach){
		"no messages": func(in *ChatCaseEvidenceAttach) { in.MessageIDs = nil },
		"duplicates":  func(in *ChatCaseEvidenceAttach) { in.MessageIDs = []string{"m1", "m1"} },
		"blank id":    func(in *ChatCaseEvidenceAttach) { in.MessageIDs = []string{" "} },
		"too many":    func(in *ChatCaseEvidenceAttach) { in.MessageIDs = many },
		"long note":   func(in *ChatCaseEvidenceAttach) { in.Note = strings.Repeat("n", chat.MaxEvidenceNoteRunes+1) },
		"long rev": func(in *ChatCaseEvidenceAttach) {
			in.CaseRevision = strings.Repeat("r", chat.MaxEvidenceRevisionRunes+1)
		},
		"no case": func(in *ChatCaseEvidenceAttach) { in.CaseRef = " " },
	} {
		if err := attach("hr", mutate); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if store.attached != 0 {
		t.Fatalf("a refused request wrote %d attachments", store.attached)
	}
}

func TestIssue3782_EvidenceIsRevalidatedOnEveryView_Security(t *testing.T) {
	store, room := evidenceFixture()
	app := evidenceApp(store, room, "admin")
	hr := chat.Principal{TenantID: "t", SubjectID: "hr"}
	if _, err := app.Attach(t.Context(), hr, ChatCaseEvidenceAttach{ConversationID: "thread", CaseRef: "ER-2041", MessageIDs: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	view := func(who string) ChatCaseEvidenceView {
		v, err := app.Evidence(t.Context(), chat.Principal{TenantID: "t", SubjectID: who}, "thread")
		if err != nil {
			t.Fatalf("%s: %v", who, err)
		}
		return v
	}
	if v := view("emp"); v.CanAttach || len(v.Excerpts) != 1 || v.Excerpts[0].Items[0].Status != chat.EvidenceSourceCurrent {
		t.Fatalf("participant view = %+v", v)
	}
	// Edited since: the frozen text is shown and marked.
	edited := room.posts["m2"]
	edited.Body, edited.Revision = "Not fine by me", 2
	room.posts["m2"] = edited
	if item := view("emp").Excerpts[0].Items[1]; item.Status != chat.EvidenceSourceEdited || item.Body != "Fine by me" {
		t.Fatalf("edited item = %+v; want the frozen text marked edited", item)
	}
	// A reader who cannot open the original sees neither text, author nor fingerprint.
	item := view("late").Excerpts[0].Items[0]
	if item.Status != chat.EvidenceSourceGone || item.Body != "" || item.AuthorID != "" || item.Fingerprint != "" || item.MessageID != "" {
		t.Fatalf("unreadable item leaked: %+v", item)
	}
	// Deleted: no reader is shown the text any more.
	deleted := room.posts["m1"]
	deleted.Deleted = true
	room.posts["m1"] = deleted
	for _, who := range []string{"hr", "emp", "admin"} {
		if it := view(who).Excerpts[0].Items[0]; it.Status != chat.EvidenceSourceGone || it.Body != "" {
			t.Fatalf("%s sees a deleted source: %+v", who, it)
		}
	}
	// A person outside the conversation reads nothing, and a case that is lifted shows nothing.
	if _, err := app.Evidence(t.Context(), chat.Principal{TenantID: "t", SubjectID: "stranger"}, "thread"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("stranger: %v", err)
	}
	store.bound = false
	if v := view("hr"); v.Bound || len(v.Excerpts) != 0 || v.CanAttach {
		t.Fatalf("lifted case view = %+v", v)
	}
	store.bound, store.err = true, errors.New("db down")
	if _, err := app.Evidence(t.Context(), hr, "thread"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("store failure: %v", err)
	}
}

func TestIssue3782_ViewNeverClaimsAnApprovalOrOutcome(t *testing.T) {
	// The view type has no field for a decision, a policy version or an outcome:
	// those come from the owning service. This pins the JSON the page is given.
	store, room := evidenceFixture()
	app := evidenceApp(store, room, "")
	hr := chat.Principal{TenantID: "t", SubjectID: "hr"}
	view, err := app.Attach(t.Context(), hr, ChatCaseEvidenceAttach{ConversationID: "thread", CaseRef: "ER-2041", MessageIDs: []string{"m1"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"approv", "outcome", "policy", "decision", "decided"} {
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			t.Fatalf("view claims %q: %s", banned, raw)
		}
	}
}

type evidenceServiceFake struct{ calls []string }

func (f *evidenceServiceFake) Evidence(_ context.Context, _ chat.Principal, c string) (ChatCaseEvidenceView, error) {
	f.calls = append(f.calls, "list:"+c)
	if c == "hidden" {
		return ChatCaseEvidenceView{}, chat.ErrNotFound
	}
	return ChatCaseEvidenceView{Bound: true, CaseRef: "ER-1"}, nil
}

func (f *evidenceServiceFake) Attach(_ context.Context, _ chat.Principal, in ChatCaseEvidenceAttach) (ChatCaseEvidenceView, error) {
	f.calls = append(f.calls, "attach:"+in.ConversationID+":"+strings.Join(in.MessageIDs, ","))
	switch in.ConversationID {
	case "denied":
		return ChatCaseEvidenceView{}, chat.ErrPermissionDenied
	case "changed":
		return ChatCaseEvidenceView{}, chat.ErrCaseEvidenceCaseChanged
	}
	return ChatCaseEvidenceView{Bound: true, CaseRef: in.CaseRef}, nil
}

func TestIssue3782_EvidenceHTTPShapesAndRefusals(t *testing.T) {
	svc := &evidenceServiceFake{}
	h := ChatCaseEvidenceHTTP{Service: svc}
	for _, tc := range []struct {
		name, path, body string
		signedIn         bool
		kind             trust.SubjectKind
		status           int
		contains         string
	}{
		{"list", ChatCaseEvidencePath + "list", `{"conversation_id":"thread"}`, true, trust.SubjectKindHuman, 200, `"case_ref":"ER-1"`},
		{"list hidden", ChatCaseEvidencePath + "list", `{"conversation_id":"hidden"}`, true, trust.SubjectKindHuman, 404, `not_found`},
		{"attach", ChatCaseEvidencePath + "attach", `{"conversation_id":"thread","case_ref":"ER-9","message_ids":["m1"]}`, true, trust.SubjectKindHuman, 200, `"case_ref":"ER-9"`},
		{"attach refused", ChatCaseEvidencePath + "attach", `{"conversation_id":"denied","case_ref":"x","message_ids":["m1"]}`, true, trust.SubjectKindHuman, 403, `permission_denied`},
		{"case changed", ChatCaseEvidencePath + "attach", `{"conversation_id":"changed","case_ref":"x","message_ids":["m1"]}`, true, trust.SubjectKindHuman, 409, `case_changed`},
		{"unknown field", ChatCaseEvidencePath + "attach", `{"conversation_id":"thread","body":"forged text"}`, true, trust.SubjectKindHuman, 400, `invalid_request`},
		{"unknown action", ChatCaseEvidencePath + "purge", `{}`, true, trust.SubjectKindHuman, 404, `not_found`},
		{"signed out", ChatCaseEvidencePath + "list", `{"conversation_id":"thread"}`, false, trust.SubjectKindHuman, 403, `permission_denied`},
		{"agent", ChatCaseEvidencePath + "attach", `{"conversation_id":"thread","case_ref":"x","message_ids":["m1"]}`, true, trust.SubjectKindAgent, 403, `permission_denied`},
	} {
		rec := caseRequest(h, "POST", tc.path, tc.body, tc.signedIn, tc.kind)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.contains) {
			t.Errorf("%s: status=%d body=%s; want %d containing %q", tc.name, rec.Code, rec.Body.String(), tc.status, tc.contains)
		}
	}
	for _, call := range svc.calls {
		if strings.Contains(call, "forged") {
			t.Fatalf("a client-supplied body reached the service: %v", svc.calls)
		}
	}
	if rec := caseRequest(h, "GET", ChatCaseEvidencePath+"list", ``, true, trust.SubjectKindHuman); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d", rec.Code)
	}
}
