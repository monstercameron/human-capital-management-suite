package chatui

import (
	"strings"
	"testing"
	"time"
)

func issue3815Model(locale, user string) Model {
	m := chatux021Model()
	m.Locale = locale
	m.CurrentUser = user
	m.Members = append(m.Members, Member{ID: "maria", HomeTenantID: "t", Name: "Maria Lopez"}, Member{ID: "sam", HomeTenantID: "t", Name: "Sam Reed"}, Member{ID: "hr", HomeTenantID: "t", Name: "Hana Roth"})
	return m
}

func issue3815Message() Message {
	return Message{ID: "policy", AuthorID: "hr", Author: "Hana Roth", Body: "New safety policy: wear a helmet on site.", SentAt: time.Date(2026, 11, 19, 9, 41, 0, 0, time.UTC)}
}

func issue3815Menu(t *testing.T, m Model, msg Message) string {
	t.Helper()
	var out strings.Builder
	for _, n := range chatux022MenuItems(m, msg) {
		out.WriteString(renderNode(t, n))
	}
	return out.String()
}

func TestIssue3815_MemberGetsOneAcknowledgeButton(t *testing.T) {
	m := issue3815Model("en-US", "maria")
	m.Acks.Views = map[string]AckView{"policy": {Loaded: true, CanAcknowledge: true}}
	got := renderNode(t, message(m, handlers{}, issue3815Message(), false))
	for _, want := range []string{"Acknowledgement required", `data-ack-form="policy"`, `type="submit"`, "I have read and understood this"} {
		if !strings.Contains(got, want) {
			t.Errorf("member row lacks %q: %s", want, got)
		}
	}
	// Reading is not acknowledging: nothing on the row says it was.
	if strings.Contains(got, "You acknowledged") {
		t.Errorf("an unacknowledged post reads as acknowledged: %s", got)
	}
	// A member is not shown the roll.
	if strings.Contains(got, "not acknowledged") || strings.Contains(got, "Hana") && strings.Contains(got, "<details") {
		t.Errorf("member sees the roll: %s", got)
	}
}

func TestIssue3815_AcknowledgedPostShowsWhenAndOffersNoButton(t *testing.T) {
	m := issue3815Model("en-US", "maria")
	m.Acks.Views = map[string]AckView{"policy": {Loaded: true, Acknowledged: true, AcknowledgedAt: time.Date(2026, 11, 19, 10, 5, 0, 0, time.UTC)}}
	got := renderNode(t, message(m, handlers{}, issue3815Message(), false))
	if !strings.Contains(got, "You acknowledged this") || !strings.Contains(got, `role="status"`) || strings.Contains(got, "data-ack-form") {
		t.Errorf("acknowledged row: %s", got)
	}
}

func TestIssue3815_BusyAcknowledgeCannotBeSentTwice(t *testing.T) {
	m := issue3815Model("en-US", "maria")
	m.Acks.Views = map[string]AckView{"policy": {Loaded: true, CanAcknowledge: true, Busy: true}}
	got := renderNode(t, message(m, handlers{}, issue3815Message(), false))
	if !strings.Contains(got, "Saving...") || !strings.Contains(got, `aria-busy="true"`) || !strings.Contains(got, "disabled") {
		t.Errorf("busy row: %s", got)
	}
}

func TestIssue3815_FailureIsAnnouncedInWords(t *testing.T) {
	m := issue3815Model("en-US", "maria")
	m.Acks.Views = map[string]AckView{"policy": {Loaded: true, CanAcknowledge: true, Notice: "failed"}}
	got := renderNode(t, message(m, handlers{}, issue3815Message(), false))
	if !strings.Contains(got, "could not be saved") || !strings.Contains(got, `role="alert"`) || !strings.Contains(got, "data-ack-form") {
		t.Errorf("failed row keeps the button and says why: %s", got)
	}
}

