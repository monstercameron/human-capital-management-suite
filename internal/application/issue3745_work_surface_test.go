package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

type issue3745Projects struct {
	projects map[string]projectservice.ProjectRecord
	tasks    map[string]projectservice.TaskRecord
	members  map[string]map[string]projectaccess.MembershipState
	rev      map[string]uint64
}

func newIssue3745Projects() *issue3745Projects {
	return &issue3745Projects{projects: map[string]projectservice.ProjectRecord{}, tasks: map[string]projectservice.TaskRecord{}, members: map[string]map[string]projectaccess.MembershipState{}, rev: map[string]uint64{}}
}

var errIssue3745Missing = errors.New("not found")

func (f *issue3745Projects) CreateProject(_ context.Context, p projectservice.ProjectRecord, actor, _ string) (projectservice.ProjectRecord, error) {
	f.projects[p.ID] = p
	f.members[p.ID] = map[string]projectaccess.MembershipState{actor: projectaccess.MembershipActive}
	f.rev[p.ID] = 1
	return p, nil
}
func (f *issue3745Projects) GetProject(_ context.Context, _, id string) (projectservice.ProjectRecord, error) {
	if p, ok := f.projects[id]; ok {
		return p, nil
	}
	return projectservice.ProjectRecord{}, errIssue3745Missing
}
func (f *issue3745Projects) GetPublished(_ context.Context, _, _ string) (projectservice.WorkflowConfiguration, error) {
	return projectservice.WorkflowConfiguration{Version: 1, Config: projectworkflow.Config{TaskTypes: []projectworkflow.TaskType{{ID: "task_default", InitialStatus: "todo"}}}}, nil
}
func (f *issue3745Projects) CreateTask(_ context.Context, t projectservice.TaskRecord, _ uint64, _, _ string) (projectservice.TaskRecord, error) {
	f.tasks[t.ProjectID+"/"+t.ID] = t
	return t, nil
}
func (f *issue3745Projects) GetTask(_ context.Context, _, projectID, id string) (projectservice.TaskRecord, error) {
	if t, ok := f.tasks[projectID+"/"+id]; ok {
		return t, nil
	}
	return projectservice.TaskRecord{}, errIssue3745Missing
}
func (f *issue3745Projects) GetMemberships(_ context.Context, _, projectID string) (projectservice.MembershipSnapshot, error) {
	out := projectservice.MembershipSnapshot{Revision: f.rev[projectID]}
	for user, state := range f.members[projectID] {
		out.Members = append(out.Members, projectservice.MembershipRecord{UserID: user, State: state})
	}
	return out, nil
}
func (f *issue3745Projects) InviteMember(_ context.Context, _, projectID, _, user string, _ projectaccess.Role, _ uint8, expected uint64, _ string) error {
	if expected != f.rev[projectID] {
		return errors.New("stale revision")
	}
	f.members[projectID][user] = projectaccess.MembershipInvited
	f.rev[projectID]++
	return nil
}
func (f *issue3745Projects) AcceptMemberInvitation(_ context.Context, _, projectID, user string, expected uint64, _ string) error {
	if expected != f.rev[projectID] || f.members[projectID][user] != projectaccess.MembershipInvited {
		return errors.New("cannot accept")
	}
	f.members[projectID][user] = projectaccess.MembershipActive
	f.rev[projectID]++
	return nil
}

type issue3745Docs struct {
	owner    map[string]string
	title    map[string]string
	deployed map[string]bool
	readers  map[string]map[string]bool
	seq      int
}

func newIssue3745Docs() *issue3745Docs {
	return &issue3745Docs{owner: map[string]string{}, title: map[string]string{}, deployed: map[string]bool{}, readers: map[string]map[string]bool{}}
}

func (f *issue3745Docs) ListPersonalDocuments(_ context.Context, _, actor string, _ int) ([]documenthubstore.DocumentSummary, error) {
	var out []documenthubstore.DocumentSummary
	for id, owner := range f.owner {
		if owner == actor {
			out = append(out, documenthubstore.DocumentSummary{ID: id, Title: f.title[id], OwnerID: owner, VersionID: "v-" + id})
		}
	}
	return out, nil
}
func (f *issue3745Docs) CreateDocument(_ context.Context, _, owner, _ string) (string, error) {
	f.seq++
	id := fmt.Sprintf("doc-%d", f.seq)
	f.owner[id] = owner
	f.readers[id] = map[string]bool{owner: true}
	return id, nil
}
func (f *issue3745Docs) SubmitCandidate(_ context.Context, _ string, v documenthubstore.Version, _ string) (documenthubstore.Version, error) {
	f.title[v.DocumentID] = v.Title
	v.ID = "v-" + v.DocumentID
	return v, nil
}
func (f *issue3745Docs) ResolveDeployment(_ context.Context, _, id, _, _ string) (documenthubstore.Deployment, error) {
	if f.deployed[id] {
		return documenthubstore.Deployment{DocumentID: id}, nil
	}
	return documenthubstore.Deployment{}, documenthubstore.ErrNoDeployment
}
func (f *issue3745Docs) RecordReview(_ context.Context, _ string, in documenthubstore.ReviewInput) (documenthubstore.Review, error) {
	if in.ReviewerID == f.owner[in.DocumentID] {
		return documenthubstore.Review{}, errors.New("author cannot review")
	}
	return documenthubstore.Review{}, nil
}
func (f *issue3745Docs) Deploy(_ context.Context, _ string, in documenthubstore.DeployInput) (documenthubstore.Deployment, error) {
	f.deployed[in.DocumentID] = true
	return documenthubstore.Deployment{DocumentID: in.DocumentID}, nil
}
func (f *issue3745Docs) ShareDocument(_ context.Context, _, id, _ string, in documenthubstore.GrantInput) (documenthubstore.Grant, error) {
	f.readers[id][in.SubjectID] = true
	return documenthubstore.Grant{}, nil
}
func (f *issue3745Docs) Authorize(_ context.Context, _, id, _, subject, _ string) error {
	if f.readers[id][subject] {
		return nil
	}
	return errors.New("denied")
}

