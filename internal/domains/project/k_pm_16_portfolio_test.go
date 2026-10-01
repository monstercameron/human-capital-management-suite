package project

import (
	"testing"
	"time"
)

type pm16PortfolioAccess struct{ initiatives, projects map[string]bool }

func (a pm16PortfolioAccess) CanReadInitiative(id string) bool { return a.initiatives[id] }
func (a pm16PortfolioAccess) CanReadProject(id ProjectID) bool { return a.projects[string(id)] }

func TestTodo_PM_063(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	health, err := BuildPortfolioHealth("tenant-1", []Initiative{{ID: "initiative-1", TenantID: "tenant-1", OwnerID: "portfolio-owner", Version: 4, RollupRulesVersion: 2, ProjectIDs: []ProjectID{"project-1", "project-2"}}}, []PortfolioProject{{ID: "project-1", TenantID: "tenant-1", OwnerID: "project-owner-1", TaskCount: 4, CompletedTaskCount: 2, LastUpdated: now.Add(-time.Hour), StatusFreshness: "CURRENT", Authorized: true}, {ID: "project-2", TenantID: "tenant-1", OwnerID: "project-owner-2", TaskCount: 5, CompletedTaskCount: 5, LastUpdated: now.Add(-48 * time.Hour), StatusFreshness: "CURRENT", Authorized: true}}, pm16PortfolioAccess{initiatives: map[string]bool{"initiative-1": true}, projects: map[string]bool{"project-1": true, "project-2": true}}, now, 24*time.Hour)
	if err != nil || len(health.Initiatives) != 1 {
		t.Fatalf("portfolio health = %+v, err=%v", health, err)
	}
	rollup := health.Initiatives[0]
	if rollup.TaskCount != 4 || rollup.CompletedTasks != 2 || rollup.StaleProjectCount != 1 || rollup.Freshness != "STALE" || rollup.Projects[0].ProjectOwnerID != "project-owner-1" {
		t.Fatalf("stale project status was counted as committed outcome: %+v", rollup)
	}
}

func TestTodo_PM_063_Security(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	health, err := BuildPortfolioHealth("tenant-1", []Initiative{{ID: "private-initiative", TenantID: "tenant-1", OwnerID: "owner", Version: 1, RollupRulesVersion: 1, ProjectIDs: []ProjectID{"private-project"}}}, []PortfolioProject{{ID: "private-project", TenantID: "tenant-1", OwnerID: "private-owner", TaskCount: 99, CompletedTaskCount: 99, LastUpdated: now, StatusFreshness: "CURRENT", Authorized: true}}, pm16PortfolioAccess{initiatives: map[string]bool{}, projects: map[string]bool{"private-project": true}}, now, time.Hour)
	if err != nil || len(health.Initiatives) != 0 {
		t.Fatalf("private initiative affected portfolio totals: %+v, err=%v", health, err)
	}
}

func TestTodo_PM_063_Integration(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	health, err := BuildPortfolioHealth("tenant-1", []Initiative{{ID: "i1", TenantID: "tenant-1", OwnerID: "io", Version: 3, RollupRulesVersion: 7, ProjectIDs: []ProjectID{"p1"}}}, []PortfolioProject{{ID: "p1", TenantID: "tenant-1", OwnerID: "po", TaskCount: 3, CompletedTaskCount: 3, LastUpdated: now, StatusFreshness: "CURRENT", Authorized: true}}, pm16PortfolioAccess{initiatives: map[string]bool{"i1": true}, projects: map[string]bool{"p1": true}}, now, time.Hour)
	if err != nil || len(health.Initiatives) != 1 || health.Initiatives[0].RuleVersion != 7 || health.Initiatives[0].CompletedTasks != 3 {
		t.Fatalf("initiative rollup integration = %+v, err=%v", health, err)
	}
}
