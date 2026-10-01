package project

import (
	"context"
	"errors"
	"testing"
	"time"
)

type pm056Access struct {
	milestones map[string]bool
	tasks      map[TaskID]bool
}

func (a pm056Access) CanViewMilestone(id string) bool         { return a.milestones[id] }
func (a pm056Access) CanViewTask(_ ProjectID, id TaskID) bool { return a.tasks[id] }
func (a pm056Access) CanViewRelease(id string) bool           { return id == "r1" }

func TestTodo_PM_056(t *testing.T) {
	milestone, err := NewMilestone("m1", "tenant-1", "p1", "Launch", "owner-1", "2026-10-10", []TaskID{"t1", "t2"})
	if err != nil {
		t.Fatal(err)
	}
	if milestone.IsComplete() {
		t.Fatal("target date alone marked milestone complete")
	}
	board, err := BuildMilestoneBoard("tenant-1", "p1", []Milestone{milestone}, []MilestoneTask{{ID: "t1", ProjectID: "p1", Title: "Visible", Status: "active"}, {ID: "t2", ProjectID: "p1", Title: "Hidden", Status: "done"}}, pm056Access{milestones: map[string]bool{"m1": true}, tasks: map[TaskID]bool{"t1": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Columns) != 1 || len(board.Columns[0].Tasks) != 1 || board.Columns[0].Tasks[0].ID != "t1" || board.Columns[0].LinkedTaskIDs[0] != "t1" {
		t.Fatalf("board did not retain only authorized linked work: %+v", board)
	}
	completed, err := milestone.Complete(milestone.Revision, "owner-1", time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("test", -4*60*60)))
	if err != nil {
		t.Fatal(err)
	}
	if !completed.IsComplete() || completed.CompletedBy != "owner-1" || completed.CompletedAt.Location() != time.UTC {
		t.Fatalf("explicit completion not recorded: %+v", completed)
	}
}

func TestTodo_PM_056_Browser(t *testing.T) {
	milestone, err := NewMilestone("m1", "tenant-1", "p1", "Onboarding", "lead", "2026-12-31", []TaskID{"task"})
	if err != nil {
		t.Fatal(err)
	}
	board, err := BuildMilestoneBoard("tenant-1", "p1", []Milestone{milestone}, []MilestoneTask{{ID: "task", ProjectID: "p1", Title: "Prepare", Status: "active", OwnerID: "lead"}}, pm056Access{milestones: map[string]bool{"m1": true}, tasks: map[TaskID]bool{"task": true}})
	if err != nil || len(board.Columns) != 1 || board.Columns[0].Complete {
		t.Fatalf("milestone presentation projection = %+v, err=%v", board, err)
	}
}

func TestTodo_PM_056_Security(t *testing.T) {
	milestone, err := NewMilestone("m1", "tenant-1", "p1", "Private milestone", "owner", "2026-12-31", []TaskID{"secret"})
	if err != nil {
		t.Fatal(err)
	}
	board, err := BuildMilestoneBoard("tenant-1", "p1", []Milestone{milestone}, []MilestoneTask{{ID: "secret", ProjectID: "p1", Title: "Confidential task"}}, pm056Access{milestones: map[string]bool{"m1": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Columns[0].Tasks) != 0 || len(board.Columns[0].LinkedTaskIDs) != 0 {
		t.Fatalf("unauthorized task affected milestone view: %+v", board.Columns[0])
	}
}

func TestTodo_PM_058(t *testing.T) {
	release, err := NewRelease("r1", "tenant-1", "p1", 7, []TaskID{"t1"}, "m1", "release-system")
	if err != nil {
		t.Fatal(err)
	}
	if release.Status != ReleaseTracked {
		t.Fatalf("new release status = %q", release.Status)
	}
	recorded, err := release.RecordVerification(release.Revision, "release-operator", ReleaseEvidence{ID: "e1", Kind: "verification", Summary: "External system recorded the outcome", RecordedBy: "release-operator", RecordedAt: time.Unix(10, 0).UTC()}, ExternalDeploymentReference{System: "deploy.example", Reference: "run-42", ObservedAt: time.Unix(10, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Status != ReleaseVerifiedExternally || recorded.OwningReleaseSystem != "release-system" {
		t.Fatalf("release verification changed the wrong authority: %+v", recorded)
	}
}

func TestTodo_PM_058_Integration(t *testing.T) {
	release, err := NewRelease("r1", "tenant-1", "p1", 3, []TaskID{"visible", "hidden"}, "m1", "deploy-system")
	if err != nil {
		t.Fatal(err)
	}
	board, err := BuildReleaseBoard("tenant-1", "p1", []Release{release}, pm056Access{milestones: map[string]bool{}, tasks: map[TaskID]bool{"visible": true}})
	if err != nil || len(board.Releases) != 1 || len(board.Releases[0].ScopeTaskIDs) != 1 || board.Releases[0].DeploymentClaim != "TRACKING_ONLY" {
		t.Fatalf("release board projection = %+v, err=%v", board, err)
	}
}

func TestTodo_PM_058_Security(t *testing.T) {
	release := Release{ID: "r1", TenantID: "tenant-1", ProjectID: "p1", ScopeRevision: 1, ScopeTaskIDs: []TaskID{"t1"}, TargetMilestoneID: "m1", OwningReleaseSystem: "deploy", Status: ReleaseVerifiedExternally, Revision: 1}
	if err := release.Validate(); !errors.Is(err, ErrInvalidRelease) {
		t.Fatalf("unsubstantiated verified release error = %v", err)
	}
	board, err := BuildReleaseBoard("tenant-1", "p1", []Release{release}, pm056Access{milestones: map[string]bool{}, tasks: map[TaskID]bool{"t1": true}})
	if err == nil || len(board.Releases) != 0 {
		t.Fatalf("invalid release crossed board boundary: board=%+v err=%v", board, err)
	}
}

func TestTodo_PM_059(t *testing.T) {
	request := IntakeRequest{ID: "req-1", TenantID: "tenant-1", ProjectID: "p1", SourceSystem: "service-desk", Kind: IntakeRequestKind, SafeTitle: "Coordinate access request", Priority: "HIGH", QueueOwnerID: "queue-1", Classification: "INTERNAL", Revision: 4, SLAReference: "sla-4"}
	called := false
	task, err := CreateCoordinationTask(context.Background(), request, "task-1", "actor-1", IntakeGateFunc(func(_ context.Context, got IntakeRequest, actor string) error {
		called = got.ID == request.ID && actor == "actor-1"
		return nil
	}))
	if err != nil || !called || !task.CoordinationOnly || task.CanMutateSource || task.CanMutateSLA || task.Link.RequestRevision != 4 {
		t.Fatalf("unsafe coordination task = %+v, err=%v", task, err)
	}
}

func TestTodo_PM_059_Integration(t *testing.T) {
	request := IntakeRequest{ID: "incident-1", TenantID: "tenant-1", ProjectID: "p1", SourceSystem: "operations", Kind: IntakeIncidentKind, SafeTitle: "Coordinate incident follow-up", Priority: "URGENT", QueueOwnerID: "ops", Classification: "CONFIDENTIAL", Confidential: true, Revision: 2}
	gate := IntakeGateFunc(func(_ context.Context, got IntakeRequest, _ string) error {
		if !got.Confidential || got.Kind != IntakeIncidentKind {
			return ErrIntakeAccess
		}
		return nil
	})
	task, err := CreateCoordinationTask(context.Background(), request, "task-incident", "operator", gate)
	if err != nil || task.Link.SourceSystem != "operations" || task.Link.Kind != IntakeIncidentKind {
		t.Fatalf("intake adapter did not preserve governed link: %+v, err=%v", task, err)
	}
}

func TestTodo_PM_059_Security(t *testing.T) {
	request := IntakeRequest{ID: "case-1", TenantID: "tenant-1", ProjectID: "p1", SourceSystem: "support", Kind: IntakeRequestKind, SafeTitle: "Safe coordination title", Priority: "NORMAL", QueueOwnerID: "support", Classification: "CONFIDENTIAL", Confidential: true, Revision: 1}
	_, err := CreateCoordinationTask(context.Background(), request, "task-1", "actor", IntakeGateFunc(func(context.Context, IntakeRequest, string) error { return ErrIntakeAccess }))
	if !errors.Is(err, ErrIntakeAccess) {
		t.Fatalf("privacy gate error = %v", err)
	}
}
