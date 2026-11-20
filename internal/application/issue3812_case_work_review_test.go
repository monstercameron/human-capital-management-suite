package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// The served chat reader is a decorator chain. If it does not carry the case
// question and the work conversion trusts it to, a restricted case conversation
// would silently turn into project tasks.
func TestIssue3812_ServedChatReaderDoesNotCarryTheCaseQuestion_Security(t *testing.T) {
	var served any = &streamingChatService{}
	if _, ok := served.(chatCaseRestrictor); ok {
		t.Skip("the served chat reader now carries the case question itself")
	}
	restricted := workRoom{err: &chat.CaseRestrictionError{Action: chat.CaseActionSave}}
	w := &ChatMessageWork{chat: &auditedChatService{}, cases: restricted}
	if err := w.caseRestriction(t.Context(), "t", "thread"); !errors.Is(err, ErrChatWorkCaseRestricted) {
		t.Fatalf("restricted case behind the served reader: err = %v; want %v", err, ErrChatWorkCaseRestricted)
	}
	// The composition names the core service; without it nothing here could refuse.
	open := &ChatMessageWork{chat: &auditedChatService{}, cases: workRoom{}}
	if err := open.caseRestriction(t.Context(), "t", "thread"); err != nil {
		t.Fatalf("unbound conversation: err = %v", err)
	}
}

func TestIssue3812_AppInstallInBoundConversationNeedsTheCaseOwner_Security(t *testing.T) {
	for name, tc := range map[string]struct {
		cases   ChatCaseService
		wantErr bool
	}{
		"no case surface": {nil, false},
		"unbound":         {&caseViewFake{view: ChatCaseView{}}, false},
		"other manager":   {&caseViewFake{view: ChatCaseView{Bound: true, CaseRef: "ER-1", Restricted: true}}, true},
		"case owner":      {&caseViewFake{view: ChatCaseView{Bound: true, OwnerIsMe: true}}, false},
	} {
		err := (&ChatExtensions{Cases: tc.cases}).caseAllowsAppInstall(t.Context(), chat.Principal{TenantID: "t", SubjectID: "m"}, "thread")
		if tc.wantErr {
			if _, ok := chat.CaseReason(err); !ok || !errors.Is(err, chat.ErrPermissionDenied) {
				t.Errorf("%s: err = %v; want a case refusal", name, err)
			}
		} else if err != nil {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

type caseViewFake struct{ view ChatCaseView }

func (f *caseViewFake) Case(context.Context, chat.Principal, string) (ChatCaseView, error) {
	return f.view, nil
}
func (f *caseViewFake) Bind(context.Context, chat.Principal, string, string, string, bool) (ChatCaseView, error) {
	return f.view, nil
}
func (f *caseViewFake) Unbind(context.Context, chat.Principal, string) (ChatCaseView, error) {
	return f.view, nil
}
