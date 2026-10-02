package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentdemo"
)

type agentuxDemoBirthdayProfileFixture struct {
	tenant uuid.UUID
	worker string
	pref   agentstore.BirthdayPreference
	calls  int
}

func (f *agentuxDemoBirthdayProfileFixture) BirthdayPreference(_ context.Context, tenant uuid.UUID, worker string, demo bool) (agentstore.BirthdayPreference, error) {
	f.calls++
	f.tenant, f.worker = tenant, worker
	if f.pref.Revision == 0 {
		return agentstore.BirthdayPreference{Share: demo}, nil
	}
	return f.pref, nil
}
func (f *agentuxDemoBirthdayProfileFixture) SaveBirthdayPreference(_ context.Context, tenant uuid.UUID, worker string, p agentstore.BirthdayPreference, _ time.Time) (agentstore.BirthdayPreference, error) {
	f.calls++
	f.tenant, f.worker = tenant, worker
	if p.Revision != f.pref.Revision {
		return agentstore.BirthdayPreference{}, agentstore.ErrSupportInboxReplay
	}
	p.Revision++
	f.pref = p
	return p, nil
}

func TestAgentUXDemo_BirthdayProfile_Security(t *testing.T) {
	_, _, ctx, _ := agentuxDemoInboxSetup(t)
	tenant := uuid.New()
	f := &agentuxDemoBirthdayProfileFixture{}
	s := AgentUXDemoBirthdayProfile{Store: f, TenantUUID: func(values.TenantId) uuid.UUID { return tenant }, Now: time.Now}
	p, err := s.ReadBirthdayPreference(ctx)
	if err != nil || !p.ShareBirthday || p.Revision != 0 || f.tenant != tenant || f.worker != localAgentDemoAdmin {
		t.Fatalf("self projection: %+v %v worker=%s", p, err, f.worker)
	}
	p.ShareBirthday = false
	p, err = s.SaveBirthdayPreference(ctx, p)
	if err != nil || p.ShareBirthday || p.Revision != 1 || f.worker != localAgentDemoAdmin {
		t.Fatalf("self opt out: %+v %v", p, err)
	}
	if _, err := s.SaveBirthdayPreference(ctx, agentdemo.BirthdayPreference{ShareBirthday: true}); !errors.Is(err, agentdemo.ErrConflict) {
		t.Fatalf("stale opt in: %v", err)
	}
	before := f.calls
	if _, err := s.SaveBirthdayPreference(context.Background(), p); !errors.Is(err, agentdemo.ErrDenied) || f.calls != before {
		t.Fatalf("anonymous mutation: %v calls=%d", err, f.calls)
	}
}
