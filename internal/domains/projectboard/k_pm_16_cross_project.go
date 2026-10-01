package projectboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrInvalidCrossProjectPage = errors.New("projectboard: invalid cross-project page")

type CrossProjectView struct {
	ID         string
	Version    uint64
	TenantID   string
	ProjectIDs []string
	Filter     Filter
	OrderBy    OrderField
	Descending bool
}

type CrossProjectScope struct {
	ProjectID string
	Granted   bool
}

type CrossProjectTask struct {
	TenantID    string
	ProjectID   string
	ProjectName string
	OwnerID     string
	Authorized  bool
	Task        Task
}

type CrossProjectCursor struct {
	TenantID       string
	ViewerID       string
	ViewID         string
	ViewVersion    uint64
	ScopeDigest    string
	AfterProjectID string
	AfterTaskID    string
}

type CrossProjectPage struct {
	ViewID  string
	Version uint64
	Tasks   []CrossProjectTask
	Next    *CrossProjectCursor
}

// BuildCrossProjectPage intersects the requested project set with current
// grants before filtering, sorting, or paging. It deliberately returns no
// total count, since a count would disclose private projects.
func BuildCrossProjectPage(view CrossProjectView, tenantID, viewerID string, scopes []CrossProjectScope, rows []CrossProjectTask, limit int, cursor *CrossProjectCursor) (CrossProjectPage, error) {
	if strings.TrimSpace(view.ID) == "" || view.Version == 0 || view.TenantID != tenantID || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(viewerID) == "" || limit < 1 || limit > MaxPageSize {
		return CrossProjectPage{}, ErrInvalidCrossProjectPage
	}
	requested := map[string]bool{}
	for _, id := range view.ProjectIDs {
		if id == "" || requested[id] {
			return CrossProjectPage{}, ErrInvalidCrossProjectPage
		}
		requested[id] = true
	}
	granted := map[string]bool{}
	for _, scope := range scopes {
		if scope.Granted && requested[scope.ProjectID] {
			granted[scope.ProjectID] = true
		}
	}
	scopeDigest := digestScope(view, granted)
	if cursor != nil && (cursor.TenantID != tenantID || cursor.ViewerID != viewerID || cursor.ViewID != view.ID || cursor.ViewVersion != view.Version || cursor.ScopeDigest != scopeDigest) {
		return CrossProjectPage{}, ErrInvalidCrossProjectPage
	}
	filtered := make([]CrossProjectTask, 0, len(rows))
	for _, row := range rows {
		if !row.Authorized || row.TenantID != tenantID || !granted[row.ProjectID] || row.Task.ID == "" || !matches(view.Filter, row.Task) {
			continue
		}
		row.Task.Labels = append([]string(nil), row.Task.Labels...)
		filtered = append(filtered, row)
	}
	sort.Slice(filtered, func(i, j int) bool { return crossProjectLess(view, filtered[i], filtered[j]) })
	start := 0
	if cursor != nil {
		for i, row := range filtered {
			if row.ProjectID == cursor.AfterProjectID && row.Task.ID == cursor.AfterTaskID {
				start = i + 1
				break
			}
		}
	}
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	page := CrossProjectPage{ViewID: view.ID, Version: view.Version, Tasks: append([]CrossProjectTask(nil), filtered[start:end]...)}
	if end < len(filtered) {
		last := filtered[end-1]
		page.Next = &CrossProjectCursor{TenantID: tenantID, ViewerID: viewerID, ViewID: view.ID, ViewVersion: view.Version, ScopeDigest: scopeDigest, AfterProjectID: last.ProjectID, AfterTaskID: last.Task.ID}
	}
	return page, nil
}

func crossProjectLess(view CrossProjectView, a, b CrossProjectTask) bool {
	if a.ProjectID != b.ProjectID {
		return a.ProjectID < b.ProjectID
	}
	if view.OrderBy == OrderTitle && a.Task.Title != b.Task.Title {
		return compareString(a.Task.Title, b.Task.Title, view.Descending)
	}
	if view.OrderBy == OrderPriority && a.Task.Priority != b.Task.Priority {
		return compareString(a.Task.Priority, b.Task.Priority, view.Descending)
	}
	if view.Descending {
		return a.Task.ID > b.Task.ID
	}
	return a.Task.ID < b.Task.ID
}

func compareString(a, b string, descending bool) bool {
	if descending {
		return a > b
	}
	return a < b
}

func digestScope(view CrossProjectView, granted map[string]bool) string {
	ids := make([]string, 0, len(granted))
	for id := range granted {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	raw, _ := json.Marshal(struct {
		ViewID   string
		Version  uint64
		Projects []string
		Filter   Filter
		Order    OrderField
		Desc     bool
	}{view.ID, view.Version, ids, view.Filter, view.OrderBy, view.Descending})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (c CrossProjectCursor) String() string {
	return fmt.Sprintf("%s/%s", c.AfterProjectID, c.AfterTaskID)
}
