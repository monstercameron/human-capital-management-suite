package chatstore

import (
	"context"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

const chatrenderLanguageMarker = "chatrender.language.v1"

// LanguageSettings keeps the interface locale separate from reading language.
// The empty conversation is the personal default; a room row overrides it.
func (s *Store) LanguageSettings(ctx context.Context, scope RenderingScope, locale string) (chatrender.Preference, error) {
	pref := chatrender.DefaultPreference(locale)
	if scope.Principal.TenantID == "" || scope.Principal.SubjectID == "" || scope.Tenant == "" || (scope.Conversation == "" && scope.Tenant != scope.Principal.TenantID) {
		return pref, chatrender.ErrDenied
	}
	read := func(tx dbport.Tx, tenantID, conversation string) error {
		var b []byte
		err := tx.QueryRow(ctx, `SELECT value FROM chat_preference WHERE tenant_id=$1 AND home_tenant_id=$2 AND member_id=$3 AND marker=$4 AND conversation_id=$5`, tenantID, scope.Principal.TenantID, scope.Principal.SubjectID, chatrenderLanguageMarker, conversation).Scan(&b)
		if err == dbport.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		return json.Unmarshal(b, &pref)
	}
	err := s.RunTenantTx(ctx, scope.Principal.TenantID, func(tx dbport.Tx) error {
		if locale == "" {
			// CHATLANG-002: a read that carries no locale defaults to the interface
			// language the person was last seen in Chat with.
			pref = chatrender.DefaultPreference(chatlangStoredLocale(ctx, tx, scope))
		}
		return read(tx, scope.Principal.TenantID, "")
	})
	if err != nil {
		return pref, err
	}
	if scope.Conversation != "" {
		err = s.renderingTx(ctx, scope, "", func(tx dbport.Tx) error { return read(tx, scope.Tenant, scope.Conversation) })
	}
	return pref, err
}
func (s *Store) PutLanguageSettings(ctx context.Context, scope RenderingScope, pref chatrender.Preference) error {
	if err := pref.Validate(); err != nil {
		return err
	}
	if scope.Principal.TenantID == "" || scope.Principal.SubjectID == "" || scope.Tenant == "" || (scope.Conversation == "" && scope.Tenant != scope.Principal.TenantID) {
		return chatrender.ErrDenied
	}
	return s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		if scope.Conversation != "" {
			if err := chatrenderAccess(ctx, tx, scope, ""); err != nil {
				return err
			}
		}
		b, err := json.Marshal(pref)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_preference(tenant_id,home_tenant_id,member_id,conversation_id,marker,value) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,home_tenant_id,member_id,conversation_id,marker) DO UPDATE SET value=EXCLUDED.value,revision=chat_preference.revision+1,updated_at=now()`, scope.Tenant, scope.Principal.TenantID, scope.Principal.SubjectID, scope.Conversation, chatrenderLanguageMarker, b)
		return err
	})
}

// RecordRevisionLanguage is the trusted send/edit observer, using persisted text.
func (s *Store) RecordRevisionLanguage(ctx context.Context, tenantID, post string, revision uint64) error {
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return RecordRenderingRevisionTx(ctx, tx, tenantID, post, revision, nil)
	})
}

// RecordRenderingRevisionTx is the one-call hook for existing revision INSERT
// paths. The caller has already authorized the mutation and set tenant RLS; the
// annotation and eager requests commit or roll back with that same revision.
func RecordRenderingRevisionTx(ctx context.Context, tx dbport.Tx, tenantID, post string, revision uint64, readers []chatrender.Preference) error {
	var body, source string
	var corrected bool
	if err := tx.QueryRow(ctx, `SELECT body,source_language,language_corrected FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3 AND NOT tombstoned FOR UPDATE`, tenantID, post, revision).Scan(&body, &source, &corrected); err != nil {
		return err
	}
	if !corrected {
		d := chatrender.PreparedDetection(ctx, body)
		source = d.Language
		b, err := json.Marshal(d.Spans)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE chat_post_revision SET source_language=$4,language_confidence=$5,language_spans=$6 WHERE tenant_id=$1 AND post_id=$2 AND revision=$3 AND NOT language_corrected`, tenantID, post, revision, d.Language, d.Confidence, b); err != nil {
			return err
		}
	}
	// A system line ("a person was added") is not text anyone reads in another
	// language, so nothing is requested for it (CHATUX-021).
	if _, system := chat.ParseMembershipAdded(body); system {
		return nil
	}
	original := chatrender.Rendering{Tenant: tenantID, Message: post, Revision: revision, Tone: chatrender.AsWritten, Language: source, SourceLanguage: source, Text: body}
	if readers == nil {
		// CHATLANG-003: translations are requested at once for the members who
		// read another language, when the workspace and channel allow it.
		var err error
		if readers, err = chatlangEagerReadersTx(ctx, tx, tenantID, post, source); err != nil {
			return err
		}
	}
	for _, target := range chatrender.RequestsForReaders(original, readers) {
		target.Text = body
		b, err := json.Marshal(target)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO chatrender_job(tenant_id,post_id,revision,tone,language,request) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, tenantID, post, revision, target.Tone, target.Language, b); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) RevisionLanguage(ctx context.Context, scope RenderingScope, post string, revision uint64) (chatrender.Detection, error) {
	var d chatrender.Detection
	err := s.renderingTx(ctx, scope, post, func(tx dbport.Tx) error {
		var b []byte
		err := tx.QueryRow(ctx, `SELECT source_language,language_confidence,language_spans,language_corrected FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, scope.Tenant, post, revision).Scan(&d.Language, &d.Confidence, &b, &d.Corrected)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, &d.Spans)
	})
	return d, err
}

// CorrectRevisionLanguage checks ownership before changing annotations, and
// invalidates derived text so a correction cannot reuse the wrong translation.
func (s *Store) CorrectRevisionLanguage(ctx context.Context, scope RenderingScope, post string, revision uint64, language string) error {
	language = chatrender.Language(language)
	if !chatrender.Supported(language) {
		return chatrender.ErrInvalid
	}
	return s.renderingTx(ctx, scope, post, func(tx dbport.Tx) error {
		var author, home string
		err := tx.QueryRow(ctx, `SELECT author_id,author_home_tenant_id FROM chat_post WHERE tenant_id=$1 AND id=$2 AND revision=$3 FOR UPDATE`, scope.Tenant, post, revision).Scan(&author, &home)
		if err != nil {
			return err
		}
		if author != scope.Principal.SubjectID || home != scope.Principal.TenantID {
			return chatrender.ErrDenied
		}
		if _, err = tx.Exec(ctx, `UPDATE chat_post_revision SET source_language=$4,language_confidence=1,language_spans='[]',language_corrected=true WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, scope.Tenant, post, revision, language); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM chatrender_rendering WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, scope.Tenant, post, revision); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM chatrender_job WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, scope.Tenant, post, revision)
		return err
	})
}

