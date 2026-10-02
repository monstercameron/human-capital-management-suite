package chatui

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

func modTestEntropy(b byte) *bytes.Reader { return bytes.NewReader([]byte{b, b + 1, b + 2}) }

func TestTodo_CHATMOD_003_Editor(t *testing.T) {
	t.Run("a new rule gets a unique id and an edit keeps its own", func(t *testing.T) {
		a, err := ModNewRuleID("Project", modTestEntropy(1))
		if err != nil || a != "project-010203" {
			t.Fatalf("id %q %v", a, err)
		}
		b, _ := ModNewRuleID("Project", modTestEntropy(9))
		if a == b {
			t.Fatal("two channels' Project filters collide")
		}
		if id, _ := ModNewRuleID("  Q4 Hiring -- Plan!  ", modTestEntropy(1)); id != "q4-hiring-plan-010203" {
			t.Fatalf("slug %q", id)
		}
		if id, _ := ModNewRuleID("مشروع", modTestEntropy(1)); id != "filter-010203" {
			t.Fatalf("a name with no plain letters %q", id)
		}
		if id, _ := ModNewRuleID("builtin", modTestEntropy(1)); strings.HasPrefix(id, "builtin-") {
			t.Fatalf("a custom id may never look like a product list: %q", id)
		}
		if _, err := ModNewRuleID("x", bytes.NewReader(nil)); err == nil {
			t.Fatal("an unreadable entropy source made an id")
		}
		if id, err := ModNewRuleID("x", nil); err != nil || len(id) != len("x-")+6 {
			t.Fatalf("default entropy %q %v", id, err)
		}
	})

	t.Run("an edit saves the next patch version", func(t *testing.T) {
		for in, want := range map[string]string{"1.0.0": "1.0.1", "1.2.9": "1.2.10", "2.0.0": "2.0.1", "bad": "1.0.1", "": "1.0.1", "1.0.9999999": "1.0.1"} {
			if got := ModNextVersion(in); got != want {
				t.Errorf("ModNextVersion(%q) = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("a definition per kind", func(t *testing.T) {
		base := ModForm{Name: "Project names", Kind: ModKindWords, Match: " Phoenix \n\nAcme Corp\r\n", Action: "mask", Scope: "channel", Channel: "room", Roles: "manager\n", Agents: "helper"}
		d, problem := ModDefinitionFromForm(base, modTestEntropy(1))
		if problem != nil {
			t.Fatal(problem)
		}
		if d.ID != "project-names-010203" || d.Version != "1.0.0" || d.Kind != "words" || !reflect.DeepEqual(d.Match, []string{"Phoenix", "Acme Corp"}) || !reflect.DeepEqual(d.Channels, []string{"room"}) || !reflect.DeepEqual(d.ExemptRoles, []string{"manager"}) || !reflect.DeepEqual(d.ExemptAgents, []string{"helper"}) || d.Action != "mask" {
			t.Fatalf("words: %+v", d)
		}
		pattern := base
		pattern.Kind, pattern.Match = ModKindPattern, "PX-[0-9]{4}"
		if d, problem = ModDefinitionFromForm(pattern, modTestEntropy(1)); problem != nil || d.Kind != "pattern" || d.Match[0] != "PX-[0-9]{4}" {
			t.Fatalf("pattern: %+v %v", d, problem)
		}
		sensitive := base
		sensitive.Kind, sensitive.Detector = ModKindSensitive, "card"
		if d, problem = ModDefinitionFromForm(sensitive, modTestEntropy(1)); problem != nil || d.Kind != "detector" || !reflect.DeepEqual(d.Match, []string{"card"}) {
			t.Fatalf("sensitive: %+v %v", d, problem)
		}
		sensitive.Detector = "everything"
		if _, problem = ModDefinitionFromForm(sensitive, modTestEntropy(1)); problem == nil || problem.Field != "detector" || problem.Key != "v_sensitive" {
			t.Fatalf("an unknown detector was accepted: %v", problem)
		}
		links := base
		links.Kind, links.Domains = ModKindLinks, "https://Docs.Example.com/page\nexample.org:8080\n\n"
		if d, problem = ModDefinitionFromForm(links, modTestEntropy(1)); problem != nil || d.Kind != "detector" || !reflect.DeepEqual(d.Match, []string{"external-link", "docs.example.com", "example.org"}) {
			t.Fatalf("links: %+v %v", d, problem)
		}
		links.Domains = ""
		if d, problem = ModDefinitionFromForm(links, modTestEntropy(1)); problem != nil || !reflect.DeepEqual(d.Match, []string{"external-link"}) {
			t.Fatalf("links with no allowed site: %+v %v", d, problem)
		}
		attachment := base
		attachment.Kind, attachment.Match = ModKindAttachment, "Application/ZIP\nimage/png"
		if d, problem = ModDefinitionFromForm(attachment, modTestEntropy(1)); problem != nil || d.Kind != "attachment" || !reflect.DeepEqual(d.Match, []string{"application/zip", "image/png"}) {
			t.Fatalf("attachment: %+v %v", d, problem)
		}
	})

	t.Run("each problem is tied to its field and says why in plain words", func(t *testing.T) {
		ok := ModForm{Name: "N", Kind: ModKindWords, Match: "word", Action: "block", Scope: "channel", Channel: "room"}
		cases := []struct {
			name         string
			mutate       func(*ModForm)
			field, key   string
			mustNotLeak  string
			expectNoProb bool
		}{
			{name: "no name", mutate: func(f *ModForm) { f.Name = "  " }, field: "name", key: "v_name"},
			{name: "long name", mutate: func(f *ModForm) { f.Name = strings.Repeat("n", 101) }, field: "name", key: "v_name_long"},
			{name: "no words", mutate: func(f *ModForm) { f.Match = "\n \n" }, field: "match", key: "v_words"},
			{name: "no pattern", mutate: func(f *ModForm) { f.Kind, f.Match = ModKindPattern, "" }, field: "match", key: "v_pattern"},
			{name: "a pattern that could backtrack", mutate: func(f *ModForm) { f.Kind, f.Match = ModKindPattern, "(a+)+b" }, field: "match", key: "v_pattern_bad"},
			{name: "an open wildcard", mutate: func(f *ModForm) { f.Kind, f.Match = ModKindPattern, "a.*b" }, field: "match", key: "v_pattern_bad"},
			{name: "a pattern that is not valid", mutate: func(f *ModForm) { f.Kind, f.Match = ModKindPattern, "([" }, field: "match", key: "v_pattern_bad"},
			{name: "no file type", mutate: func(f *ModForm) { f.Kind, f.Match = ModKindAttachment, "" }, field: "match", key: "v_attachment"},
			{name: "notify with nobody to tell", mutate: func(f *ModForm) { f.Action = "notify" }, field: "target", key: "v_target"},
			{name: "a line that is too long", mutate: func(f *ModForm) { f.Match = strings.Repeat("a", 513) }, field: "match", key: "v_line_long"},
			{name: "too many lines", mutate: func(f *ModForm) { f.Match = strings.Repeat("a\n", 129) }, field: "match", key: "v_too_many"},
			{name: "chosen channels but none chosen", mutate: func(f *ModForm) { f.Scope = "chosen" }, field: "scope", key: "v_channels"},
			{name: "a kind that does not exist", mutate: func(f *ModForm) { f.Kind = "" }, field: "kind", key: "v_invalid"},
		}
		for _, c := range cases {
			f := ok
			c.mutate(&f)
			_, problem := ModDefinitionFromForm(f, modTestEntropy(1))
			if problem == nil || problem.Field != c.field || problem.Key != c.key {
				t.Errorf("%s: got %v, want %s/%s", c.name, problem, c.field, c.key)
				continue
			}
			for locale := range []string{"en-US", "de-DE", "ar"} {
				text := modadminText(Model{Locale: []string{"en-US", "de-DE", "ar"}[locale]}, problem.Key)
				if text == "" || strings.Contains(text, "⟦") || strings.Contains(text, "builtin") || strings.Contains(text, "invalid definition") {
					t.Errorf("%s: no plain words for %s: %q", c.name, problem.Key, text)
				}
			}
		}
		if _, problem := ModDefinitionFromForm(ok, nil); problem != nil {
			t.Fatal(problem)
		}
	})

	t.Run("who may make which kind of filter", func(t *testing.T) {
		f := ModForm{Name: "N", Kind: ModKindWords, Match: "w", Action: "block", Scope: "workspace", Hard: true}
		if d, _ := ModDefinitionFromForm(f, modTestEntropy(1)); d.Hard || len(d.Channels) != 0 {
			t.Fatalf("a channel manager made a rule that also applies in direct messages: %+v", d)
		}
		f.Admin = true
		if d, _ := ModDefinitionFromForm(f, modTestEntropy(1)); !d.Hard {
			t.Fatal("an administrator's choice was dropped")
		}
		f.Scope, f.Chosen = "chosen", []string{"a", "b"}
		if d, problem := ModDefinitionFromForm(f, modTestEntropy(1)); problem != nil || !reflect.DeepEqual(d.Channels, []string{"a", "b"}) {
			t.Fatalf("chosen channels: %+v %v", d, problem)
		}
	})

	t.Run("an edit keeps its id, scope and bumps the patch", func(t *testing.T) {
		f := ModForm{Edit: true, ID: "project-ab12cd", Version: "1.4.2", KeepChannels: []string{"room"}, Name: "Project", Kind: ModKindWords, Match: "w", Action: "flag", Scope: "workspace", Channel: "other", Hard: true}
		d, problem := ModDefinitionFromForm(f, modTestEntropy(1))
		if problem != nil || d.ID != "project-ab12cd" || d.Version != "1.4.3" || !reflect.DeepEqual(d.Channels, []string{"room"}) {
			t.Fatalf("edit: %+v %v", d, problem)
		}
		if !d.Hard {
			t.Fatal("an edit of a rule that also applies in direct messages must keep that")
		}
	})

	t.Run("the Try box needs no name or place", func(t *testing.T) {
		d, problem := ModDefinitionFromForm(ModForm{Kind: ModKindWords, Match: "damn", Action: "block", Scope: "chosen", ForTry: true}, modTestEntropy(1))
		if problem != nil || d.Name == "" || len(d.Channels) != 0 {
			t.Fatalf("try definition: %+v %v", d, problem)
		}
		if _, problem = ModDefinitionFromForm(ModForm{Kind: ModKindPattern, Match: "(a+)+", Action: "block", ForTry: true}, modTestEntropy(1)); problem == nil || problem.Key != "v_pattern_bad" {
			t.Fatal("the Try box accepted a pattern the server refuses")
		}
	})

	t.Run("a stored definition lays out and builds back", func(t *testing.T) {
		for _, d := range []chatfilter.Definition{
			{ID: "a", Name: "A", Version: "1.0.0", Kind: "words", Action: "block", Match: []string{"one", "two"}},
			{ID: "b", Name: "B", Version: "1.0.0", Kind: "pattern", Action: "mask", Match: []string{"PX-[0-9]{4}"}},
			{ID: "c", Name: "C", Version: "1.0.0", Kind: "detector", Action: "flag", Match: []string{"secret"}},
			{ID: "d", Name: "D", Version: "1.0.0", Kind: "detector", Action: "block", Match: []string{"external-link", "example.com"}},
			{ID: "e", Name: "E", Version: "1.0.0", Kind: "attachment", Action: "notify", Target: "#security", Match: []string{"application/zip"}, ExemptRoles: []string{"manager"}, ExemptAgents: []string{"bot"}, Channels: []string{"room"}},
		} {
			v := ModFormValuesOf(d)
			got, problem := ModDefinitionFromForm(ModForm{Edit: true, ID: d.ID, Version: d.Version, KeepChannels: d.Channels, Name: v.Name, Kind: v.Kind, Match: v.Match, Detector: v.Detector, Domains: v.Domains, Action: v.Action, Target: v.Target, Roles: v.Roles, Agents: v.Agents}, nil)
			if problem != nil {
				t.Fatalf("%s: %v", d.ID, problem)
			}
			if !reflect.DeepEqual(got.Match, d.Match) || got.Kind != d.Kind || got.Action != d.Action || got.Target != d.Target || got.Version != ModNextVersion(d.Version) || !reflect.DeepEqual(got.ExemptRoles, d.ExemptRoles) || !reflect.DeepEqual(got.Channels, d.Channels) {
				t.Errorf("%s did not round trip: %+v vs %+v", d.ID, got, d)
			}
		}
	})
}

func TestTodo_CHATMOD_003_Editor_State(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	list := chatfilter.Definition{ID: "builtin-en-profanity", Name: "profanity", Language: "en", Kind: "words", Action: "block", Product: true}
	rows := func(r ...chatfilter.Enablement) []chatfilter.Enablement { return r }
	ws := func(on bool, action string) chatfilter.Enablement {
		return chatfilter.Enablement{RuleID: list.ID, Enabled: on, Action: action}
	}
	own := func(on bool, action string) chatfilter.Enablement {
		return chatfilter.Enablement{RuleID: list.ID, Channel: "room", Enabled: on, Action: action}
	}
	cases := []struct {
		name      string
		rows      []chatfilter.Enablement
		channel   string
		on        bool
		action    string
		hasOwn    bool
		differs   bool
		statusKey string
		reset     bool
	}{
		{"no row at all is off", nil, "room", false, "block", false, false, "s_off", false},
		{"the workspace has it on", rows(ws(true, "mask")), "room", true, "mask", false, false, "s_ws_on", false},
		{"the channel turns it off against the workspace", rows(ws(true, "block"), own(false, "block")), "room", false, "block", true, true, "s_chan_off", true},
		{"the channel turns it on against the workspace", rows(ws(false, ""), own(true, "flag")), "room", true, "flag", true, true, "s_chan_only", true},
		{"the channel turns it on with a row of its own, none for the workspace", rows(own(true, "block")), "room", true, "block", true, true, "s_chan_only", true},
		{"the channel's row beats the workspace's whichever order the server lists them", rows(own(false, "block"), ws(true, "block")), "room", false, "block", true, true, "s_chan_off", true},
		{"both on but the channel chose another outcome", rows(ws(true, "block"), own(true, "mask")), "room", true, "mask", true, true, "s_chan_own", true},
		{"a row of the channel equal to the workspace's is nothing to undo", rows(ws(true, "mask"), own(true, "mask")), "room", true, "mask", true, false, "s_ws_on", false},
		{"both off", rows(ws(false, "block"), own(false, "block")), "room", false, "block", true, false, "s_off", false},
		{"another channel's row is not this channel's", rows(ws(true, "block"), chatfilter.Enablement{RuleID: list.ID, Channel: "elsewhere", Enabled: false}), "room", true, "block", false, false, "s_ws_on", false},
		{"on the workspace page only the workspace row counts", rows(ws(true, "mask"), own(false, "block")), "", true, "mask", false, false, "s_ws_on", false},
		{"a row for another list is ignored", rows(chatfilter.Enablement{RuleID: "builtin-de-profanity", Enabled: true}), "room", false, "block", false, false, "s_off", false},
	}
	for _, c := range cases {
		state := ModResolve(list, c.rows, c.channel, now)
		if state.On != c.on || state.Action != c.action || state.HasOverride != c.hasOwn || state.Differs != c.differs {
			t.Errorf("%s: %+v", c.name, state)
		}
		if key := ModStatusKey(state, c.channel == ""); key != c.statusKey {
			t.Errorf("%s: status %s, want %s", c.name, key, c.statusKey)
		}
		if reset := state.HasOverride && state.Differs; reset != c.reset {
			t.Errorf("%s: reset offered %v", c.name, reset)
		}
	}

	t.Run("a recording row says so", func(t *testing.T) {
		state := ModResolve(list, rows(chatfilter.Enablement{RuleID: list.ID, Enabled: true, DryRunUntil: now.Add(time.Hour)}), "room", now)
		if !state.On || !state.DryRun || ModStatusKey(state, false) != "s_dry" {
			t.Fatalf("%+v", state)
		}
		if state = ModResolve(list, rows(chatfilter.Enablement{RuleID: list.ID, Enabled: true, DryRunUntil: now.Add(-time.Hour)}), "room", now); state.DryRun {
			t.Fatal("an ended recording still says it records")
		}
	})

	t.Run("a custom filter of one channel lives at that channel, any other at the workspace", func(t *testing.T) {
		one := chatfilter.Definition{ID: "x", Name: "x", Kind: "words", Action: "block", Channels: []string{"room"}}
		many := chatfilter.Definition{ID: "y", Name: "y", Kind: "words", Action: "block", Channels: []string{"room", "other"}}
		wide := chatfilter.Definition{ID: "z", Name: "z", Kind: "words", Action: "block"}
		if got := ModToggleRequest(one, nil, "room", false, now); got != (ModSwitch{RuleID: "x", Channel: "room", On: true}) {
			t.Fatalf("one-channel filter: %+v", got)
		}
		if got := ModToggleRequest(many, nil, "room", false, now); got.Channel != "" || !got.On {
			t.Fatalf("multi-channel filter: %+v", got)
		}
		if got := ModToggleRequest(wide, rows(chatfilter.Enablement{RuleID: "z", Enabled: true}), "", true, now); got != (ModSwitch{RuleID: "z", On: false}) {
			t.Fatalf("workspace filter: %+v", got)
		}
		// a channel row does not decide a filter that is not for exactly one channel
		if state := ModResolve(many, rows(chatfilter.Enablement{RuleID: "y", Enabled: true}, chatfilter.Enablement{RuleID: "y", Channel: "room", Enabled: false}), "room", now); !state.On || state.HasOverride {
			t.Fatalf("a multi-channel filter was overridden by one channel: %+v", state)
		}
	})

	t.Run("the writes a flip, a change of outcome and a reset make", func(t *testing.T) {
		r := rows(ws(true, "mask"), own(false, "block"))
		if got := ModToggleRequest(list, r, "room", false, now); got != (ModSwitch{RuleID: list.ID, Channel: "room", On: true, Action: "block"}) {
			t.Fatalf("toggle in a channel writes at the channel: %+v", got)
		}
		if got := ModToggleRequest(list, r, "room", true, now); got != (ModSwitch{RuleID: list.ID, Channel: "", Action: "mask"}) {
			t.Fatalf("toggle on the workspace page writes the workspace row: %+v", got)
		}
		if got := ModActionRequest(list, "room", false, "flag"); got != (ModSwitch{RuleID: list.ID, Channel: "room", On: true, Action: "flag"}) {
			t.Fatalf("outcome change: %+v", got)
		}
		if got := ModResetRequest(list, r, "room", now); got != (ModSwitch{RuleID: list.ID, Channel: "room", On: true, Action: "mask"}) {
			t.Fatalf("reset sets the channel's row equal to the workspace's: %+v", got)
		}
		if got := ModResetRequest(list, rows(own(true, "flag")), "room", now); got != (ModSwitch{RuleID: list.ID, Channel: "room", On: false, Action: "block"}) {
			t.Fatalf("reset with no workspace row: %+v", got)
		}
	})
}

func TestTodo_CHATMOD_003_Editor_Sections(t *testing.T) {
	defs := append(chatfilter.Builtins(),
		chatfilter.Definition{ID: "own-b", Name: "Beta", Kind: "words", Channels: []string{"room"}},
		chatfilter.Definition{ID: "own-a", Name: "alpha", Kind: "words", Channels: []string{"room"}},
		chatfilter.Definition{ID: "wide", Name: "Wide", Kind: "words"},
		chatfilter.Definition{ID: "pair", Name: "Pair", Kind: "words", Channels: []string{"room", "other"}},
		chatfilter.Definition{ID: "elsewhere", Name: "Elsewhere", Kind: "words", Channels: []string{"other"}},
	)
	got := ModGroup(defs, "room", false, "de-DE")
	if !reflect.DeepEqual(got.Languages, []string{"de", "en", "ar"}) {
		t.Fatalf("the viewer's language leads: %v", got.Languages)
	}
	var names []string
	for _, d := range got.Builtin["en"] {
		names = append(names, d.Name)
	}
	if !reflect.DeepEqual(names, []string{"profanity", "slurs", "harassment"}) {
		t.Fatalf("list order %v", names)
	}
	ids := func(list []chatfilter.Definition) []string {
		var out []string
		for _, d := range list {
			out = append(out, d.ID)
		}
		return out
	}
	if !reflect.DeepEqual(ids(got.Own), []string{"own-a", "own-b"}) || !reflect.DeepEqual(ids(got.Admin), []string{"pair", "wide"}) {
		t.Fatalf("own %v admin %v", ids(got.Own), ids(got.Admin))
	}
	ws := ModGroup(defs, "", true, "en-US")
	if len(ws.Admin) != 0 || len(ws.Own) != 5 || ws.Languages[0] != "en" {
		t.Fatalf("workspace page: own %v admin %v languages %v", ids(ws.Own), ids(ws.Admin), ws.Languages)
	}
}

func TestTodo_CHATMOD_003_Editor_Outcome(t *testing.T) {
	sample := "well damn it"
	hit := chatfilter.Hit{RuleName: "Swears", Action: "block", Span: chatfilter.Span{Start: 5, End: 9}}
	if got := ModOutcomeOf(chatfilter.Result{Action: "block", Hits: []chatfilter.Hit{hit}}, sample); got.Kind != "block" || got.Term != "damn" || got.Rule != "Swears" {
		t.Fatalf("block: %+v", got)
	}
	// the strictest hit names the sentence, not the first
	flag := chatfilter.Hit{RuleName: "Other", Action: "flag", Span: chatfilter.Span{Start: 0, End: 4}}
	if got := ModOutcomeOf(chatfilter.Result{Action: "block", Hits: []chatfilter.Hit{flag, hit}}, sample); got.Rule != "Swears" || got.Term != "damn" {
		t.Fatalf("strictest: %+v", got)
	}
	if got := ModOutcomeOf(chatfilter.Result{Action: "mask", Masked: "well [removed word] it", Hits: []chatfilter.Hit{{Action: "mask", Span: chatfilter.Span{Start: 5, End: 9}}}}, sample); got.Kind != "mask" || got.Masked != "well [removed word] it" {
		t.Fatalf("mask: %+v", got)
	}
	if got := ModOutcomeOf(chatfilter.Result{Action: "notify", Hits: []chatfilter.Hit{{Action: "notify", Target: "#security", Span: chatfilter.Span{Start: 0, End: 4}}}}, sample); got.Kind != "notify" || got.Target != "#security" {
		t.Fatalf("notify: %+v", got)
	}
	if got := ModOutcomeOf(chatfilter.Result{}, sample); got.Kind != "none" {
		t.Fatalf("no match: %+v", got)
	}
	// a span that does not fit the sample, or cuts a character in two, is not used
	for _, span := range []chatfilter.Span{{Start: 5, End: 99}, {Start: -1, End: 2}, {Start: 4, End: 4}} {
		if got := ModOutcomeOf(chatfilter.Result{Action: "block", Hits: []chatfilter.Hit{{Action: "block", Span: span}}}, sample); got.Term != "" {
			t.Fatalf("span %v gave term %q", span, got.Term)
		}
	}
	if got := ModOutcomeOf(chatfilter.Result{Action: "block", Hits: []chatfilter.Hit{{Action: "block", Span: chatfilter.Span{Start: 1, End: 2}}}}, "é"); got.Term != "" {
		t.Fatalf("a span through a character gave %q", got.Term)
	}
	long := strings.Repeat("a", 60)
	if got := ModOutcomeOf(chatfilter.Result{Action: "block", Hits: []chatfilter.Hit{{Action: "block", Span: chatfilter.Span{Start: 0, End: 60}}}}, long); len([]rune(got.Term)) != 41 {
		t.Fatalf("a long term is cut to a line: %q", got.Term)
	}
	if ModExcerpt("short [removed word]") != "short [removed word]" {
		t.Fatal("a short message was cut")
	}
	masked := strings.Repeat("x", 200) + " [removed word] " + strings.Repeat("y", 200)
	excerpt := ModExcerpt(masked)
	if !strings.Contains(excerpt, "[removed word]") || !strings.HasPrefix(excerpt, "…") || !strings.HasSuffix(excerpt, "…") || len([]rune(excerpt)) > 130 {
		t.Fatalf("excerpt %q", excerpt)
	}
}

func TestTodo_CHATMOD_003_Editor_Errors(t *testing.T) {
	for code, want := range map[string]string{"permission_denied": "er_perm", "unauthenticated": "er_perm", "request_denied": "er_perm", "filters_unavailable": "er_unavail", "version_conflict": "er_conflict", "invalid_filter": "er_invalid", "invalid_request": "er_invalid", "network": "er_network", "": "er_network", "context canceled": "er_network"} {
		if got := ModErrorKey(code); got != want {
			t.Errorf("ModErrorKey(%q) = %s, want %s", code, got, want)
		}
	}
	// every line the panel can say exists in the three languages, and none shows a key
	for key, values := range modadminCopy {
		for i, text := range values {
			if strings.TrimSpace(text) == "" || strings.Contains(text, "⟦") || strings.HasPrefix(text, "chat.") {
				t.Errorf("modadmin_%s has no plain text in column %d: %q", key, i, text)
			}
		}
		if values[0] == values[1] && values[1] == values[2] && len(values[0]) > 24 {
			t.Errorf("modadmin_%s is one text in all three languages: %q", key, values[0])
		}
	}
	// the catalog answering a key it does not know never reaches the page
	m := Model{Locale: "de-DE", Text: func(key string) string { return "⟦" + key + "⟧" }}
	if got := modadminText(m, "create"); got != "Filter erstellen" {
		t.Fatalf("catalog marker leaked: %q", got)
	}
	m.Text = func(key string) string { return "Eigener Text" }
	if got := modadminText(m, "create"); got != modadminCopy["create"][1] {
		t.Fatalf("CHATBUG-039 feature copy lost priority to the catalog: %q", got)
	}
	if got := modadminText(Model{}, "no_such_key"); got != "" {
		t.Fatalf("unknown key answered %q", got)
	}
}
