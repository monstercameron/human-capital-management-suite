package chatui

import "testing"

// The Private answer choice belongs to the unsent draft it was made for: it
// comes back with that draft after a visit to another conversation, and is
// never carried into a different conversation's draft.
func TestIssue3992_PrivateAnswerChoiceFollowsItsDraft(t *testing.T) {
	store := localStore{box: &localUI{}}
	store.forRoom("general")
	store.box.privateAnswer = true

	store.forRoom("random")
	if store.box.privateAnswer {
		t.Fatal("the choice made in #general was carried into #random")
	}
	store.forRoom("general")
	if !store.box.privateAnswer {
		t.Fatal("the choice was lost when returning to the draft it was made for")
	}

	// Two drafts keep their own choices.
	store.forRoom("random")
	store.box.privateAnswer = true
	store.forRoom("general")
	store.box.privateAnswer = false
	store.forRoom("random")
	if !store.box.privateAnswer {
		t.Fatal("#random lost its own choice")
	}
	store.forRoom("general")
	if store.box.privateAnswer {
		t.Fatal("#general kept a choice that was turned off")
	}

	// Staying in the same conversation changes nothing.
	store.box.privateAnswer = true
	store.forRoom("general")
	if !store.box.privateAnswer {
		t.Fatal("re-rendering the same conversation reset the choice")
	}
}
