// Package projectcommentstore persists immutable task comment revisions and activity.
package projectcommentstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	ErrInvalidRecord = errors.New("project comment store: invalid record")
	ErrNotFound      = projectactivity.ErrNotFound
)

// Store implements projectactivity.Store on the project-owned PostgreSQL store.
type Store struct{ projects *projectstore.Store }

// IdempotentStore is the command API for application writes. Idempotency keys
// are scoped to tenant, actor, and action; replay returns the first result.
type IdempotentStore interface {
	projectactivity.Store
	CreateIdempotent(context.Context, projectactivity.Comment, projectactivity.Activity, string) (projectactivity.Comment, error)
	CorrectIdempotent(context.Context, string, string, string, string, uint64, projectactivity.Revision, projectactivity.Activity, string) (projectactivity.Comment, error)
	DeleteIdempotent(context.Context, string, string, string, string, uint64, projectactivity.Revision, projectactivity.Activity, string) (projectactivity.Comment, error)
}

func New(projects *projectstore.Store) (*Store, error) {
	if projects == nil {
		return nil, projectactivity.ErrPortMissing
	}
	return &Store{projects: projects}, nil
}

var _ projectactivity.Store = (*Store)(nil)
var _ IdempotentStore = (*Store)(nil)

func (s *Store) CreateIdempotent(ctx context.Context, c projectactivity.Comment, a projectactivity.Activity, key string) (projectactivity.Comment, error) {
	if s == nil || s.projects == nil || !validComment(c) || len(c.Revisions) != 1 || c.Current().Number != 1 || c.Current().Tombstone || !validActivity(c, a, 1, "COMMENT_CREATED") || c.Current().ActorID != a.ActorID || !c.Current().At.Equal(a.At) || !validKey(key) {
		return projectactivity.Comment{}, ErrInvalidRecord
	}
	fingerprint, err := createFingerprint(c, a)
	if err != nil {
		return projectactivity.Comment{}, err
	}
	var out projectactivity.Comment
	err = s.projects.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		replayed, err := existingCommand(ctx, tx, c.TenantID, a.ActorID, "COMMENT_CREATE", key, fingerprint, &out)
		if err != nil || replayed {
			return err
		}
		inserted, err := claimCommand(ctx, tx, c.TenantID, a.ActorID, "COMMENT_CREATE", key, fingerprint, c)
		if err != nil {
			return err
		}
		if !inserted {
			return decodeResult(ctx, tx, c.TenantID, a.ActorID, "COMMENT_CREATE", key, fingerprint, &out)
		}
		if err := createTx(ctx, tx, c, a); err != nil {
			return err
		}
		out = c
		return nil
	})
	if err != nil {
		return projectactivity.Comment{}, err
	}
	return out, nil
}

func (s *Store) CorrectIdempotent(ctx context.Context, tenant, project, task, id string, expected uint64, revision projectactivity.Revision, activity projectactivity.Activity, key string) (projectactivity.Comment, error) {
	return s.correctIdempotent(ctx, tenant, project, task, id, expected, revision, activity, key, false)
}

func (s *Store) DeleteIdempotent(ctx context.Context, tenant, project, task, id string, expected uint64, revision projectactivity.Revision, activity projectactivity.Activity, key string) (projectactivity.Comment, error) {
	return s.correctIdempotent(ctx, tenant, project, task, id, expected, revision, activity, key, true)
}

