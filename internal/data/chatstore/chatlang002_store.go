package chatstore

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// CHATLANG-002: the interface language a person last opened Chat in is kept as
// a second private preference row. It is only the default for a person who has
// not chosen a reading language: it gives the conversation a language to count
// for them and gives a read with no Accept-Language header the right default.
// It is never shown to anyone, and is used for nothing but rendering.
const chatlangLocaleMarker = "chatlang.locale.v1"

// RecordInterfaceLocale remembers the interface language of the caller. It
// writes only when the value changed, so a page load that repeats itself does
// not write.
func (s *Store) RecordInterfaceLocale(ctx context.Context, scope RenderingScope, locale string) error {
	language := chatrender.Language(locale)
	if language == "" || len(language) > 8 || strings.ContainsAny(language, " \t\r\n\"\\{}") {
		return chatrender.ErrInvalid
	}
	if scope.Principal.TenantID == "" || scope.Principal.SubjectID == "" {
		return chatrender.ErrDenied
	}
	value, err := json.Marshal(struct {
		Locale string `json:"locale"`
	}{language})
	if err != nil {
		return err
	}
	return s.RunTenantTx(ctx, scope.Principal.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_preference(tenant_id,home_tenant_id,member_id,conversation_id,marker,value) VALUES($1,$2,$3,'',$4,$5) ON CONFLICT(tenant_id,home_tenant_id,member_id,conversation_id,marker) DO UPDATE SET value=EXCLUDED.value,revision=chat_preference.revision+1,updated_at=now() WHERE chat_preference.value IS DISTINCT FROM EXCLUDED.value`, scope.Principal.TenantID, scope.Principal.TenantID, scope.Principal.SubjectID, chatlangLocaleMarker, value)
		return err
	})
}

// chatlangStoredLocale is the interface language remembered for the caller, or
// "" when none was.
func chatlangStoredLocale(ctx context.Context, tx dbport.Tx, scope RenderingScope) string {
	var b []byte
	if err := tx.QueryRow(ctx, `SELECT value FROM chat_preference WHERE tenant_id=$1 AND home_tenant_id=$1 AND member_id=$2 AND marker=$3 AND conversation_id=''`, scope.Principal.TenantID, scope.Principal.SubjectID, chatlangLocaleMarker).Scan(&b); err != nil {
		return ""
	}
	var v struct {
		Locale string `json:"locale"`
	}
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	return v.Locale
}

// chatlangReadingLanguage folds a remembered locale to a language the product
// reads in: one it does not support is read in English, as DefaultPreference does.
func chatlangReadingLanguage(locale string) string {
	return chatrender.DefaultPreference(locale).ReadingLanguage
}

// ConversationAudience says how many other people in the conversation would
// read a translation of a message written in source, per language they read it
// in. It is the answer to "who will read this in another language": counts per
// language only, never who, and never a person's settings. A member who has
// neither chosen a reading language nor been seen in Chat is not counted: the
// product does not know what they read.
func (s *Store) ConversationAudience(ctx context.Context, scope RenderingScope, source string) (map[string]int, error) {
	counts := map[string]int{}
	source = chatrender.Language(source)
	if source == "" || source == "und" {
		return counts, nil
	}
	err := s.renderingTx(ctx, scope, "", func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT (SELECT pref.value FROM chat_preference pref WHERE pref.tenant_id=m.tenant_id AND pref.home_tenant_id=m.home_tenant_id AND pref.member_id=m.member_id AND pref.marker=$3 AND pref.conversation_id IN ('',m.conversation_id) ORDER BY (pref.conversation_id=m.conversation_id) DESC LIMIT 1),
 (SELECT loc.value->>'locale' FROM chat_preference loc WHERE loc.tenant_id=m.tenant_id AND loc.home_tenant_id=m.home_tenant_id AND loc.member_id=m.member_id AND loc.marker=$4 AND loc.conversation_id='')
 FROM chat_membership m WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.state='active' AND NOT (m.home_tenant_id=$5 AND m.member_id=$6)`, scope.Tenant, scope.Conversation, chatrenderLanguageMarker, chatlangLocaleMarker, scope.Principal.TenantID, scope.Principal.SubjectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var stored []byte
			var locale *string
			if err = rows.Scan(&stored, &locale); err != nil {
				return err
			}
			if stored == nil && locale == nil {
				continue
			}
			seen := ""
			if locale != nil {
				seen = *locale
			}
			pref := chatrender.DefaultPreference(seen)
			if stored != nil && (json.Unmarshal(stored, &pref) != nil || pref.Validate() != nil) {
				continue
			}
			if chatrender.WantsTranslation(pref, source) {
				counts[chatrender.Language(pref.ReadingLanguage)]++
			}
		}
		return rows.Err()
	})
	return counts, err
}
