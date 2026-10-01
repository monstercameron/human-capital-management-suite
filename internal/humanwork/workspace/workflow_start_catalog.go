package workspace

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// WorkflowStartConfig is the JSON-island form of one startable-workflow
// catalog entry. It carries display metadata and one bounded availability
// class only: never capability names, subject facts or policy detail.
type WorkflowStartConfig struct {
	WorkflowID      string   `json:"workflow_id"`
	Version         uint32   `json:"version"`
	SemanticVersion string   `json:"semantic_version,omitempty"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Category        string   `json:"category,omitempty"`
	Keywords        []string `json:"keywords,omitempty"`
	Icon            string   `json:"icon,omitempty"`
	Owner           string   `json:"owner,omitempty"`
	Availability    string   `json:"availability"`
	Favorite        bool     `json:"favorite,omitempty"`
	RecentRank      int64    `json:"recent_rank,omitempty"`
}

const workflowStartFavoritePrefix = "workflow-start:"

// WorkflowStartSource projects the workflows the viewer may discover in a
// tenant. canStart is the viewer's start authority already resolved from the
// role grants; the source must never widen it.
type WorkflowStartSource func(ctx context.Context, tenant values.TenantId, canStart bool) ([]WorkflowStartConfig, error)

// resolveWorkflowStarts builds the viewer's catalog. A missing source or a
// failed read yields an empty catalog: the page then states the empty case
// rather than inventing authority.
func (h *Handler) resolveWorkflowStarts(ctx context.Context, principal *trust.Principal, access productAccess) []WorkflowStartConfig {
	if h.workflowStarts == nil || principal == nil {
		return nil
	}
	entries, err := h.workflowStarts(ctx, principal.Tenant(), workflowStartAuthority(access))
	if err != nil {
		return nil
	}
	if h.preferences != nil {
		if snapshot, loadErr := h.preferences.Load(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject()); loadErr == nil {
			entries = applyWorkflowStartPreferences(entries, snapshot.User)
		}
	}
	return entries
}

func applyWorkflowStartPreferences(entries []WorkflowStartConfig, user preferences.User) []WorkflowStartConfig {
	favorites := make(map[string]bool)
	for _, value := range user.FavoritePages {
		if strings.HasPrefix(value, workflowStartFavoritePrefix) {
			favorites[strings.TrimPrefix(value, workflowStartFavoritePrefix)] = true
		}
	}
	for index := range entries {
		id := entries[index].WorkflowID
		entries[index].Favorite = favorites[id]
		entries[index].RecentRank = user.WorkflowUses[id]
	}
	return entries
}

// workflowStartAuthority is the viewer's start authority: the workflow start
// page and journey creation must both be granted. Without a durable role
// store the signed-role compatibility policy applies and the page route
// gate has already admitted the viewer.
func workflowStartAuthority(access productAccess) bool {
	if !access.configured {
		return true
	}
	return roleaccess.CanPageAction(access.permissions, string(productui.PageWorkflowStart), roleaccess.ActionView) &&
		roleaccess.CanPageAction(access.permissions, string(productui.PageJourneys), roleaccess.ActionCreate)
}

func productWorkflowStartItems(values []WorkflowStartConfig) []productui.WorkflowStartItem {
	result := make([]productui.WorkflowStartItem, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.WorkflowID) == "" || strings.TrimSpace(value.Name) == "" {
			continue
		}
		result = append(result, productui.WorkflowStartItem{
			WorkflowID: value.WorkflowID, Version: value.Version, SemanticVersion: value.SemanticVersion,
			Name: value.Name, Description: value.Description, Category: value.Category,
			Keywords: append([]string(nil), value.Keywords...), Icon: value.Icon, Owner: value.Owner,
			Availability: productui.WorkflowStartAvailability(value.Availability), Favorite: value.Favorite, RecentRank: value.RecentRank,
		})
	}
	return result
}