func (s *Store) correctIdempotent(ctx context.Context, tenant, project, task, id string, expected uint64, revision projectactivity.Revision, activity projectactivity.Activity, key string, deleting bool) (projectactivity.Comment, error) {
	if s == nil || s.projects == nil || tenant == "" || project == "" || task == "" || id == "" || expected == 0 || expected >= uint64(^uint64(0)>>1) || revision.Number != expected+1 || revision.ActorID == "" || revision.At.IsZero() || (!revision.Tombstone && !validSafeText(revision.Text)) || revision.Tombstone != deleting || !validKey(key) {
		return projectactivity.Comment{}, ErrInvalidRecord
	}
	action, kind := "COMMENT_CORRECT", "COMMENT_CORRECTED"
	if deleting {
		action, kind = "COMMENT_DELETE", "COMMENT_TOMBSTONED"
	}
	comment := projectactivity.Comment{TenantID: tenant, ProjectID: project, TaskID: task, ID: id}
	if !validActivity(comment, activity, revision.Number, kind) || revision.ActorID != activity.ActorID || !revision.At.Equal(activity.At) {
		return projectactivity.Comment{}, ErrInvalidRecord
	}
	fingerprint, err := correctionFingerprint(tenant, project, task, id, expected, revision, activity, action)
	if err != nil {
		return projectactivity.Comment{}, err
	}
	var out projectactivity.Comment
	err = s.projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		replayed, err := existingCommand(ctx, tx, tenant, activity.ActorID, action, key, fingerprint, &out)
		if err != nil || replayed {
			return err
		}
		var created time.Time
		if err := tx.QueryRow(ctx, `SELECT created_at FROM project_task_comment WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND id=$4`, tenant, project, task, id).Scan(&created); err != nil {
			return mapNoRows(err)
		}
		result := projectactivity.Comment{TenantID: tenant, ProjectID: project, TaskID: task, ID: id, CreatedAt: created, Revisions: []projectactivity.Revision{revision}}
		inserted, err := claimCommand(ctx, tx, tenant, activity.ActorID, action, key, fingerprint, result)
		if err != nil {
			return err
		}
		if !inserted {
			return decodeResult(ctx, tx, tenant, activity.ActorID, action, key, fingerprint, &out)
		}
		if err := correctTx(ctx, tx, tenant, project, task, id, expected, revision, activity); err != nil {
			return err
		}
		out = result
		return nil
	})
	if err != nil {
		return projectactivity.Comment{}, err
	}
	return out, nil
}

func (s *Store) Create(ctx context.Context, c projectactivity.Comment, a projectactivity.Activity) error {
	if s == nil || s.projects == nil || !validComment(c) || len(c.Revisions) != 1 || c.Current().Number != 1 || c.Current().Tombstone || !validActivity(c, a, 1, "COMMENT_CREATED") || c.Current().ActorID != a.ActorID || !c.Current().At.Equal(a.At) {
		return ErrInvalidRecord
	}
	return s.projects.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		return createTx(ctx, tx, c, a)
	})
}

func (s *Store) Correct(ctx context.Context, tenant, project, task, id string, expected uint64, revision projectactivity.Revision, activity projectactivity.Activity) (projectactivity.Comment, error) {
	if s == nil || s.projects == nil || tenant == "" || project == "" || task == "" || id == "" || expected == 0 || expected >= uint64(^uint64(0)>>1) || revision.Number != expected+1 || revision.ActorID == "" || revision.At.IsZero() || (!revision.Tombstone && !validSafeText(revision.Text)) {
		return projectactivity.Comment{}, ErrInvalidRecord
	}
	kind := "COMMENT_CORRECTED"
	if revision.Tombstone {
		kind = "COMMENT_TOMBSTONED"
	}
	comment := projectactivity.Comment{TenantID: tenant, ProjectID: project, TaskID: task, ID: id}
	if !validActivity(comment, activity, revision.Number, kind) || revision.ActorID != activity.ActorID || !revision.At.Equal(activity.At) {
		return projectactivity.Comment{}, ErrInvalidRecord
	}
	var out projectactivity.Comment
	err := s.projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var created time.Time
		if err := tx.QueryRow(ctx, `SELECT created_at FROM project_task_comment WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND id=$4`, tenant, project, task, id).Scan(&created); err != nil {
			return mapNoRows(err)
		}
		if err := correctTx(ctx, tx, tenant, project, task, id, expected, revision, activity); err != nil {
			return err
		}
		out = projectactivity.Comment{TenantID: tenant, ProjectID: project, TaskID: task, ID: id, CreatedAt: created, Revisions: []projectactivity.Revision{revision}}
		return nil
	})
	if err != nil {
		return projectactivity.Comment{}, err
	}
	return out, nil
}

