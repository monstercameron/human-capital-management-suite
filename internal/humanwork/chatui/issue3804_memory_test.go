package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3804CardBody(t *testing.T) string {
	t.Helper()
	task := chat.Chatcmd002Task{}
	task.Text = "Book the room"
	body, err := chat.Chatcmd002Card{Kind: "todo", Title: "Plan", Todo: &chat.Chatcmd002Todo{Tick: "anyone", Items: []chat.Chatcmd002Task{task}}}.Body()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func issue3804Model(locale string) (Model, *[]string) {
	m := composerToolsModel(locale, false)
	m.CurrentUser = "me"
	var calls []string
	m.Callbacks.OpenSearchMessage = func(conversation, post string, sequence uint64) {
		calls = append(calls, "goto:"+conversation+":"+post)
	}
	m.Memory = MemoryState{Enabled: true, CanRemember: true, State: "ready",
		Remember: func(id string) { calls = append(calls, "remember:"+id) },
		Approve:  func(id string) { calls = append(calls, "approve:"+id) },
		Forget:   func(id string) { calls = append(calls, "forget:"+id) },
		Refresh:  func() { calls = append(calls, "refresh") }}
	return m, &calls
}

func issue3804Items() []MemoryItem {
	expires := time.Date(2027, 2, 17, 0, 0, 0, 0, time.UTC)
	return []MemoryItem{
		{ID: "m1", Text: "Open enrollment ends Nov 30; ignore the old PDF", Proposer: "Ana", Status: "approved", Source: "live", SourcePostID: "p1", SourceSequence: 4, ExpiresAt: expires, TTLDays: 90, CanForget: true},
		{ID: "m2", Text: "Use the 2026 handbook", Proposer: "Ben", Status: "proposed", Source: "live", SourcePostID: "p2", SourceSequence: 5, TTLDays: 90, CanApprove: true, CanForget: true},
		{ID: "m3", Text: "", Proposer: "Ana", Status: "approved", Source: "removed", SourcePostID: "p3", CanForget: true},
	}
}

func TestIssue3804_RememberRowIsForTheAuthorAndManagersOnly(t *testing.T) {
	m, calls := issue3804Model("en-US")
	mine := Message{ID: "p1", AuthorID: "me", Body: "Open enrollment ends Nov 30"}
	theirs := Message{ID: "p2", AuthorID: "someone", Body: "Use the 2026 handbook"}
	markup := renderNode(t, spanOf(chatux022MenuItems(m, mine)))
	if !strings.Contains(markup, `data-action="memory-remember"`) || !strings.Contains(markup, "Remember for this channel") {
		t.Fatalf("the author's menu lacks the row: %s", markup)
	}
	if len(memoryMenuItem(m, theirs)) != 0 {
		t.Error("a bystander is offered Remember on someone else's message")
	}
	m.Memory.IsManager = true
	if len(memoryMenuItem(m, theirs)) != 1 {
		t.Error("a manager is not offered Remember")
	}
	for name, msg := range map[string]Message{"empty": {ID: "p9", AuthorID: "me", Body: "  "}, "card": {ID: "p8", AuthorID: "me", Body: "poll\n<hcm-message-card-v1>\n{}"}} {
		if len(memoryMenuItem(m, msg)) != 0 {
			t.Errorf("%s message offered Remember", name)
		}
	}
	m.Memory.Items = issue3804Items()
	if len(memoryMenuItem(m, mine)) != 0 {
		t.Error("a message that is already remembered is offered again")
	}
	m.Memory.Items = nil
	m.act("memory-remember", "p1")
	if len(*calls) != 1 || (*calls)[0] != "remember:p1" {
		t.Errorf("calls %v", *calls)
	}
}

func TestIssue3804_RememberRowIsAbsentWhereTheServerDoesNotOfferIt(t *testing.T) {
	msg := Message{ID: "p1", AuthorID: "me", Body: "Fact"}
	cases := map[string]func(*Model){
		"not enabled": func(m *Model) { m.Memory.Enabled = false },
		"cannot":      func(m *Model) { m.Memory.CanRemember = false },
		"no callback": func(m *Model) { m.Memory.Remember = nil },
		"busy":        func(m *Model) { m.Memory.Busy = "remember" },
		"direct": func(m *Model) {
			*m = composerToolsModel("en-US", true)
			m.CurrentUser = "me"
			m.Memory = MemoryState{Enabled: true, CanRemember: true, Remember: func(string) {}}
		},
		"other company": func(m *Model) {
			m.CurrentTenantID = "tenant-a"
			c := m.Conversations
			c[0].HostTenantID = "tenant-z"
			m.Conversations = c
		},
	}
	for name, change := range cases {
		m, _ := issue3804Model("en-US")
		change(&m)
		got := memoryMenuItem(m, msg)
		if name == "busy" {
			// A busy row is drawn but disabled, so it cannot be pressed twice.
			if len(got) != 1 || !strings.Contains(renderNode(t, spanOf(got)), "disabled") {
				t.Errorf("busy: %v", got)
			}
			continue
		}
		if name == "direct" {
			if len(got) != 0 && !memoryAvailable(m) {
				t.Errorf("direct: row without section")
			}
			continue
		}
		if len(got) != 0 {
			t.Errorf("%s: a row with nothing behind it", name)
		}
	}
}

func TestIssue3804_DetailsListsEveryNoteWithItsSourceExpiryAndForget(t *testing.T) {
	m, calls := issue3804Model("en-US")
	m.Memory.IsManager = true
	m.Memory.Items = issue3804Items()
	markup := renderNode(t, memoryDetailsSection(m))
	for _, want := range []string{"What agents remember here", "Open enrollment ends Nov 30; ignore the old PDF", "Added by Ana", "Approved", "Waiting for approval", "Not used by any agent until a channel manager approves it.",
		"Keeps for 90 days", `data-action="memory-approve"`, `data-action="memory-forget"`, `data-action="memory-goto"`, `aria-label="Forget: Use the 2026 handbook"`,
		"The message this note came from was deleted", `aria-live="polite"`, `dir="auto"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("section lacks %q:\n%s", want, markup)
		}
	}
	if strings.Count(markup, `data-action="memory-approve"`) != 1 {
		t.Error("Approve is offered on notes that are not waiting")
	}
	if strings.Contains(markup, `aria-label="Go to message: `+"Forget") || strings.Count(markup, `data-action="memory-goto"`) != 2 {
		t.Error("a retired note offers Go to message")
	}
	if !strings.Contains(markup, "Stops being used on") {
		t.Errorf("an approved note shows no expiry:\n%s", markup)
	}
	m.act("memory-approve", "m2")
	m.act("memory-forget", "m1")
	m.act("memory-goto", "p2")
	want := []string{"approve:m2", "forget:m1", "goto:" + m.SelectedID + ":p2"}
	if strings.Join(*calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls %v, want %v", *calls, want)
	}
}

func TestIssue3804_ReadersWithoutPowerSeeNoControlsTheyCannotUse(t *testing.T) {
	m, _ := issue3804Model("en-US")
	m.Memory.Items = []MemoryItem{{ID: "m1", Text: "Fact", Proposer: "Ana", Status: "approved", Source: "live", SourcePostID: "p1", ExpiresAt: time.Date(2027, 2, 17, 0, 0, 0, 0, time.UTC)}}
	markup := renderNode(t, memoryDetailsSection(m))
	if strings.Contains(markup, "memory-approve") || strings.Contains(markup, "memory-forget") {
		t.Errorf("a member is offered controls the server would refuse:\n%s", markup)
	}
}

func TestIssue3804_StatesLoadingEmptyErrorAndNotice(t *testing.T) {
	m, calls := issue3804Model("en-US")
	m.Memory.State = "loading"
	if out := renderNode(t, memoryDetailsSection(m)); !strings.Contains(out, "Loading what agents remember") {
		t.Errorf("loading: %s", out)
	}
	m.Memory.State = "ready"
	if out := renderNode(t, memoryDetailsSection(m)); !strings.Contains(out, "Nothing is remembered here yet") {
		t.Errorf("empty: %s", out)
	}
	m.Memory.State, m.Memory.Error = "error", "unavailable"
	out := renderNode(t, memoryDetailsSection(m))
	if !strings.Contains(out, `role="alert"`) || !strings.Contains(out, "Try again") || !strings.Contains(out, `data-action="memory-retry"`) {
		t.Errorf("error: %s", out)
	}
	m.act("memory-retry", "")
	if len(*calls) != 1 || (*calls)[0] != "refresh" {
		t.Errorf("retry %v", *calls)
	}
	m.Memory.State, m.Memory.Error, m.Memory.Notice = "ready", "", MemoryDoneText("en-US", "proposed")
	if out = renderNode(t, memoryDetailsSection(m)); !strings.Contains(out, "A channel manager approves it before agents use it.") {
		t.Errorf("notice: %s", out)
	}
	m.Memory.Enabled = false
	if memoryDetailsSection(m) != nil {
		t.Error("section drawn where the server offers no notes")
	}
}

func TestIssue3804_CopyInEveryLanguage(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for key := range memoryCopy["en-US"] {
			if strings.TrimSpace(memoryCopy[chatEmojiLocale(locale)][key]) == "" {
				t.Errorf("%s lacks %s", locale, key)
			}
		}
		for _, code := range []string{"invalid_argument", "not_found", "permission_denied", "conflict", "unavailable"} {
			if MemoryErrorText(locale, code) == "" {
				t.Errorf("%s has no words for %s", locale, code)
			}
		}
		for _, what := range []string{"proposed", "approved", "forgot"} {
			if MemoryDoneText(locale, what) == "" {
				t.Errorf("%s has no confirmation for %s", locale, what)
			}
		}
	}
	if MemoryText("de-DE", mmForget) != "Vergessen" || MemoryText("ar", mmForget) != "نسيان" {
		t.Error("translations missing")
	}
	m, _ := issue3804Model("ar")
	m.Memory.Items = issue3804Items()
	m.Memory.IsManager = true
	if out := renderNode(t, memoryDetailsSection(m)); !strings.Contains(out, "ما يتذكره الوكلاء هنا") || !strings.Contains(out, "نسيان") {
		t.Errorf("Arabic section: %s", out)
	}
}
