package chatstore

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ChatlangUsage is one line of the translation usage ledger: identifiers,
// counts and cost, never text.
type ChatlangUsage struct {
	Tenant, Message, Language, Provider, Model, InstructionDigest, Outcome string
	Revision                                                               uint64
	InputTokens, OutputTokens, CostMicros                                  int64
	At                                                                     time.Time
}

// ChatlangTerm is a stored glossary entry.
type ChatlangTerm struct {
	ID string `json:"id"`
	chatlang.Term
}

func chatlangReadWorkspaceTx(ctx context.Context, tx dbport.Tx, tenant string) (chatlang.Workspace, error) {
	w := chatlang.Workspace{ExternalAllowed: true}
	var languages, formality []byte
	err := tx.QueryRow(ctx, `SELECT translation='on',languages,monthly_budget_micros,external_allowed,formality,glossary_version,revision FROM chatlang_setting WHERE tenant_id=$1 AND conversation_id=''`, tenant).Scan(&w.Enabled, &languages, &w.BudgetMicros, &w.ExternalAllowed, &formality, &w.GlossaryVersion, &w.Revision)
	if err == dbport.ErrNoRows {
		return chatlang.Workspace{ExternalAllowed: true}, nil
	}
	if err != nil {
		return w, err
	}
	if err = json.Unmarshal(languages, &w.Languages); err != nil {
		return w, err
	}
	if err = json.Unmarshal(formality, &w.Formality); err != nil {
		return w, err
	}
	return w, nil
}

func chatlangReadChannelTx(ctx context.Context, tx dbport.Tx, tenant, conversation string) (chatlang.Channel, error) {
	c := chatlang.Channel{Translation: chatlang.Inherit, External: chatlang.ExternalInherit}
	if conversation == "" {
		return c, nil
	}
	var translation, external string
	err := tx.QueryRow(ctx, `SELECT translation,external FROM chatlang_setting WHERE tenant_id=$1 AND conversation_id=$2`, tenant, conversation).Scan(&translation, &external)
	if err == dbport.ErrNoRows {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	c.Translation, c.External = chatlang.Switch(translation), chatlang.External(external)
	return c, nil
}

func chatlangSpentTx(ctx context.Context, tx dbport.Tx, tenant string, at time.Time) (int64, error) {
	from, to := chatlang.Month(at)
	var spent int64
	err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(cost_micros),0) FROM chatlang_usage WHERE tenant_id=$1 AND at>=$2 AND at<$3`, tenant, from, to).Scan(&spent)
	return spent, err
}

// ChatlangWorkspace reads the workspace's translation settings. A workspace
// that has never been set has translation off.
func (s *Store) ChatlangWorkspace(ctx context.Context, tenant string) (chatlang.Workspace, error) {
	var w chatlang.Workspace
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		w, err = chatlangReadWorkspaceTx(ctx, tx, tenant)
		return err
	})
	return w, err
}

// PutChatlangWorkspace stores the workspace's settings. Enabled is kept in the
// translation column ("on" or "off") so a channel row and the workspace row
// share one shape. The glossary version is never changed from here.
func (s *Store) PutChatlangWorkspace(ctx context.Context, tenant, by string, w chatlang.Workspace) (chatlang.Workspace, error) {
	w, err := w.Normalize()
	if err != nil {
		return w, err
	}
	languages, _ := json.Marshal(w.Languages)
	formality, _ := json.Marshal(w.Formality)
	switchValue := "off"
	if w.Enabled {
		switchValue = "on"
	}
	err = s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chatlang_setting(tenant_id,conversation_id,translation,languages,monthly_budget_micros,external_allowed,formality,updated_by) VALUES($1,'',$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,conversation_id) DO UPDATE SET translation=EXCLUDED.translation,languages=EXCLUDED.languages,monthly_budget_micros=EXCLUDED.monthly_budget_micros,external_allowed=EXCLUDED.external_allowed,formality=EXCLUDED.formality,updated_by=EXCLUDED.updated_by,updated_at=now(),revision=chatlang_setting.revision+1`, tenant, switchValue, languages, w.BudgetMicros, w.ExternalAllowed, formality, by)
		if err != nil {
			return err
		}
		w, err = chatlangReadWorkspaceTx(ctx, tx, tenant)
		return err
	})
	return w, err
}