func issue3745People(n int) []WorkSurfacePerson {
	var out []WorkSurfacePerson
	for i := 0; i < n; i++ {
		out = append(out, WorkSurfacePerson{Key: fmt.Sprintf("hc-%03d-person", i+1), Name: fmt.Sprintf("Person %d", i+1)})
	}
	return append(out, WorkSurfacePerson{Key: harborcareDemoAdmin, Name: "Admin"})
}

func issue3745Deps(p *issue3745Projects, d *issue3745Docs, people []WorkSurfacePerson) WorkSurfaceDeps {
	return WorkSurfaceDeps{Projects: p, Documents: d, Tenant: harborcareDemoTenant, Admin: harborcareDemoAdmin, People: people}
}

func issue3745Item(r WorkSurfaceReceipt, kind, titlePart string) WorkSurfaceItem {
	for _, item := range r.Items {
		if item.Kind == kind && strings.Contains(item.Title, titlePart) {
			return item
		}
	}
	return WorkSurfaceItem{}
}

// TestIssue3745_PreparesOpenAndRestrictedWork: a run files an open and a
// restricted project and document, and the restricted ones reach only the
// administrator and the reviewer.
func TestIssue3745_PreparesOpenAndRestrictedWork(t *testing.T) {
	projects, docs := newIssue3745Projects(), newIssue3745Docs()
	receipt, err := ensureWorkSurface(context.Background(), issue3745Deps(projects, docs, issue3745People(6)), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Changed() || receipt.Reviewer.Key != "hc-001-person" || len(receipt.Colleagues) != 5 {
		t.Fatalf("receipt = %+v, want a reviewer and five colleagues", receipt)
	}
	restrictedMembers := projects.members[WorkSurfaceRestrictedProjectID]
	if len(restrictedMembers) != 2 || restrictedMembers[harborcareDemoAdmin] != projectaccess.MembershipActive || restrictedMembers[receipt.Reviewer.Key] != projectaccess.MembershipActive {
		t.Fatalf("restricted project members = %v, want only the administrator and the reviewer, active", restrictedMembers)
	}
	for _, c := range receipt.Colleagues {
		if _, in := restrictedMembers[c.Key]; in {
			t.Fatalf("colleague %s is a member of the restricted project", c.Key)
		}
		if projects.members[WorkSurfaceOpenProjectID][c.Key] != projectaccess.MembershipActive {
			t.Fatalf("colleague %s is not an active member of the open project", c.Key)
		}
	}
	if len(projects.tasks) != len(workSurfaceOpenTickets)+len(workSurfaceRestrictedTickets) {
		t.Fatalf("tickets filed = %d", len(projects.tasks))
	}
	restrictedDoc := issue3745Item(receipt, "document", "restricted")
	openDoc := issue3745Item(receipt, "document", "Office move")
	if !restrictedDoc.Restricted || openDoc.Restricted || restrictedDoc.Link == "" || restrictedDoc.Link == openDoc.Link {
		t.Fatalf("document items = %+v / %+v", restrictedDoc, openDoc)
	}
	for id, title := range docs.title {
		if !docs.deployed[id] {
			t.Fatalf("%q is not published, so recipients could not open it", title)
		}
		if title == workSurfaceRestrictedDocTitle {
			if len(docs.readers[id]) != 2 || !docs.readers[id][receipt.Reviewer.Key] {
				t.Fatalf("restricted document readers = %v, want the administrator and the reviewer", docs.readers[id])
			}
		}
		if title == workSurfaceOpenDocumentTitle && !docs.readers[id][receipt.Colleagues[0].Key] {
			t.Fatalf("open document is not shared with a colleague")
		}
	}
	if ticket := issue3745Item(receipt, "ticket", "pay band"); !ticket.Restricted || !strings.Contains(ticket.Link, "project="+WorkSurfaceRestrictedProjectID+"&task=cr-1") {
		t.Fatalf("restricted ticket item = %+v", ticket)
	}
}

// TestIssue3745_SecondRunChangesNothing: preparation is idempotent, and a run
// that stopped after creating a document still publishes it.
func TestIssue3745_SecondRunChangesNothing(t *testing.T) {
	projects, docs := newIssue3745Projects(), newIssue3745Docs()
	deps := issue3745Deps(projects, docs, issue3745People(4))
	if _, err := ensureWorkSurface(context.Background(), deps, time.Now()); err != nil {
		t.Fatal(err)
	}
	again, err := ensureWorkSurface(context.Background(), deps, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed() || len(docs.owner) != 2 || len(projects.projects) != 2 {
		t.Fatalf("second run = %+v, docs=%d projects=%d, want nothing added", again, len(docs.owner), len(projects.projects))
	}
	for id := range docs.deployed {
		delete(docs.deployed, id)
	}
	repaired, err := ensureWorkSurface(context.Background(), deps, time.Now())
	if err != nil || !repaired.Changed() {
		t.Fatalf("repair run = %+v, %v, want the unpublished documents published", repaired, err)
	}
	for id := range docs.owner {
		if !docs.deployed[id] {
			t.Fatalf("document %s still unpublished", id)
		}
	}
}

// TestIssue3745_NeedsPeopleAndAllowlistedTenant_Security: with too few people
// to tell the restricted work apart, or for another tenant or administrator,
// nothing is written.
func TestIssue3745_NeedsPeopleAndAllowlistedTenant_Security(t *testing.T) {
	projects, docs := newIssue3745Projects(), newIssue3745Docs()
	if _, err := ensureWorkSurface(context.Background(), issue3745Deps(projects, docs, issue3745People(1)), time.Now()); err == nil {
		t.Fatal("one other person accepted; nobody would be left outside the restricted work")
	}
	for _, mutate := range []func(*WorkSurfaceDeps){
		func(d *WorkSurfaceDeps) { d.Tenant = "acme-prod" },
		func(d *WorkSurfaceDeps) { d.Admin = "someone-else" },
	} {
		deps := issue3745Deps(projects, docs, issue3745People(5))
		mutate(&deps)
		if _, err := ensureWorkSurface(context.Background(), deps, time.Now()); err == nil {
			t.Fatalf("deps %+v accepted", deps)
		}
	}
	if len(projects.projects)+len(docs.owner) != 0 {
		t.Fatal("a refused run wrote work")
	}
}

// TestIssue3745_ConfigRefusesNonLocalRuns_Security: the run is refused outside
// local-dev, outside the demo workspaces, off loopback and without both stores.
func TestIssue3745_ConfigRefusesNonLocalRuns_Security(t *testing.T) {
	good := LocalWorkSurfaceConfig{Profile: ServeProfileLocalDev, Tenant: harborcareDemoTenant, DatabaseURL: "postgres://u@127.0.0.1:5432/core", DocumentDatabaseURL: "postgres://u@localhost:5432/doc", ProjectDatabaseURL: "postgres://u@[::1]:5432/proj"}
	if err := ValidateLocalWorkSurfaceConfig(good); err != nil {
		t.Fatalf("good config refused: %v", err)
	}
	for name, mutate := range map[string]func(*LocalWorkSurfaceConfig){
		"profile":         func(c *LocalWorkSurfaceConfig) { c.Profile = "production" },
		"tenant":          func(c *LocalWorkSurfaceConfig) { c.Tenant = "real-customer" },
		"remote document": func(c *LocalWorkSurfaceConfig) { c.DocumentDatabaseURL = "postgres://u@db.example.com:5432/doc" },
		"remote project":  func(c *LocalWorkSurfaceConfig) { c.ProjectDatabaseURL = "postgres://u@10.0.0.5:5432/proj" },
		"no project":      func(c *LocalWorkSurfaceConfig) { c.ProjectDatabaseURL = "" },
		"no document":     func(c *LocalWorkSurfaceConfig) { c.DocumentDatabaseURL = "" },
		"remote chat":     func(c *LocalWorkSurfaceConfig) { c.ChatDatabaseURL = "postgres://u@chat.example.com/chat" },
	} {
		cfg := good
		mutate(&cfg)
		if err := ValidateLocalWorkSurfaceConfig(cfg); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

// TestIssue3745_GuideNamesFlagsLinksAndWho: the printed guide is the local
// runbook: server flags, one address per item, and who sees what.
func TestIssue3745_GuideNamesFlagsLinksAndWho(t *testing.T) {
	projects, docs := newIssue3745Projects(), newIssue3745Docs()
	receipt, err := ensureWorkSurface(context.Background(), issue3745Deps(projects, docs, issue3745People(4)), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	guide := LocalWorkSurfaceGuide(receipt)
	for _, want := range []string{"-document-database-url", "-project-database-url", "-migrate", "only the administrator and the reviewer", receipt.Reviewer.Name, receipt.Admin, "withheld"} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide lacks %q:\n%s", want, guide)
		}
	}
	for _, item := range receipt.Items {
		if !strings.Contains(guide, item.Link) {
			t.Errorf("guide lacks the address of %q", item.Title)
		}
	}
}
