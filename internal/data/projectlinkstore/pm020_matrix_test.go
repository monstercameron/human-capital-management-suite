package projectlinkstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
)

// TestTodo_PM_020_Integration proves the link store against the real project
// store it joins: journey links round-trip through List, the reverse lookup
// returns the live unarchived tasks linking one target across tasks, removal
// drops a task from that lookup, and archiving hides the task without
// deleting its links.
func TestTodo_PM_020_Integration(t *testing.T) {
	links, projects, _ := linkFixture(t)
	ctx := context.Background()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		project := projectstore.ProjectRecord{ID: tenant + "-project", TenantID: tenant, OwnerID: "owner", Name: "Project", Timezone: "UTC"}
		if err := projects.CreateProject(ctx, project, "owner", "HUMAN", tenant+"-create-project"); err != nil {
			t.Fatal(err)
		}
		for _, taskID := range []string{"task-1", "task-2"} {
			task := projectstore.TaskRecord{ID: tenant + "-" + taskID, TenantID: tenant, ProjectID: project.ID, Title: "Task " + taskID, StatusID: "todo"}
			if err := projects.CreateTask(ctx, task, "owner", "HUMAN", tenant+"-create-"+taskID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := projects.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO project_workflow_version(tenant_id,project_id,version,source_revision,config_json,digest,publisher_id) VALUES('tenant-a','tenant-a-project',1,1,'{}'::jsonb,repeat('a',64),'owner')`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_workflow_current(tenant_id,project_id,version) VALUES('tenant-a','tenant-a-project',1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	add := func(id, taskID string, ref projectlink.Reference) {
		t.Helper()
		if err := links.Add(ctx, LinkRecord{ID: id, TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: taskID, Reference: ref}); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	add("journey-link-1", "tenant-a-task-1", projectlink.Reference{Kind: projectlink.Journey, ID: "journey-1"})
	add("journey-link-2", "tenant-a-task-2", projectlink.Reference{Kind: projectlink.Journey, ID: "journey-1"})
	add("work-link-1", "tenant-a-task-1", projectlink.Reference{Kind: projectlink.WorkItem, ID: "work-1"})

	// The journey kind round-trips through List like the kinds the primary
	// leg covers.
	page, err := links.List(ctx, "tenant-a", "tenant-a-project", "tenant-a-task-1", "", 10)
	if err != nil || len(page) != 2 {
		t.Fatalf("task link page=%#v err=%v", page, err)
	}
	kinds := map[projectlink.Kind]string{}
	for _, link := range page {
		kinds[link.Reference.Kind] = link.Reference.ID
	}
	if kinds[projectlink.Journey] != "journey-1" || kinds[projectlink.WorkItem] != "work-1" {
		t.Fatalf("task links=%v, want the journey and the work item", kinds)
	}

	// The reverse lookup fans out across the tasks linking one target.
	journeyTasks, err := links.ListByTarget(ctx, "tenant-a", projectlink.Journey, "journey-1", 10)
	if err != nil || len(journeyTasks) != 2 || journeyTasks[0].TaskID != "tenant-a-task-1" || journeyTasks[1].TaskID != "tenant-a-task-2" {
		t.Fatalf("journey reverse links=%+v err=%v", journeyTasks, err)
	}
	if journeyTasks[0].ProjectID != "tenant-a-project" || journeyTasks[0].Title != "Task task-1" || journeyTasks[0].StatusID != "todo" {
		t.Fatalf("reverse link lost its task row: %+v", journeyTasks[0])
	}
	workTasks, err := links.ListByTarget(ctx, "tenant-a", projectlink.WorkItem, "work-1", 10)
	if err != nil || len(workTasks) != 1 || workTasks[0].TaskID != "tenant-a-task-1" {
		t.Fatalf("work-item reverse links=%+v err=%v", workTasks, err)
	}
	foreign, err := links.ListByTarget(ctx, "tenant-b", projectlink.Journey, "journey-1", 10)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("cross-tenant reverse links=%+v err=%v", foreign, err)
	}

	// Removal drops the task from the reverse lookup.
	task2, err := projects.GetTask(ctx, "tenant-a", "tenant-a-project", "tenant-a-task-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := links.Remove(ctx, "tenant-a", "tenant-a-project", "tenant-a-task-2", "journey-link-2", "owner", "integration-remove", task2.Revision); err != nil {
		t.Fatalf("remove: %v", err)
	}
	journeyTasks, err = links.ListByTarget(ctx, "tenant-a", projectlink.Journey, "journey-1", 10)
	if err != nil || len(journeyTasks) != 1 || journeyTasks[0].TaskID != "tenant-a-task-1" {
		t.Fatalf("journey reverse links after remove=%+v err=%v", journeyTasks, err)
	}

	// Archiving hides the task from the reverse lookup without deleting the
	// links themselves.
	task1, err := projects.GetTask(ctx, "tenant-a", "tenant-a-project", "tenant-a-task-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.SetTaskArchived(ctx, "tenant-a", "tenant-a-project", "tenant-a-task-1", task1.Revision, 1, true, "owner", "HUMAN", "integration-archive"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	for _, tc := range []struct {
		kind   projectlink.Kind
		target string
	}{
		{projectlink.Journey, "journey-1"},
		{projectlink.WorkItem, "work-1"},
	} {
		got, err := links.ListByTarget(ctx, "tenant-a", tc.kind, tc.target, 10)
		if err != nil || len(got) != 0 {
			t.Fatalf("archived task still reverse-links %s: %+v err=%v", tc.target, got, err)
		}
	}
	kept, err := links.List(ctx, "tenant-a", "tenant-a-project", "tenant-a-task-1", "", 10)
	if err != nil || len(kept) != 2 {
		t.Fatalf("archiving deleted the task links: %#v err=%v", kept, err)
	}
}

// TestTodo_PM_020_Security is the adversarial leg: references outside the
// allowlist, malformed references, hostile identifiers, cross-project tasks,
// unsupported reverse kinds and nil receivers are all rejected and persist
// nothing, and row security denies reads and writes without a tenant.
func TestTodo_PM_020_Security(t *testing.T) {
	links, projects, schema := linkFixture(t)
	ctx := context.Background()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		project := projectstore.ProjectRecord{ID: tenant + "-project", TenantID: tenant, OwnerID: "owner", Name: "Project", Timezone: "UTC"}
		if err := projects.CreateProject(ctx, project, "owner", "HUMAN", tenant+"-create-project"); err != nil {
			t.Fatal(err)
		}
		task := projectstore.TaskRecord{ID: tenant + "-task", TenantID: tenant, ProjectID: project.ID, Title: "Task", StatusID: "todo"}
		if err := projects.CreateTask(ctx, task, "owner", "HUMAN", tenant+"-create-task"); err != nil {
			t.Fatal(err)
		}
	}
	good := projectlink.Reference{Kind: projectlink.WorkItem, ID: "work-1"}
	badRefs := []projectlink.Reference{
		{Kind: "PRIVATE_CHANNEL", ID: "secret"},
		{Kind: "DRAFT_DOCUMENT", ID: "draft-1", Version: "v1", ScopeID: "team-1"},
		{Kind: "HCM_EVIDENCE", ID: "evidence-1"},
		{Kind: "", ID: "nothing"},
		{Kind: "chat_post", ID: "post-1", ConversationID: "conversation-1"},
		{Kind: "WORK_ITEM ", ID: "work-1"},
		{Kind: projectlink.WorkItem, ID: "work-1", Version: "v1"},
		{Kind: projectlink.WorkItem, ID: "work-1", ScopeID: "team-1"},
		{Kind: projectlink.ChatPost, ID: "post-1"},
		{Kind: projectlink.ChatPost, ID: "post-1", ConversationID: "conversation-1", Version: "v1"},
		{Kind: projectlink.ChatConversation, ID: "conversation-1", ScopeID: "team-1"},
		{Kind: projectlink.DeployedDocument, ID: "document-1", Version: "v1"},
		{Kind: projectlink.DeployedDocument, ID: "document-1", ScopeID: "team-1"},
		{Kind: projectlink.DeployedDocument, ID: "document-1", Version: "v1", ScopeID: "team-1", ConversationID: "conversation-1"},
		{Kind: projectlink.Journey, ID: "journey-1", Version: "v1"},
		{Kind: projectlink.WorkOrder, ID: "order-1", ScopeID: "team-1"},
		{Kind: projectlink.WorkItem, ID: "x' OR '1'='1"},
		{Kind: projectlink.WorkItem, ID: "a\nb"},
		{Kind: projectlink.WorkItem, ID: " leading"},
		{Kind: projectlink.WorkItem, ID: "trailing "},
		{Kind: projectlink.WorkItem, ID: strings.Repeat("x", 129)},
		{Kind: projectlink.WorkItem, ID: ""},
		{Kind: projectlink.WorkItem, ID: "a/b"},
		{Kind: projectlink.WorkItem, ID: "a;b"},
		{Kind: projectlink.WorkItem, ID: "a b"},
		{Kind: projectlink.WorkItem, ID: "."},
		{Kind: projectlink.WorkItem, ID: "ok", ConversationID: " conversation-1"},
	}
	for i, ref := range badRefs {
		link := LinkRecord{ID: fmt.Sprintf("bad-ref-%d", i), TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: ref}
		if err := links.Add(ctx, link); !errors.Is(err, ErrInvalidRecord) {
			t.Errorf("add %#v err=%v, want ErrInvalidRecord", ref, err)
		}
	}
	badEnvelopes := []LinkRecord{
		{ID: "", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: good},
		{ID: " leading", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: good},
		{ID: "trailing ", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: good},
		{ID: strings.Repeat("x", 129), TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: good},
		{ID: "bad-tenant", TenantID: "", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: good},
		{ID: "bad-tenant-space", TenantID: " tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: good},
		{ID: "bad-project", TenantID: "tenant-a", ProjectID: "", TaskID: "tenant-a-task", Reference: good},
		{ID: "bad-task", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "", Reference: good},
	}
	for _, link := range badEnvelopes {
		if err := links.Add(ctx, link); !errors.Is(err, ErrInvalidRecord) {
			t.Errorf("add envelope %#v err=%v, want ErrInvalidRecord", link, err)
		}
	}
	if page, err := links.List(ctx, "tenant-a", "tenant-a-project", "tenant-a-task", "", 100); err != nil || len(page) != 0 {
		t.Fatalf("rejected links persisted: %#v err=%v", page, err)
	}
	if err := links.Add(ctx, LinkRecord{ID: "cross-project", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-b-task", Reference: good}); err == nil {
		t.Fatal("cross-project task link accepted")
	}
	if err := links.Add(ctx, LinkRecord{ID: "missing-task", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "no-such-task", Reference: good}); err == nil {
		t.Fatal("link to a missing task accepted")
	}

	for _, kind := range []projectlink.Kind{projectlink.ChatPost, projectlink.ChatConversation, projectlink.DeployedDocument, "", "PRIVATE_CHANNEL"} {
		if _, err := links.ListByTarget(ctx, "tenant-a", kind, "target-1", 10); !errors.Is(err, ErrInvalidRecord) {
			t.Errorf("reverse lookup of %q err=%v, want ErrInvalidRecord", kind, err)
		}
	}
	for _, limit := range []int32{0, 101} {
		if _, err := links.ListByTarget(ctx, "tenant-a", projectlink.Journey, "journey-1", limit); !errors.Is(err, ErrInvalidRecord) {
			t.Errorf("reverse lookup limit %d err=%v, want ErrInvalidRecord", limit, err)
		}
	}
	if _, err := links.List(ctx, "", "tenant-a-project", "tenant-a-task", "", 10); !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("unscoped list err=%v, want ErrInvalidRecord", err)
	}
	if _, err := links.List(ctx, "tenant-a", "tenant-a-project", "tenant-a-task", "", 0); !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("unbounded list err=%v, want ErrInvalidRecord", err)
	}

	var nilStore *Store
	if err := nilStore.Add(ctx, LinkRecord{ID: "x", TenantID: "t", ProjectID: "p", TaskID: "k", Reference: good}); !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("nil store add err=%v, want ErrInvalidRecord", err)
	}
	if _, err := nilStore.List(ctx, "t", "p", "k", "", 10); !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("nil store list err=%v, want ErrInvalidRecord", err)
	}
	if _, err := nilStore.ListByTarget(ctx, "t", projectlink.Journey, "j", 10); !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("nil store reverse lookup err=%v, want ErrInvalidRecord", err)
	}
	if _, err := nilStore.AddRevisioned(ctx, LinkRecord{ID: "x", TenantID: "t", ProjectID: "p", TaskID: "k", Reference: good}, "owner", "key", 1); !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("nil store revisioned add err=%v, want ErrInvalidRecord", err)
	}
	if _, err := nilStore.Remove(ctx, "t", "p", "k", "x", "owner", "key", 1); !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("nil store remove err=%v, want ErrInvalidRecord", err)
	}
	if err := AddTx(ctx, nil, LinkRecord{ID: "x", TenantID: "t", ProjectID: "p", TaskID: "k", Reference: good}); !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("nil transaction add err=%v, want ErrInvalidRecord", err)
	}

	// Row security denies by default: without a tenant setting a restricted
	// role sees no links and writes none.
	if err := links.Add(ctx, LinkRecord{ID: "visible-link", TenantID: "tenant-a", ProjectID: "tenant-a-project", TaskID: "tenant-a-task", Reference: good}); err != nil {
		t.Fatal(err)
	}
	role := "project_link_sec_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	qrole, qschema := `"`+role+`"`, `"`+schema+`"`
	if err := projects.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE ROLE `+qrole+` NOLOGIN NOSUPERUSER NOBYPASSRLS`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `GRANT USAGE ON SCHEMA `+qschema+` TO `+qrole); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `GRANT SELECT,INSERT ON ALL TABLES IN SCHEMA `+qschema+` TO `+qrole)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := projects.RunTx(context.Background(), func(tx dbport.Tx) error {
			if _, err := tx.Exec(context.Background(), `DROP OWNED BY `+qrole); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), `DROP ROLE `+qrole)
			return err
		}); err != nil {
			t.Errorf("drop RLS role: %v", err)
		}
	})
	visible := func(tenant string) (int, error) {
		var count int
		err := projects.RunTx(ctx, func(tx dbport.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+qrole); err != nil {
				return err
			}
			if tenant != "" {
				if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true)`, tenant); err != nil {
					return err
				}
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM project_task_link`).Scan(&count)
		})
		return count, err
	}
	if count, err := visible(""); err != nil || count != 0 {
		t.Fatalf("role without a tenant sees %d links err=%v, want default deny", count, err)
	}
	if err := projects.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+qrole); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_task_link(tenant_id,id,project_id,task_id,kind,target_id) VALUES('tenant-a','rls-ambient','tenant-a-project','tenant-a-task','WORK_ITEM','work-1')`)
		return err
	}); err == nil {
		t.Fatal("write without a tenant passed row security")
	}
	if count, err := visible("tenant-a"); err != nil || count != 1 {
		t.Fatalf("tenant-a sees %d links err=%v, want its one link", count, err)
	}
	if count, err := visible("tenant-b"); err != nil || count != 0 {
		t.Fatalf("tenant-b sees %d links err=%v, want none", count, err)
	}
}
