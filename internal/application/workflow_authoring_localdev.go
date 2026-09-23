package application

import (
	"context"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/draftcompile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/hireexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// localDevelopmentWorkflowAuthoring supplies an explicit authoring catalog
// only to the loopback local-development profile. Standard deployments stay
// fail closed until their tenant-owned capability and extension registries
// are composed; the global capability registry is never an implicit grant.
func localDevelopmentWorkflowAuthoring(cfg ServeConfig) (draftcompile.CapabilityPolicy, []designerpalette.Entry) {
	if cfg.Profile != ServeProfileLocalDev || cfg.Tenant == "" {
		return nil, nil
	}
	tenant := values.TenantId(cfg.Tenant)
	policy := draftcompile.CapabilityPolicyFunc(func(_ context.Context, requested values.TenantId, key capability.Key) bool {
		return requested == tenant && key.ID != "" && key.Version > 0
	})
	promotionDefinition := promotionexec.Definition()
	promotionPlanDigest := ""
	if promotionPlan, err := promotionexec.Compile(promotionDefinition); err == nil {
		promotionPlanDigest = promotionPlan.Digest()
	}
	// New employee hire is the second governed template (WF-HIRE-001). It runs
	// through the same driver in test/workflow; offering it here lets an
	// author open, read and adapt it, though nothing serves its runs yet.
	hireDefinition := hireexec.Definition()
	hirePlanDigest := ""
	if hirePlan, err := hireexec.Compile(hireDefinition); err == nil {
		hirePlanDigest = hirePlan.Digest()
	}
	reviewNodes := selectWorkflowNodes(promotionDefinition.Nodes, promotionexec.NodeApproveFinance, promotionexec.NodeApproveManager)
	reviewApprovals := append([]workflow.ApprovalRequirement(nil), promotionDefinition.ApprovalRequirements...)
	extensions := []designerpalette.Entry{
		{
			ID: "hcmnext.fragments.promotion_review", Version: 1, Name: "Promotion review", Kind: designerpalette.KindFragment,
			Domain: "People", Description: "Manager and finance approval checks as one collapsible group.",
			EffectClass: capability.EffectPure, Reversal: "NO_EFFECT", Status: "ACTIVE", StepType: workflow.StepApproval,
			RequiredCapabilities: requiredWorkflowCapabilities(reviewNodes),
			Expansion: designerpalette.Expansion{
				Nodes:                reviewNodes,
				Edges:                []workflow.Edge{{From: promotionexec.NodeApproveFinance, To: promotionexec.NodeApproveManager, RouteKey: "APPROVED"}},
				ApprovalRequirements: reviewApprovals,
			},
		},
		{
			ID: "hcmnext.templates.promotion", Version: 1, Name: "Promotion", Kind: designerpalette.KindTemplate,
			Domain: "People", Description: "Governed promotion path with compensation, approval, effective-date, and reconciliation controls.",
			EffectClass: capability.EffectInternalMutation, Reversal: "COMPENSATION_REQUIRED", Status: "ACTIVE",
			RequiredCapabilities: requiredWorkflowCapabilities(promotionDefinition.Nodes),
			PublishedPlanDigest:  promotionPlanDigest,
			Expansion:            designerpalette.Expansion{Template: &promotionDefinition},
		},
		{
			ID: "hcmnext.templates.new_hire", Version: 1, Name: "New employee hire", Kind: designerpalette.KindTemplate,
			Domain: "People", Description: "Offer approval, background check, new-hire forms, provisioning, a wait until the start date, and one commit of the hire.",
			EffectClass: capability.EffectInternalMutation, Reversal: "COMPENSATION_REQUIRED", Status: "ACTIVE",
			RequiredCapabilities: requiredWorkflowCapabilities(hireDefinition.Nodes),
			PublishedPlanDigest:  hirePlanDigest,
			Expansion:            designerpalette.Expansion{Template: &hireDefinition},
		},
	}
	return policy, extensions
}

func requiredWorkflowCapabilities(nodes []workflow.Node) []capability.Key {
	seen := make(map[capability.Key]bool)
	for _, node := range nodes {
		if node.Capability != nil {
			seen[node.Capability.Key()] = true
		}
	}
	result := make([]capability.Key, 0, len(seen))
	for key := range seen {
		result = append(result, key)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID == result[j].ID {
			return result[i].Version < result[j].Version
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func selectWorkflowNodes(nodes []workflow.Node, ids ...string) []workflow.Node {
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	selected := make([]workflow.Node, 0, len(ids))
	for _, node := range nodes {
		if wanted[node.ID] {
			selected = append(selected, node)
		}
	}
	return selected
}
