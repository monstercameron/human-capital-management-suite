package chatui

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

// TestTodo_CHATGATE_006_SampleForm: the form the builder shows under "Sample
// form" is for looking and for "Try it". It carried Submit answers and Save
// draft, so an administrator trying the form sent a real submission.
func TestTodo_CHATGATE_006_SampleForm(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		v := chatgateViewFixture()
		v.Locale, v.Administrator = locale, true
		draft := v.Gate.Versions[0]
		v.Gate.Draft = &draft
		builder := gateMarkup(t, v)
		// The questions are there to be tried.
		if !strings.Contains(builder, `id="gate-answer-form"`) || !strings.Contains(builder, `data-gate-action="try"`) {
			t.Fatalf("%s: the builder lost its sample form or Try it: %s", locale, builder)
		}
		for _, action := range []string{"submit", "save"} {
			if strings.Contains(builder, `data-gate-action="`+action+`"`) {
				t.Errorf("%s: the builder's sample form can %s a real answer", locale, action)
			}
		}
		// A person joining still has both.
		v.Administrator, v.Gate.Draft = false, nil
		applicant := gateMarkup(t, v)
		for _, action := range []string{"submit", "save"} {
			if !strings.Contains(applicant, `data-gate-action="`+action+`"`) {
				t.Errorf("%s: the form a person joins through lost %s", locale, action)
			}
		}
	}
}

// TestTodo_CHATGATE_005_AnswersAsText: answers are read as words, not as the
// stored JSON: no quotation marks around text, no brackets around choices.
func TestTodo_CHATGATE_005_AnswersAsText(t *testing.T) {
	for value, want := range map[string]string{`"Payroll"`: "Payroll", `["Payroll","Finance"]`: "Payroll, Finance", `true`: "true", `42`: "42", `"say \"hi\""`: `say "hi"`} {
		if got := gateAnswerText(json.RawMessage(value)); got != want {
			t.Errorf("answer %s reads %q, want %q", value, got, want)
		}
	}
	// "My answers", as the member sees them.
	v := chatgateViewFixture()
	v.Submission = &chatgate.Submission{ID: "s", Person: "member", Version: "1.0.0", Status: "admitted", SubmittedAt: time.Unix(1, 0).UTC()}
	v.Answers = map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}
	mine := gateMarkup(t, v)
	if !strings.Contains(mine, ">Team: Payroll<") || strings.Contains(mine, "&#34;Payroll&#34;") || strings.Contains(mine, `"Payroll"<`) {
		t.Errorf("a member's own answer is printed as stored: %s", regexp.MustCompile(`<p>Team:[^<]*</p>`).FindString(mine))
	}
	// The review queue and the answers table, as the administrator sees them.
	v = chatgateViewFixture()
	v.Administrator = true
	v.Submissions = []chatgate.Submission{{ID: "s", Person: "member", Version: "1.0.0", Status: "review", SubmittedAt: time.Unix(1, 0).UTC()}}
	v.VisibleAnswers = map[string]map[string]json.RawMessage{"member": {"team": json.RawMessage(`"Payroll"`)}}
	v.Names = map[string]string{"member": "Mia Member"}
	admin := gateMarkup(t, v)
	if !strings.Contains(admin, ">Team: Payroll<") || !strings.Contains(admin, "<td>Payroll</td>") {
		t.Errorf("the queue or the answers table prints the stored value: %s", regexp.MustCompile(`<td>[^<]*Payroll[^<]*</td>|<p>Team:[^<]*</p>`).FindAllString(admin, -1))
	}
	if strings.Contains(admin, "&#34;Payroll&#34;") {
		t.Error("an answer is still printed in quotation marks")
	}
}

// TestTodo_CHATGATE_006_DetailsLabel: Conversation details says "Gate" to
// everyone who administers the channel, not to its owner alone.
func TestTodo_CHATGATE_006_DetailsLabel(t *testing.T) {
	base := Model{Locale: "en-US", SelectedID: "room", CurrentUser: "mia", CurrentTenantID: "t",
		Conversations: []Conversation{{ID: "room", Name: "payroll", Kind: PrivateChannel, OwnerID: "olive", Joined: true}}}
	label := func(m Model) string {
		return regexp.MustCompile(`>([^<]+)</a>`).FindStringSubmatch(renderNode(t, chatgateDetailsSection(m)))[1]
	}
	gate, answers := GateText("en-US", "gate"), GateText("en-US", "answers")
	if got := label(base); got != answers {
		t.Errorf("a member reads %q, want %q", got, answers)
	}
	owner := base
	owner.CurrentUser = "olive"
	if got := label(owner); got != gate {
		t.Errorf("the owner reads %q, want %q", got, gate)
	}
	manager := base
	manager.ChannelTeam = ChannelTeamWidget{Members: []ChannelTeamMember{{SubjectID: "mia", HomeTenantID: "t", Role: "MEMBERSHIP_ROLE_MANAGER"}}}
	if got := label(manager); got != gate {
		t.Errorf("a channel manager reads %q, want %q", got, gate)
	}
	admin := base
	admin.IsTenantAdmin = true
	if got := label(admin); got != gate {
		t.Errorf("a workspace administrator reads %q, want %q", got, gate)
	}
	// No link in a direct message, and none where gates are switched off.
	dm := base
	dm.Conversations[0].Kind = DirectMessage
	if chatgateDetailsSection(dm) != nil {
		t.Error("a direct message offers a gate")
	}
	off := owner
	off.Conversations = []Conversation{{ID: "room", Name: "payroll", Kind: PrivateChannel, OwnerID: "olive", Joined: true}}
	off.ChatFeatures = &ChatFeatures{}
	if chatgateDetailsSection(off) != nil {
		t.Error("the gate link shows where the service is off")
	}
}
