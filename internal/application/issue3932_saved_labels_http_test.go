package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3932Port struct {
	chatsaveHTTPPort
	renamed, removed []string
	changed          int
}

func (f *issue3932Port) RenameSavedLabel(_ context.Context, p chat.Principal, tenant, from, to string) (int, error) {
	f.calls++
	f.renamed = []string{p.SubjectID, tenant, from, to}
	return f.changed, f.err
}

func (f *issue3932Port) RemoveSavedLabel(_ context.Context, p chat.Principal, tenant, label string) (int, error) {
	f.calls++
	f.removed = []string{p.SubjectID, tenant, label}
	return f.changed, f.err
}

func issue3932Post(h SavedMessagesHandler, ctx context.Context, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, SavedMessagesPath, strings.NewReader(body)).WithContext(ctx))
	return w
}

func TestIssue3932_LabelCommands(t *testing.T) {
	ctx := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	port := &issue3932Port{changed: 3}
	h := SavedMessagesHandler{Service: port}
	if w := issue3932Post(h, ctx, `{"action":"labels","conversation_id":"room","post_id":"post","add_labels":["Hiring"],"remove_labels":["Old"]}`); w.Code != 200 {
		t.Fatalf("labels=%d %s", w.Code, w.Body.String())
	}
	if !reflect.DeepEqual(port.change.AddLabels, []string{"Hiring"}) || !reflect.DeepEqual(port.change.RemoveLabels, []string{"Old"}) || port.request.PostID != "post" || port.request.Principal.SubjectID != "person" {
		t.Fatalf("change=%+v request=%+v", port.change, port.request)
	}
	w := issue3932Post(h, ctx, `{"action":"rename_label","label":"Hiring","new_label":"Recruiting"}`)
	var reply struct{ Changed int }
	_ = json.Unmarshal(w.Body.Bytes(), &reply)
	if w.Code != 200 || reply.Changed != 3 || !reflect.DeepEqual(port.renamed, []string{"person", "tenant-a", "Hiring", "Recruiting"}) {
		t.Fatalf("rename=%d %s %v", w.Code, w.Body.String(), port.renamed)
	}
	if w := issue3932Post(h, ctx, `{"action":"remove_label","label":"Recruiting"}`); w.Code != 200 || !reflect.DeepEqual(port.removed, []string{"person", "tenant-a", "Recruiting"}) {
		t.Fatalf("remove=%d %v", w.Code, port.removed)
	}
}

func TestIssue3932_LabelCommands_Security(t *testing.T) {
	port := &issue3932Port{}
	h := SavedMessagesHandler{Service: port}
	for _, kind := range []trust.SubjectKind{trust.SubjectKindAgent, trust.SubjectKindService} {
		if w := issue3932Post(h, chatsaveHTTPContext(t, kind), `{"action":"remove_label","label":"x"}`); w.Code != 401 && w.Code != 403 {
			t.Fatalf("machine caller accepted: %d", w.Code)
		}
	}
	if w := issue3932Post(h, context.Background(), `{"action":"remove_label","label":"x"}`); w.Code != 401 {
		t.Fatalf("anonymous accepted: %d", w.Code)
	}
	if port.calls != 0 {
		t.Fatal("side effect before authorization")
	}
	ctx := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	// An empty change and a label that names someone else's person are refused or ignored.
	if w := issue3932Post(h, ctx, `{"action":"labels","conversation_id":"room","post_id":"post"}`); w.Code != 400 {
		t.Fatalf("empty labels change=%d", w.Code)
	}
	if w := issue3932Post(h, ctx, `{"action":"remove_label","label":"x","person_id":"victim"}`); w.Code != 400 {
		t.Fatalf("a target person must be refused: %d", w.Code)
	}
	port.err = chat.ErrSavedLabelLimit
	w := issue3932Post(h, ctx, `{"action":"labels","conversation_id":"room","post_id":"post","add_labels":["a"]}`)
	var body SavedMessagesError
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 409 || body.Code != "label_limit" {
		t.Fatalf("limit=%d %s", w.Code, w.Body.String())
	}
	// A port without label-wide commands answers unavailable, never succeeds.
	plain := SavedMessagesHandler{Service: &chatsaveHTTPPort{}}
	if w := issue3932Post(plain, ctx, `{"action":"remove_label","label":"x"}`); w.Code != 503 {
		t.Fatalf("port without labels=%d", w.Code)
	}
}
