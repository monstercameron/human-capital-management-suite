package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
	"gopkg.in/yaml.v3"
)

type agentux023Record struct {
	SchemaVersion int    `yaml:"schema_version"`
	DecisionID    string `yaml:"decision_id"`
	Status        string `yaml:"status"`
	RunPolicy     struct {
		Role      string   `yaml:"role"`
		Grants    []string `yaml:"grants"`
		Denied    []string `yaml:"denied"`
		Migration string   `yaml:"migration"`
		Users     []string `yaml:"users"`
	} `yaml:"run_policy_authority"`
	Repair struct {
		Name      string   `yaml:"name"`
		Role      string   `yaml:"role"`
		Reads     []string `yaml:"reads"`
		Execute   []string `yaml:"execute"`
		Denied    []string `yaml:"denied"`
		Migration string   `yaml:"migration"`
		Steps     []string `yaml:"operator_steps"`
	} `yaml:"chat_repair_procedure"`
	Serving struct {
		AgentApp             string `yaml:"agent_app"`
		AgentAppPolicyAccess string `yaml:"agent_app_policy_access"`
	} `yaml:"serving_roles"`
}

func agentux023RecordPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "definitions", "planning", "agentux023-database-authority.yaml")
}

func agentux023LoadRecord(t *testing.T) (agentux023Record, []byte) {
	t.Helper()
	raw, err := os.ReadFile(agentux023RecordPath(t))
	if err != nil {
		t.Fatal(err)
	}
	var record agentux023Record
	if err := yaml.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	return record, raw
}

// TestTodo_AGENTUX_023 proves the database authority decision is recorded,
// granted, and used: the policy authority role writes run policy revisions,
// the repair role runs the chat repair procedure, and neither session acts
// as an owner.
func TestTodo_AGENTUX_023(t *testing.T) {
	record, _ := agentux023LoadRecord(t)
	if record.DecisionID != "AGENTUX-023" || record.Status != "DECIDED" {
		t.Fatalf("record=%+v", record)
	}
	if record.RunPolicy.Role != agentstore.PolicyAuthorityRole || agentstore.PolicyAuthorityRole != "hcmnext_agent_policy_authority" {
		t.Fatalf("policy role=%q const=%q", record.RunPolicy.Role, agentstore.PolicyAuthorityRole)
	}
	if record.Repair.Role != chatstore.RepairRole || chatstore.RepairRole != "hcmnext_chat_repair" {
		t.Fatalf("repair role=%q const=%q", record.Repair.Role, chatstore.RepairRole)
	}
	joined := strings.Join(record.RunPolicy.Grants, "|") + strings.Join(record.Repair.Reads, "|") + strings.Join(record.Repair.Execute, "|")
	for _, want := range []string{"persona_run_policy", "chat_conversation", "chat_repair_delete_conversation"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("grants omit %s: %+v", want, record)
		}
	}
	if len(record.Repair.Steps) == 0 || record.Serving.AgentApp != agentstore.AppRole {
		t.Fatalf("procedure or serving roles missing: %+v", record)
	}
}

// TestTodo_AGENTUX_023_Golden pins the authority record: any grant or role
// change is a deliberate edit to this digest.
func TestTodo_AGENTUX_023_Golden(t *testing.T) {
	_, raw := agentux023LoadRecord(t)
	sum := sha256.Sum256(raw)
	const want = "d4963e5527a2339402f2a695187d3d8c8496eb63de6df9ce641e992c60eb30b0"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("authority record digest = %s, want %s", got, want)
	}
}

func agentux023AgentDB(t *testing.T) (*pgtest.DB, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantUUID := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantUUID)
	return db, tenantUUID
}

