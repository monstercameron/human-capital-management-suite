package project

import (
	"errors"
	"strings"
	"time"
)

var ErrInvalidPortfolio = errors.New("project: invalid portfolio projection")

type Initiative struct {
	ID                 string
	TenantID           string
	OwnerID            string
	Version            uint64
	RollupRulesVersion uint64
	ProjectIDs         []ProjectID
}

type PortfolioProject struct {
	ID                 ProjectID
	TenantID           string
	OwnerID            string
	TaskCount          uint64
	CompletedTaskCount uint64
	LastUpdated        time.Time
	StatusFreshness    string
	Authorized         bool
}

type InitiativeAccess interface {
	CanReadInitiative(string) bool
	CanReadProject(ProjectID) bool
}

type ProjectHealth struct {
	ProjectID      ProjectID
	ProjectOwnerID string
	TaskCount      uint64
	CompletedTasks uint64
	Freshness      string
}

type InitiativeHealth struct {
	InitiativeID      string
	InitiativeOwnerID string
	RuleVersion       uint64
	ProjectCount      int
	StaleProjectCount int
	TaskCount         uint64
	CompletedTasks    uint64
	Freshness         string
	Projects          []ProjectHealth
}

type PortfolioHealth struct {
	TenantID    string
	Initiatives []InitiativeHealth
}

// BuildPortfolioHealth filters initiative and project membership before any
// rollup. Stale projects contribute no committed task outcome; they remain
// visible only as a freshness signal to an already-authorized viewer.
func BuildPortfolioHealth(tenantID string, initiatives []Initiative, projects []PortfolioProject, access InitiativeAccess, now time.Time, maxAge time.Duration) (PortfolioHealth, error) {
	if strings.TrimSpace(tenantID) == "" || access == nil || now.IsZero() || maxAge <= 0 {
		return PortfolioHealth{}, ErrInvalidPortfolio
	}
	byProject := make(map[ProjectID]PortfolioProject, len(projects))
	for _, p := range projects {
		if p.ID == "" || p.TenantID != tenantID || !p.Authorized || !access.CanReadProject(p.ID) || p.CompletedTaskCount > p.TaskCount || p.LastUpdated.IsZero() {
			continue
		}
		byProject[p.ID] = p
	}
	out := PortfolioHealth{TenantID: tenantID, Initiatives: make([]InitiativeHealth, 0, len(initiatives))}
	seenInitiatives := map[string]bool{}
	for _, initiative := range initiatives {
		if initiative.TenantID != tenantID || initiative.ID == "" || initiative.OwnerID == "" || initiative.Version == 0 || initiative.RollupRulesVersion == 0 || seenInitiatives[initiative.ID] || !access.CanReadInitiative(initiative.ID) {
			continue
		}
		seenInitiatives[initiative.ID] = true
		health := InitiativeHealth{InitiativeID: initiative.ID, InitiativeOwnerID: initiative.OwnerID, RuleVersion: initiative.RollupRulesVersion, Freshness: "NO_DATA"}
		seenProjects := map[ProjectID]bool{}
		for _, projectID := range initiative.ProjectIDs {
			if seenProjects[projectID] {
				return PortfolioHealth{}, ErrInvalidPortfolio
			}
			seenProjects[projectID] = true
			project, ok := byProject[projectID]
			if !ok {
				continue
			}
			fresh := "CURRENT"
			if project.StatusFreshness != "CURRENT" || now.Sub(project.LastUpdated) > maxAge || now.Before(project.LastUpdated) {
				fresh = "STALE"
				health.StaleProjectCount++
			}
			health.ProjectCount++
			projectHealth := ProjectHealth{ProjectID: project.ID, ProjectOwnerID: project.OwnerID, Freshness: fresh}
			if fresh == "CURRENT" {
				projectHealth.TaskCount = project.TaskCount
				projectHealth.CompletedTasks = project.CompletedTaskCount
				health.TaskCount += project.TaskCount
				health.CompletedTasks += project.CompletedTaskCount
			}
			health.Projects = append(health.Projects, projectHealth)
		}
		if health.ProjectCount > 0 {
			health.Freshness = "CURRENT"
			if health.StaleProjectCount > 0 {
				health.Freshness = "STALE"
			}
		}
		out.Initiatives = append(out.Initiatives, health)
	}
	return out, nil
}
