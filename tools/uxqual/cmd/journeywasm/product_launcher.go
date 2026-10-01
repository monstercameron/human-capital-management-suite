package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// projectLauncherActions converts the authenticated shell island into the
// narrow presentation contract. The component registry performs the final ID
// and destination validation; malformed states are omitted here.
func projectLauncherActions(values []journeyclient.LauncherAction) []productui.LauncherActionProjection {
	counts := make(map[string]int, len(values))
	for _, value := range values {
		counts[strings.TrimSpace(value.ID)]++
	}
	result := make([]productui.LauncherActionProjection, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value.ID)
		if id == "" || counts[id] != 1 || value.Priority < 0 {
			continue
		}
		state := productui.ActionState{
			Availability: productui.ActionAvailability(strings.TrimSpace(value.Availability)),
			Reason:       strings.TrimSpace(value.Reason),
			Recovery: productui.ActionLinkProps{
				Label: strings.TrimSpace(value.RecoveryLabel),
				Href:  strings.TrimSpace(value.RecoveryHref),
			},
		}
		switch state.Availability {
		case productui.ActionAvailable:
			state.Reason = ""
			state.Recovery = productui.ActionLinkProps{}
		case productui.ActionUnavailable:
			if state.Reason == "" || state.Recovery.Label == "" || state.Recovery.Href == "" {
				continue
			}
		default:
			continue
		}
		result = append(result, productui.LauncherActionProjection{ID: id, State: state, Priority: value.Priority})
	}
	return result
}

// projectWorkflowStarts converts the island's workflow start catalog into the
// page contract. Entries without an id or name, and repeated ids, are dropped;
// an unknown availability fails closed to missing_authority.
func projectWorkflowStarts(values []journeyclient.WorkflowStart) []productui.WorkflowStartItem {
	result := make([]productui.WorkflowStartItem, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		id, name := strings.TrimSpace(value.WorkflowID), strings.TrimSpace(value.Name)
		if id == "" || name == "" || seen[id] {
			continue
		}
		seen[id] = true
		availability := productui.WorkflowStartAvailability(strings.TrimSpace(value.Availability))
		switch availability {
		case productui.WorkflowStartAvailable, productui.WorkflowStartNoCapability, productui.WorkflowStartMissingAuthority,
			productui.WorkflowStartMissingPrerequisite, productui.WorkflowStartQuarantined:
		default:
			availability = productui.WorkflowStartMissingAuthority
		}
		result = append(result, productui.WorkflowStartItem{
			WorkflowID: id, Version: value.Version, SemanticVersion: value.SemanticVersion, Name: name,
			Description: value.Description, Category: value.Category, Keywords: append([]string(nil), value.Keywords...),
			Icon: value.Icon, Owner: value.Owner, Availability: availability,
		})
	}
	return result
}