// TestTodo_AGENTUX_023_Security proves the serving role still cannot write
// run policy and the repair role cannot write outside the repair tables.
func TestTodo_AGENTUX_023_Security(t *testing.T) {
	ctx := context.Background()
	db, tenantUUID := agentux023AgentDB(t)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO persona_run_policy(tenant_id,legal_entity_id,revision,effective_from,max_cost_micros,max_input_tokens,max_output_tokens,max_run_duration_ms) VALUES($1,'entity',1,now(),100,100,10,1000)`, tenantUUID); err == nil {
		t.Fatal("serving role inserted run policy")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("serving insert failed unexpectedly: %v", err)
	}
	var readable bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM persona_run_policy WHERE tenant_id=$1)`, tenantUUID).Scan(&readable); err != nil {
		t.Fatalf("serving role lost policy reads: %v", err)
	}

	chatDB, chatDSN := agentux023ChatDB(t)
	repair := agentux023ChatStore(t, chatDB, chatDSN, chatstore.RepairRole)
	repairCtx := context.Background()
	if err := repair.RunTx(repairCtx, func(tx dbport.Tx) error {
		_, err := tx.Exec(repairCtx, `INSERT INTO chat_conversation(tenant_id,id,kind) VALUES('tenant-a','repair-probe','direct')`)
		return err
	}); err == nil {
		t.Fatal("repair role inserted a conversation")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("repair insert failed unexpectedly: %v", err)
	}
	if err := repair.RunTx(repairCtx, func(tx dbport.Tx) error {
		_, err := tx.Exec(repairCtx, `DELETE FROM chat_post WHERE tenant_id='tenant-a'`)
		return err
	}); err == nil {
		t.Fatal("repair role deleted posts")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("repair post delete failed unexpectedly: %v", err)
	}
	if err := repair.RunTx(repairCtx, func(tx dbport.Tx) error {
		_, err := tx.Exec(repairCtx, `DELETE FROM chat_conversation WHERE tenant_id='tenant-a'`)
		return err
	}); err == nil {
		t.Fatal("repair role deleted a conversation directly")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("repair conversation delete failed unexpectedly: %v", err)
	}
	var executable bool
	probe := chatDB.NewConn(t)
	if _, err := probe.Exec(ctx, "SET ROLE "+chatstore.RepairRole); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow(ctx, `SELECT has_function_privilege('hcmnext_chat_repair', 'chat_repair_delete_conversation(text,text)', 'EXECUTE')`).Scan(&executable); err != nil || !executable {
		t.Fatalf("repair role cannot execute the removal function: %v", err)
	}
}

func agentux023ChatDB(t *testing.T) (*pgtest.DB, string) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	return db, u.String()
}

