package chatui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// TestTodo_CHATMOD_003_Editor_DryRunKept: a filter that is on trial ("Record
// only for 7 days") opens in the editor with that box ticked. The editor used
// to open with it cleared, so fixing a typo in the name of a filter on trial
// saved it enforcing.
func TestTodo_CHATMOD_003_Editor_DryRunKept(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	def := chatfilter.Definition{ID: "client-names", Name: "Client names", Version: "1.0.0", Kind: "words", Match: []string{"falcon"}, Action: "block"}
	for _, tc := range []struct {
		name string
		row  chatfilter.Enablement
		want bool
	}{
		{"on trial", chatfilter.Enablement{RuleID: def.ID, Enabled: true, DryRunUntil: now.Add(5 * 24 * time.Hour)}, true},
		{"trial over", chatfilter.Enablement{RuleID: def.ID, Enabled: true, DryRunUntil: now.Add(-time.Hour)}, false},
		{"enforcing", chatfilter.Enablement{RuleID: def.ID, Enabled: true}, false},
		{"switched off during its trial", chatfilter.Enablement{RuleID: def.ID, Enabled: false, DryRunUntil: now.Add(time.Hour)}, false},
	} {
		v := ModFormValuesOf(def)
		v.DryRun = ModResolve(def, []chatfilter.Enablement{tc.row}, "", now).DryRun
		if v.DryRun != tc.want {
			t.Errorf("%s: the editor opens with Record only %v, want %v", tc.name, v.DryRun, tc.want)
		}
	}
	// A filter of one channel is read at its channel, wherever it is edited from.
	own := def
	own.Channels = []string{"general"}
	if !ModResolve(own, []chatfilter.Enablement{{RuleID: own.ID, Channel: "general", Enabled: true, DryRunUntil: now.Add(time.Hour)}}, "", now).DryRun {
		t.Error("a channel's own filter on trial is not read as on trial from the workspace page")
	}
	// The editor uses it for the box.
	source, err := os.ReadFile("chatmod003_panel.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, `filterSetChecked("modadmin-dry", v.DryRun)`) || !strings.Contains(text, "v.DryRun = ModResolve(def, props.Enablements, channel, now).DryRun") {
		t.Error("the editor does not open with Record only as the filter stands")
	}
	if strings.Contains(text, `filterSetChecked("modadmin-dry", false)`) {
		t.Error("the editor still clears Record only when it opens")
	}
}