func (s *Store) Page(ctx context.Context, tenant, project, task string, after, ceiling uint64, limit int) ([]projectactivity.Comment, []projectactivity.Activity, uint64, uint64, bool, error) {
	if s == nil || s.projects == nil || tenant == "" || project == "" || task == "" || after > uint64(^uint64(0)>>1) || ceiling > uint64(^uint64(0)>>1) || (ceiling != 0 && after > ceiling) || limit < 1 || limit > projectactivity.MaxPageSize {
		return nil, nil, 0, 0, false, ErrInvalidRecord
	}
	var comments []projectactivity.Comment
	var activities []projectactivity.Activity
	var last, snapshot uint64
	var more bool
	err := s.projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0) FROM project_task_comment_activity WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3`, tenant, project, task).Scan(&snapshot); err != nil {
			return err
		}
		if ceiling == 0 {
			ceiling = snapshot
		}
		rows, err := tx.Query(ctx, `SELECT a.sequence,a.tenant_id,a.project_id,a.task_id,a.comment_id,a.actor_id,a.kind,a.revision,a.created_at,
 c.created_at,r.source_text,r.safe_html,r.mention_handles,r.mentions,r.tombstone
 FROM project_task_comment_activity a
 JOIN project_task_comment c ON (c.tenant_id,c.project_id,c.task_id,c.id)=(a.tenant_id,a.project_id,a.task_id,a.comment_id)
 JOIN project_task_comment_revision r ON (r.tenant_id,r.project_id,r.task_id,r.comment_id,r.revision)=(a.tenant_id,a.project_id,a.task_id,a.comment_id,a.revision)
 WHERE a.tenant_id=$1 AND a.project_id=$2 AND a.task_id=$3 AND a.sequence>$4 AND a.sequence<=$5
 ORDER BY a.sequence LIMIT $6`, tenant, project, task, after, ceiling, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		comments = make([]projectactivity.Comment, 0, limit)
		activities = make([]projectactivity.Activity, 0, limit)
		for rows.Next() {
			var a projectactivity.Activity
			var c projectactivity.Comment
			var r projectactivity.Revision
			var sequence, revision int64
			var handles, mentions []byte
			if err := rows.Scan(&sequence, &a.TenantID, &a.ProjectID, &a.TaskID, &a.CommentID, &a.ActorID, &a.Kind, &revision, &a.At, &c.CreatedAt, &r.Text.Source, &r.Text.HTML, &handles, &mentions, &r.Tombstone); err != nil {
				return err
			}
			if err := json.Unmarshal(handles, &r.Text.MentionHandles); err != nil {
				return err
			}
			if err := json.Unmarshal(mentions, &r.Text.Mentions); err != nil {
				return err
			}
			if sequence <= 0 || revision <= 0 {
				return ErrInvalidRecord
			}
			a.Sequence, a.Revision, r.Number, r.ActorID, r.At = uint64(sequence), uint64(revision), uint64(revision), a.ActorID, a.At
			c.TenantID, c.ProjectID, c.TaskID, c.ID = tenant, project, task, a.CommentID
			c.Revisions = []projectactivity.Revision{r}
			if len(activities) == limit {
				more = true
				break
			}
			last = a.Sequence
			activities = append(activities, a)
			comments = append(comments, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, 0, 0, false, err
	}
	return comments, activities, last, snapshot, more, nil
}

// TimelinePage merges task mutations and comment changes by the sequence
// allocated while holding the task row lock. Only approved display metadata is
// selected from project_activity; its opaque payload is never read.
func (s *Store) TimelinePage(ctx context.Context, tenant, project, task string, after, ceiling uint64, limit int) ([]projectactivity.Activity, uint64, uint64, bool, error) {
	if s == nil || s.projects == nil || tenant == "" || project == "" || task == "" || limit < 1 || limit > projectactivity.MaxPageSize || after > uint64(^uint64(0)>>1) || ceiling > uint64(^uint64(0)>>1) || (ceiling != 0 && after > ceiling) {
		return nil, 0, 0, false, ErrInvalidRecord
	}
	var entries []projectactivity.Activity
	var last, snapshot uint64
	var more bool
	err := s.projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var current int64
		if err := tx.QueryRow(ctx, `SELECT GREATEST(activity_sequence,
 (SELECT count(*) FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id=$3 AND event_type LIKE 'task.%' AND task_sequence=0) +
 (SELECT count(*) FROM project_task_comment_activity WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND task_sequence=0))
 FROM project_task WHERE tenant_id=$1 AND project_id=$2 AND id=$3`, tenant, project, task).Scan(&current); err != nil {
			return mapNoRows(err)
		}
		if current < 0 {
			return ErrInvalidRecord
		}
		snapshot = uint64(current)
		if ceiling == 0 {
			ceiling = snapshot
		}
		rows, err := tx.Query(ctx, `WITH legacy AS (
 SELECT row_number() OVER (ORDER BY created_at, source, stable_id) AS sequence, comment_id, kind, revision, actor_id, created_at
 FROM (
  SELECT 'M'::text AS source, id AS stable_id, ''::text AS comment_id, event_type AS kind, new_revision AS revision, actor_id, created_at
  FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id=$3 AND event_type LIKE 'task.%' AND task_sequence=0
  UNION ALL
  SELECT 'C'::text, sequence::text, comment_id, kind, revision, actor_id, created_at
  FROM project_task_comment_activity WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND task_sequence=0
 ) old_rows
), timeline AS (
 SELECT task_sequence AS sequence, comment_id, kind, revision, actor_id, created_at
 FROM project_task_comment_activity WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND task_sequence>0
 UNION ALL
 SELECT task_sequence, ''::text, event_type, new_revision, actor_id, created_at
 FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id=$3 AND event_type LIKE 'task.%' AND task_sequence>0
 UNION ALL
 SELECT sequence, comment_id, kind, revision, actor_id, created_at FROM legacy
)
SELECT sequence,comment_id,kind,revision,actor_id,created_at FROM timeline
WHERE sequence>$4 AND sequence<=$5 ORDER BY sequence LIMIT $6`, tenant, project, task, after, ceiling, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		entries = make([]projectactivity.Activity, 0, limit)
		for rows.Next() {
			var a projectactivity.Activity
			var seq, revision int64
			if err := rows.Scan(&seq, &a.CommentID, &a.Kind, &revision, &a.ActorID, &a.At); err != nil {
				return err
			}
			if seq <= 0 || revision <= 0 {
				return ErrInvalidRecord
			}
			a.Sequence, a.Revision, a.TenantID, a.ProjectID, a.TaskID = uint64(seq), uint64(revision), tenant, project, task
			if len(entries) == limit {
				more = true
				break
			}
			last = a.Sequence
			entries = append(entries, a)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, 0, false, err
	}
	return entries, last, snapshot, more, nil
}

func createTx(ctx context.Context, tx dbport.Tx, c projectactivity.Comment, a projectactivity.Activity) error {
	sequence, err := lockTask(ctx, tx, c.TenantID, c.ProjectID, c.TaskID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO project_task_comment(tenant_id,project_id,task_id,id,created_at) VALUES($1,$2,$3,$4,$5)`, c.TenantID, c.ProjectID, c.TaskID, c.ID, c.CreatedAt.UTC())
	if err != nil {
		return mapConflict(err)
	}
	if err := insertRevision(ctx, tx, c.TenantID, c.ProjectID, c.TaskID, c.ID, c.Current()); err != nil {
		return err
	}
	if err := insertActivity(ctx, tx, c.TenantID, c.ProjectID, c.TaskID, c.ID, a, sequence); err != nil {
		return err
	}
	return appendCommentEvent(ctx, tx, c.TenantID, c.ProjectID, c.TaskID, c.ID, a)
}

