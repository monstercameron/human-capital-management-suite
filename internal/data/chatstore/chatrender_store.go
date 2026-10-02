package chatstore

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// RenderingScope binds every user operation to the current caller. A transport
// must derive Principal from its admitted session, never from JSON.
type RenderingScope struct {
	Principal            chat.Principal
	Tenant, Conversation string
}

func chatrenderAccess(ctx context.Context, tx dbport.Tx, s RenderingScope, post string) error {
	if s.Principal.TenantID == "" || s.Principal.SubjectID == "" || s.Tenant == "" {
		return chatrender.ErrDenied
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_membership m WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' AND ($5='' OR EXISTS(SELECT 1 FROM chat_post p WHERE p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id AND p.id=$5 AND NOT p.tombstoned AND (m.history_visibility='FULL_HISTORY' OR p.created_at>=m.joined_at))))`, s.Tenant, s.Conversation, s.Principal.TenantID, s.Principal.SubjectID, post).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return chatrender.ErrDenied
	}
	return nil
}
func (s *Store) renderingTx(ctx context.Context, scope RenderingScope, post string, fn func(dbport.Tx) error) error {
	return s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		if err := chatrenderAccess(ctx, tx, scope, post); err != nil {
			return err
		}
		return fn(tx)
	})
}

// ListRenderings fetches an entire page in one query, filtering access first.
func (s *Store) ListRenderings(ctx context.Context, scope RenderingScope, posts []string) ([]chatrender.Rendering, error) {
	out := []chatrender.Rendering{}
	if len(posts) > 200 {
		return nil, chatrender.ErrInvalid
	}
	err := s.renderingTx(ctx, scope, "", func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT r.rendering FROM chatrender_rendering r JOIN chat_post p ON p.id=r.post_id AND p.tenant_id=r.tenant_id AND p.revision=r.revision JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' AND NOT p.tombstoned AND (m.history_visibility='FULL_HISTORY' OR p.created_at>=m.joined_at) AND p.id=ANY($5::text[]) ORDER BY r.post_id,r.tone,r.language`, scope.Tenant, scope.Conversation, scope.Principal.TenantID, scope.Principal.SubjectID, posts)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			var r chatrender.Rendering
			if err = rows.Scan(&b); err != nil {
				return err
			}
			if err = json.Unmarshal(b, &r); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
func validRenderingTarget(r chatrender.Rendering) bool {
	return r.Message != "" && r.Revision > 0 && (r.Tone == chatrender.AsWritten || r.Tone == chatrender.Reworded) && (chatrender.Supported(r.Language) || r.Language == "mul")
}
func (s *Store) RequestRendering(ctx context.Context, scope RenderingScope, r chatrender.Rendering) error {
	r.Language = chatrender.Language(r.Language)
	if !validRenderingTarget(r) || r.Tenant != scope.Tenant || len(r.Kinds) > 8 {
		return chatrender.ErrInvalid
	}
	wantsMask := false
	var customKinds []chatrender.Kind
	for _, kind := range r.Kinds {
		if kind == chatrender.Mask {
			wantsMask = true
		} else if kind != chatrender.Reword && kind != chatrender.Translate {
			if kind == "" || len(kind) > 80 {
				return chatrender.ErrInvalid
			}
			duplicate := false
			for _, prior := range customKinds {
				if prior == kind {
					duplicate = true
				}
			}
			if !duplicate {
				customKinds = append(customKinds, kind)
			}
		}
	}
	// Target metadata is rebuilt from the durable revision, never supplied text.
	return s.renderingTx(ctx, scope, r.Message, func(tx dbport.Tx) error {
		var body, source string
		if err := tx.QueryRow(ctx, `SELECT p.body,v.source_language FROM chat_post p JOIN chat_post_revision v ON v.tenant_id=p.tenant_id AND v.post_id=p.id AND v.revision=p.revision WHERE p.tenant_id=$1 AND p.id=$2 AND p.revision=$3 AND NOT p.tombstoned FOR SHARE OF p`, scope.Tenant, r.Message, r.Revision).Scan(&body, &source); err != nil {
			return err
		}
		r.Text = body
		r.SourceLanguage = source
		r.Kinds = nil
		r.Checks = chatrender.Checks{}
		r.Producer = chatrender.ProducerIdentity{}
		r.Producers = nil
		r.CostReference = ""
		r.Confidence = 0
		if wantsMask {
			r.Kinds = append(r.Kinds, chatrender.Mask)
		}
		if r.Tone == chatrender.Reworded {
			r.Kinds = append(r.Kinds, chatrender.Reword)
		}
		if chatrender.Language(source) != chatrender.Language(r.Language) {
			if source == "und" {
				return chatrender.ErrInvalid
			}
			r.Kinds = append(r.Kinds, chatrender.Translate)
		}
		r.Kinds = append(r.Kinds, customKinds...)
		if len(r.Kinds) == 0 {
			return chatrender.ErrInvalid
		}
		if (r.Language == "und" || r.Language == "mul") && r.Language != source {
			return chatrender.ErrInvalid
		}
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if wantsMask {
			var prior []byte
			err := tx.QueryRow(ctx, `SELECT request FROM chatrender_job WHERE tenant_id=$1 AND post_id=$2 AND revision=$3 AND tone=$4 AND language=$5 FOR UPDATE`, scope.Tenant, r.Message, r.Revision, r.Tone, r.Language).Scan(&prior)
			if err != nil && err != dbport.ErrNoRows {
				return err
			}
			if err == nil {
				var previous chatrender.Rendering
				if err = json.Unmarshal(prior, &previous); err != nil {
					return err
				}
				masked := false
				for _, kind := range previous.Kinds {
					if kind == chatrender.Mask {
						masked = true
					}
				}
				if !masked {
					if _, err = tx.Exec(ctx, `DELETE FROM chatrender_rendering WHERE tenant_id=$1 AND post_id=$2 AND revision=$3 AND tone=$4 AND language=$5`, scope.Tenant, r.Message, r.Revision, r.Tone, r.Language); err != nil {
						return err
					}
					if _, err = tx.Exec(ctx, `DELETE FROM chatrender_job WHERE tenant_id=$1 AND post_id=$2 AND revision=$3 AND tone=$4 AND language=$5`, scope.Tenant, r.Message, r.Revision, r.Tone, r.Language); err != nil {
						return err
					}
				}
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO chatrender_job(tenant_id,post_id,revision,tone,language,request) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, scope.Tenant, r.Message, r.Revision, r.Tone, r.Language, b)
		return err
	})
}

type RenderingJob = chatrender.Job

// ClaimRendering is a worker port. Lease tokens fence replay and reclaim.
func (s *Store) ClaimRendering(ctx context.Context, tenantID string, lease time.Duration) (RenderingJob, error) {
	var job RenderingJob
	if lease <= 0 || lease > 5*time.Minute {
		return job, chatrender.ErrInvalid
	}
	var claimErr error
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE chatrender_job SET state='failed',failure='lease expired',lease_until=NULL WHERE tenant_id=$1 AND state='claimed' AND attempts=3 AND lease_until<now()`, tenantID); err != nil {
			return err
		}
		var b []byte
		job.Lease = uuid.NewString()
		err := tx.QueryRow(ctx, `WITH candidate AS (SELECT j.tenant_id,j.post_id,j.revision,j.tone,j.language FROM chatrender_job j JOIN chat_post p ON p.tenant_id=j.tenant_id AND p.id=j.post_id AND p.revision=j.revision WHERE j.tenant_id=$1 AND NOT p.tombstoned AND j.attempts<3 AND (j.state='queued' OR (j.state='claimed' AND j.lease_until<now())) ORDER BY j.requested_at FOR UPDATE OF j SKIP LOCKED LIMIT 1) UPDATE chatrender_job j SET state='claimed',attempts=attempts+1,lease_token=$2,lease_until=now()+$3*interval '1 millisecond' FROM candidate c WHERE (j.tenant_id,j.post_id,j.revision,j.tone,j.language)=(c.tenant_id,c.post_id,c.revision,c.tone,c.language) RETURNING j.request,j.attempts`, tenantID, job.Lease, lease.Milliseconds()).Scan(&b, &job.Attempts)
		if err == dbport.ErrNoRows {
			claimErr = err
			return nil
		}
		if err != nil {
			return err
		}
		return json.Unmarshal(b, &job.Request)
	})
	if err != nil {
		return job, err
	}
	return job, claimErr
}
func (s *Store) CompleteRendering(ctx context.Context, job RenderingJob, r chatrender.Rendering) error {
	want := job.Request
	if !validRenderingTarget(r) || r.Tenant != want.Tenant || r.Message != want.Message || r.Revision != want.Revision || r.Tone != want.Tone || r.Language != want.Language || !r.Checks.Meaning || !r.Checks.Placeholders || len(r.Checks.Failures) > 0 || r.Text == "" || len(r.Text) > 64000 || r.Confidence < 0 || r.Confidence > 1 {
		return chatrender.ErrInvalid
	}
	return s.RunTenantTx(ctx, r.Tenant, func(tx dbport.Tx) error {
		var request []byte
		err := tx.QueryRow(ctx, `UPDATE chatrender_job j SET state='complete',lease_until=NULL WHERE tenant_id=$1 AND post_id=$2 AND revision=$3 AND tone=$4 AND language=$5 AND state='claimed' AND lease_token=$6 AND lease_until>now() AND EXISTS(SELECT 1 FROM chat_post p WHERE p.tenant_id=j.tenant_id AND p.id=j.post_id AND p.revision=j.revision AND NOT p.tombstoned) RETURNING request`, r.Tenant, r.Message, r.Revision, r.Tone, r.Language, job.Lease).Scan(&request)
		if err == dbport.ErrNoRows {
			return chatrender.ErrLease
		}
		if err != nil {
			return err
		}
		var canonical chatrender.Rendering
		if err = json.Unmarshal(request, &canonical); err != nil {
			return err
		}
		r.Kinds = append([]chatrender.Kind(nil), canonical.Kinds...)
		r.SourceLanguage = canonical.SourceLanguage
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO chatrender_rendering(tenant_id,post_id,revision,tone,language,rendering) VALUES($1,$2,$3,$4,$5,$6)`, r.Tenant, r.Message, r.Revision, r.Tone, r.Language, b)
		return err
	})
}
func (s *Store) FailRendering(ctx context.Context, job RenderingJob) error {
	r := job.Request
	return s.RunTenantTx(ctx, r.Tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE chatrender_job SET state=CASE WHEN attempts<3 THEN 'queued' ELSE 'failed' END,failure='producer unavailable',lease_until=NULL WHERE tenant_id=$1 AND post_id=$2 AND revision=$3 AND tone=$4 AND language=$5 AND state='claimed' AND lease_token=$6 AND lease_until>now()`, r.Tenant, r.Message, r.Revision, r.Tone, r.Language, job.Lease)
		if err != nil {
			return err
		}
		if n != 1 {
			return chatrender.ErrLease
		}
		return nil
	})
}
func (s *Store) RenderingAvailability(ctx context.Context, scope RenderingScope, post string) (chatrender.Available, error) {
	return s.RenderingAvailabilityForTarget(ctx, scope, post, "", "")
}
func (s *Store) RenderingAvailabilityForTarget(ctx context.Context, scope RenderingScope, post string, tone chatrender.Tone, language string) (chatrender.Available, error) {
	var a chatrender.Available
	var err error
	a.Renderings, err = s.ListRenderings(ctx, scope, []string{post})
	if err != nil {
		return a, err
	}
	err = s.renderingTx(ctx, scope, post, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT COALESCE(bool_or(j.state IN ('queued','claimed')),false),COALESCE(bool_or(j.requested_at<now()-interval '2 seconds'),false),COALESCE(bool_or(j.state='failed' OR (j.state='claimed' AND j.attempts=3 AND j.lease_until<now())),false) FROM chatrender_job j JOIN chat_post p ON p.tenant_id=j.tenant_id AND p.id=j.post_id AND p.revision=j.revision WHERE j.tenant_id=$1 AND j.post_id=$2 AND ($3='' OR j.tone=$3) AND ($4='' OR j.language=$4)`, scope.Tenant, post, string(tone), language).Scan(&a.Pending, &a.WaitExpired, &a.Failed)
	})
	return a, err
}
func (s *Store) ReportRendering(ctx context.Context, scope RenderingScope, r chatrender.Rendering, reason string) error {
	if !validRenderingTarget(r) || r.Tenant != scope.Tenant || strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return chatrender.ErrInvalid
	}
	return s.renderingTx(ctx, scope, r.Message, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO chatrender_report(tenant_id,report_id,post_id,revision,tone,language,reporter_home_tenant_id,reporter_id,reason) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9 WHERE EXISTS(SELECT 1 FROM chatrender_rendering WHERE tenant_id=$1 AND post_id=$3 AND revision=$4 AND tone=$5 AND language=$6)`, scope.Tenant, uuid.NewString(), r.Message, r.Revision, r.Tone, r.Language, scope.Principal.TenantID, scope.Principal.SubjectID, reason)
		if err != nil {
			return err
		}
		if n != 1 {
			return chatrender.ErrInvalid
		}
		return nil
	})
}

// SearchRenderings filters the audience before text matching.
func (s *Store) SearchRenderings(ctx context.Context, scope RenderingScope, query string) ([]chatrender.Rendering, error) {
	out := []chatrender.Rendering{}
	err := s.renderingTx(ctx, scope, "", func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT r.rendering FROM chatrender_rendering r JOIN chat_post p ON p.id=r.post_id AND p.tenant_id=r.tenant_id AND p.revision=r.revision JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' AND NOT p.tombstoned AND (m.history_visibility='FULL_HISTORY' OR p.created_at>=m.joined_at) AND strpos(lower(r.rendering->>'text'),lower($5))>0 ORDER BY p.sequence DESC LIMIT 200`, scope.Tenant, scope.Conversation, scope.Principal.TenantID, scope.Principal.SubjectID, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			var r chatrender.Rendering
			if err = rows.Scan(&b); err != nil {
				return err
			}
			if err = json.Unmarshal(b, &r); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
