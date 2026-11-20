package chatui

import (
	"strings"
	"testing"
)

func issue3963Model(locale string) (Model, *[]string) {
	calls := &[]string{}
	m := Model{State: StateReady, Locale: locale, CurrentUser: "alice", CurrentTenantID: "tenant", SelectedID: "general",
		Conversations: []Conversation{
			{ID: "general", Name: "general", Topic: "Company news", Kind: PublicChannel, Joined: true},
			{ID: "benefits", Name: "benefits-support", Topic: "Benefits enrollment questions for new managers", Kind: PublicChannel, Joined: true},
			{ID: "payroll", Name: "payroll-close", Topic: "Payroll close schedule and corrections", Kind: PrivateChannel, Joined: true},
			{ID: "dm", Name: "Benefits Helper", Topic: "Benefits enrollment", Kind: DirectMessage, Agent: true, Joined: true},
			{ID: "away", Name: "benefits-other", Topic: "Benefits enrollment", Kind: PublicChannel, Joined: true, HostTenantID: "other"},
			{ID: "bare", Name: "benefits-empty", Kind: PublicChannel, Joined: true},
		},
		Browse: []Conversation{
			{ID: "perks", Name: "perks", Topic: "Perks and benefits enrollment help", Kind: PublicChannel},
			{ID: "secret", Name: "benefits-secret", Topic: "Benefits enrollment", Kind: PrivateChannel},
		},
		Callbacks: Callbacks{
			SelectConversation: func(id string) { *calls = append(*calls, "select:"+id) },
			OpenSearchChannel:  func(id string) { *calls = append(*calls, "preview:"+id) },
			OpenBrowse:         func() { *calls = append(*calls, "browse") },
			JoinConversation:   func(id string) { *calls = append(*calls, "join:"+id) },
			SendMessage:        func(id, body string) { *calls = append(*calls, "send:"+id+":"+body) },
		}}
	return m, calls
}

func issue3963Search(m Model, local localStore, query string) {
	local.update(func(u *localUI) { u.find.Query = query })
	issue3963Apply(m, local, issue3963SearchAction, "")
}

func TestIssue3963_OfferedWhenTheDraftAsksWhoHandlesIt(t *testing.T) {
	m, _ := issue3963Model("en-US")
	if issue3963Panel(m, localUI{}) != nil {
		t.Fatal("offered with nothing typed")
	}
	m.Draft = "Who handles benefits enrollment for new managers?"
	got := renderNode(t, issue3963Panel(m, localUI{}))
	if !strings.Contains(got, "Find the right contact or channel") || !strings.Contains(got, `aria-expanded="false"`) {
		t.Fatalf("offer missing: %s", got)
	}
	m.Draft = "ok thanks"
	if issue3963Panel(m, localUI{}) != nil {
		t.Fatal("offered for an unrelated message")
	}
	m.Draft = "Wer ist zuständig für die Anmeldung?"
	if issue3963Panel(m, localUI{}) == nil {
		t.Fatal("German question not recognised")
	}
}

func TestIssue3963_ReturnsOnlyPublishedPurposesTheViewerCanSee(t *testing.T) {
	m, _ := issue3963Model("en-US")
	local := localStore{box: &localUI{}}
	issue3963Apply(m, local, issue3963OpenAction, "")
	issue3963Search(m, local, "benefits enrollment")
	got := local.get().find
	names := []string{}
	for _, match := range got.Matches {
		names = append(names, match.Name)
	}
	joined := strings.Join(names, ",")
	for _, hidden := range []string{"benefits-secret", "benefits-other", "benefits-empty", "Benefits Helper", "general"} {
		if strings.Contains(joined, hidden) {
			t.Fatalf("%s must not be offered: %v", hidden, names)
		}
	}
	if !strings.Contains(joined, "benefits-support") || !strings.Contains(joined, "perks") {
		t.Fatalf("visible published purposes missing: %v", names)
	}
	html := renderNode(t, issue3963Panel(m, local.get()))
	if strings.Contains(html, "benefits-secret") || strings.Contains(html, "payroll-close") && !strings.Contains(joined, "payroll-close") {
		t.Fatalf("hidden channel leaked into the card: %s", html)
	}
}

