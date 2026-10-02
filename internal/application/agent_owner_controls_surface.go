package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/memory"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AgentOwnerControls is the authenticated owner and memory projection shared
// by the product surface. Affordances come from current operation grants.
type AgentOwnerControls struct {
	Owners     *AgentOwnerOperations
	Runs       AgentOwnerRunSource
	Identities AgentControlIdentitySource
	Memory     *AgentMemoryOperations
}

func (s *AgentOwnerControls) audience(ctx context.Context, p *trust.Principal) (ownerops.Audience, []AgentOwnerRunProjection, error) {
	if s == nil || (s.Runs == nil && s.Owners == nil) {
		return "", nil, agentcontrols.ErrUnavailable
	}
	var selected ownerops.Audience
	views := []AgentOwnerRunProjection{}
	source := s.Runs
	if source == nil {
		source = s.Owners
	}
	for _, audience := range []ownerops.Audience{ownerops.AudienceOwner, ownerops.AudienceOperator, ownerops.AudienceMember} {
		rows, err := source.DashboardProjection(ctx, p, audience)
		if err == nil {
			if selected == "" {
				selected = audience
			}
			views = mergeAgentOwnerRunProjections(views, rows)
			continue
		}
		if !errors.Is(err, ownerops.ErrDenied) {
			return "", nil, err
		}
	}
	if selected != "" {
		return selected, views, nil
	}
	return "", nil, agentcontrols.ErrDenied
}
func (s *AgentOwnerControls) Snapshot(ctx context.Context) (agentcontrols.Reply, error) {
	if s == nil {
		return agentcontrols.Reply{}, agentcontrols.ErrUnavailable
	}
	p, ok := trust.FromContext(ctx)
	if !ok {
		return agentcontrols.Reply{}, agentcontrols.ErrUnauthenticated
	}
	_, views, err := s.audience(ctx, p)
	authorized := err == nil
	if err != nil && !errors.Is(err, agentcontrols.ErrDenied) && !(errors.Is(err, agentcontrols.ErrUnavailable) && s.Memory != nil) {
		return agentcontrols.Reply{}, agentControlsError(err)
	}
	snapshot := productui.AgentControlsSnapshot{Available: true, Schedules: []productui.AgentControlSchedule{}, Runs: []productui.AgentControlRun{}, Memory: []productui.AgentControlMemory{}}
	for _, view := range views {
		if s.Identities == nil {
			return agentcontrols.Reply{}, agentcontrols.ErrUnavailable
		}
		identity, identityErr := s.Identities.ResolveAgentControlIdentity(ctx, p.Tenant(), view.View.AgentID, view.View.Version)
		if identityErr != nil {
			// One run whose agent the catalog cannot name (a general agent,
			// a retired or unpublished version) must not hide every other
			// run from the owner. That run is listed under the content-free
			// fallback label; the owner name is left for the others to set.
			identity, identityErr = AgentFallbackControlIdentities{}.ResolveAgentControlIdentity(ctx, p.Tenant(), view.View.AgentID, view.View.Version)
			if identityErr != nil {
				return agentcontrols.Reply{}, agentControlsError(identityErr)
			}
			snapshot.Runs = append(snapshot.Runs, projectAgentControlRun(view, identity))
			continue
		}
		if snapshot.OwnerName == "" {
			snapshot.OwnerName = identity.OwnerName
		} else if snapshot.OwnerName != identity.OwnerName {
			snapshot.OwnerName = ""
		}
		snapshot.Runs = append(snapshot.Runs, projectAgentControlRun(view, identity))
	}
	if s.Memory != nil {
		items, e := s.Memory.Inventory(ctx, p)
		if e != nil && !errors.Is(e, memory.ErrDenied) {
			return agentcontrols.Reply{}, agentControlsError(e)
		}
		if e == nil {
			authorized = true
			snapshot.CanExport = len(items) > 0
			for _, item := range items {
				actions := []string{}
				if item.CanDelete {
					actions = append(actions, "delete")
				}
				if item.CanRevoke {
					actions = append(actions, "revoke")
				}
				snapshot.CanExport = snapshot.CanExport && item.CanExport
				snapshot.Memory = append(snapshot.Memory, productui.AgentControlMemory{ID: item.ID, Revision: 1, Source: item.SourceOwner + ":" + item.SourceID, Audience: strings.Join(item.Audience, ", "), Purpose: item.Purpose, Class: item.DataClass, Expires: item.ExpiresAt.Format(time.RFC3339), Held: item.Held, Actions: actions})
			}
		}
	}
	if !authorized {
		return agentcontrols.Reply{}, agentcontrols.ErrDenied
	}
	return agentcontrols.Reply{Snapshot: snapshot}, nil
}
func (s *AgentOwnerControls) Control(ctx context.Context, command productui.AgentControlsCommand) (agentcontrols.Reply, error) {
	p, ok := trust.FromContext(ctx)
	if !ok {
		return agentcontrols.Reply{}, agentcontrols.ErrUnauthenticated
	}
	if command.IdempotencyKey == "" || command.Reason == "" {
		return agentcontrols.Reply{}, agentcontrols.ErrInvalid
	}
	switch command.Kind {
	case "run":
		if command.Action != "pause" || command.ExpectedRevision == 0 || command.IncidentID == "" {
			return agentcontrols.Reply{}, agentcontrols.ErrInvalid
		}
		if s == nil || s.Owners == nil {
			return agentcontrols.Reply{}, agentcontrols.ErrUnavailable
		}
		err := error(ownerops.ErrDenied)
		for _, audience := range []ownerops.Audience{ownerops.AudienceOwner, ownerops.AudienceOperator, ownerops.AudienceMember} {
			_, err = s.Owners.Stop(ctx, p, audience, ownerops.StopRequest{Kind: ownerops.PauseTask, TaskID: command.ID, ExpectedRevision: command.ExpectedRevision, RequestID: command.IdempotencyKey, IncidentID: command.IncidentID, Reason: command.Reason})
			if !errors.Is(err, ownerops.ErrDenied) {
				break
			}
		}
		if err != nil {
			return agentcontrols.Reply{}, agentControlsError(err)
		}
	case "memory":
		if s == nil || s.Memory == nil {
			return agentcontrols.Reply{}, agentcontrols.ErrUnavailable
		}
		if command.Action == "export" {
			items, err := s.Memory.Export(ctx, p)
			if err != nil {
				return agentcontrols.Reply{}, agentControlsError(err)
			}
			body, err := json.Marshal(items)
			if err != nil {
				return agentcontrols.Reply{}, err
			}
			reply, err := s.Snapshot(ctx)
			reply.Export = body
			return reply, err
		}
		if command.ExpectedRevision != 1 || command.ID == "" {
			return agentcontrols.Reply{}, agentcontrols.ErrInvalid
		}
		switch command.Action {
		case "delete":
			if err := s.Memory.Delete(ctx, p, command.ID, command.Reason); err != nil {
				return agentcontrols.Reply{}, agentControlsError(err)
			}
		case "revoke":
			items, err := s.Memory.Inventory(ctx, p)
			if err != nil {
				return agentcontrols.Reply{}, agentControlsError(err)
			}
			found := false
			for _, item := range items {
				if item.ID == command.ID {
					found = true
					if err = s.Memory.Revoke(ctx, p, item.Pin, command.Reason); err != nil {
						return agentcontrols.Reply{}, agentControlsError(err)
					}
					break
				}
			}
			if !found {
				return agentcontrols.Reply{}, agentcontrols.ErrInvalid
			}
		default:
			return agentcontrols.Reply{}, agentcontrols.ErrInvalid
		}
	default:
		return agentcontrols.Reply{}, agentcontrols.ErrInvalid
	}
	return s.Snapshot(ctx)
}
func agentControlsError(err error) error {
	switch {
	case errors.Is(err, ownerops.ErrDenied), errors.Is(err, memory.ErrDenied), errors.Is(err, memory.ErrHeld), errors.Is(err, memory.ErrDisposition):
		return errors.Join(agentcontrols.ErrDenied, err)
	case errors.Is(err, agentrun.ErrConflict):
		return errors.Join(agentcontrols.ErrConflict, err)
	case errors.Is(err, ownerops.ErrInvalid), errors.Is(err, memory.ErrInvalid), errors.Is(err, agentrun.ErrNotFound):
		return errors.Join(agentcontrols.ErrInvalid, err)
	default:
		return err
	}
}
