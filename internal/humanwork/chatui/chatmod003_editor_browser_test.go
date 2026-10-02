package chatui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// modFieldShown reports whether the field wrapper holding the label of id is
// drawn (not hidden). The editor mounts every field and hides the ones the
// chosen kind and action do not use, so what is typed is never lost.
func modFieldShown(t *testing.T, markup, id string) bool {
	t.Helper()
	found := regexp.MustCompile(`<div class="chatmod-field"( hidden)?><label[^>]* for="` + id + `"`).FindStringSubmatch(markup)
	if found == nil {
		t.Fatalf("no field %s in the editor", id)
	}
	return found[1] == ""
}

func TestTodo_CHATMOD_003_Browser(t *testing.T) {
	titles := map[string]struct{ try, removed, create string }{
		"en-US": {"Try a message", "removed word", "Create a filter"},
		"de-DE": {"Nachricht ausprobieren", "entferntes Wort", "Filter erstellen"},
		"ar":    {"جرّب رسالة", "كلمة محذوفة", "أنشئ مرشحاً"},
	}
	for locale, literal := range titles {
		t.Run(locale, func(t *testing.T) {
			m := modBrowserModel(locale, true)
			member := modBrowserModel(locale, false)
			text := func(key string) string { return chatui.ModAdminText(m, key) }
			defs, rows := modBuiltinsAndCustoms("room")
			render := func(model chatui.Model, p chatui.ModAdminProps) string {
				p.Model, p.CanManage, p.Channel = model, true, "room"
				if p.Definitions == nil {
					p.Definitions, p.Enablements = defs, rows
				}
				markup := modRender(t, chatui.ModAdminPanel(p))
				modNoLeaks(t, locale, markup)
				return markup
			}
			editor := func(s chatui.ModEditorState) *chatui.ModEditorState {
				s.Open = true
				if s.Kind == "" {
					s.Kind = chatui.ModKindWords
				}
				if s.Action == "" {
					s.Action = "block"
				}
				if s.Detector == "" {
					s.Detector = "card"
				}
				return &s
			}
			noop := func(chatui.ModSwitch) {}

			// Closed: the lists and one button; the editor is mounted but hidden.
			closed := render(m, chatui.ModAdminProps{Switch: noop})
			if !regexp.MustCompile(`<form[^>]*class="chatmod-editor"[^>]* hidden`).MatchString(closed) && !regexp.MustCompile(`<form[^>]* hidden[^>]*class="chatmod-editor"`).MatchString(closed) {
				t.Errorf("the editor is not hidden until asked for: %s", regexp.MustCompile(`<form[^>]*>`).FindString(closed))
			}
			if !strings.Contains(closed, ">"+literal.create+"<") || !strings.Contains(closed, `data-action="modadmin-edit" data-id="own-1"`) {
				t.Errorf("no way to create or edit a filter")
			}

			// Open and empty: every field of the short form, labelled, with its hint.
			open := render(m, chatui.ModAdminProps{Editor: editor(chatui.ModEditorState{})})
			for _, key := range []string{"e_name", "e_kind", "e_action", "e_where", "e_exempt", "e_dry", "e_hard", "try_title", "try_label", "try_btn", "e_save", "e_cancel", "e_back", "e_name_hint", "e_save_hint", "try_hint"} {
				if !strings.Contains(open, text(key)) {
					t.Errorf("empty editor lacks %s (%q)", key, text(key))
				}
			}
			if !strings.Contains(open, ">"+literal.try+"<") {
				t.Errorf("the Try box is not titled %q", literal.try)
			}
			for _, field := range []string{"name", "kind", "match", "action", "roles", "agents", "sample"} {
				if !strings.Contains(open, `for="modadmin-`+field+`"`) {
					t.Errorf("field %s has no label", field)
				}
			}
			if regexp.MustCompile(`<details class="chatmod-details" open`).MatchString(open) || !strings.Contains(open, "<details") {
				t.Errorf("Exemptions is not collapsed by default")
			}
			if strings.Contains(open, `id="modadmin-version"`) || strings.Contains(strings.ToLower(modVisible(open)), "version") {
				t.Errorf("a version box is exposed")
			}
			for _, bound := range []string{`<input[^>]*id="modadmin-name"[^>]*value=`, `<textarea[^>]*id="modadmin-match"[^>]*value=`, `<select[^>]*id="modadmin-kind"[^>]*value=`} {
				if regexp.MustCompile(bound).MatchString(open) {
					t.Errorf("a field is bound to a value and would be overwritten by a render: %s", bound)
				}
			}
			if !strings.Contains(open, `aria-describedby="modadmin-h-name"`) || !strings.Contains(open, `<div aria-live="polite" id="modadmin-outcome" role="status"></div>`) {
				t.Errorf("hints or the result area are not tied to their fields")
			}

			// Each kind: its hint, its field, its example.
			wantShown := map[string][3]bool{ // match, detector, domains
				chatui.ModKindWords: {true, false, false}, chatui.ModKindPattern: {true, false, false}, chatui.ModKindSensitive: {false, true, false},
				chatui.ModKindLinks: {false, false, true}, chatui.ModKindAttachment: {true, false, false},
			}
			for kind, shown := range wantShown {
				markup := render(m, chatui.ModAdminProps{Editor: editor(chatui.ModEditorState{Kind: kind})})
				if !strings.Contains(markup, text("kh_"+kind)) {
					t.Errorf("%s: no hint", kind)
				}
				for i, id := range []string{"match", "detector", "domains"} {
					if modFieldShown(t, markup, "modadmin-"+id) != shown[i] {
						t.Errorf("%s: field %s shown=%v", kind, id, !shown[i])
					}
				}
				switch kind {
				case chatui.ModKindWords:
					if !strings.Contains(markup, `placeholder="`+strings.SplitN(text("ph_words"), "\n", 2)[0]) {
						t.Errorf("words: no example")
					}
				case chatui.ModKindPattern, chatui.ModKindAttachment, chatui.ModKindLinks:
					if !strings.Contains(markup, `placeholder="`+text("ph_"+kind)+`"`) {
						t.Errorf("%s: no example placeholder %q", kind, text("ph_"+kind))
					}
				case chatui.ModKindSensitive:
					for _, d := range chatui.ModDetectors {
						if !regexp.MustCompile(`<option (selected )?value="`+d+`"`).MatchString(markup) || !strings.Contains(markup, ">"+text("d_"+d)+"<") {
							t.Errorf("sensitive: detector %s is not offered in plain words", d)
						}
					}
					if strings.Contains(modVisible(markup), "national-id") || strings.Contains(modVisible(markup), "access-key") {
						t.Errorf("sensitive: a detector id is shown")
					}
				}
				attachmentNote := regexp.MustCompile(`<p class="chatmod-hint"( hidden)?>` + regexp.QuoteMeta(text("try_att"))).FindStringSubmatch(markup)
				if attachmentNote == nil || (attachmentNote[1] == "") != (kind == chatui.ModKindAttachment) {
					t.Errorf("%s: the attachment note about the Try box is wrong: %v", kind, attachmentNote)
				}
			}
			// Each action: its hint; a destination only for notify.
			for _, action := range chatui.ModActions {
				markup := render(m, chatui.ModAdminProps{Editor: editor(chatui.ModEditorState{Action: action})})
				if !strings.Contains(markup, text("ah_"+action)) || !strings.Contains(markup, `selected value="`+action+`"`) {
					t.Errorf("%s: hint or choice missing", action)
				}
				if modFieldShown(t, markup, "modadmin-target") != (action == "notify") {
					t.Errorf("%s: destination field shown wrongly", action)
				}
			}

			// Where it applies: fixed in a channel; whole workspace or chosen channels on the workspace page.
			if strings.Contains(open, `id="modadmin-scope"`) || !strings.Contains(open, ">"+text("w_channel")+"<") {
				t.Errorf("a channel's editor does not say it applies to this channel only")
			}
			wsOpen := render(m, chatui.ModAdminProps{Workspace: true, Editor: editor(chatui.ModEditorState{Scope: "chosen"})})
			if !strings.Contains(wsOpen, `id="modadmin-scope"`) || !strings.Contains(wsOpen, ">"+text("w_ws")+"<") || !strings.Contains(wsOpen, ">"+text("w_chosen")+"<") {
				t.Errorf("the workspace editor lacks its choice of place")
			}
			if strings.Count(wsOpen, `type="checkbox"`) != 2+1+1 || !strings.Contains(wsOpen, ">general<") || !strings.Contains(wsOpen, ">operations<") || strings.Contains(wsOpen, ">Jake<") {
				t.Errorf("the channels to choose from are not the channels (and the two options): %d boxes", strings.Count(wsOpen, `type="checkbox"`))
			}
			wsEdit := render(m, chatui.ModAdminProps{Workspace: true, Editor: editor(chatui.ModEditorState{EditID: "project-ab12cd", EditVersion: "1.0.0", EditChannels: []string{"room"}})})
			if strings.Contains(wsEdit, `id="modadmin-scope"`) || !strings.Contains(wsEdit, text("w_locked")) || strings.Contains(modVisible(wsEdit), "project-ab12cd") || !strings.Contains(wsEdit, text("e_title_edit")) {
				t.Errorf("an edit offers to move a filter or shows its id")
			}

			// The direct-message check is for workspace administrators only.
			if !strings.Contains(open, `id="modadmin-hard"`) {
				t.Errorf("an administrator is not offered the direct-message check")
			}
			plain := render(member, chatui.ModAdminProps{Editor: editor(chatui.ModEditorState{})})
			if strings.Contains(plain, `id="modadmin-hard"`) || strings.Contains(plain, `id="modadmin-h-hard"`) {
				t.Errorf("a channel manager is offered the direct-message check")
			}
			locked := render(m, chatui.ModAdminProps{Editor: editor(chatui.ModEditorState{EditID: "x", EditVersion: "1.0.0", EditHard: true})})
			if !regexp.MustCompile(`<input[^>]*disabled[^>]*id="modadmin-hard"`).MatchString(locked) && !regexp.MustCompile(`<input[^>]*id="modadmin-hard"[^>]*disabled`).MatchString(locked) || !strings.Contains(locked, text("e_hard_locked")) {
				t.Errorf("a rule that also applies in direct messages can be turned back")
			}

			// Plain validation, tied to its field, with no code or id.
			for field, key := range map[string]string{"name": "v_name", "match": "v_pattern_bad", "target": "v_target", "scope": "v_channels", "sample": "try_empty"} {
				kind, action, ws := chatui.ModKindWords, "block", false
				switch field {
				case "match":
					kind = chatui.ModKindPattern
				case "target":
					action = "notify"
				case "scope":
					ws = true
				}
				markup := render(m, chatui.ModAdminProps{Workspace: ws, Editor: editor(chatui.ModEditorState{Kind: kind, Action: action, Errors: map[string]string{field: key}})})
				if !strings.Contains(markup, `<p class="chatmod-error" id="modadmin-e-`+field+`" role="alert">`+text(key)+`</p>`) {
					t.Errorf("%s: the refusal is not an alert beside its field", field)
				}
				if field != "scope" && !strings.Contains(markup, `aria-invalid="true"`) {
					t.Errorf("%s: the field is not marked invalid", field)
				}
				if !regexp.MustCompile(`aria-describedby="[^"]*modadmin-e-`+field+`[^"]*"`).MatchString(markup) && field != "scope" && field != "sample" {
					t.Errorf("%s: the field is not tied to its refusal", field)
				}
			}
			if !strings.Contains(render(m, chatui.ModAdminProps{Editor: editor(chatui.ModEditorState{Kind: chatui.ModKindPattern, Errors: map[string]string{"match": "v_pattern_bad"}})}), text("v_pattern_bad")) {
				t.Errorf("the refused pattern is not explained")
			}

			// The Try box, one plain sentence per outcome.
			sample := "well damn it"
			tried := func(result *chatfilter.Result, mutate func(*chatui.ModAdminProps, *chatui.ModEditorState)) string {
				state := chatui.ModEditorState{Tried: true}
				props := chatui.ModAdminProps{Result: result, TrySample: sample}
				if mutate != nil {
					mutate(&props, &state)
				}
				props.Editor = editor(state)
				return render(m, props)
			}
			sentence := func(key string, pairs ...string) string {
				out := text(key)
				for i := 0; i+1 < len(pairs); i += 2 {
					out = strings.ReplaceAll(out, "{"+pairs[i]+"}", pairs[i+1])
				}
				return out
			}
			block := tried(&chatfilter.Result{Action: "block", Hits: []chatfilter.Hit{{RuleName: "Swears", Action: "block", Span: chatfilter.Span{Start: 5, End: 9}}}}, nil)
			if !strings.Contains(block, `<p class="chatmod-outcome" dir="auto">`+sentence("o_block", "term", "damn", "rule", "Swears")+`</p>`) {
				t.Errorf("block sentence: %s", regexp.MustCompile(`<div aria-live="polite" id="modadmin-outcome".*?</div>`).FindString(block))
			}
			if locale == "en-US" && !strings.Contains(block, "This message would be blocked: “damn” matches Swears.") {
				t.Errorf("block sentence in English: %s", block)
			}
			mask := tried(&chatfilter.Result{Action: "mask", Masked: "well [removed word] it", Hits: []chatfilter.Hit{{RuleName: "Swears", Action: "mask", Span: chatfilter.Span{Start: 5, End: 9}}}}, nil)
			if !strings.Contains(mask, text("o_mask")) || !strings.Contains(mask, `<span class="chatfilter-removed">`+literal.removed+`</span>`) || strings.Contains(mask, "[removed word]") {
				t.Errorf("mask outcome does not show the chip: %s", regexp.MustCompile(`<div aria-live="polite" id="modadmin-outcome".*?</div>`).FindString(mask))
			}
			flag := tried(&chatfilter.Result{Action: "flag", Hits: []chatfilter.Hit{{RuleName: "Swears", Action: "flag", Span: chatfilter.Span{Start: 5, End: 9}}}}, nil)
			if !strings.Contains(flag, text("o_flag")) {
				t.Errorf("flag sentence missing")
			}
			notify := tried(&chatfilter.Result{Action: "notify", Hits: []chatfilter.Hit{{RuleName: "Swears", Action: "notify", Target: "#security", Span: chatfilter.Span{Start: 5, End: 9}}}}, nil)
			if !strings.Contains(notify, sentence("o_notify", "target", "#security")) {
				t.Errorf("notify sentence missing")
			}
			none := tried(&chatfilter.Result{}, nil)
			if !strings.Contains(none, text("o_none")) {
				t.Errorf("no-match sentence missing")
			}
			dry := tried(&chatfilter.Result{Action: "block", Hits: []chatfilter.Hit{{RuleName: "Swears", Action: "block", Span: chatfilter.Span{Start: 5, End: 9}}}}, func(_ *chatui.ModAdminProps, s *chatui.ModEditorState) { s.TriedDry = true })
			if !strings.Contains(dry, text("o_dry")) || strings.Contains(block, text("o_dry")) || strings.Contains(none, text("o_dry")) {
				t.Errorf("the record-only note is shown wrongly")
			}
			refusal := tried(nil, func(p *chatui.ModAdminProps, _ *chatui.ModEditorState) { p.TryError = "er_invalid" })
			if !strings.Contains(refusal, text("er_invalid")+" "+text("sf_try")) {
				t.Errorf("a server refusal is not in plain words")
			}
			// A result of an earlier session, or one still being fetched, is not shown.
			stale := render(m, chatui.ModAdminProps{Result: &chatfilter.Result{}, TrySample: sample, Editor: editor(chatui.ModEditorState{})})
			if strings.Contains(stale, text("o_none")) {
				t.Errorf("an old result is shown in a new editor")
			}
			trying := tried(&chatfilter.Result{}, func(p *chatui.ModAdminProps, _ *chatui.ModEditorState) {
				p.Trying = true
				p.Try = func(chatfilter.Definition, string) {}
			})
			if strings.Contains(trying, text("o_none")) || !strings.Contains(trying, `aria-busy="true"`) || !strings.Contains(trying, text("try_busy")) {
				t.Errorf("a result is shown while the sample is still being tried")
			}
			ready := tried(nil, func(p *chatui.ModAdminProps, _ *chatui.ModEditorState) {
				p.Try = func(chatfilter.Definition, string) {}
			})
			if regexp.MustCompile(`<button aria-busy="false" disabled`).MatchString(ready) {
				t.Errorf("Try is disabled although a handler exists and nothing is going")
			}
			// The save outcome sits beside the Save button while the editor is open.
			saved := render(m, chatui.ModAdminProps{Status: "er_conflict", StatusNote: "sf_save", StatusError: true, Editor: editor(chatui.ModEditorState{})})
			if !regexp.MustCompile(`id="modadmin-form-status" role="alert">`+regexp.QuoteMeta(text("er_conflict")+" "+text("sf_save"))).MatchString(saved) || regexp.MustCompile(`id="modadmin-status" role="alert">[^<]`).MatchString(saved) {
				t.Errorf("a refused save does not say so beside the form")
			}
		})
	}
}