func TestIssue3963_AtMostThreeAndSourceAndMembershipAreShown(t *testing.T) {
	m, _ := issue3963Model("en-US")
	for i := 0; i < 5; i++ {
		m.Browse = append(m.Browse, Conversation{ID: "extra" + string(rune('a'+i)), Name: "extra" + string(rune('a'+i)), Topic: "Benefits enrollment", Kind: PublicChannel})
	}
	local := localStore{box: &localUI{}}
	issue3963Apply(m, local, issue3963OpenAction, "")
	issue3963Search(m, local, "benefits enrollment")
	if n := len(local.get().find.Matches); n != 3 {
		t.Fatalf("want 3 matches, got %d", n)
	}
	html := renderNode(t, issue3963Panel(m, local.get()))
	for _, want := range []string{"Benefits enrollment questions for new managers", "Listed purpose", "You are a member", "You have not joined", `data-action="find-open-channel"`, `role="status"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %s", want, html)
		}
	}
}

func TestIssue3963_PrivateChannelIsLabelledForAMember(t *testing.T) {
	m, _ := issue3963Model("en-US")
	local := localStore{box: &localUI{}}
	issue3963Apply(m, local, issue3963OpenAction, "")
	issue3963Search(m, local, "payroll close corrections")
	html := renderNode(t, issue3963Panel(m, local.get()))
	if !strings.Contains(html, "payroll-close") || !strings.Contains(html, "Private channel") {
		t.Fatalf("private member channel not shown as private: %s", html)
	}
}

func TestIssue3963_NoMatchIsHonestAndOffersOrdinarySearch(t *testing.T) {
	m, _ := issue3963Model("en-US")
	local := localStore{box: &localUI{}}
	issue3963Apply(m, local, issue3963OpenAction, "")
	issue3963Search(m, local, "forklift licensing")
	if len(local.get().find.Matches) != 0 {
		t.Fatal("invented a match")
	}
	html := renderNode(t, issue3963Panel(m, local.get()))
	for _, want := range []string{"No published channel purpose matches", `data-action="find-browse"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %s", want, html)
		}
	}
	issue3963Search(m, local, "who is the")
	if !strings.Contains(renderNode(t, issue3963Panel(m, local.get())), "few words") {
		t.Fatal("a question with no usable words should ask for a few words")
	}
}

func TestIssue3963_OpeningRechecksAccessAndNeverJoinsOrPosts(t *testing.T) {
	m, calls := issue3963Model("en-US")
	local := localStore{box: &localUI{}}
	issue3963Apply(m, local, issue3963OpenAction, "")
	issue3963Search(m, local, "perks benefits")
	issue3963Apply(m, local, issue3963OpenChannelAction, "perks")
	issue3963Apply(m, local, issue3963OpenChannelAction, "benefits")
	if got := strings.Join(*calls, "|"); got != "preview:perks|select:benefits" {
		t.Fatalf("open should only preview or select: %s", got)
	}
	// The channel disappears from what the person may see before they open it.
	gone := m
	gone.Browse = nil
	*calls = nil
	issue3963Apply(gone, local, issue3963OpenChannelAction, "perks")
	if len(*calls) != 0 {
		t.Fatalf("opened a channel the person can no longer see: %v", *calls)
	}
	html := renderNode(t, issue3963Panel(gone, local.get()))
	if !strings.Contains(html, "no longer available to you") || strings.Contains(html, `data-id="perks"`) {
		t.Fatalf("stale result kept: %s", html)
	}
}

func TestIssue3963_IntroductionIsEditableAndNothingIsSent(t *testing.T) {
	m, calls := issue3963Model("en-US")
	local := localStore{box: &localUI{}}
	issue3963Apply(m, local, issue3963OpenAction, "")
	issue3963Search(m, local, "benefits enrollment")
	issue3963Apply(m, local, issue3963IntroAction, "benefits")
	html := renderNode(t, issue3963Panel(m, local.get()))
	if !strings.Contains(html, "<textarea") || !strings.Contains(html, "benefits-support") || !strings.Contains(html, "Nothing is sent") {
		t.Fatalf("introduction not offered: %s", html)
	}
	for _, c := range *calls {
		if strings.HasPrefix(c, "send:") || strings.HasPrefix(c, "join:") {
			t.Fatalf("side effect: %v", *calls)
		}
	}
}

func TestIssue3963_OtherRoomDoesNotShowTheCardAndCloseClears(t *testing.T) {
	m, _ := issue3963Model("en-US")
	local := localStore{box: &localUI{}}
	issue3963Apply(m, local, issue3963OpenAction, "")
	other := m
	other.SelectedID = "benefits"
	if issue3963Panel(other, local.get()) != nil {
		t.Fatal("card followed the person into another room")
	}
	if got := issue3963Apply(m, local, issue3963CloseAction, ""); got != "close" || local.get().find.Open {
		t.Fatal("close did not close")
	}
}

