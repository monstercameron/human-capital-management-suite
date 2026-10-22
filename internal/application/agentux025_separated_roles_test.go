package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

// TestTodo_AGENTUX_025_Integration_SeparatedRoles composes the mention path
// exactly as the served cell does — the same wiring functions, the same
// per-tenant runtime factory, the production admission, runstate, executor
// and reply delivery — over two role-separated stores, with the deterministic
// model fixture proposing one document search and then answering. The
// administrator's mention in the public channel and in the direct
// conversation each get one delivered answer grounded in the placed
// document, one completed run and one recorded delivery checkpoint.
func TestTodo_AGENTUX_025_Integration_SeparatedRoles(t *testing.T) {
	h := newAgentux025Harness(t)
	agentux025AssertRoleSeparation(t, h)

	channelPost := h.postMention(agentux025Channel, "@policy-helper how much PTO carries over?", "agentux-025-channel-mention")
	directPost := h.postMention(agentux025DM, "@policy-helper how much PTO carries over?", "agentux-025-direct-mention")

	channelReply := h.waitForReply(agentux025Channel, channelPost.ID)
	directCopy := h.waitForDMCopy(directPost.ID)

	agentux025AssertGroundedReply(t, "channel", channelReply.Body)
	if channelReply.ParentID != channelPost.ID {
		t.Fatalf("channel reply is not threaded on its question: %q", channelReply.ParentID)
	}
	// The direct conversation is never a public audience, so its answer
	// travels the private path; in the agent's own direct conversation the
	// durable copy is the only answer, with no second recipient-only
	// envelope in the same timeline.
	agentux025AssertGroundedReply(t, "direct copy", directCopy.Body)
	if directCopy.AuthorID != agentux025AgentHandle {
		t.Fatalf("direct copy is authored by %q, want the agent", directCopy.AuthorID)
	}
	if h.runs.count() != 2 {
		t.Fatalf("started %d runs for two mentions", h.runs.count())
	}
	if failures := h.failures.list(); len(failures) != 0 {
		t.Fatalf("post-commit failures: %v", failures)
	}
	seen := map[string]bool{}
	for _, postID := range []string{channelPost.ID, directPost.ID} {
		invocationID := agentux025InvocationForPost(t, h, postID)
		if seen[invocationID] {
			t.Fatal("both mentions shared one invocation")
		}
		seen[invocationID] = true
		run := h.runFor(invocationID)
		if run.State != runstate.StateCompleted {
			t.Fatalf("run %s state=%s", run.ID, run.State)
		}
		var phases []runstate.Phase
		for _, checkpoint := range run.Checkpoints {
			phases = append(phases, checkpoint.Phase)
		}
		hasDelivery := false
		for _, phase := range phases {
			if phase == runstate.PhaseDelivery {
				hasDelivery = true
			}
		}
		if !hasDelivery {
			t.Fatalf("run %s recorded no delivery checkpoint (phases=%v)", run.ID, phases)
		}
		if got := h.model.turnKinds(run.ID); len(got) != 2 || got[0] != "search" || got[1] != "answer" {
			t.Fatalf("run %s model turns=%v, want a search proposal then the answer", run.ID, got)
		}
	}
	if h.persister.calls != 2 {
		t.Fatalf("sealed %d final outputs for two runs", h.persister.calls)
	}
}

func agentux025AssertGroundedReply(t *testing.T, where, body string) {
	t.Helper()
	if !strings.Contains(body, "Leave policy") {
		t.Fatalf("%s reply is not grounded in the placed document: %q", where, body)
	}
	if strings.Contains(body, agentux025Announcement) {
		t.Fatalf("%s reply carries the model's announcement text: %q", where, body)
	}
}

func agentux025InvocationForPost(t *testing.T, h *agentux025Harness, postID string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		h.runs.mu.Lock()
		for id, request := range h.runs.requests {
			if request.InvokingPostID == postID {
				h.runs.mu.Unlock()
				return id
			}
		}
		h.runs.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("no invocation started for post %s", postID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// agentux025AssertRoleSeparation pins the two database roles the composed
// path runs under: the agent store's least-privilege app role and the
// harness's non-login chat app role. Neither is a superuser, neither owns
// its schema, and the chat role can neither create nor read outside the
// chat schema — while the whole path above succeeds under exactly these
// roles.
func agentux025AssertRoleSeparation(t *testing.T, h *agentux025Harness) {
	t.Helper()
	ctx := context.Background()
	for _, role := range []string{agentstore.AppRole, h.chatRole} {
		var login, superuser, createdb bool
		if err := h.agentDB.SQL.QueryRowContext(ctx, `SELECT rolcanlogin, rolsuper, rolcreatedb FROM pg_roles WHERE rolname=$1`, role).Scan(&login, &superuser, &createdb); err != nil {
			t.Fatalf("role %s: %v", role, err)
		}
		if login || superuser || createdb {
			t.Fatalf("role %s is not least-privilege: login=%t superuser=%t createdb=%t", role, login, superuser, createdb)
		}
	}
	conn := h.chatDB.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+h.chatRole); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE agentux025_must_fail(id integer)`); err == nil {
		t.Fatal("chat app role created a table")
	}
	var agentSchema string
	if err := h.agentDB.SQL.QueryRowContext(ctx, `SELECT $1`, h.agentDB.Schema).Scan(&agentSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `SELECT 1 FROM `+agentSchema+`.tenant LIMIT 1`); err == nil {
		t.Fatal("chat app role read the agent schema")
	}
}
