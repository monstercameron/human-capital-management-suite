package chatui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// CHATBUG-036. The scripted Chat walk (.artifacts/lanes/agent-ui/chat-inspect.mjs)
// finds each control the way a person using a screen reader would, by role and
// accessible name, and reports a control that is not there as absent instead of
// timing out. These tests keep the script and the page from drifting apart
// without a server: they read the locators out of the script, work out the role
// and accessible name of every element in the markup the page renders, and look
// for each locator there. A panel the walk opens with a click is in the markup
// already (closed, with the hidden attribute), so a locator for something inside
// one is looked for there; the one thing only a script in the browser adds, the
// phone drawer's dialog role, is named in walkRuntimeOnly with where it is set.

const walkScriptPath = ".artifacts/lanes/agent-ui/chat-inspect.mjs"

// walkScript reads the walk from the repository, or skips the test when the
// checkout has no .artifacts directory (it is not tracked).
func walkScript(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("module root not found")
		}
		dir = parent
	}
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(walkScriptPath)))
	if err != nil {
		t.Skipf("the walk script is not in this checkout: %v", err)
	}
	return string(raw)
}

// walkCallbacks gives every callback of the page a function that does nothing,
// so each control renders enabled, the way it does for a signed-in person.
func walkCallbacks() Callbacks {
	var cb Callbacks
	v := reflect.ValueOf(&cb).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() != reflect.Func {
			continue
		}
		t := f.Type()
		f.Set(reflect.MakeFunc(t, func([]reflect.Value) []reflect.Value {
			out := make([]reflect.Value, t.NumOut())
			for j := range out {
				out[j] = reflect.Zero(t.Out(j))
			}
			return out
		}))
	}
	return cb
}

// ---- accessible roles and names of rendered markup ----------------------------

// walkNode is one element of rendered markup that has an ARIA role, with its
// accessible name. Closed is true inside an element carrying the hidden
// attribute (a panel the page opens on a click); Concealed is true inside an
// aria-hidden or inert one, which no role query ever finds.
type walkNode struct {
	Role, Name        string
	Closed, Concealed bool
}

func walkAttr(n *xhtml.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func walkHasAttr(n *xhtml.Node, key string) bool { _, ok := walkAttr(n, key); return ok }

func walkFlags(n *xhtml.Node) (closed, concealed bool) {
	for p := n; p != nil && p.Type == xhtml.ElementNode; p = p.Parent {
		if walkHasAttr(p, "hidden") {
			closed = true
		}
		if v, _ := walkAttr(p, "aria-hidden"); v == "true" || walkHasAttr(p, "inert") {
			concealed = true
		}
	}
	return closed, concealed
}

func walkRole(n *xhtml.Node) string {
	if r, ok := walkAttr(n, "role"); ok {
		if f := strings.Fields(r); len(f) > 0 {
			return f[0]
		}
	}
	switch n.Data {
	case "button":
		return "button"
	case "a":
		if walkHasAttr(n, "href") {
			return "link"
		}
	case "input":
		t, _ := walkAttr(n, "type")
		switch t {
		case "", "text", "email", "url", "tel":
			return "textbox"
		case "search":
			return "searchbox"
		case "checkbox":
			return "checkbox"
		case "radio":
			return "radio"
		case "button", "submit", "reset":
			return "button"
		}
	case "textarea":
		return "textbox"
	case "select":
		return "combobox"
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return "heading"
	case "article":
		return "article"
	case "nav":
		return "navigation"
	case "aside":
		return "complementary"
	case "dialog":
		return "dialog"
	case "ul", "ol":
		return "list"
	case "li":
		return "listitem"
	case "section", "form":
		if walkHasAttr(n, "aria-label") || walkHasAttr(n, "aria-labelledby") {
			if n.Data == "form" {
				return "form"
			}
			return "region"
		}
	}
	return ""
}

// walkNamesFromContent are the roles whose name is read from what they contain.
var walkNamesFromContent = map[string]bool{"button": true, "link": true, "heading": true, "tab": true, "menuitem": true, "menuitemradio": true, "menuitemcheckbox": true, "option": true, "radio": true, "switch": true, "checkbox": true}

func walkText(n *xhtml.Node, sb *strings.Builder) {
	switch n.Type {
	case xhtml.TextNode:
		sb.WriteString(n.Data)
	case xhtml.ElementNode:
		// Only an element hidden in its own right drops out of a name: a button
		// inside a closed panel still has its name, the panel is what is hidden.
		if walkHasAttr(n, "hidden") {
			return
		}
		if v, _ := walkAttr(n, "aria-hidden"); v == "true" {
			return
		}
		if l, ok := walkAttr(n, "aria-label"); ok && strings.TrimSpace(l) != "" {
			sb.WriteString(" " + l + " ")
			return
		}
		sb.WriteString(" ")
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkText(c, sb)
		}
		sb.WriteString(" ")
	}
}