func TestIssue3815_SenderSeesWhoIsPending(t *testing.T) {
	m := issue3815Model("en-US", "hr")
	m.Acks.Views = map[string]AckView{"policy": {Loaded: true, CanManage: true, Total: 4, Count: 1, Pending: []string{"maria", "sam", "ghost"}, PendingMore: 0}}
	got := renderNode(t, message(m, handlers{}, issue3815Message(), true))
	for _, want := range []string{"1 of 4 acknowledged", "3 pending", "<details", "<summary", "Maria Lopez", "Sam Reed", "1 more"} {
		if !strings.Contains(got, want) {
			t.Errorf("manager row lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "ghost") {
		t.Errorf("an id with no name was shown: %s", got)
	}
	if strings.Contains(got, "data-ack-form") {
		t.Errorf("the sender is asked to acknowledge their own post: %s", got)
	}
}

func TestIssue3815_NothingIsDrawnForAPostThatRequiresNothing(t *testing.T) {
	m := issue3815Model("en-US", "maria")
	got := renderNode(t, message(m, handlers{}, issue3815Message(), false))
	if strings.Contains(got, "Acknowledgement required") || strings.Contains(got, "data-ack") {
		t.Errorf("plain post: %s", got)
	}
}

func TestIssue3815_MenuOffersRequireOnlyWhereTheServerAllowsIt(t *testing.T) {
	msg := issue3815Message()
	m := issue3815Model("en-US", "hr")
	if got := issue3815Menu(t, m, msg); strings.Contains(got, "Require acknowledgement") {
		t.Errorf("offered before the server said so: %s", got)
	}
	m.Acks.CanRequire, m.Acks.Require = true, func(string) {}
	if got := issue3815Menu(t, m, msg); !strings.Contains(got, `data-action="ack-require"`) || !strings.Contains(got, "Require acknowledgement") {
		t.Errorf("sender is not offered it: %s", got)
	}
	// Not on somebody else's post, nor on one already marked.
	other := issue3815Model("en-US", "maria")
	other.Acks.CanRequire, other.Acks.Require = true, func(string) {}
	if got := issue3815Menu(t, other, msg); strings.Contains(got, "ack-require") {
		t.Errorf("offered on another person's post: %s", got)
	}
	m.Acks.Views = map[string]AckView{"policy": {Loaded: true, CanManage: true}}
	if got := issue3815Menu(t, m, msg); strings.Contains(got, "ack-require") {
		t.Errorf("offered on a post already marked: %s", got)
	}
}

func TestIssue3815_RequireActionCallsTheClientOnce(t *testing.T) {
	m := issue3815Model("en-US", "hr")
	var asked []string
	m.Acks.Require = func(id string) { asked = append(asked, id) }
	m.ackRequire("policy")
	if len(asked) != 1 || asked[0] != "policy" {
		t.Fatalf("require calls = %v", asked)
	}
}

func TestIssue3815_CopyInEveryLanguageAndRTL(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		m := issue3815Model(locale, "maria")
		m.Acks.Views = map[string]AckView{"policy": {Loaded: true, CanAcknowledge: true}}
		got := renderNode(t, message(m, handlers{}, issue3815Message(), false))
		if strings.Contains(got, "Acknowledgement required") || strings.Contains(got, "chat.issue3815") {
			t.Errorf("%s shows English or a raw key: %s", locale, got)
		}
		for key := range issue3815Copy["en-US"] {
			if issue3815Copy[locale][key] == "" {
				t.Errorf("%s lacks %s", locale, key)
			}
		}
	}
	if !strings.Contains(Issue3815Styles, "inline-size") || strings.Contains(Issue3815Styles, "margin-left") || strings.Contains(Issue3815Styles, "padding-left") {
		t.Error("the acknowledgement styles are not logical-property styles")
	}
	if !strings.Contains(Issue3815Styles, "min-block-size:44px") {
		t.Error("the Acknowledge button is smaller than a 44px target")
	}
}