func correctTx(ctx context.Context, tx dbport.Tx, tenant, project, task, id string, expected uint64, revision projectactivity.Revision, activity projectactivity.Activity) error {
	sequence, err := lockTask(ctx, tx, tenant, project, task)
	if err != nil {
		return err
	}
	var created time.Time
	if err := tx.QueryRow(ctx, `SELECT created_at FROM project_task_comment WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND id=$4 FOR UPDATE`, tenant, project, task, id).Scan(&created); err != nil {
		return mapNoRows(err)
	}
	var current int64
	var tombstone bool
	if err := tx.QueryRow(ctx, `SELECT revision,tombstone FROM project_task_comment_revision WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND comment_id=$4 ORDER BY revision DESC LIMIT 1`, tenant, project, task, id).Scan(&current, &tombstone); err != nil {
		return mapNoRows(err)
	}
	if uint64(current) != expected || tombstone {
		return projectactivity.ErrConflict
	}
	if err := insertRevision(ctx, tx, tenant, project, task, id, revision); err != nil {
		return err
	}
	if err := insertActivity(ctx, tx, tenant, project, task, id, activity, sequence); err != nil {
		return err
	}
	return appendCommentEvent(ctx, tx, tenant, project, task, id, activity)
}

func appendCommentEvent(ctx context.Context, tx dbport.Tx, tenant, project, task, comment string, activity projectactivity.Activity) error {
	kind := strings.ToLower(strings.TrimPrefix(activity.Kind, "COMMENT_"))
	eventType := "task.comment." + kind
	_, err := projectstore.AppendOutboxEventTx(ctx, tx, projectstore.AppendOutboxEvent{
		TenantID: tenant, ProjectID: project, AggregateID: task, EventType: eventType,
		SourceRevision: int64(activity.Revision), Classification: "INTERNAL",
		Value: map[string]any{"taskId": task, "commentId": comment, "commentRevision": activity.Revision, "kind": activity.Kind},
	})
	return err
}

func validKey(key string) bool { return key != "" && len(key) <= 200 && strings.TrimSpace(key) == key }

