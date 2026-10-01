package application

import (
	"context"
	"errors"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type AgentOwnerControlSurface interface {
	Snapshot(context.Context) (agentcontrols.Reply, error)
	Control(context.Context, productui.AgentControlsCommand) (agentcontrols.Reply, error)
}

// AgentControlsSurface composes owner projections. Each sub-surface resolves
// current authority from the authenticated context before reading or writing.
type AgentControlsSurface struct {
	Schedules  agentcontrols.Surface
	Operations AgentOwnerControlSurface
}

func agentControlsAuthenticated(ctx context.Context) error {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return agentcontrols.ErrUnauthenticated
	}
	if principal.SubjectKind() != trust.SubjectKindHuman {
		return agentcontrols.ErrDenied
	}
	return nil
}

func (s *AgentControlsSurface) Snapshot(ctx context.Context) (agentcontrols.Reply, error) {
	var reply agentcontrols.Reply
	if err := agentControlsAuthenticated(ctx); err != nil {
		return reply, err
	}
	if s == nil {
		return reply, agentcontrols.ErrUnavailable
	}
	if s.Schedules != nil {
		part, err := s.Schedules.Snapshot(ctx)
		if err != nil && !errors.Is(err, agentcontrols.ErrDenied) {
			return reply, err
		}
		if err == nil {
			reply.Snapshot = part.Snapshot
		}
	}
	if s.Operations != nil {
		part, err := s.Operations.Snapshot(ctx)
		if err != nil && !errors.Is(err, agentcontrols.ErrDenied) {
			return reply, err
		}
		if err == nil {
			reply.Snapshot.Available = reply.Snapshot.Available || part.Snapshot.Available
			reply.Snapshot.Runs = part.Snapshot.Runs
			reply.Snapshot.Memory = part.Snapshot.Memory
			reply.Snapshot.CanExport = part.Snapshot.CanExport
		}
	}
	return reply, nil
}

func (s *AgentControlsSurface) Control(ctx context.Context, command productui.AgentControlsCommand) (agentcontrols.Reply, error) {
	if err := agentControlsAuthenticated(ctx); err != nil {
		return agentcontrols.Reply{}, err
	}
	if s == nil {
		return agentcontrols.Reply{}, agentcontrols.ErrUnavailable
	}
	var part agentcontrols.Reply
	var err error
	switch command.Kind {
	case "schedule":
		if s.Schedules == nil {
			return part, agentcontrols.ErrUnavailable
		}
		part, err = s.Schedules.Control(ctx, command)
	case "run", "memory":
		if s.Operations == nil {
			return part, agentcontrols.ErrUnavailable
		}
		part, err = s.Operations.Control(ctx, command)
	default:
		return part, agentcontrols.ErrInvalid
	}
	if err != nil {
		return part, err
	}
	reply, err := s.Snapshot(ctx)
	reply.Export = part.Export
	mergeAgentOccurrencePreview(&reply, part)
	return reply, err
}

func (s *AgentControlsSurface) Draft(ctx context.Context, draft productui.AgentScheduleDraft) (agentcontrols.Reply, error) {
	if err := agentControlsAuthenticated(ctx); err != nil {
		return agentcontrols.Reply{}, err
	}
	if s == nil || s.Schedules == nil {
		return agentcontrols.Reply{}, agentcontrols.ErrUnavailable
	}
	if _, err := s.Schedules.Draft(ctx, draft); err != nil {
		return agentcontrols.Reply{}, err
	}
	return s.Snapshot(ctx)
}

func (s *AgentControlsSurface) Preview(ctx context.Context, draft productui.AgentScheduleDraft) (agentcontrols.Reply, error) {
	if err := agentControlsAuthenticated(ctx); err != nil {
		return agentcontrols.Reply{}, err
	}
	if s == nil || s.Schedules == nil {
		return agentcontrols.Reply{}, agentcontrols.ErrUnavailable
	}
	part, err := s.Schedules.Preview(ctx, draft)
	if err != nil {
		return part, err
	}
	reply, err := s.Snapshot(ctx)
	if err != nil {
		return reply, err
	}
	mergeAgentOccurrencePreview(&reply, part)
	return reply, nil
}

func mergeAgentOccurrencePreview(reply *agentcontrols.Reply, preview agentcontrols.Reply) {
	for _, row := range preview.Snapshot.Schedules {
		if len(row.Occurrences) == 0 {
			continue
		}
		found := false
		for i := range reply.Snapshot.Schedules {
			if reply.Snapshot.Schedules[i].ID == row.ID {
				reply.Snapshot.Schedules[i].Occurrences = row.Occurrences
				reply.Snapshot.Schedules[i].OccurrenceKeys = row.OccurrenceKeys
				found = true
				break
			}
		}
		if !found {
			row.Actions = nil
			reply.Snapshot.Schedules = append(reply.Snapshot.Schedules, row)
		}
	}
}

func OverlayAgentControlsSurface(next http.Handler, surface agentcontrols.Surface, admission transport.Config) http.Handler {
	mux := http.NewServeMux()
	handler := agentcontrols.Handler{Surface: surface}
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _, err := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if err != nil {
			http.Error(w, "request denied", err.HTTPStatus())
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
	mux.Handle(agentcontrols.Path, endpoint)
	mux.Handle(agentcontrols.Path+"/", endpoint)
	mux.Handle("/", next)
	return mux
}