func TestIssue3963_CopyInEveryLanguageAndRTL(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		m, _ := issue3963Model(locale)
		m.Draft = "Who handles benefits enrollment?"
		if locale == "ar" {
			m.Draft = "من المسؤول عن التسجيل؟"
		}
		local := localStore{box: &localUI{}}
		offer := renderNode(t, issue3963Panel(m, local.get()))
		if strings.Contains(offer, "Find the right contact") {
			t.Fatalf("%s offer is English: %s", locale, offer)
		}
		issue3963Apply(m, local, issue3963OpenAction, "")
		issue3963Search(m, local, "benefits enrollment")
		html := renderNode(t, issue3963Panel(m, local.get()))
		for _, english := range []string{"Listed purpose", "Open channel", "Describe the work", "Only you can see"} {
			if strings.Contains(html, english) {
				t.Fatalf("%s still shows %q: %s", locale, english, html)
			}
		}
		if !strings.Contains(html, `dir="auto"`) {
			t.Fatalf("%s: text not direction-neutral", locale)
		}
	}
	for key := range issue3963Table {
		for i, value := range issue3963Table[key] {
			if strings.TrimSpace(value) == "" {
				t.Fatalf("copy %q missing language %d", key, i)
			}
		}
	}
}

func TestIssue3963_FindChannelsFollowTheSameVisibilityRules_Security(t *testing.T) {
	m, _ := issue3963Model("en-US")
	m.Browse = nil
	m.FindChannels = []Conversation{
		{ID: "ok", Name: "benefits-open", Topic: "Benefits enrollment help", Kind: PublicChannel},
		{ID: "private", Name: "benefits-board", Topic: "Benefits enrollment", Kind: PrivateChannel},
		{ID: "foreign", Name: "benefits-foreign", Topic: "Benefits enrollment", Kind: PublicChannel, HostTenantID: "other"},
		{ID: "agent", Name: "benefits-agent", Topic: "Benefits enrollment", Kind: PublicChannel, Agent: true},
		{ID: "dm", Name: "benefits-dm", Topic: "Benefits enrollment", Kind: DirectMessage},
	}
	local := localStore{box: &localUI{}}
	issue3963Apply(m, local, issue3963OpenAction, "")
	issue3963Search(m, local, "benefits enrollment")
	html := renderNode(t, issue3963Panel(m, local.get()))
	if !strings.Contains(html, "benefits-open") {
		t.Fatalf("a public channel read for the finder is missing: %s", html)
	}
	for _, hidden := range []string{"benefits-board", "benefits-foreign", "benefits-agent", "benefits-dm", "benefits-secret"} {
		if strings.Contains(html, hidden) {
			t.Fatalf("%s must never be shown: %s", hidden, html)
		}
	}
	// Opening a private channel the person is not in, or another tenant's, is refused.
	for _, id := range []string{"private", "foreign", "secret"} {
		if _, ok := issue3963Current(m, id); ok {
			t.Fatalf("%s must not be openable from the finder", id)
		}
	}
}

func TestIssue3963_OpenReadsThePurposesAndSearchWaitsForThem(t *testing.T) {
	m, _ := issue3963Model("en-US")
	loads := 0
	m.Callbacks.LoadFindChannels = func() { loads++ }
	local := localStore{box: &localUI{}}
	issue3963Apply(m, local, issue3963OpenAction, "")
	if loads != 1 {
		t.Fatalf("opening the card should read the channels once, got %d", loads)
	}
	// Still reading: a search must not claim that nothing matches.
	issue3963Search(m, local, "benefits enrollment")
	html := renderNode(t, issue3963Panel(m, local.get()))
	if !strings.Contains(html, "Reading the channels") || strings.Contains(html, "No published channel purpose") || strings.Contains(html, "find-result") {
		t.Fatalf("search ran before the channels were read: %s", html)
	}
	m.FindChannelsReady = true
	if got := renderNode(t, issue3963Panel(m, local.get())); !strings.Contains(got, "Press Find channels again") {
		t.Fatalf("the person is not told to search again: %s", got)
	}
	issue3963Search(m, local, "benefits enrollment")
	if n := len(local.get().find.Matches); n == 0 {
		t.Fatal("no matches once the channels were read")
	}
	m.FindChannelsFailed = true
	if got := renderNode(t, issue3963Panel(m, local.get())); !strings.Contains(got, "could not be checked") {
		t.Fatalf("a failed read is not disclosed: %s", got)
	}
}

func TestIssue3963_TypingDrawsTheOfferOnlyWhenItStartsOrStops(t *testing.T) {
	m, _ := issue3963Model("en-US")
	local := localStore{box: &localUI{}}
	before := local.get().seq
	issue3963Typed(m, local, "hello")
	if local.get().seq != before {
		t.Fatal("redrew for a message that does not ask")
	}
	issue3963Typed(m, local, "Who handles benefits?")
	if local.get().seq == before {
		t.Fatal("the offer would not appear while typing")
	}
	m.Draft = "Who handles benefits?"
	at := local.get().seq
	issue3963Typed(m, local, "Who handles benefits? Thanks")
	if local.get().seq != at {
		t.Fatal("redrew on every keystroke")
	}
	issue3963Typed(m, local, "")
	if local.get().seq == at {
		t.Fatal("the offer would stay after the question was cleared")
	}
}