func existingCommand(ctx context.Context, tx dbport.Tx, tenant, actor, action, key, fingerprint string, result any) (bool, error) {
	var storedFingerprint string
	var storedResult []byte
	err := tx.QueryRow(ctx, `SELECT fingerprint,result_json FROM project_task_comment_idempotency WHERE tenant_id=$1 AND actor_id=$2 AND action=$3 AND client_key=$4`, tenant, actor, action, key).Scan(&storedFingerprint, &storedResult)
	if errors.Is(err, dbport.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if storedFingerprint != fingerprint {
		return false, projectactivity.ErrConflict
	}
	if err := json.Unmarshal(storedResult, result); err != nil {
		return false, err
	}
	return true, nil
}

func claimCommand(ctx context.Context, tx dbport.Tx, tenant, actor, action, key, fingerprint string, result any) (bool, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return false, err
	}
	count, err := tx.Exec(ctx, `INSERT INTO project_task_comment_idempotency(tenant_id,actor_id,action,client_key,fingerprint,result_json) VALUES($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT (tenant_id,actor_id,action,client_key) DO NOTHING`, tenant, actor, action, key, fingerprint, encoded)
	if err != nil {
		return false, err
	}
	if count == 1 {
		return true, nil
	}
	var storedFingerprint string
	if err := tx.QueryRow(ctx, `SELECT fingerprint FROM project_task_comment_idempotency WHERE tenant_id=$1 AND actor_id=$2 AND action=$3 AND client_key=$4`, tenant, actor, action, key).Scan(&storedFingerprint); err != nil {
		return false, err
	}
	if storedFingerprint != fingerprint {
		return false, projectactivity.ErrConflict
	}
	return false, nil
}

func decodeResult(ctx context.Context, tx dbport.Tx, tenant, actor, action, key, fingerprint string, result any) error {
	var storedFingerprint string
	var storedResult []byte
	if err := tx.QueryRow(ctx, `SELECT fingerprint,result_json FROM project_task_comment_idempotency WHERE tenant_id=$1 AND actor_id=$2 AND action=$3 AND client_key=$4`, tenant, actor, action, key).Scan(&storedFingerprint, &storedResult); err != nil {
		return err
	}
	if storedFingerprint != fingerprint {
		return projectactivity.ErrConflict
	}
	return json.Unmarshal(storedResult, result)
}

type revisionFingerprint struct {
	Number         uint64
	Source, HTML   string
	MentionHandles []string
	Mentions       []projectactivity.Mention
	Tombstone      bool
	ActorID        string
}
type activityFingerprint struct {
	TenantID, ProjectID, TaskID, CommentID, ActorID, Kind string
	Revision                                              uint64
}

func revisionValue(r projectactivity.Revision) revisionFingerprint {
	return revisionFingerprint{r.Number, r.Text.Source, r.Text.HTML, r.Text.MentionHandles, r.Text.Mentions, r.Tombstone, r.ActorID}
}
func activityValue(a projectactivity.Activity) activityFingerprint {
	return activityFingerprint{a.TenantID, a.ProjectID, a.TaskID, a.CommentID, a.ActorID, a.Kind, a.Revision}
}
func createFingerprint(c projectactivity.Comment, a projectactivity.Activity) (string, error) {
	value := struct {
		TenantID, ProjectID, TaskID, CommentID string
		Revision                               revisionFingerprint
		Activity                               activityFingerprint
	}{c.TenantID, c.ProjectID, c.TaskID, c.ID, revisionValue(c.Current()), activityValue(a)}
	return digest(value)
}
func correctionFingerprint(tenant, project, task, id string, expected uint64, r projectactivity.Revision, a projectactivity.Activity, action string) (string, error) {
	value := struct {
		TenantID, ProjectID, TaskID, CommentID, Action string
		Expected                                       uint64
		Revision                                       revisionFingerprint
		Activity                                       activityFingerprint
	}{tenant, project, task, id, action, expected, revisionValue(r), activityValue(a)}
	return digest(value)
}
func digest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validComment(c projectactivity.Comment) bool {
	return c.TenantID != "" && c.ProjectID != "" && c.TaskID != "" && c.ID != "" && !c.CreatedAt.IsZero() && strings.TrimSpace(c.ID) == c.ID
}
func validSafeText(t projectactivity.SafeText) bool {
	if t.Source == "" || t.HTML == "" || len(t.Source) > projectactivity.MaxTextBytes || len(t.HTML) > projectactivity.MaxTextBytes || len(t.MentionHandles) > 32 || len(t.Mentions) > 32 {
		return false
	}
	for _, mention := range t.Mentions {
		if mention.SubjectID == "" || len(mention.SubjectID) > 256 || len(mention.DisplayName) > 256 {
			return false
		}
	}
	for _, handle := range t.MentionHandles {
		if handle == "" || len(handle) > 256 {
			return false
		}
	}
	return safeMarkup(t.HTML)
}