// ConversationLanguages exposes aggregate counts, never individual settings.
func (s *Store) ConversationLanguages(ctx context.Context, scope RenderingScope) (map[string]int, error) {
	counts := map[string]int{}
	err := s.renderingTx(ctx, scope, "", func(tx dbport.Tx) error {
		// A person who has not chosen a reading language is counted in the
		// interface language they were last seen with, when it is known.
		rows, err := tx.Query(ctx, `SELECT COALESCE((SELECT pref.value->>'reading_language' FROM chat_preference pref WHERE pref.tenant_id=m.tenant_id AND pref.home_tenant_id=m.home_tenant_id AND pref.member_id=m.member_id AND pref.marker=$3 AND pref.conversation_id IN ('',m.conversation_id) ORDER BY (pref.conversation_id=m.conversation_id) DESC LIMIT 1),(SELECT loc.value->>'locale' FROM chat_preference loc WHERE loc.tenant_id=m.tenant_id AND loc.home_tenant_id=m.home_tenant_id AND loc.member_id=m.member_id AND loc.marker=$4 AND loc.conversation_id=''),'und'),count(*) FROM chat_membership m WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.state='active' GROUP BY 1`, scope.Tenant, scope.Conversation, chatrenderLanguageMarker, chatlangLocaleMarker)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var lang string
			var n int
			if err = rows.Scan(&lang, &n); err != nil {
				return err
			}
			if lang != "und" && !chatrender.Supported(lang) {
				lang = chatlangReadingLanguage(lang)
			}
			counts[lang] += n
		}
		return rows.Err()
	})
	return counts, err
}

type MessageLanguage struct {
	Message   string
	Revision  uint64
	Detection chatrender.Detection
}

// SearchMessageLanguages is the search/agent projection for detected language.
// It applies the same current membership and history boundary as message views.
func (s *Store) SearchMessageLanguages(ctx context.Context, scope RenderingScope, language string) ([]MessageLanguage, error) {
	language = chatrender.Language(language)
	if !chatrender.Supported(language) && language != "mul" {
		return nil, chatrender.ErrInvalid
	}
	out := []MessageLanguage{}
	err := s.renderingTx(ctx, scope, "", func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT p.id,p.revision,v.source_language,v.language_confidence,v.language_spans,v.language_corrected FROM chat_post p JOIN chat_post_revision v ON v.tenant_id=p.tenant_id AND v.post_id=p.id AND v.revision=p.revision JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' AND NOT p.tombstoned AND (m.history_visibility='FULL_HISTORY' OR p.created_at>=m.joined_at) AND v.source_language=$5 ORDER BY p.sequence DESC LIMIT 200`, scope.Tenant, scope.Conversation, scope.Principal.TenantID, scope.Principal.SubjectID, language)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row MessageLanguage
			var spans []byte
			if err = rows.Scan(&row.Message, &row.Revision, &row.Detection.Language, &row.Detection.Confidence, &spans, &row.Detection.Corrected); err != nil {
				return err
			}
			if err = json.Unmarshal(spans, &row.Detection.Spans); err != nil {
				return err
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}

// PresentReaders is supplied by the runtime presence owner. No list of personal
// settings is exposed to HTTP or to other conversation members.
type PresentReaders interface {
	RenderingReaders(context.Context, string, string) ([]chatrender.Preference, error)
}

// RenderingAdapter preserves the existing chat Store contract and adds the
// revision observer. The composition root must wrap its existing Adapter.
type RenderingAdapter struct {
	*Adapter
	Present PresentReaders
}

func (s *RenderingAdapter) observe(ctx context.Context, p chat.Post) error {
	var prefs []chatrender.Preference
	if s.Present != nil {
		var err error
		prefs, err = s.Present.RenderingReaders(ctx, p.TenantID, p.ConversationID)
		if err != nil {
			return err
		}
	}
	return s.RunTenantTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		return RecordRenderingRevisionTx(ctx, tx, p.TenantID, p.ID, p.Revision, prefs)
	})
}
func (s *RenderingAdapter) SendPost(ctx context.Context, r chat.SendPostRequest, p chat.Post) (chat.Post, error) {
	out, err := s.Adapter.SendPost(ctx, r, p)
	if err != nil {
		return out, err
	}
	return out, s.observe(ctx, out)
}
func (s *RenderingAdapter) EditPost(ctx context.Context, r chat.EditPostRequest) (chat.Post, error) {
	out, err := s.Adapter.EditPost(ctx, r)
	if err != nil {
		return out, err
	}
	return out, s.observe(ctx, out)
}
