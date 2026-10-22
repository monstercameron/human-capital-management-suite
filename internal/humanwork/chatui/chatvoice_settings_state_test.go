package chatui

import (
	"errors"
	"testing"
)

// TestTodo_CHATVOICE_005_PageState: the voice row's state is the page's, so an
// answer that reaches the page resolves the row whichever instance asked, and
// each kind of answer lands on the state the row draws.
func TestTodo_CHATVOICE_005_PageState(t *testing.T) {
	failure := errors.New("down")
	reading := voiceSettingsState{Loading: true}

	got, restore := voiceSettingsAnswered(reading, VoiceSettingsData{Person: true}, 200, nil, false)
	if !got.Loaded || got.Loading || got.LoadFailed || !got.Data.Person || restore {
		t.Fatalf("read answered: %+v restore=%v", got, restore)
	}
	if got, _ = voiceSettingsAnswered(reading, VoiceSettingsData{}, 0, failure, false); got.Loaded || !got.LoadFailed || got.Loading {
		t.Fatalf("read failed: %+v", got)
	}
	if got, _ = voiceSettingsAnswered(reading, VoiceSettingsData{}, 503, failure, false); !got.Unavailable || !got.Loaded || got.LoadFailed {
		t.Fatalf("not composed: %+v", got)
	}
	if got, _ = voiceSettingsAnswered(reading, VoiceSettingsData{}, 403, failure, false); !got.Denied || !got.Loaded {
		t.Fatalf("refused: %+v", got)
	}
	// A failed save keeps the value that was on show and puts the switch back.
	loaded := voiceSettingsState{Loaded: true, Saving: true, Data: VoiceSettingsData{Person: true}}
	got, restore = voiceSettingsAnswered(loaded, VoiceSettingsData{}, 500, failure, false)
	if !restore || !got.Fails || got.LoadFailed || got.Saving || !got.Data.Person {
		t.Fatalf("save failed: %+v restore=%v", got, restore)
	}

	// The store answers for any instance of the row.
	props := voiceSettingsProps{Scope: voiceScopePerson, Tenant: "tenant-a"}
	key := props.storeKey()
	delete(voiceSettingsStore, key)
	t.Cleanup(func() { delete(voiceSettingsStore, key) })
	if got := voiceSettingsCurrent(key, voiceSettingsState{}); got.Loaded {
		t.Fatalf("nothing stored yet: %+v", got)
	}
	voiceSettingsChange(key, voiceSettingsState{}, func(s *voiceSettingsState) { s.Loading = true })
	voiceSettingsChange(key, voiceSettingsState{}, func(s *voiceSettingsState) {
		*s, _ = voiceSettingsAnswered(*s, VoiceSettingsData{Person: true}, 200, nil, false)
	})
	other := voiceSettingsCurrent(key, voiceSettingsState{})
	if !other.Loaded || other.Loading || !other.Data.Person {
		t.Fatalf("an instance that did not ask sees %+v", other)
	}
	if voiceSettingsCurrent((voiceSettingsProps{Scope: voiceScopeChannel, Tenant: "tenant-a", Conversation: "room"}).storeKey(), voiceSettingsState{}).Loaded {
		t.Fatal("another row's state leaked")
	}
}
