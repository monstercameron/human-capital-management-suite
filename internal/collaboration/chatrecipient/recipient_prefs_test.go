package chatrecipient

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATEMOJI-004: a person's emoji choices and voice playback choices are kept in
// the same per-person document as the rest of their personal Chat layout. They
// round-trip, survive a read that filters a revoked room, and are size-bounded.
func TestTodo_CHATEMOJI_004_SidebarKeepsEmojiAndVoicePreferences(t *testing.T) {
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	emoji := `{"v":1,"tone":3,"usage":[{"g":"🎉","n":4,"t":9}],"seq":9}`
	voice := `{"Speed":1.5,"Collapsed":false}`
	emojiJSON, _ := json.Marshal(emoji)
	voiceJSON, _ := json.Marshal(voice)
	layout := []byte(`{"sections":[{"id":"a","chats":[{"hostTenantId":"host","conversationId":"open"},{"hostTenantId":"host","conversationId":"revoked"}]}],"emojiPrefs":` + string(emojiJSON) + `,"voicePrefs":` + string(voiceJSON) + `}`)

	db := &repo{}
	s := &Service{Conversations: conversations{allowed: true, denied: map[string]bool{"host/revoked": true}}, Repo: db}
	// Written: the server stores the document as given.
	if _, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: layout}, 1); err == nil {
		t.Fatal("a layout naming a room the caller cannot open was accepted")
	}
	open := strings.Replace(string(layout), `,{"hostTenantId":"host","conversationId":"revoked"}`, "", 1)
	if _, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: []byte(open)}, 1); err != nil {
		t.Fatalf("PutSidebar: %v", err)
	}
	var saved struct{ EmojiPrefs, VoicePrefs string }
	if err := json.Unmarshal(db.saved.Layout, &saved); err != nil || saved.EmojiPrefs != emoji || saved.VoicePrefs != voice {
		t.Fatalf("stored layout lost the preferences: %s (%v)", db.saved.Layout, err)
	}

	// Read back after a room has since been revoked: the filtered read keeps them.
	db.sidebar = Sidebar{Layout: layout, Revision: 5}
	got, err := s.Sidebar(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	var read struct {
		EmojiPrefs string `json:"emojiPrefs"`
		VoicePrefs string `json:"voicePrefs"`
		Sections   []struct {
			Chats []struct{} `json:"chats"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(got.Layout, &read); err != nil || read.EmojiPrefs != emoji || read.VoicePrefs != voice || len(read.Sections[0].Chats) != 1 {
		t.Fatalf("filtered read: %s (%v)", got.Layout, err)
	}

	// Bounded: neither string may grow without limit.
	for _, key := range []string{"emojiPrefs", "voicePrefs"} {
		big, _ := json.Marshal(map[string]any{"sections": []any{}, key: strings.Repeat("x", sidebarPrefsMax+1)})
		if _, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: big}, 1); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Errorf("an oversized %s = %v, want ErrInvalidArgument", key, err)
		}
		fits, _ := json.Marshal(map[string]any{"sections": []any{}, key: strings.Repeat("x", sidebarPrefsMax)})
		if _, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: fits}, 1); err != nil {
			t.Errorf("a %s at the limit was refused: %v", key, err)
		}
	}
}
