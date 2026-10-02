package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	dom "golang.org/x/net/html"
)

func TestAgentUXProactiveLive_FieldValidation(t *testing.T) {
	base := agentcontrols.AnnouncementDraft{PersonaID: "assistant", InstallationID: "install", ConversationID: "general", Instruction: "Post the holidays", Documents: []agentdocref.Reference{{DocumentID: "guide", VersionMode: agentdocref.ModeLatestPublished, Label: "Guide"}}, Cadence: "NOW", Zone: "UTC", IdempotencyKey: "key"}
	if got := agentAnnouncementInvalidField(base); got != "" {
		t.Fatal(got)
	}
	cases := []struct {
		field  string
		change func(*agentcontrols.AnnouncementDraft)
	}{
		{"agent", func(d *agentcontrols.AnnouncementDraft) { d.PersonaID = "" }},
		{"conversation", func(d *agentcontrols.AnnouncementDraft) { d.ConversationID = "" }},
		{"conversation", func(d *agentcontrols.AnnouncementDraft) { d.InstallationID = "" }},
		{"instruction", func(d *agentcontrols.AnnouncementDraft) { d.Instruction = " " }},
		{"instruction", func(d *agentcontrols.AnnouncementDraft) { d.Instruction = strings.Repeat("x", 1001) }},
		{"documents", func(d *agentcontrols.AnnouncementDraft) { d.Documents = nil }},
		{"when", func(d *agentcontrols.AnnouncementDraft) { d.Cadence = "" }},
		{"weekdays", func(d *agentcontrols.AnnouncementDraft) { d.Cadence = "WEEKLY" }},
		{"weekdays", func(d *agentcontrols.AnnouncementDraft) { d.Cadence = "WEEKLY"; d.Weekdays = []int{1, 1} }},
		{"month-day", func(d *agentcontrols.AnnouncementDraft) { d.Cadence = "MONTHLY" }},
		{"time", func(d *agentcontrols.AnnouncementDraft) { d.Cadence = "DAILY"; d.Time = "25:00" }},
		{"zone", func(d *agentcontrols.AnnouncementDraft) { d.Zone = "invalid" }},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			d := base
			tc.change(&d)
			if got := agentAnnouncementInvalidField(d); got != tc.field || validateAgentAnnouncementInput(d) == nil {
				t.Fatalf("field=%q want=%q", got, tc.field)
			}
		})
	}
	empty := agentcontrols.AnnouncementDraft{}
	if got := agentAnnouncementInvalidField(empty); got != "agent" {
		t.Fatalf("first missing field=%q", got)
	}
}

func TestAgentUXProactiveLive_LatestPublishedRememberedVersion(t *testing.T) {
	latest := agentAnnouncementDocumentReference("guide", "Guide", "LATEST_PUBLISHED", "", 7)
	pinned := agentAnnouncementDocumentReference("guide", "Guide", "PINNED", "", 7)
	if latest.PinnedVersion != 0 || pinned.PinnedVersion != 7 || agentdocref.Validate([]agentdocref.Reference{latest}, 5) != nil || agentdocref.Validate([]agentdocref.Reference{pinned}, 5) != nil {
		t.Fatalf("picker version modes: latest=%+v pinned=%+v", latest, pinned)
	}
}

func TestAgentUXProactiveLive_PrimaryAction(t *testing.T) {
	for _, tc := range []struct {
		when          string
		editing       bool
		action, label string
	}{
		{"NOW", false, "post", "post_now"}, {"ONCE", false, "post", "post_now"}, {"NOW", true, "save", "save_changes"}, {"DAILY", false, "save", "save"}, {"WEEKLY", false, "save", "save"}, {"MONTHLY", true, "save", "save"},
	} {
		action, label := agentAnnouncementPrimaryAction(tc.when, tc.editing)
		if action != tc.action || label != tc.label {
			t.Fatalf("%+v: %s %s", tc, action, label)
		}
	}
}

// Replay the delegated handler's ancestor lookup over all three real picker
// component trees. A container's localized removal copy must not own a click.
func TestAgentUXProactiveLive_DocumentPickerMouseAndKeyboard_Browser(t *testing.T) {
	for _, id := range []string{"announcement-documents", "persona-documents", "agents-task-documents"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			markup, err := ui.RenderToString(productui.AgentDocumentReferencePicker(productui.ResolveProductLocale(locale), productui.AgentDocumentReferencePickerModel{ID: id, Available: true, ReferenceLimit: 5, Suggestions: []productui.AgentDocumentSuggestion{{DocumentID: "guide", Title: "Guide", PublishedVersion: 7}}}))
			if err != nil {
				t.Fatal(err)
			}
			root, err := dom.Parse(strings.NewReader(markup))
			if err != nil {
				t.Fatal(err)
			}
			var option *dom.Node
			var walk func(*dom.Node)
			walk = func(n *dom.Node) {
				if proactivePickerAttr(n, "data-agentdoc-option") == "guide" {
					option = n
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
			}
			walk(root)
			if option == nil || option.FirstChild == nil {
				t.Fatal("missing result")
			}
			for _, target := range []*dom.Node{option, option.FirstChild} {
				selected, removeTag, retryTag := "", "", ""
				for n := target; n != nil; n = n.Parent {
					if selected == "" {
						selected = proactivePickerAttr(n, "data-agentdoc-option")
					}
					if removeTag == "" && proactivePickerAttr(n, "data-agentdoc-remove") != "" {
						removeTag = strings.ToUpper(n.Data)
					}
					if retryTag == "" && proactivePickerAttr(n, "data-agentdoc-retry") != "" {
						retryTag = strings.ToUpper(n.Data)
					}
				}
				if action := personaDocumentPickerClickAction(selected, removeTag, retryTag); action != "add" {
					t.Fatalf("%s %s result click=%s", id, locale, action)
				}
			}
			// Keyboard Enter sends that same option to Add. Containers stay inert;
			// actual removal/retry buttons retain their own actions.
			if personaDocumentPickerClickAction("guide", "", "") != "add" || personaDocumentPickerClickAction("", "DIV", "DIV") != "" || personaDocumentPickerClickAction("", "BUTTON", "") != "remove" || personaDocumentPickerClickAction("", "", "BUTTON") != "retry" {
				t.Fatal("picker action routing changed")
			}
		}
	}
}
func proactivePickerAttr(n *dom.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func TestAgentUXProactiveLive_DirectTabQuery(t *testing.T) {
	for _, query := range []string{"tab=announcements", "tab=announcements&agent=assistant"} {
		if got := agentOperationsSelectedTab(query); got != "announcements" {
			t.Fatalf("first load/reload tab=%s", got)
		}
	}
}
