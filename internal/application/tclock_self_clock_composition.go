package application

import (
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/application/timeclockstore"
)

// SelfClockComposition contains only authoritative ports needed by the
// browser worker self-clock. Missing ports are a composition error; callers
// must not substitute memory projections or invented profiles.
type SelfClockComposition struct {
	Service  clockservice.Service
	Workers  timeclockstore.SelfWorkerSource
	Profiles timeclockstore.SelfProfileSource
	Store    timeclockstore.SelfProjectionSource
	Clock    func() time.Time
}

// WorkerSelfService builds the worker self-clock facade from durable sources
// and the existing governed punch workflow.
func (c SelfClockComposition) WorkerSelfService() (clockservice.WorkerSelfService, error) {
	if c.Workers == nil || c.Profiles == nil || c.Store == nil || c.Clock == nil || c.Service.PunchWorkflow == nil {
		return clockservice.WorkerSelfService{}, errors.New("self clock composition: authoritative worker, profile, projection and workflow ports are required")
	}
	return clockservice.WorkerSelfService{
		Workers:  timeclockstore.WorkerResolver{Source: c.Workers},
		Profiles: timeclockstore.ProfileResolver{Source: c.Profiles},
		Store:    timeclockstore.ProjectionStore{Source: c.Store},
		Actions:  timeclockstore.WorkflowActionExecutor{Service: c.Service, Clock: c.Clock, Projection: timeclockstore.ProjectionStore{Source: c.Store}},
		Clock:    c.Clock,
	}, nil
}