func agentux023ChatStore(t *testing.T, db *pgtest.DB, dsn, role string) *chatstore.Store {
	t.Helper()
	store, err := chatstore.New(context.Background(), chatstore.Config{DSN: dsn, Role: role})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

// TestTodo_AGENTUX_023_Integration runs the preparation's privileged writes
// under the separated roles a deployed cell has: the authority role inserts
// run policy, the repair role repairs, and a revoked grant fails the same
// call.
func TestTodo_AGENTUX_023_Integration(t *testing.T) {
	ctx := context.Background()
	agentDB, tenantUUID := agentux023AgentDB(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	created, err := ensureLocalAgentDemoRunPolicy(ctx, agentux023AgentDSN(t, agentDB), tenantUUID, "entity-a", now)
	if err != nil || !created {
		t.Fatalf("authority policy insert=%t err=%v", created, err)
	}
	again, err := ensureLocalAgentDemoRunPolicy(ctx, agentux023AgentDSN(t, agentDB), tenantUUID, "entity-a", now)
	if err != nil || again {
		t.Fatalf("idempotent policy insert=%t err=%v", again, err)
	}
	agentDB.Exec(t, `REVOKE INSERT ON persona_run_policy FROM hcmnext_agent_policy_authority`)
	revokedTenant := uuid.New()
	agentDB.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, revokedTenant)
	if _, err := ensureLocalAgentDemoRunPolicy(ctx, agentux023AgentDSN(t, agentDB), revokedTenant, "entity-a", now.Add(time.Hour)); err == nil {
		t.Fatal("missing INSERT grant did not fail the policy write")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("revoked policy write failed unexpectedly: %v", err)
	}

	chatDB, chatDSN := agentux023ChatDB(t)
	agentux023AssertRepairGrantCoverage(t, chatDB)
	owner := agentux023ChatStore(t, chatDB, chatDSN, "")
	adapter := chatstore.NewAdapter(owner)
	conversation := chat.Conversation{ID: "policy-dm", TenantID: "tenant-a", Kind: chat.Direct, OwnerID: "admin"}
	members := []chat.Membership{
		{ConversationID: conversation.ID, TenantID: conversation.TenantID, HomeTenantID: conversation.TenantID, SubjectID: "admin", Role: chat.Manager, HistoryVisibility: chat.FullHistory},
		{ConversationID: conversation.ID, TenantID: conversation.TenantID, HomeTenantID: conversation.TenantID, SubjectID: "policy-helper", Role: chat.Member, HistoryVisibility: chat.FullHistory},
	}
	if _, err := adapter.CreateConversation(ctx, conversation, members, "broken-policy-dm"); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PutPersonaChannelPolicy(ctx, conversation.TenantID, conversation.ID, 0, chatstore.PersonaChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC"}, AllowedChannelClasses: []string{"ONE_TO_ONE"}, PlacementClass: "ONE_TO_ONE_DM", AlwaysPrivate: true}); err != nil {
		t.Fatal(err)
	}
	repair := agentux023ChatStore(t, chatDB, chatDSN, chatstore.RepairRole)
	repaired, err := repair.RepairUnroutedDirectConversation(ctx, conversation.TenantID, conversation.ID, conversation.OwnerID, "policy-helper")
	if err != nil || !repaired {
		t.Fatalf("repair as repair role=%t err=%v", repaired, err)
	}
	if _, err := adapter.GetConversation(ctx, conversation.TenantID, conversation.ID); err == nil {
		t.Fatal("malformed conversation remains after repair")
	}
}

// agentux023AssertRepairGrantCoverage fails when the removal function is
// not a SECURITY DEFINER executable by the repair role, or when the role
// can write any table directly.
func agentux023AssertRepairGrantCoverage(t *testing.T, db *pgtest.DB) {
	t.Helper()
	conn := db.NewConn(t)
	var secdef bool
	if err := conn.QueryRow(context.Background(), `SELECT prosecdef FROM pg_proc WHERE proname='chat_repair_delete_conversation'`).Scan(&secdef); err != nil || !secdef {
		t.Fatalf("removal function is not SECURITY DEFINER: %v", err)
	}
	for _, table := range []string{"chat_conversation", "chat_persona_channel_policy", "chat_post", "chat_membership"} {
		for _, priv := range []string{"INSERT", "UPDATE", "DELETE"} {
			var held bool
			if err := conn.QueryRow(context.Background(), `SELECT has_table_privilege($1, $2, $3)`, chatstore.RepairRole, table, priv).Scan(&held); err != nil {
				t.Fatal(err)
			}
			if table == "chat_conversation" && priv == "UPDATE" {
				continue
			}
			if held {
				t.Fatalf("repair role holds %s on %s", priv, table)
			}
		}
	}
}

func agentux023AgentDSN(t *testing.T, db *pgtest.DB) string {
	t.Helper()
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// TestTodo_AGENTUX_023_Fault proves a privilege failure surfaces instead of
// silently skipping the policy write.
func TestTodo_AGENTUX_023_Fault(t *testing.T) {
	ctx := context.Background()
	db, tenantUUID := agentux023AgentDB(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	db.Exec(t, `REVOKE INSERT ON persona_run_policy FROM hcmnext_agent_policy_authority`)
	if _, err := ensureLocalAgentDemoRunPolicy(ctx, agentux023AgentDSN(t, db), tenantUUID, "entity-a", now); err == nil {
		t.Fatal("revoked authority insert silently succeeded")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("revoked authority insert failed unexpectedly: %v", err)
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM persona_run_policy`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed write left %d rows", count)
	}
}

// TestTodo_AGENTUX_023_Recovery proves the repair is idempotent under the
// repair role: a second run reports no work and a routed conversation is
// never repair material.
func TestTodo_AGENTUX_023_Recovery(t *testing.T) {
	ctx := context.Background()
	chatDB, chatDSN := agentux023ChatDB(t)
	owner := agentux023ChatStore(t, chatDB, chatDSN, "")
	adapter := chatstore.NewAdapter(owner)
	broken := chat.Conversation{ID: "broken-dm", TenantID: "tenant-a", Kind: chat.Direct, OwnerID: "admin"}
	members := []chat.Membership{
		{ConversationID: broken.ID, TenantID: broken.TenantID, HomeTenantID: broken.TenantID, SubjectID: "admin", Role: chat.Manager, HistoryVisibility: chat.FullHistory},
		{ConversationID: broken.ID, TenantID: broken.TenantID, HomeTenantID: broken.TenantID, SubjectID: "policy-helper", Role: chat.Member, HistoryVisibility: chat.FullHistory},
	}
	if _, err := adapter.CreateConversation(ctx, broken, members, "broken-policy-dm"); err != nil {
		t.Fatal(err)
	}
	routed := chat.Conversation{ID: "routed", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "routed", OwnerID: "admin", Revision: 1}
	if _, err := adapter.CreateConversation(ctx, routed, []chat.Membership{{ConversationID: routed.ID, TenantID: routed.TenantID, HomeTenantID: routed.TenantID, SubjectID: "admin", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	repair := agentux023ChatStore(t, chatDB, chatDSN, chatstore.RepairRole)
	if repaired, err := repair.RepairUnroutedDirectConversation(ctx, broken.TenantID, broken.ID, broken.OwnerID, "policy-helper"); err != nil || !repaired {
		t.Fatalf("first repair=%t err=%v", repaired, err)
	}
	if repaired, err := repair.RepairUnroutedDirectConversation(ctx, broken.TenantID, broken.ID, broken.OwnerID, "policy-helper"); err != nil || repaired {
		t.Fatalf("second repair=%t err=%v", repaired, err)
	}
	if repaired, err := repair.RepairUnroutedDirectConversation(ctx, routed.TenantID, routed.ID, routed.OwnerID, "policy-helper"); err != nil || repaired {
		t.Fatalf("routed conversation repaired=%t err=%v", repaired, err)
	}
	if _, err := adapter.GetConversation(ctx, routed.TenantID, routed.ID); err != nil {
		t.Fatalf("routed conversation damaged: %v", err)
	}
}