func walkContent(n *xhtml.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkText(c, &sb)
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

func walkByID(root *xhtml.Node, id string) *xhtml.Node {
	var found *xhtml.Node
	var visit func(*xhtml.Node)
	visit = func(n *xhtml.Node) {
		if found != nil {
			return
		}
		if n.Type == xhtml.ElementNode {
			if v, _ := walkAttr(n, "id"); v == id {
				found = n
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(root)
	return found
}

// walkName is the accessible name by the order the ARIA name algorithm takes:
// labelledby, aria-label, a native label, the content for roles that read it,
// then title and placeholder. Spaces are put between child elements, which is
// what a browser does for the flex rows these buttons are laid out in.
func walkName(root, n *xhtml.Node, role string) string {
	if ids, ok := walkAttr(n, "aria-labelledby"); ok {
		var parts []string
		for _, id := range strings.Fields(ids) {
			if t := walkByID(root, id); t != nil {
				var sb strings.Builder
				walkText(t, &sb)
				parts = append(parts, strings.Join(strings.Fields(sb.String()), " "))
			}
		}
		if s := strings.TrimSpace(strings.Join(parts, " ")); s != "" {
			return s
		}
	}
	if l, ok := walkAttr(n, "aria-label"); ok && strings.TrimSpace(l) != "" {
		return strings.TrimSpace(l)
	}
	if n.Data == "input" || n.Data == "textarea" || n.Data == "select" {
		if id, ok := walkAttr(n, "id"); ok && id != "" {
			var label string
			var visit func(*xhtml.Node)
			visit = func(x *xhtml.Node) {
				if label != "" {
					return
				}
				if x.Type == xhtml.ElementNode && x.Data == "label" {
					if f, _ := walkAttr(x, "for"); f == id {
						label = walkContent(x)
						return
					}
				}
				for c := x.FirstChild; c != nil; c = c.NextSibling {
					visit(c)
				}
			}
			visit(root)
			if label != "" {
				return label
			}
		}
		for p := n.Parent; p != nil; p = p.Parent {
			if p.Type == xhtml.ElementNode && p.Data == "label" {
				if s := walkContent(p); s != "" {
					return s
				}
			}
		}
	}
	if walkNamesFromContent[role] {
		if s := walkContent(n); s != "" {
			return s
		}
	}
	if t, ok := walkAttr(n, "title"); ok && strings.TrimSpace(t) != "" {
		return strings.TrimSpace(t)
	}
	if p, ok := walkAttr(n, "placeholder"); ok {
		return strings.TrimSpace(p)
	}
	return ""
}

// walkInventory lists every element of the markup that has a role.
func walkInventory(t *testing.T, markup string) []walkNode {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var out []walkNode
	var visit func(*xhtml.Node)
	visit = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			if role := walkRole(n); role != "" {
				closed, concealed := walkFlags(n)
				out = append(out, walkNode{Role: role, Name: walkName(root, n, role), Closed: closed, Concealed: concealed})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(root)
	return out
}

func walkRender(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// ---- the page, in each state the walk visits --------------------------------

func walkModel(selected string) Model {
	now := time.Now()
	return Model{
		State: StateReady, SelectedID: selected, CurrentUser: "hc-1", CurrentTenantID: "t1", Locale: "en-US", SavedOpenCount: 2,
		Conversations: []Conversation{
			{ID: "general", Name: "general", Kind: PublicChannel, Joined: true, MemberCount: 12, HostTenantID: "t1"},
			{ID: "incident", Name: "incident-review", Kind: PrivateChannel, Joined: true, MemberCount: 4, HostTenantID: "t1"},
			{ID: "ann", Name: "announcements", Kind: PublicChannel, Joined: true, MemberCount: 40, HostTenantID: "t1"},
			{ID: "dm-policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, HostTenantID: "t1"},
			{ID: "dm-assistant", Name: "Assistant", Kind: DirectMessage, Agent: true, HostTenantID: "t1"},
			{ID: "dm-loretta", Name: "Loretta Haynes", Kind: DirectMessage, HostTenantID: "t1"},
			{ID: "grp", Name: "Q4 hiring huddle", Kind: GroupChat, HostTenantID: "t1"},
		},
		Messages: []Message{
			{ID: "p1", AuthorID: "hc-2", Author: "Ann Lee", Body: "Welcome to the channel", SentAt: now.Add(-time.Hour)},
			{ID: "p2", AuthorID: "hc-1", Author: "Walt Brennan", Body: "Thanks all", SentAt: now.Add(-30 * time.Minute)},
		},
		PeerIDs:       map[string]string{"dm-loretta": "hc-3"},
		Callbacks:     walkCallbacks(),
		Members:       []Member{{ID: "hc-1", Name: "Walt Brennan"}, {ID: "hc-2", Name: "Ann Lee"}, {ID: "hc-3", Name: "Loretta Haynes"}},
		ChannelPins:   []ChannelPin{{PostID: "p1", Author: "Ann Lee", Body: "Welcome"}},
		Chatattach001: &Chatattach001Composer{Choose: func() {}},
	}
}

// walkStates renders the page once for every state a step of the walk is in.
func walkStates(t *testing.T) map[string]string {
	t.Helper()
	now := time.Now()
	general := walkModel("general")
	states := map[string]string{"general": render(t, general)}
	msg := general.Messages[1]
	hover := handlers{local: localUI{pointerRow: msg.ID}}
	states["bar"] = walkRender(t, html.Div(html.Props{}, message(general, hover, msg, false)))
	menuOpen := general
	menuOpen.MenuID = msg.ID
	states["menu"] = walkRender(t, html.Div(html.Props{}, message(menuOpen, hover, msg, false)))
	thread := general
	thread.ShowThread, thread.ThreadParentID, thread.ThreadParent = true, "p1", &thread.Messages[0]
	states["thread"] = render(t, thread)
	details := general
	details.ShowDetails = true
	states["details"] = render(t, details)
	agents := general
	agents.ResolvedPersonaMentions = []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "t1", ID: "agent:policy", Display: "Policy Helper", ConversationID: "general"}}}
	agents.mentions, agents.mentionsReady = mentionIndex(agents), true
	states["agents-here"] = render(t, agents)
	phone := general
	phone.SidebarOpen = true
	states["rail-open"] = render(t, phone)
	create := general
	create.ShowCreate = true
	states["create"] = render(t, create)
	browse := general
	browse.ShowBrowse = true
	states["browse"] = render(t, browse)
	railMenu := general
	railMenu.RailMenuID = "general"
	states["rail-menu"] = render(t, railMenu)
	for _, conversation := range []string{"dm-loretta", "dm-policy", "grp", "incident", "ann", "dm-assistant"} {
		states["conversation:"+conversation] = render(t, walkModel(conversation))
	}
	search := general
	search.Search = "holiday"
	search.SearchChannels = []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}}
	search.SearchPeople = []SearchPerson{{ID: "hc-3", Name: "Loretta Haynes"}}
	search.SearchMessages = []SearchMessage{{ConversationID: "general", ConversationName: "general", Message: Message{ID: "p1", Author: "Ann Lee", Body: "holiday plans"}}}
	states["search"] = render(t, search)
	rows := []SavedMessageRow{
		{TenantID: "t1", ConversationID: "general", PostID: "p1", Author: "Ann Lee", AuthorID: "hc-2", Channel: "general", InChannel: true, Body: "Welcome", SentAt: now.Add(-time.Hour), Availability: "readable"},
		{TenantID: "t1", ConversationID: "general", PostID: "p2", Author: "Ann Lee", AuthorID: "hc-2", Channel: "general", InChannel: true, Body: "Done one", SentAt: now.Add(-time.Hour), Availability: "readable", Done: true, DoneAt: now},
	}
	for _, tab := range []string{"todo", "done"} {
		states["saved-"+tab] = walkRender(t, RenderSavedMessages(SavedMessagesView{Locale: "en-US", Tab: tab, Rows: rows, Model: general, Now: now}))
	}
	states["tray-poll"] = walkRender(t, channelTray(general, handlers{}, "poll"))
	states["tray-todo"] = walkRender(t, channelTray(general, handlers{}, "todo"))
	states["mention"] = walkRender(t, mentionMenu(agents, mentionState{Target: "chat-composer", Query: "pol", Open: true}, "chat-composer"))
	states["commands"] = walkRender(t, composerCommandMenuView(general, composerCommandMenu{Target: "chat-composer", Open: true}, "chat-composer", 1))
	answer := general
	answer.PersonaInvocations = []PersonaThreadInvocation{{PostID: "q", Projection: PersonaProgressProjection{InvocationID: "inv", DurablePostID: "ans"}}}
	states["feedback"] = walkRender(t, renderAgentFeedback(answer, localUI{}, "ans"))
	states["sources"] = walkRender(t, html.Div(html.Props{}, renderAgentReplySources(general, agentReplyEnvelope{Sources: []agentReplySource{{Title: "Leave policy", Href: "/doc/1"}, {Title: "Hidden"}}})...))
	return states
}

func walkUnion(t *testing.T, states map[string]string) []walkNode {
	t.Helper()
	var all []walkNode
	for _, markup := range states {
		all = append(all, walkInventory(t, markup)...)
	}
	return all
}

// ---- reading the locators out of the script ------------------------------------

// walkJSRegex is a regular expression literal of the script: /…/flags.
const walkJSRegex = `/(?:\\.|\[(?:\\.|[^\]\\])*\]|[^/\\\n\[])+/[a-z]*`

var (
	walkRolePat   = regexp.MustCompile(`role:\s*'(\w+)'(?:\s*,\s*(?:attr:\s*'[^']*',\s*)?name:\s*(` + walkJSRegex + `|'[^']*'))?`)
	walkByRolePat = regexp.MustCompile(`getByRole\('(\w+)'(?:\s*,\s*\{\s*name:\s*(` + walkJSRegex + `|'[^']*'))?`)
	// Helpers that take a role-and-name regular expression as an argument.
	walkMsgActionPat  = regexp.MustCompile(`(?:msgAction\(s,\s*\w+,\s*'[^']*',|toolbarBtn\(s,\s*\w+,)\s*(` + walkJSRegex + `)`)
	walkAddItemPat    = regexp.MustCompile(`addMenuItem\(s,\s*'[^']*',\s*(` + walkJSRegex + `)`)
	walkViaAddFindPat = regexp.MustCompile(`viaAddFind\(s,\s*'[^']*',\s*(` + walkJSRegex + `)`)
	walkGoConvPat     = regexp.MustCompile(`goConv\(s,\s*'([^']+)'\)`)
	walkRowListPat    = regexp.MustCompile(`\['[^']*',\s*'([^']+)'\]`)
)

type walkLocator struct {
	Role, Source string // Source is the name as written, empty when the locator has none
	Name         *regexp.Regexp
	Literal      string
	Line         int
}

func (l walkLocator) String() string {
	if l.Source == "" {
		return fmt.Sprintf("role %q (line %d)", l.Role, l.Line)
	}
	return fmt.Sprintf("role %q name %s (line %d)", l.Role, l.Source, l.Line)
}

// walkCompile turns the script's regular expression literal into a Go one.
func walkCompile(literal string) (*regexp.Regexp, error) {
	end := strings.LastIndex(literal, "/")
	body, flags := literal[1:end], literal[end+1:]
	prefix := ""
	if strings.Contains(flags, "i") {
		prefix = "(?i)"
	}
	return regexp.Compile(prefix + body)
}

func walkLine(script string, index int) int { return strings.Count(script[:index], "\n") + 1 }

func walkLocators(t *testing.T, script string) []walkLocator {
	t.Helper()
	var out []walkLocator
	add := func(role, name string, index int) {
		l := walkLocator{Role: role, Source: name, Line: walkLine(script, index)}
		switch {
		case strings.HasPrefix(name, "/"):
			re, err := walkCompile(name)
			if err != nil {
				t.Fatalf("locator %s is not a regular expression this test can read: %v", l, err)
			}
			l.Name = re
		case strings.HasPrefix(name, "'"):
			l.Literal = strings.Trim(name, "'")
		}
		out = append(out, l)
	}
	for _, pat := range []*regexp.Regexp{walkRolePat, walkByRolePat} {
		for _, m := range pat.FindAllStringSubmatchIndex(script, -1) {
			name := ""
			if m[4] >= 0 {
				name = script[m[4]:m[5]]
			}
			add(script[m[2]:m[3]], name, m[0])
		}
	}
	// A message action is a button in the hover bar, or a menu item of More actions on a phone.
	for _, m := range walkMsgActionPat.FindAllStringSubmatchIndex(script, -1) {
		add("button", script[m[2]:m[3]], m[0])
	}
	for _, m := range walkAddItemPat.FindAllStringSubmatchIndex(script, -1) {
		add("menuitem", script[m[2]:m[3]], m[0])
	}
	for _, m := range walkViaAddFindPat.FindAllStringSubmatchIndex(script, -1) {
		add("button", script[m[2]:m[3]], m[0])
	}
	return out
}

func (l walkLocator) matches(nodes []walkNode) bool {
	for _, n := range nodes {
		if n.Concealed || n.Role != l.Role {
			continue
		}
		switch {
		case l.Name != nil:
			if l.Name.MatchString(n.Name) {
				return true
			}
		case l.Literal != "":
			if strings.Contains(strings.ToLower(n.Name), strings.ToLower(l.Literal)) {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// walkExempt are the locators of the script that are not looked for in the Chat
// page's markup, and why. A locator that must be absent from the page is
// reported as stale once the page has it.
var walkExempt = map[string]struct {
	reason     string
	mustBeGone bool
}{
	`button|/Walt Brennan/`: {"the sign-in page's account button, not a control of the Chat page", false},
	`dialog|/chat navigation/i`: {"mobile_rail_focus_js.go setMobileRailModal gives the open phone drawer role=dialog; " +
		"in markup it is the navigation landmark of the same name (checked in the Browser test)", true},
}

func walkKey(l walkLocator) string { return l.Role + "|" + l.Source }

// ---- what the script does around a locator -------------------------------------

const (
	walkLocatingMarker = "// ---------------------------------------------------------------- locating controls"
	walkStepsMarker    = "// ---------------------------------------------------------------- steps"
	walkReportMarker   = "// ---------------------------------------------------------------- report"
)

// walkStripCalls removes the whole of every call that starts with one of the
// openers (page.evaluate(...) and the like): what is inside runs in the page,
// where getAttribute and textContent are the browser's own and cannot time out.
func walkStripCalls(src string, openers ...string) string {
	for _, opener := range openers {
		for {
			at := strings.Index(src, opener)
			if at < 0 {
				break
			}
			depth, i := 1, at+len(opener)
			for i < len(src) && depth > 0 {
				switch c := src[i]; c {
				case '(':
					depth++
				case ')':
					depth--
				case '\'', '"', '`':
					// A quote starts a string only where an expression can start; the
					// one in /can't/ follows a letter and is part of a regular expression.
					j := i - 1
					for j >= 0 && src[j] == ' ' {
						j--
					}
					if j >= 0 && !strings.ContainsRune("([{,:=+?!&|<>", rune(src[j])) {
						break
					}
					for i++; i < len(src) && src[i] != c; i++ {
						if src[i] == '\\' {
							i++
						}
					}
				}
				i++
			}
			src = src[:at] + "EVAL" + src[i:]
		}
	}
	return src
}

var walkRawCalls = regexp.MustCompile(`\.(click|dblclick|fill|hover|pressSequentially|check|setChecked|tap|focus|selectOption|dragTo|waitFor|inputValue|innerText|textContent|getAttribute|boundingBox|scrollIntoViewIfNeeded|evaluate|evaluateAll)\(|page\.keyboard\.`)

// walkCheckScript is every way the script can have drifted from the page: a
// locator that no element of the page matches, a step that can end in a locator
// timeout, and the helpers that turn a missing control into a finding.
func walkCheckScript(t *testing.T, script string, nodes []walkNode) []string {
	t.Helper()
	var problems []string
	locating, from, to := strings.Index(script, walkLocatingMarker), strings.Index(script, walkStepsMarker), strings.Index(script, walkReportMarker)
	if locating < 0 || from < locating || to < from {
		return []string{"the script has lost its locating, steps or report section markers"}
	}
	used := map[string]bool{}
	// The self-test and the page-side audit above the locating section run against
	// a synthetic page and the browser's own DOM; only what follows finds Chat's controls.
	for _, l := range walkLocators(t, script[locating:to]) {
		key := walkKey(l)
		entry, exempt := walkExempt[key]
		if exempt {
			used[key] = true
			if entry.mustBeGone && l.matches(nodes) {
				problems = append(problems, fmt.Sprintf("%s is exempt (%s) but the page now has it: remove the exemption", l, entry.reason))
			}
			continue
		}
		if !l.matches(nodes) {
			problems = append(problems, fmt.Sprintf("no element of the page matches %s", l))
		}
	}
	for key := range walkExempt {
		if !used[key] {
			problems = append(problems, fmt.Sprintf("exemption %q names a locator the script no longer has", key))
		}
	}

	// Conversation rows are found by rowRe(name), built in the script. This is
	// its mirror: the script must still build the same expression.
	for _, fragment := range []string{
		`'^(?:(?:Public|Private) channel |Private group |Direct message |Group )?#?\\s*'`,
		`'(?:\\s+Agent)?\\s*\\d*$'`,
	} {
		if !strings.Contains(script, fragment) {
			problems = append(problems, "rowRe no longer builds "+fragment+": update the mirror in walkRowName")
		}
	}
	rails := walkInventoryRows(t, nodes)
	seen := map[string]bool{}
	var rowNames []string
	for _, m := range walkGoConvPat.FindAllStringSubmatch(script[from:to], -1) {
		rowNames = append(rowNames, m[1])
	}
	for _, m := range walkRowListPat.FindAllStringSubmatch(script[from:to], -1) {
		rowNames = append(rowNames, m[1])
	}
	for _, name := range rowNames {
		if seen[name] {
			continue
		}
		seen[name] = true
		ok := false
		for _, row := range rails {
			if walkRowName(name).MatchString(row) {
				ok = true
			}
		}
		if !ok {
			problems = append(problems, fmt.Sprintf("no sidebar row is named like conversation %q (rows: %q)", name, rails))
		}
	}

	// Steps: no raw press, hover, fill or read of a located control, so that none
	// can end in a locator timeout.
	steps := script[from:to]
	if n := strings.Count(steps, "await step('"); n < 40 {
		problems = append(problems, fmt.Sprintf("only %d steps found; the walk has 59", n))
	}
	stripped := walkStripCalls(steps, "page.evaluate(", "evalOn(")
	for _, m := range walkRawCalls.FindAllStringIndex(stripped, -1) {
		line := strings.Count(script[:from], "\n") + strings.Count(stripped[:m[0]], "\n") + 1
		problems = append(problems, fmt.Sprintf("line %d calls %q on a located control or the keyboard outside the wrapper helpers (a timeout there ends the step)", line, stripped[m[0]:m[1]]))
	}
	for _, want := range []string{
		"const SHORT = 1500;",                          // the short wait of every press
		"timeout: SHORT",                               // click and hover use it
		"if (o.record !== false) s.absent(what);",      // ctl: not found within the wait is absent
		"s.absent(`${what_(c)} (could not be pressed:", // click: a control that cannot be pressed is absent
		"Timeout \\d+ms exceeded",                      // step: a timeout that gets past the helpers is absent, not an error
	} {
		if !strings.Contains(script, want) {
			problems = append(problems, "the script no longer has "+want)
		}
	}
	if regexp.MustCompile(`timeout: 4000([^0-9]|$)`).MatchString(script) {
		problems = append(problems, "click waits 4000 ms again instead of SHORT")
	}
	return problems
}

// walkRowName is the script's rowRe(text) in Go.
func walkRowName(text string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)^(?:(?:Public|Private) channel |Private group |Direct message |Group )?#?\s*` + regexp.QuoteMeta(text) + `(?:\s+Agent)?\s*\d*$`)
}

// walkInventoryRows are the names of the sidebar's conversation rows: buttons
// carrying aria-current, which More options, Ask and View buttons never do.
func walkInventoryRows(t *testing.T, _ []walkNode) []string {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(render(t, walkModel("general"))))
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	var visit func(*xhtml.Node)
	visit = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "button" && walkHasAttr(n, "aria-current") {
			rows = append(rows, walkName(root, n, "button"))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(root)
	return rows
}

// TestTodo_CHATBUG_036 holds the walk script to the page: every locator it
// uses by role and name has an element to match in the page's markup, every
// step is built so that a missing control is a finding and not a timeout, and
// the checks themselves are shown to fail on a script that has drifted.
func TestTodo_CHATBUG_036(t *testing.T) {
	script := walkScript(t)
	nodes := walkUnion(t, walkStates(t))
	if problems := walkCheckScript(t, script, nodes); len(problems) > 0 {
		t.Fatalf("the walk script and the page disagree:\n  %s", strings.Join(problems, "\n  "))
	}

	// The checks can fail: each of these is the kind of drift this todo is about.
	cases := []struct {
		name, from, to, want string
		all                  bool
	}{
		{"header poll button the page no longer has", "const detailsBtn =", "const gone = (s) => ctl(s, 'x', [{ role: 'button', name: /^channel poll/i, within: region }]);\nconst detailsBtn =", "no element of the page matches role \"button\" name /^channel poll/i", false},
		{"a tab renamed", "role: 'tab', name: /^done\\b/i", "role: 'tab', name: /^finished\\b/i", "no element of the page matches role \"tab\" name /^finished\\b/i", false},
		{"a raw click that can time out", "await step('header: members', async s => {", "await step('header: members', async s => {\n  await page.getByRole('button', { name: /members/i }).click();", "outside the wrapper helpers", false},
		{"a long wait on click", "const SHORT = 1500;", "const SHORT = 1500;\nconst old = { timeout: 4000 };", "click waits 4000 ms again", false},
		{"timeouts become errors again", "Timeout \\d+ms exceeded", "Timeout", "the script no longer has Timeout", false},
		{"a row that is gone", "'Loretta Haynes'", "'Nobody Here'", "no sidebar row is named like conversation \"Nobody Here\"", false},
		{"a stale exemption", "getByRole('dialog', { name: /chat navigation/i })", "getByRole('dialog', { name: /nav/i })", "names a locator the script no longer has", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(script, c.from) {
				t.Fatalf("the case's anchor %q is not in the script", c.from)
			}
			mutated := strings.Replace(script, c.from, c.to, 1)
			if c.all {
				mutated = strings.ReplaceAll(script, c.from, c.to)
			}
			problems := strings.Join(walkCheckScript(t, mutated, nodes), "\n")
			if !strings.Contains(problems, c.want) {
				t.Fatalf("the drift was not reported (want %q), problems: %s", c.want, problems)
			}
		})
	}
}

// ---- the page states the walk's paths depend on -----------------------------

func walkHas(nodes []walkNode, role, pattern string) bool {
	re := regexp.MustCompile(pattern)
	for _, n := range nodes {
		if !n.Concealed && n.Role == role && re.MatchString(n.Name) {
			return true
		}
	}
	return false
}

// TestTodo_CHATBUG_036_Browser checks, state by state, the controls the walk
// reaches and the way it reaches them: the Add menu for a poll, a to-do list,
// a location and a voice message, the Saved panel's tabs, the thread and
// details panes, the sidebar's disclosure rows, the phone's drawer and
// More actions path. What a control is absent from is as much of the contract
// as what has it: the walk reports those as findings.
func TestTodo_CHATBUG_036_Browser(t *testing.T) {
	states := walkStates(t)
	in := func(state string) []walkNode { return walkInventory(t, states[state]) }
	type want struct {
		state, role, name string
		present           bool
	}
	wants := []want{
		// The page and the sidebar, at both widths.
		{"general", "region", `^Conversation messages$`, true},
		{"general", "searchbox", `^Search Chat$`, true},
		{"general", "button", `^Saved 2$`, true},
		{"general", "button", `^Channels 3$`, true},
		{"general", "button", `^Direct messages 4$`, true},
		{"general", "button", `^Add or find channels$`, true},
		{"general", "dialog", `^Add or find channels$`, true},
		{"general", "button", `^Browse channels `, true},
		{"general", "button", `^New section `, true},
		{"general", "textbox", `^Section name$`, true},
		{"general", "button", `^Chat preferences$`, true},
		{"general", "dialog", `^Chat preferences$`, true},
		{"general", "region", `^Quiet hours$`, true},
		{"general", "switch", `^Pause notifications overnight$`, true},
		{"general", "region", `^Reading language$`, true},
		{"general", "button", `^Change$`, true},
		{"general", "button", `^New conversation$`, true},
		{"general", "button", `^Open conversations$`, true},
		{"general", "button", `^Public channel general$`, true},
		{"general", "button", `^More options for general$`, true},
		{"general", "button", `^Policy Helper Agent$`, true},
		{"general", "button", `^Private group Q4 hiring huddle$`, true},
		{"rail-open", "button", `^Close conversations$`, true},
		{"rail-menu", "menu", `^More options for general$`, true},
		{"rail-menu", "menuitem", `^Leave channel$`, true},
		// The header: Search, Pinned, Members and Details; no button for a poll or a to-do list.
		{"general", "button", `^Search Chat$`, true},
		{"general", "button", `^Pinned messages \(1\)$`, true},
		{"general", "button", `^Members \(12\)$`, true},
		{"general", "button", `^Conversation details$`, true},
		{"general", "button", `(?i)to-?do|poll`, false},
		{"agents-here", "button", `^1 agent$`, true},
		// The composer: tools in the row, a poll, a to-do list and a location in the Add menu.
		{"general", "textbox", `^Message$`, true},
		{"general", "button", `^Add to your message$`, true},
		{"general", "menu", `^Add to your message$`, true},
		{"general", "menuitem", `^Poll$`, true},
		{"general", "menuitem", `^To-do list$`, true},
		{"general", "menuitem", `^Location$`, true},
		{"general", "menuitem", `^Voice message$`, false}, // direct and group conversations only
		{"general", "button", `^Mention someone$`, true},
		{"general", "button", `^Insert emoji$`, true},
		{"general", "button", `^Formatting$`, true},
		{"general", "button", `^Bold$`, true},
		{"general", "dialog", `^Location$`, true},
		{"mention", "listbox", `^Mention someone$`, true},
		{"commands", "listbox", `^Commands$`, true},
		{"commands", "option", `^/giphy `, true},
		{"tray-poll", "dialog", `^Channel poll$`, true},
		{"tray-todo", "dialog", `^To-do list$`, true},
		{"tray-poll", "button", `^Close$`, true},
		// A direct or group conversation: Voice message in the Add menu and its recorder.
		{"conversation:dm-loretta", "menuitem", `^Voice message$`, true},
		{"conversation:dm-loretta", "dialog", `^Record voice message$`, true},
		{"conversation:grp", "menuitem", `^Voice message$`, true},
		{"conversation:incident", "menuitem", `^Voice message$`, false},
		{"conversation:dm-policy", "menuitem", `^Poll$`, false},
		// A message: the hover bar, its menu, and the two things a phone's bar leaves to the menu.
		{"bar", "toolbar", `^Message actions$`, true},
		{"bar", "button", `^Add reaction$`, true},
		{"bar", "button", `^Save for later$`, true},
		{"bar", "button", `^Reply in thread$`, true},
		{"bar", "button", `^More actions$`, true},
		{"menu", "menu", `^More actions$`, true},
		{"menu", "menuitem", `^Add reaction$`, true},
		{"menu", "menuitem", `^Save for later$`, true},
		{"menu", "menuitem", `^Reply in thread$`, true},
		{"general", "dialog", `^Choose an emoji$`, true},
		{"general", "combobox", `^Search emoji$`, true},
		// The Saved panel, its tabs and its close icon.
		{"general", "dialog", `^Saved messages$`, true},
		{"saved-done", "tab", `^To do 1$`, true},
		{"saved-done", "tab", `^Done 1$`, true},
		{"saved-done", "tab", `^All 2$`, true},
		{"saved-done", "button", `^Close$`, true},
		{"saved-todo", "listitem", `^Open in the conversation$`, true},
		// Panes.
		{"thread", "complementary", `^Thread replies$`, true},
		{"thread", "button", `^Close thread$`, true},
		{"thread", "textbox", `^Reply$`, true},
		{"details", "complementary", `^Conversation details$`, true},
		{"details", "button", `^Close details$`, true},
		{"general", "complementary", `^Conversation details$`, false}, // closed: aria-hidden
		{"create", "dialog", `^Create conversation$`, true},
		{"create", "radio", `^Direct message `, true},
		{"browse", "dialog", `^Browse channels$`, true},
		{"browse", "button", `^Open$`, true},
		// Search and the agent's answer.
		{"search", "region", `^Results for “holiday”$`, true},
		{"feedback", "button", `^Helpful$`, true},
		{"feedback", "button", `^Not right$`, true},
		{"sources", "region", `^Sources$`, true},
	}
	for _, w := range wants {
		if got := walkHas(in(w.state), w.role, w.name); got != w.present {
			t.Errorf("%s: %s named /%s/ present = %v, want %v", w.state, w.role, w.name, got, w.present)
		}
	}

	// The conversation rows keep the roles the walk's row locator reads.
	rows := walkInventoryRows(t, nil)
	for _, row := range []string{"general", "incident-review", "announcements", "Policy Helper", "Assistant", "Loretta Haynes", "Q4 hiring huddle"} {
		found := false
		for _, name := range rows {
			if walkRowName(row).MatchString(name) {
				found = true
			}
		}
		if !found {
			t.Errorf("no sidebar row reads as %q: %q", row, rows)
		}
	}

	// The phone's way in. The workspace's drawer is the navigation landmark in
	// markup and a modal dialog of the same name once opened at 760 px or less;
	// the walk opens it by "Open conversations" and finds the dialog by that name.
	if !walkHas(in("general"), "navigation", `^Chat navigation$`) {
		t.Error("the sidebar is no longer the navigation landmark named Chat navigation")
	}
	source, err := os.ReadFile("mobile_rail_focus_js.go")
	if err != nil || !strings.Contains(string(source), `rail.Call("setAttribute", "role", "dialog")`) {
		t.Errorf("the phone drawer no longer becomes role=dialog (%v): the walk's railDlg locator would not find it", err)
	}
	// At 760 px and under a message's bar shows More actions alone (the other
	// actions are display:none there), so the walk reaches Add reaction, Save and
	// Reply through the More actions menu; it looks for the bar's own button first.
	if !strings.Contains(Stylesheet, `@media(max-width:760px){.chat-layout,`) || !strings.Contains(Stylesheet, `.message-actions .message-action:not([data-action="menu"]){display:none}`) {
		t.Error("the phone's message bar no longer reduces to More actions: update msgAction's phone path")
	}
	// The composer's Location and Voice buttons are drawn transparent and take
	// no pointer events; the Add menu is the way to them, so the walk never presses them.
	if !strings.Contains(Stylesheet, `.chatmap-control>.tool-button{position:absolute;inset-block-start:0;inset-inline-start:0;background:transparent;color:transparent;pointer-events:none}`) {
		t.Error("the Location and Voice buttons are pressable again: the walk could press them directly")
	}
}
