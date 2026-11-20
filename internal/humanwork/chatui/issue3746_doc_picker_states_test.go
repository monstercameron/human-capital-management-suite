package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestIssue3746_DocPickerStatesAreDistinct(t *testing.T) {
	menu := func(locale string, status DocSuggestStatus) string {
		return renderNode(t, docSuggestMenu(Model{Locale: locale}, docSuggestState{Target: "chat-composer", Open: true, Status: status}, "chat-composer"))
	}
	none := menu("en-US", DocSuggestOK)
	failed := menu("en-US", DocSuggestFailed)
	unavailable := menu("en-US", DocSuggestUnavailable)
	if !strings.Contains(none, "No documents you can open match") || strings.Contains(none, "doc-suggest-retry") {
		t.Fatalf("no-match state = %s", none)
	}
	if strings.Contains(failed, "No documents you can open match") || !strings.Contains(failed, `data-action="doc-suggest-retry"`) || !strings.Contains(failed, "Try again") || !strings.Contains(failed, `role="status"`) {
		t.Fatalf("failure state needs its own copy and a retry: %s", failed)
	}
	if strings.Contains(unavailable, "No documents you can open match") || strings.Contains(unavailable, "doc-suggest-retry") || unavailable == none || unavailable == failed {
		t.Fatalf("not-configured state = %s", unavailable)
	}
}

func TestIssue3746_DocPickerStatusFollowsCallback(t *testing.T) {
	local := localStore{box: &localUI{}, tick: ui.UseState(uint64(0))}
	var done func([]DocSuggestion, DocSuggestStatus)
	calls := 0
	m := Model{Callbacks: Callbacks{SuggestDocuments: func(q string, d func([]DocSuggestion, DocSuggestStatus)) { calls++; done = d }}}
	docSuggestRun(m, local, docSuggestState{Target: "chat-composer", Query: "x", Open: true, Loading: true, seq: 1})
	done(nil, DocSuggestFailed)
	got := local.get().docSuggest
	if got.Loading || got.Status != DocSuggestFailed || len(got.Items) != 0 {
		t.Fatalf("state after failure = %+v", got)
	}
	docSuggestRetry(m, local)
	if calls != 2 || !local.get().docSuggest.Loading || local.get().docSuggest.Status != DocSuggestOK {
		t.Fatalf("retry calls=%d state=%+v", calls, local.get().docSuggest)
	}
	done(nil, DocSuggestOK)
	if got := local.get().docSuggest; got.Loading || got.Status != DocSuggestOK {
		t.Fatalf("state after empty success = %+v", got)
	}
}

func TestIssue3746_DocPickerRetryByKeyboardAndStaleAnswer(t *testing.T) {
	local := localStore{box: &localUI{}, tick: ui.UseState(uint64(0))}
	var dones []func([]DocSuggestion, DocSuggestStatus)
	m := Model{Callbacks: Callbacks{SuggestDocuments: func(q string, d func([]DocSuggestion, DocSuggestStatus)) { dones = append(dones, d) }}}
	if docSuggestRetryKey(m, local, "Tab", "chat-composer") {
		t.Fatal("Tab retried with no list open")
	}
	docSuggestRun(m, local, docSuggestState{Target: "chat-composer", Query: "x", Open: true, Loading: true, seq: 1})
	if docSuggestRetryKey(m, local, "Tab", "chat-composer") {
		t.Fatal("Tab retried while still searching")
	}
	dones[0](nil, DocSuggestFailed)
	if docSuggestRetryKey(m, local, "Enter", "chat-composer") || docSuggestRetryKey(m, local, "Tab", "other-composer") {
		t.Fatal("only Tab in the list's own field retries")
	}
	if !docSuggestRetryKey(m, local, "Tab", "chat-composer") || len(dones) != 2 || !local.get().docSuggest.Loading {
		t.Fatalf("Tab did not retry: calls=%d state=%+v", len(dones), local.get().docSuggest)
	}
	// An answer to the search that failed earlier must not overwrite the retry.
	dones[0](nil, DocSuggestUnavailable)
	if got := local.get().docSuggest; !got.Loading || got.Status != DocSuggestOK {
		t.Fatalf("stale answer applied: %+v", got)
	}
	dones[1]([]DocSuggestion{{ID: "d1", Title: "Plan"}}, DocSuggestOK)
	if got := local.get().docSuggest; got.Loading || len(got.Items) != 1 {
		t.Fatalf("retry answer = %+v", got)
	}
	// A non-OK status never keeps items.
	docSuggestRun(m, local, docSuggestState{Target: "chat-composer", Query: "y", Open: true, Loading: true, seq: 9})
	dones[2]([]DocSuggestion{{ID: "d2", Title: "Leak"}}, DocSuggestFailed)
	if got := local.get().docSuggest; len(got.Items) != 0 || got.Status != DocSuggestFailed {
		t.Fatalf("failure kept items: %+v", got)
	}
}