// ChatlangChannel reads one channel's setting; no row means "inherit".
func (s *Store) ChatlangChannel(ctx context.Context, tenant, conversation string) (chatlang.Channel, error) {
	var c chatlang.Channel
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		c, err = chatlangReadChannelTx(ctx, tx, tenant, conversation)
		return err
	})
	return c, err
}

// PutChatlangChannel stores one channel's setting. The conversation must exist
// in the tenant; a setting for a channel that does not is refused.
func (s *Store) PutChatlangChannel(ctx context.Context, tenant, conversation, by string, c chatlang.Channel) error {
	c, err := c.Normalize()
	if err != nil || conversation == "" {
		return chatlang.ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_conversation WHERE tenant_id=$1 AND id=$2)`, tenant, conversation).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return chatlang.ErrInvalid
		}
		_, err := tx.Exec(ctx, `INSERT INTO chatlang_setting(tenant_id,conversation_id,translation,external,updated_by) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,conversation_id) DO UPDATE SET translation=EXCLUDED.translation,external=EXCLUDED.external,updated_by=EXCLUDED.updated_by,updated_at=now(),revision=chatlang_setting.revision+1`, tenant, conversation, string(c.Translation), string(c.External), by)
		return err
	})
}

// ChatlangGlossary lists the workspace's terms, oldest first, with the version
// every change advances.
func (s *Store) ChatlangGlossary(ctx context.Context, tenant string) (chatlang.Glossary, []ChatlangTerm, error) {
	g := chatlang.Glossary{}
	var entries []ChatlangTerm
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		w, err := chatlangReadWorkspaceTx(ctx, tx, tenant)
		if err != nil {
			return err
		}
		g.Version = w.GlossaryVersion
		rows, err := tx.Query(ctx, `SELECT term_id,source_term,target_language,target_term FROM chatlang_glossary WHERE tenant_id=$1 ORDER BY created_at,term_id`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e ChatlangTerm
			if err = rows.Scan(&e.ID, &e.Source, &e.Language, &e.Target); err != nil {
				return err
			}
			entries = append(entries, e)
			g.Terms = append(g.Terms, e.Term)
		}
		return rows.Err()
	})
	return g, entries, err
}

const chatlangMaxTerms = 200

// AddChatlangTerm adds a glossary term and advances the glossary version in the
// same transaction, so a rendering's recorded version always names the list it
// was made from.
func (s *Store) AddChatlangTerm(ctx context.Context, tenant, by string, t chatlang.Term) (string, error) {
	t.Source = strings.TrimSpace(t.Source)
	t.Target = strings.TrimSpace(t.Target)
	if !chatlang.ValidTerm(t) {
		return "", chatlang.ErrInvalid
	}
	id := uuid.NewString()
	kind := "keep"
	if t.Language != "" {
		kind = "translate"
	}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chatlang_glossary WHERE tenant_id=$1`, tenant).Scan(&n); err != nil {
			return err
		}
		if n >= chatlangMaxTerms {
			return chatlang.ErrInvalid
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chatlang_glossary(tenant_id,term_id,kind,source_term,target_language,target_term,created_by) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenant, id, kind, t.Source, t.Language, t.Target, by); err != nil {
			return err
		}
		return chatlangBumpGlossaryTx(ctx, tx, tenant, by)
	})
	return id, err
}

// RemoveChatlangTerm removes a glossary term and advances the version.
func (s *Store) RemoveChatlangTerm(ctx context.Context, tenant, by, id string) error {
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `DELETE FROM chatlang_glossary WHERE tenant_id=$1 AND term_id=$2`, tenant, id)
		if err != nil {
			return err
		}
		if n != 1 {
			return chatlang.ErrInvalid
		}
		return chatlangBumpGlossaryTx(ctx, tx, tenant, by)
	})
}

