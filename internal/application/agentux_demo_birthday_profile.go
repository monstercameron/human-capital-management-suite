package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentdemo"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type AgentUXDemoBirthdayProfileStore interface {
	AgentUXDemoBirthdayPreferences
	SaveBirthdayPreference(context.Context, uuid.UUID, string, agentstore.BirthdayPreference, time.Time) (agentstore.BirthdayPreference, error)
}
type AgentUXDemoBirthdayProfile struct {
	Store      AgentUXDemoBirthdayProfileStore
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

func (s AgentUXDemoBirthdayProfile) subject(ctx context.Context) (*trust.Principal, uuid.UUID, error) {
	if ctx == nil || s.Store == nil || s.TenantUUID == nil || s.Now == nil {
		return nil, uuid.Nil, agentdemo.ErrUnavailable
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant().Validate() != nil || p.Subject() == "" {
		return nil, uuid.Nil, agentdemo.ErrDenied
	}
	id := s.TenantUUID(p.Tenant())
	if id == uuid.Nil {
		return nil, uuid.Nil, agentdemo.ErrDenied
	}
	return p, id, nil
}

func (s AgentUXDemoBirthdayProfile) ReadBirthdayPreference(ctx context.Context) (agentdemo.BirthdayPreference, error) {
	p, id, err := s.subject(ctx)
	if err != nil {
		return agentdemo.BirthdayPreference{}, err
	}
	_, demo := demoworkforce.PackFor(p.Tenant().String())
	pref, err := s.Store.BirthdayPreference(ctx, id, p.Subject(), demo)
	if err != nil {
		return agentdemo.BirthdayPreference{}, agentdemo.ErrUnavailable
	}
	return agentdemo.BirthdayPreference{ShareBirthday: pref.Share, Revision: pref.Revision}, nil
}

func (s AgentUXDemoBirthdayProfile) SaveBirthdayPreference(ctx context.Context, input agentdemo.BirthdayPreference) (agentdemo.BirthdayPreference, error) {
	p, id, err := s.subject(ctx)
	if err != nil {
		return agentdemo.BirthdayPreference{}, err
	}
	pref, err := s.Store.SaveBirthdayPreference(ctx, id, p.Subject(), agentstore.BirthdayPreference{Share: input.ShareBirthday, Revision: input.Revision}, s.Now().UTC())
	if errors.Is(err, agentstore.ErrSupportInboxReplay) {
		return agentdemo.BirthdayPreference{}, agentdemo.ErrConflict
	}
	if err != nil {
		return agentdemo.BirthdayPreference{}, agentdemo.ErrUnavailable
	}
	return agentdemo.BirthdayPreference{ShareBirthday: pref.Share, Revision: pref.Revision}, nil
}

var _ agentdemo.BirthdayPreferenceSurface = AgentUXDemoBirthdayProfile{}
