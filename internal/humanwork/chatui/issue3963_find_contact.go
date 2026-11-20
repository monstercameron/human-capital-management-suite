package chatui

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3963: "Find the right contact or channel". When the person asks in the
// message box who handles something, they can describe the work and see up to
// three channels whose published purpose matches. The lookup is deterministic and
// uses only the purpose lines of channels this person can already see (the ones
// in their list, the public ones they can browse, and search results): it names
// no person, ranks nobody and reads no message. Each match shows its purpose as
// the source and whether the person is a member. Opening a match checks again
// that the person can still see it; opening only previews or selects the
// channel, and nothing is joined, invited or posted from here. An introduction
// can be prepared as editable text that stays in the card. With no match the
// card says so and points to browsing channels.

const (
	issue3963OpenAction        = "find-open"
	issue3963CloseAction       = "find-close"
	issue3963SearchAction      = "find-search"
	issue3963OpenChannelAction = "find-open-channel"
	issue3963IntroAction       = "find-intro"
	issue3963BrowseAction      = "find-browse"
	issue3963QueryID           = "find-contact-query"
	issue3963IntroID           = "find-contact-intro"
	issue3963RegionSel         = `.find-contact[data-find-contact="open"]`
	issue3963OfferSelector     = `.find-contact-offer`
	issue3963MaxMatches        = 3
	issue3963TextLimit         = 500
)

// issue3963Match is one channel found, as it was listed when it was found.
type issue3963Match struct {
	ID, Name, Purpose string
	Private, Joined   bool
}

// issue3963UI is the finder's state, kept per person on the page.
type issue3963UI struct {
	Open     bool
	Room     string
	Query    string
	Searched bool
	NeedText bool
	Matches  []issue3963Match
	Notice   string
	IntroFor string
	Intro    string
}

var issue3963Asks = []string{"who handles", "who owns", "who do i ask", "who should i ask", "who should i contact", "who can i ask",
	"who is responsible", "who's responsible", "who to ask", "where do i ask", "which channel", "right contact", "right channel",
	"wer ist zuständig", "wer kümmert sich", "an wen wende", "welcher kanal", "من المسؤول", "من يتولى", "إلى من أتوجه", "أي قناة"}

