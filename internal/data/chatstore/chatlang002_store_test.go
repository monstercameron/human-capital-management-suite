package chatstore

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func chatlangRevision(t *testing.T, s *Store, scope RenderingScope, member string) int {
	t.Helper()
	var revision int
	err := s.RunTenantTx(context.Background(), scope.Principal.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT revision FROM chat_preference WHERE tenant_id=$1 AND home_tenant_id=$1 AND member_id=$2 AND marker=$3 AND conversation_id=''`, scope.Principal.TenantID, member, chatlangLocaleMarker).Scan(&revision)
	})
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

// TestTodo_CHATLANG_002_Locale: the interface language is the default reading
// language of a person who has not chosen one, it is remembered per person, and
// a conversation counts such a person in it.
func TestTodo_CHATLANG_002_Locale(t *testing.T) {
	s, alice, _ := chatrenderDB(t)
	ctx := context.Background()
	bob := alice
	bob.Principal.SubjectID = "bob"

	got, err := s.LanguageSettings(ctx, bob, "")
	if err != nil || got.ReadingLanguage != "en" || !got.Translate {
		t.Fatalf("nothing known: %+v %v", got, err)
	}
	for _, bad := range []string{"", "   ", "waytoolongtag", "de\"x"} {
		if err := s.RecordInterfaceLocale(ctx, bob, bad); !errors.Is(err, chatrender.ErrInvalid) {
			t.Fatalf("locale %q accepted: %v", bad, err)
		}
	}
	if err := s.RecordInterfaceLocale(ctx, RenderingScope{}, "de"); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatalf("no principal: %v", err)
	}
	if err := s.RecordInterfaceLocale(ctx, bob, "de-DE"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordInterfaceLocale(ctx, bob, "de-AT"); err != nil {
		t.Fatal(err)
	}
	if revision := chatlangRevision(t, s.Store, bob, "bob"); revision != 1 {
		t.Fatalf("an unchanged language wrote again: revision %d", revision)
	}
	if got, err = s.LanguageSettings(ctx, bob, ""); err != nil || got.ReadingLanguage != "de" {
		t.Fatalf("remembered language not the default: %+v %v", got, err)
	}
	if got, err = s.LanguageSettings(ctx, bob, "fr"); err != nil || got.ReadingLanguage != "fr" {
		t.Fatalf("a language sent with the read must win: %+v %v", got, err)
	}
	// What the person chose is never replaced by what they were seen with.
	chosen := chatrender.DefaultPreference("ar")
	chosen.Translate = false
	global := bob
	global.Conversation = ""
	if err = s.PutLanguageSettings(ctx, global, chosen); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordInterfaceLocale(ctx, bob, "es"); err != nil {
		t.Fatal(err)
	}
	if got, err = s.LanguageSettings(ctx, bob, ""); err != nil || got.ReadingLanguage != "ar" || got.Translate {
		t.Fatalf("the person's own choice was overwritten: %+v %v", got, err)
	}
	if revision := chatlangRevision(t, s.Store, bob, "bob"); revision != 2 {
		t.Fatalf("a changed language did not write: revision %d", revision)
	}

	// The conversation counts people who have not chosen by the language they
	// were seen with; a language the product does not read counts as English.
	if err = s.RunTenantTx(ctx, alice.Tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chat_preference WHERE tenant_id=$1 AND member_id='bob' AND marker=$2`, alice.Tenant, chatrenderLanguageMarker)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	counts, err := s.ConversationLanguages(ctx, alice)
	if err != nil || !reflect.DeepEqual(counts, map[string]int{"es": 1, "und": 1}) {
		t.Fatalf("counts %v %v", counts, err)
	}
	if err = s.RecordInterfaceLocale(ctx, alice, "it-IT"); err != nil {
		t.Fatal(err)
	}
	if counts, err = s.ConversationLanguages(ctx, alice); err != nil || !reflect.DeepEqual(counts, map[string]int{"es": 1, "en": 1}) {
		t.Fatalf("counts %v %v", counts, err)
	}
	outsider := alice
	outsider.Principal.SubjectID = "mallory"
	if _, err = s.ConversationLanguages(ctx, outsider); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatalf("a person outside the conversation read its languages: %v", err)
	}
}

// TestTodo_CHATLANG_002_Audience: who will read a translation, as counts per
// language, from the settings people chose and the language they were seen with.
func TestTodo_CHATLANG_002_Audience(t *testing.T) {
	s, alice, _ := chatrenderDB(t)
	ctx := context.Background()
	bob := alice
	bob.Principal.SubjectID = "bob"
	audience := func(source string) map[string]int {
		t.Helper()
		counts, err := s.ConversationAudience(ctx, alice, source)
		if err != nil {
			t.Fatal(err)
		}
		return counts
	}
	put := func(mutate func(*chatrender.Preference)) {
		t.Helper()
		pref := chatrender.DefaultPreference("de")
		mutate(&pref)
		global := bob
		global.Conversation = ""
		if err := s.PutLanguageSettings(ctx, global, pref); err != nil {
			t.Fatal(err)
		}
	}

	if got := audience("en"); len(got) != 0 {
		t.Fatalf("a person whose language is unknown was counted: %v", got)
	}
	if err := s.RecordInterfaceLocale(ctx, bob, "de-DE"); err != nil {
		t.Fatal(err)
	}
	if got := audience("en"); !reflect.DeepEqual(got, map[string]int{"de": 1}) {
		t.Fatalf("a German reader of an English message: %v", got)
	}
	if got := audience("de"); len(got) != 0 {
		t.Fatalf("a German reader of a German message needs no translation: %v", got)
	}
	for _, none := range []string{"", "und", "UND"} {
		if got := audience(none); len(got) != 0 {
			t.Fatalf("a message with no language %q has an audience: %v", none, got)
		}
	}
	put(func(p *chatrender.Preference) { p.Translate = false })
	if got := audience("en"); len(got) != 0 {
		t.Fatalf("translation off: %v", got)
	}
	put(func(p *chatrender.Preference) { p.FurtherLanguages = []string{"en"} })
	if got := audience("en"); len(got) != 0 {
		t.Fatalf("a language they also read: %v", got)
	}
	put(func(p *chatrender.Preference) { p.SourceOverrides = map[string]bool{"en": false} })
	if got := audience("en"); len(got) != 0 {
		t.Fatalf("never translate English: %v", got)
	}
	put(func(p *chatrender.Preference) { p.ReadingLanguage = "ar" })
	if got := audience("en"); !reflect.DeepEqual(got, map[string]int{"ar": 1}) {
		t.Fatalf("an Arabic reader: %v", got)
	}
	// A conversation-level choice overrides the personal one.
	room := chatrender.DefaultPreference("fr")
	if err := s.PutLanguageSettings(ctx, bob, room); err != nil {
		t.Fatal(err)
	}
	if got := audience("en"); !reflect.DeepEqual(got, map[string]int{"fr": 1}) {
		t.Fatalf("a conversation choice: %v", got)
	}
	// The writer is never counted, and only a member may ask.
	if err := s.RecordInterfaceLocale(ctx, alice, "es"); err != nil {
		t.Fatal(err)
	}
	if got := audience("en"); !reflect.DeepEqual(got, map[string]int{"fr": 1}) {
		t.Fatalf("the writer was counted: %v", got)
	}
	outsider := alice
	outsider.Principal.SubjectID = "mallory"
	if _, err := s.ConversationAudience(ctx, outsider, "en"); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatalf("a person outside the conversation read its audience: %v", err)
	}
}