func chatlangBumpGlossaryTx(ctx context.Context, tx dbport.Tx, tenant, by string) error {
	_, err := tx.Exec(ctx, `INSERT INTO chatlang_setting(tenant_id,conversation_id,translation,glossary_version,updated_by) VALUES($1,'','off',1,$2) ON CONFLICT(tenant_id,conversation_id) DO UPDATE SET glossary_version=chatlang_setting.glossary_version+1,updated_by=EXCLUDED.updated_by,updated_at=now(),revision=chatlang_setting.revision+1`, tenant, by)
	return err
}

// ChatlangSpent is what the workspace has spent on translation in the month of
// at, in millionths.
func (s *Store) ChatlangSpent(ctx context.Context, tenant string, at time.Time) (int64, error) {
	var spent int64
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		spent, err = chatlangSpentTx(ctx, tx, tenant, at)
		return err
	})
	return spent, err
}

// RecordChatlangUsage appends one usage line.
func (s *Store) RecordChatlangUsage(ctx context.Context, line ChatlangUsage) error {
	if line.Tenant == "" || line.Message == "" || line.Revision == 0 || line.CostMicros < 0 || line.InputTokens < 0 || line.OutputTokens < 0 {
		return chatlang.ErrInvalid
	}
	return s.RunTenantTx(ctx, line.Tenant, func(tx dbport.Tx) error {
		at := line.At
		if at.IsZero() {
			at = time.Now().UTC()
		}
		_, err := tx.Exec(ctx, `INSERT INTO chatlang_usage(tenant_id,line_id,at,post_id,revision,language,provider,model,instruction_digest,input_tokens,output_tokens,cost_micros,outcome) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, line.Tenant, uuid.NewString(), at, line.Message, line.Revision, line.Language, line.Provider, line.Model, line.InstructionDigest, line.InputTokens, line.OutputTokens, line.CostMicros, line.Outcome)
		return err
	})
}

// ChatlangUsageCount counts lines for one message revision and language; the
// job table already guarantees one job per message, revision and language, so
// this is how a test shows one engine call per message per language.
func (s *Store) ChatlangUsageCount(ctx context.Context, tenant, post string) (int, error) {
	var n int
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chatlang_usage WHERE tenant_id=$1 AND post_id=$2`, tenant, post).Scan(&n)
	})
	return n, err
}

// ChatlangJobFacts is what the worker needs to know about a job's message,
// read fresh from the record each time.
type ChatlangJobFacts struct {
	Tenant, Conversation, Author, AuthorHome, Body string
	Revision                                       uint64
	Direct                                         bool
}

// ChatlangJobFacts reads the message a rendering job is for. A removed message
// or a revision that is no longer current answers chatrender.ErrDenied.
func (s *Store) ChatlangJobFacts(ctx context.Context, tenant, post string, revision uint64) (ChatlangJobFacts, error) {
	f := ChatlangJobFacts{Tenant: tenant}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var kind string
		err := tx.QueryRow(ctx, `SELECT p.conversation_id,p.author_id,p.author_home_tenant_id,p.body,p.revision,c.kind FROM chat_post p JOIN chat_conversation c ON c.tenant_id=p.tenant_id AND c.id=p.conversation_id WHERE p.tenant_id=$1 AND p.id=$2 AND p.revision=$3 AND NOT p.tombstoned`, tenant, post, revision).Scan(&f.Conversation, &f.Author, &f.AuthorHome, &f.Body, &f.Revision, &kind)
		if err == dbport.ErrNoRows {
			return chatrender.ErrDenied
		}
		f.Direct = kind == "DIRECT" || kind == "GROUP"
		return err
	})
	return f, err
}