// safeMarkup permits a small inert formatting set. This is a storage boundary
// check so callers cannot bypass the service sanitizer by constructing SafeText.
func safeMarkup(source string) bool {
	contextNode := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(source), contextNode)
	if err != nil {
		return false
	}
	var visit func(*html.Node) bool
	visit = func(n *html.Node) bool {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "br", "strong", "em", "b", "i", "code", "pre", "ul", "ol", "li", "blockquote", "a":
			default:
				return false
			}
			for _, attr := range n.Attr {
				if n.Data != "a" || (attr.Key != "href" && attr.Key != "title") {
					return false
				}
				if attr.Key == "href" {
					u, err := url.Parse(strings.TrimSpace(attr.Val))
					if err != nil || (u.Scheme != "" && u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto") || strings.HasPrefix(strings.TrimSpace(attr.Val), "//") {
						return false
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if !visit(child) {
				return false
			}
		}
		return true
	}
	for _, node := range nodes {
		if !visit(node) {
			return false
		}
	}
	return len(nodes) > 0
}
func validActivity(c projectactivity.Comment, a projectactivity.Activity, revision uint64, kind string) bool {
	return a.TenantID == c.TenantID && a.ProjectID == c.ProjectID && a.TaskID == c.TaskID && a.CommentID == c.ID && a.ActorID != "" && a.Revision == revision && a.Kind == kind && !a.At.IsZero()
}
func insertRevision(ctx context.Context, tx dbport.Tx, tenant, project, task, id string, r projectactivity.Revision) error {
	if r.Number == 0 || r.Number > uint64(^uint64(0)>>1) || r.ActorID == "" || r.At.IsZero() || (!r.Tombstone && !validSafeText(r.Text)) || (r.Tombstone && (r.Text.Source != "" || r.Text.HTML != "" || len(r.Text.MentionHandles) != 0 || len(r.Text.Mentions) != 0)) {
		return ErrInvalidRecord
	}
	handles, err := json.Marshal(r.Text.MentionHandles)
	if err != nil {
		return err
	}
	mentions, err := json.Marshal(r.Text.Mentions)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO project_task_comment_revision(tenant_id,project_id,task_id,comment_id,revision,source_text,safe_html,mention_handles,mentions,tombstone,actor_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10,$11,$12)`, tenant, project, task, id, r.Number, r.Text.Source, r.Text.HTML, handles, mentions, r.Tombstone, r.ActorID, r.At.UTC())
	return mapConflict(err)
}
func insertActivity(ctx context.Context, tx dbport.Tx, tenant, project, task, id string, a projectactivity.Activity, sequence int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO project_task_comment_activity(tenant_id,project_id,task_id,comment_id,revision,actor_id,kind,created_at,task_sequence) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tenant, project, task, id, a.Revision, a.ActorID, a.Kind, a.At.UTC(), sequence)
	return mapConflict(err)
}

// Serializing writes on the parent task keeps activity sequence order aligned
// with commit order, so a snapshot cursor cannot skip a late commit.
func lockTask(ctx context.Context, tx dbport.Tx, tenant, project, task string) (int64, error) {
	var found string
	if err := tx.QueryRow(ctx, `SELECT id FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, project).Scan(&found); err != nil {
		return 0, mapNoRows(err)
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `UPDATE project_task SET activity_sequence=GREATEST(activity_sequence,
 (SELECT count(*) FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id=$3 AND event_type LIKE 'task.%' AND task_sequence=0) +
 (SELECT count(*) FROM project_task_comment_activity WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND task_sequence=0)) + 1
 WHERE tenant_id=$1 AND project_id=$2 AND id=$3 RETURNING activity_sequence`, tenant, project, task).Scan(&sequence); err != nil {
		return 0, mapNoRows(err)
	}
	return sequence, nil
}
func mapNoRows(err error) error {
	if errors.Is(err, dbport.ErrNoRows) {
		return projectactivity.ErrNotFound
	}
	return err
}
func mapConflict(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "40001") {
		return fmt.Errorf("%w: %v", projectactivity.ErrConflict, err)
	}
	return err
}