func issue3963Asking(text string) bool {
	text = strings.ToLower(text)
	for _, phrase := range issue3963Asks {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

var issue3963Stop = map[string]bool{"the": true, "and": true, "who": true, "how": true, "what": true, "where": true, "for": true, "with": true,
	"can": true, "ask": true, "handles": true, "handle": true, "about": true, "this": true, "that": true, "need": true, "have": true,
	"our": true, "you": true, "new": true, "question": true, "help": true, "contact": true, "channel": true, "which": true, "should": true,
	"whom": true, "wer": true, "wie": true, "was": true, "der": true, "die": true, "das": true, "und": true, "ist": true, "für": true,
	"mit": true, "ich": true, "ein": true, "eine": true, "zuständig": true, "frage": true, "wen": true, "kanal": true, "من": true,
	"في": true, "على": true, "هذا": true, "ما": true, "عن": true, "إلى": true}

func issue3963Words(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	seen := map[string]bool{}
	out := []string{}
	for _, word := range fields {
		if utf8.RuneCountInString(word) < 3 || issue3963Stop[word] || seen[word] {
			continue
		}
		seen[word] = true
		out = append(out, word)
	}
	return out
}

// issue3963Like reports whether two words are the same word or the same stem: one
// is the start of the other (four letters or more), or they share five letters.
func issue3963Like(a, b string) bool {
	if a == b {
		return true
	}
	ra, rb := []rune(a), []rune(b)
	common := 0
	for common < len(ra) && common < len(rb) && ra[common] == rb[common] {
		common++
	}
	short := len(ra)
	if len(rb) < short {
		short = len(rb)
	}
	return common >= 5 || (short >= 4 && common == short)
}

// issue3963Visible lists the channels this person may see, once each: the
// conversation list, the browsable channels and the channel search results.
// Direct and group conversations, agent conversations, other tenants' channels
// and private channels the person is not in are never candidates.
func issue3963Visible(m Model) []Conversation {
	seen := map[string]bool{}
	out := []Conversation{}
	for _, list := range [][]Conversation{m.Conversations, m.Browse, m.FindChannels, m.SearchChannels} {
		for _, c := range list {
			if c.ID == "" || seen[c.ID] || c.Agent {
				continue
			}
			if c.Kind != PublicChannel && c.Kind != PrivateChannel {
				continue
			}
			if c.HostTenantID != "" && c.HostTenantID != m.CurrentTenantID {
				continue
			}
			if c.Kind == PrivateChannel && !c.Joined {
				continue
			}
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	return out
}

// issue3963Find returns up to three visible channels whose published purpose
// shares words with the question. A channel with no purpose is never a match.
func issue3963Find(m Model, query string) []issue3963Match {
	words := issue3963Words(query)
	type scored struct {
		match issue3963Match
		score int
	}
	found := []scored{}
	for _, c := range issue3963Visible(m) {
		if c.ID == m.SelectedID {
			continue
		}
		purpose := strings.TrimSpace(c.Topic)
		if purpose == "" {
			continue
		}
		purposeWords, nameWords := issue3963Words(purpose), issue3963Words(c.Name)
		score := 0
		for _, word := range words {
			for _, other := range purposeWords {
				if issue3963Like(word, other) {
					score += 2
					break
				}
			}
			for _, other := range nameWords {
				if issue3963Like(word, other) {
					score++
					break
				}
			}
		}
		if score < 2 {
			continue
		}
		found = append(found, scored{issue3963Match{ID: c.ID, Name: c.Name, Purpose: purpose, Private: c.Kind == PrivateChannel, Joined: c.Joined}, score})
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score > found[j].score
		}
		if found[i].match.Joined != found[j].match.Joined {
			return found[i].match.Joined
		}
		return strings.ToLower(found[i].match.Name) < strings.ToLower(found[j].match.Name)
	})
	out := []issue3963Match{}
	for i := 0; i < len(found) && i < issue3963MaxMatches; i++ {
		out = append(out, found[i].match)
	}
	return out
}

// issue3963Loading says the channels and their purposes are still being read, so
// a search now would wrongly report that nothing matches.
func issue3963Loading(m Model) bool {
	return m.Callbacks.LoadFindChannels != nil && !m.FindChannelsReady
}

// issue3963Current looks the channel up again in what the person may see now.
func issue3963Current(m Model, id string) (Conversation, bool) {
	for _, c := range issue3963Visible(m) {
		if c.ID == id {
			return c, true
		}
	}
	return Conversation{}, false
}

func issue3963Clip(text string) string {
	text = strings.TrimSpace(text)
	if runes := []rune(text); len(runes) > issue3963TextLimit {
		return string(runes[:issue3963TextLimit])
	}
	return text
}

// issue3963Where reports whether the finder is offered in the open conversation:
// a conversation of this tenant while the unsent message asks who handles
// something.
func issue3963Where(m Model) bool {
	c := m.selected()
	if c.ID == "" {
		return false
	}
	if c.HostTenantID != "" && c.HostTenantID != m.CurrentTenantID {
		return false
	}
	return issue3963Asking(m.Draft)
}

// issue3963Typed draws the card again when typing makes the message start or
// stop asking who handles something. The message box keeps no state in the page
// model while someone types, so without this the offer would only appear on some
// later, unrelated redraw.
func issue3963Typed(m Model, local localStore, value string) {
	if issue3963Asking(value) != issue3963Asking(m.Draft) {
		local.update(func(*localUI) {})
	}
}

// issue3963Apply decides what one press does and returns the effect that remains
// for the browser: "open", "close", "search", "intro" or "".
func issue3963Apply(m Model, local localStore, action, id string) string {
	switch action {
	case issue3963OpenAction:
		local.update(func(u *localUI) {
			st := u.find
			if st.Room != m.SelectedID {
				st = issue3963UI{}
			}
			if strings.TrimSpace(st.Query) == "" {
				st.Query = issue3963Clip(m.Draft)
			}
			st.Open, st.Room = true, m.SelectedID
			u.find = st
		})
		if m.Callbacks.LoadFindChannels != nil {
			// The public channels and every purpose are read again on each open,
			// so what is searched is what the person may see now.
			m.Callbacks.LoadFindChannels()
		}
		return "open"
	case issue3963CloseAction:
		local.update(func(u *localUI) { u.find.Open = false })
		return "close"
	case issue3963SearchAction:
		if issue3963Loading(m) {
			local.update(func(u *localUI) {
				u.find.Searched, u.find.NeedText, u.find.Matches, u.find.Notice, u.find.IntroFor = false, false, nil, "loading", ""
			})
			return "search"
		}
		query := issue3963Clip(local.get().find.Query)
		if len(issue3963Words(query)) == 0 {
			local.update(func(u *localUI) {
				u.find.Searched, u.find.NeedText, u.find.Matches, u.find.Notice, u.find.IntroFor = false, true, nil, "", ""
			})
			return "search"
		}
		matches := issue3963Find(m, query)
		local.update(func(u *localUI) {
			u.find.Searched, u.find.NeedText, u.find.Matches, u.find.Notice, u.find.IntroFor, u.find.Intro = true, false, matches, "", "", ""
		})
		return "search"
	case issue3963OpenChannelAction:
		c, ok := issue3963Current(m, id)
		if !ok {
			// Access is checked again on every open: a channel that is no longer
			// listed for this person is dropped and never opened.
			local.update(func(u *localUI) {
				kept := []issue3963Match{}
				for _, match := range u.find.Matches {
					if match.ID != id {
						kept = append(kept, match)
					}
				}
				u.find.Matches, u.find.Notice = kept, "gone"
				if u.find.IntroFor == id {
					u.find.IntroFor, u.find.Intro = "", ""
				}
			})
			return "search"
		}
		local.update(func(u *localUI) { u.find.Notice = "" })
		switch {
		case c.Joined && m.Callbacks.SelectConversation != nil:
			m.Callbacks.SelectConversation(c.ID)
		case !c.Joined && m.Callbacks.OpenSearchChannel != nil:
			m.Callbacks.OpenSearchChannel(c.ID)
		}
		return ""
	case issue3963IntroAction:
		for _, match := range local.get().find.Matches {
			if match.ID != id {
				continue
			}
			local.update(func(u *localUI) {
				u.find.IntroFor = id
				u.find.Intro = issue3963Format(m, "intro_text", map[string]string{"work": issue3963Clip(u.find.Query), "channel": match.Name})
			})
			return "intro"
		}
		return ""
	case issue3963BrowseAction:
		if m.Callbacks.OpenBrowse != nil {
			m.Callbacks.OpenBrowse()
		}
		return ""
	}
	return ""
}

// issue3963Click handles the panel's presses and reports whether it was one.
func issue3963Click(e ui.Event, m Model, local localStore) bool {
	action, id, _ := eventAction(e)
	if !strings.HasPrefix(action, "find-") {
		return false
	}
	if local.get().find.Open {
		// Text the person typed, including text they cleared, is kept before
		// anything redraws the boxes.
		if typed, ok := issue3963Field(issue3963QueryID); ok {
			local.update(func(u *localUI) { u.find.Query = issue3963Clip(typed) })
		}
		if typed, ok := issue3963Field(issue3963IntroID); ok {
			local.update(func(u *localUI) { u.find.Intro = issue3963Clip(typed) })
		}
	}
	e.PreventDefault()
	switch issue3963Apply(m, local, action, id) {
	case "open", "search":
		chatux026Focus(issue3963RegionSel)
	case "close":
		chatux026Focus(issue3963OfferSelector)
	case "intro":
		chatux026Focus("#" + issue3963IntroID)
	}
	return true
}

// issue3963Panel is the offer and the finder, drawn above the message box for
// this person only.
func issue3963Panel(m Model, local localUI) ui.Node {
	st := local.find
	open := st.Open && st.Room == m.SelectedID
	if !open && !issue3963Where(m) {
		return nil
	}
	title := issue3963Text(m, "open")
	if !open {
		return html.Div(html.Props{Class: "find-contact find-contact-closed", Data: map[string]string{"find-contact": "closed"}},
			html.Button(html.Props{Class: "find-contact-offer", Type: "button", Aria: map[string]string{"expanded": "false"}, Data: map[string]string{"action": issue3963OpenAction}, Text: title}))
	}
	none := map[string]any{"form": issue3788FormNone}
	children := []ui.Node{
		html.H4(html.Props{Class: "find-title", Dir: "auto", Text: title}),
		html.P(html.Props{Class: "find-note", Dir: "auto", Text: issue3963Text(m, "scope")}),
		html.Div(html.Props{Class: "find-field"},
			html.Label(html.Props{For: issue3963QueryID, Text: issue3963Text(m, "query_label")}),
			html.Textarea(html.Props{ID: issue3963QueryID, Class: "chat-input", Rows: 2, Dir: "auto", MaxLength: issue3963TextLimit, Raw: none, Data: map[string]string{"chat-value": st.Query}})),
		html.Div(html.Props{Class: "find-actions"},
			issue3963Button(m, issue3963SearchAction, "search", true),
			issue3963Button(m, issue3963CloseAction, "close", false)),
	}
	status := ""
	switch {
	case issue3963Loading(m):
		status = issue3963Text(m, "loading")
	case st.Notice == "loading":
		status = issue3963Text(m, "retry")
	case st.NeedText:
		status = issue3963Text(m, "need_text")
	case st.Notice == "gone":
		status = issue3963Text(m, "gone")
	case st.Searched && len(st.Matches) > 0:
		status = issue3963Format(m, "found", map[string]string{"count": itoa(len(st.Matches))})
	case st.Searched:
		status = issue3963Text(m, "none")
	}
	if status != "" {
		children = append(children, html.P(html.Props{Class: "find-status", Role: "status", Aria: map[string]string{"live": "polite"}, Dir: "auto", Text: status}))
	}
	if st.Searched && m.FindChannelsFailed {
		children = append(children, html.P(html.Props{Class: "find-note", Role: "status", Dir: "auto", Text: issue3963Text(m, "partial")}))
	}
	if len(st.Matches) > 0 {
		rows := []ui.Node{}
		for _, match := range st.Matches {
			rows = append(rows, issue3963Row(m, st, match))
		}
		children = append(children, html.Ul(html.Props{Class: "find-results"}, rows...))
		children = append(children, html.P(html.Props{Class: "find-note", Dir: "auto", Text: issue3963Text(m, "private")}))
	}
	if st.Searched && len(st.Matches) == 0 {
		children = append(children, html.P(html.Props{Class: "find-note", Dir: "auto", Text: issue3963Text(m, "ordinary")}),
			html.Div(html.Props{Class: "find-actions"}, issue3963Button(m, issue3963BrowseAction, "browse", false)))
	}
	return html.Div(html.Props{Class: "find-contact", Role: "region", TabIndex: -1, Aria: map[string]string{"label": title}, Data: map[string]string{"find-contact": "open"}}, children...)
}

func issue3963Row(m Model, st issue3963UI, match issue3963Match) ui.Node {
	membership := "member_public"
	switch {
	case match.Private:
		membership = "member_private"
	case !match.Joined:
		membership = "not_joined"
	}
	open := "open_channel"
	if !match.Joined {
		open = "preview_channel"
	}
	name := match.Name
	if !match.Private {
		name = "#" + name
	}
	children := []ui.Node{
		html.Strong(html.Props{Class: "find-name", Dir: "auto", Text: name}),
		html.P(html.Props{Class: "find-purpose", Dir: "auto", Text: match.Purpose}),
		html.P(html.Props{Class: "find-source", Dir: "auto", Text: issue3963Text(m, "source")}),
		html.P(html.Props{Class: "find-member", Dir: "auto", Text: issue3963Text(m, membership)}),
		html.Div(html.Props{Class: "find-actions"},
			html.Button(html.Props{Class: "button small", Type: "button", Data: map[string]string{"action": issue3963OpenChannelAction, "id": match.ID}, Text: issue3963Text(m, open)}),
			html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": issue3963IntroAction, "id": match.ID}, Text: issue3963Text(m, "intro")})),
	}
	if st.IntroFor == match.ID {
		none := map[string]any{"form": issue3788FormNone}
		children = append(children, html.Div(html.Props{Class: "find-field"},
			html.Label(html.Props{For: issue3963IntroID, Text: issue3963Text(m, "intro_label")}),
			html.Textarea(html.Props{ID: issue3963IntroID, Class: "chat-input", Rows: 3, Dir: "auto", MaxLength: issue3963TextLimit, Raw: none, Data: map[string]string{"chat-value": st.Intro}}),
			html.P(html.Props{Class: "find-note", Dir: "auto", Text: issue3963Text(m, "intro_help")})))
	}
	return html.Li(html.Props{Class: "find-result", Data: map[string]string{"find-result": match.ID}}, children...)
}

func issue3963Button(m Model, action, key string, primary bool) ui.Node {
	class := "button small"
	if !primary {
		class = "button secondary small"
	}
	return html.Button(html.Props{Class: class, Type: "button", Data: map[string]string{"action": action}, Text: issue3963Text(m, key)})
}

func issue3963Format(m Model, key string, values map[string]string) string {
	text := issue3963Text(m, key)
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

func issue3963Text(m Model, key string) string {
	row := issue3963Table[key]
	return chatbug039Text("issue3963."+key, row[chatbug039LocaleIndex(m.Locale)], row[0])
}

var issue3963Table = map[string][3]string{
	"open":            {"Find the right contact or channel", "Passenden Kontakt oder Kanal finden", "العثور على جهة الاتصال أو القناة المناسبة"},
	"scope":           {"Only you can see this. It looks only at the purpose lines of channels you can already see. It does not look at messages, rank people or guess who knows what. Nothing is joined, invited or posted from here.", "Nur Sie sehen dies. Es werden nur die Zweckzeilen von Kanälen durchsucht, die Sie ohnehin sehen können. Nachrichten werden nicht gelesen, Personen nicht bewertet und niemand wird vermutet. Von hier aus wird nichts beigetreten, eingeladen oder gepostet.", "أنت وحدك ترى هذا. يبحث فقط في أسطر الغرض للقنوات التي يمكنك رؤيتها أصلًا. لا يقرأ الرسائل ولا يرتّب الأشخاص ولا يخمّن من يعرف ماذا. لا يتم الانضمام أو الدعوة أو النشر من هنا."},
	"query_label":     {"Describe the work", "Beschreiben Sie die Aufgabe", "صف العمل"},
	"search":          {"Find channels", "Kanäle finden", "ابحث عن القنوات"},
	"close":           {"Close", "Schließen", "إغلاق"},
	"need_text":       {"Describe the work in a few words, for example the topic or the team.", "Beschreiben Sie die Aufgabe in wenigen Worten, zum Beispiel Thema oder Team.", "صف العمل ببضع كلمات، مثل الموضوع أو الفريق."},
	"found":           {"{count} matching channels. Matches come from each channel's listed purpose.", "{count} passende Kanäle. Die Treffer stammen aus dem angegebenen Zweck des Kanals.", "{count} قنوات مطابقة. تأتي النتائج من الغرض المُدرج لكل قناة."},
	"none":            {"No published channel purpose matches this. No contact is guessed.", "Kein veröffentlichter Kanalzweck passt dazu. Es wird kein Kontakt geraten.", "لا يوجد غرض قناة منشور يطابق هذا. لا يتم تخمين أي جهة اتصال."},
	"ordinary":        {"Browse channels or use the search box to look for a channel or a person yourself.", "Durchsuchen Sie die Kanäle oder nutzen Sie das Suchfeld, um selbst nach einem Kanal oder einer Person zu suchen.", "تصفّح القنوات أو استخدم مربع البحث للبحث بنفسك عن قناة أو شخص."},
	"browse":          {"Browse channels", "Kanäle durchsuchen", "تصفّح القنوات"},
	"source":          {"Listed purpose of the channel", "Angegebener Zweck des Kanals", "الغرض المُدرج للقناة"},
	"member_public":   {"Public channel. You are a member.", "Öffentlicher Kanal. Sie sind Mitglied.", "قناة عامة. أنت عضو."},
	"member_private":  {"Private channel. You are a member.", "Privater Kanal. Sie sind Mitglied.", "قناة خاصة. أنت عضو."},
	"not_joined":      {"Public channel. You have not joined.", "Öffentlicher Kanal. Sie sind nicht beigetreten.", "قناة عامة. لم تنضم إليها."},
	"open_channel":    {"Open channel", "Kanal öffnen", "فتح القناة"},
	"preview_channel": {"Look at channel without joining", "Kanal ansehen, ohne beizutreten", "عرض القناة دون الانضمام"},
	"intro":           {"Prepare an introduction", "Vorstellung vorbereiten", "تحضير تعريف"},
	"intro_label":     {"Introduction (you can change it)", "Vorstellung (Sie können sie ändern)", "التعريف (يمكنك تعديله)"},
	"intro_help":      {"Nothing is sent from here. Open the channel and paste this if you choose to write there.", "Von hier wird nichts gesendet. Öffnen Sie den Kanal und fügen Sie den Text ein, wenn Sie dort schreiben möchten.", "لا يُرسل شيء من هنا. افتح القناة والصق هذا النص إذا اخترت الكتابة هناك."},
	"intro_text":      {"Hello, I am looking for help with: {work}. I found this channel listed for it ({channel}).", "Hallo, ich suche Hilfe zu: {work}. Ich habe diesen Kanal dafür gefunden ({channel}).", "مرحبًا، أبحث عن مساعدة بخصوص: {work}. وجدت هذه القناة مدرجة لذلك ({channel})."},
	"private":         {"Opening a channel checks again that you can still see it. Joining and posting stay your choice.", "Beim Öffnen eines Kanals wird erneut geprüft, ob Sie ihn noch sehen dürfen. Beitreten und Posten bleiben Ihre Entscheidung.", "يتحقق فتح القناة مرة أخرى من أنه ما زال بإمكانك رؤيتها. الانضمام والنشر يبقيان قرارك."},
	"loading":         {"Reading the channels you can see…", "Die Kanäle, die Sie sehen können, werden gelesen …", "جارٍ قراءة القنوات التي يمكنك رؤيتها…"},
	"retry":           {"The channels are ready. Press Find channels again.", "Die Kanäle sind bereit. Wählen Sie erneut „Kanäle finden“.", "القنوات جاهزة. اضغط «ابحث عن القنوات» مرة أخرى."},
	"partial":         {"Public channels you have not joined could not be checked, so only channels you are already in were searched.", "Öffentliche Kanäle, denen Sie nicht beigetreten sind, konnten nicht geprüft werden. Es wurden nur Kanäle durchsucht, in denen Sie bereits sind.", "تعذّر فحص القنوات العامة التي لم تنضم إليها، لذا بحثنا فقط في القنوات التي أنت عضو فيها."},
	"gone":            {"That channel is no longer available to you, so it was removed from the list.", "Dieser Kanal steht Ihnen nicht mehr zur Verfügung und wurde aus der Liste entfernt.", "لم تعد هذه القناة متاحة لك، لذا أُزيلت من القائمة."},
}

const chatIssue3963Styles = `.find-contact{display:grid;gap:8px;margin-block:8px;min-width:0}` +
	`.find-contact:not(.find-contact-closed){padding:12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface)}` +
	`.find-contact-offer{justify-self:start;min-height:36px;padding:6px 12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem;cursor:pointer;text-align:start}` +
	`.find-contact-offer:hover,.find-contact-offer:focus-visible{border-color:var(--accent);color:var(--accent)}` +
	`.find-title{margin:0;font-size:.9375rem}.find-contact p{margin:0;font-size:.8125rem;line-height:1.4;overflow-wrap:anywhere}` +
	`.find-note,.find-source,.find-member{color:var(--muted)}.find-status{font-weight:600}` +
	`.find-results{display:grid;gap:8px;margin:0;padding:0;list-style:none}` +
	`.find-result{display:grid;gap:4px;padding:8px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);min-width:0}` +
	`.find-name{overflow-wrap:anywhere}` +
	`.find-field{display:grid;gap:4px;min-width:0}.find-field textarea{width:100%;box-sizing:border-box}` +
	`.find-actions{display:flex;flex-wrap:wrap;gap:8px}` +
	`@media (pointer:coarse),(max-width:480px){.find-contact-offer,.find-actions .button{min-height:44px}.find-actions{flex-direction:column}.find-actions .button{width:100%}}`