// ChatlangPreviousBodies returns up to n message bodies written before the
// post in the same conversation, oldest first, for the engine to read as
// context. It never includes removed messages.
func (s *Store) ChatlangPreviousBodies(ctx context.Context, tenant, post string, n int) ([]string, error) {
	var out []string
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT q.body FROM (SELECT p.body,p.sequence FROM chat_post p JOIN chat_post me ON me.tenant_id=p.tenant_id AND me.conversation_id=p.conversation_id AND me.id=$2 WHERE p.tenant_id=$1 AND p.sequence<me.sequence AND NOT p.tombstoned ORDER BY p.sequence DESC LIMIT $3) q ORDER BY q.sequence`, tenant, post, n)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var body string
			if err = rows.Scan(&body); err != nil {
				return err
			}
			out = append(out, body)
		}
		return rows.Err()
	})
	return out, err
}

// FailRenderingPermanently ends a job that retrying cannot help (a placeholder
// the engine cannot keep, a spent budget, a channel that has since barred the
// engine) so the reader falls back to the original at once and no further
// engine call is made for it.
func (s *Store) FailRenderingPermanently(ctx context.Context, job RenderingJob, reason string) error {
	r := job.Request
	if len(reason) > 200 {
		reason = reason[:200]
	}
	return s.RunTenantTx(ctx, r.Tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE chatrender_job SET state='failed',attempts=3,failure=$7,lease_until=NULL WHERE tenant_id=$1 AND post_id=$2 AND revision=$3 AND tone=$4 AND language=$5 AND state='claimed' AND lease_token=$6`, r.Tenant, r.Message, r.Revision, r.Tone, r.Language, job.Lease, reason)
		if err != nil {
			return err
		}
		if n != 1 {
			return chatrender.ErrLease
		}
		return nil
	})
}

// chatlangEagerReadersTx is the send-time request for translations: one
// preference per distinct reading language among the conversation's active
// members who have translation on and do not read the message's language,
// and only when the workspace and channel allow translation and the month's
// budget is not spent. Everyone else is served lazily, on their first read.
func chatlangEagerReadersTx(ctx context.Context, tx dbport.Tx, tenant, post, source string) ([]chatrender.Preference, error) {
	if source == "" || source == "und" {
		return nil, nil
	}
	// A database that has not yet had the translation migration applied sends
	// messages exactly as before.
	var present bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('chatlang_setting') IS NOT NULL`).Scan(&present); err != nil || !present {
		return nil, err
	}
	var conversation string
	if err := tx.QueryRow(ctx, `SELECT conversation_id FROM chat_post WHERE tenant_id=$1 AND id=$2`, tenant, post).Scan(&conversation); err != nil {
		return nil, err
	}
	workspace, err := chatlangReadWorkspaceTx(ctx, tx, tenant)
	if err != nil || !workspace.Enabled {
		return nil, err
	}
	channel, err := chatlangReadChannelTx(ctx, tx, tenant, conversation)
	if err != nil {
		return nil, err
	}
	// The only engine today is outside the deployment.
	if chatlang.ChannelOn(workspace, channel) != chatlang.Allowed || !chatlang.ExternalOK(workspace, channel) {
		return nil, nil
	}
	spent, err := chatlangSpentTx(ctx, tx, tenant, time.Now())
	if err != nil || spent >= workspace.Budget() {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT pref.value FROM chat_membership m JOIN LATERAL (SELECT p.value FROM chat_preference p WHERE p.tenant_id=m.tenant_id AND p.home_tenant_id=m.home_tenant_id AND p.member_id=m.member_id AND p.marker=$3 AND p.conversation_id IN ('',m.conversation_id) ORDER BY (p.conversation_id=m.conversation_id) DESC LIMIT 1) pref ON true WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.state='active' AND m.home_tenant_id=m.tenant_id`, tenant, conversation, chatrenderLanguageMarker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var readers []chatrender.Preference
	for rows.Next() {
		var b []byte
		var pref chatrender.Preference
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if json.Unmarshal(b, &pref) != nil || pref.Validate() != nil {
			continue
		}
		language := chatrender.Language(pref.ReadingLanguage)
		if !pref.Translate || !chatrender.WantsTranslation(pref, source) || !workspace.Offers(language) {
			continue
		}
		pref.Tone = chatrender.AsWritten
		readers = append(readers, pref)
	}
	return readers, rows.Err()
}
