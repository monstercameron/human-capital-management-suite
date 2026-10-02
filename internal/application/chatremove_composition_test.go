package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestIntegrate1ModerationDirectory(t *testing.T) {
	now := time.Now()
	ctx := trust.WithPrincipal(t.Context(), personaDraftPrincipal(t, "tenant-a", "user:owner", now))
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "user:owner"}
	calls := 0
	d := chatremoveNameDirectory{Read: func(_ context.Context, tenant string, ids []string) (map[string]string, error) {
		calls++
		if tenant != "tenant-a" || len(ids) != 2 {
			t.Fatal("wrong directory scope", tenant, ids)
		}
		return map[string]string{"worker-a": "Dana Reader", "worker-b": "worker-b", "unrequested": "Secret"}, nil
	}}
	names, err := d.ModerationNames(ctx, p, p.TenantID, []string{"worker-a", "worker-b"})
	if err != nil || len(names) != 1 || names["worker-a"] != "Dana Reader" {
		t.Fatal("unsafe directory names", names, err)
	}
	if _, err = d.ModerationNames(ctx, p, "tenant-b", nil); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("cross tenant", err)
	}
	if _, err = d.ModerationNames(t.Context(), p, p.TenantID, nil); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("untrusted directory", err)
	}
	if calls != 1 {
		t.Fatal("denied request reached directory", calls)
	}
}

func TestIntegrate1ModerationRunTraceIntegration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(t.Context(), db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenant)
	now := time.Now().UTC()
	ctx := trust.WithPrincipal(t.Context(), personaDraftPrincipal(t, "tenant-a", "user:owner", now))
	s := chatremoveRunTrace{DB: workflowAgentTenantRunner{conn: db.NewConn(t)}, TenantUUID: func(t values.TenantId) uuid.UUID {
		if t == "tenant-a" {
			return tenant
		}
		return uuid.Nil
	}}
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, suffix := range []string{"a", "b"} {
		db.Exec(t, `INSERT INTO persona_final_outputs(tenant_id,invocation_id,output_id,invoker_id,conversation_id,thread_id,parent_post_id,persona_id,persona_version,installation_id,admission_digest,agent026_digest,persistence_digest,recovery_receipt,materials,citations,sealed_payload,created_at,admission_id,run_id)
		VALUES($1,$2,$3,'user:owner','room','thread','question','policy-helper','1','install',$4,$4,$4,'receipt','[]','[]','{}',$5,$6,$7)`, tenant, "invoke-"+suffix, "output-"+suffix, digest, now, "admission-"+suffix, "run-"+suffix)
		db.Exec(t, `INSERT INTO persona_reply_receipt(tenant_id,invocation_id,output_id,invoker_id,conversation_id,public_post_id,private_conversation_id,receipt)
		VALUES($1,$2,$3,'user:owner','room',$4,'private-room',$5::jsonb)`, tenant, "invoke-"+suffix, "output-"+suffix, "post-"+suffix, `{"PrivatePostID":"private-`+suffix+`"}`)
	}
	for _, tc := range []struct{ conversation, post, want string }{{"room", "post-a", "run-a"}, {"private-room", "private-b", "run-b"}, {"room", "private-b", ""}, {"other-room", "post-a", ""}, {"room", "missing", ""}} {
		got, err := s.RunForModerationPost(ctx, "tenant-a", tc.conversation, tc.post)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	if _, err := s.RunForModerationPost(ctx, "tenant-b", "room", "post-a"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("cross tenant trace", err)
	}
	if _, err := s.RunForModerationPost(t.Context(), "tenant-a", "room", "post-a"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("untrusted trace", err)
	}
	// An ambiguous immutable receipt is never guessed from the display name.
	db.Exec(t, `INSERT INTO persona_reply_receipt(tenant_id,invocation_id,output_id,invoker_id,conversation_id,public_post_id,private_conversation_id,receipt) VALUES($1,'invoke-c','output-b','user:owner','room','post-a','','{}')`, tenant)
	// The join also pins invocation_id, so this forged link cannot add a run.
	if got, err := s.RunForModerationPost(ctx, "tenant-a", "room", "post-a"); err != nil || got != "run-a" {
		t.Fatal("unbound receipt accepted", got, err)
	}
}
